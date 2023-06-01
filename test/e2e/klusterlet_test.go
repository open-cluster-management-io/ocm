package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/rand"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
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

	AfterEach(func() {
		By(fmt.Sprintf("clean klusterlet %v resources after the test case", klusterletName))
		Expect(t.cleanKlusterletResources(klusterletName, clusterName)).To(BeNil())
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
		_, err := t.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace, operatorapiv1.InstallModeDefault)
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
		_, err = t.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace, operatorapiv1.InstallModeDefault)
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

	It("Create klusterlet CR in Hosted mode", func() {
		By(fmt.Sprintf("create klusterlet %v with managed cluster name %v", klusterletName, clusterName))
		_, err := t.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace, operatorapiv1.InstallModeHosted)
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
})

var _ = Describe("Delete klusterlet CR", func() {
	var klusterletName string
	var clusterName string
	var klusterletNamespace string

	BeforeEach(func() {
		klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
		clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
		klusterletNamespace = fmt.Sprintf("open-cluster-management-agent-%s", rand.String(6))
	})

	It("Delete klusterlet CR in Hosted mode without external managed kubeconfig", func() {
		By(fmt.Sprintf("create klusterlet %v with managed cluster name %v in Hosted mode", klusterletName, clusterName))
		_, err := t.CreatePureHostedKlusterlet(klusterletName, clusterName)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := t.checkKlusterletStatus(klusterletName, "ReadyToApply", "KlusterletPrepareFailed", metav1.ConditionFalse)
			return err
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

		By(fmt.Sprintf("delete the klusterlet %s", klusterletName))
		err = t.OperatorClient.OperatorV1().Klusterlets().Delete(context.TODO(),
			klusterletName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("check klusterlet %s was deleted", klusterletName))
		Eventually(func() error {
			_, err := t.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(),
				klusterletName, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("klusterlet still exists")
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())

		By(fmt.Sprintf("check the agent namespace %s on the management cluster was deleted", klusterletName))
		Eventually(func() error {
			_, err := t.HubKubeClient.CoreV1().Namespaces().Get(context.TODO(),
				klusterletName, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("klusterlet namespace still exists")
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(Succeed())
	})

	It("Delete klusterlet CR in Hosted mode when the managed cluster was destroyed", func() {
		By(fmt.Sprintf("create klusterlet %v with managed cluster name %v", klusterletName, clusterName))
		klusterlet, err := t.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace, operatorapiv1.InstallModeHosted)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("waiting for the managed cluster %v to be created", clusterName))
		Eventually(func() error {
			_, err := t.GetCreatedManagedCluster(clusterName)
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := t.checkKlusterletStatus(klusterletName, "HubConnectionDegraded",
				"BootstrapSecretFunctional,HubKubeConfigSecretMissing", metav1.ConditionTrue)
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
			err := t.checkKlusterletStatus(klusterletName, "HubConnectionDegraded",
				"HubConnectionFunctional", metav1.ConditionFalse)
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		// change the kubeconfig host of external managed kubeconfig secret to a wrong value
		// to simulate the managed cluster was destroyed
		By("Delete external managed kubeconfig", func() {
			err = t.DeleteExternalKubeconfigSecret(klusterlet)
			Expect(err).ToNot(HaveOccurred())
		})

		By("Delete managed cluster", func() {
			// clean the managed clusters
			err = t.ClusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(),
				clusterName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		By("Delete klusterlet", func() {
			// clean the klusterlets
			err = t.OperatorClient.OperatorV1().Klusterlets().Delete(context.TODO(),
				klusterletName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		By("Create a fake external managed kubeconfig", func() {
			err = t.CreateFakeExternalKubeconfigSecret(klusterlet)
			Expect(err).ToNot(HaveOccurred())
		})

		// in the future, if the eviction can be configured, we can set a short timeout period and
		// remove the wait and update parts
		evictionTimestampAnno := "operator.open-cluster-management.io/managed-resources-eviction-timestamp"
		By("Wait for the eviction timestamp annotation", func() {
			Eventually(func() error {
				k, err := t.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(),
					klusterletName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, ok := k.Annotations[evictionTimestampAnno]
				if !ok {
					return fmt.Errorf("expected annotation %s does not exist", evictionTimestampAnno)
				}
				return nil
			}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
		})

		time.Sleep(3 * time.Second) // after the eviction timestamp exists, wait 3 seconds for cache syncing
		By("Update the eviction timestamp annotation", func() {
			Eventually(func() error {
				k, err := t.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(),
					klusterletName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				ta := time.Now().Add(-6 * time.Minute).Format(time.RFC3339)
				By(fmt.Sprintf("add time %v anno for klusterlet %s", ta, klusterletName))
				k.Annotations[evictionTimestampAnno] = ta
				_, err = t.OperatorClient.OperatorV1().Klusterlets().Update(context.TODO(),
					k, metav1.UpdateOptions{})
				return err
			}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
		})

		By("Check manged cluster and klusterlet can be deleted", func() {
			Expect(t.cleanKlusterletResources(klusterletName, clusterName)).To(BeNil())
		})
	})
})
