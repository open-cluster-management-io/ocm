package aws_irsa

import (
	"context"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	registertesting "open-cluster-management.io/ocm/pkg/registration/register/testing"
	"open-cluster-management.io/ocm/pkg/registration/register/token"
)

var _ AWSIRSAControl = &mockAWSIRSAControl{}

//TODO: Uncomment the below once required in the aws irsa authentication implementation
/*
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
func (m *mockAWSIRSAControl) create(
	_ context.Context, _ events.Recorder, objMeta metav1.ObjectMeta, _ []byte, _ string, _ *int32) (string, error) {
	mockIrsa := &unstructured.Unstructured{}
	_, err := m.awsIrsaClient.Invokes(clienttesting.CreateActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb: "create",
		},
		Object: mockIrsa,
	}, nil)
	return objMeta.Name + rand.String(4), err
}
*/
type mockAWSIRSAControl struct {
	approved      bool
	eksKubeConfig []byte
	awsIrsaClient *clienttesting.Fake
}

func (m *mockAWSIRSAControl) isApproved(name string) (bool, error) {
	_, err := m.awsIrsaClient.Invokes(clienttesting.GetActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb: "get",
			//Resource: schema.GroupVersionResource.GroupVersion().WithResource("aws"),
		},
		Name: name,
	}, nil)

	return m.approved, err
}

func (m *mockAWSIRSAControl) generateEKSKubeConfig(name string) ([]byte, error) {
	_, err := m.awsIrsaClient.Invokes(clienttesting.GetActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb: "get",
			//Resource: certificates.SchemeGroupVersion.WithResource("certificatesigningrequests"),
		},
		Name: name,
	}, nil)
	return m.eksKubeConfig, err
}

func (m *mockAWSIRSAControl) Informer() cache.SharedIndexInformer {
	panic("implement me")
}

func TestIsHubKubeConfigValidFunc(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "testvalidhubclientconfig")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cert1 := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second)
	// cert2 := testinghelpers.NewTestCert("test", 60*time.Second)

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
			secretOption := register.SecretOption{
				ClusterName:       c.clusterName,
				AgentName:         c.agentName,
				HubKubeconfigDir:  tempDir,
				HubKubeconfigFile: path.Join(tempDir, "kubeconfig"),
			}
			driver := NewAWSIRSADriver(NewAWSOption(), secretOption)
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

func TestAWSIRSADriver_Fork_TokenAuth(t *testing.T) {
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

	addonClients := &register.AddOnClients{
		AddonClient:   addonClient,
		AddonInformer: addonInformer,
	}

	// Setup mock AWS IRSA control
	mockAWSIRSACtrl := &mockAWSIRSAControl{}

	tests := []struct {
		name           string
		setupDriver    func() *AWSIRSADriver
		addonName      string
		secretOption   register.SecretOption
		regOption      register.AddonAuthConfig
		expectErr      bool
		expectErrMsg   string
		validateResult func(t *testing.T, driver register.RegisterDriver)
	}{
		{
			name: "token auth - success",
			setupDriver: func() *AWSIRSADriver {
				driver := &AWSIRSADriver{
					tokenControl:   &registertesting.MockTokenControl{},
					addonClients:   addonClients,
					awsIRSAControl: mockAWSIRSACtrl,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      certificatesv1.KubeAPIServerClientSignerName,
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
			setupDriver: func() *AWSIRSADriver {
				driver := &AWSIRSADriver{
					tokenControl:   &registertesting.MockTokenControl{},
					addonClients:   addonClients,
					awsIRSAControl: mockAWSIRSACtrl,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      certificatesv1.KubeAPIServerClientSignerName,
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
			setupDriver: func() *AWSIRSADriver {
				driver := &AWSIRSADriver{
					tokenControl:   nil,
					addonClients:   addonClients,
					awsIRSAControl: mockAWSIRSACtrl,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      certificatesv1.KubeAPIServerClientSignerName,
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
			setupDriver: func() *AWSIRSADriver {
				driver := &AWSIRSADriver{
					tokenControl:   &registertesting.MockTokenControl{},
					addonClients:   nil,
					awsIRSAControl: mockAWSIRSACtrl,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      certificatesv1.KubeAPIServerClientSignerName,
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "token",
				TokenOption:    token.NewTokenOption(),
			},
			expectErr:    true,
			expectErrMsg: "addonClients is not initialized",
		},
		{
			name: "csr auth with KubeAPIServerClientSigner - success",
			setupDriver: func() *AWSIRSADriver {
				driver := &AWSIRSADriver{
					tokenControl:   &registertesting.MockTokenControl{},
					csrControl:     newMockCSRControl(),
					addonClients:   addonClients,
					awsIRSAControl: mockAWSIRSACtrl,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      certificatesv1.KubeAPIServerClientSignerName,
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "csr",
				CSROption:      csr.NewCSROption(),
			},
			expectErr: false,
			validateResult: func(t *testing.T, driver register.RegisterDriver) {
				if _, ok := driver.(*csr.CSRDriver); !ok {
					t.Errorf("expected CSRDriver, got %T", driver)
				}
			},
		},
		{
			name: "csr auth with custom signer - success",
			setupDriver: func() *AWSIRSADriver {
				driver := &AWSIRSADriver{
					tokenControl:   &registertesting.MockTokenControl{},
					csrControl:     newMockCSRControl(),
					addonClients:   addonClients,
					awsIRSAControl: mockAWSIRSACtrl,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      "custom.signer.io/custom",
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "csr",
				CSROption:      csr.NewCSROption(),
			},
			expectErr: false,
			validateResult: func(t *testing.T, driver register.RegisterDriver) {
				if _, ok := driver.(*csr.CSRDriver); !ok {
					t.Errorf("expected CSRDriver, got %T", driver)
				}
			},
		},
		{
			name: "csr auth - invalid CSR option",
			setupDriver: func() *AWSIRSADriver {
				driver := &AWSIRSADriver{
					tokenControl:   &registertesting.MockTokenControl{},
					csrControl:     newMockCSRControl(),
					addonClients:   addonClients,
					awsIRSAControl: mockAWSIRSACtrl,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				Signer:      certificatesv1.KubeAPIServerClientSignerName,
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

// mockCSRControl is a simple mock for testing CSR-based registration
type mockCSRControl struct {
	informer cache.SharedIndexInformer
}

func (m *mockCSRControl) Create(ctx context.Context, recorder events.Recorder, objMeta metav1.ObjectMeta, csrData []byte, signerName string, expirationSeconds *int32) (string, error) {
	return "mock-csr", nil
}

func (m *mockCSRControl) IsApproved(name string) (bool, error) {
	return true, nil
}

func (m *mockCSRControl) GetIssuedCertificate(name string) ([]byte, error) {
	return []byte("mock-cert"), nil
}

func (m *mockCSRControl) Informer() cache.SharedIndexInformer {
	return m.informer
}

func newMockCSRControl() *mockCSRControl {
	// Create a fake client and informer
	return &mockCSRControl{
		informer: cache.NewSharedIndexInformer(
			nil,
			nil,
			0,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		),
	}
}
