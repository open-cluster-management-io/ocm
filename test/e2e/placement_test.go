package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	e2eTestLabel             = "created-by"
	e2eTestLabelValue        = "placement-e2e-test"
	maxNumOfClusterDecisions = 100
)

// Test cases with lable "sanity-check" could be ran as sanity check on an existing environment with
// placement controller installed and well configured . Resource leftovers should be cleaned up on
// the hub cluster.
var _ = Describe("Placement", Label("placement", "sanity-check"), func() {
	var namespace string
	var placementName string
	var clusterSet1Name string
	var clusterSetGlobal string
	var suffix string
	var err error

	BeforeEach(func(ctx SpecContext) {
		suffix = rand.String(5)
		namespace = fmt.Sprintf("ns-%s", suffix)
		placementName = fmt.Sprintf("placement-%s", suffix)
		clusterSet1Name = fmt.Sprintf("clusterset-%s", suffix)
		clusterSetGlobal = "global"

		// create testing namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Labels: map[string]string{
					e2eTestLabel: e2eTestLabelValue,
				},
			},
		}
		_, err := hub.KubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func(ctx SpecContext) {
		var errs []error
		By("Delete managedclustersets")
		err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: e2eTestLabel + "=" + e2eTestLabelValue,
		})
		if err != nil {
			errs = append(errs, err)
		}

		By("Delete managedclusters")
		err = hub.ClusterClient.ClusterV1().ManagedClusters().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: e2eTestLabel + "=" + e2eTestLabelValue,
		})
		if err != nil {
			errs = append(errs, err)
		}

		By("Delete namespace")
		err = hub.KubeClient.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
		if err != nil {
			errs = append(errs, err)
		}
		Expect(utilerrors.NewAggregate(errs)).ToNot(HaveOccurred())

	})

	assertPlacementDecisionCreated := func(ctx context.Context, placement *clusterapiv1beta1.Placement) {
		By("Check if placementdecision is created")
		Eventually(func() bool {
			pdl, err := hub.ClusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: clusterapiv1beta1.PlacementLabel + "=" + placement.Name,
			})
			if err != nil {
				return false
			}
			if len(pdl.Items) == 0 {
				return false
			}
			for _, pd := range pdl.Items {
				objectMeta := pd.ObjectMeta
				if controlled := metav1.IsControlledBy(&objectMeta, placement); !controlled {
					return false
				}
			}
			return true
		}).Should(BeTrue())
	}

	assertNumberOfDecisions := func(ctx context.Context, placementName string, desiredNOD int) {
		By("Check the number of decisions in placementdecisions")
		desiredNOPD := desiredNOD/maxNumOfClusterDecisions + 1
		Eventually(func() bool {
			pdl, err := hub.ClusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: clusterapiv1beta1.PlacementLabel + "=" + placementName,
			})
			if err != nil {
				return false
			}
			if len(pdl.Items) != desiredNOPD {
				return false
			}
			actualNOD := 0
			for _, pd := range pdl.Items {
				actualNOD += len(pd.Status.Decisions)
			}
			return actualNOD == desiredNOD
		}).Should(BeTrue())
	}

	assertPlacementStatus := func(ctx context.Context, placementName string, numOfSelectedClusters int, satisfied bool) {
		By("Check the status of placement")
		Eventually(func() bool {
			placement, err := hub.ClusterClient.ClusterV1beta1().Placements(namespace).Get(ctx, placementName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			status := metav1.ConditionFalse
			if satisfied {
				status = metav1.ConditionTrue
			}
			if !util.HasCondition(
				placement.Status.Conditions,
				clusterapiv1beta1.PlacementConditionSatisfied,
				"",
				status,
			) {
				return false
			}

			if !util.HasCondition(
				placement.Status.Conditions,
				clusterapiv1beta1.PlacementConditionMisconfigured,
				"Succeedconfigured",
				metav1.ConditionFalse,
			) {
				return false
			}

			return placement.Status.NumberOfSelectedClusters == int32(numOfSelectedClusters) //nolint:gosec
		}).Should(BeTrue())
	}

	assertCreatingClusterSet := func(ctx context.Context, clusterSetName string, matchLabel map[string]string) {
		By("Create clusterset")
		clusterset := &clusterapiv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
				Labels: map[string]string{
					e2eTestLabel: e2eTestLabelValue,
				},
			},
		}
		if matchLabel != nil {
			clusterset.Spec.ClusterSelector = clusterapiv1beta2.ManagedClusterSelector{
				SelectorType: clusterapiv1beta2.LabelSelector,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: matchLabel,
				},
			}
		}
		_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterset, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	}

	assertCreatingClusterSetBinding := func(ctx context.Context, clusterSetName string) {
		By("Create clustersetbinding")
		csb := &clusterapiv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      clusterSetName,
				Labels: map[string]string{
					e2eTestLabel: e2eTestLabelValue,
				},
			},
			Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, csb, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	}

	assertBindingClusterSet := func(ctx context.Context, clusterSetName string, matchLabel map[string]string) {
		By("Create clusterset/clustersetbinding")
		assertCreatingClusterSet(ctx, clusterSetName, matchLabel)
		assertCreatingClusterSetBinding(ctx, clusterSetName)
	}

	assertCreatingClusters := func(ctx context.Context, clusterSetName string, num int) {
		By(fmt.Sprintf("Create %d clusters", num))
		for i := 0; i < num; i++ {
			labels := map[string]string{
				e2eTestLabel: e2eTestLabelValue,
			}
			if len(clusterSetName) > 0 {
				labels[clusterapiv1beta2.ClusterSetLabel] = clusterSetName
			}
			cluster := &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "cluster-",
					Labels:       labels,
				},
			}
			_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
	}

	assertCreatingPlacement := func(ctx context.Context, name string, noc *int32, nod int) {
		By("Create placement")
		placement := &clusterapiv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
				Labels: map[string]string{
					e2eTestLabel: e2eTestLabelValue,
				},
			},
			Spec: clusterapiv1beta1.PlacementSpec{
				NumberOfClusters: noc,
				Tolerations: []clusterapiv1beta1.Toleration{
					{
						Key: clusterapiv1.ManagedClusterTaintUnreachable,
					},
				},
			},
		}

		placement, err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Create(ctx, placement, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		assertPlacementDecisionCreated(ctx, placement)
		assertNumberOfDecisions(ctx, placementName, nod)
		if noc != nil {
			assertPlacementStatus(ctx, placementName, nod, nod == int(*noc))
		}
	}

	It("Should schedule successfully", func(ctx SpecContext) {
		assertBindingClusterSet(ctx, clusterSet1Name, nil)
		assertCreatingClusters(ctx, clusterSet1Name, 5)
		assertCreatingPlacement(ctx, placementName, noc(10), 5)

		By("Reduce NOC of the placement")
		Eventually(func() error {
			placement, err := hub.ClusterClient.ClusterV1beta1().Placements(namespace).Get(ctx, placementName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			noc := int32(6)
			placement.Spec.NumberOfClusters = &noc
			_, err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Update(ctx, placement, metav1.UpdateOptions{})
			return err
		}).ShouldNot(HaveOccurred())

		assertNumberOfDecisions(ctx, placementName, 5)
		assertPlacementStatus(ctx, placementName, 5, false)

		// create global clusterset if necessary
		assertCreatingClusterSetBinding(ctx, clusterSetGlobal)

		// create 2 more clusters belong to global clusterset
		assertCreatingClusters(ctx, "", 2)
		assertNumberOfDecisions(ctx, placementName, 6)
		assertPlacementStatus(ctx, placementName, 6, true)

		By("Delete placement")
		err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Delete(ctx, placementName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Check if placementdecisions are deleted as well")
		Eventually(func() bool {
			placementDecisions, err := hub.ClusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", clusterapiv1beta1.PlacementLabel, placementName),
			})
			if err != nil {
				return false
			}

			return len(placementDecisions.Items) == 0
		}).Should(BeTrue())
	})

	It("Should delete placementdecision successfully", func(ctx SpecContext) {
		assertBindingClusterSet(ctx, clusterSet1Name, nil)
		assertCreatingClusters(ctx, clusterSet1Name, 1)
		assertCreatingPlacement(ctx, placementName, nil, 1)

		By("Add cluster predicate")
		Eventually(func() error {
			placement, err := hub.ClusterClient.ClusterV1beta1().Placements(namespace).Get(ctx, placementName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			placement.Spec.Predicates = []clusterapiv1beta1.ClusterPredicate{
				{
					RequiredClusterSelector: clusterapiv1beta1.ClusterSelector{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"a": "b",
							},
						},
					},
				},
			}
			_, err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Update(ctx, placement, metav1.UpdateOptions{})
			return err
		}).ShouldNot(HaveOccurred())

		By("Create empty placement decision")
		assertNumberOfDecisions(ctx, placementName, 0)
		assertPlacementStatus(ctx, placementName, 0, false)

		By("Delete placement")
		err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Delete(ctx, placementName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	// update this case when version upgrade
	It("Should support v1beta1 placement", func(ctx SpecContext) {
		By("Create v1beta1 placement with v1beta1 client")
		placement := &clusterapiv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "test",
			},
			Spec: clusterapiv1beta1.PlacementSpec{
				Tolerations: []clusterapiv1beta1.Toleration{
					{
						Key: clusterapiv1.ManagedClusterTaintUnreachable,
					},
				},
			},
		}

		_, err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Create(ctx, placement, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})
})

func noc(n int) *int32 {
	noc := int32(n) //nolint:gosec
	return &noc
}
