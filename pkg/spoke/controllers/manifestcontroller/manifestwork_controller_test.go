package manifestcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/diff"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/controllers"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

type testController struct {
	controller    *ManifestWorkController
	dynamicClient *fakedynamic.FakeDynamicClient
	workClient    *fakeworkclient.Clientset
	kubeClient    *fakekube.Clientset
}

func newController(work *workapiv1.ManifestWork, appliedWork *workapiv1.AppliedManifestWork, mapper meta.RESTMapper) *testController {
	fakeWorkClient := fakeworkclient.NewSimpleClientset(work)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fakeWorkClient, 5*time.Minute, workinformers.WithNamespace("cluster1"))

	controller := &ManifestWorkController{
		manifestWorkClient:        fakeWorkClient.WorkV1().ManifestWorks("cluster1"),
		manifestWorkLister:        workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("cluster1"),
		appliedManifestWorkClient: fakeWorkClient.WorkV1().AppliedManifestWorks(),
		appliedManifestWorkLister: workInformerFactory.Work().V1().AppliedManifestWorks().Lister(),
		restMapper:                mapper,
		staticResourceCache:       resourceapply.NewResourceCache(),
	}

	workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(work)
	if appliedWork != nil {
		workInformerFactory.Work().V1().AppliedManifestWorks().Informer().GetStore().Add(appliedWork)
	}

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

func assertCondition(t *testing.T, conditions []metav1.Condition, expectedCondition string, expectedStatus metav1.ConditionStatus) {
	matched := meta.IsStatusConditionPresentAndEqual(conditions, expectedCondition, expectedStatus)

	if !matched {
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
	expectedAppliedWorkAction  []string
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
		expectedAppliedWorkAction:  []string{},
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

func (t *testCase) withAppliedWorkAction(action ...string) *testCase {
	t.expectedAppliedWorkAction = action
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
	actualWorkActions := []clienttesting.Action{}
	actualAppliedWorkActions := []clienttesting.Action{}
	for _, workAction := range workClient.Actions() {
		if workAction.GetResource().Resource == "manifestworks" {
			actualWorkActions = append(actualWorkActions, workAction)
		}
		if workAction.GetResource().Resource == "appliedmanifestworks" {
			actualAppliedWorkActions = append(actualAppliedWorkActions, workAction)
		}
	}
	if len(actualWorkActions) != len(t.expectedWorkAction) {
		ts.Errorf("Expected %d action but got %#v", len(t.expectedWorkAction), actualWorkActions)
	}
	for index := range actualWorkActions {
		spoketesting.AssertAction(ts, actualWorkActions[index], t.expectedWorkAction[index])
	}
	if len(actualAppliedWorkActions) != len(t.expectedAppliedWorkAction) {
		ts.Errorf("Expected %d action but got %#v", len(t.expectedAppliedWorkAction), actualAppliedWorkActions)
	}
	for index := range actualAppliedWorkActions {
		spoketesting.AssertAction(ts, actualAppliedWorkActions[index], t.expectedAppliedWorkAction[index])
	}

	spokeDynamicActions := dynamicClient.Actions()
	if len(spokeDynamicActions) != len(t.expectedDynamicAction) {
		ts.Errorf("Expected %d action but got %#v", len(t.expectedDynamicAction), spokeDynamicActions)
	}
	for index := range spokeDynamicActions {
		spoketesting.AssertAction(ts, spokeDynamicActions[index], t.expectedDynamicAction[index])
	}
	spokeKubeActions := kubeClient.Actions()
	if len(spokeKubeActions) != len(t.expectedKubeAction) {
		ts.Errorf("Expected %d action but got %#v", len(t.expectedKubeAction), spokeKubeActions)
	}
	for index := range spokeKubeActions {
		spoketesting.AssertAction(ts, spokeKubeActions[index], t.expectedKubeAction[index])
	}

	actual, ok := actualWorkActions[len(actualWorkActions)-1].(clienttesting.UpdateActionImpl)
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

func newCondition(name, status, reason, message string, generation int64, lastTransition *metav1.Time) metav1.Condition {
	ret := metav1.Condition{
		Type:               name,
		Status:             metav1.ConditionStatus(status),
		ObservedGeneration: generation,
		Reason:             reason,
		Message:            message,
	}
	if lastTransition != nil {
		ret.LastTransitionTime = *lastTransition
	}
	return ret
}

func newManifestCondition(ordinal int32, resource string, conds ...metav1.Condition) workapiv1.ManifestCondition {
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
			withWorkManifest(spoketesting.NewUnstructured("v1", "Secret", "ns1", "test")).
			withExpectedWorkAction("update").
			withAppliedWorkAction("create").
			withExpectedKubeAction("get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("create single deployment resource").
			withWorkManifest(spoketesting.NewUnstructured("apps/v1", "Deployment", "ns1", "test")).
			withExpectedWorkAction("update").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("update single resource").
			withWorkManifest(spoketesting.NewUnstructured("v1", "Secret", "ns1", "test")).
			withSpokeObject(spoketesting.NewSecret("test", "ns1", "value2")).
			withExpectedWorkAction("update").
			withAppliedWorkAction("create").
			withExpectedKubeAction("get", "delete", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("create single unstructured resource").
			withWorkManifest(spoketesting.NewUnstructured("v1", "NewObject", "ns1", "test")).
			withExpectedWorkAction("update").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("update single unstructured resource").
			withWorkManifest(spoketesting.NewUnstructuredWithContent("v1", "NewObject", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withSpokeDynamicObject(spoketesting.NewUnstructuredWithContent("v1", "NewObject", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}})).
			withExpectedWorkAction("update").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("get", "update").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("multiple create&update resource").
			withWorkManifest(spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"), spoketesting.NewUnstructured("v1", "Secret", "ns2", "test")).
			withSpokeObject(spoketesting.NewSecret("test", "ns1", "value2")).
			withExpectedWorkAction("update").
			withAppliedWorkAction("create").
			withExpectedKubeAction("get", "delete", "create", "get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}, expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work, workKey := spoketesting.NewManifestWork(0, c.workManifest...)
			work.Finalizers = []string{controllers.ManifestWorkFinalizer}
			controller := newController(work, nil, spoketesting.NewFakeRestMapper()).
				withKubeObject(c.spokeObject...).
				withUnstructuredObject(c.spokeDynamicObject...)
			syncContext := spoketesting.NewFakeSyncContext(t, workKey)
			err := controller.controller.sync(nil, syncContext)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			c.validate(t, controller.dynamicClient, controller.workClient, controller.kubeClient)
		})
	}
}

// Test applying resource failed
func TestFailedToApplyResource(t *testing.T) {
	tc := newTestCase("multiple create&update resource").
		withWorkManifest(spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"), spoketesting.NewUnstructured("v1", "Secret", "ns2", "test")).
		withSpokeObject(spoketesting.NewSecret("test", "ns1", "value2")).
		withExpectedWorkAction("update").
		withAppliedWorkAction("create").
		withExpectedKubeAction("get", "delete", "create", "get", "create").
		withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}, expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionFalse}).
		withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionFalse})

	work, workKey := spoketesting.NewManifestWork(0, tc.workManifest...)
	work.Finalizers = []string{controllers.ManifestWorkFinalizer}
	controller := newController(work, nil, spoketesting.NewFakeRestMapper()).withKubeObject(tc.spokeObject...).withUnstructuredObject()

	// Add a reactor on fake client to throw error when creating secret on namespace ns2
	controller.kubeClient.PrependReactor("create", "secrets", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
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
	syncContext := spoketesting.NewFakeSyncContext(t, workKey)
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
			obj1:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     spoketesting.NewUnstructured("v1", "Kind2", "ns1", "n1"),
			expected: false,
		},
		{
			name:     "different namespace",
			obj1:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     spoketesting.NewUnstructured("v1", "Kind1", "ns2", "n1"),
			expected: false,
		},
		{
			name:     "different name",
			obj1:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n2"),
			expected: false,
		},
		{
			name:     "different spec",
			obj1:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}}),
			obj2:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}}),
			expected: false,
		},
		{
			name:     "same spec, different status",
			obj1:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}, "status": "status1"}),
			obj2:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}, "status": "status2"}),
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
		startingStatusConditions []metav1.Condition
		manifestConditions       []workapiv1.ManifestCondition
		generation               int64
		expectedStatusConditions []metav1.Condition
	}{
		{
			name:                     "no manifest condition exists",
			manifestConditions:       []workapiv1.ManifestCondition{},
			expectedStatusConditions: []metav1.Condition{},
		},
		{
			name: "all manifests are applied successfully",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
			},
			expectedStatusConditions: []metav1.Condition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", 0, nil),
			},
		},
		{
			name: "one of manifests is not applied",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionFalse), "my-reason", "my-message", 0, nil)),
			},
			expectedStatusConditions: []metav1.Condition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionFalse), "AppliedManifestWorkFailed", "Failed to apply manifest work", 0, nil),
			},
		},
		{
			name: "update existing status condition",
			startingStatusConditions: []metav1.Condition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", 0, &transitionTime),
			},
			generation: 1,
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
			},
			expectedStatusConditions: []metav1.Condition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", 1, &transitionTime),
			},
		},
		{
			name: "override existing status conditions",
			startingStatusConditions: []metav1.Condition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", 0, nil),
			},
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionFalse), "my-reason", "my-message", 0, nil)),
			},
			generation: 1,
			expectedStatusConditions: []metav1.Condition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionFalse), "AppliedManifestWorkFailed", "Failed to apply manifest work", 1, nil),
			},
		},
	}

	controller := &ManifestWorkController{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			updateStatusFunc := controller.generateUpdateStatusFunc(c.generation, c.manifestConditions)
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
				newManifestCondition(0, "resource0", newCondition("two", string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
				newManifestCondition(1, "resource1", newCondition("two", string(metav1.ConditionFalse), "my-reason", "my-message", 0, nil)),
			},
			expected: []bool{false, false},
		},
		{
			name:          "all manifests are in the condition",
			conditionType: "one",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
				newManifestCondition(1, "resource1", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
				newManifestCondition(2, "resource0", newCondition("two", string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
				newManifestCondition(3, "resource1", newCondition("two", string(metav1.ConditionFalse), "my-reason", "my-message", 0, nil)),
			},
			expected: []bool{true, true},
		},
		{
			name:          "one of manifests is not in the condition",
			conditionType: "two",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
				newManifestCondition(1, "resource1", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
				newManifestCondition(2, "resource0", newCondition("two", string(metav1.ConditionTrue), "my-reason", "my-message", 0, nil)),
				newManifestCondition(3, "resource1", newCondition("two", string(metav1.ConditionFalse), "my-reason", "my-message", 0, nil)),
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

func TestBuildResourceMeta(t *testing.T) {
	var secret *corev1.Secret
	var u *unstructured.Unstructured

	cases := []struct {
		name       string
		object     runtime.Object
		restMapper meta.RESTMapper
		expected   workapiv1.ManifestResourceMeta
	}{
		{
			name:     "build meta for non-unstructured object",
			object:   spoketesting.NewSecret("test", "ns1", "value2"),
			expected: workapiv1.ManifestResourceMeta{Version: "v1", Kind: "Secret", Namespace: "ns1", Name: "test"},
		},
		{
			name:       "build meta for non-unstructured object with rest mapper",
			object:     spoketesting.NewSecret("test", "ns1", "value2"),
			restMapper: spoketesting.NewFakeRestMapper(),
			expected:   workapiv1.ManifestResourceMeta{Version: "v1", Kind: "Secret", Resource: "secrets", Namespace: "ns1", Name: "test"},
		},
		{
			name:     "build meta for non-unstructured nil",
			object:   secret,
			expected: workapiv1.ManifestResourceMeta{},
		},
		{
			name:     "build meta for unstructured object",
			object:   spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			expected: workapiv1.ManifestResourceMeta{Version: "v1", Kind: "Kind1", Namespace: "ns1", Name: "n1"},
		},
		{
			name:       "build meta for unstructured object with rest mapper",
			object:     spoketesting.NewUnstructured("v1", "NewObject", "ns1", "n1"),
			restMapper: spoketesting.NewFakeRestMapper(),
			expected:   workapiv1.ManifestResourceMeta{Version: "v1", Kind: "NewObject", Resource: "newobjects", Namespace: "ns1", Name: "n1"},
		},
		{
			name:     "build meta for unstructured nil",
			object:   u,
			expected: workapiv1.ManifestResourceMeta{},
		},
		{
			name:     "build meta with nil",
			object:   nil,
			expected: workapiv1.ManifestResourceMeta{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual, _, err := buildResourceMeta(0, c.object, c.restMapper)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			actual.Ordinal = c.expected.Ordinal
			if !equality.Semantic.DeepEqual(actual, c.expected) {
				t.Errorf(diff.ObjectDiff(actual, c.expected))
			}
		})
	}
}

func TestBuildManifestResourceMeta(t *testing.T) {
	cases := []struct {
		name           string
		applyResult    runtime.Object
		manifestObject runtime.Object
		restMapper     meta.RESTMapper
		expected       workapiv1.ManifestResourceMeta
	}{
		{
			name:           "fall back to manifest",
			manifestObject: spoketesting.NewSecret("test2", "ns2", "value2"),
			restMapper:     spoketesting.NewFakeRestMapper(),
			expected:       workapiv1.ManifestResourceMeta{Version: "v1", Kind: "Secret", Resource: "secrets", Namespace: "ns2", Name: "test2"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			manifest := workapiv1.Manifest{}
			if c.manifestObject != nil {
				manifest.Object = c.manifestObject
			}
			actual, _, err := buildManifestResourceMeta(0, manifest, c.restMapper)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			actual.Ordinal = c.expected.Ordinal
			if !equality.Semantic.DeepEqual(actual, c.expected) {
				t.Errorf(diff.ObjectDiff(actual, c.expected))
			}
		})
	}
}

func TestManageOwner(t *testing.T) {
	testGVR := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

	namespace := "testns"

	name := "test"

	cases := []struct {
		name         string
		deleteOption *workapiv1.DeleteOption
		owner        metav1.OwnerReference
		expectOwner  metav1.OwnerReference
	}{
		{
			name:        "foreground by default",
			owner:       metav1.OwnerReference{UID: "testowner"},
			expectOwner: metav1.OwnerReference{UID: "testowner"},
		},
		{
			name:         "orphan the resource",
			owner:        metav1.OwnerReference{UID: "testowner"},
			deleteOption: &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan},
			expectOwner:  metav1.OwnerReference{UID: "testowner-"},
		},
		{
			name:         "add owner if no orphan rule with selectively orphan",
			owner:        metav1.OwnerReference{UID: "testowner"},
			deleteOption: &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan},
			expectOwner:  metav1.OwnerReference{UID: "testowner"},
		},
		{
			name:  "orphan the resource with selectively orphan",
			owner: metav1.OwnerReference{UID: "testowner"},
			deleteOption: &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
				SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
					OrphaningRules: []workapiv1.OrphaningRule{
						{
							Group:     "",
							Resource:  "secrets",
							Namespace: namespace,
							Name:      name,
						},
					},
				},
			},
			expectOwner: metav1.OwnerReference{UID: "testowner-"},
		},
		{
			name:  "add owner if resourcec is not matched in orphan rule with selectively orphan",
			owner: metav1.OwnerReference{UID: "testowner"},
			deleteOption: &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
				SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
					OrphaningRules: []workapiv1.OrphaningRule{
						{
							Group:     "",
							Resource:  "secrets",
							Namespace: "testns1",
							Name:      name,
						},
					},
				},
			},
			expectOwner: metav1.OwnerReference{UID: "testowner"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			owner := manageOwnerRef(testGVR, namespace, name, c.deleteOption, c.owner)

			if !equality.Semantic.DeepEqual(owner, c.expectOwner) {
				t.Errorf("Expect owner is %v, but got %v", c.expectOwner, owner)
			}
		})
	}
}

func TestApplyUnstructred(t *testing.T) {
	cases := []struct {
		name            string
		owner           metav1.OwnerReference
		existingObject  []runtime.Object
		required        *unstructured.Unstructured
		gvr             schema.GroupVersionResource
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:           "create a new object with owner",
			existingObject: []runtime.Object{},
			owner:          metav1.OwnerReference{Name: "test", UID: "testowner"},
			required:       spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:            schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect 2 actions, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "get")
				spoketesting.AssertAction(t, actions[1], "create")

				obj := actions[1].(clienttesting.CreateActionImpl).Object.(*unstructured.Unstructured)
				owners := obj.GetOwnerReferences()
				if len(owners) != 1 {
					t.Errorf("Expect 2 owners, but have %d", len(owners))
				}

				if owners[0].UID != "testowner" {
					t.Errorf("Owner UId is not correct, got %s", owners[0].UID)
				}
			},
		},
		{
			name:           "create a new object without owner",
			existingObject: []runtime.Object{},
			owner:          metav1.OwnerReference{Name: "test", UID: "testowner-"},
			required:       spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:            schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect 2 actions, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "get")
				spoketesting.AssertAction(t, actions[1], "create")

				obj := actions[1].(clienttesting.CreateActionImpl).Object.(*unstructured.Unstructured)
				owners := obj.GetOwnerReferences()
				if len(owners) != 0 {
					t.Errorf("Expect 1 owners, but have %d", len(owners))
				}
			},
		},
		{
			name:           "update an object owner",
			existingObject: []runtime.Object{spoketesting.NewUnstructured("v1", "Secret", "ns1", "test", metav1.OwnerReference{Name: "test1", UID: "testowner1"})},
			owner:          metav1.OwnerReference{Name: "test", UID: "testowner"},
			required:       spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:            schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect 2 actions, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "get")
				spoketesting.AssertAction(t, actions[1], "update")

				obj := actions[1].(clienttesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
				owners := obj.GetOwnerReferences()
				if len(owners) != 2 {
					t.Errorf("Expect 2 owners, but have %d", len(owners))
				}

				if owners[0].UID != "testowner1" {
					t.Errorf("Owner UId is not correct, got %s", owners[0].UID)
				}
				if owners[1].UID != "testowner" {
					t.Errorf("Owner UId is not correct, got %s", owners[1].UID)
				}
			},
		},
		{
			name:           "update an object without owner",
			existingObject: []runtime.Object{spoketesting.NewUnstructured("v1", "Secret", "ns1", "test", metav1.OwnerReference{Name: "test1", UID: "testowner1"})},
			owner:          metav1.OwnerReference{Name: "test", UID: "testowner-"},
			required:       spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:            schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("Expect 1 actions, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "get")
			},
		},
		{
			name:           "remove an object owner",
			existingObject: []runtime.Object{spoketesting.NewUnstructured("v1", "Secret", "ns1", "test", metav1.OwnerReference{Name: "test", UID: "testowner"})},
			owner:          metav1.OwnerReference{Name: "test", UID: "testowner-"},
			required:       spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:            schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect 2 actions, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "get")
				spoketesting.AssertAction(t, actions[1], "update")

				obj := actions[1].(clienttesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
				owners := obj.GetOwnerReferences()
				if len(owners) != 0 {
					t.Errorf("Expect 0 owner, but have %d", len(owners))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work, workKey := spoketesting.NewManifestWork(0)
			work.Finalizers = []string{controllers.ManifestWorkFinalizer}
			controller := newController(work, nil, spoketesting.NewFakeRestMapper()).
				withUnstructuredObject(c.existingObject...)
			syncContext := spoketesting.NewFakeSyncContext(t, workKey)

			data, _ := json.Marshal(c.required)
			_, _, err := controller.controller.applyUnstructured(
				context.TODO(), data, c.owner, c.gvr, syncContext.Recorder())

			if err != nil {
				t.Errorf("expect no error, but got %v", err)
			}

			c.validateActions(t, controller.dynamicClient.Actions())
		})
	}
}
