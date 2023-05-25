package v1

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
	v1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/api/cluster/v1beta1"

	corev1 "k8s.io/api/core/v1"
)

func TestValidateCreate(t *testing.T) {
	cases := []struct {
		name                   string
		cluster                *v1.ManagedCluster
		preObjs                []runtime.Object
		expectedError          bool
		allowUpdateAcceptField bool
		allowClusterset        bool
		allowUpdateClusterSets map[string]bool
	}{
		{
			name:          "Empty spec cluster",
			expectedError: false,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
			},
		},
		{
			name:                   "validate creating an accepted ManagedCluster without permission",
			expectedError:          true,
			allowUpdateAcceptField: false,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
		},
		{
			name:                   "validate creating an accepted ManagedCluster with permission",
			expectedError:          false,
			allowUpdateAcceptField: true,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
		},
		{
			name:                   "validate cluster namespace, namespace is active",
			expectedError:          false,
			allowUpdateAcceptField: true,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
			preObjs: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "set-1",
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
			},
		},
		{
			name:                   "validate cluster namespace, namespace is terminating",
			expectedError:          true,
			allowUpdateAcceptField: true,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
			preObjs: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "set-1",
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceTerminating,
					},
				},
			},
		},
		{
			name:          "validate setting clusterset label",
			expectedError: false,
			allowUpdateClusterSets: map[string]bool{
				"clusterset1": true,
			},
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
					Labels: map[string]string{
						v1beta1.ClusterSetLabel: "clusterset1",
					},
				},
			},
		},
		{
			name:          "validate setting clusterset label without permission",
			expectedError: true,
			allowUpdateClusterSets: map[string]bool{
				"clusterset1": false,
			},
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
					Labels: map[string]string{
						v1beta1.ClusterSetLabel: "clusterset1",
					},
				},
			},
		},
		{
			name:          "validate create cluster with invalid config",
			expectedError: true,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
				},
				Spec: v1.ManagedClusterSpec{
					ManagedClusterClientConfigs: []v1.ClientConfig{
						{URL: "http://127.0.0.1:8001"},
					},
				},
			},
		},
		{
			name:          "validate create cluster with valid config",
			expectedError: false,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
				},
				Spec: v1.ManagedClusterSpec{
					ManagedClusterClientConfigs: []v1.ClientConfig{
						{URL: "https://127.0.0.1:8001"},
					},
				},
			},
		},
		{
			name:          "validate cluster name",
			expectedError: true,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "01.set",
				},
				Spec: v1.ManagedClusterSpec{
					ManagedClusterClientConfigs: []v1.ClientConfig{
						{URL: "https://127.0.0.1:8001"},
					},
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.preObjs...)
			kubeClient.PrependReactor(
				"create",
				"subjectaccessreviews",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					allowed := false

					sar := action.(clienttesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
					switch sar.Spec.ResourceAttributes.Resource {
					case "managedclusters":
						allowed = c.allowUpdateAcceptField
					case "managedclustersets":
						allowed = c.allowUpdateClusterSets[sar.Spec.ResourceAttributes.Name]
					}

					return true, &authorizationv1.SubjectAccessReview{
						Status: authorizationv1.SubjectAccessReviewStatus{
							Allowed: allowed,
						},
					}, nil
				},
			)
			w := ManagedClusterWebhook{
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

			err := w.ValidateCreate(ctx, c.cluster)
			if err != nil && !c.expectedError {
				t.Errorf("Case:%v, Expect nil but got Error, err: %v", c.name, err)
			}
			if err == nil && c.expectedError {
				t.Errorf("Case:%v, Expect Error but got nil", c.name)
			}
		})
	}
	w := ManagedClusterWebhook{}
	err := w.ValidateCreate(context.Background(), &v1beta1.ManagedClusterSet{})
	if err == nil {
		t.Errorf("Non cluster obj, Expect Error but got nil")
	}
}

func TestValidateUpdate(t *testing.T) {
	cases := []struct {
		name                   string
		cluster                *v1.ManagedCluster
		oldCluster             *v1.ManagedCluster
		preObjs                []runtime.Object
		expectedError          bool
		allowUpdateAcceptField bool
		allowClusterset        bool
		allowUpdateClusterSets map[string]bool
	}{
		{
			name:                   "validate update an accepted ManagedCluster without permission",
			expectedError:          true,
			allowUpdateAcceptField: false,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: false,
				},
			},
		},
		{
			name:                   "validate update other fields without accept permission",
			expectedError:          false,
			allowUpdateAcceptField: false,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						"k": "v",
					},
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
		},
		{
			name:                   "validate update ManagedCluster when namespace is terminating",
			expectedError:          true,
			allowUpdateAcceptField: true,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: false,
				},
			},
			preObjs: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "set-1",
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceTerminating,
					},
				}},
		},
		{
			name:                   "validate update ManagedCluster when namespace is active",
			expectedError:          false,
			allowUpdateAcceptField: true,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: false,
				},
			},
			preObjs: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "set-1",
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				}},
		},
		{
			name:                   "validate updating an accepted ManagedCluster with permission",
			expectedError:          false,
			allowUpdateAcceptField: true,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
				Spec: v1.ManagedClusterSpec{
					HubAcceptsClient: false,
				},
			},
		},
		{
			name:          "validate setting clusterset label",
			expectedError: false,
			allowUpdateClusterSets: map[string]bool{
				"clusterset1": true,
			},
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
					Labels: map[string]string{
						v1beta1.ClusterSetLabel: "clusterset1",
					},
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
				},
			},
		},
		{
			name:          "validate setting clusterset label without permission",
			expectedError: true,
			allowUpdateClusterSets: map[string]bool{
				"clusterset1": false,
			},
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
					Labels: map[string]string{
						v1beta1.ClusterSetLabel: "clusterset1",
					},
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
				},
			},
		},
		{
			name:          "validate setting clusterset label with 2 set permission",
			expectedError: false,
			allowUpdateClusterSets: map[string]bool{
				"clusterset1": true,
				"clusterset2": true,
			},
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
					Labels: map[string]string{
						v1beta1.ClusterSetLabel: "clusterset1",
					},
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
					Labels: map[string]string{
						v1beta1.ClusterSetLabel: "clusterset2",
					},
				},
			},
		},
		{
			name:          "validate setting clusterset label without permission",
			expectedError: true,
			allowUpdateClusterSets: map[string]bool{
				"clusterset1": false,
				"clusterset2": false,
			},
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
					Labels: map[string]string{
						v1beta1.ClusterSetLabel: "clusterset1",
					},
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
					Labels: map[string]string{
						v1beta1.ClusterSetLabel: "clusterset2",
					},
				},
			},
		},
		{
			name:          "validate remove clusterset label without permission",
			expectedError: false,
			allowUpdateClusterSets: map[string]bool{
				"clusterset1": true,
			},
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
					Labels: map[string]string{
						v1beta1.ClusterSetLabel: "clusterset1",
					},
				},
			},
		},
		{
			name:          "validate setting clusterset label with 1 set permission",
			expectedError: true,
			allowUpdateClusterSets: map[string]bool{
				"clusterset1": true,
				"clusterset2": false,
			},
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
					Labels: map[string]string{
						v1beta1.ClusterSetLabel: "clusterset1",
					},
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
					Labels: map[string]string{
						v1beta1.ClusterSetLabel: "clusterset2",
					},
				},
			},
		},
		{
			name:          "validate update cluster with invalid config",
			expectedError: true,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
				},
				Spec: v1.ManagedClusterSpec{
					ManagedClusterClientConfigs: []v1.ClientConfig{
						{URL: "http://127.0.0.1:8001"},
					},
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
				},
			},
		},
		{
			name:          "validate update cluster with valid config",
			expectedError: false,
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
				},
				Spec: v1.ManagedClusterSpec{
					ManagedClusterClientConfigs: []v1.ClientConfig{
						{URL: "https://127.0.0.1:8001"},
					},
				},
			},
			oldCluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set",
				},
				Spec: v1.ManagedClusterSpec{
					ManagedClusterClientConfigs: []v1.ClientConfig{
						{URL: "https://127.0.0.1:8002"},
					},
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.preObjs...)
			kubeClient.PrependReactor(
				"create",
				"subjectaccessreviews",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					allowed := false

					sar := action.(clienttesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
					switch sar.Spec.ResourceAttributes.Resource {
					case "managedclusters":
						allowed = c.allowUpdateAcceptField
					case "managedclustersets":
						allowed = c.allowUpdateClusterSets[sar.Spec.ResourceAttributes.Name]
					}

					return true, &authorizationv1.SubjectAccessReview{
						Status: authorizationv1.SubjectAccessReviewStatus{
							Allowed: allowed,
						},
					}, nil
				},
			)
			w := ManagedClusterWebhook{
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

			err := w.ValidateUpdate(ctx, c.oldCluster, c.cluster)
			if err != nil && !c.expectedError {
				t.Errorf("Case:%v, Expect nil but got error: %v", c.name, err)
			}
			if err == nil && c.expectedError {
				t.Errorf("Case:%v, Expect Error but got nil:%v", c.name, err)
			}
		})
	}
	w := ManagedClusterWebhook{}
	err := w.ValidateUpdate(context.Background(), nil, &v1beta1.ManagedClusterSetBinding{})
	if err == nil {
		t.Errorf("Non cluster obj, Expect Error but got nil")
	}
}
