package placement

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("Placement", func() {
	var cancel context.CancelFunc
	var namespace string
	var placementName string
	var clusterName string
	var clusterSet1Name, clusterSet2Name string
	var suffix string
	var err error

	BeforeEach(func(ctx SpecContext) {
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
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// start controller manager
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
		err := kubeClient.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		assertCleanupClusters(ctx)
	})

	Context("Scheduling", func() {
		AfterEach(func(ctx SpecContext) {
			By("Delete placement")
			err = clusterClient.ClusterV1beta1().Placements(namespace).Delete(ctx, placementName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
			assertPlacementDeleted(ctx, placementName, namespace)

		})

		It("Should re-create placementdecisions successfully once placementdecisions are deleted", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 1)

			By("Delete placementdecisions")
			placementDecisions, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: placementLabel + "=" + placementName,
			})
			Expect(err).ToNot(HaveOccurred())
			for _, placementDecision := range placementDecisions.Items {
				err = clusterClient.ClusterV1beta1().PlacementDecisions(namespace).Delete(ctx, placementDecision.Name, metav1.DeleteOptions{})
				Expect(err).ToNot(HaveOccurred())
			}

			// check if the placementdecisions are re-created
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Get(ctx, placementName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			assertPlacementDecisionCreated(ctx, placement)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 5, 1)
			assertPlacementStatusDecisionGroups(ctx,
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{{Decisions: []string{testinghelpers.PlacementDecisionName(placementName, 1)}, ClustersCount: 5}})
		})

		It("Should re-create placementdecisions successfully once selected clusters are terminating", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 1)

			By("Find the first managedcluster")
			clusters, err := clusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(clusters.Items).To(HaveLen(5))
			terminatingCluster := clusters.Items[0].Name

			By("Add a finalizer for the managedcluster: " + terminatingCluster)
			Eventually(func() error {
				managedCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx,
					terminatingCluster, metav1.GetOptions{})
				if err != nil {
					return err
				}
				managedCluster.Finalizers = append(managedCluster.Finalizers, "test")
				_, err = clusterClient.ClusterV1().ManagedClusters().Update(ctx,
					managedCluster, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("Delete the managedcluster: " + terminatingCluster)
			err = clusterClient.ClusterV1().ManagedClusters().Delete(ctx, terminatingCluster, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())

			// check if the placementdecisions are re-created
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Get(ctx,
				placementName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			assertPlacementDecisionCreated(ctx, placement)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 4, 1)
			assertPlacementStatusDecisionGroups(ctx,
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{
					{Decisions: []string{testinghelpers.PlacementDecisionName(placementName, 1)}, ClustersCount: 4},
				})

			By("Delete the finalizer for the managedcluster: " + terminatingCluster)
			Eventually(func() error {
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
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		It("Should create empty placementdecision when no cluster selected", func(ctx SpecContext) {
			placement := testinghelpers.NewPlacement(namespace, placementName).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 0, 1)
			assertPlacementStatusDecisionGroups(ctx,
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{{Decisions: []string{testinghelpers.PlacementDecisionName(placementName, 1)}, ClustersCount: 0}})
		})

		It("Should create multiple placementdecisions once scheduled", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 101)
			placement := testinghelpers.NewPlacement(namespace, placementName).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 101, 2)

			nod := 101
			assertPlacementDecisionNumbers(ctx, placementName, namespace, nod, 2)
			assertPlacementStatusDecisionGroups(ctx,
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{{
					Decisions: []string{
						testinghelpers.PlacementDecisionName(placementName, 1),
						testinghelpers.PlacementDecisionName(placementName, 2),
					},
					ClustersCount: 101}})
			assertPlacementConditionSatisfied(ctx, placementName, namespace, nod, true)
		})

		It("Should schedule placement successfully once spec.ClusterSets changes", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertBindingClusterSet(ctx, clusterSet2Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 2)
			assertCreatingClusters(ctx, clusterSet2Name, 3)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 1)

			// update ClusterSets
			placement.Spec.ClusterSets = []string{clusterSet1Name}
			assertPatchingPlacementSpec(ctx, placement)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 2, 1)
		})

		It("Should schedule successfully once spec.NumberOfClusters is reduced", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 1)

			By("Reduce NOC of the placement")
			noc := int32(4)
			placement.Spec.NumberOfClusters = &noc
			assertPatchingPlacementSpec(ctx, placement)

			nod := int(noc)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, nod, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, nod, true)
		})

		It("Should schedule successfully once spec.NumberOfClusters is increased", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 10)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(5).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 1)

			By("Increase NOC of the placement")
			noc := int32(8)
			placement.Spec.NumberOfClusters = &noc
			assertPatchingPlacementSpec(ctx, placement)

			nod := int(noc)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, nod, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, nod, true)
		})

		It("Should schedule successfully once MisConfigured is fixed", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 1)

			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(1).AddToleration(&clusterapiv1beta1.Toleration{
				Key:      "key1",
				Operator: clusterapiv1beta1.TolerationOpExists,
				Value:    "value1",
			}).Build()
			assertCreatingPlacement(ctx, placement)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 0, 1)
			assertPlacementConditionMisconfigured(ctx, placementName, namespace, true)

			placement.Spec.Tolerations = []clusterapiv1beta1.Toleration{}
			assertPatchingPlacementSpec(ctx, placement)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 1, 1)
			assertPlacementConditionMisconfigured(ctx, placementName, namespace, false)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, 1, true)
		})

		It("Should be satisfied once new clusters are added", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 1)

			// add more clusters
			By("Add the cluster")
			assertCreatingClusters(ctx, clusterSet1Name, 5)

			nod := 10
			assertPlacementDecisionNumbers(ctx, placementName, namespace, nod, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, nod, true)
		})

		It("Should schedule successfully once clusters belong to labelselector clusterset are added/deleted", func(ctx SpecContext) {
			By("Bind clusterset to the placement namespace")
			assertCreatingClusterSet(ctx, clusterSet1Name, "vendor", "openShift")
			assertCreatingClusterSetBinding(ctx, clusterSet1Name, namespace)
			clusters1 := assertCreatingClusters(ctx, clusterName+"1", 1, "vendor", "openShift")
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 1, 1)

			// add more clusters
			By("Add the cluster")
			clusters2 := assertCreatingClusters(ctx, clusterName+"2", 1, "vendor", "openShift")

			assertPlacementDecisionNumbers(ctx, placementName, namespace, 2, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, 2, true)

			By("Delete the cluster")
			assertDeletingClusters(ctx, clusters1...)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 1, 1)

			assertDeletingClusters(ctx, clusters2...)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 0, 1)
		})

		It("Should update the decision group once clusters added/deleted", func(ctx SpecContext) {
			By("Bind clusterset to the placement namespace")
			assertCreatingClusterSet(ctx, "global")
			assertCreatingClusterSetBinding(ctx, "global", namespace)

			canary := assertCreatingClusters(ctx, clusterSet1Name, 2, "vendor", "openShift")
			noncanary := assertCreatingClusters(ctx, clusterSet1Name, 3)

			placement := testinghelpers.NewPlacement(namespace, placementName).WithGroupStrategy(clusterapiv1beta1.GroupStrategy{
				ClustersPerDecisionGroup: intstr.FromInt(2),
				DecisionGroups: []clusterapiv1beta1.DecisionGroup{
					{
						GroupName: "canary",
						ClusterSelector: clusterapiv1beta1.GroupClusterSelector{
							LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"vendor": "openShift"}},
						},
					},
				},
			}).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 3)

			assertPlacementDecisionNumbers(ctx, placementName, namespace, 5, 3)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, 5, true)
			assertPlacementStatusDecisionGroups(ctx, placementName, namespace, []clusterapiv1beta1.DecisionGroupStatus{
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

			By("Delete the cluster")
			assertDeletingClusters(ctx, canary[0], noncanary[0])
			assertPlacementStatusDecisionGroups(ctx, placementName, namespace, []clusterapiv1beta1.DecisionGroupStatus{
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

			By("Add the canary cluster")
			c := assertCreatingClusters(ctx, clusterSet1Name, 1, "vendor", "openShift")
			canary = append(canary, c...)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 4, 2)
			assertPlacementStatusDecisionGroups(ctx, placementName, namespace, []clusterapiv1beta1.DecisionGroupStatus{
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
			By("Add the non canary cluster")
			c = assertCreatingClusters(ctx, clusterSet1Name, 1)
			noncanary = append(noncanary, c...)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 5, 3)
			assertPlacementStatusDecisionGroups(ctx, placementName, namespace, []clusterapiv1beta1.DecisionGroupStatus{
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

			By("Delete all the cluster")
			assertDeletingClusters(ctx, canary[1:]...)
			assertDeletingClusters(ctx, noncanary[1:]...)
			assertDeletingClusterSet(ctx, "global")
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 0, 1)
			assertPlacementStatusDecisionGroups(ctx,
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{{Decisions: []string{testinghelpers.PlacementDecisionName(placementName, 1)}, ClustersCount: 0}})
		})

		It("Should schedule successfully once clusters belong to global(empty labelselector) clusterset are added/deleted)", func(ctx SpecContext) {
			By("Bind global clusterset to the placement namespace")
			assertCreatingClusterSet(ctx, "global")
			assertCreatingClusterSetBinding(ctx, "global", namespace)
			clusters1 := assertCreatingClusters(ctx, clusterName+"1", 1)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 1, 1)

			By("Add the cluster")
			clusters2 := assertCreatingClusters(ctx, clusterName+"2", 1)

			assertPlacementDecisionNumbers(ctx, placementName, namespace, 2, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, 2, true)

			By("Delete the cluster")
			assertDeletingClusters(ctx, clusters1...)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 1, 1)

			assertDeletingClusters(ctx, clusters2...)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 0, 1)
		})

		It("Should schedule successfully once new clusterset is bound", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 1)

			By("Bind one more clusterset to the placement namespace")
			assertBindingClusterSet(ctx, clusterSet2Name, namespace)
			assertCreatingClusters(ctx, clusterSet2Name, 3)

			nod := 8
			assertPlacementDecisionNumbers(ctx, placementName, namespace, nod, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, nod, false)
		})

		It("Should schedule successfully once new labelselector clusterset is bound", func(ctx SpecContext) {
			By("Bind clusterset to the placement namespace")
			assertCreatingClusterSet(ctx, clusterSet1Name, "vendor", "openShift")
			assertCreatingClusterSetBinding(ctx, clusterSet1Name, namespace)
			clusters1 := assertCreatingClusters(ctx, clusterName+"1", 1, "vendor", "openShift")
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 1, 1)

			By("Bind one more labelselector clusterset to the placement namespace")
			assertCreatingClusterSet(ctx, clusterSet2Name, "vendor", "IKS")
			assertCreatingClusterSetBinding(ctx, clusterSet2Name, namespace)
			clusters2 := assertCreatingClusters(ctx, clusterName+"2", 1, "vendor", "IKS")

			nod := 2
			assertPlacementDecisionNumbers(ctx, placementName, namespace, nod, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, nod, true)

			assertDeletingClusters(ctx, clusters1[0], clusters2[0])
		})

		It("Should schedule successfully once a clusterset deleted/added", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 1)

			assertPlacementDecisionNumbers(ctx, placementName, namespace, 5, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, 5, false)

			By("Delete the clusterset")
			assertDeletingClusterSet(ctx, clusterSet1Name)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 0, 1)

			By("Add the clusterset back")
			assertCreatingClusterSet(ctx, clusterSet1Name)

			assertPlacementDecisionNumbers(ctx, placementName, namespace, 5, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, 5, false)
		})

		It("Should schedule successfully once a labelselector clusterset deleted/added", func(ctx SpecContext) {
			assertCreatingClusterSet(ctx, clusterSet1Name, "vendor", "openShift")
			assertCreatingClusterSetBinding(ctx, clusterSet1Name, namespace)
			clusters1 := assertCreatingClusters(ctx, clusterName+"1", 1, "vendor", "openShift")
			clusters2 := assertCreatingClusters(ctx, clusterName+"2", 1, "vendor", "IKS")
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 1, 1)

			assertPlacementDecisionNumbers(ctx, placementName, namespace, 1, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, 1, false)

			By("Delete the clusterset")
			assertDeletingClusterSet(ctx, clusterSet1Name)

			assertPlacementDecisionNumbers(ctx, placementName, namespace, 0, 1)

			By("Add the clusterset back")
			assertCreatingClusterSet(ctx, clusterSet1Name, "vendor", "openShift")
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 1, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, 1, false)

			By("Delete the cluster")
			assertDeletingClusters(ctx, clusters1...)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 0, 1)

			assertDeletingClusters(ctx, clusters2...)
		})

		It("Should schedule successfully once a clustersetbinding deleted/added", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 5)
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 1)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 5, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, 5, false)

			By("Delete the clustersetbinding")
			assertDeletingClusterSetBinding(ctx, clusterSet1Name, namespace)

			assertPlacementDecisionNumbers(ctx, placementName, namespace, 0, 1)

			By("Add the clustersetbinding back")
			assertCreatingClusterSetBinding(ctx, clusterSet1Name, namespace)

			assertPlacementDecisionNumbers(ctx, placementName, namespace, 5, 1)
			assertPlacementConditionSatisfied(ctx, placementName, namespace, 5, false)
		})
	})
})
