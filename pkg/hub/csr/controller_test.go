package csr

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/rand"
	"net"
	"testing"

	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	authorizationv1 "k8s.io/api/authorization/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

const testCSRName = "test_csr"

var (
	labels     = map[string]string{"open-cluster-management.io/cluster-name": "managedcluster1"}
	signerName = certificatesv1beta1.KubeAPIServerClientSignerName
)

func TestSync(t *testing.T) {
	cases := []struct {
		name                 string
		startingCSRs         []runtime.Object
		autoApprovingAllowed bool
		validateActions      func(t *testing.T, actions []clienttesting.Action)
		expectedErr          string
	}{
		{
			name:         "sync a deleted csr",
			startingCSRs: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get")
			},
		},
		{
			name:         "sync a denied csr",
			startingCSRs: []runtime.Object{newDeniedCSR()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get")
			},
		},
		{
			name:         "sync an approved csr",
			startingCSRs: []runtime.Object{newApprovedCSR()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get")
			},
		},
		{
			name:         "sync an invalid csr",
			startingCSRs: []runtime.Object{newInvalidCSR()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get")
			},
		},
		{
			name:         "deny an auto approving csr",
			startingCSRs: []runtime.Object{newRenewalCSR()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get", "create")
				assertSubjectAccessReviewCreated(t, actions[1].(clienttesting.CreateActionImpl).Object)
			},
		},
		{
			name:                 "allow an auto approving csr",
			startingCSRs:         []runtime.Object{newRenewalCSR()},
			autoApprovingAllowed: true,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get", "create", "update")
				assertCondition(t, actions[2].(clienttesting.UpdateActionImpl).Object, certificatesv1beta1.CertificateApproved, "AutoApprovedByHubCSRApprovingController")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.startingCSRs...)
			kubeClient.PrependReactor(
				"create",
				"subjectaccessreviews",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, &authorizationv1.SubjectAccessReview{
						Status: authorizationv1.SubjectAccessReviewStatus{
							Allowed: c.autoApprovingAllowed,
						},
					}, nil
				},
			)

			ctrl := &csrApprovingController{kubeClient, eventstesting.NewTestingEventRecorder(t)}
			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, testCSRName))
			if len(c.expectedErr) > 0 && syncErr == nil {
				t.Errorf("expected %q error", c.expectedErr)
				return
			}
			if len(c.expectedErr) > 0 && syncErr != nil && syncErr.Error() != c.expectedErr {
				t.Errorf("expected %q error, got %q", c.expectedErr, syncErr.Error())
				return
			}
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, kubeClient.Actions())
		})
	}
}

func TestIsSpokeClusterClientCertRenewal(t *testing.T) {
	invalidSignerName := "invalidsigner"

	cases := []struct {
		name      string
		csr       *certificatesv1beta1.CertificateSigningRequest
		isRenewal bool
	}{
		{
			name:      "a spoke cluster csr without labels",
			csr:       newCSR(map[string]string{}, nil, "", []string{}, "", ""),
			isRenewal: false,
		},
		{
			name:      "an invalid signer name",
			csr:       newCSR(labels, &invalidSignerName, "", []string{}, "", ""),
			isRenewal: false,
		},
		{
			name:      "a wrong block type",
			csr:       newCSR(labels, &signerName, "", []string{}, "", "RSA PRIVATE KEY"),
			isRenewal: false,
		},
		{
			name:      "an empty organization",
			csr:       newCSR(labels, &signerName, "", []string{}, "", "CERTIFICATE REQUEST"),
			isRenewal: false,
		},
		{
			name:      "an invalid organization",
			csr:       newCSR(labels, &signerName, "", []string{"test"}, "", "CERTIFICATE REQUEST"),
			isRenewal: false,
		},
		{
			name:      "an invalid common name",
			csr:       newCSR(labels, &signerName, "", []string{"system:open-cluster-management:managedcluster1"}, "", "CERTIFICATE REQUEST"),
			isRenewal: false,
		},
		{
			name:      "an common name does not equal user name",
			csr:       newInvalidCSR(),
			isRenewal: false,
		},
		{
			name:      "a renewal csr without signer name",
			csr:       newCSRWithSignerName(nil),
			isRenewal: true,
		},
		{
			name:      "a renewal csr",
			csr:       newRenewalCSR(),
			isRenewal: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isRenewal := isSpokeClusterClientCertRenewal(c.csr)
			if isRenewal != c.isRenewal {
				t.Errorf("expected %t, but failed", c.isRenewal)
			}
		})
	}
}

func newCSR(labels map[string]string, signerName *string, cn string, orgs []string, username string, reqBlockType string) *certificatesv1beta1.CertificateSigningRequest {
	insecureRand := rand.New(rand.NewSource(0))
	pk, err := ecdsa.GenerateKey(elliptic.P256(), insecureRand)
	if err != nil {
		panic(err)
	}
	csrb, err := x509.CreateCertificateRequest(insecureRand, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   cn,
			Organization: orgs,
		},
		DNSNames:       []string{},
		EmailAddresses: []string{},
		IPAddresses:    []net.IP{},
	}, pk)
	if err != nil {
		panic(err)
	}
	return &certificatesv1beta1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
		},
		Spec: certificatesv1beta1.CertificateSigningRequestSpec{
			Username:   username,
			Usages:     []certificatesv1beta1.KeyUsage{},
			SignerName: signerName,
			Request:    pem.EncodeToMemory(&pem.Block{Type: reqBlockType, Bytes: csrb}),
		},
	}
}

func newCSRWithSignerName(signer *string) *certificatesv1beta1.CertificateSigningRequest {
	csr := newCSR(
		labels,
		signer,
		"system:open-cluster-management:managedcluster1:spokeagent1",
		[]string{"system:open-cluster-management:managedcluster1"},
		"system:open-cluster-management:managedcluster1:spokeagent1",
		"CERTIFICATE REQUEST",
	)
	csr.Name = testCSRName
	return csr
}

func newRenewalCSR() *certificatesv1beta1.CertificateSigningRequest {
	return newCSRWithSignerName(&signerName)
}

func newInvalidCSR() *certificatesv1beta1.CertificateSigningRequest {
	csr := newCSR(
		labels,
		&signerName,
		"system:open-cluster-management:managedcluster1:spokeagent2",
		[]string{"system:open-cluster-management:managedcluster1"},
		"system:open-cluster-management:managedcluster1:spokeagent1",
		"CERTIFICATE REQUEST",
	)
	csr.Name = testCSRName
	return csr
}

func newDeniedCSR() *certificatesv1beta1.CertificateSigningRequest {
	csr := newRenewalCSR()
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
		Type: certificatesv1beta1.CertificateDenied,
	})
	return csr
}

func newApprovedCSR() *certificatesv1beta1.CertificateSigningRequest {
	csr := newRenewalCSR()
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
		Type: certificatesv1beta1.CertificateApproved,
	})
	return csr
}

func assertActions(t *testing.T, actualActions []clienttesting.Action, expectedActions ...string) {
	if len(actualActions) != len(expectedActions) {
		t.Errorf("expected %d call but got: %#v", len(expectedActions), actualActions)
	}
	for i, expected := range expectedActions {
		if actualActions[i].GetVerb() != expected {
			t.Errorf("expected %s action but got: %#v", expected, actualActions[i])
		}
	}
}

func assertSubjectAccessReviewCreated(t *testing.T, actual runtime.Object) {
	_, ok := actual.(*authorizationv1.SubjectAccessReview)
	if !ok {
		t.Errorf("expected subjectaccessreview created, but got: %#v", actual)
	}
}

func assertCondition(t *testing.T, actual runtime.Object, expectedCondition certificatesv1beta1.RequestConditionType, expectedReason string) {
	csr := actual.(*certificatesv1beta1.CertificateSigningRequest)
	conditions := csr.Status.Conditions
	if len(conditions) != 1 {
		t.Errorf("expected 1 condition but got: %#v", conditions)
	}
	condition := conditions[0]
	if condition.Type != expectedCondition {
		t.Errorf("expected %s but got: %s", expectedCondition, condition.Type)
	}
	if condition.Reason != expectedReason {
		t.Errorf("expected %s but got: %s", expectedReason, condition.Reason)
	}
}
