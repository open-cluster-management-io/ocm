package authz

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

type authorizedEventKey struct{}

func WithAuthorizedEvent(ctx context.Context, evt *cloudevents.Event) context.Context {
	return context.WithValue(ctx, authorizedEventKey{}, evt)
}

func AuthorizedEventFrom(ctx context.Context) (*cloudevents.Event, bool) {
	evt, ok := ctx.Value(authorizedEventKey{}).(*cloudevents.Event)
	return evt, ok && evt != nil
}
