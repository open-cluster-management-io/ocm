package manifestworkreplicasetcontroller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	clustersdkv1alpha1 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1alpha1"
	workapplier "open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	helpertest "open-cluster-management.io/ocm/pkg/work/hub/test"
)

func TestDeployReconcileAsExpected(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	mw, _ := CreateManifestWork(mwrSet, "cls1", "place-test")
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, mw)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw); err != nil {
		t.Fatal(err)
	}
	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet); err != nil {
		t.Fatal(err)
	}
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	// Adding cls2 cluster to placement to check manifestwork added
	placement, placementDecision := helpertest.CreateTestPlacement("place-test", "default", "cls1", "cls2")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement, placementDecision)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)

	if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement); err != nil {
		t.Fatal(err)
	}
	if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision); err != nil {
		t.Fatal(err)
	}

	placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
	placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()

	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: placementDecisionLister,
		placementLister:     placementLister,
	}

	mwrSet, _, err := pmwDeployController.reconcile(context.TODO(), mwrSet)
	if err != nil {
		t.Fatal(err)
	}

	// Check for the expected manifestWorkReplicaSetSummary
	mwrSetSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       int(placement.Status.NumberOfSelectedClusters),
		Applied:     0,
		Available:   0,
		Degraded:    0,
		Progressing: 0,
	}

	if mwrSet.Status.Summary != mwrSetSummary {
		t.Fatal("Summary not as expected ", mwrSet.Status.Summary, mwrSetSummary)
	}

	// Check the PlacedManifestWork conditions
	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)

	if placeCondition == nil {
		t.Fatal("Placement condition not found ", mwrSet.Status.Conditions)
	}

	// Check placement condition status true
	if placeCondition.Status == metav1.ConditionFalse {
		t.Fatal("Placement condition not True ", placeCondition)
	}

	// Check placement condition reason
	if placeCondition.Reason != workapiv1alpha1.ReasonAsExpected {
		t.Fatal("Placement condition Reason not match AsExpected ", placeCondition)
	}
}

func TestDeployReconcileAsPlacementDecisionEmpty(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Minute)
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	// No clusters added to the placement
	placement, placementDecision := helpertest.CreateTestPlacement("place-test", "default")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement, placementDecision)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Minute)

	if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement); err != nil {
		t.Fatal(err)
	}
	if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision); err != nil {
		t.Fatal(err)
	}

	placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
	placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()

	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: placementDecisionLister,
		placementLister:     placementLister,
	}

	mwrSet, _, err := pmwDeployController.reconcile(context.TODO(), mwrSet)
	if err != nil {
		t.Fatal(err)
	}

	// Check for the expected Summary
	mwrSetSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       int(placement.Status.NumberOfSelectedClusters),
		Applied:     0,
		Available:   0,
		Degraded:    0,
		Progressing: 0,
	}

	if mwrSet.Status.Summary != mwrSetSummary {
		t.Fatal("Summary not as expected ", mwrSet.Status.Summary, mwrSetSummary)
	}

	// Check the PlacedManifestWork conditions
	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)

	if placeCondition == nil {
		t.Fatal("Placement condition not found ", mwrSet.Status.Conditions)
	}

	// Check placement condition status is false
	if placeCondition.Status == metav1.ConditionTrue {
		t.Fatal("Placement condition status not False ", placeCondition)
	}

	// Check placement condition reason is PlacementDecisionEmpty
	if placeCondition.Reason != workapiv1alpha1.ReasonPlacementDecisionEmpty {
		t.Fatal("Placement condition Reason not match PlacementDecisionEmpty ", placeCondition)
	}
}

func TestDeployReconcileAsPlacementNotExist(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-notexist")
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Minute)
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	placement, _ := helpertest.CreateTestPlacement("place-test", "default")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Minute)

	if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement); err != nil {
		t.Fatal(err)
	}

	placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
	placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()

	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: placementDecisionLister,
		placementLister:     placementLister,
	}

	mwrSet, _, err := pmwDeployController.reconcile(context.TODO(), mwrSet)
	if err != nil {
		t.Fatal(err)
	}

	// Check the PlacedManifestWork conditions
	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)

	if placeCondition == nil {
		t.Fatal("Placement condition not found ", mwrSet.Status.Conditions)
	}

	// Check placement condition status is false
	if placeCondition.Status == metav1.ConditionTrue {
		t.Fatal("Placement condition status not False ", placeCondition)
	}

	// Check placement condition reason is PlacementDecisionNotFound
	if placeCondition.Reason != workapiv1alpha1.ReasonPlacementDecisionNotFound {
		t.Fatal("Placement condition Reason not match PlacementDecisionEmpty ", placeCondition)
	}
}

func TestDeployWithRolloutStrategyReconcileAsExpected(t *testing.T) {
	// create placement with 5 clusters and 2 groups
	clusters := []string{"cls1", "cls2", "cls3", "cls4", "cls5"}
	clsPerGroup := 3
	placement, placementDecisions := helpertest.CreateTestPlacementWithDecisionStrategy("place-test", "default", clsPerGroup, clusters...)
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement, placementDecisions[0])
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)
	err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement)
	assert.Nil(t, err)

	for _, plcDecision := range placementDecisions {
		err = clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(plcDecision)
		assert.Nil(t, err)
	}
	placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
	placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()
	perGoupeRollOut := clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.ProgressivePerGroup}
	mwrSet := helpertest.CreateTestManifestWorkReplicaSetWithRollOutStrategy("mwrSet-test", "default",
		map[string]clusterv1alpha1.RolloutStrategy{placement.Name: perGoupeRollOut})
	mw := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, placement.Name, "cls1")
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, mw)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	err = workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw)
	assert.Nil(t, err)

	err = workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet)
	assert.Nil(t, err)

	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: placementDecisionLister,
		placementLister:     placementLister,
	}

	mwrSet, _, err = pmwDeployController.reconcile(context.TODO(), mwrSet)
	assert.Nil(t, err)
	assert.Equal(t, len(mwrSet.Status.PlacementsSummary), len(mwrSet.Spec.PlacementRefs))

	expectPlcSummary := workapiv1alpha1.PlacementSummary{
		Name: placement.Name,
		AvailableDecisionGroups: getAvailableDecisionGroupProgressMessage(len(placement.Status.DecisionGroups),
			clsPerGroup, placement.Status.NumberOfSelectedClusters),
		Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{
			Total:       clsPerGroup,
			Applied:     0,
			Available:   0,
			Degraded:    0,
			Progressing: 0,
		},
	}
	actualPlcSummary := mwrSet.Status.PlacementsSummary[0]
	assert.Equal(t, actualPlcSummary, expectPlcSummary)

	// Check for the expected manifestWorkReplicaSetSummary
	mwrSetSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       clsPerGroup,
		Applied:     0,
		Available:   0,
		Degraded:    0,
		Progressing: 0,
	}
	assert.Equal(t, mwrSet.Status.Summary, mwrSetSummary)

	// Check the PlacedManifestWork conditions
	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
	assert.NotNil(t, placeCondition)
	assert.Equal(t, placeCondition.Status, metav1.ConditionTrue)
	assert.Equal(t, placeCondition.Reason, workapiv1alpha1.ReasonAsExpected)

	// Check the RollOut conditions
	rollOutCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
	assert.NotNil(t, rollOutCondition)
	assert.Equal(t, rollOutCondition.Status, metav1.ConditionFalse)
	assert.Equal(t, rollOutCondition.Reason, workapiv1alpha1.ReasonProgressing)

	// re-run deploy to have all clusters manifestWorks created.
	mw = helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, "place-test", "cls2")
	err = workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw)
	assert.Nil(t, err)

	mw = helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, "place-test", "cls3")
	err = workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw)
	assert.Nil(t, err)

	mwrSet, _, err = pmwDeployController.reconcile(context.TODO(), mwrSet)
	assert.Nil(t, err)
	assert.Equal(t, len(mwrSet.Status.PlacementsSummary), len(mwrSet.Spec.PlacementRefs))

	expectPlcSummary = workapiv1alpha1.PlacementSummary{
		Name: placement.Name,
		AvailableDecisionGroups: getAvailableDecisionGroupProgressMessage(len(placement.Status.DecisionGroups),
			int(placement.Status.NumberOfSelectedClusters), placement.Status.NumberOfSelectedClusters),
		Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{
			Total:       int(placement.Status.NumberOfSelectedClusters),
			Applied:     0,
			Available:   0,
			Degraded:    0,
			Progressing: 0,
		},
	}
	actualPlcSummary = mwrSet.Status.PlacementsSummary[0]
	assert.Equal(t, expectPlcSummary, actualPlcSummary)

	// Check for the expected manifestWorkReplicaSetSummary
	mwrSetSummary = workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       len(clusters),
		Applied:     0,
		Available:   0,
		Degraded:    0,
		Progressing: 0,
	}
	assert.Equal(t, mwrSet.Status.Summary, mwrSetSummary)

	// Check the RollOut conditions - should be Complete when all clusters have succeeded
	// At this point, all ManifestWorks are created but none have Succeeded status yet
	rollOutCondition = apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
	assert.NotNil(t, rollOutCondition)
	assert.Equal(t, rollOutCondition.Status, metav1.ConditionFalse)
	assert.Equal(t, rollOutCondition.Reason, workapiv1alpha1.ReasonProgressing)

	// Now mark all ManifestWorks as Succeeded to achieve ReasonComplete
	for i := 0; i < len(clusters); i++ {
		mw := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, "place-test", clusters[i])
		apimeta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
			Type:               workapiv1.WorkApplied,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: mw.Generation,
			Reason:             "Applied",
		})
		apimeta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
			Type:               workapiv1.WorkProgressing,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: mw.Generation,
			Reason:             "Completed",
		})
		err = workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(mw)
		assert.Nil(t, err)
	}

	mwrSet, _, err = pmwDeployController.reconcile(context.TODO(), mwrSet)
	assert.Nil(t, err)

	// Now RollOut should be Complete since all clusters have Succeeded status
	rollOutCondition = apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
	assert.NotNil(t, rollOutCondition)
	assert.Equal(t, rollOutCondition.Status, metav1.ConditionTrue)
	assert.Equal(t, rollOutCondition.Reason, workapiv1alpha1.ReasonComplete)
}

func TestDeployWithMultiPlacementsReconcileAsExpected(t *testing.T) {
	placement1, placementDecision1 := helpertest.CreateTestPlacement("place-test1", "default", "cls6", "cls7")
	// create placement with 5 clusters and 2 groups
	clusters := []string{"cls1", "cls2", "cls3", "cls4", "cls5"}
	clsPerGroup := 3
	placement2, placementDecisions2 := helpertest.CreateTestPlacementWithDecisionStrategy("place-test2", "default", clsPerGroup, clusters...)
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement1, placementDecision1)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)
	err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement1)
	assert.Nil(t, err)

	err = clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement2)
	assert.Nil(t, err)

	err = clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision1)
	assert.Nil(t, err)

	for _, plcDecision := range placementDecisions2 {
		err = clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(plcDecision)
		assert.Nil(t, err)
	}

	placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
	placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()
	perGoupeRollOut := clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.ProgressivePerGroup}
	allRollOut := clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}

	mwrSet := helpertest.CreateTestManifestWorkReplicaSetWithRollOutStrategy("mwrSet-test", "default",
		map[string]clusterv1alpha1.RolloutStrategy{placement1.Name: allRollOut, placement2.Name: perGoupeRollOut})

	mw := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, placement2.Name, "cls1")
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, mw)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	err = workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw)
	assert.Nil(t, err)

	err = workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet)
	assert.Nil(t, err)

	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: placementDecisionLister,
		placementLister:     placementLister,
	}

	mwrSet, _, err = pmwDeployController.reconcile(context.TODO(), mwrSet)
	assert.Nil(t, err)
	assert.Equal(t, len(mwrSet.Status.PlacementsSummary), len(mwrSet.Spec.PlacementRefs))

	// Check placements summary
	expectPlcSummary1 := workapiv1alpha1.PlacementSummary{
		Name: placement1.Name,
		AvailableDecisionGroups: getAvailableDecisionGroupProgressMessage(len(placement1.Status.DecisionGroups),
			int(placement1.Status.NumberOfSelectedClusters), placement1.Status.NumberOfSelectedClusters),
		Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{
			Total:       int(placement1.Status.NumberOfSelectedClusters),
			Applied:     0,
			Available:   0,
			Degraded:    0,
			Progressing: 0,
		},
	}
	expectPlcSummary2 := workapiv1alpha1.PlacementSummary{
		Name: placement2.Name,
		AvailableDecisionGroups: getAvailableDecisionGroupProgressMessage(len(placement2.Status.DecisionGroups),
			clsPerGroup, placement2.Status.NumberOfSelectedClusters),
		Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{
			Total:       clsPerGroup,
			Applied:     0,
			Available:   0,
			Degraded:    0,
			Progressing: 0,
		},
	}
	for _, plcSummary := range mwrSet.Status.PlacementsSummary {
		if plcSummary.Name == placement1.Name { //nolint:gocritic
			assert.Equal(t, plcSummary, expectPlcSummary1)
		} else if plcSummary.Name == placement2.Name {
			assert.Equal(t, plcSummary, expectPlcSummary2)
		} else {
			t.Fatal("PlacementSummary Name not Exist")
		}
	}

	// Check for the expected manifestWorkReplicaSetSummary
	mwrSetSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       clsPerGroup + int(placement1.Status.NumberOfSelectedClusters),
		Applied:     0,
		Available:   0,
		Degraded:    0,
		Progressing: 0,
	}
	assert.Equal(t, mwrSet.Status.Summary, mwrSetSummary)

	// Check the PlacedManifestWork conditions
	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
	assert.NotNil(t, placeCondition)
	// Check placement condition status true
	assert.Equal(t, placeCondition.Status, metav1.ConditionTrue)
	// Check placement condition reason
	assert.Equal(t, placeCondition.Reason, workapiv1alpha1.ReasonAsExpected)

	// Check the RollOut conditions
	rollOutCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
	assert.NotNil(t, rollOutCondition)
	// Check placement condition status False
	assert.Equal(t, rollOutCondition.Status, metav1.ConditionFalse)
	// Check placement condition reason
	assert.Equal(t, rollOutCondition.Reason, workapiv1alpha1.ReasonProgressing)
}

func TestDeployMWRSetSpecChangesReconcile(t *testing.T) {
	// create placement with 5 clusters and 2 groups
	clusters := []string{"cls1", "cls2", "cls3", "cls4", "cls5"}
	clsPerGroup := 3
	placement, placementDecisions := helpertest.CreateTestPlacementWithDecisionStrategy("place-test", "default", clsPerGroup, clusters...)
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement, placementDecisions[0])
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)
	err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement)
	assert.Nil(t, err)

	for _, plcDecision := range placementDecisions {
		err = clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(plcDecision)
		assert.Nil(t, err)
	}
	placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
	placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()
	perGoupeRollOut := clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.ProgressivePerGroup}
	mwrSet := helpertest.CreateTestManifestWorkReplicaSetWithRollOutStrategy("mwrSet-test", "default",
		map[string]clusterv1alpha1.RolloutStrategy{placement.Name: perGoupeRollOut})

	var objs []runtime.Object
	for i := 0; i < clsPerGroup; i++ {
		mw := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, placement.Name, clusters[i])
		objs = append(objs, mw)
	}
	objs = append(objs, mwrSet)

	fWorkClient := fakeworkclient.NewSimpleClientset(objs...)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)
	// create manifestWorks
	for i := 0; i < clsPerGroup; i++ {
		mw := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, placement.Name, clusters[i])
		err = workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw)
		if err != nil {
			t.Error(err)
		}
		assert.Nil(t, err)
	}

	err = workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet)
	assert.Nil(t, err)

	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: placementDecisionLister,
		placementLister:     placementLister,
	}

	mwrSet, _, err = pmwDeployController.reconcile(context.TODO(), mwrSet)
	assert.Nil(t, err)
	assert.Equal(t, len(mwrSet.Status.PlacementsSummary), len(mwrSet.Spec.PlacementRefs))

	// expected all clusters manifestWork are created
	expectPlcSummary := workapiv1alpha1.PlacementSummary{
		Name: placement.Name,
		AvailableDecisionGroups: getAvailableDecisionGroupProgressMessage(len(placement.Status.DecisionGroups),
			int(placement.Status.NumberOfSelectedClusters), placement.Status.NumberOfSelectedClusters),
		Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{
			Total:       int(placement.Status.NumberOfSelectedClusters),
			Applied:     0,
			Available:   0,
			Degraded:    0,
			Progressing: 0,
		},
	}
	actualPlcSummary := mwrSet.Status.PlacementsSummary[0]
	assert.Equal(t, actualPlcSummary, expectPlcSummary)

	// Check for the expected manifestWorkReplicaSetSummary
	mwrSetSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       int(placement.Status.NumberOfSelectedClusters),
		Applied:     0,
		Available:   0,
		Degraded:    0,
		Progressing: 0,
	}
	assert.Equal(t, mwrSet.Status.Summary, mwrSetSummary)

	// Check the PlacedManifestWork conditions
	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
	assert.NotNil(t, placeCondition)
	assert.Equal(t, placeCondition.Status, metav1.ConditionTrue)
	assert.Equal(t, placeCondition.Reason, workapiv1alpha1.ReasonAsExpected)

	// Check the RollOut conditions - need to mark ManifestWorks as Succeeded first
	rollOutCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
	assert.NotNil(t, rollOutCondition)
	assert.Equal(t, rollOutCondition.Status, metav1.ConditionFalse)
	assert.Equal(t, rollOutCondition.Reason, workapiv1alpha1.ReasonProgressing)

	// Mark all ManifestWorks as Succeeded
	for i := 0; i < int(placement.Status.NumberOfSelectedClusters); i++ {
		mw := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, placement.Name, clusters[i])
		apimeta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
			Type:               workapiv1.WorkApplied,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: mw.Generation,
			Reason:             "Applied",
		})
		apimeta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
			Type:               workapiv1.WorkProgressing,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: mw.Generation,
			Reason:             "Completed",
		})
		err = workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(mw)
		assert.Nil(t, err)
	}

	mwrSet, _, err = pmwDeployController.reconcile(context.TODO(), mwrSet)
	assert.Nil(t, err)

	// Now RollOut should be Complete
	rollOutCondition = apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
	assert.NotNil(t, rollOutCondition)
	assert.Equal(t, rollOutCondition.Status, metav1.ConditionTrue)
	assert.Equal(t, rollOutCondition.Reason, workapiv1alpha1.ReasonComplete)

	// Change the mwrSet spec and re-run deploy.
	mwTemplate := helpertest.CreateTestManifestWorkSpecWithSecret("v2", "test", "ns-test", "name-test")
	mwTemplate.DeepCopyInto(&mwrSet.Spec.ManifestWorkTemplate)

	mwrSet, _, _ = pmwDeployController.reconcile(context.TODO(), mwrSet)
	assert.Equal(t, len(mwrSet.Status.PlacementsSummary), len(mwrSet.Spec.PlacementRefs))

	// As the mwrSet.Spec changed we expect to rollOut again with the num of clsPerGroup
	expectPlcSummary = workapiv1alpha1.PlacementSummary{
		Name: placement.Name,
		AvailableDecisionGroups: getAvailableDecisionGroupProgressMessage(len(placement.Status.DecisionGroups),
			clsPerGroup, placement.Status.NumberOfSelectedClusters),
		Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{
			Total:       clsPerGroup,
			Applied:     0,
			Available:   0,
			Degraded:    0,
			Progressing: 0,
		},
	}
	actualPlcSummary = mwrSet.Status.PlacementsSummary[0]
	assert.Equal(t, expectPlcSummary, actualPlcSummary)

	// Check for the expected manifestWorkReplicaSetSummary
	mwrSetSummary = workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       clsPerGroup,
		Applied:     0,
		Available:   0,
		Degraded:    0,
		Progressing: 0,
	}
	assert.Equal(t, mwrSet.Status.Summary, mwrSetSummary)

	// Check the RollOut conditions
	rollOutCondition = apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
	assert.NotNil(t, rollOutCondition)
	assert.Equal(t, rollOutCondition.Status, metav1.ConditionFalse)
	assert.Equal(t, rollOutCondition.Reason, workapiv1alpha1.ReasonProgressing)
}

func TestRequeueWithProgressDeadline(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	mwrSet.Spec.PlacementRefs[0].RolloutStrategy = clusterv1alpha1.RolloutStrategy{
		Type: clusterv1alpha1.Progressive,
		Progressive: &clusterv1alpha1.RolloutProgressive{
			MaxConcurrency: intstr.FromInt32(1),
			RolloutConfig: clusterv1alpha1.RolloutConfig{
				ProgressDeadline: "5s",
				MaxFailures:      intstr.FromInt32(1),
			},
		},
	}
	mw, _ := CreateManifestWork(mwrSet, "cls1", "place-test")
	// Set Applied=True first to ensure work has been applied by hub controller
	apimeta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
		Type:               workapiv1.WorkApplied,
		Status:             metav1.ConditionTrue,
		Reason:             "Applied",
		ObservedGeneration: mw.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	})
	// Set Progressing=True AND Degraded=True to simulate a failed work (matching new logic)
	apimeta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
		Type:               workapiv1.WorkProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "Applying",
		ObservedGeneration: mw.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	})
	apimeta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
		Type:               workapiv1.WorkDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             "ApplyFailed",
		ObservedGeneration: mw.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	})
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, mw)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw); err != nil {
		t.Fatal(err)
	}
	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet); err != nil {
		t.Fatal(err)
	}
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	placement, placementDecision := helpertest.CreateTestPlacement("place-test", "default", "cls1", "cls2")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement, placementDecision)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)

	if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement); err != nil {
		t.Fatal(err)
	}
	if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision); err != nil {
		t.Fatal(err)
	}

	placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
	placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()

	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: placementDecisionLister,
		placementLister:     placementLister,
	}

	_, _, err := pmwDeployController.reconcile(context.TODO(), mwrSet)
	var rqe helpers.RequeueError
	if !errors.As(err, &rqe) {
		t.Errorf("expect to get err %t", err)
	}
}

func TestDeployReconcileDeletesOrphanedManifestWorks(t *testing.T) {
	// Create a ManifestWorkReplicaSet that references placement2
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test2")

	// Create ManifestWorks for both placement1 (old, should be deleted) and placement2 (current, should be kept)
	mwOldPlacement, _ := CreateManifestWork(mwrSet, "cls1", "place-test1")
	mwCurrentPlacement, _ := CreateManifestWork(mwrSet, "cls2", "place-test2")

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, mwOldPlacement, mwCurrentPlacement)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mwOldPlacement); err != nil {
		t.Fatal(err)
	}
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mwCurrentPlacement); err != nil {
		t.Fatal(err)
	}
	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet); err != nil {
		t.Fatal(err)
	}
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	// Create placement2 (the current placement in the spec)
	placement2, placementDecision2 := helpertest.CreateTestPlacement("place-test2", "default", "cls2")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement2, placementDecision2)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)

	if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement2); err != nil {
		t.Fatal(err)
	}
	if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision2); err != nil {
		t.Fatal(err)
	}

	placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
	placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()

	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: placementDecisionLister,
		placementLister:     placementLister,
	}

	// Run reconcile
	_, _, err := pmwDeployController.reconcile(context.TODO(), mwrSet)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that the ManifestWork from the old placement (place-test1) was deleted
	deletedMW, err := fWorkClient.WorkV1().ManifestWorks("cls1").Get(context.TODO(), mwrSet.Name, metav1.GetOptions{})
	if err == nil {
		t.Fatalf("Expected ManifestWork for old placement to be deleted, but it still exists: %v", deletedMW)
	}

	// Verify that the ManifestWork from the current placement (place-test2) still exists
	currentMW, err := fWorkClient.WorkV1().ManifestWorks("cls2").Get(context.TODO(), mwrSet.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected ManifestWork for current placement to exist, but got error: %v", err)
	}
	if currentMW == nil {
		t.Fatal("Expected ManifestWork for current placement to exist, but it is nil")
	}

	// Verify the placement label is correct
	if currentMW.Labels[workapiv1alpha1.ManifestWorkReplicaSetPlacementNameLabelKey] != "place-test2" {
		t.Fatalf("Expected placement label to be 'place-test2', got '%s'",
			currentMW.Labels[workapiv1alpha1.ManifestWorkReplicaSetPlacementNameLabelKey])
	}
}

func TestDeployReconcileWithMultiplePlacementChanges(t *testing.T) {
	// Create a ManifestWorkReplicaSet that references both placement2 and placement3
	allRollOut := clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}
	mwrSet := helpertest.CreateTestManifestWorkReplicaSetWithRollOutStrategy("mwrSet-test", "default",
		map[string]clusterv1alpha1.RolloutStrategy{"place-test2": allRollOut, "place-test3": allRollOut})

	// Create ManifestWorks for placement1 (old), placement2 (current), and placement3 (current)
	mwOldPlacement, _ := CreateManifestWork(mwrSet, "cls1", "place-test1")
	mwCurrentPlacement2, _ := CreateManifestWork(mwrSet, "cls2", "place-test2")
	mwCurrentPlacement3, _ := CreateManifestWork(mwrSet, "cls3", "place-test3")

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, mwOldPlacement, mwCurrentPlacement2, mwCurrentPlacement3)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mwOldPlacement); err != nil {
		t.Fatal(err)
	}
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mwCurrentPlacement2); err != nil {
		t.Fatal(err)
	}
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mwCurrentPlacement3); err != nil {
		t.Fatal(err)
	}
	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet); err != nil {
		t.Fatal(err)
	}
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	// Create placement2 and placement3 (the current placements in the spec)
	placement2, placementDecision2 := helpertest.CreateTestPlacement("place-test2", "default", "cls2")
	placement3, placementDecision3 := helpertest.CreateTestPlacement("place-test3", "default", "cls3")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement2, placementDecision2, placement3, placementDecision3)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)

	if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement2); err != nil {
		t.Fatal(err)
	}
	if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision2); err != nil {
		t.Fatal(err)
	}
	if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement3); err != nil {
		t.Fatal(err)
	}
	if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision3); err != nil {
		t.Fatal(err)
	}

	placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
	placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()

	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: placementDecisionLister,
		placementLister:     placementLister,
	}

	// Run reconcile
	_, _, err := pmwDeployController.reconcile(context.TODO(), mwrSet)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that the ManifestWork from the old placement (place-test1) was deleted
	_, err = fWorkClient.WorkV1().ManifestWorks("cls1").Get(context.TODO(), mwrSet.Name, metav1.GetOptions{})
	if err == nil {
		t.Fatal("Expected ManifestWork for old placement to be deleted, but it still exists")
	}

	// Verify that ManifestWorks from current placements (place-test2 and place-test3) still exist
	currentMW2, err := fWorkClient.WorkV1().ManifestWorks("cls2").Get(context.TODO(), mwrSet.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected ManifestWork for placement2 to exist, but got error: %v", err)
	}
	assert.Equal(t, currentMW2.Labels[workapiv1alpha1.ManifestWorkReplicaSetPlacementNameLabelKey], "place-test2")

	currentMW3, err := fWorkClient.WorkV1().ManifestWorks("cls3").Get(context.TODO(), mwrSet.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected ManifestWork for placement3 to exist, but got error: %v", err)
	}
	assert.Equal(t, currentMW3.Labels[workapiv1alpha1.ManifestWorkReplicaSetPlacementNameLabelKey], "place-test3")
}
func TestDeployRolloutProgressingWhenNotAllSucceeded(t *testing.T) {
	// Test case where all ManifestWorks are created (count = total) but not all have succeeded
	// This should result in PlacementRolledOut = False with Reason = Progressing
	clusters := []string{"cls1", "cls2", "cls3"}
	placement, placementDecision := helpertest.CreateTestPlacement("place-test", "default", clusters...)
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement, placementDecision)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)

	err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement)
	assert.Nil(t, err)
	err = clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision)
	assert.Nil(t, err)

	placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
	placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()

	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")

	// Create ManifestWorks for all clusters
	mw1 := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, placement.Name, clusters[0])
	mw2 := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, placement.Name, clusters[1])
	mw3 := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, placement.Name, clusters[2])

	// Set cluster 1 as Succeeded
	apimeta.SetStatusCondition(&mw1.Status.Conditions, metav1.Condition{
		Type:               workapiv1.WorkApplied,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: mw1.Generation,
		Reason:             "Applied",
	})
	apimeta.SetStatusCondition(&mw1.Status.Conditions, metav1.Condition{
		Type:               workapiv1.WorkProgressing,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: mw1.Generation,
		Reason:             "Completed",
	})

	// Set cluster 2 as Progressing
	apimeta.SetStatusCondition(&mw2.Status.Conditions, metav1.Condition{
		Type:               workapiv1.WorkApplied,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: mw2.Generation,
		Reason:             "Applied",
	})
	apimeta.SetStatusCondition(&mw2.Status.Conditions, metav1.Condition{
		Type:               workapiv1.WorkProgressing,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: mw2.Generation,
		Reason:             "Applying",
	})

	// Set cluster 3 as Failed (Progressing=True + Degraded=True)
	apimeta.SetStatusCondition(&mw3.Status.Conditions, metav1.Condition{
		Type:               workapiv1.WorkApplied,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: mw3.Generation,
		Reason:             "Applied",
	})
	apimeta.SetStatusCondition(&mw3.Status.Conditions, metav1.Condition{
		Type:               workapiv1.WorkProgressing,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: mw3.Generation,
		Reason:             "Applying",
	})
	apimeta.SetStatusCondition(&mw3.Status.Conditions, metav1.Condition{
		Type:               workapiv1.WorkDegraded,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: mw3.Generation,
		Reason:             "ApplyFailed",
	})

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, mw1, mw2, mw3)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	err = workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw1)
	assert.Nil(t, err)
	err = workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw2)
	assert.Nil(t, err)
	err = workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw3)
	assert.Nil(t, err)
	err = workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet)
	assert.Nil(t, err)

	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: placementDecisionLister,
		placementLister:     placementLister,
	}

	mwrSet, _, err = pmwDeployController.reconcile(context.TODO(), mwrSet)
	assert.Nil(t, err)

	// Verify that Summary.Total equals the number of clusters
	assert.Equal(t, len(clusters), mwrSet.Status.Summary.Total, "Summary.Total should equal the number of clusters")

	// Verify PlacementRolledOut is False because not all clusters have succeeded
	// Only 1 out of 3 clusters has succeeded
	rollOutCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
	assert.NotNil(t, rollOutCondition)
	assert.Equal(t, metav1.ConditionFalse, rollOutCondition.Status, "PlacementRolledOut should be False when not all clusters have succeeded")
	assert.Equal(t, workapiv1alpha1.ReasonProgressing, rollOutCondition.Reason, "Reason should be Progressing when not all clusters have succeeded")
}

func TestClusterRolloutStatusFunc(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	now := metav1.Now()
	creationTime := metav1.NewTime(now.Add(-5 * time.Minute))

	tests := []struct {
		name                   string
		manifestWork           *workapiv1.ManifestWork
		expectedStatus         clustersdkv1alpha1.RolloutStatus
		expectedLastTransition *metav1.Time
	}{
		{
			name: "no progressing condition - should return ToApply",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        1,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedStatus:         clustersdkv1alpha1.ToApply,
			expectedLastTransition: nil,
		},
		{
			name: "progressing condition with unobserved generation - should return ToApply",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        2,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workapiv1.WorkProgressing,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
							LastTransitionTime: now,
							Reason:             "Completed",
						},
					},
				},
			},
			expectedStatus:         clustersdkv1alpha1.ToApply,
			expectedLastTransition: nil,
		},
		{
			name: "no Applied condition with stale Degraded - should return ToApply",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        2,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workapiv1.WorkProgressing,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Applying",
						},
						{
							Type:               workapiv1.WorkDegraded,
							Status:             metav1.ConditionTrue,
							LastTransitionTime: now,
							Reason:             "Failed",
							ObservedGeneration: 1,
						},
					},
				},
			},
			expectedStatus:         clustersdkv1alpha1.ToApply,
			expectedLastTransition: nil,
		},
		{
			name: "progressing true with current generation - should return Progressing",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        2,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workapiv1.WorkApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Applied",
						},
						{
							Type:               workapiv1.WorkProgressing,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Applying",
						},
					},
				},
			},
			expectedStatus:         clustersdkv1alpha1.Progressing,
			expectedLastTransition: &now,
		},
		{
			name: "progressing true and degraded true - should return Failed",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        2,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workapiv1.WorkApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Applied",
						},
						{
							Type:               workapiv1.WorkProgressing,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Applying",
						},
						{
							Type:               workapiv1.WorkDegraded,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Failed",
						},
					},
				},
			},
			expectedStatus:         clustersdkv1alpha1.Failed,
			expectedLastTransition: &now,
		},
		{
			name: "progressing false - should return Succeeded",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        2,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workapiv1.WorkApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Applied",
						},
						{
							Type:               workapiv1.WorkProgressing,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Completed",
						},
					},
				},
			},
			expectedStatus:         clustersdkv1alpha1.Succeeded,
			expectedLastTransition: &now,
		},
		{
			name: "progressing false with degraded true - should return Succeeded",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        2,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workapiv1.WorkApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Applied",
						},
						{
							Type:               workapiv1.WorkProgressing,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Completed",
						},
						{
							Type:               workapiv1.WorkDegraded,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Failed",
						},
					},
				},
			},
			expectedStatus:         clustersdkv1alpha1.Succeeded,
			expectedLastTransition: &now,
		},
		{
			name: "progressing unknown status - should return Progressing",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        2,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workapiv1.WorkApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Applied",
						},
						{
							Type:               workapiv1.WorkProgressing,
							Status:             metav1.ConditionUnknown,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Unknown",
						},
					},
				},
			},
			expectedStatus:         clustersdkv1alpha1.Progressing,
			expectedLastTransition: &now,
		},
		{
			name: "WorkApplied Status=False with current generation - should return ToApply",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        2,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workapiv1.WorkApplied,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "ApplyFailed",
						},
						{
							Type:               workapiv1.WorkProgressing,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Completed",
						},
					},
				},
			},
			expectedStatus:         clustersdkv1alpha1.ToApply,
			expectedLastTransition: nil,
		},
		{
			name: "WorkApplied Status=Unknown with current generation - should return ToApply",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        2,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workapiv1.WorkApplied,
							Status:             metav1.ConditionUnknown,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "ApplyStatusUnknown",
						},
						{
							Type:               workapiv1.WorkProgressing,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Completed",
						},
					},
				},
			},
			expectedStatus:         clustersdkv1alpha1.ToApply,
			expectedLastTransition: nil,
		},
		{
			name: "WorkApplied with stale ObservedGeneration (old gen 1, current gen 2) - should return ToApply",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        2,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workapiv1.WorkApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
							LastTransitionTime: now,
							Reason:             "Applied",
						},
						{
							Type:               workapiv1.WorkProgressing,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Completed",
						},
					},
				},
			},
			expectedStatus:         clustersdkv1alpha1.ToApply,
			expectedLastTransition: nil,
		},
		{
			name: "Progressing with old ObservedGeneration but newer WorkApplied - should return ToApply",
			manifestWork: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mw",
					Namespace:         "cls1",
					Generation:        3,
					CreationTimestamp: creationTime,
				},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workapiv1.WorkApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 3,
							LastTransitionTime: now,
							Reason:             "Applied",
						},
						{
							Type:               workapiv1.WorkProgressing,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 2,
							LastTransitionTime: now,
							Reason:             "Completed",
						},
					},
				},
			},
			expectedStatus:         clustersdkv1alpha1.ToApply,
			expectedLastTransition: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, tt.manifestWork)
			workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)
			mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

			placement, placementDecision := helpertest.CreateTestPlacement("place-test", "default", "cls1")
			fClusterClient := fakeclusterclient.NewSimpleClientset(placement, placementDecision)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)

			placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
			placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()

			reconciler := deployReconciler{
				workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
				manifestWorkLister:  mwLister,
				placeDecisionLister: placementDecisionLister,
				placementLister:     placementLister,
			}

			status, _ := reconciler.clusterRolloutStatusFunc(tt.manifestWork.Namespace, *tt.manifestWork)
			assert.Equal(t, tt.expectedStatus, status.Status)
			assert.Equal(t, tt.manifestWork.Namespace, status.ClusterName)
			assert.Equal(t, tt.expectedLastTransition, status.LastTransitionTime,
				"LastTransitionTime should match for test: %s", tt.name)
		})
	}
}

func TestShouldReturnToApply(t *testing.T) {
	now := metav1.Now()
	generation := int64(2)

	tests := []struct {
		name         string
		generation   int64
		appliedCond  *metav1.Condition
		progressCond *metav1.Condition
		degradedCond *metav1.Condition
		expected     bool
		description  string
	}{
		{
			name:         "applied condition is nil - should return true",
			generation:   generation,
			appliedCond:  nil,
			progressCond: &metav1.Condition{Type: workapiv1.WorkProgressing, Status: metav1.ConditionTrue, ObservedGeneration: generation},
			degradedCond: nil,
			expected:     true,
			description:  "When Applied condition doesn't exist, work hasn't been applied by hub controller yet",
		},
		{
			name:       "applied condition has stale generation - should return true",
			generation: generation,
			appliedCond: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				LastTransitionTime: now,
			},
			progressCond: &metav1.Condition{Type: workapiv1.WorkProgressing, Status: metav1.ConditionTrue, ObservedGeneration: generation},
			degradedCond: nil,
			expected:     true,
			description:  "When Applied condition hasn't observed latest spec, work needs to be reapplied",
		},
		{
			name:       "applied condition is false - should return true",
			generation: generation,
			appliedCond: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			progressCond: &metav1.Condition{Type: workapiv1.WorkProgressing, Status: metav1.ConditionTrue, ObservedGeneration: generation},
			degradedCond: nil,
			expected:     true,
			description:  "When Applied condition is False, work failed to apply",
		},
		{
			name:       "progressing condition is nil - should return true",
			generation: generation,
			appliedCond: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			progressCond: nil,
			degradedCond: nil,
			expected:     true,
			description:  "When Progressing condition doesn't exist, work hasn't been reconciled by agent yet",
		},
		{
			name:       "progressing condition has stale generation - should return true",
			generation: generation,
			appliedCond: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			progressCond: &metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				LastTransitionTime: now,
			},
			degradedCond: nil,
			expected:     true,
			description:  "When Progressing condition hasn't observed latest spec, agent hasn't reconciled the new work yet",
		},
		{
			name:       "degraded condition exists with stale generation - should return true",
			generation: generation,
			appliedCond: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			progressCond: &metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			degradedCond: &metav1.Condition{
				Type:               workapiv1.WorkDegraded,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				LastTransitionTime: now,
			},
			expected:    true,
			description: "When Degraded condition exists but hasn't observed latest spec, we wait for it to catch up",
		},
		{
			name:       "all conditions ready - should return false",
			generation: generation,
			appliedCond: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			progressCond: &metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			degradedCond: nil,
			expected:     false,
			description:  "When all conditions are current, proceed with rollout status evaluation",
		},
		{
			name:       "all conditions ready including degraded - should return false",
			generation: generation,
			appliedCond: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			progressCond: &metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			degradedCond: &metav1.Condition{
				Type:               workapiv1.WorkDegraded,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			expected:    false,
			description: "When all conditions including Degraded are current, proceed with rollout status evaluation",
		},
		{
			name:       "applied condition status unknown - should return true",
			generation: generation,
			appliedCond: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			progressCond: &metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			degradedCond: nil,
			expected:     true,
			description:  "When Applied condition status is Unknown even with current generation, work is not ready to apply",
		},
		{
			name:       "degraded ahead of applied generation - should return true",
			generation: generation,
			appliedCond: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				LastTransitionTime: now,
			},
			progressCond: &metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				LastTransitionTime: now,
			},
			degradedCond: &metav1.Condition{
				Type:               workapiv1.WorkDegraded,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			expected:    true,
			description: "When Degraded is ahead of Applied generation, wait for Applied to catch up",
		},
		{
			name:       "applied and progressing both stale but degraded current - should return true",
			generation: generation,
			appliedCond: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				LastTransitionTime: now,
			},
			progressCond: &metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				LastTransitionTime: now,
			},
			degradedCond: &metav1.Condition{
				Type:               workapiv1.WorkDegraded,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			expected:    true,
			description: "When both Applied and Progressing are stale, return ToApply regardless of Degraded status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldReturnToApply(tt.generation, tt.appliedCond, tt.progressCond, tt.degradedCond)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestIsConditionReady(t *testing.T) {
	now := metav1.Now()
	generation := int64(2)

	tests := []struct {
		name        string
		condition   *metav1.Condition
		generation  int64
		requireTrue bool
		expected    bool
		description string
	}{
		{
			name:        "nil condition - should return false",
			condition:   nil,
			generation:  generation,
			requireTrue: false,
			expected:    false,
			description: "When condition doesn't exist, it's not ready",
		},
		{
			name: "stale observed generation - should return false",
			condition: &metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				LastTransitionTime: now,
			},
			generation:  generation,
			requireTrue: false,
			expected:    false,
			description: "When condition hasn't observed latest generation, it's not ready",
		},
		{
			name: "current generation but status not true when required - should return false",
			condition: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			generation:  generation,
			requireTrue: true,
			expected:    false,
			description: "When requireTrue is set and status is False, condition is not ready",
		},
		{
			name: "current generation but status unknown when required true - should return false",
			condition: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			generation:  generation,
			requireTrue: true,
			expected:    false,
			description: "When requireTrue is set and status is Unknown, condition is not ready",
		},
		{
			name: "current generation and status true when required - should return true",
			condition: &metav1.Condition{
				Type:               workapiv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			generation:  generation,
			requireTrue: true,
			expected:    true,
			description: "When condition has current generation and status is True when required, it's ready",
		},
		{
			name: "current generation and status false when not requiring true - should return true",
			condition: &metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			generation:  generation,
			requireTrue: false,
			expected:    true,
			description: "When requireTrue is false, any status with current generation is ready",
		},
		{
			name: "current generation and status true when not requiring true - should return true",
			condition: &metav1.Condition{
				Type:               workapiv1.WorkProgressing,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: now,
			},
			generation:  generation,
			requireTrue: false,
			expected:    true,
			description: "When requireTrue is false, any status with current generation is ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isConditionReady(tt.condition, tt.generation, tt.requireTrue)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}
