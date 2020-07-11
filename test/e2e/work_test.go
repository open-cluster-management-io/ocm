package e2e

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

var _ = Describe("Create klusterlet and then create a configmap by manifestwork", func() {
	var klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
	var clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
	var agentNamespace = fmt.Sprintf("e2e-agent-%s", rand.String(6))
	var workName = fmt.Sprintf("e2e-work-configmap-%s", rand.String(6))
	var configMapNamespace = "default"
	var configMapName = fmt.Sprintf("e2e-configmap-%s", rand.String(6))

	AfterEach(func() {
		By(fmt.Sprintf("clean klusterlet %v resources after the test case", klusterletName))
		t.cleanKlusterletResources(klusterletName)
	})

	It("Create configmap using manifestwork and then delete klusterlet", func() {
		var err error
		By(fmt.Sprintf("create klusterlet %v with managed cluster name %v", klusterletName, clusterName))
		_, err = t.CreateKlusterlet(klusterletName, clusterName, agentNamespace)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("waiting for the managed cluster %v to be created", clusterName))
		Eventually(func() error {
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

		By(fmt.Sprintf("create configmap %v/%v using manifestwork %v/%v", configMapNamespace,
			configMapName, clusterName, workName))
		_, err = t.CreateWorkOfConfigMap(workName, clusterName, configMapName, configMapNamespace)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("waiting for configmap %v/%v to be created", configMapNamespace, configMapName))
		Eventually(func() error {
			_, err := t.KubeClient.CoreV1().ConfigMaps(configMapNamespace).
				Get(context.TODO(), configMapName, metav1.GetOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		// Manifest work and ns should not be deleted after delete klusterlet
		By(fmt.Sprintf("delete klusterlet %v", klusterletName))
		err = t.OperatorClient.OperatorV1().Klusterlets().Delete(context.TODO(), klusterletName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By(fmt.Sprintf("waiting for pods in agent namespace %v to be deleted", agentNamespace))
		Eventually(func() error {
			pods, err := t.KubeClient.CoreV1().Pods(agentNamespace).
				List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			if len(pods.Items) != 0 {
				return fmt.Errorf("the pods are not deleted in ns %v", agentNamespace)
			}
			return nil
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By(fmt.Sprintf("check that managed cluster namespace %v should not be deleted", clusterName))
		Eventually(func() error {
			_, err := t.KubeClient.CoreV1().Namespaces().
				Get(context.TODO(), clusterName, metav1.GetOptions{})
			return err
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

		By(fmt.Sprintf("check that manifestwork %v/%v should not be deleted", clusterName, workName))
		Eventually(func() error {
			_, err := t.WorkClient.WorkV1().ManifestWorks(clusterName).
				Get(context.TODO(), workName, metav1.GetOptions{})
			return err
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())
	})
})
