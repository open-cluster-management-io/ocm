package statuscontroller

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	ocmfeature "open-cluster-management.io/api/feature"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/spoke/conditions"
	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
	"open-cluster-management.io/ocm/pkg/work/spoke/statusfeedback"
	"open-cluster-management.io/ocm/test/integration/util"
)

func TestSyncManifestWork(t *testing.T) {
	cases := []struct {
		name              string
		existingResources []runtime.Object
		manifests         []workapiv1.ManifestCondition
		workConditions    []metav1.Condition
		validateActions   func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "unknown available status from work whose manifests become empty",
			workConditions: []metav1.Condition{
				{
					Type: workapiv1.WorkApplied,
				},
				{
					Type: workapiv1.WorkAvailable,
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}

				if !hasStatusCondition(work.Status.Conditions, workapiv1.WorkAvailable, metav1.ConditionUnknown) {
					t.Fatal(spew.Sdump(work.Status.Conditions))
				}
			},
		},
		{
			name: "No work applied condition",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
			},
			manifests: []workapiv1.ManifestCondition{
				newManifestWthCondition("", "v1", "secrets", "ns1", "n1"),
			},
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name: "Do not update if existing conditions are correct",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
			},
			manifests: []workapiv1.ManifestCondition{
				newManifestWthCondition("", "v1", "secrets", "ns1", "n1"),
			},
			workConditions: []metav1.Condition{
				{
					Type: workapiv1.WorkApplied,
				},
				{
					Type:    workapiv1.WorkAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "ResourcesAvailable",
					Message: "All resources are available",
				},
			},
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name: "build status with existing resource",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
			},
			workConditions: []metav1.Condition{
				{
					Type: workapiv1.WorkApplied,
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if len(work.Status.ResourceStatus.Manifests) != 1 {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[0].Conditions, workapiv1.ManifestAvailable, metav1.ConditionTrue) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[0].Conditions))
				}

				if !hasStatusCondition(work.Status.Conditions, workapiv1.WorkAvailable, metav1.ConditionTrue) {
					t.Fatal(spew.Sdump(work.Status.Conditions))
				}
			},
		},
		{
			name: "build status when one of resources does not exists",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("", "v1", "secrets", "ns2", "n2"),
			},
			workConditions: []metav1.Condition{
				{
					Type: workapiv1.WorkApplied,
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[0].Conditions, workapiv1.ManifestAvailable, metav1.ConditionTrue) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[0].Conditions))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[1].Conditions, workapiv1.ManifestAvailable, metav1.ConditionFalse) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[1].Conditions))
				}

				if !hasStatusCondition(work.Status.Conditions, workapiv1.WorkAvailable, metav1.ConditionFalse) {
					t.Fatal(spew.Sdump(work.Status.Conditions))
				}
			},
		},
		{
			name: "build status when one of resosurce has incompleted meta",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("", "", "", "", ""),
			},
			workConditions: []metav1.Condition{
				{
					Type: workapiv1.WorkApplied,
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if len(work.Status.ResourceStatus.Manifests) != 2 {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[0].Conditions, workapiv1.ManifestAvailable, metav1.ConditionTrue) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[0].Conditions))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[1].Conditions, workapiv1.ManifestAvailable, metav1.ConditionUnknown) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[1].Conditions))
				}

				if !hasStatusCondition(work.Status.Conditions, workapiv1.WorkAvailable, metav1.ConditionUnknown) {
					t.Fatal(spew.Sdump(work.Status.Conditions))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testingWork, _ := spoketesting.NewManifestWork(0)
			testingWork.Finalizers = []string{workapiv1.ManifestWorkFinalizer}
			testingWork.Status = workapiv1.ManifestWorkStatus{
				Conditions: c.workConditions,
				ResourceStatus: workapiv1.ManifestResourceStatus{
					Manifests: c.manifests,
				},
			}

			fakeClient := fakeworkclient.NewSimpleClientset(testingWork)
			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), c.existingResources...)
			controller := AvailableStatusController{
				spokeDynamicClient: fakeDynamicClient,
				patcher: patcher.NewPatcher[
					*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
					fakeClient.WorkV1().ManifestWorks(testingWork.Namespace)),
			}

			err := controller.syncManifestWork(context.TODO(), testingWork)
			if err != nil {
				t.Fatal(err)
			}
			c.validateActions(t, fakeClient.Actions())
		})
	}
}

func TestStatusFeedback(t *testing.T) {
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates))
	cases := []struct {
		name              string
		existingResources []runtime.Object
		configOption      []workapiv1.ManifestConfigOption
		manifests         []workapiv1.ManifestCondition
		validateActions   func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "resource identifier is not matched",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "deploy1", Namespace: "ns1"},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if len(work.Status.ResourceStatus.Manifests) != 1 {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests))
				}
				if len(work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values) != 0 {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[0].Conditions, statusFeedbackConditionType, metav1.ConditionTrue) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[0].Conditions))
				}
			},
		},
		{
			name: "get well known status",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
				testingcommon.NewUnstructuredWithContent("apps/v1", "Deployment", "ns1", "deploy1",
					map[string]interface{}{
						"status": map[string]interface{}{"readyReplicas": int64(2), "replicas": int64(3), "availableReplicas": int64(2)},
					}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "*", Namespace: "ns*"},
					FeedbackRules:      []workapiv1.FeedbackRule{{Type: workapiv1.WellKnownStatusType}},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("apps", "v1", "deployments", "ns1", "deploy1"),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if len(work.Status.ResourceStatus.Manifests) != 2 {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests))
				}

				expectedValues := []workapiv1.FeedbackValue{
					{
						Name: "ReadyReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: pointer.Int64(2),
						},
					},
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: pointer.Int64(3),
						},
					},
					{
						Name: "AvailableReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: pointer.Int64(2),
						},
					},
				}
				if !equality.Semantic.DeepEqual(work.Status.ResourceStatus.Manifests[1].StatusFeedbacks.Values, expectedValues) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[1].StatusFeedbacks.Values))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[1].Conditions, statusFeedbackConditionType, metav1.ConditionTrue) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[1].Conditions))
				}
			},
		},
		{
			name: "get wrong json path",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredWithContent("apps/v1", "Deployment", "ns1", "deploy1",
					map[string]interface{}{
						"status": map[string]interface{}{
							"replicas":   int64(3),
							"conditions": map[string]interface{}{"status": "True"},
						},
					}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "deploy1", Namespace: "*"},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
						{
							Type: workapiv1.JSONPathsType,
							JsonPaths: []workapiv1.JsonPath{
								{
									Name: "condition",
									Path: ".status.conditions",
								},
							},
						},
					},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("apps", "v1", "deployments", "ns1", "deploy1"),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if len(work.Status.ResourceStatus.Manifests) != 1 {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests))
				}

				expectedValues := []workapiv1.FeedbackValue{
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: pointer.Int64(3),
						},
					},
				}
				if !equality.Semantic.DeepEqual(work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values, expectedValues) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[0].Conditions, statusFeedbackConditionType, metav1.ConditionFalse) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[0].Conditions))
				}
			},
		},
		{
			name: "LastTransitionTime updates when feedback values change",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredWithContent("apps/v1", "Deployment", "ns1", "deploy1",
					map[string]interface{}{
						"status": map[string]interface{}{"readyReplicas": int64(3), "replicas": int64(3), "availableReplicas": int64(3)},
					}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "deploy1", Namespace: "ns1"},
					FeedbackRules:      []workapiv1.FeedbackRule{{Type: workapiv1.WellKnownStatusType}},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Group:     "apps",
						Version:   "v1",
						Resource:  "deployments",
						Namespace: "ns1",
						Name:      "deploy1",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "ReadyReplicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: pointer.Int64(2), // Different from current state (3)
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               statusFeedbackConditionType,
							Status:             metav1.ConditionTrue,
							Reason:             "StatusFeedbackSynced",
							LastTransitionTime: metav1.NewTime(metav1.Now().Add(-1 * time.Hour)), // Old timestamp
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if len(work.Status.ResourceStatus.Manifests) != 1 {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests))
				}

				// Verify feedback values were updated
				if len(work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values) == 0 {
					t.Fatal("Expected feedback values to be set")
				}

				// Verify LastTransitionTime was updated (should be recent, not 1 hour ago)
				cond := meta.FindStatusCondition(work.Status.ResourceStatus.Manifests[0].Conditions, statusFeedbackConditionType)
				if cond == nil {
					t.Fatal("Expected StatusFeedbackSynced condition")
				}

				oldTime := metav1.Now().Add(-1 * time.Hour)
				// LastTransitionTime should be updated (within last minute)
				if cond.LastTransitionTime.Before(&metav1.Time{Time: oldTime.Add(59 * time.Minute)}) {
					t.Errorf("Expected LastTransitionTime to be updated when values changed, but it's still old: %v", cond.LastTransitionTime)
				}
			},
		},
		{
			name: "one option matches multi resources",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
				testingcommon.NewUnstructuredWithContent("apps/v1", "Deployment", "ns1", "deploy1",
					map[string]interface{}{
						"status": map[string]interface{}{"readyReplicas": int64(2), "replicas": int64(3), "availableReplicas": int64(2)},
					}),
				testingcommon.NewUnstructuredWithContent("apps/v1", "Deployment", "ns2", "deploy2",
					map[string]interface{}{
						"status": map[string]interface{}{"readyReplicas": int64(2), "replicas": int64(3), "availableReplicas": int64(2)},
					}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "*", Namespace: "ns*"},
					FeedbackRules:      []workapiv1.FeedbackRule{{Type: workapiv1.WellKnownStatusType}},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("apps", "v1", "deployments", "ns1", "deploy1"),
				newManifest("apps", "v1", "deployments", "ns2", "deploy2"),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if len(work.Status.ResourceStatus.Manifests) != 3 {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests))
				}

				expectedValues := []workapiv1.FeedbackValue{
					{
						Name: "ReadyReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: pointer.Int64(2),
						},
					},
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: pointer.Int64(3),
						},
					},
					{
						Name: "AvailableReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: pointer.Int64(2),
						},
					},
				}
				for i := 1; i < len(work.Status.ResourceStatus.Manifests); i++ {
					if !equality.Semantic.DeepEqual(work.Status.ResourceStatus.Manifests[i].StatusFeedbacks.Values, expectedValues) {
						t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[i].StatusFeedbacks.Values))
					}
					if !hasStatusCondition(work.Status.ResourceStatus.Manifests[i].Conditions, statusFeedbackConditionType, metav1.ConditionTrue) {
						t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[i].Conditions))
					}
				}

			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testingWork, _ := spoketesting.NewManifestWork(0)
			testingWork.Finalizers = []string{workapiv1.ManifestWorkFinalizer}
			testingWork.Spec.ManifestConfigs = c.configOption
			testingWork.Status = workapiv1.ManifestWorkStatus{
				ResourceStatus: workapiv1.ManifestResourceStatus{
					Manifests: c.manifests,
				},
				Conditions: []metav1.Condition{
					{Type: workapiv1.WorkApplied},
				},
			}

			fakeClient := fakeworkclient.NewSimpleClientset(testingWork)
			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), c.existingResources...)
			controller := AvailableStatusController{
				spokeDynamicClient: fakeDynamicClient,
				statusReader:       statusfeedback.NewStatusReader(),
				patcher: patcher.NewPatcher[
					*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
					fakeClient.WorkV1().ManifestWorks(testingWork.Namespace)),
			}

			err := controller.syncManifestWork(context.TODO(), testingWork)
			if err != nil {
				t.Fatal(err)
			}
			c.validateActions(t, fakeClient.Actions())
		})
	}
}

func TestConditionRules(t *testing.T) {
	activeRule := workapiv1.ConditionRule{
		Type:      workapiv1.CelConditionExpressionsType,
		Condition: "Active",
		CelExpressions: []string{
			"has(object.status) && hasConditions(object.status) ? object.status.conditions.exists(c, c.type == 'Active' && c.status == 'True') : false",
		},
		MessageExpression: "result ? 'NewObject is active' : 'NewObject is not active'",
	}

	cases := []struct {
		name                       string
		existingResources          []runtime.Object
		existingConditions         []metav1.Condition
		configOption               []workapiv1.ManifestConfigOption
		manifests                  []workapiv1.ManifestCondition
		expectedManifestConditions [][]metav1.Condition
		expectedWorkConditions     []metav1.Condition
	}{
		{
			name: "condition rule successful evaluation",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns1", "n1",
					map[string]any{
						"spec": map[string]any{"key1": "val1"},
						"status": map[string]any{
							"conditions": []any{
								map[string]any{"type": "Active", "status": "True"},
							},
						}}),
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns1", "n2",
					map[string]any{"spec": map[string]any{"key1": "val2"}}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "newobjects", Namespace: "ns1", Name: "n1"},
					ConditionRules:     []workapiv1.ConditionRule{activeRule},
				},
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "newobjects", Namespace: "ns1", Name: "n2"},
					ConditionRules:     []workapiv1.ConditionRule{activeRule},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "newobjects", "ns1", "n1"),
				newManifest("", "v1", "newobjects", "ns1", "n2"),
			},
			expectedManifestConditions: [][]metav1.Condition{
				{{Type: "Active", Status: metav1.ConditionTrue, Reason: workapiv1.ConditionRuleEvaluated, Message: "NewObject is active"}},
				{{Type: "Active", Status: metav1.ConditionFalse, Reason: workapiv1.ConditionRuleEvaluated, Message: "NewObject is not active"}},
			},
		},
		{
			name: "condition rule invalid expression",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns1", "n1",
					map[string]any{"spec": map[string]any{"key1": "val1"}}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "newobjects", Namespace: "ns1", Name: "n1"},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:           workapiv1.CelConditionExpressionsType,
							Condition:      "Active",
							CelExpressions: []string{"object.invalid.key == 'Active'"},
						},
					},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "newobjects", "ns1", "n1"),
			},
			expectedManifestConditions: [][]metav1.Condition{
				{{Type: "Active", Status: metav1.ConditionFalse, Reason: workapiv1.ConditionRuleExpressionError, Message: "no such key: invalid"}},
			},
		},
		{
			name: "condition rule Complete aggregates to work condition",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns1", "n1",
					map[string]any{"spec": map[string]any{"key1": "val1"}}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "newobjects", Namespace: "ns1", Name: "n1"},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:           workapiv1.CelConditionExpressionsType,
							Condition:      workapiv1.ManifestComplete,
							CelExpressions: []string{"true"},
						},
					},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "newobjects", "ns1", "n1"),
			},
			expectedManifestConditions: [][]metav1.Condition{
				{{Type: workapiv1.ManifestComplete, Status: metav1.ConditionTrue, Reason: workapiv1.ConditionRuleEvaluated}},
			},
			expectedWorkConditions: []metav1.Condition{
				{Type: workapiv1.WorkComplete, Status: metav1.ConditionTrue, Reason: "ConditionRulesAggregated"},
			},
		},
		{
			name: "work Complete requires all completable manifests to Complete",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns1", "n1",
					map[string]any{"spec": map[string]any{"key1": "val1"}}),
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns1", "n2",
					map[string]any{"spec": map[string]any{"key1": "val2"}}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "newobjects", Namespace: "ns1", Name: "n1"},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:           workapiv1.CelConditionExpressionsType,
							Condition:      workapiv1.ManifestComplete,
							CelExpressions: []string{"true"},
						},
					},
				},
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "newobjects", Namespace: "ns1", Name: "n2"},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:           workapiv1.CelConditionExpressionsType,
							Condition:      workapiv1.ManifestComplete,
							CelExpressions: []string{"false"},
						},
					},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "newobjects", "ns1", "n1"),
				newManifest("", "v1", "newobjects", "ns1", "n2"),
			},
			expectedManifestConditions: [][]metav1.Condition{
				{{Type: workapiv1.ManifestComplete, Status: metav1.ConditionTrue, Reason: workapiv1.ConditionRuleEvaluated}},
				{{Type: workapiv1.ManifestComplete, Status: metav1.ConditionFalse, Reason: workapiv1.ConditionRuleEvaluated}},
			},
			expectedWorkConditions: []metav1.Condition{
				{Type: workapiv1.WorkComplete, Status: metav1.ConditionFalse, Reason: "ConditionRulesAggregated"},
			},
		},
		{
			name: "remove conditions set by deleted rules",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns1", "n1",
					map[string]any{"spec": map[string]any{"key1": "val1"}}),
			},
			existingConditions: []metav1.Condition{{Type: workapiv1.WorkComplete, Status: metav1.ConditionTrue, Reason: "ConditionRulesAggregated"}},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "newobjects", Namespace: "ns1", Name: "n1"},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:           workapiv1.CelConditionExpressionsType,
							Condition:      "SomeOtherRule",
							CelExpressions: []string{"true"},
						},
					},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest(
					"", "v1", "newobjects", "ns1", "n1",
					metav1.Condition{Type: workapiv1.ManifestComplete, Status: metav1.ConditionTrue, Reason: workapiv1.ConditionRuleEvaluated},
				),
			},
			expectedManifestConditions: [][]metav1.Condition{
				{
					{Type: "SomeOtherRule", Status: metav1.ConditionTrue, Reason: workapiv1.ConditionRuleEvaluated},
					{Type: workapiv1.ManifestComplete, Status: util.ConditionNotFound},
				},
			},
			expectedWorkConditions: []metav1.Condition{
				{Type: workapiv1.WorkComplete, Status: util.ConditionNotFound},
			},
		},
		{
			name: "work Progressing is True when any manifest is progressing (True wins)",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns1", "n1",
					map[string]any{"spec": map[string]any{"key1": "val1"}}),
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns1", "n2",
					map[string]any{"spec": map[string]any{"key1": "val2"}}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "newobjects", Namespace: "ns1", Name: "n1"},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:           workapiv1.CelConditionExpressionsType,
							Condition:      workapiv1.ManifestProgressing,
							CelExpressions: []string{"true"},
						},
					},
				},
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "newobjects", Namespace: "ns1", Name: "n2"},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:           workapiv1.CelConditionExpressionsType,
							Condition:      workapiv1.ManifestProgressing,
							CelExpressions: []string{"false"},
						},
					},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "newobjects", "ns1", "n1"),
				newManifest("", "v1", "newobjects", "ns1", "n2"),
			},
			expectedManifestConditions: [][]metav1.Condition{
				{{Type: workapiv1.ManifestProgressing, Status: metav1.ConditionTrue, Reason: workapiv1.ConditionRuleEvaluated}},
				{{Type: workapiv1.ManifestProgressing, Status: metav1.ConditionFalse, Reason: workapiv1.ConditionRuleEvaluated}},
			},
			expectedWorkConditions: []metav1.Condition{
				{Type: workapiv1.WorkProgressing, Status: metav1.ConditionTrue, Reason: "ConditionRulesAggregated"},
			},
		},
		{
			name: "work Progressing is False when no manifest is progressing",
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns1", "n1",
					map[string]any{"spec": map[string]any{"key1": "val1"}}),
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns1", "n2",
					map[string]any{"spec": map[string]any{"key1": "val2"}}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "newobjects", Namespace: "ns1", Name: "n1"},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:           workapiv1.CelConditionExpressionsType,
							Condition:      workapiv1.ManifestProgressing,
							CelExpressions: []string{"false"},
						},
					},
				},
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "newobjects", Namespace: "ns1", Name: "n2"},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:           workapiv1.CelConditionExpressionsType,
							Condition:      workapiv1.ManifestProgressing,
							CelExpressions: []string{"false"},
						},
					},
				},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "newobjects", "ns1", "n1"),
				newManifest("", "v1", "newobjects", "ns1", "n2"),
			},
			expectedManifestConditions: [][]metav1.Condition{
				{{Type: workapiv1.ManifestProgressing, Status: metav1.ConditionFalse, Reason: workapiv1.ConditionRuleEvaluated}},
				{{Type: workapiv1.ManifestProgressing, Status: metav1.ConditionFalse, Reason: workapiv1.ConditionRuleEvaluated}},
			},
			expectedWorkConditions: []metav1.Condition{
				{Type: workapiv1.WorkProgressing, Status: metav1.ConditionFalse, Reason: "ConditionRulesAggregated"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testingWork, _ := spoketesting.NewManifestWork(0)
			// Deleting conditions from old rules relies on change in ObservedGeneration
			// Emualate that by setting generation to something other than 0
			testingWork.Generation = 2
			testingWork.Finalizers = []string{workapiv1.ManifestWorkFinalizer}
			testingWork.Spec.ManifestConfigs = c.configOption
			existingCondition := c.existingConditions
			if meta.FindStatusCondition(c.existingConditions, workapiv1.WorkApplied) == nil {
				existingCondition = append([]metav1.Condition{{Type: workapiv1.WorkApplied}}, existingCondition...)
			}
			testingWork.Status = workapiv1.ManifestWorkStatus{
				ResourceStatus: workapiv1.ManifestResourceStatus{
					Manifests: c.manifests,
				},
				Conditions: existingCondition,
			}

			fakeClient := fakeworkclient.NewSimpleClientset(testingWork)
			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), c.existingResources...)
			conditionReader, err := conditions.NewConditionReader()
			if err != nil {
				t.Fatal(err)
			}
			controller := AvailableStatusController{
				spokeDynamicClient: fakeDynamicClient,
				statusReader:       statusfeedback.NewStatusReader(),
				conditionReader:    conditionReader,
				patcher: patcher.NewPatcher[
					*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
					fakeClient.WorkV1().ManifestWorks(testingWork.Namespace)),
			}

			err = controller.syncManifestWork(context.TODO(), testingWork)
			if err != nil {
				t.Fatal(err)
			}

			// Parse work
			actions := fakeClient.Actions()
			testingcommon.AssertActions(t, actions, "patch")
			p := actions[0].(clienttesting.PatchActionImpl).Patch
			work := &workapiv1.ManifestWork{}
			if err := json.Unmarshal(p, work); err != nil {
				t.Fatal(err)
			}
			if len(work.Status.ResourceStatus.Manifests) != len(c.manifests) {
				t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests))
			}

			// Check expected conditions on manifests and work
			conditionsFailed := false
			for i, expectedConditions := range c.expectedManifestConditions {
				manifestStatus := work.Status.ResourceStatus.Manifests[i]
				errs := util.CheckExpectedConditions(manifestStatus.Conditions, expectedConditions...)
				if errs != nil {
					conditionsFailed = true
					for _, err := range errs.Errors() {
						t.Errorf("%s: %v", manifestStatus.ResourceMeta.Name, err)
					}
				}
			}

			errs := util.CheckExpectedConditions(work.Status.Conditions, c.expectedWorkConditions...)
			if errs != nil {
				conditionsFailed = true
				for _, err := range errs.Errors() {
					t.Errorf("ManifestWork: %v", err)
				}
			}

			if conditionsFailed {
				t.FailNow()
			}
		})
	}
}

func newManifest(group, version, resource, namespace, name string, conditions ...metav1.Condition) workapiv1.ManifestCondition {
	return workapiv1.ManifestCondition{
		ResourceMeta: workapiv1.ManifestResourceMeta{
			Group:     group,
			Version:   version,
			Resource:  resource,
			Namespace: namespace,
			Name:      name,
		},
		Conditions: conditions,
	}
}

func newManifestWthCondition(group, version, resource, namespace, name string) workapiv1.ManifestCondition {
	cond := newManifest(
		group, version, resource, namespace, name,
		metav1.Condition{
			Type:    workapiv1.ManifestAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  "ResourceAvailable",
			Message: "Resource is available",
		},
		metav1.Condition{
			Type:   statusFeedbackConditionType,
			Reason: "NoStatusFeedbackSynced",
			Status: metav1.ConditionTrue,
		},
	)
	return cond
}

func hasStatusCondition(conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type != conditionType {
			continue
		}

		return condition.Status == status
	}

	return false
}
