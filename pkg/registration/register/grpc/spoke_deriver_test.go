package grpc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

func TestIsHubKubeConfigValid(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "grpc-test-is-hub-kubeconfig-valid")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cert := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second)

	cases := []struct {
		name        string
		clusterName string
		agentName   string
		tlsCert     []byte
		tlsKey      []byte
		valid       bool
	}{
		{
			name:  "no cert",
			valid: false,
		},
		{
			name:    "no key",
			tlsCert: cert.Cert,
			valid:   false,
		},
		{
			name:        "cert is not issued for cluster1:agent1",
			clusterName: "cluster2",
			agentName:   "agent2",
			tlsCert:     cert.Cert,
			tlsKey:      cert.Key,
			valid:       false,
		},
		{
			name:    "valid",
			tlsCert: cert.Cert,
			tlsKey:  cert.Key,
			valid:   true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.tlsCert != nil {
				if err := os.WriteFile(filepath.Join(tempDir, "tls.crt"), c.tlsCert, 0600); err != nil {
					t.Fatal(err)
				}
			}

			if c.tlsKey != nil {
				if err := os.WriteFile(filepath.Join(tempDir, "tls.key"), c.tlsKey, 0600); err != nil {
					t.Fatal(err)
				}
			}

			secretOption := register.SecretOption{
				ClusterName:      c.clusterName,
				AgentName:        c.agentName,
				HubKubeconfigDir: tempDir,
			}
			driver, err := NewGRPCDriver(nil, csr.NewCSROption(), secretOption)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			valid, err := driver.IsHubKubeConfigValid(context.Background(), secretOption)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if valid != c.valid {
				t.Errorf("expected valid: %v, got: %v", c.valid, valid)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "grpc-test-load-config")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configFile := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte(`url: "https://localhost:8443"`), 0600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name                string
		bootstrapped        bool
		bootstrapConfigFile string
		configFile          string
		expectedErr         bool
	}{
		{
			name:         "no bootstrap config file",
			bootstrapped: true,
			expectedErr:  true,
		},
		{
			name:         "no config file",
			bootstrapped: false,
			expectedErr:  true,
		},
		{
			name:                "load bootstrap config",
			bootstrapped:        true,
			bootstrapConfigFile: configFile,
			expectedErr:         false,
		},
		{
			name:         "load bootstrap config",
			bootstrapped: false,
			configFile:   configFile,
			expectedErr:  false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			driver := &GRPCDriver{
				opt: &Option{
					BootstrapConfigFile: c.bootstrapConfigFile,
					ConfigFile:          c.configFile,
				},
			}

			config, configData, err := driver.loadConfig(register.SecretOption{}, c.bootstrapped)
			if c.expectedErr {
				if err == nil {
					t.Errorf("expected error, but failed")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if _, ok := config.(*grpc.GRPCOptions); !ok {
				t.Errorf("expected config to be a *grpc.GRPCOptions, got: %T", config)
			}

			if len(configData) == 0 {
				t.Errorf("expected config data, but got empty")
			}
		})
	}
}
