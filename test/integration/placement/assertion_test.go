package placement

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/test/integration/util"
)

// assert placement
func assertCreatingPlacement(ctx context.Context, placement *clusterapiv1beta1.Placement) *clusterapiv1beta1.Placement {
	By("Create placement")
	newplacement, err := clusterClient.ClusterV1beta1().Placements(placement.Namespace).Create(ctx, placement, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	return newplacement
}

func assertCreatingPlacementWithDecision(ctx context.Context, placement *clusterapiv1beta1.Placement,
	numberOfDecisionClusters, numberOfPlacementDecisions int) {
	newplacement := assertCreatingPlacement(ctx, placement)
	assertPlacementDecisionCreated(ctx, newplacement)
	assertPlacementDecisionNumbers(ctx, newplacement.Name, newplacement.Namespace, numberOfDecisionClusters, numberOfPlacementDecisions)
	if placement.Spec.NumberOfClusters != nil {
		assertPlacementConditionSatisfied(ctx,
			newplacement.Name, newplacement.Namespace, numberOfDecisionClusters,
			numberOfDecisionClusters == int(*placement.Spec.NumberOfClusters))
	}
}

func assertPatchingPlacementSpec(ctx context.Context, newPlacement *clusterapiv1beta1.Placement) {
	By("Patching placement spec")
	placementPatcher := patcher.NewPatcher[
		*clusterapiv1beta1.Placement, clusterapiv1beta1.PlacementSpec, clusterapiv1beta1.PlacementStatus](
		clusterClient.ClusterV1beta1().Placements(newPlacement.Namespace))

	Eventually(func() error {
		oldPlacement, err := clusterClient.ClusterV1beta1().Placements(newPlacement.Namespace).Get(
			ctx, newPlacement.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		_, err = placementPatcher.PatchSpec(ctx, newPlacement, newPlacement.Spec, oldPlacement.Spec)
		return err
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func assertPlacementDeleted(ctx context.Context, placementName, namespace string) {
	By("Check if placement is gone")
	Eventually(func() bool {
		_, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(ctx, placementName, metav1.GetOptions{})
		if err == nil {
			return false
		}
		return errors.IsNotFound(err)
	}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
}

func assertPlacementStatusDecisionGroups(ctx context.Context, placementName, namespace string, decisionGroupStatus []clusterapiv1beta1.DecisionGroupStatus) {
	By("Check the group status of placement")
	Eventually(func() bool {
		placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(ctx, placementName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		By(fmt.Sprintf("actual decision groups %v", placement.Status.DecisionGroups))
		return reflect.DeepEqual(placement.Status.DecisionGroups, decisionGroupStatus)
	}, eventuallyTimeout*2, eventuallyInterval).Should(BeTrue())
}

func assertPlacementConditionSatisfied(ctx context.Context, placementName, namespace string, numOfSelectedClusters int, satisfied bool) {
	By("Check the condition PlacementSatisfied of placement")
	Eventually(func() bool {
		placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(ctx, placementName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		if satisfied && !util.HasCondition(
			placement.Status.Conditions,
			clusterapiv1beta1.PlacementConditionSatisfied,
			"AllDecisionsScheduled",
			metav1.ConditionTrue,
		) {
			return false
		}
		if !satisfied && !util.HasCondition(
			placement.Status.Conditions,
			clusterapiv1beta1.PlacementConditionSatisfied,
			"NotAllDecisionsScheduled",
			metav1.ConditionFalse,
		) {
			return false
		}
		return placement.Status.NumberOfSelectedClusters == int32(numOfSelectedClusters) //nolint:gosec
	}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
}

func assertPlacementConditionMisconfigured(ctx context.Context, placementName, namespace string, misConfigured bool) {
	By("Check the condition PlacementMisconfigured of placement")
	Eventually(func() bool {
		placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(ctx, placementName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		if !misConfigured && !util.HasCondition(
			placement.Status.Conditions,
			clusterapiv1beta1.PlacementConditionMisconfigured,
			"Succeedconfigured",
			metav1.ConditionFalse,
		) {
			return false
		}
		if misConfigured && !util.HasCondition(
			placement.Status.Conditions,
			clusterapiv1beta1.PlacementConditionMisconfigured,
			"Misconfigured",
			metav1.ConditionTrue,
		) {
			return false
		}
		return true
	}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
}

// assert placement decision
func assertCreatingPlacementDecision(ctx context.Context, name, namespace string, clusterNames []string) {
	By(fmt.Sprintf("Create placementdecision %s", name))
	placementDecision := &clusterapiv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				placementLabel: name,
			},
		},
	}
	placementDecision, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).Create(
		ctx, placementDecision, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	var clusterDecisions []clusterapiv1beta1.ClusterDecision
	for _, clusterName := range clusterNames {
		clusterDecisions = append(clusterDecisions, clusterapiv1beta1.ClusterDecision{
			ClusterName: clusterName,
		})
	}

	placementDecision.Status.Decisions = clusterDecisions
	_, err = clusterClient.ClusterV1beta1().PlacementDecisions(namespace).UpdateStatus(
		ctx, placementDecision, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())
}

func assertPlacementDecisionCreated(ctx context.Context, placement *clusterapiv1beta1.Placement) {
	By("Check if placementdecision is created")
	Eventually(func() bool {
		pdl, err := clusterClient.ClusterV1beta1().PlacementDecisions(placement.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: placementLabel + "=" + placement.Name,
		})
		if err != nil {
			return false
		}
		if len(pdl.Items) == 0 {
			return false
		}
		for _, pd := range pdl.Items {
			objectMeta := pd.ObjectMeta
			if controlled := metav1.IsControlledBy(&objectMeta, placement); !controlled {
				return false
			}
		}
		return true
	}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
}

func assertPlacementDecisionNumbers(ctx context.Context, placementName, namespace string, desiredNumOfDecisionClusters, desiredNumOfDecisions int) {
	By("Check the number of decisions in placementdecisions")
	Eventually(func() bool {
		pdl, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: placementLabel + "=" + placementName,
		})
		if err != nil {
			return false
		}
		if len(pdl.Items) != desiredNumOfDecisions {
			return false
		}
		actualNumOfDecisionClusters := 0
		for _, pd := range pdl.Items {
			actualNumOfDecisionClusters += len(pd.Status.Decisions)
		}
		return actualNumOfDecisionClusters == desiredNumOfDecisionClusters
	}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
}

func assertPlacementDecisionClusterNames(ctx context.Context, placementName, namespace string, desiredClusters []string) {
	By(fmt.Sprintf("Check the cluster names of placementdecisions %s", placementName))
	Eventually(func() bool {
		pdl, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: placementLabel + "=" + placementName,
		})
		if err != nil {
			return false
		}
		actualClusters := sets.NewString()
		desiredClusters := sets.NewString(desiredClusters...)
		for _, pd := range pdl.Items {
			for _, d := range pd.Status.Decisions {
				actualClusters.Insert(d.ClusterName)
			}
		}

		if actualClusters.Equal(desiredClusters) {
			return true
		}
		By(fmt.Sprintf("Expect %v, but got %v", desiredClusters.List(), actualClusters.List()))
		return false
	}, eventuallyTimeout*2, eventuallyInterval).Should(BeTrue())
}

// assert clusterset
func assertCreatingClusterSet(ctx context.Context, clusterSetName string, labels ...string) {
	By(fmt.Sprintf("Create clusterset %s", clusterSetName))
	clusterset := &clusterapiv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   clusterSetName,
			Labels: map[string]string{},
		},
		Spec: clusterapiv1beta2.ManagedClusterSetSpec{
			ClusterSelector: clusterapiv1beta2.ManagedClusterSelector{
				SelectorType: clusterapiv1beta2.LabelSelector,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{},
				},
			},
		},
	}

	if len(labels) > 0 {
		for i := 1; i < len(labels); i += 2 {
			clusterset.Spec.ClusterSelector.LabelSelector.MatchLabels[labels[i-1]] = labels[i]
		}
	}

	_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterset, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
}

func assertBindingClusterSet(ctx context.Context, clusterSetName, namespace string) {
	By("Create clusterset/clustersetbinding")
	clusterset := &clusterapiv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterSetName,
		},
	}
	_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, clusterset, metav1.CreateOptions{})
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
	_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, csb, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
}

func assertDeletingClusterSet(ctx context.Context, clusterSetName string) {
	By(fmt.Sprintf("Delete clusterset %s", clusterSetName))
	err := clusterClient.ClusterV1beta2().ManagedClusterSets().Delete(ctx, clusterSetName, metav1.DeleteOptions{})
	Expect(err).ToNot(HaveOccurred())

	By("Check if clusterset is gone")
	Eventually(func() bool {
		_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, clusterSetName, metav1.GetOptions{})
		if err == nil {
			return false
		}
		return errors.IsNotFound(err)
	}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
}

func assertCreatingClusterSetBinding(ctx context.Context, clusterSetName, namespace string) {
	By(fmt.Sprintf("Create clustersetbinding %s", clusterSetName))
	csb := &clusterapiv1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      clusterSetName,
		},
		Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: clusterSetName,
		},
	}
	_, err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(ctx, csb, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
}

func assertDeletingClusterSetBinding(ctx context.Context, clusterSetName, namespace string) {
	By(fmt.Sprintf("Delete clustersetbinding %s", clusterSetName))
	err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Delete(ctx, clusterSetName, metav1.DeleteOptions{})
	Expect(err).ToNot(HaveOccurred())

	By("Check if clustersetbinding is gone")
	Eventually(func() bool {
		_, err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(ctx, clusterSetName, metav1.GetOptions{})
		if err == nil {
			return false
		}
		return errors.IsNotFound(err)
	}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
}

// assert clusters
func assertCreatingClusters(ctx context.Context, clusterSetName string, num int, labels ...string) []string {
	By(fmt.Sprintf("Create %d clusters", num))

	var names []string
	for i := 0; i < num; i++ {
		cluster := &clusterapiv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "cluster-",
				Labels: map[string]string{
					clusterSetLabel: clusterSetName,
				},
			},
		}
		for i := 1; i < len(labels); i += 2 {
			cluster.Labels[labels[i-1]] = labels[i]
		}
		cluster, err := clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		cluster.Status.Conditions = []metav1.Condition{}
		for i := 1; i < len(labels); i += 2 {
			cluster.Status.ClusterClaims = append(cluster.Status.ClusterClaims, clusterapiv1.ManagedClusterClaim{Name: labels[i-1], Value: labels[i]})
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(ctx, cluster, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())

		names = append(names, cluster.Name)
	}

	sort.SliceStable(names, func(i, j int) bool {
		return names[i] < names[j]
	})

	return names
}

func assertCleanupClusters(ctx context.Context) []string {
	By("Cleanup all managed clusters")
	var clusterNames []string
	clusters, err := clusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{})
	Expect(err).ToNot(HaveOccurred())
	for _, cluster := range clusters.Items {
		clusterNames = append(clusterNames, cluster.Name)
	}

	assertDeletingClusters(ctx, clusterNames...)

	return clusterNames
}

func assertPatchingClusterStatusWithResources(ctx context.Context, managedClusterName string, res []string) {
	By(fmt.Sprintf("Updating ManagedClusters %s cluster resources", managedClusterName))

	oldmc, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, managedClusterName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	allocatable := map[clusterapiv1.ResourceName]resource.Quantity{}
	capacity := map[clusterapiv1.ResourceName]resource.Quantity{}

	allocatable[clusterapiv1.ResourceCPU], err = resource.ParseQuantity(res[0])
	Expect(err).ToNot(HaveOccurred())
	allocatable[clusterapiv1.ResourceMemory], err = resource.ParseQuantity(res[2])
	Expect(err).ToNot(HaveOccurred())
	capacity[clusterapiv1.ResourceCPU], err = resource.ParseQuantity(res[1])
	Expect(err).ToNot(HaveOccurred())
	capacity[clusterapiv1.ResourceMemory], err = resource.ParseQuantity(res[3])
	Expect(err).ToNot(HaveOccurred())

	newmc := oldmc.DeepCopy()
	newmc.Status.Allocatable = allocatable
	newmc.Status.Capacity = capacity

	managedClusterPatcher := patcher.NewPatcher[
		*clusterapiv1.ManagedCluster, clusterapiv1.ManagedClusterSpec, clusterapiv1.ManagedClusterStatus](
		clusterClient.ClusterV1().ManagedClusters())

	_, err = managedClusterPatcher.PatchStatus(ctx, newmc, newmc.Status, oldmc.Status)
	Expect(err).ToNot(HaveOccurred())
}

func assertPatchingClusterSpecWithTaint(ctx context.Context, managedClusterName string, taint *clusterapiv1.Taint) {
	By(fmt.Sprintf("Updating ManagedClusters %s taint", managedClusterName))
	if taint == nil {
		return
	}

	oldmc, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, managedClusterName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	newmc := oldmc.DeepCopy()
	newmc.Spec.Taints = append(newmc.Spec.Taints, *taint)

	managedClusterPatcher := patcher.NewPatcher[
		*clusterapiv1.ManagedCluster, clusterapiv1.ManagedClusterSpec, clusterapiv1.ManagedClusterStatus](
		clusterClient.ClusterV1().ManagedClusters())

	_, err = managedClusterPatcher.PatchSpec(ctx, newmc, newmc.Spec, oldmc.Spec)
	Expect(err).ToNot(HaveOccurred())
}

func assertDeletingClusters(ctx context.Context, clusterNames ...string) {
	for _, clusterName := range clusterNames {
		By(fmt.Sprintf("Delete cluster %s", clusterName))
		err := clusterClient.ClusterV1().ManagedClusters().Delete(ctx, clusterName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Check if cluster is gone")
		Eventually(func() bool {
			_, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, clusterName, metav1.GetOptions{})
			if err == nil {
				return false
			}
			return errors.IsNotFound(err)
		}, eventuallyTimeout*2, eventuallyInterval).Should(BeTrue())
	}
}

func assertCreatingAddOnPlacementScores(ctx context.Context, clusternamespace, crname, scorename string, score int32) {
	By(fmt.Sprintf("Create namespace %s for addonplacementscores %s", clusternamespace, crname))
	if _, err := kubeClient.CoreV1().Namespaces().Get(ctx, clusternamespace, metav1.GetOptions{}); err != nil {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusternamespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	}

	By(fmt.Sprintf("Create addonplacementscores %s in %s", crname, clusternamespace))
	addOn, err := clusterClient.ClusterV1alpha1().AddOnPlacementScores(clusternamespace).Get(ctx, crname, metav1.GetOptions{})
	if err != nil {
		newAddOn := &clusterapiv1alpha1.AddOnPlacementScore{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusternamespace,
				Name:      crname,
			},
		}
		addOn, err = clusterClient.ClusterV1alpha1().AddOnPlacementScores(clusternamespace).Create(ctx, newAddOn, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	}

	vu := metav1.NewTime(time.Now().Add(10 * time.Second))
	addOn.Status = clusterapiv1alpha1.AddOnPlacementScoreStatus{
		Scores: []clusterapiv1alpha1.AddOnPlacementScoreItem{
			{
				Name:  scorename,
				Value: score,
			},
		},
		ValidUntil: &vu,
	}

	_, err = clusterClient.ClusterV1alpha1().AddOnPlacementScores(clusternamespace).UpdateStatus(ctx, addOn, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())
}
