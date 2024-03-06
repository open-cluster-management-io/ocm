package client

import (
	"context"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1lister "open-cluster-management.io/api/client/work/listers/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/common"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/watcher"
)

// ManifestWorkSourceClient implements the ManifestWorkInterface.
type ManifestWorkSourceClient struct {
	cloudEventsClient *generic.CloudEventSourceClient[*workv1.ManifestWork]
	watcher           *watcher.ManifestWorkWatcher
	lister            workv1lister.ManifestWorkLister
	namespace         string
	sourceID          string
}

var _ workv1client.ManifestWorkInterface = &ManifestWorkSourceClient{}

func NewManifestWorkSourceClient(sourceID string,
	cloudEventsClient *generic.CloudEventSourceClient[*workv1.ManifestWork],
	watcher *watcher.ManifestWorkWatcher) *ManifestWorkSourceClient {
	return &ManifestWorkSourceClient{
		cloudEventsClient: cloudEventsClient,
		watcher:           watcher,
		sourceID:          sourceID,
	}
}

func (c *ManifestWorkSourceClient) SetLister(lister workv1lister.ManifestWorkLister) {
	c.lister = lister
}

func (mw *ManifestWorkSourceClient) SetNamespace(namespace string) {
	mw.namespace = namespace
}

func (c *ManifestWorkSourceClient) Create(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.CreateOptions) (*workv1.ManifestWork, error) {
	_, err := c.lister.ManifestWorks(c.namespace).Get(manifestWork.Name)
	if err == nil {
		return nil, errors.NewAlreadyExists(common.ManifestWorkGR, manifestWork.Name)
	}

	if !errors.IsNotFound(err) {
		return nil, err
	}

	eventDataType, err := types.ParseCloudEventsDataType(manifestWork.Annotations[common.CloudEventsDataTypeAnnotationKey])
	if err != nil {
		return nil, err
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: *eventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              common.CreateRequestAction,
	}

	generation, err := getWorkGeneration(manifestWork)
	if err != nil {
		return nil, err
	}

	newWork := manifestWork.DeepCopy()
	newWork.UID = kubetypes.UID(utils.UID(c.sourceID, c.namespace, newWork.Name))
	newWork.Generation = generation
	ensureSourceLabel(c.sourceID, newWork)
	if err := c.cloudEventsClient.Publish(ctx, eventType, newWork); err != nil {
		return nil, err
	}

	// add the new work to the ManifestWorkInformer local cache.
	c.watcher.Receive(watch.Event{Type: watch.Added, Object: newWork})
	return newWork.DeepCopy(), nil
}

func (c *ManifestWorkSourceClient) Update(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.UpdateOptions) (*workv1.ManifestWork, error) {
	return nil, errors.NewMethodNotSupported(common.ManifestWorkGR, "update")
}

func (c *ManifestWorkSourceClient) UpdateStatus(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.UpdateOptions) (*workv1.ManifestWork, error) {
	return nil, errors.NewMethodNotSupported(common.ManifestWorkGR, "updatestatus")
}

func (c *ManifestWorkSourceClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	work, err := c.lister.ManifestWorks(c.namespace).Get(name)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	eventDataType, err := types.ParseCloudEventsDataType(work.Annotations[common.CloudEventsDataTypeAnnotationKey])
	if err != nil {
		return err
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: *eventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              common.DeleteRequestAction,
	}

	deletingWork := work.DeepCopy()
	now := metav1.Now()
	deletingWork.DeletionTimestamp = &now

	if err := c.cloudEventsClient.Publish(ctx, eventType, deletingWork); err != nil {
		return err
	}

	// update the deleting work in the ManifestWorkInformer local cache.
	c.watcher.Receive(watch.Event{Type: watch.Modified, Object: deletingWork})
	return nil
}

func (c *ManifestWorkSourceClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(common.ManifestWorkGR, "deletecollection")
}

func (c *ManifestWorkSourceClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*workv1.ManifestWork, error) {
	klog.V(4).Infof("getting manifestwork %s", name)
	return c.lister.ManifestWorks(c.namespace).Get(name)
}

func (c *ManifestWorkSourceClient) List(ctx context.Context, opts metav1.ListOptions) (*workv1.ManifestWorkList, error) {
	klog.V(4).Infof("list manifestworks")
	// send resync request to fetch manifestwork status from agents when the ManifestWorkInformer starts
	if err := c.cloudEventsClient.Resync(ctx, types.ClusterAll); err != nil {
		return nil, err
	}

	return &workv1.ManifestWorkList{}, nil
}

func (c *ManifestWorkSourceClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return c.watcher, nil
}

func (c *ManifestWorkSourceClient) Patch(ctx context.Context, name string, pt kubetypes.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *workv1.ManifestWork, err error) {
	klog.V(4).Infof("patching manifestwork %s", name)

	if len(subresources) != 0 {
		return nil, fmt.Errorf("unsupported to update subresources %v", subresources)
	}

	lastWork, err := c.lister.ManifestWorks(c.namespace).Get(name)
	if err != nil {
		return nil, err
	}

	patchedWork, err := utils.Patch(pt, lastWork, data)
	if err != nil {
		return nil, err
	}

	generation, err := getWorkGeneration(patchedWork)
	if err != nil {
		return nil, err
	}

	if generation <= lastWork.Generation {
		return nil, fmt.Errorf("the work %s/%s current generation %d is less than or equal to the last generation %d",
			c.namespace, name, generation, lastWork.Generation)
	}

	eventDataType, err := types.ParseCloudEventsDataType(lastWork.Annotations[common.CloudEventsDataTypeAnnotationKey])
	if err != nil {
		return nil, err
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: *eventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              common.UpdateRequestAction,
	}

	newWork := patchedWork.DeepCopy()
	newWork.Generation = generation
	if err := c.cloudEventsClient.Publish(ctx, eventType, newWork); err != nil {
		return nil, err
	}

	// refresh the work in the ManifestWorkInformer local cache with patched work.
	c.watcher.Receive(watch.Event{Type: watch.Modified, Object: newWork})
	return newWork.DeepCopy(), nil
}

func getWorkGeneration(work *workv1.ManifestWork) (int64, error) {
	generation, ok := work.Annotations[common.CloudEventsGenerationAnnotationKey]
	if !ok {
		return -1, fmt.Errorf("the annotation %s is not found from work %s", common.CloudEventsGenerationAnnotationKey, work.UID)
	}

	generationInt, err := strconv.Atoi(generation)
	if err != nil {
		return -1, err
	}

	return int64(generationInt), nil
}

func ensureSourceLabel(sourceID string, work *workv1.ManifestWork) {
	if work.Labels == nil {
		work.Labels = map[string]string{}
	}

	work.Labels[common.CloudEventsOriginalSourceLabelKey] = sourceID
}
