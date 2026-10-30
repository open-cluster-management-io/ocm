package registration_test

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
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
	"open-cluster-management.io/ocm/pkg/registration/spoke/managedcluster"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("Cluster Claim", func() {
	var managedClusterName, hubKubeconfigSecret, hubKubeconfigDir string
	var claims []*clusterv1alpha1.ClusterClaim
	var maxCustomClusterClaims int
	var reservedClusterClaimSuffixes []string
	var reportHostingCluster bool
	var hostingClusterName string
	var err error
	var cancel context.CancelFunc

	BeforeEach(func() {
		// reset so one spec's opt-in can't leak into the next
		reportHostingCluster = false
		hostingClusterName = ""
	})

	JustBeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)
		hubKubeconfigSecret = fmt.Sprintf("hub-kubeconfig-secret-%s", suffix)
		hubKubeconfigDir = path.Join(util.TestDir, fmt.Sprintf("claimtest-%s", suffix), "hub-kubeconfig")

		// create claims
		for _, claim := range claims {
			_, err = clusterClient.ClusterV1alpha1().ClusterClaims().Create(context.TODO(), claim, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		}

		// run registration agent
		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:          bootstrapKubeConfigFile,
			HubKubeconfigSecret:          hubKubeconfigSecret,
			ClusterHealthCheckPeriod:     1 * time.Minute,
			MaxCustomClusterClaims:       maxCustomClusterClaims,
			ReservedClusterClaimSuffixes: reservedClusterClaimSuffixes,
			ReportHostingCluster:         reportHostingCluster,
			HostingClusterName:           hostingClusterName,
			RegisterDriverOption:         registerfactory.NewOptions(),
		}
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName
		cancel = runAgent("claimtest", agentOptions, commOptions, spokeCfg)

		DeferCleanup(func() {
			err = clusterClient.ClusterV1alpha1().ClusterClaims().DeleteCollection(
				context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			cancel()
		})
	})

	assertSuccessBootstrap := func() {
		// the spoke cluster and csr should be created after bootstrap
		By("Check existence of ManagedCluster & CSR")
		Eventually(func() bool {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		Eventually(func() bool {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// the spoke cluster should has finalizer that is added by hub controller
		Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}

			if !commonhelpers.HasFinalizer(spokeCluster.Finalizers, clusterv1.ManagedClusterFinalizer) {
				return false
			}

			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Accept and approve the ManagedCluster")
		// simulate hub cluster admin to accept the managedcluster and approve the csr
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		Expect(err).NotTo(HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
		Expect(err).NotTo(HaveOccurred())

		// the managed cluster should have accepted condition after it is accepted
		Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			accepted := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted)
			return accepted != nil
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// the hub kubeconfig secret should be filled after the csr is approved
		Eventually(func() bool {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("ManagedCluster joins the hub")
		// the spoke cluster should have joined condition finally
		Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			joined := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined)
			return joined != nil
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	Context("Sync all claims", func() {
		BeforeEach(func() {
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

		It("should sync all cluster claims on spoke to status of ManagedCluster", func() {
			assertSuccessBootstrap()

			By("Sync existing claims")
			var clusterClaims []clusterv1.ManagedClusterClaim
			for _, claim := range claims {
				clusterClaims = append(clusterClaims, clusterv1.ManagedClusterClaim{
					Name:  claim.Name,
					Value: claim.Spec.Value,
				})
			}

			Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}
				return reflect.DeepEqual(clusterClaims, spokeCluster.Status.ClusterClaims)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("Create a new claim")
			newClaim := &clusterv1alpha1.ClusterClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "c",
				},
				Spec: clusterv1alpha1.ClusterClaimSpec{
					Value: "z",
				},
			}
			newClaim, err = clusterClient.ClusterV1alpha1().ClusterClaims().Create(context.TODO(), newClaim, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			var newClusterClaims []clusterv1.ManagedClusterClaim
			newClusterClaims = append(newClusterClaims, clusterClaims...)
			newClusterClaims = append(newClusterClaims, clusterv1.ManagedClusterClaim{
				Name:  newClaim.Name,
				Value: newClaim.Spec.Value,
			})

			Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}
				return reflect.DeepEqual(newClusterClaims, spokeCluster.Status.ClusterClaims)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("Update the claim")
			newClaim.Spec.Value = "Z"
			_, err := clusterClient.ClusterV1alpha1().ClusterClaims().Update(context.TODO(), newClaim, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())

			var updatedClusterClaims []clusterv1.ManagedClusterClaim
			updatedClusterClaims = append(updatedClusterClaims, clusterClaims...)
			updatedClusterClaims = append(updatedClusterClaims, clusterv1.ManagedClusterClaim{
				Name:  newClaim.Name,
				Value: newClaim.Spec.Value,
			})

			Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}
				return reflect.DeepEqual(updatedClusterClaims, spokeCluster.Status.ClusterClaims)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("Delete the claim")
			err = clusterClient.ClusterV1alpha1().ClusterClaims().Delete(context.TODO(), newClaim.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}
				return reflect.DeepEqual(clusterClaims, spokeCluster.Status.ClusterClaims)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("Self-reported hosting cluster (KEP-188)", func() {
		BeforeEach(func() {
			reportHostingCluster = true
			hostingClusterName = "hub-cluster-b"
		})

		It("should create, update and delete the reserved hosting-cluster claim", func() {
			assertSuccessBootstrap()

			By("Claim is created with the reported value")
			Eventually(func() (string, error) {
				claim, err := clusterClient.ClusterV1alpha1().ClusterClaims().Get(
					context.TODO(), managedcluster.HostingClusterClaimName, metav1.GetOptions{})
				if err != nil {
					return "", err
				}
				return claim.Spec.Value, nil
			}, eventuallyTimeout, eventuallyInterval).Should(Equal("hub-cluster-b"))

			By("Claim is corrected if something else changes its value")
			claim, err := clusterClient.ClusterV1alpha1().ClusterClaims().Get(
				context.TODO(), managedcluster.HostingClusterClaimName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			claim.Spec.Value = "someone-else-changed-this"
			_, err = clusterClient.ClusterV1alpha1().ClusterClaims().Update(context.TODO(), claim, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() (string, error) {
				claim, err := clusterClient.ClusterV1alpha1().ClusterClaims().Get(
					context.TODO(), managedcluster.HostingClusterClaimName, metav1.GetOptions{})
				if err != nil {
					return "", err
				}
				return claim.Spec.Value, nil
			}, eventuallyTimeout, eventuallyInterval).Should(Equal("hub-cluster-b"))
		})
	})

	Context("Self-report disabled (default)", func() {
		It("should never create the reserved hosting-cluster claim", func() {
			assertSuccessBootstrap()

			Consistently(func() bool {
				_, err := clusterClient.ClusterV1alpha1().ClusterClaims().Get(
					context.TODO(), managedcluster.HostingClusterClaimName, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, 5*time.Second, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("Sync all clusterproperties", func() {
		var aboutClient aboutclient.Interface
		var err error
		var properties []*aboutv1alpha1.ClusterProperty
		BeforeEach(func() {
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
			Expect(err).NotTo(HaveOccurred())
			for _, prop := range properties {
				_, err = aboutClient.AboutV1alpha1().ClusterProperties().Create(
					context.Background(), prop, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
			}
			err = features.SpokeMutableFeatureGate.Set("ClusterProperty=true")
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func() {
				err := aboutClient.AboutV1alpha1().ClusterProperties().DeleteCollection(
					context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				err = features.SpokeMutableFeatureGate.Set("ClusterProperty=false")
				Expect(err).NotTo(HaveOccurred())
			})
		})

		It("should sync all cluster properties on spoke to status of ManagedCluster", func() {
			assertSuccessBootstrap()

			By("Sync existing clusterproperty")
			var clusterClaims []clusterv1.ManagedClusterClaim
			for _, claim := range properties {
				clusterClaims = append(clusterClaims, clusterv1.ManagedClusterClaim{
					Name:  claim.Name,
					Value: claim.Spec.Value,
				})
			}

			Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(clusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("Create a new clusterproperty")
			newProperty := &aboutv1alpha1.ClusterProperty{
				ObjectMeta: metav1.ObjectMeta{
					Name: "property3",
				},
				Spec: aboutv1alpha1.ClusterPropertySpec{
					Value: "value3",
				},
			}
			newProperty, err = aboutClient.AboutV1alpha1().ClusterProperties().Create(context.TODO(), newProperty, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			var newClusterClaims []clusterv1.ManagedClusterClaim
			newClusterClaims = append(newClusterClaims, clusterClaims...)
			newClusterClaims = append(newClusterClaims, clusterv1.ManagedClusterClaim{
				Name:  newProperty.Name,
				Value: newProperty.Spec.Value,
			})

			Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(newClusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("Update the property")
			newProperty.Spec.Value = "Z"
			_, err := aboutClient.AboutV1alpha1().ClusterProperties().Update(context.TODO(), newProperty, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())

			var updatedClusterClaims []clusterv1.ManagedClusterClaim
			updatedClusterClaims = append(updatedClusterClaims, clusterClaims...)
			updatedClusterClaims = append(updatedClusterClaims, clusterv1.ManagedClusterClaim{
				Name:  newProperty.Name,
				Value: newProperty.Spec.Value,
			})

			Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(updatedClusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("Delete the property")
			err = aboutClient.AboutV1alpha1().ClusterProperties().Delete(context.TODO(), newProperty.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(clusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		It("should sync all both properties and claims", func() {
			assertSuccessBootstrap()

			var clusterClaims []clusterv1.ManagedClusterClaim
			for _, claim := range properties {
				clusterClaims = append(clusterClaims, clusterv1.ManagedClusterClaim{
					Name:  claim.Name,
					Value: claim.Spec.Value,
				})
			}

			By("Create a new claim")
			newClaim := &clusterv1alpha1.ClusterClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "c",
				},
				Spec: clusterv1alpha1.ClusterClaimSpec{
					Value: "z",
				},
			}
			newClaim, err = clusterClient.ClusterV1alpha1().ClusterClaims().Create(context.TODO(), newClaim, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			var updatedClusterClaims []clusterv1.ManagedClusterClaim
			updatedClusterClaims = append(updatedClusterClaims, clusterv1.ManagedClusterClaim{
				Name:  newClaim.Name,
				Value: newClaim.Spec.Value,
			})
			updatedClusterClaims = append(updatedClusterClaims, clusterClaims...)

			Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(updatedClusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("create a claim with the same key")
			updateProperty, err := aboutClient.AboutV1alpha1().ClusterProperties().Get(
				context.TODO(), "property1", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			updateProperty.Spec.Value = "newValue"
			_, err = aboutClient.AboutV1alpha1().ClusterProperties().Update(context.TODO(), updateProperty, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())
			newClaim = &clusterv1alpha1.ClusterClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "property1",
				},
				Spec: clusterv1alpha1.ClusterClaimSpec{
					Value: "value1",
				},
			}
			_, err = clusterClient.ClusterV1alpha1().ClusterClaims().Create(context.TODO(), newClaim, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			updatedClusterClaims[1].Value = "newValue"

			Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(updatedClusterClaims, spokeCluster.Status.ClusterClaims) {
					return fmt.Errorf("cluster claims are not equal, got %v", spokeCluster.Status.ClusterClaims)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})
	})

	Context("Truncate exposed claims", func() {
		BeforeEach(func() {
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

		It("should sync truncated cluster claims on spoke to status of ManagedCluster", func() {
			assertSuccessBootstrap()

			By("Sync truncated claims")
			Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}

				return len(spokeCluster.Status.ClusterClaims) == maxCustomClusterClaims
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("Keep custom reserved claims", func() {
		BeforeEach(func() {
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

		It("should sync custom suffix claims to status of ManagedCluster", func() {
			assertSuccessBootstrap()

			By("Sync claims")
			Eventually(func() bool {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}

				return len(spokeCluster.Status.ClusterClaims) == maxCustomClusterClaims+1
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})
})
