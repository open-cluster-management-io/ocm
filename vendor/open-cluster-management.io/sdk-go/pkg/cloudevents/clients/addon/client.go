package addon

import (
	"context"
	"net/http"

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

// ManagedClusterAddOnClient implements the ManagedClusterAddonInterface.
type ManagedClusterAddOnClient struct {
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

func (c *ManagedClusterAddOnClient) Namespace(namespace string) *ManagedClusterAddOnClient {
	c.namespace = namespace
	return c
}

func (c *ManagedClusterAddOnClient) Create(
	ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn, opts metav1.CreateOptions) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	return nil, errors.NewMethodNotSupported(common.ManagedClusterAddOnGR, "create")
}

func (c *ManagedClusterAddOnClient) Update(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn, opts metav1.UpdateOptions) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	return nil, errors.NewMethodNotSupported(common.ManagedClusterAddOnGR, "update")
}

func (c *ManagedClusterAddOnClient) UpdateStatus(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn, opts metav1.UpdateOptions) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	return nil, errors.NewMethodNotSupported(common.ManagedClusterAddOnGR, "updatestatus")
}

func (c *ManagedClusterAddOnClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return errors.NewMethodNotSupported(common.ManagedClusterAddOnGR, "delete")
}

func (c *ManagedClusterAddOnClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(common.ManagedClusterAddOnGR, "deletecollection")
}

func (c *ManagedClusterAddOnClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	klog.V(4).Infof("getting ManagedClusterAddOn %s/%s", c.namespace, name)
	addon, exists, err := c.watcherStore.Get(c.namespace, name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if !exists {
		return nil, errors.NewNotFound(common.ManagedClusterAddOnGR, c.namespace+"/"+name)
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
	klog.V(4).Infof("patching ManagedClusterAddon %s/%s", c.namespace, name)
	last, exists, err := c.watcherStore.Get(c.namespace, name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if !exists {
		return nil, errors.NewNotFound(common.ManagedClusterAddOnGR, c.namespace+"/"+name)
	}

	patchedAddon, err := utils.Patch(pt, last, data)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: ManagedClusterAddOnEventDataType,
		SubResource:         types.SubResourceStatus,
	}

	newAddon := patchedAddon.DeepCopy()

	if !utils.IsStatusPatch(subresources) {
		msg := "subresources \"status\" is required"
		return nil, errors.NewGenericServerResponse(http.StatusMethodNotAllowed, "patch", common.ManagedClusterAddOnGR, name, msg, 0, false)
	}

	// publish the status update event to source, source will check the resource version
	// and reject the update if it's status update is outdated.
	eventType.Action = types.UpdateRequestAction
	if err := c.cloudEventsClient.Publish(ctx, eventType, newAddon); err != nil {
		return nil, cloudeventserrors.ToStatusError(common.ManagedClusterAddOnGR, name, err)
	}

	return newAddon, nil
}
