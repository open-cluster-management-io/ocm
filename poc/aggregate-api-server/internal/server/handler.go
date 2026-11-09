/* Copyright Contributors to the Open Cluster Management project */

package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/ocm/poc/aggregate-api-server/internal/store"
)

const (
	groupName = "work.open-cluster-management.io"
	version   = "v1"
	resource  = "manifestworks"
)

var resourceName = schema.GroupResource{Group: groupName, Resource: resource}

type Authorizer interface {
	Authorize(context.Context, *http.Request, string, string, string, string, string, string, string) error
}

type Handler struct {
	Store      store.Store
	Objects    store.ObjectStore
	Authorizer Authorizer
	PollPeriod time.Duration
	// RequireProxyClientCert rejects resource requests that did not present a
	// client certificate verified against the aggregation proxy CA at TLS
	// handshake time. Availability probes and discovery stay reachable
	// without one. The apiserver binary sets this to true.
	RequireProxyClientCert bool
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/livez" || r.URL.Path == "/readyz" {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
		return
	}
	if r.URL.Path == "/apis" {
		h.writeJSON(w, http.StatusOK, apiGroups())
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/apis/") {
		h.writeError(w, apierrors.NewNotFound(schema.GroupResource{Resource: "apis"}, r.URL.Path))
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/apis/"), "/")
	if len(parts) == 1 {
		group, ok := apiGroupFor(parts[0])
		if !ok {
			h.writeError(w, apierrors.NewNotFound(schema.GroupResource{Resource: "apis"}, parts[0]))
			return
		}
		h.writeJSON(w, http.StatusOK, group)
		return
	}
	if len(parts) < 2 {
		h.writeError(w, apierrors.NewNotFound(schema.GroupResource{Resource: "apis"}, r.URL.Path))
		return
	}
	group, apiVersion := parts[0], parts[1]
	if len(parts) == 2 {
		if len(resourcesForGroupVersion(group, apiVersion)) == 0 {
			h.writeError(w, apierrors.NewNotFound(schema.GroupResource{Resource: "apis"}, group+"/"+apiVersion))
			return
		}
		h.writeJSON(w, http.StatusOK, apiResourcesFor(group, apiVersion))
		return
	}
	if len(parts) < 3 {
		h.writeError(w, apierrors.NewNotFound(schema.GroupResource{Resource: "apis"}, r.URL.Path))
		return
	}
	// Aggregator proxy client certificates are verified at TLS handshake time
	// (ClientAuth: VerifyClientCertIfGiven), so their presence here proves the
	// request came through the Kubernetes API server rather than from a
	// direct connection with forged proxy headers.
	if h.RequireProxyClientCert && (r.TLS == nil || len(r.TLS.PeerCertificates) == 0) {
		h.writeError(w, apierrors.NewUnauthorized("request must be proxied by the Kubernetes API server"))
		return
	}
	resourceDescriptor, ok := resourceFor(group, apiVersion, resourcePathResource(parts[2:], r))
	if !ok {
		h.writeError(w, apierrors.NewNotFound(schema.GroupResource{Group: group, Resource: parts[len(parts)-1]}, r.URL.Path))
		return
	}
	if resourceDescriptor.Key.Group == groupName && resourceDescriptor.Key.Version == version && resourceDescriptor.Key.Resource == resource {
		if err := h.handleResource(w, r); err != nil {
			h.writeError(w, err)
		}
		return
	}
	if err := h.handleGenericResource(w, r, resourceDescriptor, parts[2:]); err != nil {
		h.writeError(w, err)
	}
}

// authorizationName mimics kube-apiserver requestinfo parsing: a collection
// list/watch whose fieldSelector requires an exact metadata.name match is
// authorized against that name. This is what makes resourceNames-scoped RBAC
// rules (OCM grants spoke agents get/list/watch on their own ManagedCluster
// only) authorize the agents' field-selector-scoped informers.
func authorizationName(r *http.Request) string {
	selector, err := fields.ParseSelector(r.URL.Query().Get("fieldSelector"))
	if err != nil {
		return ""
	}
	name, ok := selector.RequiresExactMatch("metadata.name")
	if !ok || !validName(name) {
		return ""
	}
	return name
}

func (h *Handler) handleResource(w http.ResponseWriter, r *http.Request) error {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/apis/"+groupName+"/"+version+"/"), "/")
	// Cluster-wide collection (all namespaces): /manifestworks. The hub
	// controllers' informers list ManifestWorks across every namespace.
	if len(parts) == 1 && parts[0] == resource {
		if r.Method != http.MethodGet {
			return apierrors.NewMethodNotSupported(resourceName, r.Method)
		}
		namespace := ""
		verb := requestVerb(r)
		if !isWatch(r) {
			verb = "list"
		}
		if h.Authorizer != nil {
			if err := h.Authorizer.Authorize(r.Context(), r, verb, groupName, version, namespace, resource, "", authorizationName(r)); err != nil {
				return err
			}
		}
		if isWatch(r) {
			return h.watch(w, r, namespace)
		}
		return h.list(w, r, namespace)
	}
	if len(parts) < 3 || parts[0] != "namespaces" || parts[2] != resource || len(parts) > 5 {
		return apierrors.NewNotFound(resourceName, r.URL.Path)
	}
	namespace := parts[1]
	if namespace == "" {
		return apierrors.NewBadRequest("namespace is required")
	}
	name := ""
	subresource := ""
	if len(parts) >= 4 {
		name = parts[3]
	}
	if len(parts) == 5 {
		subresource = parts[4]
	}
	if len(parts) == 4 && name == "" || len(parts) == 5 && (name == "" || subresource != "status") {
		return apierrors.NewNotFound(resourceName, r.URL.Path)
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
		if err := h.Authorizer.Authorize(r.Context(), r, verb, groupName, version, namespace, resource, subresource, authName); err != nil {
			return err
		}
	}

	if r.Method == http.MethodGet && name == "" && isWatch(r) {
		return h.watch(w, r, namespace)
	}
	switch {
	case r.Method == http.MethodGet && name == "":
		return h.list(w, r, namespace)
	case r.Method == http.MethodGet && name != "":
		return h.get(w, r, namespace, name)
	case r.Method == http.MethodPost && name == "":
		return h.create(w, r, namespace)
	case r.Method == http.MethodPut && name != "":
		return h.update(w, r, namespace, name, subresource == "status")
	case r.Method == http.MethodPatch && name != "":
		return h.patch(w, r, namespace, name, subresource == "status")
	case r.Method == http.MethodDelete && name != "" && subresource == "":
		return h.delete(w, r, namespace, name)
	default:
		return apierrors.NewMethodNotSupported(resourceName, r.Method)
	}
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, namespace, name string) error {
	obj, err := h.Store.Get(r.Context(), namespace, name)
	if err != nil {
		return err
	}
	h.writeJSON(w, http.StatusOK, obj)
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, namespace string) error {
	items, rv, err := h.Store.List(r.Context(), namespace)
	if err != nil {
		return err
	}
	labelSelector, err := labels.Parse(r.URL.Query().Get("labelSelector"))
	if err != nil {
		return apierrors.NewBadRequest(fmt.Sprintf("invalid labelSelector: %v", err))
	}
	fieldSelector, err := fields.ParseSelector(r.URL.Query().Get("fieldSelector"))
	if err != nil {
		return apierrors.NewBadRequest(fmt.Sprintf("invalid fieldSelector: %v", err))
	}
	filtered := make([]workv1.ManifestWork, 0, len(items))
	for _, item := range items {
		if !labelSelector.Matches(labels.Set(item.Labels)) {
			continue
		}
		if !fieldSelector.Matches(fields.Set{"metadata.name": item.Name, "metadata.namespace": item.Namespace}) {
			continue
		}
		filtered = append(filtered, item)
	}
	continueToken := r.URL.Query().Get("continue")
	if continueToken != "" {
		marker, err := base64.RawURLEncoding.DecodeString(continueToken)
		if err != nil {
			return apierrors.NewBadRequest("invalid continue token")
		}
		filtered = afterListMarker(filtered, string(marker))
	}
	nextToken := ""
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 0 {
			return apierrors.NewBadRequest("limit must be a non-negative integer")
		}
		if limit > 0 && len(filtered) > limit {
			last := filtered[limit-1]
			nextToken = base64.RawURLEncoding.EncodeToString([]byte(last.Namespace + "\x00" + last.Name))
			filtered = filtered[:limit]
		}
	}
	list := &workv1.ManifestWorkList{
		TypeMeta: metav1.TypeMeta{APIVersion: groupName + "/" + version, Kind: "ManifestWorkList"},
		ListMeta: metav1.ListMeta{ResourceVersion: rv, Continue: nextToken},
		Items:    filtered,
	}
	h.writeJSON(w, http.StatusOK, list)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, namespace string) error {
	obj := &workv1.ManifestWork{}
	if err := decodeBody(r, obj); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	if obj.Name == "" {
		return apierrors.NewBadRequest("metadata.name is required; generateName is not supported by this POC")
	}
	if obj.Namespace != "" && obj.Namespace != namespace {
		return apierrors.NewBadRequest("metadata.namespace does not match request namespace")
	}
	obj.Namespace = namespace
	obj.APIVersion = groupName + "/" + version
	obj.Kind = "ManifestWork"
	if err := validateManifestWork(obj); err != nil {
		return invalidManifestWork(obj.Name, err)
	}
	created, err := h.Store.Create(r.Context(), obj)
	if err != nil {
		return err
	}
	h.writeJSON(w, http.StatusCreated, created)
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, namespace, name string, statusOnly bool) error {
	obj := &workv1.ManifestWork{}
	if err := decodeBody(r, obj); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	if obj.Name != "" && obj.Name != name || obj.Namespace != "" && obj.Namespace != namespace {
		return apierrors.NewBadRequest("metadata.name and metadata.namespace must match the request path")
	}
	obj.Name, obj.Namespace = name, namespace
	obj.APIVersion, obj.Kind = groupName+"/"+version, "ManifestWork"
	if err := validateManifestWork(obj); err != nil {
		return invalidManifestWork(obj.Name, err)
	}
	updated, err := h.Store.Update(r.Context(), obj, statusOnly)
	if err != nil {
		return err
	}
	h.writeJSON(w, http.StatusOK, updated)
	return nil
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request, namespace, name string, statusOnly bool) error {
	patch, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		return apierrors.NewBadRequest("read patch: " + err.Error())
	}
	contentType := strings.Split(r.Header.Get("Content-Type"), ";")[0]
	if contentType != "application/merge-patch+json" {
		return apierrors.NewBadRequest("this POC supports application/merge-patch+json only")
	}
	existing, err := h.Store.Get(r.Context(), namespace, name)
	if err != nil {
		return err
	}
	current, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("encode current ManifestWork: %w", err)
	}
	patched, err := mergePatch(current, patch)
	if err != nil {
		return apierrors.NewBadRequest("invalid merge patch: " + err.Error())
	}
	obj := &workv1.ManifestWork{}
	if err := json.Unmarshal(patched, obj); err != nil {
		return apierrors.NewBadRequest("decode patched ManifestWork: " + err.Error())
	}
	obj.Name, obj.Namespace = name, namespace
	obj.APIVersion, obj.Kind = groupName+"/"+version, "ManifestWork"
	if err := validateManifestWork(obj); err != nil {
		return invalidManifestWork(obj.Name, err)
	}
	updated, err := h.Store.Update(r.Context(), obj, statusOnly)
	if err != nil {
		return err
	}
	h.writeJSON(w, http.StatusOK, updated)
	return nil
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, namespace, name string) error {
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
	if _, err := h.Store.Delete(r.Context(), namespace, name, expectedRV); err != nil {
		return err
	}
	h.writeJSON(w, http.StatusOK, &metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusSuccess,
		Code:     http.StatusOK,
		Details:  &metav1.StatusDetails{Name: name, Group: groupName, Kind: "manifestworks"},
	})
	return nil
}

func (h *Handler) watch(w http.ResponseWriter, r *http.Request, namespace string) error {
	rv := r.URL.Query().Get("resourceVersion")
	if rv == "" {
		var err error
		rv, err = h.Store.LatestResourceVersion(r.Context())
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
		if err := h.sendInitialManifestWorkEvents(r.Context(), w, encoder, namespace, selector, fieldSelector, &rv); err != nil {
			return err
		}
	}
	for {
		events, err := h.Store.EventsAfter(r.Context(), rv, namespace)
		if err != nil {
			status := apierrors.NewInternalError(err).Status()
			statusJSON, _ := json.Marshal(status)
			_ = encoder.Encode(metav1.WatchEvent{Type: "ERROR", Object: runtime.RawExtension{Raw: statusJSON}})
			if flusher != nil {
				flusher.Flush()
			}
			return nil
		}
		for _, event := range events {
			rv = event.ResourceVersion
			matches := func(obj *workv1.ManifestWork) bool {
				return obj != nil && selector.Matches(labels.Set(obj.Labels)) && fieldSelector.Matches(fields.Set{"metadata.name": obj.Name, "metadata.namespace": obj.Namespace})
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
					eventType = "MODIFIED"
				case !previousMatches && currentMatches:
					eventType = "ADDED"
				case previousMatches && !currentMatches:
					eventType = "DELETED"
					object = event.Previous.DeepCopy()
					object.ResourceVersion = event.ResourceVersion
				default:
					continue
				}
			case "DELETED":
				if !matches(event.Object) {
					continue
				}
			}
			if object == nil {
				continue
			}
			encoded, err := json.Marshal(object)
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

// sendInitialManifestWorkEvents implements the watch-list protocol
// (KEP-3157): when a client sends sendInitialEvents=true, stream ADDED events
// for the current objects followed by a bookmark annotated
// k8s.io/initial-events-end, then continue the regular event stream.
func (h *Handler) sendInitialManifestWorkEvents(ctx context.Context, w http.ResponseWriter, encoder *json.Encoder, namespace string, selector labels.Selector, fieldSelector fields.Selector, rv *string) error {
	items, listRV, err := h.Store.List(ctx, namespace)
	if err != nil {
		return err
	}
	for i := range items {
		item := &items[i]
		if !selector.Matches(labels.Set(item.Labels)) ||
			!fieldSelector.Matches(fields.Set{"metadata.name": item.Name, "metadata.namespace": item.Namespace}) {
			continue
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("encode initial watch event: %w", err)
		}
		if err := encoder.Encode(metav1.WatchEvent{Type: "ADDED", Object: runtime.RawExtension{Raw: encoded}}); err != nil {
			return nil
		}
	}
	bookmark := &workv1.ManifestWork{
		TypeMeta:   metav1.TypeMeta{APIVersion: groupName + "/" + version, Kind: "ManifestWork"},
		ObjectMeta: metav1.ObjectMeta{ResourceVersion: listRV, Annotations: map[string]string{"k8s.io/initial-events-end": "true"}},
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

// sendInitialEvents reports whether the client requested the watch-list
// initial events stream.
func sendInitialEvents(r *http.Request) bool {
	return r.URL.Query().Get("sendInitialEvents") == "true"
}

func (h *Handler) writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	status := &metav1.Status{Status: metav1.StatusFailure, Code: http.StatusInternalServerError, Message: "internal error", Reason: metav1.StatusReasonInternalError}
	var apiStatus apierrors.APIStatus
	if errors.As(err, &apiStatus) {
		apiStatus := apiStatus.Status()
		status = &apiStatus
	}
	h.writeJSON(w, int(status.Code), status)
}

func decodeBody(r *http.Request, into any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func requestVerb(r *http.Request) string {
	if r.Method == http.MethodGet && isWatch(r) {
		return "watch"
	}
	switch r.Method {
	case http.MethodGet:
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 4 {
			resourceName := parts[len(parts)-1]
			for _, candidate := range resources() {
				if candidate.Key.Group == parts[1] && candidate.Key.Version == parts[2] && candidate.Key.Resource == resourceName {
					return "list"
				}
			}
		}
		return "get"
	case http.MethodPost:
		return "create"
	case http.MethodPut:
		return "update"
	case http.MethodPatch:
		return "patch"
	case http.MethodDelete:
		return "delete"
	default:
		return strings.ToLower(r.Method)
	}
}

func isWatch(r *http.Request) bool {
	return r.URL.Query().Get("watch") == "true" || r.URL.Query().Get("watch") == "1"
}

func validName(name string) bool {
	return name != "." && name != ".." && !strings.ContainsAny(name, "/\\")
}

func afterListMarker(items []workv1.ManifestWork, marker string) []workv1.ManifestWork {
	separator := strings.IndexByte(marker, '\x00')
	if separator < 0 {
		return nil
	}
	markerNamespace, markerName := marker[:separator], marker[separator+1:]
	for i, item := range items {
		if item.Namespace > markerNamespace || item.Namespace == markerNamespace && item.Name > markerName {
			return items[i:]
		}
	}
	return nil
}

func validateManifestWork(obj *workv1.ManifestWork) error {
	if problems := validation.IsDNS1123Subdomain(obj.Name); len(problems) != 0 {
		return fmt.Errorf("metadata.name is invalid: %s", strings.Join(problems, "; "))
	}
	if problems := validation.IsDNS1123Label(obj.Namespace); len(problems) != 0 {
		return fmt.Errorf("metadata.namespace is invalid: %s", strings.Join(problems, "; "))
	}
	for i, manifest := range obj.Spec.Workload.Manifests {
		encoded, err := json.Marshal(manifest.RawExtension)
		if err != nil {
			return fmt.Errorf("spec.workload.manifests[%d] is invalid: %w", i, err)
		}
		var value map[string]any
		if err := json.Unmarshal(encoded, &value); err != nil {
			return fmt.Errorf("spec.workload.manifests[%d] must be a Kubernetes object: %w", i, err)
		}
		apiVersion, apiVersionIsString := value["apiVersion"].(string)
		kind, kindIsString := value["kind"].(string)
		if !apiVersionIsString || apiVersion == "" || !kindIsString || kind == "" {
			return fmt.Errorf("spec.workload.manifests[%d] must include apiVersion and kind", i)
		}
		if _, ok := value["metadata"].(map[string]any); !ok {
			return fmt.Errorf("spec.workload.manifests[%d] must include object metadata", i)
		}
	}
	for i, condition := range obj.Status.Conditions {
		if condition.Type == "" || condition.Reason == "" || condition.LastTransitionTime.IsZero() {
			return fmt.Errorf("status.conditions[%d] requires type, reason, and lastTransitionTime", i)
		}
		if condition.Status != "True" && condition.Status != "False" && condition.Status != "Unknown" {
			return fmt.Errorf("status.conditions[%d].status must be True, False, or Unknown", i)
		}
	}
	return nil
}

func invalidManifestWork(name string, err error) error {
	return apierrors.NewInvalid(
		schema.GroupKind{Group: groupName, Kind: "ManifestWork"},
		name,
		field.ErrorList{field.Invalid(field.NewPath("object"), name, err.Error())},
	)
}

// mergePatch is kept behind a small helper to make the accepted patch format explicit.
func mergePatch(current, patch []byte) ([]byte, error) {
	return jsonpatch.MergePatch(current, patch)
}
