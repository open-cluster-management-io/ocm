package spoke

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
)

// RunAgent starts the controllers on agent to register to hub.
func RunAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	<-ctx.Done()
	return nil
}
