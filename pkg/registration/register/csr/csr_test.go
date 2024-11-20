package csr

import (
	"context"
	"crypto/x509/pkix"
	"fmt"
	"os"
	"path"
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
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2/ktesting"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/hub/user"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

const (
	testNamespace  = "testns"
	testAgentName  = "testagent"
	testSecretName = "testsecret"
	testCSRName    = "testcsr"
)

var commonName = fmt.Sprintf("%s%s:%s", user.SubjectPrefix, testinghelpers.TestManagedClusterName, testAgentName)

func TestProcess(t *testing.T) {
	testSubject := &pkix.Name{
		CommonName: commonName,
	}

	cases := []struct {
		name              string
		queueKey          string
		secret            *corev1.Secret
		approvedCSRCert   *testinghelpers.TestCert
		keyDataExpected   bool
		csrNameExpected   bool
		expectedCondition *metav1.Condition
		validateActions   func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret)
	}{
		{
			name:     "syc csr after bootstrap",
			queueKey: testSecretName,
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "1", nil,
				map[string][]byte{
					register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					register.AgentNameFile:   []byte(testAgentName),
				}),
			expectedCondition: &metav1.Condition{
				Type:   ClusterCertificateRotatedCondition,
				Status: metav1.ConditionTrue,
			},
			approvedCSRCert: testinghelpers.NewTestCert(commonName, 10*time.Second),
			validateActions: func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret) {
				logger, _ := ktesting.NewTestContext(t)
				testingcommon.AssertActions(t, hubActions, "get", "get")
				valid, err := isCertificateValid(logger, secret.Data[TLSCertFile], testSubject)
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
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "1",
				testinghelpers.NewTestCert(commonName, 10000*time.Second), map[string][]byte{
					register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					register.AgentNameFile:   []byte(testAgentName),
					register.KubeconfigFile: testinghelpers.NewKubeconfig(
						"c1", "https://127.0.0.1:6001", "", "", nil, nil, nil),
				}),
			validateActions: func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret) {
				testingcommon.AssertNoActions(t, hubActions)
				if secret != nil {
					t.Errorf("expect the secret not to be generated")
				}
			},
		},
		{
			name:     "sync an expiring hub kubeconfig secret",
			queueKey: testSecretName,
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "1",
				testinghelpers.NewTestCert(commonName, -3*time.Second),
				map[string][]byte{
					register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					register.AgentNameFile:   []byte(testAgentName),
					register.KubeconfigFile: testinghelpers.NewKubeconfig(
						"c1", "https://127.0.0.1:6001", "", "", nil, nil, nil),
				}),
			keyDataExpected: true,
			csrNameExpected: true,
			validateActions: func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret) {
				testingcommon.AssertActions(t, hubActions, "create")
				actual := hubActions[0].(clienttesting.CreateActionImpl).Object
				if _, ok := actual.(*unstructured.Unstructured); !ok {
					t.Errorf("expected csr was created, but failed")
				}
			},
		},
		{
			name:     "sync when additional secret data changes",
			queueKey: testSecretName,
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "1",
				testinghelpers.NewTestCert(commonName, 10000*time.Second),
				map[string][]byte{
					register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					register.AgentNameFile:   []byte("invalid-name"),
				}),
			keyDataExpected: true,
			csrNameExpected: true,
			validateActions: func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret) {
				testingcommon.AssertActions(t, hubActions, "create")
				actual := hubActions[0].(clienttesting.CreateActionImpl).Object
				if _, ok := actual.(*unstructured.Unstructured); !ok {
					t.Errorf("expected csr was created, but failed")
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctrl := &mockCSRControl{}
			var csrs []runtime.Object
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

			additionalSecretData := map[string][]byte{
				register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
				register.AgentNameFile:   []byte(testAgentName),
			}
			csrOption := &CSROption{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "test-",
				},
				Subject:         testSubject,
				SignerName:      certificates.KubeAPIServerClientSignerName,
				HaltCSRCreation: func() bool { return false },
				CSRControl:      ctrl,
			}

			driver := &CSRDriver{}

			if c.approvedCSRCert != nil {
				driver.csrName = testCSRName
				driver.keyData = c.approvedCSRCert.Key
			}

			syncCtx := testingcommon.NewFakeSyncContext(t, "test")

			secret, cond, err := driver.Process(
				context.TODO(), "test", c.secret, additionalSecretData, syncCtx.Recorder(), csrOption)
			if err != nil {
				t.Errorf("unexpected error %v", err)
			}

			hasKeyData := driver.keyData != nil
			if c.keyDataExpected != hasKeyData {
				t.Error("controller.keyData should be set")
			}

			hasCSRName := driver.csrName != ""
			if c.csrNameExpected != hasCSRName {
				t.Error("controller.csrName should be set")
			}

			if !conditionEqual(c.expectedCondition, cond) {
				t.Errorf("condition is not correct, expected %v, got %v", c.expectedCondition, cond)
			}

			c.validateActions(t, hubKubeClient.Actions(), secret)
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

type mockCSRControl struct {
	approved       bool
	issuedCertData []byte
	csrClient      *clienttesting.Fake
}

func (m *mockCSRControl) create(
	_ context.Context, _ events.Recorder, objMeta metav1.ObjectMeta, _ []byte, _ string, _ *int32) (string, error) {
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

func TestIsHubKubeConfigValidFunc(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "testvalidhubclientconfig")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cert1 := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second)
	cert2 := testinghelpers.NewTestCert("test", 60*time.Second)

	kubeconfig := testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil)

	cases := []struct {
		name               string
		clusterName        string
		agentName          string
		kubeconfig         []byte
		bootstapKubeconfig []byte
		tlsCert            []byte
		tlsKey             []byte
		isValid            bool
	}{
		{
			name:    "no kubeconfig",
			isValid: false,
		},
		{
			name:       "no tls key",
			kubeconfig: kubeconfig,
			isValid:    false,
		},
		{
			name:       "no tls cert",
			kubeconfig: kubeconfig,
			tlsKey:     cert1.Key,
			isValid:    false,
		},
		{
			name:        "cert is not issued for cluster1:agent1",
			clusterName: "cluster1",
			agentName:   "agent1",
			kubeconfig:  kubeconfig,
			tlsKey:      cert2.Key,
			tlsCert:     cert2.Cert,
			isValid:     false,
		},
		{
			name:               "context cluster changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c2", "https://127.0.0.1:6001", "", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "hub server url changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.2:6001", "", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "proxy url changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "https://127.0.0.1:3129", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "ca bundle changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", []byte("test"), nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "ca changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "/etc/ca.crt", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "valid hub client config",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			driver := NewCSRDriver()
			secretOption := register.SecretOption{
				ClusterName:       c.clusterName,
				AgentName:         c.agentName,
				HubKubeconfigDir:  tempDir,
				HubKubeconfigFile: path.Join(tempDir, "kubeconfig"),
			}
			if c.kubeconfig != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "kubeconfig"), c.kubeconfig)
			}
			if c.tlsKey != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "tls.key"), c.tlsKey)
			}
			if c.tlsCert != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "tls.crt"), c.tlsCert)
			}
			if c.bootstapKubeconfig != nil {
				bootstrapKubeconfig, err := clientcmd.Load(c.bootstapKubeconfig)
				if err != nil {
					t.Fatal(err)
				}
				secretOption.BootStrapKubeConfig = bootstrapKubeconfig
			}

			valid, err := register.IsHubKubeConfigValidFunc(driver, secretOption)(context.TODO())
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if c.isValid != valid {
				t.Errorf("expect %t, but %t", c.isValid, valid)
			}
		})
	}
}
