package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"
)

var _ = Describe("Create klusterlet CR", func() {
	var klusterletName = ""
	BeforeEach(func() {
		klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
	})

	AfterEach(func() {
		By(fmt.Sprintf("clean klusterlet %v resources after the test case", klusterletName))
		t.cleanKlusterletResources(klusterletName)
	})

	It("Create klusterlet CR with managed cluster name", func() {
		var clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
		var agentNamespace = fmt.Sprintf("e2e-agent-%s", rand.String(6))

		By(fmt.Sprintf("create klusterlet %v with managed cluster name %v", klusterletName, clusterName))
		_, err := t.CreateKlusterlet(klusterletName, clusterName, agentNamespace)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("waiting for the managed cluster %v to be created", clusterName))
		Eventually(func() error {
			_, err := t.GetCreatedManagedCluster(clusterName)
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By(fmt.Sprintf("approve the created managed cluster %v", clusterName))
		Eventually(func() error {
			return t.ApproveCSR(clusterName)
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

		By(fmt.Sprintf("accept the created managed cluster %v", clusterName))
		Eventually(func() error {
			return t.AcceptsClient(clusterName)
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

		By(fmt.Sprintf("waiting for the managed cluster %v to be ready", clusterName))
		Eventually(func() error {
			return t.CheckManagedClusterStatus(clusterName)
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
	})

	It("Created klusterlet without managed cluster name", func() {
		var clusterName = ""
		var agentNamespace = ""
		var err error
		By(fmt.Sprintf("create klusterlet %v without managed cluster name", klusterletName))
		_, err = t.CreateKlusterlet(klusterletName, clusterName, agentNamespace)
		Expect(err).ToNot(HaveOccurred())

		By("waiting for the managed cluster to be created")
		Eventually(func() error {
			clusterName, err = t.GetClusterNameFromKlusterlet(klusterletName)
			if err != nil {
				return err
			}
			_, err = t.GetCreatedManagedCluster(clusterName)
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By(fmt.Sprintf("approve the created managed cluster %v", clusterName))
		Eventually(func() error {
			return t.ApproveCSR(clusterName)
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

		By(fmt.Sprintf("accept the created managed cluster %v", clusterName))
		Eventually(func() error {
			return t.AcceptsClient(clusterName)
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

		By(fmt.Sprintf("waiting for the managed cluster %v to be ready", clusterName))
		Eventually(func() error {
			return t.CheckManagedClusterStatus(clusterName)
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
	})
})
