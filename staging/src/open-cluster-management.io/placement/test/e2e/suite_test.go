package e2e

import (
	"flag"
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

var (
	hubKubeConfig            string
	createGlobalClusterSet   bool
	tolerateUnreachableTaint bool
	kubeClient               kubernetes.Interface
	clusterClient            clusterclient.Interface
	restConfig               *rest.Config
)

func init() {
	flag.StringVar(&hubKubeConfig, "hub-kubeconfig", "", "The kubeconfig of the hub cluster")
	flag.BoolVar(&createGlobalClusterSet, "create-global-clusterset", true, "Whether create global clusterset or not (default true, global clusterset will be created)")
	flag.BoolVar(&tolerateUnreachableTaint, "tolerate-unreachable-taint", false, "Whether tolerate the cluster.open-cluster-management.io/unreachable taint or not (default not, placements created by the test cases will not tolerate this taint)")
}

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Placement E2E Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))

	// pick up hubKubeconfig from argument first,
	// and then fall back to environment variable
	if len(hubKubeConfig) == 0 {
		hubKubeConfig = os.Getenv("KUBECONFIG")
	}
	gomega.Expect(hubKubeConfig).ToNot(gomega.BeEmpty(), "hubKubeConfig is not specified.")

	var err error
	restConfig, err = clientcmd.BuildConfigFromFlags("", hubKubeConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	kubeClient, err = kubernetes.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	clusterClient, err = clusterclient.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
