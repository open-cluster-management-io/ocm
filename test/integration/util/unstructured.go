package util

import (
	"context"

	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	guestbookCrdJson = `{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind": "CustomResourceDefinition",
		"metadata": {
			"name": "guestbooks.my.domain"
		},
		"spec": {
			"conversion": {
				"strategy": "None"
			},
			"group": "my.domain",
			"names": {
				"kind": "Guestbook",
				"listKind": "GuestbookList",
				"plural": "guestbooks",
				"singular": "guestbook"
			},
			"scope": "Namespaced",
			"versions": [
				{
					"name": "v1",
					"schema": {
                    	"openAPIV3Schema": {
							"description": "",
							"properties": {
								"apiVersion": {
									"type": "string"
								},
								"kind": {
									"type": "string"
								},
								"metadata": {
									"type": "object"
								},
								"spec": {
									"properties": {
										"foo": {
											"type": "string"
										}
									},
									"type": "object"
								},
								"status": {
									"type": "object"
								}
							},
							"type": "object"
						}
					},
					"served": true,
					"storage": true,
					"subresources": {
						"status": {}
					}
				}
			]
		}
	}`

	guestbookCrJson = `{
		"apiVersion": "my.domain/v1",
		"kind": "Guestbook",
		"metadata": {
			"name": "guestbook1",
			"namespace": "default"
		},
		"spec": {
			"foo": "bar"
		}
	}`

	deploymentJson = `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "nginx-deployment",
			"namespace": "default"
		},
		"spec": {
			"replicas": 1,
			"selector": {
				"matchLabels": {
					"app": "nginx"
				}
			},
			"template": {
				"metadata": {
					"creationTimestamp": null,
					"labels": {
						"app": "nginx"
					}
				},
				"spec": {
					"containers": [
						{
							"image": "nginx:1.14.2",
							"name": "nginx",
							"ports": [
								{
									"containerPort": 80,
									"protocol": "TCP"
								}
							]
						}
					]
				}
			}
		}
	}`
)

var (
	scheme = runtime.NewScheme()

	serviceAccountGVK = schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ServiceAccount",
	}

	serviceAccountGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "serviceaccounts",
	}

	roleGVK = schema.GroupVersionKind{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "Role",
	}

	roleGVR = schema.GroupVersionResource{
		Group:    "rbac.authorization.k8s.io",
		Version:  "v1",
		Resource: "roles",
	}

	roleBindingGVK = schema.GroupVersionKind{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "RoleBinding",
	}

	roleBindingGVR = schema.GroupVersionResource{
		Group:    "rbac.authorization.k8s.io",
		Version:  "v1",
		Resource: "rolebindings",
	}
)

func init() {
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)
}

func GuestbookCrd() (crd *unstructured.Unstructured, gvr schema.GroupVersionResource, err error) {
	crd, err = loadResourceFromJSON(guestbookCrdJson)
	gvr = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	return crd, gvr, err
}

func GuestbookCr(namespace, name string) (cr *unstructured.Unstructured, gvr schema.GroupVersionResource, err error) {
	cr, err = loadResourceFromJSON(guestbookCrJson)
	if err != nil {
		return cr, gvr, err
	}

	cr.SetNamespace(namespace)
	cr.SetName(name)
	gvr = schema.GroupVersionResource{Group: "my.domain", Version: "v1", Resource: "guestbooks"}
	return cr, gvr, nil
}

func NewDeployment(namespace, name, sa string) (u *unstructured.Unstructured, gvr schema.GroupVersionResource, err error) {
	u, err = loadResourceFromJSON(deploymentJson)
	if err != nil {
		return u, gvr, err
	}

	u.SetNamespace(namespace)
	u.SetName(name)

	err = unstructured.SetNestedField(u.Object, sa, "spec", "template", "spec", "serviceAccountName")
	if err != nil {
		return u, gvr, err
	}
	gvr = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	return u, gvr, nil
}

func toUnstructured(obj runtime.Object, gvk schema.GroupVersionKind, scheme *runtime.Scheme) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	gomega.Expect(scheme.Convert(obj, u, nil)).To(gomega.Succeed())
	u.SetGroupVersionKind(gvk)
	return u
}

func NewServiceAccount(namespace, name string) (*unstructured.Unstructured, schema.GroupVersionResource) {
	obj := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}

	return toUnstructured(obj, serviceAccountGVK, scheme), serviceAccountGVR
}

func NewRole(namespace, name string) (*unstructured.Unstructured, schema.GroupVersionResource) {
	obj := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"create", "get", "list", "watch"},
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
			},
		},
	}

	return toUnstructured(obj, roleGVK, scheme), roleGVR
}

func NewRoleBinding(namespace, name, sa, role string) (*unstructured.Unstructured, schema.GroupVersionResource) {
	obj := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     role,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: namespace,
				Name:      sa,
			},
		},
	}

	return toUnstructured(obj, roleBindingGVK, scheme), roleBindingGVR
}

func loadResourceFromJSON(json string) (*unstructured.Unstructured, error) {
	obj := unstructured.Unstructured{}
	err := obj.UnmarshalJSON([]byte(json))
	return &obj, err
}

func GetResource(namespace, name string, gvr schema.GroupVersionResource, dynamicClient dynamic.Interface) (*unstructured.Unstructured, error) {
	return dynamicClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}
