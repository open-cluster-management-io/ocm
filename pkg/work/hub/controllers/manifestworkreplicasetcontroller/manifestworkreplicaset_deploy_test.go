package manifestworkreplicasetcontroller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	clienttesting "k8s.io/client-go/testing"

	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	clustersdkv1alpha1 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1alpha1"
	workapplier "open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	workhelper "open-cluster-management.io/ocm/pkg/work/helper"
	helpertest "open-cluster-management.io/ocm/pkg/work/hub/test"
)

func TestDeployReconcileAsExpected(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	mw := buildManifestWork(mwrSet, "", "cls1", "place-test")
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

// TestDeployReconcileApplyFailureReportsNotAsExpected reproduces
// https://github.com/open-cluster-management-io/ocm/issues/1522: a Placement selects a real
// cluster, but every ManifestWork Create for it is rejected by the apiserver. Before the fix,
// count stayed 0 and the PlacementVerified condition was misreported as PlacementDecisionEmpty,
// even though the placement decision was not empty at all.
func TestDeployReconcileApplyFailureReportsNotAsExpected(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet)

	applyErr := errors.New("Deployment.apps \"test\" is invalid: spec.replicas: cannot unmarshal string into Go struct field")
	fWorkClient.PrependReactor("create", "manifestworks", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, applyErr
	})

	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Minute)
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	// One real cluster is selected by the placement.
	placement, placementDecision := helpertest.CreateTestPlacement("place-test", "default", "cls1")
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
	if err == nil {
		t.Fatal("expected reconcile to return the aggregated apply error")
	}

	if mwrSet.Status.Summary.Total != 0 {
		t.Fatal("expected Summary.Total to be 0 since every apply failed ", mwrSet.Status.Summary)
	}

	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
	if placeCondition == nil {
		t.Fatal("Placement condition not found ", mwrSet.Status.Conditions)
	}

	// This is the actual bug: an apply failure on a real, selected cluster must not be
	// reported the same way as "the placement selected no clusters."
	if placeCondition.Reason != workapiv1alpha1.ReasonNotAsExpected {
		t.Fatal("Placement condition Reason should be NotAsExpected, not PlacementDecisionEmpty ", placeCondition)
	}
	if !strings.Contains(placeCondition.Message, applyErr.Error()) {
		t.Fatalf("Placement condition message = %q, want it to contain the apply error %q", placeCondition.Message, applyErr.Error())
	}
}

// TestDeployReconcileMultiPlacementApplyFailuresAreAggregated is the multi-placement variant of
// TestDeployReconcileApplyFailureReportsNotAsExpected: reconcile processes every PlacementRef in
// one pass and only makes the PlacementVerified decision once, at the end, so the errors from
// every placement must reach the condition message, not just the last one processed.
func TestDeployReconcileMultiPlacementApplyFailuresAreAggregated(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-a", "place-b")
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet)

	// Each cluster's apply error includes its own namespace so the two failures produce
	// distinct messages: utilerrors.NewAggregate deduplicates identical error strings, and a
	// single shared message would pass this test even if only the last placement's error
	// made it into the condition.
	fWorkClient.PrependReactor("create", "manifestworks", func(action clienttesting.Action) (bool, runtime.Object, error) {
		mw, _ := action.(clienttesting.CreateAction).GetObject().(*workapiv1.ManifestWork)
		return true, nil, fmt.Errorf("apply rejected for %s", mw.Namespace)
	})

	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Minute)
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	placementA, placementDecisionA := helpertest.CreateTestPlacement("place-a", "default", "cls1")
	placementB, placementDecisionB := helpertest.CreateTestPlacement("place-b", "default", "cls2")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placementA, placementDecisionA, placementB, placementDecisionB)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Minute)

	for _, obj := range []runtime.Object{placementA, placementB} {
		if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(obj); err != nil {
			t.Fatal(err)
		}
	}
	for _, obj := range []runtime.Object{placementDecisionA, placementDecisionB} {
		if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(obj); err != nil {
			t.Fatal(err)
		}
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
		t.Fatal("expected reconcile to return the aggregated apply errors")
	}

	if mwrSet.Status.Summary.Total != 0 {
		t.Fatal("expected Summary.Total to be 0 since every apply failed in both placements ", mwrSet.Status.Summary)
	}

	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
	if placeCondition == nil {
		t.Fatal("Placement condition not found ", mwrSet.Status.Conditions)
	}
	if placeCondition.Reason != workapiv1alpha1.ReasonNotAsExpected {
		t.Fatal("Placement condition Reason should be NotAsExpected, not PlacementDecisionEmpty ", placeCondition)
	}
	// Both placements' apply errors should have reached the condition, not just the last one.
	if !strings.Contains(placeCondition.Message, "apply rejected for cls1") ||
		!strings.Contains(placeCondition.Message, "apply rejected for cls2") {
		t.Fatalf("expected the aggregated message to mention both failures, got %q", placeCondition.Message)
	}
}

// TestDeployReconcilePartialApplyFailureReportsNotAsExpected covers a placement that selects two
// clusters where the apply succeeds on one and fails on the other. count > 0 in that case, but the
// failure must still be reported instead of PlacementVerified=AsExpected.
func TestDeployReconcilePartialApplyFailureReportsNotAsExpected(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet)

	fWorkClient.PrependReactor("create", "manifestworks", func(action clienttesting.Action) (bool, runtime.Object, error) {
		mw, _ := action.(clienttesting.CreateAction).GetObject().(*workapiv1.ManifestWork)
		if mw.Namespace == "cls2" {
			return true, nil, fmt.Errorf("namespaces %q not found", mw.Namespace)
		}
		return false, nil, nil
	})

	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Minute)
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	placement, placementDecision := helpertest.CreateTestPlacement("place-test", "default", "cls1", "cls2")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement, placementDecision)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Minute)

	if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement); err != nil {
		t.Fatal(err)
	}
	if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision); err != nil {
		t.Fatal(err)
	}

	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister(),
		placementLister:     clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
	}

	mwrSet, _, err := pmwDeployController.reconcile(context.TODO(), mwrSet)
	if err == nil {
		t.Fatal("expected reconcile to return the apply error for cls2")
	}

	if mwrSet.Status.Summary.Total != 1 {
		t.Fatal("expected Summary.Total to be 1 since only the cls1 apply succeeded ", mwrSet.Status.Summary)
	}

	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
	if placeCondition == nil {
		t.Fatal("Placement condition not found ", mwrSet.Status.Conditions)
	}
	if placeCondition.Reason != workapiv1alpha1.ReasonNotAsExpected {
		t.Fatal("Placement condition Reason should be NotAsExpected when any apply fails ", placeCondition)
	}
	if !strings.Contains(placeCondition.Message, `namespaces "cls2" not found`) {
		t.Fatalf("expected the condition message to mention the cls2 failure, got %q", placeCondition.Message)
	}
}

// TestDeployReconcileRolloutErrorNotOverwrittenByOtherPlacement covers two placements where one
// fails to compute its rollout and the other rolls out fine. The NotAsExpected set for the failing
// placement must not be overwritten with AsExpected because the other placement produced works.
func TestDeployReconcileRolloutErrorNotOverwrittenByOtherPlacement(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSetWithRollOutStrategy("mwrSet-test", "default",
		map[string]clusterv1alpha1.RolloutStrategy{
			"place-bad":  {Type: "BogusStrategyType"},
			"place-good": {Type: clusterv1alpha1.All},
		})
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Minute)
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	placementBad, placementDecisionBad := helpertest.CreateTestPlacement("place-bad", "default", "cls1")
	placementGood, placementDecisionGood := helpertest.CreateTestPlacement("place-good", "default", "cls2")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placementBad, placementDecisionBad, placementGood, placementDecisionGood)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Minute)

	for _, obj := range []runtime.Object{placementBad, placementGood} {
		if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(obj); err != nil {
			t.Fatal(err)
		}
	}
	for _, obj := range []runtime.Object{placementDecisionBad, placementDecisionGood} {
		if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(obj); err != nil {
			t.Fatal(err)
		}
	}

	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister(),
		placementLister:     clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
	}

	mwrSet, _, err := pmwDeployController.reconcile(context.TODO(), mwrSet)
	if err == nil {
		t.Fatal("expected reconcile to return the rollout strategy error")
	}

	if mwrSet.Status.Summary.Total != 1 {
		t.Fatal("expected Summary.Total to be 1 from place-good ", mwrSet.Status.Summary)
	}

	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
	if placeCondition == nil {
		t.Fatal("Placement condition not found ", mwrSet.Status.Conditions)
	}
	if placeCondition.Reason != workapiv1alpha1.ReasonNotAsExpected {
		t.Fatal("Placement condition Reason should stay NotAsExpected, not be overwritten with AsExpected ", placeCondition)
	}
}

// TestDeployReconcileInvalidRolloutStrategyReportsNotAsExpected covers a second, related way the
// same count == 0 block used to misreport PlacementDecisionEmpty: rolloutHandler.GetRolloutCluster
// failing (e.g. on an invalid rollout strategy type) already set PlacementVerified=NotAsExpected
// once, but the final count == 0 block at the end of reconcile unconditionally overwrote it with
// PlacementDecisionEmpty.
func TestDeployReconcileInvalidRolloutStrategyReportsNotAsExpected(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSetWithRollOutStrategy("mwrSet-test", "default",
		map[string]clusterv1alpha1.RolloutStrategy{
			"place-test": {Type: "BogusStrategyType"},
		})
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Minute)
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	placement, placementDecision := helpertest.CreateTestPlacement("place-test", "default", "cls1")
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
	if err == nil {
		t.Fatal("expected reconcile to return the rollout strategy error")
	}

	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
	if placeCondition == nil {
		t.Fatal("Placement condition not found ", mwrSet.Status.Conditions)
	}
	if placeCondition.Reason != workapiv1alpha1.ReasonNotAsExpected {
		t.Fatal("Placement condition Reason should stay NotAsExpected, not be overwritten with PlacementDecisionEmpty ", placeCondition)
	}
}

// TestDeployReconcileSelectedClustersButEmptyDecision covers the case where the Placement status
// already reports selected clusters but the PlacementDecision has no clusters yet (for example
// informer lag). Nothing is attempted and nothing fails, so this is still a genuine
// PlacementDecisionEmpty and must not be reported as NotAsExpected or panic on an empty error list.
func TestDeployReconcileSelectedClustersButEmptyDecision(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Minute)
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	placement, _ := helpertest.CreateTestPlacement("place-test", "default", "cls1")
	_, emptyDecision := helpertest.CreateTestPlacement("place-test", "default")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement, emptyDecision)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Minute)

	if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement); err != nil {
		t.Fatal(err)
	}
	if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(emptyDecision); err != nil {
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

	placeCondition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
	if placeCondition == nil {
		t.Fatal("Placement condition not found ", mwrSet.Status.Conditions)
	}
	if placeCondition.Reason != workapiv1alpha1.ReasonPlacementDecisionEmpty {
		t.Fatal("Placement condition Reason should be PlacementDecisionEmpty when nothing was attempted and nothing failed ", placeCondition)
	}
}

func TestAggregatedErrorMessage(t *testing.T) {
	t.Run("no errors yields an empty message", func(t *testing.T) {
		if msg := aggregatedErrorMessage(nil); msg != "" {
			t.Fatalf("got %q, want empty", msg)
		}
	})

	t.Run("short message is returned unchanged", func(t *testing.T) {
		msg := aggregatedErrorMessage([]error{errors.New("boom")})
		if msg != "boom" {
			t.Fatalf("got %q, want %q", msg, "boom")
		}
	})

	t.Run("long message is truncated with a count", func(t *testing.T) {
		longErr := errors.New(strings.Repeat("x", maxAggregatedErrorMessageLen*2))
		msg := aggregatedErrorMessage([]error{longErr, errors.New("second")})

		if len(msg) > maxAggregatedErrorMessageLen {
			t.Fatalf("expected message to be capped at %d, got length %d", maxAggregatedErrorMessageLen, len(msg))
		}
		// utilerrors.NewAggregate wraps multiple distinct messages as "[msg1, msg2]", so the
		// retained prefix comes from that combined string, not from longErr alone.
		untruncated := utilerrors.NewAggregate([]error{longErr, errors.New("second")}).Error()
		if !strings.HasPrefix(untruncated, strings.TrimSuffix(msg, "... (truncated; 2 errors total)")) {
			t.Fatal("expected message to start with a prefix of the aggregated error")
		}
		if !strings.Contains(msg, "2 errors total") {
			t.Fatalf("expected message to note the total error count, got %q", msg)
		}
	})
}

func TestAggregatedErrorMessageUTF8(t *testing.T) {
	for _, tt := range []struct {
		name    string
		message string
	}{
		{name: "short Unicode message", message: "é界🙂"},
		{name: "ASCII at byte limit", message: strings.Repeat("x", maxAggregatedErrorMessageLen)},
		{name: "Unicode at byte limit", message: strings.Repeat("é", maxAggregatedErrorMessageLen/len("é"))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.message, aggregatedErrorMessage([]error{errors.New(tt.message)}))
		})
	}

	const suffix = "... (truncated; 1 errors total)"
	for _, character := range []string{"é", "界", "🙂"} {
		for cutInRune := 0; cutInRune <= len(character); cutInRune++ {
			t.Run(fmt.Sprintf("%d-byte character cut at %d", len(character), cutInRune), func(t *testing.T) {
				prefix := strings.Repeat("x", maxAggregatedErrorMessageLen-len(suffix)-cutInRune)
				message := prefix + character + strings.Repeat("x", len(suffix)+1)
				wantMessage := prefix
				if cutInRune == len(character) {
					wantMessage += character
				}
				wantMessage += suffix

				got := aggregatedErrorMessage([]error{errors.New(message)})
				assert.True(t, utf8.ValidString(got), "truncation must not split a UTF-8 character")
				assert.LessOrEqual(t, len(got), maxAggregatedErrorMessageLen)
				assert.Equal(t, wantMessage, got)
			})
		}
	}
}

func TestDeployReconcileMissingPlacementMessageUTF8(t *testing.T) {
	const prefix = "placements not found in namespace default: "
	const suffix = "... (truncated; 1 placements missing)"
	for _, character := range []string{"é", "界", "🙂"} {
		for cutInRune := 0; cutInRune <= len(character); cutInRune++ {
			t.Run(fmt.Sprintf("%d-byte character cut at %d", len(character), cutInRune), func(t *testing.T) {
				namePrefix := strings.Repeat("x", maxAggregatedErrorMessageLen-len(suffix)-len(prefix)-cutInRune)
				name := namePrefix + character + strings.Repeat("x", len(suffix)+1)
				mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", name)
				fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet)
				workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, time.Minute)
				mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
				clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fakeclusterclient.NewSimpleClientset(), time.Minute)
				reconciler := deployReconciler{
					workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
					manifestWorkLister:  mwLister,
					placeDecisionLister: clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister(),
					placementLister:     clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
				}

				_, _, err := reconciler.reconcile(context.TODO(), mwrSet)
				if err != nil {
					t.Fatal(err)
				}
				wantMessage := prefix + namePrefix
				if cutInRune == len(character) {
					wantMessage += character
				}
				wantMessage += suffix
				if assert.Len(t, mwrSet.Status.Conditions, 2) {
					for _, condition := range mwrSet.Status.Conditions {
						assert.True(t, utf8.ValidString(condition.Message), "truncation must not split a UTF-8 character")
						assert.LessOrEqual(t, len(condition.Message), maxAggregatedErrorMessageLen)
						assert.Equal(t, wantMessage, condition.Message)
					}
				}
			})
		}
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

func TestDeployReconcileMissingPlacementContinuesWithValidPlacement(t *testing.T) {
	for _, tt := range []struct {
		name         string
		missingFirst bool
		missingCount int
		applyFailure bool
	}{
		{name: "missing placement first", missingFirst: true, missingCount: 1},
		{name: "missing placement last", missingCount: 1},
		{name: "missing placement with apply failure", missingCount: 1, applyFailure: true},
		{name: "many missing placements", missingFirst: true, missingCount: 520},
		{name: "many missing placements with apply failure", missingCount: 520, applyFailure: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "valid")
			mwrSet.Generation = 2
			var missingRefs []workapiv1alpha1.LocalPlacementReference
			for i := 0; i < tt.missingCount; i++ {
				missingRef := mwrSet.Spec.PlacementRefs[0]
				missingRef.Name = "missing"
				if i > 0 {
					missingRef.Name = fmt.Sprintf("missing-%03d-%s", i, strings.Repeat("a", 51))
				}
				missingRefs = append(missingRefs, missingRef)
			}
			if tt.missingFirst {
				mwrSet.Spec.PlacementRefs = append(missingRefs, mwrSet.Spec.PlacementRefs...)
			} else {
				mwrSet.Spec.PlacementRefs = append(mwrSet.Spec.PlacementRefs, missingRefs...)
			}
			missingWork := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, "missing", "cls-missing")

			fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, missingWork)
			applyErr := fmt.Errorf("apply rejected: %s", strings.Repeat("x", maxAggregatedErrorMessageLen*2))
			if tt.applyFailure {
				fWorkClient.PrependReactor("create", "manifestworks", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, applyErr
				})
			}
			workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, time.Minute)
			if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(missingWork); err != nil {
				t.Fatal(err)
			}
			mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

			placement, decision := helpertest.CreateTestPlacement("valid", "default", "cls1")
			fClusterClient := fakeclusterclient.NewSimpleClientset(placement, decision)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, time.Minute)
			if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement); err != nil {
				t.Fatal(err)
			}
			if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(decision); err != nil {
				t.Fatal(err)
			}

			deployReconciler := deployReconciler{
				workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
				manifestWorkLister:  mwLister,
				placeDecisionLister: clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister(),
				placementLister:     clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
			}

			mwrSet, state, err := deployReconciler.reconcile(context.TODO(), mwrSet)
			wantTotal := 1
			if tt.applyFailure {
				assert.Error(t, err)
				wantTotal = 0
			} else if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, reconcileContinue, state)

			created, deleted := 0, 0
			for _, action := range fWorkClient.Actions() {
				switch {
				case action.Matches("create", "manifestworks"):
					created++
				case action.Matches("delete", "manifestworks"):
					deleted++
				}
			}
			assert.Equal(t, 1, created)
			assert.Equal(t, 0, deleted)
			assert.Equal(t, workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: wantTotal}, mwrSet.Status.Summary)
			if assert.Len(t, mwrSet.Status.PlacementsSummary, 1) {
				assert.Equal(t, "valid", mwrSet.Status.PlacementsSummary[0].Name)
				assert.Equal(t, workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: wantTotal}, mwrSet.Status.PlacementsSummary[0].Summary)
			}

			verified := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
			if assert.NotNil(t, verified) {
				assert.Equal(t, metav1.ConditionFalse, verified.Status)
				assert.Equal(t, workapiv1alpha1.ReasonPlacementDecisionNotFound, verified.Reason)
				assert.Equal(t, mwrSet.Generation, verified.ObservedGeneration)
				assert.Contains(t, verified.Message, "missing")
			}
			rolledOut := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut)
			if assert.NotNil(t, rolledOut) {
				assert.Equal(t, metav1.ConditionFalse, rolledOut.Status)
				assert.Equal(t, workapiv1alpha1.ReasonProgressing, rolledOut.Reason)
				assert.Equal(t, mwrSet.Generation, rolledOut.ObservedGeneration)
				assert.Contains(t, rolledOut.Message, "missing")
				assert.LessOrEqual(t, len(rolledOut.Message), maxAggregatedErrorMessageLen)
				if tt.missingCount > 1 {
					assert.Contains(t, rolledOut.Message, fmt.Sprintf("... (truncated; %d placements missing)", tt.missingCount))
				} else {
					assert.Equal(t, "placements not found in namespace default: missing", rolledOut.Message)
				}
				if verified != nil {
					wantMessage := rolledOut.Message
					if tt.applyFailure {
						wantMessage += "; " + aggregatedErrorMessage([]error{applyErr})
					}
					assert.Equal(t, wantMessage, verified.Message)
					assert.LessOrEqual(t, len(verified.Message), 2*maxAggregatedErrorMessageLen+len("; "))
				}
			}
		})
	}
}

func TestDeployReconcileMissingPlacementOverlap(t *testing.T) {
	for _, tt := range []struct {
		name             string
		missingFirst     bool
		rolloutType      clusterv1alpha1.RolloutType
		oldTemplate      bool
		noConditions     bool
		deleting         bool
		onlyOverlap      bool
		removeMissingRef bool
	}{
		{name: "missing first", missingFirst: true},
		{name: "missing last"},
		{name: "progressive missing first", missingFirst: true, rolloutType: clusterv1alpha1.Progressive},
		{name: "progressive missing last", rolloutType: clusterv1alpha1.Progressive},
		{name: "per group missing first", missingFirst: true, rolloutType: clusterv1alpha1.ProgressivePerGroup},
		{name: "per group missing last", rolloutType: clusterv1alpha1.ProgressivePerGroup},
		{name: "old template is preserved", oldTemplate: true, rolloutType: clusterv1alpha1.Progressive},
		{name: "unreported work is preserved", noConditions: true},
		{name: "deleting work is not replaced", deleting: true},
		{name: "only overlapping cluster", onlyOverlap: true},
		{name: "removed missing reference permits replacement", removeMissingRef: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrs-test", "default", "valid")
			mwrSet.Generation = 2
			validRef := mwrSet.Spec.PlacementRefs[0]
			if tt.rolloutType != "" {
				validRef.RolloutStrategy = clusterv1alpha1.RolloutStrategy{Type: tt.rolloutType}
				if tt.rolloutType == clusterv1alpha1.Progressive {
					validRef.RolloutStrategy.Progressive = &clusterv1alpha1.RolloutProgressive{MaxConcurrency: intstr.FromInt(1)}
				}
			}
			missingRef := validRef
			missingRef.Name = "missing"
			mwrSet.Spec.PlacementRefs = []workapiv1alpha1.LocalPlacementReference{validRef, missingRef}
			if tt.missingFirst {
				mwrSet.Spec.PlacementRefs = []workapiv1alpha1.LocalPlacementReference{missingRef, validRef}
			}
			missingWork := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, "missing", "cls0")
			if tt.oldTemplate {
				missingWork.Spec = helpertest.CreateTestManifestWorkSpecWithSecret("v1", "Secret", "default", "old")
			}
			if tt.noConditions {
				missingWork.Status.Conditions = nil
			}
			if tt.deleting {
				now := metav1.Now()
				missingWork.DeletionTimestamp = &now
			}
			originalWork := missingWork.DeepCopy()
			fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, missingWork)
			createCount := 0
			fWorkClient.PrependReactor("create", "manifestworks", func(action clienttesting.Action) (bool, runtime.Object, error) {
				// The fake client does not expand GenerateName like the API server does.
				work := action.(clienttesting.CreateAction).GetObject().(*workapiv1.ManifestWork)
				if work.Name == "" {
					createCount++
					work.Name = fmt.Sprintf("%s-%d", work.GenerateName, createCount)
				}
				return false, nil, nil
			})
			workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, time.Minute)
			workStore := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore()
			if err := workStore.Add(missingWork); err != nil {
				t.Fatal(err)
			}
			mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
			refreshWorkCache := func() {
				t.Helper()
				works, err := fWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
				if err != nil {
					t.Fatal(err)
				}
				objects := make([]interface{}, 0, len(works.Items))
				for i := range works.Items {
					objects = append(objects, &works.Items[i])
				}
				if err := workStore.Replace(objects, ""); err != nil {
					t.Fatal(err)
				}
			}

			clusters := []string{"cls0", "cls1"}
			wantTotal := 1
			if tt.onlyOverlap {
				clusters = []string{"cls0"}
				wantTotal = 0
			}
			placement, decisions := helpertest.CreateTestPlacementWithDecisionStrategy("valid", "default", 1, clusters...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fakeclusterclient.NewSimpleClientset(placement), time.Minute)
			if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement); err != nil {
				t.Fatal(err)
			}
			for _, decision := range decisions {
				if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(decision); err != nil {
					t.Fatal(err)
				}
			}
			reconciler := deployReconciler{
				workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
				manifestWorkLister:  mwLister,
				placeDecisionLister: clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister(),
				placementLister:     clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
			}

			_, state, err := reconciler.reconcile(ctx, mwrSet)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, reconcileContinue, state)
			created := 0
			for _, action := range fWorkClient.Actions() {
				if action.Matches("create", "manifestworks") {
					created++
					assert.Equal(t, "cls1", action.GetNamespace(), "only the non-overlapping cluster should get a new work")
				}
				assert.False(t, action.Matches("patch", "manifestworks"), "the retained work must not be updated or relabeled")
				assert.False(t, action.Matches("delete", "manifestworks"), "the retained work must not be deleted")
			}
			assert.Equal(t, wantTotal, created)
			assert.Equal(t, workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: wantTotal}, mwrSet.Status.Summary)
			if assert.Len(t, mwrSet.Status.PlacementsSummary, 1) {
				assert.Equal(t, "valid", mwrSet.Status.PlacementsSummary[0].Name)
				assert.Equal(t, workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: wantTotal}, mwrSet.Status.PlacementsSummary[0].Summary)
			}
			assert.Equal(t, originalWork, missingWork, "the informer object must remain unchanged")

			status := statusReconciler{manifestWorkLister: mwLister}
			if _, _, err := status.reconcile(ctx, mwrSet); err != nil {
				t.Fatal(err)
			}
			applied := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied)
			if assert.NotNil(t, applied) {
				assert.Equal(t, metav1.ConditionFalse, applied.Status)
				assert.Equal(t, workapiv1alpha1.ReasonNotAsExpected, applied.Reason)
				assert.Equal(t, mwrSet.Generation, applied.ObservedGeneration)
			}

			refreshWorkCache()
			fWorkClient.ClearActions()
			if _, _, err := reconciler.reconcile(ctx, mwrSet); err != nil {
				t.Fatal(err)
			}
			assert.Empty(t, fWorkClient.Actions(), "a repeated sync must not create another work")

			// Removing the valid reference must not delete work retained for the missing reference.
			mwrSet.Spec.PlacementRefs = []workapiv1alpha1.LocalPlacementReference{missingRef}
			if tt.removeMissingRef {
				mwrSet.Spec.PlacementRefs = []workapiv1alpha1.LocalPlacementReference{validRef}
			}
			if _, _, err := reconciler.reconcile(ctx, mwrSet); err != nil {
				t.Fatal(err)
			}
			works, err := fWorkClient.WorkV1().ManifestWorks("cls0").List(ctx, metav1.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if assert.Len(t, works.Items, 1) {
				if tt.removeMissingRef {
					assert.NotEqual(t, originalWork.Name, works.Items[0].Name)
					assert.Equal(t, "valid", works.Items[0].Labels[workapiv1alpha1.ManifestWorkReplicaSetPlacementNameLabelKey])
				} else {
					assert.Equal(t, *originalWork, works.Items[0])
				}
			}

			// Retention ends when the owning reference is removed from the spec.
			refreshWorkCache()
			mwrSet.Spec.PlacementRefs = nil
			if _, _, err := reconciler.reconcile(ctx, mwrSet); err != nil {
				t.Fatal(err)
			}
			works, err = fWorkClient.WorkV1().ManifestWorks(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			assert.Empty(t, works.Items)
		})
	}
}

// Control which work wins the cluster index without relying on informer list order.
type orderedManifestWorkLister struct {
	worklisterv1.ManifestWorkLister
	reverse bool
}

func (l orderedManifestWorkLister) List(selector labels.Selector) ([]*workapiv1.ManifestWork, error) {
	works, err := l.ManifestWorkLister.List(selector)
	sort.Slice(works, func(i, j int) bool {
		if l.reverse {
			return works[i].Name > works[j].Name
		}
		return works[i].Name < works[j].Name
	})
	return works, err
}

func TestDeployReconcileOwnWorkOnRetainedCluster(t *testing.T) {
	for _, tt := range []struct {
		name        string
		rolloutType clusterv1alpha1.RolloutType
		deleting    bool
	}{
		{name: "all", rolloutType: clusterv1alpha1.All},
		{name: "progressive", rolloutType: clusterv1alpha1.Progressive},
		{name: "per group", rolloutType: clusterv1alpha1.ProgressivePerGroup},
		{name: "deleting own work", rolloutType: clusterv1alpha1.All, deleting: true},
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%t", tt.name, reverse), func(t *testing.T) {
				ctx := context.Background()
				mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrs-test", "default", "valid")
				mwrSet.Spec.PlacementRefs[0].RolloutStrategy = clusterv1alpha1.RolloutStrategy{Type: tt.rolloutType}
				missingRef := mwrSet.Spec.PlacementRefs[0]
				missingRef.Name = "missing"
				mwrSet.Spec.PlacementRefs = append([]workapiv1alpha1.LocalPlacementReference{missingRef}, mwrSet.Spec.PlacementRefs...)

				ownWork := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, "valid", "cls0")
				ownWork.Name, ownWork.UID = "a-valid", "valid-work-uid"
				ownWork.Spec = helpertest.CreateTestManifestWorkSpecWithSecret("v1", "Secret", "default", "old")
				if tt.deleting {
					now := metav1.NewTime(time.Now().UTC().Truncate(time.Second))
					ownWork.DeletionTimestamp = &now
					ownWork.Finalizers = []string{workapiv1.ManifestWorkFinalizer}
				}
				missingWork := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, "missing", "cls0")
				missingWork.Name, missingWork.UID = "z-retained", "retained-work-uid"
				originalOwnWork, originalMissingWork := ownWork.DeepCopy(), missingWork.DeepCopy()
				fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, ownWork, missingWork)
				workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, time.Minute)
				workStore := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore()
				for _, work := range []*workapiv1.ManifestWork{ownWork, missingWork} {
					if err := workStore.Add(work); err != nil {
						t.Fatal(err)
					}
				}
				mwLister := orderedManifestWorkLister{
					ManifestWorkLister: workInformerFactory.Work().V1().ManifestWorks().Lister(),
					reverse:            reverse,
				}

				placement, decision := helpertest.CreateTestPlacement("valid", "default", "cls0")
				clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fakeclusterclient.NewSimpleClientset(placement, decision), time.Minute)
				if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement); err != nil {
					t.Fatal(err)
				}
				if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(decision); err != nil {
					t.Fatal(err)
				}
				reconciler := deployReconciler{
					workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
					manifestWorkLister:  mwLister,
					placeDecisionLister: clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister(),
					placementLister:     clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
				}

				for sync := 0; sync < 2; sync++ {
					fWorkClient.ClearActions()
					_, state, err := reconciler.reconcile(ctx, mwrSet)
					if err != nil {
						t.Fatal(err)
					}
					assert.Equal(t, reconcileContinue, state)
					patches := 0
					for _, action := range fWorkClient.Actions() {
						assert.False(t, action.Matches("create", "manifestworks"), "must not create a third work")
						assert.False(t, action.Matches("delete", "manifestworks"), "must preserve both existing works")
						if action.Matches("patch", "manifestworks") {
							patches++
							assert.Equal(t, ownWork.Name, action.(clienttesting.PatchAction).GetName())
						}
					}
					assert.Equal(t, 1-sync, patches, "update once, then leave the current work unchanged")
					updated, err := fWorkClient.WorkV1().ManifestWorks("cls0").Get(ctx, ownWork.Name, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					assert.True(t, workapplier.ManifestWorkEqual(buildManifestWork(mwrSet, ownWork.Name, "cls0", "valid"), updated))
					assert.Equal(t, ownWork.UID, updated.UID)
					assert.Equal(t, ownWork.Finalizers, updated.Finalizers)
					assert.True(t, ownWork.DeletionTimestamp.Equal(updated.DeletionTimestamp), "must preserve the deletion timestamp")
					retained, err := fWorkClient.WorkV1().ManifestWorks("cls0").Get(ctx, missingWork.Name, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					assert.Equal(t, originalMissingWork, retained)
					works, err := fWorkClient.WorkV1().ManifestWorks("cls0").List(ctx, metav1.ListOptions{})
					if err != nil {
						t.Fatal(err)
					}
					assert.Len(t, works.Items, 2)
					assert.Equal(t, originalOwnWork, ownWork, "must not mutate the informer object")
					assert.Equal(t, originalMissingWork, missingWork, "must not mutate the informer object")
					if err := workStore.Update(updated); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
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
	mwrSet.Generation = 1

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
	assert.Equal(t, int64(1), placeCondition.ObservedGeneration)

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
	assert.Equal(t, int64(1), rollOutCondition.ObservedGeneration)

	// Change the mwrSet spec and re-run deploy.
	mwTemplate := helpertest.CreateTestManifestWorkSpecWithSecret("v2", "test", "ns-test", "name-test")
	mwTemplate.DeepCopyInto(&mwrSet.Spec.ManifestWorkTemplate)
	mwrSet.Generation = 2

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
	assert.Equal(t, int64(2), rollOutCondition.ObservedGeneration)
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
	mw := buildManifestWork(mwrSet, "", "cls1", "place-test")
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
	mwOldPlacement := buildManifestWork(mwrSet, "", "cls1", "place-test1")
	mwCurrentPlacement := buildManifestWork(mwrSet, "", "cls2", "place-test2")

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
	works, err := listWorksByMWRS(context.TODO(), fWorkClient, "cls1", mwrSet.Namespace, mwrSet.Name, "place-test1")
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 0 {
		t.Fatalf("Expected ManifestWork for old placement to be deleted, but it still exists")
	}
	// Verify that the ManifestWork from the current placement (place-test2) still exists
	works, err = listWorksByMWRS(context.TODO(), fWorkClient, "cls2", mwrSet.Namespace, mwrSet.Name, "place-test2")
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 1 {
		t.Fatalf("Expected ManifestWork for current placement to exist, but got %d, works: %v", len(works), works)
	}
	currentMW := works[0]
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
	mwOldPlacement := buildManifestWork(mwrSet, mwrSet.Name, "cls1", "place-test1")
	mwCurrentPlacement2 := buildManifestWork(mwrSet, mwrSet.Name, "cls2", "place-test2")
	mwCurrentPlacement3 := buildManifestWork(mwrSet, mwrSet.Name, "cls3", "place-test3")

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
	works, err := listWorksByMWRS(context.TODO(), fWorkClient, "cls1", mwrSet.Namespace, mwrSet.Name, "place-test1")
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 0 {
		t.Fatalf("Expected ManifestWorks for old placement to be deleted, found %d", len(works))
	}

	// Verify that ManifestWorks from current placements (place-test2 and place-test3) still exist
	works, err = listWorksByMWRS(context.TODO(), fWorkClient, "cls2", mwrSet.Namespace, mwrSet.Name, "place-test2")
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 1 {
		t.Fatalf("Expected ManifestWork for placement2 to exist, but got error: %d, works: %v", len(works), works)
	}
	currentMW2 := works[0]
	assert.Equal(t, currentMW2.Labels[workapiv1alpha1.ManifestWorkReplicaSetPlacementNameLabelKey], "place-test2")

	works, err = listWorksByMWRS(context.TODO(), fWorkClient, "cls3", mwrSet.Namespace, mwrSet.Name, "place-test3")
	if err != nil {
		t.Fatalf("Expected ManifestWork for placement3 to exist, but got error: %v", err)
	}
	if len(works) != 1 {
		t.Fatalf("Expected ManifestWork for placement3 to exist, but got error: %d, works: %v", len(works), works)
	}
	currentMW3 := works[0]
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
func listWorksByMWRS(
	ctx context.Context,
	client workclient.Interface,
	cluster string,
	mwrNamespace string,
	mwrName string,
	placement string,
) ([]workapiv1.ManifestWork, error) {

	selector := labels.Set{
		workhelper.ManifestWorkReplicaSetOwnerKeyHashLabelKey:       ownerKeyHash(mwrNamespace, mwrName),
		workapiv1alpha1.ManifestWorkReplicaSetPlacementNameLabelKey: placement,
	}.AsSelector().String()

	list, err := client.WorkV1().
		ManifestWorks(cluster).
		List(ctx, metav1.ListOptions{LabelSelector: selector})

	if err != nil {
		return nil, err
	}

	return list.Items, nil
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

func TestDeployReconcileLongOwnerNameUsesHashLabel(t *testing.T) {
	// "default." + longName exceeds the 63-char label-value limit, but the
	// hash-based owner label always fits so reconcile should succeed.
	longName := "mwrset-" + strings.Repeat("x", 60)
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet(longName, "default", "place-test")

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)
	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet); err != nil {
		t.Fatal(err)
	}
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	placement, placementDecision := helpertest.CreateTestPlacement("place-test", "default", "cls1")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement, placementDecision)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)
	if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement); err != nil {
		t.Fatal(err)
	}
	if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision); err != nil {
		t.Fatal(err)
	}

	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister(),
		placementLister:     clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
	}

	mwrSet, state, err := pmwDeployController.reconcile(context.TODO(), mwrSet)
	if err != nil {
		t.Fatal("expected no error, got ", err)
	}
	if state == reconcileStop {
		t.Fatal("expected reconcileContinue, long names should work with hash labels")
	}

	// A ManifestWork should have been created.
	works, err := fWorkClient.WorkV1().ManifestWorks("cls1").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(works.Items) != 1 {
		t.Fatalf("expected 1 ManifestWork, got %d", len(works.Items))
	}

	mw := works.Items[0]
	expectedHash := ownerKeyHash(mwrSet.Namespace, mwrSet.Name)
	assert.Equal(t, expectedHash, mw.Labels[workhelper.ManifestWorkReplicaSetOwnerKeyHashLabelKey],
		"hash label should be set")
	assert.Equal(t, fmt.Sprintf("%s/%s", mwrSet.Namespace, mwrSet.Name),
		mw.Annotations[workhelper.ManifestWorkReplicaSetOwnerAnnotationKey],
		"owner annotation should be set")

	// The deprecated label must NOT be set because namespace.name exceeds the label value limit.
	_, hasOldLabel := mw.Labels[workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey]
	assert.False(t, hasOldLabel,
		"deprecated label should not be set when namespace.name exceeds 63 chars")
}

func TestOldLabelOnlyManifestWorkMigratesOnSpecChange(t *testing.T) {
	mwrsName := "mwrSet-test"
	mwrsNS := "default"
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet(mwrsName, mwrsNS, "place-test")

	// Simulate a pre-upgrade ManifestWork: only the deprecated label, no hash label or annotation.
	oldMW := helpertest.CreateTestManifestWork(mwrsName, mwrsNS, "place-test", "cls1")
	delete(oldMW.Labels, workhelper.ManifestWorkReplicaSetOwnerKeyHashLabelKey)
	oldMW.Annotations = nil

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, oldMW)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	assert.Nil(t, workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(oldMW))
	assert.Nil(t, workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet))
	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	// Step 1: Verify the old-label-only MW is discovered by the deprecated-label fallback.
	result, allMWs, err := getManifestWorkInReplicaSet(mwrSet, mwLister)
	assert.Nil(t, err)
	assert.Len(t, allMWs, 1, "old-label-only MW should be found by deprecated-label fallback")
	assert.Contains(t, result.workByCluster, "cls1")

	// Step 2: First reconcile — old MW has matching spec, so it is counted as existing.
	placement, placementDecision := helpertest.CreateTestPlacement("place-test", "default", "cls1")
	fClusterClient := fakeclusterclient.NewSimpleClientset(placement, placementDecision)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)
	assert.Nil(t, clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement))
	assert.Nil(t, clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision))

	pmwDeployController := deployReconciler{
		workApplier:         workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
		manifestWorkLister:  mwLister,
		placeDecisionLister: clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister(),
		placementLister:     clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
	}

	mwrSet, _, err = pmwDeployController.reconcile(context.TODO(), mwrSet)
	assert.Nil(t, err)
	assert.Equal(t, 1, mwrSet.Status.Summary.Total, "old MW should be counted in summary")

	// Step 3: Change the MWRS spec to trigger a re-apply on the old MW.
	newTemplate := helpertest.CreateTestManifestWorkSpecWithSecret("v2", "test", "ns-test", "name-test")
	newTemplate.DeepCopyInto(&mwrSet.Spec.ManifestWorkTemplate)

	mwrSet, _, err = pmwDeployController.reconcile(context.TODO(), mwrSet)
	assert.Nil(t, err)

	// Step 4: Verify the MW in the fake client now has the new hash label and annotation,
	// while keeping the deprecated label (namespace.name fits within label value limit).
	works, err := fWorkClient.WorkV1().ManifestWorks("cls1").List(context.TODO(), metav1.ListOptions{})
	assert.Nil(t, err)

	var migrated *workapiv1.ManifestWork
	for i := range works.Items {
		if works.Items[i].Labels[workhelper.ManifestWorkReplicaSetOwnerKeyHashLabelKey] != "" {
			migrated = &works.Items[i]
			break
		}
	}
	if assert.NotNil(t, migrated, "MW should have the new hash label after spec-change reconcile") {
		assert.Equal(t, workhelper.OwnerKeyHash(mwrsNS, mwrsName),
			migrated.Labels[workhelper.ManifestWorkReplicaSetOwnerKeyHashLabelKey],
			"hash label should be set after migration")

		assert.Equal(t, fmt.Sprintf("%s/%s", mwrsNS, mwrsName),
			migrated.Annotations[workhelper.ManifestWorkReplicaSetOwnerAnnotationKey],
			"owner annotation should be set after migration")

		assert.Equal(t, fmt.Sprintf("%s.%s", mwrsNS, mwrsName),
			migrated.Labels[workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey],
			"deprecated label should be preserved for short names")
	}
}
