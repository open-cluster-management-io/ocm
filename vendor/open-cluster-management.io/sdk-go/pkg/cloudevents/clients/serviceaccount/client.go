package serviceaccount

import (
	"context"
	"time"

	"github.com/google/uuid"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	applyconfigurationscorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/clients"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/builder"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
	cetypes "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

var (
	// TokenRequestTimeout is the timeout for CreateToken requests
	TokenRequestTimeout = 10 * time.Second
)

type ServiceAccountClient struct {
	grpcOptions *grpc.GRPCOptions
	clusterName string
}

var _ corev1client.ServiceAccountInterface = &ServiceAccountClient{}

// NewServiceAccountClient returns a ServiceAccountInterface
// This client only supports creating token via gRPC in cluster namespace on the hub.
func NewServiceAccountClient(clusterName string, opt *grpc.GRPCOptions) *ServiceAccountClient {
	return &ServiceAccountClient{
		grpcOptions: opt,
		clusterName: clusterName,
	}
}

func (sa *ServiceAccountClient) Create(ctx context.Context, serviceAccount *corev1.ServiceAccount, opts metav1.CreateOptions) (*corev1.ServiceAccount, error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource("serviceaccounts"), "create")
}

func (sa *ServiceAccountClient) Update(ctx context.Context, serviceAccount *corev1.ServiceAccount, opts metav1.UpdateOptions) (*corev1.ServiceAccount, error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource("serviceaccounts"), "update")
}

func (sa *ServiceAccountClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return errors.NewMethodNotSupported(corev1.Resource("serviceaccounts"), "delete")
}

func (sa *ServiceAccountClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(corev1.Resource("serviceaccounts"), "delete")
}

func (sa *ServiceAccountClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.ServiceAccount, error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource("serviceaccounts"), "get")
}

func (sa *ServiceAccountClient) List(ctx context.Context, opts metav1.ListOptions) (*corev1.ServiceAccountList, error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource("serviceaccounts"), "list")
}

func (sa *ServiceAccountClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource("serviceaccounts"), "watch")
}

func (sa *ServiceAccountClient) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *corev1.ServiceAccount, err error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource("serviceaccounts"), "patch")
}

func (sa *ServiceAccountClient) Apply(ctx context.Context, serviceAccount *applyconfigurationscorev1.ServiceAccountApplyConfiguration, opts metav1.ApplyOptions) (result *corev1.ServiceAccount, err error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource("serviceaccounts"), "apply")
}

func (sa *ServiceAccountClient) CreateToken(ctx context.Context, serviceAccountName string, tokenRequest *authenticationv1.TokenRequest, opts metav1.CreateOptions) (*authenticationv1.TokenRequest, error) {
	if tokenRequest == nil {
		return nil, errors.NewBadRequest("tokenRequest is nil")
	}

	tokenRequestCtx, cancel := context.WithTimeout(ctx, TokenRequestTimeout)
	defer cancel() // Ensure client resources are cleaned up

	responseChan := make(chan *authenticationv1.TokenRequest, 1)

	options, err := builder.BuildCloudEventsAgentOptions(
		sa.grpcOptions,
		sa.clusterName,
		sa.clusterName,
		TokenRequestDataType,
	)
	if err != nil {
		return nil, err
	}

	cloudEventsClient, err := clients.NewCloudEventAgentClient(
		tokenRequestCtx,
		options,
		nil, // resync is disabled, so lister is not required
		nil, // resync is disabled, so statusHashGetter is not required
		&TokenRequestCodec{},
	)
	if err != nil {
		return nil, err
	}

	requestID := types.UID(uuid.New().String())

	// subscribe before publish to avoid missing the response
	cloudEventsClient.Subscribe(tokenRequestCtx, func(handlerCtx context.Context, resp *authenticationv1.TokenRequest) error {
		if resp.UID != requestID {
			return nil
		}

		logger := klog.FromContext(handlerCtx)
		logger.V(4).Info("response token", "requestID", resp.UID, "serviceAccountName", resp.Name)
		responseChan <- resp
		return nil
	})

	// Wait for subscription to complete on the server before publishing
	select {
	case <-cloudEventsClient.SubscribedChan():
		// Subscription is ready
	case <-tokenRequestCtx.Done():
		return nil, errors.NewInternalError(tokenRequestCtx.Err())
	}

	eventType := cetypes.CloudEventsType{
		CloudEventsDataType: TokenRequestDataType,
		SubResource:         cetypes.SubResourceSpec,
		Action:              cetypes.CreateRequestAction,
	}

	newTokenRequest := tokenRequest.DeepCopy()
	newTokenRequest.UID = requestID
	newTokenRequest.Name = serviceAccountName
	newTokenRequest.Namespace = sa.clusterName // the serviceaccount should locate in cluster namespace on hub

	logger := klog.FromContext(tokenRequestCtx)
	logger.V(4).Info("request token", "requestID", requestID, "serviceAccountName", serviceAccountName)
	if err := cloudEventsClient.Publish(tokenRequestCtx, eventType, newTokenRequest); err != nil {
		return nil, errors.NewInternalError(err)
	}

	// wait until the tokenRequestResponse is received or timeout
	select {
	case tokenRequestResponse := <-responseChan:
		return tokenRequestResponse, nil
	case <-tokenRequestCtx.Done():
		return nil, errors.NewInternalError(tokenRequestCtx.Err())
	}
}
