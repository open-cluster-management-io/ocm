/* Copyright Contributors to the Open Cluster Management project */

package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func resourcePathResource(path []string, _ *http.Request) string {
	if len(path) >= 3 && path[0] == "namespaces" {
		return path[2]
	}
	if len(path) == 0 {
		return ""
	}
	return path[0]
}

func (h *Handler) handleGenericResource(w http.ResponseWriter, r *http.Request, resource Resource, path []string) error {
	if h.Objects == nil {
		return apierrors.NewServiceUnavailable("generic object storage is not configured")
	}
	namespace, name, subresource, err := parseResourcePath(resource, path)
	if err != nil {
		return err
	}
	if name != "" && !validName(name) {
		return apierrors.NewBadRequest("invalid resource name")
	}
	verb := requestVerb(r)
	if name == "" && r.Method == http.MethodGet && !isWatch(r) {
		verb = "list"
	}
	authName := name
	if authName == "" {
		authName = authorizationName(r)
	}
	if h.Authorizer != nil {
		if err := h.Authorizer.Authorize(r.Context(), r, verb, resource.Key.Group, resource.Key.Version, namespace, resource.Key.Resource, subresource, authName); err != nil {
			return err
		}
	}
	if r.Method == http.MethodGet && name == "" && isWatch(r) {
		return h.watchObjects(w, r, resource, namespace)
	}
	switch {
	case r.Method == http.MethodGet && name == "":
		return h.listObjects(w, r, resource, namespace)
	case r.Method == http.MethodGet && name != "":
		obj, err := h.Objects.GetObject(r.Context(), resource.Key, namespace, name)
		if err != nil {
			return err
		}
		h.writeJSON(w, http.StatusOK, obj.Object)
		return nil
	case r.Method == http.MethodPost && name == "":
		return h.createObject(w, r, resource, namespace)
	case r.Method == http.MethodPut && name != "":
		return h.updateObject(w, r, resource, namespace, name, subresource == "status")
	case r.Method == http.MethodPatch && name != "":
		return h.patchObject(w, r, resource, namespace, name, subresource == "status")
	case r.Method == http.MethodDelete && name != "" && subresource == "":
		return h.deleteObject(w, r, resource, namespace, name)
	default:
		return apierrors.NewMethodNotSupported(schema.GroupResource{Group: resource.Key.Group, Resource: resource.Key.Resource}, r.Method)
	}
}

func parseResourcePath(resource Resource, path []string) (namespace, name, subresource string, err error) {
	if resource.Namespaced {
		if len(path) == 1 && path[0] == resource.Key.Resource {
			return "", "", "", nil
		}
		if len(path) < 3 || path[0] != "namespaces" || path[1] == "" || path[2] != resource.Key.Resource || len(path) > 5 {
			return "", "", "", apierrors.NewNotFound(schema.GroupResource{Group: resource.Key.Group, Resource: resource.Key.Resource}, strings.Join(path, "/"))
		}
		namespace = path[1]
		if len(path) >= 4 {
			name = path[3]
		}
		if len(path) == 5 {
			subresource = path[4]
		}
	} else {
		if len(path) < 1 || path[0] != resource.Key.Resource || len(path) > 3 {
			return "", "", "", apierrors.NewNotFound(schema.GroupResource{Group: resource.Key.Group, Resource: resource.Key.Resource}, strings.Join(path, "/"))
		}
		if len(path) >= 2 {
			name = path[1]
		}
		if len(path) == 3 {
			subresource = path[2]
		}
	}
	if (len(path) == 2 && resource.Namespaced && path[1] != resource.Key.Resource) ||
		(name == "" && subresource != "") || (subresource != "" && (!resource.HasStatus || subresource != "status")) {
		return "", "", "", apierrors.NewNotFound(schema.GroupResource{Group: resource.Key.Group, Resource: resource.Key.Resource}, strings.Join(path, "/"))
	}
	return namespace, name, subresource, nil
}

func (h *Handler) listObjects(w http.ResponseWriter, r *http.Request, resource Resource, namespace string) error {
	items, rv, err := h.Objects.ListObjects(r.Context(), resource.Key, namespace)
	if err != nil {
		return err
	}
	labelSelector, err := labels.Parse(r.URL.Query().Get("labelSelector"))
	if err != nil {
		return apierrors.NewBadRequest("invalid labelSelector: " + err.Error())
	}
	fieldSelector, err := fields.ParseSelector(r.URL.Query().Get("fieldSelector"))
	if err != nil {
		return apierrors.NewBadRequest("invalid fieldSelector: " + err.Error())
	}
	filtered := make([]unstructured.Unstructured, 0, len(items))
	for _, item := range items {
		if !labelSelector.Matches(labels.Set(item.GetLabels())) || !fieldSelector.Matches(fields.Set{
			"metadata.name": item.GetName(), "metadata.namespace": item.GetNamespace(),
		}) {
			continue
		}
		filtered = append(filtered, item)
	}
	if token := r.URL.Query().Get("continue"); token != "" {
		marker, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			return apierrors.NewBadRequest("invalid continue token")
		}
		filtered = afterObjectMarker(filtered, string(marker))
	}
	continueToken := ""
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 0 {
			return apierrors.NewBadRequest("limit must be a non-negative integer")
		}
		if limit > 0 && len(filtered) > limit {
			last := filtered[limit-1]
			continueToken = base64.RawURLEncoding.EncodeToString([]byte(last.GetNamespace() + "\x00" + last.GetName()))
			filtered = filtered[:limit]
		}
	}
	objects := make([]any, 0, len(filtered))
	for i := range filtered {
		objects = append(objects, filtered[i].Object)
	}
	list := map[string]any{
		"apiVersion": resource.Key.Group + "/" + resource.Key.Version,
		"kind":       resource.Kind + "List",
		"metadata": map[string]any{
			"resourceVersion": rv,
			"continue":        continueToken,
		},
		"items": objects,
	}
	h.writeJSON(w, http.StatusOK, list)
	return nil
}

func (h *Handler) createObject(w http.ResponseWriter, r *http.Request, resource Resource, namespace string) error {
	obj, err := decodeUnstructured(r)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	if obj.GetName() == "" {
		return apierrors.NewBadRequest("metadata.name is required; generateName is not supported by this POC")
	}
	if problems := validation.IsDNS1123Subdomain(obj.GetName()); len(problems) != 0 {
		return invalidResource(resource, obj.GetName(), "metadata.name is invalid: "+strings.Join(problems, "; "))
	}
	if resource.Namespaced {
		if namespace != "" && obj.GetNamespace() != "" && obj.GetNamespace() != namespace {
			return apierrors.NewBadRequest("metadata.namespace does not match request namespace")
		}
		if namespace != "" {
			obj.SetNamespace(namespace)
		}
		if obj.GetNamespace() == "" {
			return apierrors.NewBadRequest("metadata.namespace is required")
		}
	} else if obj.GetNamespace() != "" {
		return apierrors.NewBadRequest("cluster-scoped resources must not set metadata.namespace")
	}
	obj.SetAPIVersion(resource.Key.Group + "/" + resource.Key.Version)
	obj.SetKind(resource.Kind)
	if resource.HasStatus {
		unstructured.RemoveNestedField(obj.Object, "status")
	}
	created, err := h.Objects.CreateObject(r.Context(), resource.Key, obj)
	if err != nil {
		return err
	}
	h.writeJSON(w, http.StatusCreated, created.Object)
	return nil
}

func (h *Handler) updateObject(w http.ResponseWriter, r *http.Request, resource Resource, namespace, name string, statusOnly bool) error {
	obj, err := decodeUnstructured(r)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	if obj.GetName() != "" && obj.GetName() != name || obj.GetNamespace() != "" && obj.GetNamespace() != namespace {
		return apierrors.NewBadRequest("metadata.name and metadata.namespace must match the request path")
	}
	obj.SetName(name)
	obj.SetNamespace(namespace)
	obj.SetAPIVersion(resource.Key.Group + "/" + resource.Key.Version)
	obj.SetKind(resource.Kind)
	updated, err := h.Objects.UpdateObject(r.Context(), resource.Key, obj, statusOnly)
	if err != nil {
		return err
	}
	h.writeJSON(w, http.StatusOK, updated.Object)
	return nil
}

func (h *Handler) patchObject(w http.ResponseWriter, r *http.Request, resource Resource, namespace, name string, statusOnly bool) error {
	patch, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		return apierrors.NewBadRequest("read patch: " + err.Error())
	}
	if contentType := strings.Split(r.Header.Get("Content-Type"), ";")[0]; contentType != "application/merge-patch+json" {
		return apierrors.NewBadRequest("this POC supports application/merge-patch+json only")
	}
	existing, err := h.Objects.GetObject(r.Context(), resource.Key, namespace, name)
	if err != nil {
		return err
	}
	current, err := json.Marshal(existing.Object)
	if err != nil {
		return fmt.Errorf("encode %s: %w", resource.Key.Resource, err)
	}
	patched, err := jsonpatch.MergePatch(current, patch)
	if err != nil {
		return apierrors.NewBadRequest("invalid merge patch: " + err.Error())
	}
	obj := &unstructured.Unstructured{Object: map[string]any{}}
	if err := json.Unmarshal(patched, &obj.Object); err != nil {
		return apierrors.NewBadRequest("decode patched object: " + err.Error())
	}
	obj.SetName(name)
	obj.SetNamespace(namespace)
	obj.SetAPIVersion(resource.Key.Group + "/" + resource.Key.Version)
	obj.SetKind(resource.Kind)
	updated, err := h.Objects.UpdateObject(r.Context(), resource.Key, obj, statusOnly)
	if err != nil {
		return err
	}
	h.writeJSON(w, http.StatusOK, updated.Object)
	return nil
}

func (h *Handler) deleteObject(w http.ResponseWriter, r *http.Request, resource Resource, namespace, name string) error {
	options := &metav1.DeleteOptions{}
	if r.ContentLength != 0 {
		if err := decodeBody(r, options); err != nil {
			return apierrors.NewBadRequest(err.Error())
		}
	}
	expectedRV := ""
	if options.Preconditions != nil && options.Preconditions.ResourceVersion != nil {
		expectedRV = *options.Preconditions.ResourceVersion
	}
	if _, err := h.Objects.DeleteObject(r.Context(), resource.Key, namespace, name, expectedRV); err != nil {
		return err
	}
	h.writeJSON(w, http.StatusOK, &metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusSuccess, Code: http.StatusOK,
		Details: &metav1.StatusDetails{Name: name, Group: resource.Key.Group, Kind: resource.Kind},
	})
	return nil
}

func (h *Handler) watchObjects(w http.ResponseWriter, r *http.Request, resource Resource, namespace string) error {
	rv := r.URL.Query().Get("resourceVersion")
	if rv == "" {
		var err error
		rv, err = h.Objects.LatestObjectResourceVersion(r.Context())
		if err != nil {
			return err
		}
	}
	if _, err := strconv.ParseInt(rv, 10, 64); err != nil {
		return apierrors.NewBadRequest("resourceVersion must be an integer")
	}
	selector, err := labels.Parse(r.URL.Query().Get("labelSelector"))
	if err != nil {
		return apierrors.NewBadRequest("invalid labelSelector: " + err.Error())
	}
	fieldSelector, err := fields.ParseSelector(r.URL.Query().Get("fieldSelector"))
	if err != nil {
		return apierrors.NewBadRequest("invalid fieldSelector: " + err.Error())
	}
	period := h.PollPeriod
	if period <= 0 {
		period = 250 * time.Millisecond
	}
	w.Header().Set("Content-Type", "application/json;stream=watch")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	encoder := json.NewEncoder(w)
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	if sendInitialEvents(r) {
		if err := h.sendInitialObjectEvents(r.Context(), w, encoder, resource, namespace, selector, fieldSelector, &rv); err != nil {
			return err
		}
	}
	for {
		events, err := h.Objects.ObjectEventsAfter(r.Context(), resource.Key, rv, namespace)
		if err != nil {
			statusJSON, _ := json.Marshal(apierrors.NewInternalError(err).Status())
			_ = encoder.Encode(metav1.WatchEvent{Type: "ERROR", Object: runtime.RawExtension{Raw: statusJSON}})
			if flusher != nil {
				flusher.Flush()
			}
			return nil
		}
		for _, event := range events {
			rv = event.ResourceVersion
			matches := func(obj *unstructured.Unstructured) bool {
				return obj != nil && selector.Matches(labels.Set(obj.GetLabels())) && fieldSelector.Matches(fields.Set{
					"metadata.name": obj.GetName(), "metadata.namespace": obj.GetNamespace(),
				})
			}
			object, eventType := event.Object, event.Type
			switch event.Type {
			case "ADDED":
				if !matches(event.Object) {
					continue
				}
			case "MODIFIED":
				previousMatches, currentMatches := matches(event.Previous), matches(event.Object)
				switch {
				case previousMatches && currentMatches:
				case !previousMatches && currentMatches:
					eventType = "ADDED"
				case previousMatches && !currentMatches:
					eventType, object = "DELETED", event.Previous.DeepCopy()
					object.SetResourceVersion(event.ResourceVersion)
				default:
					continue
				}
			case "DELETED":
				if !matches(event.Object) {
					continue
				}
			}
			encoded, err := json.Marshal(object.Object)
			if err != nil {
				return fmt.Errorf("encode watch event: %w", err)
			}
			if err := encoder.Encode(metav1.WatchEvent{Type: eventType, Object: runtime.RawExtension{Raw: encoded}}); err != nil {
				return nil
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		select {
		case <-r.Context().Done():
			return nil
		case <-ticker.C:
		}
	}
}

// sendInitialObjectEvents implements the watch-list protocol (KEP-3157) for
// generic resources: stream ADDED events for current objects, then a bookmark
// annotated k8s.io/initial-events-end, then continue the regular stream.
func (h *Handler) sendInitialObjectEvents(ctx context.Context, w http.ResponseWriter, encoder *json.Encoder, resource Resource, namespace string, selector labels.Selector, fieldSelector fields.Selector, rv *string) error {
	items, listRV, err := h.Objects.ListObjects(ctx, resource.Key, namespace)
	if err != nil {
		return err
	}
	for i := range items {
		item := &items[i]
		if !selector.Matches(labels.Set(item.GetLabels())) ||
			!fieldSelector.Matches(fields.Set{"metadata.name": item.GetName(), "metadata.namespace": item.GetNamespace()}) {
			continue
		}
		encoded, err := json.Marshal(item.Object)
		if err != nil {
			return fmt.Errorf("encode initial watch event: %w", err)
		}
		if err := encoder.Encode(metav1.WatchEvent{Type: "ADDED", Object: runtime.RawExtension{Raw: encoded}}); err != nil {
			return nil
		}
	}
	bookmark := map[string]any{
		"apiVersion": resource.Key.Group + "/" + resource.Key.Version,
		"kind":       resource.Kind,
		"metadata": map[string]any{
			"resourceVersion": listRV,
			"annotations":     map[string]string{"k8s.io/initial-events-end": "true"},
		},
	}
	encoded, err := json.Marshal(bookmark)
	if err != nil {
		return fmt.Errorf("encode initial events bookmark: %w", err)
	}
	if err := encoder.Encode(metav1.WatchEvent{Type: "BOOKMARK", Object: runtime.RawExtension{Raw: encoded}}); err != nil {
		return nil
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	*rv = listRV
	return nil
}

func decodeUnstructured(r *http.Request) (*unstructured.Unstructured, error) {
	defer r.Body.Close()
	obj := &unstructured.Unstructured{Object: map[string]any{}}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	if err := decoder.Decode(&obj.Object); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}
	return obj, nil
}

func invalidResource(resource Resource, name, message string) error {
	return apierrors.NewInvalid(schema.GroupKind{Group: resource.Key.Group, Kind: resource.Kind}, name,
		field.ErrorList{field.Invalid(field.NewPath("object"), name, message)})
}

func afterObjectMarker(items []unstructured.Unstructured, marker string) []unstructured.Unstructured {
	separator := strings.IndexByte(marker, '\x00')
	if separator < 0 {
		return nil
	}
	markerNamespace, markerName := marker[:separator], marker[separator+1:]
	for i := range items {
		if items[i].GetNamespace() > markerNamespace || items[i].GetNamespace() == markerNamespace && items[i].GetName() > markerName {
			return items[i:]
		}
	}
	return nil
}
