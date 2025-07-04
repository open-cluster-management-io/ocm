package lease

import (
	"context"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	v1 "k8s.io/client-go/applyconfigurations/coordination/v1"
	leasev1client "k8s.io/client-go/kubernetes/typed/coordination/v1"
	"k8s.io/klog/v2"

	cloudeventserrors "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/errors"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type LeaseClient struct {
	cloudEventsClient *generic.CloudEventAgentClient[*coordinationv1.Lease]
	watcherStore      store.ClientWatcherStore[*coordinationv1.Lease]
	namespace         string
}

func (l LeaseClient) Create(ctx context.Context, lease *coordinationv1.Lease, opts metav1.CreateOptions) (*coordinationv1.Lease, error) {
	return nil, errors.NewMethodNotSupported(coordinationv1.Resource("leases"), "create")
}

func (l LeaseClient) Update(ctx context.Context, lease *coordinationv1.Lease, opts metav1.UpdateOptions) (*coordinationv1.Lease, error) {
	eventType := types.CloudEventsType{
		CloudEventsDataType: LeaseEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              types.UpdateRequestAction,
	}

	if err := l.cloudEventsClient.Publish(ctx, eventType, lease); err != nil {
		return nil, cloudeventserrors.ToStatusError(coordinationv1.Resource("leases"), lease.Name, err)
	}

	return lease, nil
}

func (l LeaseClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return errors.NewMethodNotSupported(coordinationv1.Resource("leases"), "delete")
}

func (l LeaseClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(coordinationv1.Resource("leases"), "deletecollection")
}

func (l LeaseClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*coordinationv1.Lease, error) {
	klog.V(4).Infof("getting lease %s", name)
	lease, exists, err := l.watcherStore.Get(l.namespace, name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if !exists {
		return nil, errors.NewNotFound(coordinationv1.Resource("leases"), name)
	}
	return lease, nil
}

func (l LeaseClient) List(ctx context.Context, opts metav1.ListOptions) (*coordinationv1.LeaseList, error) {
	return nil, errors.NewMethodNotSupported(coordinationv1.Resource("leases"), "list")
}

func (l LeaseClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, errors.NewMethodNotSupported(coordinationv1.Resource("leases"), "watch")
}

func (l LeaseClient) Patch(ctx context.Context, name string, pt kubetypes.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *coordinationv1.Lease, err error) {
	return nil, errors.NewMethodNotSupported(coordinationv1.Resource("leases"), "patch")
}

func (l LeaseClient) Apply(ctx context.Context, lease *v1.LeaseApplyConfiguration, opts metav1.ApplyOptions) (result *coordinationv1.Lease, err error) {
	return nil, errors.NewMethodNotSupported(coordinationv1.Resource("leases"), "apply")
}

var _ leasev1client.LeaseInterface = &LeaseClient{}

func NewLeaseClient(
	ctx context.Context, opt *options.GenericClientOptions[*coordinationv1.Lease],
	namespace string,
) (*LeaseClient, error) {
	cloudEventsClient, err := opt.AgentClient(ctx)
	if err != nil {
		return nil, err
	}

	return &LeaseClient{
		cloudEventsClient: cloudEventsClient,
		watcherStore:      opt.WatcherStore(),
		namespace:         namespace,
	}, nil
}
