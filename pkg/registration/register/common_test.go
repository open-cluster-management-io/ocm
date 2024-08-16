package register

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestBaseKubeConfigFromBootStrap(t *testing.T) {
	server1 := "https://127.0.0.1:6443"
	server2 := "https://api.cluster1.example.com:6443"
	caData1 := []byte("fake-ca-data1")
	caData2 := []byte("fake-ca-data2")
	proxyURL := "https://127.0.0.1:3129"

	cases := []struct {
		name             string
		kubeconfig       *clientcmdapi.Config
		expectedServer   string
		expectedCAData   []byte
		expectedProxyURL string
	}{
		{
			name: "without proxy url",
			kubeconfig: &clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"test-cluster": {
						Server:                   server1,
						CertificateAuthorityData: caData1,
					}},
				// Define a context that connects the auth info and cluster, and set it as the default
				Contexts: map[string]*clientcmdapi.Context{DefaultKubeConfigContext: {
					Cluster:   "test-cluster",
					AuthInfo:  DefaultKubeConfigAuth,
					Namespace: "configuration",
				}},
				CurrentContext: DefaultKubeConfigContext,
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					DefaultKubeConfigAuth: {
						ClientCertificate: "tls.crt",
						ClientKey:         "tls.key",
					},
				},
			},
			expectedServer:   server1,
			expectedCAData:   caData1,
			expectedProxyURL: "",
		},
		{
			name: "with proxy url",
			kubeconfig: &clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"test-cluster": {
						Server:                   server2,
						CertificateAuthorityData: caData2,
						ProxyURL:                 proxyURL,
					}},
				// Define a context that connects the auth info and cluster, and set it as the default
				Contexts: map[string]*clientcmdapi.Context{DefaultKubeConfigContext: {
					Cluster:   "test-cluster",
					AuthInfo:  DefaultKubeConfigAuth,
					Namespace: "configuration",
				}},
				CurrentContext: DefaultKubeConfigContext,
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					DefaultKubeConfigAuth: {
						ClientCertificate: "tls.crt",
						ClientKey:         "tls.key",
					},
				},
			},
			expectedServer:   server2,
			expectedCAData:   caData2,
			expectedProxyURL: proxyURL,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeConfig, err := BaseKubeConfigFromBootStrap(c.kubeconfig)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			cluster := kubeConfig.Contexts[DefaultKubeConfigContext].Cluster

			if cluster != "test-cluster" {
				t.Errorf("expect context cluster %s, but %s", "test-cluster",
					kubeConfig.Contexts[DefaultKubeConfigContext].Cluster)
			}

			if c.expectedServer != kubeConfig.Clusters[cluster].Server {
				t.Errorf("expect server %s, but %s", c.expectedServer, kubeConfig.Clusters[cluster].Server)
			}

			if c.expectedProxyURL != kubeConfig.Clusters[cluster].ProxyURL {
				t.Errorf("expect proxy url %s, but %s", c.expectedProxyURL, proxyURL)
			}

			if !reflect.DeepEqual(c.expectedCAData, kubeConfig.Clusters[cluster].CertificateAuthorityData) {
				t.Errorf("expect ca data %v, but %v", c.expectedCAData, kubeConfig.Clusters[cluster].CertificateAuthorityData)
			}
		})
	}
}

type testApprover struct {
	cleanupErr error
}

func newTestApprover(err error) Approver {
	return &testApprover{cleanupErr: err}
}

func (t *testApprover) Run(_ context.Context, _ int) {}

func (t *testApprover) Cleanup(_ context.Context, _ *clusterv1.ManagedCluster) error {
	return t.cleanupErr
}

func TestAggregateApprover(t *testing.T) {
	cases := []struct {
		name      string
		approvers []Approver
		expectErr bool
	}{
		{
			name:      "noop",
			approvers: []Approver{NewNoopApprover()},
		},
		{
			name:      "two approvers, one with err",
			approvers: []Approver{NewNoopApprover(), newTestApprover(fmt.Errorf("error test"))},
			expectErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			aggregated := NewAggregatedApprover(c.approvers...)
			err := aggregated.Cleanup(context.Background(), testinghelpers.NewManagedCluster())
			if err != nil && !c.expectErr {
				t.Errorf("should have no err but got %v", err)
			}
			if err == nil && c.expectErr {
				t.Errorf("should have err")
			}
		})
	}
}

func TestIsHubKubeConfigValidFunc(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "testvalidhubclientconfig")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cert1 := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second)

	kubeconfig := testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", nil, nil, nil)

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
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c2", "https://127.0.0.1:6001", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "hub server url changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.2:6001", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "proxy url changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "https://127.0.0.1:3129", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "ca bundle changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", []byte("test"), nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "valid hub client config",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var bootstrapKubeconfig, kubeConfig *clientcmdapi.Config
			if c.bootstapKubeconfig != nil {
				bootstrapKubeconfig, err = clientcmd.Load(c.bootstapKubeconfig)
				if err != nil {
					t.Fatal(err)
				}
			}

			if c.kubeconfig != nil {
				kubeConfig, err = clientcmd.Load(c.kubeconfig)
				if err != nil {
					t.Fatal(err)
				}
			}

			valid, err := IsHubKubeconfigValid(bootstrapKubeconfig, kubeConfig)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if c.isValid != valid {
				t.Errorf("expect %t, but %t", c.isValid, valid)
			}
		})
	}
}
