package managedclustersetbinding

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
)

func TestSync(t *testing.T) {
	cases := []struct {
		name              string
		clusterSets       []runtime.Object
		clusterSetBinding *clusterv1beta1.ManagedClusterSetBinding
		validateActions   func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:              "wrong clustersetbinding",
			clusterSets:       []runtime.Object{},
			clusterSetBinding: newManagedClusterSetBinding("test", ""),
			validateActions:   testinghelpers.AssertNoActions,
		},
		{
			name:              "no clusterset",
			clusterSets:       []runtime.Object{},
			clusterSetBinding: newManagedClusterSetBinding("test", "testns"),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "patch")
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				binding := &clusterv1beta1.ManagedClusterSetBinding{}
				err := json.Unmarshal(patchData, binding)
				if err != nil {
					t.Fatal(err)
				}

				testinghelpers.AssertCondition(t, binding.Status.Conditions, metav1.Condition{
					Type:   clusterv1beta1.ClusterSetBindingBoundType,
					Status: metav1.ConditionFalse,
					Reason: "ClusterSetNotFound",
				})
			},
		},
		{
			name:              "bound clusterset",
			clusterSets:       []runtime.Object{newManagedClusterSet("test")},
			clusterSetBinding: newManagedClusterSetBinding("test", "testns"),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "patch")
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				binding := &clusterv1beta1.ManagedClusterSetBinding{}
				err := json.Unmarshal(patchData, binding)
				if err != nil {
					t.Fatal(err)
				}

				testinghelpers.AssertCondition(t, binding.Status.Conditions, metav1.Condition{
					Type:   clusterv1beta1.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
					Reason: "ClusterSetBound",
				})
			},
		},
		{
			name:        "no update",
			clusterSets: []runtime.Object{newManagedClusterSet("test")},
			clusterSetBinding: func() *clusterv1beta1.ManagedClusterSetBinding {
				binding := newManagedClusterSetBinding("test", "testns")
				meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
					Type:   clusterv1beta1.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
					Reason: "ClusterSetBound",
				})
				return binding
			}(),
			validateActions: testinghelpers.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			objects = append(objects, c.clusterSets...)
			objects = append(objects, c.clusterSetBinding)

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			informerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, 5*time.Minute)
			for _, clusterset := range c.clusterSets {
				if err := informerFactory.Cluster().V1beta1().ManagedClusterSets().Informer().GetStore().Add(clusterset); err != nil {
					t.Fatal(err)
				}
			}
			if err := informerFactory.Cluster().V1beta1().ManagedClusterSetBindings().Informer().GetStore().Add(c.clusterSetBinding); err != nil {
				t.Fatal(err)
			}

			ctrl := managedClusterSetBindingController{
				clusterClient:           clusterClient,
				clusterSetBindingLister: informerFactory.Cluster().V1beta1().ManagedClusterSetBindings().Lister(),
				clusterSetLister:        informerFactory.Cluster().V1beta1().ManagedClusterSets().Lister(),
				eventRecorder:           eventstesting.NewTestingEventRecorder(t),
			}

			key, _ := cache.MetaNamespaceKeyFunc(c.clusterSetBinding)

			syncErr := ctrl.sync(context.Background(), testinghelpers.NewFakeSyncContext(t, key))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestEnqueue(t *testing.T) {
	cases := []struct {
		name               string
		clusterSet         *clusterv1beta1.ManagedClusterSet
		clusterSetBindings []runtime.Object
		expectedQueueSize  int
	}{
		{
			name:       "no binding found",
			clusterSet: newManagedClusterSet("test"),
			clusterSetBindings: []runtime.Object{
				newManagedClusterSetBinding("test1", "testns"),
			},
			expectedQueueSize: 0,
		},
		{
			name:       "multiple binding found",
			clusterSet: newManagedClusterSet("test"),
			clusterSetBindings: []runtime.Object{
				newManagedClusterSetBinding("test", "testns1"),
				newManagedClusterSetBinding("test1", "testns1"),
				newManagedClusterSetBinding("test", "testns2"),
			},
			expectedQueueSize: 2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			objects = append(objects, c.clusterSet)
			objects = append(objects, c.clusterSetBindings...)

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			informerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, 5*time.Minute)
			err := informerFactory.Cluster().V1beta1().ManagedClusterSetBindings().Informer().AddIndexers(cache.Indexers{
				byClusterSet: indexByClusterset,
			})

			if err != nil {
				t.Fatal(err)
			}

			for _, binding := range c.clusterSetBindings {
				if err := informerFactory.Cluster().V1beta1().ManagedClusterSetBindings().Informer().GetStore().Add(binding); err != nil {
					t.Fatal(err)
				}
			}

			syncCtx := testinghelpers.NewFakeSyncContext(t, "fake")

			ctrl := managedClusterSetBindingController{
				clusterSetBindingIndexers: informerFactory.Cluster().V1beta1().ManagedClusterSetBindings().Informer().GetIndexer(),
				queue:                     syncCtx.Queue(),
			}

			ctrl.enqueueBindingsByClusterSet(c.clusterSet)

			if c.expectedQueueSize != ctrl.queue.Len() {
				t.Errorf("expect queue %d item, but got %d", c.expectedQueueSize, ctrl.queue.Len())
			}
		})
	}
}

func newManagedClusterSet(name string) *clusterv1beta1.ManagedClusterSet {
	clusterSet := &clusterv1beta1.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	return clusterSet
}

func newManagedClusterSetBinding(name, namespace string) *clusterv1beta1.ManagedClusterSetBinding {
	return &clusterv1beta1.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: clusterv1beta1.ManagedClusterSetBindingSpec{
			ClusterSet: name,
		},
	}
}
