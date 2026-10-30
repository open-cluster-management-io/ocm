package hub

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/test/integration/util"
)

var testEnv *envtest.Environment
var cfg *rest.Config

var CRDPaths = []string{
	// hub
	"../../../vendor/open-cluster-management.io/api/cluster/v1/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/work/v1/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/addon/v1alpha1/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/cluster/v1beta2/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/cluster/v1beta2/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml",
}

func TestManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Manager Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")

	var err error

	// install cluster CRD and start a local kube-apiserver
	Expect(err).ToNot(HaveOccurred())

	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     CRDPaths,
	}

	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates)
	Expect(err).ToNot(HaveOccurred())
	err = features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates)
	Expect(err).ToNot(HaveOccurred())

	err = clusterv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// enable ManagedClusterAutoApproval feature gate
	err = features.HubMutableFeatureGate.Set("ManagedClusterAutoApproval=true")
	Expect(err).NotTo(HaveOccurred())

	// enable resourceCleanup feature gate
	err = features.HubMutableFeatureGate.Set("ResourceCleanup=true")
	Expect(err).NotTo(HaveOccurred())

	err = features.HubMutableFeatureGate.Set("ClusterImporter=true")
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

var _ = Describe("start hub manager", func() {
	It("start hub manager", func() {
		runCtx, stopHub := context.WithCancel(context.Background())
		m := NewHubManagerOptions()
		m.ClusterAutoApprovalUsers = []string{util.AutoApprovalBootstrapUser}
		go func() {
			err := m.RunControllerManager(runCtx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
			})
			Expect(err).NotTo(HaveOccurred())
		}()
		stopHub()
	})
})
