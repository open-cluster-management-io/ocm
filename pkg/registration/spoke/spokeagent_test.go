package spoke

import (
	"bytes"
	"os"
	"path"
	"testing"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"

	ocmfeature "open-cluster-management.io/api/feature"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/features"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func init() {
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates))
}

func TestValidate(t *testing.T) {
	defaultCompletedOptions := NewSpokeAgentOptions()
	defaultCompletedOptions.BootstrapKubeconfig = "/spoke/bootstrap/kubeconfig"

	cases := []struct {
		name        string
		options     *SpokeAgentOptions
		pre         func()
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
		{
			name: "MultipleHubs enabled, but bootstrapkubeconfigs is empty",
			options: &SpokeAgentOptions{
				HubKubeconfigSecret:         "hub-kubeconfig-secret",
				ClusterHealthCheckPeriod:    1 * time.Minute,
				MaxCustomClusterClaims:      20,
				BootstrapKubeconfigs:        []string{},
				ClientCertExpirationSeconds: 3600,
			},
			pre: func() {
				_ = features.SpokeMutableFeatureGate.SetFromMap(map[string]bool{
					string(ocmfeature.MultipleHubs): true,
				})
			},
			expectedErr: "expect at least 2 bootstrap kubeconfigs",
		},
		{
			name: "MultipleHubs enabled with 2 bootstrapkubeconfigs",
			options: &SpokeAgentOptions{
				HubKubeconfigSecret:      "hub-kubeconfig-secret",
				ClusterHealthCheckPeriod: 1 * time.Minute,
				MaxCustomClusterClaims:   20,
				BootstrapKubeconfigs: []string{
					"/spoke/bootstrap/kubeconfig-hub1",
					"/spoke/bootstrap/kubeconfig-hub2",
				},
				ClientCertExpirationSeconds: 3600,
			},
			pre: func() {
				_ = features.SpokeMutableFeatureGate.SetFromMap(map[string]bool{
					string(ocmfeature.MultipleHubs): true,
				})
			},
			expectedErr: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.pre != nil {
				c.pre()
			}
			err := c.options.Validate()
			testingcommon.AssertError(t, err, c.expectedErr)
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
