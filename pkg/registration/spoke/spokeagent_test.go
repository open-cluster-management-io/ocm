package spoke

import (
	"bytes"
	"context"
	"os"
	"path"
	"testing"
	"time"

	"k8s.io/client-go/rest"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestValidate(t *testing.T) {
	defaultCompletedOptions := NewSpokeAgentOptions()
	defaultCompletedOptions.BootstrapKubeconfig = "/spoke/bootstrap/kubeconfig"

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
			name: "invalid external server URLs",
			options: &SpokeAgentOptions{
				BootstrapKubeconfig:     "/spoke/bootstrap/kubeconfig",
				SpokeExternalServerURLs: []string{"https://127.0.0.1:64433", "http://127.0.0.1:8080"},
			},
			expectedErr: "\"http://127.0.0.1:8080\" is invalid",
		},
		{
			name: "invalid cluster healthcheck period",
			options: &SpokeAgentOptions{
				BootstrapKubeconfig:      "/spoke/bootstrap/kubeconfig",
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
				ClusterHealthCheckPeriod:    1 * time.Minute,
				MaxCustomClusterClaims:      20,
				BootstrapKubeconfig:         "/spoke/bootstrap/kubeconfig",
				ClientCertExpirationSeconds: 3599,
			},
			expectedErr: "client certificate expiration seconds must greater or qual to 3600",
		},
		{
			name: "default completed options",
			options: &SpokeAgentOptions{
				HubKubeconfigSecret:         "hub-kubeconfig-secret",
				ClusterHealthCheckPeriod:    1 * time.Minute,
				MaxCustomClusterClaims:      20,
				BootstrapKubeconfig:         "/spoke/bootstrap/kubeconfig",
				ClientCertExpirationSeconds: 3600,
			},
			expectedErr: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.options.Validate()
			testingcommon.AssertError(t, err, c.expectedErr)
		})
	}
}

func TestHasValidHubClientConfig(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "testvalidhubclientconfig")
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

			agentOpts := &commonoptions.AgentOptions{
				SpokeClusterName: c.clusterName,
				AgentID:          c.agentName,
				HubKubeconfigDir: tempDir,
			}
			cfg := NewSpokeAgentConfig(agentOpts, NewSpokeAgentOptions())
			if err := agentOpts.Complete(); err != nil {
				t.Fatal(err)
			}
			valid, err := cfg.HasValidHubClientConfig(context.TODO())
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if c.isValid != valid {
				t.Errorf("expect %t, but %t", c.isValid, valid)
			}
		})
	}
}

func TestGetSpokeClusterCABundle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "testgetspokeclustercabundle")
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
			cfg := NewSpokeAgentConfig(commonoptions.NewAgentOptions(), c.options)
			caData, err := cfg.getSpokeClusterCABundle(restConig)
			testingcommon.AssertError(t, err, c.expectedErr)
			if c.expectedCAData == nil && caData == nil {
				return
			}
			if !bytes.Equal(caData, c.expectedCAData) {
				t.Errorf("expect %v but got %v", c.expectedCAData, caData)
			}
		})
	}
}
