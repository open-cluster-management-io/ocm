package clustermanager

import (
	"context"
	"reflect"
	"testing"
	"time"

	fakeoperatorlient "github.com/open-cluster-management/api/client/operator/clientset/versioned/fake"
	operatorinformers "github.com/open-cluster-management/api/client/operator/informers/externalversions"
	operatorapiv1 "github.com/open-cluster-management/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/workqueue"
	fakeapiregistration "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/fake"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
)

type testController struct {
	controller            *clusterManagerController
	kubeClient            *fakekube.Clientset
	apiExtensionClient    *fakeapiextensions.Clientset
	apiRegistrationClient *fakeapiregistration.Clientset
	operatorClient        *fakeoperatorlient.Clientset
}

type fakeSyncContext struct {
	key      string
	queue    workqueue.RateLimitingInterface
	recorder events.Recorder
}

func (f fakeSyncContext) Queue() workqueue.RateLimitingInterface { return f.queue }
func (f fakeSyncContext) QueueKey() string                       { return f.key }
func (f fakeSyncContext) Recorder() events.Recorder              { return f.recorder }

func newFakeSyncContext(t *testing.T, key string) *fakeSyncContext {
	return &fakeSyncContext{
		key:      key,
		queue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		recorder: eventstesting.NewTestingEventRecorder(t),
	}
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
	fakeOperatorClient := fakeoperatorlient.NewSimpleClientset(clustermanager)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)

	hubController := &clusterManagerController{
		clusterManagerClient: fakeOperatorClient.OperatorV1().ClusterManagers(),
		clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
		currentGeneration:    make([]int64, len(deploymentFiles)),
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

func assertAction(t *testing.T, actual clienttesting.Action, expected string) {
	if actual.GetVerb() != expected {
		t.Errorf("expected %s action but got: %#v", expected, actual)
	}
}

func assertEqualNumber(t *testing.T, actual, expected int) {
	if actual != expected {
		t.Errorf("expected %d number of actions but got: %d", expected, actual)
	}
}

func assertCondition(t *testing.T, actual runtime.Object, expectedCondition string, expectedStatus metav1.ConditionStatus) {
	hubCore := actual.(*operatorapiv1.ClusterManager)
	conditions := hubCore.Status.Conditions
	if len(conditions) != 1 {
		t.Errorf("expected 1 condition but got: %#v", conditions)
	}
	condition := conditions[0]
	if condition.Type != expectedCondition {
		t.Errorf("expected %s but got: %s", expectedCondition, condition.Type)
	}
	if condition.Status != expectedStatus {
		t.Errorf("expected %s but got: %s", expectedStatus, condition.Status)
	}
}

func ensureNameNamespace(t *testing.T, actualName, actualNamespace, name, namespace string) {
	if actualName != name {
		t.Errorf("Name of the object does not match, expected %s, actual %s", name, actualName)
	}

	if actualNamespace != namespace {
		t.Errorf("Namespace of the object does not match, expected %s, actual %s", namespace, actualNamespace)
	}
}

func ensureObject(t *testing.T, object runtime.Object, hubCore *operatorapiv1.ClusterManager) {
	access, err := meta.Accessor(object)
	if err != nil {
		t.Errorf("Unable to access objectmeta: %v", err)
	}

	switch o := object.(type) {
	case *corev1.Namespace:
		ensureNameNamespace(t, access.GetName(), "", clusterManagerNamespace, "")
	case *appsv1.Deployment:
		if hubCore.Spec.RegistrationImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
			t.Errorf("Image does not match to the expected.")
		}
	}
}

// TestSyncDeploy tests sync manifests of hub component
func TestSyncDeploy(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	controller := newTestController(clusterManager).withCRDObject().withKubeObject().withAPIServiceObject()
	syncContext := newFakeSyncContext(t, "testhub")

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
	assertEqualNumber(t, len(createKubeObjects), 12)
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
	assertEqualNumber(t, len(createCRDObjects), 2)

	createAPIServiceObjects := []runtime.Object{}
	apiServiceActions := controller.apiRegistrationClient.Actions()
	for _, action := range apiServiceActions {
		if action.GetVerb() == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			createAPIServiceObjects = append(createAPIServiceObjects, object)
		}
	}
	// Check if resources are created as expected
	assertEqualNumber(t, len(createAPIServiceObjects), 1)

	clusterManagerAction := controller.operatorClient.Actions()
	assertEqualNumber(t, len(clusterManagerAction), 2)
	assertAction(t, clusterManagerAction[1], "update")
	assertCondition(t, clusterManagerAction[1].(clienttesting.UpdateActionImpl).Object, clusterManagerApplied, metav1.ConditionTrue)
}

// TestSyncDelete test cleanup hub deploy
func TestSyncDelete(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	now := metav1.Now()
	clusterManager.ObjectMeta.SetDeletionTimestamp(&now)
	controller := newTestController(clusterManager).withCRDObject().withKubeObject().withAPIServiceObject()
	syncContext := newFakeSyncContext(t, "testhub")

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
	assertEqualNumber(t, len(deleteKubeActions), 10)

	deleteCRDActions := []clienttesting.DeleteActionImpl{}
	crdActions := controller.apiExtensionClient.Actions()
	for _, action := range crdActions {
		if action.GetVerb() == "delete" {
			deleteCRDAction := action.(clienttesting.DeleteActionImpl)
			deleteCRDActions = append(deleteCRDActions, deleteCRDAction)
		}
	}
	// Check if resources are created as expected
	assertEqualNumber(t, len(deleteCRDActions), 4)

	deleteAPIServiceActions := []clienttesting.DeleteActionImpl{}
	apiServiceActions := controller.apiRegistrationClient.Actions()
	for _, action := range apiServiceActions {
		if action.GetVerb() == "delete" {
			deleteAPIServiceAction := action.(clienttesting.DeleteActionImpl)
			deleteAPIServiceActions = append(deleteAPIServiceActions, deleteAPIServiceAction)
		}
	}
	// Check if resources are created as expected
	assertEqualNumber(t, len(deleteAPIServiceActions), 1)

	for _, action := range deleteKubeActions {
		switch action.Resource.Resource {
		case "namespaces":
			ensureNameNamespace(t, action.Name, "", clusterManagerNamespace, "")
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
	syncContext := newFakeSyncContext(t, "testhub")
	err := controller.controller.sync(nil, syncContext)
	if err == nil {
		t.Errorf("Expected error when sync")
	}

	err = controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected no error when sync: %v", err)
	}
}

func TestEnsureServingCertAndCA(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	controller := newTestController(clusterManager).withCRDObject().withKubeObject().withAPIServiceObject()
	ca, certificate, key, err := controller.controller.ensureServingCertAndCA(context.TODO(), "ns1", "kubeconfig", "webhook")
	if err != nil {
		t.Errorf("Expect no error when generating serving cert: %v", err)
	}
	certs, err := cert.ParseCertsPEM(certificate)
	if err != nil {
		t.Errorf("Expect no error when parsing cert")
	}
	if len(certs) != 2 {
		t.Errorf("Expect 2 cert is parsed, actual %d", len(certs))
	}
	for _, cert := range certs {
		if cert.Subject.CommonName != "webhook.ns1.svc" && cert.Subject.CommonName != "cluster-manager-webhook" {
			t.Errorf("Common name in cert is not correct, actual %s", cert.Subject.CommonName)
		}
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubeconfig",
			Namespace: "ns1",
		},
		Data: map[string][]byte{
			"ca.crt":  ca,
			"tls.crt": certificate,
			"tls.key": key,
		},
	}
	controller = newTestController(clusterManager).withCRDObject().withKubeObject(secret).withAPIServiceObject()
	actualCA, actualCert, actualKey, err := controller.controller.ensureServingCertAndCA(context.TODO(), "ns1", "kubeconfig", "webhook")
	if err != nil {
		t.Errorf("Expect no error when generating serving cert: %v", err)
	}

	if !reflect.DeepEqual(ca, actualCA) || !reflect.DeepEqual(certificate, actualCert) || !reflect.DeepEqual(key, actualKey) {
		t.Errorf("Expect the cert/key/ca is obtained from secret")
	}
}
