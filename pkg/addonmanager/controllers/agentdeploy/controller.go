package agentdeploy

import (
	"context"
	"fmt"

	"github.com/open-cluster-management/addon-framework/pkg/agent"
	addonapiv1alpha1 "github.com/open-cluster-management/api/addon/v1alpha1"
	addonv1alpha1client "github.com/open-cluster-management/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "github.com/open-cluster-management/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "github.com/open-cluster-management/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1"
	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned"
	workinformers "github.com/open-cluster-management/api/client/work/informers/externalversions/work/v1"
	worklister "github.com/open-cluster-management/api/client/work/listers/work/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// AddonWorkLabel is to label the manifestowk relates to the addon
const (
	AddonWorkLabel = "open-cluster-management.io/addon-name"
)

// managedClusterController reconciles instances of ManagedCluster on the hub.
type addonDeployController struct {
	workClient                workv1client.Interface
	addonClient               addonv1alpha1client.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	workLister                worklister.ManifestWorkLister
	agentAddons               map[string]agent.AgentAddon
	eventRecorder             events.Recorder
}

func NewAddonDeployController(
	workClient workv1client.Interface,
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	workInformers workinformers.ManifestWorkInformer,
	agentAddons map[string]agent.AgentAddon,
	recorder events.Recorder,
) factory.Controller {
	c := &addonDeployController{
		workClient:                workClient,
		addonClient:               addonClient,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		workLister:                workInformers.Lister(),
		agentAddons:               agentAddons,
		eventRecorder:             recorder.WithComponentSuffix(fmt.Sprintf("addon-deploy-controller")),
	}

	return factory.New().WithInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, addonInformers.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return fmt.Sprintf("%s/%s", accessor.GetNamespace(), accessor.GetLabels()[AddonWorkLabel])
			},
			func(obj interface{}) bool {
				accessor, _ := meta.Accessor(obj)
				if accessor.GetLabels() == nil {
					return false
				}
				_, ok := accessor.GetLabels()[AddonWorkLabel]
				return ok
			},
			workInformers.Informer(),
		).
		WithSync(c.sync).ToController(fmt.Sprintf("addon-deploy-controller"), recorder)
}

func (c *addonDeployController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling addon deploy %q", key)

	clusterName, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	agentAddon, ok := c.agentAddons[addonName]
	if !ok {
		return nil
	}

	// Get ManagedCluster
	managedCluster, err := c.managedClusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	managedClusterAddon, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	owner := metav1.NewControllerRef(managedClusterAddon, addonapiv1alpha1.GroupVersion.WithKind("ManagedClusterAddOn"))

	managedClusterAddonCopy := managedClusterAddon.DeepCopy()
	objects, err := agentAddon.Manifests(managedCluster, managedClusterAddon)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return nil
	}

	work, err := buildManifestWorkFromObject(clusterName, addonName, objects)
	if err != nil {
		return err
	}
	work.OwnerReferences = []metav1.OwnerReference{*owner}

	// apply work
	work, err = c.applyWork(ctx, work)
	if err != nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    "ManifestApplied",
			Status:  metav1.ConditionFalse,
			Reason:  "ManifestWorkApplyFailed",
			Message: fmt.Sprintf("failed to apply manifestwork: %v", err),
		})
		c.addonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterAddonCopy.Namespace).UpdateStatus(
			ctx, managedClusterAddonCopy, metav1.UpdateOptions{})
		return err
	}

	// Update addon status based on work's status
	if meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkApplied) {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    "ManifestApplied",
			Status:  metav1.ConditionTrue,
			Reason:  "AddonManifestApplied",
			Message: "manifest of addon applied successfully",
		})
	} else {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    "ManifestApplied",
			Status:  metav1.ConditionFalse,
			Reason:  "AddonManifestAppliedFailed",
			Message: fmt.Sprintf("work %s apply failed", work.Name),
		})
	}

	if equality.Semantic.DeepEqual(managedClusterAddonCopy.Status, managedClusterAddon.Status) {
		return nil
	}

	_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterAddonCopy.Namespace).UpdateStatus(
		ctx, managedClusterAddonCopy, metav1.UpdateOptions{})
	return err
}

func (c *addonDeployController) applyWork(ctx context.Context, required *workapiv1.ManifestWork) (*workapiv1.ManifestWork, error) {
	existingWork, err := c.workLister.ManifestWorks(required.Namespace).Get(required.Name)
	existingWork = existingWork.DeepCopy()
	if errors.IsNotFound(err) {
		existingWork, err = c.workClient.WorkV1().ManifestWorks(required.Namespace).Create(ctx, required, metav1.CreateOptions{})
		if err == nil {
			c.eventRecorder.Eventf("ManifestWorkCreated", "Created %s/%s because it was missing", required.Namespace, required.Name)
			return existingWork, nil
		}
		c.eventRecorder.Warningf("ManifestWorkCreateFailed", "Failed to create ManifestWork %s/%s: %v", required.Namespace, required.Name, err)
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	if ManifestsEqual(existingWork.Spec.Workload.Manifests, required.Spec.Workload.Manifests) {
		return existingWork, nil
	}
	existingWork.Spec.Workload = required.Spec.Workload
	existingWork, err = c.workClient.WorkV1().ManifestWorks(existingWork.Namespace).Update(ctx, existingWork, metav1.UpdateOptions{})
	if err == nil {
		c.eventRecorder.Eventf("ManifestWorkUpdate", "Updated %s/%s because it was changing", required.Namespace, required.Name)
		return existingWork, nil
	}
	c.eventRecorder.Warningf("ManifestWorkUpdateFailed", "Failed to update ManifestWork %s/%s: %v", required.Namespace, required.Name, err)
	return nil, err
}

func buildManifestWorkFromObject(cluster, addonName string, objects []runtime.Object) (*workapiv1.ManifestWork, error) {
	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("addon-%s-deploy", addonName),
			Namespace: cluster,
			Labels: map[string]string{
				AddonWorkLabel: addonName,
			},
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: []workapiv1.Manifest{},
			},
		},
	}

	for _, object := range objects {
		rawObject, err := runtime.Encode(unstructured.UnstructuredJSONScheme, object)
		if err != nil {
			return nil, err
		}
		work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, workapiv1.Manifest{
			RawExtension: runtime.RawExtension{Raw: rawObject},
		})
	}

	return work, nil
}

func ManifestsEqual(new, old []workapiv1.Manifest) bool {
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
