package service

const (
	// EnvEndpoint is the name of the environment variable used to set the
	// HTTP endpoint of the ScaleIO Gateway
	EnvEndpoint = "X_CSI_VXFLEXOS_ENDPOINT"

	// EnvUser is the name of the environment variable used to set the
	// username when authenticating to the ScaleIO Gateway
	EnvUser = "X_CSI_VXFLEXOS_USER"

	// EnvPassword is the name of the environment variable used to set the
	// user's password when authenticating to the ScaleIO Gateway
	/* #nosec G101 */
	EnvPassword = "X_CSI_VXFLEXOS_PASSWORD"

	// EnvInsecure is the name of the environment variable used to specify
	// that the ScaleIO Gateway's certificate chain and host name should not
	// be verified
	EnvInsecure = "X_CSI_VXFLEXOS_INSECURE"

	// EnvSystemName is the name of the environment variable used to set the
	// name of the ScaleIO system to interact with
	EnvSystemName = "X_CSI_VXFLEXOS_SYSTEMNAME"

	// EnvSDCGUID is the name of the environment variable used to set the
	// GUID of the SDC. This is only used by the Node Service, and removes
	// a need for calling an external binary to retrieve the GUID
	EnvSDCGUID = "X_CSI_VXFLEXOS_SDCGUID"

	// EnvThick is the name of the environment variable used to specify
	// that thick provisioning should be used when creating volumes
	EnvThick = "X_CSI_VXFLEXOS_THICKPROVISIONING"

	// EnvAutoProbe is the name of the environment variable used to specify
	// that the controller service should automatically probe itself if it
	// receives incoming requests before having been probed, in direct
	// violation of the CSI spec
	EnvAutoProbe = "X_CSI_VXFLEXOS_AUTOPROBE"
)
