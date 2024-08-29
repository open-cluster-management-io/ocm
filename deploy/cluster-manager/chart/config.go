package chart

import (
	"embed"
)

//go:embed cluster-manager
//go:embed cluster-manager/crds
//go:embed cluster-manager/templates
//go:embed cluster-manager/templates/_helpers.tpl
var ChartFiles embed.FS

const ChartName = "cluster-manager"
