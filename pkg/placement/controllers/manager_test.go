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

	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/test/integration/util"
)

var testEnv *envtest.Environment
var cfg *rest.Config

var CRDPaths = []string{
	"../../../vendor/open-cluster-management.io/api/cluster/v1/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/cluster/v1alpha1/0000_05_clusters.open-cluster-management.io_addonplacementscores.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/cluster/v1beta2/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/cluster/v1beta2/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/cluster/v1beta1/0000_02_clusters.open-cluster-management.io_placements.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/cluster/v1beta1/0000_03_clusters.open-cluster-management.io_placementdecisions.crd.yaml",
	"../../../manifests/cluster-manager/hub/crds/0000_00_multicluster.x-k8s.io_placementdecisions.crd.yaml",
}

func TestPlacementManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Placement Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	By("bootstrapping test environment")

	err := features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubPlacementFeatureGates)
	Expect(err).NotTo(HaveOccurred())

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     CRDPaths,
	}
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = clusterv1beta2.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = clusterv1beta1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

var _ = Describe("start hub manager", func() {
	It("start hub manager", func() {
		ctx, stopHub := context.WithCancel(context.Background())

		// start hub controller
		go func() {
			err := RunControllerManager(ctx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
			})
			Expect(err).ToNot(HaveOccurred())
		}()
		stopHub()
	})
})
