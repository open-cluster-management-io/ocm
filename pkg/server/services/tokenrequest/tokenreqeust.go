package tokenrequest

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	sace "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/serviceaccount"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"

	"open-cluster-management.io/ocm/pkg/server/services"
)

type TokenRequestService struct {
	client  kubernetes.Interface
	codec   *sace.TokenRequestCodec
	handler server.EventHandler
}

// NewTokenRequestService creates a new TokenRequestService
func NewTokenRequestService(client kubernetes.Interface) server.Service {
	return &TokenRequestService{
		client: client,
		codec:  sace.NewTokenRequestCodec(),
	}
}

func (t *TokenRequestService) List(ctx context.Context, listOpts types.ListOptions) ([]*cloudevents.Event, error) {
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
			return err
		}

		// set request id back
		tokenResponse.UID = requestID

		// Notify the handler that the token is ready for retrieval
		eventTypes := types.CloudEventsType{
			CloudEventsDataType: sace.TokenRequestDataType,
			SubResource:         types.SubResourceSpec,
			Action:              types.CreateRequestAction,
		}
		evt, err := t.codec.Encode(services.CloudEventsSourceKube, eventTypes, tokenResponse)
		if err != nil {
			return fmt.Errorf("failed to encode token response: %v", err)
		}

		if err := t.handler.HandleEvent(ctx, evt); err != nil {
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
