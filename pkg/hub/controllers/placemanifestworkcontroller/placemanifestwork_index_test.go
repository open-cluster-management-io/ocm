package placemanifestworkcontroller

import (
	"k8s.io/client-go/tools/cache"
	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	"open-cluster-management.io/api/utils/work/v1/workapplier"
	helpertest "open-cluster-management.io/work/pkg/hub/test"
	"testing"
	"time"
)

func TestPlaceMWControllerIndex(t *testing.T) {
	pmwTest := helpertest.CreateTestPlaceManifestWork("pmw-test", "default", "place-test")
	pmwTest.Status.PlacedManifestWorkSummary.Total = 1
	mw, _ := CreateManifestWork(pmwTest, "cls1")
	fWorkClient := fakeworkclient.NewSimpleClientset(pmwTest, mw)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

	err := workInformerFactory.Work().V1alpha1().PlaceManifestWorks().Informer().AddIndexers(
		cache.Indexers{placeManifestWorkByPlacement: indexPlacementManifestWorkByPlacement})

	if err != nil {
		t.Fatal(err)
	}

	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(mw); err != nil {
		t.Fatal(err)
	}
	if err := workInformerFactory.Work().V1alpha1().PlaceManifestWorks().Informer().GetStore().Add(pmwTest); err != nil {
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

	pmwController := &PlaceManifestWorkController{
		workClient:               fWorkClient,
		placeManifestWorkLister:  workInformerFactory.Work().V1alpha1().PlaceManifestWorks().Lister(),
		placeManifestWorkIndexer: workInformerFactory.Work().V1alpha1().PlaceManifestWorks().Informer().GetIndexer(),

		reconcilers: []placeManifestWorkReconcile{
			&finalizeReconciler{workApplier: workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
				workClient: fWorkClient, manifestWorkLister: mwLister},
			&addFinalizerReconciler{workClient: fWorkClient},
			&deployReconciler{workApplier: workapplier.NewWorkApplierWithTypedClient(fWorkClient, mwLister),
				manifestWorkLister: mwLister, placementLister: placementLister, placeDecisionLister: placementDecisionLister},
			&statusReconciler{manifestWorkLister: mwLister},
		},
	}

	// Check index key creation
	placementKey, err := indexPlacementManifestWorkByPlacement(pmwTest)
	if err != nil {
		t.Fatal(err)
	}
	if len(placementKey) == 0 {
		t.Fatal("Key not created", placementKey)
	}
	if placementKey[0] != pmwTest.Namespace+"/"+pmwTest.Spec.PlacementRef.Name {
		t.Fatal("placement Key not match ", placementKey[0])
	}

	expectedKey := pmwTest.Namespace + "/" + pmwTest.Name
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
	if key != pmwTest.Name {
		t.Fatal("Expected manifestwork key not match", key, " - ", pmwTest.Name)
	}
	// Check manifestWork Queue Keys not exist
	mw.Labels = map[string]string{"testLabel": "label1"}
	key = pmwController.manifestWorkQueueKeyFunc(mw)
	if key != "" {
		t.Fatal("Expected manifestwork key should not exist ", key)
	}
}
