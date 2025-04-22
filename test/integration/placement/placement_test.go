package placement

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	controllers "open-cluster-management.io/ocm/pkg/placement/controllers"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	clusterSetLabel = "cluster.open-cluster-management.io/clusterset"
	placementLabel  = "cluster.open-cluster-management.io/placement"
)

var _ = ginkgo.Describe("Placement", func() {
	var cancel context.CancelFunc
	var namespace string
	var placementName string
	var clusterName string
	var clusterSet1Name, clusterSet2Name string
	var suffix string
	var err error

	ginkgo.BeforeEach(func() {
		suffix = rand.String(5)
		namespace = fmt.Sprintf("ns-%s", suffix)
		placementName = fmt.Sprintf("placement-%s", suffix)
		clusterName = fmt.Sprintf("cluster-%s", suffix)
		clusterSet1Name = fmt.Sprintf("clusterset-%s", suffix)
		clusterSet2Name = fmt.Sprintf("clusterset-%s", rand.String(5))

		// create testing namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// start controller manager
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
		err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		assertCleanupClusters()
	})

	ginkgo.Context("Scheduling", func() {
		ginkgo.AfterEach(func() {
			ginkgo.By("Delete placement")
			err = clusterClient.ClusterV1beta1().Placements(namespace).Delete(context.Background(), placementName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			assertPlacementDeleted(placementName, namespace)

		})

		ginkgo.It("Should re-create placementdecisions successfully once placementdecisions are deleted", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(placement, 5, 1)

			ginkgo.By("Delete placementdecisions")
			placementDecisions, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: placementLabel + "=" + placementName,
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			for _, placementDecision := range placementDecisions.Items {
				err = clusterClient.ClusterV1beta1().PlacementDecisions(namespace).Delete(context.Background(), placementDecision.Name, metav1.DeleteOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			// check if the placementdecisions are re-created
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			assertPlacementDecisionCreated(placement)
			assertPlacementDecisionNumbers(placementName, namespace, 5, 1)
			assertPlacementStatusDecisionGroups(
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{{Decisions: []string{testinghelpers.PlacementDecisionName(placementName, 1)}, ClustersCount: 5}})
		})

		ginkgo.It("Should re-create placementdecisions successfully once selected clusters are terminating", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(placement, 5, 1)

			ctx := context.Background()

			ginkgo.By("Find the first managedcluster")
			clusters, err := clusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(clusters.Items).To(gomega.HaveLen(5))
			terminatingCluster := clusters.Items[0].Name

			ginkgo.By("Add a finalizer for the managedcluster: " + terminatingCluster)
			gomega.Eventually(func() error {
				managedCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx,
					terminatingCluster, metav1.GetOptions{})
				if err != nil {
					return err
				}
				managedCluster.Finalizers = append(managedCluster.Finalizers, "test")
				_, err = clusterClient.ClusterV1().ManagedClusters().Update(ctx,
					managedCluster, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Delete the managedcluster: " + terminatingCluster)
			err = clusterClient.ClusterV1().ManagedClusters().Delete(ctx, terminatingCluster, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check if the placementdecisions are re-created
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Get(ctx,
				placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			assertPlacementDecisionCreated(placement)
			assertPlacementDecisionNumbers(placementName, namespace, 4, 1)
			assertPlacementStatusDecisionGroups(
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{
					{Decisions: []string{testinghelpers.PlacementDecisionName(placementName, 1)}, ClustersCount: 4},
				})

			ginkgo.By("Delete the finalizer for the managedcluster: " + terminatingCluster)
			gomega.Eventually(func() error {
				managedCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx,
					terminatingCluster, metav1.GetOptions{})
				if err != nil {
					return err
				}
				finalizers := []string{}
				for _, finalizer := range managedCluster.Finalizers {
					if finalizer != "test" {
						finalizers = append(finalizers, finalizer)
					}
				}
				managedCluster.Finalizers = finalizers
				_, err = clusterClient.ClusterV1().ManagedClusters().Update(ctx,
					managedCluster, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		})

		ginkgo.It("Should create empty placementdecision when no cluster selected", func() {
			placement := testinghelpers.NewPlacement(namespace, placementName).Build()
			assertCreatingPlacementWithDecision(placement, 0, 1)
			assertPlacementStatusDecisionGroups(
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{{Decisions: []string{testinghelpers.PlacementDecisionName(placementName, 1)}, ClustersCount: 0}})
		})

		ginkgo.It("Should create multiple placementdecisions once scheduled", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 101)
			placement := testinghelpers.NewPlacement(namespace, placementName).Build()
			assertCreatingPlacementWithDecision(placement, 101, 2)

			nod := 101
			assertPlacementDecisionNumbers(placementName, namespace, nod, 2)
			assertPlacementStatusDecisionGroups(
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{{
					Decisions: []string{
						testinghelpers.PlacementDecisionName(placementName, 1),
						testinghelpers.PlacementDecisionName(placementName, 2),
					},
					ClustersCount: 101}})
			assertPlacementConditionSatisfied(placementName, namespace, nod, true)
		})

		ginkgo.It("Should schedule placement successfully once spec.ClusterSets changes", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertBindingClusterSet(clusterSet2Name, namespace)
			assertCreatingClusters(clusterSet1Name, 2)
			assertCreatingClusters(clusterSet2Name, 3)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(placement, 5, 1)

			// update ClusterSets
			placement.Spec.ClusterSets = []string{clusterSet1Name}
			assertPatchingPlacementSpec(placement)
			assertPlacementDecisionNumbers(placementName, namespace, 2, 1)
		})

		ginkgo.It("Should schedule successfully once spec.NumberOfClusters is reduced", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(placement, 5, 1)

			ginkgo.By("Reduce NOC of the placement")
			noc := int32(4)
			placement.Spec.NumberOfClusters = &noc
			assertPatchingPlacementSpec(placement)

			nod := int(noc)
			assertPlacementDecisionNumbers(placementName, namespace, nod, 1)
			assertPlacementConditionSatisfied(placementName, namespace, nod, true)
		})

		ginkgo.It("Should schedule successfully once spec.NumberOfClusters is increased", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 10)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(5).Build()
			assertCreatingPlacementWithDecision(placement, 5, 1)

			ginkgo.By("Increase NOC of the placement")
			noc := int32(8)
			placement.Spec.NumberOfClusters = &noc
			assertPatchingPlacementSpec(placement)

			nod := int(noc)
			assertPlacementDecisionNumbers(placementName, namespace, nod, 1)
			assertPlacementConditionSatisfied(placementName, namespace, nod, true)
		})

		ginkgo.It("Should schedule successfully once MisConfigured is fixed", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 1)

			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(1).AddToleration(&clusterapiv1beta1.Toleration{
				Key:      "key1",
				Operator: clusterapiv1beta1.TolerationOpExists,
				Value:    "value1",
			}).Build()
			assertCreatingPlacement(placement)
			assertPlacementDecisionNumbers(placementName, namespace, 0, 1)
			assertPlacementConditionMisconfigured(placementName, namespace, true)

			placement.Spec.Tolerations = []clusterapiv1beta1.Toleration{}
			assertPatchingPlacementSpec(placement)
			assertPlacementDecisionNumbers(placementName, namespace, 1, 1)
			assertPlacementConditionMisconfigured(placementName, namespace, false)
			assertPlacementConditionSatisfied(placementName, namespace, 1, true)
		})

		ginkgo.It("Should be satisfied once new clusters are added", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(placement, 5, 1)

			// add more clusters
			ginkgo.By("Add the cluster")
			assertCreatingClusters(clusterSet1Name, 5)

			nod := 10
			assertPlacementDecisionNumbers(placementName, namespace, nod, 1)
			assertPlacementConditionSatisfied(placementName, namespace, nod, true)
		})

		ginkgo.It("Should schedule successfully once clusters belong to labelselector clusterset are added/deleted", func() {
			ginkgo.By("Bind clusterset to the placement namespace")
			assertCreatingClusterSet(clusterSet1Name, "vendor", "openShift")
			assertCreatingClusterSetBinding(clusterSet1Name, namespace)
			clusters1 := assertCreatingClusters(clusterName+"1", 1, "vendor", "openShift")
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).Build()
			assertCreatingPlacementWithDecision(placement, 1, 1)

			// add more clusters
			ginkgo.By("Add the cluster")
			clusters2 := assertCreatingClusters(clusterName+"2", 1, "vendor", "openShift")

			assertPlacementDecisionNumbers(placementName, namespace, 2, 1)
			assertPlacementConditionSatisfied(placementName, namespace, 2, true)

			ginkgo.By("Delete the cluster")
			assertDeletingClusters(clusters1...)
			assertPlacementDecisionNumbers(placementName, namespace, 1, 1)

			assertDeletingClusters(clusters2...)
			assertPlacementDecisionNumbers(placementName, namespace, 0, 1)
		})

		ginkgo.It("Should update the decision group once clusters added/deleted", func() {
			ginkgo.By("Bind clusterset to the placement namespace")
			assertCreatingClusterSet("global")
			assertCreatingClusterSetBinding("global", namespace)

			canary := assertCreatingClusters(clusterSet1Name, 2, "vendor", "openShift")
			noncanary := assertCreatingClusters(clusterSet1Name, 3)

			placement := testinghelpers.NewPlacement(namespace, placementName).WithGroupStrategy(clusterapiv1beta1.GroupStrategy{
				ClustersPerDecisionGroup: intstr.FromInt(2),
				DecisionGroups: []clusterapiv1beta1.DecisionGroup{
					{
						GroupName: "canary",
						ClusterSelector: clusterapiv1beta1.ClusterSelector{
							LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"vendor": "openShift"}},
						},
					},
				},
			}).Build()
			assertCreatingPlacementWithDecision(placement, 5, 3)

			assertPlacementDecisionNumbers(placementName, namespace, 5, 3)
			assertPlacementConditionSatisfied(placementName, namespace, 5, true)
			assertPlacementStatusDecisionGroups(placementName, namespace, []clusterapiv1beta1.DecisionGroupStatus{
				{
					DecisionGroupIndex: 0,
					DecisionGroupName:  "canary",
					Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 1)},
					ClustersCount:      2,
				},
				{
					DecisionGroupIndex: 1,
					Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 2)},
					ClustersCount:      2,
				},
				{
					DecisionGroupIndex: 2,
					Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 3)},
					ClustersCount:      1,
				},
			})

			ginkgo.By("Delete the cluster")
			assertDeletingClusters(canary[0], noncanary[0])
			assertPlacementStatusDecisionGroups(placementName, namespace, []clusterapiv1beta1.DecisionGroupStatus{
				{
					DecisionGroupIndex: 0,
					DecisionGroupName:  "canary",
					Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 1)},
					ClustersCount:      1,
				},
				{
					DecisionGroupIndex: 1,
					Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 2)},
					ClustersCount:      2,
				},
			})

			ginkgo.By("Add the canary cluster")
			c := assertCreatingClusters(clusterSet1Name, 1, "vendor", "openShift")
			canary = append(canary, c...)
			assertPlacementDecisionNumbers(placementName, namespace, 4, 2)
			assertPlacementStatusDecisionGroups(placementName, namespace, []clusterapiv1beta1.DecisionGroupStatus{
				{
					DecisionGroupIndex: 0,
					DecisionGroupName:  "canary",
					Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 1)},
					ClustersCount:      2,
				},
				{
					DecisionGroupIndex: 1,
					Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 2)},
					ClustersCount:      2,
				},
			})
			ginkgo.By("Add the non canary cluster")
			c = assertCreatingClusters(clusterSet1Name, 1)
			noncanary = append(noncanary, c...)
			assertPlacementDecisionNumbers(placementName, namespace, 5, 3)
			assertPlacementStatusDecisionGroups(placementName, namespace, []clusterapiv1beta1.DecisionGroupStatus{
				{
					DecisionGroupIndex: 0,
					DecisionGroupName:  "canary",
					Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 1)},
					ClustersCount:      2,
				},
				{
					DecisionGroupIndex: 1,
					Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 2)},
					ClustersCount:      2,
				},
				{
					DecisionGroupIndex: 2,
					Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 3)},
					ClustersCount:      1,
				},
			})

			ginkgo.By("Delete all the cluster")
			assertDeletingClusters(canary[1:]...)
			assertDeletingClusters(noncanary[1:]...)
			assertDeletingClusterSet("global")
			assertPlacementDecisionNumbers(placementName, namespace, 0, 1)
			assertPlacementStatusDecisionGroups(
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{{Decisions: []string{testinghelpers.PlacementDecisionName(placementName, 1)}, ClustersCount: 0}})
		})

		ginkgo.It("Should schedule successfully once clusters belong to global(empty labelselector) clusterset are added/deleted)", func() {
			ginkgo.By("Bind global clusterset to the placement namespace")
			assertCreatingClusterSet("global")
			assertCreatingClusterSetBinding("global", namespace)
			clusters1 := assertCreatingClusters(clusterName+"1", 1)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).Build()
			assertCreatingPlacementWithDecision(placement, 1, 1)

			ginkgo.By("Add the cluster")
			clusters2 := assertCreatingClusters(clusterName+"2", 1)

			assertPlacementDecisionNumbers(placementName, namespace, 2, 1)
			assertPlacementConditionSatisfied(placementName, namespace, 2, true)

			ginkgo.By("Delete the cluster")
			assertDeletingClusters(clusters1...)
			assertPlacementDecisionNumbers(placementName, namespace, 1, 1)

			assertDeletingClusters(clusters2...)
			assertPlacementDecisionNumbers(placementName, namespace, 0, 1)
		})

		ginkgo.It("Should schedule successfully once new clusterset is bound", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(placement, 5, 1)

			ginkgo.By("Bind one more clusterset to the placement namespace")
			assertBindingClusterSet(clusterSet2Name, namespace)
			assertCreatingClusters(clusterSet2Name, 3)

			nod := 8
			assertPlacementDecisionNumbers(placementName, namespace, nod, 1)
			assertPlacementConditionSatisfied(placementName, namespace, nod, false)
		})

		ginkgo.It("Should schedule successfully once new labelselector clusterset is bound", func() {
			ginkgo.By("Bind clusterset to the placement namespace")
			assertCreatingClusterSet(clusterSet1Name, "vendor", "openShift")
			assertCreatingClusterSetBinding(clusterSet1Name, namespace)
			clusters1 := assertCreatingClusters(clusterName+"1", 1, "vendor", "openShift")
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).Build()
			assertCreatingPlacementWithDecision(placement, 1, 1)

			ginkgo.By("Bind one more labelselector clusterset to the placement namespace")
			assertCreatingClusterSet(clusterSet2Name, "vendor", "IKS")
			assertCreatingClusterSetBinding(clusterSet2Name, namespace)
			clusters2 := assertCreatingClusters(clusterName+"2", 1, "vendor", "IKS")

			nod := 2
			assertPlacementDecisionNumbers(placementName, namespace, nod, 1)
			assertPlacementConditionSatisfied(placementName, namespace, nod, true)

			assertDeletingClusters(clusters1[0], clusters2[0])
		})

		ginkgo.It("Should schedule successfully once a clusterset deleted/added", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(placement, 5, 1)

			assertPlacementDecisionNumbers(placementName, namespace, 5, 1)
			assertPlacementConditionSatisfied(placementName, namespace, 5, false)

			ginkgo.By("Delete the clusterset")
			assertDeletingClusterSet(clusterSet1Name)
			assertPlacementDecisionNumbers(placementName, namespace, 0, 1)

			ginkgo.By("Add the clusterset back")
			assertCreatingClusterSet(clusterSet1Name)

			assertPlacementDecisionNumbers(placementName, namespace, 5, 1)
			assertPlacementConditionSatisfied(placementName, namespace, 5, false)
		})

		ginkgo.It("Should schedule successfully once a labelselector clusterset deleted/added", func() {
			assertCreatingClusterSet(clusterSet1Name, "vendor", "openShift")
			assertCreatingClusterSetBinding(clusterSet1Name, namespace)
			clusters1 := assertCreatingClusters(clusterName+"1", 1, "vendor", "openShift")
			clusters2 := assertCreatingClusters(clusterName+"2", 1, "vendor", "IKS")
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(placement, 1, 1)

			assertPlacementDecisionNumbers(placementName, namespace, 1, 1)
			assertPlacementConditionSatisfied(placementName, namespace, 1, false)

			ginkgo.By("Delete the clusterset")
			assertDeletingClusterSet(clusterSet1Name)

			assertPlacementDecisionNumbers(placementName, namespace, 0, 1)

			ginkgo.By("Add the clusterset back")
			assertCreatingClusterSet(clusterSet1Name, "vendor", "openShift")
			assertPlacementDecisionNumbers(placementName, namespace, 1, 1)
			assertPlacementConditionSatisfied(placementName, namespace, 1, false)

			ginkgo.By("Delete the cluster")
			assertDeletingClusters(clusters1...)
			assertPlacementDecisionNumbers(placementName, namespace, 0, 1)

			assertDeletingClusters(clusters2...)
		})

		ginkgo.It("Should schedule successfully once a clustersetbinding deleted/added", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(placement, 5, 1)
			assertPlacementDecisionNumbers(placementName, namespace, 5, 1)
			assertPlacementConditionSatisfied(placementName, namespace, 5, false)

			ginkgo.By("Delete the clustersetbinding")
			assertDeletingClusterSetBinding(clusterSet1Name, namespace)

			assertPlacementDecisionNumbers(placementName, namespace, 0, 1)

			ginkgo.By("Add the clustersetbinding back")
			assertCreatingClusterSetBinding(clusterSet1Name, namespace)

			assertPlacementDecisionNumbers(placementName, namespace, 5, 1)
			assertPlacementConditionSatisfied(placementName, namespace, 5, false)
		})
	})
})
