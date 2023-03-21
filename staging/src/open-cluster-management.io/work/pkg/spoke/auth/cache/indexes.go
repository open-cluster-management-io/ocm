package cache

import (
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	byClusterRole = "byClusterRole"
	byRole        = "byRole"
)

func crbIndexByClusterRole(obj interface{}) ([]string, error) {
	crb, ok := obj.(*rbacv1.ClusterRoleBinding)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a ClusterRoleBinding, but is %T", obj)
	}

	return []string{crb.RoleRef.Name}, nil
}

func rbIndexByClusterRole(obj interface{}) ([]string, error) {
	rb, ok := obj.(*rbacv1.RoleBinding)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a RoleBinding, but is %T", obj)
	}

	if rb.RoleRef.Kind == "ClusterRole" {
		return []string{rb.RoleRef.Name}, nil
	}
	return []string{}, nil
}

func rbIndexByRole(obj interface{}) ([]string, error) {
	rb, ok := obj.(*rbacv1.RoleBinding)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a RoleBinding, but is %T", obj)
	}

	if rb.RoleRef.Kind == "Role" {
		return []string{fmt.Sprintf("%s/%s", rb.Namespace, rb.RoleRef.Name)}, nil
	}

	return []string{}, nil
}
