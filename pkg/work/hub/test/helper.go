package test

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
)

func CreateTestManifestWorkReplicaSet(name string, ns string, placementNames ...string) *workapiv1alpha1.ManifestWorkReplicaSet {
	placements := make(map[string]clusterv1alpha1.RolloutStrategy)
	allRollOut := clusterv1alpha1.RolloutStrategy{
		Type: clusterv1alpha1.All,
		All: &clusterv1alpha1.RolloutAll{
			RolloutConfig: clusterv1alpha1.RolloutConfig{
				ProgressDeadline: "None",
			},
		},
	}

	for _, plcName := range placementNames {
		placements[plcName] = allRollOut
	}

	return CreateTestManifestWorkReplicaSetWithRollOutStrategy(name, ns, placements)
}

func CreateTestManifestWorkReplicaSetWithRollOutStrategy(name string, ns string,
	placements map[string]clusterv1alpha1.RolloutStrategy) *workapiv1alpha1.ManifestWorkReplicaSet {
	obj := testingcommon.NewUnstructured("v1", "kind", "test-ns", "test-name")
	mw, _ := spoketesting.NewManifestWork(0, obj)
	var placementRefs []workapiv1alpha1.LocalPlacementReference

	for placementName, rollOut := range placements {
		placementRefs = append(placementRefs, workapiv1alpha1.LocalPlacementReference{
			Name:            placementName,
			RolloutStrategy: rollOut,
		})
	}

	mwrs := &workapiv1alpha1.ManifestWorkReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       ns,
			ResourceVersion: "316679655",
			UID:             "0b1441ec-717f-4877-a165-27e5b59245f5",
		},
		Spec: workapiv1alpha1.ManifestWorkReplicaSetSpec{
			ManifestWorkTemplate: mw.Spec,
			PlacementRefs:        placementRefs,
		},
	}
	return mwrs
}

func CreateTestManifestWorks(name, namespace string, placementName string, clusters ...string) []runtime.Object {
	obj := testingcommon.NewUnstructured("v1", "kind", "test-ns", "test-name")
	var works []runtime.Object
	for _, c := range clusters {
		mw, _ := spoketesting.NewManifestWork(0, obj)
		mw.Name = name
		mw.Namespace = c
		mw.Labels = map[string]string{
			"work.open-cluster-management.io/manifestworkreplicaset": fmt.Sprintf("%s.%s", namespace, name),
			"work.open-cluster-management.io/placementname":          placementName,
		}
		meta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
			Type:   workapiv1.WorkApplied,
			Status: metav1.ConditionTrue,
		})
		meta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
			Type:   workapiv1.WorkAvailable,
			Status: metav1.ConditionTrue,
		})
		works = append(works, mw)
	}
	return works
}

func CreateTestManifestWork(name, namespace string, placementName string, clusterName string) *workapiv1.ManifestWork {
	obj := testingcommon.NewUnstructured("v1", "kind", "test-ns", "test-name")
	mw, _ := spoketesting.NewManifestWork(0, obj)
	mw.Name = name
	mw.Namespace = clusterName
	mw.Labels = map[string]string{
		"work.open-cluster-management.io/manifestworkreplicaset": fmt.Sprintf("%s.%s", namespace, name),
		"work.open-cluster-management.io/placementname":          placementName,
	}
	meta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
		Type:   workapiv1.WorkApplied,
		Status: metav1.ConditionTrue,
	})
	meta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
		Type:   workapiv1.WorkAvailable,
		Status: metav1.ConditionTrue,
	})

	return mw
}

func CreateTestManifestWorkSpecWithSecret(mApiVersion string, mKind string, mNS string, mName string) workapiv1.ManifestWorkSpec {
	secret := testingcommon.NewUnstructuredSecret(mName, mNS, true, "0b1441ec-717f-4877-a165-27e5b59245f6")
	obj := testingcommon.NewUnstructuredWithContent(mApiVersion, mKind, mNS, mName, secret.Object)
	mw, _ := spoketesting.NewManifestWork(0, obj)

	return mw.Spec
}

// CreateTestPlacement Return placement with predicate of label cluster name
func CreateTestPlacement(name string, ns string, clusters ...string) (*clusterv1beta1.Placement, *clusterv1beta1.PlacementDecision) {
	placement, placementDesicions := CreateTestPlacementWithDecisionStrategy(name, ns, len(clusters), clusters...)

	if len(placementDesicions) < 1 {
		placementDecision := &clusterv1beta1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "-decision",
				Namespace: ns,
				Labels:    map[string]string{clusterv1beta1.PlacementLabel: name, clusterv1beta1.DecisionGroupIndexLabel: "0"},
			},
		}
		return placement, placementDecision
	}

	return placement, placementDesicions[0]
}

func CreateTestPlacementWithDecisionStrategy(name string, ns string, clsPerDecisionGroup int,
	clusters ...string) (*clusterv1beta1.Placement, []*clusterv1beta1.PlacementDecision) {
	namereq := metav1.LabelSelectorRequirement{}
	namereq.Key = "name"
	namereq.Operator = metav1.LabelSelectorOpIn
	namereq.Values = append(namereq.Values, clusters...)

	labelSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{namereq},
	}

	clusterPredicate := clusterv1beta1.ClusterPredicate{
		RequiredClusterSelector: clusterv1beta1.ClusterSelector{
			LabelSelector: *labelSelector,
		},
	}

	groupStrategy := clusterv1beta1.GroupStrategy{
		ClustersPerDecisionGroup: intstr.FromInt32(int32(clsPerDecisionGroup)), //nolint:gosec
	}
	decisionStrategy := clusterv1beta1.DecisionStrategy{
		GroupStrategy: groupStrategy,
	}
	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: clusterv1beta1.PlacementSpec{
			Predicates:       []clusterv1beta1.ClusterPredicate{clusterPredicate},
			DecisionStrategy: decisionStrategy,
		},
	}

	var decisionGroups []clusterv1beta1.DecisionGroupStatus
	var plcDecisions []*clusterv1beta1.PlacementDecision
	clusterGroups := getClusterGroups(clusters, clsPerDecisionGroup)

	for i, clsGroup := range clusterGroups {
		plcDecisionName := fmt.Sprintf("%s-decision-%d", name, i)
		placementDecision := &clusterv1beta1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      plcDecisionName,
				Namespace: ns,
				Labels: map[string]string{clusterv1beta1.PlacementLabel: name,
					clusterv1beta1.DecisionGroupIndexLabel: fmt.Sprintf("%d", i)},
			},
		}

		var decisions []clusterv1beta1.ClusterDecision
		for _, cls := range clsGroup {
			decisions = append(decisions, clusterv1beta1.ClusterDecision{
				ClusterName: cls,
			})
		}
		placementDecision.Status.Decisions = decisions
		plcDecisions = append(plcDecisions, placementDecision)

		decisionGroupStatus := clusterv1beta1.DecisionGroupStatus{
			DecisionGroupIndex: int32(i), //nolint:gosec
			DecisionGroupName:  "",
			Decisions:          []string{plcDecisionName},
			ClustersCount:      int32(len(clsGroup)), //nolint:gosec
		}

		decisionGroups = append(decisionGroups, decisionGroupStatus)
	}

	placement.Status.NumberOfSelectedClusters = int32(len(clusters)) //nolint:gosec
	placement.Status.DecisionGroups = decisionGroups

	return placement, plcDecisions
}

func getClusterGroups(clusters []string, clsPerDecisionGroup int) [][]string {
	var clusterGroups [][]string

	if clsPerDecisionGroup < 1 {
		return clusterGroups
	}

	decisionGroupCount := len(clusters) / clsPerDecisionGroup
	if len(clusters)%clsPerDecisionGroup > 0 {
		decisionGroupCount++
	}

	for i := 0; i < decisionGroupCount; i++ {
		idx := i * clsPerDecisionGroup
		idxLast := idx + clsPerDecisionGroup
		if idxLast > len(clusters) {
			idxLast = len(clusters)
		}
		clusterGroups = append(clusterGroups, clusters[idx:idxLast])
	}

	return clusterGroups
}
