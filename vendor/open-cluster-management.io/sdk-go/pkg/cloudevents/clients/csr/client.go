package csr

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	certificatev1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"
	cloudeventserrors "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/errors"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// CSRClient implements the CSRInterface. It sends the csr to source by
// CloudEventAgentClient.
type CSRClient struct {
	cloudEventsClient generic.CloudEventsClient[*certificatev1.CertificateSigningRequest]
	watcherStore      store.ClientWatcherStore[*certificatev1.CertificateSigningRequest]
}

var _ cache.ListerWatcher = &CSRClient{}
var _ cache.ListerWatcherWithContext = &CSRClient{}

func NewCSRClient(
	cloudEventsClient generic.CloudEventsClient[*certificatev1.CertificateSigningRequest],
	watcherStore store.ClientWatcherStore[*certificatev1.CertificateSigningRequest],
	clusterName string,
) *CSRClient {
	return &CSRClient{
		cloudEventsClient: cloudEventsClient,
		watcherStore:      watcherStore,
	}
}

func (c *CSRClient) Create(ctx context.Context, csr *certificatev1.CertificateSigningRequest, opts metav1.CreateOptions) (*certificatev1.CertificateSigningRequest, error) {
	// generate csr name if name is not set
	if csr.Name == "" && csr.GenerateName != "" {
		csr.Name = csr.GenerateName + utilrand.String(5)
		csr.GenerateName = ""
	}
	klog.V(4).Infof("creating CSR %s", csr.Name)
	_, exists, err := c.watcherStore.Get(ctx, "", csr.Name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if exists {
		return nil, errors.NewAlreadyExists(common.CSRGR, csr.Name)
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: CSREventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              types.CreateRequestAction,
	}

	// TODO: validate the csr

	if err := c.cloudEventsClient.Publish(ctx, eventType, csr); err != nil {
		return nil, cloudeventserrors.ToStatusError(common.CSRGR, csr.Name, err)
	}

	// we need to add to the store here since grpc driver may call this when it cannot
	// get from lister.
	if err := c.watcherStore.Add(csr); err != nil {
		return nil, errors.NewInternalError(err)
	}

	return csr.DeepCopy(), nil
}

func (c *CSRClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*certificatev1.CertificateSigningRequest, error) {
	klog.V(4).Infof("getting csr %s", name)
	csr, exists, err := c.watcherStore.Get(ctx, "", name)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	if !exists {
		return nil, errors.NewNotFound(common.CSRGR, name)
	}

	return csr, nil
}

func (c *CSRClient) List(opts metav1.ListOptions) (runtime.Object, error) {
	// the ListWithContext is called in informer actually
	return c.ListWithContext(context.TODO(), opts)
}

func (c *CSRClient) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	// the WatchWithContext is called in informer actually
	return c.WatchWithContext(context.TODO(), opts)
}

func (c *CSRClient) ListWithContext(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
	logger := klog.FromContext(ctx)
	logger.V(4).Info("list csr")
	csrList, err := c.watcherStore.List(ctx, "", opts)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	items := []certificatev1.CertificateSigningRequest{}
	for _, csr := range csrList.Items {
		items = append(items, *csr)
	}

	return &certificatev1.CertificateSigningRequestList{ListMeta: csrList.ListMeta, Items: items}, nil
}

func (c *CSRClient) WatchWithContext(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	logger := klog.FromContext(ctx)
	logger.V(4).Info("watch csr")
	watcher, err := c.watcherStore.GetWatcher(context.Background(), "", opts)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	return watcher, nil
}
