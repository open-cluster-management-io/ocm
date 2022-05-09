package e2e

import (
	"context"
	"fmt"
	"time"

	ginkgo "github.com/onsi/ginkgo"
	gomega "github.com/onsi/gomega"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/helpers"

	certificatesv1 "k8s.io/api/certificates/v1"
	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

var _ = ginkgo.Describe("Addon Health Check", func() {
	ginkgo.Context("Checking addon lease on managed cluster to update addon status", func() {
		var (
			err            error
			suffix         string
			managedCluster *clusterv1.ManagedCluster
			addOn          *addonv1alpha1.ManagedClusterAddOn
		)

		ginkgo.BeforeEach(func() {
			suffix = rand.String(6)

			// create a managed cluster
			clusterName := fmt.Sprintf("loopback-e2e-%v", suffix)
			ginkgo.By(fmt.Sprintf("Creating managed cluster %q", clusterName))
			managedCluster, err = createManagedCluster(clusterName, suffix)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// create an addon on created managed cluster
			addOnName := fmt.Sprintf("addon-%s", suffix)
			ginkgo.By(fmt.Sprintf("Creating managed cluster addon %q", addOnName))
			addOn, err = createManagedClusterAddOn(managedCluster, addOnName)
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
			err = cleanupManagedCluster(clusterName, suffix)
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
				if found.Status.Conditions == nil {
					return false, nil
				}
				cond := meta.FindStatusCondition(found.Status.Conditions, "Available")
				return cond.Status == metav1.ConditionUnknown, nil
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
		)

		ginkgo.BeforeEach(func() {
			suffix = rand.String(6)

			// create a managed cluster
			clusterName := fmt.Sprintf("loopback-e2e-%v", suffix)
			ginkgo.By(fmt.Sprintf("Creating managed cluster %q", clusterName))
			managedCluster, err = createManagedCluster(clusterName, suffix)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// create an addon on created managed cluster
			addOnName := fmt.Sprintf("addon-%s", suffix)
			ginkgo.By(fmt.Sprintf("Creating managed cluster addon %q", addOnName))
			addOn, err = createManagedClusterAddOn(managedCluster, addOnName)
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
			err = cleanupManagedCluster(clusterName, suffix)
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

func createManagedCluster(clusterName, suffix string) (*clusterv1.ManagedCluster, error) {
	nsName := fmt.Sprintf("loopback-spoke-%v", suffix)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}

	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		ns, err = hubClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	// This test expects a bootstrap secret to exist in open-cluster-management-agent/e2e-bootstrap-secret
	e2eBootstrapSecret, err := hubClient.CoreV1().Secrets("open-cluster-management-agent").Get(context.TODO(), "e2e-bootstrap-secret", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	bootstrapSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: nsName,
			Name:      "bootstrap-secret",
		},
	}
	bootstrapSecret.Data = e2eBootstrapSecret.Data
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = hubClient.CoreV1().Secrets(nsName).Create(context.TODO(), bootstrapSecret, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	var (
		cr         *unstructured.Unstructured
		crResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterroles",
		}
	)
	cr, err = spokeCR(suffix)
	if err != nil {
		return nil, err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = hubDynamicClient.Resource(crResource).Create(context.TODO(), cr, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	var (
		crb         *unstructured.Unstructured
		crbResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterrolebindings",
		}
	)
	crb, err = spokeCRB(nsName, suffix)
	if err != nil {
		return nil, err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = hubDynamicClient.Resource(crbResource).Create(context.TODO(), crb, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	var (
		role         *unstructured.Unstructured
		roleResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "roles",
		}
	)
	role, err = spokeRole(nsName, suffix)
	if err != nil {
		return nil, err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = hubDynamicClient.Resource(roleResource).Namespace(nsName).Create(context.TODO(), role, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	var (
		roleBinding         *unstructured.Unstructured
		roleBindingResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "rolebindings",
		}
	)
	roleBinding, err = spokeRoleBinding(nsName, suffix)
	if err != nil {
		return nil, err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = hubDynamicClient.Resource(roleBindingResource).Namespace(nsName).Create(context.TODO(), roleBinding, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: nsName,
			Name:      "spoke-agent-sa",
		},
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = hubClient.CoreV1().ServiceAccounts(nsName).Create(context.TODO(), sa, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	var (
		deployment         *unstructured.Unstructured
		deploymentResource = schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "deployments",
		}
	)
	deployment, err = spokeDeploymentWithAddonManagement(nsName, clusterName, registrationImage)
	if err != nil {
		return nil, err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = hubDynamicClient.Resource(deploymentResource).Namespace(nsName).Create(context.TODO(), deployment, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	var (
		csrs      *certificatesv1.CertificateSigningRequestList
		csrClient = hubClient.CertificatesV1().CertificateSigningRequests()
	)

	if err := wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		var err error
		csrs, err = csrClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name = %v", clusterName),
		})
		if err != nil {
			return false, err
		}

		if len(csrs.Items) >= 1 {
			return true, nil
		}

		return false, nil
	}); err != nil {
		return nil, err
	}

	var csr *certificatesv1.CertificateSigningRequest
	for i := range csrs.Items {
		csr = &csrs.Items[i]

		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
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
		}); err != nil {
			return nil, err
		}
	}

	var (
		managedCluster  *clusterv1.ManagedCluster
		managedClusters = clusterClient.ClusterV1().ManagedClusters()
	)

	if err := wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		var err error
		managedCluster, err = managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var err error
		managedCluster, err = managedClusters.Get(context.TODO(), managedCluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		managedCluster.Spec.HubAcceptsClient = true
		managedCluster.Spec.LeaseDurationSeconds = 5
		managedCluster, err = managedClusters.Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
		return err
	}); err != nil {
		return nil, err
	}

	if err := wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		var err error
		managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
			return true, nil
		}

		return false, nil
	}); err != nil {
		return nil, err
	}

	if err := wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		var err error
		managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		return meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined), nil
	}); err != nil {
		return nil, err
	}

	err = wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		return meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable), nil
	})
	return managedCluster, err
}

func spokeDeploymentWithAddonManagement(nsName, clusterName, image string) (*unstructured.Unstructured, error) {
	deployment, err := assetToUnstructured("spoke/deployment.yaml")
	if err != nil {
		return nil, err
	}
	err = unstructured.SetNestedField(deployment.Object, nsName, "meta", "namespace")
	if err != nil {
		return nil, err
	}

	containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || containers == nil {
		return nil, fmt.Errorf("deployment containers not found or error in spec: %v", err)
	}

	if err := unstructured.SetNestedField(containers[0].(map[string]interface{}), image, "image"); err != nil {
		return nil, err
	}

	args, found, err := unstructured.NestedSlice(containers[0].(map[string]interface{}), "args")
	if err != nil || !found || args == nil {
		return nil, fmt.Errorf("container args not found or error in spec: %v", err)
	}

	clusterNameArg := fmt.Sprintf("--cluster-name=%v", clusterName)
	args[2] = clusterNameArg

	args = append(args, "--feature-gates=AddonManagement=true")
	if err := unstructured.SetNestedField(containers[0].(map[string]interface{}), args, "args"); err != nil {
		return nil, err
	}

	if err := unstructured.SetNestedField(deployment.Object, containers, "spec", "template", "spec", "containers"); err != nil {
		return nil, err
	}

	return deployment, nil
}

func createManagedClusterAddOn(managedCluster *clusterv1.ManagedCluster, addOnName string) (*addonv1alpha1.ManagedClusterAddOn, error) {
	addOn := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: managedCluster.Name,
			Name:      addOnName,
		},
		Spec: addonv1alpha1.ManagedClusterAddOnSpec{
			InstallNamespace: addOnName,
		},
	}
	return hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Create(context.TODO(), addOn, metav1.CreateOptions{})
}

// cleanupManagedCluster deletes the managed cluster related resources, including the namespace
func cleanupManagedCluster(clusterName, suffix string) error {
	nsName := fmt.Sprintf("loopback-spoke-%v", suffix)

	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		err := hubClient.CoreV1().Namespaces().Delete(context.TODO(), nsName, metav1.DeleteOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("delete ns %q failed: %v", nsName, err)
	}

	if err := deleteManageClusterAndRelatedNamespace(clusterName); err != nil {
		return fmt.Errorf("delete manage cluster and related namespace for cluster %q failed: %v", clusterName, err)
	}

	var (
		err        error
		cr         *unstructured.Unstructured
		crResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterroles",
		}
	)
	cr, err = spokeCR(suffix)
	if err != nil {
		return err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		err := hubDynamicClient.Resource(crResource).Delete(context.TODO(), cr.GetName(), metav1.DeleteOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("delete cr %q failed: %v", cr.GetName(), err)
	}

	var (
		crb         *unstructured.Unstructured
		crbResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterrolebindings",
		}
	)
	crb, err = spokeCRB(nsName, suffix)
	if err != nil {
		return err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		err := hubDynamicClient.Resource(crbResource).Delete(context.TODO(), crb.GetName(), metav1.DeleteOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("delete crb %q failed: %v", crb.GetName(), err)
	}

	return nil
}

func deleteManageClusterAndRelatedNamespace(clusterName string) error {
	if err := wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		err := clusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return false, err
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("delete managed cluster %q failed: %v", clusterName, err)
	}

	// delete namespace created by hub automaticly
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		err := hubClient.CoreV1().Namespaces().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
		// some managed cluster just created, but the csr is not approved,
		// so there is not a related namespace
		if err != nil && !errors.IsNotFound(err) {
			return false, err
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("delete related namespace %q failed: %v", clusterName, err)
	}

	return nil
}
