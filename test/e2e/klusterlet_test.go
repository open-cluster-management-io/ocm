package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"
)

var _ = Describe("Test Cases: Create klusterlet", func() {
	var klusterletName = ""
	BeforeEach(func() {
		klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
		checkHubReady()
		checkKlusterletOperatorReady()
	})

	AfterEach(func() {
		By("clean resources after each case")
		cleanResources()
	})

	It("Sub Case: Created klusterlet with managed cluster name", func() {
		var clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
		var agentNamespace = fmt.Sprintf("e2e-agent-%s", rand.String(6))

		By("create klusterlet with managed cluster name")
		realClusterName := createKlusterlet(klusterletName, clusterName, agentNamespace)
		Expect(realClusterName).Should(Equal(clusterName))

		By("waiting for the managed cluster to be created and ready")
		Eventually(func() error {
			return managedClusterReady(realClusterName)
		}, eventuallyTimeout*5, eventuallyInterval*5).Should(Succeed())
	})

	It("Sub Case: Created klusterlet without managed cluster name", func() {
		var clusterName = ""
		var agentNamespace = ""

		By("create klusterlet without managed cluster name")
		realClusterName := createKlusterlet(klusterletName, clusterName, agentNamespace)
		Expect(realClusterName).ShouldNot(BeEmpty())

		By("waiting for the managed cluster to be created and ready")
		Eventually(func() error {
			return managedClusterReady(realClusterName)
		}, eventuallyTimeout*5, eventuallyInterval*5).Should(Succeed())
	})
})
