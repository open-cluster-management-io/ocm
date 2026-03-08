package v1beta2

import (
	"context"
	"fmt"

	"k8s.io/api/apps/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"open-cluster-management.io/api/cluster/v1beta2"
)

var _ admission.Validator[*v1beta2.ManagedClusterSetBinding] = &ManagedClusterSetBindingWebhook{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (b *ManagedClusterSetBindingWebhook) ValidateCreate(ctx context.Context, binding *v1beta2.ManagedClusterSetBinding) (
	admission.Warnings, error) {

	// force the instance name to match the target cluster set name
	if binding.Name != binding.Spec.ClusterSet {
		return nil, apierrors.NewBadRequest("The ManagedClusterSetBinding must have the same name as the target ManagedClusterSet")
	}

	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	return nil, AllowBindingToClusterSet(b.kubeClient, binding.Spec.ClusterSet, req.UserInfo)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (b *ManagedClusterSetBindingWebhook) ValidateUpdate(_ context.Context, _, newBinding *v1beta2.ManagedClusterSetBinding) (
	admission.Warnings, error) {
	// force the instance name to match the target cluster set name
	if newBinding.Name != newBinding.Spec.ClusterSet {
		return nil, apierrors.NewBadRequest("The ManagedClusterSetBinding must have the same name as the target ManagedClusterSet")
	}
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (b *ManagedClusterSetBindingWebhook) ValidateDelete(_ context.Context, _ *v1beta2.ManagedClusterSetBinding) (
	admission.Warnings, error) {
	return nil, nil
}

// allowBindingToClusterSet checks if the user has permission to bind a particular cluster set
func AllowBindingToClusterSet(kubeClient kubernetes.Interface, clusterSetName string, userInfo authenticationv1.UserInfo) error {
	extra := make(map[string]authorizationv1.ExtraValue)
	for k, v := range userInfo.Extra {
		extra[k] = authorizationv1.ExtraValue(v)
	}

	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   userInfo.Username,
			UID:    userInfo.UID,
			Groups: userInfo.Groups,
			Extra:  extra,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Group:       "cluster.open-cluster-management.io",
				Resource:    "managedclustersets",
				Subresource: "bind",
				Verb:        "create",
				Name:        clusterSetName,
			},
		},
	}
	sar, err := kubeClient.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		return apierrors.NewForbidden(
			v1beta1.Resource("managedclustersets/bind"),
			clusterSetName,
			err,
		)
	}
	if !sar.Status.Allowed {
		return apierrors.NewForbidden(
			v1beta1.Resource("managedclustersets/bind"),
			clusterSetName,
			fmt.Errorf("user %q is not allowed to bind cluster set %q", userInfo.Username, clusterSetName),
		)
	}
	return nil
}
