package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	goflag "flag"

	"github.com/open-cluster-management/addon-framework/pkg/version"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"

	"github.com/open-cluster-management/addon-framework/pkg/addonmanager"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.InitLogs()
	defer logs.FlushLogs()

	command := newCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("addon-controller", version.Get(), runController).
		NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the addon controller"

	return cmd
}

func runController(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	mgr, err := addonmanager.New(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	agent := &helloWorldAgent{}
	mgr.AddAgent(agent)
	agentRegistration := &helloWorldAgentWithRegistration{}
	mgr.AddAgent(agentRegistration)
	mgr.Start(ctx)

	<-ctx.Done()

	return nil
}
