package agentdeploy

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func preDeleteHookWorkName(addonName string) string {
	return fmt.Sprintf("addon-%s-pre-delete", addonName)
}

func hasFinalizer(existingFinalizers []string, finalizer string) bool {
	for _, f := range existingFinalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

func removeFinalizer(existingFinalizers []string, finalizer string) []string {
	var rst []string
	for _, f := range existingFinalizers {
		if f != finalizer {
			rst = append(rst, f)
		}
	}
	return rst
}

func addFinalizer(existingFinalizers []string, finalizer string) []string {
	if existingFinalizers != nil {
		return append(existingFinalizers, finalizer)
	}
	return []string{finalizer}
}

func manifestsEqual(new, old []workapiv1.Manifest) bool {
	if len(new) != len(old) {
		return false
	}

	for i := range new {
		if !equality.Semantic.DeepEqual(new[i].Raw, old[i].Raw) {
			return false
		}
	}
	return true
}

func manifestWorkSpecEqual(new, old workapiv1.ManifestWorkSpec) bool {
	if !manifestsEqual(new.Workload.Manifests, old.Workload.Manifests) {
		return false
	}
	if !equality.Semantic.DeepEqual(new.ManifestConfigs, old.ManifestConfigs) {
		return false
	}
	if !equality.Semantic.DeepEqual(new.DeleteOption, old.DeleteOption) {
		return false
	}
	return true
}

func newManifestWork(addonNamespace, addonName, clusterName string, manifests []workapiv1.Manifest,
	manifestWorkNameFunc func(addonNamespace, addonName string) string) *workapiv1.ManifestWork {
	if len(manifests) == 0 {
		return nil
	}

	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      manifestWorkNameFunc(addonNamespace, addonName),
			Namespace: clusterName,
			Labels: map[string]string{
				constants.AddonLabel: addonName,
			},
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}

	// if the addon namespace is not equal with the manifestwork namespace(cluster name), add the addon namespace label
	if addonNamespace != clusterName {
		work.Labels[constants.AddonNamespaceLabel] = addonNamespace
	}
	return work
}

// isPreDeleteHookObject check the object is a pre-delete hook resources.
// currently, we only support job and pod as hook resources.
// we use WellKnownStatus here to get the job/pad status fields to check if the job/pod is completed.
func (b *manifestWorkBuiler) isPreDeleteHookObject(obj runtime.Object) (bool, *workapiv1.ManifestConfigOption) {
	var resource string
	gvk := obj.GetObjectKind().GroupVersionKind()
	switch gvk.Kind {
	case "Job":
		resource = "jobs"
	case "Pod":
		resource = "pods"
	default:
		return false, nil
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return false, nil
	}
	labels := accessor.GetLabels()
	if _, ok := labels[constants.PreDeleteHookLabel]; !ok {
		return false, nil
	}

	return true, &workapiv1.ManifestConfigOption{
		ResourceIdentifier: workapiv1.ResourceIdentifier{
			Group:     gvk.Group,
			Resource:  resource,
			Name:      accessor.GetName(),
			Namespace: accessor.GetNamespace(),
		},
		FeedbackRules: []workapiv1.FeedbackRule{
			{
				Type: workapiv1.WellKnownStatusType,
			},
		},
	}
}
func newManagedManifestWorkBuilder(hostedModeEnabled bool) *manifestWorkBuiler {
	return &manifestWorkBuiler{
		processor:         &managedManifest{},
		hostedModeEnabled: hostedModeEnabled,
	}
}

func newHostingManifestWorkBuilder(hostedModeEnabled bool) *manifestWorkBuiler {
	return &manifestWorkBuiler{
		processor:         &hostingManifest{},
		hostedModeEnabled: hostedModeEnabled,
	}
}

type manifestWorkBuiler struct {
	processor         manifestProcessor
	hostedModeEnabled bool
}

type manifestProcessor interface {
	deployable(hostedModeEnabled bool, installMode string, obj runtime.Object) (bool, error)
	manifestWorkName(addonNamespace, addonName string) string
	preDeleteHookManifestWorkName(addonNamespace, addonName string) string
}

// hostingManifest process manifests which will be deployed on the hosting cluster
type hostingManifest struct {
}

func (m *hostingManifest) deployable(hostedModeEnabled bool, installMode string, obj runtime.Object) (bool, error) {
	if !hostedModeEnabled {
		// hosted mode disabled, will not deploy any resource on the hosting cluster
		return false, nil
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return false, nil
	}

	location, exist, err := constants.GetHostedManifestLocation(accessor.GetLabels())
	if err != nil {
		return false, err
	}
	if installMode != constants.InstallModeHosted {
		return false, nil
	}

	if exist && location == constants.HostedManifestLocationHostingLabelValue {
		klog.V(4).Infof("will deploy the manifest %s/%s on the hosting cluster in Hosted mode",
			accessor.GetNamespace(), accessor.GetName())
		return true, nil
	}

	return false, nil
}

func (m *hostingManifest) manifestWorkName(addonNamespace, addonName string) string {
	return constants.DeployHostingWorkName(addonNamespace, addonName)
}

func (m *hostingManifest) preDeleteHookManifestWorkName(addonNamespace, addonName string) string {
	return fmt.Sprintf("%s-hosting-%s", preDeleteHookWorkName(addonName), addonNamespace)
}

// managedManifest process manifests which will be deployed on the managed cluster
type managedManifest struct {
}

func (m *managedManifest) deployable(hostedModeEnabled bool, installMode string, obj runtime.Object) (bool, error) {
	if !hostedModeEnabled {
		// hosted mode disabled, will deploy all resources on the managed cluster
		return true, nil
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return false, nil
	}

	location, exist, err := constants.GetHostedManifestLocation(accessor.GetLabels())
	if err != nil {
		return false, err
	}

	if installMode != constants.InstallModeHosted {
		return true, nil
	}

	if !exist || location == constants.HostedManifestLocationManagedLabelValue {
		klog.V(4).Infof("will deploy the manifest %s/%s on the managed cluster in Hosted mode",
			accessor.GetNamespace(), accessor.GetName())
		return true, nil
	}

	return false, nil
}

func (m *managedManifest) manifestWorkName(addonNamespace, addonName string) string {
	return constants.DeployWorkName(addonName)
}

func (m *managedManifest) preDeleteHookManifestWorkName(addonNamespace, addonName string) string {
	return preDeleteHookWorkName(addonName)
}

// buildManifestWorkFromObject returns the deploy manifestwork and preDelete manifestwork, if there is no manifest need
// to deploy, will return nil.
func (b *manifestWorkBuiler) buildManifestWorkFromObject(
	manifestWorkNamespace string,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	objects []runtime.Object) (deployManifestWork, hookManifestWork *workapiv1.ManifestWork, err error) {
	var deployManifests []workapiv1.Manifest
	var hookManifests []workapiv1.Manifest
	var manifestConfigs []workapiv1.ManifestConfigOption

	addonName := addon.Name
	installMode, _ := constants.GetHostedModeInfo(addon.GetAnnotations())

	for _, object := range objects {
		rawObject, err := runtime.Encode(unstructured.UnstructuredJSONScheme, object)
		if err != nil {
			return nil, nil, err
		}

		deployable, err := b.processor.deployable(b.hostedModeEnabled, installMode, object)
		if err != nil {
			return nil, nil, err
		}
		if !deployable {
			continue
		}

		isHookObject, manifestConfig := b.isPreDeleteHookObject(object)
		if isHookObject {
			hookManifests = append(hookManifests, workapiv1.Manifest{
				RawExtension: runtime.RawExtension{Raw: rawObject},
			})
			manifestConfigs = append(manifestConfigs, *manifestConfig)
		} else {
			deployManifests = append(deployManifests, workapiv1.Manifest{
				RawExtension: runtime.RawExtension{Raw: rawObject},
			})
		}
	}

	deployManifestWork = newManifestWork(addon.Namespace, addonName, manifestWorkNamespace,
		deployManifests, b.processor.manifestWorkName)
	hookManifestWork = newManifestWork(addon.Namespace, addonName, manifestWorkNamespace,
		hookManifests, b.processor.preDeleteHookManifestWorkName)
	if hookManifestWork != nil {
		hookManifestWork.Spec.ManifestConfigs = manifestConfigs
	}

	return deployManifestWork, hookManifestWork, nil
}

func applyWork(
	ctx context.Context,
	workClient workv1client.Interface,
	workLister worklister.ManifestWorkLister,
	cache *workCache,
	required *workapiv1.ManifestWork) (*workapiv1.ManifestWork, error) {
	existingWork, err := workLister.ManifestWorks(required.Namespace).Get(required.Name)
	existingWork = existingWork.DeepCopy()
	if err != nil {
		if errors.IsNotFound(err) {
			existingWork, err = workClient.WorkV1().ManifestWorks(required.Namespace).Create(ctx, required, metav1.CreateOptions{})
			if err == nil {
				cache.updateCache(required, existingWork)
				return existingWork, nil
			}
			return nil, err
		}
		return nil, err
	}

	if cache.safeToSkipApply(required, existingWork) {
		return existingWork, nil
	}

	if manifestWorkSpecEqual(required.Spec, existingWork.Spec) {
		return existingWork, nil
	}

	oldData, err := json.Marshal(&workapiv1.ManifestWork{
		Spec: existingWork.Spec,
	})
	if err != nil {
		return existingWork, err
	}

	newData, err := json.Marshal(&workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			UID:             existingWork.UID,
			ResourceVersion: existingWork.ResourceVersion,
		},
		Spec: required.Spec,
	})
	if err != nil {
		return existingWork, err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return existingWork, fmt.Errorf("failed to create patch for addon %s: %w", existingWork.Name, err)
	}

	klog.V(2).Infof("Patching work %s/%s with %s", existingWork.Namespace, existingWork.Name, string(patchBytes))
	updated, err := workClient.WorkV1().ManifestWorks(existingWork.Namespace).Patch(ctx, existingWork.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err == nil {
		cache.updateCache(required, existingWork)
		return updated, nil
	}
	return nil, err
}

func deleteWork(
	ctx context.Context,
	workClient workv1client.Interface,
	namespace, name string) error {

	err := workClient.WorkV1().ManifestWorks(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func FindManifestValue(
	resourceStatus workapiv1.ManifestResourceStatus,
	identifier workapiv1.ResourceIdentifier,
	valueName string) workapiv1.FieldValue {
	for _, manifest := range resourceStatus.Manifests {
		values := manifest.StatusFeedbacks.Values
		if len(values) == 0 {
			return workapiv1.FieldValue{}
		}
		resourceMeta := manifest.ResourceMeta
		if identifier.Group == resourceMeta.Group &&
			identifier.Resource == resourceMeta.Resource &&
			identifier.Name == resourceMeta.Name &&
			identifier.Namespace == resourceMeta.Namespace {
			for _, v := range values {
				if v.Name == valueName {
					return v.Value
				}
			}
		}
	}
	return workapiv1.FieldValue{}
}
