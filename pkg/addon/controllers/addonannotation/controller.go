package addonannotation

import (
	"context"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformersv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

// addonAnnotationController watches ManagedCluster annotation changes and syncs
// annotations with the addon prefix to ManagedClusterAddOns in the cluster namespace.
type addonAnnotationController struct {
	addonClient                  addonv1alpha1client.Interface
	managedClusterLister         clusterlisterv1.ManagedClusterLister
	managedClusterAddonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
}

func NewAddonAnnotationController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	managedClusterInformer clusterinformersv1.ManagedClusterInformer,
) factory.Controller {
	c := &addonAnnotationController{
		addonClient:                  addonClient,
		managedClusterLister:         managedClusterInformer.Lister(),
		managedClusterAddonLister:    addonInformers.Lister(),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
	}

	syncCtx := factory.NewSyncContext("addon-annotation-controller")

	// Register a custom event handler that only enqueues when addon-prefix annotations change
	_, err := managedClusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// No need to sync annotations when a cluster is added
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldAccessor, err := meta.Accessor(oldObj)
			if err != nil {
				return
			}
			newAccessor, err := meta.Accessor(newObj)
			if err != nil {
				return
			}
			if addonAnnotationsChanged(oldAccessor.GetAnnotations(), newAccessor.GetAnnotations()) {
				syncCtx.Queue().Add(newAccessor.GetName())
			}
		},
		DeleteFunc: func(obj interface{}) {
			// No need to sync annotations when a cluster is deleted
		},
	})
	if err != nil {
		return nil
	}

	return factory.New().
		WithSyncContext(syncCtx).
		WithBareInformers(managedClusterInformer.Informer()).
		WithSync(c.sync).
		ToController("addon-annotation-controller")
}

// getAddonAnnotations extracts annotations with the addon prefix from a map.
func getAddonAnnotations(annotations map[string]string) map[string]string {
	result := map[string]string{}
	for k, v := range annotations {
		if strings.HasPrefix(k, addonv1alpha1.GroupName) {
			result[k] = v
		}
	}
	return result
}

// addonAnnotationsChanged returns true if the addon-prefix annotations differ between old and new.
func addonAnnotationsChanged(oldAnnotations, newAnnotations map[string]string) bool {
	oldAddon := getAddonAnnotations(oldAnnotations)
	newAddon := getAddonAnnotations(newAnnotations)

	if len(oldAddon) != len(newAddon) {
		return true
	}
	for k, v := range oldAddon {
		if newV, exists := newAddon[k]; !exists || newV != v {
			return true
		}
	}
	return false
}

func (c *addonAnnotationController) sync(ctx context.Context, syncCtx factory.SyncContext, clusterName string) error {
	logger := klog.FromContext(ctx).WithValues("cluster", clusterName)
	logger.V(4).Info("Reconciling addon annotations for cluster")

	cluster, err := c.managedClusterLister.Get(clusterName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	// Extract annotations with the addon prefix from the managed cluster
	clusterAddonAnnotations := map[string]string{}
	for k, v := range cluster.Annotations {
		if strings.HasPrefix(k, addonv1alpha1.GroupName) {
			clusterAddonAnnotations[k] = v
		}
	}

	// List all addons in the cluster namespace
	addons, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).List(labels.Everything())
	if err != nil {
		return err
	}

	var errs []error
	for _, addon := range addons {
		// Get the ClusterManagementAddOn for this addon
		cma, err := c.clusterManagementAddonLister.Get(addon.Name)
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}

		// Skip addons whose ClusterManagementAddOn install strategy type is empty or manual
		if cma.Spec.InstallStrategy.Type == "" || cma.Spec.InstallStrategy.Type == addonv1alpha1.AddonInstallStrategyManual {
			continue
		}

		// Sync addon annotations from cluster to addon
		if updated, addonCopy := syncAddonAnnotations(addon, clusterAddonAnnotations); updated {
			logger.V(2).Info("Updating addon annotations", "addon", addon.Name)
			_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Update(
				ctx, addonCopy, metav1.UpdateOptions{})
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	return utilerrors.NewAggregate(errs)
}

// syncAddonAnnotations syncs annotations with the addon prefix from the cluster to the addon.
// It returns true and the updated addon copy if there is a change.
func syncAddonAnnotations(
	addon *addonv1alpha1.ManagedClusterAddOn,
	clusterAddonAnnotations map[string]string,
) (bool, *addonv1alpha1.ManagedClusterAddOn) {
	addonCopy := addon.DeepCopy()
	if addonCopy.Annotations == nil {
		addonCopy.Annotations = map[string]string{}
	}

	changed := false

	// Remove addon-prefix annotations from addon that are no longer on the cluster
	for k := range addonCopy.Annotations {
		if strings.HasPrefix(k, addonv1alpha1.GroupName) {
			if _, exists := clusterAddonAnnotations[k]; !exists {
				delete(addonCopy.Annotations, k)
				changed = true
			}
		}
	}

	// Add or update addon-prefix annotations from the cluster
	for k, v := range clusterAddonAnnotations {
		if existing, exists := addonCopy.Annotations[k]; !exists || existing != v {
			addonCopy.Annotations[k] = v
			changed = true
		}
	}

	return changed, addonCopy
}
