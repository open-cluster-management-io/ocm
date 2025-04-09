package manifestworkreplicasetcontroller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	helpertest "open-cluster-management.io/ocm/pkg/work/hub/test"
)

func TestStatusReconcileAsExpected(t *testing.T) {
	plcName := "place-test"
	clusters := []string{"cls1", "cls2", "cls3", "cls4"}
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", plcName)
	mwrSetTest.Status.Summary.Total = len(clusters)
	mwrSetTest.Status.PlacementsSummary = []workapiv1alpha1.PlacementSummary{
		{
			Name:                    plcName,
			AvailableDecisionGroups: "1",
			Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total: len(clusters),
			},
		},
	}

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSetTest)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSetTest); err != nil {
		t.Fatal(err)
	}

	for _, cls := range clusters {
		mw, _ := CreateManifestWork(mwrSetTest, cls, plcName)
		cond := getCondition(workv1.WorkApplied, "", "", metav1.ConditionTrue)
		apimeta.SetStatusCondition(&mw.Status.Conditions, cond)

		cond = getCondition(workv1.WorkAvailable, "", "", metav1.ConditionTrue)
		apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
		if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw); err != nil {
			t.Fatal(err)
		}
	}

	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
	mwrSetStatusController := statusReconciler{
		manifestWorkLister: mwLister,
	}

	mwrSetTest, _, err := mwrSetStatusController.reconcile(context.TODO(), mwrSetTest)
	if err != nil {
		t.Fatal(err)
	}

	// Check for the expected Summary
	mwrSetSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       len(clusters),
		Applied:     len(clusters),
		Available:   len(clusters),
		Degraded:    0,
		Progressing: 0,
	}

	if mwrSetTest.Status.Summary != mwrSetSummary {
		t.Fatal("Summary not as expected ", mwrSetTest.Status.Summary, mwrSetSummary)
	}

	// Check the ManifestworkApplied conditions
	appliedCondition := apimeta.FindStatusCondition(mwrSetTest.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied)

	if appliedCondition == nil {
		t.Fatal("Applied condition not found ", mwrSetTest.Status.Conditions)
	}

	// Check ManifestworkApplied condition status true
	if appliedCondition.Status == metav1.ConditionFalse {
		t.Fatal("Applied condition not True ", appliedCondition)
	}

	// Check ManifestworkApplied condition reason
	if appliedCondition.Reason != workapiv1alpha1.ReasonAsExpected {
		t.Fatal("Applied condition Reason not match AsExpected ", appliedCondition)
	}
}

func TestStatusReconcileAsProcessing(t *testing.T) {
	plcName := "place-test"
	clusters := []string{"cls1", "cls2", "cls3", "cls4"}
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", plcName)
	mwrSetTest.Status.Summary.Total = len(clusters)
	mwrSetTest.Status.PlacementsSummary = []workapiv1alpha1.PlacementSummary{
		{
			Name:                    plcName,
			AvailableDecisionGroups: "1",
			Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total: len(clusters),
			},
		},
	}

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSetTest)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSetTest); err != nil {
		t.Fatal(err)
	}

	for id, cls := range clusters {
		mw, _ := CreateManifestWork(mwrSetTest, cls, plcName)
		cond := getCondition(workv1.WorkApplied, "", "", metav1.ConditionTrue)
		apimeta.SetStatusCondition(&mw.Status.Conditions, cond)

		if id%2 == 0 {
			cond = getCondition(workv1.WorkAvailable, "", "", metav1.ConditionTrue)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
		} else {
			cond = getCondition(workv1.WorkProgressing, "", "", metav1.ConditionTrue)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
		}

		if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw); err != nil {
			t.Fatal(err)
		}
	}

	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
	mwrSetStatusController := statusReconciler{
		manifestWorkLister: mwLister,
	}

	mwrSetTest, _, err := mwrSetStatusController.reconcile(context.TODO(), mwrSetTest)
	if err != nil {
		t.Fatal(err)
	}

	// Check for the expected Summary
	mwrSetSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       len(clusters),
		Applied:     len(clusters),
		Available:   len(clusters) / 2,
		Degraded:    0,
		Progressing: len(clusters) / 2,
	}

	if mwrSetTest.Status.Summary != mwrSetSummary {
		t.Fatal("Summary not as expected ", mwrSetTest.Status.Summary, mwrSetSummary)
	}

	// Check the ManifestworkApplied conditions
	appliedCondition := apimeta.FindStatusCondition(mwrSetTest.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied)

	if appliedCondition == nil {
		t.Fatal("Applied condition not found ", mwrSetTest.Status.Conditions)
	}

	// Check ManifestworkApplied condition status false
	if appliedCondition.Status == metav1.ConditionTrue {
		t.Fatal("Applied condition not True ", appliedCondition)
	}

	// Check ManifestworkApplied condition reason
	if appliedCondition.Reason != workapiv1alpha1.ReasonProcessing {
		t.Fatal("Applied condition Reason not match Processing ", appliedCondition)
	}
}

func TestStatusReconcileNotAsExpected(t *testing.T) {
	plcName := "place-test"
	clusters := []string{"cls1", "cls2", "cls3", "cls4"}
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", plcName)
	mwrSetTest.Status.Summary.Total = len(clusters)
	mwrSetTest.Status.PlacementsSummary = []workapiv1alpha1.PlacementSummary{
		{
			Name:                    plcName,
			AvailableDecisionGroups: "1",
			Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total: len(clusters),
			},
		},
	}

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSetTest)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSetTest); err != nil {
		t.Fatal(err)
	}

	avaCount, processingCount, degradCount := 0, 0, 0
	for id, cls := range clusters {
		mw, _ := CreateManifestWork(mwrSetTest, cls, plcName)
		cond := getCondition(workv1.WorkApplied, "", "", metav1.ConditionTrue)
		apimeta.SetStatusCondition(&mw.Status.Conditions, cond)

		if id%2 == 0 { //nolint:gocritic
			cond = getCondition(workv1.WorkAvailable, "", "", metav1.ConditionTrue)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
			avaCount++
		} else if id%3 == 0 {
			cond = getCondition(workv1.WorkDegraded, "", "", metav1.ConditionTrue)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
			processingCount++
		} else {
			cond = getCondition(workv1.WorkProgressing, "", "", metav1.ConditionTrue)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
			degradCount++
		}

		if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw); err != nil {
			t.Fatal(err)
		}
	}

	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
	mwrSetStatusController := statusReconciler{
		manifestWorkLister: mwLister,
	}

	mwrSetTest, _, err := mwrSetStatusController.reconcile(context.TODO(), mwrSetTest)
	if err != nil {
		t.Fatal(err)
	}

	// Check for the expected Summary
	mwrSetSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       len(clusters),
		Applied:     len(clusters),
		Available:   avaCount,
		Degraded:    degradCount,
		Progressing: processingCount,
	}

	if mwrSetTest.Status.Summary != mwrSetSummary {
		t.Fatal("Summary not as expected ", mwrSetTest.Status.Summary, mwrSetSummary)
	}

	// Check the ManifestworkApplied conditions
	appliedCondition := apimeta.FindStatusCondition(mwrSetTest.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied)

	if appliedCondition == nil {
		t.Fatal("Applied condition not found ", mwrSetTest.Status.Conditions)
	}

	// Check ManifestworkApplied condition status false
	if appliedCondition.Status == metav1.ConditionTrue {
		t.Fatal("Applied condition not True ", appliedCondition)
	}

	// Check ManifestworkApplied condition reason
	if appliedCondition.Reason != workapiv1alpha1.ReasonNotAsExpected {
		t.Fatal("Applied condition Reason not match NotAsExpected ", appliedCondition)
	}
}

func TestStatusWithMultiPlacementsReconcileAsExpected(t *testing.T) {
	placements := map[string][]string{"plc1": {"cls1", "cls2"}, "plc2": {"cls3", "cls4"}}
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "plc1", "plc2")
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSetTest)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)
	count := 0
	for plcName, clusters := range placements {
		mwrSetTest.Status.PlacementsSummary = append(mwrSetTest.Status.PlacementsSummary, workapiv1alpha1.PlacementSummary{
			Name: plcName,
			Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total: len(clusters),
			}})
		count += len(clusters)
		for _, cls := range clusters {
			mw := helpertest.CreateTestManifestWork(mwrSetTest.Name, mwrSetTest.Namespace, plcName, cls)
			err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw)
			assert.Nil(t, err)
		}
	}
	mwrSetTest.Status.Summary.Total = count
	err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSetTest)
	assert.Nil(t, err)

	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
	mwrSetStatusController := statusReconciler{
		manifestWorkLister: mwLister,
	}
	mwrSetTest, _, err = mwrSetStatusController.reconcile(context.TODO(), mwrSetTest)
	assert.Nil(t, err)

	// Check for the expected Summary
	mwrSetSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       count,
		Applied:     count,
		Available:   count,
		Degraded:    0,
		Progressing: 0,
	}
	assert.Equal(t, mwrSetTest.Status.Summary, mwrSetSummary)

	// Check the ManifestworkApplied conditions
	appliedCondition := apimeta.FindStatusCondition(mwrSetTest.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied)
	assert.NotNil(t, appliedCondition)
	assert.Equal(t, appliedCondition.Status, metav1.ConditionTrue)
	assert.Equal(t, appliedCondition.Reason, workapiv1alpha1.ReasonAsExpected)
}

func TestStatusWithMWRSetSpecChangesReconcile(t *testing.T) {
	plcName := "place-test"
	clusters := []string{"cls1", "cls2", "cls3", "cls4"}
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", plcName)
	mwrSetTest.Status.Summary.Total = len(clusters)
	mwrSetTest.Status.PlacementsSummary = []workapiv1alpha1.PlacementSummary{
		{
			Name:                    plcName,
			AvailableDecisionGroups: "1",
			Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{
				Total: len(clusters),
			},
		},
	}

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSetTest)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSetTest)
	assert.Nil(t, err)

	for _, cls := range clusters {
		mw := helpertest.CreateTestManifestWork(mwrSetTest.Name, mwrSetTest.Namespace, plcName, cls)
		workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw)
		assert.Nil(t, err)
	}

	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
	mwrSetStatusController := statusReconciler{
		manifestWorkLister: mwLister,
	}

	mwrSetTest, _, err = mwrSetStatusController.reconcile(context.TODO(), mwrSetTest)
	assert.Nil(t, err)

	// Check for the expected Summary
	mwrSetSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       len(clusters),
		Applied:     len(clusters),
		Available:   len(clusters),
		Degraded:    0,
		Progressing: 0,
	}
	assert.Equal(t, mwrSetTest.Status.Summary, mwrSetSummary)

	// Check the ManifestworkApplied conditions
	appliedCondition := apimeta.FindStatusCondition(mwrSetTest.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied)
	assert.NotNil(t, appliedCondition)
	assert.Equal(t, appliedCondition.Status, metav1.ConditionTrue)
	assert.Equal(t, appliedCondition.Reason, workapiv1alpha1.ReasonAsExpected)

	// Change the mwrSet spec and re-run deploy.
	mwTemplate := helpertest.CreateTestManifestWorkSpecWithSecret("v2", "kindtest", "ns-test", "name-test")
	mwTemplate.DeepCopyInto(&mwrSetTest.Spec.ManifestWorkTemplate)

	mwrSetTest, _, err = mwrSetStatusController.reconcile(context.TODO(), mwrSetTest)
	assert.Nil(t, err)

	// Check for the expected Summary
	mwrSetSummary = workapiv1alpha1.ManifestWorkReplicaSetSummary{
		Total:       len(clusters),
		Applied:     0,
		Available:   0,
		Degraded:    0,
		Progressing: 0,
	}
	assert.Equal(t, mwrSetTest.Status.Summary, mwrSetSummary)

	// Check the ManifestworkApplied conditions
	appliedCondition = apimeta.FindStatusCondition(mwrSetTest.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied)
	assert.NotNil(t, appliedCondition)
	assert.Equal(t, appliedCondition.Status, metav1.ConditionFalse)
	assert.Equal(t, appliedCondition.Reason, workapiv1alpha1.ReasonNotAsExpected)
}
