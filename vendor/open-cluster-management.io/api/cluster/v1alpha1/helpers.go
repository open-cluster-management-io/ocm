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

// ClusterRolloutStatus holds the rollout status information for a cluster.
type ClusterRolloutStatus struct {
	// cluster name
	ClusterName string
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

// RolloutResult contains list of clusters that are timeOut, removed and required to rollOut
type RolloutResult struct {
	// ClustersToRollout is a slice of ClusterRolloutStatus that will be rolled out.
	ClustersToRollout []ClusterRolloutStatus
	// ClustersTimeOut is a slice of ClusterRolloutStatus that are timeout.
	ClustersTimeOut []ClusterRolloutStatus
	// ClustersRemoved is a slice of ClusterRolloutStatus that are removed.
	ClustersRemoved []ClusterRolloutStatus
}

// ClusterRolloutStatusFunc defines a function that return the rollout status for a given workload.
// +k8s:deepcopy-gen=false
type ClusterRolloutStatusFunc[T any] func(clusterName string, workload T) (ClusterRolloutStatus, error)

// The RolloutHandler required workload type (interface/struct) to be assigned to the generic type.
// The custom implementation of the ClusterRolloutStatusFunc is required to use the RolloutHandler.
// +k8s:deepcopy-gen=false
type RolloutHandler[T any] struct {
	// placement decision tracker
	pdTracker  *clusterv1beta1.PlacementDecisionClustersTracker
	statusFunc ClusterRolloutStatusFunc[T]
}

// NewRolloutHandler creates a new RolloutHandler with the give workload type.
func NewRolloutHandler[T any](pdTracker *clusterv1beta1.PlacementDecisionClustersTracker, statusFunc ClusterRolloutStatusFunc[T]) (*RolloutHandler[T], error) {
	if pdTracker == nil {
		return nil, fmt.Errorf("invalid placement decision tracker %v", pdTracker)
	}

	return &RolloutHandler[T]{pdTracker: pdTracker, statusFunc: statusFunc}, nil
}

// The input are a RolloutStrategy and existingClusterRolloutStatus list.
// The existing ClusterRolloutStatus list should be created using the ClusterRolloutStatusFunc to determine the current workload rollout status.
// The existing ClusterRolloutStatus list should contain all the current workloads rollout status such as ToApply, Progressing, Succeeded,
// Failed, TimeOut and Skip in order to determine the added, removed, timeout clusters and next clusters to rollout.
//
// Return the actual RolloutStrategy that take effect and a RolloutResult contain list of ClusterToRollout, ClustersTimeout and ClusterRemoved.
func (r *RolloutHandler[T]) GetRolloutCluster(rolloutStrategy RolloutStrategy, existingClusterStatus []ClusterRolloutStatus) (*RolloutStrategy, RolloutResult, error) {
	switch rolloutStrategy.Type {
	case All:
		return r.getRolloutAllClusters(rolloutStrategy, existingClusterStatus)
	case Progressive:
		return r.getProgressiveClusters(rolloutStrategy, existingClusterStatus)
	case ProgressivePerGroup:
		return r.getProgressivePerGroupClusters(rolloutStrategy, existingClusterStatus)
	default:
		return nil, RolloutResult{}, fmt.Errorf("incorrect rollout strategy type %v", rolloutStrategy.Type)
	}
}

func (r *RolloutHandler[T]) getRolloutAllClusters(rolloutStrategy RolloutStrategy, existingClusterStatus []ClusterRolloutStatus) (*RolloutStrategy, RolloutResult, error) {
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

	allClusterGroups := r.pdTracker.ExistingClusterGroupsBesides()
	allClusters := allClusterGroups.GetClusters().UnsortedList()

	// Check for removed Clusters
	currentClusterStatus, removedClusterStatus := r.getRemovedClusters(allClusterGroups, existingClusterStatus)
	rolloutResult := progressivePerCluster(allClusterGroups, len(allClusters), failureTimeout, currentClusterStatus)
	rolloutResult.ClustersRemoved = removedClusterStatus

	return &strategy, rolloutResult, nil
}

func (r *RolloutHandler[T]) getProgressiveClusters(rolloutStrategy RolloutStrategy, existingClusterStatus []ClusterRolloutStatus) (*RolloutStrategy, RolloutResult, error) {
	// Prepare the rollout strategy
	strategy := RolloutStrategy{Type: Progressive}
	strategy.Progressive = rolloutStrategy.Progressive.DeepCopy()
	if strategy.Progressive == nil {
		strategy.Progressive = &RolloutProgressive{}
	}

	// Parse timeout for non-mandatory decision groups
	failureTimeout, err := parseTimeout(strategy.Progressive.Timeout.Timeout)
	if err != nil {
		return &strategy, RolloutResult{}, err
	}

	// Check for removed clusters
	clusterGroups := r.pdTracker.ExistingClusterGroupsBesides()
	currentClusterStatus, removedClusterStatus := r.getRemovedClusters(clusterGroups, existingClusterStatus)

	// Upgrade mandatory decision groups first
	groupKeys := decisionGroupsToGroupKeys(strategy.Progressive.MandatoryDecisionGroups.MandatoryDecisionGroups)
	clusterGroups = r.pdTracker.ExistingClusterGroups(groupKeys...)

	// Perform progressive rollOut for mandatory decision groups first.
	if len(clusterGroups) > 0 {
		rolloutResult := progressivePerGroup(clusterGroups, failureTimeout, currentClusterStatus)
		if len(rolloutResult.ClustersToRollout) > 0 || len(rolloutResult.ClustersTimeOut) > 0 {
			rolloutResult.ClustersRemoved = removedClusterStatus
			return &strategy, rolloutResult, nil
		}
	}

	// Calculate the size of progressive rollOut
	// If the MaxConcurrency not defined, total clusters length is considered as maxConcurrency.
	clusterGroups = r.pdTracker.ExistingClusterGroupsBesides(groupKeys...)
	length, err := calculateRolloutSize(strategy.Progressive.MaxConcurrency, len(clusterGroups.GetClusters()))
	if err != nil {
		return &strategy, RolloutResult{}, err
	}

	// Rollout the remaining clusters
	rolloutResult := progressivePerCluster(clusterGroups, length, failureTimeout, currentClusterStatus)
	rolloutResult.ClustersRemoved = removedClusterStatus

	return &strategy, rolloutResult, nil
}

func (r *RolloutHandler[T]) getProgressivePerGroupClusters(rolloutStrategy RolloutStrategy, existingClusterStatus []ClusterRolloutStatus) (*RolloutStrategy, RolloutResult, error) {
	// Prepare the rollout strategy
	strategy := RolloutStrategy{Type: ProgressivePerGroup}
	strategy.ProgressivePerGroup = rolloutStrategy.ProgressivePerGroup.DeepCopy()
	if strategy.ProgressivePerGroup == nil {
		strategy.ProgressivePerGroup = &RolloutProgressivePerGroup{}
	}

	// Parse timeout for non-mandatory decision groups
	failureTimeout, err := parseTimeout(strategy.ProgressivePerGroup.Timeout.Timeout)
	if err != nil {
		return &strategy, RolloutResult{}, err
	}

	// Check for removed Clusters
	clusterGroups := r.pdTracker.ExistingClusterGroupsBesides()
	currentClusterStatus, removedClusterStatus := r.getRemovedClusters(clusterGroups, existingClusterStatus)

	// Upgrade mandatory decision groups first
	mandatoryDecisionGroups := strategy.ProgressivePerGroup.MandatoryDecisionGroups.MandatoryDecisionGroups
	groupKeys := decisionGroupsToGroupKeys(mandatoryDecisionGroups)
	clusterGroups = r.pdTracker.ExistingClusterGroups(groupKeys...)

	// Perform progressive rollout per group for mandatory decision groups first
	if len(clusterGroups) > 0 {
		rolloutResult := progressivePerGroup(clusterGroups, failureTimeout, currentClusterStatus)

		if len(rolloutResult.ClustersToRollout) > 0 || len(rolloutResult.ClustersTimeOut) > 0 {
			rolloutResult.ClustersRemoved = removedClusterStatus
			return &strategy, rolloutResult, nil
		}
	}

	// RollOut the rest of the decision groups
	restClusterGroups := r.pdTracker.ExistingClusterGroupsBesides(groupKeys...)

	// Perform progressive rollout per group for the remaining decision groups
	rolloutResult := progressivePerGroup(restClusterGroups, failureTimeout, currentClusterStatus)
	rolloutResult.ClustersRemoved = removedClusterStatus

	return &strategy, rolloutResult, nil
}

func (r *RolloutHandler[T]) getRemovedClusters(clusterGroupsMap clusterv1beta1.ClusterGroupsMap, existingClusterStatus []ClusterRolloutStatus) ([]ClusterRolloutStatus, []ClusterRolloutStatus) {
	var currentClusterStatus, removedClusterStatus []ClusterRolloutStatus

	clusters := clusterGroupsMap.GetClusters().UnsortedList()
	for _, clusterStatus := range existingClusterStatus {
		exist := false
		for _, cluster := range clusters {
			if clusterStatus.ClusterName == cluster {
				exist = true
				currentClusterStatus = append(currentClusterStatus, clusterStatus)
				break
			}
		}

		if !exist {
			removedClusterStatus = append(removedClusterStatus, clusterStatus)
		}
	}
	return currentClusterStatus, removedClusterStatus
}

func progressivePerCluster(clusterGroupsMap clusterv1beta1.ClusterGroupsMap, length int, timeout time.Duration, existingClusterStatus []ClusterRolloutStatus) RolloutResult {
	var rolloutClusters, timeoutClusters []ClusterRolloutStatus
	existingClusters := make(map[string]bool)

	for _, status := range existingClusterStatus {
		if status.ClusterName == "" {
			continue
		}

		existingClusters[status.ClusterName] = true
		rolloutClusters, timeoutClusters = determineRolloutStatus(status, timeout, rolloutClusters, timeoutClusters)

		if len(rolloutClusters) >= length {
			return RolloutResult{
				ClustersToRollout: rolloutClusters,
				ClustersTimeOut:   timeoutClusters,
			}
		}
	}

	clusters := clusterGroupsMap.GetClusters().UnsortedList()
	clusterToGroupKey := clusterGroupsMap.ClusterToGroupKey()
	// Sort the clusters in alphabetical order to ensure consistency.
	sort.Strings(clusters)
	for _, cluster := range clusters {
		if existingClusters[cluster] {
			continue
		}

		status := ClusterRolloutStatus{
			ClusterName: cluster,
			Status:      ToApply,
			GroupKey:    clusterToGroupKey[cluster],
		}
		rolloutClusters = append(rolloutClusters, status)

		if len(rolloutClusters) >= length {
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

func progressivePerGroup(clusterGroupsMap clusterv1beta1.ClusterGroupsMap, timeout time.Duration, existingClusterStatus []ClusterRolloutStatus) RolloutResult {
	var rolloutClusters, timeoutClusters []ClusterRolloutStatus
	existingClusters := make(map[string]bool)

	for _, status := range existingClusterStatus {
		if status.ClusterName == "" {
			continue
		}

		if status.Status == ToApply {
			// Set as false to consider the cluster in the decisionGroups iteration.
			existingClusters[status.ClusterName] = false
		} else {
			existingClusters[status.ClusterName] = true
			rolloutClusters, timeoutClusters = determineRolloutStatus(status, timeout, rolloutClusters, timeoutClusters)
		}
	}

	clusterGroupKeys := clusterGroupsMap.GetOrderedGroupKeys()
	for _, key := range clusterGroupKeys {
		if subclusters, ok := clusterGroupsMap[key]; ok {
			// Iterate through clusters in the group
			clusters := subclusters.UnsortedList()
			sort.Strings(clusters)
			for _, cluster := range clusters {
				if existingClusters[cluster] {
					continue
				}

				status := ClusterRolloutStatus{
					ClusterName: cluster,
					Status:      ToApply,
					GroupKey:    key,
				}
				rolloutClusters = append(rolloutClusters, status)
			}

			// As it is perGroup Return if there are clusters to rollOut
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

// determineRolloutStatus checks whether a cluster should continue its rollout based on its current status and timeout.
// The function update the cluster status and append it to the expected slice.
//
// The timeout parameter is utilized for handling progressing and failed statuses and any other unknown status:
//  1. If timeout is set to None (maxTimeDuration), the function will append the clusterStatus to the rollOut Clusters.
//  2. If timeout is set to 0, the function append the clusterStatus to the timeOut clusters.
func determineRolloutStatus(status ClusterRolloutStatus, timeout time.Duration, rolloutClusters []ClusterRolloutStatus, timeoutClusters []ClusterRolloutStatus) ([]ClusterRolloutStatus, []ClusterRolloutStatus) {

	switch status.Status {
	case ToApply:
		rolloutClusters = append(rolloutClusters, status)
	case TimeOut, Succeeded, Skip:
		return rolloutClusters, timeoutClusters
	default: // For progressing, failed status and any other unknown status.
		timeOutTime := getTimeOutTime(status.LastTransitionTime, timeout)
		status.TimeOutTime = timeOutTime

		// check if current time is before the timeout time
		if RolloutClock.Now().Before(timeOutTime.Time) {
			rolloutClusters = append(rolloutClusters, status)
		} else {
			status.Status = TimeOut
			timeoutClusters = append(timeoutClusters, status)
		}
	}

	return rolloutClusters, timeoutClusters
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

func calculateRolloutSize(maxConcurrency intstr.IntOrString, total int) (int, error) {
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
