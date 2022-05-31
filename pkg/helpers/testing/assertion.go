package testing

import (
	"bytes"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	authorizationv1 "k8s.io/api/authorization/v1"
	certv1 "k8s.io/api/certificates/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/diff"
)

// AssertError asserts the actual error representation is the same with the expected,
// if the expected error representation is empty, the actual should be nil
func AssertError(t *testing.T, actual error, expectedErr string) {
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
	if len(actualActions) != len(expectedVerbs) {
		t.Fatalf("expected %d call but got: %#v", len(expectedVerbs), actualActions)
	}
	for i, expected := range expectedVerbs {
		if actualActions[i].GetVerb() != expected {
			t.Errorf("expected %s action but got: %#v", expected, actualActions[i])
		}
	}
}

// AssertNoActions asserts no actions are happened
func AssertNoActions(t *testing.T, actualActions []clienttesting.Action) {
	AssertActions(t, actualActions)
}

// AssertUpdateActions asserts the actions are get-then-update action
func AssertUpdateActions(t *testing.T, actions []clienttesting.Action) {
	for i := 0; i < len(actions); i = i + 2 {
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

// AssertFinalizers asserts the given runtime object has the expected finalizers
func AssertFinalizers(t *testing.T, obj runtime.Object, finalizers []string) {
	accessor, _ := meta.Accessor(obj)
	actual := accessor.GetFinalizers()
	if len(actual) == 0 && len(finalizers) == 0 {
		return
	}
	if !reflect.DeepEqual(actual, finalizers) {
		t.Fatal(diff.ObjectDiff(actual, finalizers))
	}
}

// AssertCondition asserts the actual conditions has
// the expected condition
func AssertCondition(
	t *testing.T,
	actualConditions []metav1.Condition,
	expectedCondition metav1.Condition) {
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

// AssertManagedClusterClientConfigs asserts the actual managed cluster client configs are the
// same wiht the expected
func AssertManagedClusterClientConfigs(t *testing.T, actual, expected []clusterv1.ClientConfig) {
	if len(actual) == 0 && len(expected) == 0 {
		return
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("expected client configs %#v but got: %#v", expected, actual)
	}
}

// AssertManagedClusterStatus sserts the actual managed cluster status is the same
// wiht the expected
func AssertManagedClusterStatus(t *testing.T, actual, expected clusterv1.ManagedClusterStatus) {
	if !reflect.DeepEqual(actual.Version, expected.Version) {
		t.Errorf("expected version %#v but got: %#v", expected.Version, actual.Version)
	}
	if !actual.Capacity["cpu"].Equal(expected.Capacity["cpu"]) {
		t.Errorf("expected cpu capacity %#v but got: %#v", expected.Capacity["cpu"], actual.Capacity["cpu"])
	}
	if !actual.Capacity["memory"].Equal(expected.Capacity["memory"]) {
		t.Errorf("expected memory capacity %#v but got: %#v", expected.Capacity["memory"], actual.Capacity["memory"])
	}
	if !actual.Allocatable["cpu"].Equal(expected.Allocatable["cpu"]) {
		t.Errorf("expected cpu allocatable %#v but got: %#v", expected.Allocatable["cpu"], actual.Allocatable["cpu"])
	}
	if !actual.Allocatable["memory"].Equal(expected.Allocatable["memory"]) {
		t.Errorf("expected memory alocatabel %#v but got: %#v", expected.Allocatable["memory"], actual.Allocatable["memory"])
	}
}

// AssertSubjectAccessReviewObj asserts the given runtime object is the
// authorization SubjectAccessReview object
func AssertSubjectAccessReviewObj(t *testing.T, actual runtime.Object) {
	_, ok := actual.(*authorizationv1.SubjectAccessReview)
	if !ok {
		t.Errorf("expected subjectaccessreview object, but got: %#v", actual)
	}
}

// AssertCSRCondition asserts the actual csr conditions has the expected condition
func AssertCSRCondition(
	t *testing.T,
	actualConditions []certv1.CertificateSigningRequestCondition,
	expectedCondition certv1.CertificateSigningRequestCondition) {
	var cond *certv1.CertificateSigningRequestCondition
	for i := range actualConditions {
		condition := actualConditions[i]
		if condition.Type == expectedCondition.Type {
			cond = &condition
			break
		}
	}
	if cond == nil {
		t.Errorf("expected condition %s but got: %s", expectedCondition.Type, cond.Type)
	}
	if cond.Reason != expectedCondition.Reason {
		t.Errorf("expected reason %s but got: %s", expectedCondition.Reason, cond.Reason)
	}
	if cond.Message != expectedCondition.Message {
		t.Errorf("expected message %s but got: %s", expectedCondition.Message, cond.Message)
	}
}

// AssertLeaseUpdated asserts the lease obj is updated
func AssertLeaseUpdated(t *testing.T, lease, lastLease *coordinationv1.Lease) {
	if lease == nil || lastLease == nil {
		t.Errorf("expected lease objects are not nil, but failed")
		return
	}
	if (lease.Namespace != lastLease.Namespace) || (lease.Name != lastLease.Name) {
		t.Errorf("expected two lease objects are same lease, but failed")
	}
	if !lease.Spec.RenewTime.BeforeTime(&metav1.Time{Time: lastLease.Spec.RenewTime.Time}) {
		t.Errorf("expected lease updated, but failed")
	}
}

// AssertFileExist asserts a given file exists
func AssertFileExist(t *testing.T, filePath string) {
	if _, err := os.Stat(filePath); err != nil {
		t.Errorf("expected file %q exits, but got error %v", filePath, err)
	}
}

// AssertFileContent asserts a given file content
func AssertFileContent(t *testing.T, filePath string, expectedContent []byte) {
	content, _ := ioutil.ReadFile(filePath)
	if !bytes.Equal(content, expectedContent) {
		t.Errorf("expect %v, but got %v", expectedContent, content)
	}
}
