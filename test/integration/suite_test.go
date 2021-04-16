package integration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	"github.com/open-cluster-management/addon-framework/pkg/agent"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/open-cluster-management/addon-framework/pkg/addonmanager"
	addonapiv1alpha1 "github.com/open-cluster-management/api/addon/v1alpha1"
	addonv1alpha1client "github.com/open-cluster-management/api/client/addon/clientset/versioned"
	clusterv1client "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	workclientset "github.com/open-cluster-management/api/client/work/clientset/versioned"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
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
			filepath.Join(".", "vendor", "github.com", "open-cluster-management", "api", "work", "v1", "0000_00_work.open-cluster-management.io_manifestworks.crd.yaml"),
			filepath.Join(".", "vendor", "github.com", "open-cluster-management", "api", "cluster", "v1"),
			filepath.Join(".", "vendor", "github.com", "open-cluster-management", "api", "addon", "v1alpha1"),
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
	// start hub controller
	go func() {
		mgr, err := addonmanager.New(cfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = mgr.AddAgent(testAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		mgr.Start(context.Background())
	}()

	close(done)
}, 60)

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

type testAddon struct {
	name          string
	manifests     map[string][]runtime.Object
	registrations map[string][]addonapiv1alpha1.RegistrationConfig
	approveCSR    bool
	cert          []byte
}

func (t *testAddon) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return t.manifests[cluster.Name], nil
}

func (t *testAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
	option := agent.AgentAddonOptions{
		AddonName: t.name,
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
