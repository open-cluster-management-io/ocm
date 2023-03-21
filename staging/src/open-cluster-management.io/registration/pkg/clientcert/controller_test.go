package clientcert

import (
	"context"
	"crypto/x509/pkix"
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events"
	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
	"open-cluster-management.io/registration/pkg/hub/user"
)

const (
	testNamespace  = "testns"
	testAgentName  = "testagent"
	testSecretName = "testsecret"
	testCSRName    = "testcsr"
)

var commonName = fmt.Sprintf("%s%s:%s", user.SubjectPrefix, testinghelpers.TestManagedClusterName, testAgentName)

func TestSync(t *testing.T) {
	testSubject := &pkix.Name{
		CommonName: commonName,
	}

	cases := []struct {
		name                         string
		queueKey                     string
		secrets                      []runtime.Object
		approvedCSRCert              *testinghelpers.TestCert
		keyDataExpected              bool
		csrNameExpected              bool
		additonalSecretDataSensitive bool
		expectedCondition            *metav1.Condition
		validateActions              func(t *testing.T, hubActions, agentActions []clienttesting.Action)
	}{
		{
			name:            "agent bootstrap",
			secrets:         []runtime.Object{},
			queueKey:        "key",
			keyDataExpected: true,
			csrNameExpected: true,
			validateActions: func(t *testing.T, hubActions, agentActions []clienttesting.Action) {
				testinghelpers.AssertActions(t, hubActions, "create")
				actual := hubActions[0].(clienttesting.CreateActionImpl).Object
				if _, ok := actual.(*unstructured.Unstructured); !ok {
					t.Errorf("expected csr was created, but failed")
				}
				testinghelpers.AssertActions(t, agentActions, "get")
			},
		},
		{
			name:     "syc csr after bootstrap",
			queueKey: testSecretName,
			secrets: []runtime.Object{
				testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "1", nil, map[string][]byte{
					ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					AgentNameFile:   []byte(testAgentName),
				},
				),
			},
			expectedCondition: &metav1.Condition{
				Type:   ClusterCertificateRotatedCondition,
				Status: metav1.ConditionTrue,
			},
			approvedCSRCert: testinghelpers.NewTestCert(commonName, 10*time.Second),
			validateActions: func(t *testing.T, hubActions, agentActions []clienttesting.Action) {
				testinghelpers.AssertActions(t, hubActions, "get", "get")
				testinghelpers.AssertActions(t, agentActions, "get", "update")
				actual := agentActions[1].(clienttesting.UpdateActionImpl).Object
				secret := actual.(*corev1.Secret)
				valid, err := IsCertificateValid(secret.Data[TLSCertFile], testSubject)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !valid {
					t.Error("client certificate is invalid")
				}
			},
		},
		{
			name:     "sync a valid hub kubeconfig secret",
			queueKey: testSecretName,
			secrets: []runtime.Object{
				testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "1", testinghelpers.NewTestCert(commonName, 10000*time.Second), map[string][]byte{
					ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					AgentNameFile:   []byte(testAgentName),
					KubeconfigFile:  testinghelpers.NewKubeconfig(nil, nil),
				}),
			},
			validateActions: func(t *testing.T, hubActions, agentActions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, hubActions)
				testinghelpers.AssertActions(t, agentActions, "get")
			},
		},
		{
			name:     "sync an expiring hub kubeconfig secret",
			queueKey: testSecretName,
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
				if _, ok := actual.(*unstructured.Unstructured); !ok {
					t.Errorf("expected csr was created, but failed")
				}
				testinghelpers.AssertActions(t, agentActions, "get")
			},
		},
		{
			name:     "sync when additional secret data changes",
			queueKey: testSecretName,
			secrets: []runtime.Object{
				testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "1", testinghelpers.NewTestCert(commonName, 10000*time.Second), map[string][]byte{
					ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					AgentNameFile:   []byte("invalid-name"),
				}),
			},
			keyDataExpected:              true,
			csrNameExpected:              true,
			additonalSecretDataSensitive: true,
			validateActions: func(t *testing.T, hubActions, agentActions []clienttesting.Action) {
				testinghelpers.AssertActions(t, hubActions, "create")
				actual := hubActions[0].(clienttesting.CreateActionImpl).Object
				if _, ok := actual.(*unstructured.Unstructured); !ok {
					t.Errorf("expected csr was created, but failed")
				}
				testinghelpers.AssertActions(t, agentActions, "get")
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctrl := &mockCSRControl{}
			csrs := []runtime.Object{}
			if c.approvedCSRCert != nil {
				csr := testinghelpers.NewApprovedCSR(testinghelpers.CSRHolder{Name: testCSRName})
				csr.Status.Certificate = c.approvedCSRCert.Cert
				csrs = append(csrs, csr)
				ctrl.approved = true
				ctrl.issuedCertData = c.approvedCSRCert.Cert
			}
			hubKubeClient := kubefake.NewSimpleClientset(csrs...)
			ctrl.csrClient = &hubKubeClient.Fake

			// GenerateName is not working for fake clent, we set the name with prepend reactor
			hubKubeClient.PrependReactor(
				"create",
				"certificatesigningrequests",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, testinghelpers.NewCSR(testinghelpers.CSRHolder{Name: testCSRName}), nil
				},
			)
			agentKubeClient := kubefake.NewSimpleClientset(c.secrets...)

			clientCertOption := ClientCertOption{
				SecretNamespace: testNamespace,
				SecretName:      testSecretName,
				AdditionalSecretData: map[string][]byte{
					ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					AgentNameFile:   []byte(testAgentName),
				},
				AdditionalSecretDataSensitive: c.additonalSecretDataSensitive,
			}
			csrOption := CSROption{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "test-",
				},
				Subject:         testSubject,
				SignerName:      certificates.KubeAPIServerClientSignerName,
				HaltCSRCreation: func() bool { return false },
			}

			updater := &fakeStatusUpdater{}

			controller := &clientCertificateController{
				ClientCertOption:     clientCertOption,
				CSROption:            csrOption,
				csrControl:           ctrl,
				managementCoreClient: agentKubeClient.CoreV1(),
				controllerName:       "test-agent",
				statusUpdater:        updater.update,
			}

			if c.approvedCSRCert != nil {
				controller.csrName = testCSRName
				controller.keyData = c.approvedCSRCert.Key
			}

			err := controller.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, c.queueKey))
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

			if !conditionEqual(c.expectedCondition, updater.cond) {
				t.Errorf("conditon is not correct, expected %v, got %v", c.expectedCondition, updater.cond)
			}

			c.validateActions(t, hubKubeClient.Actions(), agentKubeClient.Actions())
		})
	}
}

var _ CSRControl = &mockCSRControl{}

func conditionEqual(expected, actual *metav1.Condition) bool {
	if expected == nil && actual == nil {
		return true
	}

	if expected == nil || actual == nil {
		return false
	}

	if expected.Type != actual.Type {
		return false
	}

	if string(expected.Status) != string(actual.Status) {
		return false
	}

	return true
}

type fakeStatusUpdater struct {
	cond *metav1.Condition
}

func (f *fakeStatusUpdater) update(ctx context.Context, cond metav1.Condition) error {
	f.cond = cond.DeepCopy()
	return nil
}

type mockCSRControl struct {
	approved       bool
	issuedCertData []byte
	csrClient      *clienttesting.Fake
}

func (m *mockCSRControl) create(ctx context.Context, recorder events.Recorder, objMeta metav1.ObjectMeta, csrData []byte, signerName string) (string, error) {
	mockCSR := &unstructured.Unstructured{}
	_, err := m.csrClient.Invokes(clienttesting.CreateActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb: "create",
		},
		Object: mockCSR,
	}, nil)
	return objMeta.Name + rand.String(4), err
}

func (m *mockCSRControl) isApproved(name string) (bool, error) {
	_, err := m.csrClient.Invokes(clienttesting.GetActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb:     "get",
			Resource: certificates.SchemeGroupVersion.WithResource("certificatesigningrequests"),
		},
		Name: name,
	}, nil)

	return m.approved, err
}

func (m *mockCSRControl) getIssuedCertificate(name string) ([]byte, error) {
	_, err := m.csrClient.Invokes(clienttesting.GetActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb:     "get",
			Resource: certificates.SchemeGroupVersion.WithResource("certificatesigningrequests"),
		},
		Name: name,
	}, nil)
	return m.issuedCertData, err
}

func (m *mockCSRControl) Informer() cache.SharedIndexInformer {
	panic("implement me")
}
