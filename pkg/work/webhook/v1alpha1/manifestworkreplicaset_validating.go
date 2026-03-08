package v1alpha1

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ocmfeature "open-cluster-management.io/api/feature"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/webhook/common"
)

var _ admission.Validator[*workv1alpha1.ManifestWorkReplicaSet] = &ManifestWorkReplicaSetWebhook{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ManifestWorkReplicaSetWebhook) ValidateCreate(ctx context.Context, mwrSet *workv1alpha1.ManifestWorkReplicaSet) (
	admission.Warnings, error) {
	return nil, r.validateRequest(mwrSet, nil, ctx)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ManifestWorkReplicaSetWebhook) ValidateUpdate(ctx context.Context, oldmwrSet, newmwrSet *workv1alpha1.ManifestWorkReplicaSet) (
	admission.Warnings, error) {
	return nil, r.validateRequest(newmwrSet, oldmwrSet, ctx)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ManifestWorkReplicaSetWebhook) ValidateDelete(_ context.Context, _ *workv1alpha1.ManifestWorkReplicaSet) (
	admission.Warnings, error) {
	if err := checkFeatureEnabled(); err != nil {
		return nil, err
	}

	return nil, nil
}

func (r *ManifestWorkReplicaSetWebhook) validateRequest(
	newmwrSet *workv1alpha1.ManifestWorkReplicaSet, _ *workv1alpha1.ManifestWorkReplicaSet,
	ctx context.Context) error {
	if err := checkFeatureEnabled(); err != nil {
		return err
	}

	if newmwrSet == nil {
		return fmt.Errorf("new manifestwork replica set is nil")
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
	if !features.HubMutableFeatureGate.Enabled(ocmfeature.ManifestWorkReplicaSet) {
		return errors.New("ManifestWorkReplicaSet feature is disabled")
	}

	return nil
}
