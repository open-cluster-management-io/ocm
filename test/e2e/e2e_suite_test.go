package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"testing"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var t *Tester

// This suite is sensitive to the following environment variables:
//
// - KUBECONFIG is the location of the kubeconfig file to use
var _ = BeforeSuite(func() {
	var err error

	t, err = NewTester("")
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() error {
		return t.CheckHubReady()
	}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

	Eventually(func() error {
		return t.CheckKlusterletOperatorReady()
	}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

	err = t.SetBootstrapHubSecret("")
	Expect(err).ToNot(HaveOccurred())
})
