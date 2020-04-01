package spokecluster

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
)

// Controller manages the SpokeCluster resources
type Controller struct {
	// TODO add client and other configurations
}

// NewController creates a new spoke cluster registration controller
func NewController(recorder events.Recorder) factory.Controller {
	c := &Controller{}
	return factory.New().
		WithSync(c.sync).
		ResyncEvery(time.Minute).
		ToController("SpokeClusterController", recorder)
}

func (c Controller) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	// TODO add main reconcile loop
	return nil
}
