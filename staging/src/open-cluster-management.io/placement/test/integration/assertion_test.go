package integration

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	"open-cluster-management.io/placement/test/integration/util"
)

func assertPlacementDecisionCreated(placement *clusterapiv1beta1.Placement) {
	ginkgo.By("Check if placementdecision is created")
	gomega.Eventually(func() bool {
		pdl, err := clusterClient.ClusterV1beta1().PlacementDecisions(placement.Namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: placementLabel + "=" + placement.Name,
		})
		if err != nil {
			return false
		}
		if len(pdl.Items) == 0 {
			return false
		}
		for _, pd := range pdl.Items {
			if controlled := metav1.IsControlledBy(&pd.ObjectMeta, placement); !controlled {
				return false
			}
		}
		return true
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func assertCreatingPlacementDecision(name, namespace string, clusterNames []string) {
	ginkgo.By(fmt.Sprintf("Create placementdecision %s", name))
	placementDecision := &clusterapiv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				placementLabel: name,
			},
		},
	}
	placementDecision, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).Create(context.Background(), placementDecision, metav1.CreateOptions{})

	var clusterDecisions []clusterapiv1beta1.ClusterDecision
	for _, clusterName := range clusterNames {
		clusterDecisions = append(clusterDecisions, clusterapiv1beta1.ClusterDecision{
			ClusterName: clusterName,
		})
	}

	placementDecision.Status.Decisions = clusterDecisions
	placementDecision, err = clusterClient.ClusterV1beta1().PlacementDecisions(namespace).UpdateStatus(context.Background(), placementDecision, metav1.UpdateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

func assertPlacementDeleted(placementName, namespace string) {
	ginkgo.By("Check if placement is gone")
	gomega.Eventually(func() bool {
		_, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
		if err == nil {
			return false
		}
		return errors.IsNotFound(err)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func assertNumberOfDecisions(placementName, namespace string, desiredNOD int) {
	ginkgo.By("Check the number of decisions in placementdecisions")
	// at least one decision for each placement
	desiredNOPD := desiredNOD/maxNumOfClusterDecisions + 1
	gomega.Eventually(func() bool {
		pdl, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: placementLabel + "=" + placementName,
		})
		if err != nil {
			return false
		}
		if len(pdl.Items) != desiredNOPD {
			return false
		}
		actualNOD := 0
		for _, pd := range pdl.Items {
			actualNOD += len(pd.Status.Decisions)
		}
		return actualNOD == desiredNOD
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func assertClusterNamesOfDecisions(placementName, namespace string, desiredClusters []string) {
	ginkgo.By(fmt.Sprintf("Check the cluster names of placementdecisions %s", placementName))
	gomega.Eventually(func() bool {
		pdl, err := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{
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
		ginkgo.By(fmt.Sprintf("Expect %v, but got %v", desiredClusters.List(), actualClusters.List()))
		return false
	}, eventuallyTimeout*2, eventuallyInterval).Should(gomega.BeTrue())
}

func assertPlacementConditionSatisfied(placementName, namespace string, numOfSelectedClusters int, satisfied bool) {
	ginkgo.By("Check the condition PlacementSatisfied of placement")
	gomega.Eventually(func() bool {
		placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
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
		return placement.Status.NumberOfSelectedClusters == int32(numOfSelectedClusters)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func assertPlacementConditionMisconfigured(placementName, namespace string, misConfigured bool) {
	ginkgo.By("Check the condition PlacementMisconfigured of placement")
	gomega.Eventually(func() bool {
		placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), placementName, metav1.GetOptions{})
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
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func assertBindingClusterSet(clusterSetName, namespace string) {
	ginkgo.By("Create clusterset/clustersetbinding")
	clusterset := &clusterapiv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterSetName,
		},
	}
	_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
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
	_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), csb, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

func assertCreatingClusterSet(clusterSetName string, labels ...string) {
	ginkgo.By(fmt.Sprintf("Create clusterset %s", clusterSetName))
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

	_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

func assertDeletingClusterSet(clusterSetName string) {
	ginkgo.By(fmt.Sprintf("Delete clusterset %s", clusterSetName))
	err := clusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), clusterSetName, metav1.DeleteOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	ginkgo.By("Check if clusterset is gone")
	gomega.Eventually(func() bool {
		_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.Background(), clusterSetName, metav1.GetOptions{})
		if err == nil {
			return false
		}
		return errors.IsNotFound(err)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func assertCreatingClusterSetBinding(clusterSetName, namespace string) {
	ginkgo.By(fmt.Sprintf("Create clustersetbinding %s", clusterSetName))
	csb := &clusterapiv1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      clusterSetName,
		},
		Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: clusterSetName,
		},
	}
	_, err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), csb, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

func assertDeletingClusterSetBinding(clusterSetName, namespace string) {
	ginkgo.By(fmt.Sprintf("Delete clustersetbinding %s", clusterSetName))
	err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Delete(context.Background(), clusterSetName, metav1.DeleteOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	ginkgo.By("Check if clustersetbinding is gone")
	gomega.Eventually(func() bool {
		_, err := clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.Background(), clusterSetName, metav1.GetOptions{})
		if err == nil {
			return false
		}
		return errors.IsNotFound(err)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func assertCreatingClusters(clusterSetName string, num int, labels ...string) []string {
	ginkgo.By(fmt.Sprintf("Create %d clusters", num))

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
		cluster, err := clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		cluster.Status.Conditions = []metav1.Condition{}
		for i := 1; i < len(labels); i += 2 {
			cluster.Status.ClusterClaims = append(cluster.Status.ClusterClaims, clusterapiv1.ManagedClusterClaim{Name: labels[i-1], Value: labels[i]})
		}
		_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(context.Background(), cluster, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		names = append(names, cluster.Name)
	}

	sort.SliceStable(names, func(i, j int) bool {
		return names[i] < names[j]
	})

	return names
}

func assertCleanupClusters() []string {
	ginkgo.By("Cleanup all managed clusters")
	var clusterNames []string
	clusters, err := clusterClient.ClusterV1().ManagedClusters().List(context.Background(), metav1.ListOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	for _, cluster := range clusters.Items {
		clusterNames = append(clusterNames, cluster.Name)
	}

	assertDeletingClusters(clusterNames...)

	return clusterNames
}

func assertUpdatingClusterWithClusterResources(managedClusterName string, res []string) {
	ginkgo.By(fmt.Sprintf("Updating ManagedClusters %s cluster resources", managedClusterName))

	mc, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedClusterName, metav1.GetOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	allocatable := map[clusterapiv1.ResourceName]resource.Quantity{}
	capacity := map[clusterapiv1.ResourceName]resource.Quantity{}

	allocatable[clusterapiv1.ResourceCPU], err = resource.ParseQuantity(res[0])
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	allocatable[clusterapiv1.ResourceMemory], err = resource.ParseQuantity(res[2])
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	capacity[clusterapiv1.ResourceCPU], err = resource.ParseQuantity(res[1])
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	capacity[clusterapiv1.ResourceMemory], err = resource.ParseQuantity(res[3])
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	mc.Status = clusterapiv1.ManagedClusterStatus{
		Allocatable: allocatable,
		Capacity:    capacity,
		Conditions:  []metav1.Condition{},
	}
	_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(context.Background(), mc, metav1.UpdateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

func assertUpdatingClusterWithClusterTaint(managedClusterName string, taint *clusterapiv1.Taint) {
	ginkgo.By(fmt.Sprintf("Updating ManagedClusters %s taint", managedClusterName))
	if taint == nil {
		return
	}

	mc, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedClusterName, metav1.GetOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	mc.Spec.Taints = append(mc.Spec.Taints, *taint)

	_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.Background(), mc, metav1.UpdateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

func assertDeletingClusters(clusterNames ...string) {
	for _, clusterName := range clusterNames {
		ginkgo.By(fmt.Sprintf("Delete cluster %s", clusterName))
		err := clusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), clusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Check if cluster is gone")
		gomega.Eventually(func() bool {
			_, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), clusterName, metav1.GetOptions{})
			if err == nil {
				return false
			}
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}
}

func assertCreatingPlacement(name, namespace string, noc *int32, prioritizerPolicy clusterapiv1beta1.PrioritizerPolicy, tolerations []clusterapiv1beta1.Toleration) *clusterapiv1beta1.Placement {
	ginkgo.By("Create placement")
	placement := &clusterapiv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: clusterapiv1beta1.PlacementSpec{
			NumberOfClusters:  noc,
			PrioritizerPolicy: prioritizerPolicy,
			Tolerations:       tolerations,
		},
	}
	placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Create(context.Background(), placement, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	return placement
}

func assertCreatingPlacementWithDecision(name, namespace string, noc *int32, nod int, prioritizerPolicy clusterapiv1beta1.PrioritizerPolicy, tolerations []clusterapiv1beta1.Toleration) {
	placement := assertCreatingPlacement(name, namespace, noc, prioritizerPolicy, tolerations)
	assertPlacementDecisionCreated(placement)
	assertNumberOfDecisions(name, namespace, nod)
	if noc != nil {
		assertPlacementConditionSatisfied(name, namespace, nod, nod == int(*noc))
	}
}

func assertUpdatingPlacement(name, namespace string, noc *int32, prioritizerPolicy clusterapiv1beta1.PrioritizerPolicy, tolerations []clusterapiv1beta1.Toleration) {
	ginkgo.By("Updating placement")
	placement, err := clusterClient.ClusterV1beta1().Placements(namespace).Get(context.Background(), name, metav1.GetOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	placement.Spec.NumberOfClusters = noc
	placement.Spec.Tolerations = tolerations
	placement.Spec.PrioritizerPolicy = prioritizerPolicy
	placement, err = clusterClient.ClusterV1beta1().Placements(namespace).Update(context.Background(), placement, metav1.UpdateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

func assertCreatingAddOnPlacementScores(clusternamespace, crname, scorename string, score int32) {
	ginkgo.By(fmt.Sprintf("Create namespace %s for addonplacementscores %s", clusternamespace, crname))
	if _, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), clusternamespace, metav1.GetOptions{}); err != nil {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusternamespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	ginkgo.By(fmt.Sprintf("Create addonplacementscores %s in %s", crname, clusternamespace))
	addOn, err := clusterClient.ClusterV1alpha1().AddOnPlacementScores(clusternamespace).Get(context.Background(), crname, metav1.GetOptions{})
	if err != nil {
		newAddOn := &clusterapiv1alpha1.AddOnPlacementScore{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusternamespace,
				Name:      crname,
			},
		}
		addOn, err = clusterClient.ClusterV1alpha1().AddOnPlacementScores(clusternamespace).Create(context.Background(), newAddOn, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
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

	_, err = clusterClient.ClusterV1alpha1().AddOnPlacementScores(clusternamespace).UpdateStatus(context.Background(), addOn, metav1.UpdateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}
