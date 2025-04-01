package cluster

import (
	"context"
	"net/http"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"
	cloudeventserrors "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/errors"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// ManagedClusterClient implements the ManagedClusterInterface. It sends the ManagedCluster status back to source by
// CloudEventAgentClient.
type ManagedClusterClient struct {
	cloudEventsClient *generic.CloudEventAgentClient[*clusterv1.ManagedCluster]
	watcherStore      store.ClientWatcherStore[*clusterv1.ManagedCluster]
}

var _ clusterv1client.ManagedClusterInterface = &ManagedClusterClient{}

func NewManagedClusterClient(
	cloudEventsClient *generic.CloudEventAgentClient[*clusterv1.ManagedCluster],
	watcherStore store.ClientWatcherStore[*clusterv1.ManagedCluster],
	clusterName string,
) *ManagedClusterClient {
	return &ManagedClusterClient{
		cloudEventsClient: cloudEventsClient,
		watcherStore:      watcherStore,
	}
}

func (c *ManagedClusterClient) Create(ctx context.Context, cluster *clusterv1.ManagedCluster, opts metav1.CreateOptions) (*clusterv1.ManagedCluster, error) {
	klog.V(4).Infof("creating ManagedCluster %s", cluster.Name)
	_, exists, err := c.watcherStore.Get("", cluster.Name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if exists {
		return nil, errors.NewAlreadyExists(common.ManagedClusterGR, cluster.Name)
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: ManagedClusterEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              types.CreateRequestAction,
	}

	// TODO: validate the ManagedCluster

	if err := c.cloudEventsClient.Publish(ctx, eventType, cluster); err != nil {
		return nil, cloudeventserrors.ToStatusError(common.ManagedClusterGR, cluster.Name, err)
	}

	return cluster.DeepCopy(), nil

}

func (c *ManagedClusterClient) Update(ctx context.Context, cluster *clusterv1.ManagedCluster, opts metav1.UpdateOptions) (*clusterv1.ManagedCluster, error) {
	return nil, errors.NewMethodNotSupported(common.ManagedClusterGR, "update")
}

func (c *ManagedClusterClient) UpdateStatus(ctx context.Context, cluster *clusterv1.ManagedCluster, opts metav1.UpdateOptions) (*clusterv1.ManagedCluster, error) {
	return nil, errors.NewMethodNotSupported(common.ManagedClusterGR, "updatestatus")
}

func (c *ManagedClusterClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return errors.NewMethodNotSupported(common.ManagedClusterGR, "delete")
}

func (c *ManagedClusterClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(common.ManagedClusterGR, "deletecollection")
}

func (c *ManagedClusterClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*clusterv1.ManagedCluster, error) {
	klog.V(4).Infof("getting ManagedCluster %s", name)
	cluster, exists, err := c.watcherStore.Get("", name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if !exists {
		return nil, errors.NewNotFound(common.ManagedClusterGR, name)
	}

	return cluster, nil
}

func (c *ManagedClusterClient) List(ctx context.Context, opts metav1.ListOptions) (*clusterv1.ManagedClusterList, error) {
	klog.V(4).Info("list ManagedCluster")
	clusterList, err := c.watcherStore.List("", opts)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	items := []clusterv1.ManagedCluster{}
	for _, cluster := range clusterList.Items {
		items = append(items, *cluster)
	}

	return &clusterv1.ManagedClusterList{ListMeta: clusterList.ListMeta, Items: items}, nil
}

func (c *ManagedClusterClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	klog.V(4).Info("watch ManagedCluster")
	watcher, err := c.watcherStore.GetWatcher("", opts)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	return watcher, nil
}

func (c *ManagedClusterClient) Patch(ctx context.Context, name string, pt kubetypes.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *clusterv1.ManagedCluster, err error) {
	klog.V(4).Infof("patching ManagedCluster %s", name)
	lastCluster, exists, err := c.watcherStore.Get("", name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if !exists {
		return nil, errors.NewNotFound(common.ManagedClusterGR, name)
	}

	patchedCluster, err := utils.Patch(pt, lastCluster, data)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: ManagedClusterEventDataType,
		SubResource:         types.SubResourceStatus,
	}

	newCluster := patchedCluster.DeepCopy()

	if !utils.IsStatusPatch(subresources) {
		msg := "subresources \"status\" is required"
		return nil, errors.NewGenericServerResponse(http.StatusMethodNotAllowed, "patch", common.ManagedClusterGR, name, msg, 0, false)
	}

	// publish the status update event to source, source will check the resource version
	// and reject the update if it's status update is outdated.
	eventType.Action = types.UpdateRequestAction
	if err := c.cloudEventsClient.Publish(ctx, eventType, newCluster); err != nil {
		return nil, cloudeventserrors.ToStatusError(common.ManagedClusterGR, name, err)
	}

	return newCluster, nil
}
