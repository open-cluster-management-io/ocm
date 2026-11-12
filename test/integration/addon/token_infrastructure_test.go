package integration

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var _ = Describe("Token Infrastructure Controller Beta", func() {
	var managedClusterName, addOnName string
	var err error

	BeforeEach(func(ctx SpecContext) {
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
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// Create cluster namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
		}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func(ctx SpecContext) {
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(ctx, managedClusterName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		err = hubKubeClient.CoreV1().Namespaces().Delete(ctx, managedClusterName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	assertTokenInfrastructureReady := func(ctx context.Context, clusterName, addonName string) {
		By("Verify token infrastructure resources are created")
		serviceAccountName := fmt.Sprintf("%s-agent", addonName)
		roleName := fmt.Sprintf("%s-token-role", addonName)
		roleBindingName := fmt.Sprintf("%s-token-role", addonName)

		// Check ServiceAccount exists with correct labels
		Eventually(func() error {
			sa, err := hubKubeClient.CoreV1().ServiceAccounts(clusterName).Get(ctx, serviceAccountName, metav1.GetOptions{})
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
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// Check Role exists with correct labels
		Eventually(func() error {
			role, err := hubKubeClient.RbacV1().Roles(clusterName).Get(ctx, roleName, metav1.GetOptions{})
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
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// Check RoleBinding exists with correct labels
		Eventually(func() error {
			rb, err := hubKubeClient.RbacV1().RoleBindings(clusterName).Get(ctx, roleBindingName, metav1.GetOptions{})
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
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("Verify TokenInfrastructureReady condition is set to True")
		Eventually(func() error {
			addon, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(clusterName).Get(ctx, addonName, metav1.GetOptions{})
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
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	}

	assertTokenInfrastructureCleanedUp := func(ctx context.Context, clusterName, addonName string) {
		By("Verify token infrastructure resources are deleted")
		serviceAccountName := fmt.Sprintf("%s-agent", addonName)
		roleName := fmt.Sprintf("%s-token-role", addonName)
		roleBindingName := fmt.Sprintf("%s-token-role", addonName)

		// Check ServiceAccount is deleted
		Eventually(func() bool {
			_, err := hubKubeClient.CoreV1().ServiceAccounts(clusterName).Get(ctx, serviceAccountName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// Check Role is deleted
		Eventually(func() bool {
			_, err := hubKubeClient.RbacV1().Roles(clusterName).Get(ctx, roleName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// Check RoleBinding is deleted
		Eventually(func() bool {
			_, err := hubKubeClient.RbacV1().RoleBindings(clusterName).Get(ctx, roleBindingName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	It("should create token infrastructure when addon uses token driver", func(ctx SpecContext) {
		By("Create ManagedClusterAddOn")
		addOn := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{},
		}
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(ctx, addOn, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Update addon status with kubeClient registration and token driver")
		Eventually(func() error {
			addon, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			addon.Status.Registrations = []addonapiv1beta1.RegistrationConfig{
				{
					Type: "kubeClient",
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver: "token",
					},
				},
			}
			_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).UpdateStatus(ctx, addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		assertTokenInfrastructureReady(ctx, managedClusterName, addOnName)
	})

	It("should cleanup token infrastructure when addon switches from token to CSR driver", func(ctx SpecContext) {
		By("Create ManagedClusterAddOn with token driver")
		addOn := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{},
		}
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(ctx, addOn, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() error {
			addon, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			addon.Status.Registrations = []addonapiv1beta1.RegistrationConfig{
				{
					Type: "kubeClient",
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver: "token",
					},
				},
			}
			_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).UpdateStatus(ctx, addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		assertTokenInfrastructureReady(ctx, managedClusterName, addOnName)

		By("Switch addon to CSR driver")
		Eventually(func() error {
			addon, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Update kubeClient driver from "token" to "csr"
			addon.Status.Registrations = []addonapiv1beta1.RegistrationConfig{
				{
					Type: "kubeClient",
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver: "csr",
					},
				},
			}
			_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).UpdateStatus(ctx, addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		assertTokenInfrastructureCleanedUp(ctx, managedClusterName, addOnName)

		By("Verify TokenInfrastructureReady condition is removed")
		Eventually(func() bool {
			addon, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return false
			}

			cond := meta.FindStatusCondition(addon.Status.Conditions, "TokenInfrastructureReady")
			return cond == nil
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	})

	It("should cleanup token infrastructure when addon is deleted", func(ctx SpecContext) {
		By("Create ManagedClusterAddOn with token driver")
		addOn := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{},
		}
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(ctx, addOn, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() error {
			addon, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			addon.Status.Registrations = []addonapiv1beta1.RegistrationConfig{
				{
					Type: "kubeClient",
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver: "token",
					},
				},
			}
			_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).UpdateStatus(ctx, addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		assertTokenInfrastructureReady(ctx, managedClusterName, addOnName)

		By("Delete the addon")
		err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Delete(ctx, addOnName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		assertTokenInfrastructureCleanedUp(ctx, managedClusterName, addOnName)
	})

	It("should handle addon with multiple registrations where only one is token-based", func(ctx SpecContext) {
		By("Create ManagedClusterAddOn with multiple registrations including token driver")
		addOn := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{},
		}
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(ctx, addOn, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() error {
			addon, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			addon.Status.Registrations = []addonapiv1beta1.RegistrationConfig{
				{
					Type: "kubeClient",
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver: "token",
					},
				},
				{
					Type: "customSigner",
					CustomSigner: &addonapiv1beta1.CustomSignerConfig{
						SignerName: "example.com/custom-signer",
					},
				},
			}
			_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).UpdateStatus(ctx, addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		assertTokenInfrastructureReady(ctx, managedClusterName, addOnName)
	})
})
