package spoke

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ocmfeature "open-cluster-management.io/api/feature"
	operatorv1 "open-cluster-management.io/api/operator/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/features"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/test/integration/util"
)

var testEnv *envtest.Environment
var cfg *rest.Config
var bootstrapKubeConfigFile string
var authn *util.TestAuthn

var CRDPaths = []string{
	// agent
	"../../../vendor/open-cluster-management.io/api/cluster/v1/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/addon/v1alpha1/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml",
}

func init() {
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates))
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name        string
		options     *SpokeAgentOptions
		pre         func()
		expectedErr string
	}{
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
			name: "default completed options",
			options: func() *SpokeAgentOptions {
				defaultCompletedOptions := NewSpokeAgentOptions()
				defaultCompletedOptions.BootstrapKubeconfig = "/spoke/bootstrap/kubeconfig"
				return defaultCompletedOptions
			}(),
			expectedErr: "",
		},
		{
			name: "default completed options for aws flow",
			options: func() *SpokeAgentOptions {
				awsDefaultCompletedOptions := NewSpokeAgentOptions()
				awsDefaultCompletedOptions.BootstrapKubeconfig = "/spoke/bootstrap/kubeconfig"
				awsDefaultCompletedOptions.RegisterDriverOption.RegistrationAuth = operatorv1.AwsIrsaAuthType
				awsDefaultCompletedOptions.RegisterDriverOption.AWSIRSAOption.HubClusterArn = "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1"
				return awsDefaultCompletedOptions
			}(),
			expectedErr: "",
		},
		{
			name: "default completed options without HubClusterArn for aws flow",
			options: func() *SpokeAgentOptions {
				awsCompletedOptionsHubArnMissing := NewSpokeAgentOptions()
				awsCompletedOptionsHubArnMissing.BootstrapKubeconfig = "/spoke/bootstrap/kubeconfig"
				awsCompletedOptionsHubArnMissing.RegisterDriverOption.RegistrationAuth = operatorv1.AwsIrsaAuthType
				return awsCompletedOptionsHubArnMissing
			}(),
			expectedErr: "EksHubClusterArn cannot be empty if RegistrationAuth is awsirsa",
		},
		{
			name: "default completed options",
			options: &SpokeAgentOptions{
				HubKubeconfigSecret:      "hub-kubeconfig-secret",
				ClusterHealthCheckPeriod: 1 * time.Minute,
				MaxCustomClusterClaims:   20,
				BootstrapKubeconfig:      "/spoke/bootstrap/kubeconfig",
				RegisterDriverOption: &registerfactory.Options{
					CSROption: &csr.Option{
						ExpirationSeconds: 3599,
					},
				},
			},
			expectedErr: "client certificate expiration seconds must greater or qual to 3600",
		},
		{
			name: "default completed options",
			options: &SpokeAgentOptions{
				HubKubeconfigSecret:      "hub-kubeconfig-secret",
				ClusterHealthCheckPeriod: 1 * time.Minute,
				MaxCustomClusterClaims:   20,
				BootstrapKubeconfig:      "/spoke/bootstrap/kubeconfig",
				RegisterDriverOption: &registerfactory.Options{
					CSROption: &csr.Option{
						ExpirationSeconds: 3600,
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "MultipleHubs enabled, but bootstrapkubeconfigs is empty",
			options: &SpokeAgentOptions{
				HubKubeconfigSecret:      "hub-kubeconfig-secret",
				ClusterHealthCheckPeriod: 1 * time.Minute,
				MaxCustomClusterClaims:   20,
				BootstrapKubeconfigs:     []string{},
				RegisterDriverOption: &registerfactory.Options{
					CSROption: &csr.Option{
						ExpirationSeconds: 3600,
					},
				},
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
				RegisterDriverOption: &registerfactory.Options{
					CSROption: &csr.Option{
						ExpirationSeconds: 3600,
					},
				},
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
			_, cancel := context.WithCancel(context.TODO())
			cfg := NewSpokeAgentConfig(commonoptions.NewAgentOptions(), c.options, cancel)
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

func TestManager(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Manager Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))

	ginkgo.By("bootstrapping test environment")

	var err error

	authn = util.DefaultTestAuthn
	apiServer := &envtest.APIServer{}
	apiServer.SecureServing.Authn = authn

	testEnv = &envtest.Environment{
		ControlPlane: envtest.ControlPlane{
			APIServer: apiServer,
		},
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     CRDPaths,
	}

	cfg, err = testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	// prepare configs
	securePort := testEnv.ControlPlane.APIServer.SecureServing.Port
	gomega.Expect(len(securePort)).ToNot(gomega.BeZero())

	serverCertFile := fmt.Sprintf("%s/apiserver.crt", testEnv.ControlPlane.APIServer.CertDir)
	bootstrapKubeConfigFile = path.Join(util.TestDir, "bootstrap", "kubeconfig")
	err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapKubeConfigFile, serverCertFile, securePort, 24*time.Hour)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

var _ = ginkgo.Describe("start agent", func() {
	ginkgo.It("start hub manager", func() {
		_ = features.SpokeMutableFeatureGate.SetFromMap(map[string]bool{
			string(ocmfeature.MultipleHubs): false,
		})
		ctx, stopAgent := context.WithCancel(context.Background())
		agentOptions := NewSpokeAgentOptions()
		commonOptions := commonoptions.NewAgentOptions()
		commonOptions.AgentID = "test"
		commonOptions.SpokeClusterName = "cluster1"
		agentOptions.BootstrapKubeconfig = bootstrapKubeConfigFile

		agentConfig := NewSpokeAgentConfig(commonOptions, agentOptions, stopAgent)
		go func() {
			err := agentConfig.RunSpokeAgent(ctx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("agent"),
			})
			// should get err since bootstrap is not finished yet.
			gomega.Expect(err).Should(gomega.HaveOccurred())
		}()
		// wait for 5 second until the controller is started
		time.Sleep(5 * time.Second)
		stopAgent()
	})
})
