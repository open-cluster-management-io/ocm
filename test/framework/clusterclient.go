package framework

import (
	"context"
	"fmt"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
)

func (hub *Hub) BuildClusterClient(saNamespace, saName string, clusterPolicyRules, policyRules []rbacv1.PolicyRule) (clusterclient.Interface, error) {
	var err error

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: saNamespace,
			Name:      saName,
		},
	}
	_, err = hub.KubeClient.CoreV1().ServiceAccounts(saNamespace).Create(context.TODO(), sa, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	// create cluster role/rolebinding
	if len(clusterPolicyRules) > 0 {
		clusterRoleName := fmt.Sprintf("%s-clusterrole", saName)
		clusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterRoleName,
			},
			Rules: clusterPolicyRules,
		}
		_, err = hub.KubeClient.RbacV1().ClusterRoles().Create(context.TODO(), clusterRole, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}

		clusterRoleBinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-clusterrolebinding", saName),
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Namespace: saNamespace,
					Name:      saName,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     clusterRoleName,
			},
		}
		_, err = hub.KubeClient.RbacV1().ClusterRoleBindings().Create(context.TODO(), clusterRoleBinding, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}

	// create cluster role/rolebinding
	if len(policyRules) > 0 {
		roleName := fmt.Sprintf("%s-role", saName)
		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: saNamespace,
				Name:      roleName,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"cluster.open-cluster-management.io"},
					Resources: []string{"managedclustersetbindings"},
					Verbs:     []string{"create", "get", "update"},
				},
			},
		}
		_, err = hub.KubeClient.RbacV1().Roles(saNamespace).Create(context.TODO(), role, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}

		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: saNamespace,
				Name:      fmt.Sprintf("%s-rolebinding", saName),
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Namespace: saNamespace,
					Name:      saName,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     roleName,
			},
		}
		_, err = hub.KubeClient.RbacV1().RoleBindings(saNamespace).Create(context.TODO(), roleBinding, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}

	tokenRequest, err := hub.KubeClient.CoreV1().ServiceAccounts(saNamespace).CreateToken(
		context.TODO(),
		saName,
		&authv1.TokenRequest{
			Spec: authv1.TokenRequestSpec{
				ExpirationSeconds: pointer.Int64(8640 * 3600),
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return nil, err
	}

	unauthorizedClusterClient, err := clusterclient.NewForConfig(&rest.Config{
		Host: hub.ClusterCfg.Host,
		TLSClientConfig: rest.TLSClientConfig{
			CAData: hub.ClusterCfg.CAData,
		},
		BearerToken: tokenRequest.Status.Token,
	})
	return unauthorizedClusterClient, err
}

func (hub *Hub) CleanupClusterClient(saNamespace, saName string) error {
	err := hub.KubeClient.CoreV1().ServiceAccounts(saNamespace).Delete(context.TODO(), saName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete sa %q/%q failed: %v", saNamespace, saName, err)
	}

	// delete cluster role and cluster role binding if exists
	clusterRoleName := fmt.Sprintf("%s-clusterrole", saName)
	err = hub.KubeClient.RbacV1().ClusterRoles().Delete(context.TODO(), clusterRoleName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete cluster role %q failed: %v", clusterRoleName, err)
	}
	clusterRoleBindingName := fmt.Sprintf("%s-clusterrolebinding", saName)
	err = hub.KubeClient.RbacV1().ClusterRoleBindings().Delete(context.TODO(), clusterRoleBindingName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete cluster role binding %q failed: %v", clusterRoleBindingName, err)
	}

	return nil
}
