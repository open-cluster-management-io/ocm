package rbacfinalizerdeletion

import (
	"context"
	"fmt"
	"reflect"

	clusterv1listers "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1"
	worklister "github.com/open-cluster-management/api/client/work/listers/work/v1"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	rbacv1informers "k8s.io/client-go/informers/rbac/v1"
	rbacv1client "k8s.io/client-go/kubernetes/typed/rbac/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	rbacv1listers "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

const (
	manifestWorkFinalizer = "cluster.open-cluster-management.io/manifest-work-cleanup"
)

type finalizeController struct {
	roleLister         rbacv1listers.RoleLister
	roleBindingLister  rbacv1listers.RoleBindingLister
	rbacClient         rbacv1client.RbacV1Interface
	clusterLister      clusterv1listers.ManagedClusterLister
	namespaceLister    corelisters.NamespaceLister
	manifestWorkLister worklister.ManifestWorkLister
	eventRecorder      events.Recorder
}

// NewFinalizeController ensures all manifestworks are deleted before role/rolebinding for work
// agent are deleted in a terminating cluster namespace.
func NewFinalizeController(
	roleInformer rbacv1informers.RoleInformer,
	roleBindingInformer rbacv1informers.RoleBindingInformer,
	namespaceLister corelisters.NamespaceLister,
	clusterLister clusterv1listers.ManagedClusterLister,
	manifestWorkLister worklister.ManifestWorkLister,
	rbacClient rbacv1client.RbacV1Interface,
	eventRecorder events.Recorder,
) factory.Controller {

	controller := &finalizeController{
		roleLister:         roleInformer.Lister(),
		roleBindingLister:  roleBindingInformer.Lister(),
		namespaceLister:    namespaceLister,
		clusterLister:      clusterLister,
		manifestWorkLister: manifestWorkLister,
		rbacClient:         rbacClient,
		eventRecorder:      eventRecorder,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, roleInformer.Informer(), roleBindingInformer.Informer()).
		WithSync(controller.sync).ToController("FinalizeController", eventRecorder)
}

func (m *finalizeController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	key := controllerContext.QueueKey()
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore role/rolebinding whose key is not in format: namespace/name
		return nil
	}

	cluster, err := m.clusterLister.Get(namespace)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	ns, err := m.namespaceLister.Get(namespace)
	if err != nil {
		return err
	}

	// cluster is deleted or being deleted
	role, rolebinding, err := m.getRoleAndRoleBinding(namespace, name)
	if err != nil {
		return err
	}

	err = m.syncRoleAndRoleBinding(ctx, controllerContext, role, rolebinding, ns, cluster)

	if err != nil {
		klog.Errorf("Reconcile role/rolebinding %s fails with err: %v", key, err)
	}
	return err
}

func (m *finalizeController) syncRoleAndRoleBinding(ctx context.Context, controllerContext factory.SyncContext,
	role *rbacv1.Role, rolebinding *rbacv1.RoleBinding, ns *corev1.Namespace, cluster *clusterv1.ManagedCluster) error {
	// Skip if neither role nor rolebinding has the finalizer
	if !hasFinalizer(role, manifestWorkFinalizer) && !hasFinalizer(rolebinding, manifestWorkFinalizer) {
		return nil
	}

	// There are twp possible cases that we need to remove finalizers on role/rolebindings based on
	// clean of manifestworks.
	// 1. The namespace is finalizing.
	// 2. The cluster is finalizing but namespace fails to be deleted.
	if !ns.DeletionTimestamp.IsZero() || (cluster != nil && !cluster.DeletionTimestamp.IsZero()) {
		works, err := m.manifestWorkLister.ManifestWorks(ns.Name).List(labels.Everything())
		if err != nil {
			return err
		}

		if len(works) != 0 {
			return fmt.Errorf("Still having %d works in the cluster namespace %s", len(works), ns.Name)
		}
	}

	// remove finalizer from role/rolebinding
	if pendingFinalization(role) {
		if err := m.removeFinalizerFromRole(ctx, role, manifestWorkFinalizer); err != nil {
			return err
		}
	}

	if pendingFinalization(rolebinding) {
		if err := m.removeFinalizerFromRoleBinding(ctx, rolebinding, manifestWorkFinalizer); err != nil {
			return err
		}
	}
	return nil
}

func (m *finalizeController) getRoleAndRoleBinding(namespace, name string) (*rbacv1.Role, *rbacv1.RoleBinding, error) {
	role, err := m.roleLister.Roles(namespace).Get(name)
	if err != nil && !errors.IsNotFound(err) {
		return nil, nil, err
	}

	rolebinding, err := m.roleBindingLister.RoleBindings(namespace).Get(name)
	if err != nil && !errors.IsNotFound(err) {
		return nil, nil, err
	}

	return role, rolebinding, nil
}

// removeFinalizerFromRole removes the particular finalizer from role
func (m *finalizeController) removeFinalizerFromRole(ctx context.Context, role *rbacv1.Role, finalizer string) error {
	if role == nil {
		return nil
	}

	role = role.DeepCopy()
	if changed := removeFinalizer(role, finalizer); !changed {
		return nil
	}

	_, err := m.rbacClient.Roles(role.Namespace).Update(ctx, role, metav1.UpdateOptions{})
	return err
}

// removeFinalizerFromRoleBinding removes the particular finalizer from rolebinding
func (m *finalizeController) removeFinalizerFromRoleBinding(ctx context.Context, rolebinding *rbacv1.RoleBinding, finalizer string) error {
	if rolebinding == nil {
		return nil
	}

	rolebinding = rolebinding.DeepCopy()
	if changed := removeFinalizer(rolebinding, finalizer); !changed {
		return nil
	}

	_, err := m.rbacClient.RoleBindings(rolebinding.Namespace).Update(ctx, rolebinding, metav1.UpdateOptions{})
	return err
}

// hasFinalizer returns true if the object has the given finalizer
func hasFinalizer(obj runtime.Object, finalizer string) bool {
	if obj == nil || reflect.ValueOf(obj).IsNil() {
		return false
	}

	accessor, _ := meta.Accessor(obj)
	for _, f := range accessor.GetFinalizers() {
		if f == finalizer {
			return true
		}
	}

	return false
}

// removeFinalizer removes a finalizer from the list. It mutates its input.
func removeFinalizer(obj runtime.Object, finalizerName string) bool {
	if obj == nil || reflect.ValueOf(obj).IsNil() {
		return false
	}

	newFinalizers := []string{}
	accessor, _ := meta.Accessor(obj)
	found := false
	for _, finalizer := range accessor.GetFinalizers() {
		if finalizer == finalizerName {
			found = true
			continue
		}
		newFinalizers = append(newFinalizers, finalizer)
	}
	if found {
		accessor.SetFinalizers(newFinalizers)
	}
	return found
}

// pendingFinalization returns true if the DeletionTimestamp of the object is set
func pendingFinalization(obj runtime.Object) bool {
	if obj == nil || reflect.ValueOf(obj).IsNil() {
		return false
	}

	accessor, _ := meta.Accessor(obj)
	deletionTimestamp := accessor.GetDeletionTimestamp()

	if deletionTimestamp == nil {
		return false
	}

	if deletionTimestamp.IsZero() {
		return false
	}

	return true
}
