package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var t *Tester

func TestE2E(tt *testing.T) {
	t = NewTester("")

	OutputFail := func(message string, callerSkip ...int) {
		t.OutputDebugLogs()
		Fail(message, callerSkip...)
	}

	RegisterFailHandler(OutputFail)
	RunSpecs(tt, "registration-operator E2E Suite")
}

// This suite is sensitive to the following environment variables:
//
// - KUBECONFIG is the location of the kubeconfig file to use
var _ = BeforeSuite(func() {
	var err error

	Expect(t.Init()).ToNot(HaveOccurred())

	Eventually(t.CheckClusterManagerStatus, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

	Eventually(t.CheckHubReady, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

	Eventually(t.CheckKlusterletOperatorReady, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

	err = t.SetBootstrapHubSecret("")
	Expect(err).ToNot(HaveOccurred())
})
