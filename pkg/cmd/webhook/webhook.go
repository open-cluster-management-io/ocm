package webhook

import (
	"os"

	admissionserver "github.com/openshift/generic-admission-server/pkg/cmd/server"
	"github.com/spf13/cobra"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"open-cluster-management.io/registration/pkg/features"
	clusterwebhook "open-cluster-management.io/registration/pkg/webhook/cluster"
	clustersetbindingwebhook "open-cluster-management.io/registration/pkg/webhook/clustersetbinding"
)

func NewAdmissionHook() *cobra.Command {
	o := admissionserver.NewAdmissionServerOptions(
		os.Stdout,
		os.Stderr,
		&clusterwebhook.ManagedClusterValidatingAdmissionHook{},
		&clusterwebhook.ManagedClusterMutatingAdmissionHook{},
		&clustersetbindingwebhook.ManagedClusterSetBindingValidatingAdmissionHook{})

	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "Start Managed Cluster Admission Server",
		RunE: func(c *cobra.Command, args []string) error {
			stopCh := genericapiserver.SetupSignalHandler()

			if err := o.Complete(); err != nil {
				return err
			}
			if err := o.Validate(args); err != nil {
				return err
			}
			if err := o.RunAdmissionServer(stopCh); err != nil {
				return err
			}
			return nil
		},
	}

	o.RecommendedOptions.AddFlags(cmd.Flags())

	flags := cmd.Flags()
	features.DefaultHubMutableFeatureGate.AddFlag(flags)

	return cmd
}
