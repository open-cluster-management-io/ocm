package admissionreview

import (
	"context"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/rest"
)

type AdmissionV1HookFunc func(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse

type V1REST struct {
	hookFn AdmissionV1HookFunc
}

var _ rest.Creater = &REST{}
var _ rest.Scoper = &REST{}
var _ rest.GroupVersionKindProvider = &REST{}

func NewV1REST(hookFn AdmissionV1HookFunc) *V1REST {
	return &V1REST{
		hookFn: hookFn,
	}
}

func (r *V1REST) New() runtime.Object {
	return &admissionv1.AdmissionReview{}
}

func (r *V1REST) GroupVersionKind(containingGV schema.GroupVersion) schema.GroupVersionKind {
	return admissionv1.SchemeGroupVersion.WithKind("AdmissionReview")
}

func (r *V1REST) NamespaceScoped() bool {
	return false
}

func (r *V1REST) Create(ctx context.Context, obj runtime.Object, _ rest.ValidateObjectFunc, _ *metav1.CreateOptions) (runtime.Object, error) {
	admissionReview := obj.(*admissionv1.AdmissionReview)
	admissionReview.Response = r.hookFn(admissionReview.Request)
	// Copey request uid to response
	admissionReview.Response.UID = admissionReview.Request.UID
	return admissionReview, nil
}
