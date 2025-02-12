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

	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/registration/helpers"
	"open-cluster-management.io/ocm/pkg/registration/hub/manifests"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

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
	clusterRoleLister                rbacv1listers.ClusterRoleLister
	roleBindingLister                rbacv1listers.RoleBindingLister
	manifestWorkLister               worklister.ManifestWorkLister
	clusterPatcher                   patcher.Patcher[*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus]
	hubDriver                        register.HubDriver
	eventRecorder                    events.Recorder
	resourceCleanupFeatureGateEnable bool
}

// newGCClusterRoleController ensures the rbac of agent are deleted after cluster is deleted.
func newGCClusterRbacController(
	kubeClient kubernetes.Interface,
	clusterPatcher patcher.Patcher[*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus],
	clusterRoleLister rbacv1listers.ClusterRoleLister,
	roleBindingLister rbacv1listers.RoleBindingLister,
	manifestWorkLister worklister.ManifestWorkLister,
	hubDriver register.HubDriver,
	eventRecorder events.Recorder,
	resourceCleanupFeatureGateEnable bool,
) gcReconciler {
	return &gcClusterRbacController{
		kubeClient:                       kubeClient,
		clusterRoleLister:                clusterRoleLister,
		roleBindingLister:                roleBindingLister,
		manifestWorkLister:               manifestWorkLister,
		clusterPatcher:                   clusterPatcher,
		hubDriver:                        hubDriver,
		eventRecorder:                    eventRecorder.WithComponentSuffix("gc-cluster-rbac"),
		resourceCleanupFeatureGateEnable: resourceCleanupFeatureGateEnable,
	}
}

func (r *gcClusterRbacController) reconcile(ctx context.Context,
	cluster *clusterv1.ManagedCluster, clusterNamespace string) (gcReconcileOp, error) {
	if cluster != nil {
		if err := r.removeClusterRbac(ctx, cluster.Name, cluster.Spec.HubAcceptsClient); err != nil {
			return gcReconcileContinue, err
		}

		if err := r.hubDriver.Cleanup(ctx, cluster); err != nil {
			return gcReconcileContinue, err
		}

		// if GC feature gate is disable, the finalizer is removed from the cluster after the related rbac is deleted.
		// there is no need to wait other resources are cleaned up before remove the finalizer.
		if !r.resourceCleanupFeatureGateEnable {
			if err := r.clusterPatcher.RemoveFinalizer(ctx, cluster, clusterv1.ManagedClusterFinalizer); err != nil {
				return gcReconcileStop, err
			}
		}
	}

	works, err := r.manifestWorkLister.ManifestWorks(clusterNamespace).List(labels.Everything())
	if err != nil && !errors.IsNotFound(err) {
		return gcReconcileStop, err
	}
	if len(works) != 0 {
		return gcReconcileRequeue, nil
	}

	return gcReconcileStop, r.removeFinalizerFromWorkRoleBinding(ctx, clusterNamespace, manifestWorkFinalizer)
}

func (r *gcClusterRbacController) removeClusterRbac(ctx context.Context, clusterName string, accepted bool) error {
	var errs []error
	// Clean up managed cluster manifests
	assetFn := helpers.ManagedClusterAssetFnWithAccepted(manifests.RBACManifests, clusterName, accepted)
	resourceResults := resourceapply.DeleteAll(ctx, resourceapply.NewKubeClientHolder(r.kubeClient),
		r.eventRecorder, assetFn, append(manifests.ClusterSpecificRBACFiles, manifests.ClusterSpecificRoleBindings...)...)
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
