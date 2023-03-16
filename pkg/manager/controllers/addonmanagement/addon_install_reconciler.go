package addonmanagement

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	"open-cluster-management.io/addon-framework/pkg/index"
)

type managedClusterAddonInstallReconciler struct {
	addonClient                addonv1alpha1client.Interface
	managedClusterAddonIndexer cache.Indexer
	placementLister            clusterlisterv1beta1.PlacementLister
	placementDecisionLister    clusterlisterv1beta1.PlacementDecisionLister
}

func (d *managedClusterAddonInstallReconciler) reconcile(
	ctx context.Context, cma *addonv1alpha1.ClusterManagementAddOn) (*addonv1alpha1.ClusterManagementAddOn, reconcileState, error) {
	if cma.Spec.InstallStrategy.Type == "" || cma.Spec.InstallStrategy.Type == addonv1alpha1.AddonInstallStrategyManual {
		return cma, reconcileContinue, nil
	}

	addons, err := d.managedClusterAddonIndexer.ByIndex(index.ManagedClusterAddonByName, cma.Name)
	if err != nil {
		return cma, reconcileContinue, err
	}

	existingDeployed := sets.Set[string]{}
	for _, addonObject := range addons {
		addon := addonObject.(*addonv1alpha1.ManagedClusterAddOn)
		existingDeployed.Insert(addon.Namespace)
	}

	requiredDeployed, err := d.getAllDecisions(cma.Name, cma.Spec.InstallStrategy.Placements)
	if err != nil {
		return cma, reconcileContinue, err
	}

	owner := metav1.NewControllerRef(cma, addonv1alpha1.GroupVersion.WithKind("ClusterManagementAddOn"))
	toAdd := requiredDeployed.Difference(existingDeployed)
	toRemove := existingDeployed.Difference(requiredDeployed)

	var errs []error
	for cluster := range toAdd {
		_, err := d.addonClient.AddonV1alpha1().ManagedClusterAddOns(cluster).Create(ctx, &addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:            cma.Name,
				Namespace:       cluster,
				OwnerReferences: []metav1.OwnerReference{*owner},
			},
			Spec: addonv1alpha1.ManagedClusterAddOnSpec{},
		}, metav1.CreateOptions{})

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

func (d *managedClusterAddonInstallReconciler) getAllDecisions(addonName string, placements []addonv1alpha1.PlacementStrategy) (sets.Set[string], error) {
	var errs []error
	required := sets.Set[string]{}
	for _, strategy := range placements {
		_, err := d.placementLister.Placements(strategy.PlacementRef.Namespace).Get(strategy.PlacementRef.Name)
		if errors.IsNotFound(err) {
			klog.V(2).Infof("placement %s/%s is not found for addon %s", strategy.PlacementRef.Namespace, strategy.PlacementRef.Name, addonName)
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
