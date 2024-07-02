package clustermanager

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
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
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "ClusterManager Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))

	ginkgo.By("bootstrapping test environment")

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

	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())
})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

var _ = ginkgo.Describe("start cluster manager", func() {
	ginkgo.It("start hub manager", func() {
		ctx, stopHub := context.WithCancel(context.Background())

		// start hub controller
		go func() {
			o := &Options{}
			err := o.RunClusterManagerOperator(ctx, &controllercmd.ControllerContext{
				KubeConfig:        cfg,
				EventRecorder:     util.NewIntegrationTestEventRecorder("integration"),
				OperatorNamespace: metav1.NamespaceDefault,
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()
		stopHub()
	})
})
