package integration_test

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/rand"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/test/integration/util"
)

var _ = ginkgo.Describe("Cluster Claim", func() {
	var managedClusterName, hubKubeconfigSecret, hubKubeconfigDir string
	var claims []*clusterv1alpha1.ClusterClaim
	var maxCustomClusterClaims int
	var err error
	var cancel context.CancelFunc

	ginkgo.JustBeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)
		hubKubeconfigSecret = fmt.Sprintf("hub-kubeconfig-secret-%s", suffix)
		hubKubeconfigDir = path.Join(util.TestDir, fmt.Sprintf("claimtest-%s", suffix), "hub-kubeconfig")

		// delete all existing claims
		claimList, err := clusterClient.ClusterV1alpha1().ClusterClaims().List(context.TODO(), metav1.ListOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		for _, claim := range claimList.Items {
			err = clusterClient.ClusterV1alpha1().ClusterClaims().Delete(context.TODO(), claim.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		// create claims
		for _, claim := range claims {
			_, err = clusterClient.ClusterV1alpha1().ClusterClaims().Create(context.TODO(), claim, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		// run registration agent
		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			MaxCustomClusterClaims:   maxCustomClusterClaims,
		}
		cancel = util.RunAgent("claimtest", agentOptions, spokeCfg)
	})

	ginkgo.AfterEach(
		func() {
			cancel()
		})

	assertSuccessBootstrap := func() {
		// the spoke cluster and csr should be created after bootstrap
		ginkgo.By("Check existence of ManagedCluster & CSR")
		gomega.Eventually(func() bool {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the spoke cluster should has finalizer that is added by hub controller
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			if len(spokeCluster.Finalizers) != 1 {
				return false
			}

			if spokeCluster.Finalizers[0] != "cluster.open-cluster-management.io/api-resource-cleanup" {
				return false
			}

			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Accept and approve the ManagedCluster")
		// simulate hub cluster admin to accept the managedcluster and approve the csr
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the managed cluster should have accepted condition after it is accepted
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			accepted := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted)
			return accepted != nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() bool {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("ManagedCluster joins the hub")
		// the spoke cluster should have joined condition finally
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			joined := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined)
			return joined != nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	ginkgo.Context("Sync all claims", func() {
		ginkgo.BeforeEach(func() {
			maxCustomClusterClaims = 20
			claims = []*clusterv1alpha1.ClusterClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "a",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "x",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "b",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "y",
					},
				},
			}
		})

		ginkgo.It("should sync all cluster claims on spoke to status of ManagedCluster", func() {
			assertSuccessBootstrap()

			ginkgo.By("Sync existing claims")
			clusterClaims := []clusterv1.ManagedClusterClaim{}
			for _, claim := range claims {
				clusterClaims = append(clusterClaims, clusterv1.ManagedClusterClaim{
					Name:  claim.Name,
					Value: claim.Spec.Value,
				})
			}

			gomega.Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}
				return reflect.DeepEqual(clusterClaims, spokeCluster.Status.ClusterClaims)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("Create a new claim")
			newClaim := &clusterv1alpha1.ClusterClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "c",
				},
				Spec: clusterv1alpha1.ClusterClaimSpec{
					Value: "z",
				},
			}
			newClaim, err = clusterClient.ClusterV1alpha1().ClusterClaims().Create(context.TODO(), newClaim, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			newClusterClaims := []clusterv1.ManagedClusterClaim{}
			newClusterClaims = append(newClusterClaims, clusterClaims...)
			newClusterClaims = append(newClusterClaims, clusterv1.ManagedClusterClaim{
				Name:  newClaim.Name,
				Value: newClaim.Spec.Value,
			})

			gomega.Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}
				return reflect.DeepEqual(newClusterClaims, spokeCluster.Status.ClusterClaims)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("Update the claim")
			newClaim.Spec.Value = "Z"
			_, err := clusterClient.ClusterV1alpha1().ClusterClaims().Update(context.TODO(), newClaim, metav1.UpdateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			updatedClusterClaims := []clusterv1.ManagedClusterClaim{}
			updatedClusterClaims = append(updatedClusterClaims, clusterClaims...)
			updatedClusterClaims = append(updatedClusterClaims, clusterv1.ManagedClusterClaim{
				Name:  newClaim.Name,
				Value: newClaim.Spec.Value,
			})

			gomega.Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}
				return reflect.DeepEqual(updatedClusterClaims, spokeCluster.Status.ClusterClaims)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("Delete the claim")
			err = clusterClient.ClusterV1alpha1().ClusterClaims().Delete(context.TODO(), newClaim.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}
				return reflect.DeepEqual(clusterClaims, spokeCluster.Status.ClusterClaims)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("Truncate exposed claims", func() {
		ginkgo.BeforeEach(func() {
			maxCustomClusterClaims = 5
			claims = []*clusterv1alpha1.ClusterClaim{}
			for i := 0; i < 10; i++ {
				claims = append(claims, &clusterv1alpha1.ClusterClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("claim-%d", i),
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: fmt.Sprintf("value-%d", i),
					},
				})
			}
		})

		ginkgo.It("should sync truncated cluster claims on spoke to status of ManagedCluster", func() {
			assertSuccessBootstrap()

			ginkgo.By("Sync truncated claims")
			gomega.Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}

				return len(spokeCluster.Status.ClusterClaims) == maxCustomClusterClaims
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
	})
})
