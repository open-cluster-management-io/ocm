package managedcluster

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestGetHubClusterSetLabel(t *testing.T) {
	cases := []struct {
		name     string
		hubHash  string
		expected string
	}{
		{
			name:     "normal hub hash",
			hubHash:  "abcd1234",
			expected: "clusterset.open-cluster-management.io/abcd1234",
		},
		{
			name:     "empty hub hash",
			hubHash:  "",
			expected: "clusterset.open-cluster-management.io/",
		},
		{
			name:     "long hub hash gets truncated",
			hubHash:  "this-is-a-very-long-hub-hash-that-exceeds-the-maximum-label-name-length-limit",
			expected: "clusterset.open-cluster-management.io/this-is-a-very-long-hub-hash-that-exceeds-the-maximum-label-nam",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := GetHubClusterSetLabel(c.hubHash)
			if result != c.expected {
				t.Errorf("expected %q, got %q", c.expected, result)
			}
		})
	}
}

func TestManagedNamespaceReconcile_reconcile(t *testing.T) {
	testHubHash := "test-hub-hash"
	testClusterSetLabel := GetHubClusterSetLabel(testHubHash)

	cases := []struct {
		name                   string
		cluster                *clusterv1.ManagedCluster
		existingNamespaces     []runtime.Object
		validateActions        func(t *testing.T, actions []clienttesting.Action)
		validateClusterStatus  func(t *testing.T, cluster *clusterv1.ManagedCluster)
		expectedReconcileState reconcileState
		expectedErr            string
	}{
		{
			name:    "cluster being deleted should cleanup all namespaces",
			cluster: testinghelpers.NewDeletingManagedCluster(),
			existingNamespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace",
						Labels: map[string]string{
							testClusterSetLabel: "true",
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// resourceapply.ApplyNamespace performs GET then CREATE/UPDATE for each namespace
				if len(actions) != 2 {
					t.Errorf("expected 2 actions (get + update), got %d", len(actions))
					return
				}
				if actions[0].GetVerb() != "get" {
					t.Errorf("expected first action to be get, got %s", actions[0].GetVerb())
				}
				if actions[1].GetVerb() != "update" {
					t.Errorf("expected second action to be update, got %s", actions[1].GetVerb())
				}
			},
			expectedReconcileState: reconcileContinue,
		},
		{
			name:    "no managed namespaces should cleanup previously managed ones",
			cluster: testinghelpers.NewJoinedManagedCluster(),
			existingNamespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "old-managed-namespace",
						Labels: map[string]string{
							testClusterSetLabel: "true",
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// resourceapply.ApplyNamespace performs GET then CREATE/UPDATE for each namespace
				if len(actions) != 2 {
					t.Errorf("expected 2 actions (get + update), got %d", len(actions))
					return
				}
				if actions[0].GetVerb() != "get" {
					t.Errorf("expected first action to be get, got %s", actions[0].GetVerb())
				}
				if actions[1].GetVerb() != "update" {
					t.Errorf("expected second action to be update, got %s", actions[1].GetVerb())
				}
			},
			expectedReconcileState: reconcileContinue,
		},
		{
			name: "create new managed namespaces",
			cluster: newManagedClusterWithManagedNamespaces([]clusterv1.ClusterSetManagedNamespaceConfig{
				{
					ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{
						Name: "test-namespace-1",
					},
					ClusterSet: "clusterset-1",
				},
				{
					ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{
						Name: "test-namespace-2",
					},
					ClusterSet: "clusterset-2",
				},
			}),
			existingNamespaces: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// resourceapply.ApplyNamespace performs GET then CREATE for each new namespace (2 namespaces = 4 actions)
				if len(actions) != 4 {
					t.Errorf("expected 4 actions (2 * (get + create)), got %d", len(actions))
					return
				}
				// Check that we have alternating get and create actions
				for i := 0; i < len(actions); i += 2 {
					if actions[i].GetVerb() != "get" {
						t.Errorf("expected action %d to be get, got %s", i, actions[i].GetVerb())
					}
					if i+1 < len(actions) && actions[i+1].GetVerb() != "create" {
						t.Errorf("expected action %d to be create, got %s", i+1, actions[i+1].GetVerb())
					}
				}
			},
			validateClusterStatus: func(t *testing.T, cluster *clusterv1.ManagedCluster) {
				if len(cluster.Status.ManagedNamespaces) != 2 {
					t.Errorf("expected 2 managed namespaces, got %d", len(cluster.Status.ManagedNamespaces))
					return
				}
				for _, managedNS := range cluster.Status.ManagedNamespaces {
					if len(managedNS.Conditions) != 1 {
						t.Errorf("expected 1 condition for namespace %s, got %d", managedNS.Name, len(managedNS.Conditions))
						continue
					}
					condition := managedNS.Conditions[0]
					if condition.Type != ConditionNamespaceAvailable {
						t.Errorf("expected condition type %s, got %s", ConditionNamespaceAvailable, condition.Type)
					}
					if condition.Status != metav1.ConditionTrue {
						t.Errorf("expected condition status True, got %s", condition.Status)
					}
					if condition.Reason != ReasonNamespaceApplied {
						t.Errorf("expected reason %s, got %s", ReasonNamespaceApplied, condition.Reason)
					}
				}
			},
			expectedReconcileState: reconcileContinue,
		},
		{
			name: "update existing managed namespaces and cleanup old ones",
			cluster: newManagedClusterWithManagedNamespaces([]clusterv1.ClusterSetManagedNamespaceConfig{
				{
					ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{
						Name: "test-namespace-1",
					},
					ClusterSet: "clusterset-1",
				},
			}),
			existingNamespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace-1",
						Labels: map[string]string{
							"other-label": "value", // Different labels to ensure update is needed
						},
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "old-namespace",
						Labels: map[string]string{
							testClusterSetLabel: "true",
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// Should have 4 actions: (get + update) for existing namespace, (get + update) for cleaning up old namespace
				if len(actions) != 4 {
					t.Errorf("expected 4 actions (2 * (get + update)), got %d", len(actions))
					return
				}
				// Check that we have alternating get and update actions
				for i := 0; i < len(actions); i += 2 {
					if actions[i].GetVerb() != "get" {
						t.Errorf("expected action %d to be get, got %s", i, actions[i].GetVerb())
					}
					if i+1 < len(actions) && actions[i+1].GetVerb() != "update" {
						t.Errorf("expected action %d to be update, got %s", i+1, actions[i+1].GetVerb())
					}
				}
			},
			expectedReconcileState: reconcileContinue,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create fake clients
			spokeKubeClient := kubefake.NewSimpleClientset(c.existingNamespaces...)
			spokeKubeInformerFactory := kubeinformers.NewSharedInformerFactory(spokeKubeClient, time.Minute*10)

			// Add existing namespaces to informer store
			namespaceStore := spokeKubeInformerFactory.Core().V1().Namespaces().Informer().GetStore()
			for _, obj := range c.existingNamespaces {
				if ns, ok := obj.(*corev1.Namespace); ok {
					if err := namespaceStore.Add(ns); err != nil {
						t.Fatal(err)
					}
				}
			}

			// Create reconciler
			reconciler := &managedNamespaceReconcile{
				hubClusterSetLabel:   GetHubClusterSetLabel(testHubHash),
				spokeKubeClient:      spokeKubeClient,
				spokeNamespaceLister: spokeKubeInformerFactory.Core().V1().Namespaces().Lister(),
			}

			// Run reconcile
			ctx := context.TODO()
			syncCtx := testingcommon.NewFakeSyncContext(t, "")
			updatedCluster, state, err := reconciler.reconcile(ctx, syncCtx, c.cluster)

			// Validate error
			if c.expectedErr == "" && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if c.expectedErr != "" && (err == nil || err.Error() != c.expectedErr) {
				t.Errorf("expected error %q, got %v", c.expectedErr, err)
			}

			// Validate reconcile state
			if state != c.expectedReconcileState {
				t.Errorf("expected reconcile state %v, got %v", c.expectedReconcileState, state)
			}

			// Validate actions
			if c.validateActions != nil {
				c.validateActions(t, spokeKubeClient.Actions())
			}

			// Validate cluster status
			if c.validateClusterStatus != nil {
				c.validateClusterStatus(t, updatedCluster)
			}
		})
	}
}

func TestManagedNamespaceReconcile_createOrUpdateNamespace(t *testing.T) {
	testHubHash := "test-hub-hash"
	testClusterSetLabel := GetHubClusterSetLabel(testHubHash)

	cases := []struct {
		name               string
		nsName             string
		clusterSetName     string
		existingNamespaces []runtime.Object
		validateActions    func(t *testing.T, actions []clienttesting.Action)
		expectedErr        string
	}{
		{
			name:           "create new namespace",
			nsName:         "test-namespace",
			clusterSetName: "test-clusterset",
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// resourceapply.ApplyNamespace performs GET then CREATE for new namespace
				if len(actions) != 2 {
					t.Errorf("expected 2 actions (get + create), got %d", len(actions))
					return
				}
				if actions[0].GetVerb() != "get" {
					t.Errorf("expected first action to be get, got %s", actions[0].GetVerb())
				}
				if actions[1].GetVerb() != "create" {
					t.Errorf("expected second action to be create, got %s", actions[1].GetVerb())
				}
				if actions[0].GetResource().Resource != "namespaces" {
					t.Errorf("expected namespaces resource, got %s", actions[0].GetResource().Resource)
				}
			},
		},
		{
			name:           "update existing namespace",
			nsName:         "existing-namespace",
			clusterSetName: "test-clusterset",
			existingNamespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "existing-namespace",
						Labels: map[string]string{
							"other-label": "value",
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// resourceapply.ApplyNamespace performs GET then UPDATE for existing namespace
				if len(actions) != 2 {
					t.Errorf("expected 2 actions (get + update), got %d", len(actions))
					return
				}
				if actions[0].GetVerb() != "get" {
					t.Errorf("expected first action to be get, got %s", actions[0].GetVerb())
				}
				if actions[1].GetVerb() != "update" {
					t.Errorf("expected second action to be update, got %s", actions[1].GetVerb())
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create fake client
			spokeKubeClient := kubefake.NewSimpleClientset(c.existingNamespaces...)

			// Create reconciler
			reconciler := &managedNamespaceReconcile{
				hubClusterSetLabel: testClusterSetLabel,
				spokeKubeClient:    spokeKubeClient,
			}

			// Run createOrUpdateNamespace
			ctx := context.TODO()
			syncCtx := testingcommon.NewFakeSyncContext(t, "")
			err := reconciler.createOrUpdateNamespace(ctx, syncCtx, c.nsName, c.clusterSetName)

			// Validate error
			if c.expectedErr == "" && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if c.expectedErr != "" && (err == nil || err.Error() != c.expectedErr) {
				t.Errorf("expected error %q, got %v", c.expectedErr, err)
			}

			// Validate actions
			if c.validateActions != nil {
				c.validateActions(t, spokeKubeClient.Actions())
			}
		})
	}
}

func TestManagedNamespaceReconcile_cleanupPreviouslyManagedNamespaces(t *testing.T) {
	testHubHash := "test-hub-hash"
	testClusterSetLabel := GetHubClusterSetLabel(testHubHash)

	cases := []struct {
		name               string
		currentManagedNS   sets.Set[string]
		existingNamespaces []runtime.Object
		validateActions    func(t *testing.T, actions []clienttesting.Action)
		expectedErr        string
	}{
		{
			name:             "no existing namespaces",
			currentManagedNS: sets.Set[string]{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expected 0 actions, got %d", len(actions))
				}
			},
		},
		{
			name:             "cleanup unmanaged namespaces",
			currentManagedNS: sets.New("keep-namespace"),
			existingNamespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "keep-namespace",
						Labels: map[string]string{
							testClusterSetLabel: "true",
						},
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cleanup-namespace",
						Labels: map[string]string{
							testClusterSetLabel: "true",
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// Should only cleanup the namespace that's no longer managed (get + update)
				if len(actions) != 2 {
					t.Errorf("expected 2 actions (get + update), got %d", len(actions))
					return
				}
				if actions[0].GetVerb() != "get" {
					t.Errorf("expected first action to be get, got %s", actions[0].GetVerb())
				}
				if actions[1].GetVerb() != "update" {
					t.Errorf("expected second action to be update, got %s", actions[1].GetVerb())
				}
			},
		},
		{
			name:             "cleanup all namespaces when none are managed",
			currentManagedNS: sets.Set[string]{},
			existingNamespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cleanup-namespace-1",
						Labels: map[string]string{
							testClusterSetLabel: "true",
						},
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cleanup-namespace-2",
						Labels: map[string]string{
							testClusterSetLabel: "true",
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// Should cleanup both namespaces (2 * (get + update) = 4 actions)
				if len(actions) != 4 {
					t.Errorf("expected 4 actions (2 * (get + update)), got %d", len(actions))
					return
				}
				// Check that we have alternating get and update actions
				for i := 0; i < len(actions); i += 2 {
					if actions[i].GetVerb() != "get" {
						t.Errorf("expected action %d to be get, got %s", i, actions[i].GetVerb())
					}
					if i+1 < len(actions) && actions[i+1].GetVerb() != "update" {
						t.Errorf("expected action %d to be update, got %s", i+1, actions[i+1].GetVerb())
					}
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create fake client and informer
			spokeKubeClient := kubefake.NewSimpleClientset(c.existingNamespaces...)
			spokeKubeInformerFactory := kubeinformers.NewSharedInformerFactory(spokeKubeClient, time.Minute*10)

			// Add existing namespaces to informer store
			namespaceStore := spokeKubeInformerFactory.Core().V1().Namespaces().Informer().GetStore()
			for _, obj := range c.existingNamespaces {
				if ns, ok := obj.(*corev1.Namespace); ok {
					if err := namespaceStore.Add(ns); err != nil {
						t.Fatal(err)
					}
				}
			}

			// Create reconciler
			reconciler := &managedNamespaceReconcile{
				hubClusterSetLabel:   testClusterSetLabel,
				spokeKubeClient:      spokeKubeClient,
				spokeNamespaceLister: spokeKubeInformerFactory.Core().V1().Namespaces().Lister(),
			}

			// Run cleanupPreviouslyManagedNamespaces
			ctx := context.TODO()
			syncCtx := testingcommon.NewFakeSyncContext(t, "")
			err := reconciler.cleanupPreviouslyManagedNamespaces(ctx, syncCtx, c.currentManagedNS)

			// Validate error
			if c.expectedErr == "" && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if c.expectedErr != "" && (err == nil || err.Error() != c.expectedErr) {
				t.Errorf("expected error %q, got %v", c.expectedErr, err)
			}

			// Validate actions
			if c.validateActions != nil {
				c.validateActions(t, spokeKubeClient.Actions())
			}
		})
	}
}

// Helper function to create a ManagedCluster with managed namespaces
func newManagedClusterWithManagedNamespaces(managedNamespaces []clusterv1.ClusterSetManagedNamespaceConfig) *clusterv1.ManagedCluster {
	cluster := testinghelpers.NewJoinedManagedCluster()
	cluster.Status.ManagedNamespaces = managedNamespaces
	return cluster
}
