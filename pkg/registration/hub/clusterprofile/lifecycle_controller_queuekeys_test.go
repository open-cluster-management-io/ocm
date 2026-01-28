package clusterprofile

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	v1 "open-cluster-management.io/api/cluster/v1"
	v1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/ocm/pkg/registration/hub/managedclustersetbinding"
)

// indexByClusterSet is a test helper for indexing ManagedClusterSetBindings by ClusterSet name.
// In production, this indexer is registered by the managedclustersetbinding controller.
func indexByClusterSet(obj interface{}) ([]string, error) {
	binding, ok := obj.(*v1beta2.ManagedClusterSetBinding)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a ManagedClusterSetBinding, but is %T", obj)
	}
	return []string{binding.Spec.ClusterSet}, nil
}

func TestClusterToQueueKeys(t *testing.T) {
	cases := []struct {
		name         string
		cluster      *v1.ManagedCluster
		clusterSets  []runtime.Object
		bindings     []runtime.Object
		expectedKeys []string
		expectNoKeys bool
	}{
		{
			name: "cluster not in any clusterset",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-no-set",
				},
			},
			clusterSets:  []runtime.Object{},
			bindings:     []runtime.Object{},
			expectedKeys: []string{},
			expectNoKeys: true,
		},
		{
			name: "cluster in one clusterset with one binding",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster1",
					Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "set1"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set1",
						Namespace: "ns1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set1",
					},
				},
			},
			expectedKeys: []string{"ns1"},
		},
		{
			name: "cluster in one clusterset with multiple bindings",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster1",
					Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "set1"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set1",
						Namespace: "ns1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set1",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set1",
						Namespace: "ns2",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set1",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set1",
						Namespace: "ns3",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set1",
					},
				},
			},
			expectedKeys: []string{"ns1", "ns2", "ns3"},
		},
		{
			name: "cluster selected by ExclusiveClusterSetLabel only",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-exclusive",
					Labels: map[string]string{
						v1beta2.ClusterSetLabel: "set1",
					},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "set1"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set1",
						Namespace: "ns1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set1",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set1",
						Namespace: "ns3",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set1",
					},
				},
			},
			expectedKeys: []string{"ns1", "ns3"},
		},
		{
			name: "cluster selected by LabelSelector only",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-label-selector",
					Labels: map[string]string{
						"environment": "production",
						"region":      "us-west",
					},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "prod-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"environment": "production"},
							},
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prod-set",
						Namespace: "prod-ns",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "prod-set",
					},
				},
			},
			expectedKeys: []string{"prod-ns"},
		},
		{
			name: "cluster selected by multiple LabelSelector clustersets",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-multi-label",
					Labels: map[string]string{
						"environment": "production",
						"region":      "us-west",
						"tier":        "critical",
					},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "prod-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"environment": "production"},
							},
						},
					},
				},
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "us-west-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"region": "us-west"},
							},
						},
					},
				},
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "critical-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"tier": "critical"},
							},
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prod-set",
						Namespace: "app1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "prod-set",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "us-west-set",
						Namespace: "app2",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "us-west-set",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "critical-set",
						Namespace: "app3",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "critical-set",
					},
				},
			},
			// Cluster matches all three sets, should return all three namespaces
			expectedKeys: []string{"app1", "app2", "app3"},
		},
		{
			name: "cluster selected by both ExclusiveClusterSetLabel and LabelSelector (overlap)",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-overlap",
					Labels: map[string]string{
						v1beta2.ClusterSetLabel: "set1", // ExclusiveClusterSetLabel
						"environment":           "production",
						"region":                "us-west",
					},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "set1"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "prod-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"environment": "production"},
							},
						},
					},
				},
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "region-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"region": "us-west"},
							},
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set1",
						Namespace: "ns1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set1",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prod-set",
						Namespace: "ns2",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "prod-set",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "region-set",
						Namespace: "ns3",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "region-set",
					},
				},
			},
			// Cluster is selected by all three sets via different mechanisms
			expectedKeys: []string{"ns1", "ns2", "ns3"},
		},
		{
			name: "cluster selected by LabelSelector with MatchExpressions",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-expr",
					Labels: map[string]string{
						"environment": "production",
						"team":        "platform",
					},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "expr-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "environment",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"production", "staging"},
									},
								},
							},
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "expr-set",
						Namespace: "expr-ns",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "expr-set",
					},
				},
			},
			expectedKeys: []string{"expr-ns"},
		},
		{
			name: "cluster does not match LabelSelector",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-no-match",
					Labels: map[string]string{
						"environment": "development",
					},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "prod-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"environment": "production"},
							},
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prod-set",
						Namespace: "prod-ns",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "prod-set",
					},
				},
			},
			expectedKeys: []string{},
			expectNoKeys: true,
		},
		{
			name: "cluster selected by empty LabelSelector (matches all)",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "any-cluster",
					Labels: map[string]string{
						"foo": "bar",
					},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "all-clusters-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType:  v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{}, // Empty selector
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "all-clusters-set",
						Namespace: "global-ns",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "all-clusters-set",
					},
				},
			},
			expectedKeys: []string{"global-ns"},
		},
		{
			name: "cluster selected by multiple clustersets, some bound, some not",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-partial-binding",
					Labels: map[string]string{
						"environment": "production",
						"region":      "us-east",
					},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "prod-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"environment": "production"},
							},
						},
					},
				},
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "region-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"region": "us-east"},
							},
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prod-set",
						Namespace: "bound-ns",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "prod-set",
					},
				},
				// region-set has no bindings
			},
			// Only get namespace for bound clusterset
			expectedKeys: []string{"bound-ns"},
		},
		{
			name: "cluster selected by LabelSelector with complex MatchExpressions",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-complex",
					Labels: map[string]string{
						"environment": "production",
						"compliance":  "hipaa",
						"region":      "us-west-2",
					},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "complex-set"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"environment": "production",
								},
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "compliance",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"hipaa", "sox", "pci"},
									},
									{
										Key:      "region",
										Operator: metav1.LabelSelectorOpExists,
									},
								},
							},
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "complex-set",
						Namespace: "compliance-ns",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "complex-set",
					},
				},
			},
			expectedKeys: []string{"compliance-ns"},
		},
		{
			name: "cluster in clusterset with no bindings",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster1",
					Labels: map[string]string{v1beta2.ClusterSetLabel: "set-no-binding"},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "set-no-binding"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
			},
			bindings:     []runtime.Object{},
			expectedKeys: []string{},
			expectNoKeys: true,
		},
		{
			name: "cluster in multiple clustersets, some with bindings, some without",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-mixed",
					Labels: map[string]string{v1beta2.ClusterSetLabel: "set-with-binding"},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "set-with-binding"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "set-without-binding"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set-with-binding",
						Namespace: "ns1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set-with-binding",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set-with-binding",
						Namespace: "ns2",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set-with-binding",
					},
				},
			},
			expectedKeys: []string{"ns1", "ns2"},
		},
		{
			name: "cluster with binding to different clusterset (no match)",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster1",
					Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
				},
			},
			clusterSets: []runtime.Object{
				&v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{Name: "set1"},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set2",
						Namespace: "ns1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set2", // Different clusterset
					},
				},
			},
			expectedKeys: []string{},
			expectNoKeys: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := append(c.clusterSets, c.bindings...)
			clusterClient := clusterfake.NewSimpleClientset(objects...)
			clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 0)

			for _, set := range c.clusterSets {
				clusterInformers.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore().Add(set)
			}
			// Add indexer for bindings
			bindingInformer := clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings()
			err := bindingInformer.Informer().AddIndexers(cache.Indexers{
				managedclustersetbinding.ByClusterSetIndex: indexByClusterSet,
			})
			if err != nil {
				t.Fatal(err)
			}

			for _, binding := range c.bindings {
				bindingInformer.Informer().GetStore().Add(binding)
			}

			ctrl := &clusterProfileLifecycleController{
				clusterSetLister:         clusterInformers.Cluster().V1beta2().ManagedClusterSets().Lister(),
				clusterSetBindingLister:  bindingInformer.Lister(),
				clusterSetBindingIndexer: bindingInformer.Informer().GetIndexer(),
			}

			keys := ctrl.clusterToQueueKeys(c.cluster)

			if c.expectNoKeys {
				if len(keys) != 0 {
					t.Errorf("expected no keys, got %v", keys)
				}
				return
			}

			if len(keys) != len(c.expectedKeys) {
				t.Errorf("expected %d keys, got %d: %v", len(c.expectedKeys), len(keys), keys)
				return
			}

			// Convert to map for order-independent comparison
			keyMap := make(map[string]bool)
			for _, key := range keys {
				keyMap[key] = true
			}

			for _, expectedKey := range c.expectedKeys {
				if !keyMap[expectedKey] {
					t.Errorf("expected key %s not found in result %v", expectedKey, keys)
				}
			}
		})
	}
}

func TestClusterSetToQueueKeys(t *testing.T) {
	cases := []struct {
		name         string
		clusterSet   *v1beta2.ManagedClusterSet
		bindings     []runtime.Object
		expectedKeys []string
		expectNoKeys bool
	}{
		{
			name: "clusterset with no bindings",
			clusterSet: &v1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{Name: "set1"},
			},
			bindings:     []runtime.Object{},
			expectedKeys: []string{},
			expectNoKeys: true,
		},
		{
			name: "clusterset with one binding",
			clusterSet: &v1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{Name: "set1"},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set1",
						Namespace: "ns1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set1",
					},
				},
			},
			expectedKeys: []string{"ns1"},
		},
		{
			name: "clusterset with multiple bindings in different namespaces",
			clusterSet: &v1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{Name: "prod"},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prod",
						Namespace: "app1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "prod",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prod",
						Namespace: "app2",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "prod",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prod",
						Namespace: "app3",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "prod",
					},
				},
			},
			expectedKeys: []string{"app1", "app2", "app3"},
		},
		{
			name: "clusterset with bindings to different clustersets (no match)",
			clusterSet: &v1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{Name: "set1"},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set2",
						Namespace: "ns1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set2",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set3",
						Namespace: "ns2",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set3",
					},
				},
			},
			expectedKeys: []string{},
			expectNoKeys: true,
		},
		{
			name: "clusterset with mixed bindings (some match, some don't)",
			clusterSet: &v1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{Name: "target-set"},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "target-set",
						Namespace: "ns1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "target-set",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-set",
						Namespace: "ns2",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "other-set",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "target-set",
						Namespace: "ns3",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "target-set",
					},
				},
			},
			expectedKeys: []string{"ns1", "ns3"},
		},
		{
			name: "clusterset with many bindings (deduplication test)",
			clusterSet: &v1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{Name: "large-set"},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "large-set",
						Namespace: "ns1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "large-set",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "large-set",
						Namespace: "ns2",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "large-set",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "large-set",
						Namespace: "ns3",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "large-set",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "large-set",
						Namespace: "ns4",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "large-set",
					},
				},
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "large-set",
						Namespace: "ns5",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "large-set",
					},
				},
			},
			expectedKeys: []string{"ns1", "ns2", "ns3", "ns4", "ns5"},
		},
		{
			name: "empty clusterset name",
			clusterSet: &v1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{Name: ""},
			},
			bindings: []runtime.Object{
				&v1beta2.ManagedClusterSetBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "set1",
						Namespace: "ns1",
					},
					Spec: v1beta2.ManagedClusterSetBindingSpec{
						ClusterSet: "set1",
					},
				},
			},
			expectedKeys: []string{},
			expectNoKeys: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.bindings...)
			clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 0)

			// Add indexer for bindings
			bindingInformer := clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings()
			err := bindingInformer.Informer().AddIndexers(cache.Indexers{
				managedclustersetbinding.ByClusterSetIndex: indexByClusterSet,
			})
			if err != nil {
				t.Fatal(err)
			}

			for _, binding := range c.bindings {
				bindingInformer.Informer().GetStore().Add(binding)
			}

			ctrl := &clusterProfileLifecycleController{
				clusterSetBindingLister:  bindingInformer.Lister(),
				clusterSetBindingIndexer: bindingInformer.Informer().GetIndexer(),
			}

			keys := ctrl.clusterSetToQueueKeys(c.clusterSet)

			if c.expectNoKeys {
				if len(keys) != 0 {
					t.Errorf("expected no keys, got %v", keys)
				}
				return
			}

			if len(keys) != len(c.expectedKeys) {
				t.Errorf("expected %d keys, got %d: %v", len(c.expectedKeys), len(keys), keys)
				return
			}

			// Convert to map for order-independent comparison
			keyMap := make(map[string]bool)
			for _, key := range keys {
				keyMap[key] = true
			}

			for _, expectedKey := range c.expectedKeys {
				if !keyMap[expectedKey] {
					t.Errorf("expected key %s not found in result %v", expectedKey, keys)
				}
			}

			// Verify no duplicates
			if len(keys) != len(keyMap) {
				t.Errorf("duplicate keys found in result %v", keys)
			}
		})
	}
}

func TestBindingToQueueKey(t *testing.T) {
	cases := []struct {
		name        string
		binding     *v1beta2.ManagedClusterSetBinding
		expectedKey string
	}{
		{
			name: "basic binding",
			binding: &v1beta2.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "set1",
					Namespace: "ns1",
				},
				Spec: v1beta2.ManagedClusterSetBindingSpec{
					ClusterSet: "set1",
				},
			},
			expectedKey: "ns1",
		},
		{
			name: "binding with different name and clusterset",
			binding: &v1beta2.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-binding",
					Namespace: "kueue-system",
				},
				Spec: v1beta2.ManagedClusterSetBindingSpec{
					ClusterSet: "prod",
				},
			},
			expectedKey: "kueue-system",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctrl := &clusterProfileLifecycleController{}
			keys := ctrl.bindingToQueueKey(c.binding)

			if len(keys) != 1 {
				t.Errorf("expected 1 key, got %d: %v", len(keys), keys)
				return
			}

			if keys[0] != c.expectedKey {
				t.Errorf("expected key %s, got %s", c.expectedKey, keys[0])
			}
		})
	}
}

func TestProfileToQueueKey(t *testing.T) {
	ctrl := &clusterProfileLifecycleController{}

	// Test with profile
	profile := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns1",
		},
	}

	keys := ctrl.profileToQueueKey(profile)
	if len(keys) != 1 || keys[0] != "ns1" {
		t.Errorf("expected [ns1], got %v", keys)
	}
}

// Test error handling and edge cases
func TestClusterToQueueKeysErrorHandling(t *testing.T) {
	t.Run("nil cluster", func(t *testing.T) {
		ctrl := &clusterProfileLifecycleController{}
		keys := ctrl.clusterToQueueKeys(nil)
		if keys != nil {
			t.Errorf("expected nil for invalid input, got %v", keys)
		}
	})

	t.Run("wrong object type", func(t *testing.T) {
		ctrl := &clusterProfileLifecycleController{}
		wrongType := &v1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{Name: "not-a-cluster"},
		}
		keys := ctrl.clusterToQueueKeys(wrongType)
		if keys != nil {
			t.Errorf("expected nil for wrong type, got %v", keys)
		}
	})
}

func TestClusterSetToQueueKeysErrorHandling(t *testing.T) {
	t.Run("nil clusterset", func(t *testing.T) {
		ctrl := &clusterProfileLifecycleController{}
		keys := ctrl.clusterSetToQueueKeys(nil)
		if keys != nil {
			t.Errorf("expected nil for invalid input, got %v", keys)
		}
	})

	t.Run("wrong object type", func(t *testing.T) {
		ctrl := &clusterProfileLifecycleController{}
		wrongType := &v1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "not-a-clusterset"},
		}
		keys := ctrl.clusterSetToQueueKeys(wrongType)
		if keys != nil {
			t.Errorf("expected nil for wrong type, got %v", keys)
		}
	})
}

// Performance test to ensure listing bindings once is efficient
func BenchmarkClusterToQueueKeys(b *testing.B) {
	// Create a scenario with 10 clustersets and 100 bindings
	clusterSets := make([]runtime.Object, 10)
	bindings := make([]runtime.Object, 100)

	for i := 0; i < 10; i++ {
		clusterSets[i] = &v1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("set%d", i)},
			Spec: v1beta2.ManagedClusterSetSpec{
				ClusterSelector: v1beta2.ManagedClusterSelector{
					SelectorType: v1beta2.ExclusiveClusterSetLabel,
				},
			},
		}
	}

	for i := 0; i < 100; i++ {
		bindings[i] = &v1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("set%d", i%10),
				Namespace: fmt.Sprintf("ns%d", i),
			},
			Spec: v1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: fmt.Sprintf("set%d", i%10),
			},
		}
	}

	objects := append(clusterSets, bindings...)
	clusterClient := clusterfake.NewSimpleClientset(objects...)
	clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 0)

	for _, set := range clusterSets {
		clusterInformers.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore().Add(set)
	}

	// Add indexer for bindings
	bindingInformer := clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings()
	err := bindingInformer.Informer().AddIndexers(cache.Indexers{
		managedclustersetbinding.ByClusterSetIndex: indexByClusterSet,
	})
	if err != nil {
		b.Fatal(err)
	}

	for _, binding := range bindings {
		bindingInformer.Informer().GetStore().Add(binding)
	}

	ctrl := &clusterProfileLifecycleController{
		clusterSetLister:         clusterInformers.Cluster().V1beta2().ManagedClusterSets().Lister(),
		clusterSetBindingLister:  bindingInformer.Lister(),
		clusterSetBindingIndexer: bindingInformer.Informer().GetIndexer(),
	}

	cluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-cluster",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctrl.clusterToQueueKeys(cluster)
	}
}
