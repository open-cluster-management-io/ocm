package e2e

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

var t *Tester

var (
	// kubeconfigs
	hubKubeconfig     string
	managedKubeconfig string

	// customized test parameters
	klusterletDeployMode  string
	nilExecutorValidating bool

	// images
	registrationImage string
	workImage         string
	singletonImage    string
)

func init() {
	flag.StringVar(&hubKubeconfig, "hub-kubeconfig", "", "The kubeconfig of the hub cluster")
	flag.StringVar(&managedKubeconfig, "managed-kubeconfig", "", "The kubeconfig of the managed cluster")

	flag.StringVar(&klusterletDeployMode, "klusterlet-deploy-mode", string(operatorapiv1.InstallModeDefault), "The image of the work")
	flag.BoolVar(&nilExecutorValidating, "nil-executor-validating", false, "Whether validate the nil executor or not (default false)")

	flag.StringVar(&registrationImage, "registration-image", "", "The image of the registration")
	flag.StringVar(&workImage, "work-image", "", "The image of the work")
	flag.StringVar(&singletonImage, "singleton-image", "", "The image of the klusterlet agent")
}

// The e2e will always create one universal klusterlet, the developers can reuse this klusterlet in their case
// but also pay attention, because the klusterlet is shared, so the developers should not delete the klusterlet.
// And there might be some side effects on other cases if the developers change the klusterlet's spec for their cases.
const (
	universalClusterName    = "e2e-universal-managedcluster"            //nolint:gosec
	universalKlusterletName = "e2e-universal-klusterlet"                //nolint:gosec
	universalAgentNamespace = "open-cluster-management-agent-universal" //nolint:gosec
	universalClusterSetName = "universal"                               //nolint:gosec
)

func TestE2E(tt *testing.T) {
	t = NewTester(hubKubeconfig, managedKubeconfig, registrationImage, workImage, singletonImage)

	OutputFail := func(message string, callerSkip ...int) {
		t.OutputDebugLogs()
		Fail(message, callerSkip...)
	}

	RegisterFailHandler(OutputFail)
	RunSpecs(tt, "ocm E2E Suite")
}

// This suite is sensitive to the following environment variables:
//
// - KUBECONFIG is the location of the kubeconfig file to use
var _ = BeforeSuite(func() {
	var err error

	// In most OCM cases, we expect user should see the result in 90 seconds.
	// For cases that need more than 90 seconds, please set the timeout in the test case EXPLICITLY.
	SetDefaultEventuallyTimeout(90 * time.Second)
	SetDefaultEventuallyPollingInterval(5 * time.Second)

	Expect(t.Init()).ToNot(HaveOccurred())

	Eventually(t.CheckHubReady).Should(Succeed())

	Eventually(t.CheckKlusterletOperatorReady).Should(Succeed())

	err = t.SetBootstrapHubSecret("")

	if nilExecutorValidating {
		Eventually(func() error {
			return t.EnableWorkFeature("NilExecutorValidating")
		}).Should(Succeed())
	}
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() error {
		return t.EnableWorkFeature("ManifestWorkReplicaSet")
	}).Should(Succeed())
	Eventually(t.CheckHubReady).Should(Succeed())

	By("Create a universal Klusterlet/managedcluster")
	_, err = t.CreateApprovedKlusterlet(
		universalKlusterletName, universalClusterName, universalAgentNamespace, operatorapiv1.InstallMode(klusterletDeployMode))
	Expect(err).ToNot(HaveOccurred())

	By("Create a universal ClusterSet and bind it with the universal managedcluster")
	_, err = t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.TODO(), &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: universalClusterSetName,
		},
		Spec: clusterv1beta2.ManagedClusterSetSpec{
			ClusterSelector: clusterv1beta2.ManagedClusterSelector{
				SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
			},
		},
	}, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() error {
		umc, err := t.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), universalClusterName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		labels := umc.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[clusterv1beta2.ClusterSetLabel] = universalClusterSetName
		umc.SetLabels(labels)
		_, err = t.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), umc, metav1.UpdateOptions{})
		return err
	}).Should(Succeed())
})

var _ = AfterSuite(func() {
	By(fmt.Sprintf("clean klusterlet %v resources after the test case", universalKlusterletName))
	Expect(t.cleanKlusterletResources(universalKlusterletName, universalClusterName)).To(BeNil())
})
