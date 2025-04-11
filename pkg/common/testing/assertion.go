package testing

import (
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"
)

// AssertError asserts the actual error representation is the same with the expected,
// if the expected error representation is empty, the actual should be nil
func AssertError(t *testing.T, actual error, expectedErr string) {
	t.Helper()
	if len(expectedErr) > 0 && actual == nil {
		t.Errorf("expected %q error", expectedErr)
		return
	}
	if len(expectedErr) > 0 && actual != nil && actual.Error() != expectedErr {
		t.Errorf("expected %q error, but got %q", expectedErr, actual.Error())
		return
	}
	if len(expectedErr) == 0 && actual != nil {
		t.Errorf("unexpected err: %v", actual)
		return
	}
}

// AssertError asserts the actual error representation starts with the expected prerfix,
// if the expected error prefix is empty, the actual should be nil
func AssertErrorWithPrefix(t *testing.T, actual error, expectedErrorPrefix string) {
	t.Helper()
	if len(expectedErrorPrefix) > 0 && actual == nil {
		t.Errorf("expected error with prefix %q", expectedErrorPrefix)
		return
	}
	if len(expectedErrorPrefix) > 0 && actual != nil && !strings.HasPrefix(actual.Error(), expectedErrorPrefix) {
		t.Errorf("expected error with prefix %q, but got %q", expectedErrorPrefix, actual.Error())
		return
	}
	if len(expectedErrorPrefix) == 0 && actual != nil {
		t.Errorf("unexpected err: %v", actual)
		return
	}
}

// AssertActions asserts the actual actions have the expected action verb
func AssertActions(t *testing.T, actualActions []clienttesting.Action, expectedVerbs ...string) {
	t.Helper()
	if len(actualActions) != len(expectedVerbs) {
		t.Fatalf("expected %d call but got %d: %#v", len(expectedVerbs), len(actualActions), actualActions)
	}
	for i, expected := range expectedVerbs {
		if actualActions[i].GetVerb() != expected {
			t.Errorf("expected %s action but got: %#v", expected, actualActions[i])
		}
	}
}

// AssertNoActions asserts no actions are happened
func AssertNoActions(t *testing.T, actualActions []clienttesting.Action) {
	t.Helper()
	AssertActions(t, actualActions)
}

func AssertAction(t *testing.T, actual clienttesting.Action, expected string) {
	t.Helper()
	if actual.GetVerb() != expected {
		t.Errorf("expected %s action but got: %#v", expected, actual)
	}
}

func AssertGet(t *testing.T, actual clienttesting.Action, group, version, resource string) {
	t.Helper()
	if actual.GetVerb() != "get" {
		t.Error(spew.Sdump(actual))
	}
	if actual.GetResource() != (schema.GroupVersionResource{Group: group, Version: version, Resource: resource}) {
		t.Error(spew.Sdump(actual))
	}
}

func AssertDelete(t *testing.T, actual clienttesting.Action, resource, namespace, name string) {
	t.Helper()
	deleteAction, ok := actual.(clienttesting.DeleteAction)
	if !ok {
		t.Error(spew.Sdump(actual))
	}
	if deleteAction.GetResource().Resource != resource {
		t.Error(spew.Sdump(actual))
	}
	if deleteAction.GetNamespace() != namespace {
		t.Error(spew.Sdump(actual))
	}
	if deleteAction.GetName() != name {
		t.Error(spew.Sdump(actual))
	}
}

// AssertUpdateActions asserts the actions are get-then-update action
func AssertUpdateActions(t *testing.T, actions []clienttesting.Action) {
	t.Helper()
	for i := 0; i < len(actions); i += 2 {
		if actions[i].GetVerb() != "get" {
			t.Errorf("expected action %d is get, but %v", i, actions[i])
		}
		if actions[i+1].GetVerb() != "update" {
			t.Errorf("expected action %d is update, but %v", i, actions[i+1])
		}
	}
}

// AssertNoMoreUpdates asserts only one update action in given actions
func AssertNoMoreUpdates(t *testing.T, actions []clienttesting.Action) {
	t.Helper()
	updateActions := 0
	for _, action := range actions {
		if action.GetVerb() == "update" {
			updateActions++
		}
	}
	if updateActions != 1 {
		t.Errorf("expected there is only one update action, but failed")
	}
}

// AssertCondition asserts the actual conditions has
// the expected condition
func AssertCondition(
	t *testing.T,
	actualConditions []metav1.Condition,
	expectedCondition metav1.Condition) {
	t.Helper()
	cond := meta.FindStatusCondition(actualConditions, expectedCondition.Type)
	if cond == nil {
		t.Errorf("expected condition %s but got: %s", expectedCondition.Type, cond.Type)
	}
	if cond.Status != expectedCondition.Status {
		t.Errorf("expected status %s but got: %s", expectedCondition.Status, cond.Status)
	}
	if cond.Reason != expectedCondition.Reason {
		t.Errorf("expected reason %s but got: %s", expectedCondition.Reason, cond.Reason)
	}
	if cond.Message != expectedCondition.Message {
		t.Errorf("expected message %s but got: %s", expectedCondition.Message, cond.Message)
	}
}

func AssertEqualNumber(t *testing.T, actual, expected int) {
	t.Helper()
	if actual != expected {
		t.Errorf("expected %d number of actions but got: %d", expected, actual)
	}
}

func AssertEqualNameNamespace(t *testing.T, actualName, actualNamespace, name, namespace string) {
	t.Helper()
	if actualName != name {
		t.Errorf("Name of the object does not match, expected %s, actual %s", name, actualName)
	}

	if actualNamespace != namespace {
		t.Errorf("Namespace of the object does not match, expected %s, actual %s", namespace, actualNamespace)
	}
}
