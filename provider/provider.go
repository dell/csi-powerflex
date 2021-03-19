package provider

import (
	"github.com/dell/csi-vxflexos/service"
	"github.com/dell/gocsi"
)

// New returns a new Mock Storage Plug-in Provider.
func New() gocsi.StoragePluginProvider {
	svc := service.New()
	return &gocsi.StoragePlugin{
		Controller:                svc,
		Identity:                  svc,
		Node:                      svc,
		BeforeServe:               svc.BeforeServe,
		RegisterAdditionalServers: svc.RegisterAdditionalServers,

		EnvVars: []string{
			// Enable request validation
			gocsi.EnvVarSpecReqValidation + "=true",

			// Enable serial volume access
			gocsi.EnvVarSerialVolAccess + "=true",
		},
	}
}
