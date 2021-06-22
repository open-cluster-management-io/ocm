package integration

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	controllers "open-cluster-management.io/placement/pkg/controllers"
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
	var clusterSet1Name, clusterSet2Name string
	var suffix string
	var err error

	ginkgo.BeforeEach(func() {
		suffix = rand.String(5)
		namespace = fmt.Sprintf("ns-%s", suffix)
		placementName = fmt.Sprintf("placement-%s", suffix)
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

	assertPlacementDecisionCreated := func(placement *clusterapiv1alpha1.Placement) {
		ginkgo.By("Check if placementdecision is created")
		gomega.Eventually(func() bool {
			pdl, err := clusterClient.ClusterV1alpha1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
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

	assertPlacementDeleted := func(placementName string) {
		ginkgo.By("Check if placement is gone")
		gomega.Eventually(func() bool {
			_, err := clusterClient.ClusterV1alpha1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			if err == nil {
				return false
			}
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertNumberOfDecisions := func(placementName string, desiredNOD int) {
		ginkgo.By("Check the number of decisions in placementdecisions")
		desiredNOPD := desiredNOD / maxNumOfClusterDecisions
		if desiredNOD%maxNumOfClusterDecisions != 0 {
			desiredNOPD++
		}
		gomega.Eventually(func() bool {
			pdl, err := clusterClient.ClusterV1alpha1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
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
			placement, err := clusterClient.ClusterV1alpha1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			if satisfied && !util.HasCondition(
				placement.Status.Conditions,
				clusterapiv1alpha1.PlacementConditionSatisfied,
				"AllDecisionsScheduled",
				metav1.ConditionTrue,
			) {
				return false
			}
			if !satisfied && !util.HasCondition(
				placement.Status.Conditions,
				clusterapiv1alpha1.PlacementConditionSatisfied,
				"NotAllDecisionsScheduled",
				metav1.ConditionFalse,
			) {
				return false
			}
			return placement.Status.NumberOfSelectedClusters == int32(numOfSelectedClusters)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertBindingClusterSet := func(clusterSetName string) {
		ginkgo.By("Create clusterset/clustersetbinding")
		clusterset := &clusterapiv1alpha1.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1alpha1().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		csb := &clusterapiv1alpha1.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      clusterSetName,
			},
			Spec: clusterapiv1alpha1.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1alpha1().ManagedClusterSetBindings(namespace).Create(context.Background(), csb, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	assertCreatingClusters := func(clusterSetName string, num int, labels ...string) {
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
			for i := 1; i < len(labels); i += 2 {
				cluster.Labels[labels[i-1]] = labels[i]
			}
			_, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
	}

	assertCreatingPlacement := func(name string, noc *int32, nod int) {
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

		assertPlacementDecisionCreated(placement)
		assertNumberOfDecisions(placementName, nod)
		if noc != nil {
			assertPlacementStatus(placementName, nod, nod == int(*noc))
		}
	}

	ginkgo.Context("Scheduling", func() {
		ginkgo.AfterEach(func() {
			ginkgo.By("Delete placement")
			err = clusterClient.ClusterV1alpha1().Placements(namespace).Delete(context.Background(), placementName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			assertPlacementDeleted(placementName)
		})

		ginkgo.It("Should re-create placementdecisions successfully once placementdecisions are deleted", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacement(placementName, noc(10), 5)

			ginkgo.By("Delete placementdecisions")
			placementDecisions, err := clusterClient.ClusterV1alpha1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: placementLabel + "=" + placementName,
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			for _, placementDecision := range placementDecisions.Items {
				err = clusterClient.ClusterV1alpha1().PlacementDecisions(namespace).Delete(context.Background(), placementDecision.Name, metav1.DeleteOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			// check if the placementdecisions are re-created
			placement, err := clusterClient.ClusterV1alpha1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			assertPlacementDecisionCreated(placement)
			assertNumberOfDecisions(placementName, 5)
		})

		ginkgo.It("Should schedule placement successfully once spec.ClusterSets changes", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertBindingClusterSet(clusterSet2Name)
			assertCreatingClusters(clusterSet1Name, 2)
			assertCreatingClusters(clusterSet2Name, 3)
			assertCreatingPlacement(placementName, noc(10), 5)

			// update ClusterSets
			placement, err := clusterClient.ClusterV1alpha1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			placement.Spec.ClusterSets = []string{clusterSet1Name}
			placement, err = clusterClient.ClusterV1alpha1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			assertNumberOfDecisions(placementName, 2)
		})

		ginkgo.It("Should schedule placement successfully once spec.Predicates changes", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 2)
			assertCreatingClusters(clusterSet1Name, 3, "cloud", "Amazon")
			assertCreatingPlacement(placementName, noc(10), 5)

			// add a predicates
			placement, err := clusterClient.ClusterV1alpha1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			placement.Spec.Predicates = []clusterapiv1alpha1.ClusterPredicate{
				{
					RequiredClusterSelector: clusterapiv1alpha1.ClusterSelector{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"cloud": "Amazon",
							},
						},
					},
				},
			}
			placement, err = clusterClient.ClusterV1alpha1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			assertNumberOfDecisions(placementName, 3)
		})

		ginkgo.It("Should schedule successfully once spec.NumberOfClusters is reduced", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacement(placementName, noc(10), 5)

			ginkgo.By("Reduce NOC of the placement")
			placement, err := clusterClient.ClusterV1alpha1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			noc := int32(4)
			placement.Spec.NumberOfClusters = &noc
			placement, err = clusterClient.ClusterV1alpha1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			nod := int(noc)
			assertNumberOfDecisions(placementName, nod)
			assertPlacementStatus(placementName, nod, true)
		})

		ginkgo.It("Should schedule successfully once spec.NumberOfClusters is increased", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 10)
			assertCreatingPlacement(placementName, noc(5), 5)

			ginkgo.By("Increase NOC of the placement")
			placement, err := clusterClient.ClusterV1alpha1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			noc := int32(8)
			placement.Spec.NumberOfClusters = &noc
			placement, err = clusterClient.ClusterV1alpha1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			nod := int(noc)
			assertNumberOfDecisions(placementName, nod)
			assertPlacementStatus(placementName, nod, true)
		})

		ginkgo.It("Should be satisfied once new clusters are added", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacement(placementName, noc(10), 5)

			// add more clusters
			assertCreatingClusters(clusterSet1Name, 5)

			nod := 10
			assertNumberOfDecisions(placementName, nod)
			assertPlacementStatus(placementName, nod, true)
		})

		ginkgo.It("Should schedule successfully once new clusterset is bound", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacement(placementName, noc(10), 5)

			ginkgo.By("Bind one more clusterset to the placement namespace")
			assertBindingClusterSet(clusterSet2Name)
			assertCreatingClusters(clusterSet2Name, 3)

			nod := 8
			assertNumberOfDecisions(placementName, nod)
			assertPlacementStatus(placementName, nod, false)
		})

		ginkgo.It("Should create multiple placementdecisions once scheduled", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 101)
			assertCreatingPlacement(placementName, nil, 101)

			nod := 101
			assertNumberOfDecisions(placementName, nod)
			assertPlacementStatus(placementName, nod, true)
		})
	})
})

func noc(n int) *int32 {
	noc := int32(n)
	return &noc
}
