package helper

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/diff"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workapiv1 "open-cluster-management.io/api/work/v1"
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
					t.Errorf(diff.ObjectDiff(expected, actual))
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
					t.Errorf(diff.ObjectDiff(actualCondition, expectedCondition))
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
					t.Errorf(diff.ObjectDiff(actual, expect))
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
			name: "skip with multiple owners",
			existingResources: []runtime.Object{
				newSecret("ns1", "n1", false, "ns1-n1", metav1.OwnerReference{Name: "n1", UID: "a"}, metav1.OwnerReference{Name: "n2", UID: "b"}),
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
	corev1.AddToScheme(scheme)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, c.existingResources...)
			actual, err := DeleteAppliedResources(c.resourcesToRemove, "testing", fakeDynamicClient, eventstesting.NewTestingEventRecorder(t), c.owner)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			if !equality.Semantic.DeepEqual(actual, c.expectedResourcesPendingFinalization) {
				t.Errorf(diff.ObjectDiff(actual, c.expectedResourcesPendingFinalization))
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
