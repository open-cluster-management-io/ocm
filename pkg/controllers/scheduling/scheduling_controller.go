package scheduling

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1alpha1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
)

const (
	clusterSetLabel          = "cluster.open-cluster-management.io/clusterset"
	placementLabel           = "cluster.open-cluster-management.io/placement"
	schedulingControllerName = "SchedulingController"
)

type enqueuePlacementFunc func(namespace, name string)

// schedulingController schedules cluster decisions for Placements
type schedulingController struct {
	clusterClient           clusterclient.Interface
	clusterLister           clusterlisterv1.ManagedClusterLister
	clusterSetLister        clusterlisterv1alpha1.ManagedClusterSetLister
	clusterSetBindingLister clusterlisterv1alpha1.ManagedClusterSetBindingLister
	placementLister         clusterlisterv1alpha1.PlacementLister
	placementDecisionLister clusterlisterv1alpha1.PlacementDecisionLister
	enqueuePlacementFunc    enqueuePlacementFunc
	scheduleFunc            scheduleFunc
}

// NewDecisionSchedulingController return an instance of schedulingController
func NewSchedulingController(
	clusterClient clusterclient.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1alpha1.ManagedClusterSetInformer,
	clusterSetBindingInformer clusterinformerv1alpha1.ManagedClusterSetBindingInformer,
	placementInformer clusterinformerv1alpha1.PlacementInformer,
	placementDecisionInformer clusterinformerv1alpha1.PlacementDecisionInformer,
	recorder events.Recorder,
) factory.Controller {
	syncCtx := factory.NewSyncContext(schedulingControllerName, recorder)
	enqueuePlacementFunc := func(namespace, name string) {
		syncCtx.Queue().Add(fmt.Sprintf("%s/%s", namespace, name))
	}

	// build controller
	c := schedulingController{
		clusterClient:           clusterClient,
		clusterLister:           clusterInformer.Lister(),
		clusterSetLister:        clusterSetInformer.Lister(),
		clusterSetBindingLister: clusterSetBindingInformer.Lister(),
		placementLister:         placementInformer.Lister(),
		placementDecisionLister: placementDecisionInformer.Lister(),
		scheduleFunc:            schedule,
		enqueuePlacementFunc:    enqueuePlacementFunc,
	}

	// setup event handler for cluster informer.
	// Once a cluster changes, clusterEventHandler enqueues all placements which are
	// impacted potentially for further reconciliation. It might not function before the
	// informers/listers of clusterset/clustersetbinding/placement are synced during
	// controller booting. But that should not cause any problem because all existing
	// placements will be enqueued by the controller anyway when booting.
	clusterInformer.Informer().AddEventHandler(&clusterEventHandler{
		clusterSetLister:        clusterSetInformer.Lister(),
		clusterSetBindingLister: clusterSetBindingInformer.Lister(),
		placementLister:         placementInformer.Lister(),
		enqueuePlacementFunc:    enqueuePlacementFunc,
	})

	// setup event handler for clusterset informer
	// Once a clusterset changes, clusterSetEventHandler enqueues all placements which are
	// impacted potentially for further reconciliation. It might not function before the
	// informers/listers of clustersetbinding/placement are synced during controller
	// booting. But that should not cause any problem because all existing placements will
	// be enqueued by the controller anyway when booting.
	clusterSetInformer.Informer().AddEventHandler(&clusterSetEventHandler{
		clusterSetBindingLister: clusterSetBindingInformer.Lister(),
		placementLister:         placementInformer.Lister(),
		enqueuePlacementFunc:    enqueuePlacementFunc,
	})

	// setup event handler for clustersetbinding informer
	// Once a clustersetbinding changes, clusterSetBindingEventHandler enqueues all placements
	// which are impacted potentially for further reconciliation. It might not function before
	// the informers/listers of clusterset/placement are synced during controller booting. But
	// that should not cause any problem because all existing placements will be enqueued by
	// the controller anyway when booting.
	clusterSetBindingInformer.Informer().AddEventHandler(&clusterSetBindingEventHandler{
		clusterSetLister:     clusterSetInformer.Lister(),
		placementLister:      placementInformer.Lister(),
		enqueuePlacementFunc: enqueuePlacementFunc,
	})

	return factory.New().
		WithSyncContext(syncCtx).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, placementInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			labels := accessor.GetLabels()
			placementName := labels[placementLabel]
			return fmt.Sprintf("%s/%s", accessor.GetNamespace(), placementName)
		}, func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			labels := accessor.GetLabels()
			if _, ok := labels[placementLabel]; ok {
				return true
			}
			return false
		}, placementDecisionInformer.Informer()).
		WithBareInformers(clusterInformer.Informer(), clusterSetInformer.Informer(), clusterSetBindingInformer.Informer()).
		WithSync(c.sync).
		ToController(schedulingControllerName, recorder)
}

func (c *schedulingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	queueKey := syncCtx.QueueKey()
	namespace, name, err := cache.SplitMetaNamespaceKey(queueKey)
	if err != nil {
		// ignore placement whose key is not in format: namespace/name
		utilruntime.HandleError(err)
		return nil
	}

	klog.V(4).Infof("Reconciling placement %q", queueKey)
	placement, err := c.placementLister.Placements(namespace).Get(name)
	if errors.IsNotFound(err) {
		// no work if placement is deleted
		return nil
	}
	if err != nil {
		return err
	}

	// no work if placement is deleting
	if !placement.DeletionTimestamp.IsZero() {
		return nil
	}

	// get all valid clustersetbindings in the placement namespace
	bindings, err := c.getValidManagedClusterSetBindings(placement.Namespace)
	if err != nil {
		return err
	}

	// get eligible clustersets for the placement
	clusterSetNames := c.getEligibleClusterSets(placement, bindings)

	// get available clusters for the placement
	clusters, err := c.getAvailableClusters(clusterSetNames)
	if err != nil {
		return err
	}

	// schedule placement with scheduler
	scheduleResult, err := c.scheduleFunc(ctx, placement, clusters, c.clusterClient, c.placementDecisionLister)
	if err != nil {
		return err
	}

	// update placement status if necessary to signal no bindings
	return c.updateStatus(ctx, placement, clusterSetNames, len(bindings), len(clusters), scheduleResult)
}

// getManagedClusterSetBindings returns all bindings found in the placement namespace.
func (c *schedulingController) getValidManagedClusterSetBindings(placementNamespace string) ([]*clusterapiv1alpha1.ManagedClusterSetBinding, error) {
	// get all clusterset bindings under the placement namespace
	bindings, err := c.clusterSetBindingLister.ManagedClusterSetBindings(placementNamespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		bindings = nil
	}

	validBindings := []*clusterapiv1alpha1.ManagedClusterSetBinding{}
	for _, binding := range bindings {
		// ignore clustersetbinding refers to a non-existent clusterset
		_, err := c.clusterSetLister.Get(binding.Name)
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		validBindings = append(validBindings, binding)
	}

	return validBindings, nil
}

// getEligibleClusterSets returns the names of clusterset that eligible for the placement
func (c *schedulingController) getEligibleClusterSets(placement *clusterapiv1alpha1.Placement, bindings []*clusterapiv1alpha1.ManagedClusterSetBinding) []string {
	// filter out invaid clustersetbindings
	clusterSetNames := sets.NewString()
	for _, binding := range bindings {
		clusterSetNames.Insert(binding.Name)
	}

	// get intersection of clustesets bound to placement namespace and clustesets specified
	// in placement spec
	if len(placement.Spec.ClusterSets) != 0 {
		clusterSetNames = clusterSetNames.Intersection(sets.NewString(placement.Spec.ClusterSets...))
	}

	return clusterSetNames.List()
}

// getAvailableClusters returns available clusters for the given placement. The clusters must
// 1) Be from clustersets bound to the placement namespace;
// 2) Belong to one of particular clustersets if .spec.clusterSets is specified;
func (c *schedulingController) getAvailableClusters(clusterSetNames []string) ([]*clusterapiv1.ManagedCluster, error) {
	if len(clusterSetNames) == 0 {
		return nil, nil
	}
	// list clusters from those clustersets
	requirement, err := labels.NewRequirement(clusterSetLabel, selection.In, clusterSetNames)
	if err != nil {
		return nil, err
	}
	labelSelector := labels.NewSelector().Add(*requirement)
	return c.clusterLister.List(labelSelector)
}

// updateStatus updates the status of the placement according to intermediate scheduling data.
func (c *schedulingController) updateStatus(
	ctx context.Context,
	placement *clusterapiv1alpha1.Placement,
	eligibleClusterSetNames []string,
	numOfBindings,
	numOfAvailableClusters int,
	scheduleResult *scheduleResult,
) error {
	newPlacement := placement.DeepCopy()
	newPlacement.Status.NumberOfSelectedClusters = int32(scheduleResult.scheduledDecisions)

	satisfiedCondition := newSatisfiedCondition(
		placement.Spec.ClusterSets,
		eligibleClusterSetNames,
		numOfBindings,
		numOfAvailableClusters,
		scheduleResult.feasibleClusters,
		scheduleResult.unscheduledDecisions,
	)

	meta.SetStatusCondition(&newPlacement.Status.Conditions, satisfiedCondition)
	if reflect.DeepEqual(newPlacement.Status, placement.Status) {
		return nil
	}
	_, err := c.clusterClient.ClusterV1alpha1().Placements(newPlacement.Namespace).UpdateStatus(ctx, newPlacement, metav1.UpdateOptions{})
	return err
}

// newSatisfiedCondition returns a new condition with type PlacementConditionSatisfied
func newSatisfiedCondition(
	clusterSetsInSpec []string,
	eligibleClusterSets []string,
	numOfBindings,
	numOfAvailableClusters,
	numOfFeasibleClusters,
	numOfUnscheduledDecisions int,
) metav1.Condition {
	condition := metav1.Condition{
		Type: clusterapiv1alpha1.PlacementConditionSatisfied,
	}
	switch {
	case numOfBindings == 0:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "NoManagedClusterSetBindings"
		condition.Message = "No valid ManagedClusterSetBindings found in placement namespace"
	case len(eligibleClusterSets) == 0:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "NoIntersection"
		condition.Message = fmt.Sprintf("None of ManagedClusterSets [%s] is bound to placement namespace", strings.Join(clusterSetsInSpec, ","))
	case numOfAvailableClusters == 0:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "AllManagedClusterSetsEmpty"
		condition.Message = fmt.Sprintf("All ManagedClusterSets [%s] have no member ManagedCluster", strings.Join(eligibleClusterSets, ","))
	case numOfFeasibleClusters == 0:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "NoManagedClusterMatched"
		condition.Message = "No ManagedCluster matches any of the cluster predicate"
	case numOfUnscheduledDecisions == 0:
		condition.Status = metav1.ConditionTrue
		condition.Reason = "AllDecisionsScheduled"
		condition.Message = "All cluster decisions scheduled"
	default:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "NotAllDecisionsScheduled"
		condition.Message = fmt.Sprintf("%d cluster decisions unscheduled", numOfUnscheduledDecisions)
	}
	return condition
}
