package spoke

import (
	"bytes"
	"context"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"open-cluster-management.io/registration/pkg/clientcert"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestComplete(t *testing.T) {
	// get component namespace
	var componentNamespace string
	nsBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		componentNamespace = defaultSpokeComponentNamespace
	} else {
		componentNamespace = string(nsBytes)
	}

	cases := []struct {
		name                string
		clusterName         string
		secret              *corev1.Secret
		expectedClusterName string
		expectedAgentName   string
	}{
		{
			name: "generate random cluster/agent name",
		},
		{
			name:                "specify cluster name",
			clusterName:         "cluster1",
			expectedClusterName: "cluster1",
		},
		{
			name:        "override cluster name in secret with specified value",
			clusterName: "cluster1",
			secret: testinghelpers.NewHubKubeconfigSecret(componentNamespace, "hub-kubeconfig-secret", "", nil, map[string][]byte{
				"cluster-name": []byte("cluster2"),
				"agent-name":   []byte("agent2"),
			}),
			expectedClusterName: "cluster1",
			expectedAgentName:   "agent2",
		},
		{
			name:        "override cluster name in cert with specified value",
			clusterName: "cluster1",
			secret: testinghelpers.NewHubKubeconfigSecret(componentNamespace, "hub-kubeconfig-secret", "", testinghelpers.NewTestCert("system:open-cluster-management:cluster2:agent2", 60*time.Second), map[string][]byte{
				"kubeconfig":   testinghelpers.NewKubeconfig(nil, nil),
				"cluster-name": []byte("cluster3"),
				"agent-name":   []byte("agent3"),
			}),
			expectedClusterName: "cluster1",
			expectedAgentName:   "agent2",
		},
		{
			name: "take cluster/agent name from secret",
			secret: testinghelpers.NewHubKubeconfigSecret(componentNamespace, "hub-kubeconfig-secret", "", nil, map[string][]byte{
				"cluster-name": []byte("cluster1"),
				"agent-name":   []byte("agent1"),
			}),
			expectedClusterName: "cluster1",
			expectedAgentName:   "agent1",
		},
		{
			name:                "take cluster/agent name from cert",
			secret:              testinghelpers.NewHubKubeconfigSecret(componentNamespace, "hub-kubeconfig-secret", "", testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second), map[string][]byte{}),
			expectedClusterName: "cluster1",
			expectedAgentName:   "agent1",
		},
		{
			name: "override cluster name in secret with value from cert",
			secret: testinghelpers.NewHubKubeconfigSecret(componentNamespace, "hub-kubeconfig-secret", "", testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second), map[string][]byte{
				"cluster-name": []byte("cluster2"),
				"agent-name":   []byte("agent2"),
			}),
			expectedClusterName: "cluster1",
			expectedAgentName:   "agent1",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// setup kube client
			objects := []runtime.Object{}
			if c.secret != nil {
				objects = append(objects, c.secret)
			}
			kubeClient := kubefake.NewSimpleClientset(objects...)

			// create a tmp dir to dump hub kubeconfig
			dir, err := ioutil.TempDir("", "hub-kubeconfig")
			if err != nil {
				t.Error("unable to create a tmp dir")
			}
			defer os.RemoveAll(dir)

			options := &SpokeAgentOptions{
				ClusterName:         c.clusterName,
				HubKubeconfigSecret: "hub-kubeconfig-secret",
				HubKubeconfigDir:    dir,
			}

			if err := options.Complete(kubeClient.CoreV1(), context.TODO(), eventstesting.NewTestingEventRecorder(t)); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if options.ComponentNamespace == "" {
				t.Error("component namespace should not be empty")
			}
			if options.ClusterName == "" {
				t.Error("cluster name should not be empty")
			}
			if options.AgentName == "" {
				t.Error("agent name should not be empty")
			}
			if len(c.expectedClusterName) > 0 && options.ClusterName != c.expectedClusterName {
				t.Errorf("expect cluster name %q but got %q", c.expectedClusterName, options.ClusterName)
			}
			if len(c.expectedAgentName) > 0 && options.AgentName != c.expectedAgentName {
				t.Errorf("expect agent name %q but got %q", c.expectedAgentName, options.AgentName)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	defaultCompletedOptions := NewSpokeAgentOptions()
	defaultCompletedOptions.BootstrapKubeconfig = "/spoke/bootstrap/kubeconfig"
	defaultCompletedOptions.ClusterName = "testcluster"
	defaultCompletedOptions.AgentName = "testagent"

	cases := []struct {
		name        string
		options     *SpokeAgentOptions
		expectedErr string
	}{
		{
			name:        "no bootstrap kubeconfig",
			options:     &SpokeAgentOptions{},
			expectedErr: "bootstrap-kubeconfig is required",
		},
		{
			name:        "no cluster name",
			options:     &SpokeAgentOptions{BootstrapKubeconfig: "/spoke/bootstrap/kubeconfig"},
			expectedErr: "cluster name is empty",
		},
		{
			name:        "no agent name",
			options:     &SpokeAgentOptions{BootstrapKubeconfig: "/spoke/bootstrap/kubeconfig", ClusterName: "testcluster"},
			expectedErr: "agent name is empty",
		},
		{
			name: "invalid external server URLs",
			options: &SpokeAgentOptions{
				BootstrapKubeconfig:     "/spoke/bootstrap/kubeconfig",
				ClusterName:             "testcluster",
				AgentName:               "testagent",
				SpokeExternalServerURLs: []string{"https://127.0.0.1:64433", "http://127.0.0.1:8080"},
			},
			expectedErr: "\"http://127.0.0.1:8080\" is invalid",
		},
		{
			name: "invalid cluster healthcheck period",
			options: &SpokeAgentOptions{
				BootstrapKubeconfig:      "/spoke/bootstrap/kubeconfig",
				ClusterName:              "testcluster",
				AgentName:                "testagent",
				ClusterHealthCheckPeriod: 0,
			},
			expectedErr: "cluster healthcheck period must greater than zero",
		},
		{
			name:        "default completed options",
			options:     defaultCompletedOptions,
			expectedErr: "",
		},
		{
			name: "default completed options",
			options: &SpokeAgentOptions{
				HubKubeconfigSecret:         "hub-kubeconfig-secret",
				HubKubeconfigDir:            "/spoke/hub-kubeconfig",
				ClusterHealthCheckPeriod:    1 * time.Minute,
				MaxCustomClusterClaims:      20,
				BootstrapKubeconfig:         "/spoke/bootstrap/kubeconfig",
				ClusterName:                 "testcluster",
				AgentName:                   "testagent",
				ClientCertExpirationSeconds: 599,
			},
			expectedErr: "client certificate expiration seconds must greater or qual to 600",
		},
		{
			name: "default completed options",
			options: &SpokeAgentOptions{
				HubKubeconfigSecret:         "hub-kubeconfig-secret",
				HubKubeconfigDir:            "/spoke/hub-kubeconfig",
				ClusterHealthCheckPeriod:    1 * time.Minute,
				MaxCustomClusterClaims:      20,
				BootstrapKubeconfig:         "/spoke/bootstrap/kubeconfig",
				ClusterName:                 "testcluster",
				AgentName:                   "testagent",
				ClientCertExpirationSeconds: 600,
			},
			expectedErr: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.options.Validate()
			testinghelpers.AssertError(t, err, c.expectedErr)
		})
	}
}

func TestHasValidHubClientConfig(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "testvalidhubclientconfig")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cert1 := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second)
	cert2 := testinghelpers.NewTestCert("test", 60*time.Second)

	kubeconfig := testinghelpers.NewKubeconfig(nil, nil)

	cases := []struct {
		name        string
		clusterName string
		agentName   string
		kubeconfig  []byte
		tlsCert     []byte
		tlsKey      []byte
		isValid     bool
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
			name:        "valid hub client config",
			clusterName: "cluster1",
			agentName:   "agent1",
			kubeconfig:  kubeconfig,
			tlsKey:      cert1.Key,
			tlsCert:     cert1.Cert,
			isValid:     true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.kubeconfig != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "kubeconfig"), c.kubeconfig)
			}
			if c.tlsKey != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "tls.key"), c.tlsKey)
			}
			if c.tlsCert != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "tls.crt"), c.tlsCert)
			}

			options := &SpokeAgentOptions{
				ClusterName:      c.clusterName,
				AgentName:        c.agentName,
				HubKubeconfigDir: tempDir,
			}
			valid, err := options.hasValidHubClientConfig()
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if c.isValid != valid {
				t.Errorf("expect %t, but %t", c.isValid, valid)
			}
		})
	}
}

func TestGetOrGenerateClusterAgentNames(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "testgetorgenerateclusteragentnames")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cases := []struct {
		name                string
		options             *SpokeAgentOptions
		expectedClusterName string
		expectedAgentName   string
	}{
		{
			name:                "cluster name is specified",
			options:             &SpokeAgentOptions{ClusterName: "cluster0"},
			expectedClusterName: "cluster0",
		},
		{
			name:                "cluster name and agent name are in file",
			options:             &SpokeAgentOptions{HubKubeconfigDir: tempDir},
			expectedClusterName: "cluster1",
			expectedAgentName:   "agent1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.options.HubKubeconfigDir != "" {
				testinghelpers.WriteFile(path.Join(tempDir, clientcert.ClusterNameFile), []byte(c.expectedClusterName))
				testinghelpers.WriteFile(path.Join(tempDir, clientcert.AgentNameFile), []byte(c.expectedAgentName))
			}
			clusterName, agentName := c.options.getOrGenerateClusterAgentNames()
			if clusterName != c.expectedClusterName {
				t.Errorf("expect cluster name %q but got %q", c.expectedClusterName, clusterName)
			}

			// agent name cannot be empty, it is either generated or from file
			if agentName == "" {
				t.Error("agent name should not be empty")
			}

			if c.expectedAgentName != "" && c.expectedAgentName != agentName {
				t.Errorf("expect agent name %q but got %q", c.expectedAgentName, agentName)
			}
		})
	}
}

func TestGetSpokeClusterCABundle(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "testgetspokeclustercabundle")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cases := []struct {
		name           string
		caFile         string
		options        *SpokeAgentOptions
		expectedErr    string
		expectedCAData []byte
	}{
		{
			name:           "no external server URLs",
			options:        &SpokeAgentOptions{},
			expectedErr:    "",
			expectedCAData: nil,
		},
		{
			name:           "no ca data",
			options:        &SpokeAgentOptions{SpokeExternalServerURLs: []string{"https://127.0.0.1:6443"}},
			expectedErr:    "open : no such file or directory",
			expectedCAData: nil,
		},
		{
			name:           "has ca data",
			options:        &SpokeAgentOptions{SpokeExternalServerURLs: []string{"https://127.0.0.1:6443"}},
			expectedErr:    "",
			expectedCAData: []byte("cadata"),
		},
		{
			name:           "has ca file",
			caFile:         "ca.data",
			options:        &SpokeAgentOptions{SpokeExternalServerURLs: []string{"https://127.0.0.1:6443"}},
			expectedErr:    "",
			expectedCAData: []byte("cadata"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			restConig := &rest.Config{}
			if c.expectedCAData != nil {
				restConig.CAData = c.expectedCAData
			}
			if c.caFile != "" {
				testinghelpers.WriteFile(path.Join(tempDir, c.caFile), c.expectedCAData)
				restConig.CAData = nil
				restConig.CAFile = path.Join(tempDir, c.caFile)
			}
			caData, err := c.options.getSpokeClusterCABundle(restConig)
			testinghelpers.AssertError(t, err, c.expectedErr)
			if c.expectedCAData == nil && caData == nil {
				return
			}
			if !bytes.Equal(caData, c.expectedCAData) {
				t.Errorf("expect %v but got %v", c.expectedCAData, caData)
			}
		})
	}
}
