package integration

import (
	"context"
	"fmt"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	"open-cluster-management.io/work/test/integration/util"
)

var _ = ginkgo.Describe("ManifestWorkReplicaSet", func() {
	var namespaceName string
	var placement *clusterv1beta1.Placement
	var placementDecision *clusterv1beta1.PlacementDecision
	var generateTestFixture func(numberOfClusters int) (*workapiv1alpha1.ManifestWorkReplicaSet, sets.Set[string], error)

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
				Labels:    map[string]string{clusterv1beta1.PlacementLabel: placement.Name},
			},
		}

		generateTestFixture = func(numberOfClusters int) (*workapiv1alpha1.ManifestWorkReplicaSet, sets.Set[string], error) {
			clusterNames := sets.New[string]()
			manifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap("defaut", "cm1", map[string]string{"a": "b"}, nil)),
			}
			placementRef := workapiv1alpha1.LocalPlacementReference{Name: placement.Name}

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

			_, err := hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespaceName).Create(context.TODO(), manifestWorkReplicaSet, metav1.CreateOptions{})
			if err != nil {
				return nil, clusterNames, err
			}

			_, err = hubClusterClient.ClusterV1beta1().Placements(placement.Namespace).Create(context.TODO(), placement, metav1.CreateOptions{})
			if err != nil {
				return nil, clusterNames, err
			}

			decision, err := hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace).Create(context.TODO(), placementDecision, metav1.CreateOptions{})
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

			decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace).UpdateStatus(context.TODO(), decision, metav1.UpdateOptions{})
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

			gomega.Eventually(assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
			gomega.Eventually(assertSummary(workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total: 3,
			}, manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Update decision so manifestworks should be updated")
			decision, err := hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace).Get(context.TODO(), placementDecision.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			removedCluster := decision.Status.Decisions[2].ClusterName
			decision.Status.Decisions = decision.Status.Decisions[:2]
			decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace).UpdateStatus(context.TODO(), decision, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			clusterNames.Delete(removedCluster)
			gomega.Eventually(assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
			gomega.Eventually(assertSummary(workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total: 2,
			}, manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Delete manifestworkreplicaset")
			err = hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespaceName).Delete(context.TODO(), manifestWorkReplicaSet.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Eventually(assertWorksByReplicaSet(sets.New[string](), manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		})

		ginkgo.It("status should update when manifestwork status change", func() {
			manifestWorkReplicaSet, clusterNames, err := generateTestFixture(1)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(assertWorksByReplicaSet(clusterNames, manifestWorkReplicaSet), eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
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

func assertWorksByReplicaSet(clusterNames sets.Set[string], mwrs *workapiv1alpha1.ManifestWorkReplicaSet) func() error {
	return func() error {
		key := fmt.Sprintf("%s.%s", mwrs.Namespace, mwrs.Name)
		works, err := hubWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("work.open-cluster-management.io/manifestworkreplicaset=%s", key),
		})
		if err != nil {
			return err
		}

		if len(works.Items) != clusterNames.Len() {
			return fmt.Errorf("The number of applied works should equal to %d, but got %d", clusterNames.Len(), len(works.Items))
		}

		for _, work := range works.Items {
			if !clusterNames.Has(work.Namespace) {
				return fmt.Errorf("unexpected work %s/%s", work.Namespace, work.Name)
			}
		}

		return nil
	}
}
