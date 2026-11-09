/* Copyright Contributors to the Open Cluster Management project */

package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

type SARAuthorizer struct {
	client kubernetes.Interface
}

func NewSARAuthorizer(client kubernetes.Interface) *SARAuthorizer {
	return &SARAuthorizer{client: client}
}

func (a *SARAuthorizer) Authorize(ctx context.Context, r *http.Request, verb, group, version, namespace, resource, subresource, name string) error {
	user := r.Header.Get("X-Remote-User")
	if user == "" {
		return apierrors.NewUnauthorized("aggregator did not provide X-Remote-User")
	}
	groups := append([]string(nil), r.Header.Values("X-Remote-Group")...)
	for i := range groups {
		groups[i] = strings.TrimSpace(groups[i])
	}
	groups = compact(groups)
	review, err := a.client.AuthorizationV1().SubjectAccessReviews().Create(ctx, &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   user,
			Groups: groups,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb:        verb,
				Group:       group,
				Version:     version,
				Resource:    resource,
				Subresource: subresource,
				Namespace:   namespace,
				Name:        name,
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("request delegated authorization: %w", err)
	}
	if !review.Status.Allowed {
		return apierrors.NewForbidden(schema.GroupResource{Group: group, Resource: resource}, name,
			fmt.Errorf("request denied by delegated authorization (reason: %s)", review.Status.Reason))
	}
	return nil
}

func compact(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
