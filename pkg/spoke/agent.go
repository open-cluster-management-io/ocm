package spoke

import (
	"context"

	"github.com/open-cluster-management/registration/pkg/spoke/bootstrap"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

// Agent holds configuration for spoke cluster agent
type Agent struct {
	bootstrapOptions *bootstrap.Options
}

// NewAgent returns an Agent
func NewAgent() *Agent {
	return &Agent{
		bootstrapOptions: bootstrap.NewOptions(),
	}
}

// RunAgent starts the controllers on agent to register to hub.
func (a *Agent) RunAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	if err := a.bootstrapOptions.Validate(); err != nil {
		klog.Fatal(err)
	}

	if err := a.bootstrapOptions.Complete(); err != nil {
		klog.Fatal(err)
	}

	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// bootstrap the agent and register the spoke cluster
	_, _, err = bootstrap.Bootstrap(spokeKubeClient.CoreV1(), a.bootstrapOptions)
	if err != nil {
		return err
	}

	<-ctx.Done()
	return nil
}

// AddFlags registers flags for Agent
func (a *Agent) AddFlags(fs *pflag.FlagSet) {
	a.bootstrapOptions.AddFlags(fs)
}

// Validate verifies the inputs.
func (a *Agent) Validate() error {
	if err := a.bootstrapOptions.Validate(); err != nil {
		return err
	}
	return nil
}

// Complete fills in missing values.
func (a *Agent) Complete() error {
	if err := a.bootstrapOptions.Complete(); err != nil {
		return err
	}
	return nil
}
