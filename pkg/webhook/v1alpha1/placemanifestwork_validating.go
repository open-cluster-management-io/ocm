package v1alpha1

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	webhookv1 "open-cluster-management.io/work/pkg/webhook/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ webhook.CustomValidator = &PlaceManifestWorkWebhook{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *PlaceManifestWorkWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	placeWork, ok := obj.(*workv1alpha1.PlaceManifestWork)
	if !ok {
		return apierrors.NewBadRequest("Request placeManifestwork obj format is not right")
	}
	return r.validateRequest(placeWork, nil, ctx)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *PlaceManifestWorkWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	newPlaceWork, ok := newObj.(*workv1alpha1.PlaceManifestWork)
	if !ok {
		return apierrors.NewBadRequest("Request placeManifestwork obj format is not right")
	}

	oldPlaceWork, ok := oldObj.(*workv1alpha1.PlaceManifestWork)
	if !ok {
		return apierrors.NewBadRequest("Request placeManifestwork obj format is not right")
	}

	return r.validateRequest(newPlaceWork, oldPlaceWork, ctx)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *PlaceManifestWorkWebhook) ValidateDelete(_ context.Context, obj runtime.Object) error {
	return nil
}

func (r *PlaceManifestWorkWebhook) validateRequest(newPlaceWork *workv1alpha1.PlaceManifestWork, oldPlaceWork *workv1alpha1.PlaceManifestWork, ctx context.Context) error {
	if err := validatePlaceManifests(newPlaceWork); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}

	_, err := admission.RequestFromContext(ctx)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}

	return nil
}

func validatePlaceManifests(placeWork *workv1alpha1.PlaceManifestWork) error {
	return webhookv1.ValidateManifests(placeWork.Spec.ManifestWorkTemplate.Workload.Manifests)
}
