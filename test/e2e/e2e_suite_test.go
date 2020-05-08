package e2e

import (
	"os"
	"testing"

	ginkgo "github.com/onsi/ginkgo"
	gomega "github.com/onsi/gomega"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clusterv1client "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E suite")
}

var (
	hubClient        kubernetes.Interface
	hubDynamicClient dynamic.Interface
	clusterClient    clusterv1client.Interface
	imageRegistry    string
)

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.LoggerTo(ginkgo.GinkgoWriter, true))
	kubeconfig := os.Getenv("KUBECONFIG")
	imageRegistry = os.Getenv("IMAGE_REGISTRY")
	if imageRegistry == "" {
		imageRegistry = "quay.io/open-cluster-management"
	}

	err := func() error {
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return err
		}

		hubClient, err = kubernetes.NewForConfig(config)
		if err != nil {
			return err
		}

		hubDynamicClient, err = dynamic.NewForConfig(config)
		if err != nil {
			return err
		}

		clusterClient, err = clusterv1client.NewForConfig(config)

		return err
	}()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
