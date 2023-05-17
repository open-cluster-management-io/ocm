package csr

import (
	"context"
	"testing"
	"time"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
	"open-cluster-management.io/registration/pkg/hub/user"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"

	authorizationv1 "k8s.io/api/authorization/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		startingClusters     []runtime.Object
		startingCSRs         []runtime.Object
		approvalUsers        []string
		autoApprovingAllowed bool
		validateActions      func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:             "sync a deleted csr",
			startingClusters: []runtime.Object{},
			startingCSRs:     []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:             "sync a denied csr",
			startingClusters: []runtime.Object{},
			startingCSRs:     []runtime.Object{testinghelpers.NewDeniedCSR(validCSR)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:             "sync an approved csr",
			startingClusters: []runtime.Object{},
			startingCSRs:     []runtime.Object{testinghelpers.NewApprovedCSR(validCSR)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:             "sync an invalid csr",
			startingClusters: []runtime.Object{},
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
			name:             "deny an auto approving csr",
			startingClusters: []runtime.Object{},
			startingCSRs:     []runtime.Object{testinghelpers.NewCSR(validCSR)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "create")
				testinghelpers.AssertSubjectAccessReviewObj(t, actions[0].(clienttesting.CreateActionImpl).Object)
			},
		},
		{
			name:                 "allow an auto approving csr",
			startingClusters:     []runtime.Object{},
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
			name:             "allow an auto approving csr w/o ManagedClusterGroup for backward-compatibility",
			startingClusters: []runtime.Object{},
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
		{
			name: "auto approve a bootstrap csr request",
			startingClusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedcluster1",
					},
				},
			},
			startingCSRs: []runtime.Object{func() *certificatesv1.CertificateSigningRequest {
				csr := testinghelpers.NewCSR(validCSR)
				csr.Spec.Username = "test"
				return csr
			}()},
			autoApprovingAllowed: true,
			approvalUsers:        []string{"test"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := certificatesv1.CertificateSigningRequestCondition{
					Type:    certificatesv1.CertificateApproved,
					Status:  corev1.ConditionTrue,
					Reason:  "AutoApprovedByHubCSRApprovingController",
					Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
				}
				testinghelpers.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
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

			clusterClient := clusterfake.NewSimpleClientset(c.startingClusters...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.startingClusters {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			recorder := eventstesting.NewTestingEventRecorder(t)
			ctrl := &csrApprovingController[*certificatesv1.CertificateSigningRequest]{
				lister:   informerFactory.Certificates().V1().CertificateSigningRequests().Lister(),
				approver: NewCSRV1Approver(kubeClient),
				reconcilers: []Reconciler{
					&csrBootstrapReconciler{
						kubeClient:    kubeClient,
						eventRecorder: recorder,
						approvalUsers: sets.Set[string]{},
					},
					NewCSRRenewalReconciler(kubeClient, recorder),
					NewCSRBootstrapReconciler(
						kubeClient,
						clusterClient,
						clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
						c.approvalUsers,
						recorder,
					),
				},
			}
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
		name        string
		csr         testinghelpers.CSRHolder
		isRenewal   bool
		clusterName string
		commonName  string
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
			name:        "a renewal csr",
			csr:         validCSR,
			isRenewal:   true,
			clusterName: "managedcluster1",
			commonName:  validCSR.CN,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isRenewal, clusterName, commonName := validateCSR(newCSRInfo(testinghelpers.NewCSR(c.csr)))
			if isRenewal != c.isRenewal {
				t.Errorf("expected %t, but failed", c.isRenewal)
			}
			if clusterName != c.clusterName {
				t.Errorf("expected %s, but failed", commonName)
			}
			if commonName != c.commonName {
				t.Errorf("expected %s, but failed", commonName)
			}
		})
	}
}
