package registration_test

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
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/hub/clusterprofile"
)

var _ = ginkgo.Describe("ClusterProfile", func() {
	var clusterProfileClient cpclientset.Interface
	var testNamespaces []string

	ginkgo.BeforeEach(func() {
		// Enable ClusterProfile feature gate
		err := features.HubMutableFeatureGate.Set(fmt.Sprintf("%s=true", ocmfeature.ClusterProfile))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Create ClusterProfile client
		clusterProfileClient, err = cpclientset.NewForConfig(hubCfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Clean up test namespaces
		testNamespaces = []string{}
	})

	ginkgo.AfterEach(func() {
		// Clean up test namespaces
		for _, ns := range testNamespaces {
			err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				fmt.Printf("Failed to delete namespace %s: %v\n", ns, err)
			}
		}
	})

	ginkgo.It("should create ClusterProfile when ManagedClusterSetBinding is created", func() {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		ginkgo.By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedClusterSet")
		clusterSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
				},
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedCluster")
		cluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					clusterv1beta2.ClusterSetLabel: clusterSetName,
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
			Status: clusterv1.ManagedClusterStatus{
				Version: clusterv1.ManagedClusterVersion{
					Kubernetes: "v1.25.0",
				},
				ClusterClaims: []clusterv1.ManagedClusterClaim{
					{Name: "platform", Value: "aws"},
				},
				Conditions: []metav1.Condition{
					{
						Type:   clusterv1.ManagedClusterConditionAvailable,
						Status: metav1.ConditionTrue,
						Reason: "ManagedClusterAvailable",
					},
				},
			},
		}
		cluster, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Update cluster status - get latest version first to avoid conflicts with other controllers
		gomega.Eventually(func() error {
			latest, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), clusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			latest.Status.Version.Kubernetes = "v1.25.0"
			_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(context.Background(), latest, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		ginkgo.By("Create ManagedClusterSetBinding")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), binding, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Wait for binding to become bound")
		gomega.Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.Background(), clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Verify ClusterProfile is created")
		gomega.Eventually(func() error {
			profile, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(context.Background(), clusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Verify profile spec
			if profile.Spec.ClusterManager.Name != clusterprofile.ClusterProfileManagerName {
				return fmt.Errorf("unexpected cluster manager name: %s", profile.Spec.ClusterManager.Name)
			}
			if profile.Spec.DisplayName != clusterName {
				return fmt.Errorf("unexpected display name: %s", profile.Spec.DisplayName)
			}

			// Verify labels
			if profile.Labels[cpv1alpha1.LabelClusterManagerKey] != clusterprofile.ClusterProfileManagerName {
				return fmt.Errorf("missing or incorrect cluster manager label")
			}
			if profile.Labels[cpv1alpha1.LabelClusterSetKey] != clusterSetName {
				return fmt.Errorf("missing or incorrect clusterset label")
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
	})

	ginkgo.It("should create ClusterProfiles in multiple namespaces", func() {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace1 := fmt.Sprintf("test-ns1-%s", suffix)
		namespace2 := fmt.Sprintf("test-ns2-%s", suffix)
		testNamespaces = append(testNamespaces, namespace1, namespace2)

		ginkgo.By("Create test namespaces")
		for _, ns := range []string{namespace1, namespace2} {
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns,
				},
			}
			_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), namespace, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		ginkgo.By("Create ManagedClusterSet")
		clusterSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
				},
			},
		}
		_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedCluster")
		cluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					clusterv1beta2.ClusterSetLabel: clusterSetName,
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedClusterSetBinding in namespace1")
		binding1 := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace1,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace1).Create(context.Background(), binding1, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedClusterSetBinding in namespace2")
		binding2 := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace2,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace2).Create(context.Background(), binding2, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Wait for bindings to become bound")
		for _, ns := range []string{namespace1, namespace2} {
			gomega.Eventually(func() bool {
				binding, err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(ns).Get(context.Background(), clusterSetName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		}

		ginkgo.By("Verify ClusterProfile is created in both namespaces")
		for _, ns := range []string{namespace1, namespace2} {
			gomega.Eventually(func() error {
				profile, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(ns).Get(context.Background(), clusterName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if profile.Namespace != ns {
					return fmt.Errorf("unexpected namespace: %s", profile.Namespace)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		}
	})

	ginkgo.It("should delete ClusterProfile when ManagedClusterSetBinding is deleted", func() {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		ginkgo.By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedClusterSet")
		clusterSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
				},
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedCluster")
		cluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					clusterv1beta2.ClusterSetLabel: clusterSetName,
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedClusterSetBinding")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), binding, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Wait for binding to become bound")
		gomega.Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.Background(), clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Verify ClusterProfile is created")
		gomega.Eventually(func() error {
			_, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(context.Background(), clusterName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		ginkgo.By("Delete ManagedClusterSetBinding")
		err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Delete(context.Background(), clusterSetName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Verify ClusterProfile is deleted")
		gomega.Eventually(func() bool {
			_, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(context.Background(), clusterName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	})

	ginkgo.It("should delete ClusterProfile when ManagedCluster is deleted", func() {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		ginkgo.By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedClusterSet")
		clusterSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
				},
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedCluster")
		cluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					clusterv1beta2.ClusterSetLabel: clusterSetName,
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedClusterSetBinding")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), binding, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Wait for binding to become bound")
		gomega.Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.Background(), clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Verify ClusterProfile is created")
		gomega.Eventually(func() error {
			_, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(context.Background(), clusterName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		ginkgo.By("Delete ManagedCluster")
		err = clusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), clusterName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Verify ClusterProfile is deleted")
		gomega.Eventually(func() bool {
			_, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(context.Background(), clusterName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	})

	ginkgo.It("should deduplicate ClusterProfiles when cluster is in multiple sets with bindings in same namespace", func() {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSet1Name := fmt.Sprintf("clusterset1-%s", suffix)
		clusterSet2Name := fmt.Sprintf("clusterset2-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		ginkgo.By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create first ManagedClusterSet")
		clusterSet1 := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSet1Name,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
				},
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterSet1, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create second ManagedClusterSet with label selector")
		clusterSet2 := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSet2Name,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "production",
						},
					},
				},
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterSet2, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedCluster that belongs to both sets")
		cluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					clusterv1beta2.ClusterSetLabel: clusterSet1Name,
					"environment":                  "production", // Also selected by clusterSet2
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedClusterSetBinding for first set")
		binding1 := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSet1Name,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSet1Name,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), binding1, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedClusterSetBinding for second set")
		binding2 := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSet2Name,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSet2Name,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), binding2, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Wait for both bindings to become bound")
		for _, bindingName := range []string{clusterSet1Name, clusterSet2Name} {
			gomega.Eventually(func() bool {
				binding, err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.Background(), bindingName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		}

		ginkgo.By("Verify only ONE ClusterProfile is created (deduplication)")
		gomega.Eventually(func() error {
			profile, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(context.Background(), clusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Verify it's the same cluster
			if profile.Name != clusterName {
				return fmt.Errorf("unexpected profile name: %s", profile.Name)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		ginkgo.By("Verify there is exactly one ClusterProfile for the cluster in the namespace")
		profiles, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).List(context.Background(), metav1.ListOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		clusterProfileCount := 0
		for _, profile := range profiles.Items {
			if profile.Name == clusterName {
				clusterProfileCount++
			}
		}
		gomega.Expect(clusterProfileCount).To(gomega.Equal(1), "Expected exactly one ClusterProfile, but found %d", clusterProfileCount)
	})

	ginkgo.It("should sync ClusterProfile status from ManagedCluster", func() {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		ginkgo.By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedClusterSet")
		clusterSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
				},
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Create ManagedCluster with status")
		cluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					clusterv1beta2.ClusterSetLabel: clusterSetName,
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		cluster, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Update cluster status - get latest version first to avoid conflicts with other controllers
		gomega.Eventually(func() error {
			latest, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), clusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			latest.Status = clusterv1.ManagedClusterStatus{
				Version: clusterv1.ManagedClusterVersion{
					Kubernetes: "v1.26.0",
				},
				ClusterClaims: []clusterv1.ManagedClusterClaim{
					{Name: "platform", Value: "gcp"},
					{Name: "region", Value: "us-west1"},
				},
				Conditions: []metav1.Condition{
					{
						Type:               clusterv1.ManagedClusterConditionAvailable,
						Status:             metav1.ConditionTrue,
						Reason:             "ManagedClusterAvailable",
						Message:            "Cluster is available",
						LastTransitionTime: metav1.Now(),
					},
					{
						Type:               clusterv1.ManagedClusterConditionJoined,
						Status:             metav1.ConditionTrue,
						Reason:             "ManagedClusterJoined",
						Message:            "Cluster is joined",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(context.Background(), latest, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		ginkgo.By("Create ManagedClusterSetBinding")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), binding, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Wait for binding to become bound")
		gomega.Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.Background(), clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Verify ClusterProfile status is synced from ManagedCluster")
		gomega.Eventually(func() error {
			profile, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(context.Background(), clusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Verify version
			if profile.Status.Version.Kubernetes != "v1.26.0" {
				return fmt.Errorf("unexpected kubernetes version: %s", profile.Status.Version.Kubernetes)
			}

			// Verify properties (cluster claims)
			if len(profile.Status.Properties) != 2 {
				return fmt.Errorf("expected 2 properties but got %d", len(profile.Status.Properties))
			}

			foundPlatform := false
			foundRegion := false
			for _, prop := range profile.Status.Properties {
				if prop.Name == "platform" && prop.Value == "gcp" {
					foundPlatform = true
				}
				if prop.Name == "region" && prop.Value == "us-west1" {
					foundRegion = true
				}
			}
			if !foundPlatform || !foundRegion {
				return fmt.Errorf("expected properties not found")
			}

			// Verify conditions
			availableCond := meta.FindStatusCondition(profile.Status.Conditions, cpv1alpha1.ClusterConditionControlPlaneHealthy)
			if availableCond == nil || availableCond.Status != metav1.ConditionTrue {
				return fmt.Errorf("expected ControlPlaneHealthy condition to be True")
			}

			joinedCond := meta.FindStatusCondition(profile.Status.Conditions, "Joined")
			if joinedCond == nil || joinedCond.Status != metav1.ConditionTrue {
				return fmt.Errorf("expected Joined condition to be True")
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
	})
})
