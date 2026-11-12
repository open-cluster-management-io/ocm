package registration_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	cpv1alpha2 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha2"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/hub/clusterprofile"
)

var _ = Describe("ClusterProfile", func() {
	var clusterProfileClient cpclientset.Interface
	var testNamespaces []string

	BeforeEach(func() {
		// Enable ClusterProfile feature gate
		err := features.HubMutableFeatureGate.Set(fmt.Sprintf("%s=true", ocmfeature.ClusterProfile))
		Expect(err).NotTo(HaveOccurred())

		// Create ClusterProfile client
		clusterProfileClient, err = cpclientset.NewForConfig(hubCfg)
		Expect(err).NotTo(HaveOccurred())

		// Clean up test namespaces
		testNamespaces = []string{}
	})

	AfterEach(func(ctx SpecContext) {
		// Clean up test namespaces
		for _, ns := range testNamespaces {
			err := kubeClient.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				fmt.Printf("Failed to delete namespace %s: %v\n", ns, err)
			}
		}
	})

	It("should create ClusterProfile when ManagedClusterSetBinding is created", func(ctx SpecContext) {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSet")
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
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedCluster")
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
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Update cluster status - get latest version first to avoid conflicts with other controllers
		Eventually(func() error {
			latest, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, clusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			latest.Status.Version.Kubernetes = "v1.25.0"
			_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(ctx, latest, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("Create ManagedClusterSetBinding")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for binding to become bound")
		Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Verify ClusterProfile is created")
		Eventually(func() error {
			profile, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
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
			if profile.Labels[cpv1alpha2.LabelInventoryMemberIDKey] != clusterName {
				return fmt.Errorf("unexpected inventory member ID: %s", profile.Labels[cpv1alpha2.LabelInventoryMemberIDKey])
			}

			// Verify labels
			if profile.Labels[cpv1alpha2.LabelClusterManagerKey] != clusterprofile.ClusterProfileManagerName {
				return fmt.Errorf("missing or incorrect cluster manager label")
			}
			if profile.Labels[cpv1alpha2.LabelClusterSetKey] != clusterSetName {
				return fmt.Errorf("missing or incorrect clusterset label")
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("Preserve access providers written through v1alpha1 during v1alpha2 status synchronization")
		legacyProfile, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Patch(
			ctx, clusterName, types.MergePatchType,
			[]byte(`{"status":{"accessProviders":[{"name":"kubeconfig","cluster":{"server":"https://api.example.com:6443","tls-server-name":"kubernetes","disable-compression":true}}]}}`),
			metav1.PatchOptions{}, "status")
		Expect(err).NotTo(HaveOccurred())
		_, err = clusterClient.ClusterV1().ManagedClusters().Patch(ctx, clusterName, types.MergePatchType,
			[]byte(`{"status":{"version":{"kubernetes":"v1.26.0"}}}`), metav1.PatchOptions{}, "status")
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() error {
			profile, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if profile.UID != legacyProfile.UID {
				return fmt.Errorf("ClusterProfile was recreated")
			}
			if profile.Status.Version.Kubernetes != "v1.26.0" {
				return fmt.Errorf("ClusterProfile version has not been synchronized")
			}
			if len(profile.Status.AccessProviders) != 1 {
				return fmt.Errorf("access providers were not preserved: %v", profile.Status.AccessProviders)
			}
			provider := profile.Status.AccessProviders[0]
			if provider.Name != "kubeconfig" || provider.Cluster.Server != "https://api.example.com:6443" ||
				provider.Cluster.TLSServerName != "kubernetes" || !provider.Cluster.DisableCompression {
				return fmt.Errorf("access provider was modified: %v", provider)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("Keep the synchronized profile readable through v1alpha1")
		legacyProfile, err = clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(legacyProfile.Status.Version.Kubernetes).To(Equal("v1.26.0"))
		Expect(legacyProfile.Status.AccessProviders).To(HaveLen(1))
	})

	It("should create ClusterProfiles in multiple namespaces", func(ctx SpecContext) {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace1 := fmt.Sprintf("test-ns1-%s", suffix)
		namespace2 := fmt.Sprintf("test-ns2-%s", suffix)
		testNamespaces = append(testNamespaces, namespace1, namespace2)

		By("Create test namespaces")
		for _, ns := range []string{namespace1, namespace2} {
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns,
				},
			}
			_, err := kubeClient.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		}

		By("Create ManagedClusterSet")
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
		_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedCluster")
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
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSetBinding in namespace1")
		binding1 := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace1,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace1).Create(ctx, binding1, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSetBinding in namespace2")
		binding2 := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace2,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace2).Create(ctx, binding2, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for bindings to become bound")
		for _, ns := range []string{namespace1, namespace2} {
			Eventually(func() bool {
				binding, err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(ns).Get(ctx, clusterSetName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		}

		By("Verify ClusterProfile is created in both namespaces")
		for _, ns := range []string{namespace1, namespace2} {
			Eventually(func() error {
				profile, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(ns).Get(ctx, clusterName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if profile.Namespace != ns {
					return fmt.Errorf("unexpected namespace: %s", profile.Namespace)
				}
				if profile.Labels[cpv1alpha2.LabelInventoryMemberIDKey] != clusterName {
					return fmt.Errorf("unexpected inventory member ID: %s", profile.Labels[cpv1alpha2.LabelInventoryMemberIDKey])
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		}
	})

	It("should synchronize inventory member IDs across namespaces", func(ctx SpecContext) {
		suffix := rand.String(6)
		clusterName := "cluster-" + suffix
		clusterSetName := "clusterset-" + suffix
		namespaces := []string{"inventory1-" + suffix, "inventory2-" + suffix}
		testNamespaces = append(testNamespaces, namespaces...)

		_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{Name: clusterSetName},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{SelectorType: clusterv1beta2.ExclusiveClusterSetLabel},
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(ctx SpecContext) {
			Expect(clusterClient.ClusterV1beta2().ManagedClusterSets().Delete(ctx, clusterSetName, metav1.DeleteOptions{})).To(Succeed())
		})
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					clusterv1beta2.ClusterSetLabel:       clusterSetName,
					cpv1alpha2.LabelInventoryMemberIDKey: "Prod_JP.cluster-01",
				},
			},
			Spec: clusterv1.ManagedClusterSpec{HubAcceptsClient: true},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(ctx SpecContext) {
			Expect(clusterClient.ClusterV1().ManagedClusters().Delete(ctx, clusterName, metav1.DeleteOptions{})).To(Succeed())
		})
		for _, namespace := range namespaces {
			_, err = kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: namespace},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, &clusterv1beta2.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{Name: clusterSetName, Namespace: namespace},
				Spec:       clusterv1beta2.ManagedClusterSetBindingSpec{ClusterSet: clusterSetName},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		}
		expectMemberID := func(memberID string) {
			Eventually(func() error {
				for _, namespace := range namespaces {
					profile, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					if got := profile.Labels[cpv1alpha2.LabelInventoryMemberIDKey]; got != memberID {
						return fmt.Errorf("profile %s/%s: expected inventory member ID %q, got %q", namespace, clusterName, memberID, got)
					}
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		}
		By("Copy the coordinated ID to both inventories")
		expectMemberID("Prod_JP.cluster-01")

		By("Restore a missing ID on an existing profile and preserve custom metadata")
		_, err = clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespaces[0]).Patch(ctx, clusterName, types.MergePatchType,
			[]byte(fmt.Sprintf(`{"metadata":{"labels":{%q:null,"custom-label":"keep-me"},"annotations":{"custom-annotation":"keep-me"}}}`,
				cpv1alpha2.LabelInventoryMemberIDKey)), metav1.PatchOptions{})
		Expect(err).NotTo(HaveOccurred())
		expectMemberID("Prod_JP.cluster-01")

		for _, tc := range []struct {
			name       string
			labelJSON  string
			expectedID string
		}{
			{name: "Change the coordinated ID", labelJSON: `"Prod_JP.cluster-02"`, expectedID: "Prod_JP.cluster-02"},
			{name: "Fall back to the cluster name when the label is removed", labelJSON: `null`, expectedID: clusterName},
			{name: "Restore the coordinated ID", labelJSON: `"Prod_JP.cluster-03"`, expectedID: "Prod_JP.cluster-03"},
		} {
			By(tc.name)
			patch := fmt.Sprintf(`{"metadata":{"labels":{%q:%s}}}`, cpv1alpha2.LabelInventoryMemberIDKey, tc.labelJSON)
			_, err = clusterClient.ClusterV1().ManagedClusters().Patch(ctx, clusterName, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
			Expect(err).NotTo(HaveOccurred())
			expectMemberID(tc.expectedID)
		}
		profile, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespaces[0]).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(profile.Labels["custom-label"]).To(Equal("keep-me"))
		Expect(profile.Annotations["custom-annotation"]).To(Equal("keep-me"))
	})

	It("should delete ClusterProfile when ManagedClusterSetBinding is deleted", func(ctx SpecContext) {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSet")
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
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedCluster")
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
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSetBinding")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for binding to become bound")
		Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Verify ClusterProfile is created")
		Eventually(func() error {
			_, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("Delete ManagedClusterSetBinding")
		err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Delete(ctx, clusterSetName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Verify ClusterProfile is deleted")
		Eventually(func() bool {
			_, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	})

	It("should delete ClusterProfile when ManagedCluster is deleted", func(ctx SpecContext) {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSet")
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
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedCluster")
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
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSetBinding")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for binding to become bound")
		Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Verify ClusterProfile is created")
		Eventually(func() error {
			_, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("Delete ManagedCluster")
		err = clusterClient.ClusterV1().ManagedClusters().Delete(ctx, clusterName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Verify ClusterProfile is deleted")
		Eventually(func() bool {
			_, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	})

	It("should deduplicate ClusterProfiles when cluster is in multiple sets with bindings in same namespace", func(ctx SpecContext) {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSet1Name := fmt.Sprintf("clusterset1-%s", suffix)
		clusterSet2Name := fmt.Sprintf("clusterset2-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create first ManagedClusterSet")
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
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterSet1, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create second ManagedClusterSet with label selector")
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
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterSet2, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedCluster that belongs to both sets")
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
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSetBinding for first set")
		binding1 := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSet1Name,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSet1Name,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding1, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSetBinding for second set")
		binding2 := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSet2Name,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSet2Name,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding2, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for both bindings to become bound")
		for _, bindingName := range []string{clusterSet1Name, clusterSet2Name} {
			Eventually(func() bool {
				binding, err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, bindingName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		}

		By("Verify only ONE ClusterProfile is created (deduplication)")
		Eventually(func() error {
			profile, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Verify it's the same cluster
			if profile.Name != clusterName {
				return fmt.Errorf("unexpected profile name: %s", profile.Name)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("Verify there is exactly one ClusterProfile for the cluster in the namespace")
		profiles, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		clusterProfileCount := 0
		for _, profile := range profiles.Items {
			if profile.Name == clusterName {
				clusterProfileCount++
			}
		}
		Expect(clusterProfileCount).To(Equal(1), "Expected exactly one ClusterProfile, but found %d", clusterProfileCount)
	})

	It("should sync ClusterProfile status from ManagedCluster", func(ctx SpecContext) {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSet")
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
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedCluster with status")
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
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Update cluster status - get latest version first to avoid conflicts with other controllers
		Eventually(func() error {
			latest, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, clusterName, metav1.GetOptions{})
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
			_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(ctx, latest, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("Create ManagedClusterSetBinding")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for binding to become bound")
		Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Verify ClusterProfile status is synced from ManagedCluster")
		Eventually(func() error {
			profile, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
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
			availableCond := meta.FindStatusCondition(profile.Status.Conditions, cpv1alpha2.ClusterConditionControlPlaneHealthy)
			if availableCond == nil || availableCond.Status != metav1.ConditionTrue {
				return fmt.Errorf("expected ControlPlaneHealthy condition to be True")
			}

			joinedCond := meta.FindStatusCondition(profile.Status.Conditions, "Joined")
			if joinedCond == nil || joinedCond.Status != metav1.ConditionTrue {
				return fmt.Errorf("expected Joined condition to be True")
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	})

	It("should create ClusterProfiles with global clusterset (empty LabelSelector)", func(ctx SpecContext) {
		suffix := rand.String(6)
		cluster1Name := fmt.Sprintf("cluster1-%s", suffix)
		cluster2Name := fmt.Sprintf("cluster2-%s", suffix)
		clusterSetName := fmt.Sprintf("global-clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create global ManagedClusterSet with empty LabelSelector")
		globalClusterSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType:  clusterv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{}, // empty selector matches ALL clusters
				},
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, globalClusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create first ManagedCluster with labels")
		cluster1 := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: cluster1Name,
				Labels: map[string]string{
					"environment": "production",
					"region":      "us-west",
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster1, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create second ManagedCluster with no labels")
		cluster2 := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   cluster2Name,
				Labels: map[string]string{}, // no labels
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster2, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSetBinding for global clusterset")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for binding to become bound")
		Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Verify ClusterProfile is created for both clusters (global selector matches all)")
		for _, clusterName := range []string{cluster1Name, cluster2Name} {
			Eventually(func() error {
				profile, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if profile.Name != clusterName {
					return fmt.Errorf("unexpected profile name: %s", profile.Name)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		}
	})

	It("should create ClusterProfiles with LabelSelector MatchExpressions", func(ctx SpecContext) {
		suffix := rand.String(6)
		prodClusterName := fmt.Sprintf("prod-cluster-%s", suffix)
		devClusterName := fmt.Sprintf("dev-cluster-%s", suffix)
		testClusterName := fmt.Sprintf("test-cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("env-clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSet with MatchExpressions")
		clusterSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "environment",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"production", "development"},
							},
						},
					},
				},
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create production cluster")
		prodCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: prodClusterName,
				Labels: map[string]string{
					"environment": "production",
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, prodCluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create development cluster")
		devCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: devClusterName,
				Labels: map[string]string{
					"environment": "development",
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, devCluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create test cluster (should NOT match)")
		testCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: testClusterName,
				Labels: map[string]string{
					"environment": "testing", // not in [production, development]
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, testCluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSetBinding")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for binding to become bound")
		Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Verify ClusterProfiles are created for matching clusters only")
		for _, clusterName := range []string{prodClusterName, devClusterName} {
			Eventually(func() error {
				_, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		}

		By("Verify ClusterProfile is NOT created for test cluster")
		Consistently(func() bool {
			_, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, testClusterName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, "10s", "1s").Should(BeTrue())
	})

	It("should deduplicate ClusterProfiles with multiple overlapping LabelSelectors", func(ctx SpecContext) {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		prodSetName := fmt.Sprintf("prod-set-%s", suffix)
		usWestSetName := fmt.Sprintf("uswest-set-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create production LabelSelector clusterset")
		prodSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: prodSetName,
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
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, prodSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create us-west LabelSelector clusterset")
		usWestSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: usWestSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"region": "us-west",
						},
					},
				},
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, usWestSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create cluster that matches BOTH label selectors")
		cluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					"environment": "production",
					"region":      "us-west",
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create binding for production clusterset")
		prodBinding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      prodSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: prodSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, prodBinding, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create binding for us-west clusterset")
		usWestBinding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      usWestSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: usWestSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, usWestBinding, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for both bindings to become bound")
		for _, bindingName := range []string{prodSetName, usWestSetName} {
			Eventually(func() bool {
				binding, err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, bindingName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		}

		By("Verify only ONE ClusterProfile is created (deduplication across LabelSelectors)")
		Eventually(func() error {
			_, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("Verify there is exactly one ClusterProfile")
		profiles, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		clusterProfileCount := 0
		for _, profile := range profiles.Items {
			if profile.Name == clusterName {
				clusterProfileCount++
			}
		}
		Expect(clusterProfileCount).To(Equal(1), "Expected exactly one ClusterProfile, but found %d", clusterProfileCount)
	})

	It("should sync clusterset label from ManagedCluster to ClusterProfile", func(ctx SpecContext) {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSet")
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
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedCluster with clusterset label")
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
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSetBinding")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for binding to become bound")
		Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Verify ClusterProfile has correct clusterset label")
		Eventually(func() error {
			profile, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Verify clusterset label is synced
			if profile.Labels[cpv1alpha2.LabelClusterSetKey] != clusterSetName {
				return fmt.Errorf("expected clusterset label %s, got %s", clusterSetName, profile.Labels[cpv1alpha2.LabelClusterSetKey])
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	})

	It("should NOT create ClusterProfile for cluster in deletion state", func(ctx SpecContext) {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSet")
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
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedCluster with finalizer")
		cluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					clusterv1beta2.ClusterSetLabel: clusterSetName,
				},
				Finalizers: []string{"test-finalizer"}, // Add finalizer to keep cluster in deletion state
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Delete ManagedCluster (put in deletion state)")
		err = clusterClient.ClusterV1().ManagedClusters().Delete(ctx, clusterName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Verify cluster has DeletionTimestamp (stays in deletion due to finalizer)")
		Eventually(func() bool {
			cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, clusterName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return cluster.DeletionTimestamp != nil
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Create ManagedClusterSetBinding AFTER cluster is in deletion")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for binding to become bound")
		Eventually(func() bool {
			binding, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, clusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Verify ClusterProfile is NOT created for cluster in deletion")
		Consistently(func() bool {
			_, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, "15s", "2s").Should(BeTrue())

		By("Clean up: Remove finalizer to allow cluster deletion")
		Eventually(func() error {
			cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, clusterName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil // Already deleted
				}
				return err
			}
			cluster.Finalizers = []string{}
			_, err = clusterClient.ClusterV1().ManagedClusters().Update(ctx, cluster, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	})

	It("should NOT create ClusterProfile when binding is not bound", func(ctx SpecContext) {
		suffix := rand.String(6)
		clusterName := fmt.Sprintf("cluster-%s", suffix)
		clusterSetName := fmt.Sprintf("clusterset-%s", suffix)
		namespace := fmt.Sprintf("test-ns-%s", suffix)
		testNamespaces = append(testNamespaces, namespace)

		By("Create test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// NOTE: intentionally do NOT create the ManagedClusterSet. Without it, the
		// ManagedClusterSetBinding controller keeps the binding unbound
		// (condition Bound=False, reason ClusterSetNotFound), which deterministically
		// exercises the "binding is not bound" path. If the clusterset existed the
		// binding would become bound and a ClusterProfile would (correctly) be
		// created, which previously made this test race against that transition.

		By("Create ManagedCluster labeled for the absent clusterset")
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
		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create ManagedClusterSetBinding referencing the absent clusterset")
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Verify the binding is reported as not bound")
		Eventually(func(g Gomega) {
			b, err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, clusterSetName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType)).To(BeFalse())
		}, "10s", "1s").Should(Succeed())

		By("Verify ClusterProfile is NOT created while the binding is not bound")
		Consistently(func(g Gomega) {
			_, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(namespace).Get(ctx, clusterName, metav1.GetOptions{})
			g.Expect(errors.IsNotFound(err)).To(BeTrue(), "ClusterProfile should not be created while the binding is not bound")
		}, "10s", "2s").Should(Succeed())
	})
})
