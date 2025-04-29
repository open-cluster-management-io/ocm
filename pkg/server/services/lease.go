package services

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
)

type LeaseService struct {
	client   kubernetes.Interface
	informer leasev1.LeaseInformer
	lister   leaselister.LeaseLister
	codec    leasece.LeaseCodec
}

func (l LeaseService) Get(ctx context.Context, resourceID string) (*cloudevents.Event, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(resourceID)
	if err != nil {
		return nil, err
	}
	lease, err := l.lister.Leases(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return l.codec.Encode(source, types.CloudEventsType{CloudEventsDataType: leasece.LeaseEventDataType}, lease)
}

func (l LeaseService) List(listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	if len(listOpts.ClusterName) == 0 {
		return nil, fmt.Errorf("cluster name is empty")
	}

	selector := labels.SelectorFromSet(labels.Set{
		clusterv1.ClusterNameLabelKey: listOpts.ClusterName,
	})
	leases, err := l.lister.Leases(listOpts.ClusterName).List(selector)
	if err != nil {
		return nil, err
	}
	var cloudevts []*cloudevents.Event
	for _, lease := range leases {
		cloudevt, err := l.codec.Encode(source, types.CloudEventsType{CloudEventsDataType: leasece.LeaseEventDataType}, lease)
		if err != nil {
			return nil, err
		}
		cloudevts = append(cloudevts, cloudevt)
	}
	return cloudevts, nil
}

func (l LeaseService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}
	lease, err := l.codec.Decode(evt)
	if err != nil {
		return err
	}

	klog.V(4).Infof("lease status update (%s) %s/%s", eventType.Action, lease.Namespace, lease.Name)

	// only create and update action
	switch eventType.Action {
	case updateRequestAction:
		_, err := l.client.CoordinationV1().Leases(lease.Namespace).Update(ctx, lease, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (l LeaseService) RegisterHandler(handler server.EventHandler) {
	l.informer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			if err := handler.OnCreate(context.Background(), leasece.LeaseEventDataType, key); err != nil {
				klog.Error(err)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			key, _ := cache.MetaNamespaceKeyFunc(newObj)
			if err := handler.OnUpdate(context.Background(), leasece.LeaseEventDataType, key); err != nil {
				klog.Error(err)
			}
		},
		// agent does not need to care about delete event
	})
}

func NewLeaseService(client kubernetes.Interface, informer leasev1.LeaseInformer) *LeaseService {
	return &LeaseService{
		client:   client,
		informer: informer,
		lister:   informer.Lister(),
	}
}

var _ server.Service = &LeaseService{}
