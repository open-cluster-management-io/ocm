package gc

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	rbacv1listers "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/klog/v2"

	informerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/registration/helpers"
	"open-cluster-management.io/ocm/pkg/registration/hub/manifests"
)

var clusterRbacFiles = []string{
	"rbac/managedcluster-clusterrole.yaml",
	"rbac/managedcluster-clusterrolebinding.yaml",
	"rbac/managedcluster-registration-rolebinding.yaml",
	"rbac/managedcluster-work-rolebinding.yaml",
}

const (
	manifestWorkFinalizer = "cluster.open-cluster-management.io/manifest-work-cleanup"
)

var (
	mclClusterRoleName = func(clusterName string) string {
		return fmt.Sprintf("open-cluster-management:managedcluster:%s", clusterName)
	}
	mclClusterRoleBindingName = func(clusterName string) string {
		return fmt.Sprintf("open-cluster-management:managedcluster:%s", clusterName)
	}
	registrationRoleBindingName = func(clusterName string) string {
		return fmt.Sprintf("open-cluster-management:managedcluster:%s:registration", clusterName)
	}
	workRoleBindingName = func(clusterName string) string {
		return fmt.Sprintf("open-cluster-management:managedcluster:%s:work", clusterName)
	}
)

type gcClusterRbacController struct {
	kubeClient                       kubernetes.Interface
	clusterLister                    clusterv1listers.ManagedClusterLister
	clusterRoleLister                rbacv1listers.ClusterRoleLister
	clusterRoleBingLister            rbacv1listers.ClusterRoleBindingLister
	roleBindingLister                rbacv1listers.RoleBindingLister
	manifestWorkLister               worklister.ManifestWorkLister
	clusterPatcher                   patcher.Patcher[*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus]
	eventRecorder                    events.Recorder
	resourceCleanupFeatureGateEnable bool
}

// newGCClusterRoleController ensures the rbac of agent are deleted after cluster is deleted.
func newGCClusterRbacController(
	kubeClient kubernetes.Interface,
	clusterPatcher patcher.Patcher[*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus],
	clusterInformer informerv1.ManagedClusterInformer,
	clusterRoleLister rbacv1listers.ClusterRoleLister,
	clusterRoleBindingLister rbacv1listers.ClusterRoleBindingLister,
	roleBindingLister rbacv1listers.RoleBindingLister,
	manifestWorkLister worklister.ManifestWorkLister,
	eventRecorder events.Recorder,
	resourceCleanupFeatureGateEnable bool,
) gcReconciler {

	return &gcClusterRbacController{
		kubeClient:                       kubeClient,
		clusterLister:                    clusterInformer.Lister(),
		clusterRoleLister:                clusterRoleLister,
		clusterRoleBingLister:            clusterRoleBindingLister,
		roleBindingLister:                roleBindingLister,
		manifestWorkLister:               manifestWorkLister,
		clusterPatcher:                   clusterPatcher,
		eventRecorder:                    eventRecorder.WithComponentSuffix("gc-cluster-rbac"),
		resourceCleanupFeatureGateEnable: resourceCleanupFeatureGateEnable,
	}
}

func (r *gcClusterRbacController) reconcile(ctx context.Context, cluster *clusterv1.ManagedCluster) (gcReconcileOp, error) {
	if cluster.DeletionTimestamp.IsZero() {
		_, err := r.clusterPatcher.AddFinalizer(ctx, cluster, clusterv1.ManagedClusterFinalizer)
		return gcReconcileStop, err
	}

	if err := r.removeClusterRbac(ctx, cluster.Name); err != nil {
		return gcReconcileContinue, err
	}

	works, err := r.manifestWorkLister.ManifestWorks(cluster.Name).List(labels.Everything())
	if err != nil && !errors.IsNotFound(err) {
		return gcReconcileStop, err
	}
	if len(works) != 0 {
		klog.V(2).Infof("cluster %s is deleting, waiting %d works in the cluster namespace to be deleted.",
			cluster.Name, len(works))

		// remove finalizer to delete the cluster for backwards compatible.
		if !r.resourceCleanupFeatureGateEnable {
			return gcReconcileStop, r.clusterPatcher.RemoveFinalizer(ctx, cluster, clusterv1.ManagedClusterFinalizer)
		}
		return gcReconcileRequeue, nil
	}

	if err = r.removeFinalizerFromWorkRoleBinding(ctx, cluster.Name, manifestWorkFinalizer); err != nil {
		return gcReconcileStop, err
	}

	r.eventRecorder.Eventf("ManagedClusterGC",
		"managed cluster %s is deleting and the cluster rbac are deleted", cluster.Name)

	return gcReconcileContinue, r.clusterPatcher.RemoveFinalizer(ctx, cluster, clusterv1.ManagedClusterFinalizer)
}

func (r *gcClusterRbacController) removeClusterRbac(ctx context.Context, clusterName string) error {
	var errs []error
	// Clean up managed cluster manifests
	assetFn := helpers.ManagedClusterAssetFn(manifests.RBACManifests, clusterName)
	resourceResults := resourceapply.DeleteAll(ctx, resourceapply.NewKubeClientHolder(r.kubeClient),
		r.eventRecorder, assetFn, clusterRbacFiles...)
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}
	return operatorhelpers.NewMultiLineAggregate(errs)
}

func (r *gcClusterRbacController) removeFinalizerFromWorkRoleBinding(ctx context.Context,
	clusterName, finalizer string) error {
	workRoleBinding, err := r.roleBindingLister.RoleBindings(clusterName).Get(workRoleBindingName(clusterName))
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	roleBindingFinalizerPatcher := patcher.NewPatcher[*v1.RoleBinding, v1.RoleBinding,
		v1.RoleBinding](r.kubeClient.RbacV1().RoleBindings(clusterName))
	return roleBindingFinalizerPatcher.RemoveFinalizer(ctx, workRoleBinding, finalizer)

}
