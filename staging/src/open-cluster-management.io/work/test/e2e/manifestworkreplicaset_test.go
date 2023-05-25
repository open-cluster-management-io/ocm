package e2e

import (
	"context"
	"fmt"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

var _ = ginkgo.Describe("ManifestWorkReplicaSet", func() {
	var err error
	var nameSuffix string

	ginkgo.BeforeEach(func() {
		nameSuffix = rand.String(5)
	})

	ginkgo.Context("Creating a ManifestWorkReplicaSet", func() {
		ginkgo.It("Should create ManifestWorkReplicaSet successfullt", func() {
			ginkgo.By("create manifestworkreplicaset")
			ns1 := fmt.Sprintf("ns1-%s", nameSuffix)
			work := newManifestWork("", "",
				newConfigmap(ns1, "cm1", nil, nil),
				newConfigmap(ns1, "cm2", nil, nil),
				newNamespace(ns1))
			placementRef := workapiv1alpha1.LocalPlacementReference{Name: "placement-test"}
			manifestWorkReplicaSet := &workapiv1alpha1.ManifestWorkReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "mwrset-",
					Namespace:    metav1.NamespaceDefault,
				},
				Spec: workapiv1alpha1.ManifestWorkReplicaSetSpec{
					ManifestWorkTemplate: work.Spec,
					PlacementRefs:        []workapiv1alpha1.LocalPlacementReference{placementRef},
				},
			}
			manifestWorkReplicaSet, err = hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(metav1.NamespaceDefault).Create(context.TODO(), manifestWorkReplicaSet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			placement := &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementRef.Name,
					Namespace: metav1.NamespaceDefault,
				},
			}

			placementDecision := &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placement.Name,
					Namespace: metav1.NamespaceDefault,
					Labels:    map[string]string{clusterv1beta1.PlacementLabel: placement.Name},
				},
			}

			placement, err = hubClusterClient.ClusterV1beta1().Placements(placement.Namespace).Create(context.TODO(), placement, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			placementDecision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace).Create(context.TODO(), placementDecision, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			placementDecision.Status.Decisions = []clusterv1beta1.ClusterDecision{{ClusterName: clusterName}}
			placementDecision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace).UpdateStatus(context.TODO(), placementDecision, metav1.UpdateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("check if resources are applied for manifests")
			gomega.Eventually(func() error {
				_, err := spokeKubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = spokeKubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm2", metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = spokeKubeClient.CoreV1().Namespaces().Get(context.Background(), ns1, metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("check if manifestworkreplicaset status")
			gomega.Eventually(func() error {
				mwrs, err := hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(metav1.NamespaceDefault).Get(context.TODO(), manifestWorkReplicaSet.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				expectedSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
					Total:     1,
					Available: 1,
					Applied:   1,
				}

				if mwrs.Status.Summary != expectedSummary {
					return fmt.Errorf("summary is not correct, expect %v, got %v", expectedSummary, mwrs.Status.Summary)
				}

				if !meta.IsStatusConditionTrue(mwrs.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied) {
					return fmt.Errorf("manifestwork replicaset condition is not correct")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// TODO we should also update manifestwork replicaset and test

			err = hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(metav1.NamespaceDefault).Delete(context.TODO(), manifestWorkReplicaSet.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = hubClusterClient.ClusterV1beta1().Placements(placement.Namespace).Delete(context.TODO(), placement.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementDecision.Namespace).Delete(context.TODO(), placementDecision.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})
})
