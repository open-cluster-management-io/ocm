package manifestcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/spoke/apply"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth/basic"
	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
	"open-cluster-management.io/ocm/test/integration/util"
)

const defaultOwner = "testowner"

type testController struct {
	controller    *ManifestWorkController
	dynamicClient *fakedynamic.FakeDynamicClient
	workClient    *fakeworkclient.Clientset
	kubeClient    *fakekube.Clientset
	mwReconciler  *manifestworkReconciler
}

func newController(t *testing.T, work *workapiv1.ManifestWork, appliedWork *workapiv1.AppliedManifestWork, mapper meta.RESTMapper) *testController {
	fakeWorkClient := fakeworkclient.NewSimpleClientset(work)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fakeWorkClient, 5*time.Minute, workinformers.WithNamespace("cluster1"))
	spokeKubeClient := fakekube.NewSimpleClientset()
	controller := &ManifestWorkController{
		manifestWorkPatcher: patcher.NewPatcher[
			*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
			fakeWorkClient.WorkV1().ManifestWorks("cluster1")),
		manifestWorkLister: workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("cluster1"),
		appliedManifestWorkPatcher: patcher.NewPatcher[
			*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus](
			fakeWorkClient.WorkV1().AppliedManifestWorks()),
		appliedManifestWorkClient: fakeWorkClient.WorkV1().AppliedManifestWorks(),
		appliedManifestWorkLister: workInformerFactory.Work().V1().AppliedManifestWorks().Lister(),
		reconcilers:               []workReconcile{},
	}

	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(work); err != nil {
		t.Fatal(err)
	}
	if appliedWork != nil {
		if err := workInformerFactory.Work().V1().AppliedManifestWorks().Informer().GetStore().Add(appliedWork); err != nil {
			t.Fatal(err)
		}
	}

	return &testController{
		controller: controller,
		workClient: fakeWorkClient,
		mwReconciler: &manifestworkReconciler{
			restMapper: mapper,
			validator:  basic.NewSARValidator(nil, spokeKubeClient),
		},
	}
}

func (t *testController) toController() *ManifestWorkController {
	t.mwReconciler.appliers = apply.NewAppliers(t.dynamicClient, t.kubeClient, nil)
	t.controller.reconcilers = []workReconcile{
		t.mwReconciler,
	}
	return t.controller
}

func (t *testController) withKubeObject(objects ...runtime.Object) *testController {
	kubeClient := fakekube.NewClientset(objects...)
	t.kubeClient = kubeClient
	return t
}

func (t *testController) withUnstructuredObject(objects ...runtime.Object) *testController {
	scheme := runtime.NewScheme()
	dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)
	t.dynamicClient = dynamicClient
	return t
}

func assertManifestCondition(
	t *testing.T, conds []workapiv1.ManifestCondition, index int32, expected metav1.Condition) {
	cond := findManifestConditionByIndex(index, conds)
	if cond == nil {
		t.Errorf("expected to find the condition with index %d", index)
		return
	}

	if err := util.CheckExpectedConditions(cond.Conditions, expected); err != nil {
		t.Error(err)
	}
}

type testCase struct {
	name                       string
	workManifest               []*unstructured.Unstructured
	workManifestConfig         []workapiv1.ManifestConfigOption
	deleteOption               *workapiv1.DeleteOption
	spokeObject                []runtime.Object
	spokeDynamicObject         []runtime.Object
	expectedWorkAction         []string
	expectedKubeAction         []string
	expectedAppliedWorkAction  []string
	expectedDynamicAction      []string
	expectedManifestConditions []metav1.Condition
	expectedWorkConditions     []metav1.Condition
	existingWorkConditions     []metav1.Condition
}

func expectedCondition(conditionType string, status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{
		Type:   conditionType,
		Status: status,
	}
}

func newTestCase(name string) *testCase {
	return &testCase{
		name:                       name,
		workManifest:               []*unstructured.Unstructured{},
		workManifestConfig:         []workapiv1.ManifestConfigOption{},
		spokeObject:                []runtime.Object{},
		spokeDynamicObject:         []runtime.Object{},
		expectedWorkAction:         []string{},
		expectedAppliedWorkAction:  []string{},
		expectedKubeAction:         []string{},
		expectedDynamicAction:      []string{},
		expectedManifestConditions: []metav1.Condition{},
		expectedWorkConditions:     []metav1.Condition{},
		existingWorkConditions:     []metav1.Condition{},
	}
}

func (t *testCase) withWorkManifest(objects ...*unstructured.Unstructured) *testCase {
	t.workManifest = objects
	return t
}

func (t *testCase) withManifestConfig(configs ...workapiv1.ManifestConfigOption) *testCase {
	t.workManifestConfig = configs
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

func (t *testCase) withExpectedManifestCondition(conds ...metav1.Condition) *testCase {
	t.expectedManifestConditions = conds
	return t
}

func (t *testCase) withExpectedWorkCondition(conds ...metav1.Condition) *testCase {
	t.expectedWorkConditions = conds
	return t
}

func (t *testCase) withExistingWorkCondition(conds ...metav1.Condition) *testCase {
	t.existingWorkConditions = conds
	return t
}

func (t *testCase) validate(
	ts *testing.T,
	dynamicClient *fakedynamic.FakeDynamicClient,
	workClient *fakeworkclient.Clientset,
	kubeClient *fakekube.Clientset) {
	var actualWorkActions, actualAppliedWorkActions []clienttesting.Action
	for _, workAction := range workClient.Actions() {
		if workAction.GetResource().Resource == "manifestworks" {
			actualWorkActions = append(actualWorkActions, workAction)
		}
		if workAction.GetResource().Resource == "appliedmanifestworks" {
			actualAppliedWorkActions = append(actualAppliedWorkActions, workAction)
		}
	}
	testingcommon.AssertActions(ts, actualWorkActions, t.expectedWorkAction...)
	testingcommon.AssertActions(ts, actualAppliedWorkActions, t.expectedAppliedWorkAction...)

	spokeDynamicActions := dynamicClient.Actions()
	testingcommon.AssertActions(ts, spokeDynamicActions, t.expectedDynamicAction...)
	spokeKubeActions := kubeClient.Actions()
	testingcommon.AssertActions(ts, spokeKubeActions, t.expectedKubeAction...)

	if len(t.expectedWorkAction) == 0 {
		// No work actions to inspect
		return
	}

	actualWork := &workapiv1.ManifestWork{}
	switch actual := actualWorkActions[len(actualWorkActions)-1].(type) {
	case clienttesting.PatchActionImpl:
		p := actual.Patch
		if err := json.Unmarshal(p, actualWork); err != nil {
			ts.Fatal(err)
		}
	case clienttesting.DeleteActionImpl:
		if len(t.expectedManifestConditions) > 0 || len(t.expectedWorkConditions) > 0 {
			ts.Fatal("Unable to validate conditions on a delete action")
		}
	default:
		ts.Fatalf("Unexpected work action: %+v", actual)
	}

	for index, cond := range t.expectedManifestConditions {
		assertManifestCondition(ts, actualWork.Status.ResourceStatus.Manifests, int32(index), cond) //nolint:gosec
	}
	if errs := util.CheckExpectedConditions(actualWork.Status.Conditions, t.expectedWorkConditions...); errs != nil {
		for _, err := range errs.Errors() {
			ts.Error(err)
		}
		ts.FailNow()
	}
}

func (t *testCase) newManifestWork() (*workapiv1.ManifestWork, string) {
	work, workKey := spoketesting.NewManifestWork(0, t.workManifest...)
	work.Status.Conditions = t.existingWorkConditions
	work.Spec.ManifestConfigs = t.workManifestConfig
	work.Spec.DeleteOption = t.deleteOption
	work.Finalizers = []string{workapiv1.ManifestWorkFinalizer}

	return work, workKey
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
			withWorkManifest(testingcommon.NewUnstructured("v1", "Secret", "ns1", "test")).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedKubeAction("get", "create").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("create single deployment resource").
			withWorkManifest(testingcommon.NewUnstructured("apps/v1", "Deployment", "ns1", "test")).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("get", "create").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("update single resource").
			withWorkManifest(testingcommon.NewUnstructured("v1", "Secret", "ns1", "test")).
			withSpokeObject(spoketesting.NewSecret("test", "ns1", "value2")).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedKubeAction("get", "delete", "create").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("create single unstructured resource").
			withWorkManifest(testingcommon.NewUnstructured("v1", "NewObject", "ns1", "test")).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("get", "create").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("update single unstructured resource").
			withWorkManifest(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withSpokeDynamicObject(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}})).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("get", "update").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("multiple create&update resource").
			withWorkManifest(testingcommon.NewUnstructured(
				"v1", "Secret", "ns1", "test"),
				testingcommon.NewUnstructured("v1", "Secret", "ns2", "test")).
			withSpokeObject(spoketesting.NewSecret("test", "ns1", "value2")).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedKubeAction("get", "delete", "create", "get", "create").
			withExpectedManifestCondition(
				expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue),
				expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("ignore completed manifestwork").
			withWorkManifest(testingcommon.NewUnstructured("v1", "Secret", "ns1", "test")).
			withSpokeObject(spoketesting.NewSecret("test", "ns1", "value2")).
			withExistingWorkCondition(metav1.Condition{Type: workapiv1.WorkComplete, Status: metav1.ConditionTrue}),
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work, workKey := c.newManifestWork()
			controller := newController(t, work, nil, spoketesting.NewFakeRestMapper()).
				withKubeObject(c.spokeObject...).
				withUnstructuredObject(c.spokeDynamicObject...)
			syncContext := testingcommon.NewFakeSyncContext(t, workKey)
			err := controller.toController().sync(context.TODO(), syncContext, work.Name)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			c.validate(t, controller.dynamicClient, controller.workClient, controller.kubeClient)

			// Verify ObservedGeneration is set in ManifestApplied conditions
			if len(c.expectedWorkAction) > 0 {
				var actualWorkActions []clienttesting.Action
				for _, workAction := range controller.workClient.Actions() {
					if workAction.GetResource().Resource == "manifestworks" {
						actualWorkActions = append(actualWorkActions, workAction)
					}
				}
				if len(actualWorkActions) > 0 {
					if patchAction, ok := actualWorkActions[len(actualWorkActions)-1].(clienttesting.PatchActionImpl); ok {
						patchedWork := &workapiv1.ManifestWork{}
						if err := json.Unmarshal(patchAction.Patch, patchedWork); err == nil {
							// Get the expected generation from the original work object, not the patch payload
							expectedGeneration := work.Generation
							for _, manifest := range patchedWork.Status.ResourceStatus.Manifests {
								appliedCond := meta.FindStatusCondition(manifest.Conditions, workapiv1.ManifestApplied)
								if appliedCond != nil && appliedCond.ObservedGeneration != expectedGeneration {
									t.Errorf("Expected ObservedGeneration %d in ManifestApplied condition, got %d",
										expectedGeneration, appliedCond.ObservedGeneration)
								}
							}
						}
					}
				}
			}
		})
	}
}

// Test applying resource failed
func TestFailedToApplyResource(t *testing.T) {
	tc := newTestCase("multiple create&update resource").
		withWorkManifest(testingcommon.NewUnstructured(
			"v1", "Secret", "ns1", "test"),
			testingcommon.NewUnstructured("v1", "Secret", "ns2", "test")).
		withSpokeObject(spoketesting.NewSecret("test", "ns1", "value2")).
		withExpectedWorkAction("patch").
		withAppliedWorkAction("create").
		withExpectedKubeAction("get", "delete", "create", "get", "create").
		withExpectedManifestCondition(
			expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue),
			expectedCondition(workapiv1.ManifestApplied, metav1.ConditionFalse)).
		withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionFalse))

	work, workKey := tc.newManifestWork()
	controller := newController(t, work, nil, spoketesting.NewFakeRestMapper()).withKubeObject(tc.spokeObject...).withUnstructuredObject()

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

		return true, &corev1.Secret{}, fmt.Errorf("fake error")
	})
	syncContext := testingcommon.NewFakeSyncContext(t, workKey)
	err := controller.toController().sync(context.TODO(), syncContext, work.Name)
	if err == nil {
		t.Errorf("Should return an err")
	}

	tc.validate(t, controller.dynamicClient, controller.workClient, controller.kubeClient)
}

func TestUpdateStrategy(t *testing.T) {
	cases := []*testCase{
		newTestCase("update single resource with nil updateStrategy").
			withWorkManifest(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withSpokeDynamicObject(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}})).
			withManifestConfig(newManifestConfigOption(
				"", "newobjects", "ns1", "n1", nil)).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("get", "update").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("update single resource with update updateStrategy").
			withWorkManifest(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withSpokeDynamicObject(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}})).
			withManifestConfig(newManifestConfigOption(
				"", "newobjects", "ns1", "n1",
				&workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeUpdate})).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("get", "update").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("create single resource with updateStrategy not found").
			withWorkManifest(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withSpokeDynamicObject(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}})).
			withManifestConfig(newManifestConfigOption(
				"", "newobjects", "ns1", "n2",
				&workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeServerSideApply})).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("get", "update").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("create single resource with server side apply updateStrategy").
			withWorkManifest(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withManifestConfig(newManifestConfigOption(
				"", "newobjects", "ns1", "n1",
				&workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeServerSideApply})).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("patch", "patch").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("update single resource with server side apply updateStrategy").
			withWorkManifest(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withSpokeDynamicObject(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}})).
			withManifestConfig(newManifestConfigOption(
				"", "newobjects", "ns1", "n1",
				&workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeServerSideApply})).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("patch", "patch").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("update single resource with create only updateStrategy").
			withWorkManifest(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withSpokeDynamicObject(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}})).
			withManifestConfig(newManifestConfigOption(
				"", "newobjects", "ns*", "*",
				&workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeCreateOnly})).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("get", "patch").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
		newTestCase("update multi resources with create only updateStrategy").
			withWorkManifest(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}}),
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns2", "n2",
					map[string]interface{}{"spec": map[string]interface{}{"key2": "val2"}})).
			withSpokeDynamicObject(testingcommon.NewUnstructuredWithContent(
				"v1", "NewObject", "ns1", "n1",
				map[string]interface{}{"spec": map[string]interface{}{"key2": "val2"}}),
				testingcommon.NewUnstructuredWithContent(
					"v1", "NewObject", "ns2", "n2",
					map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withManifestConfig(newManifestConfigOption(
				"", "newobjects", "ns*", "*",
				&workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeCreateOnly})).
			withExpectedWorkAction("patch").
			withAppliedWorkAction("create").
			withExpectedDynamicAction("get", "patch", "get", "patch").
			withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionTrue)).
			withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionTrue)),
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work, workKey := c.newManifestWork()
			controller := newController(t, work, nil, spoketesting.NewFakeRestMapper()).
				withKubeObject(c.spokeObject...).
				withUnstructuredObject(c.spokeDynamicObject...)

			// The default reactor doesn't support apply, so we need our own (trivial) reactor
			controller.dynamicClient.PrependReactor("patch", "newobjects",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					// clusterroleaggregator drops returned objects so no point in constructing them
					return true, testingcommon.NewUnstructuredWithContent(
						"v1", "NewObject", "ns1", "n1",
						map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}}), nil
				})
			syncContext := testingcommon.NewFakeSyncContext(t, workKey)
			err := controller.toController().sync(context.TODO(), syncContext, work.Name)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			c.validate(t, controller.dynamicClient, controller.workClient, controller.kubeClient)
		})
	}
}

func TestServerSideApplyConflict(t *testing.T) {
	testCase := newTestCase("update single resource with server side apply updateStrategy").
		withWorkManifest(testingcommon.NewUnstructuredWithContent(
			"v1", "NewObject", "ns1", "n1",
			map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
		withSpokeDynamicObject(testingcommon.NewUnstructuredWithContent(
			"v1", "NewObject", "ns1", "n1",
			map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}})).
		withManifestConfig(newManifestConfigOption(
			"", "newobjects", "ns1", "n1",
			&workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeServerSideApply})).
		withExpectedWorkAction("patch").
		withAppliedWorkAction("create").
		withExpectedDynamicAction("patch").
		withExpectedManifestCondition(expectedCondition(workapiv1.ManifestApplied, metav1.ConditionFalse)).
		withExpectedWorkCondition(expectedCondition(workapiv1.WorkApplied, metav1.ConditionFalse))

	work, workKey := testCase.newManifestWork()
	controller := newController(t, work, nil, spoketesting.NewFakeRestMapper()).
		withKubeObject(testCase.spokeObject...).
		withUnstructuredObject(testCase.spokeDynamicObject...)

	// The default reactor doesn't support apply, so we need our own (trivial) reactor
	controller.dynamicClient.PrependReactor("patch", "newobjects", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.NewConflict(schema.GroupResource{Resource: "newobjects"}, "n1", fmt.Errorf("conflict error"))
	})
	syncContext := testingcommon.NewFakeSyncContext(t, workKey)
	err := controller.toController().sync(context.TODO(), syncContext, work.Name)
	if err != nil {
		t.Errorf("Should be success with no err: %v", err)
	}

	testCase.validate(t, controller.dynamicClient, controller.workClient, controller.kubeClient)
}

func newManifestConfigOption(
	group, resource, namespace, name string, strategy *workapiv1.UpdateStrategy, rules ...workapiv1.ConditionRule,
) workapiv1.ManifestConfigOption {
	return workapiv1.ManifestConfigOption{
		ResourceIdentifier: workapiv1.ResourceIdentifier{
			Resource:  resource,
			Group:     group,
			Namespace: namespace,
			Name:      name,
		},
		UpdateStrategy: strategy,
		ConditionRules: rules,
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
			object:   testingcommon.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			expected: workapiv1.ManifestResourceMeta{Version: "v1", Kind: "Kind1", Namespace: "ns1", Name: "n1"},
		},
		{
			name:       "build meta for unstructured object with rest mapper",
			object:     testingcommon.NewUnstructured("v1", "NewObject", "ns1", "n1"),
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
			actual, _, err := helper.BuildResourceMeta(0, c.object, c.restMapper)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			actual.Ordinal = c.expected.Ordinal
			if !equality.Semantic.DeepEqual(actual, c.expected) {
				t.Errorf("%s", cmp.Diff(actual, c.expected))
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
			actual, _, err := helper.BuildResourceMeta(0, c.manifestObject, c.restMapper)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			actual.Ordinal = c.expected.Ordinal
			if !equality.Semantic.DeepEqual(actual, c.expected) {
				t.Errorf("%s", cmp.Diff(actual, c.expected))
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
			owner:       metav1.OwnerReference{UID: defaultOwner},
			expectOwner: metav1.OwnerReference{UID: defaultOwner},
		},
		{
			name:         "orphan the resource",
			owner:        metav1.OwnerReference{UID: defaultOwner},
			deleteOption: &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan},
			expectOwner:  metav1.OwnerReference{UID: "testowner-"},
		},
		{
			name:         "add owner if no orphan rule with selectively orphan",
			owner:        metav1.OwnerReference{UID: defaultOwner},
			deleteOption: &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan},
			expectOwner:  metav1.OwnerReference{UID: defaultOwner},
		},
		{
			name:  "orphan the resource with selectively orphan",
			owner: metav1.OwnerReference{UID: defaultOwner},
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
			owner: metav1.OwnerReference{UID: defaultOwner},
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
			expectOwner: metav1.OwnerReference{UID: defaultOwner},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			owner := manageOwnerRef(helper.OwnedByTheWork(testGVR, namespace, name, c.deleteOption), c.owner)

			if !equality.Semantic.DeepEqual(owner, c.expectOwner) {
				t.Errorf("Expect owner is %v, but got %v", c.expectOwner, owner)
			}
		})
	}
}

func TestOnAddFunc(t *testing.T) {
	cases := []struct {
		name         string
		obj          interface{}
		expectQueued bool
	}{
		{
			name: "object with finalizer should be queued",
			obj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Finalizers: []string{workapiv1.ManifestWorkFinalizer},
				},
			},
			expectQueued: true,
		},
		{
			name: "object without finalizer should not be queued",
			obj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Finalizers: []string{},
				},
			},
			expectQueued: false,
		},
		{
			name: "object with other finalizers should not be queued",
			obj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Finalizers: []string{"other.finalizer"},
				},
			},
			expectQueued: false,
		},
		{
			name: "object with multiple finalizers including manifest work finalizer should be queued",
			obj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Finalizers: []string{"other.finalizer", workapiv1.ManifestWorkFinalizer},
				},
			},
			expectQueued: true,
		},
		{
			name:         "invalid object should not queue anything",
			obj:          "invalid-object",
			expectQueued: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			queue := workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]())
			addFunc := onAddFunc(queue)

			addFunc(c.obj)

			queueLen := queue.Len()

			if c.expectQueued && queueLen == 0 {
				t.Errorf("Expected object to be queued but queue is empty")
			}
			if !c.expectQueued && queueLen > 0 {
				t.Errorf("Expected object not to be queued but queue has %d items", queueLen)
			}

			if queueLen > 0 {
				item, _ := queue.Get()
				queue.Done(item)
				if c.expectQueued {
					expectedName := c.obj.(*workapiv1.ManifestWork).Name
					if item != expectedName {
						t.Errorf("Expected queued item to be %s but got %s", expectedName, item)
					}
				}
			}
		})
	}
}

func TestOnUpdateFunc(t *testing.T) {
	cases := []struct {
		name         string
		oldObj       interface{}
		newObj       interface{}
		expectQueued bool
	}{
		{
			name: "object with finalizer and spec change should be queued",
			oldObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 1,
					Finalizers: []string{workapiv1.ManifestWorkFinalizer},
				},
				Spec: workapiv1.ManifestWorkSpec{
					DeleteOption: &workapiv1.DeleteOption{
						PropagationPolicy: workapiv1.DeletePropagationPolicyTypeForeground,
					},
				},
			},
			newObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 2,
					Finalizers: []string{workapiv1.ManifestWorkFinalizer},
				},
				Spec: workapiv1.ManifestWorkSpec{
					DeleteOption: &workapiv1.DeleteOption{
						PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan,
					},
				},
			},
			expectQueued: true,
		},
		{
			name: "object with finalizer added should be queued",
			oldObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 1,
					Finalizers: []string{},
				},
			},
			newObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 1,
					Finalizers: []string{workapiv1.ManifestWorkFinalizer},
				},
			},
			expectQueued: true,
		},
		{
			name: "object with finalizer but no generation change should not be queued",
			oldObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 1,
					Finalizers: []string{workapiv1.ManifestWorkFinalizer},
				},
			},
			newObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 1,
					Finalizers: []string{workapiv1.ManifestWorkFinalizer},
				},
			},
			expectQueued: false,
		},
		{
			name: "object without finalizer should not be queued",
			oldObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 1,
					Finalizers: []string{},
				},
			},
			newObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 2,
					Finalizers: []string{},
				},
			},
			expectQueued: false,
		},
		{
			name: "object where finalizer was removed should not be queued",
			oldObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 1,
					Finalizers: []string{workapiv1.ManifestWorkFinalizer},
				},
			},
			newObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 2,
					Finalizers: []string{},
				},
			},
			expectQueued: false,
		},
		{
			name: "invalid new object should not queue anything",
			oldObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 1,
					Finalizers: []string{workapiv1.ManifestWorkFinalizer},
				},
			},
			newObj:       "invalid-object",
			expectQueued: false,
		},
		{
			name:   "invalid old object should not queue anything",
			oldObj: "invalid-object",
			newObj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Generation: 2,
					Finalizers: []string{workapiv1.ManifestWorkFinalizer},
				},
			},
			expectQueued: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			queue := workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]())
			updateFunc := onUpdateFunc(queue)

			updateFunc(c.oldObj, c.newObj)

			queueLen := queue.Len()

			if c.expectQueued && queueLen == 0 {
				t.Errorf("Expected object to be queued but queue is empty")
			}
			if !c.expectQueued && queueLen > 0 {
				t.Errorf("Expected object not to be queued but queue has %d items", queueLen)
			}

			if queueLen > 0 {
				item, _ := queue.Get()
				queue.Done(item)
				if c.expectQueued {
					expectedName := c.newObj.(*workapiv1.ManifestWork).Name
					if item != expectedName {
						t.Errorf("Expected queued item to be %s but got %s", expectedName, item)
					}
				}
			}
		})
	}
}
