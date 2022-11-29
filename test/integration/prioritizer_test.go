package integration

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
	controllers "open-cluster-management.io/placement/pkg/controllers"
	"open-cluster-management.io/placement/pkg/controllers/scheduling"
	"open-cluster-management.io/placement/test/integration/util"
	"time"
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
		scheduling.ResyncInterval = time.Second * 5
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

			// placement settings
			prioritizerPolicy := clusterapiv1beta1.PrioritizerPolicy{}

			//Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			for i, name := range clusterNames {
				assertUpdatingClusterWithClusterResources(name, clusterResources[i])
			}

			//Checking the result of the placement
			assertCreatingPlacementWithDecision(placementName, namespace, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[0], clusterNames[1]})
		})

		ginkgo.It("Should schedule successfully based on SchedulePolicy ResourceAllocatableCPU & ResourceAllocatableMemory", func() {
			// cluster settings
			clusterResources := make([][]string, 3)
			clusterResources[0] = []string{"10", "10", "50", "100"}
			clusterResources[1] = []string{"7", "10", "90", "100"}
			clusterResources[2] = []string{"9", "10", "80", "100"}

			// placement settings
			prioritizerPolicy := clusterapiv1beta1.PrioritizerPolicy{
				Mode: clusterapiv1beta1.PrioritizerPolicyModeExact,
				Configurations: []clusterapiv1beta1.PrioritizerConfig{
					{
						ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
							Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
							BuiltIn: "ResourceAllocatableCPU",
						},
						Weight: 1,
					},
					{
						ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
							Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
							BuiltIn: "ResourceAllocatableMemory",
						},
						Weight: 1,
					},
				},
			}

			//Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			for i, name := range clusterNames {
				assertUpdatingClusterWithClusterResources(name, clusterResources[i])
			}

			//Checking the result of the placement
			assertCreatingPlacementWithDecision(placementName, namespace, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[0], clusterNames[2]})

		})

		ginkgo.It("Should keep steady successfully even placementdecisions' balance and cluster situation changes", func() {
			// cluster settings
			clusterResources := make([][]string, 3)
			clusterResources[0] = []string{"10", "10", "50", "100"}
			clusterResources[1] = []string{"7", "10", "90", "100"}
			clusterResources[2] = []string{"9", "10", "80", "100"}

			// placement settings
			prioritizerPolicy := clusterapiv1beta1.PrioritizerPolicy{
				Mode: clusterapiv1beta1.PrioritizerPolicyModeAdditive,
				Configurations: []clusterapiv1beta1.PrioritizerConfig{
					{
						ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
							Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
							BuiltIn: "Steady",
						},
						Weight: 3,
					},
					{
						ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
							Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
							BuiltIn: "ResourceAllocatableCPU",
						},
						Weight: 1,
					},
					{
						ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
							Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
							BuiltIn: "ResourceAllocatableMemory",
						},
						Weight: 1,
					},
				},
			}
			//Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			for i, name := range clusterNames {
				assertUpdatingClusterWithClusterResources(name, clusterResources[i])
			}

			//Checking the result of the placement
			assertCreatingPlacementWithDecision(placementName, namespace, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[0], clusterNames[2]})

			ginkgo.By("Adding fake placement decisions")
			assertCreatingPlacementDecision(placementName+"-1", namespace, []string{clusterNames[1]})
			ginkgo.By("Adding a new cluster with resources")
			clusterNames = append(clusterNames, clusterName+"-4")
			newClusterResources := []string{"10", "10", "100", "100"}
			newClusters := assertCreatingClusters(clusterSet1Name, 1)
			assertUpdatingClusterWithClusterResources(newClusters[0], newClusterResources)

			//Checking the result of the placement
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[0], clusterNames[2]})
		})

		ginkgo.It("Should schedule successfully based on SchedulePolicy balance", func() {
			// cluster settings
			clusterResources := make([][]string, 3)
			clusterResources[0] = []string{"10", "10", "50", "100"}
			clusterResources[1] = []string{"7", "10", "90", "100"}
			clusterResources[2] = []string{"9", "10", "80", "100"}

			// placement settings
			prioritizerPolicy := clusterapiv1beta1.PrioritizerPolicy{
				Mode: clusterapiv1beta1.PrioritizerPolicyModeExact,
				Configurations: []clusterapiv1beta1.PrioritizerConfig{
					{
						ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
							Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
							BuiltIn: "Balance",
						},
						Weight: 2,
					},
					{
						ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
							Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
							BuiltIn: "ResourceAllocatableCPU",
						},
						Weight: 1,
					},
				},
			}
			//Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			for i, name := range clusterNames {
				assertUpdatingClusterWithClusterResources(name, clusterResources[i])
			}

			ginkgo.By("Adding fake placement decisions")
			assertCreatingPlacementDecision("fake-1", namespace, []string{clusterNames[0]})

			//Checking the result of the placement
			assertCreatingPlacementWithDecision(placementName, namespace, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[1], clusterNames[2]})
		})

		ginkgo.It("Should re-schedule successfully once a new cluster with resources added/deleted", func() {
			// cluster settings
			clusterResources := make([][]string, 3)
			clusterResources[0] = []string{"10", "10", "50", "100"}
			clusterResources[1] = []string{"7", "10", "90", "100"}
			clusterResources[2] = []string{"9", "10", "80", "100"}

			// placement settings
			prioritizerPolicy := clusterapiv1beta1.PrioritizerPolicy{
				Mode: clusterapiv1beta1.PrioritizerPolicyModeExact,
				Configurations: []clusterapiv1beta1.PrioritizerConfig{
					{
						ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
							Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
							BuiltIn: "ResourceAllocatableCPU",
						},
						Weight: 1,
					},
					{
						ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
							Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
							BuiltIn: "ResourceAllocatableMemory",
						},
						Weight: 1,
					},
				},
			}

			//Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			for i, name := range clusterNames {
				assertUpdatingClusterWithClusterResources(name, clusterResources[i])
			}

			//Checking the result of the placement
			assertCreatingPlacementWithDecision(placementName, namespace, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[0], clusterNames[2]})

			ginkgo.By("Adding a new cluster with resources")
			clusterNames = append(clusterNames, clusterName+"-4")
			newClusterResources := []string{"10", "10", "100", "100"}
			newClusters := assertCreatingClusters(clusterSet1Name, 1)
			assertUpdatingClusterWithClusterResources(newClusters[0], newClusterResources)

			//Checking the result of the placement
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[2], newClusters[0]})

			ginkgo.By("Deleting the cluster")
			assertDeletingClusters(newClusters[0])

			//Checking the result of the placement
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[0], clusterNames[2]})
		})
	})

	ginkgo.Context("AddonScore", func() {
		ginkgo.It("Should schedule successfully based on AddOnPlacementScore", func() {
			// cluster settings

			// placement settings
			prioritizerPolicy := clusterapiv1beta1.PrioritizerPolicy{
				Mode: clusterapiv1beta1.PrioritizerPolicyModeExact,
				Configurations: []clusterapiv1beta1.PrioritizerConfig{
					{
						ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
							Type: clusterapiv1beta1.ScoreCoordinateTypeAddOn,
							AddOn: &clusterapiv1beta1.AddOnScore{
								ResourceName: "demo",
								ScoreName:    "demo",
							},
						},
						Weight: 1,
					},
				},
			}

			//Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)
			assertCreatingAddOnPlacementScores(clusterNames[0], "demo", "demo", 80)
			assertCreatingAddOnPlacementScores(clusterNames[1], "demo", "demo", 90)
			assertCreatingAddOnPlacementScores(clusterNames[2], "demo", "demo", 100)

			//Checking the result of the placement
			assertCreatingPlacementWithDecision(placementName, namespace, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[1], clusterNames[2]})
		})

		ginkgo.It("Should reschedule every ResyncInterval and update desicion when AddOnPlacementScore changes", func() {
			// placement settings
			prioritizerPolicy := clusterapiv1beta1.PrioritizerPolicy{
				Mode: clusterapiv1beta1.PrioritizerPolicyModeExact,
				Configurations: []clusterapiv1beta1.PrioritizerConfig{
					{
						ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
							Type: clusterapiv1beta1.ScoreCoordinateTypeAddOn,
							AddOn: &clusterapiv1beta1.AddOnScore{
								ResourceName: "demo",
								ScoreName:    "demo",
							},
						},
						Weight: 1,
					},
				},
			}

			//Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)

			//Creating the placement
			assertCreatingPlacementWithDecision(placementName, namespace, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})

			//Checking the result of the placement when no AddOnPlacementScores
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[0], clusterNames[1]})

			//Creating the AddOnPlacementScores
			assertCreatingAddOnPlacementScores(clusterNames[0], "demo", "demo", 80)
			assertCreatingAddOnPlacementScores(clusterNames[1], "demo", "demo", 90)
			assertCreatingAddOnPlacementScores(clusterNames[2], "demo", "demo", 100)

			//Checking the result of the placement when AddOnPlacementScores added
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[1], clusterNames[2]})

			//update the AddOnPlacementScores
			assertCreatingAddOnPlacementScores(clusterNames[0], "demo", "demo", 100)
			assertCreatingAddOnPlacementScores(clusterNames[1], "demo", "demo", 90)
			assertCreatingAddOnPlacementScores(clusterNames[2], "demo", "demo", 100)

			//Checking the result of the placement when AddOnPlacementScores updated
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[0], clusterNames[2]})

		})
	})
})
