package webhook

import (
	"os"

	clusterwebhook "github.com/open-cluster-management/registration/pkg/webhook/cluster"
	clustersetbindingwebhook "github.com/open-cluster-management/registration/pkg/webhook/clustersetbinding"
	admissionserver "github.com/openshift/generic-admission-server/pkg/cmd/server"
	"github.com/spf13/cobra"
	genericapiserver "k8s.io/apiserver/pkg/server"
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

	return cmd
}
