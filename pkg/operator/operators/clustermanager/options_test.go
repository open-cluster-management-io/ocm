package clustermanager

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"open-cluster-management.io/ocm/test/integration/util"
)

var testEnv *envtest.Environment
var cfg *rest.Config

func TestClusterManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ClusterManager Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")

	var err error
	// install operator CRDs and start a local kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join("../../../../", "deploy", "cluster-manager", "olm-catalog", "latest", "manifests"),
		},
	}
	cfg, err = testEnv.Start()
	cfg.QPS = 100
	cfg.Burst = 200

	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

var _ = Describe("start cluster manager", func() {
	It("start hub manager", func() {
		runCtx, stopHub := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(1)

		// start hub controller
		go func() {
			defer GinkgoRecover()
			defer wg.Done()
			o := &Options{ImagePullSecretName: "my-registry-secret"}
			err := o.RunClusterManagerOperator(runCtx, &controllercmd.ControllerContext{
				KubeConfig:        cfg,
				EventRecorder:     util.NewIntegrationTestEventRecorder("integration"),
				OperatorNamespace: metav1.NamespaceDefault,
			})
			Expect(err).NotTo(HaveOccurred())
		}()
		stopHub()
		// Wait so AfterSuite does not stop envtest while ServerVersion is still dialing.
		wg.Wait()
	})
})
