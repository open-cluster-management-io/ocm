package integration

import (
	"context"
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var _ = ginkgo.Describe("Token Infrastructure Controller", func() {
	var managedClusterName, addOnName string
	var err error

	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("cluster-%s", suffix)
		addOnName = fmt.Sprintf("addon-%s", suffix)

		// Create managed cluster
		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// Create cluster namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
		}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	assertTokenInfrastructureReady := func(clusterName, addonName string) {
		ginkgo.By("Verify token infrastructure resources are created")
		serviceAccountName := fmt.Sprintf("%s-agent", addonName)
		roleName := fmt.Sprintf("%s-token-role", addonName)
		roleBindingName := fmt.Sprintf("%s-token-role", addonName)

		// Check ServiceAccount exists with correct labels
		gomega.Eventually(func() error {
			sa, err := hubKubeClient.CoreV1().ServiceAccounts(clusterName).Get(context.Background(), serviceAccountName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if sa.Labels["addon.open-cluster-management.io/token-infrastructure"] != "true" {
				return fmt.Errorf("ServiceAccount missing token-infrastructure label")
			}
			if sa.Labels["addon.open-cluster-management.io/name"] != addonName {
				return fmt.Errorf("ServiceAccount missing addon name label")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// Check Role exists with correct labels
		gomega.Eventually(func() error {
			role, err := hubKubeClient.RbacV1().Roles(clusterName).Get(context.Background(), roleName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if role.Labels["addon.open-cluster-management.io/token-infrastructure"] != "true" {
				return fmt.Errorf("Role missing token-infrastructure label")
			}
			if role.Labels["addon.open-cluster-management.io/name"] != addonName {
				return fmt.Errorf("Role missing addon name label")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// Check RoleBinding exists with correct labels
		gomega.Eventually(func() error {
			rb, err := hubKubeClient.RbacV1().RoleBindings(clusterName).Get(context.Background(), roleBindingName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if rb.Labels["addon.open-cluster-management.io/token-infrastructure"] != "true" {
				return fmt.Errorf("RoleBinding missing token-infrastructure label")
			}
			if rb.Labels["addon.open-cluster-management.io/name"] != addonName {
				return fmt.Errorf("RoleBinding missing addon name label")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		ginkgo.By("Verify TokenInfrastructureReady condition is set to True")
		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(context.Background(), addonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			cond := meta.FindStatusCondition(addon.Status.Conditions, "TokenInfrastructureReady")
			if cond == nil {
				return fmt.Errorf("TokenInfrastructureReady condition not found")
			}
			if cond.Status != metav1.ConditionTrue {
				return fmt.Errorf("TokenInfrastructureReady condition is not True: %s - %s", cond.Reason, cond.Message)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
	}

	assertTokenInfrastructureCleanedUp := func(clusterName, addonName string) {
		ginkgo.By("Verify token infrastructure resources are deleted")
		serviceAccountName := fmt.Sprintf("%s-agent", addonName)
		roleName := fmt.Sprintf("%s-token-role", addonName)
		roleBindingName := fmt.Sprintf("%s-token-role", addonName)

		// Check ServiceAccount is deleted
		gomega.Eventually(func() bool {
			_, err := hubKubeClient.CoreV1().ServiceAccounts(clusterName).Get(context.Background(), serviceAccountName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// Check Role is deleted
		gomega.Eventually(func() bool {
			_, err := hubKubeClient.RbacV1().Roles(clusterName).Get(context.Background(), roleName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// Check RoleBinding is deleted
		gomega.Eventually(func() bool {
			_, err := hubKubeClient.RbacV1().RoleBindings(clusterName).Get(context.Background(), roleBindingName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	ginkgo.It("should create token infrastructure when addon uses token driver", func() {
		ginkgo.By("Create ManagedClusterAddOn")
		addOn := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: addOnName,
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addOn, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Update addon status with kubeClient registration and token driver")
		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			addon.Status.Registrations = []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificates.KubeAPIServerClientSignerName,
				},
			}
			addon.Status.KubeClientDriver = "token"
			_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.Background(), addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		assertTokenInfrastructureReady(managedClusterName, addOnName)
	})

	ginkgo.It("should cleanup token infrastructure when addon switches from token to CSR driver", func() {
		ginkgo.By("Create ManagedClusterAddOn with token driver")
		addOn := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: addOnName,
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addOn, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			addon.Status.Registrations = []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificates.KubeAPIServerClientSignerName,
				},
			}
			addon.Status.KubeClientDriver = "token"
			_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.Background(), addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		assertTokenInfrastructureReady(managedClusterName, addOnName)

		ginkgo.By("Switch addon to CSR driver")
		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Update kubeClientDriver to switch to CSR driver
			addon.Status.KubeClientDriver = "csr"
			_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.Background(), addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		assertTokenInfrastructureCleanedUp(managedClusterName, addOnName)

		ginkgo.By("Verify TokenInfrastructureReady condition is removed")
		gomega.Eventually(func() bool {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addOnName, metav1.GetOptions{})
			if err != nil {
				return false
			}

			cond := meta.FindStatusCondition(addon.Status.Conditions, "TokenInfrastructureReady")
			return cond == nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	})

	ginkgo.It("should cleanup token infrastructure when addon is deleted", func() {
		ginkgo.By("Create ManagedClusterAddOn with token driver")
		addOn := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: addOnName,
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addOn, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			addon.Status.Registrations = []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificates.KubeAPIServerClientSignerName,
				},
			}
			addon.Status.KubeClientDriver = "token"
			_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.Background(), addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		assertTokenInfrastructureReady(managedClusterName, addOnName)

		ginkgo.By("Delete the addon")
		err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.Background(), addOnName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		assertTokenInfrastructureCleanedUp(managedClusterName, addOnName)
	})

	ginkgo.It("should handle addon with multiple registrations where only one is token-based", func() {
		ginkgo.By("Create ManagedClusterAddOn with multiple registrations including token driver")
		addOn := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: addOnName,
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addOn, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			addon.Status.Registrations = []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificates.KubeAPIServerClientSignerName,
				},
				{
					SignerName: "example.com/custom-signer",
				},
			}
			addon.Status.KubeClientDriver = "token"
			_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.Background(), addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		assertTokenInfrastructureReady(managedClusterName, addOnName)
	})
})
