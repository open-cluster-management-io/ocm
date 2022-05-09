package csr

import (
	"context"
	"testing"
	"time"

	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
	"open-cluster-management.io/registration/pkg/hub/user"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"

	authorizationv1 "k8s.io/api/authorization/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

var (
	validCSR = testinghelpers.CSRHolder{
		Name:         "testcsr",
		Labels:       map[string]string{"open-cluster-management.io/cluster-name": "managedcluster1"},
		SignerName:   certificatesv1.KubeAPIServerClientSignerName,
		CN:           user.SubjectPrefix + "managedcluster1:spokeagent1",
		Orgs:         []string{user.SubjectPrefix + "managedcluster1", user.ManagedClustersGroup},
		Username:     user.SubjectPrefix + "managedcluster1:spokeagent1",
		ReqBlockType: "CERTIFICATE REQUEST",
	}
)

func TestSync(t *testing.T) {
	cases := []struct {
		name                 string
		startingCSRs         []runtime.Object
		autoApprovingAllowed bool
		validateActions      func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:         "sync a deleted csr",
			startingCSRs: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:         "sync a denied csr",
			startingCSRs: []runtime.Object{testinghelpers.NewDeniedCSR(validCSR)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:         "sync an approved csr",
			startingCSRs: []runtime.Object{testinghelpers.NewApprovedCSR(validCSR)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name: "sync an invalid csr",
			startingCSRs: []runtime.Object{testinghelpers.NewCSR(testinghelpers.CSRHolder{
				Name:         validCSR.Name,
				Labels:       validCSR.Labels,
				SignerName:   validCSR.SignerName,
				CN:           "system:open-cluster-management:managedcluster1:invalidagent",
				Orgs:         validCSR.Orgs,
				Username:     validCSR.Username,
				ReqBlockType: validCSR.ReqBlockType,
			})},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:         "deny an auto approving csr",
			startingCSRs: []runtime.Object{testinghelpers.NewCSR(validCSR)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "create")
				testinghelpers.AssertSubjectAccessReviewObj(t, actions[0].(clienttesting.CreateActionImpl).Object)
			},
		},
		{
			name:                 "allow an auto approving csr",
			startingCSRs:         []runtime.Object{testinghelpers.NewCSR(validCSR)},
			autoApprovingAllowed: true,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := certificatesv1.CertificateSigningRequestCondition{
					Type:    certificatesv1.CertificateApproved,
					Status:  corev1.ConditionTrue,
					Reason:  "AutoApprovedByHubCSRApprovingController",
					Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
				}
				testinghelpers.AssertActions(t, actions, "create", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				testinghelpers.AssertCSRCondition(t, actual.(*certificatesv1.CertificateSigningRequest).Status.Conditions, expectedCondition)
			},
		},
		{
			name: "allow an auto approving csr w/o ManagedClusterGroup for backward-compatibility",
			startingCSRs: []runtime.Object{testinghelpers.NewCSR(testinghelpers.CSRHolder{
				Name:         validCSR.Name,
				Labels:       validCSR.Labels,
				SignerName:   validCSR.SignerName,
				CN:           validCSR.CN,
				Orgs:         sets.NewString(validCSR.Orgs...).Delete(user.ManagedClustersGroup).List(),
				Username:     validCSR.Username,
				ReqBlockType: validCSR.ReqBlockType,
			})},
			autoApprovingAllowed: true,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := certificatesv1.CertificateSigningRequestCondition{
					Type:    certificatesv1.CertificateApproved,
					Status:  corev1.ConditionTrue,
					Reason:  "AutoApprovedByHubCSRApprovingController",
					Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
				}
				testinghelpers.AssertActions(t, actions, "create", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				testinghelpers.AssertCSRCondition(t, actual.(*certificatesv1.CertificateSigningRequest).Status.Conditions, expectedCondition)
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
			informerFactory := informers.NewSharedInformerFactory(kubeClient, 3*time.Minute)
			csrStore := informerFactory.Certificates().V1().CertificateSigningRequests().Informer().GetStore()
			for _, csr := range c.startingCSRs {
				if err := csrStore.Add(csr); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := &csrApprovingController{kubeClient, informerFactory.Certificates().V1().CertificateSigningRequests().Lister(), eventstesting.NewTestingEventRecorder(t)}
			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, validCSR.Name))
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
		csr       testinghelpers.CSRHolder
		isRenewal bool
	}{
		{
			name:      "a spoke cluster csr without labels",
			csr:       testinghelpers.CSRHolder{},
			isRenewal: false,
		},
		{
			name: "an invalid signer name",
			csr: testinghelpers.CSRHolder{
				Labels:     validCSR.Labels,
				SignerName: invalidSignerName,
			},
			isRenewal: false,
		},
		{
			name: "a wrong block type",
			csr: testinghelpers.CSRHolder{
				Labels:       validCSR.Labels,
				SignerName:   validCSR.SignerName,
				ReqBlockType: "RSA PRIVATE KEY",
			},
			isRenewal: false,
		},
		{
			name: "an empty organization",
			csr: testinghelpers.CSRHolder{
				Labels:       validCSR.Labels,
				SignerName:   validCSR.SignerName,
				ReqBlockType: validCSR.ReqBlockType,
			},
			isRenewal: false,
		},
		{
			name: "an invalid organization",
			csr: testinghelpers.CSRHolder{
				Labels:       validCSR.Labels,
				SignerName:   validCSR.SignerName,
				Orgs:         []string{"test"},
				ReqBlockType: validCSR.ReqBlockType,
			},
			isRenewal: false,
		},
		{
			name: "an invalid common name",
			csr: testinghelpers.CSRHolder{
				Labels:       validCSR.Labels,
				SignerName:   validCSR.SignerName,
				Orgs:         validCSR.Orgs,
				ReqBlockType: validCSR.ReqBlockType,
			},
			isRenewal: false,
		},
		{
			name: "an common name does not equal user name",
			csr: testinghelpers.CSRHolder{
				Name:         validCSR.Name,
				Labels:       validCSR.Labels,
				SignerName:   validCSR.SignerName,
				CN:           "system:open-cluster-management:managedcluster1:invalidagent",
				Orgs:         validCSR.Orgs,
				Username:     validCSR.Username,
				ReqBlockType: validCSR.ReqBlockType,
			},
			isRenewal: false,
		},
		{
			name: "a renewal csr without signer name",
			csr: testinghelpers.CSRHolder{
				Name:         validCSR.Name,
				Labels:       validCSR.Labels,
				SignerName:   "",
				CN:           validCSR.CN,
				Orgs:         validCSR.Orgs,
				Username:     validCSR.Username,
				ReqBlockType: validCSR.ReqBlockType,
			},
			isRenewal: false,
		},
		{
			name:      "a renewal csr",
			csr:       validCSR,
			isRenewal: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isRenewal := isSpokeClusterClientCertRenewal(testinghelpers.NewCSR(c.csr))
			if isRenewal != c.isRenewal {
				t.Errorf("expected %t, but failed", c.isRenewal)
			}
		})
	}
}
