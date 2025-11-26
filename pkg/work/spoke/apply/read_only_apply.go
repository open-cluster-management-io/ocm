package apply

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
)

type ReadOnlyApply struct{}

func NewReadOnlyApply() *ReadOnlyApply {
	return &ReadOnlyApply{}
}

func (c *ReadOnlyApply) Apply(ctx context.Context,
	gvr schema.GroupVersionResource,
	required *unstructured.Unstructured,
	_ metav1.OwnerReference,
	_ *workapiv1.ManifestConfigOption,
	_ events.Recorder) (runtime.Object, error) {
	logger := klog.FromContext(ctx)
	logger.Info("Noop because its read-only",
		"gvr", gvr.String(), "resourceNamespace", required.GetNamespace(), "resourceName", required.GetName())
	return required, nil
}
