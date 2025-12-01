package clusterrole

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/apimachinery/pkg/labels"
	rbacv1informers "k8s.io/client-go/informers/rbac/v1"
	"k8s.io/client-go/kubernetes"

	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/common/apply"
	"open-cluster-management.io/ocm/pkg/common/queue"
	commonrecorder "open-cluster-management.io/ocm/pkg/common/recorder"
	"open-cluster-management.io/ocm/pkg/registration/helpers"
	"open-cluster-management.io/ocm/pkg/registration/hub/manifests"
)

const (
	registrationClusterRole = "open-cluster-management:managedcluster:registration"
	workClusterRole         = "open-cluster-management:managedcluster:work"
)

// clusterroleController maintains the necessary clusterroles for registration and work agent on hub cluster.
type clusterroleController struct {
	kubeClient    kubernetes.Interface
	clusterLister clusterv1listers.ManagedClusterLister
	applier       *apply.PermissionApplier
	cache         resourceapply.ResourceCache
	labels        map[string]string
}

// NewManagedClusterClusterroleController creates a clusterrole controller on hub cluster.
func NewManagedClusterClusterroleController(
	kubeClient kubernetes.Interface,
	clusterInformer clusterv1informer.ManagedClusterInformer,
	clusterRoleInformer rbacv1informers.ClusterRoleInformer,
	labels map[string]string) factory.Controller {

	// Creating a deep copy of the labels to avoid controllers from reading the same map concurrently.
	deepCopyLabels := make(map[string]string, len(labels))
	for k, v := range labels {
		deepCopyLabels[k] = v
	}
	c := &clusterroleController{
		kubeClient:    kubeClient,
		clusterLister: clusterInformer.Lister(),
		cache:         resourceapply.NewResourceCache(),
		applier: apply.NewPermissionApplier(
			kubeClient,
			nil,
			nil,
			clusterRoleInformer.Lister(),
			nil,
		),
		labels: deepCopyLabels,
	}
	return factory.New().
		WithFilteredEventsInformers(
			queue.FilterByNames(registrationClusterRole, workClusterRole),
			clusterRoleInformer.Informer()).
		WithInformers(clusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterClusterRoleController")
}

func (c *clusterroleController) sync(ctx context.Context, syncCtx factory.SyncContext, _ string) error {
	managedClusters, err := c.clusterLister.List(labels.Everything())
	if err != nil {
		return err
	}
	var errs []error
	assetFn := helpers.ManagedClusterAssetFnWithAccepted(manifests.RBACManifests, "", false, c.labels)

	recorderWrapper := commonrecorder.NewEventsRecorderWrapper(ctx, syncCtx.Recorder())
	// Clean up managedcluser cluserroles if there are no managed clusters
	if len(managedClusters) == 0 {
		results := resourceapply.DeleteAll(
			ctx,
			resourceapply.NewKubeClientHolder(c.kubeClient),
			recorderWrapper,
			assetFn,
			manifests.CommonClusterRoleFiles...,
		)
		for _, result := range results {
			if result.Error != nil {
				errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
			}
		}
		return operatorhelpers.NewMultiLineAggregate(errs)
	}

	// Make sure the managedcluser cluserroles are existed if there are clusters
	results := c.applier.Apply(
		ctx,
		syncCtx.Recorder(),
		assetFn,
		manifests.CommonClusterRoleFiles...,
	)

	for _, result := range results {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}
