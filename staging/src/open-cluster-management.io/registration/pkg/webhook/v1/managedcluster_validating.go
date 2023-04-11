package v1

import (
	"context"
	"fmt"
	"strings"

	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	v1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/helpers"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ webhook.CustomValidator = &ManagedClusterWebhook{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ManagedClusterWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	managedCluster, ok := obj.(*v1.ManagedCluster)
	if !ok {
		return apierrors.NewBadRequest("Request cluster obj format is not right")
	}
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	//Validate if Spec.ManagedClusterClientConfigs is Valid HTTPS URL
	err = r.validateManagedClusterObj(*managedCluster)
	if err != nil {
		return err
	}

	// the HubAcceptsClient field is changed, we need to check the request user whether
	// has been allowed to change the HubAcceptsClient field with SubjectAccessReview api
	if managedCluster.Spec.HubAcceptsClient {
		err := r.allowUpdateAcceptField(managedCluster.Name, req.UserInfo)
		if err != nil {
			return err
		}
	}

	// check whether the request user has been allowed to set clusterset label
	var clusterSetName string
	if len(managedCluster.Labels) > 0 {
		clusterSetName = managedCluster.Labels[clusterv1beta2.ClusterSetLabel]
	}

	return r.allowSetClusterSetLabel(req.UserInfo, "", clusterSetName)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ManagedClusterWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	managedCluster, ok := newObj.(*v1.ManagedCluster)
	if !ok {
		return apierrors.NewBadRequest("Request new cluster obj format is not right")
	}
	oldManagedCluster, ok := oldObj.(*v1.ManagedCluster)
	if !ok {
		return apierrors.NewBadRequest("Request old cluster obj format is not right")
	}
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}

	//Validate if Spec.ManagedClusterClientConfigs is Valid HTTPS URL
	err = r.validateManagedClusterObj(*managedCluster)
	if err != nil {
		return err
	}

	// the HubAcceptsClient field is changed, we need to check the request user whether
	// has been allowed to change the HubAcceptsClient field with SubjectAccessReview api
	if managedCluster.Spec.HubAcceptsClient != oldManagedCluster.Spec.HubAcceptsClient {
		if managedCluster.Spec.HubAcceptsClient {
			err := r.allowUpdateAcceptField(managedCluster.Name, req.UserInfo)
			if err != nil {
				return err
			}
		}
	}

	// check whether the request user has been allowed to set clusterset label
	var originalClusterSetName, currentClusterSetName string
	if len(oldManagedCluster.Labels) > 0 {
		originalClusterSetName = oldManagedCluster.Labels[clusterv1beta2.ClusterSetLabel]
	}
	if len(managedCluster.Labels) > 0 {
		currentClusterSetName = managedCluster.Labels[clusterv1beta2.ClusterSetLabel]
	}

	return r.allowSetClusterSetLabel(req.UserInfo, originalClusterSetName, currentClusterSetName)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ManagedClusterWebhook) ValidateDelete(_ context.Context, obj runtime.Object) error {
	return nil
}

// validateManagedClusterObj validates the fileds of ManagedCluster object
func (r *ManagedClusterWebhook) validateManagedClusterObj(cluster v1.ManagedCluster) error {
	errs := []error{}
	// The cluster name must be the same format of namespace name.
	if errMsgs := apimachineryvalidation.ValidateNamespaceName(cluster.Name, false); len(errMsgs) > 0 {
		errs = append(errs, fmt.Errorf("metadata.name format is not correct: %s", strings.Join(errMsgs, ",")))
	}
	// there are no spoke client configs, finish the validation process
	if len(cluster.Spec.ManagedClusterClientConfigs) == 0 {
		return nil
	}

	// validate the url in spoke client configs
	for _, clientConfig := range cluster.Spec.ManagedClusterClientConfigs {
		if !helpers.IsValidHTTPSURL(clientConfig.URL) {
			errs = append(errs, fmt.Errorf("url %q is invalid in client configs", clientConfig.URL))
		}
	}
	if len(errs) != 0 {
		return apierrors.NewBadRequest(operatorhelpers.NewMultiLineAggregate(errs).Error())
	}
	return nil
}

// allowUpdateHubAcceptsClientField using SubjectAccessReview API to check whether a request user has been authorized to update
// HubAcceptsClient field
func (r *ManagedClusterWebhook) allowUpdateAcceptField(clusterName string, userInfo authenticationv1.UserInfo) error {
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
				Group:       "register.open-cluster-management.io",
				Resource:    "managedclusters",
				Verb:        "update",
				Subresource: "accept",
				Name:        clusterName,
			},
		},
	}
	sar, err := r.kubeClient.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		return apierrors.NewForbidden(
			v1.Resource("managedclusters/accept"),
			clusterName,
			err,
		)
	}

	if !sar.Status.Allowed {
		return apierrors.NewForbidden(
			v1.Resource("managedclusters/accept"),
			clusterName,
			fmt.Errorf("user %q cannot update the HubAcceptsClient field", userInfo.Username),
		)
	}

	return nil
}

// allowSetClusterSetLabel checks whether a request user has been authorized to set clusterset label
func (r *ManagedClusterWebhook) allowSetClusterSetLabel(userInfo authenticationv1.UserInfo, originalClusterSet, newClusterSet string) error {
	if originalClusterSet == newClusterSet {
		return nil
	}

	if len(originalClusterSet) > 0 {
		err := r.allowUpdateClusterSet(userInfo, originalClusterSet)
		if err != nil {
			return err
		}
	}

	if len(newClusterSet) > 0 {
		err := r.allowUpdateClusterSet(userInfo, newClusterSet)
		if err != nil {
			return err
		}
	}

	return nil
}

// allowUpdateClusterSet checks whether a request user has been authorized to add/remove a ManagedCluster
// to/from the ManagedClusterSet
func (r *ManagedClusterWebhook) allowUpdateClusterSet(userInfo authenticationv1.UserInfo, clusterSetName string) error {
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
				Subresource: "join",
				Name:        clusterSetName,
				Verb:        "create",
			},
		},
	}
	sar, err := r.kubeClient.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		return apierrors.NewForbidden(
			v1.Resource("managedclustersets/join"),
			clusterSetName,
			err,
		)
	}

	if !sar.Status.Allowed {
		return apierrors.NewForbidden(
			v1.Resource("managedclustersets/join"),
			clusterSetName,
			fmt.Errorf("user %q cannot add/remove a ManagedCluster to/from ManagedClusterSet %q", userInfo.Username, clusterSetName),
		)
	}

	return nil
}
