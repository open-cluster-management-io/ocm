package scheduling

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	cache "k8s.io/client-go/tools/cache"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	testinghelpers "open-cluster-management.io/placement/pkg/helpers/testing"
)

func TestEnqueuePlacementsByClusterSet(t *testing.T) {
	cases := []struct {
		name           string
		clusterSetName string
		initObjs       []runtime.Object
		queuedKeys     []string
	}{
		{
			name:           "enqueue placements in a namespace",
			clusterSetName: "clusterset1",
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSet("clusterset2").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewClusterSetBinding("ns1", "clusterset2"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns1", "placement2").WithClusterSets("clusterset2").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)

			queuedKeys := sets.NewString()
			err := enqueuePlacementsByClusterSet(
				c.clusterSetName,
				clusterInformerFactory.Cluster().V1beta1().ManagedClusterSetBindings().Lister(),
				clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
				func(namespace, name string) {
					queuedKeys.Insert(fmt.Sprintf("%s/%s", namespace, name))
				},
			)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			expectedQueuedKeys := sets.NewString(c.queuedKeys...)
			if !queuedKeys.Equal(expectedQueuedKeys) {
				t.Errorf("expected queued placements %q, but got %s", strings.Join(expectedQueuedKeys.List(), ","), strings.Join(queuedKeys.List(), ","))
			}
		})
	}
}

func TestOnClusterSetAdd(t *testing.T) {
	cases := []struct {
		name       string
		obj        interface{}
		initObjs   []runtime.Object
		queuedKeys []string
	}{
		{
			name: "invalid object type",
			obj:  "invalid object type",
		},
		{
			name: "clusterset",
			obj:  testinghelpers.NewClusterSet("clusterset1").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)

			queuedKeys := sets.NewString()
			handler := &clusterSetEventHandler{
				clusterSetBindingLister: clusterInformerFactory.Cluster().V1beta1().ManagedClusterSetBindings().Lister(),
				placementLister:         clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
				enqueuePlacementFunc: func(namespace, name string) {
					queuedKeys.Insert(fmt.Sprintf("%s/%s", namespace, name))
				},
			}

			handler.OnAdd(c.obj)
			expectedQueuedKeys := sets.NewString(c.queuedKeys...)
			if !queuedKeys.Equal(expectedQueuedKeys) {
				t.Errorf("expected queued placements %q, but got %s", strings.Join(expectedQueuedKeys.List(), ","), strings.Join(queuedKeys.List(), ","))
			}
		})
	}
}

func TestOnClusterSetDelete(t *testing.T) {
	cases := []struct {
		name       string
		obj        interface{}
		initObjs   []runtime.Object
		queuedKeys []string
	}{
		{
			name: "invalid object type",
			obj:  "invalid object type",
		},
		{
			name: "clusterset",
			obj:  testinghelpers.NewClusterSet("clusterset1").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
		{
			name: "tombstone",
			obj: cache.DeletedFinalStateUnknown{
				Obj: testinghelpers.NewClusterSet("clusterset1").Build(),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
		{
			name: "tombstone with invalid object type",
			obj: cache.DeletedFinalStateUnknown{
				Obj: "invalid object type",
			},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)

			queuedKeys := sets.NewString()
			handler := &clusterSetEventHandler{
				clusterSetBindingLister: clusterInformerFactory.Cluster().V1beta1().ManagedClusterSetBindings().Lister(),
				placementLister:         clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
				enqueuePlacementFunc: func(namespace, name string) {
					queuedKeys.Insert(fmt.Sprintf("%s/%s", namespace, name))
				},
			}

			handler.OnDelete(c.obj)
			expectedQueuedKeys := sets.NewString(c.queuedKeys...)
			if !queuedKeys.Equal(expectedQueuedKeys) {
				t.Errorf("expected queued placements %q, but got %s", strings.Join(expectedQueuedKeys.List(), ","), strings.Join(queuedKeys.List(), ","))
			}
		})
	}
}
