package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	workclientset "github.com/open-cluster-management/api/client/work/clientset/versioned"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	workImage       string
	restConfig      *rest.Config
	spokeKubeClient kubernetes.Interface
	hubWorkClient   workclientset.Interface
)

func TestE2e(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2e Suite")
}

// This suite is sensitive to the following environment variables:
//
// - IMAGE_NAME sets the exact image to deploy for the work agent
// - IMAGE_REGISTRY sets the image registry to use to build the IMAGE_NAME if
//   IMAGE_NAME is unset: IMAGE_REGISTRY/work:latest
// - KUBECONFIG is the location of the kubeconfig file to use
var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.LoggerTo(ginkgo.GinkgoWriter, true))
	workImage = os.Getenv("IMAGE_NAME")
	if workImage == "" {
		imageRegistry := os.Getenv("IMAGE_REGISTRY")
		if imageRegistry == "" {
			imageRegistry = "quay.io/open-cluster-management"
		}
		workImage = fmt.Sprintf("%v/work:latest", imageRegistry)
	}

	var err error
	kubeconfig := os.Getenv("KUBECONFIG")
	restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	spokeKubeClient, err = kubernetes.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	hubWorkClient, err = workclientset.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
