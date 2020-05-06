package controllers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "github.com/open-cluster-management/api/client/work/informers/externalversions/work/v1"
	worklister "github.com/open-cluster-management/api/client/work/listers/work/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	"github.com/open-cluster-management/work/pkg/helper"
	"github.com/open-cluster-management/work/pkg/spoke/resource"
)

// ManifestWorkController is to reconcile the workload resources
// fetched from hub cluster on spoke cluster.
type ManifestWorkController struct {
	manifestWorkClient      workv1client.ManifestWorkInterface
	manifestWorkLister      worklister.ManifestWorkNamespaceLister
	spokeDynamicClient      dynamic.Interface
	spokeKubeclient         kubernetes.Interface
	spokeAPIExtensionClient apiextensionsclient.Interface
	// restMapper is a cached resource mapping obtained fron discovery client
	restMapper *resource.Mapper
}

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

const (
	manifestWorkFinalizer = "cluster.open-cluster-management.io/manifest-work-cleanup"
)

// NewManifestWorkController returns a ManifestWorkController
func NewManifestWorkController(
	ctx context.Context,
	recorder events.Recorder,
	spokeDynamicClient dynamic.Interface,
	spokeKubeClient kubernetes.Interface,
	spokeAPIExtensionClient apiextensionsclient.Interface,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	restMapper *resource.Mapper) factory.Controller {

	controller := &ManifestWorkController{
		manifestWorkClient:      manifestWorkClient,
		manifestWorkLister:      manifestWorkLister,
		spokeDynamicClient:      spokeDynamicClient,
		spokeKubeclient:         spokeKubeClient,
		spokeAPIExtensionClient: spokeAPIExtensionClient,
		restMapper:              restMapper,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}).
		WithInformers(manifestWorkInformer.Informer()).
		WithSync(controller.sync).ResyncEvery(5*time.Minute).ToController("ManifestWorkAgent", recorder)
}

// sync is the main reconcile loop for manefist work. It is triggered in two scenarios
// 1. ManifestWork API changes
// 2. Resources defined in manifest changed on spoke
func (m *ManifestWorkController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	manifestWorkName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ManifestWork %q", manifestWorkName)

	manifestWork, err := m.manifestWorkLister.Get(manifestWorkName)
	if err != nil {
		return err
	}
	manifestWork = manifestWork.DeepCopy()

	// Update finalizer at first
	if manifestWork.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range manifestWork.Finalizers {
			if manifestWork.Finalizers[i] == manifestWorkFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			manifestWork.Finalizers = append(manifestWork.Finalizers, manifestWorkFinalizer)
			_, err := m.manifestWorkClient.Update(ctx, manifestWork, metav1.UpdateOptions{})
			return err
		}
	}

	// Work is deleting, we remove its related resources on spoke cluster
	// TODO: once we make this work initially, the finalizer would live in a different loop.
	// It will have different backoff considerations.
	if !manifestWork.DeletionTimestamp.IsZero() {
		if errs := m.cleanupResourceOfWork(manifestWork); len(errs) != 0 {
			return utilerrors.NewAggregate(errs)
		}
		return m.removeWorkFinalizer(ctx, manifestWork)
	}

	errs := []error{}
	// Apply resources on spoke cluster.
	resourceResults := m.applyManifest(manifestWork.Spec.Workload.Manifests, controllerContext.Recorder())
	for index, result := range resourceResults {
		manifestCondition := helper.FindManifestConditionByIndex(int32(index), manifestWork.Status.ResourceStatus.Manifests)
		if manifestCondition == nil {
			manifestCondition = &workapiv1.ManifestCondition{
				ResourceMeta: workapiv1.ManifestResourceMeta{
					Ordinal: int32(index),
				},
				Conditions: []workapiv1.StatusCondition{},
			}
		}

		if result.Error != nil {
			errs = append(errs, result.Error)
			helper.SetStatusCondition(&manifestCondition.Conditions, workapiv1.StatusCondition{
				Type:    string(workapiv1.ManifestApplied),
				Status:  metav1.ConditionFalse,
				Reason:  "AppliedManifestFailed",
				Message: "Failed to apply manifest",
			})
		} else {
			helper.SetStatusCondition(&manifestCondition.Conditions, workapiv1.StatusCondition{
				Type:    string(workapiv1.ManifestApplied),
				Status:  metav1.ConditionTrue,
				Reason:  "AppliedManifestComplete",
				Message: "Apply manifest complete",
			})
		}
		helper.SetManifestCondition(&manifestWork.Status.ResourceStatus.Manifests, *manifestCondition)
	}

	// TODO find resources to be deleted if it is not owned by manifestwork any longer

	// Update work status
	_, _, err = helper.UpdateManifestWorkStatus(
		ctx, m.manifestWorkClient, manifestWork.Name, m.generateUpdateStatusFunc(manifestWork.Status.ResourceStatus))
	if err != nil {
		errs = append(errs, fmt.Errorf("Failed to update work status with err %w", err))
	}
	if len(errs) > 0 {
		err = utilerrors.NewAggregate(errs)
		klog.Errorf("Reconcile work %s fails with err %w: ", manifestWorkName, err)
	}
	return err
}

func (m *ManifestWorkController) cleanupResourceOfWork(work *workapiv1.ManifestWork) []error {
	errs := []error{}
	for _, manifest := range work.Spec.Workload.Manifests {
		gvr, object, err := m.decodeUnstructured(manifest.Raw)
		if err != nil {
			klog.Errorf("Failed to decode object: %w", err)
			errs = append(errs, err)
			continue
		}

		err = m.spokeDynamicClient.
			Resource(gvr).
			Namespace(object.GetNamespace()).
			Delete(context.TODO(), object.GetName(), metav1.DeleteOptions{})
		switch {
		case errors.IsNotFound(err):
			// no-oop
		case err != nil:
			errs = append(errs, fmt.Errorf(
				"Failed to delete resource %v with key %s/%s: %w",
				gvr, object.GetNamespace(), object.GetName(), err))
			continue
		}
		klog.V(4).Infof("Successfully delete resource %w with key %s/%s", gvr, object.GetNamespace(), object.GetName())
	}

	return errs
}

func (m *ManifestWorkController) removeWorkFinalizer(ctx context.Context, manifestWork *workapiv1.ManifestWork) error {
	copiedFinalizers := []string{}
	for i := range manifestWork.Finalizers {
		if manifestWork.Finalizers[i] == manifestWorkFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, manifestWork.Finalizers[i])
	}

	if len(manifestWork.Finalizers) != len(copiedFinalizers) {
		manifestWork.Finalizers = copiedFinalizers
		_, err := m.manifestWorkClient.Update(ctx, manifestWork, metav1.UpdateOptions{})
		return err
	}

	return nil
}

func (m *ManifestWorkController) applyManifest(manifests []runtime.RawExtension, recorder events.Recorder) []resourceapply.ApplyResult {
	clientHolder := resourceapply.NewClientHolder().
		WithAPIExtensionsClient(m.spokeAPIExtensionClient).
		WithKubernetes(m.spokeKubeclient)

	// Using index as the file name and apply manifests
	indexstrings := []string{}
	for i := 0; i < len(manifests); i++ {
		indexstrings = append(indexstrings, strconv.Itoa(i))
	}
	results := resourceapply.ApplyDirectly(clientHolder, recorder, func(name string) ([]byte, error) {
		index, _ := strconv.ParseInt(name, 10, 32)
		return manifests[index].Raw, nil
	}, indexstrings...)

	// Try apply with dynamic client if the manifest cannot be decoded by scheme or typed client is not found
	// TODO we should check the certain error.
	for index, result := range results {
		// Use dynamic client when scheme cannot decode manifest or typed client cannot handle the object
		if isDecodeError(result.Error) || isUnhandledError(result.Error) {
			results[index].Result, results[index].Changed, results[index].Error = m.applyUnstructrued(manifests[index].Raw, recorder)
		}
	}
	return results
}

func (m *ManifestWorkController) decodeUnstructured(data []byte) (schema.GroupVersionResource, *unstructured.Unstructured, error) {
	unstructuredObj := &unstructured.Unstructured{}
	err := unstructuredObj.UnmarshalJSON(data)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("Failed to decode object: %w", err)
	}
	mapping, err := m.restMapper.MappingForGVK(unstructuredObj.GroupVersionKind())
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("Failed to find grv from restmapping: %w", err)
	}

	return mapping.Resource, unstructuredObj, nil
}

func (m *ManifestWorkController) applyUnstructrued(data []byte, recorder events.Recorder) (*unstructured.Unstructured, bool, error) {
	gvr, required, err := m.decodeUnstructured(data)
	if err != nil {
		return nil, false, err
	}

	existing, err := m.spokeDynamicClient.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		actual, err := m.spokeDynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Create(
			context.TODO(), required, metav1.CreateOptions{})
		recorder.Eventf(fmt.Sprintf(
			"%s Created", required.GetKind()), "Created %s/%s because it was missing", required.GetNamespace(), required.GetName())
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	// Compare and update the unstrcuctured.
	if isSameUnstructured(required, existing) {
		return existing, false, nil
	}
	required.SetResourceVersion(existing.GetResourceVersion())
	actual, err := m.spokeDynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Update(
		context.TODO(), required, metav1.UpdateOptions{})
	recorder.Eventf(fmt.Sprintf(
		"%s Updated", required.GetKind()), "Updated %s/%s", required.GetNamespace(), required.GetName())
	return actual, true, err
}

func (m *ManifestWorkController) generateUpdateStatusFunc(manifestStatus workapiv1.ManifestResourceStatus) helper.UpdateManifestWorkStatusFunc {
	return func(oldStatus *workapiv1.ManifestWorkStatus) error {
		oldStatus.ResourceStatus = manifestStatus
		// TODO aggreagae manifest condition to generate work condition
		return nil
	}
}

// isDecodeError is to check if the error returned from resourceapply is due to that the object cannnot
// be decoded or no typed client can handle the object.
func isDecodeError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "cannot decode")
}

// isUnhandledError is to check if the error returned from resourceapply is due to that no typed
// client can handle the object
func isUnhandledError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "cannot decode")
}

// isSameUnstructured compares the two unstructured object.
// The comparison ignores the metadata and status field, and check if the two objects are sementically equal.
func isSameUnstructured(obj1, obj2 *unstructured.Unstructured) bool {
	obj1Copy := obj1.DeepCopy()
	obj2Copy := obj2.DeepCopy()

	// Comapre gvk, name, namespace at first
	if obj1Copy.GroupVersionKind() != obj2Copy.GroupVersionKind() {
		return false
	}
	if obj1Copy.GetName() != obj2Copy.GetName() {
		return false
	}
	if obj1Copy.GetNamespace() != obj2Copy.GetNamespace() {
		return false
	}

	// Compare label and annotations
	if !equality.Semantic.DeepEqual(obj1Copy.GetLabels(), obj2Copy.GetLabels()) {
		return false
	}
	if !equality.Semantic.DeepEqual(obj1Copy.GetAnnotations(), obj2Copy.GetAnnotations()) {
		return false
	}

	// Compare sementically after removing metadata and status field
	delete(obj1Copy.Object, "metadata")
	delete(obj2Copy.Object, "metadata")
	delete(obj1Copy.Object, "status")
	delete(obj2Copy.Object, "status")

	return equality.Semantic.DeepEqual(obj1Copy.Object, obj2Copy.Object)
}
