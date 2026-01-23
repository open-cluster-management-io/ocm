package objectreader

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/util/workqueue"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func TestNewObjectReader(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = workapiv1.Install(scheme)

	fakeWorkClient := fakeworkclient.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	workInformer := workInformerFactory.Work().V1().ManifestWorks()

	fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme)

	reader, err := NewObjectReader(fakeDynamicClient, workInformer)
	if err != nil {
		t.Fatal(err)
	}

	if reader == nil {
		t.Fatal("Expected ObjectReader to be created, but got nil")
	}

	if reader.dynamicClient != fakeDynamicClient {
		t.Error("Expected dynamicClient to be set correctly")
	}

	if reader.informers == nil {
		t.Error("Expected informers map to be initialized")
	}

	if reader.indexer == nil {
		t.Error("Expected indexer to be set correctly")
	}
}

func TestGet_IncompleteResourceMeta(t *testing.T) {
	cases := []struct {
		name         string
		resourceMeta workapiv1.ManifestResourceMeta
		expectedMsg  string
	}{
		{
			name: "missing resource",
			resourceMeta: workapiv1.ManifestResourceMeta{
				Version: "v1",
				Name:    "test",
			},
			expectedMsg: "Resource meta is incomplete",
		},
		{
			name: "missing version",
			resourceMeta: workapiv1.ManifestResourceMeta{
				Resource: "secrets",
				Name:     "test",
			},
			expectedMsg: "Resource meta is incomplete",
		},
		{
			name: "missing name",
			resourceMeta: workapiv1.ManifestResourceMeta{
				Resource: "secrets",
				Version:  "v1",
			},
			expectedMsg: "Resource meta is incomplete",
		},
	}

	scheme := runtime.NewScheme()
	_ = workapiv1.Install(scheme)
	fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme)
	fakeWorkClient := fakeworkclient.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	workInformer := workInformerFactory.Work().V1().ManifestWorks()

	reader, err := NewObjectReader(fakeDynamicClient, workInformer)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj, condition, err := reader.Get(context.TODO(), c.resourceMeta)

			if obj != nil {
				t.Errorf("Expected nil object, got %v", obj)
			}

			if err == nil {
				t.Error("Expected error, got nil")
			}

			if condition.Type != workapiv1.ManifestAvailable {
				t.Errorf("Expected condition type %s, got %s", workapiv1.ManifestAvailable, condition.Type)
			}

			if condition.Status != metav1.ConditionUnknown {
				t.Errorf("Expected condition status %s, got %s", metav1.ConditionUnknown, condition.Status)
			}

			if condition.Reason != "IncompleteResourceMeta" {
				t.Errorf("Expected reason IncompleteResourceMeta, got %s", condition.Reason)
			}

			if condition.Message != c.expectedMsg {
				t.Errorf("Expected message %s, got %s", c.expectedMsg, condition.Message)
			}
		})
	}
}

func TestGet_ResourceNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = workapiv1.Install(scheme)
	fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme)
	fakeWorkClient := fakeworkclient.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	workInformer := workInformerFactory.Work().V1().ManifestWorks()

	reader, err := NewObjectReader(fakeDynamicClient, workInformer)
	if err != nil {
		t.Fatal(err)
	}

	resourceMeta := workapiv1.ManifestResourceMeta{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "default",
		Name:      "test-secret",
	}

	obj, condition, err := reader.Get(context.TODO(), resourceMeta)

	if obj != nil {
		t.Errorf("Expected nil object, got %v", obj)
	}

	if !errors.IsNotFound(err) {
		t.Errorf("Expected NotFound error, got %v", err)
	}

	if condition.Type != workapiv1.ManifestAvailable {
		t.Errorf("Expected condition type %s, got %s", workapiv1.ManifestAvailable, condition.Type)
	}

	if condition.Status != metav1.ConditionFalse {
		t.Errorf("Expected condition status %s, got %s", metav1.ConditionFalse, condition.Status)
	}

	if condition.Reason != "ResourceNotAvailable" {
		t.Errorf("Expected reason ResourceNotAvailable, got %s", condition.Reason)
	}

	if condition.Message != "Resource is not available" {
		t.Errorf("Expected message 'Resource is not available', got %s", condition.Message)
	}
}

func TestGet_ResourceFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = workapiv1.Install(scheme)

	secret := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "test-secret",
				"namespace": "default",
			},
			"data": map[string]any{
				"key": "dmFsdWU=",
			},
		},
	}

	fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, secret)
	fakeWorkClient := fakeworkclient.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	workInformer := workInformerFactory.Work().V1().ManifestWorks()

	reader, err := NewObjectReader(fakeDynamicClient, workInformer)
	if err != nil {
		t.Fatal(err)
	}

	resourceMeta := workapiv1.ManifestResourceMeta{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "default",
		Name:      "test-secret",
	}

	obj, condition, err := reader.Get(context.TODO(), resourceMeta)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if obj == nil {
		t.Fatal("Expected object to be returned, got nil")
	}

	if obj.GetName() != "test-secret" {
		t.Errorf("Expected object name test-secret, got %s", obj.GetName())
	}

	if obj.GetNamespace() != "default" {
		t.Errorf("Expected object namespace default, got %s", obj.GetNamespace())
	}

	if condition.Type != workapiv1.ManifestAvailable {
		t.Errorf("Expected condition type %s, got %s", workapiv1.ManifestAvailable, condition.Type)
	}

	if condition.Status != metav1.ConditionTrue {
		t.Errorf("Expected condition status %s, got %s", metav1.ConditionTrue, condition.Status)
	}

	if condition.Reason != "ResourceAvailable" {
		t.Errorf("Expected reason ResourceAvailable, got %s", condition.Reason)
	}

	if condition.Message != "Resource is available" {
		t.Errorf("Expected message 'Resource is available', got %s", condition.Message)
	}
}

func TestRegisterInformer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = workapiv1.Install(scheme)

	fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme)
	fakeWorkClient := fakeworkclient.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	workInformer := workInformerFactory.Work().V1().ManifestWorks()
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	reader, err := NewObjectReader(fakeDynamicClient, workInformer)
	if err != nil {
		t.Fatal(err)
	}

	resourceMeta := workapiv1.ManifestResourceMeta{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "default",
		Name:      "test-secret",
	}

	ctx := t.Context()

	// Register informer for the first time
	err = reader.RegisterInformer(ctx, "test-work", resourceMeta, queue)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify informer was created
	gvr := schema.GroupVersionResource{
		Group:    resourceMeta.Group,
		Version:  resourceMeta.Version,
		Resource: resourceMeta.Resource,
	}
	key := informerKey{GroupVersionResource: gvr, namespace: resourceMeta.Namespace}

	reader.RLock()
	informer, found := reader.informers[key]
	reader.RUnlock()

	if !found {
		t.Fatal("Expected informer to be registered")
	}

	if informer.informer == nil {
		t.Error("Expected informer to be set")
	}

	if informer.lister == nil {
		t.Error("Expected lister to be set")
	}

	if informer.cancel == nil {
		t.Error("Expected cancel function to be set")
	}

	if len(informer.registrations) != 1 {
		t.Errorf("Expected 1 registration, got %d", len(informer.registrations))
	}

	// Register the same informer again (should be idempotent)
	err = reader.RegisterInformer(ctx, "test-work", resourceMeta, queue)
	if err != nil {
		t.Fatalf("Expected no error on second registration, got %v", err)
	}

	reader.RLock()
	informer, found = reader.informers[key]
	reader.RUnlock()

	if !found {
		t.Fatal("Expected informer to still be registered")
	}

	if len(informer.registrations) != 1 {
		t.Errorf("Expected 1 registration after duplicate registration, got %d", len(informer.registrations))
	}
}

func TestRegisterInformer_MultipleResources(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = workapiv1.Install(scheme)

	fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme)
	fakeWorkClient := fakeworkclient.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	workInformer := workInformerFactory.Work().V1().ManifestWorks()
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	reader, err := NewObjectReader(fakeDynamicClient, workInformer)
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()

	// Register first resource
	resourceMeta1 := workapiv1.ManifestResourceMeta{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "default",
		Name:      "secret1",
	}

	err = reader.RegisterInformer(ctx, "test-work", resourceMeta1, queue)
	if err != nil {
		t.Fatalf("Expected no error registering first resource, got %v", err)
	}

	// Register second resource in the same namespace (should reuse informer)
	resourceMeta2 := workapiv1.ManifestResourceMeta{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "default",
		Name:      "secret2",
	}

	err = reader.RegisterInformer(ctx, "test-work", resourceMeta2, queue)
	if err != nil {
		t.Fatalf("Expected no error registering second resource, got %v", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}
	key := informerKey{GroupVersionResource: gvr, namespace: "default"}

	reader.RLock()
	informer, found := reader.informers[key]
	reader.RUnlock()

	if !found {
		t.Fatal("Expected informer to be registered")
	}

	// Should have 2 registrations for the same informer
	if len(informer.registrations) != 2 {
		t.Errorf("Expected 2 registrations, got %d", len(informer.registrations))
	}
}

func TestUnRegisterInformer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = workapiv1.Install(scheme)

	fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme)
	fakeWorkClient := fakeworkclient.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	workInformer := workInformerFactory.Work().V1().ManifestWorks()
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	reader, err := NewObjectReader(fakeDynamicClient, workInformer)
	if err != nil {
		t.Fatal(err)
	}

	resourceMeta := workapiv1.ManifestResourceMeta{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "default",
		Name:      "test-secret",
	}

	ctx := t.Context()

	// Register informer first
	err = reader.RegisterInformer(ctx, "test-work", resourceMeta, queue)
	if err != nil {
		t.Fatalf("Expected no error registering informer, got %v", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    resourceMeta.Group,
		Version:  resourceMeta.Version,
		Resource: resourceMeta.Resource,
	}
	key := informerKey{GroupVersionResource: gvr, namespace: resourceMeta.Namespace}

	// Verify informer exists
	reader.RLock()
	_, found := reader.informers[key]
	reader.RUnlock()
	if !found {
		t.Fatal("Expected informer to be registered")
	}

	// Unregister informer
	err = reader.UnRegisterInformer("test-work", resourceMeta)
	if err != nil {
		t.Fatalf("Expected no error unregistering informer, got %v", err)
	}

	// Verify informer was removed (since it was the last registration)
	reader.RLock()
	_, found = reader.informers[key]
	reader.RUnlock()
	if found {
		t.Error("Expected informer to be removed")
	}
}

func TestUnRegisterInformer_MultipleRegistrations(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = workapiv1.Install(scheme)

	fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme)
	fakeWorkClient := fakeworkclient.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	workInformer := workInformerFactory.Work().V1().ManifestWorks()
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	reader, err := NewObjectReader(fakeDynamicClient, workInformer)
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()

	// Register two resources
	resourceMeta1 := workapiv1.ManifestResourceMeta{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "default",
		Name:      "secret1",
	}

	resourceMeta2 := workapiv1.ManifestResourceMeta{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "default",
		Name:      "secret2",
	}

	err = reader.RegisterInformer(ctx, "test-work", resourceMeta1, queue)
	if err != nil {
		t.Fatalf("Expected no error registering first resource, got %v", err)
	}

	err = reader.RegisterInformer(ctx, "test-work", resourceMeta2, queue)
	if err != nil {
		t.Fatalf("Expected no error registering second resource, got %v", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}
	key := informerKey{GroupVersionResource: gvr, namespace: "default"}

	// Unregister first resource
	err = reader.UnRegisterInformer("test-work", resourceMeta1)
	if err != nil {
		t.Fatalf("Expected no error unregistering first resource, got %v", err)
	}

	// Verify informer still exists (since second resource is still registered)
	reader.RLock()
	informer, found := reader.informers[key]
	reader.RUnlock()
	if !found {
		t.Fatal("Expected informer to still be registered")
	}

	if len(informer.registrations) != 1 {
		t.Errorf("Expected 1 registration after unregistering first resource, got %d", len(informer.registrations))
	}

	// Unregister second resource
	err = reader.UnRegisterInformer("test-work", resourceMeta2)
	if err != nil {
		t.Fatalf("Expected no error unregistering second resource, got %v", err)
	}

	// Verify informer was removed (since it was the last registration)
	reader.RLock()
	_, found = reader.informers[key]
	reader.RUnlock()
	if found {
		t.Error("Expected informer to be removed after unregistering all resources")
	}
}

func TestUnRegisterInformer_NotRegistered(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = workapiv1.Install(scheme)

	fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme)
	fakeWorkClient := fakeworkclient.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	workInformer := workInformerFactory.Work().V1().ManifestWorks()

	reader, err := NewObjectReader(fakeDynamicClient, workInformer)
	if err != nil {
		t.Fatal(err)
	}

	resourceMeta := workapiv1.ManifestResourceMeta{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "default",
		Name:      "test-secret",
	}

	// Unregister without registering first (should not error)
	err = reader.UnRegisterInformer("test-work", resourceMeta)
	if err != nil {
		t.Errorf("Expected no error when unregistering non-existent informer, got %v", err)
	}
}

func TestIndexWorkByResource(t *testing.T) {
	cases := []struct {
		name         string
		obj          any
		expectedKeys []string
	}{
		{
			name:         "non-manifestwork object",
			obj:          &unstructured.Unstructured{},
			expectedKeys: []string{},
		},
		{
			name: "manifestwork with no resources",
			obj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-work",
				},
			},
			expectedKeys: nil,
		},
		{
			name: "manifestwork with single resource",
			obj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-work",
				},
				Status: workapiv1.ManifestWorkStatus{
					ResourceStatus: workapiv1.ManifestResourceStatus{
						Manifests: []workapiv1.ManifestCondition{
							{
								ResourceMeta: workapiv1.ManifestResourceMeta{
									Group:     "",
									Version:   "v1",
									Resource:  "secrets",
									Namespace: "default",
									Name:      "secret1",
								},
							},
						},
					},
				},
			},
			expectedKeys: []string{"/secrets/v1/default/secret1"},
		},
		{
			name: "manifestwork with multiple resources",
			obj: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-work",
				},
				Status: workapiv1.ManifestWorkStatus{
					ResourceStatus: workapiv1.ManifestResourceStatus{
						Manifests: []workapiv1.ManifestCondition{
							{
								ResourceMeta: workapiv1.ManifestResourceMeta{
									Group:     "",
									Version:   "v1",
									Resource:  "secrets",
									Namespace: "default",
									Name:      "secret1",
								},
							},
							{
								ResourceMeta: workapiv1.ManifestResourceMeta{
									Group:     "apps",
									Version:   "v1",
									Resource:  "deployments",
									Namespace: "kube-system",
									Name:      "deployment1",
								},
							},
						},
					},
				},
			},
			expectedKeys: []string{
				"/secrets/v1/default/secret1",
				"apps/deployments/v1/kube-system/deployment1",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			keys, err := indexWorkByResource(c.obj)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}

			if len(keys) != len(c.expectedKeys) {
				t.Fatalf("Expected %d keys, got %d", len(c.expectedKeys), len(keys))
			}

			for i, key := range keys {
				if key != c.expectedKeys[i] {
					t.Errorf("Expected key %s at index %d, got %s", c.expectedKeys[i], i, key)
				}
			}
		})
	}
}
