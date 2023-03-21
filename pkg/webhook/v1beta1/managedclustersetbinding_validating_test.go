package v1beta1

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"

	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/api/cluster/v1beta1"
)

func TestValidateCreate(t *testing.T) {
	cases := []struct {
		name                     string
		setbinding               *v1beta1.ManagedClusterSetBinding
		expectedError            bool
		allowBindingToClusterSet bool
	}{
		{
			name:                     "Right Clustersetbinding",
			expectedError:            false,
			allowBindingToClusterSet: true,
			setbinding: &v1beta1.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns-1",
					Name:      "setbinding-1",
				},
				Spec: v1beta1.ManagedClusterSetBindingSpec{
					ClusterSet: "setbinding-1",
				},
			},
		},
		{
			name:                     "Set name is not right",
			expectedError:            true,
			allowBindingToClusterSet: true,
			setbinding: &v1beta1.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns-1",
					Name:      "setbinding-1",
				},
				Spec: v1beta1.ManagedClusterSetBindingSpec{
					ClusterSet: "setbinding",
				},
			},
		},
		{
			name:                     "Do not have permission",
			expectedError:            true,
			allowBindingToClusterSet: false,
			setbinding: &v1beta1.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns-1",
					Name:      "setbinding-1",
				},
				Spec: v1beta1.ManagedClusterSetBindingSpec{
					ClusterSet: "setbinding-1",
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset()
			kubeClient.PrependReactor(
				"create",
				"subjectaccessreviews",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, &authorizationv1.SubjectAccessReview{
						Status: authorizationv1.SubjectAccessReviewStatus{
							Allowed: c.allowBindingToClusterSet,
						},
					}, nil
				},
			)
			w := ManagedClusterSetBindingWebhook{
				kubeClient: kubeClient,
			}
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource: metav1.GroupVersionResource{
						Group:    "test.open-cluster-management.io",
						Version:  "v1",
						Resource: "tests",
					},
				},
			}

			ctx := admission.NewContextWithRequest(context.Background(), req)

			err := w.ValidateCreate(ctx, c.setbinding)
			if err != nil && !c.expectedError {
				t.Errorf("Case:%v, Expect nil Error but got err:%v", c.name, err)
			}
			if err == nil && c.expectedError {
				t.Errorf("Case:%v, Expect Error but got nil", c.name)
			}
		})
	}
	w := ManagedClusterSetBindingWebhook{}
	err := w.ValidateCreate(context.Background(), &v1beta1.ManagedClusterSet{})
	if err == nil {
		t.Errorf("Non setbinding obj, Expect Error but got nil")
	}
}

func TestValidateUpddate(t *testing.T) {
	cases := []struct {
		name          string
		setbinding    *v1beta1.ManagedClusterSetBinding
		expectedError bool
	}{
		{
			name:          "Right Clustersetbinding",
			expectedError: false,
			setbinding: &v1beta1.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns-1",
					Name:      "setbinding-1",
				},
				Spec: v1beta1.ManagedClusterSetBindingSpec{
					ClusterSet: "setbinding-1",
				},
			},
		},
		{
			name:          "Set name is not right",
			expectedError: true,
			setbinding: &v1beta1.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns-1",
					Name:      "setbinding-1",
				},
				Spec: v1beta1.ManagedClusterSetBindingSpec{
					ClusterSet: "setbinding",
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := ManagedClusterSetBindingWebhook{}
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource: metav1.GroupVersionResource{
						Group:    "test.open-cluster-management.io",
						Version:  "v1",
						Resource: "tests",
					},
				},
			}

			ctx := admission.NewContextWithRequest(context.Background(), req)

			err := w.ValidateUpdate(ctx, nil, c.setbinding)
			if err != nil && !c.expectedError {
				t.Errorf("Case:%v, Expect nil Error but not err:%v", c.name, err)
			}
			if err == nil && c.expectedError {
				t.Errorf("Case:%v, Expect Error but not nil", c.name)
			}
		})
	}
	w := ManagedClusterSetBindingWebhook{}
	err := w.ValidateUpdate(context.Background(), nil, &v1beta1.ManagedClusterSet{})
	if err == nil {
		t.Errorf("Non setbinding obj, Expect Error but got nil")
	}
}
