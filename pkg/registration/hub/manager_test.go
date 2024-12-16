package hub

import (
	"context"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
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
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Manager Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))

	ginkgo.By("bootstrapping test environment")

	var err error

	// install cluster CRD and start a local kube-apiserver
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     CRDPaths,
	}

	cfg, err = testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	err = features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	err = features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = clusterv1.Install(scheme.Scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// enable ManagedClusterAutoApproval feature gate
	err = features.HubMutableFeatureGate.Set("ManagedClusterAutoApproval=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// enable resourceCleanup feature gate
	err = features.HubMutableFeatureGate.Set("ResourceCleanup=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = features.HubMutableFeatureGate.Set("ClusterImporter=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

var _ = ginkgo.Describe("start hub manager", func() {
	ginkgo.It("start hub manager", func() {
		ctx, stopHub := context.WithCancel(context.Background())
		m := NewHubManagerOptions()
		m.ClusterAutoApprovalUsers = []string{util.AutoApprovalBootstrapUser}
		go func() {
			err := m.RunControllerManager(ctx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()
		stopHub()
	})
})
