package cluster

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"

	"open-cluster-management.io/ocm/pkg/server/services"
)

type ClusterService struct {
	clusterClient   clusterclient.Interface
	clusterLister   clusterlisterv1.ManagedClusterLister
	clusterInformer clusterinformerv1.ManagedClusterInformer
	codec           *clusterce.ManagedClusterCodec
}

func NewClusterService(clusterClient clusterclient.Interface, clusterInformer clusterinformerv1.ManagedClusterInformer) server.Service {
	return &ClusterService{
		clusterClient:   clusterClient,
		clusterLister:   clusterInformer.Lister(),
		clusterInformer: clusterInformer,
		codec:           clusterce.NewManagedClusterCodec(),
	}
}

func (c *ClusterService) Get(_ context.Context, resourceID string) (*cloudevents.Event, error) {
	cluster, err := c.clusterLister.Get(resourceID)
	if err != nil {
		return nil, err
	}

	return c.codec.Encode(services.CloudEventsSourceKube, types.CloudEventsType{CloudEventsDataType: clusterce.ManagedClusterEventDataType}, cluster)
}

func (c *ClusterService) List(listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	var evts []*cloudevents.Event
	cluster, err := c.clusterLister.Get(listOpts.ClusterName)
	if errors.IsNotFound(err) {
		return evts, nil
	}
	if err != nil {
		return nil, err
	}

	evt, err := c.codec.Encode(services.CloudEventsSourceKube, types.CloudEventsType{CloudEventsDataType: clusterce.ManagedClusterEventDataType}, cluster)
	if err != nil {
		return nil, err
	}

	return append(evts, evt), nil
}

func (c *ClusterService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	logger := klog.FromContext(ctx)

	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}
	cluster, err := c.codec.Decode(evt)
	if err != nil {
		return err
	}

	logger.V(4).Info("handle cluster event",
		"clusterName", cluster.Name, "subResource", eventType.SubResource, "action", eventType.Action)

	switch eventType.Action {
	case types.CreateRequestAction:
		_, err := c.clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		return err
	case types.UpdateRequestAction:
		if eventType.SubResource == types.SubResourceStatus {
			_, err := c.clusterClient.ClusterV1().ManagedClusters().UpdateStatus(ctx, cluster, metav1.UpdateOptions{})
			return err
		}

		_, err := c.clusterClient.ClusterV1().ManagedClusters().Update(ctx, cluster, metav1.UpdateOptions{})
		return err
	default:
		return fmt.Errorf("unsupported action %s for cluster %s", eventType.Action, cluster.Name)
	}
}

func (c *ClusterService) RegisterHandler(ctx context.Context, handler server.EventHandler) {
	logger := klog.FromContext(ctx)
	if _, err := c.clusterInformer.Informer().AddEventHandler(c.EventHandlerFuncs(ctx, handler)); err != nil {
		logger.Error(err, "failed to register cluster informer event handler")
	}
}

func (c *ClusterService) EventHandlerFuncs(ctx context.Context, handler server.EventHandler) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to get accessor for cluster")
				return
			}
			if err := handler.OnCreate(ctx, clusterce.ManagedClusterEventDataType, accessor.GetName()); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to create cluster", "clusterName", accessor.GetName())
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			accessor, err := meta.Accessor(newObj)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to get accessor for cluster")
				return
			}
			if err := handler.OnUpdate(ctx, clusterce.ManagedClusterEventDataType, accessor.GetName()); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to update cluster", "clusterName", accessor.GetName())
			}
		},
	}
}
