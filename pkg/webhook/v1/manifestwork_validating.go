package v1

import (
	"context"
	"fmt"
	"open-cluster-management.io/work/pkg/webhook/common"
	"reflect"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes"
	ocmfeature "open-cluster-management.io/api/feature"
	workv1 "open-cluster-management.io/api/work/v1"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ webhook.CustomValidator = &ManifestWorkWebhook{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ManifestWorkWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	work, ok := obj.(*workv1.ManifestWork)
	if !ok {
		return apierrors.NewBadRequest("Request manifestwork obj format is not right")
	}
	return r.validateRequest(work, nil, ctx)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ManifestWorkWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	newWork, ok := newObj.(*workv1.ManifestWork)
	if !ok {
		return apierrors.NewBadRequest("Request manifestwork obj format is not right")
	}

	oldWork, ok := oldObj.(*workv1.ManifestWork)
	if !ok {
		return apierrors.NewBadRequest("Request manifestwork obj format is not right")
	}

	return r.validateRequest(newWork, oldWork, ctx)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ManifestWorkWebhook) ValidateDelete(_ context.Context, obj runtime.Object) error {
	return nil
}

func (r *ManifestWorkWebhook) validateRequest(newWork, oldWork *workv1.ManifestWork, ctx context.Context) error {
	if len(newWork.Spec.Workload.Manifests) == 0 {
		return apierrors.NewBadRequest("manifests should not be empty")
	}

	if err := common.ManifestValidator.ValidateManifests(newWork.Spec.Workload.Manifests); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}

	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}

	// do not need to check the executor when it is not changed
	if oldWork != nil && reflect.DeepEqual(oldWork.Spec.Executor, newWork.Spec.Executor) {
		return nil
	}
	return validateExecutor(r.kubeClient, newWork, req.UserInfo)
}

func validateExecutor(kubeClient kubernetes.Interface, work *workv1.ManifestWork, userInfo authenticationv1.UserInfo) error {
	executor := work.Spec.Executor
	if !utilfeature.DefaultMutableFeatureGate.Enabled(ocmfeature.NilExecutorValidating) {
		if executor == nil {
			return nil
		}
	}

	if work.Spec.Executor == nil {
		executor = &workv1.ManifestWorkExecutor{
			Subject: workv1.ManifestWorkExecutorSubject{
				Type: workv1.ExecutorSubjectTypeServiceAccount,
				ServiceAccount: &workv1.ManifestWorkSubjectServiceAccount{
					// give the default value "system:serviceaccount::klusterlet-work-sa"
					Namespace: "",                   // TODO: Not sure what value is reasonable
					Name:      "klusterlet-work-sa", // the default sa of the work agent
				},
			},
		}
	}

	if executor.Subject.Type == workv1.ExecutorSubjectTypeServiceAccount && executor.Subject.ServiceAccount == nil {
		return apierrors.NewBadRequest("executor service account can not be nil")
	}

	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   userInfo.Username,
			UID:    userInfo.UID,
			Groups: userInfo.Groups,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Group:     "work.open-cluster-management.io",
				Resource:  "manifestworks",
				Verb:      "execute-as",
				Namespace: work.Namespace,
				Name: fmt.Sprintf("system:serviceaccount:%s:%s",
					executor.Subject.ServiceAccount.Namespace, executor.Subject.ServiceAccount.Name),
			},
		},
	}
	sar, err := kubeClient.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}

	if !sar.Status.Allowed {
		return apierrors.NewBadRequest(fmt.Sprintf("user %s cannot manipulate the Manifestwork with executor %s/%s in namespace %s",
			userInfo.Username, executor.Subject.ServiceAccount.Namespace, executor.Subject.ServiceAccount.Name, work.Namespace))
	}

	return nil
}
