package utils

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"

	workv1 "open-cluster-management.io/api/work/v1"
)

var genericScheme = runtime.NewScheme()

// BuildResourceMeta builds manifest resource meta for the object
func BuildResourceMeta(
	index int,
	object runtime.Object,
	restMapper meta.RESTMapper) (workv1.ManifestResourceMeta, schema.GroupVersionResource, error) {
	resourceMeta := workv1.ManifestResourceMeta{
		Ordinal: int32(index),
	}

	if object == nil || reflect.ValueOf(object).IsNil() {
		return resourceMeta, schema.GroupVersionResource{}, nil
	}

	// set gvk
	gvk, err := GuessObjectGroupVersionKind(object)
	if err != nil {
		return resourceMeta, schema.GroupVersionResource{}, err
	}
	resourceMeta.Group = gvk.Group
	resourceMeta.Version = gvk.Version
	resourceMeta.Kind = gvk.Kind

	// set namespace/name
	if accessor, e := meta.Accessor(object); e != nil {
		err = fmt.Errorf("cannot access metadata of %v: %w", object, e)
	} else {
		resourceMeta.Namespace = accessor.GetNamespace()
		resourceMeta.Name = accessor.GetName()
	}

	// set resource
	if restMapper == nil {
		return resourceMeta, schema.GroupVersionResource{}, err
	}
	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return resourceMeta, schema.GroupVersionResource{}, fmt.Errorf("the server doesn't have a resource type %q", gvk.Kind)
	}

	resourceMeta.Resource = mapping.Resource.Resource
	return resourceMeta, mapping.Resource, err
}

// GuessObjectGroupVersionKind returns GVK for the passed runtime object.
func GuessObjectGroupVersionKind(object runtime.Object) (*schema.GroupVersionKind, error) {
	if gvk := object.GetObjectKind().GroupVersionKind(); len(gvk.Kind) > 0 {
		return &gvk, nil
	}

	if kinds, _, _ := scheme.Scheme.ObjectKinds(object); len(kinds) > 0 {
		return &kinds[0], nil
	}

	// otherwise fall back to genericScheme
	if kinds, _, _ := genericScheme.ObjectKinds(object); len(kinds) > 0 {
		return &kinds[0], nil
	}

	return nil, fmt.Errorf("cannot get gvk of %v", object)
}
