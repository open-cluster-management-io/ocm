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
	"k8s.io/apimachinery/pkg/util/rand"

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

	eventuallyTimeout time.Duration
)

func init() {
	flag.StringVar(&hubKubeconfig, "hub-kubeconfig", "", "The kubeconfig of the hub cluster")
	flag.StringVar(&managedKubeconfig, "managed-kubeconfig", "", "The kubeconfig of the managed cluster")

	flag.StringVar(&klusterletDeployMode, "klusterlet-deploy-mode", string(operatorapiv1.InstallModeDefault), "The image of the work")
	flag.BoolVar(&nilExecutorValidating, "nil-executor-validating", false, "Whether validate the nil executor or not (default false)")

	flag.StringVar(&registrationImage, "registration-image", "", "The image of the registration")
	flag.StringVar(&workImage, "work-image", "", "The image of the work")
	flag.StringVar(&singletonImage, "singleton-image", "", "The image of the klusterlet agent")

	flag.DurationVar(&eventuallyTimeout, "eventually-timeout", 60*time.Second, "The timeout of Gomega's Eventually (default 60 seconds)")
}

// The e2e will always create one universal klusterlet, the developers can reuse this klusterlet in their case
// but also pay attention, because the klusterlet is shared, so the developers should not delete the klusterlet.
// And there might be some side effects on other cases if the developers change the klusterlet's spec for their cases.
var (
	universalClusterName    string
	universalKlusterletName string
	universalAgentNamespace string
)

const (
	UNIVERSAL_CLUSTERSET = "universal"
)

func TestE2E(tt *testing.T) {
	t = NewTester(hubKubeconfig, managedKubeconfig, registrationImage, workImage, singletonImage, eventuallyTimeout)

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

	Expect(t.Init()).ToNot(HaveOccurred())

	Eventually(t.CheckHubReady, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

	Eventually(t.CheckKlusterletOperatorReady, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

	err = t.SetBootstrapHubSecret("")

	if nilExecutorValidating {
		Eventually(func() error {
			return t.EnableWorkFeature("NilExecutorValidating")
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
	}
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() error {
		return t.EnableWorkFeature("ManifestWorkReplicaSet")
	}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
	Eventually(t.CheckHubReady, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

	By("Create a universal Klusterlet/managedcluster, and bind it with universal managedclusterset")
	universalKlusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
	universalClusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
	universalAgentNamespace = fmt.Sprintf("open-cluster-management-agent-%s", rand.String(6))
	_, err = t.CreateApprovedKlusterlet(
		universalKlusterletName, universalClusterName, universalAgentNamespace, operatorapiv1.InstallMode(klusterletDeployMode))
	Expect(err).ToNot(HaveOccurred())

	_, err = t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.TODO(), &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: UNIVERSAL_CLUSTERSET,
		},
	}, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
})
