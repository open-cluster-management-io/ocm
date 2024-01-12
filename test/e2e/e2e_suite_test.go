package e2e

import (
	"flag"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

var t *Tester

var (
	clusterName           string
	klusterletName        string
	agentNamespace        string
	hubKubeconfig         string
	nilExecutorValidating bool
	deployKlusterlet      bool
	managedKubeconfig     string
	eventuallyTimeout     time.Duration
	registrationImage     string
	workImage             string
	singletonImage        string
	klusterletDeployMode  string
)

func init() {
	flag.StringVar(&clusterName, "cluster-name", "", "The name of the managed cluster on which the testing will be run")
	flag.StringVar(&hubKubeconfig, "hub-kubeconfig", "", "The kubeconfig of the hub cluster")
	flag.BoolVar(&nilExecutorValidating, "nil-executor-validating", false, "Whether validate the nil executor or not (default false)")
	flag.BoolVar(&deployKlusterlet, "deploy-klusterlet", false, "Whether deploy the klusterlet on the managed cluster or not (default false)")
	flag.StringVar(&managedKubeconfig, "managed-kubeconfig", "", "The kubeconfig of the managed cluster")
	flag.DurationVar(&eventuallyTimeout, "eventually-timeout", 60*time.Second, "The timeout of Gomega's Eventually (default 60 seconds)")
	flag.StringVar(&registrationImage, "registration-image", "", "The image of the registration")
	flag.StringVar(&workImage, "work-image", "", "The image of the work")
	flag.StringVar(&singletonImage, "singleton-image", "", "The image of the klusterlet agent")
	flag.StringVar(&klusterletDeployMode, "klusterlet-deploy-mode", string(operatorapiv1.InstallModeDefault), "The image of the work")
}

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

	if deployKlusterlet {
		klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
		clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
		agentNamespace = fmt.Sprintf("open-cluster-management-agent-%s", rand.String(6))
		_, err := t.CreateApprovedKlusterlet(
			klusterletName, clusterName, agentNamespace, operatorapiv1.InstallMode(klusterletDeployMode))
		Expect(err).ToNot(HaveOccurred())
	}
})

var _ = AfterSuite(func() {
	if deployKlusterlet {
		By(fmt.Sprintf("clean klusterlet %v resources after the test case", klusterletName))
		Expect(t.cleanKlusterletResources(klusterletName, clusterName)).To(BeNil())
	}
})
