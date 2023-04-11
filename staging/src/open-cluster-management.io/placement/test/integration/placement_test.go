package integration

import (
	"context"
	"fmt"
	"time"

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
)

const (
	clusterSetLabel          = "cluster.open-cluster-management.io/clusterset"
	placementLabel           = "cluster.open-cluster-management.io/placement"
	maxNumOfClusterDecisions = 100
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
			assertCreatingPlacementWithDecision(placementName, namespace, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

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
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			assertPlacementDecisionCreated(placement)
			assertNumberOfDecisions(placementName, namespace, 5)
		})

		ginkgo.It("Should create empty placementdecision when no cluster selected", func() {
			placement := assertCreatingPlacement(placementName, namespace, nil, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})
			assertPlacementDecisionCreated(placement)
			assertNumberOfDecisions(placementName, namespace, 0)
		})

		ginkgo.It("Should create multiple placementdecisions once scheduled", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 101)
			assertCreatingPlacementWithDecision(placementName, namespace, nil, 101, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			nod := 101
			assertNumberOfDecisions(placementName, namespace, nod)
			assertPlacementConditionSatisfied(placementName, namespace, nod, true)
		})

		ginkgo.It("Should schedule placement successfully once spec.ClusterSets changes", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertBindingClusterSet(clusterSet2Name, namespace)
			assertCreatingClusters(clusterSet1Name, 2)
			assertCreatingClusters(clusterSet2Name, 3)
			assertCreatingPlacementWithDecision(placementName, namespace, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			// update ClusterSets
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			placement.Spec.ClusterSets = []string{clusterSet1Name}
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			assertNumberOfDecisions(placementName, namespace, 2)
		})

		ginkgo.It("Should schedule placement successfully once spec.Predicates LabelSelector changes", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 2, "cloud", "Azure")
			assertCreatingClusters(clusterSet1Name, 3, "cloud", "Amazon")
			assertCreatingPlacementWithDecision(placementName, namespace, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			ginkgo.By("add the predicates")
			// add a predicates
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			placement.Spec.Predicates = []clusterapiv1beta1.ClusterPredicate{
				{
					RequiredClusterSelector: clusterapiv1beta1.ClusterSelector{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"cloud": "Amazon",
							},
						},
					},
				},
			}
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			assertNumberOfDecisions(placementName, namespace, 3)

			ginkgo.By("change the predicates")
			// change the predicates
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			placement.Spec.Predicates = []clusterapiv1beta1.ClusterPredicate{
				{
					RequiredClusterSelector: clusterapiv1beta1.ClusterSelector{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"cloud": "Azure",
							},
						},
					},
				},
			}
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			assertNumberOfDecisions(placementName, namespace, 2)
		})

		ginkgo.It("Should schedule placement successfully once spec.Predicates ClaimSelector changes", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 2, "cloud", "Azure")
			assertCreatingClusters(clusterSet1Name, 3, "cloud", "Amazon")
			assertCreatingPlacementWithDecision(placementName, namespace, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			ginkgo.By("add the predicates")
			// add a predicates
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			placement.Spec.Predicates = []clusterapiv1beta1.ClusterPredicate{
				{
					RequiredClusterSelector: clusterapiv1beta1.ClusterSelector{
						ClaimSelector: clusterapiv1beta1.ClusterClaimSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "cloud",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"Amazon"},
								},
							},
						},
					},
				},
			}
			_, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			assertNumberOfDecisions(placementName, namespace, 3)

			ginkgo.By("change the predicates")
			// change the predicates
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			placement.Spec.Predicates = []clusterapiv1beta1.ClusterPredicate{
				{
					RequiredClusterSelector: clusterapiv1beta1.ClusterSelector{
						ClaimSelector: clusterapiv1beta1.ClusterClaimSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "cloud",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"Azure"},
								},
							},
						},
					},
				},
			}
			_, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			assertNumberOfDecisions(placementName, namespace, 2)
		})

		ginkgo.It("Should schedule successfully once spec.NumberOfClusters is reduced", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacementWithDecision(placementName, namespace, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			ginkgo.By("Reduce NOC of the placement")
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			noc := int32(4)
			placement.Spec.NumberOfClusters = &noc
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			nod := int(noc)
			assertNumberOfDecisions(placementName, namespace, nod)
			assertPlacementConditionSatisfied(placementName, namespace, nod, true)
		})

		ginkgo.It("Should schedule successfully once spec.NumberOfClusters is increased", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 10)
			assertCreatingPlacementWithDecision(placementName, namespace, noc(5), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			ginkgo.By("Increase NOC of the placement")
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			noc := int32(8)
			placement.Spec.NumberOfClusters = &noc
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			nod := int(noc)
			assertNumberOfDecisions(placementName, namespace, nod)
			assertPlacementConditionSatisfied(placementName, namespace, nod, true)
		})

		ginkgo.It("Should schedule successfully once MisConfigured is fixed", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 1)

			assertCreatingPlacement(placementName, namespace, noc(1), clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: clusterapiv1beta1.TolerationOpExists,
					Value:    "value1",
				},
			})
			assertNumberOfDecisions(placementName, namespace, 0)
			assertPlacementConditionMisconfigured(placementName, namespace, true)

			assertUpdatingPlacement(placementName, namespace, noc(1), clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})
			assertNumberOfDecisions(placementName, namespace, 1)
			assertPlacementConditionMisconfigured(placementName, namespace, false)
			assertPlacementConditionSatisfied(placementName, namespace, 1, true)
		})

		ginkgo.It("Should be satisfied once new clusters are added", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacementWithDecision(placementName, namespace, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			// add more clusters
			ginkgo.By("Add the cluster")
			assertCreatingClusters(clusterSet1Name, 5)

			nod := 10
			assertNumberOfDecisions(placementName, namespace, nod)
			assertPlacementConditionSatisfied(placementName, namespace, nod, true)
		})

		ginkgo.It("Should schedule successfully once clusters belong to labelselector clusterset are added/deleted", func() {
			ginkgo.By("Bind clusterset to the placement namespace")
			assertCreatingClusterSet(clusterSet1Name, "vendor", "openShift")
			assertCreatingClusterSetBinding(clusterSet1Name, namespace)
			clusters1 := assertCreatingClusters(clusterName+"1", 1, "vendor", "openShift")
			assertCreatingPlacementWithDecision(placementName, namespace, noc(2), 1, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			// add more clusters
			ginkgo.By("Add the cluster")
			clusters2 := assertCreatingClusters(clusterName+"2", 1, "vendor", "openShift")

			assertNumberOfDecisions(placementName, namespace, 2)
			assertPlacementConditionSatisfied(placementName, namespace, 2, true)

			ginkgo.By("Delete the cluster")
			assertDeletingClusters(clusters1...)
			assertNumberOfDecisions(placementName, namespace, 1)

			assertDeletingClusters(clusters2...)
			assertNumberOfDecisions(placementName, namespace, 0)
		})

		ginkgo.It("Should schedule successfully once clusters belong to global(empty labelselector) clusterset are added/deleted)", func() {
			ginkgo.By("Bind global clusterset to the placement namespace")
			assertCreatingClusterSet("global")
			assertCreatingClusterSetBinding("global", namespace)
			clusters1 := assertCreatingClusters(clusterName+"1", 1)
			assertCreatingPlacementWithDecision(placementName, namespace, noc(2), 1, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			ginkgo.By("Add the cluster")
			clusters2 := assertCreatingClusters(clusterName+"2", 1)

			assertNumberOfDecisions(placementName, namespace, 2)
			assertPlacementConditionSatisfied(placementName, namespace, 2, true)

			ginkgo.By("Delete the cluster")
			assertDeletingClusters(clusters1...)
			assertNumberOfDecisions(placementName, namespace, 1)

			assertDeletingClusters(clusters2...)
			assertNumberOfDecisions(placementName, namespace, 0)
		})

		ginkgo.It("Should schedule successfully once new clusterset is bound", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacementWithDecision(placementName, namespace, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			ginkgo.By("Bind one more clusterset to the placement namespace")
			assertBindingClusterSet(clusterSet2Name, namespace)
			assertCreatingClusters(clusterSet2Name, 3)

			nod := 8
			assertNumberOfDecisions(placementName, namespace, nod)
			assertPlacementConditionSatisfied(placementName, namespace, nod, false)
		})

		ginkgo.It("Should schedule successfully once new labelselector clusterset is bound", func() {
			ginkgo.By("Bind clusterset to the placement namespace")
			assertCreatingClusterSet(clusterSet1Name, "vendor", "openShift")
			assertCreatingClusterSetBinding(clusterSet1Name, namespace)
			clusters1 := assertCreatingClusters(clusterName+"1", 1, "vendor", "openShift")
			assertCreatingPlacementWithDecision(placementName, namespace, noc(2), 1, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			ginkgo.By("Bind one more labelselector clusterset to the placement namespace")
			assertCreatingClusterSet(clusterSet2Name, "vendor", "IKS")
			assertCreatingClusterSetBinding(clusterSet2Name, namespace)
			clusters2 := assertCreatingClusters(clusterName+"2", 1, "vendor", "IKS")

			nod := 2
			assertNumberOfDecisions(placementName, namespace, nod)
			assertPlacementConditionSatisfied(placementName, namespace, nod, true)

			assertDeletingClusters(clusters1[0], clusters2[0])
		})

		ginkgo.It("Should schedule successfully once a clusterset deleted/added", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacementWithDecision(placementName, namespace, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			assertNumberOfDecisions(placementName, namespace, 5)
			assertPlacementConditionSatisfied(placementName, namespace, 5, false)

			ginkgo.By("Delete the clusterset")
			assertDeletingClusterSet(clusterSet1Name)
			assertNumberOfDecisions(placementName, namespace, 0)

			ginkgo.By("Add the clusterset back")
			assertCreatingClusterSet(clusterSet1Name)

			assertNumberOfDecisions(placementName, namespace, 5)
			assertPlacementConditionSatisfied(placementName, namespace, 5, false)
		})

		ginkgo.It("Should schedule successfully once a labelselector clusterset deleted/added", func() {
			assertCreatingClusterSet(clusterSet1Name, "vendor", "openShift")
			assertCreatingClusterSetBinding(clusterSet1Name, namespace)
			clusters1 := assertCreatingClusters(clusterName+"1", 1, "vendor", "openShift")
			clusters2 := assertCreatingClusters(clusterName+"2", 1, "vendor", "IKS")
			assertCreatingPlacementWithDecision(placementName, namespace, noc(10), 1, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			assertNumberOfDecisions(placementName, namespace, 1)
			assertPlacementConditionSatisfied(placementName, namespace, 1, false)

			ginkgo.By("Delete the clusterset")
			assertDeletingClusterSet(clusterSet1Name)

			assertNumberOfDecisions(placementName, namespace, 0)

			ginkgo.By("Add the clusterset back")
			assertCreatingClusterSet(clusterSet1Name, "vendor", "openShift")
			assertNumberOfDecisions(placementName, namespace, 1)
			assertPlacementConditionSatisfied(placementName, namespace, 1, false)

			ginkgo.By("Delete the cluster")
			assertDeletingClusters(clusters1...)
			assertNumberOfDecisions(placementName, namespace, 0)

			assertDeletingClusters(clusters2...)
		})

		ginkgo.It("Should schedule successfully once a clustersetbinding deleted/added", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacementWithDecision(placementName, namespace, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			assertNumberOfDecisions(placementName, namespace, 5)
			assertPlacementConditionSatisfied(placementName, namespace, 5, false)

			ginkgo.By("Delete the clustersetbinding")
			assertDeletingClusterSetBinding(clusterSet1Name, namespace)

			assertNumberOfDecisions(placementName, namespace, 0)

			ginkgo.By("Add the clustersetbinding back")
			assertCreatingClusterSetBinding(clusterSet1Name, namespace)

			assertNumberOfDecisions(placementName, namespace, 5)
			assertPlacementConditionSatisfied(placementName, namespace, 5, false)
		})

	})
})

func noc(n int) *int32 {
	noc := int32(n)
	return &noc
}
