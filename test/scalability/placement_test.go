package scalability

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	clusterSetLabel   = "cluster.open-cluster-management.io/clusterset"
	placementLabel    = "cluster.open-cluster-management.io/placement"
	placementSetLabel = "cluster.open-cluster-management.io/placementset"
)

var _ = ginkgo.Describe("Placement scalability test", func() {
	var namespace string
	var placementSetName string
	var clusterSetName string
	var suffix string
	var sampleCount = 10
	var err error

	assertNumberOfDecisions := func(placement *clusterapiv1beta1.Placement, desiredNOD int) error {
		var localerr error
		gomega.Eventually(func() bool {
			localerr = nil
			pdl, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
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

			actualNOD := 0
			for _, pd := range pdl.Items {
				if controlled := metav1.IsControlledBy(&pd.ObjectMeta, placement); !controlled {
					localerr = errors.New("No controllerRef found for a placement")
					return false
				}
				actualNOD += len(pd.Status.Decisions)
			}
			if actualNOD != desiredNOD {
				localerr = errors.New(fmt.Sprintf("Mismatch value %v:%v", actualNOD, desiredNOD))
			}
			return actualNOD == desiredNOD
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		return localerr
	}

	assertPlacementStatus := func(placement *clusterapiv1beta1.Placement, numOfSelectedClusters int, satisfied bool) error {
		var localerr error
		gomega.Eventually(func() bool {
			localerr = nil
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placement.Name, metav1.GetOptions{})
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
				clusterapiv1beta1.PlacementConditionSatisfied,
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

	assertBindingClusterSet := func() error {
		ginkgo.By("Create clusterset/clustersetbinding")
		clusterset := &clusterapiv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

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

		return err
	}

	assertCreatingClusters := func(num int) error {
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

	assertCreatingPlacement := func(noc *int32, nod int) error {
		placement := &clusterapiv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    namespace,
				GenerateName: "placement-",
				Labels: map[string]string{
					placementSetLabel: placementSetName,
				},
			},
			Spec: clusterapiv1beta1.PlacementSpec{
				NumberOfClusters: noc,
			},
		}
		pl, err := clusterClient.ClusterV1beta1().Placements(namespace).Create(context.Background(), placement, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		err = assertNumberOfDecisions(pl, nod)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		if noc != nil {
			err = assertPlacementStatus(pl, nod, nod == int(*noc))
		}
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		return err
	}

	assertCreatingPlacements := func(num int, noc *int32, nod int) error {
		ginkgo.By(fmt.Sprintf("Create %d placements", num))
		for i := 0; i < num; i++ {
			err := assertCreatingPlacement(noc, nod)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			time.Sleep(time.Duration(1) * time.Second) //sleep 1 second in case API server is too busy
		}
		return err
	}

	assertUpdatingPlacements := func(num int, nod int) error {
		ginkgo.By(fmt.Sprintf("Check %v updated placements", num))

		var localerr error

		pls, err := clusterClient.ClusterV1beta1().Placements(namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: placementSetLabel + "=" + placementSetName,
		})
		if err != nil {
			localerr = err
			return localerr
		}

		sampleCap := num / sampleCount
		targetSample := 0
		currentSample := 0

		for _, pl := range pls.Items {
			currentSample++
			if currentSample > targetSample {
				targetSample += sampleCap
				err := assertNumberOfDecisions(&pl, nod)
				if err != nil {
					localerr = err
					break
				}
			}
		}

		return err
	}

	ginkgo.BeforeEach(func() {
		suffix = rand.String(5)
		namespace = fmt.Sprintf("ns-%s", suffix)
		placementSetName = fmt.Sprintf("placementset-%s", suffix)
		clusterSetName = fmt.Sprintf("clusterset-%s", suffix)

		// create testing namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err = kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		assertBindingClusterSet()
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Delete placement")
		err = clusterClient.ClusterV1beta1().Placements(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: placementSetLabel + "=" + placementSetName,
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Delete managedclusterset")
		clusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), clusterSetName, metav1.DeleteOptions{})

		ginkgo.By("Delete managedclusters")
		clusterClient.ClusterV1().ManagedClusters().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: clusterSetLabel + "=" + clusterSetName,
		})

		err = kubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	/* we create N managedclusters here, and create a placement whose NumberOfClusters is N-1 to ensure the placement logic will
	 * do comparison to select N-1 managedclusters from N candidates
	 * N will be 100, 1000, 2000.
	 */

	ginkgo.Measure(fmt.Sprintf("Should create placement efficiently"), func(b ginkgo.Benchmarker) {
		totalClusters := 100
		ginkgo.By(fmt.Sprintf("Create 1 placement with %v managedclusters", totalClusters))
		err = assertCreatingClusters(totalClusters)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		runtime := b.Time("runtime", func() {
			err = assertCreatingPlacement(noc(totalClusters-1), totalClusters-1)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Ω(runtime.Seconds()).Should(gomega.BeNumerically("<", 5), "Something during creating placement take too long.")
	}, 1)

	ginkgo.Measure(fmt.Sprintf("Should create placement efficiently"), func(b ginkgo.Benchmarker) {
		totalClusters := 1000
		ginkgo.By(fmt.Sprintf("Create 1 placement with %v managedclusters", totalClusters))
		err = assertCreatingClusters(totalClusters)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		runtime := b.Time("runtime", func() {
			err = assertCreatingPlacement(noc(totalClusters-1), totalClusters-1)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Ω(runtime.Seconds()).Should(gomega.BeNumerically("<", 10), "Something during creating placement take too long.")
	}, 1)

	ginkgo.Measure(fmt.Sprintf("Should create placement efficiently"), func(b ginkgo.Benchmarker) {
		totalClusters := 2000
		ginkgo.By(fmt.Sprintf("Create 1 placement with %v managedclusters", totalClusters))
		err = assertCreatingClusters(totalClusters)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		runtime := b.Time("runtime", func() {
			err = assertCreatingPlacement(noc(totalClusters-1), totalClusters-1)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Ω(runtime.Seconds()).Should(gomega.BeNumerically("<", 20), "Something during creating placement take too long.")
	}, 1)

	/* To check the scalability of placement creating/updating, we will
	 * 1. create N-2 managedclusters, and create M placements whose NumberOfClusters is N-1, then select several placements to ensure each one has N-2 decisions, this is for creating.
	 * 2. create 2 managedclusters, then select several placements to ensure each one has N-1 decisions, this is for updating.
	 * M will be 10, 100, 300
	 * N will be 100
	 */

	ginkgo.Measure(fmt.Sprintf("Should create/update placement efficiently"), func(b ginkgo.Benchmarker) {
		totalPlacements := 10
		totalClusters := 100
		ginkgo.By(fmt.Sprintf("Create %v placement with %v managedclusters", totalPlacements, totalClusters))
		err = assertCreatingClusters(totalClusters - 2)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		createtime := b.Time("createtime", func() {
			err = assertCreatingPlacements(totalPlacements, noc(totalClusters-1), totalClusters-2)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Ω(createtime.Seconds()).Should(gomega.BeNumerically("<", 50), "Something during creating placement take too long.")

		err = assertCreatingClusters(2)
		updatetime := b.Time("updatetime", func() {
			err = assertUpdatingPlacements(totalPlacements, totalClusters-1)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Ω(updatetime.Seconds()).Should(gomega.BeNumerically("<", 20), "Something during updating placement take too long.")
	}, 1)

	ginkgo.Measure(fmt.Sprintf("Should create/update placement efficiently"), func(b ginkgo.Benchmarker) {
		totalPlacements := 100
		totalClusters := 100
		ginkgo.By(fmt.Sprintf("Create %v placement with %v managedclusters", totalPlacements, totalClusters))
		err = assertCreatingClusters(totalClusters - 2)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		createtime := b.Time("createtime", func() {
			err = assertCreatingPlacements(totalPlacements, noc(totalClusters-1), totalClusters-2)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Ω(createtime.Seconds()).Should(gomega.BeNumerically("<", 400), "Something during creating placement take too long.")

		err = assertCreatingClusters(2)
		updatetime := b.Time("updatetime", func() {
			err = assertUpdatingPlacements(totalPlacements, totalClusters-1)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Ω(updatetime.Seconds()).Should(gomega.BeNumerically("<", 60), "Something during updating placement take too long.")
	}, 1)

	ginkgo.Measure(fmt.Sprintf("Should create/update placement efficiently"), func(b ginkgo.Benchmarker) {
		totalPlacements := 300
		totalClusters := 100
		ginkgo.By(fmt.Sprintf("Create %v placement with %v managedclusters", totalPlacements, totalClusters))
		err = assertCreatingClusters(totalClusters - 2)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		createtime := b.Time("createtime", func() {
			err = assertCreatingPlacements(totalPlacements, noc(totalClusters-1), totalClusters-2)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Ω(createtime.Seconds()).Should(gomega.BeNumerically("<", 1200), "Something during creating placement take too long.")

		err = assertCreatingClusters(2)
		updatetime := b.Time("updatetime", func() {
			err = assertUpdatingPlacements(totalPlacements, totalClusters-1)
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Ω(updatetime.Seconds()).Should(gomega.BeNumerically("<", 200), "Something during updating placement take too long.")
	}, 1)
})

func noc(n int) *int32 {
	noc := int32(n)
	return &noc
}
