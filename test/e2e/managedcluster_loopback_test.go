package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	certificatesv1 "k8s.io/api/certificates/v1"
	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"

	"open-cluster-management.io/ocm/pkg/registration/helpers"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

var _ = ginkgo.Describe("Loopback registration [development]", func() {
	var clusterId string

	ginkgo.BeforeEach(func() {
		// create ClusterClaim cr
		clusterId = rand.String(12)
		claim := &clusterv1alpha1.ClusterClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "id.k8s.io",
			},
			Spec: clusterv1alpha1.ClusterClaimSpec{
				Value: clusterId,
			},
		}
		// create the claim
		gomega.Eventually(func() error {
			existing, err := hub.ClusterClient.ClusterV1alpha1().ClusterClaims().Get(context.TODO(), claim.Name, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				_, err = hub.ClusterClient.ClusterV1alpha1().ClusterClaims().Create(context.TODO(), claim, metav1.CreateOptions{})
				return err
			}
			if err != nil {
				return err
			}
			if existing.Spec.Value != clusterId {
				existing.Spec.Value = clusterId
				_, err = hub.ClusterClient.ClusterV1alpha1().ClusterClaims().Update(context.TODO(), existing, metav1.UpdateOptions{})
				return err
			}
			return nil
		}).Should(gomega.Succeed())
	})

	ginkgo.It("Should register the hub as a managed cluster", func() {
		var (
			err    error
			suffix = rand.String(6)
			nsName = fmt.Sprintf("loopback-spoke-%v", suffix)
		)
		ginkgo.By(fmt.Sprintf("Deploying the agent using suffix=%q ns=%q", suffix, nsName))
		var (
			managedCluster  *clusterv1.ManagedCluster
			managedClusters = hub.ClusterClient.ClusterV1().ManagedClusters()
		)

		ginkgo.By(fmt.Sprintf("Waiting for ManagedCluster %q to exist", universalClusterName))
		gomega.Eventually(func() error {
			var err error
			managedCluster, err = managedClusters.Get(context.TODO(), universalClusterName, metav1.GetOptions{})
			return err
		}).Should(gomega.Succeed())

		ginkgo.By("Waiting for ManagedCluster to have HubAccepted=true")
		gomega.Eventually(func() error {
			var err error
			managedCluster, err := managedClusters.Get(context.TODO(), universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
				return nil
			}

			return fmt.Errorf("ManagedCluster %q is not yet HubAccepted", universalClusterName)
		}).Should(gomega.Succeed())

		ginkgo.By("Waiting for ManagedCluster to join the hub cluser")
		gomega.Eventually(func() error {
			var err error
			managedCluster, err := managedClusters.Get(context.TODO(), universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
				return fmt.Errorf("ManagedCluster %q is not joined", universalClusterName)
			}
			return nil
		}).Should(gomega.Succeed())

		ginkgo.By("Waiting for ManagedCluster available")
		gomega.Eventually(func() error {
			managedCluster, err := managedClusters.Get(context.TODO(), universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable) {
				return nil
			}
			return fmt.Errorf("ManagedCluster %q is not available", universalClusterName)
		}).Should(gomega.Succeed())

		leaseName := "managed-cluster-lease"
		ginkgo.By(fmt.Sprintf("Make sure ManagedCluster lease %q exists", leaseName))
		var lastRenewTime *metav1.MicroTime
		gomega.Eventually(func() error {
			lease, err := hub.KubeClient.CoordinationV1().Leases(universalClusterName).Get(context.TODO(), leaseName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if lease.Spec.RenewTime == nil {
				return fmt.Errorf("ManagedCluster lease %q RenewTime is nil", leaseName)
			}
			lastRenewTime = lease.Spec.RenewTime
			return nil
		}).Should(gomega.Succeed())

		ginkgo.By(fmt.Sprintf("Make sure ManagedCluster lease %q is updated", leaseName))
		gomega.Eventually(func() error {
			lease, err := hub.KubeClient.CoordinationV1().Leases(universalClusterName).Get(context.TODO(), leaseName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if lastRenewTime == nil || lease.Spec.RenewTime == nil {
				return fmt.Errorf("ManagedCluster lease %q RenewTime is nil", leaseName)
			}
			leaseUpdated := lastRenewTime.Before(lease.Spec.RenewTime)
			if leaseUpdated {
				lastRenewTime = lease.Spec.RenewTime
			} else {
				return fmt.Errorf("ManagedCluster lease %q is not updated", leaseName)
			}
			return nil
		}).Should(gomega.Succeed())

		ginkgo.By(fmt.Sprintf("Make sure ManagedCluster lease %q is updated again", leaseName))
		gomega.Eventually(func() error {
			lease, err := hub.KubeClient.CoordinationV1().Leases(universalClusterName).Get(context.TODO(), leaseName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if lastRenewTime == nil || lease.Spec.RenewTime == nil {
				return fmt.Errorf("ManagedCluster lease %q RenewTime is nil", leaseName)
			}
			if lastRenewTime.Before(lease.Spec.RenewTime) {
				return nil
			}
			return fmt.Errorf("ManagedCluster lease %q is not updated", leaseName)
		}).Should(gomega.Succeed())

		ginkgo.By("Make sure ManagedCluster is still available")
		gomega.Eventually(func() error {
			managedCluster, err := managedClusters.Get(context.TODO(), universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable) {
				return nil
			}
			return fmt.Errorf("ManagedCluster %q is not available", universalClusterName)
		}).Should(gomega.Succeed())

		// make sure the cpu and memory are still in the status, for compatibility
		ginkgo.By("Make sure cpu and memory exist in status")
		gomega.Eventually(func() error {
			managedCluster, err := managedClusters.Get(context.TODO(), universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if _, exist := managedCluster.Status.Allocatable[clusterv1.ResourceCPU]; !exist {
				return fmt.Errorf("Resource %v doesn't exist in Allocatable", clusterv1.ResourceCPU)
			}

			if _, exist := managedCluster.Status.Allocatable[clusterv1.ResourceMemory]; !exist {
				return fmt.Errorf("Resource %v doesn't exist in Allocatable", clusterv1.ResourceMemory)
			}

			if _, exist := managedCluster.Status.Capacity[clusterv1.ResourceCPU]; !exist {
				return fmt.Errorf("Resource %v doesn't exist in Capacity", clusterv1.ResourceCPU)
			}

			if _, exist := managedCluster.Status.Capacity[clusterv1.ResourceMemory]; !exist {
				return fmt.Errorf("Resource %v doesn't exist in Capacity", clusterv1.ResourceMemory)
			}

			return nil
		}).Should(gomega.Succeed())

		ginkgo.By("Make sure ClusterClaims are synced")
		gomega.Eventually(func() error {
			mc, err := managedClusters.Get(context.TODO(), universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			found := false
			for _, c := range mc.Status.ClusterClaims {
				if c.Name == "id.k8s.io" && c.Value == clusterId {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("expected claim id.k8s.io=%s not found; got: %+v", clusterId, mc.Status.ClusterClaims)
			}
			return nil
		}).Should(gomega.Succeed())

		ginkgo.By("Create addon on hub")
		addOnName := fmt.Sprintf("loopback-e2e-addon-%v", suffix)
		// create namespace for addon on spoke
		addOnNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnName,
			},
		}
		_, err = spoke.KubeClient.CoreV1().Namespaces().Create(context.TODO(), addOnNs, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// create an addon
		addOn := &addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: universalClusterName,
			},
			Spec: addonv1alpha1.ManagedClusterAddOnSpec{},
		}
		_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Create(context.TODO(), addOn, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			created, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			created.Status = addonv1alpha1.ManagedClusterAddOnStatus{
				Registrations: []addonv1alpha1.RegistrationConfig{
					{
						SignerName: certificates.KubeAPIServerClientSignerName,
					},
				},
				Namespace: addOnName,
			}
			_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).UpdateStatus(context.TODO(), created, metav1.UpdateOptions{})
			return err
		}).Should(gomega.Succeed())

		var (
			csrs      *certificatesv1.CertificateSigningRequestList
			csrClient = hub.KubeClient.CertificatesV1().CertificateSigningRequests()
		)

		ginkgo.By(fmt.Sprintf("Waiting for the CSR for addOn %q to exist", addOnName))
		gomega.Eventually(func() error {
			var err error
			csrs, err = csrClient.List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name=%s,open-cluster-management.io/addon-name=%s", universalClusterName, addOnName),
			})
			if err != nil {
				return err
			}

			if len(csrs.Items) >= 1 {
				return nil
			}

			return fmt.Errorf("csr is not created for cluster %s", universalClusterName)
		}).Should(gomega.Succeed())
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Approving all pending CSRs")
		for i := range csrs.Items {
			csr := &csrs.Items[i]

			gomega.Eventually(func() error {
				csr, err = csrClient.Get(context.TODO(), csr.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if helpers.IsCSRInTerminalState(&csr.Status) {
					return nil
				}

				csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
					Type:    certificatesv1.CertificateApproved,
					Status:  corev1.ConditionTrue,
					Reason:  "Approved by E2E",
					Message: "Approved as part of Loopback e2e",
				})
				_, err := csrClient.UpdateApproval(context.TODO(), csr.Name, csr, metav1.UpdateOptions{})
				return err
			}).Should(gomega.Succeed())
		}

		ginkgo.By("Check addon client certificate in secret")
		secretName := fmt.Sprintf("%s-hub-kubeconfig", addOnName)
		gomega.Eventually(func() error {
			secret, err := spoke.KubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if _, ok := secret.Data[csr.TLSKeyFile]; !ok {
				return fmt.Errorf("secret %s/%s does not have a TLS key", addOnName, secretName)
			}
			if _, ok := secret.Data[csr.TLSCertFile]; !ok {
				return fmt.Errorf("secret %s/%s does not have a TLS certificate", addOnName, secretName)
			}
			if _, ok := secret.Data[register.KubeconfigFile]; !ok {
				return fmt.Errorf("secret %s/%s does not have a kubeconfig", addOnName, secretName)
			}
			return nil
		}).Should(gomega.Succeed())

		ginkgo.By("Check addon status")
		gomega.Eventually(func() error {
			found, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.TODO(), addOn.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(found.Status.Conditions, csr.ClusterCertificateRotatedCondition) {
				return fmt.Errorf("Client cert condition is not correct")
			}

			return nil
		}).Should(gomega.Succeed())

		ginkgo.By("Delete the addon and check if secret is gone")
		err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() bool {
			_, err = spoke.KubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}).Should(gomega.BeTrue())

		ginkgo.By(fmt.Sprintf("Cleaning managed cluster addon installation namespace %q", addOnName))
		err = spoke.KubeClient.CoreV1().Namespaces().Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
})
