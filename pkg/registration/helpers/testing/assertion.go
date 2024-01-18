package testing

import (
	"bytes"
	"os"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	certv1 "k8s.io/api/certificates/v1"
	certv1beta1 "k8s.io/api/certificates/v1beta1"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// AssertManagedClusterClientConfigs asserts the actual managed cluster client configs are the
// same with the expected
func AssertManagedClusterClientConfigs(t *testing.T, actual, expected []clusterv1.ClientConfig) {
	if len(actual) == 0 && len(expected) == 0 {
		return
	}
	if !equality.Semantic.DeepEqual(actual, expected) {
		t.Errorf("expected client configs %#v but got: %#v", expected, actual)
	}
}

// AssertManagedClusterStatus asserts the actual managed cluster status is the same
// with the expected
func AssertManagedClusterStatus(t *testing.T, actual, expected clusterv1.ManagedClusterStatus) {
	if !equality.Semantic.DeepEqual(actual.Version, expected.Version) {
		t.Errorf("expected version %#v but got: %#v", expected.Version, actual.Version)
	}

	if !equality.Semantic.DeepEqual(actual.Capacity, expected.Capacity) {
		t.Errorf("expected cluster capacity %#v but got: %#v", expected.Capacity, actual.Capacity)
	}

	if !equality.Semantic.DeepEqual(actual.Allocatable, expected.Allocatable) {
		t.Errorf("expected cluster allocatable %#v but got: %#v", expected.Allocatable, actual.Allocatable)
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

// AssertV1beta1CSRCondition asserts the actual csr conditions has the expected condition
func AssertV1beta1CSRCondition(
	t *testing.T,
	actualConditions []certv1beta1.CertificateSigningRequestCondition,
	expectedCondition certv1beta1.CertificateSigningRequestCondition) {
	var cond *certv1beta1.CertificateSigningRequestCondition
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
	content, _ := os.ReadFile(filePath)
	if !bytes.Equal(content, expectedContent) {
		t.Errorf("expect %v, but got %v", expectedContent, content)
	}
}
