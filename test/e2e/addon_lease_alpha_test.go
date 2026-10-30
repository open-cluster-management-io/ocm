package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/test/framework"
)

var _ = Describe("Addon Health Check (v1alpha1)", Label("addon-lease"), func() {
	const availableLabelValue = "available"
	Context("Checking addon lease on managed cluster to update addon status", func() {
		var addOnName string
		BeforeEach(func(ctx SpecContext) {
			// create an addon on created managed cluster
			addOnName = fmt.Sprintf("addon-%s", rand.String(6))
			By(fmt.Sprintf("Creating managed cluster addon %q", addOnName))
			err := hub.CreateManagedClusterAddOnV1Alpha1(universalClusterName, addOnName, addOnName)
			Expect(err).ToNot(HaveOccurred())

			// create addon installation namespace
			By(fmt.Sprintf("Creating managed cluster addon installation namespace %q", addOnName))
			_, err = spoke.KubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: addOnName,
				},
			}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func(ctx SpecContext) {
			By(fmt.Sprintf("Cleaning managed cluster addon installation namespace %q", addOnName))
			err := spoke.KubeClient.CoreV1().Namespaces().Delete(ctx, addOnName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should keep addon status to available", func(ctx SpecContext) {
			By(fmt.Sprintf("Creating lease %q for managed cluster addon %q", addOnName, addOnName))
			_, err := spoke.KubeClient.CoordinationV1().Leases(addOnName).Create(ctx, &coordv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      addOnName,
					Namespace: addOnName,
				},
				Spec: coordv1.LeaseSpec{
					RenewTime: &metav1.MicroTime{Time: time.Now()},
				},
			}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				found, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionTrue(found.Status.Conditions, "Available") {
					return fmt.Errorf("condition should be available, got %v", found.Status.Conditions)
				}
				return nil
			}).Should(Succeed())

			// check if the cluster has a label for addon with expected value
			Eventually(func() bool {
				cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(ctx, universalClusterName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if len(cluster.Labels) == 0 {
					return false
				}
				key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addOnName)
				return cluster.Labels[key] == availableLabelValue
			}).Should(BeTrue())
		})

		It("Should update addon status to unavailable if addon stops to update its lease", func(ctx SpecContext) {
			By(fmt.Sprintf("Creating lease %q for managed cluster addon %q", addOnName, addOnName))
			_, err := spoke.KubeClient.CoordinationV1().Leases(addOnName).Create(ctx, &coordv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      addOnName,
					Namespace: addOnName,
				},
				Spec: coordv1.LeaseSpec{
					RenewTime: &metav1.MicroTime{Time: time.Now()},
				},
			}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				found, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionTrue(found.Status.Conditions, "Available") {
					return fmt.Errorf("condition should be available")
				}
				return nil
			}).Should(Succeed())

			// check if the cluster has a label for addon with expected value
			Eventually(func() bool {
				cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(ctx, universalClusterName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if len(cluster.Labels) == 0 {
					return false
				}
				key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addOnName)
				return cluster.Labels[key] == availableLabelValue
			}).Should(BeTrue())

			By(fmt.Sprintf("Updating lease %q with a past time", addOnName))
			lease, err := spoke.KubeClient.CoordinationV1().Leases(addOnName).Get(ctx, addOnName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			lease.Spec.RenewTime = &metav1.MicroTime{Time: time.Now().Add(-10 * time.Minute)}
			_, err = spoke.KubeClient.CoordinationV1().Leases(addOnName).Update(ctx, lease, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				found, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionFalse(found.Status.Conditions, "Available") {
					return fmt.Errorf("condition should be available")
				}
				return nil
			}).Should(Succeed())

			// check if the cluster has a label for addon with expected value
			Eventually(func() bool {
				cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(ctx, universalClusterName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if len(cluster.Labels) == 0 {
					return false
				}
				key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addOnName)
				return cluster.Labels[key] == "unhealthy"
			}).Should(BeTrue())
		})

		It("Should update addon status to unknown if there is no lease for this addon", func(ctx SpecContext) {
			By(fmt.Sprintf("Creating lease %q for managed cluster addon %q", addOnName, addOnName))
			_, err := spoke.KubeClient.CoordinationV1().Leases(addOnName).Create(ctx, &coordv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      addOnName,
					Namespace: addOnName,
				},
				Spec: coordv1.LeaseSpec{
					RenewTime: &metav1.MicroTime{Time: time.Now()},
				},
			}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				found, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionTrue(found.Status.Conditions, "Available") {
					return fmt.Errorf("condition should be available")
				}
				return nil
			}).Should(Succeed())

			// check if the cluster has a label for addon with expected value
			Eventually(func() bool {
				cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(ctx, universalClusterName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if len(cluster.Labels) == 0 {
					return false
				}
				key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addOnName)
				return cluster.Labels[key] == availableLabelValue
			}).Should(BeTrue())

			By(fmt.Sprintf("Deleting lease %q", addOnName))
			err = spoke.KubeClient.CoordinationV1().Leases(addOnName).Delete(ctx, addOnName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				found, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionTrue(found.Status.Conditions, "Available") {
					return fmt.Errorf("condition should be available")
				}
				return nil
			}).Should(Succeed())

			// check if the cluster has a label for addon with expected value
			Eventually(func() bool {
				cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(ctx, universalClusterName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if len(cluster.Labels) == 0 {
					return false
				}
				key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addOnName)
				return cluster.Labels[key] == "unreachable"
			}).Should(BeTrue())
		})
	})

	Context("Checking managed cluster status to update addon status", func() {
		var klusterletName, clusterName, addOnName string
		BeforeEach(func(ctx SpecContext) {
			klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
			clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
			agentNamespace := fmt.Sprintf("open-cluster-management-agent-%s", rand.String(6))
			framework.CreateAndApproveKlusterlet(
				hub, spoke,
				klusterletName, clusterName, agentNamespace, operatorapiv1.InstallMode(klusterletDeployMode), bootstrapHubKubeConfigSecret, images, registrationDriver)
			// create an addon on created managed cluster
			addOnName = fmt.Sprintf("addon-%s", rand.String(6))
			By(fmt.Sprintf("Creating managed cluster addon %q", addOnName))
			Expect(hub.CreateManagedClusterAddOnV1Alpha1(clusterName, addOnName, addOnName)).ToNot(HaveOccurred())

			// create addon installation namespace
			By(fmt.Sprintf("Creating managed cluster addon installation namespace %q", addOnName))
			_, err := spoke.KubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: addOnName,
				},
			}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func(ctx SpecContext) {
			By(fmt.Sprintf("Cleaning managed cluster addon installation namespace %q", addOnName))
			err := hub.KubeClient.CoreV1().Namespaces().Delete(ctx, addOnName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
			By(fmt.Sprintf("clean klusterlet %v resources after the test case", klusterletName))
			framework.CleanKlusterletRelatedResources(hub, spoke, klusterletName, clusterName)
		})

		It("Should update addon status to unknown if managed cluster stops to update its lease", func(ctx SpecContext) {
			By(fmt.Sprintf("Creating lease %q for managed cluster addon %q", addOnName, addOnName))
			_, err := spoke.KubeClient.CoordinationV1().Leases(addOnName).Create(ctx, &coordv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      addOnName,
					Namespace: addOnName,
				},
				Spec: coordv1.LeaseSpec{
					RenewTime: &metav1.MicroTime{Time: time.Now()},
				},
			}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				found, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if found.Status.Conditions == nil {
					return fmt.Errorf("condition should not be nil")
				}
				cond := meta.FindStatusCondition(found.Status.Conditions, "Available")
				if cond.Status != metav1.ConditionTrue {
					return fmt.Errorf("available status should be true")
				}
				return nil
			}).Should(Succeed())

			// delete registration agent to stop agent update its status
			By("Stoping klusterlet")
			err = spoke.OperatorClient.OperatorV1().Klusterlets().Delete(ctx, klusterletName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				_, err := spoke.OperatorClient.OperatorV1().Klusterlets().Get(ctx, klusterletName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					klog.Infof("klusterlet %s deleted successfully", klusterletName)
					return nil
				}
				if err != nil {
					klog.Infof("get klusterlet %s error: %v", klusterletName, err)
					return err
				}
				return fmt.Errorf("klusterlet is still deleting")
			}).Should(Succeed())

			// for speeding up test, update managed cluster status to unknown manually
			By(fmt.Sprintf("Updating managed cluster %s status to unknown", clusterName))
			found, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(ctx, clusterName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			found.Status = clusterv1.ManagedClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               clusterv1.ManagedClusterConditionAvailable,
						Status:             metav1.ConditionUnknown,
						Reason:             "ManagedClusterLeaseUpdateStopped",
						Message:            "Registration agent stopped updating its lease.",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			_, err = hub.ClusterClient.ClusterV1().ManagedClusters().UpdateStatus(ctx, found, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				found, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if found.Status.Conditions == nil {
					return fmt.Errorf("condition should not be nil")
				}
				cond := meta.FindStatusCondition(found.Status.Conditions, "Available")
				if cond.Status != metav1.ConditionUnknown {
					return fmt.Errorf("available status should be unknown")
				}
				return nil
			}).Should(Succeed())
		})
	})
})
