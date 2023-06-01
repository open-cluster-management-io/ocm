package e2e

import (
	"flag"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var t *Tester

var (
	clusterName           string
	hubKubeconfig         string
	nilExecutorValidating bool
	deployKlusterlet      bool
	managedKubeconfig     string
	eventuallyTimeout     time.Duration
)

func init() {
	flag.StringVar(&clusterName, "cluster-name", "", "The name of the managed cluster on which the testing will be run")
	flag.StringVar(&hubKubeconfig, "hub-kubeconfig", "", "The kubeconfig of the hub cluster")
	flag.BoolVar(&nilExecutorValidating, "nil-executor-validating", false, "Whether validate the nil executor or not (default false)")
	flag.BoolVar(&deployKlusterlet, "deploy-klusterlet", false, "Whether deploy the klusterlet on the managed cluster or not (default false)")
	flag.StringVar(&managedKubeconfig, "managed-kubeconfig", "", "The kubeconfig of the managed cluster")
	flag.DurationVar(&eventuallyTimeout, "eventually-timeout", 60*time.Second, "The timeout of Gomega's Eventually (default 60 seconds)")
}

func TestE2E(tt *testing.T) {
	t = NewTester(hubKubeconfig, managedKubeconfig, eventuallyTimeout)

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
})
