package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	clusterclient "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	operatorclient "github.com/open-cluster-management/api/client/operator/clientset/versioned"
	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"testing"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

// This suite is sensitive to the following environment variables:
//
// - KUBECONFIG is the location of the kubeconfig file to use
var _ = BeforeSuite(func() {
	kubeconfig := os.Getenv("KUBECONFIG")
	err := func() error {
		var err error
		clusterCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return err
		}
		kubeClient, err = kubernetes.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}
		operatorClient, err = operatorclient.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}
		clusterClient, err = clusterclient.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}
		workClient, err = workv1client.NewForConfig(clusterCfg)
		return err
	}()
	Expect(err).ToNot(HaveOccurred())

	checkHubReady()
	checkKlusterletOperatorReady()
	cleanResources()
})
