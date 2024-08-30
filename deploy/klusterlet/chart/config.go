package chart

import (
	"embed"
)

//go:embed klusterlet
//go:embed klusterlet/crds
//go:embed klusterlet/templates
//go:embed klusterlet/templates/_helpers.tpl
var ChartFiles embed.FS

const ChartName = "klusterlet"
