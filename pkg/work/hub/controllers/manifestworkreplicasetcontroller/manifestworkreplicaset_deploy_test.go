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

	// Check the RollOut conditions
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

	// Check the RollOut conditions
	rollOutCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
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
	apimeta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionFalse, Reason: "ApplyTest"})
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
