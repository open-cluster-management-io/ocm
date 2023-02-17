package integration

import (
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/helper"
	"open-cluster-management.io/work/test/integration/util"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var tempDir string
var hubKubeconfigFileName string
var spokeRestConfig *rest.Config
var testEnv *envtest.Environment
var spokeKubeClient kubernetes.Interface
var spokeWorkClient workclientset.Interface
var hubWorkClient workclientset.Interface
var hubHash string

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Integration Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	ginkgo.By("bootstrapping test environment")

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "deploy", "spoke"),
			filepath.Join(".", "deploy", "hub"),
		},
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	// create kubeconfig file for hub in a tmp dir
	tempDir, err = os.MkdirTemp("", "test")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(tempDir).ToNot(gomega.BeEmpty())
	hubKubeconfigFileName = path.Join(tempDir, "kubeconfig")
	err = util.CreateKubeconfigFile(cfg, hubKubeconfigFileName)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = workapiv1.AddToScheme(scheme.Scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	spokeRestConfig = cfg
	hubHash = helper.HubHash(spokeRestConfig.Host)
	spokeKubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	hubWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	spokeWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	if tempDir != "" {
		os.RemoveAll(tempDir)
	}
})
