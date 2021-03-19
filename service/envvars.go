package service

const (
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

	// EnvAllowRWOMultiPodAccess is the name of the environment variable that specifes
	// within a single node multiple pods should be able to access the same Filesystem volume with access mode ReadWriteOnce.
	// Multi-node access is still not allowed for ReadWriteOnce Filesystem volumes.
	// Enabling this option techincally violates the CSI 1.3 spec in the NodePublishVolume stating the required error returns.
	EnvAllowRWOMultiPodAccess = "X_CSI_ALLOW_RWO_MULTI_POD_ACCESS"
)
