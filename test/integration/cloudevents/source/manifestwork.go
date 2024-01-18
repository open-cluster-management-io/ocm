package source

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1lister "open-cluster-management.io/api/client/work/listers/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/watcher"
)

const ManifestsDeleted = "Deleted"

const (
	UpdateRequestAction = "update_request"
	DeleteRequestAction = "delete_request"
)

type manifestWorkSourceClient struct {
	cloudEventsClient *generic.CloudEventSourceClient[*workv1.ManifestWork]
	watcher           *watcher.ManifestWorkWatcher
	lister            workv1lister.ManifestWorkLister
	namespace         string
}

var manifestWorkGR = schema.GroupResource{Group: workv1.GroupName, Resource: "manifestworks"}

var _ workv1client.ManifestWorkInterface = &manifestWorkSourceClient{}

func newManifestWorkSourceClient(cloudEventsClient *generic.CloudEventSourceClient[*workv1.ManifestWork],
	watcher *watcher.ManifestWorkWatcher) *manifestWorkSourceClient {
	return &manifestWorkSourceClient{
		cloudEventsClient: cloudEventsClient,
		watcher:           watcher,
	}
}

func (c *manifestWorkSourceClient) SetNamespace(namespace string) *manifestWorkSourceClient {
	c.namespace = namespace
	return c
}

func (c *manifestWorkSourceClient) SetLister(lister workv1lister.ManifestWorkLister) {
	c.lister = lister
}

func (c *manifestWorkSourceClient) Create(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.CreateOptions) (*workv1.ManifestWork, error) {
	if manifestWork.Name == "" {
		manifestWork.Name = manifestWork.GenerateName + rand.String(5)
	}

	klog.Infof("create manifestwork %s/%s", c.namespace, manifestWork.Name)
	_, err := c.lister.ManifestWorks(c.namespace).Get(manifestWork.Name)
	if errors.IsNotFound(err) {
		newObj := manifestWork.DeepCopy()
		newObj.UID = kubetypes.UID(uuid.New().String())
		newObj.ResourceVersion = "1"
		newObj.Generation = 1
		newObj.Namespace = c.namespace

		//TODO support manifestbundles
		eventType := types.CloudEventsType{
			CloudEventsDataType: payload.ManifestEventDataType,
			SubResource:         types.SubResourceSpec,
			Action:              "create_request",
		}
		if err := c.cloudEventsClient.Publish(ctx, eventType, newObj); err != nil {
			return nil, err
		}

		// refresh cache
		c.watcher.Receive(watch.Event{Type: watch.Added, Object: newObj})
		return newObj, nil
	}

	if err != nil {
		return nil, err
	}

	return nil, errors.NewAlreadyExists(manifestWorkGR, manifestWork.Name)
}

func (c *manifestWorkSourceClient) Update(ctx context.Context, manifestWork *workv1.ManifestWork, opts metav1.UpdateOptions) (*workv1.ManifestWork, error) {
	klog.Infof("update manifestwork %s/%s", c.namespace, manifestWork.Name)
	lastWork, err := c.lister.ManifestWorks(c.namespace).Get(manifestWork.Name)
	if err != nil {
		return nil, err
	}

	if equality.Semantic.DeepEqual(lastWork.Spec, manifestWork.Spec) {
		return manifestWork, nil
	}

	updatedObj := manifestWork.DeepCopy()
	updatedObj.Generation = updatedObj.Generation + 1
	updatedObj.ResourceVersion = fmt.Sprintf("%d", updatedObj.Generation)

	//TODO support manifestbundles
	eventType := types.CloudEventsType{
		CloudEventsDataType: payload.ManifestEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              "update_request",
	}
	if err := c.cloudEventsClient.Publish(ctx, eventType, updatedObj); err != nil {
		return nil, err
	}

	// refresh cache
	c.watcher.Receive(watch.Event{Type: watch.Modified, Object: updatedObj})

	return updatedObj, nil
}

func (c *manifestWorkSourceClient) UpdateStatus(ctx context.Context,
	manifestWork *workv1.ManifestWork, opts metav1.UpdateOptions) (*workv1.ManifestWork, error) {
	return nil, errors.NewMethodNotSupported(manifestWorkGR, "updatestatus")
}

func (c *manifestWorkSourceClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	klog.Infof("delete manifestwork %s/%s", c.namespace, name)
	manifestWork, err := c.lister.ManifestWorks(c.namespace).Get(name)
	if err != nil {
		return err
	}

	// actual deletion should be done after hub receive delete status
	deletedObj := manifestWork.DeepCopy()
	now := metav1.Now()
	deletedObj.DeletionTimestamp = &now

	//TODO support manifestbundles
	eventType := types.CloudEventsType{
		CloudEventsDataType: payload.ManifestEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              "delete_request",
	}

	if err := c.cloudEventsClient.Publish(ctx, eventType, deletedObj); err != nil {
		return err
	}

	// refresh cache
	c.watcher.Receive(watch.Event{Type: watch.Modified, Object: deletedObj})
	return nil
}

func (c *manifestWorkSourceClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(manifestWorkGR, "deletecollection")
}

func (c *manifestWorkSourceClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*workv1.ManifestWork, error) {
	work, err := c.lister.ManifestWorks(c.namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return work.DeepCopy(), nil
}

func (c *manifestWorkSourceClient) List(ctx context.Context, opts metav1.ListOptions) (*workv1.ManifestWorkList, error) {
	return &workv1.ManifestWorkList{}, nil
}

func (c *manifestWorkSourceClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return c.watcher, nil
}

func (c *manifestWorkSourceClient) Patch(ctx context.Context, name string, pt kubetypes.PatchType, data []byte,
	opts metav1.PatchOptions, subresources ...string) (result *workv1.ManifestWork, err error) {
	return nil, errors.NewMethodNotSupported(manifestWorkGR, "patch")
}
