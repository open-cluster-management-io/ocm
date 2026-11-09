/* Copyright Contributors to the Open Cluster Management project */

package store

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	workv1 "open-cluster-management.io/api/work/v1"
)

type ResourceKey struct {
	Group    string
	Version  string
	Resource string
}

type ObjectEvent struct {
	ResourceVersion string
	Type            string
	Object          *unstructured.Unstructured
	Previous        *unstructured.Unstructured
}

type ObjectStore interface {
	GetObject(context.Context, ResourceKey, string, string) (*unstructured.Unstructured, error)
	ListObjects(context.Context, ResourceKey, string) ([]unstructured.Unstructured, string, error)
	CreateObject(context.Context, ResourceKey, *unstructured.Unstructured) (*unstructured.Unstructured, error)
	UpdateObject(context.Context, ResourceKey, *unstructured.Unstructured, bool) (*unstructured.Unstructured, error)
	DeleteObject(context.Context, ResourceKey, string, string, string) (*unstructured.Unstructured, error)
	LatestObjectResourceVersion(context.Context) (string, error)
	ObjectEventsAfter(context.Context, ResourceKey, string, string) ([]ObjectEvent, error)
}

type Event struct {
	ResourceVersion string
	Type            string
	Object          *workv1.ManifestWork
	Previous        *workv1.ManifestWork
}

type Store interface {
	Get(context.Context, string, string) (*workv1.ManifestWork, error)
	List(context.Context, string) ([]workv1.ManifestWork, string, error)
	Create(context.Context, *workv1.ManifestWork) (*workv1.ManifestWork, error)
	Update(context.Context, *workv1.ManifestWork, bool) (*workv1.ManifestWork, error)
	Delete(context.Context, string, string, string) (*workv1.ManifestWork, error)
	LatestResourceVersion(context.Context) (string, error)
	EventsAfter(context.Context, string, string) ([]Event, error)
}
