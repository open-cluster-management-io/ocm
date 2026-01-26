package lease

import (
	"context"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	leasev1 "k8s.io/client-go/informers/coordination/v1"
	"k8s.io/client-go/kubernetes"
	leaselister "k8s.io/client-go/listers/coordination/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	leasece "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"

	"open-cluster-management.io/ocm/pkg/server/services"
)

type LeaseService struct {
	client   kubernetes.Interface
	informer leasev1.LeaseInformer
	lister   leaselister.LeaseLister
	codec    *leasece.LeaseCodec
}

func NewLeaseService(client kubernetes.Interface, informer leasev1.LeaseInformer) server.Service {
	return &LeaseService{
		client:   client,
		informer: informer,
		lister:   informer.Lister(),
		codec:    leasece.NewLeaseCodec(),
	}
}

func (l *LeaseService) List(ctx context.Context, listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	leases, err := l.lister.Leases(listOpts.ClusterName).List(labels.SelectorFromSet(labels.Set{
		clusterv1.ClusterNameLabelKey: listOpts.ClusterName,
	}))
	if err != nil {
		return nil, err
	}

	var cloudevts []*cloudevents.Event
	for _, lease := range leases {
		cloudevt, err := l.codec.Encode(services.CloudEventsSourceKube, types.CloudEventsType{CloudEventsDataType: leasece.LeaseEventDataType}, lease)
		if err != nil {
			return nil, err
		}
		cloudevts = append(cloudevts, cloudevt)
	}
	return cloudevts, nil
}

func (l *LeaseService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}
	lease, err := l.codec.Decode(evt)
	if err != nil {
		return err
	}

	klog.V(4).Infof("lease %s/%s %s %s", lease.Namespace, lease.Name, eventType.SubResource, eventType.Action)

	switch eventType.Action {
	case types.UpdateRequestAction:
		_, err := l.client.CoordinationV1().Leases(lease.Namespace).Update(ctx, lease, metav1.UpdateOptions{})
		return err
	default:
		return fmt.Errorf("unsupported action %s for lease %s/%s", eventType.Action, lease.Namespace, lease.Name)
	}
}

func (l *LeaseService) RegisterHandler(ctx context.Context, handler server.EventHandler) {
	logger := klog.FromContext(ctx)
	if _, err := l.informer.Informer().AddEventHandler(l.EventHandlerFuncs(ctx, handler)); err != nil {
		logger.Error(err, "failed to register lease informer event handler")
	}
}

// TODO handle type check error and event handler error
func (l *LeaseService) EventHandlerFuncs(ctx context.Context, handler server.EventHandler) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			lease, ok := obj.(*coordinationv1.Lease)
			if !ok {
				utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", obj), "lease add")
				return
			}

			eventTypes := types.CloudEventsType{
				CloudEventsDataType: leasece.LeaseEventDataType,
				SubResource:         types.SubResourceSpec,
				Action:              types.CreateRequestAction,
			}
			evt, err := l.codec.Encode(services.CloudEventsSourceKube, eventTypes, lease)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to encode lease", "namespace", lease.Namespace, "name", lease.Name)
				return
			}

			if err := handler.HandleEvent(ctx, evt); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to create lease", "namespace", lease.Namespace, "name", lease.Name)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			lease, ok := newObj.(*coordinationv1.Lease)
			if !ok {
				utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", newObj), "lease update")
				return
			}

			eventTypes := types.CloudEventsType{
				CloudEventsDataType: leasece.LeaseEventDataType,
				SubResource:         types.SubResourceSpec,
				Action:              types.UpdateRequestAction,
			}
			evt, err := l.codec.Encode(services.CloudEventsSourceKube, eventTypes, lease)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to encode lease", "namespace", lease.Namespace, "name", lease.Name)
				return
			}

			if err := handler.HandleEvent(ctx, evt); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to update lease", "namespace", lease.Namespace, "name", lease.Name)
			}
		},
		DeleteFunc: func(obj interface{}) {
			lease, ok := obj.(*coordinationv1.Lease)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", obj), "lease delete")
					return
				}

				lease, ok = tombstone.Obj.(*coordinationv1.Lease)
				if !ok {
					utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", obj), "lease delete")
					return
				}
			}

			lease = lease.DeepCopy()
			if lease.DeletionTimestamp.IsZero() {
				lease.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			}

			eventTypes := types.CloudEventsType{
				CloudEventsDataType: leasece.LeaseEventDataType,
				SubResource:         types.SubResourceSpec,
				Action:              types.DeleteRequestAction,
			}
			evt, err := l.codec.Encode(services.CloudEventsSourceKube, eventTypes, lease)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to encode lease", "namespace", lease.Namespace, "name", lease.Name)
				return
			}

			if err := handler.HandleEvent(ctx, evt); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to delete lease", "namespace", lease.Namespace, "name", lease.Name)
			}
		},
	}
}
