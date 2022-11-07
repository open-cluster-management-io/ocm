package scheduling

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	errorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	cache "k8s.io/client-go/tools/cache"
	kevents "k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/placement/pkg/controllers/framework"
)

const (
	clusterSetLabel                = "cluster.open-cluster-management.io/clusterset"
	placementLabel                 = "cluster.open-cluster-management.io/placement"
	schedulingControllerName       = "SchedulingController"
	schedulingControllerResyncName = "SchedulingControllerResync"
	maxNumOfClusterDecisions       = 100
	maxEventMessageLength          = 1000 //the event message can have at most 1024 characters, use 1000 as limitation here to keep some buffer
)

var ResyncInterval = time.Minute * 5

type enqueuePlacementFunc func(namespace, name string)

// schedulingController schedules cluster decisions for Placements
type schedulingController struct {
	clusterClient           clusterclient.Interface
	clusterLister           clusterlisterv1.ManagedClusterLister
	clusterSetLister        clusterlisterv1beta1.ManagedClusterSetLister
	clusterSetBindingLister clusterlisterv1beta1.ManagedClusterSetBindingLister
	placementLister         clusterlisterv1beta1.PlacementLister
	placementDecisionLister clusterlisterv1beta1.PlacementDecisionLister
	enqueuePlacementFunc    enqueuePlacementFunc
	scheduler               Scheduler
	recorder                kevents.EventRecorder
}

// NewDecisionSchedulingController return an instance of schedulingController
func NewSchedulingController(
	clusterClient clusterclient.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta1.ManagedClusterSetInformer,
	clusterSetBindingInformer clusterinformerv1beta1.ManagedClusterSetBindingInformer,
	placementInformer clusterinformerv1beta1.PlacementInformer,
	placementDecisionInformer clusterinformerv1beta1.PlacementDecisionInformer,
	scheduler Scheduler,
	recorder events.Recorder, krecorder kevents.EventRecorder,
) factory.Controller {
	syncCtx := factory.NewSyncContext(schedulingControllerName, recorder)
	enqueuePlacementFunc := func(namespace, name string) {
		syncCtx.Queue().Add(fmt.Sprintf("%s/%s", namespace, name))
	}

	// build controller
	c := &schedulingController{
		clusterClient:           clusterClient,
		clusterLister:           clusterInformer.Lister(),
		clusterSetLister:        clusterSetInformer.Lister(),
		clusterSetBindingLister: clusterSetBindingInformer.Lister(),
		placementLister:         placementInformer.Lister(),
		placementDecisionLister: placementDecisionInformer.Lister(),
		enqueuePlacementFunc:    enqueuePlacementFunc,
		recorder:                krecorder,
		scheduler:               scheduler,
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

func NewSchedulingControllerResync(
	clusterClient clusterclient.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta1.ManagedClusterSetInformer,
	clusterSetBindingInformer clusterinformerv1beta1.ManagedClusterSetBindingInformer,
	placementInformer clusterinformerv1beta1.PlacementInformer,
	placementDecisionInformer clusterinformerv1beta1.PlacementDecisionInformer,
	scheduler Scheduler,
	recorder events.Recorder, krecorder kevents.EventRecorder,
) factory.Controller {
	syncCtx := factory.NewSyncContext(schedulingControllerResyncName, recorder)
	enqueuePlacementFunc := func(namespace, name string) {
		syncCtx.Queue().Add(fmt.Sprintf("%s/%s", namespace, name))
	}

	// build controller
	c := &schedulingController{
		clusterClient:           clusterClient,
		clusterLister:           clusterInformer.Lister(),
		clusterSetLister:        clusterSetInformer.Lister(),
		clusterSetBindingLister: clusterSetBindingInformer.Lister(),
		placementLister:         placementInformer.Lister(),
		placementDecisionLister: placementDecisionInformer.Lister(),
		enqueuePlacementFunc:    enqueuePlacementFunc,
		recorder:                krecorder,
		scheduler:               scheduler,
	}

	return factory.New().
		WithSyncContext(syncCtx).
		WithSync(c.resync).
		ResyncEvery(ResyncInterval).
		ToController(schedulingControllerResyncName, recorder)

}

// Resync the placement which depends on AddOnPlacementScore periodically
func (c *schedulingController) resync(ctx context.Context, syncCtx factory.SyncContext) error {
	queueKey := syncCtx.QueueKey()
	klog.V(4).Infof("Resync placement %q", queueKey)

	if queueKey == "key" {
		placements, err := c.placementLister.List(labels.Everything())
		if err != nil {
			return err
		}

		for _, placement := range placements {
			for _, config := range placement.Spec.PrioritizerPolicy.Configurations {
				if config.ScoreCoordinate != nil && config.ScoreCoordinate.Type == clusterapiv1beta1.ScoreCoordinateTypeAddOn {
					key, _ := cache.MetaNamespaceKeyFunc(placement)
					klog.V(4).Infof("Requeue placement %s", key)
					syncCtx.Queue().Add(key)
					break
				}
			}
		}

		return nil
	} else {
		placement, err := c.getPlacement(queueKey)
		if errors.IsNotFound(err) {
			// no work if placement is deleted
			return nil
		}
		if err != nil {
			return err
		}
		// Do not pass syncCtx to syncPlacement, since don't want to requeue the placement when resyncing the placement.
		return c.syncPlacement(ctx, nil, placement)
	}
}

func (c *schedulingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	queueKey := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling placement %q", queueKey)

	placement, err := c.getPlacement(queueKey)
	if errors.IsNotFound(err) {
		// no work if placement is deleted
		return nil
	}
	if err != nil {
		return err
	}

	return c.syncPlacement(ctx, syncCtx, placement)
}

func (c *schedulingController) getPlacement(queueKey string) (*clusterapiv1beta1.Placement, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(queueKey)
	if err != nil {
		// ignore placement whose key is not in format: namespace/name
		utilruntime.HandleError(err)
		return nil, nil
	}

	placement, err := c.placementLister.Placements(namespace).Get(name)
	if err != nil {
		return nil, err
	}

	return placement, nil
}

func (c *schedulingController) syncPlacement(ctx context.Context, syncCtx factory.SyncContext, placement *clusterapiv1beta1.Placement) error {
	// no work if placement is deleting
	if !placement.DeletionTimestamp.IsZero() {
		return nil
	}

	// no work if placement has cluster.open-cluster-management.io/experimental-scheduling-disable: "true" annotation
	if value, ok := placement.GetAnnotations()[clusterapiv1beta1.PlacementDisableAnnotation]; ok && value == "true" {
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
	scheduleResult, status := c.scheduler.Schedule(ctx, placement, clusters)
	misconfiguredCondition := newMisconfiguredCondition(status)
	satisfiedCondition := newSatisfiedCondition(
		placement.Spec.ClusterSets,
		clusterSetNames,
		len(bindings),
		len(clusters),
		len(scheduleResult.Decisions()),
		scheduleResult.NumOfUnscheduled(),
		status,
	)

	// update status and return error if schedule() returns error
	if status.IsError() {
		if err := c.updateStatus(ctx, placement, int32(len(scheduleResult.Decisions())), misconfiguredCondition, satisfiedCondition); err != nil {
			return err
		}
		return status.AsError()
	}

	// requeue placement if requeueAfter is defined in scheduleResult
	if syncCtx != nil && scheduleResult.RequeueAfter() != nil {
		key, _ := cache.MetaNamespaceKeyFunc(placement)
		t := scheduleResult.RequeueAfter()
		klog.V(4).Infof("Requeue placement %s after %t", key, t)
		syncCtx.Queue().AddAfter(key, *t)
	}

	err = c.bind(ctx, placement, scheduleResult.Decisions(), scheduleResult.PrioritizerScores(), status)
	if err != nil {
		return err
	}

	// update placement status if necessary to signal no bindings
	return c.updateStatus(ctx, placement, int32(len(scheduleResult.Decisions())), misconfiguredCondition, satisfiedCondition)
}

// getManagedClusterSetBindings returns all bindings found in the placement namespace.
func (c *schedulingController) getValidManagedClusterSetBindings(placementNamespace string) ([]*clusterapiv1beta1.ManagedClusterSetBinding, error) {
	// get all clusterset bindings under the placement namespace
	bindings, err := c.clusterSetBindingLister.ManagedClusterSetBindings(placementNamespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		bindings = nil
	}

	validBindings := []*clusterapiv1beta1.ManagedClusterSetBinding{}
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
func (c *schedulingController) getEligibleClusterSets(placement *clusterapiv1beta1.Placement, bindings []*clusterapiv1beta1.ManagedClusterSetBinding) []string {
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
	//all available clusters
	availableClusters := map[string]*clusterapiv1.ManagedCluster{}

	for _, name := range clusterSetNames {
		// ignore clusterset if failed to get
		clusterSet, err := c.clusterSetLister.Get(name)
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		clusters, err := clusterapiv1beta1.GetClustersFromClusterSet(clusterSet, c.clusterLister)
		if err != nil {
			return nil, fmt.Errorf("failed to get clusterset: %v, clusters, Error: %v", clusterSet.Name, err)
		}
		for i := range clusters {
			availableClusters[clusters[i].Name] = clusters[i]
		}
	}

	if len(availableClusters) == 0 {
		return nil, nil
	}

	result := []*clusterapiv1.ManagedCluster{}
	for _, c := range availableClusters {
		result = append(result, c)
	}

	return result, nil
}

// updateStatus updates the status of the placement according to intermediate scheduling data.
func (c *schedulingController) updateStatus(
	ctx context.Context,
	placement *clusterapiv1beta1.Placement,
	numberOfSelectedClusters int32,
	conditions ...metav1.Condition,
) error {
	newPlacement := placement.DeepCopy()
	newPlacement.Status.NumberOfSelectedClusters = numberOfSelectedClusters

	for _, c := range conditions {
		meta.SetStatusCondition(&newPlacement.Status.Conditions, c)
	}
	if reflect.DeepEqual(newPlacement.Status, placement.Status) {
		return nil
	}

	_, err := c.clusterClient.ClusterV1beta1().
		Placements(newPlacement.Namespace).
		UpdateStatus(ctx, newPlacement, metav1.UpdateOptions{})
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
	status *framework.Status,
) metav1.Condition {
	condition := metav1.Condition{
		Type: clusterapiv1beta1.PlacementConditionSatisfied,
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
	case status.Code() == framework.Error:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "NotAllDecisionsScheduled"
		condition.Message = status.AsError().Error()
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

func newMisconfiguredCondition(status *framework.Status) metav1.Condition {
	if status.Code() == framework.Misconfigured {
		return metav1.Condition{
			Type:    clusterapiv1beta1.PlacementConditionMisconfigured,
			Status:  metav1.ConditionTrue,
			Reason:  "Misconfigured",
			Message: fmt.Sprintf("%s:%s", status.Plugin(), status.Message()),
		}
	} else {
		return metav1.Condition{
			Type:    clusterapiv1beta1.PlacementConditionMisconfigured,
			Status:  metav1.ConditionFalse,
			Reason:  "Succeedconfigured",
			Message: "Placement configurations check pass",
		}
	}
}

// bind updates the cluster decisions in the status of the placementdecisions with the given
// cluster decision slice. New placementdecisions will be created if no one exists.
func (c *schedulingController) bind(
	ctx context.Context,
	placement *clusterapiv1beta1.Placement,
	clusterDecisions []clusterapiv1beta1.ClusterDecision,
	clusterScores PrioritizerScore,
	status *framework.Status,
) error {
	// sort clusterdecisions by cluster name
	sort.SliceStable(clusterDecisions, func(i, j int) bool {
		return clusterDecisions[i].ClusterName < clusterDecisions[j].ClusterName
	})

	// split the cluster decisions into slices, the size of each slice cannot exceed
	// maxNumOfClusterDecisions.
	decisionSlices := [][]clusterapiv1beta1.ClusterDecision{}
	remainingDecisions := clusterDecisions
	for index := 0; len(remainingDecisions) > 0; index++ {
		var decisionSlice []clusterapiv1beta1.ClusterDecision
		switch {
		case len(remainingDecisions) > maxNumOfClusterDecisions:
			decisionSlice = remainingDecisions[0:maxNumOfClusterDecisions]
			remainingDecisions = remainingDecisions[maxNumOfClusterDecisions:]
		default:
			decisionSlice = remainingDecisions
			remainingDecisions = nil
		}
		decisionSlices = append(decisionSlices, decisionSlice)
	}

	// bind cluster decision slices to placementdecisions.
	errs := []error{}

	placementDecisionNames := sets.NewString()
	for index, decisionSlice := range decisionSlices {
		placementDecisionName := fmt.Sprintf("%s-decision-%d", placement.Name, index+1)
		placementDecisionNames.Insert(placementDecisionName)
		err := c.createOrUpdatePlacementDecision(
			ctx, placement, placementDecisionName, decisionSlice, clusterScores, status)
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) != 0 {
		return errorhelpers.NewMultiLineAggregate(errs)
	}

	// query all placementdecisions of the placement
	requirement, err := labels.NewRequirement(placementLabel, selection.Equals, []string{placement.Name})
	if err != nil {
		return err
	}
	labelSelector := labels.NewSelector().Add(*requirement)
	placementDecisions, err := c.placementDecisionLister.PlacementDecisions(placement.Namespace).List(labelSelector)
	if err != nil {
		return err
	}

	// delete redundant placementdecisions
	errs = []error{}
	for _, placementDecision := range placementDecisions {
		if placementDecisionNames.Has(placementDecision.Name) {
			continue
		}
		err := c.clusterClient.ClusterV1beta1().PlacementDecisions(
			placementDecision.Namespace).Delete(ctx, placementDecision.Name, metav1.DeleteOptions{})
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
		}
		c.recorder.Eventf(
			placement, placementDecision, corev1.EventTypeNormal,
			"DecisionDelete", "DecisionDeleted",
			"Decision %s is deleted with placement %s in namespace %s", placementDecision.Name, placement.Name, placement.Namespace)
	}
	return errorhelpers.NewMultiLineAggregate(errs)
}

// createOrUpdatePlacementDecision creates a new PlacementDecision if it does not exist and
// then updates the status with the given ClusterDecision slice if necessary
func (c *schedulingController) createOrUpdatePlacementDecision(
	ctx context.Context,
	placement *clusterapiv1beta1.Placement,
	placementDecisionName string,
	clusterDecisions []clusterapiv1beta1.ClusterDecision,
	clusterScores PrioritizerScore,
	status *framework.Status,
) error {
	if len(clusterDecisions) > maxNumOfClusterDecisions {
		return fmt.Errorf("the number of clusterdecisions %q exceeds the max limitation %q", len(clusterDecisions), maxNumOfClusterDecisions)
	}

	placementDecision, err := c.placementDecisionLister.PlacementDecisions(placement.Namespace).Get(placementDecisionName)
	switch {
	case errors.IsNotFound(err):
		// create the placementdecision if not exists
		owner := metav1.NewControllerRef(placement, clusterapiv1beta1.GroupVersion.WithKind("Placement"))
		placementDecision = &clusterapiv1beta1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      placementDecisionName,
				Namespace: placement.Namespace,
				Labels: map[string]string{
					placementLabel: placement.Name,
				},
				OwnerReferences: []metav1.OwnerReference{*owner},
			},
		}
		var err error
		placementDecision, err = c.clusterClient.ClusterV1beta1().PlacementDecisions(
			placement.Namespace).Create(ctx, placementDecision, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		c.recorder.Eventf(
			placement, placementDecision, corev1.EventTypeNormal,
			"DecisionCreate", "DecisionCreated",
			"Decision %s is created with placement %s in namespace %s", placementDecision.Name, placement.Name, placement.Namespace)
	case err != nil:
		return err
	}

	// update the status of the placementdecision if decisions change
	if apiequality.Semantic.DeepEqual(placementDecision.Status.Decisions, clusterDecisions) {
		return nil
	}

	newPlacementDecision := placementDecision.DeepCopy()
	newPlacementDecision.Status.Decisions = clusterDecisions
	newPlacementDecision, err = c.clusterClient.ClusterV1beta1().PlacementDecisions(newPlacementDecision.Namespace).
		UpdateStatus(ctx, newPlacementDecision, metav1.UpdateOptions{})

	if err != nil {
		return err
	}

	// update the event with warning
	if status.Code() == framework.Warning {
		c.recorder.Eventf(
			placement, placementDecision, corev1.EventTypeWarning,
			"DecisionUpdate", "DecisionUpdated",
			"Decision %s is updated with placement %s in namespace %s: %s in plugin %s", placementDecision.Name, placement.Name, placement.Namespace,
			status.Message(),
			status.Plugin())
	} else {
		c.recorder.Eventf(
			placement, placementDecision, corev1.EventTypeNormal,
			"DecisionUpdate", "DecisionUpdated",
			"Decision %s is updated with placement %s in namespace %s", placementDecision.Name, placement.Name, placement.Namespace)
	}

	// update the event with prioritizer score.
	scoreStr := ""
	for k, v := range clusterScores {
		tmpScore := fmt.Sprintf("%s:%d ", k, v)
		if len(scoreStr)+len(tmpScore) > maxEventMessageLength {
			scoreStr += "......"
			break
		} else {
			scoreStr += tmpScore
		}
	}

	c.recorder.Eventf(
		placement, placementDecision, corev1.EventTypeNormal,
		"ScoreUpdate", "ScoreUpdated",
		scoreStr)

	return nil
}
