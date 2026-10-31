package spoke

import (
	"context"
	"os"
	"path"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ocmfeature "open-cluster-management.io/api/feature"
	workapiv1 "open-cluster-management.io/api/work/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/test/integration/util"
)

var testEnv *envtest.Environment
var sourceConfigFileName string
var cfg *rest.Config

var CRDPaths = []string{
	// hub
	"../../../vendor/open-cluster-management.io/api/work/v1/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
	"../../../vendor/open-cluster-management.io/api/work/v1/0000_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml",
}

func TestWorkAgent(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Work Manager Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	By("bootstrapping test environment")
	var err error

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     CRDPaths,
	}
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = workapiv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// create kubeconfig file for hub in a tmp dir
	tempDir, err := os.MkdirTemp("", "test")
	Expect(err).ToNot(HaveOccurred())
	Expect(tempDir).ToNot(BeEmpty())

	sourceConfigFileName = path.Join(tempDir, "kubeconfig")
	err = util.CreateKubeconfigFile(cfg, sourceConfigFileName)
	Expect(err).ToNot(HaveOccurred())

	err = features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates)
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

var _ = Describe("start work agent", func() {
	It("start work agent", func() {
		runCtx, stopAgent := context.WithCancel(context.Background())
		commonOpts := commonoptions.NewAgentOptions()
		commonOpts.SpokeClusterName = "cluster1"
		opts := NewWorkloadAgentOptions()
		opts.WorkloadSourceConfig = sourceConfigFileName
		agentConfig := NewWorkAgentConfig(commonOpts, opts)

		// start hub controller
		go func() {
			err := agentConfig.RunWorkloadAgent(runCtx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("work-agent"),
			})
			Expect(err).NotTo(HaveOccurred())
		}()
		stopAgent()
	})
})
