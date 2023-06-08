package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"open-cluster-management.io/api/addon/v1alpha1"
	v1 "open-cluster-management.io/api/cluster/v1"
)

func TestPermissionBuilder(t *testing.T) {
	testCluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
	}
	testAddon := &v1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{Name: "test-addon"},
	}
	creatingClusterRole1 := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "foo1", UID: "foo1"},
	}
	creatingRole1 := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: testCluster.Name, Name: "foo1", UID: "foo1"},
	}
	existingClusterRole2 := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "foo2", UID: "foo2"},
	}
	existingRole2 := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: testCluster.Name, Name: "foo2", UID: "foo2"},
	}
	updatingClusterRole2 := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "foo2", UID: "foo2"},
	}
	updatingRole2 := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: testCluster.Name, Name: "foo2", UID: "foo2"},
	}
	fakeKubeClient := fake.NewSimpleClientset(existingClusterRole2, existingRole2)
	permissionConfigFn := NewRBACPermissionConfigBuilder(fakeKubeClient).
		WithStaticClusterRole(creatingClusterRole1).
		WithStaticClusterRole(updatingClusterRole2).
		WithStaticRole(creatingRole1).
		WithStaticRole(updatingRole2).
		Build()

	assert.NoError(t, permissionConfigFn(testCluster, testAddon))

	actualClusterRole1, err := fakeKubeClient.RbacV1().ClusterRoles().Get(context.TODO(), "foo1", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, creatingClusterRole1.UID, actualClusterRole1.UID)
	actualClusterRole2, err := fakeKubeClient.RbacV1().ClusterRoles().Get(context.TODO(), "foo2", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, updatingClusterRole2.UID, actualClusterRole2.UID)
	actualRole1, err := fakeKubeClient.RbacV1().Roles(testCluster.Name).Get(context.TODO(), "foo1", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, creatingRole1.UID, actualRole1.UID)
	actualRole2, err := fakeKubeClient.RbacV1().Roles(testCluster.Name).Get(context.TODO(), "foo2", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, updatingRole2.UID, actualRole2.UID)
}
