package server

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"k8s.io/apiserver/pkg/server"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/rest"
	cliflag "k8s.io/component-base/cli/flag"
	_ "k8s.io/component-base/metrics/prometheus/workqueue" // for workqueue metric registration
	"k8s.io/component-base/version/verflag"
	"k8s.io/klog/v2"

	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/ocm-controlplane/pkg/apiserver"
	"open-cluster-management.io/ocm-controlplane/pkg/apiserver/options"
)

// NewAPIServerCommand creates a *cobra.Command object with default parameters
func NewAPIServerCommand() *cobra.Command {
	s := options.NewServerRunOptions()
	cmd := &cobra.Command{
		Use: "ocm-apiserver",

		// stop printing usage when the command errors
		SilenceUsage: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			// silence client-go warnings.
			// kube-apiserver loopback clients should not log self-issued warnings.
			rest.SetDefaultWarningHandler(rest.NoWarnings{})
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			verflag.PrintAndExitIfRequested()
			fs := cmd.Flags()

			// TODO(ycyaoxdu): add DefaultHubRegistrationFeatureGates
			// add OCM feature gates
			featureGate := utilfeature.DefaultFeatureGate.DeepCopy()
			featureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates)

			// Activate logging as soon as possible, after that
			// show flags with the final logging configuration.
			if err := s.Logs.ValidateAndApply(featureGate); err != nil {
				return err
			}
			cliflag.PrintFlags(fs)

			// set default options
			completedOptions, err := apiserver.Complete(s)
			if err != nil {
				return err
			}

			if err := s.Validate(args); err != nil {
				return err
			}

			shutdownCtx, cancel := context.WithCancel(context.TODO())
			shutdownHandler := server.SetupSignalHandler()
			go func() {
				defer cancel()
				<-shutdownHandler
				klog.Infof("Received SIGTERM or SIGINT signal, shutting down controller.")
			}()

			return completedOptions.Run(shutdownCtx)
		},
		Args: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if len(arg) > 0 {
					return fmt.Errorf("%q does not take any arguments, got %q", cmd.CommandPath(), args)
				}
			}
			return nil
		},
	}

	fs := cmd.Flags()
	s.AddFlags(fs)

	return cmd
}
