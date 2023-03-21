package v1beta2

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/runtime"
	"open-cluster-management.io/api/cluster/v1beta2"
	internalv1beta1 "open-cluster-management.io/registration/pkg/webhook/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ webhook.CustomValidator = &ManagedClusterSetBindingWebhook{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (b *ManagedClusterSetBindingWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	binding, ok := obj.(*v1beta2.ManagedClusterSetBinding)
	if !ok {
		return apierrors.NewBadRequest("Request clustersetbinding obj format is not right")
	}

	// force the instance name to match the target cluster set name
	if binding.Name != binding.Spec.ClusterSet {
		return apierrors.NewBadRequest("The ManagedClusterSetBinding must have the same name as the target ManagedClusterSet")
	}

	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	return internalv1beta1.AllowBindingToClusterSet(b.kubeClient, binding.Spec.ClusterSet, req.UserInfo)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (b *ManagedClusterSetBindingWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	binding, ok := newObj.(*v1beta2.ManagedClusterSetBinding)
	if !ok {
		return apierrors.NewBadRequest("Request clustersetbinding obj format is not right")
	}

	// force the instance name to match the target cluster set name
	if binding.Name != binding.Spec.ClusterSet {
		return apierrors.NewBadRequest("The ManagedClusterSetBinding must have the same name as the target ManagedClusterSet")
	}
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (b *ManagedClusterSetBindingWebhook) ValidateDelete(_ context.Context, obj runtime.Object) error {
	return nil
}
