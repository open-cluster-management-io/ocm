package ssarcontroller

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

// With the spiffe driver there is no hub kubeconfig to check: the hub connection is reported as
// not degraded, without any SubjectAccessReview.
func TestSyncSPIFFE(t *testing.T) {
	k := newKlusterlet("klusterlet", "open-cluster-management-agent", "cluster1")
	k.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationConfiguration{
		RegistrationDriver: operatorapiv1.RegistrationDriver{AuthType: operatorapiv1.SPIFFEAuthType}}
	controller := newTestController(t, k)
	if err := controller.controller.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, k.Name), k.Name); err != nil {
		t.Fatal(err)
	}
	got, err := controller.operatorClient.OperatorV1().Klusterlets().Get(context.TODO(), k.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, operatorapiv1.ConditionHubConnectionDegraded)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != ReasonHubConnectionSPIFFE {
		t.Errorf("HubConnectionDegraded %+v", cond)
	}
}
