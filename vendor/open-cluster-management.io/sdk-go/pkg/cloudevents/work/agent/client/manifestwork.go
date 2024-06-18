package client

import (
	"context"
	"fmt"

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
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/utils"
)

// ManifestWorkAgentClient implements the ManifestWorkInterface. It sends the manifestworks status back to source by
// CloudEventAgentClient.
type ManifestWorkAgentClient struct {
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
	return c.watcherStore.Get(c.namespace, name)
}

func (c *ManifestWorkAgentClient) List(ctx context.Context, opts metav1.ListOptions) (*workv1.ManifestWorkList, error) {
	klog.V(4).Infof("list manifestworks from cluster %s", c.namespace)
	works, err := c.watcherStore.List(c.namespace, opts)
	if err != nil {
		return nil, err
	}

	items := []workv1.ManifestWork{}
	for _, work := range works {
		items = append(items, *work)
	}

	return &workv1.ManifestWorkList{Items: items}, nil
}

func (c *ManifestWorkAgentClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	klog.V(4).Infof("watch manifestworks from cluster %s", c.namespace)
	return c.watcherStore.GetWatcher(c.namespace, opts)
}

func (c *ManifestWorkAgentClient) Patch(ctx context.Context, name string, pt kubetypes.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *workv1.ManifestWork, err error) {
	klog.V(4).Infof("patching manifestwork %s/%s", c.namespace, name)
	lastWork, err := c.watcherStore.Get(c.namespace, name)
	if err != nil {
		return nil, err
	}

	patchedWork, err := utils.Patch(pt, lastWork, data)
	if err != nil {
		return nil, err
	}

	eventDataType, err := types.ParseCloudEventsDataType(patchedWork.Annotations[common.CloudEventsDataTypeAnnotationKey])
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
		eventType.Action = common.UpdateRequestAction
		if err := c.cloudEventsClient.Publish(ctx, eventType, newWork); err != nil {
			return nil, err
		}

		if err := c.watcherStore.Update(newWork); err != nil {
			return nil, err

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
			return nil, err
		}

		if err := c.watcherStore.Delete(newWork); err != nil {
			return nil, err
		}

		return newWork, nil
	}

	if err := c.watcherStore.Update(newWork); err != nil {
		return nil, err
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
