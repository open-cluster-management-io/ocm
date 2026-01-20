package serviceaccount

import (
	"fmt"

	authenticationv1 "k8s.io/api/authentication/v1"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

var TokenRequestDataType = types.CloudEventsDataType{
	Group:    authenticationv1.GroupName,
	Version:  "v1",
	Resource: "tokenrequests",
}

// TokenRequestCodec is a codec to encode/decode a event/cloudevent for an agent.
type TokenRequestCodec struct{}

func NewTokenRequestCodec() *TokenRequestCodec {
	return &TokenRequestCodec{}
}

// EventDataType always returns the event data type `authentication.k8s.io.v1.tokenrequests`.
func (c *TokenRequestCodec) EventDataType() types.CloudEventsDataType {
	return TokenRequestDataType
}

// Encode the event to a cloudevent
func (c *TokenRequestCodec) Encode(source string, eventType types.CloudEventsType, tokenRequest *authenticationv1.TokenRequest) (*cloudevents.Event, error) {
	if tokenRequest == nil {
		return nil, fmt.Errorf("tokenRequest is nil")
	}

	if eventType.CloudEventsDataType != TokenRequestDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %v", eventType.CloudEventsDataType)
	}

	evt := types.NewEventBuilder(source, eventType).
		WithResourceID(string(tokenRequest.UID)).
		WithClusterName(tokenRequest.Namespace).
		NewEvent()

	if err := evt.SetData(cloudevents.ApplicationJSON, tokenRequest); err != nil {
		return nil, fmt.Errorf("failed to encode event to a cloudevent: %v", err)
	}

	return &evt, nil
}

// Decode a cloudevent to an event object
func (c *TokenRequestCodec) Decode(evt *cloudevents.Event) (*authenticationv1.TokenRequest, error) {
	tokenRequest := &authenticationv1.TokenRequest{}
	if err := evt.DataAs(tokenRequest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	return tokenRequest, nil
}
