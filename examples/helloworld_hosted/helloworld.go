package helloworld_hosted

import (
	"embed"
)

const (
	AddonName = "helloworldhosted"
)

//go:embed manifests
//go:embed manifests/templates
var FS embed.FS
