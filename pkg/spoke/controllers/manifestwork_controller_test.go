package controllers

import (
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/diff"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/restmapper"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	fakeworkclient "github.com/open-cluster-management/api/client/work/clientset/versioned/fake"
	workinformers "github.com/open-cluster-management/api/client/work/informers/externalversions"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
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
				Manifests: []workapiv1.Manifest{},
			},
		},
	}

	for _, object := range objects {
		objectStr, _ := object.MarshalJSON()
		manifest := workapiv1.Manifest{}
		manifest.Raw = objectStr
		work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifest)
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
		{
			Group: metav1.APIGroup{
				Name: "apps",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1", GroupVersion: "apps/v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1", GroupVersion: "apps/v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "deployments", Group: "apps", Namespaced: true, Kind: "Deployment"},
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
	cond := findManifestConditionByIndex(index, conds)
	if cond == nil {
		t.Errorf("expected to find the condition with index %d", index)
	}

	assertCondition(t, cond.Conditions, expectedCondition, expectedStatus)
}

type testCase struct {
	name                       string
	workManifest               []*unstructured.Unstructured
	spokeObject                []runtime.Object
	spokeDynamicObject         []runtime.Object
	expectedWorkAction         []string
	expectedKubeAction         []string
	expectedDynamicAction      []string
	expectedManifestConditions []expectedCondition
	expectedWorkConditions     []expectedCondition
}

type expectedCondition struct {
	conditionType string
	status        metav1.ConditionStatus
}

func newTestCase(name string) *testCase {
	return &testCase{
		name:                       name,
		workManifest:               []*unstructured.Unstructured{},
		spokeObject:                []runtime.Object{},
		spokeDynamicObject:         []runtime.Object{},
		expectedWorkAction:         []string{},
		expectedKubeAction:         []string{},
		expectedDynamicAction:      []string{},
		expectedManifestConditions: []expectedCondition{},
		expectedWorkConditions:     []expectedCondition{},
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

func (t *testCase) withExpectedManifestCondition(conds ...expectedCondition) *testCase {
	t.expectedManifestConditions = conds
	return t
}

func (t *testCase) withExpectedWorkCondition(conds ...expectedCondition) *testCase {
	t.expectedWorkConditions = conds
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
	for index, cond := range t.expectedManifestConditions {
		assertManifestCondition(ts, actualWork.Status.ResourceStatus.Manifests, int32(index), cond.conditionType, cond.status)
	}
	for _, cond := range t.expectedWorkConditions {
		assertCondition(ts, actualWork.Status.Conditions, cond.conditionType, cond.status)
	}
}

func newCondition(name, status, reason, message string, lastTransition *metav1.Time) workapiv1.StatusCondition {
	ret := workapiv1.StatusCondition{
		Type:    name,
		Status:  metav1.ConditionStatus(status),
		Reason:  reason,
		Message: message,
	}
	if lastTransition != nil {
		ret.LastTransitionTime = *lastTransition
	}
	return ret
}

func newManifestCondition(ordinal int32, resource string, conds ...workapiv1.StatusCondition) workapiv1.ManifestCondition {
	return workapiv1.ManifestCondition{
		ResourceMeta: workapiv1.ManifestResourceMeta{Ordinal: ordinal, Resource: resource},
		Conditions:   conds,
	}
}

func findManifestConditionByIndex(index int32, conds []workapiv1.ManifestCondition) *workapiv1.ManifestCondition {
	// Finds the cond conds that ordinal is the same as index
	if conds == nil {
		return nil
	}
	for i, cond := range conds {
		if index == cond.ResourceMeta.Ordinal {
			return &conds[i]
		}
	}

	return nil
}

// TestSync test cases when running sync
func TestSync(t *testing.T) {
	cases := []*testCase{
		newTestCase("create single resource").
			withWorkManifest(newUnstructured("v1", "Secret", "ns1", "test")).
			withExpectedWorkAction("get", "update").
			withExpectedKubeAction("get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("create single deployment resource").
			withWorkManifest(newUnstructured("apps/v1", "Deployment", "ns1", "test")).
			withExpectedWorkAction("get", "update").
			withExpectedDynamicAction("get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("update single resource").
			withWorkManifest(newUnstructured("v1", "Secret", "ns1", "test")).
			withSpokeObject(newSecret("test", "ns1", "value2")).
			withExpectedWorkAction("get", "update").
			withExpectedKubeAction("get", "delete", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("create single unstructured resource").
			withWorkManifest(newUnstructured("v1", "NewObject", "ns1", "test")).
			withExpectedWorkAction("get", "update").
			withExpectedDynamicAction("get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("update single unstructured resource").
			withWorkManifest(newUnstructuredWithContent("v1", "NewObject", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withSpokeDynamicObject(newUnstructuredWithContent("v1", "NewObject", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}})).
			withExpectedWorkAction("get", "update").
			withExpectedDynamicAction("get", "update").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("multiple create&update resource").
			withWorkManifest(newUnstructured("v1", "Secret", "ns1", "test"), newUnstructured("v1", "Secret", "ns2", "test")).
			withSpokeObject(newSecret("test", "ns1", "value2")).
			withExpectedWorkAction("get", "update").
			withExpectedKubeAction("get", "delete", "create", "get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}, expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
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
		withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}, expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionFalse}).
		withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionFalse})

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

func TestGenerateUpdateStatusFunc(t *testing.T) {
	transitionTime := metav1.Now()

	cases := []struct {
		name                     string
		startingStatusConditions []workapiv1.StatusCondition
		manifestConditions       []workapiv1.ManifestCondition
		expectedStatusConditions []workapiv1.StatusCondition
	}{
		{
			name:                     "no manifest condition exists",
			manifestConditions:       []workapiv1.ManifestCondition{},
			expectedStatusConditions: []workapiv1.StatusCondition{},
		},
		{
			name: "all manifests are applied successfully",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
			},
			expectedStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", nil),
			},
		},
		{
			name: "one of manifests is not applied",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionFalse), "my-reason", "my-message", nil)),
			},
			expectedStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionFalse), "AppliedManifestWorkFailed", "Failed to apply manifest work", nil),
			},
		},
		{
			name: "update existing status condition",
			startingStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", &transitionTime),
			},
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
			},
			expectedStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", &transitionTime),
			},
		},
		{
			name: "override existing status conditions",
			startingStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", nil),
			},
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionFalse), "my-reason", "my-message", nil)),
			},
			expectedStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionFalse), "AppliedManifestWorkFailed", "Failed to apply manifest work", nil),
			},
		},
	}

	controller := &ManifestWorkController{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			updateStatusFunc := controller.generateUpdateStatusFunc(workapiv1.ManifestResourceStatus{Manifests: c.manifestConditions})
			manifestWorkStatus := &workapiv1.ManifestWorkStatus{
				Conditions: c.startingStatusConditions,
			}
			err := updateStatusFunc(manifestWorkStatus)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			for i, expect := range c.expectedStatusConditions {
				actual := manifestWorkStatus.Conditions[i]
				if expect.LastTransitionTime == (metav1.Time{}) {
					actual.LastTransitionTime = metav1.Time{}
				}

				if !equality.Semantic.DeepEqual(actual, expect) {
					t.Errorf(diff.ObjectDiff(actual, expect))
				}
			}
		})
	}
}

func TestAllInCondition(t *testing.T) {
	cases := []struct {
		name               string
		conditionType      string
		manifestConditions []workapiv1.ManifestCondition
		expected           []bool
	}{
		{
			name:          "condition does not exist",
			conditionType: "one",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition("two", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition("two", string(metav1.ConditionFalse), "my-reason", "my-message", nil)),
			},
			expected: []bool{false, false},
		},
		{
			name:          "all manifests are in the condition",
			conditionType: "one",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(2, "resource0", newCondition("two", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(3, "resource1", newCondition("two", string(metav1.ConditionFalse), "my-reason", "my-message", nil)),
			},
			expected: []bool{true, true},
		},
		{
			name:          "one of manifests is not in the condition",
			conditionType: "two",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(2, "resource0", newCondition("two", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(3, "resource1", newCondition("two", string(metav1.ConditionFalse), "my-reason", "my-message", nil)),
			},
			expected: []bool{false, true},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inCondition, exists := allInCondition(c.conditionType, c.manifestConditions)
			if c.expected[0] != inCondition {
				t.Errorf("expected %t, but %t", c.expected[0], inCondition)
			}

			if c.expected[1] != exists {
				t.Errorf("expected %t, but %t", c.expected[1], exists)
			}
		})
	}
}
