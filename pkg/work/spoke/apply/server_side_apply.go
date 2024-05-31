package apply

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	workapiv1 "open-cluster-management.io/api/work/v1"

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
	required *unstructured.Unstructured,
	owner metav1.OwnerReference,
	applyOption *workapiv1.ManifestConfigOption,
	recorder events.Recorder) (runtime.Object, error) {

	force := false
	fieldManager := workapiv1.DefaultFieldManager

	if applyOption.UpdateStrategy.ServerSideApply != nil {
		force = applyOption.UpdateStrategy.ServerSideApply.Force
		if len(applyOption.UpdateStrategy.ServerSideApply.FieldManager) > 0 {
			fieldManager = applyOption.UpdateStrategy.ServerSideApply.FieldManager
		}
	}

	obj, err := c.client.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Apply(ctx, required.GetName(), required, metav1.ApplyOptions{FieldManager: fieldManager, Force: force})
	resourceKey, _ := cache.MetaNamespaceKeyFunc(required)
	if err != nil {
		recorder.Eventf(fmt.Sprintf(
			"Server Side Applied %s %s", required.GetKind(), resourceKey), "Patched with field manager %s", fieldManager)
	}

	if errors.IsConflict(err) {
		return obj, &ServerSideApplyConflictError{ssaErr: err}
	}

	if err == nil {
		err = helper.ApplyOwnerReferences(ctx, c.client, gvr, obj, owner)
	}

	return obj, err
}
