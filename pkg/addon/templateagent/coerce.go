package templateagent

import (
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// coerceKnownIntFields converts string values that should be integers in
// well-known unstructured fields.
//
// AODC customized variables are always strings, so a template like
//
//	replicas: "{{replicaCount}}"
//
// renders as JSON "replicas": "5". This helper coerces those known fields to
// integers before decorators / ManifestWork apply.
//
// Extend this function (and add focused helpers) as more integer fields are
// supported — keep an explicit allow-list rather than promoting every
// numeric-looking string (ConfigMap/Secret/env data must stay strings).
func coerceKnownIntFields(obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	if gvk.Group == "apps" && gvk.Version == "v1" &&
		(gvk.Kind == "Deployment" || gvk.Kind == "StatefulSet") {
		return coerceSpecReplicas(obj)
	}
	return nil
}

// coerceSpecReplicas ensures spec.replicas is stored as int64 rather than string.
func coerceSpecReplicas(obj *unstructured.Unstructured) error {
	val, found, err := unstructured.NestedFieldNoCopy(obj.Object, "spec", "replicas")
	if err != nil || !found {
		return err
	}
	s, ok := val.(string)
	if !ok {
		// already numeric (e.g. float64 from JSON decode of an unquoted default)
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return unstructured.SetNestedField(obj.Object, n, "spec", "replicas")
}
