package addon

import (
	"context"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	addonce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"

	"open-cluster-management.io/ocm/pkg/server/services"
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

func (s *AddonService) List(ctx context.Context, listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	addons, err := s.addonLister.ManagedClusterAddOns(listOpts.ClusterName).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var evts []*cloudevents.Event
	for _, addon := range addons {
		evt, err := s.codec.Encode(services.CloudEventsSourceKube, types.CloudEventsType{CloudEventsDataType: addonce.ManagedClusterAddOnEventDataType}, addon)
		if err != nil {
			return nil, err
		}
		evts = append(evts, evt)
	}

	return evts, nil
}

func (s *AddonService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}

	addon, err := s.codec.Decode(evt)
	if err != nil {
		return err
	}

	klog.V(4).Infof("addon %s/%s status update", addon.Namespace, addon.Name)

	switch eventType.Action {
	case types.UpdateRequestAction:
		_, err := s.addonClient.AddonV1alpha1().ManagedClusterAddOns(addon.Namespace).UpdateStatus(ctx, addon, metav1.UpdateOptions{})
		return err
	default:
		return fmt.Errorf("unsupported action %s for addon %s/%s", eventType.Action, addon.Namespace, addon.Name)
	}
}

func (s *AddonService) RegisterHandler(ctx context.Context, handler server.EventHandler) {
	logger := klog.FromContext(ctx)
	if _, err := s.addonInformer.Informer().AddEventHandler(s.EventHandlerFuncs(ctx, handler)); err != nil {
		logger.Error(err, "failed to register addon informer event handler")
	}
}

// TODO handle type check error and event handler error
func (s *AddonService) EventHandlerFuncs(ctx context.Context, handler server.EventHandler) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addon, ok := obj.(*addonv1alpha1.ManagedClusterAddOn)
			if !ok {
				utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", obj), "addon add")
				return
			}

			createEventTypes := types.CloudEventsType{
				CloudEventsDataType: addonce.ManagedClusterAddOnEventDataType,
				SubResource:         types.SubResourceSpec,
				Action:              types.CreateRequestAction,
			}
			evt, err := s.codec.Encode(services.CloudEventsSourceKube, createEventTypes, addon)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to encode addon",
					"namespace", addon.Namespace, "name", addon.Name)
				return
			}

			if err := handler.HandleEvent(ctx, evt); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to create addon",
					"namespace", addon.Namespace, "name", addon.Name)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			addon, ok := newObj.(*addonv1alpha1.ManagedClusterAddOn)
			if !ok {
				utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", newObj), "addon add")
				return
			}

			updateEventTypes := types.CloudEventsType{
				CloudEventsDataType: addonce.ManagedClusterAddOnEventDataType,
				SubResource:         types.SubResourceSpec,
				Action:              types.UpdateRequestAction,
			}
			evt, err := s.codec.Encode(services.CloudEventsSourceKube, updateEventTypes, addon)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to encode addon",
					"namespace", addon.Namespace, "name", addon.Name)
				return
			}

			if err := handler.HandleEvent(ctx, evt); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to update addon",
					"namespace", addon.Namespace, "name", addon.Name)
			}
		},
		DeleteFunc: func(obj interface{}) {
			addon, ok := obj.(*addonv1alpha1.ManagedClusterAddOn)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", obj), "addon delete")
					return
				}

				addon, ok = tombstone.Obj.(*addonv1alpha1.ManagedClusterAddOn)
				if !ok {
					utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", obj), "addon delete")
					return
				}
			}

			addon = addon.DeepCopy()
			if addon.DeletionTimestamp.IsZero() {
				addon.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			}

			deleteEventTypes := types.CloudEventsType{
				CloudEventsDataType: addonce.ManagedClusterAddOnEventDataType,
				SubResource:         types.SubResourceSpec,
				Action:              types.DeleteRequestAction,
			}
			evt, err := s.codec.Encode(services.CloudEventsSourceKube, deleteEventTypes, addon)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to encode addon",
					"namespace", addon.Namespace, "name", addon.Name)
				return
			}

			if err := handler.HandleEvent(ctx, evt); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to delete addon",
					"namespace", addon.Namespace, "name", addon.Name)
			}
		},
	}
}
