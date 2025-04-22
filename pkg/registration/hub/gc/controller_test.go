package gc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakemetadataclient "k8s.io/client-go/metadata/fake"
	clienttesting "k8s.io/client-go/testing"
	clocktesting "k8s.io/utils/clock/testing"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestGController(t *testing.T) {
	cases := []struct {
		name            string
		key             string
		cluster         *clusterv1.ManagedCluster
		objs            []runtime.Object
		validateActions func(t *testing.T, clusterActions []clienttesting.Action)
	}{
		{
			name:    "invalid key",
			key:     factory.DefaultQueueKey,
			cluster: testinghelpers.NewDeletingManagedCluster(),

			validateActions: func(t *testing.T, clusterActions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, clusterActions)
			},
		},
		{
			name:    " cluster is not deleting",
			key:     testinghelpers.TestManagedClusterName,
			cluster: testinghelpers.NewManagedCluster(),
			validateActions: func(t *testing.T, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, clusterActions, "patch")
				patch := clusterActions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				if !commonhelpers.HasFinalizer(managedCluster.Finalizers, commonhelpers.GcFinalizer) {
					t.Errorf("expected gc finalizer")
				}
			},
		},
		{
			name:    "cluster is deleting with no resources",
			key:     testinghelpers.TestManagedClusterName,
			cluster: testinghelpers.NewDeletingManagedClusterWithFinalizers([]string{commonhelpers.GcFinalizer}),
			validateActions: func(t *testing.T, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, clusterActions, "patch", "patch")
				expectedCondition := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionDeleting,
					Status:  metav1.ConditionTrue,
					Reason:  clusterv1.ConditionDeletingReasonNoResource,
					Message: "No cleaned resource in cluster ns."}

				patch := clusterActions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				if err := json.Unmarshal(patch, managedCluster); err != nil {
					t.Fatal(err)
				}
				testingcommon.AssertCondition(t, managedCluster.Status.Conditions, expectedCondition)

				patch = clusterActions[1].(clienttesting.PatchAction).GetPatch()
				if err := json.Unmarshal(patch, managedCluster); err != nil {
					t.Fatal(err)
				}
				if len(managedCluster.Finalizers) != 0 {
					t.Errorf("expected no finalizer")
				}
			},
		},
		{
			name:    "cluster is deleting with resources",
			key:     testinghelpers.TestManagedClusterName,
			cluster: testinghelpers.NewDeletingManagedClusterWithFinalizers([]string{commonhelpers.GcFinalizer}),
			objs:    []runtime.Object{newAddonMetadata(testinghelpers.TestManagedClusterName, "test", nil)},
			validateActions: func(t *testing.T, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, clusterActions, "patch")
				expectedCondition := metav1.Condition{
					Type:   clusterv1.ManagedClusterConditionDeleting,
					Status: metav1.ConditionFalse,
					Reason: clusterv1.ConditionDeletingReasonResourceRemaining,
					Message: "The resource managedclusteraddons is remaning, the remaining count is 1, " +
						"the finalizer pending count is 0",
				}

				patch := clusterActions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				if err := json.Unmarshal(patch, managedCluster); err != nil {
					t.Fatal(err)
				}
				testingcommon.AssertCondition(t, managedCluster.Status.Conditions, expectedCondition)

			},
		},
		{
			name:    "cluster is gone with resources",
			key:     "test",
			cluster: testinghelpers.NewDeletingManagedClusterWithFinalizers([]string{commonhelpers.GcFinalizer}),
			objs:    []runtime.Object{newAddonMetadata(testinghelpers.TestManagedClusterName, "test", nil)},
			validateActions: func(t *testing.T, clusterActions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, clusterActions)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scheme := fakemetadataclient.NewTestScheme()
			_ = addonv1alpha1.Install(scheme)
			_ = workv1.Install(scheme)
			_ = metav1.AddMetaToScheme(scheme)
			metadataClient := fakemetadataclient.NewSimpleMetadataClient(scheme, c.objs...)

			clusterClient := fakeclusterclient.NewSimpleClientset(c.cluster)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			if c.cluster != nil {
				if err := clusterStore.Add(c.cluster); err != nil {
					t.Fatal(err)
				}
			}

			_ = NewGCController(
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				clusterClient,
				metadataClient,
				events.NewInMemoryRecorder("", clocktesting.NewFakePassiveClock(time.Now())),
				[]string{"addon.open-cluster-management.io/v1alpha1/managedclusteraddons",
					"work.open-cluster-management.io/v1/manifestworks"},
			)

			clusterPatcher := patcher.NewPatcher[
				*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
				clusterClient.ClusterV1().ManagedClusters())

			ctrl := &GCController{
				clusterLister:         clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterPatcher:        clusterPatcher,
				gcResourcesController: newGCResourcesController(metadataClient, []schema.GroupVersionResource{addonGvr, workGvr}),
				eventRecorder:         events.NewInMemoryRecorder("", clocktesting.NewFakePassiveClock(time.Now())),
			}

			controllerContext := testingcommon.NewFakeSyncContext(t, c.key)
			err := ctrl.sync(context.TODO(), controllerContext)
			if err != nil && !errors.Is(err, requeueError) {
				t.Error(err)
			}
			c.validateActions(t, clusterClient.Actions())
		})
	}
}
