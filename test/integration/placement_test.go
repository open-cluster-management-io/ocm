package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	controllers "open-cluster-management.io/placement/pkg/controllers"
	scheduling "open-cluster-management.io/placement/pkg/controllers/scheduling"
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

	assertCreatingPlacementDecision := func(name string, clusterNames []string) {
		ginkgo.By(fmt.Sprintf("Create placementdecision %s", name))
		placementDecision := &clusterapiv1beta1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
				Labels: map[string]string{
					placementLabel: name,
				},
			},
		}
		placementDecision, err = clusterClient.ClusterV1beta1().PlacementDecisions(namespace).Create(context.Background(), placementDecision, metav1.CreateOptions{})

		clusterDecisions := []clusterapiv1beta1.ClusterDecision{}
		for _, clusterName := range clusterNames {
			clusterDecisions = append(clusterDecisions, clusterapiv1beta1.ClusterDecision{
				ClusterName: clusterName,
			})
		}

		placementDecision.Status.Decisions = clusterDecisions
		placementDecision, err = clusterClient.ClusterV1beta1().PlacementDecisions(namespace).UpdateStatus(context.Background(), placementDecision, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	assertPlacementDeleted := func(placementName string) {
		ginkgo.By("Check if placement is gone")
		gomega.Eventually(func() bool {
			_, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
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

	assertClusterNamesOfDecisions := func(placementName string, desiredClusters []string) {
		ginkgo.By(fmt.Sprintf("Check the cluster names of placementdecisions %s", placementName))
		gomega.Eventually(func() bool {
			pdl, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: placementLabel + "=" + placementName,
			})
			if err != nil {
				return false
			}
			actualClusters := []string{}
			for _, pd := range pdl.Items {
				for _, d := range pd.Status.Decisions {
					actualClusters = append(actualClusters, d.ClusterName)
				}
			}
			ginkgo.By(fmt.Sprintf("Expect %v, but got %v", desiredClusters, actualClusters))
			return apiequality.Semantic.DeepEqual(desiredClusters, actualClusters)
		}, eventuallyTimeout*2, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertPlacementStatus := func(placementName string, numOfSelectedClusters int, satisfied bool) {
		ginkgo.By("Check the status of placement")
		gomega.Eventually(func() bool {
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			if satisfied && !util.HasCondition(
				placement.Status.Conditions,
				clusterapiv1beta1.PlacementConditionSatisfied,
				"AllDecisionsScheduled",
				metav1.ConditionTrue,
			) {
				return false
			}
			if !satisfied && !util.HasCondition(
				placement.Status.Conditions,
				clusterapiv1beta1.PlacementConditionSatisfied,
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
	}

	assertCreatingClusterSet := func(clusterSetName string) {
		ginkgo.By(fmt.Sprintf("Create clusterset %s", clusterSetName))
		clusterset := &clusterapiv1beta1.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	assertCreatingLabelSelectorClusterSet := func(clusterSetName string, matchLabel map[string]string) {
		ginkgo.By(fmt.Sprintf("Create clusterset %s", clusterSetName))
		clusterset := &clusterapiv1beta1.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
			Spec: clusterapiv1beta1.ManagedClusterSetSpec{
				ClusterSelector: clusterapiv1beta1.ManagedClusterSelector{
					SelectorType: clusterapiv1beta1.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: matchLabel,
					},
				},
			},
		}
		_, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	assertDeletingClusterSet := func(clusterSetName string) {
		ginkgo.By(fmt.Sprintf("Delete clusterset %s", clusterSetName))
		err = clusterClient.ClusterV1beta1().ManagedClusterSets().Delete(context.Background(), clusterSetName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Check if clusterset is gone")
		gomega.Eventually(func() bool {
			_, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.Background(), clusterSetName, metav1.GetOptions{})
			if err == nil {
				return false
			}
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertCreatingClusterSetBinding := func(clusterSetName string) {
		ginkgo.By(fmt.Sprintf("Create clustersetbinding %s", clusterSetName))
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
	}

	assertDeletingClusterSetBinding := func(clusterSetName string) {
		ginkgo.By(fmt.Sprintf("Delete clustersetbinding %s", clusterSetName))
		err = clusterClient.ClusterV1beta1().ManagedClusterSetBindings(namespace).Delete(context.Background(), clusterSetName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Check if clustersetbinding is gone")
		gomega.Eventually(func() bool {
			_, err := clusterClient.ClusterV1beta1().ManagedClusterSetBindings(namespace).Get(context.Background(), clusterSetName, metav1.GetOptions{})
			if err == nil {
				return false
			}
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertCreatingClusterWithLabel := func(clusterName string, labels map[string]string) {
		ginkgo.By(fmt.Sprintf("Create cluster %v", clusterName))
		cluster := &clusterapiv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   clusterName,
				Labels: labels,
			},
		}

		_, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
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

	assertCreatingClustersWithNames := func(clusterSetName string, managedClusterNames []string) {
		ginkgo.By(fmt.Sprintf("Create %d clusters", len(managedClusterNames)))
		for _, name := range managedClusterNames {
			cluster := &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "cluster-",
					Labels: map[string]string{
						clusterSetLabel: clusterSetName,
					},
					Name: name,
				},
			}
			ginkgo.By(fmt.Sprintf("Create cluster %s", name))
			_, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
	}

	assertUpdatingClusterWithClusterResources := func(managedClusterName string, res []string) {
		ginkgo.By(fmt.Sprintf("Updating ManagedClusters %s cluster resources", managedClusterName))

		mc, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedClusterName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		allocatable := map[clusterapiv1.ResourceName]resource.Quantity{}
		capacity := map[clusterapiv1.ResourceName]resource.Quantity{}

		allocatable[clusterapiv1.ResourceCPU], err = resource.ParseQuantity(res[0])
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		allocatable[clusterapiv1.ResourceMemory], err = resource.ParseQuantity(res[2])
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		capacity[clusterapiv1.ResourceCPU], err = resource.ParseQuantity(res[1])
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		capacity[clusterapiv1.ResourceMemory], err = resource.ParseQuantity(res[3])
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		mc.Status = clusterapiv1.ManagedClusterStatus{
			Allocatable: allocatable,
			Capacity:    capacity,
			Conditions:  []metav1.Condition{},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(context.Background(), mc, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	assertUpdatingClusterWithClusterTaint := func(managedClusterName string, taint *clusterapiv1.Taint) {
		ginkgo.By(fmt.Sprintf("Updating ManagedClusters %s taint", managedClusterName))
		if taint == nil {
			return
		}

		mc, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedClusterName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		mc.Spec.Taints = append(mc.Spec.Taints, *taint)

		_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.Background(), mc, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	assertDeletingCluster := func(clusterName string) {
		ginkgo.By(fmt.Sprintf("Delete cluster %s", clusterName))
		err = clusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), clusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Check if cluster is gone")
		gomega.Eventually(func() bool {
			_, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), clusterName, metav1.GetOptions{})
			if err == nil {
				return false
			}
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertCreatingPlacement := func(name string, noc *int32, nod int, prioritizerPolicy clusterapiv1beta1.PrioritizerPolicy, tolerations []clusterapiv1beta1.Toleration) {
		ginkgo.By("Create placement")
		placement := &clusterapiv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Spec: clusterapiv1beta1.PlacementSpec{
				NumberOfClusters:  noc,
				PrioritizerPolicy: prioritizerPolicy,
				Tolerations:       tolerations,
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

	assertCreatingAddOnPlacementScores := func(clusternamespace, crname, scorename string, score int32) {
		ginkgo.By(fmt.Sprintf("Create namespace %s for addonplacementscores %s", clusternamespace, crname))
		if _, err = kubeClient.CoreV1().Namespaces().Get(context.Background(), clusternamespace, metav1.GetOptions{}); err != nil {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusternamespace,
				},
			}
			_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		ginkgo.By(fmt.Sprintf("Create addonplacementscores %s in %s", crname, clusternamespace))
		var addOn *clusterapiv1alpha1.AddOnPlacementScore
		if addOn, err = clusterClient.ClusterV1alpha1().AddOnPlacementScores(clusternamespace).Get(context.Background(), crname, metav1.GetOptions{}); err != nil {
			newAddOn := &clusterapiv1alpha1.AddOnPlacementScore{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: clusternamespace,
					Name:      crname,
				},
			}
			addOn, err = clusterClient.ClusterV1alpha1().AddOnPlacementScores(clusternamespace).Create(context.Background(), newAddOn, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		vu := metav1.NewTime(time.Now().Add(10 * time.Second))
		addOn.Status = clusterapiv1alpha1.AddOnPlacementScoreStatus{
			Scores: []clusterapiv1alpha1.AddOnPlacementScoreItem{
				{
					Name:  scorename,
					Value: score,
				},
			},
			ValidUntil: &vu,
		}

		_, err = clusterClient.ClusterV1alpha1().AddOnPlacementScores(clusternamespace).UpdateStatus(context.Background(), addOn, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	ginkgo.Context("Scheduling", func() {
		ginkgo.AfterEach(func() {
			ginkgo.By("Delete placement")
			err = clusterClient.ClusterV1beta1().Placements(namespace).Delete(context.Background(), placementName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			assertPlacementDeleted(placementName)
		})

		ginkgo.It("Should re-create placementdecisions successfully once placementdecisions are deleted", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacement(placementName, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

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
			assertNumberOfDecisions(placementName, 5)
		})

		ginkgo.It("Should schedule placement successfully once spec.ClusterSets changes", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertBindingClusterSet(clusterSet2Name)
			assertCreatingClusters(clusterSet1Name, 2)
			assertCreatingClusters(clusterSet2Name, 3)
			assertCreatingPlacement(placementName, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			// update ClusterSets
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			placement.Spec.ClusterSets = []string{clusterSet1Name}
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			assertNumberOfDecisions(placementName, 2)
		})

		ginkgo.It("Should schedule placement successfully once spec.Predicates changes", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 2)
			assertCreatingClusters(clusterSet1Name, 3, "cloud", "Amazon")
			assertCreatingPlacement(placementName, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

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

			assertNumberOfDecisions(placementName, 3)
		})

		ginkgo.It("Should schedule successfully once spec.NumberOfClusters is reduced", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacement(placementName, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			ginkgo.By("Reduce NOC of the placement")
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			noc := int32(4)
			placement.Spec.NumberOfClusters = &noc
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			nod := int(noc)
			assertNumberOfDecisions(placementName, nod)
			assertPlacementStatus(placementName, nod, true)
		})

		ginkgo.It("Should schedule successfully once spec.NumberOfClusters is increased", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 10)
			assertCreatingPlacement(placementName, noc(5), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			ginkgo.By("Increase NOC of the placement")
			placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			noc := int32(8)
			placement.Spec.NumberOfClusters = &noc
			placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			nod := int(noc)
			assertNumberOfDecisions(placementName, nod)
			assertPlacementStatus(placementName, nod, true)
		})

		ginkgo.It("Should be satisfied once new clusters are added", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacement(placementName, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			// add more clusters
			assertCreatingClusters(clusterSet1Name, 5)

			nod := 10
			assertNumberOfDecisions(placementName, nod)
			assertPlacementStatus(placementName, nod, true)
		})

		ginkgo.It("Should schedule successfully once new clusterset is bound", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacement(placementName, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

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
			assertCreatingPlacement(placementName, nil, 101, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			nod := 101
			assertNumberOfDecisions(placementName, nod)
			assertPlacementStatus(placementName, nod, true)
		})

		ginkgo.It("Should schedule successfully with taint/toleration and requeue when expire TolerationSeconds", func() {
			addedTime := metav1.Now()
			addedTime_60 := addedTime.Add(-60 * time.Second)
			tolerationSeconds_10 := int64(10)
			tolerationSeconds_20 := int64(20)

			// cluster settings
			clusterNames := []string{
				clusterName + "-1",
				clusterName + "-2",
				clusterName + "-3",
				clusterName + "-4",
			}

			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClustersWithNames(clusterSet1Name, clusterNames)

			assertUpdatingClusterWithClusterTaint(clusterNames[0], &clusterapiv1.Taint{
				Key:       "key1",
				Value:     "value1",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: addedTime,
			})
			assertUpdatingClusterWithClusterTaint(clusterNames[1], &clusterapiv1.Taint{
				Key:       "key2",
				Value:     "value2",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: addedTime,
			})
			assertUpdatingClusterWithClusterTaint(clusterNames[2], &clusterapiv1.Taint{
				Key:       "key2",
				Value:     "value2",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: metav1.NewTime(addedTime_60),
			})

			//Checking the result of the placement
			assertCreatingPlacement(placementName, noc(4), 3, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{
				{
					Key:               "key1",
					Operator:          clusterapiv1beta1.TolerationOpExists,
					TolerationSeconds: &tolerationSeconds_10,
				},
				{
					Key:               "key2",
					Operator:          clusterapiv1beta1.TolerationOpExists,
					TolerationSeconds: &tolerationSeconds_20,
				},
			})
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[0], clusterNames[1], clusterNames[3]})

			//Check placement requeue, clusterNames[0] should be removed when TolerationSeconds expired.
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[1], clusterNames[3]})
			//Check placement requeue, clusterNames[1] should be removed when TolerationSeconds expired.
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[3]})
			//Check placement update, clusterNames[3] should be removed when new taint added.
			assertUpdatingClusterWithClusterTaint(clusterNames[3], &clusterapiv1.Taint{
				Key:       "key3",
				Value:     "value3",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: metav1.NewTime(addedTime_60),
			})
			assertClusterNamesOfDecisions(placementName, []string{})
		})

		ginkgo.It("Should schedule successfully with default SchedulePolicy", func() {
			// cluster settings
			clusterNames := []string{
				clusterName + "-1",
				clusterName + "-2",
				clusterName + "-3",
			}
			clusterResources := make([][]string, len(clusterNames))
			clusterResources[0] = []string{"10", "10", "50", "100"}
			clusterResources[1] = []string{"7", "10", "90", "100"}
			clusterResources[2] = []string{"9", "10", "80", "100"}

			// placement settings
			prioritizerPolicy := clusterapiv1beta1.PrioritizerPolicy{}

			//Creating the clusters with resources
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClustersWithNames(clusterSet1Name, clusterNames)
			for i, name := range clusterNames {
				assertUpdatingClusterWithClusterResources(name, clusterResources[i])
			}

			//Checking the result of the placement
			assertCreatingPlacement(placementName, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[0], clusterNames[1]})
		})

		ginkgo.It("Should schedule successfully based on SchedulePolicy ResourceAllocatableCPU & ResourceAllocatableMemory", func() {
			// cluster settings
			clusterNames := []string{
				clusterName + "-1",
				clusterName + "-2",
				clusterName + "-3",
			}
			clusterResources := make([][]string, len(clusterNames))
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
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClustersWithNames(clusterSet1Name, clusterNames)
			for i, name := range clusterNames {
				assertUpdatingClusterWithClusterResources(name, clusterResources[i])
			}

			//Checking the result of the placement
			assertCreatingPlacement(placementName, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[0], clusterNames[2]})

		})

		ginkgo.It("Should schedule successfully based on AddOnPlacementScore", func() {
			// cluster settings
			clusterNames := []string{
				clusterName + "-1",
				clusterName + "-2",
				clusterName + "-3",
			}

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
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClustersWithNames(clusterSet1Name, clusterNames)
			assertCreatingAddOnPlacementScores(clusterNames[0], "demo", "demo", 80)
			assertCreatingAddOnPlacementScores(clusterNames[1], "demo", "demo", 90)
			assertCreatingAddOnPlacementScores(clusterNames[2], "demo", "demo", 100)

			//Checking the result of the placement
			assertCreatingPlacement(placementName, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[1], clusterNames[2]})
		})

		ginkgo.It("Should reschedule every ResyncInterval and update desicion when AddOnPlacementScore changes", func() {
			// cluster settings
			clusterNames := []string{
				clusterName + "-1",
				clusterName + "-2",
				clusterName + "-3",
			}

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
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClustersWithNames(clusterSet1Name, clusterNames)

			//Creating the placement
			assertCreatingPlacement(placementName, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})

			//Checking the result of the placement when no AddOnPlacementScores
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[0], clusterNames[1]})

			//Creating the AddOnPlacementScores
			assertCreatingAddOnPlacementScores(clusterNames[0], "demo", "demo", 80)
			assertCreatingAddOnPlacementScores(clusterNames[1], "demo", "demo", 90)
			assertCreatingAddOnPlacementScores(clusterNames[2], "demo", "demo", 100)

			//Checking the result of the placement when AddOnPlacementScores added
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[1], clusterNames[2]})

			//update the AddOnPlacementScores
			assertCreatingAddOnPlacementScores(clusterNames[0], "demo", "demo", 100)
			assertCreatingAddOnPlacementScores(clusterNames[1], "demo", "demo", 90)
			assertCreatingAddOnPlacementScores(clusterNames[2], "demo", "demo", 100)

			//Checking the result of the placement when AddOnPlacementScores updated
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[0], clusterNames[2]})

		})

		ginkgo.It("Should keep steady successfully even placementdecisions' balance and cluster situation changes", func() {
			// cluster settings
			clusterNames := []string{
				clusterName + "-1",
				clusterName + "-2",
				clusterName + "-3",
			}
			clusterResources := make([][]string, len(clusterNames))
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
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClustersWithNames(clusterSet1Name, clusterNames)
			for i, name := range clusterNames {
				assertUpdatingClusterWithClusterResources(name, clusterResources[i])
			}

			//Checking the result of the placement
			assertCreatingPlacement(placementName, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[0], clusterNames[2]})

			ginkgo.By("Adding fake placement decisions")
			assertCreatingPlacementDecision(placementName+"-1", []string{clusterNames[1]})
			ginkgo.By("Adding a new cluster with resources")
			clusterNames = append(clusterNames, clusterName+"-4")
			newClusterResources := []string{"10", "10", "100", "100"}
			assertCreatingClustersWithNames(clusterSet1Name, clusterNames[3:4])
			assertUpdatingClusterWithClusterResources(clusterNames[3], newClusterResources)

			//Checking the result of the placement
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[0], clusterNames[2]})
		})

		ginkgo.It("Should re-schedule successfully once a new cluster added/deleted", func() {
			// cluster settings
			clusterNames := []string{
				clusterName + "-1",
				clusterName + "-2",
				clusterName + "-3",
			}
			clusterResources := make([][]string, len(clusterNames))
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
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClustersWithNames(clusterSet1Name, clusterNames)
			for i, name := range clusterNames {
				assertUpdatingClusterWithClusterResources(name, clusterResources[i])
			}

			//Checking the result of the placement
			assertCreatingPlacement(placementName, noc(2), 2, prioritizerPolicy, []clusterapiv1beta1.Toleration{})
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[0], clusterNames[2]})

			ginkgo.By("Adding a new cluster with resources")
			clusterNames = append(clusterNames, clusterName+"-4")
			newClusterResources := []string{"10", "10", "100", "100"}
			assertCreatingClustersWithNames(clusterSet1Name, clusterNames[3:4])
			assertUpdatingClusterWithClusterResources(clusterNames[3], newClusterResources)

			//Checking the result of the placement
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[2], clusterNames[3]})

			ginkgo.By("Deleting the cluster")
			assertDeletingCluster(clusterName + "-4")

			//Checking the result of the placement
			assertClusterNamesOfDecisions(placementName, []string{clusterNames[0], clusterNames[2]})
		})

		ginkgo.It("Should re-schedule successfully once a clusterset deleted/added", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacement(placementName, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			assertNumberOfDecisions(placementName, 5)
			assertPlacementStatus(placementName, 5, false)

			ginkgo.By("Delete the clusterset")
			assertDeletingClusterSet(clusterSet1Name)

			assertNumberOfDecisions(placementName, 0)

			ginkgo.By("Add the clusterset back")
			assertCreatingClusterSet(clusterSet1Name)

			assertNumberOfDecisions(placementName, 5)
			assertPlacementStatus(placementName, 5, false)
		})

		ginkgo.It("Should re-schedule successfully once a labelselector type clusterset deleted/added", func() {
			assertCreatingLabelSelectorClusterSet(clusterSet1Name, map[string]string{
				"vendor": "openShift",
			})
			assertCreatingClusterSetBinding(clusterSet1Name)
			assertCreatingClusterWithLabel("cluster1", map[string]string{
				"vendor": "openShift",
			})
			assertCreatingClusterWithLabel("cluster2", map[string]string{
				"vendor": "IKS",
			})
			assertCreatingPlacement(placementName, noc(10), 1, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			assertNumberOfDecisions(placementName, 1)
			assertPlacementStatus(placementName, 1, false)

			ginkgo.By("Delete the clusterset")
			assertDeletingClusterSet(clusterSet1Name)

			assertNumberOfDecisions(placementName, 0)

			ginkgo.By("Add the clusterset back")
			assertCreatingLabelSelectorClusterSet(clusterSet1Name, map[string]string{
				"vendor": "openShift",
			})
			assertNumberOfDecisions(placementName, 1)
			assertPlacementStatus(placementName, 1, false)
		})
		ginkgo.It("Should re-schedule successfully once a clustersetbinding deleted/added", func() {
			assertBindingClusterSet(clusterSet1Name)
			assertCreatingClusters(clusterSet1Name, 5)
			assertCreatingPlacement(placementName, noc(10), 5, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{})

			assertNumberOfDecisions(placementName, 5)
			assertPlacementStatus(placementName, 5, false)

			ginkgo.By("Delete the clustersetbinding")
			assertDeletingClusterSetBinding(clusterSet1Name)

			assertNumberOfDecisions(placementName, 0)

			ginkgo.By("Add the clustersetbinding back")
			assertCreatingClusterSetBinding(clusterSet1Name)

			assertNumberOfDecisions(placementName, 5)
			assertPlacementStatus(placementName, 5, false)
		})
	})
})

func noc(n int) *int32 {
	noc := int32(n)
	return &noc
}
