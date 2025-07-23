package util

import (
	"fmt"

	authnv1 "k8s.io/api/authentication/v1"
	authzv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func KubeAuthnClient() kubernetes.Interface {
	client := fake.NewSimpleClientset()
	client.Fake.PrependReactor(
		"create",
		"tokenreviews",
		func(action clienttesting.Action) (bool, runtime.Object, error) {
			createAction, ok := action.(clienttesting.CreateAction)
			if !ok {
				return false, nil, fmt.Errorf("unexpected action %T", action)
			}

			tokenReview := createAction.GetObject().(*authnv1.TokenReview)
			if tokenReview.Spec.Token != "test-client" {
				return true, &authnv1.TokenReview{Status: authnv1.TokenReviewStatus{Authenticated: false}}, nil
			}

			return true, &authnv1.TokenReview{
				Status: authnv1.TokenReviewStatus{
					Authenticated: true,
					User: authnv1.UserInfo{
						Username: "test-client",
					},
				},
			}, nil
		},
	)
	return client
}

func KubeAuthzClient() kubernetes.Interface {
	client := fake.NewSimpleClientset()

	client.Fake.PrependReactor(
		"create",
		"subjectaccessreviews",
		func(action clienttesting.Action) (bool, runtime.Object, error) {
			createAction, ok := action.(clienttesting.CreateAction)
			if !ok {
				return false, nil, fmt.Errorf("unexpected action %T", action)
			}

			sarObj := createAction.GetObject()
			sar, ok := sarObj.(*authzv1.SubjectAccessReview)
			if !ok {
				return false, nil, fmt.Errorf("unexpected object %T", sarObj)
			}

			if sar.Spec.User != "test-client" {
				return true, &authzv1.SubjectAccessReview{Status: authzv1.SubjectAccessReviewStatus{Allowed: false}}, nil
			}

			return true, &authzv1.SubjectAccessReview{Status: authzv1.SubjectAccessReviewStatus{Allowed: true}}, nil
		},
	)
	return client
}
