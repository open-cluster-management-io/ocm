// Copyright Contributors to the Open Cluster Management project

package helpers

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/stolostron/applier/pkg/asset"
)

func GetExampleHeader() string {
	switch os.Args[0] {
	case "oc":
		return "oc cm"
	case "kubectl":
		return "kubectl cm"
	default:
		return os.Args[0]
	}
}

func UsageTempate(cmd *cobra.Command, reader asset.ScenarioReader, valuesTemplatePath string) string {
	baseUsage := cmd.UsageTemplate()
	b, err := reader.Asset(valuesTemplatePath)
	if err != nil {
		return fmt.Sprintf("%s\n\n Values template:\n%s", baseUsage, err.Error())
	}
	return fmt.Sprintf("%s\n\n Values template:\n%s", baseUsage, string(b))
}

func DryRunMessage(dryRun bool) {
	if dryRun {
		fmt.Printf("%s is running in dry-run mode\n", GetExampleHeader())
	}
}
