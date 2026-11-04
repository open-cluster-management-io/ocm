package manifestworkreplicasetcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	workapplier "open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	helpertest "open-cluster-management.io/ocm/pkg/work/hub/test"
)

func TestManifestWorkReplicaSetMissingPlacementRefreshesStatus(t *testing.T) {
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("test", "default", "missing")
	mwrSet.Generation = 2
	mwrSet.Finalizers = []string{workapiv1alpha1.ManifestWorkReplicaSetFinalizer}
	mwrSet.Status.Summary = workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 1, Applied: 1, Available: 1}
	mwrSet.Status.PlacementsSummary = []workapiv1alpha1.PlacementSummary{{
		Name:    "missing",
		Summary: mwrSet.Status.Summary,
	}}
	meta.SetStatusCondition(&mwrSet.Status.Conditions, getPlacementDecisionVerified(1, workapiv1alpha1.ReasonAsExpected, ""))
	meta.SetStatusCondition(&mwrSet.Status.Conditions, getPlacementRollOut(1, workapiv1alpha1.ReasonComplete, ""))
	meta.SetStatusCondition(&mwrSet.Status.Conditions, getManifestworkApplied(1, workapiv1alpha1.ReasonAsExpected, ""))
	missingWork := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, "missing", "cls1")

	fakeWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, missingWork)
	workInformers := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	if err := workInformers.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet); err != nil {
		t.Fatal(err)
	}
	if err := workInformers.Work().V1().ManifestWorks().Informer().GetStore().Add(missingWork); err != nil {
		t.Fatal(err)
	}
	clusterInformers := clusterinformers.NewSharedInformerFactory(fakeclusterclient.NewSimpleClientset(), 10*time.Minute)
	mwLister := workInformers.Work().V1().ManifestWorks().Lister()
	ctrl := newController(
		fakeWorkClient,
		workapplier.NewWorkApplierWithTypedClient(fakeWorkClient, mwLister),
		workInformers.Work().V1alpha1().ManifestWorkReplicaSets(),
		workInformers.Work().V1().ManifestWorks(),
		clusterInformers.Cluster().V1beta1().Placements(),
		clusterInformers.Cluster().V1beta1().PlacementDecisions(),
	)

	key := mwrSet.Namespace + "/" + mwrSet.Name
	if err := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, key), key); err != nil {
		t.Fatal(err)
	}

	actions := fakeWorkClient.Actions()
	testingcommon.AssertActions(t, actions, "patch")
	patched, err := fakeWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(mwrSet.Namespace).Get(context.TODO(), mwrSet.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if patched.Status.Summary != (workapiv1alpha1.ManifestWorkReplicaSetSummary{}) || len(patched.Status.PlacementsSummary) != 0 {
		t.Fatalf("missing placement should clear the previous summary, got %+v", patched.Status)
	}
	for _, conditionType := range []string{
		workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified,
		workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut,
		workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied,
	} {
		condition := meta.FindStatusCondition(patched.Status.Conditions, conditionType)
		if condition == nil || condition.Status != metav1.ConditionFalse || condition.ObservedGeneration != mwrSet.Generation {
			t.Errorf("%s should be False at generation %d, got %+v", conditionType, mwrSet.Generation, condition)
		}
	}
	applied := meta.FindStatusCondition(patched.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied)
	if applied == nil || applied.Reason != workapiv1alpha1.ReasonNotAsExpected {
		t.Errorf("missing placement should report ManifestworkApplied=NotAsExpected, got %+v", applied)
	} else if applied.Message != "placements not found in namespace default: missing" {
		t.Errorf("missing placement message was not propagated: %q", applied.Message)
	}
}

func TestManifestWorkReplicaSetDeletingSkipsStatus(t *testing.T) {
	for _, tt := range []struct {
		name          string
		policy        workapiv1alpha1.CascadeDeletionPolicy
		deleteFailure bool
		wantActions   []string
	}{
		{
			name:        "background deletion",
			policy:      workapiv1alpha1.Background,
			wantActions: []string{"delete", "patch"},
		},
		{
			name:        "foreground deletion with pending works",
			policy:      workapiv1alpha1.Foreground,
			wantActions: []string{"delete"},
		},
		{
			name:          "work deletion fails",
			policy:        workapiv1alpha1.Background,
			deleteFailure: true,
			wantActions:   []string{"delete"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mwrSet := helpertest.CreateTestManifestWorkReplicaSet("test", "default", "missing")
			mwrSet.Generation = 2
			mwrSet.Finalizers = []string{workapiv1alpha1.ManifestWorkReplicaSetFinalizer}
			now := metav1.Now()
			mwrSet.DeletionTimestamp = &now
			mwrSet.Spec.CascadeDeletionPolicy = tt.policy
			mwrSet.Status.Summary = workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 1, Applied: 1, Available: 1}
			mwrSet.Status.PlacementsSummary = []workapiv1alpha1.PlacementSummary{{Name: "missing", Summary: mwrSet.Status.Summary}}
			// Stale conditions make an accidental status reconciliation observable.
			meta.SetStatusCondition(&mwrSet.Status.Conditions, getPlacementDecisionVerified(1, workapiv1alpha1.ReasonAsExpected, ""))
			meta.SetStatusCondition(&mwrSet.Status.Conditions, getPlacementRollOut(1, workapiv1alpha1.ReasonComplete, ""))
			meta.SetStatusCondition(&mwrSet.Status.Conditions, getManifestworkApplied(1, workapiv1alpha1.ReasonAsExpected, ""))
			// Finalizer patches round-trip timestamps through JSON, which only preserves seconds.
			for i := range mwrSet.Status.Conditions {
				mwrSet.Status.Conditions[i].LastTransitionTime = metav1.NewTime(now.Truncate(time.Second))
			}
			originalStatus := mwrSet.DeepCopy().Status
			work := helpertest.CreateTestManifestWork(mwrSet.Name, mwrSet.Namespace, "missing", "cls1")

			fakeWorkClient := fakeworkclient.NewSimpleClientset(mwrSet, work)
			deleteErr := errors.New("manifestwork deletion failed")
			if tt.deleteFailure {
				fakeWorkClient.PrependReactor("delete", "manifestworks", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, deleteErr
				})
			}
			workInformers := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
			if err := workInformers.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSet); err != nil {
				t.Fatal(err)
			}
			if err := workInformers.Work().V1().ManifestWorks().Informer().GetStore().Add(work); err != nil {
				t.Fatal(err)
			}
			clusterInformers := clusterinformers.NewSharedInformerFactory(fakeclusterclient.NewSimpleClientset(), 10*time.Minute)
			ctrl := newController(
				fakeWorkClient,
				workapplier.NewWorkApplierWithTypedClient(fakeWorkClient, workInformers.Work().V1().ManifestWorks().Lister()),
				workInformers.Work().V1alpha1().ManifestWorkReplicaSets(),
				workInformers.Work().V1().ManifestWorks(),
				clusterInformers.Cluster().V1beta1().Placements(),
				clusterInformers.Cluster().V1beta1().PlacementDecisions(),
			)

			key := mwrSet.Namespace + "/" + mwrSet.Name
			err := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, key), key)
			if tt.deleteFailure {
				if !errors.Is(err, deleteErr) {
					t.Fatalf("expected work deletion error, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			actions := fakeWorkClient.Actions()
			testingcommon.AssertActions(t, actions, tt.wantActions...)
			for _, action := range actions {
				if action.GetSubresource() == "status" {
					t.Fatalf("deleting MWRS should not patch status: %+v", action)
				}
			}
			stored, err := fakeWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(mwrSet.Namespace).Get(context.TODO(), mwrSet.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(originalStatus, stored.Status) {
				t.Fatalf("status changed during deletion: got %+v, want %+v", stored.Status, originalStatus)
			}
		})
	}
}

func TestManifestWorkReplicaSetControllerPatchStatus(t *testing.T) {
	cases := []struct {
		name            string
		works           []runtime.Object
		mwrSet          *workapiv1alpha1.ManifestWorkReplicaSet
		placement       *clusterv1beta1.Placement
		decision        *clusterv1beta1.PlacementDecision
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:   "add finalizer",
			mwrSet: helpertest.CreateTestManifestWorkReplicaSet("test", "default", "placement"),
			works:  []runtime.Object{},
			placement: func() *clusterv1beta1.Placement {
				p, _ := helpertest.CreateTestPlacement("default", "placement")
				return p
			}(),
			decision: func() *clusterv1beta1.PlacementDecision {
				_, d := helpertest.CreateTestPlacement("default", "placement")
				return d
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				workSet := &workapiv1alpha1.ManifestWorkReplicaSet{}
				if err := json.Unmarshal(p, workSet); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(workSet.Finalizers, []string{workapiv1alpha1.ManifestWorkReplicaSetFinalizer}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name: "placement not found",
			mwrSet: func() *workapiv1alpha1.ManifestWorkReplicaSet {
				w := helpertest.CreateTestManifestWorkReplicaSet("test", "default", "placement")
				w.Finalizers = []string{workapiv1alpha1.ManifestWorkReplicaSetFinalizer}
				return w
			}(),
			works: []runtime.Object{},
			placement: func() *clusterv1beta1.Placement {
				p, _ := helpertest.CreateTestPlacement("placement1", "default")
				return p
			}(),
			decision: func() *clusterv1beta1.PlacementDecision {
				_, d := helpertest.CreateTestPlacement("placement", "default")
				return d
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				workSet := &workapiv1alpha1.ManifestWorkReplicaSet{}
				if err := json.Unmarshal(p, workSet); err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionFalse(workSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified) {
					t.Fatal(spew.Sdump(workSet.Status.Conditions))
				}
			},
		},
		{
			name: "placement decision not found",
			mwrSet: func() *workapiv1alpha1.ManifestWorkReplicaSet {
				w := helpertest.CreateTestManifestWorkReplicaSet("test", "default", "placement")
				w.Finalizers = []string{workapiv1alpha1.ManifestWorkReplicaSetFinalizer}
				return w
			}(),
			works: []runtime.Object{},
			placement: func() *clusterv1beta1.Placement {
				p, _ := helpertest.CreateTestPlacement("placement", "default")
				return p
			}(),
			decision: func() *clusterv1beta1.PlacementDecision {
				_, d := helpertest.CreateTestPlacement("placement1", "default")
				return d
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				workSet := &workapiv1alpha1.ManifestWorkReplicaSet{}
				if err := json.Unmarshal(p, workSet); err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionFalse(workSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied) {
					t.Fatal(spew.Sdump(workSet.Status.Conditions))
				}
			},
		},
		{
			name: "apply correctly",
			mwrSet: func() *workapiv1alpha1.ManifestWorkReplicaSet {
				w := helpertest.CreateTestManifestWorkReplicaSet("test", "default", "placement")
				w.Finalizers = []string{workapiv1alpha1.ManifestWorkReplicaSetFinalizer}
				return w
			}(),
			works: []runtime.Object{},
			placement: func() *clusterv1beta1.Placement {
				p, _ := helpertest.CreateTestPlacement("placement", "default", "cluster1", "cluster2")
				return p
			}(),
			decision: func() *clusterv1beta1.PlacementDecision {
				_, d := helpertest.CreateTestPlacement("placement", "default", "cluster1", "cluster2")
				return d
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "create", "patch")
				p := actions[2].(clienttesting.PatchActionImpl).Patch
				workSet := &workapiv1alpha1.ManifestWorkReplicaSet{}
				if err := json.Unmarshal(p, workSet); err != nil {
					t.Fatal(err)
				}
				if workSet.Status.Summary.Total != 2 {
					t.Error(spew.Sdump(workSet.Status.Summary))
				}
				if !meta.IsStatusConditionFalse(workSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied) {
					t.Error(spew.Sdump(workSet.Status.Conditions))
				}
			},
		},
		{
			name: "no additional apply needed",
			mwrSet: func() *workapiv1alpha1.ManifestWorkReplicaSet {
				w := helpertest.CreateTestManifestWorkReplicaSet("test", "default", "placement")
				w.Finalizers = []string{workapiv1alpha1.ManifestWorkReplicaSetFinalizer}
				return w
			}(),
			works: helpertest.CreateTestManifestWorks("test", "default", "placement", "cluster1", "cluster2"),
			placement: func() *clusterv1beta1.Placement {
				p, _ := helpertest.CreateTestPlacement("placement", "default", "cluster1", "cluster2")
				return p
			}(),
			decision: func() *clusterv1beta1.PlacementDecision {
				_, d := helpertest.CreateTestPlacement("placement", "default", "cluster1", "cluster2")
				return d
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				workSet := &workapiv1alpha1.ManifestWorkReplicaSet{}
				if err := json.Unmarshal(p, workSet); err != nil {
					t.Fatal(err)
				}
				if workSet.Status.Summary.Applied != 2 {
					t.Error(spew.Sdump(workSet.Status.Summary))
				}
				if !meta.IsStatusConditionTrue(workSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied) {
					t.Error(spew.Sdump(workSet.Status.Conditions))
				}
			},
		},
		{
			name: "add and delete",
			mwrSet: func() *workapiv1alpha1.ManifestWorkReplicaSet {
				w := helpertest.CreateTestManifestWorkReplicaSet("test", "default", "placement")
				w.Finalizers = []string{workapiv1alpha1.ManifestWorkReplicaSetFinalizer}
				return w
			}(),
			works: helpertest.CreateTestManifestWorks("test", "default", "placement", "cluster1", "cluster2"),
			placement: func() *clusterv1beta1.Placement {
				p, _ := helpertest.CreateTestPlacement("placement", "default", "cluster2", "cluster3", "cluster4")
				return p
			}(),
			decision: func() *clusterv1beta1.PlacementDecision {
				_, d := helpertest.CreateTestPlacement("placement", "default", "cluster2", "cluster3", "cluster4")
				return d
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "create", "delete", "patch")
				p := actions[3].(clienttesting.PatchActionImpl).Patch
				workSet := &workapiv1alpha1.ManifestWorkReplicaSet{}
				if err := json.Unmarshal(p, workSet); err != nil {
					t.Fatal(err)
				}
				if workSet.Status.Summary.Total != 3 {
					t.Error(spew.Sdump(workSet.Status.Summary))
				}
				if !meta.IsStatusConditionFalse(workSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied) {
					t.Error(spew.Sdump(workSet.Status.Conditions))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			workObjects := []runtime.Object{c.mwrSet}
			workObjects = append(workObjects, c.works...)
			fakeClient := fakeworkclient.NewSimpleClientset(workObjects...)
			workInformers := workinformers.NewSharedInformerFactory(fakeClient, 10*time.Minute)
			err := workInformers.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(c.mwrSet)
			if err != nil {
				t.Fatal(err)
			}
			for _, o := range c.works {
				err = workInformers.Work().V1().ManifestWorks().Informer().GetStore().Add(o)
				if err != nil {
					t.Fatal(err)
				}
			}

			fakeClusterClient := fakeclusterclient.NewSimpleClientset(c.placement, c.decision)
			clusterInformers := clusterinformers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)
			err = clusterInformers.Cluster().V1beta1().Placements().Informer().GetStore().Add(c.placement)
			if err != nil {
				t.Fatal(err)
			}
			err = clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(c.decision)
			if err != nil {
				t.Fatal(err)
			}

			ctrl := newController(
				fakeClient,
				workapplier.NewWorkApplierWithTypedClient(fakeClient, workInformers.Work().V1().ManifestWorks().Lister()),
				workInformers.Work().V1alpha1().ManifestWorkReplicaSets(),
				workInformers.Work().V1().ManifestWorks(),
				clusterInformers.Cluster().V1beta1().Placements(),
				clusterInformers.Cluster().V1beta1().PlacementDecisions(),
			)

			controllerContext := testingcommon.NewFakeSyncContext(t, c.mwrSet.Namespace+"/"+c.mwrSet.Name)
			err = ctrl.sync(context.TODO(), controllerContext, c.mwrSet.Namespace+"/"+c.mwrSet.Name)
			if err != nil {
				t.Error(err)
			}

			c.validateActions(t, fakeClient.Actions())
		})
	}
}
