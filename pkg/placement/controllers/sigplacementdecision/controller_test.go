package sigplacementdecision

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpfake "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned/fake"
	cpinformers "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	v1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func newOCMPlacementDecision(namespace, name, placementName string, groupIndex string, decisions []v1beta1.ClusterDecision) *v1beta1.PlacementDecision {
	labels := map[string]string{}
	if placementName != "" {
		labels[v1beta1.PlacementLabel] = placementName
	}
	if groupIndex != "" {
		labels[v1beta1.DecisionGroupIndexLabel] = groupIndex
	}

	return &v1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Status: v1beta1.PlacementDecisionStatus{
			Decisions: decisions,
		},
	}
}

func newSIGPlacementDecision(namespace, name string, labels map[string]string, decisions []cpv1alpha1.ClusterDecision, schedulerName string) *cpv1alpha1.PlacementDecision {
	return &cpv1alpha1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Decisions:     decisions,
		SchedulerName: schedulerName,
	}
}

func TestSIGPlacementDecisionControllerSync(t *testing.T) {
	cases := []struct {
		name            string
		key             string
		ocmPDs          []runtime.Object
		existingSIGPDs  []runtime.Object
		expectedCreates int
		expectedUpdates int
		expectedDeletes int
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "create SIG PD from OCM PD with decisions",
			key:  "ns1/pd-1",
			ocmPDs: []runtime.Object{
				newOCMPlacementDecision("ns1", "pd-1", "my-placement", "0", []v1beta1.ClusterDecision{
					{ClusterName: "cluster1", Reason: "score"},
					{ClusterName: "cluster2", Reason: "affinity"},
				}),
			},
			existingSIGPDs:  nil,
			expectedCreates: 1,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				created := actions[0].(clienttesting.CreateAction).GetObject().(*cpv1alpha1.PlacementDecision)
				if created.Name != "pd-1" {
					t.Errorf("expected name pd-1, got %s", created.Name)
				}
				if created.Namespace != "ns1" {
					t.Errorf("expected namespace ns1, got %s", created.Namespace)
				}
				if created.SchedulerName != SchedulerName {
					t.Errorf("expected scheduler %s, got %s", SchedulerName, created.SchedulerName)
				}
				if len(created.Decisions) != 2 {
					t.Fatalf("expected 2 decisions, got %d", len(created.Decisions))
				}
				if created.Decisions[0].ClusterProfileRef.Name != "cluster1" {
					t.Errorf("expected cluster1, got %s", created.Decisions[0].ClusterProfileRef.Name)
				}
				if created.Decisions[0].Reason != "score" {
					t.Errorf("expected reason 'score', got %s", created.Decisions[0].Reason)
				}
				if created.Decisions[1].ClusterProfileRef.Name != "cluster2" {
					t.Errorf("expected cluster2, got %s", created.Decisions[1].ClusterProfileRef.Name)
				}
				if created.Labels[cpv1alpha1.LabelClusterManagerKey] != SchedulerName {
					t.Errorf("expected cluster-manager label %s, got %s", SchedulerName, created.Labels[cpv1alpha1.LabelClusterManagerKey])
				}
				if created.Labels[cpv1alpha1.PlacementKeyLabel] != "my-placement" {
					t.Errorf("expected placement-key label 'my-placement', got %s", created.Labels[cpv1alpha1.PlacementKeyLabel])
				}
				if created.Labels[cpv1alpha1.DecisionKeyLabel] != "my-placement" {
					t.Errorf("expected decision-key label 'my-placement', got %s", created.Labels[cpv1alpha1.DecisionKeyLabel])
				}
				if created.Labels[cpv1alpha1.DecisionIndexLabel] != "0" {
					t.Errorf("expected decision-index label '0', got %s", created.Labels[cpv1alpha1.DecisionIndexLabel])
				}
			},
		},
		{
			name: "create SIG PD with empty decisions",
			key:  "ns1/pd-empty",
			ocmPDs: []runtime.Object{
				newOCMPlacementDecision("ns1", "pd-empty", "placement-1", "0", nil),
			},
			existingSIGPDs:  nil,
			expectedCreates: 1,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				created := actions[0].(clienttesting.CreateAction).GetObject().(*cpv1alpha1.PlacementDecision)
				if len(created.Decisions) != 0 {
					t.Errorf("expected 0 decisions, got %d", len(created.Decisions))
				}
			},
		},
		{
			name:   "delete SIG PD when OCM PD is removed",
			key:    "ns1/pd-deleted",
			ocmPDs: nil,
			existingSIGPDs: []runtime.Object{
				newSIGPlacementDecision("ns1", "pd-deleted", map[string]string{
					cpv1alpha1.LabelClusterManagerKey: SchedulerName,
				}, []cpv1alpha1.ClusterDecision{
					{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "cluster1"}},
				}, SchedulerName),
			},
			expectedDeletes: 1,
		},
		{
			name:   "skip delete of SIG PD not owned by us",
			key:    "ns1/pd-foreign",
			ocmPDs: nil,
			existingSIGPDs: []runtime.Object{
				newSIGPlacementDecision("ns1", "pd-foreign", map[string]string{
					cpv1alpha1.LabelClusterManagerKey: "other-scheduler",
				}, []cpv1alpha1.ClusterDecision{
					{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "cluster1"}},
				}, "other-scheduler"),
			},
			expectedDeletes: 0,
		},
		{
			name: "update SIG PD when decisions change",
			key:  "ns1/pd-update",
			ocmPDs: []runtime.Object{
				newOCMPlacementDecision("ns1", "pd-update", "my-placement", "0", []v1beta1.ClusterDecision{
					{ClusterName: "cluster1", Reason: "score"},
					{ClusterName: "cluster3", Reason: "new"},
				}),
			},
			existingSIGPDs: []runtime.Object{
				newSIGPlacementDecision("ns1", "pd-update", map[string]string{
					cpv1alpha1.LabelClusterManagerKey: SchedulerName,
					cpv1alpha1.PlacementKeyLabel:      "my-placement",
					cpv1alpha1.DecisionKeyLabel:       "my-placement",
					cpv1alpha1.DecisionIndexLabel:     "0",
				}, []cpv1alpha1.ClusterDecision{
					{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "cluster1"}, Reason: "score"},
					{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "cluster2"}, Reason: "old"},
				}, SchedulerName),
			},
			expectedUpdates: 1,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				updated := actions[0].(clienttesting.UpdateAction).GetObject().(*cpv1alpha1.PlacementDecision)
				if len(updated.Decisions) != 2 {
					t.Fatalf("expected 2 decisions, got %d", len(updated.Decisions))
				}
				if updated.Decisions[1].ClusterProfileRef.Name != "cluster3" {
					t.Errorf("expected cluster3, got %s", updated.Decisions[1].ClusterProfileRef.Name)
				}
			},
		},
		{
			name: "no update when SIG PD is already correct",
			key:  "ns1/pd-noop",
			ocmPDs: []runtime.Object{
				newOCMPlacementDecision("ns1", "pd-noop", "my-placement", "0", []v1beta1.ClusterDecision{
					{ClusterName: "cluster1", Reason: "score"},
				}),
			},
			existingSIGPDs: []runtime.Object{
				newSIGPlacementDecision("ns1", "pd-noop", map[string]string{
					cpv1alpha1.LabelClusterManagerKey: SchedulerName,
					cpv1alpha1.PlacementKeyLabel:      "my-placement",
					cpv1alpha1.DecisionKeyLabel:       "my-placement",
					cpv1alpha1.DecisionIndexLabel:     "0",
				}, []cpv1alpha1.ClusterDecision{
					{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "cluster1"}, Reason: "score"},
				}, SchedulerName),
			},
			expectedUpdates: 0,
			expectedCreates: 0,
		},
		{
			name:            "no-op when OCM PD not found and no SIG PD exists",
			key:             "ns1/pd-nonexistent",
			ocmPDs:          nil,
			existingSIGPDs:  nil,
			expectedDeletes: 0,
			expectedCreates: 0,
		},
		{
			name: "skip update of SIG PD not owned by us",
			key:  "ns1/pd-foreign-update",
			ocmPDs: []runtime.Object{
				newOCMPlacementDecision("ns1", "pd-foreign-update", "my-placement", "0", []v1beta1.ClusterDecision{
					{ClusterName: "cluster1"},
				}),
			},
			existingSIGPDs: []runtime.Object{
				newSIGPlacementDecision("ns1", "pd-foreign-update", map[string]string{
					cpv1alpha1.LabelClusterManagerKey: "other-scheduler",
				}, []cpv1alpha1.ClusterDecision{
					{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "different-cluster"}},
				}, "other-scheduler"),
			},
			expectedUpdates: 0,
			expectedCreates: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterObjects := c.ocmPDs
			clusterClient := clusterfake.NewSimpleClientset(clusterObjects...)
			clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 0)

			cpClient := cpfake.NewSimpleClientset(c.existingSIGPDs...)
			sigPDInformers := cpinformers.NewSharedInformerFactory(cpClient, 0)

			for _, pd := range c.ocmPDs {
				clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(pd)
			}
			for _, pd := range c.existingSIGPDs {
				sigPDInformers.Apis().V1alpha1().PlacementDecisions().Informer().GetStore().Add(pd)
			}

			ctrl := &sigPlacementDecisionController{
				ocmPDLister: clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister(),
				sigPDClient: cpClient,
				sigPDLister: sigPDInformers.Apis().V1alpha1().PlacementDecisions().Lister(),
			}

			syncCtx := testingcommon.NewFakeSyncContext(t, c.key)
			err := ctrl.sync(context.TODO(), syncCtx, c.key)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			actions := cpClient.Actions()

			var creates, updates, deletes int
			for _, action := range actions {
				switch action.GetVerb() {
				case "create":
					creates++
				case "update":
					updates++
				case "delete":
					deletes++
				}
			}

			if creates != c.expectedCreates {
				t.Errorf("expected %d creates, got %d", c.expectedCreates, creates)
			}
			if updates != c.expectedUpdates {
				t.Errorf("expected %d updates, got %d", c.expectedUpdates, updates)
			}
			if deletes != c.expectedDeletes {
				t.Errorf("expected %d deletes, got %d", c.expectedDeletes, deletes)
			}

			if c.validateActions != nil {
				mutatingActions := []clienttesting.Action{}
				for _, a := range actions {
					if a.GetVerb() != "get" && a.GetVerb() != "list" && a.GetVerb() != "watch" {
						mutatingActions = append(mutatingActions, a)
					}
				}
				c.validateActions(t, mutatingActions)
			}
		})
	}
}

func TestBuildSIGPlacementDecision(t *testing.T) {
	ocmPD := newOCMPlacementDecision("ns1", "pd-1", "my-placement", "2", []v1beta1.ClusterDecision{
		{ClusterName: "cluster-a", Reason: "top-score"},
		{ClusterName: "cluster-b", Reason: ""},
	})

	result := buildSIGPlacementDecision(ocmPD)

	if result.Name != "pd-1" {
		t.Errorf("expected name pd-1, got %s", result.Name)
	}
	if result.Namespace != "ns1" {
		t.Errorf("expected namespace ns1, got %s", result.Namespace)
	}
	if result.SchedulerName != SchedulerName {
		t.Errorf("expected scheduler %s, got %s", SchedulerName, result.SchedulerName)
	}
	if result.Labels[cpv1alpha1.LabelClusterManagerKey] != SchedulerName {
		t.Errorf("expected cluster-manager label")
	}
	if result.Labels[cpv1alpha1.PlacementKeyLabel] != "my-placement" {
		t.Errorf("expected placement-key label")
	}
	if result.Labels[cpv1alpha1.DecisionKeyLabel] != "my-placement" {
		t.Errorf("expected decision-key label")
	}
	if result.Labels[cpv1alpha1.DecisionIndexLabel] != "2" {
		t.Errorf("expected decision-index label '2', got %s", result.Labels[cpv1alpha1.DecisionIndexLabel])
	}
	if len(result.Decisions) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(result.Decisions))
	}
	if result.Decisions[0].ClusterProfileRef.Name != "cluster-a" {
		t.Errorf("expected cluster-a, got %s", result.Decisions[0].ClusterProfileRef.Name)
	}
	if result.Decisions[0].Reason != "top-score" {
		t.Errorf("expected reason 'top-score', got %s", result.Decisions[0].Reason)
	}
	if result.Decisions[1].ClusterProfileRef.Name != "cluster-b" {
		t.Errorf("expected cluster-b, got %s", result.Decisions[1].ClusterProfileRef.Name)
	}
}

func TestSigPDEqual(t *testing.T) {
	cases := []struct {
		name     string
		existing *cpv1alpha1.PlacementDecision
		desired  *cpv1alpha1.PlacementDecision
		expected bool
	}{
		{
			name: "equal",
			existing: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: SchedulerName,
				cpv1alpha1.PlacementKeyLabel:      "p1",
			}, []cpv1alpha1.ClusterDecision{
				{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "c1"}, Reason: "r1"},
			}, SchedulerName),
			desired: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: SchedulerName,
				cpv1alpha1.PlacementKeyLabel:      "p1",
			}, []cpv1alpha1.ClusterDecision{
				{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "c1"}, Reason: "r1"},
			}, SchedulerName),
			expected: true,
		},
		{
			name: "different decisions",
			existing: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: SchedulerName,
			}, []cpv1alpha1.ClusterDecision{
				{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "c1"}},
			}, SchedulerName),
			desired: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: SchedulerName,
			}, []cpv1alpha1.ClusterDecision{
				{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "c2"}},
			}, SchedulerName),
			expected: false,
		},
		{
			name: "different scheduler name",
			existing: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: SchedulerName,
			}, []cpv1alpha1.ClusterDecision{}, SchedulerName),
			desired: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: SchedulerName,
			}, []cpv1alpha1.ClusterDecision{}, "other"),
			expected: false,
		},
		{
			name: "different labels",
			existing: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: SchedulerName,
			}, []cpv1alpha1.ClusterDecision{}, SchedulerName),
			desired: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: SchedulerName,
				cpv1alpha1.PlacementKeyLabel:      "new",
			}, []cpv1alpha1.ClusterDecision{}, SchedulerName),
			expected: false,
		},
		{
			name: "different decision count",
			existing: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: SchedulerName,
			}, []cpv1alpha1.ClusterDecision{
				{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "c1"}},
			}, SchedulerName),
			desired: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: SchedulerName,
			}, []cpv1alpha1.ClusterDecision{
				{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "c1"}},
				{ClusterProfileRef: cpv1alpha1.ClusterProfileReference{Name: "c2"}},
			}, SchedulerName),
			expected: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := sigPDEqual(c.existing, c.desired)
			if result != c.expected {
				t.Errorf("expected %v, got %v", c.expected, result)
			}
		})
	}
}

func TestSigPDToQueueKey(t *testing.T) {
	ctrl := &sigPlacementDecisionController{}

	cases := []struct {
		name        string
		obj         *cpv1alpha1.PlacementDecision
		expectedLen int
	}{
		{
			name: "owned by us",
			obj: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: SchedulerName,
			}, nil, SchedulerName),
			expectedLen: 1,
		},
		{
			name: "not owned by us",
			obj: newSIGPlacementDecision("ns1", "pd-1", map[string]string{
				cpv1alpha1.LabelClusterManagerKey: "other",
			}, nil, "other"),
			expectedLen: 0,
		},
		{
			name:        "no labels",
			obj:         newSIGPlacementDecision("ns1", "pd-1", nil, nil, ""),
			expectedLen: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			keys := ctrl.sigPDToQueueKey(c.obj)
			if len(keys) != c.expectedLen {
				t.Errorf("expected %d keys, got %d: %v", c.expectedLen, len(keys), keys)
			}
		})
	}
}
