package placement

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	clusterinformerv1 "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1alpha1 "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterlisterv1 "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1"
	clusterlisterv1alpha1 "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1alpha1"
)

const (
	clusterSetLabel = "cluster.open-cluster-management.io/clusterset"
)

// placementController makes placement decisions for Placements
type placementController struct {
	clusterLister    clusterlisterv1.ManagedClusterLister
	clusterSetLister clusterlisterv1alpha1.ManagedClusterSetLister
}

// NewPlacementController return an instance of placementController
func NewPlacementController(
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1alpha1.ManagedClusterSetInformer,
	recorder events.Recorder,
) factory.Controller {
	c := placementController{
		clusterLister:    clusterInformer.Lister(),
		clusterSetLister: clusterSetInformer.Lister(),
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return fmt.Sprintf("cluster:%s", accessor.GetName())
		}, func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}

			// ignore cluster belongs to no clusterset
			labels := accessor.GetLabels()
			clusterSetName, ok := labels[clusterSetLabel]
			if !ok {
				return false
			}

			// ignore cluster if its clusterset does not exist
			_, err = c.clusterSetLister.Get(clusterSetName)
			return err == nil
		}, clusterInformer.Informer()).
		WithSync(c.sync).
		ToController("PlacementController", recorder)
}

func (c *placementController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	return nil
}
