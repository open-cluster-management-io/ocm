package services

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	addonce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
)

type AddonService struct {
	addonClient   addonclientset.Interface
	addonLister   addonlisterv1alpha1.ManagedClusterAddOnLister
	addonInformer addoninformerv1alpha1.ManagedClusterAddOnInformer
	codec         *addonce.ManagedClusterAddOnCodec
}

func NewAddonService(addonClient addonclientset.Interface, addonInformer addoninformerv1alpha1.ManagedClusterAddOnInformer) server.Service {
	return &AddonService{
		addonClient:   addonClient,
		addonLister:   addonInformer.Lister(),
		addonInformer: addonInformer,
		codec:         addonce.NewManagedClusterAddOnCodec(),
	}
}

func (c *AddonService) Get(_ context.Context, resourceID string) (*cloudevents.Event, error) {
	namespace, name, _ := cache.SplitMetaNamespaceKey(resourceID)
	addon, err := c.addonLister.ManagedClusterAddOns(namespace).Get(name)
	if err != nil {
		return nil, err
	}

	evt, err := c.codec.Encode(source, types.CloudEventsType{CloudEventsDataType: addonce.ManagedClusterAddOnEventDataType}, addon)
	if err != nil {
		return nil, err
	}

	return evt, nil
}

func (c *AddonService) List(listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	var evts []*cloudevents.Event
	//only get single cluster
	addons, err := c.addonLister.ManagedClusterAddOns(listOpts.ClusterName).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, addon := range addons {
		evt, err := c.codec.Encode(source, types.CloudEventsType{CloudEventsDataType: addonce.ManagedClusterAddOnEventDataType}, addon)
		if err != nil {
			return nil, err
		}
		evts = append(evts, evt)
	}

	return evts, nil
}

// q if there is resourceVersion, this will return directly to the agent as conflict?
func (c *AddonService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}
	addon, err := c.codec.Decode(evt)
	if err != nil {
		return err
	}

	// only create and update action
	switch eventType.Action {
	case updateRequestAction:
		if eventType.SubResource == types.SubResourceStatus {
			_, err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(addon.Namespace).UpdateStatus(ctx, addon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *AddonService) RegisterHandler(handler server.EventHandler) {
	c.addonInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			if err := handler.OnCreate(context.Background(), addonce.ManagedClusterAddOnEventDataType, key); err != nil {
				klog.Error(err)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			key, _ := cache.MetaNamespaceKeyFunc(newObj)
			if err := handler.OnUpdate(context.Background(), addonce.ManagedClusterAddOnEventDataType, key); err != nil {
				klog.Error(err)
			}
		},
		// agent does not need to care about delete event
	})
}
