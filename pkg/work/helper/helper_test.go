package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/diff"

	workapiv1 "open-cluster-management.io/api/work/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
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
				newManifestCondition(0, "resource1",
					newCondition("one", "True", "my-reason", "my-message", nil),
					newCondition("two", "False", "my-reason", "my-message", nil)),
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

func TestFindManifestConfiguration(t *testing.T) {
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
			name: "no feedbackRules and updateStrategy",
			options: []workapiv1.ManifestConfigOption{
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "nodes", Name: "node1"}},
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "*", Namespace: "test*"}},
			},
			resourceMeta:   workapiv1.ManifestResourceMeta{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
			expectedOption: nil,
		},
		{
			name: "options found",
			options: []workapiv1.ManifestConfigOption{
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "nodes", Name: "node1"}},
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
					FeedbackRules: []workapiv1.FeedbackRule{{Type: workapiv1.WellKnownStatusType}},
				},
			},
			resourceMeta: workapiv1.ManifestResourceMeta{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
			expectedOption: &workapiv1.ManifestConfigOption{
				ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
				FeedbackRules:      []workapiv1.FeedbackRule{{Type: workapiv1.WellKnownStatusType}},
			},
		},
		{
			name: "options found include *",
			options: []workapiv1.ManifestConfigOption{
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "nodes", Name: "node1"}},
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "*", Namespace: "test*"},
					FeedbackRules:  []workapiv1.FeedbackRule{{Type: workapiv1.WellKnownStatusType}},
					UpdateStrategy: &workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeUpdate}},
			},
			resourceMeta: workapiv1.ManifestResourceMeta{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
			expectedOption: &workapiv1.ManifestConfigOption{
				ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
				FeedbackRules:      []workapiv1.FeedbackRule{{Type: workapiv1.WellKnownStatusType}},
				UpdateStrategy:     &workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeUpdate},
			},
		},
		{
			name: "multi options matched,return the first one",
			options: []workapiv1.ManifestConfigOption{
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "test", Namespace: "test*"},
					FeedbackRules: []workapiv1.FeedbackRule{{Type: workapiv1.WellKnownStatusType}}},
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "nodes", Name: "node1"}},
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
					FeedbackRules: []workapiv1.FeedbackRule{{Type: workapiv1.JSONPathsType}}},
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "*", Namespace: "testns"},
					UpdateStrategy: &workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeUpdate}},
				{ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "*", Namespace: "testns"},
					UpdateStrategy: &workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeCreateOnly}},
			},
			resourceMeta: workapiv1.ManifestResourceMeta{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},

			expectedOption: &workapiv1.ManifestConfigOption{
				ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "configmaps", Name: "test", Namespace: "testns"},
				FeedbackRules:      []workapiv1.FeedbackRule{{Type: workapiv1.WellKnownStatusType}},
				UpdateStrategy:     &workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeUpdate},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			option := FindManifestConfiguration(c.resourceMeta, c.options)
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
			obj:         testingcommon.NewUnstructured("v1", "Secret", "ns1", "test"),
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
			obj:         testingcommon.NewUnstructured("test/v1", "NewObject", "ns1", "test"),
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
			if c.expectedErr == nil { //nolint:gocritic
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

func TestFindUntrackedResources(t *testing.T) {
	cases := []struct {
		name                       string
		appliedResources           []workapiv1.AppliedManifestResourceMeta
		newAppliedResources        []workapiv1.AppliedManifestResourceMeta
		expectedUntrackedResources []workapiv1.AppliedManifestResourceMeta
	}{
		{
			name:             "no resource untracked",
			appliedResources: nil,
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
			},
			expectedUntrackedResources: nil,
		},
		{
			name: "some of original resources untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
				{Version: "v3", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g3", Resource: "r3", Namespace: "ns3", Name: "n3"}},
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
			},
		},
		{
			name: "all original resources untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v3", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g3", Resource: "r3", Namespace: "ns3", Name: "n3"}},
				{Version: "v4", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g4", Resource: "r4", Namespace: "ns4", Name: "n4"}},
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
			},
		},
		{
			name: "changing version of original resources does not make it untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
				{Version: "v4", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g4", Resource: "r4", Namespace: "ns4", Name: "n4"}},
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := FindUntrackedResources(c.appliedResources, c.newAppliedResources)
			if !reflect.DeepEqual(actual, c.expectedUntrackedResources) {
				t.Errorf(diff.ObjectDiff(actual, c.expectedUntrackedResources))
			}
		})
	}
}

func TestNameMatch(t *testing.T) {
	cases := []struct {
		name             string
		resource, target string
		expected         bool
	}{
		{
			"case 1",
			"my-test",
			"my*",
			true,
		},
		{
			"case 2",
			"my-test",
			"*my",
			false,
		},
		{
			"case 2",
			"my-test",
			"*m*",
			true,
		},
		{
			"case 3",
			"my-test",
			"*t",
			true,
		},
		{
			"case 4",
			"my-test",
			"*",
			true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rst := wildcardMatch(c.resource, c.target)
			if rst != c.expected {
				t.Errorf("expected %v, got %v", c.expected, rst)
			}
		})
	}
}
