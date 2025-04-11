package scheduling

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	errorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	kevents "k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1alpha1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterinformerv1beta2 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta2"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	clustersdkv1beta2 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1beta2"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/placement/controllers/framework"
	"open-cluster-management.io/ocm/pkg/placement/controllers/metrics"
	"open-cluster-management.io/ocm/pkg/placement/helpers"
)

const (
	schedulingControllerName = "SchedulingController"
	maxNumOfClusterDecisions = 100
	maxEventMessageLength    = 1000 // the event message can have at most 1024 characters, use 1000 as limitation here to keep some buffer
)

// decisionGroups groups the cluster decisions by group strategy
type clusterDecisionGroups []clusterDecisionGroup

type clusterDecisionGroup struct {
	decisionGroupName string
	clusterDecisions  []clusterapiv1beta1.ClusterDecision
}

// schedulingController schedules cluster decisions for Placements
type schedulingController struct {
	clusterClient           clusterclient.Interface
	clusterLister           clusterlisterv1.ManagedClusterLister
	clusterSetLister        clusterlisterv1beta2.ManagedClusterSetLister
	clusterSetBindingLister clusterlisterv1beta2.ManagedClusterSetBindingLister
	placementLister         clusterlisterv1beta1.PlacementLister
	placementDecisionLister clusterlisterv1beta1.PlacementDecisionLister
	scheduler               Scheduler
	eventsRecorder          kevents.EventRecorder
	metricsRecorder         *metrics.ScheduleMetrics
}

// NewSchedulingController return an instance of schedulingController
func NewSchedulingController(
	ctx context.Context,
	clusterClient clusterclient.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta2.ManagedClusterSetInformer,
	clusterSetBindingInformer clusterinformerv1beta2.ManagedClusterSetBindingInformer,
	placementInformer clusterinformerv1beta1.PlacementInformer,
	placementDecisionInformer clusterinformerv1beta1.PlacementDecisionInformer,
	placementScoreInformer clusterinformerv1alpha1.AddOnPlacementScoreInformer,
	scheduler Scheduler,
	recorder events.Recorder, krecorder kevents.EventRecorder,
	metricsRecorder *metrics.ScheduleMetrics,
) factory.Controller {
	syncCtx := factory.NewSyncContext(schedulingControllerName, recorder)

	enQueuer := newEnqueuer(ctx, syncCtx.Queue(), clusterInformer, clusterSetInformer, placementInformer, clusterSetBindingInformer)

	// build controller
	c := &schedulingController{
		clusterClient:           clusterClient,
		clusterLister:           clusterInformer.Lister(),
		clusterSetLister:        clusterSetInformer.Lister(),
		clusterSetBindingLister: clusterSetBindingInformer.Lister(),
		placementLister:         placementInformer.Lister(),
		placementDecisionLister: placementDecisionInformer.Lister(),
		scheduler:               scheduler,
		eventsRecorder:          krecorder,
		metricsRecorder:         metricsRecorder,
	}

	// setup event handler for cluster informer.
	// Once a cluster changes, clusterEventHandler enqueues all placements which are
	// impacted potentially for further reconciliation. It might not function before the
	// informers/listers of clusterset/clustersetbinding/placement are synced during
	// controller booting. But that should not cause any problem because all existing
	// placements will be enqueued by the controller anyway when booting.
	_, err := clusterInformer.Informer().AddEventHandler(&clusterEventHandler{
		enqueuer: enQueuer,
	})
	if err != nil {
		utilruntime.HandleError(err)
	}

	// setup event handler for clusterset informer
	// Once a clusterset changes, clusterSetEventHandler enqueues all placements which are
	// impacted potentially for further reconciliation. It might not function before the
	// informers/listers of clustersetbinding/placement are synced during controller
	// booting. But that should not cause any problem because all existing placements will
	// be enqueued by the controller anyway when booting.
	_, err = clusterSetInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: enQueuer.enqueueClusterSet,
		UpdateFunc: func(oldObj, newObj interface{}) {
			enQueuer.enqueueClusterSet(newObj)
		},
		DeleteFunc: enQueuer.enqueueClusterSet,
	})
	if err != nil {
		utilruntime.HandleError(err)
	}

	// setup event handler for clustersetbinding informer
	// Once a clustersetbinding changes, clusterSetBindingEventHandler enqueues all placements
	// which are impacted potentially for further reconciliation. It might not function before
	// the informers/listers of clusterset/placement are synced during controller booting. But
	// that should not cause any problem because all existing placements will be enqueued by
	// the controller anyway when booting.
	_, err = clusterSetBindingInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: enQueuer.enqueueClusterSetBinding,
		UpdateFunc: func(oldObj, newObj interface{}) {
			enQueuer.enqueueClusterSetBinding(newObj)
		},
		DeleteFunc: enQueuer.enqueueClusterSetBinding,
	})
	if err != nil {
		utilruntime.HandleError(err)
	}

	// setup event handler for placementscore informer
	_, err = placementScoreInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: enQueuer.enqueuePlacementScore,
		UpdateFunc: func(oldObj, newObj interface{}) {
			enQueuer.enqueuePlacementScore(newObj)
		},
		DeleteFunc: enQueuer.enqueuePlacementScore,
	})
	if err != nil {
		utilruntime.HandleError(err)
	}

	return factory.New().
		WithSyncContext(syncCtx).
		WithInformersQueueKeysFunc(
			queue.QueueKeyByMetaNamespaceName,
			placementInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			placementName := accessor.GetLabels()[clusterapiv1beta1.PlacementLabel]
			return fmt.Sprintf("%s/%s", accessor.GetNamespace(), placementName)
		},
			queue.FileterByLabel(clusterapiv1beta1.PlacementLabel),
			placementDecisionInformer.Informer()).
		WithBareInformers(clusterInformer.Informer(), clusterSetInformer.Informer(), clusterSetBindingInformer.Informer(), placementScoreInformer.Informer()).
		WithSync(c.sync).
		ToController(schedulingControllerName, recorder)
}

func (c *schedulingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	logger := klog.FromContext(ctx)
	queueKey := syncCtx.QueueKey()
	logger.V(4).Info("Reconciling placement", "queueKey", queueKey)

	placement, err := c.getPlacement(queueKey)
	if errors.IsNotFound(err) {
		// no work if placement is deleted
		return nil
	}
	if err != nil {
		return err
	}

	return c.syncPlacement(ctx, syncCtx, queueKey, placement)
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

func (c *schedulingController) syncPlacement(ctx context.Context, syncCtx factory.SyncContext, queueKey string, placement *clusterapiv1beta1.Placement) error {
	logger := klog.FromContext(ctx)
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
	c.metricsRecorder.StartSchedule(queueKey)
	scheduleResult, status := c.scheduler.Schedule(ctx, placement, clusters)
	// generate placement decision and status
	decisions, groupStatus, s := c.generatePlacementDecisionsAndStatus(placement, scheduleResult.Decisions())
	if s.IsError() {
		status = s
	}
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

	// requeue placement if requeueAfter is defined in scheduleResult
	if syncCtx != nil && scheduleResult.RequeueAfter() != nil {
		key, _ := cache.MetaNamespaceKeyFunc(placement)
		t := scheduleResult.RequeueAfter()
		logger.V(4).Info("Requeue placement after time", "placementKey", key, "time", t)
		syncCtx.Queue().AddAfter(key, *t)
	}

	// create/update placement decisions
	c.metricsRecorder.StartBind(queueKey)
	defer c.metricsRecorder.Done(queueKey)
	err = c.bind(ctx, placement, decisions, scheduleResult.PrioritizerScores(), status)
	if err != nil {
		return err
	}

	// update placement status if necessary to signal no bindings
	if err := c.updateStatus(
		ctx, placement, groupStatus, int32(len(scheduleResult.Decisions())), misconfiguredCondition, satisfiedCondition); err != nil { // nolint:gosec
		return err
	}

	return status.AsError()
}

// getManagedClusterSetBindings returns all bindings found in the placement namespace.
func (c *schedulingController) getValidManagedClusterSetBindings(placementNamespace string) ([]*clusterapiv1beta2.ManagedClusterSetBinding, error) {
	// get all clusterset bindings under the placement namespace
	bindings, err := c.clusterSetBindingLister.ManagedClusterSetBindings(placementNamespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		bindings = nil
	}

	var validBindings []*clusterapiv1beta2.ManagedClusterSetBinding
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
func (c *schedulingController) getEligibleClusterSets(placement *clusterapiv1beta1.Placement, bindings []*clusterapiv1beta2.ManagedClusterSetBinding) []string {
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
// 3) Not in terminating state;
func (c *schedulingController) getAvailableClusters(clusterSetNames []string) ([]*clusterapiv1.ManagedCluster, error) {
	if len(clusterSetNames) == 0 {
		return nil, nil
	}
	// all available clusters
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
		clusters, err := clustersdkv1beta2.GetClustersFromClusterSet(clusterSet, c.clusterLister)
		if err != nil {
			return nil, fmt.Errorf("failed to get clusterset: %v, clusters, Error: %v", clusterSet.Name, err)
		}
		for i := range clusters {
			if clusters[i].DeletionTimestamp.IsZero() {
				availableClusters[clusters[i].Name] = clusters[i]
			}
		}
	}

	if len(availableClusters) == 0 {
		return nil, nil
	}

	var result []*clusterapiv1.ManagedCluster
	for _, c := range availableClusters {
		result = append(result, c)
	}

	return result, nil
}

// updateStatus updates the status of the placement according to intermediate scheduling data.
func (c *schedulingController) updateStatus(
	ctx context.Context,
	placement *clusterapiv1beta1.Placement,
	decisionGroupStatus []*clusterapiv1beta1.DecisionGroupStatus,
	numberOfSelectedClusters int32,
	conditions ...metav1.Condition,
) error {
	newPlacement := placement.DeepCopy()
	newPlacement.Status.NumberOfSelectedClusters = numberOfSelectedClusters
	newPlacement.Status.DecisionGroups = []clusterapiv1beta1.DecisionGroupStatus{}

	for _, status := range decisionGroupStatus {
		newPlacement.Status.DecisionGroups = append(newPlacement.Status.DecisionGroups, *status)
	}

	for _, c := range conditions {
		meta.SetStatusCondition(&newPlacement.Status.Conditions, c)
	}
	if reflect.DeepEqual(newPlacement.Status, placement.Status) {
		return nil
	}

	placementPatcher := patcher.NewPatcher[
		*clusterapiv1beta1.Placement, clusterapiv1beta1.PlacementSpec, clusterapiv1beta1.PlacementStatus](
		c.clusterClient.ClusterV1beta1().Placements(newPlacement.Namespace))

	_, err := placementPatcher.PatchStatus(ctx, newPlacement, newPlacement.Status, placement.Status)
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

// generate placement decision and decision group status of placement
func (c *schedulingController) generatePlacementDecisionsAndStatus(
	placement *clusterapiv1beta1.Placement,
	clusters []*clusterapiv1.ManagedCluster,
) ([]*clusterapiv1beta1.PlacementDecision, []*clusterapiv1beta1.DecisionGroupStatus, *framework.Status) {
	placementDecisionIndex := 1
	var placementDecisions []*clusterapiv1beta1.PlacementDecision
	var decisionGroupStatus []*clusterapiv1beta1.DecisionGroupStatus

	// generate decision group
	decisionGroups, status := c.generateDecisionGroups(placement, clusters)

	// generate placement decision for each decision group
	for decisionGroupIndex, decisionGroup := range decisionGroups {
		// generate placement decisions and status, decision group index starts from 0
		// placement name index starts from 1 to keep backward compatibility
		// TODO: should be consistent with index or using a random generate name when version bumps
		pds, groupStatus := c.generateDecision(placement, decisionGroup, decisionGroupIndex, placementDecisionIndex)

		placementDecisions = append(placementDecisions, pds...)
		decisionGroupStatus = append(decisionGroupStatus, groupStatus)
		placementDecisionIndex += len(pds)
	}

	return placementDecisions, decisionGroupStatus, status
}

// generateDecisionGroups group clusters based on the placement decision strategy.
func (c *schedulingController) generateDecisionGroups(
	placement *clusterapiv1beta1.Placement,
	clusters []*clusterapiv1.ManagedCluster,
) (clusterDecisionGroups, *framework.Status) {
	var groups []clusterDecisionGroup

	// Calculate the group length
	// The number of items in each group is determined by the specific number or percentage defined in
	// spec.DecisionStrategy.GroupStrategy.ClustersPerDecisionGroup.
	groupLength, status := calculateLength(&placement.Spec.DecisionStrategy.GroupStrategy.ClustersPerDecisionGroup, len(clusters))
	if status.IsError() {
		return groups, status
	}

	// Record the cluster names
	clusterNameSet := sets.New[string]()
	for _, cluster := range clusters {
		clusterNameSet.Insert(cluster.Name)
	}

	// First groups the clusters by ClusterSelector defined in spec.DecisionStrategy.GroupStrategy.DecisionGroups.
	for _, d := range placement.Spec.DecisionStrategy.GroupStrategy.DecisionGroups {
		// filter clusters by cluster selector
		matched, status := filterClustersBySelector(d.ClusterSelector, clusters, clusterNameSet)
		if status.IsError() {
			return groups, status
		}
		// If matched clusters number meets groupLength, divide into multiple groups.
		decisionGroups := divideDecisionGroups(d.GroupName, matched, groupLength)
		groups = append(groups, decisionGroups...)
	}

	// The rest of the clusters will also be put into decision groups.
	var matched []clusterapiv1beta1.ClusterDecision
	for _, cluster := range clusterNameSet.UnsortedList() {
		matched = append(matched, clusterapiv1beta1.ClusterDecision{
			ClusterName: cluster,
		})
	}

	// If the rest of clusters number meets groupLength, divide into multiple groups.
	decisionGroups := divideDecisionGroups("", matched, groupLength)
	groups = append(groups, decisionGroups...)

	// generate at least on empty decisionGroup, this is to ensure there's an empty placement decision if no cluster selected.
	if len(groups) == 0 {
		groups = append(groups, clusterDecisionGroup{})
	}

	return groups, framework.NewStatus("", framework.Success, "")
}

func (c *schedulingController) generateDecision(
	placement *clusterapiv1beta1.Placement,
	clusterDecisionGroup clusterDecisionGroup,
	decisionGroupIndex, placementDecisionIndex int,
) ([]*clusterapiv1beta1.PlacementDecision, *clusterapiv1beta1.DecisionGroupStatus) {
	// split the cluster decisions into slices, the size of each slice cannot exceed
	// maxNumOfClusterDecisions.
	var decisionSlices [][]clusterapiv1beta1.ClusterDecision
	remainingDecisions := clusterDecisionGroup.clusterDecisions
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

	// if decisionSlices is empty, append one empty slice.
	// so that can create a PlacementDecision with empty decisions in status.
	if len(decisionSlices) == 0 {
		decisionSlices = append(decisionSlices, []clusterapiv1beta1.ClusterDecision{})
	}

	var placementDecisionNames []string
	var placementDecisions []*clusterapiv1beta1.PlacementDecision
	for index, decisionSlice := range decisionSlices {
		placementDecisionName := fmt.Sprintf("%s-decision-%d", placement.Name, placementDecisionIndex+index)
		owner := metav1.NewControllerRef(placement, clusterapiv1beta1.GroupVersion.WithKind("Placement"))
		placementDecision := &clusterapiv1beta1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      placementDecisionName,
				Namespace: placement.Namespace,
				Labels: map[string]string{
					clusterapiv1beta1.PlacementLabel:          placement.Name,
					clusterapiv1beta1.DecisionGroupNameLabel:  clusterDecisionGroup.decisionGroupName,
					clusterapiv1beta1.DecisionGroupIndexLabel: fmt.Sprint(decisionGroupIndex),
				},
				OwnerReferences: []metav1.OwnerReference{*owner},
			},
			Status: clusterapiv1beta1.PlacementDecisionStatus{
				Decisions: decisionSlice,
			},
		}
		placementDecisions = append(placementDecisions, placementDecision)
		placementDecisionNames = append(placementDecisionNames, placementDecisionName)
	}

	decisionGroupStatus := &clusterapiv1beta1.DecisionGroupStatus{
		DecisionGroupIndex: int32(decisionGroupIndex), //nolint:gosec
		DecisionGroupName:  clusterDecisionGroup.decisionGroupName,
		Decisions:          placementDecisionNames,
		ClustersCount:      int32(len(clusterDecisionGroup.clusterDecisions)), //nolint:gosec
	}

	return placementDecisions, decisionGroupStatus
}

// bind updates the cluster decisions in the status of the placementdecisions with the given
// cluster decision slice. New placementdecisions will be created if no one exists.
// bind will also return the decision groups for placement status.
func (c *schedulingController) bind(
	ctx context.Context,
	placement *clusterapiv1beta1.Placement,
	placementdecisions []*clusterapiv1beta1.PlacementDecision,
	clusterScores PrioritizerScore,
	status *framework.Status,
) error {
	var errs []error
	placementDecisionNames := sets.NewString()

	// create/update placement decisions
	for _, pd := range placementdecisions {
		placementDecisionNames.Insert(pd.Name)
		err := c.createOrUpdatePlacementDecision(ctx, placement, pd, clusterScores, status)
		if err != nil {
			errs = append(errs, err)
		}
	}

	// query all placementdecisions of the placement
	requirement, err := labels.NewRequirement(clusterapiv1beta1.PlacementLabel, selection.Equals, []string{placement.Name})
	if err != nil {
		return err
	}
	labelSelector := labels.NewSelector().Add(*requirement)
	pds, err := c.placementDecisionLister.PlacementDecisions(placement.Namespace).List(labelSelector)
	if err != nil {
		return err
	}

	// delete redundant placementdecisions
	errs = []error{}
	for _, pd := range pds {
		if placementDecisionNames.Has(pd.Name) {
			continue
		}
		err := c.clusterClient.ClusterV1beta1().PlacementDecisions(
			pd.Namespace).Delete(ctx, pd.Name, metav1.DeleteOptions{})
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
		}
		c.eventsRecorder.Eventf(
			placement, pd, corev1.EventTypeNormal,
			"DecisionDelete", "DecisionDeleted",
			"Decision %s is deleted with placement %s in namespace %s", pd.Name, placement.Name, placement.Namespace)
	}
	return errorhelpers.NewMultiLineAggregate(errs)
}

// createOrUpdatePlacementDecision creates a new PlacementDecision if it does not exist and
// then updates the status with the given ClusterDecision slice if necessary
func (c *schedulingController) createOrUpdatePlacementDecision(
	ctx context.Context,
	placement *clusterapiv1beta1.Placement,
	placementDecision *clusterapiv1beta1.PlacementDecision,
	clusterScores PrioritizerScore,
	status *framework.Status,
) error {
	placementDecisionName := placementDecision.Name
	clusterDecisions := placementDecision.Status.Decisions

	if len(clusterDecisions) > maxNumOfClusterDecisions {
		return fmt.Errorf("the number of clusterdecisions %q exceeds the max limitation %q", len(clusterDecisions), maxNumOfClusterDecisions)
	}

	existPlacementDecision, err := c.placementDecisionLister.PlacementDecisions(placementDecision.Namespace).Get(placementDecisionName)
	switch {
	case errors.IsNotFound(err):
		var err error
		existPlacementDecision, err = c.clusterClient.ClusterV1beta1().PlacementDecisions(
			placement.Namespace).Create(ctx, placementDecision, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		c.eventsRecorder.Eventf(
			placement, existPlacementDecision, corev1.EventTypeNormal,
			"DecisionCreate", "DecisionCreated",
			"Decision %s is created with placement %s in namespace %s", existPlacementDecision.Name, placement.Name, placement.Namespace)
	case err != nil:
		return err
	}

	// update the status and labels of the placementdecision if decisions change
	placementDecisionPatcher := patcher.NewPatcher[
		*clusterapiv1beta1.PlacementDecision, interface{}, clusterapiv1beta1.PlacementDecisionStatus](
		c.clusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace))

	newPlacementDecision := existPlacementDecision.DeepCopy()
	newPlacementDecision.Labels = placementDecision.Labels
	newPlacementDecision.Status.Decisions = clusterDecisions
	updated, err := placementDecisionPatcher.PatchStatus(ctx, newPlacementDecision, newPlacementDecision.Status, existPlacementDecision.Status)
	// If status has been updated, just return, this is to avoid conflict when updating the label later.
	// Labels and annotations will still be updated in next reconcile.
	if updated {
		return err
	}
	_, err = placementDecisionPatcher.PatchLabelAnnotations(ctx, newPlacementDecision, newPlacementDecision.ObjectMeta, existPlacementDecision.ObjectMeta)
	if err != nil {
		return err
	}

	// update the event with warning
	if status.Code() == framework.Warning {
		c.eventsRecorder.Eventf(
			placement, existPlacementDecision, corev1.EventTypeWarning,
			"DecisionUpdate", "DecisionUpdated",
			"Decision %s is updated with placement %s in namespace %s: %s in plugin %s", existPlacementDecision.Name, placement.Name, placement.Namespace,
			status.Message(),
			status.Plugin())
	} else {
		c.eventsRecorder.Eventf(
			placement, existPlacementDecision, corev1.EventTypeNormal,
			"DecisionUpdate", "DecisionUpdated",
			"Decision %s is updated with placement %s in namespace %s", existPlacementDecision.Name, placement.Name, placement.Namespace)
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

	c.eventsRecorder.Eventf(
		placement, existPlacementDecision, corev1.EventTypeNormal,
		"ScoreUpdate", "ScoreUpdated",
		scoreStr)

	return nil
}

func calculateLength(intOrStr *intstr.IntOrString, total int) (int, *framework.Status) {
	length := total

	switch intOrStr.Type {
	case intstr.Int:
		length = intOrStr.IntValue()
	case intstr.String:
		str := intOrStr.StrVal
		if strings.HasSuffix(str, "%") {
			f, err := strconv.ParseFloat(str[:len(str)-1], 64)
			if err != nil {
				msg := fmt.Sprintf("%v invalid type: string is not a percentage", intOrStr)
				return length, framework.NewStatus("", framework.Misconfigured, msg)
			}
			length = int(math.Ceil(f / 100 * float64(total)))
		} else {
			msg := fmt.Sprintf("%v invalid type: string is not a percentage", intOrStr)
			return length, framework.NewStatus("", framework.Misconfigured, msg)
		}
	}

	if length <= 0 || length > total {
		length = total
	}
	return length, framework.NewStatus("", framework.Success, "")
}

// filterClustersBySelector filters clusters based on the provided label selector and returns the matched clusters.
func filterClustersBySelector(
	selector clusterapiv1beta1.ClusterSelector,
	clusters []*clusterapiv1.ManagedCluster,
	clusterNames sets.Set[string],
) ([]clusterapiv1beta1.ClusterDecision, *framework.Status) {
	var matched []clusterapiv1beta1.ClusterDecision
	// create cluster label selector
	clusterSelector, err := helpers.NewClusterSelector(selector)
	if err != nil {
		status := framework.NewStatus("", framework.Misconfigured, err.Error())
		return matched, status
	}

	// filter clusters by label selector
	for _, cluster := range clusters {
		if ok := clusterSelector.Matches(cluster.Labels, helpers.GetClusterClaims(cluster)); !ok {
			continue
		}
		if !clusterNames.Has(cluster.Name) {
			continue
		}

		matched = append(matched, clusterapiv1beta1.ClusterDecision{
			ClusterName: cluster.Name,
		})
		clusterNames.Delete(cluster.Name)
	}

	return matched, framework.NewStatus("", framework.Success, "")
}

// divideDecisionGroups divide the matched clusters to the groups and ensuring that each group has the specified length.
func divideDecisionGroups(groupName string, matched []clusterapiv1beta1.ClusterDecision, groupLength int) []clusterDecisionGroup {
	var groups []clusterDecisionGroup

	// sort the matched cluster decisions by name before divide
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].ClusterName < matched[j].ClusterName
	})

	for len(matched) > 0 {
		groupClusters := matched
		if groupLength < len(matched) {
			groupClusters = matched[:groupLength]
			matched = matched[groupLength:]
		} else {
			matched = nil
		}

		decisionGroup := clusterDecisionGroup{
			decisionGroupName: groupName,
			clusterDecisions:  groupClusters,
		}
		groups = append(groups, decisionGroup)
	}

	return groups
}
