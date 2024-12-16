package templateagent

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestNamespaceDecorator(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		object         *unstructured.Unstructured
		validateObject func(t *testing.T, obj *unstructured.Unstructured)
	}{
		{
			name:   "no namespace set",
			object: testingcommon.NewUnstructured("v1", "Pod", "default", "test"),
			validateObject: func(t *testing.T, obj *unstructured.Unstructured) {
				testingcommon.AssertEqualNameNamespace(t, obj.GetName(), obj.GetNamespace(), "test", "default")
			},
		},
		{
			name:      "namespace set",
			object:    testingcommon.NewUnstructured("v1", "Pod", "default", "test"),
			namespace: "newns",
			validateObject: func(t *testing.T, obj *unstructured.Unstructured) {
				testingcommon.AssertEqualNameNamespace(t, obj.GetName(), obj.GetNamespace(), "test", "newns")
			},
		},
		{
			name: "clusterRoleBinding",
			object: func() *unstructured.Unstructured {
				clusterRoleBinding := &rbacv1.ClusterRoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind: "ClusterRoleBinding",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Subjects: []rbacv1.Subject{
						{
							Name:      "user1",
							Namespace: "default",
						},
						{
							Name:      "user2",
							Namespace: "default",
						},
					},
				}
				data, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(clusterRoleBinding)
				return &unstructured.Unstructured{Object: data}
			}(),
			namespace: "newns",
			validateObject: func(t *testing.T, obj *unstructured.Unstructured) {
				binding := &rbacv1.ClusterRoleBinding{}
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, binding)
				if err != nil {
					t.Fatal(err)
				}
				for _, s := range binding.Subjects {
					if s.Namespace != "newns" {
						t.Errorf("namespace of subject is not correct, got %v", s)
					}
				}
			},
		},
		{
			name: "roleBinding",
			object: func() *unstructured.Unstructured {
				roleBinding := &rbacv1.RoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind: "RoleBinding",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
					Subjects: []rbacv1.Subject{
						{
							Name:      "user1",
							Namespace: "default",
						},
						{
							Name:      "user2",
							Namespace: "default",
						},
					},
				}
				data, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(roleBinding)
				return &unstructured.Unstructured{Object: data}
			}(),
			namespace: "newns",
			validateObject: func(t *testing.T, obj *unstructured.Unstructured) {
				testingcommon.AssertEqualNameNamespace(t, obj.GetName(), obj.GetNamespace(), "test", "newns")
				binding := &rbacv1.RoleBinding{}
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, binding)
				if err != nil {
					t.Fatal(err)
				}
				for _, s := range binding.Subjects {
					if s.Namespace != "newns" {
						t.Errorf("namespace of subject is not correct, got %v", s)
					}
				}
			},
		},
		{
			name: "namespace",
			object: func() *unstructured.Unstructured {
				ns := &corev1.Namespace{
					TypeMeta: metav1.TypeMeta{
						Kind: "Namespace",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
				}
				data, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(ns)
				return &unstructured.Unstructured{Object: data}
			}(),
			namespace: "newns",
			validateObject: func(t *testing.T, obj *unstructured.Unstructured) {
				ns := &corev1.Namespace{}
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, ns)
				if err != nil {
					t.Fatal(err)
				}
				if ns.Name != "newns" {
					t.Errorf("name of namespace is not correct, got %v", ns.Name)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			values := addonfactory.Values{}
			if len(tc.namespace) > 0 {
				values[InstallNamespacePrivateValueKey] = tc.namespace
			}

			d := newNamespaceDecorator(values)
			result, err := d.decorate(tc.object)
			if err != nil {
				t.Fatal(err)
			}
			tc.validateObject(t, result)
		})
	}

}

func TestProxyDecorator(t *testing.T) {
	tests := []struct {
		name           string
		config         addonapiv1alpha1.AddOnDeploymentConfig
		pod            *corev1.PodTemplateSpec
		validateObject func(t *testing.T, pod *corev1.PodTemplateSpec)
	}{
		{
			name: "no proxy set",
			pod: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "test",
						},
					},
				},
			},
			validateObject: func(t *testing.T, pod *corev1.PodTemplateSpec) {
				for _, c := range pod.Spec.Containers {
					if c.Env != nil {
						for _, e := range c.Env {
							if e.Name == "HTTP_PROXY" || e.Name == "HTTPS_PROXY" || e.Name == "NO_PROXY" ||
								e.Name == "http_proxy" || e.Name == "https_proxy" || e.Name == "no_proxy" {
								t.Errorf("proxy env is not expected, got %v", e)
							}
						}
					}
				}
			},
		},
		{
			name: "proxy set",
			pod: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "test",
						},
					},
				},
			},
			config: addonapiv1alpha1.AddOnDeploymentConfig{
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					ProxyConfig: addonapiv1alpha1.ProxyConfig{
						HTTPProxy:  "http://proxy",
						HTTPSProxy: "https://proxy",
						NoProxy:    "no-proxy",
					},
				},
			},
			validateObject: func(t *testing.T, pod *corev1.PodTemplateSpec) {
				for _, c := range pod.Spec.Containers {
					if c.Env != nil {
						for _, e := range c.Env {
							if e.Name == "HTTP_PROXY" || e.Name == "http_proxy" {
								if e.Value != "http://proxy" {
									t.Errorf("http proxy env is not correct, got %v", e)
								}
							} else if e.Name == "HTTPS_PROXY" || e.Name == "https_proxy" {
								if e.Value != "https://proxy" {
									t.Errorf("https proxy env is not correct, got %v", e)
								}
							} else if e.Name == "NO_PROXY" || e.Name == "no_proxy" {
								if e.Value != "no-proxy" {
									t.Errorf("no proxy env is not correct, got %v", e)
								}
							}
						}
					}
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			values, err := ToAddOnProxyPrivateValues(tc.config)
			if err != nil {
				t.Fatal(err)
			}
			d := newProxyDecorator(values)
			err = d.decorate(tc.pod)
			if err != nil {
				t.Fatal(err)
			}
			tc.validateObject(t, tc.pod)
		})
	}

}
