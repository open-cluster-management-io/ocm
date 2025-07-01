package lease

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

func (l *LeaseService) Get(ctx context.Context, resourceID string) (*cloudevents.Event, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(resourceID)
	if err != nil {
		return nil, err
	}
	lease, err := l.lister.Leases(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return l.codec.Encode(services.CloudEventsSourceKube, types.CloudEventsType{CloudEventsDataType: leasece.LeaseEventDataType}, lease)
}

func (l *LeaseService) List(listOpts types.ListOptions) ([]*cloudevents.Event, error) {
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

func (l *LeaseService) RegisterHandler(handler server.EventHandler) {
	if _, err := l.informer.Informer().AddEventHandler(l.EventHandlerFuncs(handler)); err != nil {
		klog.Errorf("failed to register lease informer event handler, %v", err)
	}
}

func (l *LeaseService) EventHandlerFuncs(handler server.EventHandler) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err != nil {
				klog.Errorf("failed to get key for lease %v", err)
				return
			}
			if err := handler.OnCreate(context.Background(), leasece.LeaseEventDataType, key); err != nil {
				klog.Error(err)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(newObj)
			if err != nil {
				klog.Errorf("failed to get key for lease %v", err)
				return
			}
			if err := handler.OnUpdate(context.Background(), leasece.LeaseEventDataType, key); err != nil {
				klog.Error(err)
			}
		},
	}
}
