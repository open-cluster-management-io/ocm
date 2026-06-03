package placement

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	ocmfeature "open-cluster-management.io/api/feature"

	controllers "open-cluster-management.io/ocm/pkg/placement/controllers"
	"open-cluster-management.io/ocm/pkg/placement/controllers/sigplacementdecision"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("SIGPlacementDecision", func() {
	var cancel context.CancelFunc
	var namespace string
	var placementName string
	var clusterSet1Name string
	var suffix string
	var cpClient cpclientset.Interface

	ginkgo.BeforeEach(func() {
		suffix = rand.String(5)
		namespace = fmt.Sprintf("ns-%s", suffix)
		placementName = fmt.Sprintf("placement-%s", suffix)
		clusterSet1Name = fmt.Sprintf("clusterset-%s", suffix)

		err := features.HubMutableFeatureGate.Set(fmt.Sprintf("%s=true", ocmfeature.SIGPlacementDecision))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		cpClient, err = cpclientset.NewForConfig(restConfig)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err = kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go controllers.RunControllerManager(ctx, &controllercmd.ControllerContext{
			KubeConfig:    restConfig,
			EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
		})
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		err := features.HubMutableFeatureGate.Set(fmt.Sprintf("%s=false", ocmfeature.SIGPlacementDecision))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = kubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.Context("Syncing OCM PlacementDecision to SIG MC PlacementDecision", func() {
		ginkgo.It("should create SIG MC PlacementDecision from OCM PlacementDecision", func() {
			ginkgo.By("Create a ManagedClusterSet")
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
			_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterSet, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Create ManagedClusterSetBinding")
			binding := &clusterapiv1beta2.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterSet1Name,
					Namespace: namespace,
				},
				Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
					ClusterSet: clusterSet1Name,
				},
			}
			_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), binding, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Create ManagedClusters")
			clusterNames := []string{
				fmt.Sprintf("cluster1-%s", suffix),
				fmt.Sprintf("cluster2-%s", suffix),
			}
			for _, name := range clusterNames {
				cluster := testinghelpers.NewManagedCluster(name).WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSet1Name).Build()
				_, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
				_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(context.Background(), cluster, metav1.UpdateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			ginkgo.By("Create Placement")
			placement := &clusterapiv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementName,
					Namespace: namespace,
				},
				Spec: clusterapiv1beta1.PlacementSpec{
					ClusterSets: []string{clusterSet1Name},
				},
			}
			_, err = clusterClient.ClusterV1beta1().Placements(namespace).Create(context.Background(), placement, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Wait for OCM PlacementDecision to be created by scheduling controller")
			gomega.Eventually(func() bool {
				pdList, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", clusterapiv1beta1.PlacementLabel, placementName),
				})
				if err != nil || len(pdList.Items) == 0 {
					return false
				}
				return len(pdList.Items[0].Status.Decisions) > 0
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("Verify SIG MC PlacementDecision is created")
			gomega.Eventually(func() bool {
				sigPDList, err := cpClient.ApisV1alpha1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", cpv1alpha1.LabelClusterManagerKey, sigplacementdecision.SchedulerName),
				})
				if err != nil || len(sigPDList.Items) == 0 {
					return false
				}
				sigPD := sigPDList.Items[0]
				if sigPD.SchedulerName != sigplacementdecision.SchedulerName {
					return false
				}
				if sigPD.Labels[cpv1alpha1.PlacementKeyLabel] != placementName {
					return false
				}
				return len(sigPD.Decisions) > 0
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("Verify SIG MC PlacementDecision has correct ClusterProfileRef")
			sigPDList, err := cpClient.ApisV1alpha1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", cpv1alpha1.LabelClusterManagerKey, sigplacementdecision.SchedulerName),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(len(sigPDList.Items)).To(gomega.BeNumerically(">", 0))

			sigPD := sigPDList.Items[0]
			for _, d := range sigPD.Decisions {
				gomega.Expect(d.ClusterProfileRef.Name).ToNot(gomega.BeEmpty())
			}

			ginkgo.By("Delete the OCM PlacementDecision and verify SIG MC PD is cleaned up")
			ocmPDList, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", clusterapiv1beta1.PlacementLabel, placementName),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			for _, pd := range ocmPDList.Items {
				err = clusterClient.ClusterV1beta1().PlacementDecisions(namespace).Delete(context.Background(), pd.Name, metav1.DeleteOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}

			gomega.Eventually(func() int {
				sigPDList, err := cpClient.ApisV1alpha1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", cpv1alpha1.LabelClusterManagerKey, sigplacementdecision.SchedulerName),
				})
				if err != nil {
					return -1
				}
				return len(sigPDList.Items)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Equal(0))

			ginkgo.By("Clean up clusters")
			for _, name := range clusterNames {
				err = clusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), name, metav1.DeleteOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}
		})
	})
})
