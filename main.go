//go:generate go generate ./core

package main

import (
	"context"
	"github.com/dell/csi-vxflexos/provider"
	"github.com/dell/csi-vxflexos/service"
	"github.com/rexray/gocsi"
)

// main is ignored when this package is built as a go plug-in
func main() {
	gocsi.Run(
		context.Background(),
		service.Name,
		"A VxFlex OS Container Storage Interface (CSI) Plugin",
		usage,
		provider.New())
}

const usage = `    X_CSI_VXFLEXOS_ENDPOINT
        Specifies the HTTP endpoint for the VXFLEXOS gateway. This parameter is
        required when running the Controller service.

        The default value is empty.

    X_CSI_VXFLEXOS_USER
        Specifies the user name when authenticating to the VXFLEXOS Gateway.

        The default value is admin.

    X_CSI_VXFLEXOS_PASSWORD
        Specifies the password of the user defined by X_CSI_VXFLEXOS_USER to use
        when authenticating to the VXFLEXOS Gateway. This parameter is required
        when running the Controller service.

        The default value is empty.

    X_CSI_VXFLEXOS_INSECURE
        Specifies that the VXFLEXOS Gateway's hostname and certificate chain
	should not be verified.

        The default value is false.

    X_CSI_VXFLEXOS_SYSTEMNAME
        Specifies the name of the VXFLEXOS system to interact with.

        The default value is default.

    X_CSI_VXFLEXOS_SDCGUID
        Specifies the GUID of the SDC. This is only used by the Node Service,
        and removes a need for calling an external binary to retrieve the GUID.
        If not set, the external binary will be invoked.

        The default value is empty.

    X_CSI_VXFLEXOS_THICKPROVISIONING
        Specifies whether thick provisiong should be used when creating volumes.

        The default value is false.

    X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE
        When a snapshot is deleted, if it is a member of a Consistency Group, enable automatic deletion
        of all snapshots in the consistency group.

        The default value is false.

    X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS
        When listing volumes, if this option is is enabled, then volumes and snapshots will be returned.
        Otherwise only volumes are returned.

        The default value is false.
`
