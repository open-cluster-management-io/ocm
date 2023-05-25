package manifestworkreplicasetcontroller

import (
	"context"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	"open-cluster-management.io/api/utils/work/v1/workapplier"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	helpertest "open-cluster-management.io/work/pkg/hub/test"
	"testing"
	"time"
)

func TestDeployReconcileAsExpected(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	mw, _ := CreateManifestWork(mwrSet, "cls1")
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
	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, string(workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified))

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
	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, string(workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified))

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
	if err == nil {
		t.Fatal("Expected Not Found Error ", err)
	}

	// Check the PlacedManifestWork conditions
	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, string(workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified))

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
