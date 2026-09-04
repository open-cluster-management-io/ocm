package agentdeploy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addoninformerv1beta1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1beta1"
	addonlisterv1beta1 "open-cluster-management.io/api/client/addon/listers/addon/v1beta1"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta2 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta2"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/index"
)

var excludedAutoDiscoveryClusterSets = map[string]struct{}{
	"default": {},
	"global":  {},
}

type hostedModeResolver struct {
	controller                   *addonDeployController
	clusterManagementAddonLister addonlisterv1beta1.ClusterManagementAddOnLister
	managedClusterSetLister      clusterlisterv1beta2.ManagedClusterSetLister
	managedClusterSetInformer    cache.SharedIndexInformer
	informerStartOnce            sync.Once
}

// WithHostedModeAutoDiscovery enables KEP-188 hosting-cluster resolution for the deploy
// controller. It owns the indexes and informers discovery needs, so callers that do not host
// addons can omit this option and do not need ManagedClusterSet read permissions.
func WithHostedModeAutoDiscovery(
	clusterManagementAddonInformer addoninformerv1beta1.ClusterManagementAddOnInformer,
	clusterClient clusterclient.Interface,
) AddonDeployControllerOption {
	return func(controller *addonDeployController) error {
		if err := addMissingIndexers(controller.managedClusterAddonIndexer, cache.Indexers{
			index.ManagedClusterAddonByName:                   index.IndexManagedClusterAddonByName,
			index.ManagedClusterAddonByHostedMode:             index.IndexManagedClusterAddonByHostedMode,
			index.ManagedClusterAddonByDeclaredHostingCluster: index.IndexManagedClusterAddonByDeclaredHostingCluster,
		}); err != nil {
			return fmt.Errorf("failed to configure hosted mode addon indexes: %w", err)
		}
		if err := addMissingIndexers(controller.managedClusterIndexer, cache.Indexers{
			index.ManagedClusterByHostingCluster: index.IndexManagedClusterByHostingCluster,
		}); err != nil {
			return fmt.Errorf("failed to configure hosted mode cluster indexes: %w", err)
		}

		managedClusterSetInformer := clusterinformerv1beta2.NewManagedClusterSetInformer(
			clusterClient, 10*time.Minute, cache.Indexers{})
		controller.hostedModeResolver = &hostedModeResolver{
			controller:                   controller,
			clusterManagementAddonLister: clusterManagementAddonInformer.Lister(),
			managedClusterSetLister: clusterlisterv1beta2.NewManagedClusterSetLister(
				managedClusterSetInformer.GetIndexer()),
			managedClusterSetInformer: managedClusterSetInformer,
		}
		controller.discoveryInformers = append(controller.discoveryInformers,
			clusterManagementAddonInformer.Informer())
		controller.setHostedModeDiscoveryAddonHandler(clusterManagementAddonInformer)
		controller.setHostedModeDiscoveryClusterSetHandler(managedClusterSetInformer)
		return nil
	}
}

func addMissingIndexers(indexer cache.Indexer, desired cache.Indexers) error {
	missing := cache.Indexers{}
	existing := indexer.GetIndexers()
	for name, indexFunc := range desired {
		if _, found := existing[name]; !found {
			missing[name] = indexFunc
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return indexer.AddIndexers(missing)
}

// resolve returns stop=true when reconciliation must wait for discovery or when an annotation
// update was sent to the API. Annotation updates are deliberately one-shot and use resourceVersion
// conflict handling from Update rather than overwriting a concurrent human choice.
func (r *hostedModeResolver) resolve(
	ctx context.Context,
	agentAddon agent.AgentAddon,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn,
) (*addonapiv1beta1.ManagedClusterAddOn, bool, error) {
	installModeHosted := addon.Annotations[addonapiv1beta1.InstallModeAnnotationKey] == constants.InstallModeHosted
	hostingClusterName := addon.Annotations[addonapiv1beta1.HostingClusterNameAnnotationKey]
	managedByDiscovery := addon.Annotations[addonapiv1beta1.HostingClusterNameManagedByAnnotationKey] ==
		addonapiv1beta1.HostingClusterNameManagedByAutoDiscoveryValue

	if managedByDiscovery && !installModeHosted && addon.DeletionTimestamp.IsZero() {
		updated := addon.DeepCopy()
		delete(updated.Annotations, addonapiv1beta1.HostingClusterNameAnnotationKey)
		delete(updated.Annotations, addonapiv1beta1.HostingClusterNameManagedByAnnotationKey)
		return addon, true, r.updateAddon(ctx, updated)
	}

	// A nonempty value is an explicit decision (human-set or previously discovered). Discovery is
	// one-shot, so claim and ClusterSet changes only affect the normal validity check from here on.
	if hostingClusterName != "" {
		return addon, false, nil
	}

	if !installModeHosted {
		return removeHostingClusterValidityCondition(addon), false, nil
	}

	if !agentAddon.GetAgentAddonOptions().HostedModeEnabled ||
		!r.autoDiscoveryEnabled(addon.Name) ||
		!addon.DeletionTimestamp.IsZero() {
		return removeAutoDiscoveryPendingCondition(addon), false, nil
	}

	r.startManagedClusterSetInformer(ctx)
	if !r.managedClusterSetCacheSynced() {
		return setAutoDiscoveryPending(addon, "waiting for the ManagedClusterSet cache to sync"), true, nil
	}

	claimedHostingCluster, ok := selfReportedHostingCluster(cluster)
	if !ok {
		return setAutoDiscoveryPending(addon,
			fmt.Sprintf("managed cluster %s has not reported a hosting cluster", cluster.Name)), true, nil
	}

	hostingCluster, err := r.controller.managedClusterLister.Get(claimedHostingCluster)
	if errors.IsNotFound(err) {
		return setAutoDiscoveryPending(addon,
			fmt.Sprintf("self-reported hosting cluster %s is not a managed cluster of the hub", claimedHostingCluster)), true, nil
	}
	if err != nil {
		return addon, true, err
	}
	if !cluster.DeletionTimestamp.IsZero() || !hostingCluster.DeletionTimestamp.IsZero() {
		return setAutoDiscoveryPending(addon,
			fmt.Sprintf("managed cluster %s or hosting cluster %s is being deleted",
				cluster.Name, hostingCluster.Name)), true, nil
	}

	shared, err := r.shareManagedClusterSet(ctx, cluster, hostingCluster)
	if err != nil {
		return addon, true, err
	}
	if !shared {
		return setAutoDiscoveryPending(addon,
			fmt.Sprintf("managed cluster %s and hosting cluster %s do not share a ManagedClusterSet",
				cluster.Name, hostingCluster.Name)), true, nil
	}

	updated := addon.DeepCopy()
	if updated.Annotations == nil {
		updated.Annotations = map[string]string{}
	}
	updated.Annotations[addonapiv1beta1.HostingClusterNameAnnotationKey] = claimedHostingCluster
	updated.Annotations[addonapiv1beta1.HostingClusterNameManagedByAnnotationKey] =
		addonapiv1beta1.HostingClusterNameManagedByAutoDiscoveryValue
	return addon, true, r.updateAddon(ctx, updated)
}

// updateAddon writes a resolver-owned annotation change back to the hub. The update is
// deliberately a full Update instead of a patch: a concurrent human edit makes it fail on
// resourceVersion and the resolution is retried against the fresh object.
func (r *hostedModeResolver) updateAddon(ctx context.Context, addon *addonapiv1beta1.ManagedClusterAddOn) error {
	_, err := r.controller.addonClient.AddonV1beta1().ManagedClusterAddOns(addon.Namespace).
		Update(ctx, addon, metav1.UpdateOptions{})
	return err
}

// startManagedClusterSetInformer starts the controller-wide discovery cache on the first unresolved
// auto-discovery request. It is deliberately not part of the deploy controller's required caches:
// legacy hosted addons must keep reconciling when auto-discovery is unused or lacks RBAC. ctx is
// the deploy controller's queue context, so it lives as long as the controller does.
func (r *hostedModeResolver) startManagedClusterSetInformer(ctx context.Context) {
	if r.managedClusterSetInformer == nil {
		return
	}
	r.informerStartOnce.Do(func() {
		go r.managedClusterSetInformer.Run(ctx.Done())
		go func() {
			if cache.WaitForCacheSync(ctx.Done(), r.managedClusterSetInformer.HasSynced) {
				r.controller.enqueueHostedModeAddons()
			}
		}()
	})
}

func (r *hostedModeResolver) managedClusterSetCacheSynced() bool {
	return r.managedClusterSetInformer == nil || r.managedClusterSetInformer.HasSynced()
}

func (r *hostedModeResolver) autoDiscoveryEnabled(addonName string) bool {
	cma, err := r.clusterManagementAddonLister.Get(addonName)
	if err != nil {
		return false
	}

	if cma.Spec.HostedModeAutoDiscovery != nil {
		return cma.Spec.HostedModeAutoDiscovery.Mode == addonapiv1beta1.HostedModeAutoDiscoveryModeEnable
	}

	// Fallback for hubs that don't round-trip the typed field yet.
	return cma.Annotations[constants.HostedModeAutoDiscoveryAnnotationKey] ==
		string(addonapiv1beta1.HostedModeAutoDiscoveryModeEnable)
}

func (r *hostedModeResolver) shouldOnlyCleanup(
	agentAddon agent.AgentAddon,
	addon *addonapiv1beta1.ManagedClusterAddOn,
) bool {
	return !addon.DeletionTimestamp.IsZero() &&
		agentAddon.GetAgentAddonOptions().HostedModeEnabled &&
		addon.Annotations[addonapiv1beta1.InstallModeAnnotationKey] == constants.InstallModeHosted &&
		addon.Annotations[addonapiv1beta1.HostingClusterNameAnnotationKey] == ""
}

func (r *hostedModeResolver) shareManagedClusterSet(
	ctx context.Context,
	target, hosting *clusterv1.ManagedCluster,
) (bool, error) {
	clusterSets, err := r.managedClusterSetLister.List(labels.Everything())
	if err != nil {
		return false, err
	}

	shared := false
	for _, clusterSet := range clusterSets {
		if _, excluded := excludedAutoDiscoveryClusterSets[clusterSet.Name]; excluded ||
			!clusterSet.DeletionTimestamp.IsZero() {
			continue
		}

		matches, err := managedClusterSetMatchesBoth(clusterSet, target, hosting)
		if err != nil {
			klog.FromContext(ctx).Error(err, "Skipping ManagedClusterSet with an invalid cluster selector",
				"managedClusterSet", clusterSet.Name)
			continue
		}
		if matches {
			shared = true
		}
	}

	return shared, nil
}

func managedClusterSetMatchesBoth(
	clusterSet *clusterv1beta2.ManagedClusterSet,
	target, hosting *clusterv1.ManagedCluster,
) (bool, error) {
	switch clusterSet.Spec.ClusterSelector.SelectorType {
	case "", clusterv1beta2.ExclusiveClusterSetLabel:
		return target.Labels[clusterv1beta2.ClusterSetLabel] == clusterSet.Name &&
			hosting.Labels[clusterv1beta2.ClusterSetLabel] == clusterSet.Name, nil
	case clusterv1beta2.LabelSelector:
		if clusterSet.Spec.ClusterSelector.LabelSelector == nil {
			return false, fmt.Errorf("labelSelector is required for selector type %s", clusterv1beta2.LabelSelector)
		}
		selector, err := metav1.LabelSelectorAsSelector(clusterSet.Spec.ClusterSelector.LabelSelector)
		if err != nil {
			return false, err
		}
		return selector.Matches(labels.Set(target.Labels)) && selector.Matches(labels.Set(hosting.Labels)), nil
	default:
		return false, fmt.Errorf("unsupported selector type %q", clusterSet.Spec.ClusterSelector.SelectorType)
	}
}

func setAutoDiscoveryPending(addon *addonapiv1beta1.ManagedClusterAddOn, message string) *addonapiv1beta1.ManagedClusterAddOn {
	updated := addon.DeepCopy()
	meta.SetStatusCondition(&updated.Status.Conditions, metav1.Condition{
		Type:    addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity,
		Status:  metav1.ConditionFalse,
		Reason:  addonapiv1beta1.HostingClusterValidityReasonAutoDiscoveryPending,
		Message: message,
	})
	return updated
}

func removeAutoDiscoveryPendingCondition(addon *addonapiv1beta1.ManagedClusterAddOn) *addonapiv1beta1.ManagedClusterAddOn {
	condition := meta.FindStatusCondition(addon.Status.Conditions,
		addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity)
	if condition == nil || condition.Reason != addonapiv1beta1.HostingClusterValidityReasonAutoDiscoveryPending {
		return addon
	}

	return removeHostingClusterValidityCondition(addon)
}

func removeHostingClusterValidityCondition(addon *addonapiv1beta1.ManagedClusterAddOn) *addonapiv1beta1.ManagedClusterAddOn {
	if meta.FindStatusCondition(addon.Status.Conditions,
		addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity) == nil {
		return addon
	}

	updated := addon.DeepCopy()
	meta.RemoveStatusCondition(&updated.Status.Conditions,
		addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity)
	return updated
}

func (c *addonDeployController) setHostedModeDiscoveryAddonHandler(
	informer addoninformerv1beta1.ClusterManagementAddOnInformer,
) {
	_, err := informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueAddonsByName(objectName(obj))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldCMA, oldOK := oldObj.(*addonapiv1beta1.ClusterManagementAddOn)
			newCMA, newOK := newObj.(*addonapiv1beta1.ClusterManagementAddOn)
			if !oldOK || !newOK {
				return
			}
			if !equality.Semantic.DeepEqual(
				oldCMA.Spec.HostedModeAutoDiscovery, newCMA.Spec.HostedModeAutoDiscovery) ||
				oldCMA.Annotations[constants.HostedModeAutoDiscoveryAnnotationKey] !=
					newCMA.Annotations[constants.HostedModeAutoDiscoveryAnnotationKey] {
				c.enqueueAddonsByName(newCMA.Name)
			}
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueueAddonsByName(objectName(obj))
		},
	})
	if err != nil {
		utilruntime.HandleError(err)
	}
}

func (c *addonDeployController) setHostedModeDiscoveryClusterSetHandler(
	informer cache.SharedIndexInformer,
) {
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(interface{}) {
			c.enqueueHostedModeAddons()
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldClusterSet, oldOK := oldObj.(*clusterv1beta2.ManagedClusterSet)
			newClusterSet, newOK := newObj.(*clusterv1beta2.ManagedClusterSet)
			if oldOK && newOK && (!equality.Semantic.DeepEqual(oldClusterSet.Spec, newClusterSet.Spec) ||
				!equality.Semantic.DeepEqual(oldClusterSet.DeletionTimestamp, newClusterSet.DeletionTimestamp)) {
				c.enqueueHostedModeAddons()
			}
		},
		DeleteFunc: func(interface{}) {
			c.enqueueHostedModeAddons()
		},
	})
	if err != nil {
		utilruntime.HandleError(err)
	}
}

func (c *addonDeployController) setHostedModeDiscoveryManagedClusterHandler(
	informer clusterinformerv1.ManagedClusterInformer,
) {
	if c.hostedModeResolver == nil {
		return
	}

	_, err := informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cluster, ok := obj.(*clusterv1.ManagedCluster)
			if !ok {
				return
			}
			c.enqueueAddonsByClusterName(cluster.Name)
			c.enqueueAddonsClaimingHost(cluster.Name)
			c.enqueueAddonsByDeclaredHostingCluster(cluster.Name)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldCluster, oldOK := oldObj.(*clusterv1.ManagedCluster)
			newCluster, newOK := newObj.(*clusterv1.ManagedCluster)
			if !oldOK || !newOK {
				return
			}

			oldClaim, oldHasClaim := selfReportedHostingCluster(oldCluster)
			newClaim, newHasClaim := selfReportedHostingCluster(newCluster)
			if oldHasClaim != newHasClaim || oldClaim != newClaim {
				c.enqueueAddonsByClusterName(newCluster.Name)
			}
			deletionChanged := !equality.Semantic.DeepEqual(
				oldCluster.DeletionTimestamp, newCluster.DeletionTimestamp)
			if !equality.Semantic.DeepEqual(oldCluster.Labels, newCluster.Labels) || deletionChanged {
				c.enqueueAddonsByClusterName(newCluster.Name)
				c.enqueueAddonsClaimingHost(newCluster.Name)
				c.enqueueAddonsByDeclaredHostingCluster(newCluster.Name)
			}
			if deletionChanged {
				c.enqueueHostedModeAddons()
			}
		},
		DeleteFunc: c.handleHostedModeDiscoveryManagedClusterDelete,
	})
	if err != nil {
		utilruntime.HandleError(err)
	}
}

func (c *addonDeployController) handleHostedModeDiscoveryManagedClusterDelete(obj interface{}) {
	clusterName := objectName(obj)
	c.enqueueAddonsByClusterName(clusterName)
	c.enqueueAddonsClaimingHost(clusterName)
	c.enqueueAddonsByDeclaredHostingCluster(clusterName)
	c.enqueueHostedModeAddons()
}

func (c *addonDeployController) enqueueAddonsByDeclaredHostingCluster(hostingClusterName string) {
	if hostingClusterName == "" {
		return
	}
	items, err := c.managedClusterAddonIndexer.ByIndex(
		index.ManagedClusterAddonByDeclaredHostingCluster, hostingClusterName)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.enqueueAddons(items)
}

func (c *addonDeployController) enqueueAddonsClaimingHost(hostingClusterName string) {
	if hostingClusterName == "" {
		return
	}
	targets, err := c.managedClusterIndexer.ByIndex(index.ManagedClusterByHostingCluster, hostingClusterName)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	for _, target := range targets {
		c.enqueueAddonsByClusterName(objectName(target))
	}
}

func (c *addonDeployController) enqueueAddonsByName(addonName string) {
	if addonName == "" {
		return
	}
	items, err := c.managedClusterAddonIndexer.ByIndex(index.ManagedClusterAddonByName, addonName)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.enqueueAddons(items)
}

func (c *addonDeployController) enqueueHostedModeAddons() {
	items, err := c.managedClusterAddonIndexer.ByIndex(index.ManagedClusterAddonByHostedMode, index.HostedModeIndexKey)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.enqueueAddons(items)
}

func (c *addonDeployController) enqueueAddons(items []interface{}) {
	for _, item := range items {
		key, err := cache.MetaNamespaceKeyFunc(item)
		if err != nil {
			utilruntime.HandleError(err)
			continue
		}
		c.queue.Add(key)
	}
}

func objectName(obj interface{}) string {
	switch tombstone := obj.(type) {
	case cache.DeletedFinalStateUnknown:
		obj = tombstone.Obj
	case *cache.DeletedFinalStateUnknown:
		obj = tombstone.Obj
	}
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return ""
	}
	return accessor.GetName()
}
