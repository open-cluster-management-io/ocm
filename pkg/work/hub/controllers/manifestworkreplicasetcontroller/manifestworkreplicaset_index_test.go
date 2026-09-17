package manifestworkreplicasetcontroller

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	workapplier "open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"

	workhelper "open-cluster-management.io/ocm/pkg/work/helper"
	helpertest "open-cluster-management.io/ocm/pkg/work/hub/test"
)

func TestPlaceMWControllerIndex(t *testing.T) {
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	mwrSetTest.Status.Summary.Total = 1
	mw := buildManifestWork(mwrSetTest, "test-1", "cls1", "place-test")
	fWorkClient := fakeworkclient.NewSimpleClientset(mwrSetTest, mw)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().AddIndexers(
		cache.Indexers{manifestWorkReplicaSetByPlacement: indexManifestWorkReplicaSetByPlacement})

	if err != nil {
		t.Fatal(err)
	}

	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw); err != nil {
		t.Fatal(err)
	}
	if err := workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetStore().Add(mwrSetTest); err != nil {
		t.Fatal(err)
	}

	placement, placementDecision := helpertest.CreateTestPlacement("place-test", "default", "cls1")

	fClusterClient := fakeclusterclient.NewSimpleClientset(placement, placementDecision)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(fClusterClient, 1*time.Second)

	if err := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore().Add(placement); err != nil {
		t.Fatal(err)
	}
	if err := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(placementDecision); err != nil {
		t.Fatal(err)
	}

	mwLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
	placementLister := clusterInformerFactory.Cluster().V1beta1().Placements().Lister()
	placementDecisionLister := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister()

	pmwController := &ManifestWorkReplicaSetController{
		workClient:                    fWorkClient,
		manifestWorkReplicaSetLister:  workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Lister(),
		manifestWorkReplicaSetIndexer: workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets().Informer().GetIndexer(),

		reconcilers: []ManifestWorkReplicaSetReconcile{
			&finalizeReconciler{workApplier: workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
				workClient: fWorkClient, manifestWorkLister: mwLister},
			&addFinalizerReconciler{workClient: fWorkClient},
			&deployReconciler{workApplier: workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
				manifestWorkLister: mwLister, placementLister: placementLister, placeDecisionLister: placementDecisionLister},
			&statusReconciler{manifestWorkLister: mwLister},
		},
	}

	// Check index key creation
	placementKey, err := indexManifestWorkReplicaSetByPlacement(mwrSetTest)
	if err != nil {
		t.Fatal(err)
	}
	if len(placementKey) == 0 {
		t.Fatal("Key not created", placementKey)
	}
	if placementKey[0] != mwrSetTest.Namespace+"/"+mwrSetTest.Spec.PlacementRefs[0].Name {
		t.Fatal("placement Key not match ", placementKey[0])
	}

	expectedKey := mwrSetTest.Namespace + "/" + mwrSetTest.Name
	placeNotExist, placeDecisinNotExist := helpertest.CreateTestPlacement("place-notExist", "ns-notExist")
	// Check placement Queue Keys
	keys := pmwController.placementQueueKeysFunc(placement)
	if len(keys) == 0 {
		t.Fatal("placement index keys not exist")
	}
	if keys[0] != expectedKey {
		t.Fatal("Expected placement key not match ", keys[0], " - ", expectedKey)
	}
	// Check placement Queue Keys not exist
	keys = pmwController.placementQueueKeysFunc(placeNotExist)
	if len(keys) > 0 {
		t.Fatal("placement index keys should not exist ", keys)
	}

	// Check placementDecision Queue Keys
	keys = pmwController.placementDecisionQueueKeysFunc(placementDecision)
	if len(keys) == 0 {
		t.Fatal("placement decision index keys not exist")
	}
	if keys[0] != expectedKey {
		t.Fatal("Expected placementDecision key not match ", keys[0], " - ", expectedKey)
	}
	// Check placementDecision Queue Keys not exist
	keys = pmwController.placementDecisionQueueKeysFunc(placeDecisinNotExist)
	if len(keys) > 0 {
		t.Fatal("placement decision index keys should not exist ", keys)
	}

	// Check manifestWork Queue Keys
	key := pmwController.manifestWorkQueueKeyFunc(mw)
	if key != mwrSetTest.Namespace+"/"+mwrSetTest.Name {
		t.Fatal("Expected manifestwork key not match", key, " - ", mwrSetTest.Name)
	}
	// Check manifestWork Queue Keys not exist (clear both labels and annotations)
	mw.Labels = map[string]string{"testLabel": "label1"}
	mw.Annotations = nil
	key = pmwController.manifestWorkQueueKeyFunc(mw)
	if key != "" {
		t.Fatal("Expected manifestwork key should not exist ", key)
	}
}

func TestFilterByMWRSOwnership(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{
			name:     "no labels",
			labels:   nil,
			expected: false,
		},
		{
			name:     "unrelated labels only",
			labels:   map[string]string{"app": "test"},
			expected: false,
		},
		{
			name: "new hash label only",
			labels: map[string]string{
				workhelper.ManifestWorkReplicaSetOwnerKeyHashLabelKey: "abc123",
			},
			expected: true,
		},
		{
			name: "deprecated label only",
			labels: map[string]string{
				workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey: "default.mwrs",
			},
			expected: true,
		},
		{
			name: "both labels",
			labels: map[string]string{
				workhelper.ManifestWorkReplicaSetOwnerKeyHashLabelKey:        "abc123",
				workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey: "default.mwrs",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mw",
					Namespace: "cls1",
					Labels:    tt.labels,
				},
			}
			result := filterByMWRSOwnership(mw)
			if result != tt.expected {
				t.Errorf("filterByMWRSOwnership() = %v, want %v", result, tt.expected)
			}
		})
	}

	// Regression: tombstone wrapping must not panic or return wrong result.
	t.Run("tombstone with hash-labeled ManifestWork", func(t *testing.T) {
		mw := &workapiv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-mw",
				Namespace: "cls1",
				Labels: map[string]string{
					workhelper.ManifestWorkReplicaSetOwnerKeyHashLabelKey: "abc123",
				},
			},
		}
		tombstone := cache.DeletedFinalStateUnknown{Key: "cls1/test-mw", Obj: mw}
		if !filterByMWRSOwnership(tombstone) {
			t.Error("filterByMWRSOwnership(tombstone) = false, want true")
		}
	})

	t.Run("tombstone with non-MWRS object", func(t *testing.T) {
		mw := &workapiv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-mw",
				Namespace: "cls1",
				Labels:    map[string]string{"app": "other"},
			},
		}
		tombstone := cache.DeletedFinalStateUnknown{Key: "cls1/test-mw", Obj: mw}
		if filterByMWRSOwnership(tombstone) {
			t.Error("filterByMWRSOwnership(tombstone) = true, want false")
		}
	})
}

func TestManifestWorkQueueKeyFuncWithDottedNames(t *testing.T) {
	pmwController := &ManifestWorkReplicaSetController{}

	tests := []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
		expectedKey string
	}{
		{
			name: "annotation preferred over label",
			labels: map[string]string{
				workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey: "default.mwrs",
			},
			annotations: map[string]string{
				workhelper.ManifestWorkReplicaSetOwnerAnnotationKey: "default/mwrs",
			},
			expectedKey: "default/mwrs",
		},
		{
			name: "old label with dotted namespace handled by SplitN",
			labels: map[string]string{
				workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey: "my.ns.mwrs",
			},
			expectedKey: "my/ns.mwrs",
		},
		{
			name: "old label with simple namespace.name",
			labels: map[string]string{
				workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey: "default.mwrs",
			},
			expectedKey: "default/mwrs",
		},
		{
			name:        "no label or annotation",
			labels:      map[string]string{"unrelated": "value"},
			expectedKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-mw",
					Namespace:   "cls1",
					Labels:      tt.labels,
					Annotations: tt.annotations,
				},
			}
			key := pmwController.manifestWorkQueueKeyFunc(mw)
			if key != tt.expectedKey {
				t.Errorf("manifestWorkQueueKeyFunc() = %q, want %q", key, tt.expectedKey)
			}
		})
	}
}
