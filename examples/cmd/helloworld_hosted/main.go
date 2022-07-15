package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	goflag "flag"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"
	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/examples/cmdfactory"
	helloworld "open-cluster-management.io/addon-framework/examples/helloworld"
	"open-cluster-management.io/addon-framework/examples/helloworld/agent"
	"open-cluster-management.io/addon-framework/examples/helloworld_hosted"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/version"

	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.NewOptions().AddFlags(pflag.CommandLine)

	command := newCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addon",
		Short: "helloworld example addon",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}
			os.Exit(1)
		},
	}

	if v := version.Get().String(); len(v) == 0 {
		cmd.Version = "<unknown>"
	} else {
		cmd.Version = v
	}

	cmd.AddCommand(newControllerCommand())
	cmd.AddCommand(agent.NewAgentCommand(helloworld_hosted.AddonName))

	return cmd
}

func newControllerCommand() *cobra.Command {
	cmd := cmdfactory.
		NewControllerCommandConfig("helloworld-addon-controller", version.Get(), runController).
		NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the addon controller"

	return cmd
}

func runController(ctx context.Context, kubeConfig *rest.Config) error {
	mgr, err := addonmanager.New(kubeConfig)
	if err != nil {
		return err
	}
	registrationOption := helloworld.NewRegistrationOption(
		kubeConfig,
		helloworld_hosted.AddonName,
		utilrand.String(5))

	agentAddon, err := addonfactory.NewAgentAddonFactory(
		helloworld_hosted.AddonName, helloworld_hosted.FS, "manifests/templates").
		WithGetValuesFuncs(helloworld.GetValues, addonfactory.GetValuesFromAddonAnnotation).
		WithAgentRegistrationOption(registrationOption).
		WithAgentHostedModeEnabledOption().
		BuildTemplateAgentAddon()
	if err != nil {
		klog.Errorf("failed to build agent %v", err)
		return err
	}

	err = mgr.AddAgent(agentAddon)
	if err != nil {
		klog.Fatal(err)
	}

	err = mgr.Start(ctx)
	if err != nil {
		klog.Fatal(err)
	}
	<-ctx.Done()

	return nil
}
