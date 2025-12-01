package apply

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	"open-cluster-management.io/ocm/pkg/work/helper"
)

type CreateOnlyApply struct {
	client dynamic.Interface
}

func NewCreateOnlyApply(client dynamic.Interface) *CreateOnlyApply {
	return &CreateOnlyApply{client: client}
}

func (c *CreateOnlyApply) Apply(ctx context.Context,
	gvr schema.GroupVersionResource,
	required *unstructured.Unstructured,
	owner metav1.OwnerReference,
	_ *workapiv1.ManifestConfigOption,
	_ events.Recorder) (runtime.Object, error) {

	logger := klog.FromContext(ctx)
	obj, err := c.client.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Get(ctx, required.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		required.SetOwnerReferences([]metav1.OwnerReference{owner})
		obj, err = c.client.Resource(gvr).Namespace(required.GetNamespace()).Create(
			ctx, resourcemerge.WithCleanLabelsAndAnnotations(required).(*unstructured.Unstructured), metav1.CreateOptions{})
		if err != nil {
			logger.Info("Resource created because of missing",
				"gvr", gvr.String(), "resourceNamespace", required.GetNamespace(), "resourceName", required.GetName())
		}
	}

	if err == nil {
		err = helper.ApplyOwnerReferences(ctx, c.client, gvr, obj, owner)
	}

	return obj, err
}
