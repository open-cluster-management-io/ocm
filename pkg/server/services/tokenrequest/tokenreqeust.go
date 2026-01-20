package tokenrequest

import (
	"context"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	sace "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/serviceaccount"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"

	"open-cluster-management.io/ocm/pkg/server/services"
)

var (
	// TokenCacheTTL is the time-to-live for cached token responses
	// Tokens are cached temporarily until the agent retrieves them
	TokenCacheTTL = 30 * time.Second
)

type TokenRequestService struct {
	client  kubernetes.Interface
	codec   *sace.TokenRequestCodec
	handler server.EventHandler
	store   cache.Store
}

// NewTokenRequestService creates a new TokenRequestService with a TTL-based token cache
func NewTokenRequestService(client kubernetes.Interface) server.Service {
	return &TokenRequestService{
		client: client,
		codec:  sace.NewTokenRequestCodec(),
		store: cache.NewTTLStore(func(obj interface{}) (string, error) {
			tokenRequest, ok := obj.(*authenticationv1.TokenRequest)
			if !ok {
				return "", fmt.Errorf("object is not a TokenRequest")
			}
			return string(tokenRequest.UID), nil
		}, TokenCacheTTL),
	}
}

func (t *TokenRequestService) Get(ctx context.Context, resourceID string) (*cloudevents.Event, error) {
	// Get the token request from store by resourceID
	obj, exists, err := t.store.GetByKey(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get token request from store: %v", err)
	}
	if !exists {
		return nil, errors.NewNotFound(authenticationv1.Resource("tokenrequests"), resourceID)
	}

	tokenRequest, ok := obj.(*authenticationv1.TokenRequest)
	if !ok {
		return nil, fmt.Errorf("stored object is not a TokenRequest")
	}

	// Token will be automatically removed from cache when TTL expires
	return t.codec.Encode(services.CloudEventsSourceKube, types.CloudEventsType{CloudEventsDataType: sace.TokenRequestDataType}, tokenRequest)
}

func (t *TokenRequestService) List(listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	// resync is not needed, so list is not required
	return nil, nil
}

func (t *TokenRequestService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	logger := klog.FromContext(ctx)

	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}

	tokenRequest, err := t.codec.Decode(evt)
	if err != nil {
		return err
	}

	requestID := tokenRequest.UID

	logger.V(4).Info("handle token request", "namespace", tokenRequest.Namespace,
		"serviceAccount", tokenRequest.Name, "requestID", requestID,
		"subResource", eventType.SubResource, "actionType", eventType.Action)

	switch eventType.Action {
	case types.CreateRequestAction:
		// Create a token for the service account
		tokenResponse, err := t.client.CoreV1().ServiceAccounts(tokenRequest.Namespace).CreateToken(ctx, tokenRequest.Name, tokenRequest, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create token for service account %s/%s: %v", tokenRequest.Namespace, tokenRequest.Name, err)
		}

		// set request id back
		tokenResponse.UID = requestID

		// Cache the token response in the store for later retrieval
		if err := t.store.Add(tokenResponse); err != nil {
			return fmt.Errorf("failed to cache token response: %v", err)
		}

		// Notify the handler that the token is ready for retrieval
		if err := t.handler.OnCreate(ctx, eventType.CloudEventsDataType, string(tokenRequest.UID)); err != nil {
			return fmt.Errorf("failed to notify handler: %v", err)
		}

		return nil
	default:
		return fmt.Errorf("unsupported action %s for tokenRequest %s/%s", eventType.Action, tokenRequest.Namespace, tokenRequest.Name)
	}
}

func (t *TokenRequestService) RegisterHandler(ctx context.Context, handler server.EventHandler) {
	t.handler = handler
}
