package e2e

import (
	"os"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E Suite")
}

var (
	kubeClient    kubernetes.Interface
	clusterClient clusterclient.Interface
	restConfig    *rest.Config
)

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	kubeconfig := os.Getenv("KUBECONFIG")

	var err error
	restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	kubeClient, err = kubernetes.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	clusterClient, err = clusterclient.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
