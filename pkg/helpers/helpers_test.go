package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/diff"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

const testManagedClusterGroup = "system:open-cluster-management:testgroup"

func TestUpdateStatusCondition(t *testing.T) {
	nowish := metav1.Now()
	beforeish := metav1.Time{Time: nowish.Add(-10 * time.Second)}
	afterish := metav1.Time{Time: nowish.Add(10 * time.Second)}

	cases := []struct {
		name               string
		startingConditions []clusterv1.StatusCondition
		newCondition       clusterv1.StatusCondition
		expextedUpdated    bool
		expectedConditions []clusterv1.StatusCondition
	}{
		{
			name:               "add to empty",
			startingConditions: []clusterv1.StatusCondition{},
			newCondition:       newCondition("test", "True", "my-reason", "my-message", nil),
			expextedUpdated:    true,
			expectedConditions: []clusterv1.StatusCondition{newCondition("test", "True", "my-reason", "my-message", nil)},
		},
		{
			name: "add to non-conflicting",
			startingConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
			},
			newCondition:    newCondition("one", "True", "my-reason", "my-message", nil),
			expextedUpdated: true,
			expectedConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", nil),
			},
		},
		{
			name: "change existing status",
			startingConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", nil),
			},
			newCondition:    newCondition("one", "False", "my-different-reason", "my-othermessage", nil),
			expextedUpdated: true,
			expectedConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "False", "my-different-reason", "my-othermessage", nil),
			},
		},
		{
			name: "leave existing transition time",
			startingConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", &beforeish),
			},
			newCondition:    newCondition("one", "True", "my-reason", "my-message", &afterish),
			expextedUpdated: false,
			expectedConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", &beforeish),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClusterClient := clusterfake.NewSimpleClientset(&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "testspokecluster"},
				Status: clusterv1.ManagedClusterStatus{
					Conditions: c.startingConditions,
				},
			})

			status, updated, err := UpdateManagedClusterStatus(
				context.TODO(),
				fakeClusterClient,
				"testspokecluster",
				UpdateManagedClusterConditionFn(c.newCondition),
			)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			if updated != c.expextedUpdated {
				t.Errorf("expected %t, but %t", c.expextedUpdated, updated)
			}
			for i := range c.expectedConditions {
				expected := c.expectedConditions[i]
				actual := status.Conditions[i]
				if expected.LastTransitionTime == (metav1.Time{}) {
					actual.LastTransitionTime = metav1.Time{}
				}
				if !equality.Semantic.DeepEqual(expected, actual) {
					t.Errorf(diff.ObjectDiff(expected, actual))
				}
			}
		})
	}
}

func TestIsValidHTTPSURL(t *testing.T) {
	cases := []struct {
		name      string
		serverURL string
		isValid   bool
	}{
		{
			name:      "an empty url",
			serverURL: "",
			isValid:   false,
		},
		{
			name:      "an invalid url",
			serverURL: "/path/path/path",
			isValid:   false,
		},
		{
			name:      "a http url",
			serverURL: "http://127.0.0.1:8080",
			isValid:   false,
		},
		{
			name:      "a https url",
			serverURL: "https://127.0.0.1:6443",
			isValid:   true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isValid := IsValidHTTPSURL(c.serverURL)
			if isValid != c.isValid {
				t.Errorf("expected %t, but %t", c.isValid, isValid)
			}
		})
	}
}

func TestCleanUpManagedClusterManifests(t *testing.T) {
	applyFiles := map[string]runtime.Object{
		"namespace":          newUnstructured("v1", "Namespace", "", "n1"),
		"clusterrole":        newUnstructured("rbac.authorization.k8s.io/v1", "ClusterRole", "", "cr1"),
		"clusterrolebinding": newUnstructured("rbac.authorization.k8s.io/v1", "ClusterRoleBinding", "", "crb1"),
		"role":               newUnstructured("rbac.authorization.k8s.io/v1", "Role", "n1", "r1"),
		"rolebinding":        newUnstructured("rbac.authorization.k8s.io/v1", "RoleBinding", "n1", "rb1"),
	}
	cases := []struct {
		name            string
		applyObject     []runtime.Object
		applyFiles      map[string]runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedErr     string
	}{
		{
			name: "delete applied objects",
			applyObject: []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "n1"}},
				&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "cr1"}},
				&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "crb1"}},
				&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "n1"}},
				&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "rb1", Namespace: "n1"}},
			},
			applyFiles: applyFiles,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertDeleteActions(t, len(applyFiles), actions)
			},
		},
		{
			name:        "there are no applied objects",
			applyObject: []runtime.Object{},
			applyFiles:  applyFiles,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertDeleteActions(t, len(applyFiles), actions)
			},
		},
		{
			name:        "unhandled types",
			applyObject: []runtime.Object{},
			applyFiles:  map[string]runtime.Object{"secret": newUnstructured("v1", "Secret", "n1", "s1")},
			expectedErr: "unhandled type *v1.Secret",
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expected no actions, but %v", actions)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := fakekube.NewSimpleClientset(c.applyObject...)
			cleanUpErr := CleanUpManagedClusterManifests(
				context.TODO(),
				kubeClient,
				eventstesting.NewTestingEventRecorder(t),
				func(name string) ([]byte, error) {
					if c.applyFiles[name] == nil {
						return nil, fmt.Errorf("Failed to find file")
					}
					return json.Marshal(c.applyFiles[name])
				},
				getApplyFileNames(c.applyFiles)...,
			)
			if len(c.expectedErr) > 0 && cleanUpErr == nil {
				t.Errorf("expected %q error", c.expectedErr)
				return
			}
			if len(c.expectedErr) > 0 && cleanUpErr != nil && cleanUpErr.Error() != c.expectedErr {
				t.Errorf("expected %q error, got %q", c.expectedErr, cleanUpErr.Error())
				return
			}
			if len(c.expectedErr) == 0 && cleanUpErr != nil {
				t.Errorf("unexpected err: %v", cleanUpErr)
				return
			}

			c.validateActions(t, kubeClient.Actions())
		})
	}
}

func TestCleanUpGroupFromClusterRoleBindings(t *testing.T) {
	cases := []struct {
		name            string
		object          []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "clean up group from clusterrolebindings",
			object: []runtime.Object{
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "crb1"},
					Subjects: []rbacv1.Subject{
						{Kind: "Group", Name: testManagedClusterGroup},
					},
				},
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "crb2"},
					Subjects: []rbacv1.Subject{
						{Kind: "Group", Name: testManagedClusterGroup},
						{Kind: "Group", Name: "test"},
						{Kind: "User", Name: testManagedClusterGroup},
					},
				},
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "crb3"},
					Subjects: []rbacv1.Subject{
						{Kind: "Group", Name: "test"},
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 3 {
					t.Errorf("expected 3 actions, but %v", actions)
				}
				if actions[1].(clienttesting.DeleteActionImpl).Name != "crb1" {
					t.Errorf("expected to delete crb1, but %v", actions[1])
				}
				actual := (actions[2].(clienttesting.UpdateActionImpl).Object).(*rbacv1.ClusterRoleBinding)
				expected := []rbacv1.Subject{{Kind: "Group", Name: "test"}, {Kind: "User", Name: testManagedClusterGroup}}
				if !reflect.DeepEqual(actual.Subjects, expected) {
					t.Errorf("expected %v, but %v", expected, actual.Subjects)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := fakekube.NewSimpleClientset(c.object...)
			err := CleanUpGroupFromClusterRoleBindings(
				context.TODO(),
				kubeClient,
				eventstesting.NewTestingEventRecorder(t),
				testManagedClusterGroup,
			)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
				return
			}
			c.validateActions(t, kubeClient.Actions())
		})
	}
}

func TestCleanUpGroupFromRoleBindings(t *testing.T) {
	cases := []struct {
		name            string
		object          []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "clean up group from rolebindings",
			object: []runtime.Object{
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "rb1", Namespace: "n1"},
					Subjects: []rbacv1.Subject{
						{Kind: "Group", Name: testManagedClusterGroup},
					},
				},
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "rb2", Namespace: "n1"},
					Subjects: []rbacv1.Subject{
						{Kind: "Group", Name: testManagedClusterGroup},
						{Kind: "Group", Name: "test"},
						{Kind: "User", Name: testManagedClusterGroup},
					},
				},
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "rb3", Namespace: "n2"},
					Subjects: []rbacv1.Subject{
						{Kind: "Group", Name: "test"},
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 3 {
					t.Errorf("expected 3 actions, but %v", actions)
				}
				if actions[1].(clienttesting.DeleteActionImpl).Name != "rb1" ||
					actions[1].(clienttesting.DeleteActionImpl).Namespace != "n1" {
					t.Errorf("expected to delete crb1, but %v", actions[1])
				}
				actual := (actions[2].(clienttesting.UpdateActionImpl).Object).(*rbacv1.RoleBinding)
				expected := []rbacv1.Subject{{Kind: "Group", Name: "test"}, {Kind: "User", Name: testManagedClusterGroup}}
				if !reflect.DeepEqual(actual.Subjects, expected) {
					t.Errorf("expected %v, but %v", expected, actual.Subjects)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := fakekube.NewSimpleClientset(c.object...)
			err := CleanUpGroupFromRoleBindings(
				context.TODO(),
				kubeClient,
				eventstesting.NewTestingEventRecorder(t),
				testManagedClusterGroup,
			)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
				return
			}
			c.validateActions(t, kubeClient.Actions())
		})
	}
}

func assertDeleteActions(t *testing.T, actionCounts int, actions []clienttesting.Action) {
	if len(actions) != actionCounts {
		t.Errorf("expected %d actions, but %v", actionCounts, actions)
	}
	for _, action := range actions {
		if action.GetVerb() != "delete" {
			t.Errorf("expected delete actions, but %v", action)
		}
	}
}

func newCondition(name, status, reason, message string, lastTransition *metav1.Time) clusterv1.StatusCondition {
	ret := clusterv1.StatusCondition{
		Type:    name,
		Status:  metav1.ConditionStatus(status),
		Reason:  reason,
		Message: message,
	}
	if lastTransition != nil {
		ret.LastTransitionTime = *lastTransition
	}
	return ret
}

func newUnstructured(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}
	return object
}

func getApplyFileNames(applyFiles map[string]runtime.Object) []string {
	keys := []string{}
	for key := range applyFiles {
		keys = append(keys, key)
	}
	return keys
}
