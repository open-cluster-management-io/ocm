package e2e

import (
	"context"
	"fmt"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	mwrSetLabel = "work.open-cluster-management.io/manifestworkreplicaset"
)

var _ = ginkgo.Describe("Test ManifestWorkReplicaSet", func() {
	var err error
	var nameSuffix string

	ginkgo.BeforeEach(func() {
		// Enable manifestWorkReplicaSet feature if not enabled
		gomega.Eventually(func() error {
			return t.EnableWorkFeature("ManifestWorkReplicaSet")
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

		// the work controller deployment should be running
		gomega.Eventually(t.CheckHubReady, t.EventuallyTimeout, t.EventuallyInterval).Should(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		gomega.Eventually(func() error {
			return t.RemoveWorkFeature("ManifestWorkReplicaSet")
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())
	})

	ginkgo.Context("Creating a ManifestWorkReplicaSet and check created resources", func() {
		var klusterletName, clusterName string
		ginkgo.JustBeforeEach(func() {
			nameSuffix = rand.String(5)

			if deployKlusterlet {
				klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
				clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
				agentNamespace := fmt.Sprintf("open-cluster-management-agent-%s", rand.String(6))
				_, err := t.CreateApprovedKlusterlet(klusterletName, clusterName, agentNamespace, operatorapiv1.InstallModeDefault)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}
		})

		ginkgo.JustAfterEach(func() {
			if deployKlusterlet {
				ginkgo.By(fmt.Sprintf("clean klusterlet %v resources after the test case", klusterletName))
				gomega.Expect(t.cleanKlusterletResources(klusterletName, clusterName)).To(gomega.BeNil())
			}
		})

		ginkgo.It("Should create ManifestWorkReplicaSet successfullt", func() {
			ginkgo.By("create manifestworkreplicaset")
			ns1 := fmt.Sprintf("ns1-%s", nameSuffix)
			work := newManifestWork("", "",
				util.NewConfigmap(ns1, "cm1", nil, nil),
				util.NewConfigmap(ns1, "cm2", nil, nil),
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
			manifestWorkReplicaSet, err = t.HubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(metav1.NamespaceDefault).Create(context.TODO(), manifestWorkReplicaSet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			csb := &clusterapiv1beta2.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      "default",
				},
				Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
					ClusterSet: "default",
				},
			}
			_, err = t.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(metav1.NamespaceDefault).Create(context.Background(), csb, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			placement := &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementRef.Name,
					Namespace: metav1.NamespaceDefault,
				},
			}

			placement, err = t.ClusterClient.ClusterV1beta1().Placements(placement.Namespace).Create(context.TODO(), placement, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("check if resources are applied for manifests")
			gomega.Eventually(func() error {
				_, err := t.SpokeKubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = t.SpokeKubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm2", metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = t.SpokeKubeClient.CoreV1().Namespaces().Get(context.Background(), ns1, metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("check if manifestworkreplicaset status")
			gomega.Eventually(func() error {
				mwrs, err := t.HubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(metav1.NamespaceDefault).Get(context.TODO(), manifestWorkReplicaSet.Name, metav1.GetOptions{})
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

			err = t.HubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(metav1.NamespaceDefault).Delete(context.TODO(), manifestWorkReplicaSet.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = t.ClusterClient.ClusterV1beta1().Placements(placement.Namespace).Delete(context.TODO(), placement.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("Creating a ManifestWorkReplicaSet across multiple clusters", func() {
		var suffix string
		var clusterNamePrefix string
		var namespace string
		var placementName string
		var clusterSetName string
		var mwReplicaSetName string

		ginkgo.JustBeforeEach(func() {
			suffix = rand.String(6)
			clusterNamePrefix = fmt.Sprintf("cls-%s", suffix)
			clusterSetName = fmt.Sprintf("clusterset-%s", suffix)
			namespace = fmt.Sprintf("ns-%s", suffix)
			placementName = fmt.Sprintf("placement-%s", suffix)
			mwReplicaSetName = fmt.Sprintf("mwrset-%s", suffix)

			ginkgo.By("Create namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			_, err := t.HubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.JustAfterEach(func() {
			// delete namespace
			err := t.HubKubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// delete clusters created
			clusterList, err := t.ClusterClient.ClusterV1().ManagedClusters().List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", clusterapiv1beta2.ClusterSetLabel, clusterSetName),
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			for _, cluster := range clusterList.Items {
				err = t.ClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), cluster.Name, metav1.DeleteOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			// delete created clusterset
			err = t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), clusterSetName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// delete placement
			err = t.ClusterClient.ClusterV1beta1().Placements(namespace).Delete(context.TODO(), placementName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("Create ManifestWorkReplicaSet and check the created manifestWorks", func() {
			ginkgo.By("Create clusterset and clustersetbinding")
			clusterset := &clusterapiv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterSetName,
				},
			}
			_, err := t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
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
			_, err = t.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), csb, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			numOfClusters := 3
			ginkgo.By(fmt.Sprintf("Create %d clusters", numOfClusters))
			for i := 0; i < numOfClusters; i++ {
				clsName := fmt.Sprintf("%s-%d", clusterNamePrefix, i)
				cluster := &clusterapiv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: clsName,
						Labels: map[string]string{
							clusterapiv1beta2.ClusterSetLabel: clusterSetName,
						},
					},
				}
				_, err = t.ClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: clsName,
					},
				}
				_, err = t.HubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			ginkgo.By("Create placement")
			placement := &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      placementName,
				},
				Spec: clusterv1beta1.PlacementSpec{
					Tolerations: []clusterv1beta1.Toleration{
						{
							Key:      "cluster.open-cluster-management.io/unreachable",
							Operator: clusterv1beta1.TolerationOpExists,
						},
					},
				},
			}

			_, err = t.ClusterClient.ClusterV1beta1().Placements(namespace).Create(context.TODO(), placement, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Create manifestWorkReplicaSet")
			manifest := workapiv1.Manifest{}
			manifest.Object = util.NewConfigmap("default", "cm", map[string]string{"a": "b"}, nil)
			placementRef := workapiv1alpha1.LocalPlacementReference{Name: placementName}
			mwReplicaSet := &workapiv1alpha1.ManifestWorkReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mwReplicaSetName,
					Namespace: namespace,
				},
				Spec: workapiv1alpha1.ManifestWorkReplicaSetSpec{
					ManifestWorkTemplate: workapiv1.ManifestWorkSpec{
						Workload: workapiv1.ManifestsTemplate{
							Manifests: []workapiv1.Manifest{
								manifest,
							},
						},
					},
					PlacementRefs: []workapiv1alpha1.LocalPlacementReference{placementRef},
				},
			}
			_, err = t.HubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Create(context.TODO(), mwReplicaSet, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Check manifestWork replicaSet status is updated")
			gomega.Eventually(func() bool {
				mwrSet, err := t.HubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Get(context.TODO(), mwReplicaSetName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				if !meta.IsStatusConditionTrue(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified) {
					return false
				}

				return int(mwrSet.Status.Summary.Total) == numOfClusters
			}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeTrue())

			ginkgo.By("Check manifestWorks are created")
			gomega.Eventually(func() bool {
				manifestWorkList, err := t.HubWorkClient.WorkV1().ManifestWorks("").List(context.TODO(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s.%s", mwrSetLabel, namespace, mwReplicaSetName),
				})
				if err != nil {
					return false
				}

				return len(manifestWorkList.Items) == numOfClusters
			}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeTrue())

			ginkgo.By("Delete manifestWorkReplicaSet")
			err = t.HubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Delete(context.TODO(), mwReplicaSetName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Check manifestworks are deleted")
			gomega.Eventually(func() bool {
				manifestWorkList, err := t.HubWorkClient.WorkV1().ManifestWorks("").List(context.TODO(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s.%s", mwrSetLabel, namespace, mwReplicaSetName),
				})
				if err != nil {
					return false
				}

				return len(manifestWorkList.Items) == 0
			}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeTrue())
		})
	})
})
