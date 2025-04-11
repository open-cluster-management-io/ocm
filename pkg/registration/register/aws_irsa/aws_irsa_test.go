package aws_irsa

import (
	"context"
	"os"
	"path"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

const (
	testNamespace  = "testns"
	testAgentName  = "testagent"
	testSecretName = "testsecret"
	testIrsaName   = "testirsa"
)

// var commonName = fmt.Sprintf("%s%s:%s", user.SubjectPrefix, testinghelpers.TestManagedClusterName, testAgentName)

func TestProcess(t *testing.T) {
	cases := []struct {
		name                string
		queueKey            string
		secret              *corev1.Secret
		approvedIrsaRequest *testinghelpers.TestIrsaRequest
		keyDataExpected     bool
		irsaNameExpected    bool
		expectedCondition   *metav1.Condition
		validateActions     func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret)
	}{
		{},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctrl := &mockAWSIRSAControl{}
			var irsas []runtime.Object

			hubKubeClient := kubefake.NewSimpleClientset(irsas...)

			additionalSecretData := map[string][]byte{
				register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
				register.AgentNameFile:   []byte(testAgentName),
			}

			awsOption := &AWSOption{}

			driver := &AWSIRSADriver{
				awsIRSAControl: ctrl,
			}

			if c.approvedIrsaRequest != nil {
				driver.name = testIrsaName
			}

			syncCtx := testingcommon.NewFakeSyncContext(t, "test")

			klog.Info(hubKubeClient, additionalSecretData, awsOption, syncCtx)

		})
	}
}

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
