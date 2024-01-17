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
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1sdk "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1beta1"
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
	GroupKey clusterv1beta1sdk.GroupKey
	// Status is the required field indicating the rollout status.
	Status RolloutStatus
	// LastTransitionTime is the last transition time of the rollout status (optional field).
	// Used to calculate timeout for progressing and failed status and minimum success time (i.e. soak
	// time) for succeeded status.
	LastTransitionTime *metav1.Time
	// TimeOutTime is the timeout time when the status is progressing or failed (optional field).
	TimeOutTime *metav1.Time
}

// RolloutResult contains list of clusters that are timeOut, removed and required to rollOut. A
// boolean is also provided signaling that the rollout may be shortened due to the number of failed
// clusters exceeding the MaxFailure threshold.
type RolloutResult struct {
	// ClustersToRollout is a slice of ClusterRolloutStatus that will be rolled out.
	ClustersToRollout []ClusterRolloutStatus
	// ClustersTimeOut is a slice of ClusterRolloutStatus that are timeout.
	ClustersTimeOut []ClusterRolloutStatus
	// ClustersRemoved is a slice of ClusterRolloutStatus that are removed.
	ClustersRemoved []ClusterRolloutStatus
	// MaxFailureBreach is a boolean signaling whether the rollout was cut short because of failed clusters.
	MaxFailureBreach bool
	// RecheckAfter is the time duration to recheck the rollout status.
	RecheckAfter *time.Duration
}

// ClusterRolloutStatusFunc defines a function that return the rollout status for a given workload.
type ClusterRolloutStatusFunc[T any] func(clusterName string, workload T) (ClusterRolloutStatus, error)

// The RolloutHandler required workload type (interface/struct) to be assigned to the generic type.
// The custom implementation of the ClusterRolloutStatusFunc is required to use the RolloutHandler.
type RolloutHandler[T any] struct {
	// placement decision tracker
	pdTracker  *clusterv1beta1sdk.PlacementDecisionClustersTracker
	statusFunc ClusterRolloutStatusFunc[T]
}

// NewRolloutHandler creates a new RolloutHandler with the given workload type.
func NewRolloutHandler[T any](pdTracker *clusterv1beta1sdk.PlacementDecisionClustersTracker, statusFunc ClusterRolloutStatusFunc[T]) (*RolloutHandler[T], error) {
	if pdTracker == nil {
		return nil, fmt.Errorf("invalid placement decision tracker %v", pdTracker)
	}

	return &RolloutHandler[T]{pdTracker: pdTracker, statusFunc: statusFunc}, nil
}

// The inputs are a RolloutStrategy and existingClusterRolloutStatus list.
// The existing ClusterRolloutStatus list should be created using the ClusterRolloutStatusFunc to determine the current workload rollout status.
// The existing ClusterRolloutStatus list should contain all the current workloads rollout status such as ToApply, Progressing, Succeeded,
// Failed, TimeOut and Skip in order to determine the added, removed, timeout clusters and next clusters to rollout.
//
// Return the actual RolloutStrategy that take effect and a RolloutResult contain list of ClusterToRollout, ClustersTimeout and ClusterRemoved.
func (r *RolloutHandler[T]) GetRolloutCluster(rolloutStrategy clusterv1alpha1.RolloutStrategy, existingClusterStatus []ClusterRolloutStatus) (*clusterv1alpha1.RolloutStrategy, RolloutResult, error) {
	switch rolloutStrategy.Type {
	case clusterv1alpha1.All:
		return r.getRolloutAllClusters(rolloutStrategy, existingClusterStatus)
	case clusterv1alpha1.Progressive:
		return r.getProgressiveClusters(rolloutStrategy, existingClusterStatus)
	case clusterv1alpha1.ProgressivePerGroup:
		return r.getProgressivePerGroupClusters(rolloutStrategy, existingClusterStatus)
	default:
		return nil, RolloutResult{}, fmt.Errorf("incorrect rollout strategy type %v", rolloutStrategy.Type)
	}
}

func (r *RolloutHandler[T]) getRolloutAllClusters(rolloutStrategy clusterv1alpha1.RolloutStrategy, existingClusterStatus []ClusterRolloutStatus) (*clusterv1alpha1.RolloutStrategy, RolloutResult, error) {
	// Prepare the rollout strategy
	strategy := clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}
	strategy.All = rolloutStrategy.All.DeepCopy()
	if strategy.All == nil {
		strategy.All = &clusterv1alpha1.RolloutAll{}
	}

	// Parse timeout for the rollout
	failureTimeout, err := parseTimeout(strategy.All.ProgressDeadline)
	if err != nil {
		return &strategy, RolloutResult{}, err
	}

	allClusterGroups := r.pdTracker.ExistingClusterGroupsBesides()
	allClusters := allClusterGroups.GetClusters().UnsortedList()

	// Check for removed Clusters
	currentClusterStatus, removedClusterStatus := r.getRemovedClusters(allClusterGroups, existingClusterStatus)
	rolloutResult := progressivePerCluster(allClusterGroups, len(allClusters), len(allClusters), time.Duration(0), failureTimeout, currentClusterStatus)
	rolloutResult.ClustersRemoved = removedClusterStatus

	return &strategy, rolloutResult, nil
}

func (r *RolloutHandler[T]) getProgressiveClusters(rolloutStrategy clusterv1alpha1.RolloutStrategy, existingClusterStatus []ClusterRolloutStatus) (*clusterv1alpha1.RolloutStrategy, RolloutResult, error) {
	// Prepare the rollout strategy
	strategy := clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.Progressive}
	strategy.Progressive = rolloutStrategy.Progressive.DeepCopy()
	if strategy.Progressive == nil {
		strategy.Progressive = &clusterv1alpha1.RolloutProgressive{}
	}
	minSuccessTime := strategy.Progressive.MinSuccessTime.Duration

	// Parse timeout for non-mandatory decision groups
	failureTimeout, err := parseTimeout(strategy.Progressive.ProgressDeadline)
	if err != nil {
		return &strategy, RolloutResult{}, err
	}

	// Check for removed clusters
	clusterGroups := r.pdTracker.ExistingClusterGroupsBesides()
	currentClusterStatus, removedClusterStatus := r.getRemovedClusters(clusterGroups, existingClusterStatus)

	// Parse maximum failure threshold for continuing the rollout, defaulting to zero
	maxFailures, err := calculateRolloutSize(strategy.Progressive.MaxFailures, len(clusterGroups.GetClusters()), 0)
	if err != nil {
		return &strategy, RolloutResult{}, fmt.Errorf("failed to parse the provided maxFailures: %w", err)
	}

	// Upgrade mandatory decision groups first
	groupKeys := decisionGroupsToGroupKeys(strategy.Progressive.MandatoryDecisionGroups.MandatoryDecisionGroups)
	clusterGroups = r.pdTracker.ExistingClusterGroups(groupKeys...)

	// Perform progressive rollOut for mandatory decision groups first, tolerating no failures
	if len(clusterGroups) > 0 {
		rolloutResult := progressivePerGroup(
			clusterGroups, intstr.FromInt32(0), minSuccessTime, failureTimeout, currentClusterStatus,
		)
		if len(rolloutResult.ClustersToRollout) > 0 || len(rolloutResult.ClustersTimeOut) > 0 {
			rolloutResult.ClustersRemoved = removedClusterStatus
			return &strategy, rolloutResult, nil
		}
	}

	// Calculate the size of progressive rollOut
	// If the MaxConcurrency not defined, total clusters length is considered as maxConcurrency.
	clusterGroups = r.pdTracker.ExistingClusterGroupsBesides(groupKeys...)
	rolloutSize, err := calculateRolloutSize(strategy.Progressive.MaxConcurrency, len(clusterGroups.GetClusters()), len(clusterGroups.GetClusters()))
	if err != nil {
		return &strategy, RolloutResult{}, fmt.Errorf("failed to parse the provided maxConcurrency: %w", err)
	}

	// Rollout the remaining clusters
	rolloutResult := progressivePerCluster(clusterGroups, rolloutSize, maxFailures, minSuccessTime, failureTimeout, currentClusterStatus)
	rolloutResult.ClustersRemoved = removedClusterStatus

	return &strategy, rolloutResult, nil
}

func (r *RolloutHandler[T]) getProgressivePerGroupClusters(rolloutStrategy clusterv1alpha1.RolloutStrategy, existingClusterStatus []ClusterRolloutStatus) (*clusterv1alpha1.RolloutStrategy, RolloutResult, error) {
	// Prepare the rollout strategy
	strategy := clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.ProgressivePerGroup}
	strategy.ProgressivePerGroup = rolloutStrategy.ProgressivePerGroup.DeepCopy()
	if strategy.ProgressivePerGroup == nil {
		strategy.ProgressivePerGroup = &clusterv1alpha1.RolloutProgressivePerGroup{}
	}
	minSuccessTime := strategy.ProgressivePerGroup.MinSuccessTime.Duration
	maxFailures := strategy.ProgressivePerGroup.MaxFailures

	// Parse timeout for non-mandatory decision groups
	failureTimeout, err := parseTimeout(strategy.ProgressivePerGroup.ProgressDeadline)
	if err != nil {
		return &strategy, RolloutResult{}, err
	}

	// Check format of MaxFailures--this value will be re-parsed and used in progressivePerGroup()
	err = parseRolloutSize(maxFailures)
	if err != nil {
		return &strategy, RolloutResult{}, fmt.Errorf("failed to parse the provided maxFailures: %w", err)
	}

	// Check for removed Clusters
	clusterGroups := r.pdTracker.ExistingClusterGroupsBesides()
	currentClusterStatus, removedClusterStatus := r.getRemovedClusters(clusterGroups, existingClusterStatus)

	// Upgrade mandatory decision groups first
	mandatoryDecisionGroups := strategy.ProgressivePerGroup.MandatoryDecisionGroups.MandatoryDecisionGroups
	groupKeys := decisionGroupsToGroupKeys(mandatoryDecisionGroups)
	clusterGroups = r.pdTracker.ExistingClusterGroups(groupKeys...)

	// Perform progressive rollout per group for mandatory decision groups first, tolerating no failures
	if len(clusterGroups) > 0 {
		rolloutResult := progressivePerGroup(clusterGroups, intstr.FromInt32(0), minSuccessTime, failureTimeout, currentClusterStatus)

		if len(rolloutResult.ClustersToRollout) > 0 || len(rolloutResult.ClustersTimeOut) > 0 {
			rolloutResult.ClustersRemoved = removedClusterStatus
			return &strategy, rolloutResult, nil
		}
	}

	// RollOut the rest of the decision groups
	restClusterGroups := r.pdTracker.ExistingClusterGroupsBesides(groupKeys...)

	// Perform progressive rollout per group for the remaining decision groups
	rolloutResult := progressivePerGroup(restClusterGroups, maxFailures, minSuccessTime, failureTimeout, currentClusterStatus)
	rolloutResult.ClustersRemoved = removedClusterStatus

	return &strategy, rolloutResult, nil
}

func (r *RolloutHandler[T]) getRemovedClusters(clusterGroupsMap clusterv1beta1sdk.ClusterGroupsMap, existingClusterStatus []ClusterRolloutStatus) ([]ClusterRolloutStatus, []ClusterRolloutStatus) {
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

// progressivePerCluster parses the rollout status for the given clusters and returns the rollout
// result. It sorts the clusters alphabetically in order to determine the rollout groupings and the
// rollout group size is determined by the MaxConcurrency setting.
func progressivePerCluster(
	clusterGroupsMap clusterv1beta1sdk.ClusterGroupsMap,
	rolloutSize int,
	maxFailures int,
	minSuccessTime time.Duration,
	timeout time.Duration,
	existingClusterStatus []ClusterRolloutStatus,
) RolloutResult {
	var rolloutClusters, timeoutClusters []ClusterRolloutStatus
	existingClusters := make(map[string]bool)
	failureCount := 0
	failureBreach := false

	// Sort existing cluster status for consistency in case ToApply was determined by the workload applier
	sort.Slice(existingClusterStatus, func(i, j int) bool {
		return existingClusterStatus[i].ClusterName < existingClusterStatus[j].ClusterName
	})

	// Collect existing cluster status and determine any TimeOut statuses
	for _, status := range existingClusterStatus {
		if status.ClusterName == "" {
			continue
		}

		existingClusters[status.ClusterName] = true

		// If there was a breach of MaxFailures, only handle clusters that have already had workload applied
		if !failureBreach || failureBreach && status.Status != ToApply {
			// For progress per cluster, the length of existing `rolloutClusters` will be compared with the
			// target rollout size to determine whether to return or not first.
			// The timeoutClusters, as well as failed clusters will be counted into failureCount, the next rollout
			// will stop if failureCount > maxFailures.
			rolloutClusters, timeoutClusters = determineRolloutStatus(&status, minSuccessTime, timeout, rolloutClusters, timeoutClusters)
		}

		// Keep track of TimeOut or Failed clusters and check total against MaxFailures
		if status.Status == TimeOut || status.Status == Failed {
			failureCount++

			failureBreach = failureCount > maxFailures
		}

		// Return if the list of exsiting rollout clusters has reached the target rollout size
		if len(rolloutClusters) >= rolloutSize {
			return RolloutResult{
				ClustersToRollout: rolloutClusters,
				ClustersTimeOut:   timeoutClusters,
				MaxFailureBreach:  failureBreach,
				RecheckAfter:      minRecheckAfter(rolloutClusters, minSuccessTime),
			}
		}
	}

	// Return if the exsiting rollout clusters maxFailures is breached.
	if failureBreach {
		return RolloutResult{
			ClustersToRollout: rolloutClusters,
			ClustersTimeOut:   timeoutClusters,
			MaxFailureBreach:  failureBreach,
			RecheckAfter:      minRecheckAfter(rolloutClusters, minSuccessTime),
		}
	}

	clusters := clusterGroupsMap.GetClusters().UnsortedList()
	clusterToGroupKey := clusterGroupsMap.ClusterToGroupKey()

	// Sort the clusters in alphabetical order to ensure consistency.
	sort.Strings(clusters)

	// Amend clusters to the rollout up to the rollout size
	for _, cluster := range clusters {
		if existingClusters[cluster] {
			continue
		}

		// For clusters without a rollout status, set the status to ToApply
		status := ClusterRolloutStatus{
			ClusterName: cluster,
			Status:      ToApply,
			GroupKey:    clusterToGroupKey[cluster],
		}
		rolloutClusters = append(rolloutClusters, status)

		// Return if the list of rollout clusters has reached the target rollout size
		if len(rolloutClusters) >= rolloutSize {
			return RolloutResult{
				ClustersToRollout: rolloutClusters,
				ClustersTimeOut:   timeoutClusters,
				RecheckAfter:      minRecheckAfter(rolloutClusters, minSuccessTime),
			}
		}
	}

	return RolloutResult{
		ClustersToRollout: rolloutClusters,
		ClustersTimeOut:   timeoutClusters,
		RecheckAfter:      minRecheckAfter(rolloutClusters, minSuccessTime),
	}
}

func progressivePerGroup(
	clusterGroupsMap clusterv1beta1sdk.ClusterGroupsMap,
	maxFailures intstr.IntOrString,
	minSuccessTime time.Duration,
	timeout time.Duration,
	existingClusterStatus []ClusterRolloutStatus,
) RolloutResult {
	var rolloutClusters, timeoutClusters []ClusterRolloutStatus
	existingClusters := make(map[string]RolloutStatus)

	// Collect existing cluster status and determine any TimeOut statuses
	for _, status := range existingClusterStatus {
		if status.ClusterName == "" {
			continue
		}

		// ToApply will be reconsidered in the decisionGroups iteration.
		if status.Status != ToApply {
			// For progress per group, the existing rollout clusters and timeout clusters status will be recored in existingClusters first,
			// then go through group by group.
			rolloutClusters, timeoutClusters = determineRolloutStatus(&status, minSuccessTime, timeout, rolloutClusters, timeoutClusters)
			existingClusters[status.ClusterName] = status.Status
		}
	}

	totalFailureCount := 0
	failureBreach := false
	clusterGroupKeys := clusterGroupsMap.GetOrderedGroupKeys()
	for _, key := range clusterGroupKeys {
		groupFailureCount := 0
		if subclusters, ok := clusterGroupsMap[key]; ok {
			// Calculate the max failure threshold for the group--the returned error was checked
			// previously, so it's ignored here
			maxGroupFailures, _ := calculateRolloutSize(maxFailures, len(subclusters), 0)
			// Iterate through clusters in the group
			clusters := subclusters.UnsortedList()
			sort.Strings(clusters)
			for _, cluster := range clusters {
				if status, ok := existingClusters[cluster]; ok {
					// Keep track of TimeOut or Failed clusters and check total against MaxFailures
					if status == TimeOut || status == Failed {
						groupFailureCount++

						failureBreach = groupFailureCount > maxGroupFailures
					}

					continue
				}

				status := ClusterRolloutStatus{
					ClusterName: cluster,
					Status:      ToApply,
					GroupKey:    key,
				}
				rolloutClusters = append(rolloutClusters, status)
			}

			totalFailureCount += groupFailureCount

			// As it is perGroup, return if there are clusters to rollOut that aren't
			// Failed/Timeout, or there was a breach of the MaxFailure configuration
			if len(rolloutClusters)-totalFailureCount > 0 || failureBreach {
				return RolloutResult{
					ClustersToRollout: rolloutClusters,
					ClustersTimeOut:   timeoutClusters,
					MaxFailureBreach:  failureBreach,
					RecheckAfter:      minRecheckAfter(rolloutClusters, minSuccessTime),
				}
			}
		}
	}

	return RolloutResult{
		ClustersToRollout: rolloutClusters,
		ClustersTimeOut:   timeoutClusters,
		MaxFailureBreach:  failureBreach,
		RecheckAfter:      minRecheckAfter(rolloutClusters, minSuccessTime),
	}
}

// determineRolloutStatus checks whether a cluster should continue its rollout based on its current
// status and timeout. The function updates the cluster status and appends it to the expected slice.
// Nothing is done for TimeOut or Skip statuses.
//
// The minSuccessTime parameter is utilized for handling succeeded clusters that are still within
// the configured soak time, in which case the cluster will be returned as a rolloutCluster.
//
// The timeout parameter is utilized for handling progressing and failed statuses and any other
// unknown status:
//  1. If timeout is set to None (maxTimeDuration), the function will append the clusterStatus to
//     the rollOut Clusters.
//  2. If timeout is set to 0, the function append the clusterStatus to the timeOut clusters.
func determineRolloutStatus(
	status *ClusterRolloutStatus,
	minSuccessTime time.Duration,
	timeout time.Duration,
	rolloutClusters []ClusterRolloutStatus,
	timeoutClusters []ClusterRolloutStatus,
) ([]ClusterRolloutStatus, []ClusterRolloutStatus) {

	switch status.Status {
	case ToApply:
		rolloutClusters = append(rolloutClusters, *status)
	case Succeeded:
		// If the cluster succeeded but is still within the MinSuccessTime (i.e. "soak" time),
		// still add it to the list of rolloutClusters
		minSuccessTimeTime := getTimeOutTime(status.LastTransitionTime, minSuccessTime)
		if RolloutClock.Now().Before(minSuccessTimeTime.Time) {
			rolloutClusters = append(rolloutClusters, *status)
		}

		return rolloutClusters, timeoutClusters
	case TimeOut, Skip:
		return rolloutClusters, timeoutClusters
	default: // For progressing, failed, or unknown status.
		timeOutTime := getTimeOutTime(status.LastTransitionTime, timeout)
		status.TimeOutTime = timeOutTime
		// check if current time is before the timeout time
		if timeOutTime == nil || RolloutClock.Now().Before(timeOutTime.Time) {
			rolloutClusters = append(rolloutClusters, *status)
		} else {
			status.Status = TimeOut
			timeoutClusters = append(timeoutClusters, *status)
		}
	}

	return rolloutClusters, timeoutClusters
}

// getTimeOutTime calculates the timeout time given a start time and duration, instantiating the
// RolloutClock if a start time isn't provided.
func getTimeOutTime(startTime *metav1.Time, timeout time.Duration) *metav1.Time {
	var timeoutTime time.Time
	// if timeout is not set (default to maxTimeDuration), the timeout time should not be set
	if timeout == maxTimeDuration {
		return nil
	}
	if startTime == nil {
		timeoutTime = RolloutClock.Now().Add(timeout)
	} else {
		timeoutTime = startTime.Add(timeout)
	}
	return &metav1.Time{Time: timeoutTime}
}

// calculateRolloutSize calculates the maximum portion from a total number of clusters by parsing a
// maximum threshold value that can be either a quantity or a percent, returning an error if the
// threshold can't be parsed to either of those.
func calculateRolloutSize(maxThreshold intstr.IntOrString, total int, defaultThreshold int) (int, error) {
	length := defaultThreshold

	// Verify the format of the IntOrString value
	err := parseRolloutSize(maxThreshold)
	if err != nil {
		return length, err
	}

	// Calculate the rollout size--errors are ignored because
	// they were handled in parseRolloutSize() previously
	switch maxThreshold.Type {
	case intstr.Int:
		length = maxThreshold.IntValue()
	case intstr.String:
		str := maxThreshold.StrVal
		f, _ := strconv.ParseFloat(str[:len(str)-1], 64)
		length = int(math.Ceil(f / 100 * float64(total)))
	}

	if length <= 0 || length > total {
		length = defaultThreshold
	}

	return length, nil
}

// parseRolloutSize parses a maximum threshold value that can be either a quantity or a percent,
// returning an error if the threshold can't be parsed to either of those.
func parseRolloutSize(maxThreshold intstr.IntOrString) error {

	switch maxThreshold.Type {
	case intstr.Int:
		break
	case intstr.String:
		str := maxThreshold.StrVal
		if strings.HasSuffix(str, "%") {
			_, err := strconv.ParseFloat(str[:len(str)-1], 64)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("'%s' is an invalid maximum threshold value: string is not a percentage", str)
		}
	default:
		return fmt.Errorf("invalid maximum threshold type %+v", maxThreshold.Type)
	}

	return nil
}

// ParseTimeout will return the maximum possible duration given "None", an empty string, or an
// invalid duration, otherwise parsing and returning the duration provided.
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

func decisionGroupsToGroupKeys(decisionsGroup []clusterv1alpha1.MandatoryDecisionGroup) []clusterv1beta1sdk.GroupKey {
	var result []clusterv1beta1sdk.GroupKey
	for _, d := range decisionsGroup {
		gk := clusterv1beta1sdk.GroupKey{}
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

func minRecheckAfter(rolloutClusters []ClusterRolloutStatus, minSuccessTime time.Duration) *time.Duration {
	var minRecheckAfter *time.Duration
	for _, r := range rolloutClusters {
		if r.TimeOutTime != nil {
			timeOut := r.TimeOutTime.Sub(RolloutClock.Now())
			if minRecheckAfter == nil || *minRecheckAfter > timeOut {
				minRecheckAfter = &timeOut
			}
		}
	}
	if minSuccessTime != 0 && (minRecheckAfter == nil || minSuccessTime < *minRecheckAfter) {
		minRecheckAfter = &minSuccessTime
	}

	return minRecheckAfter
}
