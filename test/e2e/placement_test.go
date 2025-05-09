package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
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
var _ = ginkgo.Describe("Placement", ginkgo.Label("placement", "sanity-check"), func() {
	var namespace string
	var placementName string
	var clusterSet1Name string
	var clusterSetGlobal string
	var suffix string
	var err error

	ginkgo.BeforeEach(func() {
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
		_, err := hub.KubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		var errs []error
		ginkgo.By("Delete managedclustersets")
		err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: e2eTestLabel + "=" + e2eTestLabelValue,
		})
		if err != nil {
			errs = append(errs, err)
		}

		ginkgo.By("Delete managedclusters")
		err = hub.ClusterClient.ClusterV1().ManagedClusters().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: e2eTestLabel + "=" + e2eTestLabelValue,
		})
		if err != nil {
			errs = append(errs, err)
		}

		ginkgo.By("Delete namespace")
		err = hub.KubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		if err != nil {
			errs = append(errs, err)
		}
		gomega.Expect(utilerrors.NewAggregate(errs)).ToNot(gomega.HaveOccurred())

	})

	assertPlacementDecisionCreated := func(placement *clusterapiv1beta1.Placement) {
		ginkgo.By("Check if placementdecision is created")
		gomega.Eventually(func() bool {
			pdl, err := hub.ClusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
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
		}).Should(gomega.BeTrue())
	}

	assertNumberOfDecisions := func(placementName string, desiredNOD int) {
		ginkgo.By("Check the number of decisions in placementdecisions")
		desiredNOPD := desiredNOD/maxNumOfClusterDecisions + 1
		gomega.Eventually(func() bool {
			pdl, err := hub.ClusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
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
		}).Should(gomega.BeTrue())
	}

	assertPlacementStatus := func(placementName string, numOfSelectedClusters int, satisfied bool) {
		ginkgo.By("Check the status of placement")
		gomega.Eventually(func() bool {
			placement, err := hub.ClusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
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
		}).Should(gomega.BeTrue())
	}

	assertCreatingClusterSet := func(clusterSetName string, matchLabel map[string]string) {
		ginkgo.By("Create clusterset")
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
		_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	assertCreatingClusterSetBinding := func(clusterSetName string) {
		ginkgo.By("Create clustersetbinding")
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
		_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), csb, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	assertBindingClusterSet := func(clusterSetName string, matchLabel map[string]string) {
		ginkgo.By("Create clusterset/clustersetbinding")
		assertCreatingClusterSet(clusterSetName, matchLabel)
		assertCreatingClusterSetBinding(clusterSetName)
	}

	assertCreatingClusters := func(clusterSetName string, num int) {
		ginkgo.By(fmt.Sprintf("Create %d clusters", num))
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
			_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
	}

	assertCreatingPlacement := func(name string, noc *int32, nod int) {
		ginkgo.By("Create placement")
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

		placement, err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Create(context.Background(), placement, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		assertPlacementDecisionCreated(placement)
		assertNumberOfDecisions(placementName, nod)
		if noc != nil {
			assertPlacementStatus(placementName, nod, nod == int(*noc))
		}
	}

	ginkgo.It("Should schedule successfully", func() {
		assertBindingClusterSet(clusterSet1Name, nil)
		assertCreatingClusters(clusterSet1Name, 5)
		assertCreatingPlacement(placementName, noc(10), 5)

		ginkgo.By("Reduce NOC of the placement")
		gomega.Eventually(func() error {
			placement, err := hub.ClusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			noc := int32(6)
			placement.Spec.NumberOfClusters = &noc
			_, err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			return err
		}).ShouldNot(gomega.HaveOccurred())

		assertNumberOfDecisions(placementName, 5)
		assertPlacementStatus(placementName, 5, false)

		// create global clusterset if necessary
		assertCreatingClusterSetBinding(clusterSetGlobal)

		// create 2 more clusters belong to global clusterset
		assertCreatingClusters("", 2)
		assertNumberOfDecisions(placementName, 6)
		assertPlacementStatus(placementName, 6, true)

		ginkgo.By("Delete placement")
		err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Delete(context.TODO(), placementName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Check if placementdecisions are deleted as well")
		gomega.Eventually(func() bool {
			placementDecisions, err := hub.ClusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", clusterapiv1beta1.PlacementLabel, placementName),
			})
			if err != nil {
				return false
			}

			return len(placementDecisions.Items) == 0
		}).Should(gomega.BeTrue())
	})

	ginkgo.It("Should delete placementdecision successfully", func() {
		assertBindingClusterSet(clusterSet1Name, nil)
		assertCreatingClusters(clusterSet1Name, 1)
		assertCreatingPlacement(placementName, nil, 1)

		ginkgo.By("Add cluster predicate")
		gomega.Eventually(func() error {
			placement, err := hub.ClusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
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
			_, err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			return err
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Create empty placement decision")
		assertNumberOfDecisions(placementName, 0)
		assertPlacementStatus(placementName, 0, false)

		ginkgo.By("Delete placement")
		err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Delete(context.TODO(), placementName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	// update this case when version upgrade
	ginkgo.It("Should support v1beta1 placement", func() {
		ginkgo.By("Create v1beta1 placement with v1beta1 client")
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

		_, err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Create(context.Background(), placement, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
})

func noc(n int) *int32 {
	noc := int32(n) //nolint:gosec
	return &noc
}
