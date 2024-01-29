package client

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1lister "open-cluster-management.io/api/client/work/listers/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/agent/codec"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/watcher"
)

const ManifestsDeleted = "Deleted"

const (
	UpdateRequestAction = "update_request"
	DeleteRequestAction = "delete_request"
)

// ManifestWorkAgentClient implements the ManifestWorkInterface. It sends the manifestworks status back to source by
// CloudEventAgentClient.
type ManifestWorkAgentClient struct {
	cloudEventsClient *generic.CloudEventAgentClient[*workv1.ManifestWork]
	watcher           *watcher.ManifestWorkWatcher
	lister            workv1lister.ManifestWorkNamespaceLister
}

var manifestWorkGR = schema.GroupResource{Group: workv1.GroupName, Resource: "manifestworks"}

var _ workv1client.ManifestWorkInterface = &ManifestWorkAgentClient{}

func NewManifestWorkAgentClient(cloudEventsClient *generic.CloudEventAgentClient[*workv1.ManifestWork], watcher *watcher.ManifestWorkWatcher) *ManifestWorkAgentClient {
	return &ManifestWorkAgentClient{
		cloudEventsClient: cloudEventsClient,
		watcher:           watcher,
	}
}

func (c *ManifestWorkAgentClient) SetLister(lister workv1lister.ManifestWorkNamespaceLister) {
	c.lister = lister
}

func (c *ManifestWorkAgentClient) Create(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.CreateOptions) (*workv1.ManifestWork, error) {
	return nil, errors.NewMethodNotSupported(manifestWorkGR, "create")
}

func (c *ManifestWorkAgentClient) Update(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.UpdateOptions) (*workv1.ManifestWork, error) {
	return nil, errors.NewMethodNotSupported(manifestWorkGR, "update")
}

func (c *ManifestWorkAgentClient) UpdateStatus(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.UpdateOptions) (*workv1.ManifestWork, error) {
	return nil, errors.NewMethodNotSupported(manifestWorkGR, "updatestatus")
}

func (c *ManifestWorkAgentClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return errors.NewMethodNotSupported(manifestWorkGR, "delete")
}

func (c *ManifestWorkAgentClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(manifestWorkGR, "deletecollection")
}

func (c *ManifestWorkAgentClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*workv1.ManifestWork, error) {
	klog.V(4).Infof("getting manifestwork %s", name)
	return c.lister.Get(name)
}

func (c *ManifestWorkAgentClient) List(ctx context.Context, opts metav1.ListOptions) (*workv1.ManifestWorkList, error) {
	klog.V(4).Infof("sync manifestworks")
	// send resync request to fetch manifestworks from source when the ManifestWorkInformer starts
	if err := c.cloudEventsClient.Resync(ctx, types.SourceAll); err != nil {
		return nil, err
	}

	return &workv1.ManifestWorkList{}, nil
}

func (c *ManifestWorkAgentClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	// TODO (skeeey) consider resync the manifestworks when the ManifestWorkInformer reconnected
	return c.watcher, nil
}

func (c *ManifestWorkAgentClient) Patch(ctx context.Context, name string, pt kubetypes.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *workv1.ManifestWork, err error) {
	klog.V(4).Infof("patching manifestwork %s", name)

	lastWork, err := c.lister.Get(name)
	if err != nil {
		return nil, err
	}

	patchedWork, err := utils.Patch(pt, lastWork, data)
	if err != nil {
		return nil, err
	}

	eventDataType, err := types.ParseCloudEventsDataType(patchedWork.Annotations[codec.CloudEventsDataTypeAnnotationKey])
	if err != nil {
		return nil, err
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: *eventDataType,
		SubResource:         types.SubResourceStatus,
	}

	newWork := patchedWork.DeepCopy()

	statusUpdated, err := isStatusUpdate(subresources)
	if err != nil {
		return nil, err
	}

	if statusUpdated {
		eventType.Action = UpdateRequestAction
		if err := c.cloudEventsClient.Publish(ctx, eventType, newWork); err != nil {
			return nil, err
		}

		// refresh the work status in the ManifestWorkInformer local cache with patched work.
		c.watcher.Receive(watch.Event{Type: watch.Modified, Object: newWork})
		return newWork, nil
	}

	// the finalizers of a deleting manifestwork are removed, marking the manifestwork status to deleted and sending
	// it back to source
	if !newWork.DeletionTimestamp.IsZero() && len(newWork.Finalizers) == 0 {
		meta.SetStatusCondition(&newWork.Status.Conditions, metav1.Condition{
			Type:    ManifestsDeleted,
			Status:  metav1.ConditionTrue,
			Reason:  "ManifestsDeleted",
			Message: fmt.Sprintf("The manifests are deleted from the cluster %s", newWork.Namespace),
		})

		eventType.Action = DeleteRequestAction
		if err := c.cloudEventsClient.Publish(ctx, eventType, newWork); err != nil {
			return nil, err
		}

		// delete the manifestwork from the ManifestWorkInformer local cache.
		c.watcher.Receive(watch.Event{Type: watch.Deleted, Object: newWork})
		return newWork, nil
	}

	// refresh the work in the ManifestWorkInformer local cache with patched work.
	c.watcher.Receive(watch.Event{Type: watch.Modified, Object: newWork})
	return newWork, nil
}

func isStatusUpdate(subresources []string) (bool, error) {
	if len(subresources) == 0 {
		return false, nil
	}

	if len(subresources) == 1 && subresources[0] == "status" {
		return true, nil
	}

	return false, fmt.Errorf("unsupported subresources %v", subresources)
}
