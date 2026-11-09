/* Copyright Contributors to the Open Cluster Management project */

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"open-cluster-management.io/ocm/poc/aggregate-api-server/internal/store"
)

type memoryObjectStore struct {
	mu       sync.Mutex
	revision int64
	objects  map[string]*unstructured.Unstructured
	events   []store.ObjectEvent
}

func newMemoryObjectStore() *memoryObjectStore {
	return &memoryObjectStore{objects: map[string]*unstructured.Unstructured{}}
}

func objectKey(key store.ResourceKey, namespace, name string) string {
	return strings.Join([]string{key.Group, key.Version, key.Resource, namespace, name}, "/")
}

func (s *memoryObjectStore) GetObject(_ context.Context, key store.ResourceKey, namespace, name string) (*unstructured.Unstructured, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj := s.objects[objectKey(key, namespace, name)]
	if obj == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: key.Group, Resource: key.Resource}, name)
	}
	return obj.DeepCopy(), nil
}

func (s *memoryObjectStore) ListObjects(_ context.Context, key store.ResourceKey, namespace string) ([]unstructured.Unstructured, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []unstructured.Unstructured{}
	for storeKey, obj := range s.objects {
		keyParts := strings.Split(storeKey, "/")
		if len(keyParts) == 5 && keyParts[0] == key.Group && keyParts[1] == key.Version && keyParts[2] == key.Resource && obj.GetAPIVersion() == key.Group+"/"+key.Version && (namespace == "" || obj.GetNamespace() == namespace) {
			items = append(items, *obj.DeepCopy())
		}
	}
	return items, strconv.FormatInt(s.revision, 10), nil
}

func (s *memoryObjectStore) CreateObject(_ context.Context, key store.ResourceKey, input *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	storeKey := objectKey(key, input.GetNamespace(), input.GetName())
	if s.objects[storeKey] != nil {
		return nil, apierrors.NewAlreadyExists(schema.GroupResource{Group: key.Group, Resource: key.Resource}, input.GetName())
	}
	s.revision++
	obj := input.DeepCopy()
	obj.SetUID("test-uid")
	obj.SetResourceVersion(strconv.FormatInt(s.revision, 10))
	obj.SetGeneration(1)
	obj.SetCreationTimestamp(metav1.Now())
	s.objects[storeKey] = obj
	s.events = append(s.events, store.ObjectEvent{ResourceVersion: obj.GetResourceVersion(), Type: "ADDED", Object: obj.DeepCopy()})
	return obj.DeepCopy(), nil
}

func (s *memoryObjectStore) UpdateObject(_ context.Context, key store.ResourceKey, input *unstructured.Unstructured, statusOnly bool) (*unstructured.Unstructured, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	storeKey := objectKey(key, input.GetNamespace(), input.GetName())
	existing := s.objects[storeKey]
	if existing == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: key.Group, Resource: key.Resource}, input.GetName())
	}
	if input.GetResourceVersion() != existing.GetResourceVersion() {
		return nil, apierrors.NewConflict(schema.GroupResource{Group: key.Group, Resource: key.Resource}, input.GetName(), fmt.Errorf("stale resourceVersion"))
	}
	previous := existing.DeepCopy()
	updated := input.DeepCopy()
	updated.SetUID(existing.GetUID())
	updated.SetCreationTimestamp(existing.GetCreationTimestamp())
	if statusOnly {
		status, found, _ := unstructured.NestedFieldCopy(input.Object, "status")
		updated.Object = existing.DeepCopy().Object
		if found {
			_ = unstructured.SetNestedField(updated.Object, status, "status")
		} else {
			unstructured.RemoveNestedField(updated.Object, "status")
		}
	}
	s.revision++
	updated.SetResourceVersion(strconv.FormatInt(s.revision, 10))
	s.objects[storeKey] = updated
	s.events = append(s.events, store.ObjectEvent{ResourceVersion: updated.GetResourceVersion(), Type: "MODIFIED", Object: updated.DeepCopy(), Previous: previous})
	return updated.DeepCopy(), nil
}

func (s *memoryObjectStore) DeleteObject(_ context.Context, key store.ResourceKey, namespace, name, expectedRV string) (*unstructured.Unstructured, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	storeKey := objectKey(key, namespace, name)
	obj := s.objects[storeKey]
	if obj == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: key.Group, Resource: key.Resource}, name)
	}
	if expectedRV != "" && expectedRV != obj.GetResourceVersion() {
		return nil, apierrors.NewConflict(schema.GroupResource{Group: key.Group, Resource: key.Resource}, name, fmt.Errorf("stale resourceVersion"))
	}
	s.revision++
	deleted := obj.DeepCopy()
	deleted.SetResourceVersion(strconv.FormatInt(s.revision, 10))
	delete(s.objects, storeKey)
	s.events = append(s.events, store.ObjectEvent{ResourceVersion: deleted.GetResourceVersion(), Type: "DELETED", Object: deleted.DeepCopy(), Previous: obj.DeepCopy()})
	return deleted, nil
}

func (s *memoryObjectStore) LatestObjectResourceVersion(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strconv.FormatInt(s.revision, 10), nil
}

func (s *memoryObjectStore) ObjectEventsAfter(_ context.Context, key store.ResourceKey, rv, namespace string) ([]store.ObjectEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, _ := strconv.ParseInt(rv, 10, 64)
	events := []store.ObjectEvent{}
	for _, event := range s.events {
		eventVersion, _ := strconv.ParseInt(event.ResourceVersion, 10, 64)
		resource, ok := resourceFor(key.Group, key.Version, key.Resource)
		if eventVersion <= version || !ok || event.Object.GetAPIVersion() != key.Group+"/"+key.Version || event.Object.GetKind() != resource.Kind || event.Object.GetNamespace() != namespace && namespace != "" {
			continue
		}
		copy := event
		copy.Object = event.Object.DeepCopy()
		if event.Previous != nil {
			copy.Previous = event.Previous.DeepCopy()
		}
		events = append(events, copy)
	}
	return events, nil
}

func TestAggregatedDiscoveryIncludesRequestedOCMAPIs(t *testing.T) {
	handler := &Handler{Store: newMemoryStore(), Objects: newMemoryObjectStore()}
	response := perform(handler, http.MethodGet, "/apis", "")
	if response.Code != http.StatusOK {
		t.Fatalf("discovery returned %d: %s", response.Code, response.Body.String())
	}
	for _, group := range []string{
		"addon.open-cluster-management.io", "cluster.open-cluster-management.io",
		"internal.open-cluster-management.io", "work.open-cluster-management.io",
	} {
		if !strings.Contains(response.Body.String(), group) {
			t.Errorf("API group %q missing from discovery", group)
		}
	}

	response = perform(handler, http.MethodGet, "/apis/internal.open-cluster-management.io/v1beta1", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "managedclusterinfos") {
		t.Fatalf("ManagedClusterInfo discovery returned %d: %s", response.Code, response.Body.String())
	}
}

func TestAPIServiceCatalogMatchesDiscoveredVersions(t *testing.T) {
	discovered := map[string]struct{}{}
	for _, resource := range resources() {
		discovered[resource.Key.Group+"/"+resource.Key.Version] = struct{}{}
	}
	configured := map[string]struct{}{}
	for _, version := range apiServiceVersions() {
		configured[version] = struct{}{}
	}
	if len(configured) != len(discovered) {
		t.Fatalf("APIService catalog versions=%v, discovered versions=%v", apiServiceVersions(), discovered)
	}
	for version := range discovered {
		if _, ok := configured[version]; !ok {
			t.Errorf("APIService catalog is missing %s", version)
		}
	}
}

func TestGenericManagedClusterInfoCRUDAndStatusIsolation(t *testing.T) {
	objects := newMemoryObjectStore()
	handler := &Handler{Store: newMemoryStore(), Objects: objects, Authorizer: allowAll{}}
	path := "/apis/internal.open-cluster-management.io/v1beta1/namespaces/hub-a/managedclusterinfos"
	body := `{"apiVersion":"internal.open-cluster-management.io/v1beta1","kind":"ManagedClusterInfo","metadata":{"name":"cluster-a"},"spec":{"masterEndpoint":"https://api.example"},"status":{"clusterID":"id-1"}}`
	response := perform(handler, http.MethodPost, path, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"namespace":"hub-a"`) || !strings.Contains(response.Body.String(), `"resourceVersion":"1"`) {
		t.Fatalf("create response missing assigned metadata: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"clusterID":"id-1"`) {
		t.Fatalf("create accepted a status-subresource write: %s", response.Body.String())
	}

	getPath := path + "/cluster-a"
	response = perform(handler, http.MethodPatch, getPath+"/status", `{"status":{"clusterID":"id-2"}}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported patch content type returned %d, want 400", response.Code)
	}
	request := httptest.NewRequest(http.MethodPatch, getPath+"/status", strings.NewReader(`{"status":{"clusterID":"id-2"}}`))
	request.Header.Set("Content-Type", "application/merge-patch+json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"clusterID":"id-2"`) || !strings.Contains(response.Body.String(), `"masterEndpoint":"https://api.example"`) {
		t.Fatalf("status patch did not isolate spec: %d %s", response.Code, response.Body.String())
	}

	response = perform(handler, http.MethodGet, "/apis/internal.open-cluster-management.io/v1beta1/namespaces/hub-a/managedclusterinfos", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"kind":"ManagedClusterInfoList"`) {
		t.Fatalf("list returned %d: %s", response.Code, response.Body.String())
	}
}

func TestGenericClusterScopedManagedClusterRejectsNamespace(t *testing.T) {
	handler := &Handler{Objects: newMemoryObjectStore(), Authorizer: allowAll{}}
	body := `{"apiVersion":"cluster.open-cluster-management.io/v1","kind":"ManagedCluster","metadata":{"name":"cluster-a","namespace":"hub-a"}}`
	response := perform(handler, http.MethodPost, "/apis/cluster.open-cluster-management.io/v1/managedclusters", body)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("cluster-scoped create returned %d, want 400: %s", response.Code, response.Body.String())
	}
}

func TestGenericListDelegatesListVerb(t *testing.T) {
	authorizer := &recordingAuthorizer{}
	handler := &Handler{Objects: newMemoryObjectStore(), Authorizer: authorizer}
	response := perform(handler, http.MethodGet, "/apis/cluster.open-cluster-management.io/v1/managedclusters", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", response.Code, response.Body.String())
	}
	if authorizer.verb != "list" || authorizer.group != "cluster.open-cluster-management.io" || authorizer.version != "v1" {
		t.Fatalf("delegated authorization = %s %s/%s, want list cluster.open-cluster-management.io/v1", authorizer.verb, authorizer.group, authorizer.version)
	}
}

func TestGenericFieldSelectorListAuthorizesNamedResource(t *testing.T) {
	authorizer := &recordingAuthorizer{}
	handler := &Handler{Objects: newMemoryObjectStore(), Authorizer: authorizer}
	response := perform(handler, http.MethodGet, "/apis/cluster.open-cluster-management.io/v1/managedclusters?fieldSelector=metadata.name%3Dlocal-cluster", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", response.Code, response.Body.String())
	}
	if authorizer.verb != "list" || authorizer.name != "local-cluster" {
		t.Fatalf("delegated authorization = %s name=%q, want list name=local-cluster", authorizer.verb, authorizer.name)
	}
}

type recordingAuthorizer struct {
	verb, group, version, name string
}

func (a *recordingAuthorizer) Authorize(_ context.Context, _ *http.Request, verb, group, version, _, _, _, name string) error {
	a.verb, a.group, a.version, a.name = verb, group, version, name
	return nil
}
