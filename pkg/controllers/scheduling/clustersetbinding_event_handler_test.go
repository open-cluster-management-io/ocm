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

func TestOnClusterSetBindingChange(t *testing.T) {
	cases := []struct {
		name       string
		obj        interface{}
		initObjs   []runtime.Object
		queuedKeys []string
	}{
		{
			name: "invalid resource type",
			obj:  "invalid resource type",
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").Build(),
			},
		},
		{
			name: "clusterset does not exist",
			obj:  testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").Build(),
			},
		},
		{
			name: "on clustersetbinding change",
			obj:  testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").Build(),
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
			handler := &clusterSetBindingEventHandler{
				clusterSetLister: clusterInformerFactory.Cluster().V1beta1().ManagedClusterSets().Lister(),
				placementLister:  clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
				enqueuePlacementFunc: func(namespace, name string) {
					queuedKeys.Insert(fmt.Sprintf("%s/%s", namespace, name))
				},
			}

			handler.onChange(c.obj)
			expectedQueuedKeys := sets.NewString(c.queuedKeys...)
			if !queuedKeys.Equal(expectedQueuedKeys) {
				t.Errorf("expected queued placements %q, but got %s", strings.Join(expectedQueuedKeys.List(), ","), strings.Join(queuedKeys.List(), ","))
			}
		})
	}
}

func TestEnqueuePlacementsByClusterSetBinding(t *testing.T) {
	cases := []struct {
		name                  string
		namespace             string
		clusterSetBindingName string
		initObjs              []runtime.Object
		queuedKeys            []string
	}{
		{
			name:                  "enqueue placements by clusterSetBinding",
			namespace:             "ns1",
			clusterSetBindingName: "clusterset1",
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns1", "placement2").WithClusterSets("clusterset2").Build(),
				testinghelpers.NewPlacement("ns2", "placement3").WithClusterSets("clusterset1").Build(),
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
			err := enqueuePlacementsByClusterSetBinding(
				c.namespace,
				c.clusterSetBindingName,
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

func TestOnClusterSetBindingDelete(t *testing.T) {
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
			obj:  testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
		{
			name: "tombstone",
			obj: cache.DeletedFinalStateUnknown{
				Obj: testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
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
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)

			queuedKeys := sets.NewString()
			handler := &clusterSetBindingEventHandler{
				clusterSetLister: clusterInformerFactory.Cluster().V1beta1().ManagedClusterSets().Lister(),
				placementLister:  clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
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
