package apply

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/kubernetes"
	rbacv1listers "k8s.io/client-go/listers/rbac/v1"

	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
)

type PermissionApplier struct {
	roleLister               rbacv1listers.RoleLister
	roleBindingLister        rbacv1listers.RoleBindingLister
	clusterRoleLister        rbacv1listers.ClusterRoleLister
	clusterRoleBindingLister rbacv1listers.ClusterRoleBindingLister
	client                   kubernetes.Interface
}

func NewPermissionApplier(
	client kubernetes.Interface,
	roleLister rbacv1listers.RoleLister,
	roleBindingLister rbacv1listers.RoleBindingLister,
	clusterRoleLister rbacv1listers.ClusterRoleLister,
	clusterRoleBindingLister rbacv1listers.ClusterRoleBindingLister,
) *PermissionApplier {
	return &PermissionApplier{
		client:                   client,
		roleLister:               roleLister,
		roleBindingLister:        roleBindingLister,
		clusterRoleLister:        clusterRoleLister,
		clusterRoleBindingLister: clusterRoleBindingLister,
	}
}

func (m *PermissionApplier) Apply(
	ctx context.Context,
	recorder events.Recorder,
	manifests resourceapply.AssetFunc,
	files ...string) []resourceapply.ApplyResult {
	var ret []resourceapply.ApplyResult
	for _, file := range files {
		result := resourceapply.ApplyResult{File: file}
		objBytes, err := manifests(file)
		if err != nil {
			result.Error = fmt.Errorf("missing %q: %v", file, err)
			ret = append(ret, result)
			continue
		}
		requiredObj, err := resourceread.ReadGenericWithUnstructured(objBytes)
		if err != nil {
			result.Error = fmt.Errorf("cannot decode %q: %v", file, err)
			ret = append(ret, result)
			continue
		}
		result.Type = fmt.Sprintf("%T", requiredObj)
		switch t := requiredObj.(type) {
		case *rbacv1.ClusterRole:
			result.Result, result.Changed, result.Error = Apply[*rbacv1.ClusterRole](
				ctx, m.clusterRoleLister, m.client.RbacV1().ClusterRoles(), compareClusterRole, t, recorder)
		case *rbacv1.ClusterRoleBinding:
			result.Result, result.Changed, result.Error = Apply[*rbacv1.ClusterRoleBinding](
				ctx, m.clusterRoleBindingLister, m.client.RbacV1().ClusterRoleBindings(), compareClusterRoleBinding, t, recorder)
		case *rbacv1.Role:
			result.Result, result.Changed, result.Error = Apply[*rbacv1.Role](
				ctx, m.roleLister.Roles(t.Namespace), m.client.RbacV1().Roles(t.Namespace), compareRole, t, recorder)
		case *rbacv1.RoleBinding:
			result.Result, result.Changed, result.Error = Apply[*rbacv1.RoleBinding](
				ctx, m.roleBindingLister.RoleBindings(t.Namespace), m.client.RbacV1().RoleBindings(t.Namespace), compareRoleBinding, t, recorder)
		default:
			result.Error = fmt.Errorf("object type is not correct")
		}
	}
	return ret
}

func compareRole(required, existing *rbacv1.Role) (*rbacv1.Role, bool) {
	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	contentSame := equality.Semantic.DeepEqual(existingCopy.Rules, required.Rules)

	if contentSame && !*modified {
		return existingCopy, false
	}

	existingCopy.Rules = required.Rules
	return existingCopy, true
}

func compareClusterRole(required, existing *rbacv1.ClusterRole) (*rbacv1.ClusterRole, bool) {
	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	contentSame := equality.Semantic.DeepEqual(existingCopy.Rules, required.Rules)

	if contentSame && !*modified {
		return existingCopy, false
	}

	existingCopy.Rules = required.Rules
	return existingCopy, true
}

func compareRoleBinding(required, existing *rbacv1.RoleBinding) (*rbacv1.RoleBinding, bool) {
	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)

	subjectsAreSame := equality.Semantic.DeepEqual(existingCopy.Subjects, required.Subjects)
	roleRefIsSame := equality.Semantic.DeepEqual(existingCopy.RoleRef, required.RoleRef)

	if subjectsAreSame && roleRefIsSame && !*modified {
		return existingCopy, false
	}

	existingCopy.Subjects = required.Subjects
	existingCopy.RoleRef = required.RoleRef

	return existingCopy, true
}

func compareClusterRoleBinding(required, existing *rbacv1.ClusterRoleBinding) (*rbacv1.ClusterRoleBinding, bool) {
	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)

	subjectsAreSame := equality.Semantic.DeepEqual(existingCopy.Subjects, required.Subjects)
	roleRefIsSame := equality.Semantic.DeepEqual(existingCopy.RoleRef, required.RoleRef)

	if subjectsAreSame && roleRefIsSame && !*modified {
		return existingCopy, false
	}

	existingCopy.Subjects = required.Subjects
	existingCopy.RoleRef = required.RoleRef

	return existingCopy, true
}
