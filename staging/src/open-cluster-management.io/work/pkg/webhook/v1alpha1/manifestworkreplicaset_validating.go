package v1alpha1

import (
	"context"
	"errors"
	"open-cluster-management.io/work/pkg/webhook/common"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	ocmfeature "open-cluster-management.io/api/feature"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ webhook.CustomValidator = &ManifestWorkReplicaSetWebhook{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ManifestWorkReplicaSetWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	mwrSet, ok := obj.(*workv1alpha1.ManifestWorkReplicaSet)
	if !ok {
		return apierrors.NewBadRequest("Request manifestWorkReplicaSet obj format is not right")
	}
	return r.validateRequest(mwrSet, nil, ctx)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ManifestWorkReplicaSetWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	newmwrSet, ok := newObj.(*workv1alpha1.ManifestWorkReplicaSet)
	if !ok {
		return apierrors.NewBadRequest("Request manifestWorkReplicaSet obj format is not right")
	}

	oldmwrSet, ok := oldObj.(*workv1alpha1.ManifestWorkReplicaSet)
	if !ok {
		return apierrors.NewBadRequest("Request manifestWorkReplicaSet obj format is not right")
	}

	return r.validateRequest(newmwrSet, oldmwrSet, ctx)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ManifestWorkReplicaSetWebhook) ValidateDelete(_ context.Context, obj runtime.Object) error {
	if err := checkFeatureEnabled(); err != nil {
		return err
	}

	return nil
}

func (r *ManifestWorkReplicaSetWebhook) validateRequest(newmwrSet *workv1alpha1.ManifestWorkReplicaSet, oldmwrSet *workv1alpha1.ManifestWorkReplicaSet, ctx context.Context) error {
	if err := checkFeatureEnabled(); err != nil {
		return err
	}

	if err := validatePlaceManifests(newmwrSet); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}

	_, err := admission.RequestFromContext(ctx)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}

	return nil
}

func validatePlaceManifests(mwrSet *workv1alpha1.ManifestWorkReplicaSet) error {
	return common.ManifestValidator.ValidateManifests(mwrSet.Spec.ManifestWorkTemplate.Workload.Manifests)
}

func checkFeatureEnabled() error {
	if !utilfeature.DefaultMutableFeatureGate.Enabled(ocmfeature.ManifestWorkReplicaSet) {
		return errors.New("ManifestWorkReplicaSet feature is disabled")
	}

	return nil
}
