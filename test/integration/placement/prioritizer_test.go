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

	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	controllers "open-cluster-management.io/ocm/pkg/placement/controllers"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Prioritizers", func() {
	var cancel context.CancelFunc
	var namespace string
	var placementName string
	var clusterName string
	var clusterSet1Name string
	var suffix string

	ginkgo.BeforeEach(func() {
		suffix = rand.String(5)
		namespace = fmt.Sprintf("ns-%s", suffix)
		placementName = fmt.Sprintf("placement-%s", suffix)
		clusterName = fmt.Sprintf("cluster-%s", suffix)
		clusterSet1Name = fmt.Sprintf("clusterset-%s", suffix)

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
	})

	ginkgo.Context("BuiltinPlugin", func() {
		ginkgo.It("Should schedule successfully with default SchedulePolicy", func() {
			clusterResources := make([][]string, 3)
			clusterResources[0] = []string{"10", "10", "50", "100"}
			clusterResources[1] = []string{"7", "10", "90", "100"}
			clusterResources[2] = []string{"9", "10", "80", "100"}

			// Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			for i, name := range clusterNames {
				assertPatchingClusterStatusWithResources(name, clusterResources[i])
			}

			// Checking the result of the placement
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).Build()
			assertCreatingPlacementWithDecision(placement, 2, 1)
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[0], clusterNames[1]})
		})

		ginkgo.It("Should schedule successfully based on SchedulePolicy ResourceAllocatableCPU & ResourceAllocatableMemory", func() {
			// cluster settings
			clusterResources := make([][]string, 3)
			clusterResources[0] = []string{"10", "10", "50", "100"}
			clusterResources[1] = []string{"7", "10", "90", "100"}
			clusterResources[2] = []string{"9", "10", "80", "100"}

			// Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			for i, name := range clusterNames {
				assertPatchingClusterStatusWithResources(name, clusterResources[i])
			}

			// Checking the result of the placement
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).
				WithPrioritizerPolicy(clusterapiv1beta1.PrioritizerPolicyModeExact).
				WithPrioritizerConfig("ResourceAllocatableCPU", 1).
				WithPrioritizerConfig("ResourceAllocatableMemory", 1).Build()
			assertCreatingPlacementWithDecision(placement, 2, 1)
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[0], clusterNames[2]})

		})

		ginkgo.It("Should keep steady successfully even placementdecisions' balance and cluster situation changes", func() {
			// cluster settings
			clusterResources := make([][]string, 3)
			clusterResources[0] = []string{"10", "10", "50", "100"}
			clusterResources[1] = []string{"7", "10", "90", "100"}
			clusterResources[2] = []string{"9", "10", "80", "100"}

			// Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			for i, name := range clusterNames {
				assertPatchingClusterStatusWithResources(name, clusterResources[i])
			}

			// Checking the result of the placement
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).
				WithPrioritizerPolicy(clusterapiv1beta1.PrioritizerPolicyModeAdditive).
				WithPrioritizerConfig("Steady", 3).
				WithPrioritizerConfig("ResourceAllocatableCPU", 1).
				WithPrioritizerConfig("ResourceAllocatableMemory", 1).Build()
			assertCreatingPlacementWithDecision(placement, 2, 1)
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[0], clusterNames[2]})

			ginkgo.By("Adding fake placement decisions")
			assertCreatingPlacementDecision(placementName+"-1", namespace, []string{clusterNames[1]})
			ginkgo.By("Adding a new cluster with resources")
			clusterNames = append(clusterNames, clusterName+"-4")
			newClusterResources := []string{"10", "10", "100", "100"}
			newClusters := assertCreatingClusters(clusterSet1Name, 1)
			assertPatchingClusterStatusWithResources(newClusters[0], newClusterResources)

			// Checking the result of the placement
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[0], clusterNames[2]})
		})

		ginkgo.It("Should schedule successfully based on SchedulePolicy balance", func() {
			// cluster settings
			clusterResources := make([][]string, 3)
			clusterResources[0] = []string{"10", "10", "50", "100"}
			clusterResources[1] = []string{"7", "10", "90", "100"}
			clusterResources[2] = []string{"9", "10", "80", "100"}

			// Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			for i, name := range clusterNames {
				assertPatchingClusterStatusWithResources(name, clusterResources[i])
			}

			ginkgo.By("Adding fake placement decisions")
			assertCreatingPlacementDecision("fake-1", namespace, []string{clusterNames[0]})

			// Checking the result of the placement
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).
				WithPrioritizerPolicy(clusterapiv1beta1.PrioritizerPolicyModeExact).
				WithPrioritizerConfig("Balance", 2).
				WithPrioritizerConfig("ResourceAllocatableCPU", 1).Build()
			assertCreatingPlacementWithDecision(placement, 2, 1)
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[1], clusterNames[2]})
		})

		ginkgo.It("Should re-schedule successfully once a new cluster with resources added/deleted", func() {
			// cluster settings
			clusterResources := make([][]string, 3)
			clusterResources[0] = []string{"10", "10", "50", "100"}
			clusterResources[1] = []string{"7", "10", "90", "100"}
			clusterResources[2] = []string{"9", "10", "80", "100"}

			// Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			for i, name := range clusterNames {
				assertPatchingClusterStatusWithResources(name, clusterResources[i])
			}

			// Checking the result of the placement
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).
				WithPrioritizerPolicy(clusterapiv1beta1.PrioritizerPolicyModeExact).
				WithPrioritizerConfig("ResourceAllocatableCPU", 1).
				WithPrioritizerConfig("ResourceAllocatableMemory", 1).Build()
			assertCreatingPlacementWithDecision(placement, 2, 1)
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[0], clusterNames[2]})

			ginkgo.By("Adding a new cluster with resources")
			clusterNames = append(clusterNames, clusterName+"-4")
			newClusterResources := []string{"10", "10", "100", "100"}
			newClusters := assertCreatingClusters(clusterSet1Name, 1)
			assertPatchingClusterStatusWithResources(newClusters[0], newClusterResources)

			// Checking the result of the placement
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[2], newClusters[0]})

			ginkgo.By("Deleting the cluster")
			assertDeletingClusters(newClusters[0])

			// Checking the result of the placement
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[0], clusterNames[2]})
		})
	})

	ginkgo.Context("AddonScore", func() {
		ginkgo.It("Should schedule successfully based on AddOnPlacementScore", func() {
			// cluster settings

			// Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			assertCreatingAddOnPlacementScores(clusterNames[0], "demo", "demo", 80)
			assertCreatingAddOnPlacementScores(clusterNames[1], "demo", "demo", 90)
			assertCreatingAddOnPlacementScores(clusterNames[2], "demo", "demo", 100)

			// Checking the result of the placement
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).
				WithPrioritizerPolicy(clusterapiv1beta1.PrioritizerPolicyModeExact).
				WithScoreCoordinateAddOn("demo", "demo", 1).Build()
			assertCreatingPlacementWithDecision(placement, 2, 1)
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[1], clusterNames[2]})
		})

		ginkgo.It("Should reschedule every ResyncInterval and update desicion when AddOnPlacementScore changes", func() {
			// Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)

			// Creating the placement
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(2).
				WithPrioritizerPolicy(clusterapiv1beta1.PrioritizerPolicyModeExact).
				WithScoreCoordinateAddOn("demo", "demo", 1).Build()
			assertCreatingPlacementWithDecision(placement, 2, 1)

			// Checking the result of the placement when no AddOnPlacementScores
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[0], clusterNames[1]})

			// Creating the AddOnPlacementScores
			assertCreatingAddOnPlacementScores(clusterNames[0], "demo", "demo", 80)
			assertCreatingAddOnPlacementScores(clusterNames[1], "demo", "demo", 90)
			assertCreatingAddOnPlacementScores(clusterNames[2], "demo", "demo", 100)

			// Checking the result of the placement when AddOnPlacementScores added
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[1], clusterNames[2]})

			// update the AddOnPlacementScores
			assertCreatingAddOnPlacementScores(clusterNames[0], "demo", "demo", 100)
			assertCreatingAddOnPlacementScores(clusterNames[1], "demo", "demo", 90)
			assertCreatingAddOnPlacementScores(clusterNames[2], "demo", "demo", 100)

			// Checking the result of the placement when AddOnPlacementScores updated
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[0], clusterNames[2]})

		})
	})
})
