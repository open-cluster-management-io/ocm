package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

var _ = Describe("Addon install with install strategy (v1alpha1)", Ordered, Label("addon-install"), func() {
	var addOnName string
	var clusterNames []string
	const globalSetNamespace = "open-cluster-management-global-set"

	BeforeAll(func() {
		suffix := rand.String(6)
		addOnName = fmt.Sprintf("addon-%s", suffix)
		clusterNames = nil

		By("create namespace " + globalSetNamespace)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: globalSetNamespace,
			},
		}
		_, err := hub.KubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		By("create Placement global in " + globalSetNamespace)
		placement := &clusterv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "global",
				Namespace: globalSetNamespace,
			},
			Spec: clusterv1beta1.PlacementSpec{
				ClusterSets: []string{"global"},
				Tolerations: []clusterv1beta1.Toleration{
					{
						Key:      "cluster.open-cluster-management.io/unreachable",
						Operator: clusterv1beta1.TolerationOpEqual,
					},
					{
						Key:      "cluster.open-cluster-management.io/unavailable",
						Operator: clusterv1beta1.TolerationOpEqual,
					},
				},
				DecisionStrategy: clusterv1beta1.DecisionStrategy{
					GroupStrategy: clusterv1beta1.GroupStrategy{},
				},
				PrioritizerPolicy: clusterv1beta1.PrioritizerPolicy{
					Mode: clusterv1beta1.PrioritizerPolicyModeAdditive,
				},
			},
		}
		_, err = hub.ClusterClient.ClusterV1beta1().Placements(globalSetNamespace).Create(
			context.TODO(), placement, metav1.CreateOptions{})
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		By("create ManagedClusterSetBinding global in " + globalSetNamespace)
		binding := &clusterv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "global",
				Namespace: globalSetNamespace,
			},
			Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: "global",
			},
		}
		_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(globalSetNamespace).Create(
			context.TODO(), binding, metav1.CreateOptions{})
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		By(fmt.Sprintf("create ClusterManagementAddOn %s with install strategy", addOnName))
		cma := &addonapiv1alpha1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnName,
			},
			Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
				InstallStrategy: addonapiv1alpha1.InstallStrategy{
					Type: addonapiv1alpha1.AddonInstallStrategyPlacements,
					Placements: []addonapiv1alpha1.PlacementStrategy{
						{
							PlacementRef: addonapiv1alpha1.PlacementRef{
								Name:      "global",
								Namespace: globalSetNamespace,
							},
							RolloutStrategy: clusterv1alpha1.RolloutStrategy{
								Type: clusterv1alpha1.All,
							},
						},
					},
				},
			},
		}
		_, err = hub.AddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(
			context.TODO(), cma, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterAll(func() {
		By(fmt.Sprintf("delete ClusterManagementAddOn %s", addOnName))
		err := hub.AddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(
			context.TODO(), addOnName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		for _, clusterName := range clusterNames {
			By(fmt.Sprintf("delete ManagedCluster %s", clusterName))
			err := hub.ClusterClient.ClusterV1().ManagedClusters().Delete(
				context.TODO(), clusterName, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				Expect(err).ToNot(HaveOccurred())
			}
		}

		By("delete namespace " + globalSetNamespace)
		err = hub.KubeClient.CoreV1().Namespaces().Delete(
			context.TODO(), globalSetNamespace, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		Eventually(func() error {
			_, err := hub.KubeClient.CoreV1().Namespaces().Get(
				context.TODO(), globalSetNamespace, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("namespace " + globalSetNamespace + " still exists")
		}).Should(Succeed())
	})

	It("Should create addon without addon annotations when managed cluster has no addon annotations", func() {
		clusterName := fmt.Sprintf("e2e-addon-install-%s", rand.String(6))
		clusterNames = append(clusterNames, clusterName)

		By(fmt.Sprintf("create ManagedCluster %s with non-addon annotations", clusterName))
		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Annotations: map[string]string{
					"foo.example.com/bar": "value1",
					"baz.example.com/qux": "value2",
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err := hub.ClusterClient.ClusterV1().ManagedClusters().Create(
			context.TODO(), managedCluster, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("check ManagedClusterAddOn %s is created in cluster namespace %s", addOnName, clusterName))
		Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(
				context.TODO(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(addon.Annotations) != 0 {
				return fmt.Errorf("expected no annotations on ManagedClusterAddOn, got %v", addon.Annotations)
			}
			return nil
		}).Should(Succeed())
	})

	It("Should sync addon annotations from managed cluster to addon", func() {
		clusterName := fmt.Sprintf("e2e-addon-install-%s", rand.String(6))
		clusterNames = append(clusterNames, clusterName)

		annotations := map[string]string{
			"addon.open-cluster-management.io/hosting-cluster-name": "hosting-cluster",
			"addon.open-cluster-management.io/test-annotation":      "test-value",
		}

		By(fmt.Sprintf("create ManagedCluster %s with addon annotations", clusterName))
		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        clusterName,
				Annotations: annotations,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err := hub.ClusterClient.ClusterV1().ManagedClusters().Create(
			context.TODO(), managedCluster, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("check ManagedClusterAddOn %s is created with synced annotations", addOnName))
		Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(
				context.TODO(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			for key, expectedValue := range annotations {
				actualValue, ok := addon.Annotations[key]
				if !ok {
					return fmt.Errorf("expected annotation %s not found on ManagedClusterAddOn", key)
				}
				if actualValue != expectedValue {
					return fmt.Errorf("annotation %s: expected %q, got %q", key, expectedValue, actualValue)
				}
			}
			return nil
		}).Should(Succeed())

		By(fmt.Sprintf("update annotation on ManagedCluster %s", clusterName))
		Eventually(func() error {
			cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
				context.TODO(), clusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			cluster.Annotations["addon.open-cluster-management.io/test-annotation"] = "updated-value"
			_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(
				context.TODO(), cluster, metav1.UpdateOptions{})
			return err
		}).Should(Succeed())

		By(fmt.Sprintf("check updated annotation is synced to ManagedClusterAddOn %s", addOnName))
		Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(
				context.TODO(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			actualValue, ok := addon.Annotations["addon.open-cluster-management.io/test-annotation"]
			if !ok {
				return fmt.Errorf("expected annotation addon.open-cluster-management.io/test-annotation not found on ManagedClusterAddOn")
			}
			if actualValue != "updated-value" {
				return fmt.Errorf("annotation addon.open-cluster-management.io/test-annotation: expected %q, got %q", "updated-value", actualValue)
			}
			return nil
		}).Should(Succeed())
	})
})
