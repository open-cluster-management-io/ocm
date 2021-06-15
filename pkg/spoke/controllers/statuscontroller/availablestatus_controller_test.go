package statuscontroller

import (
	"context"
	"testing"

	"github.com/davecgh/go-spew/spew"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
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
			name: "remove available status from work whose manifests become empty",
			workConditions: []metav1.Condition{
				{
					Type: string(workapiv1.WorkAvailable),
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}

				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.ManifestWork)
				if len(work.Status.Conditions) != 0 {
					t.Fatal(spew.Sdump(actions))
				}
			},
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
					Type:    string(workapiv1.WorkAvailable),
					Status:  metav1.ConditionTrue,
					Reason:  "ResourcesAvailable",
					Message: "All resources are available",
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name: "build status with existing resource",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}

				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.ManifestWork)
				if len(work.Status.ResourceStatus.Manifests) != 1 {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[0].Conditions, string(workapiv1.ManifestAvailable), metav1.ConditionTrue) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[0].Conditions))
				}

				if !hasStatusCondition(work.Status.Conditions, string(workapiv1.WorkAvailable), metav1.ConditionTrue) {
					t.Fatal(spew.Sdump(work.Status.Conditions))
				}
			},
		},
		{
			name: "build status when one of resources doess not exists",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1"),
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("", "v1", "secrets", "ns2", "n2"),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}

				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.ManifestWork)
				if len(work.Status.ResourceStatus.Manifests) != 2 {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[0].Conditions, string(workapiv1.ManifestAvailable), metav1.ConditionTrue) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[0].Conditions))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[1].Conditions, string(workapiv1.ManifestAvailable), metav1.ConditionFalse) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[1].Conditions))
				}

				if !hasStatusCondition(work.Status.Conditions, string(workapiv1.WorkAvailable), metav1.ConditionFalse) {
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
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}

				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.ManifestWork)
				if len(work.Status.ResourceStatus.Manifests) != 2 {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[0].Conditions, string(workapiv1.ManifestAvailable), metav1.ConditionTrue) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[0].Conditions))
				}
				if !hasStatusCondition(work.Status.ResourceStatus.Manifests[1].Conditions, string(workapiv1.ManifestAvailable), metav1.ConditionUnknown) {
					t.Fatal(spew.Sdump(work.Status.ResourceStatus.Manifests[1].Conditions))
				}

				if !hasStatusCondition(work.Status.Conditions, string(workapiv1.WorkAvailable), metav1.ConditionUnknown) {
					t.Fatal(spew.Sdump(work.Status.Conditions))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testingWork, _ := spoketesting.NewManifestWork(0)
			testingWork.Status = workapiv1.ManifestWorkStatus{
				Conditions: c.workConditions,
				ResourceStatus: workapiv1.ManifestResourceStatus{
					Manifests: c.manifests,
				},
			}

			fakeClient := fakeworkclient.NewSimpleClientset(testingWork)
			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), c.existingResources...)
			controller := AvailableStatusController{
				manifestWorkClient: fakeClient.WorkV1().ManifestWorks(testingWork.Namespace),
				spokeDynamicClient: fakeDynamicClient,
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
			Type:    string(workapiv1.ManifestAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ResourceAvailable",
			Message: "Resource is available",
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
