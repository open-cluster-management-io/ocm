package csr

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	authorizationv1 "k8s.io/api/authorization/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
	"open-cluster-management.io/registration/pkg/hub/user"
)

var validV1beta1CSR = testinghelpers.CSRHolder{
	Name:         "testcsr",
	Labels:       map[string]string{"open-cluster-management.io/cluster-name": "managedcluster1"},
	SignerName:   certificatesv1beta1.KubeAPIServerClientSignerName,
	CN:           user.SubjectPrefix + "managedcluster1:spokeagent1",
	Orgs:         []string{user.SubjectPrefix + "managedcluster1", user.ManagedClustersGroup},
	Username:     user.SubjectPrefix + "managedcluster1:spokeagent1",
	ReqBlockType: "CERTIFICATE REQUEST",
}

func Test_v1beta1CSRApprovingController_sync(t *testing.T) {
	tests := []struct {
		name                 string
		startingCSRs         []runtime.Object
		autoApprovingAllowed bool
		wantErr              bool
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
			startingCSRs: []runtime.Object{testinghelpers.NewDeniedV1beta1CSR(validV1beta1CSR)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:         "sync an approved csr",
			startingCSRs: []runtime.Object{testinghelpers.NewApprovedV1beta1CSR(validV1beta1CSR)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name: "sync an invalid csr",
			startingCSRs: []runtime.Object{testinghelpers.NewV1beta1CSR(testinghelpers.CSRHolder{
				Name:         validV1beta1CSR.Name,
				Labels:       validV1beta1CSR.Labels,
				SignerName:   validV1beta1CSR.SignerName,
				CN:           "system:open-cluster-management:managedcluster1:invalidagent",
				Orgs:         validV1beta1CSR.Orgs,
				Username:     validV1beta1CSR.Username,
				ReqBlockType: validV1beta1CSR.ReqBlockType,
			})},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:         "deny an auto approving csr",
			startingCSRs: []runtime.Object{testinghelpers.NewV1beta1CSR(validV1beta1CSR)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "create")
				testinghelpers.AssertSubjectAccessReviewObj(t, actions[0].(clienttesting.CreateActionImpl).Object)
			},
		},
		{
			name:                 "allow an auto approving csr",
			startingCSRs:         []runtime.Object{testinghelpers.NewV1beta1CSR(validV1beta1CSR)},
			autoApprovingAllowed: true,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := certificatesv1beta1.CertificateSigningRequestCondition{
					Type:    certificatesv1beta1.CertificateApproved,
					Status:  corev1.ConditionTrue,
					Reason:  "AutoApprovedByHubCSRApprovingController",
					Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
				}
				testinghelpers.AssertActions(t, actions, "create", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				testinghelpers.AssertV1beta1CSRCondition(t, actual.(*certificatesv1beta1.CertificateSigningRequest).Status.Conditions, expectedCondition)
			},
		},
		{
			name: "allow an auto approving csr w/o ManagedClusterGroup for backward-compatibility",
			startingCSRs: []runtime.Object{testinghelpers.NewV1beta1CSR(testinghelpers.CSRHolder{
				Name:         validV1beta1CSR.Name,
				Labels:       validV1beta1CSR.Labels,
				SignerName:   validV1beta1CSR.SignerName,
				CN:           validV1beta1CSR.CN,
				Orgs:         sets.NewString(validV1beta1CSR.Orgs...).Delete(user.ManagedClustersGroup).List(),
				Username:     validV1beta1CSR.Username,
				ReqBlockType: validV1beta1CSR.ReqBlockType,
			})},
			autoApprovingAllowed: true,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := certificatesv1beta1.CertificateSigningRequestCondition{
					Type:    certificatesv1beta1.CertificateApproved,
					Status:  corev1.ConditionTrue,
					Reason:  "AutoApprovedByHubCSRApprovingController",
					Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
				}
				testinghelpers.AssertActions(t, actions, "create", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				testinghelpers.AssertV1beta1CSRCondition(t, actual.(*certificatesv1beta1.CertificateSigningRequest).Status.Conditions, expectedCondition)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(tt.startingCSRs...)
			kubeClient.PrependReactor(
				"create",
				"subjectaccessreviews",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					return true,
						&authorizationv1.SubjectAccessReview{
							Status: authorizationv1.SubjectAccessReviewStatus{Allowed: tt.autoApprovingAllowed},
						}, nil
				},
			)

			informerFactory := informers.NewSharedInformerFactory(kubeClient, 3*time.Minute)
			csrStore := informerFactory.Certificates().V1beta1().CertificateSigningRequests().Informer().GetStore()
			for _, csr := range tt.startingCSRs {
				if err := csrStore.Add(csr); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := &v1beta1CSRApprovingController{
				informerFactory.Certificates().V1beta1().CertificateSigningRequests().Lister(),
				[]Reconciler{
					&csrBootstrapReconciler{},
					&csrRenewalReconciler{
						kubeClient:    kubeClient,
						eventRecorder: eventstesting.NewTestingEventRecorder(t),
					},
				},
			}
			if err := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, validV1beta1CSR.Name)); (err != nil) != tt.wantErr {
				t.Errorf("v1beta1CSRApprovingController.sync() error = %v, wantErr %v", err, tt.wantErr)
			}
			tt.validateActions(t, kubeClient.Actions())
		})
	}
}
