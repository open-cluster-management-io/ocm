package client

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"
	cloudeventserrors "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/errors"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// ManifestWorkSourceClient implements the ManifestWorkInterface.
type ManifestWorkSourceClient struct {
	cloudEventsClient *generic.CloudEventSourceClient[*workv1.ManifestWork]
	watcherStore      store.ClientWatcherStore[*workv1.ManifestWork]
	namespace         string
	sourceID          string
}

var _ workv1client.ManifestWorkInterface = &ManifestWorkSourceClient{}

func NewManifestWorkSourceClient(
	sourceID string,
	watcherStore store.ClientWatcherStore[*workv1.ManifestWork],
	cloudEventsClient *generic.CloudEventSourceClient[*workv1.ManifestWork],
) *ManifestWorkSourceClient {
	return &ManifestWorkSourceClient{
		cloudEventsClient: cloudEventsClient,
		watcherStore:      watcherStore,
		sourceID:          sourceID,
	}
}

func (c *ManifestWorkSourceClient) SetNamespace(namespace string) {
	c.namespace = namespace
}

func (c *ManifestWorkSourceClient) Create(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.CreateOptions) (*workv1.ManifestWork, error) {
	if manifestWork.Namespace != "" && manifestWork.Namespace != c.namespace {
		returnErr := errors.NewInvalid(common.ManifestWorkGK, manifestWork.Name, field.ErrorList{
			field.Invalid(
				field.NewPath("metadata").Child("namespace"),
				manifestWork.Namespace,
				fmt.Sprintf("does not match the namespace %s", c.namespace),
			),
		})
		generic.IncreaseWorkProcessedCounter("create", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	_, exists, err := c.watcherStore.Get(c.namespace, manifestWork.Name)
	if err != nil {
		returnErr := errors.NewInternalError(err)
		generic.IncreaseWorkProcessedCounter("create", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}
	if exists {
		returnErr := errors.NewAlreadyExists(common.ManifestWorkGR, manifestWork.Name)
		generic.IncreaseWorkProcessedCounter("create", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	// TODO if we support multiple data type in future, we may need to get the data type from
	// the cloudevents data type annotation
	eventType := types.CloudEventsType{
		CloudEventsDataType: payload.ManifestBundleEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              common.CreateRequestAction,
	}

	newWork := manifestWork.DeepCopy()
	newWork.UID = kubetypes.UID(utils.UID(c.sourceID, common.ManifestWorkGR.String(), c.namespace, newWork.Name))
	newWork.Namespace = c.namespace
	newWork.ResourceVersion = getWorkResourceVersion(manifestWork)

	if err := utils.EncodeManifests(newWork); err != nil {
		returnErr := errors.NewInternalError(err)
		generic.IncreaseWorkProcessedCounter("create", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	if errs := utils.ValidateWork(newWork); len(errs) != 0 {
		returnErr := errors.NewInvalid(common.ManifestWorkGK, manifestWork.Name, errs)
		generic.IncreaseWorkProcessedCounter("create", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	if err := c.cloudEventsClient.Publish(ctx, eventType, newWork); err != nil {
		returnErr := cloudeventserrors.ToStatusError(common.ManifestWorkGR, manifestWork.Name, err)
		generic.IncreaseWorkProcessedCounter("create", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	// add the new work to the local cache.
	if err := c.watcherStore.Add(newWork); err != nil {
		returnErr := errors.NewInternalError(err)
		generic.IncreaseWorkProcessedCounter("create", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	generic.IncreaseWorkProcessedCounter("create", metav1.StatusSuccess)
	return newWork.DeepCopy(), nil
}

func (c *ManifestWorkSourceClient) Update(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.UpdateOptions) (*workv1.ManifestWork, error) {
	return nil, errors.NewMethodNotSupported(common.ManifestWorkGR, "update")
}

func (c *ManifestWorkSourceClient) UpdateStatus(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.UpdateOptions) (*workv1.ManifestWork, error) {
	return nil, errors.NewMethodNotSupported(common.ManifestWorkGR, "updatestatus")
}

func (c *ManifestWorkSourceClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	work, exists, err := c.watcherStore.Get(c.namespace, name)
	if err != nil {
		returnErr := errors.NewInternalError(err)
		generic.IncreaseWorkProcessedCounter("delete", string(returnErr.ErrStatus.Reason))
		return returnErr
	}
	if !exists {
		return nil
	}

	// TODO if we support multiple data type in future, we may need to get the data type from
	// the cloudevents data type annotation
	eventType := types.CloudEventsType{
		CloudEventsDataType: payload.ManifestBundleEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              common.DeleteRequestAction,
	}

	deletingWork := work.DeepCopy()
	now := metav1.Now()
	deletingWork.DeletionTimestamp = &now

	if err := c.cloudEventsClient.Publish(ctx, eventType, deletingWork); err != nil {
		returnErr := cloudeventserrors.ToStatusError(common.ManifestWorkGR, name, err)
		generic.IncreaseWorkProcessedCounter("delete", string(returnErr.ErrStatus.Reason))
		return returnErr
	}

	if len(work.Finalizers) == 0 {
		// the work has no any finalizers, there are two cases in this scenario
		// 1) the agent does not start yet, we delete this work from the local cache directly.
		// 2) the agent is running, but the status response does not be handled by source yet,
		//    after the deleted status is back, we need ignore this work in the ManifestWorkSourceHandler.
		if err := c.watcherStore.Delete(deletingWork); err != nil {
			returnErr := errors.NewInternalError(err)
			generic.IncreaseWorkProcessedCounter("delete", string(returnErr.ErrStatus.Reason))
			return returnErr
		}

		generic.IncreaseWorkProcessedCounter("delete", metav1.StatusSuccess)
		return nil
	}

	// update the work with deletion timestamp in the local cache.
	if err := c.watcherStore.Update(deletingWork); err != nil {
		returnErr := errors.NewInternalError(err)
		generic.IncreaseWorkProcessedCounter("delete", string(returnErr.ErrStatus.Reason))
		return returnErr
	}

	return nil
}

func (c *ManifestWorkSourceClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(common.ManifestWorkGR, "deletecollection")
}

func (c *ManifestWorkSourceClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*workv1.ManifestWork, error) {
	klog.V(4).Infof("getting manifestwork %s", name)
	work, exists, err := c.watcherStore.Get(c.namespace, name)
	if err != nil {
		returnErr := errors.NewInternalError(err)
		generic.IncreaseWorkProcessedCounter("get", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}
	if !exists {
		returnErr := errors.NewNotFound(common.ManifestWorkGR, name)
		generic.IncreaseWorkProcessedCounter("get", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	generic.IncreaseWorkProcessedCounter("get", metav1.StatusSuccess)
	return work, nil
}

func (c *ManifestWorkSourceClient) List(ctx context.Context, opts metav1.ListOptions) (*workv1.ManifestWorkList, error) {
	klog.V(4).Infof("list manifestworks")
	works, err := c.watcherStore.List(c.namespace, opts)
	if err != nil {
		returnErr := errors.NewInternalError(err)
		generic.IncreaseWorkProcessedCounter("list", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	generic.IncreaseWorkProcessedCounter("list", metav1.StatusSuccess)
	items := []workv1.ManifestWork{}
	for _, work := range works.Items {
		items = append(items, *work)
	}

	return &workv1.ManifestWorkList{ListMeta: works.ListMeta, Items: items}, nil
}

func (c *ManifestWorkSourceClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	watcher, err := c.watcherStore.GetWatcher(c.namespace, opts)
	if err != nil {
		returnErr := errors.NewInternalError(err)
		generic.IncreaseWorkProcessedCounter("watch", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	generic.IncreaseWorkProcessedCounter("watch", metav1.StatusSuccess)
	return watcher, nil
}

func (c *ManifestWorkSourceClient) Patch(ctx context.Context, name string, pt kubetypes.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *workv1.ManifestWork, err error) {
	klog.V(4).Infof("patching manifestwork %s", name)

	if len(subresources) != 0 {
		msg := fmt.Sprintf("unsupported to update subresources %v", subresources)
		returnErr := errors.NewGenericServerResponse(http.StatusMethodNotAllowed, "patch", common.ManifestWorkGR, name, msg, 0, false)
		generic.IncreaseWorkProcessedCounter("patch", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	lastWork, exists, err := c.watcherStore.Get(c.namespace, name)
	if err != nil {
		returnErr := errors.NewInternalError(err)
		generic.IncreaseWorkProcessedCounter("patch", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}
	if !exists {
		returnErr := errors.NewNotFound(common.ManifestWorkGR, name)
		generic.IncreaseWorkProcessedCounter("patch", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	patchedWork, err := utils.Patch(pt, lastWork, data)
	if err != nil {
		returnErr := errors.NewInternalError(err)
		generic.IncreaseWorkProcessedCounter("patch", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	// TODO if we support multiple data type in future, we may need to get the data type from
	// the cloudevents data type annotation
	eventType := types.CloudEventsType{
		CloudEventsDataType: payload.ManifestBundleEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              common.UpdateRequestAction,
	}

	newWork := patchedWork.DeepCopy()
	newWork.ResourceVersion = getWorkResourceVersion(patchedWork)

	if errs := utils.ValidateWork(newWork); len(errs) != 0 {
		returnErr := errors.NewInvalid(common.ManifestWorkGK, name, errs)
		generic.IncreaseWorkProcessedCounter("patch", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	if err := c.cloudEventsClient.Publish(ctx, eventType, newWork); err != nil {
		returnErr := cloudeventserrors.ToStatusError(common.ManifestWorkGR, name, err)
		generic.IncreaseWorkProcessedCounter("patch", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	// modify the updated work in the local cache.
	if err := c.watcherStore.Update(newWork); err != nil {
		returnErr := errors.NewInternalError(err)
		generic.IncreaseWorkProcessedCounter("patch", string(returnErr.ErrStatus.Reason))
		return nil, returnErr
	}

	generic.IncreaseWorkProcessedCounter("patch", metav1.StatusSuccess)
	return newWork.DeepCopy(), nil
}

// getWorkResourceVersion returns the resource version from a work. We will get the resource
// version from the the annotation with the key "cloudevents.open-cluster-management.io/resourceversion"
// firstly, if no annotation is set, we will get the the resource version from work itself,
// if the wok does not have it, "0" will be returned, which means the version of the work
// will not be maintained on source, the message broker guarantees the work update order.
func getWorkResourceVersion(work *workv1.ManifestWork) string {
	resourceVersion, ok := work.Annotations[common.CloudEventsResourceVersionAnnotationKey]
	if ok {
		return resourceVersion
	}

	if work.ResourceVersion != "" {
		return work.ResourceVersion
	}

	return "0"
}
