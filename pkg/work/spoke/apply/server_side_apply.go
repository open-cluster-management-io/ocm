package apply

import (
	"context"
	"crypto/md5" //nolint:gosec
	"fmt"
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
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

	required := requiredOriginal.DeepCopy()

	if applyOption.UpdateStrategy.ServerSideApply != nil {
		force = applyOption.UpdateStrategy.ServerSideApply.Force
		if len(applyOption.UpdateStrategy.ServerSideApply.FieldManager) > 0 {
			fieldManager = applyOption.UpdateStrategy.ServerSideApply.FieldManager
		}

		ignoreFields := applyOption.UpdateStrategy.ServerSideApply.IgnoreFields
		if len(ignoreFields) > 0 {
			for _, field := range ignoreFields {
				// for IgnoreFieldsConditionOnSpokeChange, it will still be included when computing the hash. So when
				// hash dismatch, these fields will still the patched on the cluster.
				if field.Condition == workapiv1.IgnoreFieldsConditionOnSpokeChange {
					continue
				}
				for _, path := range field.JSONPaths {
					removeFieldByJSONPath(required.UnstructuredContent(), path, logger)
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
		case errors.IsNotFound(err):
			// if object is not found, use requiredOriginal to apply so the ignore fields are kept when create
			required = requiredOriginal
		case err != nil:
			return nil, err
		case len(existing.GetAnnotations()) > 0:
			// skip the apply operation when the hash of the existing resource matches the required hash
			existingHash := existing.GetAnnotations()[workapiv1.ManifestConfigSpecHashAnnotationKey]
			if requiredHash == existingHash {
				// still needs to apply ownerref since it might be changed due to deleteoption update.
				err := helper.ApplyOwnerReferences(ctx, c.client, gvr, existing, owner)
				return existing, err
			}
		}
	}

	obj, err := c.client.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Apply(ctx, required.GetName(), required, metav1.ApplyOptions{FieldManager: fieldManager, Force: force})
	logger.Info("Server side applied",
		"gvr", gvr.String(), "resourceNamespace", required.GetNamespace(),
		"resourceName", required.GetName(), "fieldManager", fieldManager)

	if errors.IsConflict(err) {
		return obj, &ServerSideApplyConflictError{ssaErr: err}
	}

	if err == nil {
		err = helper.ApplyOwnerReferences(ctx, c.client, gvr, obj, owner)
	}

	return obj, err
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
