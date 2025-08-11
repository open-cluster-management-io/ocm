package sar

import (
	"context"
	"fmt"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authn"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authz"
)

type SARAuthorizer struct {
	kubeClient kubernetes.Interface
}

func NewSARAuthorizer(kubeClient kubernetes.Interface) authz.Authorizer {
	return &SARAuthorizer{
		kubeClient: kubeClient,
	}
}

func (s *SARAuthorizer) Authorize(ctx context.Context, cluster string, eventsType types.CloudEventsType) error {
	user, groups, err := userInfo(ctx)
	if err != nil {
		return err
	}

	sar, err := toSubjectAccessReview(cluster, user, groups, eventsType)
	if err != nil {
		return err
	}

	created, err := s.kubeClient.AuthorizationV1().SubjectAccessReviews().Create(
		ctx, sar, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	if !created.Status.Allowed {
		return fmt.Errorf("the event %s is not allowed, (cluster=%s, sar=%v, reason=%v)",
			eventsType, cluster, sar.Spec, created.Status)
	}
	return nil
}

func userInfo(ctx context.Context) (user string, groups []string, err error) {
	userValue := ctx.Value(authn.ContextUserKey)
	groupsValue := ctx.Value(authn.ContextGroupsKey)
	if userValue == nil && groupsValue == nil {
		return user, groups, fmt.Errorf("no user and groups in context")
	}

	if userValue != nil {
		var ok bool
		user, ok = userValue.(string)
		if !ok {
			return user, groups, fmt.Errorf("invalid user type in context")
		}
	}

	if groupsValue != nil {
		var ok bool
		groups, ok = groupsValue.([]string)
		if !ok {
			return user, groups, fmt.Errorf("invalid groups in context")
		}
	}

	return user, groups, nil
}

func toSubjectAccessReview(clusterName string, user string, groups []string, eventsType types.CloudEventsType) (*authv1.SubjectAccessReview, error) {
	verb, err := toVerb(eventsType.Action)
	if err != nil {
		return nil, err
	}

	sar := &authv1.SubjectAccessReview{
		Spec: authv1.SubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Verb:      verb,
				Namespace: clusterName,
			},
			Groups: groups,
		},
	}

	if len(sar.Spec.Groups) == 0 {
		sar.Spec.User = user
	}

	if eventsType.SubResource == types.SubResourceStatus {
		sar.Spec.ResourceAttributes.Subresource = "status"
	}

	switch eventsType.CloudEventsDataType {
	case cluster.ManagedClusterEventDataType:
		sar.Spec.ResourceAttributes.Group = eventsType.Group
		sar.Spec.ResourceAttributes.Resource = eventsType.Resource
		sar.Spec.ResourceAttributes.Name = clusterName
		return sar, nil
	case addon.ManagedClusterAddOnEventDataType,
		csr.CSREventDataType,
		event.EventEventDataType,
		lease.LeaseEventDataType:
		sar.Spec.ResourceAttributes.Group = eventsType.Group
		sar.Spec.ResourceAttributes.Resource = eventsType.Resource
		return sar, nil
	case payload.ManifestBundleEventDataType:
		sar.Spec.ResourceAttributes.Group = workv1.SchemeGroupVersion.Group
		sar.Spec.ResourceAttributes.Resource = "manifestworks"
		return sar, nil
	default:
		return nil, fmt.Errorf("unsupported event type %s", eventsType.CloudEventsDataType)
	}
}

func toVerb(action types.EventAction) (string, error) {
	switch action {
	case types.CreateRequestAction:
		return "create", nil
	case types.UpdateRequestAction:
		return "update", nil
	case types.DeleteRequestAction:
		return "delete", nil
	case types.WatchRequestAction:
		return "watch", nil
	case types.ResyncRequestAction:
		return "list", nil
	default:
		return "", fmt.Errorf("unsupported action %s", action)
	}
}
