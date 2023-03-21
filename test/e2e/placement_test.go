package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/placement/test/integration/util"
)

const (
	clusterSetLabel          = "cluster.open-cluster-management.io/clusterset"
	placementLabel           = "cluster.open-cluster-management.io/placement"
	maxNumOfClusterDecisions = 100
)

var _ = ginkgo.Describe("Placement", func() {
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
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Delete managedclusterset")
		clusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), clusterSet1Name, metav1.DeleteOptions{})
		clusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), clusterSetGlobal, metav1.DeleteOptions{})

		ginkgo.By("Delete managedclusters")
		clusterClient.ClusterV1().ManagedClusters().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: clusterSetLabel + "=" + clusterSet1Name,
		})

		err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	assertPlacementDecisionCreated := func(placement *clusterapiv1beta1.Placement) {
		ginkgo.By("Check if placementdecision is created")
		gomega.Eventually(func() bool {
			pdl, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: placementLabel + "=" + placement.Name,
			})
			if err != nil {
				return false
			}
			if len(pdl.Items) == 0 {
				return false
			}
			for _, pd := range pdl.Items {
				if controlled := metav1.IsControlledBy(&pd.ObjectMeta, placement); !controlled {
					return false
				}
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertNumberOfDecisions := func(placementName string, desiredNOD int) {
		ginkgo.By("Check the number of decisions in placementdecisions")
		desiredNOPD := desiredNOD/maxNumOfClusterDecisions + 1
		gomega.Eventually(func() bool {
			pdl, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: placementLabel + "=" + placementName,
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
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertPlacementStatus := func(placementName string, numOfSelectedClusters int, satisfied bool) {
		ginkgo.By("Check the status of placement")
		gomega.Eventually(func() bool {
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
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

			return placement.Status.NumberOfSelectedClusters == int32(numOfSelectedClusters)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertCreatingClusterSet := func(clusterSetName string, matchLabel map[string]string) {
		ginkgo.By("Create clusterset")
		clusterset := &clusterapiv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
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
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	assertCreatingClusterSetBinding := func(clusterSetName string) {
		ginkgo.By("Create clustersetbinding")
		csb := &clusterapiv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      clusterSetName,
			},
			Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), csb, metav1.CreateOptions{})
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
			cluster := &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "cluster-",
					Labels: map[string]string{
						clusterSetLabel: clusterSetName,
					},
				},
			}
			_, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
	}

	assertCreatingPlacement := func(name string, noc *int32, nod int) {
		ginkgo.By("Create placement")
		placement := &clusterapiv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Spec: clusterapiv1beta1.PlacementSpec{
				NumberOfClusters: noc,
			},
		}
		placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Create(context.Background(), placement, metav1.CreateOptions{})
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
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			noc := int32(6)
			placement.Spec.NumberOfClusters = &noc
			_, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		assertNumberOfDecisions(placementName, 5)
		assertPlacementStatus(placementName, 5, false)

		// create global clusterset
		assertBindingClusterSet(clusterSetGlobal, map[string]string{})
		// create 2 more clusters belong to global clusterset
		assertCreatingClusters("", 2)
		assertNumberOfDecisions(placementName, 6)
		assertPlacementStatus(placementName, 6, true)

		ginkgo.By("Delete placement")
		err = clusterClient.ClusterV1beta1().Placements(namespace).Delete(context.TODO(), placementName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Check if placementdecisions are deleted as well")
		gomega.Eventually(func() bool {
			placementDecisions, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", placementLabel, placementName),
			})
			if err != nil {
				return false
			}

			return len(placementDecisions.Items) == 0
		}, eventuallyTimeout*5, eventuallyInterval*5).Should(gomega.BeTrue())
	})

	ginkgo.It("Should delete placementdecision successfully", func() {
		assertBindingClusterSet(clusterSet1Name, nil)
		assertCreatingClusters(clusterSet1Name, 1)
		assertCreatingPlacement(placementName, nil, 1)

		ginkgo.By("Add cluster predicate")
		gomega.Eventually(func() error {
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
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
			_, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Create empty placement decision")
		assertNumberOfDecisions(placementName, 0)
		assertPlacementStatus(placementName, 0, false)

		ginkgo.By("Delete placement")
		err = clusterClient.ClusterV1beta1().Placements(namespace).Delete(context.TODO(), placementName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	//update this case when version upgrade
	ginkgo.It("Should support v1beta1 placement", func() {
		ginkgo.By("Create v1beta1 placement with v1beta1 client")
		placement := &clusterapiv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "test",
			},
		}
		_, err = clusterClient.ClusterV1beta1().Placements(namespace).Create(context.Background(), placement, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
})

func noc(n int) *int32 {
	noc := int32(n)
	return &noc
}
