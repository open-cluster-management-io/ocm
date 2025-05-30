package registration_test

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	aboutv1alpha1 "sigs.k8s.io/about-api/pkg/apis/v1alpha1"
	aboutclient "sigs.k8s.io/about-api/pkg/generated/clientset/versioned"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Cluster Claim", func() {
	var managedClusterName, hubKubeconfigSecret, hubKubeconfigDir string
	var claims []*clusterv1alpha1.ClusterClaim
	var maxCustomClusterClaims int
	var reservedClusterClaimSuffixes []string
	var err error
	var cancel context.CancelFunc

	ginkgo.JustBeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)
		hubKubeconfigSecret = fmt.Sprintf("hub-kubeconfig-secret-%s", suffix)
		hubKubeconfigDir = path.Join(util.TestDir, fmt.Sprintf("claimtest-%s", suffix), "hub-kubeconfig")

		// create claims
		for _, claim := range claims {
			_, err = clusterClient.ClusterV1alpha1().ClusterClaims().Create(context.TODO(), claim, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		// run registration agent
		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:          bootstrapKubeConfigFile,
			HubKubeconfigSecret:          hubKubeconfigSecret,
			ClusterHealthCheckPeriod:     1 * time.Minute,
			MaxCustomClusterClaims:       maxCustomClusterClaims,
			ReservedClusterClaimSuffixes: reservedClusterClaimSuffixes,
			RegisterDriverOption:         registerfactory.NewOptions(),
		}
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName
		cancel = runAgent("claimtest", agentOptions, commOptions, spokeCfg)

		ginkgo.DeferCleanup(func() {
			err = clusterClient.ClusterV1alpha1().ClusterClaims().DeleteCollection(
				context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			cancel()
		})
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

			if !commonhelpers.HasFinalizer(spokeCluster.Finalizers, clusterv1.ManagedClusterFinalizer) {
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
			var clusterClaims []clusterv1.ManagedClusterClaim
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

			var newClusterClaims []clusterv1.ManagedClusterClaim
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

			var updatedClusterClaims []clusterv1.ManagedClusterClaim
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

	ginkgo.Context("Sync all clusterproperties", func() {
		var aboutClient aboutclient.Interface
		var err error
		var properties []*aboutv1alpha1.ClusterProperty
		ginkgo.BeforeEach(func() {
			maxCustomClusterClaims = 20
			claims = []*clusterv1alpha1.ClusterClaim{}
			properties = []*aboutv1alpha1.ClusterProperty{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "property1",
					},
					Spec: aboutv1alpha1.ClusterPropertySpec{
						Value: "value1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "property2",
					},
					Spec: aboutv1alpha1.ClusterPropertySpec{
						Value: "value2",
					},
				},
			}
			aboutClient, err = aboutclient.NewForConfig(spokeCfg)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			for _, prop := range properties {
				_, err = aboutClient.AboutV1alpha1().ClusterProperties().Create(
					context.Background(), prop, metav1.CreateOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}
			err = features.SpokeMutableFeatureGate.Set("ClusterProperty=true")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.DeferCleanup(func() {
				err := aboutClient.AboutV1alpha1().ClusterProperties().DeleteCollection(
					context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				err = features.SpokeMutableFeatureGate.Set("ClusterProperty=false")
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			})
		})

		ginkgo.It("should sync all cluster properties on spoke to status of ManagedCluster", func() {
			assertSuccessBootstrap()

			ginkgo.By("Sync existing clusterproperty")
			var clusterClaims []clusterv1.ManagedClusterClaim
			for _, claim := range properties {
				clusterClaims = append(clusterClaims, clusterv1.ManagedClusterClaim{
					Name:  claim.Name,
					Value: claim.Spec.Value,
				})
			}

			gomega.Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(clusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Create a new clusterproperty")
			newProperty := &aboutv1alpha1.ClusterProperty{
				ObjectMeta: metav1.ObjectMeta{
					Name: "property3",
				},
				Spec: aboutv1alpha1.ClusterPropertySpec{
					Value: "value3",
				},
			}
			newProperty, err = aboutClient.AboutV1alpha1().ClusterProperties().Create(context.TODO(), newProperty, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			var newClusterClaims []clusterv1.ManagedClusterClaim
			newClusterClaims = append(newClusterClaims, clusterClaims...)
			newClusterClaims = append(newClusterClaims, clusterv1.ManagedClusterClaim{
				Name:  newProperty.Name,
				Value: newProperty.Spec.Value,
			})

			gomega.Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(newClusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Update the property")
			newProperty.Spec.Value = "Z"
			_, err := aboutClient.AboutV1alpha1().ClusterProperties().Update(context.TODO(), newProperty, metav1.UpdateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			var updatedClusterClaims []clusterv1.ManagedClusterClaim
			updatedClusterClaims = append(updatedClusterClaims, clusterClaims...)
			updatedClusterClaims = append(updatedClusterClaims, clusterv1.ManagedClusterClaim{
				Name:  newProperty.Name,
				Value: newProperty.Spec.Value,
			})

			gomega.Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(updatedClusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Delete the property")
			err = aboutClient.AboutV1alpha1().ClusterProperties().Delete(context.TODO(), newProperty.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(clusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		})

		ginkgo.It("should sync all both properties and claims", func() {
			assertSuccessBootstrap()

			var clusterClaims []clusterv1.ManagedClusterClaim
			for _, claim := range properties {
				clusterClaims = append(clusterClaims, clusterv1.ManagedClusterClaim{
					Name:  claim.Name,
					Value: claim.Spec.Value,
				})
			}

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

			var updatedClusterClaims []clusterv1.ManagedClusterClaim
			updatedClusterClaims = append(updatedClusterClaims, clusterv1.ManagedClusterClaim{
				Name:  newClaim.Name,
				Value: newClaim.Spec.Value,
			})
			updatedClusterClaims = append(updatedClusterClaims, clusterClaims...)

			gomega.Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(updatedClusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("create a claim with the same key")
			updateProperty, err := aboutClient.AboutV1alpha1().ClusterProperties().Get(
				context.TODO(), "property1", metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			updateProperty.Spec.Value = "newValue"
			_, err = aboutClient.AboutV1alpha1().ClusterProperties().Update(context.TODO(), updateProperty, metav1.UpdateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			newClaim = &clusterv1alpha1.ClusterClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "property1",
				},
				Spec: clusterv1alpha1.ClusterClaimSpec{
					Value: "value1",
				},
			}
			newClaim, err = clusterClient.ClusterV1alpha1().ClusterClaims().Create(context.TODO(), newClaim, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			updatedClusterClaims[1].Value = "newValue"

			gomega.Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(updatedClusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
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

	ginkgo.Context("Keep custom reserved claims", func() {
		ginkgo.BeforeEach(func() {
			maxCustomClusterClaims = 5
			reservedClusterClaimSuffixes = []string{"reserved.io"}
			claims = []*clusterv1alpha1.ClusterClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "claim.reserved.io",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "value-test",
					},
				},
			}
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

		ginkgo.It("should sync custom suffix claims to status of ManagedCluster", func() {
			assertSuccessBootstrap()

			ginkgo.By("Sync claims")
			gomega.Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}

				return len(spokeCluster.Status.ClusterClaims) == maxCustomClusterClaims+1
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
	})
})
