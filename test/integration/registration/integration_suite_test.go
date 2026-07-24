package registration_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	operatorv1 "open-cluster-management.io/api/operator/v1"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/hub"
	importoptions "open-cluster-management.io/ocm/pkg/registration/hub/importer/options"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/pkg/registration/spoke/addon"
	"open-cluster-management.io/ocm/pkg/registration/spoke/registration"
	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var hubCfg, spokeCfg *rest.Config
var bootstrapKubeConfigFile string
var bootstrapKubeConfigHTTPProxyFile string
var bootstrapKubeConfigHTTPSProxyFile string

var httpProxyURL string
var httpsProxyURL string

var testEnv *envtest.Environment
var securePort string
var serverCertFile string

var kubeClient kubernetes.Interface
var clusterClient clusterclientset.Interface
var addOnClient addonclientset.Interface
var workClient workclientset.Interface

var testNamespace string

var authn *util.TestAuthn

var stopHub context.CancelFunc
var stopProxy context.CancelFunc
var stopWebhook context.CancelFunc

var startHub func(m *hub.HubManagerOptions)
var hubOption *hub.HubManagerOptions

var CRDPaths = []string{
	// hub
	"./vendor/open-cluster-management.io/api/cluster/v1/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml",
	"./vendor/open-cluster-management.io/api/work/v1/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
	"./vendor/open-cluster-management.io/api/addon/v1beta1/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml",
	"./vendor/open-cluster-management.io/api/cluster/v1beta2/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml",
	"./vendor/open-cluster-management.io/api/cluster/v1beta2/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml",
	// spoke
	"./vendor/open-cluster-management.io/api/cluster/v1alpha1/0000_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml",
	// external API deps
	"./test/integration/testdeps/capi/cluster.x-k8s.io_clusters.yaml",
	"./manifests/klusterlet/managed/clusterproperties.crd.yaml",
	// cluster profile
	"./manifests/cluster-manager/hub/crds/0000_00_multicluster.x-k8s.io_clusterprofiles.crd.yaml",
}

func runAgent(name string, opt *spoke.SpokeAgentOptions, commOption *commonoptions.AgentOptions, cfg *rest.Config) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	agentConfig := spoke.NewSpokeAgentConfig(commOption, opt, cancel)
	runAgentWithContext(ctx, name, agentConfig, cfg)
	return cancel
}

func runAgentWithContext(ctx context.Context, name string, agentConfig *spoke.SpokeAgentConfig, cfg *rest.Config) {
	go func() {
		err := agentConfig.RunSpokeAgent(ctx, &controllercmd.ControllerContext{
			KubeConfig:    cfg,
			EventRecorder: util.NewIntegrationTestEventRecorder(name),
		})
		if err != nil {
			fmt.Printf("Failed to run agent %s: %v\n", name, err)
			return
		}
	}()
}

func init() {
	klog.InitFlags(nil)
	klog.SetOutput(ginkgo.GinkgoWriter)
}

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Integration Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))

	ginkgo.By("bootstrapping test environment")

	var err error

	// crank up the sync speed
	transport.CertCallbackRefreshDuration = 5 * time.Second
	register.ControllerResyncInterval = 5 * time.Second
	registration.CreatingControllerSyncInterval = 1 * time.Second

	// crank up the addon lease sync and udpate speed
	spoke.AddOnLeaseControllerSyncInterval = 5 * time.Second
	addon.AddOnLeaseControllerLeaseDurationSeconds = 1

	// install cluster CRD and start a local kube-apiserver
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	authn = util.DefaultTestAuthn
	apiserver := &envtest.APIServer{}
	apiserver.SecureServing.Authn = authn

	testEnv = &envtest.Environment{
		ControlPlane: envtest.ControlPlane{
			APIServer: apiserver,
		},
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     CRDPaths,
		WebhookInstallOptions: envtest.WebhookInstallOptions{},
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates)
	features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates)

	// install scheme
	scheme := runtime.NewScheme()
	err = clusterv1.Install(scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	err = clientgoscheme.AddToScheme(scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// prepare configs
	securePort = testEnv.ControlPlane.APIServer.SecureServing.Port
	gomega.Expect(len(securePort)).ToNot(gomega.BeZero())

	serverCertFile = fmt.Sprintf("%s/apiserver.crt", testEnv.ControlPlane.APIServer.CertDir)

	hubCfg = cfg
	spokeCfg = cfg
	gomega.Expect(spokeCfg).ToNot(gomega.BeNil())

	bootstrapKubeConfigFile = path.Join(util.TestDir, "bootstrap", "kubeconfig")
	err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapKubeConfigFile, serverCertFile, securePort, 24*time.Hour)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// prepare clients
	kubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(kubeClient).ToNot(gomega.BeNil())

	// patch addon for conversion webhook and start webhook
	apiExtensionClient, err := apiextensionsclient.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	err = util.AddConversionForAddonAPI(context.TODO(), apiExtensionClient, testEnv, "managedclusteraddons.addon.open-cluster-management.io")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	clusterClient, err = clusterclientset.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(clusterClient).ToNot(gomega.BeNil())

	addOnClient, err = addonclientset.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(clusterClient).ToNot(gomega.BeNil())

	workClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(clusterClient).ToNot(gomega.BeNil())

	// prepare test namespace
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		testNamespace = "open-cluster-management-agent"
	} else {
		testNamespace = string(nsBytes)
	}
	err = util.PrepareSpokeAgentNamespace(kubeClient, testNamespace)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// enable DefaultClusterSet feature gate
	err = features.HubMutableFeatureGate.Set("DefaultClusterSet=true")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// enable ManagedClusterAutoApproval feature gate
	err = features.HubMutableFeatureGate.Set("ManagedClusterAutoApproval=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// enable resourceCleanup feature gate
	err = features.HubMutableFeatureGate.Set("ResourceCleanup=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// enable clusterImporter feature gate
	err = features.HubMutableFeatureGate.Set("ClusterImporter=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// enable ClusterProfile feature gate
	err = features.HubMutableFeatureGate.Set("ClusterProfile=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// start hub controller
	var ctx context.Context

	hubOption = hub.NewHubManagerOptions()
	hubOption.EnabledRegistrationDrivers = []string{operatorv1.CSRAuthType}
	hubOption.ClusterAutoApprovalUsers = []string{util.AutoApprovalBootstrapUser}

	startHub = func(m *hub.HubManagerOptions) {
		ctx, stopHub = context.WithCancel(context.Background())
		go func() {
			defer ginkgo.GinkgoRecover()
			m.ImportOption.APIServerURL = cfg.Host
			m.ImportOption.ImporterRenderers = []string{importoptions.RenderFromConfigSecret}
			err := m.RunControllerManager(ctx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()
	}

	startHub(hubOption)
	var webhookCtx context.Context
	webhookCtx, stopWebhook = context.WithCancel(context.Background())
	go func() {
		defer ginkgo.GinkgoRecover()
		err := util.StartWebhook(webhookCtx, testEnv, cfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	// start a proxy server
	proxyCertData, proxyKeyData, err := authn.SignServerCert("proxyserver", 24*time.Hour)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	proxyServer := util.NewProxyServer(proxyCertData, proxyKeyData)
	ctx, stopProxy = context.WithCancel(context.Background())
	err = proxyServer.Start(ctx, 5*time.Second)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	httpProxyURL = proxyServer.HTTPProxyURL
	httpsProxyURL = proxyServer.HTTPSProxyURL

	// create bootstrap hub kubeconfig with http/https proxy settings
	bootstrapKubeConfigHTTPProxyFile = path.Join(util.TestDir, "bootstrap-http-proxy", "kubeconfig")
	err = authn.CreateBootstrapKubeConfigWithProxy(bootstrapKubeConfigHTTPProxyFile, serverCertFile, securePort, httpProxyURL, nil)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	bootstrapKubeConfigHTTPSProxyFile = path.Join(util.TestDir, "bootstrap-https-proxy", "kubeconfig")
	err = authn.CreateBootstrapKubeConfigWithProxy(bootstrapKubeConfigHTTPSProxyFile, serverCertFile, securePort, httpsProxyURL, proxyCertData)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")
	if stopHub != nil {
		stopHub()
	}
	if stopProxy != nil {
		stopProxy()
	}
	if stopWebhook != nil {
		stopWebhook()
	}

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = os.RemoveAll(util.TestDir)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

// Common test helper functions

// assertSuccessClusterBootstrap verifies that a managed cluster successfully bootstraps to the hub
func assertSuccessClusterBootstrap(managedClusterName, hubKubeconfigSecret string) {
	// the spoke cluster and csr should be created after bootstrap
	ginkgo.By("Check existence of ManagedCluster & CSR")
	gomega.Eventually(func() error {
		if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

	gomega.Eventually(func() error {
		if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

	// the spoke cluster should has finalizer that is added by hub controller
	gomega.Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
		if err != nil {
			return err
		}

		if !commonhelpers.HasFinalizer(spokeCluster.Finalizers, clusterv1.ManagedClusterFinalizer) {
			return fmt.Errorf("managed cluster does not have finalizer")
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

	ginkgo.By("Accept and approve the ManagedCluster")
	// simulate hub cluster admin to accept the managedcluster and approve the csr
	err := util.AcceptManagedCluster(clusterClient, managedClusterName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// the managed cluster should have accepted condition after it is accepted
	gomega.Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
		if err != nil {
			return err
		}
		accepted := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted)
		if accepted == nil {
			return fmt.Errorf("managed cluster is not accepted")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

	// the hub kubeconfig secret should be filled after the csr is approved
	gomega.Eventually(func() error {
		if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

	ginkgo.By("ManagedCluster joins the hub")
	// the spoke cluster should have joined condition finally
	gomega.Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
		if err != nil {
			return err
		}
		joined := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined)
		if joined == nil {
			return fmt.Errorf("managed cluster has not joined")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

	// ensure cluster namespace is in place
	gomega.Eventually(func() error {
		_, err := kubeClient.CoreV1().Namespaces().Get(context.TODO(), managedClusterName, metav1.GetOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
}

// assertAddonLabel verifies that the addon status label exists on the managed cluster
func assertAddonLabel(clusterName, addonName, status string) {
	ginkgo.By("Check addon status label on managed cluster")
	gomega.Eventually(func() error {
		cluster, err := util.GetManagedCluster(clusterClient, clusterName)
		if err != nil {
			return err
		}
		if len(cluster.Labels) == 0 {
			return fmt.Errorf("no labels found on managed cluster")
		}
		key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addonName)
		if cluster.Labels[key] != status {
			return fmt.Errorf("addon label %s is %q, expected %q", key, cluster.Labels[key], status)
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
}
