package hub

import (
	"fmt"
	"testing"
	"time"

	fakenucleusclient "github.com/open-cluster-management/api/client/nucleus/clientset/versioned/fake"
	nucleusinformers "github.com/open-cluster-management/api/client/nucleus/informers/externalversions"
	nucleusapiv1 "github.com/open-cluster-management/api/nucleus/v1"
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
	"k8s.io/client-go/util/workqueue"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
)

type testController struct {
	controller         *nucleusHubController
	kubeClient         *fakekube.Clientset
	apiExtensionClient *fakeapiextensions.Clientset
	nucleusClient      *fakenucleusclient.Clientset
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

func newHubCore(name string) *nucleusapiv1.HubCore {
	return &nucleusapiv1.HubCore{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Finalizers: []string{"nucleus.open-cluster-management.io/hub-core-cleanup"},
		},
		Spec: nucleusapiv1.HubCoreSpec{
			RegistrationImagePullSpec: "testregistration",
		},
	}
}

func newTestController(hubcore *nucleusapiv1.HubCore) *testController {
	fakeNucleusClient := fakenucleusclient.NewSimpleClientset(hubcore)
	nucleusInformers := nucleusinformers.NewSharedInformerFactory(fakeNucleusClient, 5*time.Minute)

	hubController := &nucleusHubController{
		nucleusClient: fakeNucleusClient.NucleusV1().HubCores(),
		nucleusLister: nucleusInformers.Nucleus().V1().HubCores().Lister(),
	}

	store := nucleusInformers.Nucleus().V1().HubCores().Informer().GetStore()
	store.Add(hubcore)

	return &testController{
		controller:    hubController,
		nucleusClient: fakeNucleusClient,
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

func assertAction(t *testing.T, actual clienttesting.Action, expected string) {
	if actual.GetVerb() != expected {
		t.Errorf("expected %s action but got: %#v", expected, actual)
	}
}

func assertCondition(t *testing.T, actual runtime.Object, expectedCondition string, expectedStatus metav1.ConditionStatus) {
	hubCore := actual.(*nucleusapiv1.HubCore)
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

func ensureObject(t *testing.T, object runtime.Object, hubCore *nucleusapiv1.HubCore) {
	access, err := meta.Accessor(object)
	if err != nil {
		t.Errorf("Unable to access objectmeta: %v", err)
	}

	switch o := object.(type) {
	case *corev1.Namespace:
		ensureNameNamespace(t, access.GetName(), "", nucluesHubCoreNamespace, "")
	case *corev1.ServiceAccount:
		ensureNameNamespace(t, access.GetName(), access.GetNamespace(), fmt.Sprintf("%s-sa", hubCore.Name), nucluesHubCoreNamespace)
	case *appsv1.Deployment:
		ensureNameNamespace(t, access.GetName(), access.GetNamespace(), fmt.Sprintf("%s-controller", hubCore.Name), nucluesHubCoreNamespace)
		if hubCore.Spec.RegistrationImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
			t.Errorf("Image does not match to the expected.")
		}
	}
}

// TestSyncDeploy tests sync manifests of hub component
func TestSyncDeploy(t *testing.T) {
	hubCore := newHubCore("testhub")
	controller := newTestController(hubCore).withCRDObject().withKubeObject()
	syncContext := newFakeSyncContext(t, "testhub")

	err := controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	createObjects := []runtime.Object{}
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			createObjects = append(createObjects, object)
		}
	}

	// Check if resources are created as expected
	if len(createObjects) != 5 {
		t.Errorf("Expect 5 objects created in the sync loop, actual %q", len(createObjects))
	}
	for _, object := range createObjects {
		ensureObject(t, object, hubCore)
	}

	nucleusAction := controller.nucleusClient.Actions()
	if len(nucleusAction) != 2 {
		t.Errorf("Expect 2 actions in the sync loop")
	}

	assertAction(t, nucleusAction[1], "update")
	assertCondition(t, nucleusAction[1].(clienttesting.UpdateActionImpl).Object, hubCoreApplied, metav1.ConditionTrue)
}

// TestSyncDelete test cleanup hub deploy
func TestSyncDelete(t *testing.T) {
	hubCore := newHubCore("testhub")
	now := metav1.Now()
	hubCore.ObjectMeta.SetDeletionTimestamp(&now)
	controller := newTestController(hubCore).withCRDObject().withKubeObject()
	syncContext := newFakeSyncContext(t, "testhub")

	err := controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	deleteActions := []clienttesting.DeleteActionImpl{}
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "delete" {
			deleteAction := action.(clienttesting.DeleteActionImpl)
			deleteActions = append(deleteActions, deleteAction)
		}
	}

	for _, action := range deleteActions {
		switch action.Resource.Resource {
		case "clusterroles":
			ensureNameNamespace(t, action.Name, "", fmt.Sprintf("system:open-cluster-management:%s", hubCore.Name), "")
		case "clusterrolebindings":
			ensureNameNamespace(t, action.Name, "", fmt.Sprintf("system:open-cluster-management:%s", hubCore.Name), "")
		case "namespaces":
			ensureNameNamespace(t, action.Name, "", nucluesHubCoreNamespace, "")
		case "serviceaccounts":
			ensureNameNamespace(t, action.Name, action.Namespace, fmt.Sprintf("%s-sa", hubCore.Name), nucluesHubCoreNamespace)
		case "deployments":
			ensureNameNamespace(t, action.Name, action.Namespace, fmt.Sprintf("%s-controller", hubCore.Name), nucluesHubCoreNamespace)
		}
	}
}

// TestDeleteCRD test delete crds
func TestDeleteCRD(t *testing.T) {
	hubCore := newHubCore("testhub")
	now := metav1.Now()
	hubCore.ObjectMeta.SetDeletionTimestamp(&now)
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: crdNames[0],
		},
	}
	controller := newTestController(hubCore).withCRDObject(crd).withKubeObject()

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
