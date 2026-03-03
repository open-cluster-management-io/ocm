package addonmanagement

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	addonindex "open-cluster-management.io/ocm/pkg/addon/index"
)

type managedClusterAddonInstallReconciler struct {
	addonClient                addonv1alpha1client.Interface
	managedClusterAddonIndexer cache.Indexer
	managedClusterLister       clusterlisterv1.ManagedClusterLister
	placementLister            clusterlisterv1beta1.PlacementLister
	placementDecisionLister    clusterlisterv1beta1.PlacementDecisionLister
	addonFilterFunc            factory.EventFilterFunc
}

func (d *managedClusterAddonInstallReconciler) reconcile(
	ctx context.Context, cma *addonv1alpha1.ClusterManagementAddOn) (*addonv1alpha1.ClusterManagementAddOn, reconcileState, error) {
	logger := klog.FromContext(ctx)
	// skip apply install strategy for self-managed addon
	// this is to avoid conflict when addon also define WithInstallStrategy()
	// the filter will be removed after WithInstallStrategy() is removed from framework.
	if !d.addonFilterFunc(cma) {
		return cma, reconcileContinue, nil
	}

	if cma.Spec.InstallStrategy.Type == "" || cma.Spec.InstallStrategy.Type == addonv1alpha1.AddonInstallStrategyManual {
		return cma, reconcileContinue, nil
	}

	addons, err := d.managedClusterAddonIndexer.ByIndex(addonindex.ManagedClusterAddonByName, cma.Name)
	if err != nil {
		return cma, reconcileContinue, err
	}

	existingDeployed := sets.Set[string]{}
	for _, addonObject := range addons {
		addon := addonObject.(*addonv1alpha1.ManagedClusterAddOn)
		existingDeployed.Insert(addon.Namespace)
	}

	requiredDeployed, err := d.getAllDecisions(logger, cma.Name, cma.Spec.InstallStrategy.Placements)
	if err != nil {
		return cma, reconcileContinue, err
	}

	owner := metav1.NewControllerRef(cma, schema.GroupVersionKind{
		Group:   addonv1alpha1.GroupName,
		Version: addonv1alpha1.GroupVersion.Version,
		Kind:    "ClusterManagementAddOn",
	})
	toAdd := requiredDeployed.Difference(existingDeployed)
	toRemove := existingDeployed.Difference(requiredDeployed)

	var errs []error
	for cluster := range toAdd {
		addon := &addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:            cma.Name,
				Namespace:       cluster,
				OwnerReferences: []metav1.OwnerReference{*owner},
			},
			Spec: addonv1alpha1.ManagedClusterAddOnSpec{},
		}

		// Check if the managed cluster is in hosted mode and add the hosting cluster name annotation
		if hostingClusterName := d.getHostingClusterName(cluster, logger); hostingClusterName != "" {
			addon.Annotations = map[string]string{
				addonv1alpha1.HostingClusterNameAnnotationKey: hostingClusterName,
			}
			logger.V(2).Info("Adding hosting cluster name annotation to addon",
				"cluster", cluster, "hostingCluster", hostingClusterName, "addon", cma.Name)
		}

		_, err := d.addonClient.AddonV1alpha1().ManagedClusterAddOns(cluster).Create(ctx, addon, metav1.CreateOptions{})
		if err != nil && !errors.IsAlreadyExists(err) {
			errs = append(errs, err)
		}
	}

	for cluster := range toRemove {
		err := d.addonClient.AddonV1alpha1().ManagedClusterAddOns(cluster).Delete(ctx, cma.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			errs = append(errs, err)
		}
	}

	return cma, reconcileContinue, utilerrors.NewAggregate(errs)
}

// getHostingClusterName returns the hosting cluster name from the ManagedCluster's
// addon.open-cluster-management.io/hosting-cluster-name annotation, or empty string if not set.
func (d *managedClusterAddonInstallReconciler) getHostingClusterName(clusterName string, logger klog.Logger) string {
	cluster, err := d.managedClusterLister.Get(clusterName)
	if err != nil {
		logger.Error(err, "failed to get cluster")
		return ""
	}

	return cluster.Annotations[addonv1alpha1.HostingClusterNameAnnotationKey]
}

func (d *managedClusterAddonInstallReconciler) getAllDecisions(
	logger klog.Logger,
	addonName string,
	placements []addonv1alpha1.PlacementStrategy) (sets.Set[string], error) {
	var errs []error
	required := sets.Set[string]{}
	for _, strategy := range placements {
		_, err := d.placementLister.Placements(strategy.PlacementRef.Namespace).Get(strategy.PlacementRef.Name)
		if errors.IsNotFound(err) {
			logger.V(2).Info("Placement not found for addon", "placementNamespace",
				strategy.PlacementRef.Namespace, "placementName", strategy.PlacementRef.Name, "addonName", addonName)
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}

		decisionSelector := labels.SelectorFromSet(labels.Set{
			clusterv1beta1.PlacementLabel: strategy.PlacementRef.Name,
		})
		decisions, err := d.placementDecisionLister.PlacementDecisions(strategy.PlacementRef.Namespace).List(decisionSelector)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		for _, d := range decisions {
			for _, sd := range d.Status.Decisions {
				required.Insert(sd.ClusterName)
			}
		}
	}

	return required, utilerrors.NewAggregate(errs)
}
