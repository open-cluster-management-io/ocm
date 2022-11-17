package integration

import (
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var testEnv *envtest.Environment
var restConfig *rest.Config
var kubeClient kubernetes.Interface
var clusterClient clusterv1client.Interface

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Integration Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	ginkgo.By("bootstrapping test environment")

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "deploy", "hub"),
		},
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	kubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	clusterClient, err = clusterv1client.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	restConfig = cfg
})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
