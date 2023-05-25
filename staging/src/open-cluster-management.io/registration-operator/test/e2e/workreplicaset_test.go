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
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

const (
	mwrSetLabel = "work.open-cluster-management.io/manifestworkreplicaset"
)

var _ = Describe("Test ManifestWorkReplicaSet", func() {
	var suffix string
	var clusterNamePrefix string
	var namespace string
	var placementName string
	var clusterSetName string
	var mwReplicaSetName string

	BeforeEach(func() {
		// Enable manifestWorkReplicaSet feature if not enabled
		if !t.hubWorkControllerEnabled {
			Eventually(func() error {
				clusterManager, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), t.clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if clusterManager.Spec.WorkConfiguration == nil {
					clusterManager.Spec.WorkConfiguration = &operatorapiv1.WorkConfiguration{}
				}

				if len(clusterManager.Spec.WorkConfiguration.FeatureGates) == 0 {
					clusterManager.Spec.WorkConfiguration.FeatureGates = make([]operatorapiv1.FeatureGate, 0)
				}

				featureGate := operatorapiv1.FeatureGate{
					Feature: "ManifestWorkReplicaSet",
					Mode:    operatorapiv1.FeatureGateModeTypeEnable,
				}

				clusterManager.Spec.WorkConfiguration.FeatureGates = append(clusterManager.Spec.WorkConfiguration.FeatureGates, featureGate)
				_, err = t.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), clusterManager, metav1.UpdateOptions{})
				if err != nil {
					return err
				}
				return nil
			}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

			// the work controller deployment should be running
			Eventually(func() error {
				hubWorkControllerDeployment, err := t.KubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
					Get(context.TODO(), t.hubWorkControllerDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				replicas := *hubWorkControllerDeployment.Spec.Replicas
				readyReplicas := hubWorkControllerDeployment.Status.ReadyReplicas
				if readyReplicas != replicas {
					return fmt.Errorf("deployment %s should have %d but got %d ready replicas", t.hubWorkControllerDeployment, replicas, readyReplicas)
				}
				return nil
			}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(BeNil())

			// feature gate status should be valid
			Eventually(func() error {
				clusterManager, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), t.clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if meta.IsStatusConditionFalse(clusterManager.Status.Conditions, "Applied") {
					return fmt.Errorf("components of cluster manager are not all applied")
				}
				if meta.IsStatusConditionFalse(clusterManager.Status.Conditions, "ValidFeatureGates") {
					return fmt.Errorf("feature gates are not all valid")
				}
				return nil
			}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(BeNil())
		}

		suffix = rand.String(6)
		clusterNamePrefix = fmt.Sprintf("cls-%s", suffix)
		clusterSetName = fmt.Sprintf("clusterset-%s", suffix)
		namespace = fmt.Sprintf("ns-%s", suffix)
		placementName = fmt.Sprintf("placement-%s", suffix)
		mwReplicaSetName = fmt.Sprintf("mwrset-%s", suffix)

		By("Create namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := t.KubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// delete namespace
		err := t.KubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		// delete clusters created
		clusterList, err := t.ClusterClient.ClusterV1().ManagedClusters().List(context.Background(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", clusterapiv1beta1.ClusterSetLabel, clusterSetName),
		})
		Expect(err).ToNot(HaveOccurred())
		for _, cluster := range clusterList.Items {
			err = t.ClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), cluster.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}

		// delete created clusterset
		err = t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), clusterSetName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		// delete placement
		err = t.ClusterClient.ClusterV1beta1().Placements(namespace).Delete(context.TODO(), placementName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		if !t.hubWorkControllerEnabled {
			Eventually(func() error {
				clusterManager, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), t.clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for indx, fg := range clusterManager.Spec.WorkConfiguration.FeatureGates {
					if fg.Feature == "ManifestWorkReplicaSet" {
						clusterManager.Spec.WorkConfiguration.FeatureGates[indx].Mode = operatorapiv1.FeatureGateModeTypeDisable
						break
					}
				}
				_, err = t.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), clusterManager, metav1.UpdateOptions{})
				return err
			}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
		}
	})

	It("Create ManifestWorkReplicaSet and check the created manifestWorks", func() {
		By("Create clusterset and clustersetbinding")
		clusterset := &clusterapiv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
		}
		_, err := t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

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
		Expect(err).ToNot(HaveOccurred())

		numOfClusters := 3
		By(fmt.Sprintf("Create %d clusters", numOfClusters))
		for i := 0; i < numOfClusters; i++ {
			clsName := fmt.Sprintf("%s-%d", clusterNamePrefix, i)
			cluster := &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clsName,
					Labels: map[string]string{
						clusterapiv1beta1.ClusterSetLabel: clusterSetName,
					},
				},
			}
			_, err = t.ClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: clsName,
				},
			}
			_, err = t.KubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		}

		By("Create placement")
		placement := &clusterapiv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
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

		_, err = t.ClusterClient.ClusterV1beta1().Placements(namespace).Create(context.TODO(), placement, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Create manifestWorkReplicaSet")
		manifest := workapiv1.Manifest{}
		manifest.Object = newConfigmap("default", "cm", map[string]string{"a": "b"})
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
		_, err = t.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Create(context.TODO(), mwReplicaSet, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Check manifestWork replicaSet status is updated")
		Eventually(func() bool {
			mwrSet, err := t.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Get(context.TODO(), mwReplicaSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}

			if !meta.IsStatusConditionTrue(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified) {
				return false
			}

			return int(mwrSet.Status.Summary.Total) == numOfClusters
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(BeTrue())

		By("Check manifestWorks are created")
		Eventually(func() bool {
			manifestWorkList, err := t.WorkClient.WorkV1().ManifestWorks("").List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s.%s", mwrSetLabel, namespace, mwReplicaSetName),
			})
			if err != nil {
				return false
			}

			return len(manifestWorkList.Items) == numOfClusters
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(BeTrue())

		By("Delete manifestWorkReplicaSet")
		err = t.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Delete(context.TODO(), mwReplicaSetName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Check manifestworks are deleted")
		Eventually(func() bool {
			manifestWorkList, err := t.WorkClient.WorkV1().ManifestWorks("").List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s.%s", mwrSetLabel, namespace, mwReplicaSetName),
			})
			if err != nil {
				return false
			}

			return len(manifestWorkList.Items) == 0
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(BeTrue())
	})
})
