package manifestworkreplicasetcontroller

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/apimachinery/pkg/api/meta"
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
