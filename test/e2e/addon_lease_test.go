package e2e

import (
	"context"
	"fmt"
	"time"

	ginkgo "github.com/onsi/ginkgo"
	gomega "github.com/onsi/gomega"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
)

var _ = ginkgo.Describe("Addon Health Check", func() {
	ginkgo.Context("Checking addon lease on managed cluster to update addon status", func() {
		var (
			err            error
			suffix         string
			managedCluster *clusterv1.ManagedCluster
			addOn          *addonv1alpha1.ManagedClusterAddOn
			u              *utilClients
		)

		ginkgo.BeforeEach(func() {
			suffix = rand.String(6)
			u = &utilClients{
				hubClient:         hubClient,
				hubDynamicClient:  hubDynamicClient,
				hubAddOnClient:    hubAddOnClient,
				clusterClient:     clusterClient,
				registrationImage: registrationImage,
			}

			// create a managed cluster
			clusterName := fmt.Sprintf("loopback-e2e-%v", suffix)
			ginkgo.By(fmt.Sprintf("Creating managed cluster %q", clusterName))
			managedCluster, err = u.createManagedCluster(clusterName, suffix)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// create an addon on created managed cluster
			addOnName := fmt.Sprintf("addon-%s", suffix)
			ginkgo.By(fmt.Sprintf("Creating managed cluster addon %q", addOnName))
			addOn, err = u.createManagedClusterAddOn(managedCluster, addOnName)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// create addon installation namespace
			ginkgo.By(fmt.Sprintf("Creating managed cluster addon installation namespace %q", addOnName))
			_, err := hubClient.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: addOn.Name,
				},
			}, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			clusterName := fmt.Sprintf("loopback-e2e-%v", suffix)
			ginkgo.By(fmt.Sprintf("Cleaning managed cluster %q", clusterName))
			err = u.cleanupManagedCluster(clusterName, suffix)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			addOnName := fmt.Sprintf("addon-%s", suffix)
			ginkgo.By(fmt.Sprintf("Cleaning managed cluster addon installation namespace %q", addOnName))
			err = hubClient.CoreV1().Namespaces().Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("Should keep addon status to available", func() {
			ginkgo.By(fmt.Sprintf("Creating lease %q for managed cluster addon %q", addOn.Name, addOn.Name))
			_, err := hubClient.CoordinationV1().Leases(addOn.Name).Create(context.TODO(), &coordv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      addOn.Name,
					Namespace: addOn.Name,
				},
				Spec: coordv1.LeaseSpec{
					RenewTime: &metav1.MicroTime{Time: time.Now()},
				},
			}, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			err = wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
				found, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.TODO(), addOn.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}

				if !meta.IsStatusConditionTrue(found.Status.Conditions, "Available") {
					return false, nil
				}

				return true, nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check if the cluster has a label for addon with expected value
			err = wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
				cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), managedCluster.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if len(cluster.Labels) == 0 {
					return false, nil
				}
				key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addOn.Name)
				return cluster.Labels[key] == "available", nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("Should update addon status to unavailable if addon stops to update its lease", func() {
			ginkgo.By(fmt.Sprintf("Creating lease %q for managed cluster addon %q", addOn.Name, addOn.Name))
			_, err := hubClient.CoordinationV1().Leases(addOn.Name).Create(context.TODO(), &coordv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      addOn.Name,
					Namespace: addOn.Name,
				},
				Spec: coordv1.LeaseSpec{
					RenewTime: &metav1.MicroTime{Time: time.Now()},
				},
			}, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			err = wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
				found, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.TODO(), addOn.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if found.Status.Conditions == nil {
					return false, nil
				}
				cond := meta.FindStatusCondition(found.Status.Conditions, "Available")
				return cond.Status == metav1.ConditionTrue, nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check if the cluster has a label for addon with expected value
			err = wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
				cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), managedCluster.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if len(cluster.Labels) == 0 {
					return false, nil
				}
				key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addOn.Name)
				return cluster.Labels[key] == "available", nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By(fmt.Sprintf("Updating lease %q with a past time", addOn.Name))
			lease, err := hubClient.CoordinationV1().Leases(addOn.Name).Get(context.TODO(), addOn.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			lease.Spec.RenewTime = &metav1.MicroTime{Time: time.Now().Add(-10 * time.Minute)}
			_, err = hubClient.CoordinationV1().Leases(addOn.Name).Update(context.TODO(), lease, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			err = wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
				found, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.TODO(), addOn.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if found.Status.Conditions == nil {
					return false, nil
				}
				cond := meta.FindStatusCondition(found.Status.Conditions, "Available")
				return cond.Status == metav1.ConditionFalse, nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check if the cluster has a label for addon with expected value
			err = wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
				cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), managedCluster.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if len(cluster.Labels) == 0 {
					return false, nil
				}
				key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addOn.Name)
				return cluster.Labels[key] == "unhealthy", nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("Should update addon status to unknown if there is no lease for this addon", func() {
			ginkgo.By(fmt.Sprintf("Creating lease %q for managed cluster addon %q", addOn.Name, addOn.Name))
			_, err := hubClient.CoordinationV1().Leases(addOn.Name).Create(context.TODO(), &coordv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      addOn.Name,
					Namespace: addOn.Name,
				},
				Spec: coordv1.LeaseSpec{
					RenewTime: &metav1.MicroTime{Time: time.Now()},
				},
			}, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			err = wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
				found, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.TODO(), addOn.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if found.Status.Conditions == nil {
					return false, nil
				}
				cond := meta.FindStatusCondition(found.Status.Conditions, "Available")
				return cond.Status == metav1.ConditionTrue, nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check if the cluster has a label for addon with expected value
			err = wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
				cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), managedCluster.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if len(cluster.Labels) == 0 {
					return false, nil
				}
				key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addOn.Name)
				return cluster.Labels[key] == "available", nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By(fmt.Sprintf("Deleting lease %q", addOn.Name))
			err = hubClient.CoordinationV1().Leases(addOn.Name).Delete(context.TODO(), addOn.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			err = wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
				found, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.TODO(), addOn.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if !meta.IsStatusConditionTrue(found.Status.Conditions, "Available") {
					return false, nil
				}

				return true, nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check if the cluster has a label for addon with expected value
			err = wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
				cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), managedCluster.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if len(cluster.Labels) == 0 {
					return false, nil
				}
				key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addOn.Name)
				return cluster.Labels[key] == "unreachable", nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("Checking managed cluster status to update addon status", func() {
		var (
			err            error
			suffix         string
			managedCluster *clusterv1.ManagedCluster
			addOn          *addonv1alpha1.ManagedClusterAddOn
			u              *utilClients
		)

		ginkgo.BeforeEach(func() {
			suffix = rand.String(6)
			u = &utilClients{
				hubClient:         hubClient,
				hubDynamicClient:  hubDynamicClient,
				hubAddOnClient:    hubAddOnClient,
				clusterClient:     clusterClient,
				registrationImage: registrationImage,
			}

			// create a managed cluster
			clusterName := fmt.Sprintf("loopback-e2e-%v", suffix)
			ginkgo.By(fmt.Sprintf("Creating managed cluster %q", clusterName))
			managedCluster, err = u.createManagedCluster(clusterName, suffix)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// create an addon on created managed cluster
			addOnName := fmt.Sprintf("addon-%s", suffix)
			ginkgo.By(fmt.Sprintf("Creating managed cluster addon %q", addOnName))
			addOn, err = u.createManagedClusterAddOn(managedCluster, addOnName)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// create addon installation namespace
			ginkgo.By(fmt.Sprintf("Creating managed cluster addon installation namespace %q", addOnName))
			_, err := hubClient.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: addOn.Name,
				},
			}, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			clusterName := fmt.Sprintf("loopback-e2e-%v", suffix)
			ginkgo.By(fmt.Sprintf("Cleaning managed cluster %q", clusterName))
			err = u.cleanupManagedCluster(clusterName, suffix)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			addOnName := fmt.Sprintf("addon-%s", suffix)
			ginkgo.By(fmt.Sprintf("Cleaning managed cluster addon installation namespace %q", addOnName))
			err = hubClient.CoreV1().Namespaces().Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("Should update addon status to unknow if managed cluster stops to update its lease", func() {
			ginkgo.By(fmt.Sprintf("Creating lease %q for managed cluster addon %q", addOn.Name, addOn.Name))
			_, err := hubClient.CoordinationV1().Leases(addOn.Name).Create(context.TODO(), &coordv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      addOn.Name,
					Namespace: addOn.Name,
				},
				Spec: coordv1.LeaseSpec{
					RenewTime: &metav1.MicroTime{Time: time.Now()},
				},
			}, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			err = wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
				found, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.TODO(), addOn.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if found.Status.Conditions == nil {
					return false, nil
				}
				cond := meta.FindStatusCondition(found.Status.Conditions, "Available")
				return cond.Status == metav1.ConditionTrue, nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// delete registration agent to stop agent update its status
			ginkgo.By(fmt.Sprintf("Stoping registration agent on loopback-spoke-%s", suffix))
			err = hubClient.AppsV1().Deployments(fmt.Sprintf("loopback-spoke-%s", suffix)).Delete(context.TODO(), "spoke-agent", metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// for speeding up test, update managed cluster status to unknown manually
			ginkgo.By(fmt.Sprintf("Updating managed cluster %s status to unknown", managedCluster.Name))
			found, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), managedCluster.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
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
			_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(context.TODO(), found, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			err = wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
				found, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.TODO(), addOn.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if found.Status.Conditions == nil {
					return false, nil
				}
				cond := meta.FindStatusCondition(found.Status.Conditions, "Available")
				return cond.Status == metav1.ConditionUnknown, nil
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})
})
