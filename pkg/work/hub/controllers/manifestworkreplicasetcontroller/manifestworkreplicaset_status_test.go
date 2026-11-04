package manifestworkreplicasetcontroller

import (
	"context"
	"fmt"
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
	mwrSetTest.Generation = 3
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
		mw := buildManifestWork(mwrSetTest, mwrSetTest.Name, cls, plcName)
		cond := getCondition(workv1.WorkApplied, "", "", metav1.ConditionTrue, 0)
		apimeta.SetStatusCondition(&mw.Status.Conditions, cond)

		cond = getCondition(workv1.WorkAvailable, "", "", metav1.ConditionTrue, 0)
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

	if appliedCondition.ObservedGeneration != mwrSetTest.Generation {
		t.Fatal("Applied condition ObservedGeneration not match Generation ", appliedCondition, mwrSetTest.Generation)
	}
}

func TestStatusReconcileNewGeneration(t *testing.T) {
	const placementName = "place-test"
	cases := []struct {
		name                  string
		observedGeneration    int64
		availableGeneration   int64
		progressingGeneration int64
		degradedGeneration    int64
		oldTemplate           bool
		oldWorkCount          int
		total                 int
		noConditions          bool
		omitAvailable         bool
		omitProgressing       bool
		missingWorks          int
		deleting              bool
		failed                bool
		applyFailedAvailable  bool
		wantSummary           workapiv1alpha1.ManifestWorkReplicaSetSummary
		wantReason            string
		wantStatus            metav1.ConditionStatus
	}{
		{
			name:         "new work without conditions",
			noConditions: true,
			wantSummary:  workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Progressing: 2},
			wantReason:   workapiv1alpha1.ReasonProcessing,
			wantStatus:   metav1.ConditionFalse,
		},
		{
			name:               "conditions from previous work generation",
			observedGeneration: 1,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Progressing: 2},
			wantReason:         workapiv1alpha1.ReasonProcessing,
			wantStatus:         metav1.ConditionFalse,
		},
		{
			name:               "work spec still uses previous template",
			observedGeneration: 2,
			oldTemplate:        true,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Progressing: 2},
			wantReason:         workapiv1alpha1.ReasonProcessing,
			wantStatus:         metav1.ConditionFalse,
		},
		{
			name:               "applied current but available not reported yet",
			observedGeneration: 2,
			omitAvailable:      true,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Progressing: 2},
			wantReason:         workapiv1alpha1.ReasonProcessing,
			wantStatus:         metav1.ConditionFalse,
		},
		{
			name:                "available condition from previous work generation",
			observedGeneration:  2,
			availableGeneration: 1,
			wantSummary:         workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Progressing: 2},
			wantReason:          workapiv1alpha1.ReasonProcessing,
			wantStatus:          metav1.ConditionFalse,
		},
		{
			name:                  "progressing condition from previous work generation",
			observedGeneration:    2,
			progressingGeneration: 1,
			wantSummary:           workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Progressing: 2},
			wantReason:            workapiv1alpha1.ReasonProcessing,
			wantStatus:            metav1.ConditionFalse,
		},
		{
			name:               "degraded condition from previous work generation",
			observedGeneration: 2,
			degradedGeneration: 1,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Progressing: 2},
			wantReason:         workapiv1alpha1.ReasonProcessing,
			wantStatus:         metav1.ConditionFalse,
		},
		{
			name:               "new works not yet in the informer cache",
			observedGeneration: 2,
			missingWorks:       2,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Progressing: 2},
			wantReason:         workapiv1alpha1.ReasonProcessing,
			wantStatus:         metav1.ConditionFalse,
		},
		{
			name:               "one new work not yet in the informer cache",
			observedGeneration: 2,
			missingWorks:       1,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Applied: 1, Available: 1, Progressing: 1},
			wantReason:         workapiv1alpha1.ReasonProcessing,
			wantStatus:         metav1.ConditionFalse,
		},
		{
			name:               "old works do not hide a missing work from the cache",
			observedGeneration: 2,
			missingWorks:       1,
			oldWorkCount:       4,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Applied: 1, Available: 1, Progressing: 1},
			wantReason:         workapiv1alpha1.ReasonProcessing,
			wantStatus:         metav1.ConditionFalse,
		},
		{
			name:         "progressive rollout excludes old works from total",
			missingWorks: 2,
			oldWorkCount: 4,
			total:        1,
			wantSummary:  workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 1, Progressing: 1},
			wantReason:   workapiv1alpha1.ReasonProcessing,
			wantStatus:   metav1.ConditionFalse,
		},
		{
			name:               "deleting old works do not hide missing current works",
			observedGeneration: 2,
			oldTemplate:        true,
			deleting:           true,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Progressing: 2},
			wantReason:         workapiv1alpha1.ReasonProcessing,
			wantStatus:         metav1.ConditionFalse,
		},
		{
			name:               "deleting works are skipped",
			observedGeneration: 2,
			deleting:           true,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2},
			wantReason:         workapiv1alpha1.ReasonNotAsExpected,
			wantStatus:         metav1.ConditionFalse,
		},
		{
			name:               "current generation succeeded",
			observedGeneration: 2,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Applied: 2, Available: 2},
			wantReason:         workapiv1alpha1.ReasonAsExpected,
			wantStatus:         metav1.ConditionTrue,
		},
		{
			name:               "current generation without optional progressing condition",
			observedGeneration: 2,
			omitProgressing:    true,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Applied: 2, Available: 2},
			wantReason:         workapiv1alpha1.ReasonAsExpected,
			wantStatus:         metav1.ConditionTrue,
		},
		{
			name:                 "current apply failed while resource remains available",
			observedGeneration:   2,
			omitProgressing:      true,
			applyFailedAvailable: true,
			wantSummary:          workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Available: 2},
			wantReason:           workapiv1alpha1.ReasonNotAsExpected,
			wantStatus:           metav1.ConditionFalse,
		},
		{
			name:               "current generation failed",
			observedGeneration: 2,
			failed:             true,
			wantSummary:        workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Progressing: 2, Degraded: 2},
			wantReason:         workapiv1alpha1.ReasonNotAsExpected,
			wantStatus:         metav1.ConditionFalse,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", placementName)
			mwrSet.Generation = 2
			total := 2
			if tt.total != 0 {
				total = tt.total
			}
			mwrSet.Status.Summary.Total = total
			mwrSet.Status.PlacementsSummary = []workapiv1alpha1.PlacementSummary{{
				Name:    placementName,
				Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: total},
			}}

			workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fakeworkclient.NewSimpleClientset(), time.Second)
			clusters := []string{"cls1", "cls2"}
			for _, cluster := range clusters[:len(clusters)-tt.missingWorks] {
				mw := buildManifestWork(mwrSet, mwrSet.Name, cluster, placementName)
				mw.Generation = 2
				if tt.deleting {
					deletionTime := metav1.Now()
					mw.DeletionTimestamp = &deletionTime
				}
				if tt.oldTemplate {
					mw.Spec = helpertest.CreateTestManifestWorkSpecWithSecret("v2", "kindtest", "ns-test", "name-test")
				}
				if !tt.noConditions {
					appliedStatus, availableStatus, progressingStatus := metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionFalse
					if tt.failed {
						appliedStatus, availableStatus, progressingStatus = metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue
					}
					if tt.applyFailedAvailable {
						appliedStatus = metav1.ConditionFalse
					}
					if tt.failed || tt.degradedGeneration != 0 {
						degradedGeneration := tt.observedGeneration
						if tt.degradedGeneration != 0 {
							degradedGeneration = tt.degradedGeneration
						}
						apimeta.SetStatusCondition(&mw.Status.Conditions,
							getCondition(workv1.WorkDegraded, "", "", metav1.ConditionTrue, degradedGeneration))
					}
					apimeta.SetStatusCondition(&mw.Status.Conditions,
						getCondition(workv1.WorkApplied, "", "", appliedStatus, tt.observedGeneration))
					availableGeneration := tt.observedGeneration
					if tt.availableGeneration != 0 {
						availableGeneration = tt.availableGeneration
					}
					if !tt.omitAvailable {
						apimeta.SetStatusCondition(&mw.Status.Conditions,
							getCondition(workv1.WorkAvailable, "", "", availableStatus, availableGeneration))
					}
					if !tt.omitProgressing {
						progressingGeneration := tt.observedGeneration
						if tt.progressingGeneration != 0 {
							progressingGeneration = tt.progressingGeneration
						}
						apimeta.SetStatusCondition(&mw.Status.Conditions,
							getCondition(workv1.WorkProgressing, "", "", progressingStatus, progressingGeneration))
					}
				}
				if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < tt.oldWorkCount; i++ {
				mw := buildManifestWork(mwrSet, mwrSet.Name, fmt.Sprintf("old-cls-%d", i), placementName)
				mw.Spec = helpertest.CreateTestManifestWorkSpecWithSecret("v2", "kindtest", "ns-test", "name-test")
				if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw); err != nil {
					t.Fatal(err)
				}
			}

			reconciler := statusReconciler{manifestWorkLister: workInformerFactory.Work().V1().ManifestWorks().Lister()}
			_, _, err := reconciler.reconcile(context.Background(), mwrSet)
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, tt.wantSummary, mwrSet.Status.Summary)
			assert.Equal(t, tt.wantSummary, mwrSet.Status.PlacementsSummary[0].Summary)
			condition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied)
			if assert.NotNil(t, condition) {
				assert.Equal(t, tt.wantReason, condition.Reason)
				assert.Equal(t, tt.wantStatus, condition.Status)
				assert.Equal(t, mwrSet.Generation, condition.ObservedGeneration)
			}
		})
	}
}

func TestStatusReconcileMissingPlacement(t *testing.T) {
	for _, tt := range []struct {
		name               string
		total              int
		observedGeneration int64
		wantReason         string
		wantStatus         metav1.ConditionStatus
		wantMissingMessage bool
	}{
		{
			name:               "current missing placement prevents success",
			total:              1,
			observedGeneration: 2,
			wantReason:         workapiv1alpha1.ReasonNotAsExpected,
			wantStatus:         metav1.ConditionFalse,
			wantMissingMessage: true,
		},
		{
			name:               "stale missing placement is ignored",
			total:              1,
			observedGeneration: 1,
			wantReason:         workapiv1alpha1.ReasonAsExpected,
			wantStatus:         metav1.ConditionTrue,
		},
		{
			name:               "current missing placement with zero total preserves message",
			observedGeneration: 2,
			wantReason:         workapiv1alpha1.ReasonNotAsExpected,
			wantStatus:         metav1.ConditionFalse,
			wantMissingMessage: true,
		},
		{
			name:               "stale missing placement with zero total does not propagate message",
			observedGeneration: 1,
			wantReason:         workapiv1alpha1.ReasonNotAsExpected,
			wantStatus:         metav1.ConditionFalse,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const placementName = "valid"
			mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", placementName)
			mwrSet.Generation = 2
			mwrSet.Status.Summary.Total = tt.total
			if tt.total > 0 {
				mwrSet.Status.PlacementsSummary = []workapiv1alpha1.PlacementSummary{{
					Name:    placementName,
					Summary: workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: tt.total},
				}}
			}
			message := "placements not found in namespace default: missing"
			apimeta.SetStatusCondition(&mwrSet.Status.Conditions,
				getPlacementDecisionVerified(tt.observedGeneration, workapiv1alpha1.ReasonPlacementDecisionNotFound, message))

			workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fakeworkclient.NewSimpleClientset(), time.Second)
			if tt.total > 0 {
				mw := buildManifestWork(mwrSet, mwrSet.Name, "cls1", placementName)
				apimeta.SetStatusCondition(&mw.Status.Conditions, getCondition(workv1.WorkApplied, "", "", metav1.ConditionTrue, 0))
				apimeta.SetStatusCondition(&mw.Status.Conditions, getCondition(workv1.WorkAvailable, "", "", metav1.ConditionTrue, 0))
				if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw); err != nil {
					t.Fatal(err)
				}
			}

			reconciler := statusReconciler{manifestWorkLister: workInformerFactory.Work().V1().ManifestWorks().Lister()}
			_, _, err := reconciler.reconcile(context.TODO(), mwrSet)
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: tt.total, Applied: tt.total, Available: tt.total}, mwrSet.Status.Summary)
			applied := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied)
			if assert.NotNil(t, applied) {
				assert.Equal(t, tt.wantStatus, applied.Status)
				assert.Equal(t, tt.wantReason, applied.Reason)
				assert.Equal(t, mwrSet.Generation, applied.ObservedGeneration)
				wantMessage := ""
				if tt.wantMissingMessage {
					wantMessage = message
				}
				assert.Equal(t, wantMessage, applied.Message)
			}
		})
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
		mw := buildManifestWork(mwrSetTest, mwrSetTest.Name, cls, plcName)
		cond := getCondition(workv1.WorkApplied, "", "", metav1.ConditionTrue, 0)
		apimeta.SetStatusCondition(&mw.Status.Conditions, cond)

		if id%2 == 0 {
			cond = getCondition(workv1.WorkAvailable, "", "", metav1.ConditionTrue, 0)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
		} else {
			cond = getCondition(workv1.WorkAvailable, "", "", metav1.ConditionFalse, 0)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
			cond = getCondition(workv1.WorkProgressing, "", "", metav1.ConditionTrue, 0)
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
		mw := buildManifestWork(mwrSetTest, mwrSetTest.Name, cls, plcName)
		cond := getCondition(workv1.WorkApplied, "", "", metav1.ConditionTrue, 0)
		apimeta.SetStatusCondition(&mw.Status.Conditions, cond)

		if id%2 == 0 { //nolint:gocritic
			cond = getCondition(workv1.WorkAvailable, "", "", metav1.ConditionTrue, 0)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
			avaCount++
		} else if id%3 == 0 {
			cond = getCondition(workv1.WorkAvailable, "", "", metav1.ConditionFalse, 0)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
			cond = getCondition(workv1.WorkDegraded, "", "", metav1.ConditionTrue, 0)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
			processingCount++
		} else {
			cond = getCondition(workv1.WorkAvailable, "", "", metav1.ConditionFalse, 0)
			apimeta.SetStatusCondition(&mw.Status.Conditions, cond)
			cond = getCondition(workv1.WorkProgressing, "", "", metav1.ConditionTrue, 0)
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
		err = workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw)
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
		Progressing: len(clusters),
	}
	assert.Equal(t, mwrSetTest.Status.Summary, mwrSetSummary)

	// Check the ManifestworkApplied conditions
	appliedCondition = apimeta.FindStatusCondition(mwrSetTest.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied)
	assert.NotNil(t, appliedCondition)
	assert.Equal(t, appliedCondition.Status, metav1.ConditionFalse)
	assert.Equal(t, appliedCondition.Reason, workapiv1alpha1.ReasonProcessing)
}
