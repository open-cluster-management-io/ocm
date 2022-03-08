package utils

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// RBACPermissionBuilder builds a agent.PermissionConfigFunc that applies Kubernetes RBAC policies.
type RBACPermissionBuilder interface {
	// BindClusterRoleToUser is a shortcut that ensures a cluster role and binds to a hub user.
	BindClusterRoleToUser(clusterRole *rbacv1.ClusterRole, username string) RBACPermissionBuilder
	// BindClusterRoleToGroup is a shortcut that ensures a cluster role and binds to a hub user group.
	BindClusterRoleToGroup(clusterRole *rbacv1.ClusterRole, userGroup string) RBACPermissionBuilder
	// BindRoleToUser is a shortcut that ensures a role and binds to a hub user.
	BindRoleToUser(clusterRole *rbacv1.Role, username string) RBACPermissionBuilder
	// BindRoleToGroup is a shortcut that ensures a role binding and binds to a hub user.
	BindRoleToGroup(clusterRole *rbacv1.Role, userGroup string) RBACPermissionBuilder

	// WithStaticClusterRole ensures a cluster role to the hub cluster.
	WithStaticClusterRole(clusterRole *rbacv1.ClusterRole) RBACPermissionBuilder
	// WithStaticClusterRoleBinding ensures a cluster role binding to the hub cluster.
	WithStaticClusterRoleBinding(clusterRole *rbacv1.ClusterRoleBinding) RBACPermissionBuilder
	// WithStaticRole ensures a role to the hub cluster.
	WithStaticRole(clusterRole *rbacv1.Role) RBACPermissionBuilder
	// WithStaticRole ensures a role binding to the hub cluster.
	WithStaticRoleBinding(clusterRole *rbacv1.RoleBinding) RBACPermissionBuilder

	// Build wraps up the builder chain, and return a agent.PermissionConfigFunc.
	Build() agent.PermissionConfigFunc
}

var _ RBACPermissionBuilder = &permissionBuilder{}

type permissionBuilder struct {
	kubeClient kubernetes.Interface
	u          *unionPermissionBuilder
	recorder   events.Recorder
}

// NewRBACPermissionConfigBuilder instantiates a default RBACPermissionBuilder.
func NewRBACPermissionConfigBuilder(kubeClient kubernetes.Interface) RBACPermissionBuilder {
	return &permissionBuilder{
		u:          &unionPermissionBuilder{},
		kubeClient: kubeClient,
		recorder:   events.NewInMemoryRecorder("PermissionConfigBuilder"),
	}
}

func (p *permissionBuilder) BindClusterRoleToUser(clusterRole *rbacv1.ClusterRole, username string) RBACPermissionBuilder {
	return p.WithStaticClusterRole(clusterRole).
		WithStaticClusterRoleBinding(&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterRole.Name, // The same name as the cluster role
			},
			RoleRef: rbacv1.RoleRef{
				Kind: "ClusterRole",
				Name: clusterRole.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind: rbacv1.UserKind,
					Name: username,
				},
			},
		})
}

func (p *permissionBuilder) BindClusterRoleToGroup(clusterRole *rbacv1.ClusterRole, userGroup string) RBACPermissionBuilder {
	return p.WithStaticClusterRole(clusterRole).
		WithStaticClusterRoleBinding(&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterRole.Name, // The same name as the cluster role
			},
			RoleRef: rbacv1.RoleRef{
				Kind: "ClusterRole",
				Name: clusterRole.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind: rbacv1.GroupKind,
					Name: userGroup,
				},
			},
		})
}

func (p *permissionBuilder) BindRoleToUser(role *rbacv1.Role, username string) RBACPermissionBuilder {
	return p.WithStaticRole(role).
		WithStaticRoleBinding(&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: role.Name, // The same name as the cluster role
			},
			RoleRef: rbacv1.RoleRef{
				Kind: "Role",
				Name: role.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind: rbacv1.UserKind,
					Name: username,
				},
			},
		})
}

func (p *permissionBuilder) BindRoleToGroup(role *rbacv1.Role, userGroup string) RBACPermissionBuilder {
	return p.WithStaticRole(role).
		WithStaticRoleBinding(&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: role.Name, // The same name as the cluster role
			},
			RoleRef: rbacv1.RoleRef{
				Kind: "Role",
				Name: role.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind: rbacv1.GroupKind,
					Name: userGroup,
				},
			},
		})
}

func (p *permissionBuilder) WithStaticClusterRole(clusterRole *rbacv1.ClusterRole) RBACPermissionBuilder {
	p.u.fns = append(p.u.fns, func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		_, _, err := resourceapply.ApplyClusterRole(context.TODO(), p.kubeClient.RbacV1(), p.recorder, clusterRole)
		return err
	})
	return p
}

func (p *permissionBuilder) WithStaticClusterRoleBinding(binding *rbacv1.ClusterRoleBinding) RBACPermissionBuilder {
	p.u.fns = append(p.u.fns, func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		_, _, err := resourceapply.ApplyClusterRoleBinding(context.TODO(), p.kubeClient.RbacV1(), p.recorder, binding)
		return err
	})
	return p
}

func (p *permissionBuilder) WithStaticRole(role *rbacv1.Role) RBACPermissionBuilder {
	p.u.fns = append(p.u.fns, func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		role.Namespace = cluster.Name
		ensureAddonOwnerReference(&role.ObjectMeta, addon)
		_, _, err := resourceapply.ApplyRole(context.TODO(), p.kubeClient.RbacV1(), p.recorder, role)
		return err
	})
	return p
}

func (p *permissionBuilder) WithStaticRoleBinding(binding *rbacv1.RoleBinding) RBACPermissionBuilder {
	p.u.fns = append(p.u.fns, func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		binding.Namespace = cluster.Name
		ensureAddonOwnerReference(&binding.ObjectMeta, addon)
		_, _, err := resourceapply.ApplyRoleBinding(context.TODO(), p.kubeClient.RbacV1(), p.recorder, binding)
		return err
	})
	return p
}

func (p *permissionBuilder) Build() agent.PermissionConfigFunc {
	return p.u.build()
}

type unionPermissionBuilder struct {
	fns []agent.PermissionConfigFunc
}

func (b *unionPermissionBuilder) build() agent.PermissionConfigFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		for _, fn := range b.fns {
			if err := fn(cluster, addon); err != nil {
				return err
			}
		}
		return nil
	}
}

func ensureAddonOwnerReference(metadata *metav1.ObjectMeta, addon *addonapiv1alpha1.ManagedClusterAddOn) {
	metadata.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion:         addonapiv1alpha1.GroupVersion.String(),
			Kind:               "ManagedClusterAddOn",
			Name:               addon.Name,
			BlockOwnerDeletion: pointer.Bool(true),
			UID:                addon.UID,
		},
	}
}
