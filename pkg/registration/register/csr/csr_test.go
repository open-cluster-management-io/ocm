package csr

import (
	"context"
	"crypto/x509/pkix"
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2/ktesting"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/features"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/hub/user"
	"open-cluster-management.io/ocm/pkg/registration/register"
	registertesting "open-cluster-management.io/ocm/pkg/registration/register/testing"
	"open-cluster-management.io/ocm/pkg/registration/register/token"
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
				valid, err := IsCertificateValid(logger, secret.Data[TLSCertFile], testSubject)
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
			hubKubeClient := kubefake.NewClientset(csrs...)
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
				Subject:    testSubject,
				SignerName: certificates.KubeAPIServerClientSignerName,
			}

			driver := &CSRDriver{
				csrControl:      ctrl,
				haltCSRCreation: func() bool { return false },
				csrOption:       csrOption,
				opt:             NewCSROption(),
			}

			if c.approvedCSRCert != nil {
				driver.csrName = testCSRName
				driver.keyData = c.approvedCSRCert.Key
			}

			syncCtx := testingcommon.NewFakeSyncContext(t, "test")

			secret, cond, err := driver.Process(
				context.TODO(), "test", c.secret, additionalSecretData, syncCtx.Recorder())
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

func (m *mockCSRControl) Create(
	_ context.Context, _ events.Recorder, objMeta metav1.ObjectMeta, _ []byte, _ string, _ *int32) (string, error) {
	mockCSR := &unstructured.Unstructured{}
	_, err := m.csrClient.Invokes(clienttesting.CreateActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb:     "create",
			Resource: certificates.SchemeGroupVersion.WithResource("certificatesigningrequests"),
		},
		Object: mockCSR,
	}, nil)
	return objMeta.Name + rand.String(4), err
}

func (m *mockCSRControl) IsApproved(name string) (bool, error) {
	_, err := m.csrClient.Invokes(clienttesting.GetActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb:     "get",
			Resource: certificates.SchemeGroupVersion.WithResource("certificatesigningrequests"),
		},
		Name: name,
	}, nil)

	return m.approved, err
}

func (m *mockCSRControl) GetIssuedCertificate(name string) ([]byte, error) {
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
	client := kubefake.NewClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	return informerFactory.Certificates().V1().CertificateSigningRequests().Informer()
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
			name:               "no tls key",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "/etc/ca.crt", nil, nil, nil),
			isValid:            false,
		},
		{
			name:               "no tls cert",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "/etc/ca.crt", nil, nil, nil),
			tlsKey:             cert1.Key,
			isValid:            false,
		},
		{
			name:               "cert is not issued for cluster1:agent1",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "/etc/ca.crt", nil, nil, nil),
			tlsKey:             cert2.Key,
			tlsCert:            cert2.Cert,
			isValid:            false,
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
			name:               "invalid issuer",
			clusterName:        "cluster2",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil),
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
			secretOption := register.SecretOption{
				ClusterName:       c.clusterName,
				AgentName:         c.agentName,
				HubKubeconfigDir:  tempDir,
				HubKubeconfigFile: path.Join(tempDir, "kubeconfig"),
			}
			driver, _ := NewCSRDriver(NewCSROption(), secretOption)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
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
				testinghelpers.WriteFile(path.Join(tempDir, "bootstrap-kubeconfig"), c.bootstapKubeconfig)
				if err != nil {
					t.Fatal(err)
				}
				secretOption.BootStrapKubeConfigFile = path.Join(tempDir, "bootstrap-kubeconfig")
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

func TestFilterCSREvents(t *testing.T) {
	clusterName := "cluster1"
	signerName := "signer1"
	addOnName := "addon1"

	cases := []struct {
		name     string
		csr      *certificates.CertificateSigningRequest
		expected bool
	}{
		{
			name: "csr not from the managed cluster",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "csr not for the addon",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "csr with different signer name",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "valid csr",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						// the labels are only hints. Anyone could set/modify them.
						clusterv1.ClusterNameLabelKey: clusterName,
						addonv1alpha1.AddonLabelKey:   addOnName,
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: signerName,
				},
			},
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			filterFunc := createCSREventFilterFunc(clusterName, addOnName, signerName)
			actual := filterFunc(c.csr)
			if actual != c.expected {
				t.Errorf("Expected %v but got %v", c.expected, actual)
			}
		})
	}
}

func TestIndexByClusterName(t *testing.T) {
	testcases := []struct {
		name     string
		csr      *certificates.CertificateSigningRequest
		expected []string
	}{
		{
			name:     "no index",
			csr:      &certificates.CertificateSigningRequest{},
			expected: []string{},
		},
		{
			name: "has cluster label",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{clusterv1.ClusterNameLabelKey: "cluster1"},
				},
			},
			expected: []string{"cluster1"},
		},
		{
			name: "has cluster label",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster1",
						addonv1alpha1.AddonLabelKey:   "addon1",
					},
				},
			},
			expected: []string{},
		},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := indexByClusterFunc(tt.csr)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("Expected %v but got %v", tt.expected, actual)
			}
		})
	}
}

func TestIndexByAddonFunc(t *testing.T) {
	testcases := []struct {
		name     string
		csr      *certificates.CertificateSigningRequest
		expected []string
	}{
		{
			name:     "no index",
			csr:      &certificates.CertificateSigningRequest{},
			expected: []string{},
		},
		{
			name: "has cluster label",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{clusterv1.ClusterNameLabelKey: "cluster1"},
				},
			},
			expected: []string{},
		},
		{
			name: "has cluster label",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster1",
						addonv1alpha1.AddonLabelKey:   "addon1",
					},
				},
			},
			expected: []string{"cluster1/addon1"},
		},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := indexByAddonFunc(tt.csr)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("Expected %v but got %v", tt.expected, actual)
			}
		})
	}
}

func TestNewCSRDriver(t *testing.T) {
	secretOpts := register.SecretOption{
		ClusterName: "cluster1",
		AgentName:   "agent1",
	}
	driver, err := NewCSRDriver(NewCSROption(), secretOpts)
	if err == nil {
		t.Errorf("expect error, but got nil")
	}
	secretOpts.BootStrapKubeConfigFile = "bootstrap-kubeconfig"
	driver, err = NewCSRDriver(NewCSROption(), secretOpts)
	if err != nil {
		t.Fatal(err)
	}
	if driver.csrOption.Subject.CommonName != fmt.Sprintf("%scluster1:agent1", user.SubjectPrefix) {
		t.Errorf("common name is not set correctly, got %s", driver.csrOption.Subject.CommonName)
	}
	ctrl := &mockCSRControl{}
	hubKubeClient := kubefake.NewClientset()
	ctrl.csrClient = &hubKubeClient.Fake
	driver.csrControl = ctrl

	addonSecretOptions := register.SecretOption{
		ClusterName: "cluster1",
		AgentName:   "addonagent1",
		Subject: &pkix.Name{
			CommonName: "addonagent1",
		},
	}
	regOption := &registertesting.TestAddonAuthConfig{
		KubeClientAuth: "csr",
		CSROption:      NewCSROption(),
	}
	forkedDriver, err := driver.Fork("addon1", regOption, addonSecretOptions)
	if err != nil {
		t.Fatal(err)
	}
	csrAddonDriver := forkedDriver.(*CSRDriver)
	if csrAddonDriver.csrOption.Subject.CommonName != "addonagent1" {
		t.Errorf("common name is not set correctly")
	}
}

func TestCSREventFilterFunc(t *testing.T) {
	filter := createCSREventFilterFunc("cluster1", "addon1", "signer1")
	cases := []struct {
		name     string
		csr      *certificates.CertificateSigningRequest
		expected bool
	}{
		{
			name: "incorrect cluster",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster2",
						addonv1alpha1.AddonLabelKey:   "addon1",
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: "signer1",
				},
			},
			expected: false,
		},
		{
			name: "incorrect addon",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster1",
						addonv1alpha1.AddonLabelKey:   "addon2",
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: "signer1",
				},
			},
			expected: false,
		},
		{
			name: "incorrect signer",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster1",
						addonv1alpha1.AddonLabelKey:   "addon1",
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: "signer2",
				},
			},
			expected: false,
		},
		{
			name: "all correct",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster1",
						addonv1alpha1.AddonLabelKey:   "addon1",
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: "signer1",
				},
			},
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			passed := filter(c.csr)
			if passed != c.expected {
				t.Errorf("Expected %v but got %v", c.expected, passed)
			}
		})
	}
}

func TestBuildClient(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "testvalidhubclientconfig")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	cert1 := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second)
	defer os.RemoveAll(tempDir)

	kubeconfig := testinghelpers.NewKubeconfig(
		"cluster1", "https://127.0.0.1:6001", "", "", nil, cert1.Key, cert1.Cert)

	cases := []struct {
		name                string
		kubeconfig          []byte
		bootstrapKubeconfig []byte
		bootstrap           bool
		expectErr           bool
	}{
		{
			name:       "bootstrap is not set",
			kubeconfig: kubeconfig,
			bootstrap:  true,
			expectErr:  true,
		},
		{
			name:                "bootstrap is set",
			kubeconfig:          nil,
			bootstrapKubeconfig: kubeconfig,
			bootstrap:           true,
			expectErr:           false,
		},
		{
			name:                "bootstrap is false",
			kubeconfig:          nil,
			bootstrapKubeconfig: kubeconfig,
			bootstrap:           false,
			expectErr:           true,
		},
		{
			name:                "bootstrap is false with correct kubeconfig",
			kubeconfig:          kubeconfig,
			bootstrapKubeconfig: nil,
			bootstrap:           false,
			expectErr:           false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err = features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates)
			if err != nil {
				t.Fatal(err)
			}

			secretOpts := register.SecretOption{
				ClusterName:             "cluster1",
				AgentName:               "agent1",
				BootStrapKubeConfigFile: "boostrap.yaml",
			}
			if tt.kubeconfig != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "kubeconfig"), tt.kubeconfig)
				secretOpts.HubKubeconfigFile = path.Join(tempDir, "kubeconfig")
			}
			if tt.bootstrapKubeconfig != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "bootstrap-kubeconfig"), tt.bootstrapKubeconfig)
				secretOpts.BootStrapKubeConfigFile = path.Join(tempDir, "bootstrap-kubeconfig")
			}
			driver, _ := NewCSRDriver(NewCSROption(), secretOpts)
			_, err := driver.BuildClients(context.TODO(), secretOpts, tt.bootstrap)
			if (err != nil) != tt.expectErr {
				t.Errorf("expected error %v but got %v", tt.expectErr, err)
			}
		})
	}
}

func TestCSRDriver_Fork_TokenAuth(t *testing.T) {
	// Setup addon client and informer
	addon := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "addon1",
			Namespace: "cluster1",
		},
	}
	addonClient := addonfake.NewSimpleClientset(addon)
	addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	addonInformer := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns()

	ctrl := &mockCSRControl{}
	hubKubeClient := kubefake.NewClientset()
	ctrl.csrClient = &hubKubeClient.Fake

	addonClients := &register.AddOnClients{
		AddonClient:   addonClient,
		AddonInformer: addonInformer,
	}

	tests := []struct {
		name           string
		setupDriver    func() *CSRDriver
		addonName      string
		secretOption   register.SecretOption
		regOption      register.AddonAuthConfig
		expectErr      bool
		expectErrMsg   string
		validateResult func(t *testing.T, driver register.RegisterDriver)
	}{
		{
			name: "token auth - success",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: &registertesting.MockTokenControl{},
					addonClients: addonClients,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      certificates.KubeAPIServerClientSignerName,
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "token",
				TokenOption:    token.NewTokenOption(),
			},
			expectErr: false,
			validateResult: func(t *testing.T, driver register.RegisterDriver) {
				if _, ok := driver.(*token.TokenDriver); !ok {
					t.Errorf("expected TokenDriver, got %T", driver)
				}
			},
		},
		{
			name: "token auth - invalid token option",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: &registertesting.MockTokenControl{},
					addonClients: addonClients,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      certificates.KubeAPIServerClientSignerName,
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "token",
				TokenOption:    nil,
			},
			expectErr:    true,
			expectErrMsg: "token authentication requested but TokenConfiguration is nil",
		},
		{
			name: "token auth - tokenControl not initialized",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: nil,
					addonClients: addonClients,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      certificates.KubeAPIServerClientSignerName,
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "token",
				TokenOption:    token.NewTokenOption(),
			},
			expectErr:    true,
			expectErrMsg: "tokenControl is not initialized",
		},
		{
			name: "token auth - addonClients not initialized",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: &registertesting.MockTokenControl{},
					addonClients: nil,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      certificates.KubeAPIServerClientSignerName,
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "token",
				TokenOption:    token.NewTokenOption(),
			},
			expectErr:    true,
			expectErrMsg: "addonClients is not initialized",
		},
		{
			name: "csr auth with custom signer - success",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: &registertesting.MockTokenControl{},
					addonClients: addonClients,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      "custom.signer.io/custom",
				Subject: &pkix.Name{
					CommonName: "custom-addon",
				},
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "csr",
				CSROption:      NewCSROption(),
			},
			expectErr: false,
			validateResult: func(t *testing.T, driver register.RegisterDriver) {
				if _, ok := driver.(*CSRDriver); !ok {
					t.Errorf("expected CSRDriver, got %T", driver)
				}
			},
		},
		{
			name: "csr auth - invalid CSR option",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: &registertesting.MockTokenControl{},
					addonClients: addonClients,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      certificates.KubeAPIServerClientSignerName,
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "csr",
				CSROption:      nil,
			},
			expectErr:    true,
			expectErrMsg: "CSR configuration is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver := tt.setupDriver()

			forkedDriver, err := driver.Fork(tt.addonName, tt.regOption, tt.secretOption)

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error but got nil")
					return
				}
				if tt.expectErrMsg != "" && !strings.Contains(err.Error(), tt.expectErrMsg) {
					t.Errorf("expected error message to contain %q, got %q", tt.expectErrMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.validateResult != nil {
				tt.validateResult(t, forkedDriver)
			}
		})
	}
}

func TestCSRDriver_SetTokenControl(t *testing.T) {
	driver := &CSRDriver{}
	mockControl := &registertesting.MockTokenControl{}

	driver.SetTokenControl(mockControl)

	if driver.tokenControl != mockControl {
		t.Error("SetTokenControl did not set tokenControl correctly")
	}
}

func TestCSRDriver_SetAddonClients(t *testing.T) {
	driver := &CSRDriver{}

	addon := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "addon1",
			Namespace: "cluster1",
		},
	}
	addonClient := addonfake.NewSimpleClientset(addon)
	addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	addonInformer := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns()

	addonClients := &register.AddOnClients{
		AddonClient:   addonClient,
		AddonInformer: addonInformer,
	}

	driver.SetAddonClients(addonClients)

	if driver.addonClients != addonClients {
		t.Error("SetAddonClients did not set addonClients correctly")
	}
}
