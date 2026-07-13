package integration

import (
	"context"
	"path/filepath"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	certificatesv1 "k8s.io/api/certificates/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/addon"
	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var addOnDeploymentConfigGVR = schema.GroupVersionResource{
	Group:    "addon.open-cluster-management.io",
	Version:  "v1alpha1",
	Resource: "addondeploymentconfigs",
}

var testEnv *envtest.Environment
var hubWorkClient workclientset.Interface
var hubClusterClient clusterv1client.Interface
var hubAddonClient addonclient.Interface
var hubKubeClient kubernetes.Interface
var testAddonImpl *testAddon
var testAddOnConfigsImpl *testAddon

var cancel context.CancelFunc
var mgrContext context.Context
var stopWebhook context.CancelFunc
var addonManager addonmanager.AddonManager

func init() {
	klog.InitFlags(nil)
	klog.SetOutput(ginkgo.GinkgoWriter)
}

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Integration Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	ginkgo.By("bootstrapping test environment")

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "work", "v1", "0000_00_work.open-cluster-management.io_manifestworks.crd.yaml"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "cluster", "v1"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "cluster", "v1beta1"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "addon", "v1beta1"),
		},
		WebhookInstallOptions: envtest.WebhookInstallOptions{},
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	// Configure conversion webhooks for addon CRDs so the API server can convert between
	// v1alpha1 (storage version) and v1beta1 (used by controllers).
	apiExtClient, err := apiextensionsclient.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	err = util.AddConversionForAddonAPI(context.TODO(), apiExtClient, testEnv,
		"clustermanagementaddons.addon.open-cluster-management.io",
		"managedclusteraddons.addon.open-cluster-management.io")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	var webhookCtx context.Context
	webhookCtx, stopWebhook = context.WithCancel(context.Background())
	go func() {
		defer ginkgo.GinkgoRecover()
		err := util.StartWebhook(webhookCtx, testEnv, cfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	hubWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubClusterClient, err = clusterv1client.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubAddonClient, err = addonclient.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubKubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	testAddonImpl = &testAddon{
		name:          "test",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]agent.RegistrationConfig{},
	}

	testAddOnConfigsImpl = &testAddon{
		name:                "test-addon-configs",
		manifests:           map[string][]runtime.Object{},
		registrations:       map[string][]agent.RegistrationConfig{},
		supportedConfigGVRs: []schema.GroupVersionResource{addOnDeploymentConfigGVR},
	}

	mgrContext, cancel = context.WithCancel(context.Background())
	// start hub controller
	go func() {
		addonManager, err = addonmanager.New(cfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = addonManager.AddAgent(testAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(testAddOnConfigsImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = addonManager.Start(mgrContext)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = addon.RunManager(mgrContext, &controllercmd.ControllerContext{
			KubeConfig:    cfg,
			EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	cancel()
	err := testEnv.Stop()

	if cancel != nil {
		cancel()
	}

	if stopWebhook != nil {
		stopWebhook()
	}
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

type testAddon struct {
	name                string
	manifests           map[string][]runtime.Object
	registrations       map[string][]agent.RegistrationConfig
	approveCSR          bool
	cert                []byte
	prober              *agent.HealthProber
	hostedModeEnabled   bool
	supportedConfigGVRs []schema.GroupVersionResource
}

func (t *testAddon) Manifests(ctx context.Context, cluster *clusterv1.ManagedCluster, _ *addonv1beta1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return t.manifests[cluster.Name], nil
}

func (t *testAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
	option := agent.AgentAddonOptions{
		AddonName:           t.name,
		HealthProber:        t.prober,
		HostedModeEnabled:   t.hostedModeEnabled,
		SupportedConfigGVRs: t.supportedConfigGVRs,
	}

	if len(t.registrations) > 0 {
		option.Registration = &agent.RegistrationOption{
			Configurations: func(ctx context.Context, cluster *clusterv1.ManagedCluster, addon *addonv1beta1.ManagedClusterAddOn,
			) ([]agent.RegistrationConfig, error) {
				return t.registrations[cluster.Name], nil
			},
			CSRApproveCheck: func(ctx context.Context, cluster *clusterv1.ManagedCluster, addon *addonv1beta1.ManagedClusterAddOn,
				csr *certificatesv1.CertificateSigningRequest) bool {
				return t.approveCSR
			},
			CSRSign: func(ctx context.Context, cluster *clusterv1.ManagedCluster, addon *addonv1beta1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) ([]byte, error) {
				return t.cert, nil
			},
		}
	}

	return option
}

func newClusterManagementAddonAlpha(name string) *addonapiv1alpha1.ClusterManagementAddOn {
	return &addonapiv1alpha1.ClusterManagementAddOn{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
			InstallStrategy: addonapiv1alpha1.InstallStrategy{
				Type: addonapiv1alpha1.AddonInstallStrategyManual,
			},
		},
	}
}

func newClusterManagementAddonBeta(name string) *addonv1beta1.ClusterManagementAddOn {
	return &addonv1beta1.ClusterManagementAddOn{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: addonv1beta1.ClusterManagementAddOnSpec{
			InstallStrategy: addonv1beta1.InstallStrategy{
				Type: addonv1beta1.AddonInstallStrategyManual,
			},
		},
	}
}
