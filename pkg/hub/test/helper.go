package test

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

func CreateTestPlaceManifestWork(name string, ns string, placementName string) *workapiv1alpha1.PlaceManifestWork {
	pmw := &workapiv1alpha1.PlaceManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       ns,
			ResourceVersion: "316679655",
			UID:             "0b1441ec-717f-4877-a165-27e5b59245f5",
		},
		Spec: workapiv1alpha1.PlaceManifestWorkSpec{
			ManifestWorkTemplate: workapiv1.ManifestWorkSpec{
				Workload: workapiv1.ManifestsTemplate{
					Manifests: []workapiv1.Manifest{},
				},
			},
			PlacementRef: workapiv1alpha1.LocalPlacementReference{
				Name: placementName,
			},
		},
	}
	return pmw
}

// Return placement with predicate of label cluster name
func CreateTestPlacement(name string, ns string, clusters ...string) (*clusterv1beta1.Placement, *clusterv1beta1.PlacementDecision) {
	namereq := metav1.LabelSelectorRequirement{}
	namereq.Key = "name"
	namereq.Operator = metav1.LabelSelectorOpIn
	for _, cls := range clusters {
		namereq.Values = append(namereq.Values, cls)
	}

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
