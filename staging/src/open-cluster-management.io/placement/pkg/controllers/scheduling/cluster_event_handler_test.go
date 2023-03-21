package scheduling

import (
	"k8s.io/client-go/util/workqueue"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	testinghelpers "open-cluster-management.io/placement/pkg/helpers/testing"
)

func TestOnClusterChange(t *testing.T) {
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
			obj:  testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset1").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").Build(),
			},
		},
		{
			name: "clusterset exists",
			obj:  testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset1").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSet("clusterset2").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewClusterSetBinding("ns2", "clusterset1"),
				testinghelpers.NewClusterSetBinding("ns2", "clusterset2"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").WithClusterSets("clusterset1").Build(),
				testinghelpers.NewPlacement("ns2", "placement3").WithClusterSets("clusterset2").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
				"ns2/placement2",
			},
		},
		{
			name: "cluster blongs to multiple clusterset",
			obj:  testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").WithLabel(clusterSetLabel, "clusterset2").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("global").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType:  clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{},
				}).Build(),
				testinghelpers.NewClusterSet("clusterset1").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType: clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"cloud": "Amazon",
						},
					},
				}).Build(),
				testinghelpers.NewClusterSet("clusterset2").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "global"),
				testinghelpers.NewClusterSetBinding("ns2", "clusterset1"),
				testinghelpers.NewClusterSetBinding("ns3", "clusterset2"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").Build(),
				testinghelpers.NewPlacement("ns3", "placement3").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
				"ns2/placement2",
				"ns3/placement3",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := newClusterInformerFactory(clusterClient, c.initObjs...)

			syncCtx := testinghelpers.NewFakeSyncContext(t, "fake")
			q := newEnqueuer(
				syncCtx.Queue(),
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets(),
				clusterInformerFactory.Cluster().V1beta1().Placements(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings(),
			)
			queuedKeys := sets.NewString()
			fakeEnqueuePlacement := func(obj interface{}, queue workqueue.RateLimitingInterface) {
				key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
				queuedKeys.Insert(key)
			}
			q.enqueuePlacementFunc = fakeEnqueuePlacement

			q.enqueueCluster(c.obj)
			expectedQueuedKeys := sets.NewString(c.queuedKeys...)
			if !queuedKeys.Equal(expectedQueuedKeys) {
				t.Errorf("expected queued placements %q, but got %s", strings.Join(expectedQueuedKeys.List(), ","), strings.Join(queuedKeys.List(), ","))
			}
		})
	}
}

func TestOnClusterUpdate(t *testing.T) {
	cases := []struct {
		name       string
		newObj     interface{}
		oldObj     interface{}
		initObjs   []runtime.Object
		queuedKeys []string
	}{
		{
			name:   "cluster belongs to no clusterset",
			newObj: testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
			oldObj: testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Google").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
		},
		{
			name:   "cluster blongs to multiple clusterset",
			newObj: testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset2").WithLabel("cloud", "Google").Build(),
			oldObj: testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset2").WithLabel("cloud", "Amazon").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("global").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType:  clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{},
				}).Build(),
				testinghelpers.NewClusterSet("clusterset1").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType: clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"cloud": "Amazon",
						},
					},
				}).Build(),
				testinghelpers.NewClusterSet("clusterset2").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "global"),
				testinghelpers.NewClusterSetBinding("ns2", "clusterset1"),
				testinghelpers.NewClusterSetBinding("ns3", "clusterset2"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").Build(),
				testinghelpers.NewPlacement("ns3", "placement3").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
				"ns2/placement2",
				"ns3/placement3",
			},
		},
		{
			name: "assign a cluster to a clusterset",
			newObj: testinghelpers.NewManagedCluster("cluster1").
				WithLabel(clusterSetLabel, "clusterset1").WithLabel("cloud", "Amazon").Build(),
			oldObj: testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
		{
			name:   "remove cluster from a clusterset",
			newObj: testinghelpers.NewManagedCluster("cluster1").Build(),
			oldObj: testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset2").WithLabel("cloud", "Amazon").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("global").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType:  clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{},
				}).Build(),
				testinghelpers.NewClusterSet("clusterset1").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType: clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"cloud": "Amazon",
						},
					},
				}).Build(),
				testinghelpers.NewClusterSet("clusterset2").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "global"),
				testinghelpers.NewClusterSetBinding("ns2", "clusterset1"),
				testinghelpers.NewClusterSetBinding("ns3", "clusterset2"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").Build(),
				testinghelpers.NewPlacement("ns3", "placement3").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
				"ns2/placement2",
				"ns3/placement3",
			},
		},
		{
			name: "label change only",
			newObj: testinghelpers.NewManagedCluster("cluster1").
				WithLabel(clusterSetLabel, "clusterset1").WithLabel("cloud", "Amazon").Build(),
			oldObj: testinghelpers.NewManagedCluster("cluster1").
				WithLabel(clusterSetLabel, "clusterset1").WithLabel("cloud", "google").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
		{
			name: "move cluster from one clusterset to another",
			newObj: testinghelpers.NewManagedCluster("cluster1").
				WithLabel(clusterSetLabel, "clusterset2").WithLabel("cloud", "Amazon").Build(),
			oldObj: testinghelpers.NewManagedCluster("cluster1").
				WithLabel(clusterSetLabel, "clusterset1").WithLabel("cloud", "Amazon").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSet("clusterset2").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewClusterSetBinding("ns2", "clusterset2"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
				"ns2/placement2",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := newClusterInformerFactory(clusterClient, c.initObjs...)

			syncCtx := testinghelpers.NewFakeSyncContext(t, "fake")
			q := newEnqueuer(
				syncCtx.Queue(),
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets(),
				clusterInformerFactory.Cluster().V1beta1().Placements(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings(),
			)
			queuedKeys := sets.NewString()
			fakeEnqueuePlacement := func(obj interface{}, queue workqueue.RateLimitingInterface) {
				key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
				queuedKeys.Insert(key)
			}
			q.enqueuePlacementFunc = fakeEnqueuePlacement
			handler := &clusterEventHandler{
				enqueuer: q,
			}

			handler.OnUpdate(c.oldObj, c.newObj)
			expectedQueuedKeys := sets.NewString(c.queuedKeys...)
			if !queuedKeys.Equal(expectedQueuedKeys) {
				t.Errorf("expected queued placements %q, but got %s", strings.Join(expectedQueuedKeys.List(), ","), strings.Join(queuedKeys.List(), ","))
			}
		})
	}
}

func TestOnClusterDelete(t *testing.T) {
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
			name: "cluster",
			obj:  testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset2").WithLabel("cloud", "Amazon").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("global").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType:  clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{},
				}).Build(),
				testinghelpers.NewClusterSet("clusterset1").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType: clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"cloud": "Amazon",
						},
					},
				}).Build(),
				testinghelpers.NewClusterSet("clusterset2").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "global"),
				testinghelpers.NewClusterSetBinding("ns2", "clusterset1"),
				testinghelpers.NewClusterSetBinding("ns3", "clusterset2"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").Build(),
				testinghelpers.NewPlacement("ns3", "placement3").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
				"ns2/placement2",
				"ns3/placement3",
			},
		},
		{
			name: "tombstone",
			obj: cache.DeletedFinalStateUnknown{
				Obj: testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset1").Build(),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
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
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := newClusterInformerFactory(clusterClient, c.initObjs...)

			syncCtx := testinghelpers.NewFakeSyncContext(t, "fake")
			q := newEnqueuer(
				syncCtx.Queue(),
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets(),
				clusterInformerFactory.Cluster().V1beta1().Placements(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings(),
			)
			queuedKeys := sets.NewString()
			fakeEnqueuePlacement := func(obj interface{}, queue workqueue.RateLimitingInterface) {
				key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
				queuedKeys.Insert(key)
			}
			q.enqueuePlacementFunc = fakeEnqueuePlacement
			handler := &clusterEventHandler{
				enqueuer: q,
			}

			handler.OnDelete(c.obj)
			expectedQueuedKeys := sets.NewString(c.queuedKeys...)
			if !queuedKeys.Equal(expectedQueuedKeys) {
				t.Errorf("expected queued placements %q, but got %s", strings.Join(expectedQueuedKeys.List(), ","), strings.Join(queuedKeys.List(), ","))
			}
		})
	}
}
