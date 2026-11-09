/* Copyright Contributors to the Open Cluster Management project */

package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/ocm/poc/aggregate-api-server/internal/store"
)

type memoryStore struct {
	mu       sync.Mutex
	revision int64
	objects  map[string]*workv1.ManifestWork
	events   []store.Event
}

func newMemoryStore() *memoryStore {
	return &memoryStore{objects: map[string]*workv1.ManifestWork{}}
}

func (s *memoryStore) Get(_ context.Context, namespace, name string) (*workv1.ManifestWork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj := s.objects[namespace+"/"+name]
	if obj == nil {
		return nil, errNotFound(name)
	}
	return obj.DeepCopy(), nil
}

func (s *memoryStore) List(_ context.Context, namespace string) ([]workv1.ManifestWork, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []workv1.ManifestWork{}
	for _, obj := range s.objects {
		if namespace == "" || obj.Namespace == namespace {
			items = append(items, *obj.DeepCopy())
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace == items[j].Namespace {
			return items[i].Name < items[j].Name
		}
		return items[i].Namespace < items[j].Namespace
	})
	return items, strconv.FormatInt(s.revision, 10), nil
}

func (s *memoryStore) Create(_ context.Context, input *workv1.ManifestWork) (*workv1.ManifestWork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := input.Namespace + "/" + input.Name
	if s.objects[key] != nil {
		return nil, errAlreadyExists(input.Name)
	}
	s.revision++
	obj := input.DeepCopy()
	obj.UID = types.UID(key)
	obj.ResourceVersion = strconv.FormatInt(s.revision, 10)
	obj.Generation = 1
	s.objects[key] = obj
	s.events = append(s.events, store.Event{ResourceVersion: obj.ResourceVersion, Type: "ADDED", Object: obj.DeepCopy()})
	return obj.DeepCopy(), nil
}

func (s *memoryStore) Update(_ context.Context, input *workv1.ManifestWork, statusOnly bool) (*workv1.ManifestWork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := input.Namespace + "/" + input.Name
	old := s.objects[key]
	if old == nil {
		return nil, errNotFound(input.Name)
	}
	if input.ResourceVersion != old.ResourceVersion {
		return nil, errConflict(input.Name)
	}
	previous := old.DeepCopy()
	updated := input.DeepCopy()
	if statusOnly {
		updated.Spec = old.Spec
		updated.ObjectMeta = *old.ObjectMeta.DeepCopy()
	} else {
		updated.Status = old.Status
	}
	s.revision++
	updated.ResourceVersion = strconv.FormatInt(s.revision, 10)
	s.objects[key] = updated
	s.events = append(s.events, store.Event{ResourceVersion: updated.ResourceVersion, Type: "MODIFIED", Object: updated.DeepCopy(), Previous: previous})
	return updated.DeepCopy(), nil
}

func (s *memoryStore) Delete(_ context.Context, namespace, name, expectedRV string) (*workv1.ManifestWork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := namespace + "/" + name
	old := s.objects[key]
	if old == nil {
		return nil, errNotFound(name)
	}
	if expectedRV != "" && expectedRV != old.ResourceVersion {
		return nil, errConflict(name)
	}
	s.revision++
	deleted := old.DeepCopy()
	deleted.ResourceVersion = strconv.FormatInt(s.revision, 10)
	delete(s.objects, key)
	s.events = append(s.events, store.Event{ResourceVersion: deleted.ResourceVersion, Type: "DELETED", Object: deleted, Previous: old.DeepCopy()})
	return deleted, nil
}

func (s *memoryStore) LatestResourceVersion(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strconv.FormatInt(s.revision, 10), nil
}

func (s *memoryStore) EventsAfter(_ context.Context, rv, namespace string) ([]store.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	after, _ := strconv.ParseInt(rv, 10, 64)
	result := []store.Event{}
	for _, event := range s.events {
		version, _ := strconv.ParseInt(event.ResourceVersion, 10, 64)
		if version > after && (namespace == "" || event.Object.Namespace == namespace) {
			result = append(result, event)
		}
	}
	return result, nil
}

type allowAll struct{}

func (allowAll) Authorize(context.Context, *http.Request, string, string, string, string, string, string, string) error {
	return nil
}

func TestHandlerCreateListGetAndStatusUpdate(t *testing.T) {
	storage := newMemoryStore()
	handler := &Handler{Store: storage, Authorizer: allowAll{}}

	created := perform(handler, http.MethodPost, "/apis/"+groupName+"/"+version+"/namespaces/hub-a/manifestworks", `{"apiVersion":"work.open-cluster-management.io/v1","kind":"ManifestWork","metadata":{"name":"work-a","labels":{"team":"one"}},"spec":{"workload":{"manifests":[{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cfg"}}]}}}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", created.Code, created.Body.String())
	}
	obj := &workv1.ManifestWork{}
	if err := json.Unmarshal(created.Body.Bytes(), obj); err != nil {
		t.Fatal(err)
	}
	if obj.Namespace != "hub-a" || obj.Name != "work-a" || obj.ResourceVersion == "" {
		t.Fatalf("created object has unexpected metadata: %#v", obj.ObjectMeta)
	}

	listed := perform(handler, http.MethodGet, "/apis/"+groupName+"/"+version+"/namespaces/hub-a/manifestworks?labelSelector=team%3Done", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", listed.Code, listed.Body.String())
	}
	list := &workv1.ManifestWorkList{}
	if err := json.Unmarshal(listed.Body.Bytes(), list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.ListMeta.ResourceVersion != obj.ResourceVersion {
		t.Fatalf("list returned unexpected items or resourceVersion: %#v", list)
	}

	got := perform(handler, http.MethodGet, "/apis/"+groupName+"/"+version+"/namespaces/hub-a/manifestworks/work-a", "")
	if got.Code != http.StatusOK {
		t.Fatalf("get returned %d: %s", got.Code, got.Body.String())
	}

	statusBody, err := json.Marshal(&workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{Name: obj.Name, Namespace: obj.Namespace, ResourceVersion: obj.ResourceVersion},
		Status: workv1.ManifestWorkStatus{Conditions: []metav1.Condition{{
			Type: "Applied", Status: metav1.ConditionTrue, Reason: "Applied", Message: "work applied", LastTransitionTime: metav1.Now(),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	status := perform(handler, http.MethodPut, "/apis/"+groupName+"/"+version+"/namespaces/hub-a/manifestworks/work-a/status", string(statusBody))
	if status.Code != http.StatusOK {
		t.Fatalf("status update returned %d: %s", status.Code, status.Body.String())
	}
	readAfterStatus := perform(handler, http.MethodGet, "/apis/"+groupName+"/"+version+"/namespaces/hub-a/manifestworks/work-a", "")
	stored := &workv1.ManifestWork{}
	if err := json.Unmarshal(readAfterStatus.Body.Bytes(), stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Spec.Workload.Manifests) != 1 {
		t.Fatal("status update unexpectedly changed spec.workload.manifests")
	}
	if len(stored.Status.Conditions) != 1 || stored.Status.Conditions[0].Reason != "Applied" {
		t.Fatalf("status update did not persist the condition: %#v", stored.Status.Conditions)
	}
}

func TestHandlerValidatesEmbeddedManifestShape(t *testing.T) {
	handler := &Handler{Store: newMemoryStore(), Authorizer: allowAll{}}
	obj := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{Name: "invalid-work"},
		Spec: workv1.ManifestWorkSpec{Workload: workv1.ManifestsTemplate{
			Manifests: []workv1.Manifest{{RawExtension: runtime.RawExtension{Raw: []byte(`{"kind":"ConfigMap"}`)}}},
		}},
	}
	body, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	response := perform(handler, http.MethodPost, "/apis/"+groupName+"/"+version+"/namespaces/hub-a/manifestworks", string(body))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid embedded manifest returned %d, want %d: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
}

func TestHandlerRejectsStaleResourceVersion(t *testing.T) {
	storage := newMemoryStore()
	obj, err := storage.Create(context.Background(), &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "work-a", Namespace: "hub-a"}})
	if err != nil {
		t.Fatal(err)
	}
	obj.ResourceVersion = "stale"
	body, _ := json.Marshal(obj)
	handler := &Handler{Store: storage, Authorizer: allowAll{}}
	response := perform(handler, http.MethodPut, "/apis/"+groupName+"/"+version+"/namespaces/hub-a/manifestworks/work-a", string(body))
	if response.Code != http.StatusConflict {
		t.Fatalf("stale update returned %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}
}

func TestManifestWorkClusterWideList(t *testing.T) {
	storage := newMemoryStore()
	handler := &Handler{Store: storage, Authorizer: allowAll{}}
	if _, err := storage.Create(context.Background(), &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "work-a", Namespace: "ns-a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Create(context.Background(), &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "work-b", Namespace: "ns-b"}}); err != nil {
		t.Fatal(err)
	}
	response := perform(handler, http.MethodGet, "/apis/"+groupName+"/"+version+"/"+resource, "")
	if response.Code != http.StatusOK {
		t.Fatalf("cluster-wide list returned %d: %s", response.Code, response.Body.String())
	}
	list := &workv1.ManifestWorkList{}
	if err := json.Unmarshal(response.Body.Bytes(), list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("cluster-wide list returned %d items, want 2", len(list.Items))
	}
}

func TestWatchListSendsInitialEventsAndBookmark(t *testing.T) {
	storage := newMemoryStore()
	handler := &Handler{Store: storage, Authorizer: allowAll{}, PollPeriod: 10 * time.Millisecond}
	if _, err := storage.Create(context.Background(), &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "watch-work", Namespace: "hub-a", Labels: map[string]string{"team": "one"}}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet,
		server.URL+"/apis/"+groupName+"/"+version+"/namespaces/hub-a/"+resource+"?watch=true&sendInitialEvents=true&resourceVersionMatch=NotOlderThan&allowWatchBookmarks=true&labelSelector=team%3Done", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("watch-list returned %d", response.StatusCode)
	}

	events := []metav1.WatchEvent{}
	decoder := json.NewDecoder(response.Body)
	deadline := time.Now().Add(5 * time.Second)
	for len(events) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %d events: %#v", len(events), events)
		}
		var event metav1.WatchEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatalf("decode watch event: %v", err)
		}
		events = append(events, event)
	}
	if events[0].Type != "ADDED" || !strings.Contains(string(events[0].Object.Raw), `"name":"watch-work"`) {
		t.Fatalf("first initial event = %s %s, want ADDED watch-work", events[0].Type, events[0].Object.Raw)
	}
	if events[1].Type != "BOOKMARK" || !strings.Contains(string(events[1].Object.Raw), `"k8s.io/initial-events-end":"true"`) {
		t.Fatalf("second initial event = %s %s, want initial-events-end BOOKMARK", events[1].Type, events[1].Object.Raw)
	}
}

func TestHandlerDiscovery(t *testing.T) {
	handler := &Handler{Store: newMemoryStore(), Authorizer: allowAll{}}
	response := perform(handler, http.MethodGet, "/apis/"+groupName+"/"+version, "")
	if response.Code != http.StatusOK {
		t.Fatalf("discovery returned %d: %s", response.Code, response.Body.String())
	}
	resources := &metav1.APIResourceList{}
	if err := json.Unmarshal(response.Body.Bytes(), resources); err != nil {
		t.Fatal(err)
	}
	if resources.GroupVersion != groupName+"/"+version || len(resources.APIResources) != 2 {
		t.Fatalf("unexpected resource discovery response: %#v", resources)
	}
}

func TestGeneratedWorkClientCRUDAndWatch(t *testing.T) {
	storage := newMemoryStore()
	httpServer := httptest.NewServer(&Handler{Store: storage, Authorizer: allowAll{}})
	defer httpServer.Close()
	client, err := workclientset.NewForConfig(&rest.Config{Host: httpServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	works := client.WorkV1().ManifestWorks("hub-a")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	created, err := works.Create(ctx, &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "typed-client-work", Labels: map[string]string{"team": "one"}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("generated client create: %v", err)
	}
	if created.ResourceVersion == "" {
		t.Fatal("generated client received an object without resourceVersion")
	}
	if _, err := works.Get(ctx, created.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("generated client get: %v", err)
	}
	list, err := works.List(ctx, metav1.ListOptions{})
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("generated client list returned %d items, err=%v", len(list.Items), err)
	}

	watcher, err := works.Watch(ctx, metav1.ListOptions{ResourceVersion: created.ResourceVersion, LabelSelector: "team=one"})
	if err != nil {
		t.Fatalf("generated client watch: %v", err)
	}
	defer watcher.Stop()
	updated := created.DeepCopy()
	updated.Labels = map[string]string{"team": "two"}
	if _, err := works.Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("generated client update: %v", err)
	}
	select {
	case event := <-watcher.ResultChan():
		if event.Type != "DELETED" {
			t.Fatalf("watch event type = %q, want DELETED when selector stops matching", event.Type)
		}
		if _, ok := event.Object.(*workv1.ManifestWork); !ok {
			t.Fatalf("watch returned object type %T, want *ManifestWork", event.Object)
		}
		if got := event.Object.(*workv1.ManifestWork).Labels["team"]; got != "one" {
			t.Fatalf("selector-removal event object label team = %q, want previous value one", got)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for generated client watch event")
	}
}

func TestProxyClientCertificateRequired(t *testing.T) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "poc-test-proxy-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	issue := func(commonName string, serial int64, template x509.Certificate) (tls.Certificate, error) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return tls.Certificate{}, err
		}
		template.SerialNumber = big.NewInt(serial)
		template.Subject = pkix.Name{CommonName: commonName}
		template.NotBefore = time.Now().Add(-time.Hour)
		template.NotAfter = time.Now().Add(time.Hour)
		template.KeyUsage = x509.KeyUsageDigitalSignature
		der, err := x509.CreateCertificate(rand.Reader, &template, caCertificate, &key.PublicKey, caKey)
		if err != nil {
			return tls.Certificate{}, err
		}
		return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
	}
	serverCertificate, err := issue("poc-test-server", 2, x509.Certificate{
		DNSNames:    []string{"127.0.0.1"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	if err != nil {
		t.Fatal(err)
	}
	proxyCertificate, err := issue("front-proxy-client", 3, x509.Certificate{
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		t.Fatal(err)
	}

	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(caCertificate)
	server := httptest.NewUnstartedServer(&Handler{Store: newMemoryStore(), Authorizer: allowAll{}, RequireProxyClientCert: true})
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCertificate},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    clientCAs,
	}
	server.StartTLS()
	defer server.Close()

	plainClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool()}}}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	plainClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	proxyClient := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, Certificates: []tls.Certificate{proxyCertificate}},
	}}

	// Without a client certificate, probes and discovery stay reachable...
	for _, path := range []string{"/readyz", "/apis/" + groupName + "/" + version} {
		response, err := plainClient.Get(server.URL + path)
		if err != nil {
			t.Fatalf("cert-less GET %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("cert-less GET %s returned %d, want 200", path, response.StatusCode)
		}
	}
	// ...but resource requests are rejected.
	response, err := plainClient.Get(server.URL + "/apis/" + groupName + "/" + version + "/namespaces/hub-a/manifestworks")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cert-less list returned %d, want 401", response.StatusCode)
	}
	// With a verified proxy client certificate, resource requests proceed.
	response, err = proxyClient.Get(server.URL + "/apis/" + groupName + "/" + version + "/namespaces/hub-a/manifestworks")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("proxied list returned %d, want 200", response.StatusCode)
	}
}

func perform(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func errNotFound(name string) error { return apierrors.NewNotFound(resourceName, name) }

func errAlreadyExists(name string) error { return apierrors.NewAlreadyExists(resourceName, name) }

func errConflict(name string) error {
	return apierrors.NewConflict(resourceName, name, fmt.Errorf("stale resourceVersion"))
}
