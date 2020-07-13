package hubclientcert

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"

	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	testNamespace  = "testns"
	testAgentName  = "testagent"
	testSecretName = "testsecret"
	testCSRName    = "testcsr"
)

var commonName = fmt.Sprintf("%s%s:%s", subjectPrefix, testinghelpers.TestManagedClusterName, testAgentName)

func TestSync(t *testing.T) {
	cases := []struct {
		name            string
		secrets         []runtime.Object
		approvedCSRCert *testinghelpers.TestCert
		keyDataExpected bool
		csrNameExpected bool
		validateActions func(t *testing.T, hubActions, agentActions []clienttesting.Action)
	}{
		{
			name:            "agent bootstrap",
			secrets:         []runtime.Object{},
			keyDataExpected: true,
			csrNameExpected: true,
			validateActions: func(t *testing.T, hubActions, agentActions []clienttesting.Action) {
				testinghelpers.AssertActions(t, hubActions, "create")
				actual := hubActions[0].(clienttesting.CreateActionImpl).Object
				if _, ok := actual.(*certificates.CertificateSigningRequest); !ok {
					t.Errorf("expected csr was created, but failed")
				}
				expectedSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testNamespace,
						Name:      testSecretName,
					},
					Data: map[string][]byte{
						ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
						AgentNameFile:   []byte(testAgentName),
					},
				}
				testinghelpers.AssertActions(t, agentActions, "create")
				actualSecret := agentActions[0].(clienttesting.CreateActionImpl).Object
				if !reflect.DeepEqual(expectedSecret, actualSecret) {
					t.Errorf("expected secret %v, but got %v", expectedSecret, actualSecret)
				}
			},
		},
		{
			name: "syc csr after bootstrap",
			secrets: []runtime.Object{
				testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "1", nil, map[string][]byte{
					ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					AgentNameFile:   []byte(testAgentName),
				},
				),
			},
			approvedCSRCert: testinghelpers.NewTestCert(testinghelpers.TestManagedClusterName, 10*time.Second),
			validateActions: func(t *testing.T, hubActions, agentActions []clienttesting.Action) {
				testinghelpers.AssertActions(t, hubActions, "get")
				testinghelpers.AssertActions(t, agentActions, "update")
				actual := agentActions[0].(clienttesting.UpdateActionImpl).Object
				if !hasValidKubeconfig(actual.(*corev1.Secret)) {
					t.Error("kubeconfig secret is invalid")
				}
			},
		},
		{
			name: "sync a valid hub kubeconfig secret",
			secrets: []runtime.Object{
				testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "1", testinghelpers.NewTestCert(commonName, 100*time.Second), map[string][]byte{
					ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					AgentNameFile:   []byte(testAgentName),
					KubeconfigFile:  testinghelpers.NewKubeconfig(nil, nil),
				}),
			},
			validateActions: func(t *testing.T, hubActions, agentActions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, hubActions)
				testinghelpers.AssertNoActions(t, agentActions)
			},
		},
		{
			name: "sync an expiring hub kubeconfig secret",
			secrets: []runtime.Object{
				testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "1", testinghelpers.NewTestCert(commonName, -3*time.Second), map[string][]byte{
					ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					AgentNameFile:   []byte(testAgentName),
					KubeconfigFile:  testinghelpers.NewKubeconfig(nil, nil),
				}),
			},
			keyDataExpected: true,
			csrNameExpected: true,
			validateActions: func(t *testing.T, hubActions, agentActions []clienttesting.Action) {
				testinghelpers.AssertActions(t, hubActions, "create")
				actual := hubActions[0].(clienttesting.CreateActionImpl).Object
				if _, ok := actual.(*certificates.CertificateSigningRequest); !ok {
					t.Errorf("expected csr was created, but failed")
				}
				testinghelpers.AssertNoActions(t, agentActions)
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			csrs := []runtime.Object{}
			if c.approvedCSRCert != nil {
				csr := testinghelpers.NewApprovedCSR(testinghelpers.CSRHolder{Name: testCSRName})
				csr.Status.Certificate = c.approvedCSRCert.Cert
				csrs = append(csrs, csr)
			}
			hubKubeClient := kubefake.NewSimpleClientset(csrs...)
			// GenerateName is not working for fake clent, we set the name with prepend reactor
			hubKubeClient.PrependReactor(
				"create",
				"certificatesigningrequests",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, testinghelpers.NewCSR(testinghelpers.CSRHolder{Name: testCSRName}), nil
				},
			)
			hubInformerFactory := informers.NewSharedInformerFactory(hubKubeClient, 3*time.Minute)

			agentKubeClient := kubefake.NewSimpleClientset(c.secrets...)
			agentInformerFactory := informers.NewSharedInformerFactory(agentKubeClient, 3*time.Minute)
			secretStore := agentInformerFactory.Core().V1().Secrets().Informer().GetStore()
			for _, secret := range c.secrets {
				secretStore.Add(secret)
			}

			controller := &ClientCertForHubController{
				clusterName:                  testinghelpers.TestManagedClusterName,
				agentName:                    testAgentName,
				hubKubeconfigSecretNamespace: testNamespace,
				hubKubeconfigSecretName:      testSecretName,
				hubCSRLister:                 hubInformerFactory.Certificates().V1beta1().CertificateSigningRequests().Lister(),
				hubCSRClient:                 hubKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
				spokeSecretLister:            agentInformerFactory.Core().V1().Secrets().Lister(),
				spokeCoreClient:              agentKubeClient.CoreV1(),
			}

			if c.approvedCSRCert != nil {
				controller.csrName = testCSRName
				controller.keyData = c.approvedCSRCert.Key
				kubeconfig := testinghelpers.NewKubeconfig(c.approvedCSRCert.Key, c.approvedCSRCert.Cert)
				clientConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				controller.hubClientConfig = clientConfig
			}

			err := controller.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, ""))
			if err != nil {
				t.Errorf("unexpected error %v", err)
			}

			hasKeyData := controller.keyData != nil
			if c.keyDataExpected != hasKeyData {
				t.Error("controller.keyData should be set")
			}

			hasCSRName := controller.csrName != ""
			if c.csrNameExpected != hasCSRName {
				t.Error("controller.csrName should be set")
			}

			c.validateActions(t, hubKubeClient.Actions(), agentKubeClient.Actions())
		})
	}
}
