package apply

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	workapiv1 "open-cluster-management.io/api/work/v1"
)

type ReadOnlyApply struct{}

func NewReadOnlyApply() *ReadOnlyApply {
	return &ReadOnlyApply{}
}

func (c *ReadOnlyApply) Apply(ctx context.Context,
	_ schema.GroupVersionResource,
	required *unstructured.Unstructured,
	_ metav1.OwnerReference,
	_ *workapiv1.ManifestConfigOption,
	recorder events.Recorder) (runtime.Object, error) {

	recorder.Eventf(fmt.Sprintf(
		"%s noop", required.GetKind()), "Noop for %s/%s because its read-only", required.GetNamespace(), required.GetName())
	return required, nil
}
