package v1alpha1

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/clock"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var RolloutClock = clock.Clock(clock.RealClock{})
var maxTimeDuration = time.Duration(math.MaxInt64)

// RolloutStatus represents the status of a rollout operation.
type RolloutStatus int

const (
	// ToApply indicates that the resource's desired status has not been applied yet.
	ToApply RolloutStatus = iota
	// Progressing indicates that the resource's desired status is applied and last applied status is not updated.
	Progressing
	// Succeeded indicates that the resource's desired status is applied and last applied status is successful.
	Succeeded
	// Failed indicates that the resource's desired status is applied and last applied status has failed.
	Failed
	// TimeOut indicates that the rollout status is progressing or failed and the status remains
	// for longer than the timeout, resulting in a timeout status.
	TimeOut
	// Skip indicates that the rollout should be skipped on this cluster.
	Skip
)

// ClusterRolloutStatusFunc defines a function to return the rollout status for a managed cluster.
type ClusterRolloutStatusFunc func(clusterName string) ClusterRolloutStatus

// ClusterRolloutStatus holds the rollout status information for a cluster.
type ClusterRolloutStatus struct {
	// GroupKey represents the cluster group key (optional field).
	GroupKey clusterv1beta1.GroupKey
	// Status is the required field indicating the rollout status.
	Status RolloutStatus
	// LastTransitionTime is the last transition time of the rollout status (optional field).
	// Used to calculate timeout for progressing and failed status.
	LastTransitionTime *metav1.Time
	// TimeOutTime is the timeout time when the status is progressing or failed (optional field).
	TimeOutTime *metav1.Time
}

// RolloutResult contains the clusters to be rolled out and the clusters that have timed out.
type RolloutResult struct {
	// ClustersToRollout is a map where the key is the cluster name and the value is the ClusterRolloutStatus.
	ClustersToRollout map[string]ClusterRolloutStatus
	// ClustersTimeOut is a map where the key is the cluster name and the value is the ClusterRolloutStatus.
	ClustersTimeOut map[string]ClusterRolloutStatus
}

// +k8s:deepcopy-gen=false
type RolloutHandler struct {
	// placement decision tracker
	pdTracker *clusterv1beta1.PlacementDecisionClustersTracker
}

func NewRolloutHandler(pdTracker *clusterv1beta1.PlacementDecisionClustersTracker) (*RolloutHandler, error) {
	if pdTracker == nil {
		return nil, fmt.Errorf("invalid placement decision tracker %v", pdTracker)
	}

	return &RolloutHandler{pdTracker: pdTracker}, nil
}

// The input is a duck type RolloutStrategy and a ClusterRolloutStatusFunc to return the rollout status on each managed cluster.
// Return the strategy actual take effect and a list of clusters that need to rollout and that are timeout.
//
// ClustersToRollout: If mandatory decision groups are defined in strategy, will return the clusters to rollout in mandatory decision groups first.
// When all the mandatory decision groups rollout successfully, will return the rest of the clusters that need to rollout.
//
// ClustersTimeOut: If the cluster status is Progressing or Failed, and the status lasts longer than timeout defined in strategy,
// will list them RolloutResult.ClustersTimeOut with status TimeOut.
func (r *RolloutHandler) GetRolloutCluster(rolloutStrategy RolloutStrategy, statusFunc ClusterRolloutStatusFunc) (*RolloutStrategy, RolloutResult, error) {
	switch rolloutStrategy.Type {
	case All:
		return r.getRolloutAllClusters(rolloutStrategy, statusFunc)
	case Progressive:
		return r.getProgressiveClusters(rolloutStrategy, statusFunc)
	case ProgressivePerGroup:
		return r.getProgressivePerGroupClusters(rolloutStrategy, statusFunc)
	default:
		return nil, RolloutResult{}, fmt.Errorf("incorrect rollout strategy type %v", rolloutStrategy.Type)
	}
}

func (r *RolloutHandler) getRolloutAllClusters(rolloutStrategy RolloutStrategy, statusFunc ClusterRolloutStatusFunc) (*RolloutStrategy, RolloutResult, error) {
	// Prepare the rollout strategy
	strategy := RolloutStrategy{Type: All}
	strategy.All = rolloutStrategy.All.DeepCopy()
	if strategy.All == nil {
		strategy.All = &RolloutAll{}
	}

	// Parse timeout for the rollout
	failureTimeout, err := parseTimeout(strategy.All.Timeout.Timeout)
	if err != nil {
		return &strategy, RolloutResult{}, err
	}

	// Get all clusters and perform progressive rollout
	totalClusterGroups := r.pdTracker.ExistingClusterGroupsBesides()
	totalClusters := totalClusterGroups.GetClusters().UnsortedList()
	rolloutResult := progressivePerCluster(totalClusterGroups, len(totalClusters), failureTimeout, statusFunc)

	return &strategy, rolloutResult, nil
}

func (r *RolloutHandler) getProgressiveClusters(rolloutStrategy RolloutStrategy, statusFunc ClusterRolloutStatusFunc) (*RolloutStrategy, RolloutResult, error) {
	// Prepare the rollout strategy
	strategy := RolloutStrategy{Type: Progressive}
	strategy.Progressive = rolloutStrategy.Progressive.DeepCopy()
	if strategy.Progressive == nil {
		strategy.Progressive = &RolloutProgressive{}
	}

	// Upgrade mandatory decision groups first
	groupKeys := decisionGroupsToGroupKeys(strategy.Progressive.MandatoryDecisionGroups.MandatoryDecisionGroups)
	clusterGroups := r.pdTracker.ExistingClusterGroups(groupKeys...)

	// Perform progressive rollout for mandatory decision groups
	rolloutResult := progressivePerGroup(clusterGroups, maxTimeDuration, statusFunc)
	if len(rolloutResult.ClustersToRollout) > 0 {
		return &strategy, rolloutResult, nil
	}

	// Parse timeout for non-mandatory decision groups
	failureTimeout, err := parseTimeout(strategy.Progressive.Timeout.Timeout)
	if err != nil {
		return &strategy, RolloutResult{}, err
	}

	// Calculate the length for progressive rollout
	totalClusters := r.pdTracker.ExistingClusterGroupsBesides().GetClusters()
	length, err := calculateLength(strategy.Progressive.MaxConcurrency, len(totalClusters))
	if err != nil {
		return &strategy, RolloutResult{}, err
	}

	// Upgrade the remaining clusters
	restClusterGroups := r.pdTracker.ExistingClusterGroupsBesides(clusterGroups.GetOrderedGroupKeys()...)
	rolloutResult = progressivePerCluster(restClusterGroups, length, failureTimeout, statusFunc)

	return &strategy, rolloutResult, nil
}

func (r *RolloutHandler) getProgressivePerGroupClusters(rolloutStrategy RolloutStrategy, statusFunc ClusterRolloutStatusFunc) (*RolloutStrategy, RolloutResult, error) {
	// Prepare the rollout strategy
	strategy := RolloutStrategy{Type: ProgressivePerGroup}
	strategy.ProgressivePerGroup = rolloutStrategy.ProgressivePerGroup.DeepCopy()
	if strategy.ProgressivePerGroup == nil {
		strategy.ProgressivePerGroup = &RolloutProgressivePerGroup{}
	}

	// Upgrade mandatory decision groups first
	mandatoryDecisionGroups := strategy.ProgressivePerGroup.MandatoryDecisionGroups.MandatoryDecisionGroups
	groupKeys := decisionGroupsToGroupKeys(mandatoryDecisionGroups)
	clusterGroups := r.pdTracker.ExistingClusterGroups(groupKeys...)

	// Perform progressive rollout per group for mandatory decision groups
	rolloutResult := progressivePerGroup(clusterGroups, maxTimeDuration, statusFunc)
	if len(rolloutResult.ClustersToRollout) > 0 {
		return &strategy, rolloutResult, nil
	}

	// Parse timeout for non-mandatory decision groups
	failureTimeout, err := parseTimeout(strategy.ProgressivePerGroup.Timeout.Timeout)
	if err != nil {
		return &strategy, RolloutResult{}, err
	}

	// Upgrade the rest of the decision groups
	restClusterGroups := r.pdTracker.ExistingClusterGroupsBesides(clusterGroups.GetOrderedGroupKeys()...)

	// Perform progressive rollout per group for the remaining decision groups
	rolloutResult = progressivePerGroup(restClusterGroups, failureTimeout, statusFunc)
	return &strategy, rolloutResult, nil
}

func progressivePerCluster(clusterGroupsMap clusterv1beta1.ClusterGroupsMap, length int, timeout time.Duration, statusFunc ClusterRolloutStatusFunc) RolloutResult {
	rolloutClusters := map[string]ClusterRolloutStatus{}
	timeoutClusters := map[string]ClusterRolloutStatus{}

	if length == 0 {
		return RolloutResult{
			ClustersToRollout: rolloutClusters,
			ClustersTimeOut:   timeoutClusters,
		}
	}

	clusters := clusterGroupsMap.GetClusters().UnsortedList()
	clusterToGroupKey := clusterGroupsMap.ClusterToGroupKey()

	// Sort the clusters in alphabetical order to ensure consistency.
	sort.Strings(clusters)
	for _, cluster := range clusters {
		status := statusFunc(cluster)
		if groupKey, exists := clusterToGroupKey[cluster]; exists {
			status.GroupKey = groupKey
		}

		newStatus, needToRollout := determineRolloutStatusAndContinue(status, timeout)
		status.Status = newStatus.Status
		status.TimeOutTime = newStatus.TimeOutTime

		if needToRollout {
			rolloutClusters[cluster] = status
		}
		if status.Status == TimeOut {
			timeoutClusters[cluster] = status
		}

		if len(rolloutClusters)%length == 0 && len(rolloutClusters) > 0 {
			return RolloutResult{
				ClustersToRollout: rolloutClusters,
				ClustersTimeOut:   timeoutClusters,
			}
		}
	}

	return RolloutResult{
		ClustersToRollout: rolloutClusters,
		ClustersTimeOut:   timeoutClusters,
	}
}

func progressivePerGroup(clusterGroupsMap clusterv1beta1.ClusterGroupsMap, timeout time.Duration, statusFunc ClusterRolloutStatusFunc) RolloutResult {
	rolloutClusters := map[string]ClusterRolloutStatus{}
	timeoutClusters := map[string]ClusterRolloutStatus{}

	clusterGroupKeys := clusterGroupsMap.GetOrderedGroupKeys()

	for _, key := range clusterGroupKeys {
		if subclusters, ok := clusterGroupsMap[key]; ok {
			// Iterate through clusters in the group
			for _, cluster := range subclusters.UnsortedList() {
				status := statusFunc(cluster)
				status.GroupKey = key

				newStatus, needToRollout := determineRolloutStatusAndContinue(status, timeout)
				status.Status = newStatus.Status
				status.TimeOutTime = newStatus.TimeOutTime

				if needToRollout {
					rolloutClusters[cluster] = status
				}
				if status.Status == TimeOut {
					timeoutClusters[cluster] = status
				}
			}

			// Return if there are clusters to rollout
			if len(rolloutClusters) > 0 {
				return RolloutResult{
					ClustersToRollout: rolloutClusters,
					ClustersTimeOut:   timeoutClusters,
				}
			}
		}
	}

	return RolloutResult{
		ClustersToRollout: rolloutClusters,
		ClustersTimeOut:   timeoutClusters,
	}
}

// determineRolloutStatusAndContinue checks whether a cluster should continue its rollout based on
// its current status and timeout. The function returns an updated cluster status and a boolean
// indicating whether the rollout should continue.
//
// The timeout parameter is utilized for handling progressing and failed statuses:
//  1. If timeout is set to None (maxTimeDuration), the function will wait until cluster reaching a success status.
//     It returns true to include the cluster in the result and halts the rollout of other clusters or groups.
//  2. If timeout is set to 0, the function proceeds with upgrading other clusters without waiting.
//     It returns false to skip waiting for the cluster to reach a success status and continues to rollout others.
func determineRolloutStatusAndContinue(status ClusterRolloutStatus, timeout time.Duration) (*ClusterRolloutStatus, bool) {
	newStatus := status.DeepCopy()
	switch status.Status {
	case ToApply:
		return newStatus, true
	case TimeOut, Succeeded, Skip:
		return newStatus, false
	case Progressing, Failed:
		timeOutTime := getTimeOutTime(status.LastTransitionTime, timeout)
		newStatus.TimeOutTime = timeOutTime

		// check if current time is before the timeout time
		if RolloutClock.Now().Before(timeOutTime.Time) {
			return newStatus, true
		} else {
			newStatus.Status = TimeOut
			return newStatus, false
		}
	default:
		return newStatus, true
	}
}

// get the timeout time
func getTimeOutTime(startTime *metav1.Time, timeout time.Duration) *metav1.Time {
	var timeoutTime time.Time
	if startTime == nil {
		timeoutTime = RolloutClock.Now().Add(timeout)
	} else {
		timeoutTime = startTime.Add(timeout)
	}
	return &metav1.Time{Time: timeoutTime}
}

func calculateLength(maxConcurrency intstr.IntOrString, total int) (int, error) {
	length := total

	switch maxConcurrency.Type {
	case intstr.Int:
		length = maxConcurrency.IntValue()
	case intstr.String:
		str := maxConcurrency.StrVal
		if strings.HasSuffix(str, "%") {
			f, err := strconv.ParseFloat(str[:len(str)-1], 64)
			if err != nil {
				return length, err
			}
			length = int(math.Ceil(f / 100 * float64(total)))
		} else {
			return length, fmt.Errorf("%v invalid type: string is not a percentage", maxConcurrency)
		}
	default:
		return length, fmt.Errorf("incorrect MaxConcurrency type %v", maxConcurrency.Type)
	}

	if length <= 0 || length > total {
		length = total
	}

	return length, nil
}

func parseTimeout(timeoutStr string) (time.Duration, error) {
	// Define the regex pattern to match the timeout string
	pattern := "^(([0-9])+[h|m|s])|None$"
	regex := regexp.MustCompile(pattern)

	if timeoutStr == "None" || timeoutStr == "" {
		// If the timeout is "None" or empty, return the maximum duration
		return maxTimeDuration, nil
	}

	// Check if the timeout string matches the pattern
	if !regex.MatchString(timeoutStr) {
		return maxTimeDuration, fmt.Errorf("invalid timeout format")
	}

	return time.ParseDuration(timeoutStr)
}

func decisionGroupsToGroupKeys(decisionsGroup []MandatoryDecisionGroup) []clusterv1beta1.GroupKey {
	result := []clusterv1beta1.GroupKey{}
	for _, d := range decisionsGroup {
		gk := clusterv1beta1.GroupKey{}
		// GroupName is considered first to select the decisionGroups then GroupIndex.
		if d.GroupName != "" {
			gk.GroupName = d.GroupName
		} else {
			gk.GroupIndex = d.GroupIndex
		}
		result = append(result, gk)
	}
	return result
}
