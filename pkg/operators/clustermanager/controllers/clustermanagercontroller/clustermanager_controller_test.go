package clustermanagercontroller

import (
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	fakeapiregistration "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/fake"
	fakeoperatorlient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/registration-operator/pkg/helpers"
	testinghelper "open-cluster-management.io/registration-operator/pkg/helpers/testing"
)

type testController struct {
	controller            *clusterManagerController
	kubeClient            *fakekube.Clientset
	apiExtensionClient    *fakeapiextensions.Clientset
	apiRegistrationClient *fakeapiregistration.Clientset
	operatorClient        *fakeoperatorlient.Clientset
}

func newClusterManager(name string) *operatorapiv1.ClusterManager {
	return &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Finalizers: []string{clusterManagerFinalizer},
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "testregistration",
		},
	}
}

func newTestController(clustermanager *operatorapiv1.ClusterManager) *testController {
	kubeClient := fakekube.NewSimpleClientset()
	kubeInfomers := kubeinformers.NewSharedInformerFactory(kubeClient, 5*time.Minute)
	fakeOperatorClient := fakeoperatorlient.NewSimpleClientset(clustermanager)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)

	hubController := &clusterManagerController{
		clusterManagerClient: fakeOperatorClient.OperatorV1().ClusterManagers(),
		clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
		currentGeneration:    make([]int64, len(deploymentFiles)),
		configMapLister:      kubeInfomers.Core().V1().ConfigMaps().Lister(),
	}

	store := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
	store.Add(clustermanager)

	return &testController{
		controller:     hubController,
		operatorClient: fakeOperatorClient,
	}
}

func (t *testController) withKubeObject(objects ...runtime.Object) *testController {
	fakeKubeClient := fakekube.NewSimpleClientset(objects...)
	t.controller.kubeClient = fakeKubeClient
	t.kubeClient = fakeKubeClient
	return t
}

func (t *testController) withCRDObject(objects ...runtime.Object) *testController {
	fakeAPIExtensionClient := fakeapiextensions.NewSimpleClientset(objects...)
	t.controller.apiExtensionClient = fakeAPIExtensionClient
	t.apiExtensionClient = fakeAPIExtensionClient
	return t
}

func (t *testController) withAPIServiceObject(objects ...runtime.Object) *testController {
	fakeAPIRegistrationClient := fakeapiregistration.NewSimpleClientset(objects...)
	t.controller.apiRegistrationClient = fakeAPIRegistrationClient.ApiregistrationV1()
	t.apiRegistrationClient = fakeAPIRegistrationClient
	return t
}

func ensureObject(t *testing.T, object runtime.Object, hubCore *operatorapiv1.ClusterManager) {
	access, err := meta.Accessor(object)
	if err != nil {
		t.Errorf("Unable to access objectmeta: %v", err)
	}

	switch o := object.(type) {
	case *corev1.Namespace:
		testinghelper.AssertEqualNameNamespace(t, access.GetName(), "", helpers.ClusterManagerNamespace, "")
	case *appsv1.Deployment:
		if strings.Contains(o.Name, "registration") && hubCore.Spec.RegistrationImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
			t.Errorf("Registration image does not match to the expected.")
		}
		if strings.Contains(o.Name, "placement") && hubCore.Spec.PlacementImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
			t.Errorf("Placement image does not match to the expected.")
		}
	}
}

// TestSyncDeploy tests sync manifests of hub component
func TestSyncDeploy(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	controller := newTestController(clusterManager).withCRDObject().withKubeObject().withAPIServiceObject()
	syncContext := testinghelper.NewFakeSyncContext(t, "testhub")

	err := controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	createKubeObjects := []runtime.Object{}
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			createKubeObjects = append(createKubeObjects, object)
		}
	}

	// Check if resources are created as expected
	testinghelper.AssertEqualNumber(t, len(createKubeObjects), 23)
	for _, object := range createKubeObjects {
		ensureObject(t, object, clusterManager)
	}

	createCRDObjects := []runtime.Object{}
	crdActions := controller.apiExtensionClient.Actions()
	for _, action := range crdActions {
		if action.GetVerb() == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			createCRDObjects = append(createCRDObjects, object)
		}
	}
	// Check if resources are created as expected
	testinghelper.AssertEqualNumber(t, len(createCRDObjects), 8)

	createAPIServiceObjects := []runtime.Object{}
	apiServiceActions := controller.apiRegistrationClient.Actions()
	for _, action := range apiServiceActions {
		if action.GetVerb() == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			createAPIServiceObjects = append(createAPIServiceObjects, object)
		}
	}
	// Check if resources are created as expected
	testinghelper.AssertEqualNumber(t, len(createAPIServiceObjects), 2)

	clusterManagerAction := controller.operatorClient.Actions()
	testinghelper.AssertEqualNumber(t, len(clusterManagerAction), 2)
	testinghelper.AssertAction(t, clusterManagerAction[1], "update")
	testinghelper.AssertOnlyConditions(
		t, clusterManagerAction[1].(clienttesting.UpdateActionImpl).Object,
		testinghelper.NamedCondition(clusterManagerApplied, "ClusterManagerApplied", metav1.ConditionTrue))
}

// TestSyncDelete test cleanup hub deploy
func TestSyncDelete(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	now := metav1.Now()
	clusterManager.ObjectMeta.SetDeletionTimestamp(&now)
	controller := newTestController(clusterManager).withCRDObject().withKubeObject().withAPIServiceObject()
	syncContext := testinghelper.NewFakeSyncContext(t, "testhub")

	err := controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	deleteKubeActions := []clienttesting.DeleteActionImpl{}
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "delete" {
			deleteKubeAction := action.(clienttesting.DeleteActionImpl)
			deleteKubeActions = append(deleteKubeActions, deleteKubeAction)
		}
	}
	testinghelper.AssertEqualNumber(t, len(deleteKubeActions), 19)

	deleteCRDActions := []clienttesting.DeleteActionImpl{}
	crdActions := controller.apiExtensionClient.Actions()
	for _, action := range crdActions {
		if action.GetVerb() == "delete" {
			deleteCRDAction := action.(clienttesting.DeleteActionImpl)
			deleteCRDActions = append(deleteCRDActions, deleteCRDAction)
		}
	}
	// Check if resources are created as expected
	testinghelper.AssertEqualNumber(t, len(deleteCRDActions), 10)

	deleteAPIServiceActions := []clienttesting.DeleteActionImpl{}
	apiServiceActions := controller.apiRegistrationClient.Actions()
	for _, action := range apiServiceActions {
		if action.GetVerb() == "delete" {
			deleteAPIServiceAction := action.(clienttesting.DeleteActionImpl)
			deleteAPIServiceActions = append(deleteAPIServiceActions, deleteAPIServiceAction)
		}
	}
	// Check if resources are created as expected
	testinghelper.AssertEqualNumber(t, len(deleteAPIServiceActions), 2)

	for _, action := range deleteKubeActions {
		switch action.Resource.Resource {
		case "namespaces":
			testinghelper.AssertEqualNameNamespace(t, action.Name, "", helpers.ClusterManagerNamespace, "")
		}
	}
}

// TestDeleteCRD test delete crds
func TestDeleteCRD(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	now := metav1.Now()
	clusterManager.ObjectMeta.SetDeletionTimestamp(&now)
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: crdNames[0],
		},
	}
	controller := newTestController(clusterManager).withCRDObject(crd).withKubeObject().withAPIServiceObject()

	// Return crd with the first get, and return not found with the 2nd get
	getCount := 0
	controller.apiExtensionClient.PrependReactor("get", "customresourcedefinitions", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		if getCount == 0 {
			getCount = getCount + 1
			return true, crd, nil
		}
		return true, &apiextensionsv1.CustomResourceDefinition{}, errors.NewNotFound(
			apiextensionsv1.Resource("customresourcedefinitions"), crdNames[0])

	})
	syncContext := testinghelper.NewFakeSyncContext(t, "testhub")
	err := controller.controller.sync(nil, syncContext)
	if err == nil {
		t.Errorf("Expected error when sync")
	}

	err = controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected no error when sync: %v", err)
	}
}
