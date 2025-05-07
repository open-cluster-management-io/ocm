package services

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubetypes "k8s.io/apimachinery/pkg/types"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklisters "open-cluster-management.io/api/client/work/listers/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/source/codec"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

type WorkService struct {
	workClient   workclient.Interface
	workInformer workinformers.ManifestWorkInformer
	workLister   worklisters.ManifestWorkLister
	codec        *codec.ManifestBundleCodec
}

var _ server.Service = &WorkService{}

func NewWorkService(
	workClient workclient.Interface,
	workInformer workinformers.ManifestWorkInformer,
) *WorkService {
	return &WorkService{
		workClient:   workClient,
		workInformer: workInformer,
		workLister:   workInformer.Lister(),
		codec:        codec.NewManifestBundleCodec(),
	}
}

func (w *WorkService) Get(ctx context.Context, resourceID string) (*cloudevents.Event, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(resourceID)
	if err != nil {
		return nil, err
	}
	work, err := w.workLister.ManifestWorks(namespace).Get(name)
	if err != nil {
		return nil, err
	}

	evt, err := w.codec.Encode(source, types.CloudEventsType{CloudEventsDataType: payload.ManifestBundleEventDataType}, work)
	if err != nil {
		return nil, err
	}

	return evt, nil
}

func (w *WorkService) List(listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	works, err := w.workLister.ManifestWorks(listOpts.ClusterName).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var evts []*cloudevents.Event
	for _, work := range works {
		evt, err := w.codec.Encode(source, types.CloudEventsType{CloudEventsDataType: payload.ManifestBundleEventDataType}, work)
		if err != nil {
			return nil, err
		}

		evts = append(evts, evt)
	}

	return evts, nil
}

func (w *WorkService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}

	clusterName, err := cloudeventstypes.ToString(evt.Extensions()[types.ExtensionClusterName])
	if err != nil {
		return fmt.Errorf("failed to get cluster name, %v", err)
	}

	resourceVersion, err := cloudeventstypes.ToInteger(evt.Extensions()[types.ExtensionResourceVersion])
	if err != nil {
		return fmt.Errorf("failed to get resource version, %v", err)
	}

	work, err := w.codec.Decode(evt)
	if err != nil {
		return err
	}

	last, err := w.getWorkByUID(clusterName, work.UID)
	if errors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	lastResourceVersion, err := cloudeventstypes.ParseInteger(last.ResourceVersion)
	if err != nil {
		return err
	}

	klog.V(4).Infof("work status update (%s) %s/%s lastResourceVersion=%d resourceVersion=%d",
		eventType.Action, last.Namespace, last.Name, lastResourceVersion, resourceVersion)

	workPatcher := patcher.NewPatcher[
		*workv1.ManifestWork, workv1.ManifestWorkSpec, workv1.ManifestWorkStatus](
		w.workClient.WorkV1().ManifestWorks(clusterName))

	switch eventType.Action {
	case updateRequestAction:
		if lastResourceVersion > resourceVersion {
			// work could have been changed, do nothing.
			return nil
		}

		// the work was handled by agent, ensure it has the resource finalizer
		if _, err := workPatcher.AddFinalizer(ctx, last, common.ResourceFinalizer); err != nil {
			return err
		}

		if _, err = workPatcher.PatchStatus(ctx, last, work.Status, last.Status); err != nil {
			return err
		}
	case deleteRequestAction:
		if err := workPatcher.RemoveFinalizer(ctx, last, common.ResourceFinalizer); err != nil {
			return err
		}
	}
	return nil
}

func (w *WorkService) RegisterHandler(handler server.EventHandler) {
	w.workInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			accessor, _ := meta.Accessor(obj)
			id := accessor.GetNamespace() + "/" + accessor.GetName()
			if err := handler.OnCreate(context.Background(), payload.ManifestBundleEventDataType, id); err != nil {
				klog.Error(err)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			accessor, _ := meta.Accessor(newObj)
			id := accessor.GetNamespace() + "/" + accessor.GetName()
			if err := handler.OnUpdate(context.Background(), payload.ManifestBundleEventDataType, id); err != nil {
				klog.Error(err)
			}
		},
		DeleteFunc: func(obj interface{}) {
			accessor, _ := meta.Accessor(obj)
			id := accessor.GetNamespace() + "/" + accessor.GetName()
			if err := handler.OnDelete(context.Background(), payload.ManifestBundleEventDataType, id); err != nil {
				klog.Error(err)
			}
		},
	})
}

func (w *WorkService) getWorkByUID(clusterName string, uid kubetypes.UID) (*workv1.ManifestWork, error) {
	works, err := w.workLister.ManifestWorks(clusterName).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	for _, work := range works {
		if work.UID == uid {
			return work, nil
		}
	}

	return nil, errors.NewNotFound(common.ManifestWorkGR, string(uid))
}
