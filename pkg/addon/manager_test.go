package addon

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/test/integration/util"
)

var testEnv *envtest.Environment
var cfg *rest.Config

func TestAddonManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Addon Manager Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	By("bootstrapping test environment")
	var err error

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join("../../", "vendor", "open-cluster-management.io", "api", "work", "v1", "0000_00_work.open-cluster-management.io_manifestworks.crd.yaml"),
			filepath.Join("../../", "vendor", "open-cluster-management.io", "api", "cluster", "v1"),
			filepath.Join("../../", "vendor", "open-cluster-management.io", "api", "cluster", "v1beta1"),
			filepath.Join("../../", "vendor", "open-cluster-management.io", "api", "addon", "v1beta1"),
		},
	}
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = workv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = clusterv1beta1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = clusterv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = addonv1beta1.Install(scheme.Scheme)
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

		// start hub controller
		go func() {
			err := RunManager(runCtx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
			})
			Expect(err).ToNot(HaveOccurred())
		}()
		stopHub()
	})
})
