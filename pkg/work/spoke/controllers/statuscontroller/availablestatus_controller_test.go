package statuscontroller

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/apimachinery/pkg/api/equality"
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
	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
	"open-cluster-management.io/ocm/pkg/work/spoke/statusfeedback"
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
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
			},
			manifests: []workapiv1.ManifestCondition{
				newManifestWthCondition("", "v1", "secrets", "ns1", "n1"),
			},
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name: "Do not update if existing conditions are correct",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
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
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
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
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
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
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
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
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
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
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
				spoketesting.NewUnstructuredWithContent("apps/v1", "Deployment", "ns1", "deploy1",
					map[string]interface{}{
						"status": map[string]interface{}{"readyReplicas": int64(2), "replicas": int64(3), "availableReplicas": int64(2)},
					}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "deploy1", Namespace: "ns1"},
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
				spoketesting.NewUnstructuredWithContent("apps/v1", "Deployment", "ns1", "deploy1",
					map[string]interface{}{
						"status": map[string]interface{}{
							"replicas":   int64(3),
							"conditions": map[string]interface{}{"status": "True"},
						},
					}),
			},
			configOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "deploy1", Namespace: "ns1"},
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

func newManifest(group, version, resource, namespace, name string) workapiv1.ManifestCondition {
	return workapiv1.ManifestCondition{
		ResourceMeta: workapiv1.ManifestResourceMeta{
			Group:     group,
			Version:   version,
			Resource:  resource,
			Namespace: namespace,
			Name:      name,
		},
	}
}

func newManifestWthCondition(group, version, resource, namespace, name string) workapiv1.ManifestCondition {
	cond := newManifest(group, version, resource, namespace, name)
	cond.Conditions = []metav1.Condition{
		{
			Type:    workapiv1.ManifestAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  "ResourceAvailable",
			Message: "Resource is available",
		},
		{
			Type:   statusFeedbackConditionType,
			Reason: "NoStatusFeedbackSynced",
			Status: metav1.ConditionTrue,
		},
	}
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
