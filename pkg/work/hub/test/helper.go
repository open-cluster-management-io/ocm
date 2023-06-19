package test

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
)

func CreateTestManifestWorkReplicaSet(name string, ns string, placementName string) *workapiv1alpha1.ManifestWorkReplicaSet {
	obj := spoketesting.NewUnstructured("v1", "kind", "test-ns", "test-name")
	mw, _ := spoketesting.NewManifestWork(0, obj)
	placementRef := workapiv1alpha1.LocalPlacementReference{Name: placementName}

	mwrs := &workapiv1alpha1.ManifestWorkReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       ns,
			ResourceVersion: "316679655",
			UID:             "0b1441ec-717f-4877-a165-27e5b59245f5",
		},
		Spec: workapiv1alpha1.ManifestWorkReplicaSetSpec{
			ManifestWorkTemplate: mw.Spec,
			PlacementRefs:        []workapiv1alpha1.LocalPlacementReference{placementRef},
		},
	}
	return mwrs
}

func CreateTestManifestWorks(name, namespace string, clusters ...string) []runtime.Object {
	obj := spoketesting.NewUnstructured("v1", "kind", "test-ns", "test-name")
	works := []runtime.Object{}
	for _, c := range clusters {
		mw, _ := spoketesting.NewManifestWork(0, obj)
		mw.Name = name
		mw.Namespace = c
		mw.Labels = map[string]string{
			"work.open-cluster-management.io/manifestworkreplicaset": fmt.Sprintf("%s.%s", namespace, name),
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

// Return placement with predicate of label cluster name
func CreateTestPlacement(name string, ns string, clusters ...string) (*clusterv1beta1.Placement, *clusterv1beta1.PlacementDecision) {
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

	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: clusterv1beta1.PlacementSpec{
			Predicates: []clusterv1beta1.ClusterPredicate{clusterPredicate},
		},
	}
	placement.Status.NumberOfSelectedClusters = int32(len(clusters))

	placementDecision := &clusterv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-decision",
			Namespace: ns,
			Labels:    map[string]string{clusterv1beta1.PlacementLabel: name},
		},
	}

	decisions := []clusterv1beta1.ClusterDecision{}
	for _, cls := range clusters {
		decisions = append(decisions, clusterv1beta1.ClusterDecision{
			ClusterName: cls,
		})
	}
	placementDecision.Status.Decisions = decisions

	return placement, placementDecision
}
