package main

import (
	"os"

	"k8s.io/component-base/cli"
	_ "k8s.io/component-base/logs/json/register"          // for JSON log format registration
	_ "k8s.io/component-base/metrics/prometheus/clientgo" // load all the prometheus client-go plugins
	_ "k8s.io/component-base/metrics/prometheus/version"  // for version metric registration

	"open-cluster-management.io/ocm-controlplane/cmd/server"
)

func main() {
	cmd := server.NewAPIServerCommand()
	code := cli.Run(cmd)
	os.Exit(code)
}
