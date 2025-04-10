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

var _ = ginkgo.Describe("Predicates", func() {
	var cancel context.CancelFunc
	var namespace string
	var placementName string
	var clusterSet1Name string
	var suffix string

	ginkgo.BeforeEach(func() {
		suffix = rand.String(5)
		namespace = fmt.Sprintf("ns-%s", suffix)
		placementName = fmt.Sprintf("placement-%s", suffix)
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

	ginkgo.Context("Cluster Selector", func() {
		ginkgo.It("Should schedule placement successfully once spec.Predicates LabelSelector changes", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 2, "cloud", "Azure")
			assertCreatingClusters(clusterSet1Name, 3, "cloud", "Amazon")
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(placement, 5, 1)

			ginkgo.By("add the predicates")
			// add a predicates
			predicates := []clusterapiv1beta1.ClusterPredicate{
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
			placement.Spec.Predicates = predicates
			assertPatchingPlacementSpec(placement)
			assertPlacementDecisionNumbers(placementName, namespace, 3, 1)
			assertPlacementStatusDecisionGroups(
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{{Decisions: []string{testinghelpers.PlacementDecisionName(placementName, 1)}, ClustersCount: 3}})

			ginkgo.By("change the predicates")
			// change the predicates
			predicates = []clusterapiv1beta1.ClusterPredicate{
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
			placement.Spec.Predicates = predicates
			assertPatchingPlacementSpec(placement)
			assertPlacementDecisionNumbers(placementName, namespace, 2, 1)
		})

		ginkgo.It("Should schedule placement successfully once spec.Predicates ClaimSelector changes", func() {
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 2, "cloud", "Azure")
			assertCreatingClusters(clusterSet1Name, 3, "cloud", "Amazon")
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(placement, 5, 1)

			ginkgo.By("add the predicates")
			// add a predicates
			predicates := []clusterapiv1beta1.ClusterPredicate{
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
			placement.Spec.Predicates = predicates
			assertPatchingPlacementSpec(placement)
			assertPlacementDecisionNumbers(placementName, namespace, 3, 1)

			ginkgo.By("change the predicates")
			// change the predicates
			predicates = []clusterapiv1beta1.ClusterPredicate{
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
			placement.Spec.Predicates = predicates
			assertPatchingPlacementSpec(placement)
			assertPlacementDecisionNumbers(placementName, namespace, 2, 1)
		})

		ginkgo.It("Should schedule placement successfully once spec.Predicates CelSelector changes", func() {
			// cluster settings
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 2)

			// Creating the clusters with resources
			assertCreatingAddOnPlacementScores(clusterNames[0], "demo", "demo", 70)
			assertCreatingAddOnPlacementScores(clusterNames[1], "demo", "demo", 90)

			// Checking the result of the placement
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(1).AddPredicate(nil, nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`xxxx.expression`},
				}).Build()
			assertCreatingPlacement(placement)
			assertPlacementDecisionNumbers(placementName, namespace, 0, 1)
			assertPlacementConditionMisconfigured(placementName, namespace, true)

			placement.Spec.Predicates[0].RequiredClusterSelector.CelSelector.CelExpressions = []string{
				`managedCluster.scores("demo").filter(s, s.name == 'demo').all(e, e.value > 80)`,
			}
			assertPatchingPlacementSpec(placement)
			assertPlacementDecisionNumbers(placementName, namespace, 1, 1)
			assertPlacementDecisionClusterNames(placementName, namespace, []string{clusterNames[1]})
		})
	})
})
