package e2e

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/util/retry"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

const (
	defaultClusterSetName = "default"
	invalidURL            = "127.0.0.1:8001"
	validURL              = "https://127.0.0.1:8443"
	saNamespace           = "default"
)

var _ = ginkgo.Describe("Admission webhook", func() {
	var admissionName string

	ginkgo.Context("ManagedCluster", func() {
		var clusterName string
		ginkgo.BeforeEach(func() {
			admissionName = "managedclustervalidators.admission.cluster.open-cluster-management.io"
			clusterName = fmt.Sprintf("webhook-spoke-%s", rand.String(6))
		})

		ginkgo.AfterEach(func() {
			gomega.Expect(hub.DeleteManageClusterAndRelatedNamespace(clusterName)).ToNot(gomega.HaveOccurred())
		})

		ginkgo.Context("Creating a managed cluster", func() {
			ginkgo.It("Should have the default LeaseDurationSeconds", func() {
				ginkgo.By(fmt.Sprintf("create a managed cluster %q", clusterName))

				_, err := hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.TODO(),
					newManagedCluster(clusterName, false, validURL), metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(),
					clusterName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(managedCluster.Spec.LeaseDurationSeconds).To(gomega.Equal(int32(60)))
			})
			ginkgo.It("Should have the default Clusterset Label (no labels in cluster)", func() {
				ginkgo.By(fmt.Sprintf("create a managed cluster %q", clusterName))
				oriManagedCluster := newManagedCluster(clusterName, false, validURL)
				_, err := hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), oriManagedCluster, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(managedCluster.Labels[clusterv1beta2.ClusterSetLabel]).To(gomega.Equal(string(defaultClusterSetName)))

				gomega.Expect(len(managedCluster.Labels)).To(gomega.Equal(len(oriManagedCluster.Labels) + 1))
			})
			ginkgo.It("Should have the default Clusterset Label (has labels in cluster)", func() {
				ginkgo.By(fmt.Sprintf("create a managed cluster %q", clusterName))
				oriManagedCluster := newManagedCluster(clusterName, false, validURL)

				oriManagedCluster.Labels = map[string]string{
					"test": "test_value",
				}
				_, err := hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), oriManagedCluster, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(managedCluster.Labels[clusterv1beta2.ClusterSetLabel]).To(gomega.Equal(string(defaultClusterSetName)))

				gomega.Expect(len(managedCluster.Labels)).To(gomega.Equal(len(oriManagedCluster.Labels) + 1))
			})
			ginkgo.It("Should have the default Clusterset Label when clusterset label is a null string", func() {
				ginkgo.By(fmt.Sprintf("create a managed cluster %q", clusterName))
				oriManagedCluster := newManagedCluster(clusterName, false, validURL)

				oriManagedCluster.Labels = map[string]string{
					clusterv1beta2.ClusterSetLabel: "",
				}
				_, err := hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), oriManagedCluster, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(managedCluster.Labels[clusterv1beta2.ClusterSetLabel]).To(gomega.Equal(string(defaultClusterSetName)))
			})
			ginkgo.It("Should have the timeAdded for taints", func() {
				ginkgo.By(fmt.Sprintf("create a managed cluster %q with taint", clusterName))

				cluster := newManagedCluster(clusterName, false, validURL)
				cluster.Spec.Taints = []clusterv1.Taint{
					{
						Key:    "a",
						Value:  "b",
						Effect: clusterv1.TaintEffectNoSelect,
					},
				}
				_, err := hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), cluster, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				ginkgo.By("check if timeAdded of the taint is set automatically")
				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				taint := findTaint(managedCluster.Spec.Taints, "a", "b", clusterv1.TaintEffectNoSelect)
				gomega.Expect(taint).ShouldNot(gomega.BeNil())
				gomega.Expect(taint.TimeAdded.IsZero()).To(gomega.BeFalse())

				ginkgo.By("chang the effect of the taint")
				// sleep and make sure the update is performed 1 second later than the creation
				time.Sleep(1 * time.Second)
				gomega.Eventually(func() error {
					managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					for index, taint := range managedCluster.Spec.Taints {
						if taint.Key != "a" {
							continue
						}
						if taint.Value != "b" {
							continue
						}
						managedCluster.Spec.Taints[index] = clusterv1.Taint{
							Key:    taint.Key,
							Value:  taint.Value,
							Effect: clusterv1.TaintEffectNoSelectIfNew,
						}
					}
					_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
					return err
				}).Should(gomega.Succeed())

				ginkgo.By("check if timeAdded of the taint is reset")
				managedCluster, err = hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				updatedTaint := findTaint(managedCluster.Spec.Taints, "a", "b", clusterv1.TaintEffectNoSelectIfNew)
				gomega.Expect(updatedTaint).ShouldNot(gomega.BeNil())
				gomega.Expect(taint.TimeAdded.Equal(&updatedTaint.TimeAdded)).To(gomega.BeFalse(),
					"timeAdded of taint should be updated (before=%s, after=%s)", taint.TimeAdded.Time.String(), updatedTaint.TimeAdded.Time.String())
			})

			ginkgo.It("Should respond bad request when creating a managed cluster with invalid external server URLs", func() {
				ginkgo.By(fmt.Sprintf("create a managed cluster %q with an invalid external server URL %q", clusterName, invalidURL))

				managedCluster := newManagedCluster(clusterName, false, invalidURL)

				_, err := hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
				gomega.Expect(err.Error()).Should(gomega.Equal(fmt.Sprintf(
					"admission webhook \"%s\" denied the request: url \"%s\" is invalid in client configs",
					admissionName,
					invalidURL,
				)))
			})

			ginkgo.It("Should respond bad request when cluster name is invalid", func() {
				ginkgo.By(fmt.Sprintf("create a managed cluster %q with an invalid name", clusterName))
				clusterName = fmt.Sprintf("webhook.spoke-%s", rand.String(6))
				managedCluster := newManagedCluster(clusterName, false, validURL)

				_, err := hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			})

			ginkgo.It("Should forbid the request when creating an accepted managed cluster by unauthorized user", func() {
				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))

				ginkgo.By(fmt.Sprintf("create an managed cluster %q with unauthorized service account %q", clusterName, sa))

				// prepare an unauthorized cluster client from a service account who can create/get/update ManagedCluster
				// but cannot change the ManagedCluster HubAcceptsClient field
				unauthorizedClient, err := hub.BuildClusterClient(saNamespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclusters"},
						Verbs:     []string{"create", "get", "update"},
					},
				}, nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster := newManagedCluster(clusterName, true, validURL)

				_, err = unauthorizedClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsForbidden(err)).Should(gomega.BeTrue())
				gomega.Expect(hub.DeleteManageClusterAndRelatedNamespace(clusterName)).ToNot(gomega.HaveOccurred())
				gomega.Expect(hub.CleanupClusterClient(saNamespace, sa)).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("Should forbid the request when creating a managed cluster with a termaniting namespace", func() {
				var err error

				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))

				authorizedClient, err := hub.BuildClusterClient(saNamespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclusters"},
						Verbs:     []string{"create", "get", "update"},
					},
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersets/join"},
						Verbs:     []string{"create"},
					},
					{
						APIGroups:     []string{"register.open-cluster-management.io"},
						Resources:     []string{"managedclusters/accept"},
						ResourceNames: []string{clusterName},
						Verbs:         []string{"update"},
					},
				}, nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// create a namespace, add a finilizer to it, and delete it
				_, err = hub.KubeClient.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName,
						Finalizers: []string{
							"open-cluster-mangement.io/finalizer",
						},
					},
				}, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// delete the namespace
				err = hub.KubeClient.CoreV1().Namespaces().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// create a managed cluster, should be denied
				managedCluster := newManagedCluster(clusterName, true, validURL)
				_, err = authorizedClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
				gomega.Expect(errors.IsForbidden(err)).Should(gomega.BeTrue())

				// remove the finalizer to truly delete the namespace
				ns, err := hub.KubeClient.CoreV1().Namespaces().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				ns.Finalizers = []string{}
				_, err = hub.KubeClient.CoreV1().Namespaces().Update(context.TODO(), ns, metav1.UpdateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(hub.CleanupClusterClient(saNamespace, sa)).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("Should accept the request when creating an accepted managed cluster by authorized user", func() {
				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))

				ginkgo.By(fmt.Sprintf("create an managed cluster %q with authorized service account %q", clusterName, sa))
				authorizedClient, err := hub.BuildClusterClient(saNamespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclusters"},
						Verbs:     []string{"create", "get", "update"},
					},
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersets/join"},
						Verbs:     []string{"create"},
					},
					{
						APIGroups:     []string{"register.open-cluster-management.io"},
						Resources:     []string{"managedclusters/accept"},
						ResourceNames: []string{clusterName},
						Verbs:         []string{"update"},
					},
				}, nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster := newManagedCluster(clusterName, true, validURL)
				_, err = authorizedClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(hub.CleanupClusterClient(saNamespace, sa)).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("Should accept the request when update managed cluster other field by unauthorized user", func() {
				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))

				ginkgo.By(fmt.Sprintf("update an managed cluster %q label with unauthorized service account %q", clusterName, sa))

				// prepare an unauthorized cluster client from a service account who can create/get/update ManagedCluster
				// but cannot change the ManagedCluster HubAcceptsClient field
				unauthorizedClient, err := hub.BuildClusterClient(saNamespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclusters"},
						Verbs:     []string{"create", "get", "update"},
					},
				}, nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster := newManagedCluster(clusterName, true, validURL)

				managedCluster, err = hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
					managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), managedCluster.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}
					managedCluster.Labels = map[string]string{
						"k": "v",
					}
					_, err = unauthorizedClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
					return err
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(hub.CleanupClusterClient(saNamespace, sa)).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("Should accept the request when creating a managed cluster with clusterset specified by authorized user", func() {
				clusterSetName := fmt.Sprintf("webhook-spoke-%s", rand.String(6))
				ginkgo.By(fmt.Sprintf("create a managed cluster set %q", clusterSetName))

				managedClusterSet := &clusterv1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterSetName,
					},
				}

				_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.TODO(), managedClusterSet, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))

				ginkgo.By(fmt.Sprintf("create a managed cluster %q with unauthorized service account %q", clusterName, sa))

				authorizedClient, err := hub.BuildClusterClient(saNamespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclusters"},
						Verbs:     []string{"create", "get", "update"},
					},
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersets/join"},
						Verbs:     []string{"create"},
					},
				}, nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster := newManagedCluster(clusterName, false, validURL)
				managedCluster.Labels = map[string]string{
					clusterv1beta2.ClusterSetLabel: clusterSetName,
				}
				_, err = authorizedClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(hub.CleanupClusterClient(saNamespace, sa)).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("Should forbid the request when creating a managed cluster with clusterset specified by unauthorized user", func() {
				clusterSetName := fmt.Sprintf("webhook-spoke-%s", rand.String(6))
				ginkgo.By(fmt.Sprintf("create a managed cluster set %q", clusterSetName))

				managedClusterSet := &clusterv1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterSetName,
					},
				}

				_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.TODO(), managedClusterSet, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))

				ginkgo.By(fmt.Sprintf("create a managed cluster %q with unauthorized service account %q", clusterName, sa))

				// prepare an unauthorized cluster client from a service account who can create/get/update ManagedCluster
				// but cannot set the clusterset label
				unauthorizedClient, err := hub.BuildClusterClient(saNamespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclusters"},
						Verbs:     []string{"create", "get", "update"},
					},
				}, nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster := newManagedCluster(clusterName, false, validURL)
				managedCluster.Labels = map[string]string{
					clusterv1beta2.ClusterSetLabel: clusterSetName,
				}
				_, err = unauthorizedClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsForbidden(err)).Should(gomega.BeTrue())
				gomega.Expect(hub.CleanupClusterClient(saNamespace, sa)).ToNot(gomega.HaveOccurred())
			})
		})

		ginkgo.Context("Updating a managed cluster", func() {
			var clusterName string
			ginkgo.BeforeEach(func() {
				ginkgo.By(fmt.Sprintf("Creating managed cluster %q", clusterName))
				clusterName = fmt.Sprintf("webhook-spoke-%s", rand.String(6))
				managedCluster := newManagedCluster(clusterName, false, validURL)
				_, err := hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			})

			ginkgo.AfterEach(func() {
				ginkgo.By(fmt.Sprintf("Cleaning managed cluster %q", clusterName))
				gomega.Expect(hub.DeleteManageClusterAndRelatedNamespace(clusterName)).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("Should not update the LeaseDurationSeconds to zero", func() {
				ginkgo.By(fmt.Sprintf("try to update managed cluster %q LeaseDurationSeconds to zero", clusterName))
				err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					managedCluster.Spec.LeaseDurationSeconds = 0
					_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
					return err
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(managedCluster.Spec.LeaseDurationSeconds).To(gomega.Equal(int32(60)))
			})

			ginkgo.It("Should not delete the default ClusterSet Label", func() {
				ginkgo.By(fmt.Sprintf("try to update managed cluster %q ClusterSet label", clusterName))
				err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					delete(managedCluster.Labels, clusterv1beta2.ClusterSetLabel)
					_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
					return err
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(managedCluster.Labels[clusterv1beta2.ClusterSetLabel]).To(gomega.Equal(string(defaultClusterSetName)))
			})

			ginkgo.It("Should not update the other ClusterSet Label", func() {
				ginkgo.By(fmt.Sprintf("try to update managed cluster %q ClusterSet label", clusterName))
				err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					managedCluster.Labels[clusterv1beta2.ClusterSetLabel] = "s1"

					_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
					return err
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(managedCluster.Labels[clusterv1beta2.ClusterSetLabel]).To(gomega.Equal("s1"))
			})

			ginkgo.It("Should respond bad request when updating a managed cluster with invalid external server URLs", func() {
				ginkgo.By(fmt.Sprintf("update managed cluster %q with an invalid external server URL %q", clusterName, invalidURL))

				err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					managedCluster.Spec.ManagedClusterClientConfigs[0].URL = invalidURL
					_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
					return err
				})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			})

			ginkgo.It("Should forbid the request when updating an unaccepted managed cluster to accepted by unauthorized user", func() {
				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
				ginkgo.By(fmt.Sprintf("accept managed cluster %q by an unauthorized user %q", clusterName, sa))

				// prepare an unauthorized cluster client from a service account who can create/get/update ManagedCluster
				// but cannot change the ManagedCluster HubAcceptsClient field
				unauthorizedClient, err := hub.BuildClusterClient(saNamespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclusters"},
						Verbs:     []string{"create", "get", "update"},
					},
				}, nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
					managedCluster, err := unauthorizedClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					managedCluster.Spec.HubAcceptsClient = true
					_, err = unauthorizedClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
					return err
				})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsForbidden(err)).Should(gomega.BeTrue())

				gomega.Expect(hub.CleanupClusterClient(saNamespace, sa)).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("Should forbid the request when updating a managed cluster with a terminating namespace", func() {
				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
				var err error
				authorizedClient, err := hub.BuildClusterClient(saNamespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclusters"},
						Verbs:     []string{"create", "get", "update"},
					},
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersets/join"},
						Verbs:     []string{"create"},
					},
					{
						APIGroups:     []string{"register.open-cluster-management.io"},
						Resources:     []string{"managedclusters/accept"},
						ResourceNames: []string{clusterName},
						Verbs:         []string{"update"},
					},
				}, nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// create a namespace, add a finilizer to it, and delete it
				_, err = hub.KubeClient.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName,
						Finalizers: []string{
							"open-cluster-mangement.io/finalizer",
						},
					},
				}, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// delete the namespace
				err = hub.KubeClient.CoreV1().Namespaces().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// update the HubAcceptsClient field to true
				err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
					managedCluster, err := authorizedClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					managedCluster.Spec.HubAcceptsClient = true
					_, err = authorizedClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
					return err
				})
				gomega.Expect(errors.IsForbidden(err)).To(gomega.BeTrue())

				gomega.Expect(hub.CleanupClusterClient(saNamespace, sa)).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("Should accept the request when updating the clusterset of a managed cluster by authorized user", func() {
				clusterSetName := fmt.Sprintf("webhook-spoke-%s", rand.String(6))
				ginkgo.By(fmt.Sprintf("create a managed cluster set %q", clusterSetName))

				managedClusterSet := &clusterv1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterSetName,
					},
				}

				_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.TODO(), managedClusterSet, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
				ginkgo.By(fmt.Sprintf("accept managed cluster %q by an unauthorized user %q", clusterName, sa))

				authorizedClient, err := hub.BuildClusterClient(saNamespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclusters"},
						Verbs:     []string{"create", "get", "update"},
					},
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersets/join"},
						Verbs:     []string{"create"},
					},
				}, nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
					managedCluster, err := authorizedClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					managedCluster.Labels = map[string]string{
						clusterv1beta2.ClusterSetLabel: clusterSetName,
					}
					_, err = authorizedClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
					return err
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(hub.CleanupClusterClient(saNamespace, sa)).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("Should forbid the request when updating the clusterset of a managed cluster by unauthorized user", func() {
				clusterSetName := fmt.Sprintf("webhook-spoke-%s", rand.String(6))
				ginkgo.By(fmt.Sprintf("create a managed cluster set %q", clusterSetName))

				managedClusterSet := &clusterv1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterSetName,
					},
				}

				_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.TODO(), managedClusterSet, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
				ginkgo.By(fmt.Sprintf("accept managed cluster %q by an unauthorized user %q", clusterName, sa))

				// prepare an unauthorized cluster client from a service account who can create/get/update ManagedCluster
				// but cannot change the clusterset label
				unauthorizedClient, err := hub.BuildClusterClient(saNamespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclusters"},
						Verbs:     []string{"create", "get", "update"},
					},
					{
						APIGroups:     []string{"cluster.open-cluster-management.io"},
						Resources:     []string{"managedclustersets/join"},
						ResourceNames: []string{"default"},
						Verbs:         []string{"create"},
					},
				}, nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
					managedCluster, err := unauthorizedClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					managedCluster.Labels = map[string]string{
						clusterv1beta2.ClusterSetLabel: clusterSetName,
					}
					_, err = unauthorizedClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
					return err
				})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsForbidden(err)).Should(gomega.BeTrue())

				gomega.Expect(hub.CleanupClusterClient(saNamespace, sa)).ToNot(gomega.HaveOccurred())
			})
		})
	})

	ginkgo.Context("ManagedClusterSetBinding", func() {
		var namespace string

		ginkgo.BeforeEach(func() {
			admissionName = "managedclustersetbindingvalidators.admission.cluster.open-cluster-management.io"

			// create a namespace for testing
			namespace = fmt.Sprintf("ns-%s", rand.String(6))
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			_, err := hub.KubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// make sure the managedclusterset can be created successfully
			gomega.Eventually(func() error {
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).
					Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				if err != nil {
					return err
				}
				return hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Delete(context.TODO(), clusterSetName, metav1.DeleteOptions{})
			}).Should(gomega.Succeed())
		})

		ginkgo.AfterEach(func() {
			err := hub.KubeClient.CoreV1().Namespaces().Delete(context.TODO(), namespace, metav1.DeleteOptions{})
			if errors.IsNotFound(err) {
				return
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.Context("Creating a ManagedClusterSetBinding", func() {
			ginkgo.It("should deny the request when creating a ManagedClusterSetBinding with unmatched cluster set name", func() {
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
				clusterSetBindingName := fmt.Sprintf("clustersetbinding-%s", rand.String(6))
				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetBindingName, clusterSetName)
				_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).
					Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			})

			ginkgo.It("should accept the request when creating a ManagedClusterSetBinding by authorized user", func() {
				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))

				authorizedClient, err := hub.BuildClusterClient(namespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersets/bind"},
						Verbs:     []string{"create"},
					},
				}, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersetbindings"},
						Verbs:     []string{"create", "get", "update", "patch"},
					},
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				_, err = authorizedClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				gomega.Expect(hub.CleanupClusterClient(namespace, sa)).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("should forbid the request when creating a ManagedClusterSetBinding by unauthorized user", func() {
				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))

				// prepare an unauthorized cluster client from a service account who can create/get/update ManagedClusterSetBinding
				// but cannot bind ManagedClusterSet
				unauthorizedClient, err := hub.BuildClusterClient(namespace, sa, nil, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersetbindings"},
						Verbs:     []string{"create", "get", "update", "patch"},
					},
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				_, err = unauthorizedClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).
					Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsForbidden(err)).Should(gomega.BeTrue())

				gomega.Expect(hub.CleanupClusterClient(namespace, sa)).ToNot(gomega.HaveOccurred())
			})
		})

		ginkgo.Context("Updating a ManagedClusterSetBinding", func() {
			ginkgo.It("should deny the request when updating a ManagedClusterSetBinding with a new cluster set", func() {
				// create a cluster set binding
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				managedClusterSetBinding, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(
					context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// update the cluster set binding
				clusterSetName = fmt.Sprintf("clusterset-%s", rand.String(6))
				patch := fmt.Sprintf("{\"spec\": {\"clusterSet\": %q}}", clusterSetName)
				_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Patch(
					context.TODO(), managedClusterSetBinding.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			})

			ginkgo.It("should accept the request when updating the label of the ManagedClusterSetBinding by user without binding permission", func() {
				// create a cluster set binding
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).
					Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// create a client without clusterset binding permission
				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
				unauthorizedClient, err := hub.BuildClusterClient(namespace, sa, nil, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersetbindings"},
						Verbs:     []string{"create", "get", "update"},
					},
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// update the cluster set binding by unauthorized user
				gomega.Eventually(func() error {
					binding, err := unauthorizedClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.TODO(), clusterSetName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					binding.Labels = map[string]string{"owner": "user"}
					_, err = unauthorizedClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Update(context.TODO(), binding, metav1.UpdateOptions{})
					return err
				}).Should(gomega.Succeed())
			})
		})

		ginkgo.Context("Creating a ManagedClusterSetBinding v1beta2", func() {
			ginkgo.It("should deny the request when creating a ManagedClusterSetBinding with unmatched cluster set name", func() {
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
				clusterSetBindingName := fmt.Sprintf("clustersetbinding-%s", rand.String(6))
				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetBindingName, clusterSetName)
				_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).
					Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			})

			ginkgo.It("should accept the request when creating a ManagedClusterSetBinding by authorized user", func() {
				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))

				authorizedClient, err := hub.BuildClusterClient(namespace, sa, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersets/bind"},
						Verbs:     []string{"create"},
					},
				}, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersetbindings"},
						Verbs:     []string{"create", "get", "update", "patch"},
					},
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				_, err = authorizedClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				gomega.Expect(hub.CleanupClusterClient(namespace, sa)).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("should forbid the request when creating a ManagedClusterSetBinding by unauthorized user", func() {
				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))

				// prepare an unauthorized cluster client from a service account who can create/get/update ManagedClusterSetBinding
				// but cannot bind ManagedClusterSet
				unauthorizedClient, err := hub.BuildClusterClient(namespace, sa, nil, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersetbindings"},
						Verbs:     []string{"create", "get", "update", "patch"},
					},
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				_, err = unauthorizedClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsForbidden(err)).Should(gomega.BeTrue())

				gomega.Expect(hub.CleanupClusterClient(namespace, sa)).ToNot(gomega.HaveOccurred())
			})
		})

		ginkgo.Context("Updating a ManagedClusterSetBinding", func() {
			ginkgo.It("should deny the request when updating a ManagedClusterSetBinding with a new cluster set", func() {
				// create a cluster set binding
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				managedClusterSetBinding, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(
					context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// update the cluster set binding
				clusterSetName = fmt.Sprintf("clusterset-%s", rand.String(6))
				patch := fmt.Sprintf("{\"spec\": {\"clusterSet\": %q}}", clusterSetName)
				_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Patch(
					context.TODO(), managedClusterSetBinding.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			})

			ginkgo.It("should accept the request when updating the label of the ManagedClusterSetBinding by user without binding permission", func() {
				// create a cluster set binding
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).
					Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// create a client without clusterset binding permission
				sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
				unauthorizedClient, err := hub.BuildClusterClient(namespace, sa, nil, []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersetbindings"},
						Verbs:     []string{"create", "get", "update"},
					},
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// update the cluster set binding by unauthorized user
				gomega.Eventually(func() error {
					binding, err := unauthorizedClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.TODO(), clusterSetName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					binding.Labels = map[string]string{"owner": "user"}
					_, err = unauthorizedClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Update(context.TODO(), binding, metav1.UpdateOptions{})
					return err
				}).Should(gomega.Succeed())
			})
		})

	})
})

func newManagedCluster(name string, accepted bool, externalURL string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},

		Spec: clusterv1.ManagedClusterSpec{
			HubAcceptsClient: accepted,
			ManagedClusterClientConfigs: []clusterv1.ClientConfig{
				{
					URL: externalURL,
				},
			},
		},
	}
}

// findTaint returns the first matched taint in the given taint array. Return nil if no taint matched.
func findTaint(taints []clusterv1.Taint, key, value string, effect clusterv1.TaintEffect) *clusterv1.Taint {
	for _, taint := range taints {
		if len(key) != 0 && taint.Key != key {
			continue
		}

		if len(value) != 0 && taint.Value != value {
			continue
		}

		if len(effect) != 0 && taint.Effect != effect {
			continue
		}

		return &taint
	}
	return nil
}
