package apply

import (
	"context"
	"crypto/md5" //nolint:gosec
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/itchyny/gojq"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/jsonpath"
	"k8s.io/klog/v2"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	"open-cluster-management.io/ocm/pkg/work/helper"
)

type ServerSideApply struct {
	client dynamic.Interface
}

type ServerSideApplyConflictError struct {
	ssaErr error
}

func (e *ServerSideApplyConflictError) Error() string {
	return e.ssaErr.Error()
}

func NewServerSideApply(client dynamic.Interface) *ServerSideApply {
	return &ServerSideApply{client: client}
}

func (c *ServerSideApply) Apply(
	ctx context.Context,
	gvr schema.GroupVersionResource,
	requiredOriginal *unstructured.Unstructured,
	owner metav1.OwnerReference,
	applyOption *workapiv1.ManifestConfigOption,
	_ events.Recorder) (runtime.Object, error) {
	logger := klog.FromContext(ctx)
	// Currently, if the required object has zero creationTime in metadata, it will cause
	// kube-apiserver to increment generation even if nothing else changes. more details see:
	// https://github.com/kubernetes/kubernetes/issues/67610
	//
	// TODO Remove this after the above issue fixed in Kubernetes
	removeCreationTimeFromMetadata(requiredOriginal.Object, logger)

	force := false
	fieldManager := workapiv1.DefaultFieldManager
	var requiredHash string
	var ignoreFields []workapiv1.IgnoreField

	required := requiredOriginal.DeepCopy()

	if applyOption.UpdateStrategy.ServerSideApply != nil {
		force = applyOption.UpdateStrategy.ServerSideApply.Force
		if len(applyOption.UpdateStrategy.ServerSideApply.FieldManager) > 0 {
			fieldManager = applyOption.UpdateStrategy.ServerSideApply.FieldManager
		}

		ignoreFields = applyOption.UpdateStrategy.ServerSideApply.IgnoreFields
		if len(ignoreFields) > 0 {
			for _, field := range ignoreFields {
				// for IgnoreFieldsConditionOnSpokeChange, it will still be included when computing the hash. So when
				// hash dismatch, these fields will still the patched on the cluster.
				if field.Condition == workapiv1.IgnoreFieldsConditionOnSpokeChange {
					continue
				}

				// Process JSONPaths (existing behavior)
				for _, path := range field.JSONPaths {
					removeFieldByJSONPath(required.UnstructuredContent(), path, logger)
				}

				// Process JSONPointers (RFC 6901)
				for _, pointer := range field.JSONPointers {
					if err := removeFieldByJSONPointer(required, pointer, logger); err != nil {
						logger.Error(err, "failed to remove field by JSON Pointer", "pointer", pointer)
						return nil, NewIgnoreFieldError(err, "JSON Pointer error in '%s': %v", pointer, err)
					}
				}

				// Process JQPathExpressions
				for _, expr := range field.JQPathExpressions {
					if err := removeFieldByJQExpression(ctx, required, expr, logger); err != nil {
						logger.Error(err, "failed to remove field by JQ expression", "expression", expr)
						return nil, NewIgnoreFieldError(err, "JQ expression error in '%s': %v", expr, err)
					}
				}
			}
			requiredHash = hashOfResourceStruct(required)
			annotation := required.GetAnnotations()
			if annotation == nil {
				annotation = map[string]string{}
			}
			annotation[workapiv1.ManifestConfigSpecHashAnnotationKey] = requiredHash
			required.SetAnnotations(annotation)
			requiredOriginal.SetAnnotations(annotation)
		}
	}

	// only get existing resource and compare hash if the hash is computed.
	if len(requiredHash) > 0 {
		existing, err := c.client.Resource(gvr).Namespace(required.GetNamespace()).Get(
			ctx, required.GetName(), metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			// if object is not found, use requiredOriginal to apply so the ignore fields are kept when create
			required = requiredOriginal
		case err != nil:
			return nil, err
		default:
			// skip the apply operation when the hash of the existing resource matches the required hash
			if existingHash := existing.GetAnnotations()[workapiv1.ManifestConfigSpecHashAnnotationKey]; len(
				existingHash) > 0 && existingHash == requiredHash {
				// still needs to apply ownerref since it might be changed due to deleteoption update.
				err := helper.ApplyOwnerReferences(ctx, c.client, gvr, existing, owner)
				return existing, err
			}
			// Build the payload from the manifest and overlay the live values of the
			// ignored fields on top of it. Server side apply deletes fields that this
			// field manager previously owned but are absent from the applied object,
			// so the ignored fields must stay in the payload — carrying the live
			// values — to be preserved on the spoke.
			required = requiredOriginal.DeepCopy()
			if err := overlayIgnoredFieldsFromLive(ctx, existing, required, ignoreFields, logger); err != nil {
				return nil, NewIgnoreFieldError(err, "Failed to overlay ignored fields from the live object: %v", err)
			}
		}
	}

	obj, err := c.client.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Apply(ctx, required.GetName(), required, metav1.ApplyOptions{FieldManager: fieldManager, Force: force})

	if apierrors.IsConflict(err) {
		return obj, &ServerSideApplyConflictError{ssaErr: err}
	}

	if err != nil {
		return obj, err
	}

	logger.Info("Server side applied",
		"gvr", gvr.String(), "resourceNamespace", required.GetNamespace(),
		"resourceName", required.GetName(), "fieldManager", fieldManager)

	err = helper.ApplyOwnerReferences(ctx, c.client, gvr, obj, owner)
	return obj, err
}

const (
	// DefaultJQExecutionTimeout is the default timeout for jq expression execution
	DefaultJQExecutionTimeout = 1 * time.Second
)

// IgnoreFieldError represents an error that occurred during ignoreFields processing
// This error type is used to distinguish ignoreFields errors from other apply errors
// so they can be reported with the specific AppliedManifestSSAIgnoreFieldError reason
type IgnoreFieldError struct {
	message string
	cause   error
}

func (e *IgnoreFieldError) Error() string {
	return e.message
}

func (e *IgnoreFieldError) Unwrap() error {
	return e.cause
}

// NewIgnoreFieldError creates a new IgnoreFieldError wrapping the given cause
func NewIgnoreFieldError(cause error, format string, args ...interface{}) error {
	return &IgnoreFieldError{
		message: fmt.Sprintf(format, args...),
		cause:   cause,
	}
}

// removeFieldByJSONPath remove the field from object by json path. The json path should not point to a
// list, since removing list from the object and apply would bring unexpected behavior.
func removeFieldByJSONPath(obj interface{}, path string, logger klog.Logger) {
	listKeys := strings.Split(path, ".")
	if len(listKeys) == 0 {
		return
	}
	lastKey := listKeys[len(listKeys)-1]
	pathWithoutLastKey := strings.TrimSuffix(path, "."+lastKey)
	finder := jsonpath.New("ignoreFields").AllowMissingKeys(true)
	if err := finder.Parse(fmt.Sprintf("{%s}", pathWithoutLastKey)); err != nil {
		logger.Error(err, "parse jsonpath", "path", pathWithoutLastKey)
	}
	results, err := finder.FindResults(obj)
	if err != nil {
		logger.Error(err, "find jsonpath", "path", pathWithoutLastKey)
	}
	for _, result := range results {
		for _, r := range result {
			mapResult, ok := r.Interface().(map[string]interface{})
			if !ok {
				continue
			}
			delete(mapResult, lastKey)
		}
	}
}

// removeFieldByJSONPointer removes the field from object using RFC 6901 JSON Pointer.
func removeFieldByJSONPointer(obj *unstructured.Unstructured, pointer string, logger klog.Logger) error {
	// Marshal the object to JSON
	objJSON, err := obj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal object: %w", err)
	}

	// Create a JSON Patch to remove the field
	patchData, err := json.Marshal([]map[string]string{{"op": "remove", "path": pointer}})
	if err != nil {
		return fmt.Errorf("failed to create patch: %w", err)
	}

	patch, err := jsonpatch.DecodePatch(patchData)
	if err != nil {
		return fmt.Errorf("failed to decode patch: %w", err)
	}

	// Apply the patch
	patchedJSON, err := patch.Apply(objJSON)
	if err != nil {
		// Silently ignore if path doesn't exist (using proper error checking)
		if errors.Is(err, jsonpatch.ErrMissing) {
			logger.V(4).Info("JSON Pointer path not found, skipping", "pointer", pointer)
			return nil
		}
		return fmt.Errorf("failed to apply JSON Pointer patch: %w", err)
	}

	// Unmarshal back to the object
	if err := obj.UnmarshalJSON(patchedJSON); err != nil {
		return fmt.Errorf("failed to unmarshal patched object: %w", err)
	}

	return nil
}

// removeFieldByJQExpression removes fields from object using jq path expression.
func removeFieldByJQExpression(ctx context.Context, obj *unstructured.Unstructured, expression string, logger klog.Logger) error {
	// Parse the jq expression wrapped in del()
	jqDeletionQuery, err := gojq.Parse(fmt.Sprintf("del(%s)", expression))
	if err != nil {
		return fmt.Errorf("failed to parse jq expression: %w", err)
	}

	// Compile the query
	jqDeletionCode, err := gojq.Compile(jqDeletionQuery)
	if err != nil {
		return fmt.Errorf("failed to compile jq expression: %w", err)
	}

	// Get object as map
	dataJSON := obj.UnstructuredContent()

	// Execute with timeout derived from parent context
	// This will cancel when EITHER parent context is cancelled OR timeout expires
	timeoutCtx, cancel := context.WithTimeout(ctx, DefaultJQExecutionTimeout)
	defer cancel()

	iter := jqDeletionCode.RunWithContext(timeoutCtx, dataJSON)
	first, ok := iter.Next()
	if !ok {
		return fmt.Errorf("jq expression did not return any data")
	}

	// Check for errors
	if errVal, ok := first.(error); ok {
		if errors.Is(errVal, context.DeadlineExceeded) {
			return fmt.Errorf("jq expression execution timed out after %v", DefaultJQExecutionTimeout)
		}
		if errors.Is(errVal, context.Canceled) {
			return fmt.Errorf("jq expression execution cancelled: %w", errVal)
		}
		return fmt.Errorf("jq expression returned error: %w", errVal)
	}

	// Check for multiple results
	_, ok = iter.Next()
	if ok {
		return fmt.Errorf("jq expression returned multiple objects")
	}

	// Set the modified content back
	if resultMap, ok := first.(map[string]interface{}); ok {
		obj.SetUnstructuredContent(resultMap)
	} else {
		return fmt.Errorf("jq expression result is not a valid object")
	}

	return nil
}

// overlayIgnoredFieldsFromLive overlays the live values of the OnSpokePresent ignored fields
// onto the payload built from the manifest. The payload is never stripped of the ignored
// fields: server side apply deletes fields that this field manager owns but are absent from
// the applied object. Rules per ignored path:
//   - present on live and its containing element exists in the payload (matched by name for
//     lists): only the live value at the exact path is copied.
//   - the containing element or parent is absent from the payload: the complete live element
//     or parent subtree is carried over, but only when the selection targets the element
//     itself. A leaf selection inside a payload-absent element does not carry it, so an
//     element removed from the manifest is not added back from live.
//   - absent on live: the field is omitted from the payload when its containing element
//     exists on live (adding it would be an update to an ignored field); the manifest value
//     in the payload applies only when the containing element itself is missing on live.
func overlayIgnoredFieldsFromLive(
	ctx context.Context,
	live, payload *unstructured.Unstructured,
	ignoreFields []workapiv1.IgnoreField,
	logger klog.Logger,
) error {
	for _, field := range ignoreFields {
		if field.Condition != workapiv1.IgnoreFieldsConditionOnSpokePresent {
			continue
		}

		for _, path := range field.JSONPaths {
			if err := overlayFieldByJSONPath(live.Object, payload.Object, path, logger); err != nil {
				return err
			}
		}

		for _, pointer := range field.JSONPointers {
			if err := overlayFieldByJSONPointer(live.Object, payload.Object, pointer, logger); err != nil {
				return err
			}
		}

		for _, expr := range field.JQPathExpressions {
			if err := overlayFieldByJQExpression(ctx, live.Object, payload.Object, expr, logger); err != nil {
				return err
			}
		}
	}
	return nil
}

// overlayFieldByJSONPointer overlays the value at the given RFC 6901 JSON pointer from the
// live object onto the payload. When the pointer does not exist on the live object, the field
// is omitted from the payload instead — adding it would be an update to an ignored field —
// unless its containing element is missing on live too, in which case the manifest value in
// the payload applies.
func overlayFieldByJSONPointer(live, payload map[string]interface{}, pointer string, logger klog.Logger) error {
	if pointer == "" {
		return nil
	}

	path, value, found := resolvePointer(live, pointer)
	if found {
		// the top level is the payload map itself, mutated in place
		_, _ = overlayValuePositionally(payload, live, path, value)
		return nil
	}

	// absent on live: omit the field from the payload when its parent exists on live
	path, _, found = resolvePointer(payload, pointer)
	if !found {
		return nil
	}
	if _, ok := path[len(path)-1].(string); !ok {
		// a list element missing on live keeps its manifest value
		return nil
	}
	if _, found := getValueAtPath(live, path[:len(path)-1]); !found {
		return nil
	}
	logger.V(4).Info("JSON Pointer path not found on the live object, omitting it from the payload",
		"pointer", pointer)
	removeValueAtPath(payload, path)
	return nil
}

// resolvePointer walks the object along the RFC 6901 JSON pointer and returns the typed path
// (map keys as strings, slice indices as ints) and the value, or found=false when the pointer
// does not resolve.
func resolvePointer(obj map[string]interface{}, pointer string) ([]interface{}, interface{}, bool) {
	tokens := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	path := make([]interface{}, 0, len(tokens))

	var current interface{} = obj
	for _, raw := range tokens {
		token := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch c := current.(type) {
		case map[string]interface{}:
			v, ok := c[token]
			if !ok {
				return nil, nil, false
			}
			path = append(path, token)
			current = v
		case []interface{}:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(c) {
				return nil, nil, false
			}
			path = append(path, index)
			current = c[index]
		default:
			return nil, nil, false
		}
	}
	return path, current, true
}

// overlayValuePositionally sets the live value at the path on the payload by position.
// When the path continues past a list index the payload does not have, the complete live
// element is appended instead of synthesizing a partial element from the remaining path.
// It reports whether the payload changed, so callers can avoid writing back containers
// (potentially synthesized empty ones) when the overlay was skipped.
func overlayValuePositionally(payload, live interface{}, path []interface{}, value interface{}) (interface{}, bool) {
	if len(path) == 0 {
		return value, true
	}
	switch seg := path[0].(type) {
	case string:
		m, ok := payload.(map[string]interface{})
		if !ok {
			m = map[string]interface{}{}
		}
		child, changed := overlayValuePositionally(m[seg], mapValue(live, seg), path[1:], value)
		if !changed {
			return payload, false
		}
		m[seg] = child
		return m, true
	case int:
		s, ok := payload.([]interface{})
		if !ok {
			s = []interface{}{}
		}
		var liveElem interface{}
		if liveList, ok := live.([]interface{}); ok && seg >= 0 && seg < len(liveList) {
			liveElem = liveList[seg]
		}
		if seg >= 0 && seg < len(s) {
			child, changed := overlayValuePositionally(s[seg], liveElem, path[1:], value)
			if !changed {
				return payload, false
			}
			s[seg] = child
			return s, true
		}
		// out of range: carry the element only when the pointer targets the element
		// itself; a leaf inside an element the payload does not have is not carried,
		// since the element's lifecycle is not covered by the ignored path
		if len(path) == 1 {
			return append(s, value), true
		}
		// skipped: report the original slice (which may be a fresh empty one that the
		// caller must not write back) with changed=false
		return payload, false
	}
	return payload, false
}

// overlayFieldByJSONPath overlays the value at the given json path from the live object onto
// the payload, mirroring how removeFieldByJSONPath resolves the path. Payload maps are paired
// with live maps by their name key when available, and by position otherwise. When the payload
// omits the parent, the complete live parent subtree is carried over. When the path is absent
// on live, the manifest value in the payload applies unchanged.
func overlayFieldByJSONPath(live, payload map[string]interface{}, path string, logger klog.Logger) error {
	listKeys := strings.Split(path, ".")
	if len(listKeys) == 0 {
		return nil
	}
	lastKey := listKeys[len(listKeys)-1]
	pathWithoutLastKey := strings.TrimSuffix(path, "."+lastKey)

	findMaps := func(obj map[string]interface{}) []map[string]interface{} {
		finder := jsonpath.New("ignoreFields").AllowMissingKeys(true)
		if err := finder.Parse(fmt.Sprintf("{%s}", pathWithoutLastKey)); err != nil {
			logger.Error(err, "parse jsonpath", "path", pathWithoutLastKey)
			return nil
		}
		results, err := finder.FindResults(obj)
		if err != nil {
			logger.Error(err, "find jsonpath", "path", pathWithoutLastKey)
			return nil
		}
		var maps []map[string]interface{}
		for _, result := range results {
			for _, r := range result {
				if mapResult, ok := r.Interface().(map[string]interface{}); ok {
					maps = append(maps, mapResult)
				}
			}
		}
		return maps
	}

	liveMaps := findMaps(live)
	payloadMaps := findMaps(payload)

	if len(payloadMaps) == 0 && len(liveMaps) > 0 {
		parentPath, ok := parentKeyChain(pathWithoutLastKey)
		if !ok {
			logger.V(4).Info("json path parent not found in the payload, skipping overlay", "path", path)
			return nil
		}
		liveParent, found := getValueAtPath(live, parentPath)
		if !found {
			return nil
		}
		// The payload has the parent list, but the selection matched nothing in it
		// (e.g. a [?()] filter): carry the selected live entries alongside the
		// existing ones instead of replacing the whole list.
		if existing, exists := getValueAtPath(payload, parentPath); exists {
			if existingList, isList := existing.([]interface{}); isList {
				for _, liveMap := range liveMaps {
					if _, matched := indexOfMapByName(existingList, liveMap); !matched {
						existingList = append(existingList, liveMap)
					}
				}
				setValueAtPath(payload, parentPath, existingList)
				return nil
			}
		}
		// The payload omits the parent: the whole parent subtree is managed by the
		// spoke, so carry the complete live parent instead of synthesizing partial
		// elements.
		setValueAtPath(payload, parentPath, liveParent)
		return nil
	}

	for i, payloadMap := range payloadMaps {
		var liveMap map[string]interface{}
		if _, hasName := payloadMap["name"]; hasName {
			// named elements are paired by identity only; without a live match the
			// manifest value in the payload applies
			liveMap = matchMapByName(liveMaps, payloadMap)
		} else if i < len(liveMaps) {
			// unnamed elements are paired by position
			liveMap = liveMaps[i]
		}
		if liveMap == nil {
			continue
		}
		if value, ok := liveMap[lastKey]; ok {
			payloadMap[lastKey] = value
			continue
		}
		// the field is absent on the live element: adding it would be an update to an
		// ignored field, so omit it from the payload
		delete(payloadMap, lastKey)
	}
	return nil
}

// parentKeyChain converts a json path without its last key into a typed path of map keys.
// A trailing list selector (e.g. .spec.http[*] or .spec.http[0]) resolves to the chain of
// the list itself. Paths with mid-path list selectors cannot be resolved to a single parent.
func parentKeyChain(pathWithoutLastKey string) ([]interface{}, bool) {
	p := pathWithoutLastKey
	if strings.HasSuffix(p, "]") {
		p = p[:strings.LastIndex(p, "[")]
	}
	if strings.Contains(p, "[") {
		return nil, false
	}
	p = strings.TrimPrefix(strings.TrimPrefix(p, "$"), ".")
	if p == "" {
		return nil, false
	}
	segments := strings.Split(p, ".")
	path := make([]interface{}, 0, len(segments))
	for _, segment := range segments {
		path = append(path, segment)
	}
	return path, true
}

// matchMapByName returns the unique live map that has the same name as the target map,
// or nil when either map has no name or the name is not unique among the live maps.
func matchMapByName(liveMaps []map[string]interface{}, targetMap map[string]interface{}) map[string]interface{} {
	name, ok := targetMap["name"].(string)
	if !ok || name == "" {
		return nil
	}
	var matched map[string]interface{}
	for _, liveMap := range liveMaps {
		if liveName, ok := liveMap["name"].(string); ok && liveName == name {
			if matched != nil {
				return nil
			}
			matched = liveMap
		}
	}
	return matched
}

// overlayFieldByJQExpression overlays the values selected by the jq path expression from the
// live object onto the payload. Fields selected in the payload but absent on the live object
// are omitted from the payload instead — adding them would be an update to an ignored field —
// unless their containing element is missing on live, in which case the manifest value in the
// payload applies.
func overlayFieldByJQExpression(
	ctx context.Context, live, payload map[string]interface{}, expression string, logger klog.Logger) error {
	// omit fields that are selected in the payload but absent on the live object
	payloadPaths, err := selectJQPaths(ctx, expression, payload)
	if err != nil {
		return err
	}
	for _, path := range payloadPaths {
		if _, found := getValueByIdentity(live, payload, path); found {
			// present on live: the overlay below sets the live value
			continue
		}
		if _, ok := path[len(path)-1].(string); !ok {
			// a list element missing on live keeps its manifest value
			continue
		}
		if _, found := getValueByIdentity(live, payload, path[:len(path)-1]); !found {
			// the containing element is missing on live: the manifest value applies
			continue
		}
		removeValueAtPath(payload, path)
	}

	// overlay the live values of the selected fields
	livePaths, err := selectJQPaths(ctx, expression, live)
	if err != nil {
		return err
	}
	for _, path := range livePaths {
		value, found := getValueAtPath(live, path)
		if !found {
			continue
		}
		// the top level is the payload map itself, mutated in place
		_, _ = overlaySetByIdentity(payload, live, path, value)
	}
	return nil
}

// selectJQPaths resolves the concrete typed paths (map keys as strings, slice indices as
// ints) the jq path expression selects on the given object. Iterations over missing or
// scalar structures fail, so expressions should use the optional `[]?` form when a list
// may be absent.
func selectJQPaths(ctx context.Context, expression string, obj map[string]interface{}) ([][]interface{}, error) {
	pathQuery, err := gojq.Parse(fmt.Sprintf("[path(%s)]", expression))
	if err != nil {
		return nil, fmt.Errorf("failed to parse jq path query: %w", err)
	}
	pathCode, err := gojq.Compile(pathQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to compile jq path query: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, DefaultJQExecutionTimeout)
	defer cancel()

	iter := pathCode.RunWithContext(timeoutCtx, obj)
	first, ok := iter.Next()
	if !ok {
		return nil, nil
	}
	if errVal, ok := first.(error); ok {
		if errors.Is(errVal, context.DeadlineExceeded) {
			return nil, fmt.Errorf("jq expression execution timed out after %v", DefaultJQExecutionTimeout)
		}
		if errors.Is(errVal, context.Canceled) {
			return nil, fmt.Errorf("jq expression execution cancelled: %w", errVal)
		}
		return nil, fmt.Errorf("jq path query returned error: %w", errVal)
	}

	rawPaths, ok := first.([]interface{})
	if !ok {
		return nil, fmt.Errorf("jq path query result is not a valid path list")
	}

	var paths [][]interface{}
	for _, p := range rawPaths {
		if path, ok := p.([]interface{}); ok {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// getValueByIdentity walks the live object following the payload-derived path, resolving list
// indices by matching the payload element's name key, and returns the live value if the full
// path exists on live.
func getValueByIdentity(live, payload interface{}, path []interface{}) (interface{}, bool) {
	liveCurrent, payloadCurrent := live, payload
	for _, seg := range path {
		switch s := seg.(type) {
		case string:
			m, ok := liveCurrent.(map[string]interface{})
			if !ok {
				return nil, false
			}
			if liveCurrent, ok = m[s]; !ok {
				return nil, false
			}
			payloadCurrent = mapValue(payloadCurrent, s)
		case int:
			liveList, ok := liveCurrent.([]interface{})
			if !ok {
				return nil, false
			}
			var payloadElem interface{}
			if pl, ok := payloadCurrent.([]interface{}); ok && s >= 0 && s < len(pl) {
				payloadElem = pl[s]
			}
			index := s
			if payloadMap, isMap := payloadElem.(map[string]interface{}); isMap {
				if _, named := payloadMap["name"]; named {
					// named elements are matched by identity only: without a live match the
					// element is missing on live and the positional live element would be a
					// different element
					j, found := indexOfMapByName(liveList, payloadElem)
					if !found {
						return nil, false
					}
					index = j
				}
			}
			if index < 0 || index >= len(liveList) {
				return nil, false
			}
			liveCurrent = liveList[index]
			payloadCurrent = payloadElem
		}
	}
	return liveCurrent, true
}

// removeValueAtPath removes the map key at the path from the payload. Paths ending at a list
// index are left untouched: an element missing on live keeps its manifest value.
func removeValueAtPath(payload map[string]interface{}, path []interface{}) {
	if len(path) == 0 {
		return
	}
	key, ok := path[len(path)-1].(string)
	if !ok {
		return
	}
	parent, found := getValueAtPath(payload, path[:len(path)-1])
	if !found {
		return
	}
	if m, ok := parent.(map[string]interface{}); ok {
		delete(m, key)
	}
}

// overlaySetByIdentity sets the live value at the path on the payload. List elements are
// matched by their name key: a named live element without a name match in the payload list is
// appended whole, so a spoke-only element is preserved under its own identity instead of
// overwriting an unrelated element at the same index. Unnamed elements fall back to their
// position. It reports whether the payload changed, so callers can avoid writing back
// containers (potentially synthesized empty ones) when the overlay was skipped.
func overlaySetByIdentity(payload, live interface{}, path []interface{}, value interface{}) (interface{}, bool) {
	if len(path) == 0 {
		return value, true
	}
	switch seg := path[0].(type) {
	case string:
		m, ok := payload.(map[string]interface{})
		if !ok {
			m = map[string]interface{}{}
		}
		child, changed := overlaySetByIdentity(m[seg], mapValue(live, seg), path[1:], value)
		if !changed {
			return payload, false
		}
		m[seg] = child
		return m, true
	case int:
		var liveElem interface{}
		if liveList, ok := live.([]interface{}); ok && seg >= 0 && seg < len(liveList) {
			liveElem = liveList[seg]
		}
		s, ok := payload.([]interface{})
		if !ok {
			s = []interface{}{}
		}
		if index, found := indexOfMapByName(s, liveElem); found {
			if len(path) == 1 {
				s[index] = value
				return s, true
			}
			child, changed := overlaySetByIdentity(s[index], liveElem, path[1:], value)
			if !changed {
				return payload, false
			}
			s[index] = child
			return s, true
		}
		named := false
		if liveMap, isMap := liveElem.(map[string]interface{}); isMap {
			name, ok := liveMap["name"].(string)
			named = ok && name != ""
		}
		if seg >= 0 && seg < len(s) && !named {
			// unnamed elements are matched by position; a named element never falls
			// back to position, since the element at that position is a different one
			if len(path) == 1 {
				s[seg] = value
				return s, true
			}
			child, changed := overlaySetByIdentity(s[seg], liveElem, path[1:], value)
			if !changed {
				return payload, false
			}
			s[seg] = child
			return s, true
		}
		if len(path) == 1 {
			// the selection targets the element itself: carry the complete live element
			// the payload does not have, under its own identity
			return append(s, value), true
		}
		// a leaf inside an element the payload does not have is not carried: the
		// element's lifecycle is governed by the manifest or its own field manager, so
		// carrying it would defeat removals of the element from the manifest. Report the
		// original container with changed=false so no empty parents are written back.
		return payload, false
	}
	return payload, false
}

// mapValue returns the value of the key when the container is a map, or nil otherwise.
func mapValue(container interface{}, key string) interface{} {
	if m, ok := container.(map[string]interface{}); ok {
		return m[key]
	}
	return nil
}

// indexOfMapByName returns the index of the unique list element that has the same name as
// the given element, or false when the element has no name or the name is not unique.
func indexOfMapByName(list []interface{}, elem interface{}) (int, bool) {
	elemMap, ok := elem.(map[string]interface{})
	if !ok {
		return -1, false
	}
	name, ok := elemMap["name"].(string)
	if !ok || name == "" {
		return -1, false
	}
	found := -1
	for i, item := range list {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if itemName, ok := itemMap["name"].(string); ok && itemName == name {
			if found >= 0 {
				return -1, false
			}
			found = i
		}
	}
	return found, found >= 0
}

// getValueAtPath walks the object following the typed path (string map keys, int slice
// indices) and returns the value if the full path exists.
func getValueAtPath(obj interface{}, path []interface{}) (interface{}, bool) {
	current := obj
	for _, seg := range path {
		switch s := seg.(type) {
		case string:
			m, ok := current.(map[string]interface{})
			if !ok {
				return nil, false
			}
			if current, ok = m[s]; !ok {
				return nil, false
			}
		case int:
			l, ok := current.([]interface{})
			if !ok || s < 0 || s >= len(l) {
				return nil, false
			}
			current = l[s]
		default:
			return nil, false
		}
	}
	return current, true
}

// setValueAtPath sets the value at the typed path on the container, creating intermediate
// maps and slices as needed. Slice indices at or beyond the current length are appended.
func setValueAtPath(container interface{}, path []interface{}, value interface{}) interface{} {
	if len(path) == 0 {
		return value
	}
	switch seg := path[0].(type) {
	case string:
		m, ok := container.(map[string]interface{})
		if !ok {
			m = map[string]interface{}{}
		}
		m[seg] = setValueAtPath(m[seg], path[1:], value)
		return m
	case int:
		s, ok := container.([]interface{})
		if !ok {
			s = []interface{}{}
		}
		if seg >= 0 && seg < len(s) {
			s[seg] = setValueAtPath(s[seg], path[1:], value)
			return s
		}
		return append(s, setValueAtPath(nil, path[1:], value))
	}
	return container
}

// detect changes in a resource by caching a hash of the string representation of the resource
// note: some changes in a resource e.g. nil vs empty, will not be detected this way
func hashOfResourceStruct(o interface{}) string {
	oString := fmt.Sprintf("%v", o)
	h := md5.New() //nolint:gosec
	if _, err := io.WriteString(h, oString); err != nil {
		return ""
	}
	rval := fmt.Sprintf("%x", h.Sum(nil))
	return rval
}

func removeCreationTimeFromMetadata(obj map[string]interface{}, logger klog.Logger) {
	if metadata, found := obj["metadata"]; found {
		if metaObj, ok := metadata.(map[string]interface{}); ok {
			creationTimestamp, ok := metaObj["creationTimestamp"]
			if ok && creationTimestamp == nil {
				unstructured.RemoveNestedField(metaObj, "creationTimestamp")
			}
		}
	}

	for _, v := range obj {
		switch val := v.(type) {
		case map[string]interface{}:
			removeCreationTimeFromMetadata(val, logger)
		case []interface{}:
			for _, item := range val {
				if itemObj, ok := item.(map[string]interface{}); ok {
					removeCreationTimeFromMetadata(itemObj, logger)
				}
			}
		}
	}
}
