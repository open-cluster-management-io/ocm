package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	mwrSetLabel = "work.open-cluster-management.io/manifestworkreplicaset"
)

var _ = ginkgo.Describe("Test ManifestWorkReplicaSet", ginkgo.Label("manifestworkreplicaset"), func() {
	var err error
	var nameSuffix string

	ginkgo.BeforeEach(func() {
		nameSuffix = rand.String(6)
	})

	ginkgo.Context("Creating a ManifestWorkReplicaSet and check created resources", func() {
		ginkgo.It("Should create ManifestWorkReplicaSet successfully", func() {
			ginkgo.By("create manifestworkreplicaset")
			ns1 := fmt.Sprintf("ns1-%s", nameSuffix)
			work := newManifestWork("", "",
				util.NewConfigmap(ns1, "cm1", nil, nil),
				util.NewConfigmap(ns1, "cm2", nil, nil),
				newNamespace(ns1))
			placementRef := workapiv1alpha1.LocalPlacementReference{
				Name: "placement-test",
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{
					Type: clusterv1alpha1.All,
					All: &clusterv1alpha1.RolloutAll{
						RolloutConfig: clusterv1alpha1.RolloutConfig{
							ProgressDeadline: "None",
						},
					},
				},
			}
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
			manifestWorkReplicaSet, err = hub.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(metav1.NamespaceDefault).Create(
				context.TODO(), manifestWorkReplicaSet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			csb := &clusterapiv1beta2.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      universalClusterSetName,
				},
				Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
					ClusterSet: universalClusterSetName,
				},
			}
			_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(metav1.NamespaceDefault).Create(
				context.Background(), csb, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			placement := &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementRef.Name,
					Namespace: metav1.NamespaceDefault,
				},
				Spec: clusterv1beta1.PlacementSpec{
					ClusterSets: []string{universalClusterSetName},
				},
			}

			placement, err = hub.ClusterClient.ClusterV1beta1().Placements(placement.Namespace).Create(context.TODO(), placement, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("check if resources are applied for manifests")
			gomega.Eventually(func() error {
				_, err := spoke.KubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = spoke.KubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm2", metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = spoke.KubeClient.CoreV1().Namespaces().Get(context.Background(), ns1, metav1.GetOptions{})
				return err
			}).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("check if manifestworkreplicaset status")
			gomega.Eventually(func() error {
				mwrs, err := hub.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(metav1.NamespaceDefault).Get(
					context.TODO(), manifestWorkReplicaSet.Name, metav1.GetOptions{})
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
			}).ShouldNot(gomega.HaveOccurred())

			// TODO we should also update manifestwork replicaset and test

			err = hub.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(metav1.NamespaceDefault).Delete(
				context.TODO(), manifestWorkReplicaSet.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = hub.ClusterClient.ClusterV1beta1().Placements(placement.Namespace).Delete(context.TODO(), placement.Name, metav1.DeleteOptions{})
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
		var numOfClusters = 3

		ginkgo.JustBeforeEach(func() {
			suffix = rand.String(6)
			clusterNamePrefix = fmt.Sprintf("cls-%s", suffix)
			clusterSetName = fmt.Sprintf("clusterset-%s", suffix)
			namespace = fmt.Sprintf("ns-%s", suffix)
			placementName = fmt.Sprintf("placement-%s", suffix)
			mwReplicaSetName = fmt.Sprintf("mwrset-%s", suffix)

			ginkgo.By(fmt.Sprintf("Create namespace %s", namespace))
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			_, err := hub.KubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By(fmt.Sprintf("Create clusterset %s and clustersetbinding %s", clusterSetName, clusterSetName))
			clusterset := &clusterapiv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterSetName,
				},
			}
			_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
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
			_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), csb, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
				_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: clsName,
					},
				}
				_, err = hub.KubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			ginkgo.By(fmt.Sprintf("Create placement %s", placementName))
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

			_, err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Create(context.TODO(), placement, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.JustAfterEach(func() {
			// delete namespace
			err := hub.KubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// delete clusters created
			clusterList, err := hub.ClusterClient.ClusterV1().ManagedClusters().List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", clusterapiv1beta2.ClusterSetLabel, clusterSetName),
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			for _, cluster := range clusterList.Items {
				err = hub.ClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), cluster.Name, metav1.DeleteOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			// delete created clusterset
			err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), clusterSetName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// delete placement
			err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Delete(context.TODO(), placementName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("Create ManifestWorkReplicaSet and check the created manifestWorks", func() {
			ginkgo.By(fmt.Sprintf("Create manifestWorkReplicaSet %s", mwReplicaSetName))
			manifest := workapiv1.Manifest{}
			manifest.Object = util.NewConfigmap("default", "cm", map[string]string{"a": "b"}, nil)
			placementRef := workapiv1alpha1.LocalPlacementReference{
				Name: placementName,
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{
					Type: clusterv1alpha1.All,
					All: &clusterv1alpha1.RolloutAll{
						RolloutConfig: clusterv1alpha1.RolloutConfig{
							ProgressDeadline: "None",
						},
					},
				},
			}
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
			_, err = hub.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Create(context.TODO(), mwReplicaSet, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Check manifestWork replicaSet status is updated")
			gomega.Eventually(func() error {
				mwrSet, err := hub.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Get(context.TODO(), mwReplicaSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionTrue(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified) {
					return fmt.Errorf("manifestwork replicaset PlacementVerified condition is not true")
				}

				if mwrSet.Status.Summary.Total != numOfClusters {
					return fmt.Errorf("total number of clusters is not correct, expect %d, got %d", numOfClusters, mwrSet.Status.Summary.Total)
				}
				return nil
			}).Should(gomega.Succeed())

			ginkgo.By("Check manifestWorks are created")
			gomega.Eventually(func() error {
				manifestWorkList, err := hub.WorkClient.WorkV1().ManifestWorks("").List(context.TODO(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s.%s", mwrSetLabel, namespace, mwReplicaSetName),
				})
				if err != nil {
					return err
				}

				if len(manifestWorkList.Items) != numOfClusters {
					return fmt.Errorf("manifestworks are not created, expect %d, got %d", numOfClusters, len(manifestWorkList.Items))
				}
				return nil
			}).Should(gomega.Succeed())

			ginkgo.By("Delete manifestWorkReplicaSet")
			err = hub.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Delete(context.TODO(), mwReplicaSetName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Check manifestworks are deleted")
			gomega.Eventually(func() error {
				manifestWorkList, err := hub.WorkClient.WorkV1().ManifestWorks("").List(context.TODO(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s.%s", mwrSetLabel, namespace, mwReplicaSetName),
				})
				if err != nil {
					return err
				}

				if len(manifestWorkList.Items) != 0 {
					return fmt.Errorf("manifestworks are not deleted, expect %d, got %d", 0, len(manifestWorkList.Items))
				}
				return nil
			}).Should(gomega.Succeed())
		})

		ginkgo.It("delete ManifestWorkReplicaSet Foreground", func() {
			ginkgo.By(fmt.Sprintf("Create manifestWorkReplicaSet %s with Foreground", mwReplicaSetName))
			manifest := workapiv1.Manifest{}
			manifest.Object = util.NewConfigmap("default", "cm", map[string]string{"a": "b"}, nil)
			placementRef := workapiv1alpha1.LocalPlacementReference{
				Name: placementName,
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{
					Type: clusterv1alpha1.All,
					All: &clusterv1alpha1.RolloutAll{
						RolloutConfig: clusterv1alpha1.RolloutConfig{
							ProgressDeadline: "None",
						},
					},
				},
			}
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
					CascadeDeletionPolicy: workapiv1alpha1.Foreground,
					PlacementRefs:         []workapiv1alpha1.LocalPlacementReference{placementRef},
				},
			}
			_, err = hub.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Create(context.TODO(), mwReplicaSet, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Check manifestWork replicaSet status is updated")
			gomega.Eventually(func() error {
				mwrSet, err := hub.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Get(context.TODO(), mwReplicaSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionTrue(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified) {
					return fmt.Errorf("manifestwork replicaset PlacementVerified condition is not true")
				}

				if mwrSet.Status.Summary.Total != numOfClusters {
					return fmt.Errorf("total number of clusters is not correct, expect %d, got %d", numOfClusters, mwrSet.Status.Summary.Total)
				}
				return nil
			}).Should(gomega.Succeed())

			ginkgo.By("Check manifestWorks are created")
			manifestWorkList := &workapiv1.ManifestWorkList{}
			gomega.Eventually(func() error {
				manifestWorkList, err = hub.WorkClient.WorkV1().ManifestWorks("").List(context.TODO(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s.%s", mwrSetLabel, namespace, mwReplicaSetName),
				})
				if err != nil {
					return err
				}

				if len(manifestWorkList.Items) != numOfClusters {
					return fmt.Errorf("manifestworks are not created, expect %d, got %d", numOfClusters, len(manifestWorkList.Items))
				}
				return nil
			}).Should(gomega.Succeed())

			ginkgo.By("Update manifestWorks with a finalizer to stop deleting")
			leftWork := manifestWorkList.Items[0].DeepCopy()
			gomega.Eventually(func() error {
				leftWork.Finalizers = append(leftWork.Finalizers, "test")
				_, err = hub.WorkClient.WorkV1().ManifestWorks(leftWork.Namespace).Update(context.TODO(), leftWork, metav1.UpdateOptions{})
				if err != nil {
					return fmt.Errorf("failed to update work %v, %v", leftWork.Name, err)
				}
				return nil
			}).Should(gomega.Succeed())

			ginkgo.By("Delete manifestWorkReplicaSet")
			err = hub.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Delete(context.TODO(), mwReplicaSetName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Check manifestworks are deleted")
			gomega.Eventually(func() error {
				manifestWorkList, err := hub.WorkClient.WorkV1().ManifestWorks("").List(context.TODO(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s.%s", mwrSetLabel, namespace, mwReplicaSetName),
				})
				if err != nil {
					return err
				}

				if len(manifestWorkList.Items) != 1 {
					return fmt.Errorf("expect 1 leftWork left, got %d", len(manifestWorkList.Items))
				}

				if manifestWorkList.Items[0].DeletionTimestamp.IsZero() {
					return fmt.Errorf("expect the leftWork %s is deleting", manifestWorkList.Items[0].Name)
				}
				return nil
			}).Should(gomega.Succeed())

			ginkgo.By("Check manifestWorkReplicaSet is deleting")
			gomega.Eventually(func() error {
				workset, err := hub.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).
					Get(context.TODO(), mwReplicaSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if workset.DeletionTimestamp.IsZero() {
					return fmt.Errorf("expect the workset %s is deleting", workset.Name)
				}
				if len(workset.Finalizers) == 0 {
					return fmt.Errorf("expect the workset %s has finalizer", workset.Name)
				}
				return nil
			}).Should(gomega.Succeed())

			ginkgo.By("Remove finalizer from the manifestwork")
			copyLeftWork, err := hub.WorkClient.WorkV1().ManifestWorks(leftWork.Namespace).
				Get(context.TODO(), leftWork.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				copyLeftWork.Finalizers = []string{}
				_, err = hub.WorkClient.WorkV1().ManifestWorks(copyLeftWork.Namespace).
					Update(context.TODO(), copyLeftWork, metav1.UpdateOptions{})
				return err
			}).Should(gomega.Succeed())

			ginkgo.By("The manifestworks and ManifestWorkReplicaSet should be deleted")
			gomega.Eventually(func() error {
				manifestWorkList, err = hub.WorkClient.WorkV1().ManifestWorks("").List(context.TODO(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s.%s", mwrSetLabel, namespace, mwReplicaSetName),
				})
				if err != nil {
					return err
				}

				if len(manifestWorkList.Items) != 0 {
					return fmt.Errorf("manifestworks should be deleted")
				}

				_, err := hub.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).
					Get(context.TODO(), mwReplicaSetName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("ManifestWorkReplicaSets should be deleted")
			}).Should(gomega.Succeed())
		})
	})
})
