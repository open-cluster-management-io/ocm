package integration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	certificatesv1 "k8s.io/api/certificates/v1"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var testEnv *envtest.Environment
var hubWorkClient workclientset.Interface
var hubClusterClient clusterv1client.Interface
var hubAddonClient addonv1alpha1client.Interface
var hubKubeClient kubernetes.Interface
var testAddonImpl *testAddon
var testHostedAddonImpl *testAddon
var testInstallByLableAddonImpl *testAddon
var cancel context.CancelFunc
var mgrContext context.Context

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Integration Suite")
}

var _ = ginkgo.BeforeSuite(func(done ginkgo.Done) {
	ginkgo.By("bootstrapping test environment")

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "work", "v1", "0000_00_work.open-cluster-management.io_manifestworks.crd.yaml"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "cluster", "v1"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "addon", "v1alpha1"),
		},
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	hubWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubClusterClient, err = clusterv1client.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubAddonClient, err = addonv1alpha1client.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubKubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	testAddonImpl = &testAddon{
		name:          "test",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]addonapiv1alpha1.RegistrationConfig{},
	}

	testHostedAddonImpl = &testAddon{
		name:              "test-hosted",
		manifests:         map[string][]runtime.Object{},
		registrations:     map[string][]addonapiv1alpha1.RegistrationConfig{},
		hostedModeEnabled: true,
	}

	testInstallByLableAddonImpl = &testAddon{
		name:          "test-install-all",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]addonapiv1alpha1.RegistrationConfig{},
		installStrategy: &agent.InstallStrategy{
			Type:             agent.InstallByLabel,
			InstallNamespace: "default",
			LabelSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{
					"test": "test",
				},
			},
		},
	}

	mgrContext, cancel = context.WithCancel(context.TODO())
	// start hub controller
	go func() {
		mgr, err := addonmanager.New(cfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = mgr.AddAgent(testAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = mgr.AddAgent(testInstallByLableAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = mgr.AddAgent(testHostedAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = mgr.Start(mgrContext)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	close(done)
}, 300)

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	cancel()
	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

type testAddon struct {
	name              string
	manifests         map[string][]runtime.Object
	registrations     map[string][]addonapiv1alpha1.RegistrationConfig
	approveCSR        bool
	cert              []byte
	prober            *agent.HealthProber
	installStrategy   *agent.InstallStrategy
	hostedModeEnabled bool
}

func (t *testAddon) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return t.manifests[cluster.Name], nil
}

func (t *testAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
	option := agent.AgentAddonOptions{
		AddonName:         t.name,
		HealthProber:      t.prober,
		InstallStrategy:   t.installStrategy,
		HostedModeEnabled: t.hostedModeEnabled,
	}

	if len(t.registrations) > 0 {
		option.Registration = &agent.RegistrationOption{
			CSRConfigurations: func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
				return t.registrations[cluster.Name]
			},
			CSRApproveCheck: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
				return t.approveCSR
			},
			CSRSign: func(csr *certificatesv1.CertificateSigningRequest) []byte {
				return t.cert
			},
		}
	}

	return option
}
