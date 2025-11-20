package work

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"
	"github.com/google/go-cmp/cmp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	kubetypes "k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

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

	"open-cluster-management.io/ocm/pkg/server/services"
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

	work = work.DeepCopy()
	return w.codec.Encode(services.CloudEventsSourceKube, types.CloudEventsType{CloudEventsDataType: payload.ManifestBundleEventDataType}, work)
}

func (w *WorkService) List(listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	works, err := w.workLister.ManifestWorks(listOpts.ClusterName).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var evts []*cloudevents.Event
	for _, work := range works {
		work = work.DeepCopy()
		// use the work generation as the work cloudevent resource version
		work.ResourceVersion = fmt.Sprintf("%d", work.Generation)
		evt, err := w.codec.Encode(services.CloudEventsSourceKube, types.CloudEventsType{CloudEventsDataType: payload.ManifestBundleEventDataType}, work)
		if err != nil {
			return nil, err
		}

		evts = append(evts, evt)
	}

	return evts, nil
}

func (w *WorkService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	logger := klog.FromContext(ctx)

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
	if apierrors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	logger.V(4).Info("handle work event",
		"manifestWorkNamespace", last.Namespace, "manifestWorkName", last.Name,
		"subResource", eventType.SubResource, "actionType", eventType.Action)

	workPatcher := patcher.NewPatcher[
		*workv1.ManifestWork, workv1.ManifestWorkSpec, workv1.ManifestWorkStatus](
		w.workClient.WorkV1().ManifestWorks(clusterName))

	switch eventType.Action {
	case types.UpdateRequestAction:
		// the work was deleted by agent, remove its finalizers
		if meta.IsStatusConditionTrue(work.Status.Conditions, common.ResourceDeleted) {
			return workPatcher.RemoveFinalizer(ctx, last, workv1.ManifestWorkFinalizer)
		}

		if last.Generation > int64(resourceVersion) {
			// work could have been changed, do nothing.
			return nil
		}

		// the work was handled by agent, ensure it has the manifestwork finalizer
		if _, err := workPatcher.AddFinalizer(ctx, last, workv1.ManifestWorkFinalizer); err != nil {
			return err
		}

		_, err = workPatcher.PatchStatus(ctx, last, work.Status, last.Status)
		return err
	default:
		return fmt.Errorf("unsupported action %s for work %s/%s", eventType.Action, work.Namespace, work.Name)
	}
}

func (w *WorkService) RegisterHandler(ctx context.Context, handler server.EventHandler) {
	logger := klog.FromContext(ctx)
	if _, err := w.workInformer.Informer().AddEventHandler(w.EventHandlerFuncs(ctx, handler)); err != nil {
		logger.Error(err, "failed to register work informer event handler")
	}
}

func (w *WorkService) EventHandlerFuncs(ctx context.Context, handler server.EventHandler) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc:    handleOnCreateFunc(ctx, handler),
		UpdateFunc: handleOnUpdateFunc(ctx, handler),
		DeleteFunc: handleOnDeleteFunc(ctx, handler),
	}
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

	return nil, apierrors.NewNotFound(common.ManifestWorkGR, string(uid))
}

func handleOnCreateFunc(ctx context.Context, handler server.EventHandler) func(obj interface{}) {
	return func(obj interface{}) {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to get accessor for work")
			return
		}
		id := accessor.GetNamespace() + "/" + accessor.GetName()
		if err := handler.OnCreate(ctx, payload.ManifestBundleEventDataType, id); err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to create work",
				"manifestWork", accessor.GetName(), "manifestWorkNamespace", accessor.GetNamespace())
		}
	}
}

func handleOnUpdateFunc(ctx context.Context, handler server.EventHandler) func(oldObj, newObj interface{}) {
	return func(oldObj, newObj interface{}) {
		oldAccessor, err := meta.Accessor(oldObj)
		if err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to get accessor for work")
			return
		}
		newAccessor, err := meta.Accessor(newObj)
		if err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to get accessor for work")
			return
		}

		// the manifestwork is not changed and is not deleting
		if cmp.Equal(oldAccessor.GetLabels(), newAccessor.GetLabels()) &&
			cmp.Equal(oldAccessor.GetAnnotations(), newAccessor.GetAnnotations()) &&
			oldAccessor.GetGeneration() >= newAccessor.GetGeneration() &&
			newAccessor.GetDeletionTimestamp().IsZero() {
			return
		}

		id := newAccessor.GetNamespace() + "/" + newAccessor.GetName()
		if err := handler.OnUpdate(ctx, payload.ManifestBundleEventDataType, id); err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to update work",
				"manifestWork", newAccessor.GetName(), "manifestWorkNamespace", newAccessor.GetNamespace())
		}
	}
}

func handleOnDeleteFunc(ctx context.Context, handler server.EventHandler) func(obj interface{}) {
	return func(obj interface{}) {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to get accessor for work")
			return
		}
		id := accessor.GetNamespace() + "/" + accessor.GetName()
		if err := handler.OnDelete(ctx, payload.ManifestBundleEventDataType, id); err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to delete work",
				"manifestWork", accessor.GetName(), "manifestWorkNamespace", accessor.GetNamespace())
		}
	}
}
