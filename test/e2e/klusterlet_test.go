package e2e

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/test/framework"
)

var _ = Describe("Create klusterlet CR", Label("klusterlet"), func() {
	// Skip entire suite if using grpc driver
	if registrationDriver == "grpc" {
		// TODO enable this test after https://github.com/open-cluster-management-io/ocm/issues/1246 is resolved
		klog.Infof("Skip the klusterlet test when registrationDriver is grpc")
		return
	}

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
		framework.CleanKlusterletRelatedResources(hub, spoke, klusterletName, clusterName)
	})

	// This test case is helpful for the Backward compatibility
	It("Create klusterlet CR with install mode empty", func() {
		By(fmt.Sprintf("create klusterlet %v with managed cluster name %v", klusterletName, clusterName))
		// Set install mode empty
		_, err := spoke.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace,
			"", bootstrapHubKubeConfigSecret, images, registrationDriver)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("waiting for the managed cluster %v to be created", clusterName))
		Eventually(func() error {
			_, err := hub.GetManagedCluster(clusterName)
			return err
		}).Should(Succeed())

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := spoke.CheckKlusterletStatus(klusterletName, "HubConnectionDegraded",
				"BootstrapSecretFunctional,HubKubeConfigSecretMissing", metav1.ConditionTrue)
			return err
		}).Should(Succeed())

		By(fmt.Sprintf("approve the created managed cluster %v", clusterName))
		Eventually(func() error {
			return hub.ApproveManagedClusterCSR(clusterName)
		}).Should(Succeed())

		By(fmt.Sprintf("accept the created managed cluster %v", clusterName))
		Eventually(func() error {
			return hub.AcceptManageCluster(clusterName)
		}).Should(Succeed())

		By(fmt.Sprintf("waiting for the managed cluster %v to be ready", clusterName))
		Eventually(func() error {
			return hub.CheckManagedClusterStatus(clusterName)
		}).Should(Succeed())

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := spoke.CheckKlusterletStatus(klusterletName, "HubConnectionDegraded",
				"HubConnectionFunctional", metav1.ConditionFalse)
			return err
		}).Should(Succeed())
	})

	It("Create klusterlet CR with managed cluster name", func() {
		By(fmt.Sprintf("create klusterlet %v with managed cluster name %v", klusterletName, clusterName))
		_, err := spoke.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace,
			operatorapiv1.InstallMode(klusterletDeployMode), bootstrapHubKubeConfigSecret, images, registrationDriver)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("waiting for the managed cluster %v to be created", clusterName))
		Eventually(func() error {
			_, err := hub.GetManagedCluster(clusterName)
			return err
		}).Should(Succeed())

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := spoke.CheckKlusterletStatus(klusterletName, "HubConnectionDegraded",
				"BootstrapSecretFunctional,HubKubeConfigSecretMissing", metav1.ConditionTrue)
			return err
		}).Should(Succeed())

		By(fmt.Sprintf("approve the created managed cluster %v", clusterName))
		Eventually(func() error {
			return hub.ApproveManagedClusterCSR(clusterName)
		}).Should(Succeed())

		By(fmt.Sprintf("accept the created managed cluster %v", clusterName))
		Eventually(func() error {
			return hub.AcceptManageCluster(clusterName)
		}).Should(Succeed())

		By(fmt.Sprintf("waiting for the managed cluster %v to be ready", clusterName))
		Eventually(func() error {
			return hub.CheckManagedClusterStatus(clusterName)
		}).Should(Succeed())

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := spoke.CheckKlusterletStatus(klusterletName, "HubConnectionDegraded",
				"HubConnectionFunctional", metav1.ConditionFalse)
			return err
		}).Should(Succeed())
	})

	It("Created klusterlet without managed cluster name", func() {
		clusterName = ""
		klusterletNamespace = ""
		var err error
		By(fmt.Sprintf("create klusterlet %v without managed cluster name", klusterletName))
		_, err = spoke.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace,
			operatorapiv1.InstallMode(klusterletDeployMode), bootstrapHubKubeConfigSecret, images, registrationDriver)
		Expect(err).ToNot(HaveOccurred())

		By("waiting for the managed cluster to be created")
		Eventually(func() error {
			// GetRandomClusterName gets the clusterName generated by registration randomly.
			// the cluster name is the random name if it has not prefix "e2e-".
			// TODO: get random cluster name from event
			managedClusterList, err := hub.ClusterClient.ClusterV1().ManagedClusters().List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, managedCluster := range managedClusterList.Items {
				if !strings.HasPrefix(managedCluster.Name, "e2e-") {
					clusterName = managedCluster.Name
					return nil
				}
			}
			return fmt.Errorf("there is no managedCluster with the random name")
		}).Should(Succeed())

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			return spoke.CheckKlusterletStatus(klusterletName, "HubConnectionDegraded",
				"BootstrapSecretFunctional,HubKubeConfigSecretMissing", metav1.ConditionTrue)
		}).Should(Succeed())

		By(fmt.Sprintf("approve the created managed cluster %v", clusterName))
		Eventually(func() error {
			return hub.ApproveManagedClusterCSR(clusterName)
		}).Should(Succeed())

		By(fmt.Sprintf("accept the created managed cluster %v", clusterName))
		Eventually(func() error {
			return hub.AcceptManageCluster(clusterName)
		}).Should(Succeed())

		By(fmt.Sprintf("waiting for the managed cluster %v to be ready", clusterName))
		Eventually(func() error {
			return hub.CheckManagedClusterStatus(clusterName)
		}).Should(Succeed())

		By(fmt.Sprintf("check klusterlet %s status", klusterletName))
		Eventually(func() error {
			err := spoke.CheckKlusterletStatus(klusterletName, "HubConnectionDegraded",
				"HubConnectionFunctional", metav1.ConditionFalse)
			return err
		}).Should(Succeed())
	})

	It("Update klusterlet CR namespace", func() {
		By(fmt.Sprintf("create klusterlet %v with managed cluster name %v", klusterletName, clusterName))
		_, err := spoke.CreateKlusterlet(klusterletName, clusterName, klusterletNamespace,
			operatorapiv1.InstallMode(klusterletDeployMode), bootstrapHubKubeConfigSecret, images, registrationDriver)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("waiting for the managed cluster %v to be created", clusterName))
		Eventually(func() error {
			_, err := hub.GetManagedCluster(clusterName)
			return err
		}).Should(Succeed())

		By(fmt.Sprintf("approve the created managed cluster %v", clusterName))
		Eventually(func() error {
			return hub.ApproveManagedClusterCSR(clusterName)
		}).Should(Succeed())

		By(fmt.Sprintf("accept the created managed cluster %v", clusterName))
		Eventually(func() error {
			return hub.AcceptManageCluster(clusterName)
		}).Should(Succeed())

		By(fmt.Sprintf("waiting for the managed cluster %v to be ready", clusterName))
		Eventually(func() error {
			return hub.CheckManagedClusterStatus(clusterName)
		}).Should(Succeed())

		By("update klusterlet namespace")
		newNamespace := "open-cluster-management-agent-another"
		Eventually(func() error {
			klusterlet, err := spoke.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(), klusterletName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			klusterlet.Spec.Namespace = newNamespace
			_, err = spoke.OperatorClient.OperatorV1().Klusterlets().Update(context.TODO(), klusterlet, metav1.UpdateOptions{})
			return err
		}).Should(Succeed())

		By("copy bootstrap secret to the new namespace")
		Eventually(func() error {
			secret := bootstrapHubKubeConfigSecret.DeepCopy()
			_, err = spoke.KubeClient.CoreV1().Secrets(newNamespace).Create(context.TODO(), secret, metav1.CreateOptions{})
			if errors.IsAlreadyExists(err) {
				return nil
			}
			return err
		}).Should(Succeed())

		By("old namespace should be removed")
		Eventually(func() error {
			_, err := spoke.KubeClient.CoreV1().Namespaces().Get(context.TODO(), klusterletNamespace, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("namespace still exists")
		}).Should(Succeed())

		By("addon namespace should be kept")
		Eventually(func() error {
			_, err := spoke.KubeClient.CoreV1().Namespaces().Get(context.TODO(), helpers.DefaultAddonNamespace, metav1.GetOptions{})
			return err
		}).Should(Succeed())

		By(fmt.Sprintf("approve the managed cluster %v since it is registered in the new namespace", clusterName))
		Eventually(func() error {
			return hub.ApproveManagedClusterCSR(clusterName)
		}).Should(Succeed())

		By("klusterlet status should be ok")
		Eventually(func() error {
			err := spoke.CheckKlusterletStatus(klusterletName, "HubConnectionDegraded", "HubConnectionFunctional", metav1.ConditionFalse)
			return err
		}).Should(Succeed())
	})
})
