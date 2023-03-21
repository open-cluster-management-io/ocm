package basic

import (
	"context"
	"fmt"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	workapiv1 "open-cluster-management.io/api/work/v1"
)

type NotAllowedError struct {
	Err         error
	RequeueTime time.Duration
}

func (e *NotAllowedError) Error() string {
	err := e.Err.Error()
	if e.RequeueTime > 0 {
		err = fmt.Sprintf("%s, will try again in %s", err, e.RequeueTime.String())
	}
	return err
}

// NewSARValidator creates a SARValidator
func NewSARValidator(config *rest.Config, kubeClient kubernetes.Interface) *SarValidator {
	return &SarValidator{
		kubeClient:               kubeClient,
		config:                   config,
		newImpersonateClientFunc: defaultNewImpersonateClient,
	}
}

type SarValidator struct {
	kubeClient               kubernetes.Interface
	config                   *rest.Config
	newImpersonateClientFunc newImpersonateClient
}

type newImpersonateClient func(config *rest.Config, username string) (dynamic.Interface, error)

func defaultNewImpersonateClient(config *rest.Config, username string) (dynamic.Interface, error) {
	if config == nil {
		return nil, fmt.Errorf("kube config should not be nil")
	}
	impersonatedConfig := *config
	impersonatedConfig.Impersonate.UserName = username
	return dynamic.NewForConfig(&impersonatedConfig)
}

// Validate checks whether the executor has permission to operate the specific gvr resource by
// sending sar requests to the api server.
func (v *SarValidator) Validate(ctx context.Context, executor *workapiv1.ManifestWorkExecutor,
	gvr schema.GroupVersionResource, namespace, name string,
	ownedByTheWork bool, obj *unstructured.Unstructured) error {
	if executor == nil {
		return nil
	}

	if err := v.ExecutorBasicCheck(executor); err != nil {
		return err
	}

	if err := v.CheckSubjectAccessReviews(ctx, executor.Subject.ServiceAccount,
		gvr, namespace, name, ownedByTheWork); err != nil {
		return err
	}

	// subjectaccessreview can not check permission escalation, use an impersonation request to check again
	return v.CheckEscalation(ctx, executor.Subject.ServiceAccount, gvr, namespace, name, obj)
}

// ExecutorBasicCheck do some basic checks for the executor
func (v *SarValidator) ExecutorBasicCheck(executor *workapiv1.ManifestWorkExecutor) error {
	if executor.Subject.Type != workapiv1.ExecutorSubjectTypeServiceAccount {
		return fmt.Errorf("only support %s type for the executor", workapiv1.ExecutorSubjectTypeServiceAccount)
	}

	sa := executor.Subject.ServiceAccount
	if sa == nil {
		return fmt.Errorf("the executor service account is nil")
	}

	return nil
}

// CheckSubjectAccessReviews checks if the sa has permission to operate the gvr resource by subjectAccessReview requests
func (v *SarValidator) CheckSubjectAccessReviews(ctx context.Context, sa *workapiv1.ManifestWorkSubjectServiceAccount,
	gvr schema.GroupVersionResource, namespace, name string, ownedByTheWork bool) error {

	verbs := []string{"create", "update", "patch", "get"}
	if ownedByTheWork {
		// if the resource to be applied is owned by the manifestwork, will check the delete permission in
		// the applying phase in advance. it means the resource will be applied if the executor has the
		// delete permission when applying. and even if the delete permission of the executor is revoked
		// after applying success, the resource will also be deleted when deleting the manifestwork
		verbs = append(verbs, "delete")
	}

	resource := authorizationv1.ResourceAttributes{
		Namespace: namespace,
		Name:      name,
		Group:     gvr.Group,
		Version:   gvr.Version,
		Resource:  gvr.Resource,
	}

	reviews := buildSubjectAccessReviews(sa.Namespace, sa.Name, resource, verbs...)
	allowed, err := validateBySubjectAccessReviews(ctx, v.kubeClient, reviews)
	if err != nil {
		return err
	}

	if !allowed {
		return &NotAllowedError{
			Err: fmt.Errorf("not allowed to apply the resource %s %s, %s %s",
				resource.Group, resource.Resource, resource.Namespace, resource.Name),
			RequeueTime: 60 * time.Second,
		}
	}

	return nil
}

// CheckEscalation checks whether the sa is escalated to operate the gvr(RBAC) resources.
func (v *SarValidator) CheckEscalation(ctx context.Context, sa *workapiv1.ManifestWorkSubjectServiceAccount,
	gvr schema.GroupVersionResource, namespace, name string, obj *unstructured.Unstructured) error {

	if gvr.Group != "rbac.authorization.k8s.io" {
		return nil
	}
	if gvr.Resource != "roles" && gvr.Resource != "rolebindings" &&
		gvr.Resource != "clusterroles" && gvr.Resource != "clusterrolebindings" {
		return nil
	}

	dynamicClient, err := v.newImpersonateClientFunc(v.config, username(sa.Namespace, sa.Name))
	if err != nil {
		return err
	}

	_, err = dynamicClient.Resource(gvr).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{
		DryRun: []string{"All"},
	})
	if apierrors.IsForbidden(err) {
		klog.Infof("not allowed to apply the resource %s %s, %s %s, error: %s",
			gvr.Group, gvr.Resource, namespace, name, err.Error())
		return &NotAllowedError{
			Err: fmt.Errorf("not allowed to apply the resource %s %s, %s %s, error: permission escalation",
				gvr.Group, gvr.Resource, namespace, name),
			RequeueTime: 60 * time.Second,
		}
	}

	if apierrors.IsAlreadyExists(err) {
		// it is not necessary to further check the permission for update when the resource exists, because
		// the API server checks the permission escalation before checking the existence.
		return nil
	}
	return err
}

func username(saNamespace, saName string) string {
	return fmt.Sprintf("system:serviceaccount:%s:%s", saNamespace, saName)
}
func groups(saNamespace string) []string {
	return []string{"system:serviceaccounts", "system:authenticated",
		fmt.Sprintf("system:serviceaccounts:%s", saNamespace)}
}

func buildSubjectAccessReviews(saNamespace string, saName string,
	resource authorizationv1.ResourceAttributes,
	verbs ...string) []authorizationv1.SubjectAccessReview {

	reviews := []authorizationv1.SubjectAccessReview{}
	for _, verb := range verbs {
		reviews = append(reviews, authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Group:       resource.Group,
					Resource:    resource.Resource,
					Version:     resource.Version,
					Subresource: resource.Subresource,
					Name:        resource.Name,
					Namespace:   resource.Namespace,
					Verb:        verb,
				},
				User:   username(saNamespace, saName),
				Groups: groups(saNamespace),
			},
		})
	}
	return reviews
}

func validateBySubjectAccessReviews(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	subjectAccessReviews []authorizationv1.SubjectAccessReview) (bool, error) {

	for i := range subjectAccessReviews {
		subjectAccessReview := subjectAccessReviews[i]

		sar, err := kubeClient.AuthorizationV1().SubjectAccessReviews().Create(
			ctx, &subjectAccessReview, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}
		if !sar.Status.Allowed {
			return false, nil
		}
	}
	return true, nil
}
