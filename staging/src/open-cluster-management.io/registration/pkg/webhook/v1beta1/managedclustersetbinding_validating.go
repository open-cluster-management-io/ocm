package v1beta1

import (
	"context"
	"fmt"

	authenticationv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"open-cluster-management.io/api/cluster/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ webhook.CustomValidator = &ManagedClusterSetBindingWebhook{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (b *ManagedClusterSetBindingWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	binding, ok := obj.(*v1beta1.ManagedClusterSetBinding)
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
	return AllowBindingToClusterSet(b.kubeClient, binding.Spec.ClusterSet, req.UserInfo)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (b *ManagedClusterSetBindingWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	binding, ok := newObj.(*v1beta1.ManagedClusterSetBinding)
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
