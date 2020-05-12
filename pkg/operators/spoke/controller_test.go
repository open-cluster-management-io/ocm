package spoke

import (
	"fmt"
	"strings"
	"testing"
	"time"

	fakenucleusclient "github.com/open-cluster-management/api/client/nucleus/clientset/versioned/fake"
	nucleusinformers "github.com/open-cluster-management/api/client/nucleus/informers/externalversions"
	nucleusapiv1 "github.com/open-cluster-management/api/nucleus/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
)

type testController struct {
	controller    *nucleusSpokeController
	kubeClient    *fakekube.Clientset
	nucleusClient *fakenucleusclient.Clientset
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

func newSpokeCore(name, namespace, clustername string) *nucleusapiv1.SpokeCore {
	return &nucleusapiv1.SpokeCore{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Finalizers: []string{nucleusSpokeFinalizer},
		},
		Spec: nucleusapiv1.SpokeCoreSpec{
			RegistrationImagePullSpec: "testregistration",
			WorkImagePullSpec:         "testwork",
			ClusterName:               clustername,
			Namespace:                 namespace,
		},
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

func newNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newTestController(spokecore *nucleusapiv1.SpokeCore, objects ...runtime.Object) *testController {
	fakeKubeClient := fakekube.NewSimpleClientset(objects...)
	fakeNucleusClient := fakenucleusclient.NewSimpleClientset(spokecore)
	nucleusInformers := nucleusinformers.NewSharedInformerFactory(fakeNucleusClient, 5*time.Minute)

	hubController := &nucleusSpokeController{
		nucleusClient: fakeNucleusClient.NucleusV1().SpokeCores(),
		kubeClient:    fakeKubeClient,
		nucleusLister: nucleusInformers.Nucleus().V1().SpokeCores().Lister(),
	}

	store := nucleusInformers.Nucleus().V1().SpokeCores().Informer().GetStore()
	store.Add(spokecore)

	return &testController{
		controller:    hubController,
		kubeClient:    fakeKubeClient,
		nucleusClient: fakeNucleusClient,
	}
}

func assertAction(t *testing.T, actual clienttesting.Action, expected string) {
	if actual.GetVerb() != expected {
		t.Errorf("expected %s action but got: %#v", expected, actual)
	}
}

func assertCondition(t *testing.T, actual runtime.Object, expectedCondition string, expectedStatus metav1.ConditionStatus) {
	spokeCore := actual.(*nucleusapiv1.SpokeCore)
	conditions := spokeCore.Status.Conditions
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

func ensureObject(t *testing.T, object runtime.Object, spokeCore *nucleusapiv1.SpokeCore) {
	access, err := meta.Accessor(object)
	if err != nil {
		t.Errorf("Unable to access objectmeta: %v", err)
	}

	switch o := object.(type) {
	case *corev1.ServiceAccount:
		ensureNameNamespace(t, access.GetName(), access.GetNamespace(), fmt.Sprintf("%s-sa", spokeCore.Name), spokeCore.Spec.Namespace)
	case *appsv1.Deployment:
		if strings.Contains(access.GetName(), "registration") {
			ensureNameNamespace(
				t, access.GetName(), access.GetNamespace(),
				fmt.Sprintf("%s-registration-agent", spokeCore.Name), spokeCore.Spec.Namespace)
			if spokeCore.Spec.RegistrationImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
				t.Errorf("Image does not match to the expected.")
			}
		} else if strings.Contains(access.GetName(), "work") {
			ensureNameNamespace(
				t, access.GetName(), access.GetNamespace(),
				fmt.Sprintf("%s-work-agent", spokeCore.Name), spokeCore.Spec.Namespace)
			if spokeCore.Spec.WorkImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
				t.Errorf("Image does not match to the expected.")
			}
		} else {
			t.Errorf("Unexpected deployment")
		}
	}
}

// TestSyncDeploy test deployment of spoke components
func TestSyncDeploy(t *testing.T) {
	spokeCore := newSpokeCore("testspoke", "testns", "cluster1")
	bootStrapSecret := newSecret(bootstrapHubKubeConfigSecret, "testns")
	hubKubeConfigSecret := newSecret(hubKubeConfigSecret, "testns")
	hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	namespace := newNamespace("testns")
	controller := newTestController(spokeCore, bootStrapSecret, hubKubeConfigSecret, namespace)
	syncContext := newFakeSyncContext(t, "testspoke")

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
	if len(createObjects) != 4 {
		t.Errorf("Expect 4 objects created in the sync loop, actual %d", len(createObjects))
	}
	for _, object := range createObjects {
		ensureObject(t, object, spokeCore)
	}

	nucleusAction := controller.nucleusClient.Actions()
	if len(nucleusAction) != 2 {
		t.Errorf("Expect 2 actions in the sync loop, actual %#v", nucleusAction)
	}

	assertAction(t, nucleusAction[1], "update")
	assertCondition(t, nucleusAction[1].(clienttesting.UpdateActionImpl).Object, spokeCoreApplied, metav1.ConditionTrue)
}

// TestSyncWithNoSecret test the scenario that bootstrap secret and hub config secret does not exist
func TestSyncWithNoSecret(t *testing.T) {
	spokeCore := newSpokeCore("testspoke", "testns", "")
	bootStrapSecret := newSecret(bootstrapHubKubeConfigSecret, "testns")
	hubSecret := newSecret(hubKubeConfigSecret, "testns")
	namespace := newNamespace("testns")
	controller := newTestController(spokeCore, namespace)
	syncContext := newFakeSyncContext(t, "testspoke")

	// Return err since bootstrap secret does not exist
	err := controller.controller.sync(nil, syncContext)
	if err == nil {
		t.Errorf("Expected error when sync")
	}
	nucleusAction := controller.nucleusClient.Actions()
	if len(nucleusAction) != 2 {
		t.Errorf("Expect 2 actions in the sync loop, actual %#v", nucleusAction)
	}

	assertAction(t, nucleusAction[1], "update")
	assertCondition(t, nucleusAction[1].(clienttesting.UpdateActionImpl).Object, spokeCoreApplied, metav1.ConditionFalse)

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
	nucleusAction = controller.nucleusClient.Actions()
	if len(nucleusAction) != 4 {
		t.Errorf("Expect 4 actions in the sync loop, actual %#v", nucleusAction)
	}

	assertAction(t, nucleusAction[3], "update")
	assertCondition(t, nucleusAction[3].(clienttesting.UpdateActionImpl).Object, spokeCoreApplied, metav1.ConditionFalse)

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
	nucleusAction = controller.nucleusClient.Actions()
	if len(nucleusAction) != 6 {
		t.Errorf("Expect 6 actions in the sync loop, actual %#v", nucleusAction)
	}

	assertAction(t, nucleusAction[5], "update")
	assertCondition(t, nucleusAction[5].(clienttesting.UpdateActionImpl).Object, spokeCoreApplied, metav1.ConditionTrue)
}
