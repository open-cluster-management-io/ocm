package e2e

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

var _ = Describe("Test Cases: Create klusterlet and then create a configmap by manifestwork", func() {
	var klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
	var clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
	var agentNamespace = fmt.Sprintf("e2e-agent-%s", rand.String(6))
	var workName = fmt.Sprintf("e2e-work-configmap-%s", rand.String(6))
	var configMapNamespace = "default"
	var configMapName = fmt.Sprintf("e2e-configmap-%s", rand.String(6))

	BeforeEach(func() {
		checkHubReady()
		checkKlusterletOperatorReady()
	})

	AfterEach(func() {
		By("clean resources after each case")
		cleanResources()
	})

	It("Sub Case: Create configmap using manifestwork and then delete klusterlet", func() {
		By("create klusterlet")
		realClusterName := createKlusterlet(klusterletName, clusterName, agentNamespace)
		Expect(realClusterName).Should(Equal(clusterName))

		By("waiting for managed cluster to be ready")
		Eventually(func() error {
			return managedClusterReady(realClusterName)
		}, eventuallyTimeout*5, eventuallyInterval*5).Should(Succeed())

		By("create configmap using manifestwork")
		createWorkOfConfigMap(workName, realClusterName, configMapName, configMapNamespace)

		By("waiting for configmap to be created")
		Eventually(func() error {
			_, err := kubeClient.CoreV1().ConfigMaps(configMapNamespace).
				Get(context.TODO(), configMapName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout*5, eventuallyInterval*5).Should(Succeed())

		// Manifest work and ns should not be deleted after delete klusterlet
		By("delete klusterlet")
		err := operatorClient.OperatorV1().Klusterlets().Delete(context.TODO(), klusterletName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for pods in agent namespace to be deleted")
		Eventually(func() error {
			pods, err := kubeClient.CoreV1().Pods(agentNamespace).
				List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			if len(pods.Items) != 0 {
				return fmt.Errorf("the pods are not deleted in ns %v", agentNamespace)
			}
			return nil
		}, eventuallyTimeout*5, eventuallyInterval*5).Should(Succeed())

		By("check that managed cluster namespace should not be deleted")
		Eventually(func() error {
			_, err := kubeClient.CoreV1().Namespaces().
				Get(context.TODO(), clusterName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("check that manifestwork should not be deleted")
		Eventually(func() error {
			_, err := workClient.WorkV1().ManifestWorks(clusterName).
				Get(context.TODO(), workName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	})
})
