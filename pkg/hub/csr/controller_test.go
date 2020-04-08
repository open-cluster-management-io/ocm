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

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	authorizationv1 "k8s.io/api/authorization/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
)

const testCSRName = "test_csr"

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
				if len(actions) != 1 {
					t.Errorf("expected 1 call but got: %#v", actions)
				}
				assertAction(t, actions[0], "get")
			},
		},
		{
			name:         "sync a denied csr",
			startingCSRs: []runtime.Object{newDeniedCSR()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expected 1 call but got: %#v", actions)
				}
				assertAction(t, actions[0], "get")
			},
		},
		{
			name:         "sync an unrecognized csr",
			startingCSRs: []runtime.Object{newUnrecognizedCSR()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expected 1 call but got: %#v", actions)
				}
				assertAction(t, actions[0], "get")
			},
		},
		{
			name:         "sync a hub cluster admin approved csr",
			startingCSRs: []runtime.Object{newApprovedCSR()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 5 {
					t.Errorf("expected 5 call but got: %#v", actions)
				}
				assertAction(t, actions[4], "create")
				assertClusterRoleBindingCreated(t, actions[4].(clienttesting.CreateActionImpl).Object)
			},
		},
		{
			name:         "deny an auto approving csr",
			startingCSRs: []runtime.Object{newRecognizedCSR()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("expected 2 call but got: %#v", actions)
				}
				assertAction(t, actions[1], "create")
				assertSubjectAccessReviewCreated(t, actions[1].(clienttesting.CreateActionImpl).Object)
			},
		},
		{
			name:                 "allow an auto approving csr",
			startingCSRs:         []runtime.Object{newRecognizedCSR()},
			autoApprovingAllowed: true,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 3 {
					t.Errorf("expected 3 call but got: %#v", actions)
				}
				assertAction(t, actions[2], "update")
				assertCondition(t, actions[2].(clienttesting.UpdateActionImpl).Object, certificatesv1beta1.CertificateApproved, "AutoApproved")
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

			ctrl := &csrController{kubeClient, eventstesting.NewTestingEventRecorder(t)}
			syncErr := ctrl.sync(context.TODO(), newFakeSyncContext(t))
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

func TestRecognize(t *testing.T) {
	cases := []struct {
		name                string
		csr                 *certificatesv1beta1.CertificateSigningRequest
		recognized          bool
		expectedClusterName string
	}{
		{
			name:       "a wrong block type",
			csr:        newCSR("", []string{}, "", "RSA PRIVATE KEY"),
			recognized: false,
		},
		{
			name:       "an empty organization",
			csr:        newCSR("", []string{}, "", "CERTIFICATE REQUEST"),
			recognized: false,
		},
		{
			name:       "an invalid organization",
			csr:        newCSR("", []string{"test"}, "", "CERTIFICATE REQUEST"),
			recognized: false,
		},
		{
			name:       "an invalid common name",
			csr:        newCSR("", []string{"system:open-cluster-management:spokecluster1"}, "", "CERTIFICATE REQUEST"),
			recognized: false,
		},
		{
			name:                "a recognied csr",
			csr:                 newRecognizedCSR(),
			recognized:          true,
			expectedClusterName: "spokecluster1",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recognized, clusterName := recognize(c.csr)
			if recognized != c.recognized {
				t.Errorf("expected %t, but failed", c.recognized)
			}
			if clusterName != c.expectedClusterName {
				t.Errorf("expected %q, but got %q", c.expectedClusterName, clusterName)
			}
		})
	}
}

func newCSR(cn string, orgs []string, username string, reqBlockType string) *certificatesv1beta1.CertificateSigningRequest {
	insecureRand := rand.New(rand.NewSource(0))
	signerName := "tester"
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
		Spec: certificatesv1beta1.CertificateSigningRequestSpec{
			Username:   username,
			Usages:     []certificatesv1beta1.KeyUsage{},
			SignerName: &signerName,
			Request:    pem.EncodeToMemory(&pem.Block{Type: reqBlockType, Bytes: csrb}),
		},
	}
}

func newRecognizedCSR() *certificatesv1beta1.CertificateSigningRequest {
	csr := newCSR(
		"system:open-cluster-management:spokecluster1:spokeagent1",
		[]string{"system:open-cluster-management:spokecluster1"},
		"system:open-cluster-management:spokecluster1:spokeagent1",
		"CERTIFICATE REQUEST",
	)
	csr.Name = testCSRName
	return csr
}

func newUnrecognizedCSR() *certificatesv1beta1.CertificateSigningRequest {
	csr := newCSR(
		"system:open-cluster-management:spokecluster1:spokeagent2",
		[]string{"system:open-cluster-management:spokecluster1"},
		"system:open-cluster-management:spokecluster1:spokeagent1",
		"CERTIFICATE REQUEST",
	)
	csr.Name = testCSRName
	return csr
}

func newDeniedCSR() *certificatesv1beta1.CertificateSigningRequest {
	csr := newRecognizedCSR()
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
		Type: certificatesv1beta1.CertificateDenied,
	})
	return csr
}

func newApprovedCSR() *certificatesv1beta1.CertificateSigningRequest {
	csr := newRecognizedCSR()
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
		Type: certificatesv1beta1.CertificateApproved,
	})
	return csr
}

func assertAction(t *testing.T, actual clienttesting.Action, expected string) {
	if actual.GetVerb() != expected {
		t.Errorf("expected %s action but got: %#v", expected, actual)
	}
}

func assertClusterRoleBindingCreated(t *testing.T, actual runtime.Object) {
	_, ok := actual.(*rbacv1.ClusterRoleBinding)
	if !ok {
		t.Errorf("expected clusterrolebinding created, but got: %#v", actual)
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

type fakeSyncContext struct {
	csrName  string
	recorder events.Recorder
}

func newFakeSyncContext(t *testing.T) *fakeSyncContext {
	return &fakeSyncContext{
		csrName:  testCSRName,
		recorder: eventstesting.NewTestingEventRecorder(t),
	}
}

func (f fakeSyncContext) Queue() workqueue.RateLimitingInterface { return nil }
func (f fakeSyncContext) QueueKey() string                       { return f.csrName }
func (f fakeSyncContext) Recorder() events.Recorder              { return f.recorder }
