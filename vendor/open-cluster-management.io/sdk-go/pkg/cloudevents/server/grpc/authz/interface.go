package authz

import (
	"context"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type Authorizer interface {
	Authorize(ctx context.Context, cluster string, eventsType types.CloudEventsType) error
}
