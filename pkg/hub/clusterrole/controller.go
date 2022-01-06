package clusterrole

import (
	"context"
	"embed"
	"fmt"

	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	"open-cluster-management.io/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	rbacv1informers "k8s.io/client-go/informers/rbac/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	registrationClusterRole = "open-cluster-management:managedcluster:registration"
	workClusterRole         = "open-cluster-management:managedcluster:work"
)

var clusterRoleFiles = []string{
	"manifests/managedcluster-registration-clusterrole.yaml",
	"manifests/managedcluster-work-clusterrole.yaml",
}

//go:embed manifests
var manifestFiles embed.FS

// clusterroleController maintains the necessary clusterroles for registraion and work agent on hub cluster.
type clusterroleController struct {
	kubeClient    kubernetes.Interface
	clusterLister clusterv1listers.ManagedClusterLister
	eventRecorder events.Recorder
}

// NewManagedClusterClusterroleController creates a clusterrole controller on hub cluster.
func NewManagedClusterClusterroleController(
	kubeClient kubernetes.Interface,
	clusterInformer clusterv1informer.ManagedClusterInformer,
	clusterRoleInformer rbacv1informers.ClusterRoleInformer,
	recorder events.Recorder) factory.Controller {
	c := &clusterroleController{
		kubeClient:    kubeClient,
		clusterLister: clusterInformer.Lister(),
		eventRecorder: recorder.WithComponentSuffix("managed-cluster-clusterrole-controller"),
	}
	return factory.New().
		WithFilteredEventsInformers(
			func(obj interface{}) bool {
				clusterRoles := sets.NewString(registrationClusterRole, workClusterRole)
				metaObj := obj.(metav1.Object)
				if clusterRoles.Has(metaObj.GetName()) {
					return true
				}
				return false
			}, clusterRoleInformer.Informer()).
		WithInformers(clusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterClusterRoleController", recorder)
}

func (c *clusterroleController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	managedClusters, err := c.clusterLister.List(labels.Everything())
	if err != nil {
		return err
	}

	// Clean up managedcluser cluserroles if there are no managed clusters
	if len(managedClusters) == 0 {
		return helpers.CleanUpManagedClusterManifests(
			ctx,
			c.kubeClient,
			c.eventRecorder,
			manifestFiles.ReadFile,
			clusterRoleFiles...,
		)
	}

	cache := resourceapply.NewResourceCache()

	// Make sure the managedcluser cluserroles are existed if there are clusters
	results := resourceapply.ApplyDirectly(
		ctx,
		resourceapply.NewKubeClientHolder(c.kubeClient),
		syncCtx.Recorder(),
		cache,
		manifestFiles.ReadFile,
		clusterRoleFiles...,
	)

	errs := []error{}
	for _, result := range results {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}
