package client

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/common"
	workerrors "open-cluster-management.io/sdk-go/pkg/cloudevents/work/errors"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/utils"
)

// ManifestWorkAgentClient implements the ManifestWorkInterface. It sends the manifestworks status back to source by
// CloudEventAgentClient.
type ManifestWorkAgentClient struct {
	sync.RWMutex

	cloudEventsClient *generic.CloudEventAgentClient[*workv1.ManifestWork]
	watcherStore      store.WorkClientWatcherStore

	// this namespace should be same with the cluster name to which this client subscribes
	namespace string
}

var _ workv1client.ManifestWorkInterface = &ManifestWorkAgentClient{}

func NewManifestWorkAgentClient(
	cloudEventsClient *generic.CloudEventAgentClient[*workv1.ManifestWork],
	watcherStore store.WorkClientWatcherStore,
	clusterName string,
) *ManifestWorkAgentClient {
	return &ManifestWorkAgentClient{
		cloudEventsClient: cloudEventsClient,
		watcherStore:      watcherStore,
	}
}

func (c *ManifestWorkAgentClient) SetNamespace(namespace string) {
	c.namespace = namespace
}

func (c *ManifestWorkAgentClient) Create(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.CreateOptions) (*workv1.ManifestWork, error) {
	return nil, errors.NewMethodNotSupported(common.ManifestWorkGR, "create")
}

func (c *ManifestWorkAgentClient) Update(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.UpdateOptions) (*workv1.ManifestWork, error) {
	return nil, errors.NewMethodNotSupported(common.ManifestWorkGR, "update")
}

func (c *ManifestWorkAgentClient) UpdateStatus(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.UpdateOptions) (*workv1.ManifestWork, error) {
	return nil, errors.NewMethodNotSupported(common.ManifestWorkGR, "updatestatus")
}

func (c *ManifestWorkAgentClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return errors.NewMethodNotSupported(common.ManifestWorkGR, "delete")
}

func (c *ManifestWorkAgentClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(common.ManifestWorkGR, "deletecollection")
}

func (c *ManifestWorkAgentClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*workv1.ManifestWork, error) {
	klog.V(4).Infof("getting manifestwork %s/%s", c.namespace, name)
	work, exists, err := c.watcherStore.Get(c.namespace, name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if !exists {
		return nil, errors.NewNotFound(common.ManifestWorkGR, name)
	}

	return work, nil
}

func (c *ManifestWorkAgentClient) List(ctx context.Context, opts metav1.ListOptions) (*workv1.ManifestWorkList, error) {
	klog.V(4).Infof("list manifestworks from cluster %s", c.namespace)
	works, err := c.watcherStore.List(c.namespace, opts)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	return works, nil
}

func (c *ManifestWorkAgentClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	klog.V(4).Infof("watch manifestworks from cluster %s", c.namespace)
	watcher, err := c.watcherStore.GetWatcher(c.namespace, opts)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	return watcher, nil
}

func (c *ManifestWorkAgentClient) Patch(ctx context.Context, name string, pt kubetypes.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *workv1.ManifestWork, err error) {
	klog.V(4).Infof("patching manifestwork %s/%s", c.namespace, name)
	lastWork, exists, err := c.watcherStore.Get(c.namespace, name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if !exists {
		return nil, errors.NewNotFound(common.ManifestWorkGR, name)
	}

	patchedWork, err := utils.Patch(pt, lastWork, data)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	eventDataType, err := types.ParseCloudEventsDataType(patchedWork.Annotations[common.CloudEventsDataTypeAnnotationKey])
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: *eventDataType,
		SubResource:         types.SubResourceStatus,
	}

	newWork := patchedWork.DeepCopy()

	statusUpdated, err := isStatusUpdate(subresources)
	if err != nil {
		return nil, errors.NewGenericServerResponse(http.StatusMethodNotAllowed, "patch", common.ManifestWorkGR, name, err.Error(), 0, false)
	}

	if statusUpdated {
		// avoid race conditions among the agent's go routines
		c.Lock()
		defer c.Unlock()

		eventType.Action = common.UpdateRequestAction
		// publish the status update event to source, source will check the resource version
		// and reject the update if it's status update is outdated.
		if err := c.cloudEventsClient.Publish(ctx, eventType, newWork); err != nil {
			return nil, workerrors.NewPublishError(common.ManifestWorkGR, name, err)
		}

		// Fetch the latest work from the store and verify the resource version to avoid updating the store
		// with outdated work. Return a conflict error if the resource version is outdated.
		// Due to the lack of read-modify-write guarantees in the store, race conditions may occur between
		// this update operation and one from the agent informer after receiving the event from the source.
		latestWork, exists, err := c.watcherStore.Get(c.namespace, name)
		if err != nil {
			return nil, errors.NewInternalError(err)
		}
		if !exists {
			return nil, errors.NewNotFound(common.ManifestWorkGR, name)
		}
		lastResourceVersion, err := strconv.ParseInt(latestWork.GetResourceVersion(), 10, 64)
		if err != nil {
			return nil, errors.NewInternalError(err)
		}
		newResourceVersion, err := strconv.ParseInt(newWork.GetResourceVersion(), 10, 64)
		if err != nil {
			return nil, errors.NewInternalError(err)
		}
		// ensure the resource version of the work is not outdated
		if newResourceVersion < lastResourceVersion {
			// It's safe to return a conflict error here, even if the status update event
			// has already been sent. The source may reject the update due to an outdated resource version.
			return nil, errors.NewConflict(common.ManifestWorkGR, name, fmt.Errorf("the resource version of the work is outdated"))
		}
		if err := c.watcherStore.Update(newWork); err != nil {
			return nil, errors.NewInternalError(err)
		}
		return newWork, nil
	}

	// the finalizers of a deleting manifestwork are removed, marking the manifestwork status to deleted and sending
	// it back to source
	if !newWork.DeletionTimestamp.IsZero() && len(newWork.Finalizers) == 0 {
		meta.SetStatusCondition(&newWork.Status.Conditions, metav1.Condition{
			Type:    common.ManifestsDeleted,
			Status:  metav1.ConditionTrue,
			Reason:  "ManifestsDeleted",
			Message: fmt.Sprintf("The manifests are deleted from the cluster %s", newWork.Namespace),
		})

		eventType.Action = common.DeleteRequestAction
		if err := c.cloudEventsClient.Publish(ctx, eventType, newWork); err != nil {
			return nil, workerrors.NewPublishError(common.ManifestWorkGR, name, err)
		}

		if err := c.watcherStore.Delete(newWork); err != nil {
			return nil, errors.NewInternalError(err)
		}

		return newWork, nil
	}

	if err := c.watcherStore.Update(newWork); err != nil {
		return nil, errors.NewInternalError(err)
	}

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
