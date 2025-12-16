package v1beta1

import (
	"context"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"net/http"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"

	addonv1beta1client "open-cluster-management.io/api/client/addon/clientset/versioned/typed/addon/v1beta1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"
	cloudeventserrors "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/errors"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// ManagedClusterAddOnClient implements the ManagedClusterAddonInterface.
type ManagedClusterAddOnClient struct {
	cloudEventsClient generic.CloudEventsClient[*addonapiv1beta1.ManagedClusterAddOn]
	watcherStore      store.ClientWatcherStore[*addonapiv1beta1.ManagedClusterAddOn]
	namespace         string
}

var _ addonv1beta1client.ManagedClusterAddOnInterface = &ManagedClusterAddOnClient{}

func NewManagedClusterAddOnClient(
	cloudEventsClient generic.CloudEventsClient[*addonapiv1beta1.ManagedClusterAddOn],
	watcherStore store.ClientWatcherStore[*addonapiv1beta1.ManagedClusterAddOn],
) *ManagedClusterAddOnClient {
	return &ManagedClusterAddOnClient{
		cloudEventsClient: cloudEventsClient,
		watcherStore:      watcherStore,
	}
}

func (c *ManagedClusterAddOnClient) Namespace(namespace string) *ManagedClusterAddOnClient {
	return &ManagedClusterAddOnClient{
		cloudEventsClient: c.cloudEventsClient,
		watcherStore:      c.watcherStore,
		namespace:         namespace,
	}
}

func (c *ManagedClusterAddOnClient) Create(
	_ context.Context, _ *addonapiv1beta1.ManagedClusterAddOn, _ metav1.CreateOptions) (*addonapiv1beta1.ManagedClusterAddOn, error) {
	return nil, errors.NewMethodNotSupported(common.ManagedClusterAddOnGR, "create")
}

func (c *ManagedClusterAddOnClient) Update(_ context.Context, _ *addonapiv1beta1.ManagedClusterAddOn, _ metav1.UpdateOptions) (*addonapiv1beta1.ManagedClusterAddOn, error) {
	return nil, errors.NewMethodNotSupported(common.ManagedClusterAddOnGR, "update")
}

func (c *ManagedClusterAddOnClient) UpdateStatus(_ context.Context, _ *addonapiv1beta1.ManagedClusterAddOn, _ metav1.UpdateOptions) (*addonapiv1beta1.ManagedClusterAddOn, error) {
	return nil, errors.NewMethodNotSupported(common.ManagedClusterAddOnGR, "updatestatus")
}

func (c *ManagedClusterAddOnClient) Delete(_ context.Context, _ string, _ metav1.DeleteOptions) error {
	return errors.NewMethodNotSupported(common.ManagedClusterAddOnGR, "delete")
}

func (c *ManagedClusterAddOnClient) DeleteCollection(_ context.Context, _ metav1.DeleteOptions, _ metav1.ListOptions) error {
	return errors.NewMethodNotSupported(common.ManagedClusterAddOnGR, "deletecollection")
}

func (c *ManagedClusterAddOnClient) Get(ctx context.Context, name string, _ metav1.GetOptions) (*addonapiv1beta1.ManagedClusterAddOn, error) {
	logger := klog.FromContext(ctx)
	logger.V(4).Info("getting ManagedClusterAddOn", "namespace", c.namespace, "name", name)
	addon, exists, err := c.watcherStore.Get(c.namespace, name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if !exists {
		return nil, errors.NewNotFound(common.ManagedClusterAddOnGR, c.namespace+"/"+name)
	}

	return addon, nil
}

func (c *ManagedClusterAddOnClient) List(ctx context.Context, opts metav1.ListOptions) (*addonapiv1beta1.ManagedClusterAddOnList, error) {
	logger := klog.FromContext(ctx)
	logger.V(4).Info("list ManagedClusterAddon")
	addonList, err := c.watcherStore.List(c.namespace, opts)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	items := []addonapiv1beta1.ManagedClusterAddOn{}
	for _, cluster := range addonList.Items {
		items = append(items, *cluster)
	}

	return &addonapiv1beta1.ManagedClusterAddOnList{ListMeta: addonList.ListMeta, Items: items}, nil
}

func (c *ManagedClusterAddOnClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	logger := klog.FromContext(ctx)
	logger.V(4).Info("watch ManagedClusterAddOn")
	watcher, err := c.watcherStore.GetWatcher(c.namespace, opts)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	return watcher, nil
}

func (c *ManagedClusterAddOnClient) Patch(
	ctx context.Context, name string, pt kubetypes.PatchType, data []byte, _ metav1.PatchOptions, subresources ...string) (*addonapiv1beta1.ManagedClusterAddOn, error) {
	logger := klog.FromContext(ctx)
	logger.V(4).Info("patching ManagedClusterAddon", "namespace", c.namespace, "name", name)
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

// AddonClientWrapper wraps ManagedClusterAddOnClient to AddonV1beta1Interface
type AddonClientWrapper struct {
	client *ManagedClusterAddOnClient
}

var _ addonv1beta1client.AddonV1beta1Interface = &AddonClientWrapper{}

func NewAddonClientWrapper(client *ManagedClusterAddOnClient) *AddonClientWrapper {
	return &AddonClientWrapper{client: client}
}

func (c *AddonClientWrapper) ClusterManagementAddOns() addonv1beta1client.ClusterManagementAddOnInterface {
	panic("ClusterManagementAddOns is unsupported")
}

func (c *AddonClientWrapper) RESTClient() rest.Interface {
	panic("RESTClient is unsupported")
}

func (c *AddonClientWrapper) ManagedClusterAddOns(namespace string) addonv1beta1client.ManagedClusterAddOnInterface {
	return c.client.Namespace(namespace)
}
