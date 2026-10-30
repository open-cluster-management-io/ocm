package placement

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	controllers "open-cluster-management.io/ocm/pkg/placement/controllers"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("Predicates", func() {
	var cancel context.CancelFunc
	var namespace string
	var placementName string
	var clusterSet1Name string
	var suffix string

	BeforeEach(func(ctx SpecContext) {
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
	})

	Context("Cluster Selector", func() {
		It("Should schedule placement successfully once spec.Predicates LabelSelector changes", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 2, "cloud", "Azure")
			assertCreatingClusters(ctx, clusterSet1Name, 3, "cloud", "Amazon")
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 1)

			By("add the predicates")
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
			assertPatchingPlacementSpec(ctx, placement)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 3, 1)
			assertPlacementStatusDecisionGroups(ctx,
				placementName, namespace,
				[]clusterapiv1beta1.DecisionGroupStatus{{Decisions: []string{testinghelpers.PlacementDecisionName(placementName, 1)}, ClustersCount: 3}})

			By("change the predicates")
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
			assertPatchingPlacementSpec(ctx, placement)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 2, 1)
		})

		It("Should schedule placement successfully once spec.Predicates ClaimSelector changes", func(ctx SpecContext) {
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 2, "cloud", "Azure")
			assertCreatingClusters(ctx, clusterSet1Name, 3, "cloud", "Amazon")
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(10).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 5, 1)

			By("add the predicates")
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
			assertPatchingPlacementSpec(ctx, placement)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 3, 1)

			By("change the predicates")
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
			assertPatchingPlacementSpec(ctx, placement)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 2, 1)
		})

		It("Should schedule placement successfully once spec.Predicates CelSelector changes", func(ctx SpecContext) {
			// cluster settings
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(ctx, clusterSet1Name, 2)

			// Creating the clusters with resources
			assertCreatingAddOnPlacementScores(ctx, clusterNames[0], "demo", "demo", 70)
			assertCreatingAddOnPlacementScores(ctx, clusterNames[1], "demo", "demo", 90)

			// Checking the result of the placement
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(1).AddPredicate(nil, nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`xxxx.expression`},
				}).Build()
			assertCreatingPlacement(ctx, placement)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 0, 1)
			assertPlacementConditionMisconfigured(ctx, placementName, namespace, true)

			placement.Spec.Predicates[0].RequiredClusterSelector.CelSelector.CelExpressions = []string{
				`managedCluster.scores("demo").filter(s, s.name == 'demo').all(e, e.value > 80)`,
			}
			assertPatchingPlacementSpec(ctx, placement)
			assertPlacementDecisionNumbers(ctx, placementName, namespace, 1, 1)
			assertPlacementDecisionClusterNames(ctx, placementName, namespace, []string{clusterNames[1]})
		})
	})
})
