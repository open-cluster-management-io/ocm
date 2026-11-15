package cluster

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	clusterce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
	grpcauthn "open-cluster-management.io/sdk-go/pkg/server/grpc/authn"

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

func (c *ClusterService) List(ctx context.Context, listOpts types.ListOptions) ([]*cloudevents.Event, error) {
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
	if err := services.ValidateClusterName(evt, cluster.Name); err != nil {
		return err
	}

	logger.V(4).Info("handle cluster event",
		"clusterName", cluster.Name, "subResource", eventType.SubResource, "action", eventType.Action)

	switch eventType.Action {
	case types.CreateRequestAction:
		if eventType.SubResource != types.SubResourceSpec {
			return fmt.Errorf("unsupported subresource %s for cluster %s", eventType.SubResource, cluster.Name)
		}
		delete(cluster.Labels, clusterv1beta2.ClusterSetLabel)
		pinTrustDomain(ctx, cluster)
		_, err := c.clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
		return err
	case types.UpdateRequestAction:
		if eventType.SubResource == types.SubResourceStatus {
			_, err := c.clusterClient.ClusterV1().ManagedClusters().UpdateStatus(ctx, cluster, metav1.UpdateOptions{})
			return err
		}

		current, err := c.clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		clusterSet, hasClusterSet := current.Labels[clusterv1beta2.ClusterSetLabel]
		if hasClusterSet {
			if cluster.Labels == nil {
				cluster.Labels = map[string]string{}
			}
			cluster.Labels[clusterv1beta2.ClusterSetLabel] = clusterSet
		} else {
			delete(cluster.Labels, clusterv1beta2.ClusterSetLabel)
		}
		// Agents may not change the trust domain the cluster is pinned to, or who registered it;
		// only hub users with update access to ManagedClusters can.
		for _, key := range hubSetAnnotations {
			if value, ok := current.Annotations[key]; ok {
				if cluster.Annotations == nil {
					cluster.Annotations = map[string]string{}
				}
				cluster.Annotations[key] = value
			} else {
				delete(cluster.Annotations, key)
			}
		}

		_, err = c.clusterClient.ClusterV1().ManagedClusters().Update(ctx, cluster, metav1.UpdateOptions{})
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

// TODO handle type check error and event handler error
func (c *ClusterService) EventHandlerFuncs(ctx context.Context, handler server.EventHandler) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cluster, ok := obj.(*clusterv1.ManagedCluster)
			if !ok {
				utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", obj), "cluster add")
				return
			}

			eventTypes := types.CloudEventsType{
				CloudEventsDataType: clusterce.ManagedClusterEventDataType,
				SubResource:         types.SubResourceSpec,
				Action:              types.CreateRequestAction,
			}
			evt, err := c.codec.Encode(services.CloudEventsSourceKube, eventTypes, cluster)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to encode cluster", "clusterName", cluster.Name)
				return
			}

			if err := handler.HandleEvent(ctx, evt); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to create cluster", "clusterName", cluster.Name)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			cluster, ok := newObj.(*clusterv1.ManagedCluster)
			if !ok {
				utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", newObj), "cluster update")
				return
			}

			eventTypes := types.CloudEventsType{
				CloudEventsDataType: clusterce.ManagedClusterEventDataType,
				SubResource:         types.SubResourceSpec,
				Action:              types.UpdateRequestAction,
			}
			evt, err := c.codec.Encode(services.CloudEventsSourceKube, eventTypes, cluster)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to encode cluster", "clusterName", cluster.Name)
				return
			}

			if err := handler.HandleEvent(ctx, evt); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to update cluster", "clusterName", cluster.Name)
			}
		},
	}
}

// hubSetAnnotations are set by the hub from the creating agent's SVID; agents can't set or
// change them.
var hubSetAnnotations = []string{clusterv1.SPIFFETrustDomainAnnotationKey, clusterv1.SPIFFERegisteredByAnnotationKey}

// pinTrustDomain sets the annotations of a cluster an agent creates from the agent's SVID,
// whatever the request says: the trust domain the cluster is pinned to, and the SPIFFE ID that
// registered it, which auto-approval matches. Any other agent may not set them.
func pinTrustDomain(ctx context.Context, cluster *clusterv1.ManagedCluster) {
	for _, key := range hubSetAnnotations {
		delete(cluster.Annotations, key)
	}
	id, ok := grpcauthn.OCMIdentityFromContext(ctx)
	if !ok {
		return
	}
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}
	cluster.Annotations[clusterv1.SPIFFETrustDomainAnnotationKey] = id.TrustDomain
	if sid, ok := ctx.Value(grpcauthn.ContextSPIFFEIDKey).(spiffeid.ID); ok {
		cluster.Annotations[clusterv1.SPIFFERegisteredByAnnotationKey] = sid.String()
	}
}
