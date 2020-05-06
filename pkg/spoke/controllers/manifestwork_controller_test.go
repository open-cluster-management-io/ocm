package controllers

import (
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/restmapper"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	fakeworkclient "github.com/open-cluster-management/api/client/work/clientset/versioned/fake"
	workinformers "github.com/open-cluster-management/api/client/work/informers/externalversions"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/work/pkg/helper"
	"github.com/open-cluster-management/work/pkg/spoke/resource"
)

type testController struct {
	controller    *ManifestWorkController
	dynamicClient *fakedynamic.FakeDynamicClient
	workClient    *fakeworkclient.Clientset
	kubeClient    *fakekube.Clientset
}

type fakeSyncContext struct {
	workKey  string
	queue    workqueue.RateLimitingInterface
	recorder events.Recorder
}

func newFakeSyncContext(t *testing.T, workKey string) *fakeSyncContext {
	return &fakeSyncContext{
		workKey:  workKey,
		queue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		recorder: eventstesting.NewTestingEventRecorder(t),
	}
}

func (f fakeSyncContext) Queue() workqueue.RateLimitingInterface { return f.queue }
func (f fakeSyncContext) QueueKey() string                       { return f.workKey }
func (f fakeSyncContext) Recorder() events.Recorder              { return f.recorder }

func newSecret(name, namespace string, content string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"test": []byte(content),
		},
	}
}

func newUnstructured(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}
}

func newUnstructuredWithContent(
	apiVersion, kind, namespace, name string, content map[string]interface{}) *unstructured.Unstructured {
	object := newUnstructured(apiVersion, kind, namespace, name)
	for key, val := range content {
		object.Object[key] = val
	}

	return object
}

func newManifestWork(index int, objects ...*unstructured.Unstructured) (*workapiv1.ManifestWork, string) {
	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("work-%d", index),
			Namespace: "cluster1",
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: []runtime.RawExtension{},
			},
		},
	}

	for _, object := range objects {
		objectStr, _ := object.MarshalJSON()
		work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, runtime.RawExtension{Raw: objectStr})
	}

	return work, fmt.Sprintf("%s", work.Name)
}

func newFakeMapper() *resource.Mapper {
	resources := []*restmapper.APIGroupResources{
		{
			Group: metav1.APIGroup{
				Name: "",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "secrets", Namespaced: true, Kind: "Secret"},
					{Name: "pods", Namespaced: true, Kind: "Pod"},
					{Name: "newobjects", Namespaced: true, Kind: "NewObject"},
				},
			},
		},
	}
	return &resource.Mapper{
		Mapper: restmapper.NewDiscoveryRESTMapper(resources),
	}
}

func newController(work *workapiv1.ManifestWork, mapper *resource.Mapper) *testController {
	fakeWorkClient := fakeworkclient.NewSimpleClientset(work)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fakeWorkClient, 5*time.Minute, workinformers.WithNamespace("cluster1"))

	controller := &ManifestWorkController{
		manifestWorkClient: fakeWorkClient.WorkV1().ManifestWorks("cluster1"),
		manifestWorkLister: workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("cluster1"),
		restMapper:         mapper,
	}

	store := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore()
	store.Add(work)

	return &testController{
		controller: controller,
		workClient: fakeWorkClient,
	}
}

func (t *testController) withKubeObject(objects ...runtime.Object) *testController {
	kubeClient := fakekube.NewSimpleClientset(objects...)
	t.controller.spokeKubeclient = kubeClient
	t.kubeClient = kubeClient
	return t
}

func (t *testController) withUnstructuredObject(objects ...runtime.Object) *testController {
	scheme := runtime.NewScheme()
	dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)
	t.controller.spokeDynamicClient = dynamicClient
	t.dynamicClient = dynamicClient
	return t
}

func assertAction(t *testing.T, actual clienttesting.Action, expected string) {
	if actual.GetVerb() != expected {
		t.Errorf("expected %s action but got: %#v", expected, actual)
	}
}

func assertCondition(t *testing.T, conditions []workapiv1.StatusCondition, expectedCondition string, expectedStatus metav1.ConditionStatus) {
	conditionTypeFound := false
	for _, condition := range conditions {
		if condition.Type != expectedCondition {
			continue
		}
		conditionTypeFound = true
		if condition.Status != expectedStatus {
			t.Errorf("expected %s but got: %s", expectedStatus, condition.Status)
			break
		}
	}

	if !conditionTypeFound {
		t.Errorf("expected condition %s but got: %#v", expectedCondition, conditions)
	}
}

func assertManifestCondition(
	t *testing.T, conds []workapiv1.ManifestCondition, index int32, expectedCondition string, expectedStatus metav1.ConditionStatus) {
	cond := helper.FindManifestConditionByIndex(index, conds)
	if cond == nil {
		t.Errorf("expected to find the condition with index %d", index)
	}

	assertCondition(t, cond.Conditions, expectedCondition, expectedStatus)
}

type testCase struct {
	name                  string
	workManifest          []*unstructured.Unstructured
	spokeObject           []runtime.Object
	spokeDynamicObject    []runtime.Object
	expectedWorkAction    []string
	expectedKubeAction    []string
	expectedDynamicAction []string
	expectedConditions    []expectedCondition
}

type expectedCondition struct {
	conditionType string
	status        metav1.ConditionStatus
}

func newTestCase(name string) *testCase {
	return &testCase{
		name:                  name,
		workManifest:          []*unstructured.Unstructured{},
		spokeObject:           []runtime.Object{},
		spokeDynamicObject:    []runtime.Object{},
		expectedWorkAction:    []string{},
		expectedKubeAction:    []string{},
		expectedDynamicAction: []string{},
		expectedConditions:    []expectedCondition{},
	}
}

func (t *testCase) withWorkManifest(objects ...*unstructured.Unstructured) *testCase {
	t.workManifest = objects
	return t
}

func (t *testCase) withSpokeObject(objects ...runtime.Object) *testCase {
	t.spokeObject = objects
	return t
}

func (t *testCase) withSpokeDynamicObject(objects ...runtime.Object) *testCase {
	t.spokeDynamicObject = objects
	return t
}

func (t *testCase) withExpectedWorkAction(actions ...string) *testCase {
	t.expectedWorkAction = actions
	return t
}

func (t *testCase) withExpectedKubeAction(actions ...string) *testCase {
	t.expectedKubeAction = actions
	return t
}

func (t *testCase) withExpectedDynamicAction(actions ...string) *testCase {
	t.expectedDynamicAction = actions
	return t
}

func (t *testCase) withExpectedCondition(conds ...expectedCondition) *testCase {
	t.expectedConditions = conds
	return t
}

func (t *testCase) validate(
	ts *testing.T,
	dynamicClient *fakedynamic.FakeDynamicClient,
	workClient *fakeworkclient.Clientset,
	kubeClient *fakekube.Clientset) {
	workActions := workClient.Actions()
	if len(workActions) != len(t.expectedWorkAction) {
		ts.Errorf("Expected %d action but got %#v", len(t.expectedWorkAction), workActions)
	}
	for index := range workActions {
		assertAction(ts, workActions[index], t.expectedWorkAction[index])
	}

	spokeDynamicActions := dynamicClient.Actions()
	if len(spokeDynamicActions) != len(t.expectedDynamicAction) {
		ts.Errorf("Expected %d action but got %#v", len(t.expectedDynamicAction), spokeDynamicActions)
	}
	for index := range spokeDynamicActions {
		assertAction(ts, spokeDynamicActions[index], t.expectedDynamicAction[index])
	}
	spokeKubeActions := kubeClient.Actions()
	if len(spokeKubeActions) != len(t.expectedKubeAction) {
		ts.Errorf("Expected %d action but got %#v", len(t.expectedKubeAction), spokeKubeActions)
	}
	for index := range spokeKubeActions {
		assertAction(ts, spokeKubeActions[index], t.expectedKubeAction[index])
	}

	actual, ok := workActions[len(workActions)-1].(clienttesting.UpdateActionImpl)
	if !ok {
		ts.Errorf("Expected to get update action")
	}
	actualWork := actual.Object.(*workapiv1.ManifestWork)
	for index, cond := range t.expectedConditions {
		assertManifestCondition(ts, actualWork.Status.ResourceStatus.Manifests, int32(index), cond.conditionType, cond.status)
	}
}

// TestSync test cases when running sync
func TestSync(t *testing.T) {
	cases := []*testCase{
		newTestCase("create single resource").
			withWorkManifest(newUnstructured("v1", "Secret", "ns1", "test")).
			withExpectedWorkAction("get", "update").
			withExpectedKubeAction("get", "create").
			withExpectedCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}),
		newTestCase("update single resource").
			withWorkManifest(newUnstructured("v1", "Secret", "ns1", "test")).
			withSpokeObject(newSecret("test", "ns1", "value2")).
			withExpectedWorkAction("get", "update").
			withExpectedKubeAction("get", "delete", "create").
			withExpectedCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}),
		newTestCase("create single unstructured resource").
			withWorkManifest(newUnstructured("v1", "NewObject", "ns1", "test")).
			withExpectedWorkAction("get", "update").
			withExpectedDynamicAction("get", "create").
			withExpectedCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}),
		newTestCase("update single unstructured resource").
			withWorkManifest(newUnstructuredWithContent("v1", "NewObject", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withSpokeDynamicObject(newUnstructuredWithContent("v1", "NewObject", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}})).
			withExpectedWorkAction("get", "update").
			withExpectedDynamicAction("get", "update").
			withExpectedCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}),
		newTestCase("multiple create&update resource").
			withWorkManifest(newUnstructured("v1", "Secret", "ns1", "test"), newUnstructured("v1", "Secret", "ns2", "test")).
			withSpokeObject(newSecret("test", "ns1", "value2")).
			withExpectedWorkAction("get", "update").
			withExpectedKubeAction("get", "delete", "create", "get", "create").
			withExpectedCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}, expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}),
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work, workKey := newManifestWork(0, c.workManifest...)
			work.Finalizers = []string{manifestWorkFinalizer}
			controller := newController(work, newFakeMapper()).
				withKubeObject(c.spokeObject...).
				withUnstructuredObject(c.spokeDynamicObject...)
			syncContext := newFakeSyncContext(t, workKey)
			err := controller.controller.sync(nil, syncContext)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			c.validate(t, controller.dynamicClient, controller.workClient, controller.kubeClient)
		})
	}
}

// TestDeleteWork tests the action when work is deleted
func TestDeleteWork(t *testing.T) {
	tc := newTestCase("delete multiple resources").
		withWorkManifest(newUnstructured("v1", "Secret", "ns1", "test"), newUnstructured("v1", "Secret", "ns2", "test")).
		withSpokeObject(newSecret("test", "ns1", "value2")).
		withExpectedWorkAction("update").
		withExpectedDynamicAction("delete", "delete")

	work, workKey := newManifestWork(0, tc.workManifest...)
	work.Finalizers = []string{manifestWorkFinalizer}
	now := metav1.Now()
	work.ObjectMeta.SetDeletionTimestamp(&now)
	controller := newController(work, newFakeMapper()).withKubeObject(tc.spokeObject...).withUnstructuredObject()
	syncContext := newFakeSyncContext(t, workKey)
	err := controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Should be success with no err: %v", err)
	}

	tc.validate(t, controller.dynamicClient, controller.workClient, controller.kubeClient)

	// Verify that finalizer is removed
	workActions := controller.workClient.Actions()
	actual, ok := workActions[len(workActions)-1].(clienttesting.UpdateActionImpl)
	if !ok {
		t.Errorf("Expected to get update action")
	}
	actualWork := actual.Object.(*workapiv1.ManifestWork)
	if len(actualWork.Finalizers) != 0 {
		t.Errorf("Expected 0 finailizer but got %#v", actualWork.Finalizers)
	}
}

// Test applying resource failed
func TestFailedToApplyResource(t *testing.T) {
	tc := newTestCase("multiple create&update resource").
		withWorkManifest(newUnstructured("v1", "Secret", "ns1", "test"), newUnstructured("v1", "Secret", "ns2", "test")).
		withSpokeObject(newSecret("test", "ns1", "value2")).
		withExpectedWorkAction("get", "update").
		withExpectedKubeAction("get", "delete", "create", "get", "create").
		withExpectedCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}, expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionFalse})

	work, workKey := newManifestWork(0, tc.workManifest...)
	work.Finalizers = []string{manifestWorkFinalizer}
	controller := newController(work, newFakeMapper()).withKubeObject(tc.spokeObject...).withUnstructuredObject()

	// Add a reactor on fake client to throw error when creating secret on namespace ns2
	controller.kubeClient.PrependReactor("create", "secrets", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		fmt.Printf("the action get into %v\n", action)
		if action.GetVerb() != "create" {
			return false, nil, nil
		}

		createAction := action.(clienttesting.CreateActionImpl)
		createObject := createAction.Object.(*corev1.Secret)
		if createObject.Namespace == "ns1" {
			return false, createObject, nil
		}

		return true, &corev1.Secret{}, fmt.Errorf("Fake error")
	})
	syncContext := newFakeSyncContext(t, workKey)
	err := controller.controller.sync(nil, syncContext)
	if err == nil {
		t.Errorf("Should return an err")
	}

	tc.validate(t, controller.dynamicClient, controller.workClient, controller.kubeClient)
}

// Test unstructured compare
func TestIsSameUnstructured(t *testing.T) {
	cases := []struct {
		name     string
		obj1     *unstructured.Unstructured
		obj2     *unstructured.Unstructured
		expected bool
	}{
		{
			name:     "different kind",
			obj1:     newUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     newUnstructured("v1", "Kind2", "ns1", "n1"),
			expected: false,
		},
		{
			name:     "different namespace",
			obj1:     newUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     newUnstructured("v1", "Kind1", "ns2", "n1"),
			expected: false,
		},
		{
			name:     "different name",
			obj1:     newUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     newUnstructured("v1", "Kind1", "ns1", "n2"),
			expected: false,
		},
		{
			name:     "different spec",
			obj1:     newUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}}),
			obj2:     newUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}}),
			expected: false,
		},
		{
			name:     "same spec, different status",
			obj1:     newUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}, "status": "status1"}),
			obj2:     newUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}, "status": "status2"}),
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := isSameUnstructured(c.obj1, c.obj2)
			if c.expected != actual {
				t.Errorf("expected %t, but %t", c.expected, actual)
			}
		})
	}
}
