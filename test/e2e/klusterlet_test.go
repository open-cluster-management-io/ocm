package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

var _ = Describe("Create klusterlet CR", func() {
	var klusterletName string
	var clusterName string
	var klusterletNamespace string

	BeforeEach(func() {
		klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
		clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
		klusterletNamespace = fmt.Sprintf("open-cluster-management-agent-%s", rand.String(6))
	})

	// This test case is helpful for the Backward compatibility
	It("Create klusterlet CR with install mode empty", func() {
		By(fmt.Sprintf("create klusterlet %v with managed cluster name %v", klusterletName, clusterName))
		// Set install mode empty
		_, err := t.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace, "")
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("waiting for the managed cluster %v to be created", clusterName))
		Eventually(func() error {
			_, err := t.GetCreatedManagedCluster(clusterName)
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := t.checkKlusterletStatus(klusterletName, "HubConnectionDegraded", "BootstrapSecretFunctional,HubKubeConfigSecretMissing", metav1.ConditionTrue)
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

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := t.checkKlusterletStatus(klusterletName, "HubConnectionDegraded", "HubConnectionFunctional", metav1.ConditionFalse)
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
	})

	It("Create klusterlet CR with managed cluster name", func() {
		By(fmt.Sprintf("create klusterlet %v with managed cluster name %v", klusterletName, clusterName))
		_, err := t.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace, operatorapiv1.InstallMode(klusterletDeployMode))
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("waiting for the managed cluster %v to be created", clusterName))
		Eventually(func() error {
			_, err := t.GetCreatedManagedCluster(clusterName)
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := t.checkKlusterletStatus(klusterletName, "HubConnectionDegraded", "BootstrapSecretFunctional,HubKubeConfigSecretMissing", metav1.ConditionTrue)
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

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := t.checkKlusterletStatus(klusterletName, "HubConnectionDegraded", "HubConnectionFunctional", metav1.ConditionFalse)
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
	})

	It("Created klusterlet without managed cluster name", func() {
		clusterName = ""
		klusterletNamespace = ""
		var err error
		By(fmt.Sprintf("create klusterlet %v without managed cluster name", klusterletName))
		_, err = t.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace, operatorapiv1.InstallMode(klusterletDeployMode))
		Expect(err).ToNot(HaveOccurred())

		By("waiting for the managed cluster to be created")
		Eventually(func() error {
			clusterName, err = t.GetRandomClusterName()
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := t.checkKlusterletStatus(klusterletName, "HubConnectionDegraded", "BootstrapSecretFunctional,HubKubeConfigSecretMissing", metav1.ConditionTrue)
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

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := t.checkKlusterletStatus(klusterletName, "HubConnectionDegraded", "HubConnectionFunctional", metav1.ConditionFalse)
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
	})

	It("Update klusterlet CR namespace", func() {
		By(fmt.Sprintf("create klusterlet %v with managed cluster name %v", klusterletName, clusterName))
		_, err := t.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace, operatorapiv1.InstallMode(klusterletDeployMode))
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

		By("update klusterlet namespace")
		newNamespace := "open-cluster-management-agent-another"
		Eventually(func() error {
			klusterlet, err := t.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(), klusterletName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			klusterlet.Spec.Namespace = newNamespace
			_, err = t.OperatorClient.OperatorV1().Klusterlets().Update(context.TODO(), klusterlet, metav1.UpdateOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By("copy bootstrap secret to the new namespace")
		Eventually(func() error {
			secret := t.bootstrapHubSecret.DeepCopy()
			_, err = t.SpokeKubeClient.CoreV1().Secrets(newNamespace).Create(context.TODO(), secret, metav1.CreateOptions{})
			if errors.IsAlreadyExists(err) {
				return nil
			}
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By("old namespace should be removed")
		Eventually(func() error {
			_, err := t.SpokeKubeClient.CoreV1().Namespaces().Get(context.TODO(), klusterletNamespace, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("namespace still exists")
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By("addon namespace should be kept")
		Eventually(func() error {
			_, err := t.SpokeKubeClient.CoreV1().Namespaces().Get(context.TODO(), helpers.DefaultAddonNamespace, metav1.GetOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By(fmt.Sprintf("approve the managed cluster %v since it is registered in the new namespace", clusterName))
		Eventually(func() error {
			return t.ApproveCSR(clusterName)
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

		By("klusterlet status should be ok")
		Eventually(func() error {
			err := t.checkKlusterletStatus(klusterletName, "HubConnectionDegraded", "HubConnectionFunctional", metav1.ConditionFalse)
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By("Check manged cluster and klusterlet can be deleted", func() {
			Expect(t.cleanKlusterletResources(klusterletName, clusterName)).To(BeNil())
		})
	})
})
