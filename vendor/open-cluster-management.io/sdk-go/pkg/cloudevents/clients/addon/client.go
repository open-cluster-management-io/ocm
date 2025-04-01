package addon

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned/typed/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"
	cloudeventserrors "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/errors"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// ManagedClusterAddOnClient implements the ManagedClusterAddonInterface. It sends the ManagedCluster status back to source by
// CloudEventAgentClient.
type ManagedClusterAddOnClient struct {
	sync.RWMutex

	cloudEventsClient *generic.CloudEventAgentClient[*addonapiv1alpha1.ManagedClusterAddOn]
	watcherStore      store.ClientWatcherStore[*addonapiv1alpha1.ManagedClusterAddOn]
	namespace         string
}

var _ addonv1alpha1client.ManagedClusterAddOnInterface = &ManagedClusterAddOnClient{}

func NewManagedClusterAddOnClient(
	cloudEventsClient *generic.CloudEventAgentClient[*addonapiv1alpha1.ManagedClusterAddOn],
	watcherStore store.ClientWatcherStore[*addonapiv1alpha1.ManagedClusterAddOn],
) *ManagedClusterAddOnClient {
	return &ManagedClusterAddOnClient{
		cloudEventsClient: cloudEventsClient,
		watcherStore:      watcherStore,
	}
}

func (c *ManagedClusterAddOnClient) Namespace(name string) *ManagedClusterAddOnClient {
	c.namespace = name
	return c
}

func (c *ManagedClusterAddOnClient) Create(
	ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn, opts metav1.CreateOptions) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	return nil, errors.NewMethodNotSupported(common.ManagedClusterGR, "update")

}

func (c *ManagedClusterAddOnClient) Update(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn, opts metav1.UpdateOptions) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	return nil, errors.NewMethodNotSupported(common.ManagedClusterGR, "update")
}

func (c *ManagedClusterAddOnClient) UpdateStatus(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn, opts metav1.UpdateOptions) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	return nil, errors.NewMethodNotSupported(common.ManagedClusterGR, "updatestatus")
}

func (c *ManagedClusterAddOnClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return errors.NewMethodNotSupported(common.ManagedClusterGR, "delete")
}

func (c *ManagedClusterAddOnClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(common.ManagedClusterGR, "deletecollection")
}

func (c *ManagedClusterAddOnClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	klog.V(4).Infof("getting ManagedCluster %s", name)
	addon, exists, err := c.watcherStore.Get(c.namespace, name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if !exists {
		return nil, errors.NewNotFound(common.ManagedClusterGR, name)
	}

	return addon, nil
}

func (c *ManagedClusterAddOnClient) List(ctx context.Context, opts metav1.ListOptions) (*addonapiv1alpha1.ManagedClusterAddOnList, error) {
	klog.V(4).Info("list ManagedClusterAddon")
	addonList, err := c.watcherStore.List(c.namespace, opts)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	items := []addonapiv1alpha1.ManagedClusterAddOn{}
	for _, cluster := range addonList.Items {
		items = append(items, *cluster)
	}

	return &addonapiv1alpha1.ManagedClusterAddOnList{ListMeta: addonList.ListMeta, Items: items}, nil
}

func (c *ManagedClusterAddOnClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	klog.V(4).Info("watch ManagedClusterAddOn")
	watcher, err := c.watcherStore.GetWatcher(c.namespace, opts)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	return watcher, nil
}

func (c *ManagedClusterAddOnClient) Patch(
	ctx context.Context, name string, pt kubetypes.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	klog.V(4).Infof("patching ManagedClusterAddon %s", name)
	lastCluster, exists, err := c.watcherStore.Get("", name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if !exists {
		return nil, errors.NewNotFound(common.ManagedClusterGR, name)
	}

	patchedAddon, err := utils.Patch(pt, lastCluster, data)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: ManagedClusterAddOnEventDataType,
		SubResource:         types.SubResourceStatus,
	}

	newAddon := patchedAddon.DeepCopy()

	statusUpdated, err := isStatusUpdate(subresources)
	if err != nil {
		return nil, errors.NewGenericServerResponse(http.StatusMethodNotAllowed, "patch", common.ManagedClusterGR, name, err.Error(), 0, false)
	}

	if statusUpdated {
		// avoid race conditions among the agent's go routines
		c.Lock()
		defer c.Unlock()

		eventType.Action = common.UpdateRequestAction
		// publish the status update event to source, source will check the resource version
		// and reject the update if it's status update is outdated.
		if err := c.cloudEventsClient.Publish(ctx, eventType, newAddon); err != nil {
			return nil, cloudeventserrors.ToStatusError(common.ManagedClusterGR, name, err)
		}

		// Fetch the latest cluster from the store and verify the resource version to avoid updating the store
		// with outdated cluster. Return a conflict error if the resource version is outdated.
		// Due to the lack of read-modify-write guarantees in the store, race conditions may occur between
		// this update operation and one from the agent informer after receiving the event from the source.
		lastCluster, exists, err := c.watcherStore.Get("", name)
		if err != nil {
			return nil, errors.NewInternalError(err)
		}
		if !exists {
			return nil, errors.NewNotFound(common.ManagedClusterGR, name)
		}
		lastResourceVersion, err := strconv.ParseInt(lastCluster.GetResourceVersion(), 10, 64)
		if err != nil {
			return nil, errors.NewInternalError(err)
		}
		newResourceVersion, err := strconv.ParseInt(newAddon.GetResourceVersion(), 10, 64)
		if err != nil {
			return nil, errors.NewInternalError(err)
		}
		// ensure the resource version of the cluster is not outdated
		if newResourceVersion < lastResourceVersion {
			// It's safe to return a conflict error here, even if the status update event
			// has already been sent. The source may reject the update due to an outdated resource version.
			return nil, errors.NewConflict(common.ManagedClusterGR, name, fmt.Errorf("the resource version of the cluster is outdated"))
		}
		if err := c.watcherStore.Update(newAddon); err != nil {
			return nil, errors.NewInternalError(err)
		}

		return newAddon, nil
	}

	if err := c.watcherStore.Update(newAddon); err != nil {
		return nil, errors.NewInternalError(err)
	}

	return newAddon, nil
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
