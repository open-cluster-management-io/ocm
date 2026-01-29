package clusterprofile

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpfake "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned/fake"
	cpinformers "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	v1 "open-cluster-management.io/api/cluster/v1"
	v1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestLifecycleControllerSync(t *testing.T) {
	// ========== Clusters ==========
	// Clusters with ExclusiveClusterSetLabel
	cluster1 := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster1",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "default"},
		},
	}

	cluster2 := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster2",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "default"},
		},
	}

	clusterDeleting := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cluster-deleting",
			Labels:            map[string]string{v1beta2.ClusterSetLabel: "default"},
			DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
		},
	}

	clusterBoundSet := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster-bound-set",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "set-bound"},
		},
	}

	clusterUnboundSet := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster-unbound-set",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "set-unbound"},
		},
	}

	// Clusters with custom labels for LabelSelector
	clusterProdUSWest := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-prod-uswest",
			Labels: map[string]string{
				"environment": "production",
				"region":      "us-west",
			},
		},
	}

	clusterDevUSWest := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-dev-uswest",
			Labels: map[string]string{
				"environment": "development",
				"region":      "us-west",
			},
		},
	}

	clusterMixed := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-mixed",
			Labels: map[string]string{
				v1beta2.ClusterSetLabel: "default", // Also in default via ExclusiveClusterSetLabel
				"environment":           "production",
				"region":                "eu-west",
			},
		},
	}

	clusterProdEUWest := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-prod-eu-west",
			Labels: map[string]string{
				"environment": "production",
				"region":      "eu-west",
			},
		},
	}

	clusterNoLabels := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster-no-labels",
			Labels: map[string]string{},
		},
	}

	// ========== ManagedClusterSets ==========
	// ManagedClusterSet with ExclusiveClusterSetLabel selector
	defaultClusterSet := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType: v1beta2.ExclusiveClusterSetLabel,
			},
		},
	}

	clusterSetbound := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "set-bound",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType: v1beta2.ExclusiveClusterSetLabel,
			},
		},
	}

	clusterSetUnbound := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "set-unbound",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType: v1beta2.ExclusiveClusterSetLabel,
			},
		},
	}

	// ManagedClusterSet with LabelSelector environment: production
	clusterSetProdLabelSelector := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "set-prod-label-selector",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType: v1beta2.LabelSelector,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"environment": "production",
					},
				},
			},
		},
	}

	// ManagedClusterSet with LabelSelector region: us-west
	clusterSetUSWestLabelSelector := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "set-uswest-label-selector",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType: v1beta2.LabelSelector,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"region": "us-west",
					},
				},
			},
		},
	}

	clusterSetEnvMatchExpressions := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "set-env-match-expressions",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType: v1beta2.LabelSelector,
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "environment",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"production", "development"},
						},
					},
				},
			},
		},
	}

	// Global clusterset with empty selector matches ALL clusters
	globalClusterSet := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "global",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType:  v1beta2.LabelSelector,
				LabelSelector: &metav1.LabelSelector{}, // empty selector
			},
		},
	}

	// ========== ManagedClusterSetBindings ==========
	boundBindingDefault := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "ns1",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "default",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	unboundBindingDefault := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "ns2",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "default",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionFalse,
				},
			},
		},
	}

	boundBindingSet := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set-bound",
			Namespace: "ns1",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set-bound",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	unboundBindingSetUnbound := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set-unbound",
			Namespace: "ns1",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set-unbound",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionFalse, // NOT bound
				},
			},
		},
	}

	boundBindingProdLabelSelector := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set-prod-label-selector",
			Namespace: "ns-label",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set-prod-label-selector",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	boundBindingEnvMatchExpr := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set-env-match-expressions",
			Namespace: "ns-expr",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set-env-match-expressions",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	boundBindingDefaultOverlap := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "ns-overlap",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "default",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	boundBindingProdLabelSelectorOverlap := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set-prod-label-selector",
			Namespace: "ns-overlap",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set-prod-label-selector",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	boundBindingProdLabelSelectorLabelOverlap := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set-prod-label-selector",
			Namespace: "ns-label-overlap",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set-prod-label-selector",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	boundBindingUSWestLabelSelectorLabelOverlap := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set-uswest-label-selector",
			Namespace: "ns-label-overlap",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set-uswest-label-selector",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	boundBindingGlobal := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "global",
			Namespace: "ns-global",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "global",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	boundBindingDefaultComplex := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "ns-complex",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "default",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	boundBindingProdLabelSelectorComplex := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set-prod-label-selector",
			Namespace: "ns-complex",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set-prod-label-selector",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	boundBindingEnvMatchExprComplex := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set-env-match-expressions",
			Namespace: "ns-complex",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set-env-match-expressions",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	boundBindingGlobalOverlap := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "global",
			Namespace: "ns-global-overlap",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "global",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	boundBindingDefaultGlobalOverlap := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "ns-global-overlap",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "default",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	// ========== ClusterProfiles ==========
	existingProfile := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns1",
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: "cluster1",
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	staleProfile := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stale-cluster",
			Namespace: "ns1",
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: "stale-cluster",
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	// ========== Helper function ==========
	createNamespace := func(name string) *corev1.Namespace {
		return &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
	}

	// ========== Namespaces ==========
	ns1 := createNamespace("ns1")
	nsTerminating := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "ns-terminating",
			DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
		},
	}

	// ========== Test Cases ==========
	cases := []struct {
		name               string
		key                string
		namespace          *corev1.Namespace // if nil, namespace doesn't exist
		clusters           []runtime.Object
		clusterSets        []runtime.Object
		bindings           []runtime.Object
		existingProfiles   []runtime.Object
		expectedCreates    []string // cluster names that should be created
		expectedDeletes    []string // cluster names that should be deleted
		expectedNumActions int
	}{
		{
			name:               "namespace not found - skip reconciliation",
			key:                "ns-not-found",
			namespace:          nil, // namespace doesn't exist
			clusters:           []runtime.Object{cluster1, cluster2},
			clusterSets:        []runtime.Object{defaultClusterSet},
			bindings:           []runtime.Object{boundBindingDefault},
			expectedCreates:    nil,
			expectedDeletes:    nil,
			expectedNumActions: 0, // should skip reconciliation
		},
		{
			name:        "namespace terminating - skip reconciliation",
			key:         "ns-terminating",
			namespace:   nsTerminating,
			clusters:    []runtime.Object{cluster1, cluster2},
			clusterSets: []runtime.Object{defaultClusterSet},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: "ns-terminating",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "default",
					},
					Status: v1beta2.ManagedClusterSetBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:   v1beta2.ClusterSetBindingBoundType,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			expectedCreates:    nil,
			expectedDeletes:    nil,
			expectedNumActions: 0, // should skip reconciliation
		},
		{
			name:               "create profiles for bound binding",
			key:                "ns1",
			namespace:          ns1,
			clusters:           []runtime.Object{cluster1, cluster2},
			clusterSets:        []runtime.Object{defaultClusterSet},
			bindings:           []runtime.Object{boundBindingDefault},
			expectedCreates:    []string{"cluster1", "cluster2"},
			expectedNumActions: 2, // 2 creates
		},
		{
			name:               "no action for unbound binding",
			key:                "ns2",
			namespace:          createNamespace("ns2"),
			clusters:           []runtime.Object{cluster1, cluster2},
			clusterSets:        []runtime.Object{defaultClusterSet},
			bindings:           []runtime.Object{unboundBindingDefault},
			expectedCreates:    nil,
			expectedDeletes:    nil,
			expectedNumActions: 0,
		},
		{
			name:               "update existing profile (no creates)",
			key:                "ns1",
			namespace:          ns1,
			clusters:           []runtime.Object{cluster1, cluster2},
			clusterSets:        []runtime.Object{defaultClusterSet},
			bindings:           []runtime.Object{boundBindingDefault},
			existingProfiles:   []runtime.Object{existingProfile},
			expectedCreates:    []string{"cluster2"}, // only cluster2 needs to be created
			expectedNumActions: 1,                    // 1 create
		},
		{
			name:               "delete stale profile",
			key:                "ns1",
			namespace:          ns1,
			clusters:           []runtime.Object{cluster1, cluster2},
			clusterSets:        []runtime.Object{defaultClusterSet},
			bindings:           []runtime.Object{boundBindingDefault},
			existingProfiles:   []runtime.Object{existingProfile, staleProfile},
			expectedCreates:    []string{"cluster2"},
			expectedDeletes:    []string{"stale-cluster"},
			expectedNumActions: 2, // 1 create + 1 delete
		},
		{
			name:               "namespace with no bindings",
			key:                "empty-ns",
			namespace:          createNamespace("empty-ns"),
			clusters:           []runtime.Object{cluster1},
			clusterSets:        []runtime.Object{defaultClusterSet},
			bindings:           []runtime.Object{},
			expectedCreates:    nil,
			expectedNumActions: 0,
		},
		{
			name:               "multiple bindings in same namespace - non-overlapping",
			key:                "ns1",
			namespace:          ns1,
			clusters:           []runtime.Object{cluster1, cluster2, clusterBoundSet},
			clusterSets:        []runtime.Object{defaultClusterSet, clusterSetbound},
			bindings:           []runtime.Object{boundBindingDefault, boundBindingSet},
			expectedCreates:    []string{"cluster1", "cluster2", "cluster-bound-set"},
			expectedNumActions: 3, // 3 creates
		},
		{
			name:               "multiple bindings with clusters in deletion state",
			key:                "ns1",
			namespace:          ns1,
			clusters:           []runtime.Object{cluster1, clusterDeleting, cluster2},
			clusterSets:        []runtime.Object{defaultClusterSet},
			bindings:           []runtime.Object{boundBindingDefault},
			expectedCreates:    []string{"cluster1", "cluster2"}, // cluster-deleting should be skipped
			expectedNumActions: 2,                                // 2 creates (no profile for deleting cluster)
		},
		{
			name:               "multiple bindings, one unbound",
			key:                "ns1",
			namespace:          ns1,
			clusters:           []runtime.Object{cluster1, cluster2, clusterUnboundSet},
			clusterSets:        []runtime.Object{defaultClusterSet, clusterSetUnbound},
			bindings:           []runtime.Object{boundBindingDefault, unboundBindingSetUnbound},
			expectedCreates:    []string{"cluster1", "cluster2"}, // only clusters from default
			expectedNumActions: 2,                                // cluster-unbound-set should not have a profile
		},
		// LabelSelector test cases
		{
			name:               "LabelSelector - create profiles for clusters matching label selector",
			key:                "ns-label",
			namespace:          createNamespace("ns-label"),
			clusters:           []runtime.Object{clusterProdUSWest, clusterDevUSWest, clusterMixed},
			clusterSets:        []runtime.Object{clusterSetProdLabelSelector},
			bindings:           []runtime.Object{boundBindingProdLabelSelector},
			expectedCreates:    []string{"cluster-prod-uswest", "cluster-mixed"}, // only production clusters
			expectedNumActions: 2,
		},
		{
			name:               "LabelSelector with MatchExpressions - select multiple environments",
			key:                "ns-expr",
			namespace:          createNamespace("ns-expr"),
			clusters:           []runtime.Object{clusterProdUSWest, clusterDevUSWest, cluster1},
			clusterSets:        []runtime.Object{clusterSetEnvMatchExpressions},
			bindings:           []runtime.Object{boundBindingEnvMatchExpr},
			expectedCreates:    []string{"cluster-prod-uswest", "cluster-dev-uswest"},
			expectedNumActions: 2,
		},
		{
			name:               "overlap - ExclusiveClusterSetLabel and LabelSelector selecting same cluster",
			key:                "ns-overlap",
			namespace:          createNamespace("ns-overlap"),
			clusters:           []runtime.Object{cluster1, clusterMixed, clusterProdUSWest},
			clusterSets:        []runtime.Object{defaultClusterSet, clusterSetProdLabelSelector},
			bindings:           []runtime.Object{boundBindingDefaultOverlap, boundBindingProdLabelSelectorOverlap},
			expectedCreates:    []string{"cluster1", "cluster-mixed", "cluster-prod-uswest"}, // cluster-mixed should only be created once
			expectedNumActions: 3,                                                            // Should deduplicate cluster-mixed
		},
		{
			name:               "overlap - multiple LabelSelectors selecting overlapping clusters",
			key:                "ns-label-overlap",
			namespace:          createNamespace("ns-label-overlap"),
			clusters:           []runtime.Object{clusterProdUSWest, clusterProdEUWest},
			clusterSets:        []runtime.Object{clusterSetProdLabelSelector, clusterSetUSWestLabelSelector},
			bindings:           []runtime.Object{boundBindingProdLabelSelectorLabelOverlap, boundBindingUSWestLabelSelectorLabelOverlap},
			expectedCreates:    []string{"cluster-prod-uswest", "cluster-prod-eu-west"}, // cluster-prod should only be created once
			expectedNumActions: 2,                                                       // Should deduplicate cluster-prod
		},
		{
			name:               "global clusterset - matches all clusters",
			key:                "ns-global",
			namespace:          createNamespace("ns-global"),
			clusters:           []runtime.Object{cluster1, clusterProdUSWest, clusterDevUSWest, clusterNoLabels},
			clusterSets:        []runtime.Object{globalClusterSet},
			bindings:           []runtime.Object{boundBindingGlobal},
			expectedCreates:    []string{"cluster1", "cluster-prod-uswest", "cluster-dev-uswest", "cluster-no-labels"}, // all clusters including no labels
			expectedNumActions: 4,
		},
		{
			name:               "global clusterset - skips clusters in deletion",
			key:                "ns-global",
			namespace:          createNamespace("ns-global"),
			clusters:           []runtime.Object{cluster1, clusterProdUSWest, clusterDeleting},
			clusterSets:        []runtime.Object{globalClusterSet},
			bindings:           []runtime.Object{boundBindingGlobal},
			expectedCreates:    []string{"cluster1", "cluster-prod-uswest"}, // should skip cluster-deleting
			expectedNumActions: 2,
		},
		{
			name:               "global clusterset - overlap with default clusterset",
			key:                "ns-global-overlap",
			namespace:          createNamespace("ns-global-overlap"),
			clusters:           []runtime.Object{cluster1, cluster2, clusterProdUSWest},
			clusterSets:        []runtime.Object{globalClusterSet, defaultClusterSet},
			bindings:           []runtime.Object{boundBindingGlobalOverlap, boundBindingDefaultGlobalOverlap},
			expectedCreates:    []string{"cluster1", "cluster2", "cluster-prod-uswest"}, // all unique clusters, deduplicate cluster1 and cluster2
			expectedNumActions: 3,
		},
		{
			name:               "complex overlap - ExclusiveClusterSetLabel, LabelSelector with MatchLabels, and MatchExpressions",
			key:                "ns-complex",
			namespace:          createNamespace("ns-complex"),
			clusters:           []runtime.Object{cluster1, clusterMixed, clusterProdUSWest, clusterDevUSWest},
			clusterSets:        []runtime.Object{defaultClusterSet, clusterSetProdLabelSelector, clusterSetEnvMatchExpressions},
			bindings:           []runtime.Object{boundBindingDefaultComplex, boundBindingProdLabelSelectorComplex, boundBindingEnvMatchExprComplex},
			expectedCreates:    []string{"cluster1", "cluster-mixed", "cluster-prod-uswest", "cluster-dev-uswest"}, // all unique clusters
			expectedNumActions: 4,                                                                                  // Should deduplicate all overlaps
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create kubeClient with namespace if specified
			var kubeClient *kubefake.Clientset
			if c.namespace != nil {
				kubeClient = kubefake.NewSimpleClientset(c.namespace)
			} else {
				kubeClient = kubefake.NewSimpleClientset()
			}

			clusterObjects := append(c.clusters, c.clusterSets...)
			clusterObjects = append(clusterObjects, c.bindings...)
			clusterClient := clusterfake.NewSimpleClientset(clusterObjects...)
			clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 0)

			cpClient := cpfake.NewSimpleClientset(c.existingProfiles...)
			cpInformers := cpinformers.NewSharedInformerFactory(cpClient, 0)

			// Populate informers
			for _, cluster := range c.clusters {
				clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(cluster)
			}
			for _, set := range c.clusterSets {
				clusterInformers.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore().Add(set)
			}
			for _, binding := range c.bindings {
				clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings().Informer().GetStore().Add(binding)
			}
			for _, profile := range c.existingProfiles {
				cpInformers.Apis().V1alpha1().ClusterProfiles().Informer().GetStore().Add(profile)
			}

			ctrl := &clusterProfileLifecycleController{
				kubeClient:               kubeClient,
				clusterLister:            clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				clusterSetLister:         clusterInformers.Cluster().V1beta2().ManagedClusterSets().Lister(),
				clusterSetBindingLister:  clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings().Lister(),
				clusterSetBindingIndexer: clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings().Informer().GetIndexer(),
				clusterProfileClient:     cpClient,
				clusterProfileLister:     cpInformers.Apis().V1alpha1().ClusterProfiles().Lister(),
			}

			syncCtx := testingcommon.NewFakeSyncContext(t, c.key)
			err := ctrl.sync(context.TODO(), syncCtx, c.key)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Verify actions
			actions := cpClient.Actions()
			createActions := []clienttesting.CreateAction{}
			deleteActions := []clienttesting.DeleteAction{}

			for _, action := range actions {
				if action.GetVerb() == "create" {
					createActions = append(createActions, action.(clienttesting.CreateAction))
				}
				if action.GetVerb() == "delete" {
					deleteActions = append(deleteActions, action.(clienttesting.DeleteAction))
				}
			}

			if len(actions) != c.expectedNumActions {
				t.Errorf("expected %d actions, got %d: %v", c.expectedNumActions, len(actions), actions)
			}

			// Verify creates
			if len(createActions) != len(c.expectedCreates) {
				t.Errorf("expected %d creates, got %d", len(c.expectedCreates), len(createActions))
			}
			for _, expectedName := range c.expectedCreates {
				found := false
				for _, action := range createActions {
					profile := action.GetObject().(*cpv1alpha1.ClusterProfile)
					if profile.Name == expectedName {
						found = true
						// Verify labels
						if profile.Labels[cpv1alpha1.LabelClusterManagerKey] != ClusterProfileManagerName {
							t.Errorf("expected label %s, got %s", ClusterProfileManagerName, profile.Labels[cpv1alpha1.LabelClusterManagerKey])
						}
						if profile.Labels[v1.ClusterNameLabelKey] != expectedName {
							t.Errorf("expected cluster-name label %s, got %s", expectedName, profile.Labels[v1.ClusterNameLabelKey])
						}
						break
					}
				}
				if !found {
					t.Errorf("expected profile %s to be created, but it wasn't", expectedName)
				}
			}

			// Verify deletes
			if len(deleteActions) != len(c.expectedDeletes) {
				t.Errorf("expected %d deletes, got %d", len(c.expectedDeletes), len(deleteActions))
			}
			for _, expectedName := range c.expectedDeletes {
				found := false
				for _, action := range deleteActions {
					if action.GetName() == expectedName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected profile %s to be deleted, but it wasn't", expectedName)
				}
			}
		})
	}
}
