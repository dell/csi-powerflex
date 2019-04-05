package provider

import (
	"github.com/rexray/gocsi"

	"github.com/dell/csi-vxflexos/service"
)

// New returns a new Mock Storage Plug-in Provider.
func New() gocsi.StoragePluginProvider {
	svc := service.New()
	return &gocsi.StoragePlugin{
		Controller:  svc,
		Identity:    svc,
		Node:        svc,
		BeforeServe: svc.BeforeServe,

		EnvVars: []string{
			// Enable request validation
			gocsi.EnvVarSpecReqValidation + "=true",

			// Enable serial volume access
			gocsi.EnvVarSerialVolAccess + "=true",

			// Treat the following fields as required:
			//    * ControllerPublishVolumeRequest.NodeId
			//    * GetNodeIDResponse.NodeId
			gocsi.EnvVarRequireNodeID + "=true",
		},
	}
}
