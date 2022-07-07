package cmdfactory

import (
	"context"
	"math/rand"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
)

// ControllerFlags provides the "normal" controller flags
type ControllerFlags struct {
	// KubeConfigFile points to a kubeconfig file if you don't want to use the in cluster config
	KubeConfigFile string
}

// NewControllerFlags returns flags with default values set
func NewControllerFlags() *ControllerFlags {
	return &ControllerFlags{}
}

// AddFlags register and binds the default flags
func (f *ControllerFlags) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	// This command only supports reading from config

	flags.StringVar(&f.KubeConfigFile, "kubeconfig", f.KubeConfigFile, "Location of the master configuration file to run from.")
	cmd.MarkFlagFilename("kubeconfig", "kubeconfig")
}

// ControllerCommandConfig holds values required to construct a command to run.
type ControllerCommandConfig struct {
	componentName string
	startFunc     StartFunc
	version       version.Info

	basicFlags *ControllerFlags
}

// StartFunc is the function to call on leader election start
type StartFunc func(context.Context, *rest.Config) error

// NewControllerConfig returns a new ControllerCommandConfig which can be used to wire up all the boiler plate of a controller
// TODO add more methods around wiring health checks and the like
func NewControllerCommandConfig(componentName string, version version.Info, startFunc StartFunc) *ControllerCommandConfig {
	return &ControllerCommandConfig{
		startFunc:     startFunc,
		componentName: componentName,
		version:       version,

		basicFlags: NewControllerFlags(),
	}
}

func (c *ControllerCommandConfig) NewCommand() *cobra.Command {
	ctx := context.TODO()
	cmd := &cobra.Command{
		Run: func(cmd *cobra.Command, args []string) {
			// boiler plate for the "normal" command
			rand.Seed(time.Now().UTC().UnixNano())
			logs.InitLogs()

			// handle SIGTERM and SIGINT by cancelling the context.
			shutdownCtx, cancel := context.WithCancel(ctx)
			shutdownHandler := server.SetupSignalHandler()
			go func() {
				defer cancel()
				<-shutdownHandler
				klog.Infof("Received SIGTERM or SIGINT signal, shutting down controller.")
			}()

			ctx, terminate := context.WithCancel(shutdownCtx)
			defer terminate()

			if err := c.StartController(ctx); err != nil {
				klog.Fatal(err)
			}
		},
	}

	c.basicFlags.AddFlags(cmd)

	return cmd
}

// StartController runs the controller. This is the recommend entrypoint when you don't need
// to customize the builder.
func (c *ControllerCommandConfig) StartController(ctx context.Context) error {
	kubeConfig, err := clientcmd.BuildConfigFromFlags("", c.basicFlags.KubeConfigFile)
	if err != nil {
		return err
	}
	return c.startFunc(ctx, kubeConfig)
}
