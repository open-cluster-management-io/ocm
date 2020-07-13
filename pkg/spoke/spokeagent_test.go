package spoke

import (
	"bytes"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"
	"github.com/open-cluster-management/registration/pkg/spoke/hubclientcert"

	"k8s.io/client-go/rest"
)

func TestComplete(t *testing.T) {
	options := NewSpokeAgentOptions()
	if err := options.Complete(); err != nil {
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

	cert := testinghelpers.NewTestCert("test", 60*time.Second)
	kubeconfig := testinghelpers.NewKubeconfig(cert.Key, cert.Cert)

	cases := []struct {
		name       string
		kubeconfig []byte
		tlsCert    []byte
		tlsKey     []byte
		isValid    bool
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
			tlsKey:     cert.Key,
			isValid:    false,
		},
		{
			name:       "valid hub client config",
			kubeconfig: kubeconfig,
			tlsKey:     cert.Key,
			tlsCert:    cert.Cert,
			isValid:    true,
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

			options := &SpokeAgentOptions{HubKubeconfigDir: tempDir}
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
				testinghelpers.WriteFile(path.Join(tempDir, hubclientcert.ClusterNameFile), []byte(c.expectedClusterName))
				testinghelpers.WriteFile(path.Join(tempDir, hubclientcert.AgentNameFile), []byte(c.expectedAgentName))
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
