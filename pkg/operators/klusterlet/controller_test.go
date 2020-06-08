package klusterlet

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	fakeoperatorclient "github.com/open-cluster-management/api/client/operator/clientset/versioned/fake"
	operatorinformers "github.com/open-cluster-management/api/client/operator/informers/externalversions"
	opratorapiv1 "github.com/open-cluster-management/api/operator/v1"
	"github.com/open-cluster-management/registration-operator/pkg/helpers"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
)

type testController struct {
	controller     *klusterletController
	kubeClient     *fakekube.Clientset
	operatorClient *fakeoperatorclient.Clientset
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

func newSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{},
	}
}

func newKlusterlet(name, namespace, clustername string) *opratorapiv1.Klusterlet {
	return &opratorapiv1.Klusterlet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Finalizers: []string{klusterletFinalizer},
		},
		Spec: opratorapiv1.KlusterletSpec{
			RegistrationImagePullSpec: "testregistration",
			WorkImagePullSpec:         "testwork",
			ClusterName:               clustername,
			Namespace:                 namespace,
			ExternalServerURLs:        []opratorapiv1.ServerURL{},
		},
	}
}

func newNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newTestController(klusterlet *opratorapiv1.Klusterlet, objects ...runtime.Object) *testController {
	fakeKubeClient := fakekube.NewSimpleClientset(objects...)
	fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(klusterlet)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)

	hubController := &klusterletController{
		klusterletClient: fakeOperatorClient.OperatorV1().Klusterlets(),
		kubeClient:       fakeKubeClient,
		klusterletLister: operatorInformers.Operator().V1().Klusterlets().Lister(),
	}

	store := operatorInformers.Operator().V1().Klusterlets().Informer().GetStore()
	store.Add(klusterlet)

	return &testController{
		controller:     hubController,
		kubeClient:     fakeKubeClient,
		operatorClient: fakeOperatorClient,
	}
}

func assertAction(t *testing.T, actual clienttesting.Action, expected string) {
	if actual.GetVerb() != expected {
		t.Errorf("expected %s action but got: %#v", expected, actual)
	}
}

func assertGet(t *testing.T, actual clienttesting.Action, group, version, resource string) {
	t.Helper()
	if actual.GetVerb() != "get" {
		t.Error(spew.Sdump(actual))
	}
	if actual.GetResource() != (schema.GroupVersionResource{Group: group, Version: version, Resource: resource}) {
		t.Error(spew.Sdump(actual))
	}
}

func namedCondition(name string, status metav1.ConditionStatus) opratorapiv1.StatusCondition {
	return opratorapiv1.StatusCondition{Type: name, Status: status}
}

func assertOnlyConditions(t *testing.T, actual runtime.Object, expectedConditions ...opratorapiv1.StatusCondition) {
	t.Helper()

	klusterlet := actual.(*opratorapiv1.Klusterlet)
	actualConditions := klusterlet.Status.Conditions
	if len(actualConditions) != len(expectedConditions) {
		t.Errorf("expected %v condition but got: %v", len(expectedConditions), spew.Sdump(actualConditions))
	}

	for _, expectedCondition := range expectedConditions {
		actual := helpers.FindOperatorCondition(actualConditions, expectedCondition.Type)
		if actual == nil {
			t.Errorf("missing %v in %v", spew.Sdump(expectedCondition), spew.Sdump(actual))
		}
		if actual.Status != expectedCondition.Status {
			t.Errorf("wrong result for %v in %v", spew.Sdump(expectedCondition), spew.Sdump(actual))
		}
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

func ensureObject(t *testing.T, object runtime.Object, klusterlet *opratorapiv1.Klusterlet) {
	access, err := meta.Accessor(object)
	if err != nil {
		t.Errorf("Unable to access objectmeta: %v", err)
	}

	switch o := object.(type) {
	case *appsv1.Deployment:
		if strings.Contains(access.GetName(), "registration") {
			ensureNameNamespace(
				t, access.GetName(), access.GetNamespace(),
				fmt.Sprintf("%s-registration-agent", klusterlet.Name), klusterlet.Spec.Namespace)
			if klusterlet.Spec.RegistrationImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
				t.Errorf("Image does not match to the expected.")
			}
		} else if strings.Contains(access.GetName(), "work") {
			ensureNameNamespace(
				t, access.GetName(), access.GetNamespace(),
				fmt.Sprintf("%s-work-agent", klusterlet.Name), klusterlet.Spec.Namespace)
			if klusterlet.Spec.WorkImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
				t.Errorf("Image does not match to the expected.")
			}
		} else {
			t.Errorf("Unexpected deployment")
		}
	}
}

// TestSyncDeploy test deployment of klusterlet components
func TestSyncDeploy(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	bootStrapSecret := newSecret(bootstrapHubKubeConfigSecret, "testns")
	hubKubeConfigSecret := newSecret(hubKubeConfigSecret, "testns")
	hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	namespace := newNamespace("testns")
	controller := newTestController(klusterlet, bootStrapSecret, hubKubeConfigSecret, namespace)
	syncContext := newFakeSyncContext(t, "klusterlet")

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
	if len(createObjects) != 11 {
		t.Errorf("Expect 11 objects created in the sync loop, actual %d", len(createObjects))
	}
	for _, object := range createObjects {
		ensureObject(t, object, klusterlet)
	}

	operatorAction := controller.operatorClient.Actions()
	if len(operatorAction) != 4 {
		t.Errorf("Expect 4 actions in the sync loop, actual %#v", operatorAction)
	}

	assertGet(t, operatorAction[0], "operator.open-cluster-management.io", "v1", "klusterlets")
	assertAction(t, operatorAction[1], "update")
	assertOnlyConditions(t, operatorAction[1].(clienttesting.UpdateActionImpl).Object,
		namedCondition(klusterletApplied, metav1.ConditionTrue))
	assertGet(t, operatorAction[2], "operator.open-cluster-management.io", "v1", "klusterlets")
	assertAction(t, operatorAction[3], "update")
	assertOnlyConditions(t, operatorAction[3].(clienttesting.UpdateActionImpl).Object,
		namedCondition(klusterletApplied, metav1.ConditionTrue), namedCondition(klusterletRegistrationDegraded, metav1.ConditionFalse))
}

// TestSyncWithNoSecret test the scenario that bootstrap secret and hub config secret does not exist
func TestSyncWithNoSecret(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "")
	bootStrapSecret := newSecret(bootstrapHubKubeConfigSecret, "testns")
	hubSecret := newSecret(hubKubeConfigSecret, "testns")
	namespace := newNamespace("testns")
	controller := newTestController(klusterlet, namespace)
	syncContext := newFakeSyncContext(t, "klusterlet")

	// Return err since bootstrap secret does not exist
	err := controller.controller.sync(nil, syncContext)
	if err == nil {
		t.Errorf("Expected error when sync")
	}
	operatorAction := controller.operatorClient.Actions()
	if len(operatorAction) != 2 {
		t.Errorf("Expect 2 actions in the sync loop, actual %#v", operatorAction)
	}

	assertGet(t, operatorAction[0], "operator.open-cluster-management.io", "v1", "klusterlets")
	assertAction(t, operatorAction[1], "update")
	assertOnlyConditions(t, operatorAction[1].(clienttesting.UpdateActionImpl).Object, namedCondition(klusterletApplied, metav1.ConditionFalse))

	// reset for round 2
	controller.operatorClient.ClearActions()
	// Add bootstrap secret and sync again
	controller.kubeClient.PrependReactor("get", "secrets", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetVerb() != "get" {
			return false, nil, nil
		}

		getAction := action.(clienttesting.GetActionImpl)
		if getAction.Name != bootstrapHubKubeConfigSecret {
			return false, nil, errors.NewNotFound(
				corev1.Resource("secrets"), bootstrapHubKubeConfigSecret)
		}
		return true, bootStrapSecret, nil
	})
	// Return err since cluster-name cannot be found in hubkubeconfig secret
	err = controller.controller.sync(nil, syncContext)
	if err == nil {
		t.Errorf("Expected error when sync")
	}
	operatorAction = controller.operatorClient.Actions()
	if len(operatorAction) != 4 {
		t.Errorf("Expect 4 actions in the sync loop, actual %#v", operatorAction)
	}

	assertGet(t, operatorAction[0], "operator.open-cluster-management.io", "v1", "klusterlets")
	assertAction(t, operatorAction[1], "update")
	assertOnlyConditions(t, operatorAction[1].(clienttesting.UpdateActionImpl).Object,
		namedCondition(klusterletApplied, metav1.ConditionTrue))
	assertGet(t, operatorAction[2], "operator.open-cluster-management.io", "v1", "klusterlets")
	assertAction(t, operatorAction[3], "update")
	assertOnlyConditions(t, operatorAction[3].(clienttesting.UpdateActionImpl).Object,
		namedCondition(klusterletApplied, metav1.ConditionTrue), namedCondition(klusterletRegistrationDegraded, metav1.ConditionTrue))

	// reset for round 3
	controller.operatorClient.ClearActions()
	// Add hub config secret and sync again
	hubSecret.Data["kubeconfig"] = []byte("dummykubeconfig")
	hubSecret.Data["cluster-name"] = []byte("cluster1")
	controller.kubeClient.PrependReactor("get", "secrets", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetVerb() != "get" {
			return false, nil, nil
		}

		getAction := action.(clienttesting.GetActionImpl)
		if getAction.Name != hubKubeConfigSecret {
			return false, nil, errors.NewNotFound(
				corev1.Resource("secrets"), hubKubeConfigSecret)
		}
		return true, hubSecret, nil
	})
	err = controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected no error when sync: %v", err)
	}
	operatorAction = controller.operatorClient.Actions()
	if len(operatorAction) != 3 {
		t.Errorf("Expect 3 actions in the sync loop, actual %#v", operatorAction)
	}

	assertGet(t, operatorAction[0], "operator.open-cluster-management.io", "v1", "klusterlets")
	assertGet(t, operatorAction[1], "operator.open-cluster-management.io", "v1", "klusterlets")
	assertAction(t, operatorAction[2], "update")
	assertOnlyConditions(t, operatorAction[2].(clienttesting.UpdateActionImpl).Object,
		namedCondition(klusterletApplied, metav1.ConditionTrue), namedCondition(klusterletRegistrationDegraded, metav1.ConditionFalse))
}

// TestSyncDelete test cleanup hub deploy
func TestSyncDelete(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "")
	now := metav1.Now()
	klusterlet.ObjectMeta.SetDeletionTimestamp(&now)
	namespace := newNamespace("testns")
	controller := newTestController(klusterlet, namespace)
	syncContext := newFakeSyncContext(t, "klusterlet")

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

	if len(kubeActions) != 12 {
		t.Errorf("Expected 7 delete actions, but got %d", len(kubeActions))
	}
}

// TestGetServersFromKlusterlet tests getServersFromKlusterlet func
func TestGetServersFromKlusterlet(t *testing.T) {
	cases := []struct {
		name     string
		servers  []string
		expected string
	}{
		{
			name:     "Null",
			servers:  nil,
			expected: "",
		},
		{
			name:     "Empty string",
			servers:  []string{},
			expected: "",
		},
		{
			name:     "Single server",
			servers:  []string{"https://server1"},
			expected: "https://server1",
		},
		{
			name:     "Multiple servers",
			servers:  []string{"https://server1", "https://server2"},
			expected: "https://server1,https://server2",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			klusterlet := newKlusterlet("klusterlet", "testns", "")
			for _, server := range c.servers {
				klusterlet.Spec.ExternalServerURLs = append(klusterlet.Spec.ExternalServerURLs,
					opratorapiv1.ServerURL{URL: server})
			}
			actual := getServersFromKlusterlet(klusterlet)
			if actual != c.expected {
				t.Errorf("Expected to be same, actual %q, expected %q", actual, c.expected)
			}
		})
	}
}
