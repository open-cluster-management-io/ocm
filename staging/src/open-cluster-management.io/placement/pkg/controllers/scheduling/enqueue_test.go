package scheduling

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	testinghelpers "open-cluster-management.io/placement/pkg/helpers/testing"
	"strings"
	"testing"
	"time"
)

func newClusterInformerFactory(clusterClient clusterclient.Interface, objects ...runtime.Object) clusterinformers.SharedInformerFactory {
	clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)

	clusterInformerFactory.Cluster().V1beta1().Placements().Informer().AddIndexers(cache.Indexers{
		placementsByScore:             indexPlacementsByScore,
		placementsByClusterSetBinding: indexPlacementByClusterSetBinding,
	})

	clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Informer().AddIndexers(cache.Indexers{
		clustersetBindingsByClusterSet: indexClusterSetBindingByClusterSet,
	})

	clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
	clusterSetStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore()
	clusterSetBindingStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Informer().GetStore()
	placementStore := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore()
	placementDecisionStore := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore()
	addOnPlacementStore := clusterInformerFactory.Cluster().V1alpha1().AddOnPlacementScores().Informer().GetStore()

	for _, obj := range objects {
		switch obj.(type) {
		case *clusterapiv1.ManagedCluster:
			clusterStore.Add(obj)
		case *clusterapiv1beta2.ManagedClusterSet:
			clusterSetStore.Add(obj)
		case *clusterapiv1beta2.ManagedClusterSetBinding:
			clusterSetBindingStore.Add(obj)
		case *clusterapiv1beta1.Placement:
			placementStore.Add(obj)
		case *clusterapiv1beta1.PlacementDecision:
			placementDecisionStore.Add(obj)
		case *clusterapiv1alpha1.AddOnPlacementScore:
			addOnPlacementStore.Add(obj)
		}
	}

	return clusterInformerFactory
}

func TestEnqueuePlacementsByClusterSet(t *testing.T) {
	cases := []struct {
		name       string
		clusterSet interface{}
		initObjs   []runtime.Object
		queuedKeys []string
	}{
		{
			name:       "enqueue placements in a namespace",
			clusterSet: testinghelpers.NewClusterSet("clusterset1").Build(),
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
		{
			name:       "invalid object type",
			clusterSet: "invalid object type",
		},
		{
			name:       "clusterset selector type is LegacyClusterSetLabel",
			clusterSet: testinghelpers.NewClusterSet("clusterset1").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
		{
			name: "clusterset selector type is LabelSelector",
			clusterSet: testinghelpers.NewClusterSet("clusterset1").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
				SelectorType: clusterapiv1beta2.LabelSelector,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				},
			}).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
		{
			name:       "clusterset selector type is LegacyClusterSetLabel",
			clusterSet: testinghelpers.NewClusterSet("clusterset1").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
		{
			name: "clusterset selector type is LabelSelector",
			clusterSet: testinghelpers.NewClusterSet("clusterset1").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
				SelectorType: clusterapiv1beta2.LabelSelector,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				},
			}).Build(),
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
			clusterSet: cache.DeletedFinalStateUnknown{
				Key: "clusterset1",
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
			clusterSet: cache.DeletedFinalStateUnknown{
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
			q.enqueueClusterSet(c.clusterSet)

			expectedQueuedKeys := sets.NewString(c.queuedKeys...)
			if !queuedKeys.Equal(expectedQueuedKeys) {
				t.Errorf("expected queued placements %q, but got %s", strings.Join(expectedQueuedKeys.List(), ","), strings.Join(queuedKeys.List(), ","))
			}
		})
	}
}

func TestEnqueuePlacementsByClusterSetBinding(t *testing.T) {
	cases := []struct {
		name              string
		namespace         string
		clusterSetBinding interface{}
		initObjs          []runtime.Object
		queuedKeys        []string
	}{
		{
			name:              "enqueue placements by clusterSetBinding",
			namespace:         "ns1",
			clusterSetBinding: testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns1", "placement2").WithClusterSets("clusterset2").Build(),
				testinghelpers.NewPlacement("ns2", "placement3").WithClusterSets("clusterset1").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
		{
			name:              "invalid resource type",
			clusterSetBinding: "invalid resource type",
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").Build(),
			},
		},
		{
			name:              "on clustersetbinding change",
			clusterSetBinding: testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewPlacement("ns1", "placement1").Build(),
				testinghelpers.NewPlacement("ns2", "placement2").Build(),
			},
			queuedKeys: []string{
				"ns1/placement1",
			},
		},
		{
			name:              "clusterset",
			clusterSetBinding: testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
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
			clusterSetBinding: cache.DeletedFinalStateUnknown{
				Key: "ns1/clusterset1",
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
			clusterSetBinding: cache.DeletedFinalStateUnknown{
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
			q.enqueueClusterSetBinding(c.clusterSetBinding)

			expectedQueuedKeys := sets.NewString(c.queuedKeys...)
			if !queuedKeys.Equal(expectedQueuedKeys) {
				t.Errorf("expected queued placements %q, but got %s", strings.Join(expectedQueuedKeys.List(), ","), strings.Join(queuedKeys.List(), ","))
			}
		})
	}
}

func TestEnqueuePlacementsByScore(t *testing.T) {
	cases := []struct {
		name       string
		namespace  string
		score      interface{}
		initObjs   []runtime.Object
		queuedKeys []string
	}{
		{
			name:  "ensueue score",
			score: testinghelpers.NewAddOnPlacementScore("cluster1", "score1").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement("ns1", "placement1").WithScoreCoordinateAddOn("score1", "cpu", 1).Build(),
				testinghelpers.NewPlacement("ns2", "placement2").WithScoreCoordinateAddOn("score2", "cpu", 1).Build(),
				testinghelpers.NewPlacement("ns3", "placement3").WithScoreCoordinateAddOn("score1", "cpu", 1).Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset1").Build(),
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
				testinghelpers.NewClusterSetBinding("ns3", "clusterset1"),
			},
			queuedKeys: []string{
				"ns1/placement1",
				"ns3/placement3",
			},
		},
		{
			name:  "only enqueue score with filtered placement",
			score: testinghelpers.NewAddOnPlacementScore("cluster1", "score1").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement("ns1", "placement1").WithScoreCoordinateAddOn("score1", "cpu", 1).Build(),
				testinghelpers.NewPlacement("ns2", "placement2").WithScoreCoordinateAddOn("score2", "cpu", 1).Build(),
				testinghelpers.NewPlacement("ns3", "placement3").WithScoreCoordinateAddOn("score1", "cpu", 1).Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset1").Build(),
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "clusterset2"),
				testinghelpers.NewClusterSetBinding("ns3", "clusterset1"),
			},
			queuedKeys: []string{
				"ns3/placement3",
			},
		},
		{
			name: "tombstone",
			score: cache.DeletedFinalStateUnknown{
				Key: "cluster1/score1",
				Obj: testinghelpers.NewAddOnPlacementScore("cluster1", "score1").Build(),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement("ns1", "placement1").WithScoreCoordinateAddOn("score1", "cpu", 1).Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset1").Build(),
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding("ns1", "clusterset1"),
			},
			queuedKeys: []string{
				"ns1/placement1",
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
			q.enqueuePlacementScore(c.score)

			expectedQueuedKeys := sets.NewString(c.queuedKeys...)
			if !queuedKeys.Equal(expectedQueuedKeys) {
				t.Errorf("expected queued placements %q, but got %s", strings.Join(expectedQueuedKeys.List(), ","), strings.Join(queuedKeys.List(), ","))
			}
		})
	}
}
