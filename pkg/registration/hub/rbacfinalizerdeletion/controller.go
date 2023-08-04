package rbacfinalizerdeletion

import (
	"context"
	"reflect"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	corev1informers "k8s.io/client-go/informers/core/v1"
	rbacv1client "k8s.io/client-go/kubernetes/typed/rbac/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	rbacv1listers "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	informerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

type finalizeController struct {
	roleBindingLister  rbacv1listers.RoleBindingLister
	rbacClient         rbacv1client.RbacV1Interface
	clusterLister      clusterv1listers.ManagedClusterLister
	namespaceLister    corelisters.NamespaceLister
	manifestWorkLister worklister.ManifestWorkLister
	eventRecorder      events.Recorder
}

// NewFinalizeController ensures all manifestworks are deleted before rolebinding for work
// agent are deleted in a terminating cluster namespace.
func NewFinalizeController(
	roleBindingLister rbacv1listers.RoleBindingLister,
	namespaceInformer corev1informers.NamespaceInformer,
	clusterInformer informerv1.ManagedClusterInformer,
	manifestWorkLister worklister.ManifestWorkLister,
	rbacClient rbacv1client.RbacV1Interface,
	eventRecorder events.Recorder,
) factory.Controller {

	controller := &finalizeController{
		roleBindingLister:  roleBindingLister,
		namespaceLister:    namespaceInformer.Lister(),
		clusterLister:      clusterInformer.Lister(),
		manifestWorkLister: manifestWorkLister,
		rbacClient:         rbacClient,
		eventRecorder:      eventRecorder,
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaNamespaceName, clusterInformer.Informer(), namespaceInformer.Informer()).
		WithSync(controller.sync).ToController("FinalizeController", eventRecorder)
}

func (m *finalizeController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	key := controllerContext.QueueKey()
	if key == "" {
		return nil
	}

	_, clusterName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil
	}

	cluster, err := m.clusterLister.Get(clusterName)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	ns, err := m.namespaceLister.Get(clusterName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// There are two possible cases that we need to remove finalizers on rolebindings based on
	// clean of manifestworks.
	// 1. The namespace is finalizing.
	// 2. The cluster is finalizing or not found.
	if !ns.DeletionTimestamp.IsZero() || cluster == nil ||
		(cluster != nil && !cluster.DeletionTimestamp.IsZero()) {
		works, err := m.manifestWorkLister.ManifestWorks(ns.Name).List(labels.Everything())
		if err != nil {
			return err
		}

		if len(works) != 0 {
			controllerContext.Queue().AddAfter(clusterName, 10*time.Second)
			klog.Warningf("still having %d works in the cluster namespace %s", len(works), ns.Name)
			return nil
		}
		return m.syncRoleBindings(ctx, controllerContext, clusterName)
	}
	return nil
}

func (m *finalizeController) syncRoleBindings(ctx context.Context, controllerContext factory.SyncContext,
	namespace string) error {
	requirement, _ := labels.NewRequirement(clusterv1.ClusterNameLabelKey, selection.Exists, []string{})
	selector := labels.NewSelector().Add(*requirement)
	roleBindings, err := m.roleBindingLister.RoleBindings(namespace).List(selector)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	for _, roleBinding := range roleBindings {
		// Skip if roleBinding has no the finalizer
		if !hasFinalizer(roleBinding, workapiv1.ManifestWorkFinalizer) {
			continue
		}
		// remove finalizer from roleBinding
		if pendingFinalization(roleBinding) {
			if err := m.removeFinalizerFromRoleBinding(ctx, roleBinding, workapiv1.ManifestWorkFinalizer); err != nil {
				return err
			}
		}
	}
	return nil
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

	var newFinalizers []string
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
