/* Copyright Contributors to the Open Cluster Management project */

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	workv1 "open-cluster-management.io/api/work/v1"
)

func TestPostgresManifestWorkLifecycle(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set POSTGRES_TEST_DSN to run the PostgreSQL integration test")
	}
	ctx := context.Background()
	s, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	namespace := "poc-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	name := "manifestwork-lifecycle"

	created, err := s.Create(ctx, &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: namespace, Finalizers: []string{"poc.example/finalizer"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = s.Delete(ctx, namespace, name, "") }()
	if created.ResourceVersion == "" || created.UID == "" {
		t.Fatalf("create did not assign Kubernetes metadata: %#v", created.ObjectMeta)
	}

	items, listRV, err := s.List(ctx, namespace)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ResourceVersion != created.ResourceVersion {
		t.Fatalf("list did not return a consistent snapshot: items=%#v listRV=%q", items, listRV)
	}
	listVersion, listErr := strconv.ParseInt(listRV, 10, 64)
	createdVersion, createdErr := strconv.ParseInt(created.ResourceVersion, 10, 64)
	if listErr != nil || createdErr != nil || listVersion < createdVersion {
		t.Fatalf("list resourceVersion %q is older than listed object %q", listRV, created.ResourceVersion)
	}

	stale := created.DeepCopy()
	stale.ResourceVersion = "stale"
	if _, err := s.Update(ctx, stale, false); !apierrors.IsConflict(err) {
		t.Fatalf("stale update error = %v, want conflict", err)
	}

	updated, err := s.Update(ctx, created, true)
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.EventsAfter(ctx, created.ResourceVersion, namespace)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "MODIFIED" || events[0].ResourceVersion != updated.ResourceVersion {
		t.Fatalf("unexpected persisted watch events: %#v", events)
	}

	deleting, err := s.Delete(ctx, namespace, name, updated.ResourceVersion)
	if err != nil {
		t.Fatal(err)
	}
	if deleting.DeletionTimestamp.IsZero() {
		t.Fatal("delete with a finalizer did not set deletionTimestamp")
	}
	deleting.Finalizers = nil
	deleted, err := s.Update(ctx, deleting, false)
	if err != nil {
		t.Fatal(err)
	}
	events, err = s.EventsAfter(ctx, deleting.ResourceVersion, namespace)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "DELETED" || events[0].ResourceVersion != deleted.ResourceVersion {
		t.Fatalf("finalizer removal did not persist a delete event: %#v", events)
	}
	items, _, err = s.List(ctx, namespace)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("delete left %d ManifestWorks", len(items))
	}

	key := ResourceKey{Group: "internal.open-cluster-management.io", Version: "v1beta1", Resource: "managedclusterinfos"}
	info := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": key.Group + "/" + key.Version,
		"kind":       "ManagedClusterInfo",
		"metadata":   map[string]any{"name": "managedclusterinfo-lifecycle", "namespace": namespace},
		"spec":       map[string]any{"masterEndpoint": "https://api.example.invalid"},
		"status":     map[string]any{"clusterID": "cluster-1"},
	}}
	createdInfo, err := s.CreateObject(ctx, key, info)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = s.DeleteObject(ctx, key, namespace, info.GetName(), "") }()
	if createdInfo.GetUID() == "" || createdInfo.GetResourceVersion() == "" {
		t.Fatalf("generic create did not assign metadata: %#v", createdInfo.Object["metadata"])
	}
	updatedInfo := createdInfo.DeepCopy()
	_ = unstructured.SetNestedField(updatedInfo.Object, "cluster-2", "status", "clusterID")
	updatedInfo, err = s.UpdateObject(ctx, key, updatedInfo, true)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint, _, _ := unstructured.NestedString(updatedInfo.Object, "spec", "masterEndpoint"); endpoint != "https://api.example.invalid" {
		t.Fatalf("status update modified spec.masterEndpoint: %q", endpoint)
	}
	if clusterID, _, _ := unstructured.NestedString(updatedInfo.Object, "status", "clusterID"); clusterID != "cluster-2" {
		t.Fatalf("status update did not persist clusterID: %q", clusterID)
	}
	objectEvents, err := s.ObjectEventsAfter(ctx, key, createdInfo.GetResourceVersion(), namespace)
	if err != nil || len(objectEvents) != 1 || objectEvents[0].Type != "MODIFIED" {
		t.Fatalf("unexpected generic watch events: events=%#v err=%v", objectEvents, err)
	}
	if _, err := s.DeleteObject(ctx, key, namespace, info.GetName(), updatedInfo.GetResourceVersion()); err != nil {
		t.Fatal(err)
	}
	genericItems, _, err := s.ListObjects(ctx, key, namespace)
	if err != nil || len(genericItems) != 0 {
		t.Fatalf("generic delete left objects=%d err=%v", len(genericItems), err)
	}
}
