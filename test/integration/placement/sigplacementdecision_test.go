package placement

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	cpv1alpha2 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha2"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/features"
	controllers "open-cluster-management.io/ocm/pkg/placement/controllers"
	"open-cluster-management.io/ocm/pkg/placement/controllers/sigplacementdecision"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("SIGPlacementDecision", func() {
	var cancel context.CancelFunc
	var namespace string
	var placementName string
	var clusterSet1Name string
	var suffix string
	var cpClient cpclientset.Interface

	BeforeEach(func(ctx SpecContext) {
		suffix = rand.String(5)
		namespace = fmt.Sprintf("ns-%s", suffix)
		placementName = fmt.Sprintf("placement-%s", suffix)
		clusterSet1Name = fmt.Sprintf("clusterset-%s", suffix)

		err := features.HubMutableFeatureGate.Set(fmt.Sprintf("%s=true", ocmfeature.SIGPlacementDecision))
		Expect(err).NotTo(HaveOccurred())

		cpClient, err = cpclientset.NewForConfig(restConfig)
		Expect(err).NotTo(HaveOccurred())

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err = kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		var runCtx context.Context
		runCtx, cancel = context.WithCancel(context.Background())
		go func() {
			defer GinkgoRecover()
			err := controllers.RunControllerManager(runCtx, &controllercmd.ControllerContext{
				KubeConfig:    restConfig,
				EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
			})
			Expect(err).NotTo(HaveOccurred())
		}()
	})

	AfterEach(func(ctx SpecContext) {
		if cancel != nil {
			cancel()
		}
		err := features.HubMutableFeatureGate.Set(fmt.Sprintf("%s=false", ocmfeature.SIGPlacementDecision))
		Expect(err).NotTo(HaveOccurred())

		err = kubeClient.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("Syncing OCM PlacementDecision to SIG MC PlacementDecision", func() {
		It("should create SIG MC PlacementDecision from OCM PlacementDecision", func(ctx SpecContext) {
			By("Create a ManagedClusterSet")
			clusterSet := &clusterapiv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterSet1Name,
				},
				Spec: clusterapiv1beta2.ManagedClusterSetSpec{
					ClusterSelector: clusterapiv1beta2.ManagedClusterSelector{
						SelectorType: clusterapiv1beta2.ExclusiveClusterSetLabel,
					},
				},
			}
			_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterSet, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Create ManagedClusterSetBinding")
			binding := &clusterapiv1beta2.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterSet1Name,
					Namespace: namespace,
				},
				Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
					ClusterSet: clusterSet1Name,
				},
			}
			_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Create ManagedClusters")
			clusterNames := []string{
				fmt.Sprintf("cluster1-%s", suffix),
				fmt.Sprintf("cluster2-%s", suffix),
			}
			for _, name := range clusterNames {
				cluster := testinghelpers.NewManagedCluster(name).WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSet1Name).Build()
				cluster, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())

				cluster.Status = clusterapiv1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               clusterapiv1.ManagedClusterConditionAvailable,
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Available",
							Message:            "cluster is available",
						},
						{
							Type:               clusterapiv1.ManagedClusterConditionJoined,
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Joined",
							Message:            "cluster is joined",
						},
					},
				}
				_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(ctx, cluster, metav1.UpdateOptions{})
				Expect(err).ToNot(HaveOccurred())
			}

			By("Create Placement")
			placement := &clusterapiv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementName,
					Namespace: namespace,
				},
				Spec: clusterapiv1beta1.PlacementSpec{
					ClusterSets: []string{clusterSet1Name},
				},
			}
			_, err = clusterClient.ClusterV1beta1().Placements(namespace).Create(ctx, placement, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Wait for OCM PlacementDecision to be created by scheduling controller")
			Eventually(func() bool {
				pdList, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", clusterapiv1beta1.PlacementLabel, placementName),
				})
				if err != nil || len(pdList.Items) == 0 {
					return false
				}
				return len(pdList.Items[0].Status.Decisions) > 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("Verify SIG MC PlacementDecision is created")
			Eventually(func() bool {
				sigPDList, err := cpClient.ApisV1alpha2().PlacementDecisions(namespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", cpv1alpha2.LabelClusterManagerKey, sigplacementdecision.SchedulerName),
				})
				if err != nil || len(sigPDList.Items) == 0 {
					return false
				}
				sigPD := sigPDList.Items[0]
				if sigPD.SchedulerName != sigplacementdecision.SchedulerName {
					return false
				}
				if sigPD.Labels[cpv1alpha2.PlacementKeyLabel] != placementName {
					return false
				}
				return len(sigPD.Decisions) > 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("Verify SIG MC PlacementDecision has correct ClusterProfileRef")
			sigPDList, err := cpClient.ApisV1alpha2().PlacementDecisions(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", cpv1alpha2.LabelClusterManagerKey, sigplacementdecision.SchedulerName),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(sigPDList.Items)).To(BeNumerically(">", 0))

			sigPD := sigPDList.Items[0]
			for _, d := range sigPD.Decisions {
				Expect(d.ClusterProfileRef.Name).ToNot(BeEmpty())
			}

			By("Keep the v1alpha2 decision readable through v1alpha1")
			legacyPD, err := cpClient.ApisV1alpha1().PlacementDecisions(namespace).Get(ctx, sigPD.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(legacyPD.UID).To(Equal(sigPD.UID))
			Expect(legacyPD.Decisions).To(HaveLen(len(sigPD.Decisions)))
			for i, d := range legacyPD.Decisions {
				Expect(d.ClusterProfileRef.Name).To(Equal(sigPD.Decisions[i].ClusterProfileRef.Name))
				Expect(d.ClusterProfileRef.Namespace).To(Equal(namespace))
			}

			By("Verify SIG MC PlacementDecision has an OwnerReference to the OCM PlacementDecision")
			ocmPD, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).Get(ctx, sigPD.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(sigPD.OwnerReferences).To(HaveLen(1))
			ownerRef := sigPD.OwnerReferences[0]
			Expect(ownerRef.Kind).To(Equal("PlacementDecision"))
			Expect(ownerRef.Name).To(Equal(ocmPD.Name))
			Expect(ownerRef.UID).To(Equal(ocmPD.UID))
			Expect(ownerRef.Controller).ToNot(BeNil())
			Expect(*ownerRef.Controller).To(BeTrue())

			By("Delete the Placement so the scheduling controller does not regenerate the OCM PlacementDecision")
			err = clusterClient.ClusterV1beta1().Placements(namespace).Delete(ctx, placementName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Delete the OCM PlacementDecision and verify the controller does not error out")
			ocmPDList, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", clusterapiv1beta1.PlacementLabel, placementName),
			})
			Expect(err).NotTo(HaveOccurred())
			for _, pd := range ocmPDList.Items {
				err = clusterClient.ClusterV1beta1().PlacementDecisions(namespace).Delete(ctx, pd.Name, metav1.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred())
			}

			Eventually(func() bool {
				_, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).Get(ctx, ocmPD.Name, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("Clean up clusters")
			for _, name := range clusterNames {
				err = clusterClient.ClusterV1().ManagedClusters().Delete(ctx, name, metav1.DeleteOptions{})
				Expect(err).ToNot(HaveOccurred())
			}
		})
	})
})
