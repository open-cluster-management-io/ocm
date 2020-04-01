package hub

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/open-cluster-management/registration/pkg/hub/spokecluster"
)

// RunControllerManager starts the controllers on hub to manage spoke cluster registraiton.
func RunControllerManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	spokeClusterController := spokecluster.NewController(controllerContext.EventRecorder)

	go spokeClusterController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
