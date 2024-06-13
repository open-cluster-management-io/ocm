package helpers

import (
	"context"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CreateSelfSubjectAccessReviews(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	selfSubjectAccessReviews []authorizationv1.SelfSubjectAccessReview) (bool, *authorizationv1.SelfSubjectAccessReview, error) {

	for i := range selfSubjectAccessReviews {
		subjectAccessReview := selfSubjectAccessReviews[i]

		ssar, err := kubeClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &subjectAccessReview, metav1.CreateOptions{})
		if err != nil {
			return false, &subjectAccessReview, err
		}
		if !ssar.Status.Allowed {
			return false, &subjectAccessReview, nil
		}
	}
	return true, nil, nil
}

func GetBootstrapSSARs() []authorizationv1.SelfSubjectAccessReview {
	var reviews []authorizationv1.SelfSubjectAccessReview
	clusterResource := authorizationv1.ResourceAttributes{
		Group:    "cluster.open-cluster-management.io",
		Resource: "managedclusters",
		// TODO: add the resourceName @xuezhaojun https://github.com/open-cluster-management-io/ocm/pull/443#discussion_r1609202000
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(clusterResource, "create", "get")...)

	certResource := authorizationv1.ResourceAttributes{
		Group:    "certificates.k8s.io",
		Resource: "certificatesigningrequests",
	}
	return append(reviews, generateSelfSubjectAccessReviews(certResource, "create", "get", "list", "watch")...)
}

func GetHubConfigSSARs(clusterName string) []authorizationv1.SelfSubjectAccessReview {
	var reviews []authorizationv1.SelfSubjectAccessReview
	// registration resources
	certResource := authorizationv1.ResourceAttributes{
		Group:    "certificates.k8s.io",
		Resource: "certificatesigningrequests",
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(certResource, "get", "list", "watch")...)

	clusterResource := authorizationv1.ResourceAttributes{
		Group:    "cluster.open-cluster-management.io",
		Resource: "managedclusters",
		Name:     clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(clusterResource, "get", "list", "update", "watch")...)

	clusterStatusResource := authorizationv1.ResourceAttributes{
		Group:       "cluster.open-cluster-management.io",
		Resource:    "managedclusters",
		Subresource: "status",
		Name:        clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(clusterStatusResource, "patch", "update")...)

	clusterCertResource := authorizationv1.ResourceAttributes{
		Group:       "register.open-cluster-management.io",
		Resource:    "managedclusters",
		Subresource: "clientcertificates",
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(clusterCertResource, "renew")...)

	leaseResource := authorizationv1.ResourceAttributes{
		Group:     "coordination.k8s.io",
		Resource:  "leases",
		Name:      "managed-cluster-lease",
		Namespace: clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(leaseResource, "get", "update")...)

	// work resources
	eventResource := authorizationv1.ResourceAttributes{
		Resource:  "events",
		Namespace: clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(eventResource, "create", "patch", "update")...)

	eventResource = authorizationv1.ResourceAttributes{
		Group:     "events.k8s.io",
		Resource:  "events",
		Namespace: clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(eventResource, "create", "patch", "update")...)

	workResource := authorizationv1.ResourceAttributes{
		Group:     "work.open-cluster-management.io",
		Resource:  "manifestworks",
		Namespace: clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(workResource, "get", "list", "watch", "update")...)

	workStatusResource := authorizationv1.ResourceAttributes{
		Group:       "work.open-cluster-management.io",
		Resource:    "manifestworks",
		Subresource: "status",
		Namespace:   clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(workStatusResource, "patch", "update")...)
	return reviews
}

func generateSelfSubjectAccessReviews(resource authorizationv1.ResourceAttributes, verbs ...string) []authorizationv1.SelfSubjectAccessReview {
	var reviews []authorizationv1.SelfSubjectAccessReview
	for _, verb := range verbs {
		reviews = append(reviews, authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Group:       resource.Group,
					Resource:    resource.Resource,
					Subresource: resource.Subresource,
					Name:        resource.Name,
					Namespace:   resource.Namespace,
					Verb:        verb,
				},
			},
		})
	}
	return reviews
}
