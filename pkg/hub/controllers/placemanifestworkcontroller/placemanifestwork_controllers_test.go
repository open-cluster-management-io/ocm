package placemanifestworkcontroller

import (
	"context"
	clienttesting "k8s.io/client-go/testing"
	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	"open-cluster-management.io/api/utils/work/v1/workapplier"
	helpertest "open-cluster-management.io/work/pkg/hub/test"
	"testing"
	"time"
)

func TestPlaceMWControllerPatchStatus(t *testing.T) {
	pmwTest := helpertest.CreateTestPlaceManifestWork("pmw-test", "default", "place-test")
	pmwTest.Status.PlacedManifestWorkSummary.Total = 1
	mw, _ := CreateManifestWork(pmwTest, "cls1")
	fWorkClient := fakeworkclient.NewSimpleClientset(pmwTest, mw)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fWorkClient, 1*time.Second)

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

	// create new pmwTestNew with status
	pmwTestNew := helpertest.CreateTestPlaceManifestWork("pmw-test-new", "default", "place-test-new")
	pmwTestNew.Status.PlacedManifestWorkSummary.Total = pmwTest.Status.PlacedManifestWorkSummary.Total + 3

	err := pmwController.patchPlaceManifestStatus(context.TODO(), pmwTest, pmwTestNew)
	if err != nil {
		t.Fatal(err)
	}

	// Check kubeClient has a patch action to apply
	actions := ([]clienttesting.Action)(fWorkClient.Actions())
	if len(actions) == 0 {
		t.Fatal("fWorkClient Should have patch action ")
	}
	// Check placeMW patch name
	if pmwTest.Name != actions[0].(clienttesting.PatchAction).GetName() {
		t.Fatal("PlaceMW patch action not match ", actions[0])
	}
}
