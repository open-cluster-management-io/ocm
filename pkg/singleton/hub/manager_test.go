package hub

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ocmfeature "open-cluster-management.io/api/feature"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/test/integration/util"
)

var testEnv *envtest.Environment
var sourceConfigFileName string
var cfg *rest.Config

var CRDPaths = []string{
	// hub
	"../../../vendor/open-cluster-management.io/api/work/v1/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/work/v1alpha1/0000_00_work.open-cluster-management.io_manifestworkreplicasets.crd.yaml",
	filepath.Join("../../../", "vendor", "open-cluster-management.io", "api", "cluster", "v1"),
	filepath.Join("../../../", "vendor", "open-cluster-management.io", "api", "cluster", "v1beta1"),
	filepath.Join("../../../", "vendor", "open-cluster-management.io", "api", "cluster", "v1beta2"),
	filepath.Join("../../../", "vendor", "open-cluster-management.io", "api", "cluster", "v1alpha1"),
	filepath.Join("../../../", "vendor", "open-cluster-management.io", "api", "addon", "v1alpha1"),
}

func TestWorkManager(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Singleton Hub Manager Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	ginkgo.By("bootstrapping test environment")
	var err error

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     CRDPaths,
	}
	cfg, err = testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	err = workapiv1.Install(scheme.Scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubWorkFeatureGates)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubAddonManagerFeatureGates)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// enable ManagedClusterAutoApproval feature gate
	err = features.HubMutableFeatureGate.Set("ManagedClusterAutoApproval=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// enable resourceCleanup feature gate
	err = features.HubMutableFeatureGate.Set("ResourceCleanup=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = features.HubMutableFeatureGate.Set("ManifestWorkReplicaSet=true")
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
		opts := NewHubOption()
		opts.workOption.WorkDriver = "kube"
		opts.workOption.WorkDriverConfig = sourceConfigFileName
		opts.registrationOption.ClusterAutoApprovalUsers = []string{util.AutoApprovalBootstrapUser}

		// start hub controller
		go func() {
			err := opts.RunManager(ctx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()

		time.Sleep(5 * time.Second)
		stopHub()
	})
})
