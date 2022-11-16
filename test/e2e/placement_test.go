package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

const (
	clusterSetLabel = "cluster.open-cluster-management.io/clusterset"
	placementLabel  = "cluster.open-cluster-management.io/placement"
)

var _ = Describe("Placement", func() {
	var suffix string
	var clusterNamePrefix string
	var placementNamespace string
	var placementName string
	var clusterSetName string

	BeforeEach(func() {
		suffix = rand.String(6)
		clusterNamePrefix = fmt.Sprintf("cluster-%s", suffix)
		clusterSetName = fmt.Sprintf("clusterset-%s", suffix)
		placementNamespace = fmt.Sprintf("ns-%s", suffix)
		placementName = fmt.Sprintf("ns-%s", suffix)

		By("Create placement namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: placementNamespace,
			},
		}
		_, err := t.KubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// delete namespace
		err := t.KubeClient.CoreV1().Namespaces().Delete(context.Background(), placementNamespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		// delete clusters created
		clusterList, err := t.ClusterClient.ClusterV1().ManagedClusters().List(context.Background(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", clusterSetLabel, clusterSetName),
		})
		Expect(err).ToNot(HaveOccurred())
		for _, cluster := range clusterList.Items {
			err = t.ClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), cluster.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}

		// delete created clusterset
		err = t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), clusterSetName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should schedule placement successfully", func() {
		By("Create clusterset/clustersetbinding")
		clusterset := &clusterapiv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
		}
		_, err := t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		csb := &clusterapiv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: placementNamespace,
				Name:      clusterSetName,
			},
			Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = t.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(placementNamespace).Create(context.Background(), csb, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		numOfClusters := 5
		By(fmt.Sprintf("Create %d clusters", numOfClusters))
		for i := 0; i < numOfClusters; i++ {
			cluster := &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: clusterNamePrefix,
					Labels: map[string]string{
						clusterSetLabel: clusterSetName,
					},
				},
			}
			_, err = t.ClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		}

		By("Create a placement")
		placement := &clusterapiv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: placementNamespace,
				Name:      placementName,
			},
			Spec: clusterapiv1beta1.PlacementSpec{
				Tolerations: []clusterapiv1beta1.Toleration{
					{
						Key:      "cluster.open-cluster-management.io/unreachable",
						Operator: clusterapiv1beta1.TolerationOpExists,
					},
				},
			},
		}

		_, err = t.ClusterClient.ClusterV1beta1().Placements(placementNamespace).Create(context.TODO(), placement, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Check if placementdecisions are created with desired number of decisions")
		Eventually(func() bool {
			placementDecisions, err := t.ClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", placementLabel, placementName),
			})
			if err != nil {
				return false
			}

			nod := 0
			for _, placementDecision := range placementDecisions.Items {
				nod += len(placementDecision.Status.Decisions)
			}

			return nod == numOfClusters
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(BeTrue())

		By("Check if placement status is updated")
		Eventually(func() bool {
			placement, err := t.ClusterClient.ClusterV1beta1().Placements(placementNamespace).Get(context.TODO(), placementName, metav1.GetOptions{})
			if err != nil {
				return false
			}

			if int(placement.Status.NumberOfSelectedClusters) != numOfClusters {
				return false
			}

			return meta.IsStatusConditionTrue(placement.Status.Conditions, clusterapiv1beta1.PlacementConditionSatisfied)
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(BeTrue())

		By("Delete placement")
		err = t.ClusterClient.ClusterV1beta1().Placements(placementNamespace).Delete(context.TODO(), placementName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Check if placementdecisions are deleted as well")
		Eventually(func() bool {
			placementDecisions, err := t.ClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", placementLabel, placementName),
			})
			if err != nil {
				return false
			}

			return len(placementDecisions.Items) == 0
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(BeTrue())
	})
})
