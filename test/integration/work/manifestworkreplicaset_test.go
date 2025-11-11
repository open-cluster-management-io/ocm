package work

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	"open-cluster-management.io/ocm/test/integration/util"
)

type decorator func(rs *workapiv1alpha1.ManifestWorkReplicaSet) *workapiv1alpha1.ManifestWorkReplicaSet

var _ = ginkgo.Describe("ManifestWorkReplicaSet", func() {
	var namespaceName string
	var placement *clusterv1beta1.Placement
	var placementDecision *clusterv1beta1.PlacementDecision
	var generateTestFixture func(numberOfClusters int, decs ...decorator) (*workapiv1alpha1.ManifestWorkReplicaSet, sets.Set[string], error)

	ginkgo.BeforeEach(func() {
		namespaceName = utilrand.String(5)
		ns := &corev1.Namespace{}
		ns.Name = namespaceName
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		placement = &clusterv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-placement",
				Namespace: namespaceName,
			},
		}

		placementDecision = &clusterv1beta1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-placement-decision",
				Namespace: namespaceName,
				Labels: map[string]string{
					clusterv1beta1.PlacementLabel:          placement.Name,
					clusterv1beta1.DecisionGroupIndexLabel: "0",
				},
			},
		}

		generateTestFixture = func(numberOfClusters int, decs ...decorator) (*workapiv1alpha1.ManifestWorkReplicaSet, sets.Set[string], error) {
			clusterNames := sets.New[string]()
			manifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap("defaut", cm1, map[string]string{"a": "b"}, nil)),
			}
			placementRef := workapiv1alpha1.LocalPlacementReference{
				Name:            placement.Name,
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
			}

			manifestWorkReplicaSet := &workapiv1alpha1.ManifestWorkReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: namespaceName,
				},
				Spec: workapiv1alpha1.ManifestWorkReplicaSetSpec{
					ManifestWorkTemplate: workapiv1.ManifestWorkSpec{
						Workload: workapiv1.ManifestsTemplate{
							Manifests: manifests,
						},
					},
					PlacementRefs: []workapiv1alpha1.LocalPlacementReference{placementRef},
				},
			}

			for _, dec := range decs {
				manifestWorkReplicaSet = dec(manifestWorkReplicaSet)
			}

			_, err := hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespaceName).Create(context.TODO(), manifestWorkReplicaSet, metav1.CreateOptions{})
			if err != nil {
				return nil, clusterNames, err
			}

			_, err = hubClusterClient.ClusterV1beta1().Placements(placement.Namespace).Create(context.TODO(), placement, metav1.CreateOptions{})
			if err != nil {
				return nil, clusterNames, err
			}

			decision, err := hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace).Create(
				context.TODO(), placementDecision, metav1.CreateOptions{})
			if err != nil {
				return nil, clusterNames, err
			}

			for i := 0; i < numberOfClusters; i++ {
				clusterName := "cluster-" + utilrand.String(5)
				ns := &corev1.Namespace{}
				ns.Name = clusterName
				_, err = spokeKubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
				if err != nil {
					return nil, clusterNames, err
				}
				decision.Status.Decisions = append(decision.Status.Decisions, clusterv1beta1.ClusterDecision{ClusterName: clusterName})
				clusterNames.Insert(clusterName)
			}

			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace).UpdateStatus(context.TODO(), decision, metav1.UpdateOptions{})
			return manifestWorkReplicaSet, clusterNames, err
		}
	})

	ginkgo.AfterEach(func() {
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), namespaceName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	// A sanity check ensuring crd is created correctly which should be refactored later
	ginkgo.Context("Create and update a manifestWorkReplicaSet", func() {
		ginkgo.It("should create/update/delete successfully", func() {
			manifestWorkReplicaSet, clusterNames, err := generateTestFixture(3)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet, 3), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
			gomega.Eventually(assertWorksSameLabelAsReplicaSet(manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
			gomega.Eventually(assertSummary(workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total: 3,
			}, manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Update decision so manifestworks should be updated")
			decision, err := hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace).Get(
				context.TODO(), placementDecision.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			removedCluster := decision.Status.Decisions[2].ClusterName
			decision.Status.Decisions = decision.Status.Decisions[:2]
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace).UpdateStatus(context.TODO(), decision, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			clusterNames.Delete(removedCluster)
			gomega.Eventually(assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet, 2), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
			gomega.Eventually(assertSummary(workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total: 2,
			}, manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Delete manifestworkreplicaset")
			err = hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespaceName).Delete(context.TODO(), manifestWorkReplicaSet.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Eventually(assertWorksByReplicaSet(sets.New[string](), manifestWorkReplicaSet, 0), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		})

		ginkgo.It("status should update when manifestwork status change", func() {
			manifestWorkReplicaSet, clusterNames, err := generateTestFixture(1)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet, 1), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
			gomega.Eventually(assertSummary(workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total: 1,
			}, manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			key := fmt.Sprintf("%s.%s", manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name)
			works, err := hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s", key),
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			for _, work := range works.Items {
				workCopy := work.DeepCopy()
				meta.SetStatusCondition(&workCopy.Status.Conditions, metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "ApplyTest"})
				meta.SetStatusCondition(&workCopy.Status.Conditions, metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "ApplyTest"})
				_, err := hubWorkClient.WorkV1().ManifestWorks(workCopy.Namespace).UpdateStatus(context.TODO(), workCopy, metav1.UpdateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			gomega.Eventually(assertSummary(workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total:     1,
				Applied:   1,
				Available: 1,
			}, manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			works, err = hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s", key),
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			for _, work := range works.Items {
				workCopy := work.DeepCopy()
				meta.SetStatusCondition(&workCopy.Status.Conditions, metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionFalse, Reason: "ApplyTest"})
				_, err := hubWorkClient.WorkV1().ManifestWorks(workCopy.Namespace).UpdateStatus(context.TODO(), workCopy, metav1.UpdateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			gomega.Eventually(assertSummary(workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total:     1,
				Applied:   1,
				Available: 0,
			}, manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		})

		ginkgo.It("should delete manifestworks from old placement when placementRef changes", func() {
			// Create first placement and placementDecision
			placement1 := &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement-1",
					Namespace: namespaceName,
				},
			}
			_, err := hubClusterClient.ClusterV1beta1().Placements(placement1.Namespace).Create(context.TODO(), placement1, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			placementDecision1 := &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement-decision-1",
					Namespace: namespaceName,
					Labels: map[string]string{
						clusterv1beta1.PlacementLabel:          placement1.Name,
						clusterv1beta1.DecisionGroupIndexLabel: "0",
					},
				},
			}
			decision1, err := hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision1.Namespace).Create(
				context.TODO(), placementDecision1, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Create clusters for first placement
			cluster1Names := sets.New[string]()
			for i := 0; i < 2; i++ {
				clusterName := "cluster-placement1-" + utilrand.String(5)
				ns := &corev1.Namespace{}
				ns.Name = clusterName
				_, err = spokeKubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				decision1.Status.Decisions = append(decision1.Status.Decisions, clusterv1beta1.ClusterDecision{ClusterName: clusterName})
				cluster1Names.Insert(clusterName)
			}
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision1.Namespace).UpdateStatus(context.TODO(), decision1, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Create manifestWorkReplicaSet with first placement
			manifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap("defaut", cm1, map[string]string{"a": "b"}, nil)),
			}
			placementRef1 := workapiv1alpha1.LocalPlacementReference{
				Name:            placement1.Name,
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
			}

			manifestWorkReplicaSet := &workapiv1alpha1.ManifestWorkReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: namespaceName,
				},
				Spec: workapiv1alpha1.ManifestWorkReplicaSetSpec{
					ManifestWorkTemplate: workapiv1.ManifestWorkSpec{
						Workload: workapiv1.ManifestsTemplate{
							Manifests: manifests,
						},
					},
					PlacementRefs: []workapiv1alpha1.LocalPlacementReference{placementRef1},
				},
			}

			_, err = hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespaceName).Create(context.TODO(), manifestWorkReplicaSet, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Verify manifestworks are created for first placement
			gomega.Eventually(func() error {
				key := fmt.Sprintf("%s.%s", manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name)
				works, err := hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s,work.open-cluster-management.io/placementname=%s", key, placement1.Name),
				})
				if err != nil {
					return err
				}
				if len(works.Items) != 2 {
					return fmt.Errorf("expected 2 manifestworks, got %d", len(works.Items))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Create second placement and update manifestWorkReplicaSet to use it")
			placement2 := &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement-2",
					Namespace: namespaceName,
				},
			}
			_, err = hubClusterClient.ClusterV1beta1().Placements(placement2.Namespace).Create(context.TODO(), placement2, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			placementDecision2 := &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement-decision-2",
					Namespace: namespaceName,
					Labels: map[string]string{
						clusterv1beta1.PlacementLabel:          placement2.Name,
						clusterv1beta1.DecisionGroupIndexLabel: "0",
					},
				},
			}
			decision2, err := hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision2.Namespace).Create(
				context.TODO(), placementDecision2, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Create clusters for second placement
			cluster2Names := sets.New[string]()
			for i := 0; i < 2; i++ {
				clusterName := "cluster-placement2-" + utilrand.String(5)
				ns := &corev1.Namespace{}
				ns.Name = clusterName
				_, err = spokeKubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				decision2.Status.Decisions = append(decision2.Status.Decisions, clusterv1beta1.ClusterDecision{ClusterName: clusterName})
				cluster2Names.Insert(clusterName)
			}
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision2.Namespace).UpdateStatus(context.TODO(), decision2, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Update manifestWorkReplicaSet to use second placement
			placementRef2 := workapiv1alpha1.LocalPlacementReference{
				Name:            placement2.Name,
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
			}

			mwrs, err := hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespaceName).Get(context.TODO(), manifestWorkReplicaSet.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			mwrs.Spec.PlacementRefs = []workapiv1alpha1.LocalPlacementReference{placementRef2}
			_, err = hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespaceName).Update(context.TODO(), mwrs, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Verify manifestworks from first placement are deleted")
			gomega.Eventually(func() error {
				key := fmt.Sprintf("%s.%s", manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name)
				works, err := hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s,work.open-cluster-management.io/placementname=%s", key, placement1.Name),
				})
				if err != nil {
					return err
				}
				if len(works.Items) != 0 {
					return fmt.Errorf("expected 0 manifestworks for placement1, got %d", len(works.Items))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Verify manifestworks for second placement are created")
			gomega.Eventually(func() error {
				key := fmt.Sprintf("%s.%s", manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name)
				works, err := hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s,work.open-cluster-management.io/placementname=%s", key, placement2.Name),
				})
				if err != nil {
					return err
				}
				if len(works.Items) != 2 {
					return fmt.Errorf("expected 2 manifestworks for placement2, got %d", len(works.Items))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		})
	})

	ginkgo.It("rollout progressive", func() {
		manifestWorkReplicaSet, clusterNames, err := generateTestFixture(6, func(rs *workapiv1alpha1.ManifestWorkReplicaSet) *workapiv1alpha1.ManifestWorkReplicaSet {
			rs.Spec.PlacementRefs[0].RolloutStrategy = clusterv1alpha1.RolloutStrategy{
				Type: clusterv1alpha1.Progressive,
				Progressive: &clusterv1alpha1.RolloutProgressive{
					MaxConcurrency: intstr.FromInt32(2),
				},
			}
			return rs
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet, 2), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		ginkgo.By("set work status to true")
		key := fmt.Sprintf("%s.%s", manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name)
		works, err := hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s", key),
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		for _, work := range works.Items {
			workCopy := work.DeepCopy()
			meta.SetStatusCondition(&workCopy.Status.Conditions, metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				Reason:             "AppliedManifestWorkComplete",
				ObservedGeneration: workCopy.Generation,
			})
			meta.SetStatusCondition(&workCopy.Status.Conditions, metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionFalse,
				Reason:             "AppliedManifestWorkComplete",
				ObservedGeneration: workCopy.Generation,
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			_, err := hubWorkClient.WorkV1().ManifestWorks(workCopy.Namespace).UpdateStatus(context.TODO(), workCopy, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
		gomega.Eventually(assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet, 4), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		works, err = hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s", key),
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		for _, work := range works.Items {
			workCopy := work.DeepCopy()
			meta.SetStatusCondition(&workCopy.Status.Conditions, metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				Reason:             "AppliedManifestWorkComplete",
				ObservedGeneration: workCopy.Generation,
			})
			meta.SetStatusCondition(&workCopy.Status.Conditions, metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionFalse,
				Reason:             "AppliedManifestWorkComplete",
				ObservedGeneration: workCopy.Generation,
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			_, err := hubWorkClient.WorkV1().ManifestWorks(workCopy.Namespace).UpdateStatus(context.TODO(), workCopy, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
		gomega.Eventually(assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet, 6), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
	})

	ginkgo.It("rolling exceeds max failure", func() {
		manifestWorkReplicaSet, clusterNames, err := generateTestFixture(3, func(rs *workapiv1alpha1.ManifestWorkReplicaSet) *workapiv1alpha1.ManifestWorkReplicaSet {
			rs.Spec.PlacementRefs[0].RolloutStrategy = clusterv1alpha1.RolloutStrategy{
				Type: clusterv1alpha1.Progressive,
				Progressive: &clusterv1alpha1.RolloutProgressive{
					MaxConcurrency: intstr.FromInt32(2),
					RolloutConfig: clusterv1alpha1.RolloutConfig{
						MaxFailures: intstr.FromInt32(1),
					},
				},
			}
			return rs
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet, 2), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		ginkgo.By("set work status to fail")
		key := fmt.Sprintf("%s.%s", manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name)
		works, err := hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s", key),
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		for _, work := range works.Items {
			workCopy := work.DeepCopy()
			meta.SetStatusCondition(&workCopy.Status.Conditions, metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				Reason:             "Applied",
				ObservedGeneration: workCopy.Generation,
			})
			meta.SetStatusCondition(&workCopy.Status.Conditions, metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionTrue,
				Reason:             "Applying",
				ObservedGeneration: workCopy.Generation,
			})
			meta.SetStatusCondition(&workCopy.Status.Conditions, metav1.Condition{
				Type:               workapiv1.WorkDegraded,
				Status:             metav1.ConditionTrue,
				Reason:             "ApplyFailed",
				ObservedGeneration: workCopy.Generation,
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			_, err := hubWorkClient.WorkV1().ManifestWorks(workCopy.Namespace).UpdateStatus(context.TODO(), workCopy, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		ginkgo.By("rollout stop since max failure exceeds")
		gomega.Eventually(
			assertCondition(
				workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut, metav1.ConditionFalse, manifestWorkReplicaSet),
			eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		gomega.Eventually(
			assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet, 2), eventuallyTimeout, eventuallyInterval).
			Should(gomega.Succeed())
	})

	ginkgo.It("rolling with min success time", func() {
		manifestWorkReplicaSet, clusterNames, err := generateTestFixture(2,
			func(rs *workapiv1alpha1.ManifestWorkReplicaSet) *workapiv1alpha1.ManifestWorkReplicaSet {
				rs.Spec.PlacementRefs[0].RolloutStrategy = clusterv1alpha1.RolloutStrategy{
					Type: clusterv1alpha1.Progressive,
					Progressive: &clusterv1alpha1.RolloutProgressive{
						MaxConcurrency: intstr.FromInt32(1),
						RolloutConfig: clusterv1alpha1.RolloutConfig{
							MinSuccessTime: metav1.Duration{
								Duration: 5 * time.Second,
							},
						},
					},
				}
				return rs
			})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(
			assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet, 1), eventuallyTimeout, eventuallyInterval).
			Should(gomega.Succeed())

		ginkgo.By("set work status to true")
		key := fmt.Sprintf("%s.%s", manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name)
		works, err := hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s", key),
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		for _, work := range works.Items {
			workCopy := work.DeepCopy()
			meta.SetStatusCondition(
				&workCopy.Status.Conditions,
				metav1.Condition{
					Type:               workapiv1.WorkApplied,
					Status:             metav1.ConditionTrue,
					Reason:             "Applied",
					ObservedGeneration: workCopy.Generation,
				})
			meta.SetStatusCondition(
				&workCopy.Status.Conditions,
				metav1.Condition{
					Type:               workapiv1.WorkProgressing,
					Status:             metav1.ConditionFalse,
					Reason:             "AppliedManifestWorkComplete",
					ObservedGeneration: workCopy.Generation,
				})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			_, err := hubWorkClient.WorkV1().
				ManifestWorks(workCopy.Namespace).UpdateStatus(context.TODO(), workCopy, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		ginkgo.By("stay in progression during minssucess time and should be complete after it")
		gomega.Eventually(func() error {
			rs, err := hubWorkClient.WorkV1alpha1().
				ManifestWorkReplicaSets(manifestWorkReplicaSet.Namespace).
				Get(context.TODO(), manifestWorkReplicaSet.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			cond := meta.FindStatusCondition(rs.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
			if cond == nil {
				return fmt.Errorf("condition type %s is not found", workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
			}
			if metav1.Now().Sub(cond.LastTransitionTime.Time) < 5*time.Second {
				if cond.Status != metav1.ConditionFalse {
					return fmt.Errorf("should not set success before min success time")
				}
			} else {
				if cond.Status != metav1.ConditionTrue {
					return fmt.Errorf("should set success after min success time")
				}
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
	})

	ginkgo.It("rolling deadline meets and tolerate max failure", func() {
		manifestWorkReplicaSet, clusterNames, err := generateTestFixture(2,
			func(rs *workapiv1alpha1.ManifestWorkReplicaSet) *workapiv1alpha1.ManifestWorkReplicaSet {
				rs.Spec.PlacementRefs[0].RolloutStrategy = clusterv1alpha1.RolloutStrategy{
					Type: clusterv1alpha1.Progressive,
					Progressive: &clusterv1alpha1.RolloutProgressive{
						MaxConcurrency: intstr.FromInt32(1),
						RolloutConfig: clusterv1alpha1.RolloutConfig{
							ProgressDeadline: "5s",
							MaxFailures:      intstr.FromInt32(1),
						},
					},
				}
				return rs
			})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(
			assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet, 1), eventuallyTimeout, eventuallyInterval).
			Should(gomega.Succeed())

		ginkgo.By("set work status to fail")
		key := fmt.Sprintf("%s.%s", manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name)
		works, err := hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s", key),
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		for _, work := range works.Items {
			workCopy := work.DeepCopy()
			meta.SetStatusCondition(
				&workCopy.Status.Conditions,
				metav1.Condition{
					Type:               workapiv1.WorkApplied,
					Status:             metav1.ConditionTrue,
					Reason:             "Applied",
					ObservedGeneration: workCopy.Generation,
				})
			meta.SetStatusCondition(
				&workCopy.Status.Conditions,
				metav1.Condition{
					Type:               workapiv1.WorkProgressing,
					Status:             metav1.ConditionTrue,
					Reason:             "Applying",
					ObservedGeneration: workCopy.Generation,
				})
			meta.SetStatusCondition(
				&workCopy.Status.Conditions,
				metav1.Condition{
					Type:               workapiv1.WorkDegraded,
					Status:             metav1.ConditionTrue,
					Reason:             "ApplyFailed",
					ObservedGeneration: workCopy.Generation,
				})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			_, err := hubWorkClient.WorkV1().
				ManifestWorks(workCopy.Namespace).UpdateStatus(context.TODO(), workCopy, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		ginkgo.By("proceed to the next cluster after timeout")
		gomega.Eventually(
			assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet, 2), eventuallyTimeout, eventuallyInterval).
			Should(gomega.Succeed())
	})
})

func assertSummary(summary workapiv1alpha1.ManifestWorkReplicaSetSummary, mwrs *workapiv1alpha1.ManifestWorkReplicaSet) func() error {
	return func() error {
		rs, err := hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(mwrs.Namespace).Get(context.TODO(), mwrs.Name, metav1.GetOptions{})

		if err != nil {
			return err
		}

		if rs.Status.Summary != summary {
			return fmt.Errorf("unexpected summary expected: %v, got :%v", summary, rs.Status.Summary)
		}

		return nil
	}
}

func assertCondition(condType string, status metav1.ConditionStatus, mwrs *workapiv1alpha1.ManifestWorkReplicaSet) func() error {
	return func() error {
		rs, err := hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(mwrs.Namespace).Get(context.TODO(), mwrs.Name, metav1.GetOptions{})

		if err != nil {
			return err
		}

		cond := meta.FindStatusCondition(rs.Status.Conditions, condType)
		if cond == nil {
			return fmt.Errorf("condition type %s is not found", condType)
		}
		if cond.Status != status {
			return fmt.Errorf("condition status is not correct, want %v got %v", status, cond.Status)
		}

		return nil
	}
}

func assertWorksByReplicaSet(clusterNames sets.Set[string], mwrs *workapiv1alpha1.ManifestWorkReplicaSet, num int) func() error {
	return func() error {
		key := fmt.Sprintf("%s.%s", mwrs.Namespace, mwrs.Name)
		works, err := hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s", key),
		})
		if err != nil {
			return err
		}

		if len(works.Items) != num {
			return fmt.Errorf("the number of applied works should equal to %d, but got %d", num, len(works.Items))
		}

		for _, work := range works.Items {
			if !clusterNames.Has(work.Namespace) {
				return fmt.Errorf("unexpected work %s/%s", work.Namespace, work.Name)
			}
		}

		return nil
	}
}

func assertWorksSameLabelAsReplicaSet(mwrs *workapiv1alpha1.ManifestWorkReplicaSet) func() error {
	return func() error {
		key := fmt.Sprintf("%s.%s", mwrs.Namespace, mwrs.Name)
		works, err := hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s", key),
		})
		if err != nil {
			return err
		}

		for _, work := range works.Items {
			for k, v := range mwrs.Labels {
				if work.Labels[k] != v {
					return fmt.Errorf("label %s mismatch: expected %s, got %s", k, v, work.Labels[k])
				}
			}
		}
		return nil
	}
}
