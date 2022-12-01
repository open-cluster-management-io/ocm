package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/go-cmp/cmp"
	"reflect"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

func newCondition(name, status, reason, message string, lastTransition *metav1.Time) metav1.Condition {
	ret := metav1.Condition{
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

func updateSpokeClusterConditionFn(cond metav1.Condition) UpdateManifestWorkStatusFunc {
	return func(oldStatus *workapiv1.ManifestWorkStatus) error {
		meta.SetStatusCondition(&oldStatus.Conditions, cond)
		return nil
	}
}

func newManifestCondition(ordinal int32, resource string, conds ...metav1.Condition) workapiv1.ManifestCondition {
	return workapiv1.ManifestCondition{
		ResourceMeta: workapiv1.ManifestResourceMeta{Ordinal: ordinal, Resource: resource},
		Conditions:   conds,
	}
}

func newSecret(namespace, name string, terminated bool, uid string, owner ...metav1.OwnerReference) *corev1.Secret {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			OwnerReferences: owner,
		},
	}

	if terminated {
		now := metav1.Now()
		secret.DeletionTimestamp = &now
	}
	if uid != "" {
		secret.UID = types.UID(uid)
	}

	return secret
}

// TestUpdateStatusCondition tests UpdateManifestWorkStatus function
func TestUpdateStatusCondition(t *testing.T) {
	nowish := metav1.Now()
	beforeish := metav1.Time{Time: nowish.Add(-10 * time.Second)}
	afterish := metav1.Time{Time: nowish.Add(10 * time.Second)}

	cases := []struct {
		name               string
		startingConditions []metav1.Condition
		newCondition       metav1.Condition
		expectedUpdated    bool
		expectedConditions []metav1.Condition
	}{
		{
			name:               "add to empty",
			startingConditions: []metav1.Condition{},
			newCondition:       newCondition("test", "True", "my-reason", "my-message", nil),
			expectedUpdated:    true,
			expectedConditions: []metav1.Condition{newCondition("test", "True", "my-reason", "my-message", nil)},
		},
		{
			name: "add to non-conflicting",
			startingConditions: []metav1.Condition{
				newCondition("two", "True", "my-reason", "my-message", nil),
			},
			newCondition:    newCondition("one", "True", "my-reason", "my-message", nil),
			expectedUpdated: true,
			expectedConditions: []metav1.Condition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", nil),
			},
		},
		{
			name: "change existing status",
			startingConditions: []metav1.Condition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", nil),
			},
			newCondition:    newCondition("one", "False", "my-different-reason", "my-othermessage", nil),
			expectedUpdated: true,
			expectedConditions: []metav1.Condition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "False", "my-different-reason", "my-othermessage", nil),
			},
		},
		{
			name: "leave existing transition time",
			startingConditions: []metav1.Condition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", &beforeish),
			},
			newCondition:    newCondition("one", "True", "my-reason", "my-message", &afterish),
			expectedUpdated: false,
			expectedConditions: []metav1.Condition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", &beforeish),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			manifestWork := &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "work1", Namespace: "cluster1"},
				Status: workapiv1.ManifestWorkStatus{
					Conditions: c.startingConditions,
				},
			}
			fakeWorkClient := fakeworkclient.NewSimpleClientset(manifestWork)

			status, updated, err := UpdateManifestWorkStatus(
				context.TODO(),
				fakeWorkClient.WorkV1().ManifestWorks("cluster1"),
				manifestWork,
				updateSpokeClusterConditionFn(c.newCondition),
			)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			if updated != c.expectedUpdated {
				t.Errorf("expected %t, but %t", c.expectedUpdated, updated)
			}
			for i := range c.expectedConditions {
				expected := c.expectedConditions[i]
				actual := status.Conditions[i]
				if expected.LastTransitionTime == (metav1.Time{}) {
					actual.LastTransitionTime = metav1.Time{}
				}
				if !equality.Semantic.DeepEqual(expected, actual) {
					t.Errorf(cmp.Diff(expected, actual))
				}
			}
		})
	}
}

// TestSetManifestCondition tests SetManifestCondition function
func TestMergeManifestConditions(t *testing.T) {
	transitionTime := metav1.Now()

	cases := []struct {
		name               string
		startingConditions []workapiv1.ManifestCondition
		newConditions      []workapiv1.ManifestCondition
		expectedConditions []workapiv1.ManifestCondition
	}{
		{
			name:               "add to empty",
			startingConditions: []workapiv1.ManifestCondition{},
			newConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("one", "True", "my-reason", "my-message", nil)),
			},
			expectedConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("one", "True", "my-reason", "my-message", nil)),
			},
		},
		{
			name: "add new conddtion",
			startingConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("one", "True", "my-reason", "my-message", nil)),
			},
			newConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("one", "True", "my-reason", "my-message", nil)),
				newManifestCondition(0, "resource2", newCondition("two", "True", "my-reason", "my-message", nil)),
			},
			expectedConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("one", "True", "my-reason", "my-message", nil)),
				newManifestCondition(0, "resource2", newCondition("two", "True", "my-reason", "my-message", nil)),
			},
		},
		{
			name: "update existing",
			startingConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("one", "True", "my-reason", "my-message", nil)),
			},
			newConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("one", "False", "my-reason", "my-message", nil)),
			},
			expectedConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("one", "False", "my-reason", "my-message", nil)),
			},
		},
		{
			name: "merge new",
			startingConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("one", "True", "my-reason", "my-message", nil)),
			},
			newConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("two", "False", "my-reason", "my-message", nil)),
			},
			expectedConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("one", "True", "my-reason", "my-message", nil), newCondition("two", "False", "my-reason", "my-message", nil)),
			},
		},
		{
			name: "remove useless",
			startingConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource1", newCondition("one", "True", "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource2", newCondition("two", "True", "my-reason", "my-message", &transitionTime)),
			},
			newConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource2", newCondition("two", "True", "my-reason", "my-message", nil)),
			},
			expectedConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource2", newCondition("two", "True", "my-reason", "my-message", &transitionTime)),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			merged := MergeManifestConditions(c.startingConditions, c.newConditions)

			if len(merged) != len(c.expectedConditions) {
				t.Errorf("expected condition size %d but got: %d", len(c.expectedConditions), len(merged))
			}

			for i, expectedCondition := range c.expectedConditions {
				actualCondition := merged[i]
				if len(actualCondition.Conditions) != len(expectedCondition.Conditions) {
					t.Errorf("expected condition size %d but got: %d", len(expectedCondition.Conditions), len(actualCondition.Conditions))
				}
				for j, expect := range expectedCondition.Conditions {
					if expect.LastTransitionTime == (metav1.Time{}) {
						actualCondition.Conditions[j].LastTransitionTime = metav1.Time{}
					}
				}

				if !equality.Semantic.DeepEqual(actualCondition, expectedCondition) {
					t.Errorf(cmp.Diff(actualCondition, expectedCondition))
				}
			}
		})
	}
}

func TestMergeStatusConditions(t *testing.T) {
	transitionTime := metav1.Now()

	cases := []struct {
		name               string
		startingConditions []metav1.Condition
		newConditions      []metav1.Condition
		expectedConditions []metav1.Condition
	}{
		{
			name: "add status condition",
			newConditions: []metav1.Condition{
				newCondition("one", "True", "my-reason", "my-message", nil),
			},
			expectedConditions: []metav1.Condition{
				newCondition("one", "True", "my-reason", "my-message", nil),
			},
		},
		{
			name: "merge status condition",
			startingConditions: []metav1.Condition{
				newCondition("one", "True", "my-reason", "my-message", nil),
			},
			newConditions: []metav1.Condition{
				newCondition("one", "False", "my-reason", "my-message", nil),
				newCondition("two", "True", "my-reason", "my-message", nil),
			},
			expectedConditions: []metav1.Condition{
				newCondition("one", "False", "my-reason", "my-message", nil),
				newCondition("two", "True", "my-reason", "my-message", nil),
			},
		},
		{
			name: "remove old status condition",
			startingConditions: []metav1.Condition{
				newCondition("one", "False", "my-reason", "my-message", &transitionTime),
				newCondition("two", "True", "my-reason", "my-message", nil),
			},
			newConditions: []metav1.Condition{
				newCondition("one", "False", "my-reason", "my-message", nil),
			},
			expectedConditions: []metav1.Condition{
				newCondition("one", "False", "my-reason", "my-message", &transitionTime),
				newCondition("two", "True", "my-reason", "my-message", nil),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			merged := MergeStatusConditions(c.startingConditions, c.newConditions)
			for i, expect := range c.expectedConditions {
				actual := merged[i]
				if expect.LastTransitionTime == (metav1.Time{}) {
					actual.LastTransitionTime = metav1.Time{}
				}
				if !equality.Semantic.DeepEqual(actual, expect) {
					t.Errorf(cmp.Diff(actual, expect))
				}
			}
		})
	}
}

func TestDeleteAppliedResourcess(t *testing.T) {
	cases := []struct {
		name                                 string
		existingResources                    []runtime.Object
		resourcesToRemove                    []workapiv1.AppliedManifestResourceMeta
		expectedResourcesPendingFinalization []workapiv1.AppliedManifestResourceMeta
		owner                                metav1.OwnerReference
	}{
		{
			name: "skip if resource does not exist",
			resourcesToRemove: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}},
			},
			owner: metav1.OwnerReference{Name: "n1", UID: "a"},
		},
		{
			name: "skip if resource have different uid",
			existingResources: []runtime.Object{
				newSecret("ns1", "n1", false, "ns1-n1-xxx", metav1.OwnerReference{Name: "n1", UID: "a"}),
				newSecret("ns2", "n2", true, "ns2-n2-xxx", metav1.OwnerReference{Name: "n1", UID: "a"}),
			},
			resourcesToRemove: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
			},
			owner: metav1.OwnerReference{Name: "n1", UID: "a"},
		},
		{
			name: "delete resources",
			existingResources: []runtime.Object{
				newSecret("ns1", "n1", false, "ns1-n1", metav1.OwnerReference{Name: "n1", UID: "a"}),
				newSecret("ns2", "n2", false, "ns2-n2", metav1.OwnerReference{Name: "n2", UID: "b"}),
			},
			resourcesToRemove: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
			},
			expectedResourcesPendingFinalization: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
			},
			owner: metav1.OwnerReference{Name: "n1", UID: "a"},
		},
		{
			name: "skip without uid",
			existingResources: []runtime.Object{
				newSecret("ns1", "n1", false, "ns1-n1", metav1.OwnerReference{Name: "n1", UID: "a"}),
				newSecret("ns2", "n2", true, "ns2-n2", metav1.OwnerReference{Name: "n1", UID: "a"}),
			},
			resourcesToRemove: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
			},
			expectedResourcesPendingFinalization: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
			},
			owner: metav1.OwnerReference{Name: "n1", UID: "a"},
		},
		{
			name: "skip if it is now owned",
			existingResources: []runtime.Object{
				newSecret("ns1", "n1", false, "ns1-n1", metav1.OwnerReference{Name: "n2", UID: "b"}),
			},
			resourcesToRemove: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
			},
			expectedResourcesPendingFinalization: []workapiv1.AppliedManifestResourceMeta{},
			owner:                                metav1.OwnerReference{Name: "n1", UID: "a"},
		},
		{
			name: "delete with multiple owners",
			existingResources: []runtime.Object{
				newSecret("ns1", "n1", false, "ns1-n1",
					metav1.OwnerReference{Name: "n1", UID: "a"}, metav1.OwnerReference{Name: "n2", UID: "b"}),
			},
			resourcesToRemove: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
			},
			expectedResourcesPendingFinalization: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"}},
			owner: metav1.OwnerReference{Name: "n1", UID: "a"},
		},
		{
			name: "skip with multiple applied manifest work owners",
			existingResources: []runtime.Object{
				newSecret("ns1", "n1", false, "ns1-n1",
					metav1.OwnerReference{Name: "n1", UID: "a"},
					metav1.OwnerReference{Name: "n2", UID: "b",
						APIVersion: "work.open-cluster-management.io/v1", Kind: "AppliedManifestWork"}),
			},
			resourcesToRemove: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
			},
			expectedResourcesPendingFinalization: []workapiv1.AppliedManifestResourceMeta{},
			owner:                                metav1.OwnerReference{Name: "n1", UID: "a"},
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, c.existingResources...)
			actual, err := DeleteAppliedResources(context.TODO(), c.resourcesToRemove, "testing", fakeDynamicClient, eventstesting.NewTestingEventRecorder(t), c.owner)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			if !equality.Semantic.DeepEqual(actual, c.expectedResourcesPendingFinalization) {
				t.Errorf(cmp.Diff(actual, c.expectedResourcesPendingFinalization))
			}
		})
	}
}

func TestRemoveFinalizer(t *testing.T) {
	cases := []struct {
		name               string
		obj                runtime.Object
		finalizerToRemove  string
		expectedFinalizers []string
	}{
		{
			name:               "No finalizers in object",
			obj:                &workapiv1.ManifestWork{},
			finalizerToRemove:  "a",
			expectedFinalizers: []string{},
		},
		{
			name:               "remove finalizer",
			obj:                &workapiv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{"a"}}},
			finalizerToRemove:  "a",
			expectedFinalizers: []string{},
		},
		{
			name:               "multiple finalizers",
			obj:                &workapiv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{"b", "a", "c"}}},
			finalizerToRemove:  "a",
			expectedFinalizers: []string{"b", "c"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			RemoveFinalizer(c.obj, c.finalizerToRemove)
			accessor, _ := meta.Accessor(c.obj)
			finalizers := accessor.GetFinalizers()
			if !equality.Semantic.DeepEqual(finalizers, c.expectedFinalizers) {
				t.Errorf("Expected finalizers are same, but got %v", finalizers)
			}
		})
	}
}

func TestHubHash(t *testing.T) {
	cases := []struct {
		name  string
		key1  string
		key2  string
		equal bool
	}{
		{
			name:  "same key",
			key1:  "http://localhost",
			key2:  "http://localhost",
			equal: true,
		},
		{
			name:  "same key",
			key1:  "http://localhost",
			key2:  "http://remotehost",
			equal: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hash1 := HubHash(c.key1)
			hash2 := HubHash(c.key2)

			if hash1 == hash2 && !c.equal {
				t.Errorf("Expected not equal hash value, got %s, %s", hash1, hash2)
			} else if hash1 != hash2 && c.equal {
				t.Errorf("Expected equal hash value, got %s, %s", hash1, hash2)
			}
		})
	}
}

func TestFindManifestConiguration(t *testing.T) {
	cases := []struct {
		name           string
		options        []workapiv1.ManifestConfigOption
		resourceMeta   workapiv1.ManifestResourceMeta
		expectedOption *workapiv1.ManifestConfigOption
	}{
		{
			name:           "nil options",
			options:        nil,
			resourceMeta:   workapiv1.ManifestResourceMeta{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
			expectedOption: nil,
		},
		{
			name: "options not found",
			options: []workapiv1.ManifestConfigOption{
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "nodes", Name: "node1"}},
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "test1", Namespace: "testns"}},
			},
			resourceMeta:   workapiv1.ManifestResourceMeta{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
			expectedOption: nil,
		},
		{
			name: "options found",
			options: []workapiv1.ManifestConfigOption{
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "nodes", Name: "node1"}},
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"}},
			},
			resourceMeta: workapiv1.ManifestResourceMeta{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
			expectedOption: &workapiv1.ManifestConfigOption{
				ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			option := FindManifestConiguration(c.resourceMeta, c.options)
			if !equality.Semantic.DeepEqual(option, c.expectedOption) {
				t.Errorf("expect option to be %v, but got %v", c.expectedOption, option)
			}
		})
	}
}

func TestApplyOwnerReferences(t *testing.T) {
	testCases := []struct {
		name     string
		existing []metav1.OwnerReference
		required metav1.OwnerReference

		wantPatch  bool
		wantOwners []metav1.OwnerReference
	}{
		{
			name:       "add a owner",
			required:   metav1.OwnerReference{Name: "n1", UID: "a"},
			wantPatch:  true,
			wantOwners: []metav1.OwnerReference{{Name: "n1", UID: "a"}},
		},
		{
			name:       "append a owner",
			existing:   []metav1.OwnerReference{{Name: "n2", UID: "b"}},
			required:   metav1.OwnerReference{Name: "n1", UID: "a"},
			wantPatch:  true,
			wantOwners: []metav1.OwnerReference{{Name: "n2", UID: "b"}, {Name: "n1", UID: "a"}},
		},
		{
			name:       "remove a owner",
			existing:   []metav1.OwnerReference{{Name: "n2", UID: "b"}, {Name: "n1", UID: "a"}},
			required:   metav1.OwnerReference{Name: "n1", UID: "a-"},
			wantPatch:  true,
			wantOwners: []metav1.OwnerReference{{Name: "n2", UID: "b"}},
		},
		{
			name:      "remove a non existing owner",
			existing:  []metav1.OwnerReference{{Name: "n2", UID: "b"}, {Name: "n1", UID: "a"}},
			required:  metav1.OwnerReference{Name: "n3", UID: "c-"},
			wantPatch: false,
		},
		{
			name:      "append an existing owner",
			existing:  []metav1.OwnerReference{{Name: "n2", UID: "b"}, {Name: "n1", UID: "a"}},
			required:  metav1.OwnerReference{Name: "n1", UID: "a"},
			wantPatch: false,
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			object := newSecret("ns1", "n1", false, "ns1-n1", c.existing...)
			fakeClient := fakedynamic.NewSimpleDynamicClient(scheme, object)
			gvr := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
			err := ApplyOwnerReferences(context.TODO(), fakeClient, gvr, object, c.required)
			if err != nil {
				t.Errorf("apply err: %v", err)
			}

			actions := fakeClient.Actions()
			if !c.wantPatch {
				if len(actions) > 0 {
					t.Fatalf("expect not patch but got %v", actions)
				}
				return
			}

			if len(actions) != 1 {
				t.Fatalf("expect patch action but got %v", actions)
			}

			patch := actions[0].(clienttesting.PatchAction).GetPatch()
			patchedObject := &metav1.PartialObjectMetadata{}
			err = json.Unmarshal(patch, patchedObject)
			if err != nil {
				t.Fatalf("failed to marshal patch: %v", err)
			}

			if !equality.Semantic.DeepEqual(c.wantOwners, patchedObject.GetOwnerReferences()) {
				t.Errorf("want ownerrefs %v, but got %v", c.wantOwners, patchedObject.GetOwnerReferences())
			}
		})
	}
}

func TestOwnedByTheWork(t *testing.T) {
	testGVR := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	namespace := "testns"
	name := "test"

	cases := []struct {
		name         string
		deleteOption *workapiv1.DeleteOption
		expected     bool
	}{
		{
			name:     "foreground by default",
			expected: true,
		},
		{
			name:         "orphan the resource",
			deleteOption: &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan},
			expected:     false,
		},
		{
			name:         "no orphan rule with selectively orphan",
			deleteOption: &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan},
			expected:     true,
		},
		{
			name: "orphan the resource with selectively orphan",
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
			expected: false,
		},
		{
			name: "resourcec is not matched in orphan rule with selectively orphan",
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
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			own := OwnedByTheWork(testGVR, namespace, name, c.deleteOption)

			if own != c.expected {
				t.Errorf("Expect owned by the work is %v, but got %v", c.expected, own)
			}
		})
	}
}

func TestBuildResourceMeta(t *testing.T) {
	restMapper := spoketesting.NewFakeRestMapper()

	cases := []struct {
		name         string
		index        int
		obj          runtime.Object
		expectedErr  error
		expectedGVR  schema.GroupVersionResource
		expectedMeta workapiv1.ManifestResourceMeta
	}{
		{
			name:        "object nil",
			index:       0,
			obj:         nil,
			expectedErr: nil,
			expectedGVR: schema.GroupVersionResource{},
			expectedMeta: workapiv1.ManifestResourceMeta{
				Ordinal: int32(0),
			},
		},
		{
			name:        "secret success",
			index:       1,
			obj:         spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			expectedErr: nil,
			expectedGVR: schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "secrets",
			},
			expectedMeta: workapiv1.ManifestResourceMeta{
				Ordinal:   int32(1),
				Group:     "",
				Version:   "v1",
				Kind:      "Secret",
				Resource:  "secrets",
				Namespace: "ns1",
				Name:      "test",
			},
		},
		{
			name:        "unknow object type",
			index:       1,
			obj:         spoketesting.NewUnstructured("test/v1", "NewObject", "ns1", "test"),
			expectedErr: fmt.Errorf("the server doesn't have a resource type %q", "NewObject"),
			expectedGVR: schema.GroupVersionResource{},
			expectedMeta: workapiv1.ManifestResourceMeta{
				Ordinal:   int32(1),
				Group:     "test",
				Version:   "v1",
				Kind:      "NewObject",
				Namespace: "ns1",
				Name:      "test",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			meta, gvr, err := BuildResourceMeta(c.index, c.obj, restMapper)
			if c.expectedErr == nil {
				if err != nil {
					t.Errorf("Case name: %s, expect error nil, but got %v", c.name, err)
				}
			} else if err == nil {
				t.Errorf("Case name: %s, expect error %s, but got nil", c.name, c.expectedErr)
			} else if c.expectedErr.Error() != err.Error() {
				t.Errorf("Case name: %s, expect error %s, but got %s", c.name, c.expectedErr, err)
			}

			if !reflect.DeepEqual(c.expectedGVR, gvr) {
				t.Errorf("Case name: %s, expect gvr %v, but got %v", c.name, c.expectedGVR, gvr)
			}

			if !reflect.DeepEqual(c.expectedMeta, meta) {
				t.Errorf("Case name: %s, expect meta %v, but got %v", c.name, c.expectedMeta, meta)
			}
		})
	}
}
