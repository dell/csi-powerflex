// Copyright © 2019-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//      http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

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
	EnvAutoProbe = "X_CSI_VXFLEXOS_AUTOPROBE" // #nosec G101

	// EnvAllowRWOMultiPodAccess is the name of the environment variable that specifies
	// within a single node multiple pods should be able to access the same Filesystem volume with access mode ReadWriteOnce.
	// Multi-node access is still not allowed for ReadWriteOnce Filesystem volumes.
	// Enabling this option techincally violates the CSI 1.3 spec in the NodePublishVolume stating the required error returns.
	EnvAllowRWOMultiPodAccess = "X_CSI_ALLOW_RWO_MULTI_POD_ACCESS"

	// EnvIsHealthMonitorEnabled is the name of the environment variable that specifies if
	// the driver should be reporting on volume condition. To do so, requires the alpha feature gate CSIVolumeHealth set
	// to true in the cluster. If the feature gate is on, this should be enabled. Otherwise, this should be set to false.
	EnvIsHealthMonitorEnabled = "X_CSI_HEALTH_MONITOR_ENABLED"

	// EnvIsSDCRenameEnabled is the name of the environment variable that specifies if the renaming for SDC is to be
	// carried out or not. This is only used by the Node Service.
	EnvIsSDCRenameEnabled = "X_CSI_RENAME_SDC_ENABLED" // #nosec G101

	// EnvSDCPrefix is the name of the environment variable used to set the prefix for SDC name. This is only used by
	// the Node Service.
	EnvSDCPrefix = "X_CSI_RENAME_SDC_PREFIX"

	// EnvIsApproveSDCEnabled is the name of the environment variable that specifies if the SDC approval is to be
	// carried out or not.
	EnvIsApproveSDCEnabled = "X_CSI_APPROVE_SDC_ENABLED"

	// EnvReplicationContextPrefix enables sidecars to read required information from volume context.
	EnvReplicationContextPrefix = "X_CSI_REPLICATION_CONTEXT_PREFIX"

	// EnvReplicationPrefix is used as a prefix to find out if replication is enabled.
	EnvReplicationPrefix = "X_CSI_REPLICATION_PREFIX" // #nosec G101

	// EnvMaxVolumesPerNode specifies maximum number of volumes that controller can publish to the node.
	EnvMaxVolumesPerNode = "X_CSI_MAX_VOLUMES_PER_NODE"

	// EnvQuotaEnabled enables setting of quota for NFS volumes.
	EnvQuotaEnabled = "X_CSI_QUOTA_ENABLED"

	// EnvExternalAccess is the IP of an additional router you wish to add for nfs export
	EnvExternalAccess = "X_CSI_POWERFLEX_EXTERNAL_ACCESS"

	// EnvKubeNodeName is the name of the environment variable which stores current kubernetes node name
	EnvKubeNodeName = "X_CSI_POWERFLEX_KUBE_NODE_NAME"

	// EnvMaxProbeTimeout is the name of the environment variable which stores the maximum probe timeout
	EnvMaxProbeTimeout = "X_CSI_PROBE_TIMEOUT"

	// EnvNodeChrootPath is the name of the environment variable which store path to chroot where to execute NVMe commands
	EnvNodeChrootPath = "X_CSI_POWERFLEX_NODE_CHROOT_PATH"

	// EnvPodmonEnabled indicates that podmon is enabled
	EnvPodmonEnabled = "X_CSI_PODMON_ENABLED"

	// EnvPodmonArrayConnectivityAPIPORT indicates the port to be used for exposing podmon API health
	EnvPodmonArrayConnectivityAPIPORT = "X_CSI_PODMON_API_PORT"

	// EnvPodmonArrayConnectivityPollRate indicates the polling frequency to check array connectivity
	EnvPodmonArrayConnectivityPollRate = "X_CSI_PODMON_ARRAY_CONNECTIVITY_POLL_RATE"

	// EnvAuthTyoe is the name of the environment variable which stores the authentication type such as OIDC or Standard Username Password
	EnvAuthType = "X_CSI_AUTH_TYPE"

	// EnvFsCheckEnabled is the name of the environment variable that specifies
	// if file system check should be run before mounting a volume.
	EnvFsCheckEnabled = "X_CSI_FS_CHECK_ENABLED"

	// EnvFsCheckMode is the name of the environment variable that specifies
	// the file system check mode: "checkOnly" or "checkAndRepair".
	EnvFsCheckMode = "X_CSI_FS_CHECK_MODE"

	// EnvMetricsEnabled enables the shared HTTP Prometheus metrics endpoint.
	// When true, the controller pod starts the metrics server regardless of
	// whether gateway monitoring is also enabled. Defaults to false.
	EnvMetricsEnabled = "X_CSI_METRICS_ENABLED"

	// EnvGatewayMonitoringEnabled enables gateway health monitoring.
	EnvGatewayMonitoringEnabled = "X_CSI_GATEWAY_MONITORING_ENABLED"

	// EnvGatewayMonitoringLeaderElectionEnabled enables leader election for gateway monitoring.
	// When true, gateway monitoring only runs on the controller that holds the leader election lease.
	EnvGatewayMonitoringLeaderElectionEnabled = "X_CSI_GATEWAY_MONITORING_LEADER_ELECTION_ENABLED"

	// EnvGatewayMonitoringPollInterval sets the polling interval for gateway health checks.
	EnvGatewayMonitoringPollInterval = "X_CSI_GATEWAY_MONITORING_POLL_INTERVAL"

	// EnvMetricsPort sets the HTTP port for the Prometheus metrics endpoint.
	EnvMetricsPort = "X_CSI_METRICS_PORT"

	// EnvMetricsTLSCertFile is the path to the TLS certificate file for the metrics endpoint.
	// When both EnvMetricsTLSCertFile and EnvMetricsTLSKeyFile are set, the metrics endpoint
	// is served over HTTPS instead of plain HTTP.
	EnvMetricsTLSCertFile = "X_CSI_METRICS_TLS_CERT_FILE"

	// EnvMetricsTLSKeyFile is the path to the TLS private key file for the metrics endpoint.
	// When both EnvMetricsTLSCertFile and EnvMetricsTLSKeyFile are set, the metrics endpoint
	// is served over HTTPS instead of plain HTTP.
	EnvMetricsTLSKeyFile = "X_CSI_METRICS_TLS_KEY_FILE"

	// EnvDriverNamespace is the name of the environment variable which stores the namespace where the driver is deployed
	EnvDriverNamespace = "X_CSI_DRIVER_NAMESPACE"
	// EnvSpaceReclamationEnabled enables the space reclamation feature.
	EnvSpaceReclamationEnabled = "X_CSI_SPACE_RECLAMATION_ENABLED"

	// EnvSpaceReclamationSchedule is the cron schedule for reclamation.
	EnvSpaceReclamationSchedule = "X_CSI_SPACE_RECLAMATION_SCHEDULE"

	// EnvSpaceReclamationMaxConcurrent is max concurrent reclamation jobs per node.
	EnvSpaceReclamationMaxConcurrent = "X_CSI_SPACE_RECLAMATION_MAX_CONCURRENT"

	// EnvSpaceReclamationTimeout is per-volume timeout in seconds.
	EnvSpaceReclamationTimeout = "X_CSI_SPACE_RECLAMATION_TIMEOUT"
)
