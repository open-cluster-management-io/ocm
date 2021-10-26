package scalability

import (
	"context"
	"errors"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/placement/test/integration/util"
)

const (
	clusterSetLabel = "cluster.open-cluster-management.io/clusterset"
	placementLabel  = "cluster.open-cluster-management.io/placement"
)

var _ = ginkgo.Describe("Placement scalability test", func() {
	var namespace string
	var placementName string
	var clusterSet1Name string
	var suffix string
	var err error

	assertPlacementDecisionCreated := func(placement *clusterapiv1alpha1.Placement) error {
		ginkgo.By("Check if placementdecision is created")
		var localerr error
		gomega.Eventually(func() bool {
			localerr = nil
			pdl, err := clusterClient.ClusterV1alpha1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: placementLabel + "=" + placement.Name,
			})
			if err != nil {
				localerr = err
				return false
			}
			if len(pdl.Items) == 0 {
				localerr = errors.New("No placementdecision found")
				return false
			}
			for _, pd := range pdl.Items {
				if controlled := metav1.IsControlledBy(&pd.ObjectMeta, placement); !controlled {
					localerr = errors.New("No controllerRef found for a placement")
					return false
				}
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		return localerr
	}

	assertNumberOfDecisions := func(placementName string, desiredNOD int) error {
		ginkgo.By("Check the number of decisions in placementdecisions")
		var localerr error
		gomega.Eventually(func() bool {
			localerr = nil
			pdl, err := clusterClient.ClusterV1alpha1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: placementLabel + "=" + placementName,
			})
			if err != nil {
				localerr = err
				return false
			}
			actualNOD := 0
			for _, pd := range pdl.Items {
				actualNOD += len(pd.Status.Decisions)
			}
			if actualNOD != desiredNOD {
				localerr = errors.New(fmt.Sprintf("Mismatch value %v:%v", actualNOD, desiredNOD))
			}
			return actualNOD == desiredNOD
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		return localerr
	}

	assertPlacementStatus := func(placementName string, numOfSelectedClusters int, satisfied bool) error {
		ginkgo.By("Check the status of placement")
		var localerr error
		gomega.Eventually(func() bool {
			localerr = nil
			placement, err := clusterClient.ClusterV1alpha1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			if err != nil {
				localerr = err
				return false
			}
			status := metav1.ConditionFalse
			if satisfied {
				status = metav1.ConditionTrue
			}
			if !util.HasCondition(
				placement.Status.Conditions,
				clusterapiv1alpha1.PlacementConditionSatisfied,
				"",
				status,
			) {
				localerr = errors.New("Contition check failed")
				return false
			}
			if placement.Status.NumberOfSelectedClusters != int32(numOfSelectedClusters) {
				localerr = errors.New(fmt.Sprintf("Mismatch value %v:%v", placement.Status.NumberOfSelectedClusters, int32(numOfSelectedClusters)))
			}
			return placement.Status.NumberOfSelectedClusters == int32(numOfSelectedClusters)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		return localerr
	}

	assertBindingClusterSet := func(clusterSetName string) error {
		ginkgo.By("Create clusterset/clustersetbinding")
		clusterset := &clusterapiv1beta1.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		csb := &clusterapiv1beta1.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      clusterSetName,
			},
			Spec: clusterapiv1beta1.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta1().ManagedClusterSetBindings(namespace).Create(context.Background(), csb, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		return err
	}

	assertCreatingClusters := func(clusterSetName string, num int) error {
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
		return err
	}

	assertCreatingPlacement := func(name string, noc *int32, nod int) error {
		ginkgo.By("Create placement")
		placement := &clusterapiv1alpha1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Spec: clusterapiv1alpha1.PlacementSpec{
				NumberOfClusters: noc,
			},
		}
		placement, err = clusterClient.ClusterV1alpha1().Placements(namespace).Create(context.Background(), placement, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		err = assertPlacementDecisionCreated(placement)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		err = assertNumberOfDecisions(placementName, nod)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		if noc != nil {
			err = assertPlacementStatus(placementName, nod, nod == int(*noc))
		}
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		return err
	}

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
		_, err = kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		assertBindingClusterSet(clusterSet1Name)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Delete placement")
		err = clusterClient.ClusterV1alpha1().Placements(namespace).Delete(context.TODO(), placementName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Delete managedclusterset")
		clusterClient.ClusterV1beta1().ManagedClusterSets().Delete(context.Background(), clusterSet1Name, metav1.DeleteOptions{})

		ginkgo.By("Delete managedclusters")
		clusterClient.ClusterV1().ManagedClusters().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: clusterSetLabel + "=" + clusterSet1Name,
		})

		err = kubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	/* we create N managedclusters here, and create a placement whose NumberOfClusters is N-1 to ensure the placement logic will
	do comparison to select N-1 managedclusters from N candidates */

	totalClusters_1 := 100
	ginkgo.Measure(fmt.Sprintf("Should create placement efficiently with %d managedclusters", totalClusters_1), func(b ginkgo.Benchmarker) {
		err = assertCreatingClusters(clusterSet1Name, totalClusters_1)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		runtime := b.Time("runtime", func() {
			err = assertCreatingPlacement(placementName, noc(totalClusters_1-1), totalClusters_1-1)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Ω(runtime.Seconds()).Should(gomega.BeNumerically("<", 5), "Something during creating placement take too long.")
	}, 1)

	totalClusters_2 := 1000
	ginkgo.Measure(fmt.Sprintf("Should create placement efficiently with %d managedclusters", totalClusters_2), func(b ginkgo.Benchmarker) {
		err = assertCreatingClusters(clusterSet1Name, totalClusters_2)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		runtime := b.Time("runtime", func() {
			err = assertCreatingPlacement(placementName, noc(totalClusters_2-1), totalClusters_2-1)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Ω(runtime.Seconds()).Should(gomega.BeNumerically("<", 10), "Something during creating placement take too long.")
	}, 1)

	totalClusters_3 := 2000
	ginkgo.Measure(fmt.Sprintf("Should create placement efficiently with %d managedclusters", totalClusters_3), func(b ginkgo.Benchmarker) {
		err = assertCreatingClusters(clusterSet1Name, totalClusters_3)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		runtime := b.Time("runtime", func() {
			err = assertCreatingPlacement(placementName, noc(totalClusters_3-1), totalClusters_3-1)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Ω(runtime.Seconds()).Should(gomega.BeNumerically("<", 20), "Something during creating placement take too long.")
	}, 1)
})

func noc(n int) *int32 {
	noc := int32(n)
	return &noc
}
