package manifestworkreplicasetcontroller

import (
	"context"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	helpertest "open-cluster-management.io/work/pkg/hub/test"
	"testing"
	"time"
)

func TestStatusReconcileAsExpected(t *testing.T) {
	clusters := []string{"cls1", "cls2", "cls3", "cls4"}
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	mwrSetTest.Status.Summary.Total = len(clusters)

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSetTest)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSetTest); err != nil {
		t.Fatal(err)
	}

	for _, cls := range clusters {
		mw, _ := CreateManifestWork(mwrSetTest, cls)
		cond := getCondition(string(workv1.ManifestApplied), "", "", metav1.ConditionTrue)
		apimeta.SetStatusCondition(&mw.Status.Conditions, cond)

		cond = getCondition(string(workv1.ManifestAvailable), "", "", metav1.ConditionTrue)
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
	clusters := []string{"cls1", "cls2", "cls3", "cls4"}
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	mwrSetTest.Status.Summary.Total = len(clusters)

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSetTest)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSetTest); err != nil {
		t.Fatal(err)
	}

	for id, cls := range clusters {
		mw, _ := CreateManifestWork(mwrSetTest, cls)
		cond := getCondition(string(workv1.ManifestApplied), "", "", metav1.ConditionTrue)
		apimeta.SetStatusCondition(&mw.Status.Conditions, cond)

		if id%2 == 0 {
			cond = getCondition(string(workv1.ManifestAvailable), "", "", metav1.ConditionTrue)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
		} else {
			cond = getCondition(string(workv1.ManifestProgressing), "", "", metav1.ConditionTrue)
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
	clusters := []string{"cls1", "cls2", "cls3", "cls4"}
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	mwrSetTest.Status.Summary.Total = len(clusters)

	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSetTest)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSetTest); err != nil {
		t.Fatal(err)
	}

	avaCount, processingCount, degradCount := 0, 0, 0
	for id, cls := range clusters {
		mw, _ := CreateManifestWork(mwrSetTest, cls)
		cond := getCondition(string(workv1.ManifestApplied), "", "", metav1.ConditionTrue)
		apimeta.SetStatusCondition(&mw.Status.Conditions, cond)

		if id%2 == 0 {
			cond = getCondition(string(workv1.ManifestAvailable), "", "", metav1.ConditionTrue)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
			avaCount++
		} else if id%3 == 0 {
			cond = getCondition(string(workv1.ManifestDegraded), "", "", metav1.ConditionTrue)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
			processingCount++
		} else {
			cond = getCondition(string(workv1.ManifestProgressing), "", "", metav1.ConditionTrue)
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
