// Copyright © 2019-2026 Dell Inc. or its subsidiaries. All Rights Reserved.
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

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ecosystems/container-storage-modules/src/csi-metadata-retriever/retriever"
	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/core"
	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/k8sutils"
	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service/collectors"
	svcmetrics "github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service/metrics"
	"github.com/Ecosystems/container-storage-modules/src/csmlog"
	"github.com/Ecosystems/container-storage-modules/src/dell-csi-extensions/podmon"
	"github.com/Ecosystems/container-storage-modules/src/dell-csi-extensions/replication"
	"github.com/Ecosystems/container-storage-modules/src/gobrick"
	"github.com/Ecosystems/container-storage-modules/src/gocsi"
	csictx "github.com/Ecosystems/container-storage-modules/src/gocsi/context"
	"github.com/Ecosystems/container-storage-modules/src/gonvme"
	"github.com/Ecosystems/container-storage-modules/src/goscaleio"
	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	"github.com/apparentlymart/go-cidr/cidr"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/fsnotify/fsnotify"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Name is the name of the CSI plug-in.
	Name = "csi-vxflexos.dellemc.com"

	// VerboseName is a longer description of the driver, used in the Application-Type HTTP header.
	VerboseName = "CSI Driver for Dell EMC PowerFlex"

	// KeyThickProvisioning is the key used to get a flag indicating that
	// a volume should be thick provisioned from the volume create params
	KeyThickProvisioning = "thickprovisioning"

	thinProvisioned  = "ThinProvisioned"
	thickProvisioned = "ThickProvisioned"
	defaultPrivDir   = "/dev/disk/csi-vxflexos"

	// SystemTopologySystemValue is the supported topology key
	SystemTopologySystemValue string = "csi-vxflexos.dellemc.com"

	// DefaultLogLevel is the default log level for the driver.
	DefaultLogLevel = csmlog.InfoLevel

	// ParamCSILogLevel is the CSI driver csmlog level.
	ParamCSILogLevel = "CSI_LOG_LEVEL"
	// DriverConfigMap is the name of the driver config map.
	DriverConfigMap = "vxflexos-config-params"
	// ConfigMapFilePath is the file path to the driver config params.
	ConfigMapFilePath = "/vxflexos-config-params/driver-config-params.yaml"

	// defaultNodeChrootPath for nvme commands
	defaultNodeChrootPath = "/noderoot"

	// NVMeTCP is the NVMe over TCP protocol.
	NVMeTCP = "NVMeTCP"
	// SDC is the ScaleIO Data Client protocol.
	SDC = "SDC"

	// Timeout for making http requests
	Timeout = time.Second * 5

	// DefaultPodmonPollRate for podmon
	DefaultPodmonPollRate = 60
)

var (
	mx = sync.Mutex{}
	px = sync.Mutex{}
)

// DriverNamespace is the namespace where the driver is deployed, defaults to "vxflexos" for backward compatibility
var DriverNamespace = "vxflexos"

// LookupEnv - Fetches the environment var value
var LookupEnv = lookupEnv

// ArrayConfigFile is file name with array connection data
var ArrayConfigFile string

// DriverConfigParamsFile is the name of the input driver config params file
var DriverConfigParamsFile string

// KubeConfig is the kube config
var KubeConfig string

// K8sClientset is the client to query k8s
var K8sClientset kubernetes.Interface

// globalOperationInterceptor is the shared CSI operation interceptor used across the driver.
var globalOperationInterceptor grpc.UnaryServerInterceptor

// PodmonAPIToken is the shared secret token for authenticating podmon API requests.
// This variable is package-scoped; each driver binary maintains its own instance.
var PodmonAPIToken string

// Log controlls the logger
// give default value, will be overwritten by configmap

// ArrayConnectionData contains data required to connect to array
type ArrayConnectionData struct {
	SystemID                  string             `json:"systemID"`
	Username                  string             `json:"username"`
	Password                  string             `json:"password"`
	Endpoint                  string             `json:"endpoint"`
	SkipCertificateValidation bool               `json:"skipCertificateValidation,omitempty"`
	Insecure                  bool               `json:"insecure,omitempty"`
	IsDefault                 bool               `json:"isDefault,omitempty"`
	AllSystemNames            string             `json:"allSystemNames"`
	NasName                   string             `json:"nasName"`
	Mdm                       string             `json:"mdm,omitempty"`
	AvailabilityZone          *AvailabilityZone  `json:"zone,omitempty"`
	Zones                     []AvailabilityZone `json:"zones,omitempty"`
	BlockProtocol             string             `json:"blockProtocol,omitempty"`
	AuthType                  string             `json:"authType,omitempty"`
	CiamClientID              string             `json:"ciamClientId,omitempty"`
	CiamClientSecret          string             `json:"ciamClientSecret,omitempty"`
	OidcClientID              string             `json:"oidcClientId,omitempty"`
	OidcClientSecret          string             `json:"oidcClientSecret,omitempty"`
	Issuer                    string             `json:"issuer,omitempty"`
	Scopes                    string             `json:"scopes,omitempty"`
}

// ZoneName is the name of an availability zone.
type ZoneName string

// ZoneTargetMap maps a zone name to a list of protection domains.
type ZoneTargetMap map[ZoneName][]ProtectionDomain

// ProtectionDomainName is the name of a protection domain.
type ProtectionDomainName string

// PoolName is the name of a storage pool.
type PoolName string

// AvailabilityZone provides a mapping between cluster zones labels and storage systems
type AvailabilityZone struct {
	Name              ZoneName           `json:"name"`
	LabelKey          string             `json:"labelKey"`
	ProtectionDomains []ProtectionDomain `json:"protectionDomains"`
}

// ArrayConnectivityStatus Status of the array probe
type ArrayConnectivityStatus struct {
	LastSuccess int64 `json:"lastSuccess"` // connectivity status
	LastAttempt int64 `json:"lastAttempt"` // last timestamp attempted to check connectivity
}

// ProtectionDomain provides protection domain information for a cluster's availability zone
type ProtectionDomain struct {
	Name  ProtectionDomainName `json:"name"`
	Pools []PoolName           `json:"pools"`
}

// ManifestSemver is the manifest version of the driver.
var ManifestSemver string

// Manifest is the SP's manifest.
var Manifest = map[string]string{
	"semver": ManifestSemver,
	"formed": core.CommitTime.Format(time.RFC1123),
}

// Service is the CSI Mock service provider.
type Service interface {
	csi.ControllerServer
	// NOTE: csi.GroupControllerServer is implemented by a separate groupControllerService type in groupcontroller.go.
	csi.IdentityServer
	csi.NodeServer
	BeforeServe(context.Context, *gocsi.StoragePlugin, net.Listener) error
	RegisterAdditionalServers(server *grpc.Server)
	ProcessMapSecretChange() error
}

// NetworkInterface is an abstraction for network interface operations.
type NetworkInterface interface {
	InterfaceByName(name string) (*net.Interface, error)
	Addrs(interfaceObj *net.Interface) ([]net.Addr, error)
}

// Opts defines service configuration options.
type Opts struct {
	// map from system name to ArrayConnectionData
	arrays                                 map[string]*ArrayConnectionData
	defaultSystemID                        string // ID of default system
	SdcGUID                                string
	Thick                                  bool
	AutoProbe                              bool
	DisableCerts                           bool   // used for unit testing only
	Lsmod                                  string // used for unit testing only
	drvCfgQueryMDM                         string // used for testing only
	EnableSnapshotCGDelete                 bool   // when snapshot deleted, enable deleting of all snaps in the CG of the snapshot
	EnableListVolumesSnapshots             bool   // when listing volumes, include snapshots and volumes
	AllowRWOMultiPodAccess                 bool   // allow multiple pods to access a RWO volume on the same node
	IsHealthMonitorEnabled                 bool   // allow driver to make use of the alpha feature gate, CSIVolumeHealth
	IsSdcRenameEnabled                     bool   // allow driver to enable renaming SDC
	SdcPrefix                              string // prefix to be set for SDC name
	TrimSDCNameEnabled                     bool   // truncate SDC name to 31 chars (PowerFlex limit)
	IsApproveSDCEnabled                    bool
	replicationContextPrefix               string
	replicationPrefix                      string
	MaxVolumesPerNode                      int64
	IsQuotaEnabled                         bool   // allow driver to enable quota limits for NFS volumes
	ExternalAccess                         string // used for adding extra IP/IP range to the NFS export
	KubeNodeName                           string
	zoneLabelKey                           string
	probeTimeout                           time.Duration
	NodeChrootPath                         string
	IsPodmonEnabled                        bool   // used to indicate that podmon is enabled
	PodmonPort                             string // to indicates the port to be used for exposing podmon API health
	PodmonPollingFreq                      string // indicates the polling frequency to check array connectivity
	AuthType                               string // indicate what auth type to use
	FsCheckEnabled                         bool   // enable file system check before mount
	FsCheckMode                            string // "checkOnly" (default) or "checkAndRepair"
	MetricsEnabled                         bool   // independently enables the shared metrics HTTP server
	GatewayMonitoringEnabled               bool
	GatewayMonitoringLeaderElectionEnabled bool
	GatewayMonitoringInterval              time.Duration
	MetricsPort                            string
	MetricsTLSCertFile                     string // path to TLS cert for the metrics endpoint
	MetricsTLSKeyFile                      string // path to TLS key for the metrics endpoint
}

// PlatformInfo contains platform information for a PowerFlex system.
type PlatformInfo struct {
	SystemID     string
	ArrayVersion float64
	GenType      string
}

type service struct {
	// satisfies the Service interface and provides unimplemented defaults to functions not implemented
	csi.UnimplementedControllerServer
	// NOTE: GroupControllerServer is served by a separate groupControllerService type (see groupcontroller.go).
	csi.UnimplementedIdentityServer
	csi.UnimplementedNodeServer
	replication.UnimplementedReplicationServer

	opts                Opts
	adminClients        map[string]*sio.Client
	systems             map[string]*sio.System
	platformInfos       map[string]*PlatformInfo
	mode                string
	volCache            []*siotypes.Volume
	volCacheRWL         sync.RWMutex
	volCacheSystemID    string // systemID for cached volumes
	snapCache           []*siotypes.Volume
	snapCacheRWL        sync.RWMutex
	snapCacheSystemID   string // systemID for cached snapshots
	privDir             string
	storagePoolIDToName map[string]string
	statisticsCounter   int
	// maps the first 24 bits of a volume ID to the volume's systemID
	volumePrefixToSystems   map[string][]string
	connectedSystemNameToID map[string]string
	nvmeTargetNqn           map[string]string
	nvmeTargetNqnMutex      sync.RWMutex
	nvmeConnector           NVMEConnector
	nvmeLib                 gonvme.NVMEinterface
	useNVME                 bool
	useSDC                  bool
	nodeID                  string
	probeStatus             *sync.Map
	probeLocks              sync.Map // map[string]*sync.Mutex
	metricsServer           *svcmetrics.SharedMetricsServer
	gatewayMonitor          *svcmetrics.GatewayMonitor
	spaceReclaimMgr         *SpaceReclamationManager
	roundingEmitter         *RoundingEventEmitter
	granularityMetrics      *svcmetrics.GranularityMetrics
	collectorManager        *collectors.CollectorManager
	metricsStale            *prometheus.GaugeVec
	operationInterceptor    grpc.UnaryServerInterceptor
}

// Config contains driver configuration parameters.
type Config struct {
	InterfaceNames map[string]string `yaml:"interfaceNames"`
}

// GetIPAddressByInterfacefunc is a function type for getting IP address by interface name.
type GetIPAddressByInterfacefunc func(string, NetworkInterface) (string, error)

func (s *service) InterfaceByName(name string) (*net.Interface, error) {
	return net.InterfaceByName(name)
}

func (s *service) Addrs(interfaceObj *net.Interface) ([]net.Addr, error) {
	return interfaceObj.Addrs()
}

// Process dynamic changes to configMap or Secret.
func (s *service) ProcessMapSecretChange() error {
	// Update dynamic config params
	vc := viper.New()
	vc.AutomaticEnv()
	csmlog.WithFields(csmlog.Fields{"file": DriverConfigParamsFile}).Info("driver configuration file")
	vc.SetConfigFile(DriverConfigParamsFile)
	if err := vc.ReadInConfig(); err != nil {
		csmlog.Errorf("unable to read config file, using default values: %v", err)
	}
	if err := s.updateDriverConfigParams(vc); err != nil {
		return err
	}
	vc.WatchConfig()
	vc.OnConfigChange(func(_ fsnotify.Event) {
		// Putting in mutex to allow tests to pass with race flag
		mx.Lock()
		defer mx.Unlock()
		csmlog.WithFields(csmlog.Fields{"file": DriverConfigParamsFile}).Info("driver configuration file")
		if err := s.updateDriverConfigParams(vc); err != nil {
			csmlog.Warn(err.Error())
		}
	})

	// dynamic array secret change
	va := viper.New()
	va.SetConfigFile(ArrayConfigFile)

	csmlog.WithFields(csmlog.Fields{"file": ArrayConfigFile}).Info("driver configuration file")

	va.WatchConfig()

	va.OnConfigChange(func(_ fsnotify.Event) {
		// Putting in mutex to allow tests to pass with race flag and to protect
		// s.opts.arrays / adminClients while we reload and resolve default pools.
		mx.Lock()
		defer mx.Unlock()
		csmlog.WithFields(csmlog.Fields{"file": ArrayConfigFile}).Info("driver configuration file")
		var err error
		s.opts.arrays, err = getArrayConfig(context.Background())
		if err != nil {
			csmlog.Errorf("unable to reload multi array config file: %v", err)
			return
		}
		err = s.doProbe(context.Background())
		if err != nil {
			csmlog.Errorf("unable to probe array in multi array config: %v", err)
			return
		}
		// After probing, resolve empty pools for any zone entries that omit pools.
		// Admin clients are now connected, so we can query the PowerFlex API.
		// This is done under the reload lock to avoid races with RPC handlers.
		for _, arr := range s.opts.arrays {
			if len(arr.Zones) > 0 && s.adminClients[arr.SystemID] != nil {
				system := s.systems[arr.SystemID]
				if system != nil {
					lookup := &goscaleioPoolLookup{system: system}
					if resolveErr := resolveDefaultPools(arr.SystemID, arr.Zones, lookup); resolveErr != nil {
						csmlog.Errorf("unable to resolve default pools for system %s: %v", arr.SystemID, resolveErr)
					} else {
						csmlog.Infof("resolved default pools for system %s zones", arr.SystemID)
					}
				} else {
					csmlog.Warnf("system %s has zones[] configured but no system client after probe; default pool resolution skipped", arr.SystemID)
				}
			}
		}
		// log csiNode topology keys
		if err = s.logCsiNodeTopologyKeys(); err != nil {
			csmlog.Errorf("unable to log csiNode topology keys: %v", err)
		}
	})
	return nil
}

func (s *service) logCsiNodeTopologyKeys() error {
	if K8sClientset == nil {
		err := k8sutils.CreateKubeClientSet(KubeConfig)
		if err != nil {
			csmlog.Errorf("unable to create k8s clientset for query: %v", err)
			return err
		}
		K8sClientset = k8sutils.Clientset
	}

	csiNodes, err := K8sClientset.StorageV1().CSINodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		csmlog.Errorf("unable to get node list: %v", err)
		return err
	}
	node, err := s.NodeGetInfo(context.Background(), nil)
	if node != nil {
		csmlog.WithFields(csmlog.Fields{"node info": node.NodeId}).Info("NodeInfo ID")
		segMap := node.AccessibleTopology.Segments

		for key := range segMap {
			csmlog.WithFields(csmlog.Fields{"node info key": key}).Info("NodeInfo topologykeys")
		}

		if err == nil {
			for _, csiNode := range csiNodes.Items {
				for _, driver := range csiNode.Spec.Drivers {
					if driver.NodeID == node.NodeId && driver.Name == Name {
						csmlog.WithFields(csmlog.Fields{"csinode": csiNode.Name}).Info("csiNode name")
						csmlog.WithFields(csmlog.Fields{"csinode ID": driver.NodeID}).Info("csiNode id")
						tkeys := driver.TopologyKeys
						if tkeys != nil {
							csmlog.WithFields(csmlog.Fields{"csinode topologykeys": len(tkeys)}).Info("count")
							needMap := make(map[string]string)
							for key := range segMap {
								for _, tkey := range tkeys {
									if tkey != key {
										needMap[key] = "missing"
									} else {
										csmlog.WithFields(csmlog.Fields{"csinode topologykeys": "ok"}).Info("found")
									}
								}
							}
							for akey := range needMap {
								csmlog.WithFields(csmlog.Fields{"csinode missing topology key": akey}).Info("node key")
							}
						}
					}
				}
			}
		} else {
			csmlog.Errorf("unable to list csiNodes in cluster: %v", err)
		}
	}
	return nil
}

// New returns a handle to service
func New() Service {
	return &service{
		storagePoolIDToName:     map[string]string{},
		connectedSystemNameToID: map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
	}
}

func (s *service) updateDriverConfigParams(v *viper.Viper) error {
	logFormat := v.GetString("CSI_LOG_FORMAT")
	logFormat = strings.ToLower(logFormat)
	csmlog.WithFields(csmlog.Fields{"format": logFormat}).Info("Read CSI_LOG_FORMAT from log configuration file")
	if strings.EqualFold(logFormat, "text") {
		csmlog.SetFormat("text")
	} else {
		// use json formatter by default
		if logFormat != "json" {
			csmlog.WithFields(csmlog.Fields{"format": logFormat}).Info("CSI_LOG_FORMAT value not recognized, setting to json")
		}
		csmlog.SetFormat("json")
	}

	level := DefaultLogLevel
	if v.IsSet(ParamCSILogLevel) {
		logLevel := v.GetString(ParamCSILogLevel)
		if logLevel != "" {
			logLevel = strings.ToLower(logLevel)
			csmlog.WithFields(csmlog.Fields{"level": logLevel}).Info("Read CSI_LOG_LEVEL from log configuration file")
			var err error
			level, err = csmlog.ParseLevel(logLevel)
			if err != nil {
				csmlog.Errorf("invalid CSI_LOG_LEVEL %s, defaulting to %s: %v", logLevel, DefaultLogLevel, err)
				csmlog.SetLevel(DefaultLogLevel)
				return fmt.Errorf("input log level %q is not valid", logLevel)
			}
		}
	}
	csmlog.SetLevel(level)
	// set X_CSI_LOG_LEVEL so that gocsi doesn't overwrite the loglevel set by us
	_ = os.Setenv(gocsi.EnvVarLogLevel, level.String())
	return nil
}

func (s *service) BeforeServe(
	//nolint:revive
	ctx context.Context, sp *gocsi.StoragePlugin, lis net.Listener,
) error {
	defer func() {
		fields := map[string]interface{}{
			"sdcGUID":                s.opts.SdcGUID,
			"thickprovision":         s.opts.Thick,
			"privatedir":             s.privDir,
			"autoprobe":              s.opts.AutoProbe,
			"mode":                   s.mode,
			"allowRWOMultiPodAccess": s.opts.AllowRWOMultiPodAccess,
			"IsHealthMonitorEnabled": s.opts.IsHealthMonitorEnabled,
			"IsSdcRenameEnabled":     s.opts.IsSdcRenameEnabled,
			"sdcPrefix":              s.opts.SdcPrefix,
			"IsApproveSDCEnabled":    s.opts.IsApproveSDCEnabled,
			"MaxVolumesPerNode":      s.opts.MaxVolumesPerNode,
			"IsQuotaEnabled":         s.opts.IsQuotaEnabled,
			"ExternalAccess":         s.opts.ExternalAccess,
			"KubeNodeName":           s.opts.KubeNodeName,
			"isPodmonEnabled":        s.opts.IsPodmonEnabled,
			"PodmonPort":             s.opts.PodmonPort,
			"PodmonFrequency":        s.opts.PodmonPollingFreq,
			"FsCheckEnabled":         s.opts.FsCheckEnabled,
			"FsCheckMode":            s.opts.FsCheckMode,
		}

		csmlog.WithFields(fields).Infof("configured %s", Name)
	}()

	// Get the SP's operating mode.
	s.mode = csictx.Getenv(ctx, gocsi.EnvVarMode)

	opts := Opts{}

	if ns, ok := csictx.LookupEnv(ctx, EnvDriverNamespace); ok {
		DriverNamespace = ns
	}

	var err error

	// Process configuration file and initialize system clients
	opts.arrays, err = getArrayConfig(ctx)
	if err != nil {
		csmlog.Warnf("unable to get arrays from config: %s", err.Error())
		return err
	}

	// if custom zoning is being used, find the common label from the array secret
	opts.zoneLabelKey, err = getZoneKeyLabelFromSecret(opts.arrays)
	if err != nil {
		csmlog.Warnf("unable to get zone key from secret: %s", err.Error())
		return err
	}

	if err = s.ProcessMapSecretChange(); err != nil {
		csmlog.Warnf("unable to configure dynamic configMap secret change detection : %s", err.Error())
		return err
	}

	if guid, ok := csictx.LookupEnv(ctx, EnvSDCGUID); ok {
		opts.SdcGUID = guid
	}
	if pd, ok := csictx.LookupEnv(ctx, "X_CSI_PRIVATE_MOUNT_DIR"); ok {
		s.privDir = pd
	}
	if snapshotCGDelete, ok := csictx.LookupEnv(ctx, "X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE"); ok {
		if snapshotCGDelete == "true" {
			opts.EnableSnapshotCGDelete = true
		}
	}
	if listVolumesSnapshots, ok := csictx.LookupEnv(ctx, "X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS"); ok {
		if listVolumesSnapshots == "true" {
			opts.EnableListVolumesSnapshots = true
		}
	}
	if allowRWOMultiPodAccess, ok := csictx.LookupEnv(ctx, EnvAllowRWOMultiPodAccess); ok {
		if allowRWOMultiPodAccess == "true" {
			opts.AllowRWOMultiPodAccess = true
			mountAllowRWOMultiPodAccess = true
		}
	}
	if healthMonitor, ok := csictx.LookupEnv(ctx, EnvIsHealthMonitorEnabled); ok {
		if healthMonitor == "true" {
			opts.IsHealthMonitorEnabled = true
		}
	}
	if renameSDC, ok := csictx.LookupEnv(ctx, EnvIsSDCRenameEnabled); ok {
		if renameSDC == "true" {
			opts.IsSdcRenameEnabled = true
		}
	}
	if sdcPrefix, ok := csictx.LookupEnv(ctx, EnvSDCPrefix); ok {
		opts.SdcPrefix = sdcPrefix
	}
	if trimSDCName, ok := csictx.LookupEnv(ctx, EnvTrimSDCNameEnabled); ok {
		if trimSDCName == "true" {
			opts.TrimSDCNameEnabled = true
		}
	}
	if approveSDC, ok := csictx.LookupEnv(ctx, EnvIsApproveSDCEnabled); ok {
		if approveSDC == "true" {
			opts.IsApproveSDCEnabled = true
		}
	}

	if nodeChrootPath, ok := csictx.LookupEnv(ctx, EnvNodeChrootPath); ok {
		opts.NodeChrootPath = nodeChrootPath
	}

	if opts.NodeChrootPath == "" {
		opts.NodeChrootPath = defaultNodeChrootPath
	}

	if quotaEnabled, ok := csictx.LookupEnv(ctx, EnvQuotaEnabled); ok {
		if quotaEnabled == "true" {
			opts.IsQuotaEnabled = true
		}
	}

	if s.privDir == "" {
		s.privDir = defaultPrivDir
	}

	if replicationContextPrefix, ok := csictx.LookupEnv(ctx, EnvReplicationContextPrefix); ok {
		opts.replicationContextPrefix = replicationContextPrefix + "/"
	}

	if replicationPrefix, ok := csictx.LookupEnv(ctx, EnvReplicationPrefix); ok {
		opts.replicationPrefix = replicationPrefix
	}
	if MaxVolumesPerNode, err := ParseInt64FromContext(ctx, EnvMaxVolumesPerNode); err != nil {
		csmlog.Warnf("error while parsing env variable '%s', %s, defaulting to 0", EnvMaxVolumesPerNode, err)
		opts.MaxVolumesPerNode = 0
	} else {
		opts.MaxVolumesPerNode = MaxVolumesPerNode
	}
	if externalAccess, ok := csictx.LookupEnv(ctx, EnvExternalAccess); ok {
		// Trimming spaces if any
		externalAccess = strings.TrimSpace(externalAccess)
		if externalAccess == "" {
			csmlog.Infof("externalAccess is not provided")
			opts.ExternalAccess = ""
		} else {
			opts.ExternalAccess, err = ParseCIDR(externalAccess)
			if err != nil {
				csmlog.Warnf("error while parsing the externalAccess : %s, defaulting to empty", err)
				opts.ExternalAccess = ""
			}
		}
	}
	if kubeNodeName, ok := csictx.LookupEnv(ctx, EnvKubeNodeName); ok {
		opts.KubeNodeName = kubeNodeName
	}

	if isPodmonEnabled, ok := csictx.LookupEnv(ctx, EnvPodmonEnabled); ok {
		opts.IsPodmonEnabled = strings.EqualFold(isPodmonEnabled, "true")
	}

	if podmonPort, ok := csictx.LookupEnv(ctx, EnvPodmonArrayConnectivityAPIPORT); ok {
		opts.PodmonPort = fmt.Sprintf(":%s", podmonPort)
	}

	if podmonPollRate, ok := csictx.LookupEnv(ctx, EnvPodmonArrayConnectivityPollRate); ok {
		opts.PodmonPollingFreq = podmonPollRate
	}

	// Load podmon API token for authenticating requests to node podmon API endpoints
	if podmonAPIToken, ok := csictx.LookupEnv(ctx, EnvPodmonAPIToken); ok && strings.TrimSpace(podmonAPIToken) != "" {
		PodmonAPIToken = strings.TrimSpace(podmonAPIToken)
	} else if opts.IsPodmonEnabled {
		csmlog.Warnf("%s is not set; podmon API endpoints will not require authentication", EnvPodmonAPIToken)
	}

	// log csiNode topology keys
	if err = s.logCsiNodeTopologyKeys(); err != nil {
		csmlog.Errorf("unable to log csiNode topology keys: %v", err)
	}

	if EnvAuthType, ok := csictx.LookupEnv(ctx, EnvAuthType); ok {
		opts.AuthType = EnvAuthType
	}

	// FSCheck configuration
	if fsCheckEnabled, ok := csictx.LookupEnv(ctx, EnvFsCheckEnabled); ok {
		if strings.EqualFold(fsCheckEnabled, "true") {
			opts.FsCheckEnabled = true
		} else if fsCheckEnabled != "" && !strings.EqualFold(fsCheckEnabled, "false") {
			csmlog.Warnf("invalid value %q for %s, defaulting to false", fsCheckEnabled, EnvFsCheckEnabled)
		}
	}

	opts.FsCheckMode = "checkOnly" // default
	if fsCheckMode, ok := csictx.LookupEnv(ctx, EnvFsCheckMode); ok {
		switch strings.ToLower(fsCheckMode) {
		case "checkonly":
			opts.FsCheckMode = "checkOnly"
		case "checkandrepair":
			opts.FsCheckMode = "checkAndRepair"
		default:
			csmlog.Warnf("invalid value %q for %s, defaulting to \"checkOnly\"", fsCheckMode, EnvFsCheckMode)
		}
	}

	csmlog.Infof("FsCheckEnabled: %s", strconv.FormatBool(opts.FsCheckEnabled))
	csmlog.Infof("FsCheckMode: %s", opts.FsCheckMode)

	// Setting package-level FSCK variables
	mountFsCheckEnabled = opts.FsCheckEnabled
	mountFsCheckMode = opts.FsCheckMode

	// Initializing the MetadataRetriever Client
	if s.mode == "node" {
		// Ensure k8s client is initialized for FS check event recording
		if k8sutils.Clientset == nil {
			csmlog.Infof("Initializing k8s client for FS check events...")
			if err := k8sutils.CreateKubeClientSet(); err != nil {
				csmlog.Errorf("Failed to initialize k8s client for FS check events: %v - PVC events will not be posted", err)
			}
		}
		metadataRetrieverClient = retriever.NewMetadataRetrieverClient(nil, 30*time.Second)
		csmlog.Infof("Successfully initialized the Metadata Retriever Client: %s", metadataRetrieverClient)

		mountFsCheckEventRecorder = initFsCheckEventRecorder()
		csmlog.Infof("Successfully initialized the FS check event recorder: %s", mountFsCheckEventRecorder)
	}

	opts.probeTimeout = DefaultAPITimeout
	if envProbeTimeout, ok := csictx.LookupEnv(ctx, EnvMaxProbeTimeout); ok {
		duration, err := time.ParseDuration(envProbeTimeout)
		if err != nil {
			csmlog.Warnf("error while parsing env variable '%s', %s, defaulting to %s", EnvMaxProbeTimeout, err, DefaultAPITimeout)
			opts.probeTimeout = DefaultAPITimeout
		} else {
			csmlog.Infof("env variable '%s' provided with value %s", EnvMaxProbeTimeout, envProbeTimeout)
			opts.probeTimeout = duration
		}
	} else {
		csmlog.Infof("env variable '%s' not provided, defaulting to %s", EnvMaxProbeTimeout, DefaultAPITimeout)
	}

	// pb parses an environment variable into a boolean value. If an error
	// is encountered, default is set to false, and error is logged
	pb := func(n string) bool {
		if v, ok := csictx.LookupEnv(ctx, n); ok {
			b, err := strconv.ParseBool(v)
			if err != nil {
				csmlog.WithFields(csmlog.Fields{n: v}).Debug("invalid boolean value. defaulting to false")
				return false
			}
			return b
		}
		return false
	}

	opts.Thick = pb(EnvThick)
	opts.AutoProbe = true

	// Metrics service configuration
	if metricsEnabled, ok := csictx.LookupEnv(ctx, EnvMetricsEnabled); ok {
		opts.MetricsEnabled = strings.EqualFold(metricsEnabled, "true")
	}

	// Gateway monitoring configuration
	if gatewayMonEnabled, ok := csictx.LookupEnv(ctx, EnvGatewayMonitoringEnabled); ok {
		opts.GatewayMonitoringEnabled = strings.EqualFold(gatewayMonEnabled, "true")
	}
	if gatewayMonLEEnabled, ok := csictx.LookupEnv(ctx, EnvGatewayMonitoringLeaderElectionEnabled); ok {
		opts.GatewayMonitoringLeaderElectionEnabled = strings.EqualFold(gatewayMonLEEnabled, "true")
	}
	if gatewayMonInterval, ok := csictx.LookupEnv(ctx, EnvGatewayMonitoringPollInterval); ok {
		if d, parseErr := time.ParseDuration(gatewayMonInterval); parseErr == nil {
			opts.GatewayMonitoringInterval = d
		} else {
			csmlog.Warnf("invalid value %q for %s, defaulting to 30s", gatewayMonInterval, EnvGatewayMonitoringPollInterval)
			opts.GatewayMonitoringInterval = 30 * time.Second
		}
	}
	if metricsPort, ok := csictx.LookupEnv(ctx, EnvMetricsPort); ok {
		opts.MetricsPort = svcmetrics.FormatMetricsAddr(metricsPort)
	} else {
		opts.MetricsPort = svcmetrics.DefaultMetricsPort
	}
	if metricsTLSCert, ok := csictx.LookupEnv(ctx, EnvMetricsTLSCertFile); ok {
		opts.MetricsTLSCertFile = metricsTLSCert
	}
	if metricsTLSKey, ok := csictx.LookupEnv(ctx, EnvMetricsTLSKeyFile); ok {
		opts.MetricsTLSKeyFile = metricsTLSKey
	}

	s.opts = opts
	s.adminClients = make(map[string]*sio.Client)
	s.systems = make(map[string]*sio.System)
	s.platformInfos = make(map[string]*PlatformInfo)
	s.nvmeTargetNqn = make(map[string]string)

	// Initialize NVMe connectors
	s.initConnectors()

	// Setup NVMe host for array version >= 4.0
	nvmeInitiators, err := s.getInitiators()
	csmlog.Infof("nvmeInitiators: %s", nvmeInitiators)
	if err != nil {
		csmlog.Errorf("can not get initiators of the node: %s", err.Error())
	}

	for _, arr := range s.opts.arrays {
		csmlog.Infof("checking array version for array: %s", arr.SystemID)
		version, err := s.getArrayVersion(ctx, arr.SystemID)
		if err != nil {
			csmlog.Errorf("can not get version of the array: %s", err.Error())
		} else {
			csmlog.Infof("array version: %f", version)
		}

		switch arr.BlockProtocol {
		case NVMeTCP:
			csmlog.Infof("block protocol is set to NVMeTCP")
			if version < 4.0 {
				csmlog.Warnf("NVMeTCP transport is not supported for array version %f", version)
			}
			if len(nvmeInitiators) == 0 {
				csmlog.Errorf("NVMeTCP transport was requested but NVMe initiator is not available")
			}
			s.useNVME = true
		case SDC:
			csmlog.Infof("block protocol is set to SDC")
			s.useSDC = true
		case "auto":
			csmlog.Infof("block protocol is set to auto — node will be either SDC or NVMe, or SDC by default")
			s.configureAutoBlockProtocol(ctx, version, len(nvmeInitiators))
		default:
			csmlog.Infof("block protocol is not set, defaulting to NFS")
		}

		if s.useNVME {
			if s.adminClients[arr.SystemID] == nil {
				csmlog.Infof("skipping NVMe host setup for array %s: not probed (may be in a different zone)", arr.SystemID)
				continue
			}
			if err := s.setupNVMeHost(nvmeInitiators, arr.SystemID); err != nil {
				csmlog.Errorf("can not setup NVMe host for array: %s", err.Error())
			}
		}
	}

	// Initialize rounding event emitter for controller mode (FR-5).
	// Uses K8sClientset if already set; falls back to k8sutils.Clientset.
	if s.roundingEmitter == nil {
		cs := K8sClientset
		if cs == nil {
			cs = k8sutils.Clientset
		}
		s.roundingEmitter = NewRoundingEventEmitter(cs, Name)
	}

	if s.mode == "node" {
		// Initialize space reclamation manager after useNVME is set
		initSpaceReclamation(ctx, s, k8sutils.Clientset)

		// Update the ConfigMap with the Interface IPs
		s.updateConfigMap(s.getIPAddressByInterface, ConfigMapFilePath)

		// Start the podmon API service
		go s.startAPIService(ctx)
	}

	// Gateway monitoring requires the metrics server.  If the metrics server is
	// disabled while gateway monitoring is enabled, suppress gateway monitoring
	// and log a warning so the misconfiguration is visible in the driver logs.
	if s.opts.GatewayMonitoringEnabled && !s.opts.MetricsEnabled {
		csmlog.Warnf("%s is true but %s is false — gateway monitoring will not start; enable the metrics server first",
			EnvGatewayMonitoringEnabled, EnvMetricsEnabled)
		s.opts.GatewayMonitoringEnabled = false
	}

	// Start the metrics server on every pod (controller and node) when the
	// metrics service is enabled.  The gateway polling loop is started
	// separately below only on the leader.
	if s.opts.MetricsEnabled {
		s.startMetricsServer()
		if s.metricsServer != nil {
			s.startCollectors(ctx)
		}
	}

	// Start the gateway polling loop when gateway monitoring is enabled.
	if s.opts.GatewayMonitoringEnabled && s.mode != "node" {
		if s.opts.GatewayMonitoringLeaderElectionEnabled {
			csmlog.Info("Gateway monitoring leader election enabled — polling will start on the lease holder")
			go s.startGatewayMonitoringWithLeaderElection(ctx)
		} else {
			s.startGatewayMonitor(ctx)
		}
	}

	if _, ok := csictx.LookupEnv(ctx, "X_CSI_VXFLEXOS_NO_PROBE_ON_START"); !ok {
		csmlog.Infof("BeforeServe probing starting %s", time.Now().Format("15:04:05.000000000"))
		newContext, cancel := context.WithTimeout(ctx, s.opts.probeTimeout)
		defer cancel()

		err := s.doProbe(newContext)

		csmlog.Infof("BeforeServe probing complete %s", time.Now().Format("15:04:05.000000000"))

		// After probing, resolve empty pools for any zone entries that omit pools.
		// Admin clients are now connected, so we can query the PowerFlex API.
		if err == nil {
			for _, arr := range s.opts.arrays {
				if len(arr.Zones) > 0 && s.adminClients[arr.SystemID] != nil {
					system := s.systems[arr.SystemID]
					if system != nil {
						lookup := &goscaleioPoolLookup{system: system}
						if resolveErr := resolveDefaultPools(arr.SystemID, arr.Zones, lookup); resolveErr != nil {
							return fmt.Errorf("failed to resolve default pools for system %s: %w", arr.SystemID, resolveErr)
						}
						csmlog.Infof("resolved default pools for system %s zones", arr.SystemID)
					} else {
						csmlog.Warnf("system %s has zones[] configured but no system client after probe; default pool resolution skipped", arr.SystemID)
					}
				}
			}
		}

		return err
	}

	return nil
}

func (s *service) configureAutoBlockProtocol(ctx context.Context, version float64, nvmeInitiators int) {
	// do nodeProbe to detect SDC
	if err := s.nodeProbe(ctx); err != nil {
		csmlog.WithContext(ctx).Infof("nodeProbe failed: %s", err.Error())
	}

	isSDC := s.opts.SdcGUID != ""
	isNVMe := nvmeInitiators != 0 && version >= 4.0

	switch {
	case isSDC:
		csmlog.WithContext(ctx).Info("SDC is available; using SDC protocol")
		s.useSDC = true
	case isNVMe:
		csmlog.WithContext(ctx).Info("NVMe/TCP is available; using NVMe/TCP protocol")
		s.useNVME = true
	default:
		csmlog.WithContext(ctx).Info("Neither SDC nor NVMe/TCP was detected; using NFS protocol")
	}
}

func (s *service) setupNVMeHost(nvmeInitiators []string, systemID string) error {
	csmlog.Infof("setting up NVMe host for array %s", systemID)
	defer csmlog.Infof("finished setting up NVMe host for array %s", systemID)

	if len(nvmeInitiators) == 0 {
		return fmt.Errorf("NVMe initiators not found on node")
	}
	csmlog.Infof("NVMe initiators found on node: %s", nvmeInitiators)

	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return fmt.Errorf("admin client not found for system %s", systemID)
	}

	// Set up NVMe host
	system, err := adminClient.FindSystem(systemID, "", "")
	if err != nil {
		csmlog.Errorf("unable to find system: %s", err.Error())
		return err
	}

	// Check if host with same name exists
	hosts, err := system.GetAllNvmeHosts()
	if err != nil {
		csmlog.Errorf("unable to get nvme hosts: %s", err.Error())
		return err
	}

	// Get node ID
	s.nodeID, err = s.generateNodeID()
	if err != nil {
		csmlog.Errorf("failed to generate node ID: %s", err.Error())
		return err
	}

	for _, host := range hosts {
		if host.Name == s.nodeID {
			csmlog.Infof("host with same name already exists: %s", host.Name)
			return nil
		}
		if host.Nqn != "" && slices.Contains(nvmeInitiators, host.Nqn) {
			s.nodeID = host.Name
			csmlog.Infof("Found existing host with matching NQN: %s", host.Name)
			return nil
		}
	}

	// If host not found → create new host
	nvmeHostParams := siotypes.NvmeHostParam{
		Name: s.nodeID,
		Nqn:  nvmeInitiators[0],
	}

	_, err = system.CreateNvmeHost(nvmeHostParams)
	if err != nil {
		csmlog.Errorf("unable to create nvme host: %s", err.Error())
		return err
	}

	return nil
}

func (s *service) generateNodeID() (string, error) {
	nodeUID, err := s.GetNodeUID(context.Background())
	if err != nil {
		return "", err
	}

	nodeIP, err := s.GetNodeIP(context.Background())
	if err != nil {
		return "", err
	}

	hashedNodeID := hashNodeID(nodeUID)
	nodeID := fmt.Sprintf("%s-%s", nodeIP, hashedNodeID)
	return nodeID[:31], nil
}

func (s *service) updateConfigMap(getIPAddressByInterfacefunc GetIPAddressByInterfacefunc, configFilePath string) {
	// Prepare driverConfigMap name using the release name
	releaseName := os.Getenv("RELEASE_NAME")
	driverConfigMap := releaseName + "-config-params"

	configFileData, err := os.ReadFile(filepath.Clean(configFilePath))
	if err != nil {
		csmlog.Errorf("Failed to read ConfigMap file: %v", err)
		return
	}

	var config Config
	err = yaml.Unmarshal(configFileData, &config)
	if err != nil {
		csmlog.Errorf("Failed to parse configMap data: %v", err)
		return
	}

	updateInterfaceNamesWithIPs := map[string]string{}
	for node, interfaceList := range config.InterfaceNames {

		if !strings.EqualFold(node, s.opts.KubeNodeName) {
			continue
		}
		interfaces := strings.Split(interfaceList, ",")
		var ipAddresses []string

		for _, interfaceName := range interfaces {
			interfaceName = strings.TrimSpace(interfaceName)

			// Find the IP of the Interfaces
			ipAddress, err := getIPAddressByInterfacefunc(interfaceName, &service{})
			if err != nil {
				csmlog.Errorf("failed to get IP address for interface %s: %v", interfaceName, err)
				continue
			}
			ipAddresses = append(ipAddresses, ipAddress)
		}
		if len(ipAddresses) > 0 {
			updateInterfaceNamesWithIPs[node] = strings.Join(ipAddresses, ",")
		}
	}

	// Get the Kubernetes ClientSet
	if K8sClientset == nil {
		err = k8sutils.CreateKubeClientSet()
		if err != nil {
			csmlog.Errorf("Failed to create Kubernetes ClientSet: %v", err)
			return
		}
		K8sClientset = k8sutils.Clientset
	}

	// Get the vxflexos-config-params ConfigMap
	cm, err := K8sClientset.CoreV1().ConfigMaps(DriverNamespace).Get(context.TODO(), driverConfigMap, metav1.GetOptions{})
	if err != nil {
		csmlog.Errorf("Failed to get ConfigMap: %v", err)
		return
	}

	var configData map[string]interface{}
	if existingYaml, ok := cm.Data["driver-config-params.yaml"]; ok {

		err := yaml.Unmarshal([]byte(existingYaml), &configData)
		if err != nil {
			csmlog.Errorf("Failed to parse ConfigMap data: %v", err)
			return
		}

		// Check and update Interfaces with the IPs
		if interfaceNames, ok := configData["interfaceNames"].(map[string]interface{}); ok {
			for node, ipAddressList := range updateInterfaceNamesWithIPs {
				interfaceNames[node] = ipAddressList
			}
		} else {
			csmlog.Errorf("interfaceNames key missing or not in expected format")
			return
		}

		updatedYaml, err := yaml.Marshal(configData)
		if err != nil {
			csmlog.Errorf("Failed to marshal updated data: %v", err)
			return
		}
		cm.Data["driver-config-params.yaml"] = string(updatedYaml)
	}

	// Update the vxflexos-config-params ConfigMap
	_, err = K8sClientset.CoreV1().ConfigMaps("vxflexos").Update(context.TODO(), cm, metav1.UpdateOptions{})
	if err != nil {
		csmlog.Errorf("Failed to update ConfigMap: %v", err)
		return
	}

	csmlog.Infof("ConfigMap updated successfully")
}

func (s *service) getIPAddressByInterface(interfaceName string, networkInterface NetworkInterface) (string, error) {
	interfaceObj, err := networkInterface.InterfaceByName(interfaceName)
	if err != nil {
		return "", err
	}

	addrs, err := networkInterface.Addrs(interfaceObj)
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ipNet.IP.To4() != nil {
			return ipNet.IP.String(), nil
		}
	}

	return "", fmt.Errorf("no IPv4 address found for interface %s", interfaceName)
}

func (s *service) isGenTypeNotSupportsNfsAndReplication(genType string) bool {
	return genType == "EC"
}

func (s *service) isNfsNotSupported(version float64) bool {
	// NFS is only supported in PowerFlex version 4.0+ and not in version <4.0 and in Nairobi
	return version < 4.0
}

func (s *service) isReplicationNotSupported(genType string) bool {
	// For Nairobi(5.0), replication is not supported.
	return genType == "EC"
}

func (s *service) isNFSEnabled(ctx context.Context, systemID string) (bool, error) {
	if err := s.systemProbeAll(ctx); err != nil {
		return false, err
	}

	version, err := s.GetPlatformVersion(systemID)
	if err != nil {
		return false, err
	}

	if s.isNfsNotSupported(version) {
		return false, nil
	}

	arrayConData, err := getArrayConfig(ctx)
	if err != nil {
		return false, err
	}

	array, exists := arrayConData[systemID]
	if !exists {
		return false, errors.New("array configuration not found for system: " + systemID)
	}

	// If no NAS name configured, NFS cannot be used
	if strings.TrimSpace(array.NasName) == "" {
		csmlog.WithContext(ctx).Warn("nasName is not set in the secret; it is required for NFS volume operations")
		return false, nil
	}

	system, err := s.adminClients[systemID].FindSystem(systemID, "", "")
	if err != nil {
		return false, errors.New("system not found: " + systemID)
	}

	return system.IsNFSEnabled()
}

// Probe all systems managed by driver
func (s *service) doProbe(ctx context.Context) error {
	// Putting in mutex to allow tests to pass with race flag
	px.Lock()
	defer px.Unlock()

	if !s.isNodeMode() {
		csmlog.WithContext(ctx).Info("[doProbe] running controller probe")
		if err := s.systemProbeAll(ctx); err != nil {
			return err
		}
	}

	// Do a node probe
	if !s.isControllerMode() {
		// Probe all systems managed by driver
		csmlog.WithContext(ctx).Info("[doProbe] running node probe")
		if err := s.systemProbeAll(ctx); err != nil {
			return err
		}

		if err := s.nodeProbe(ctx); err != nil {
			csmlog.WithContext(ctx).Infof("nodeProbe failed: %v", err)
		}
	}
	return nil
}

// RegisterAdditionalServers registers any additional grpc services that use the CSI socket.
func (s *service) RegisterAdditionalServers(server *grpc.Server) {
	csmlog.Info("Registering additional GRPC servers")
	podmon.RegisterPodmonServer(server, s)
	replication.RegisterReplicationServer(server, s)
	csi.RegisterGroupControllerServer(server, &groupControllerService{s: s})
}

// getVolProvisionType returns a string indicating thin or thick provisioning
// If the type is specified in the params map, that value is used, if not, defer
// to the service config
func (s *service) getVolProvisionType(params map[string]string) string {
	volType := thinProvisioned
	if s.opts.Thick {
		volType = thickProvisioned
	}

	if tp, ok := params[KeyThickProvisioning]; ok {
		tpb, err := strconv.ParseBool(tp)
		if err != nil {
			csmlog.Warnf("invalid boolean received %s=(%#v) in params",
				KeyThickProvisioning, tp)
		} else if tpb {
			volType = thickProvisioned
		} else {
			volType = thinProvisioned
		}
	}

	return volType
}

// getVolByID returns the PowerFlex volume from the given Powerflex volume ID
func (s *service) getVolByID(id string, systemID string) (*siotypes.Volume, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}
	// The GetVolume API returns a slice of volumes, but when only passing
	// in a volume ID, the response will be just the one volume
	vols, err := adminClient.GetVolume("", strings.TrimSpace(id), "", "", false)
	if err != nil {
		return nil, err
	}
	return vols[0], nil
}

// getFilesystemByID returns the PowerFlex filesystem from the given Powerflex filesystem ID
func (s *service) getFilesystemByID(id string, systemID string) (*siotypes.FileSystem, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}
	system, err := adminClient.FindSystem(systemID, "", "")
	if err != nil {
		return nil, fmt.Errorf("can't find system by id %s", systemID)
	}
	// The GetFileSystemByIDName API returns a filesystem, but when only passing
	// in a filesystem ID or name, the response will be just the one filesystem
	fs, err := system.GetFileSystemByIDName(id, "")
	if err != nil {
		return nil, err
	}
	return fs, nil
}

// getSDCID returns SDC ID from the given sdc GUID and system ID.
func (s *service) getSDCID(sdcGUID string, systemID string) (string, error) {
	sdcGUID = strings.ToUpper(sdcGUID)

	// Need to translate sdcGUID to fmt.Errorf("getSDCID error systemID not found: %s", systemID)
	if s.systems[systemID] == nil {
		return "", fmt.Errorf("getSDCID error systemID not found: %s", systemID)
	}
	id, err := s.systems[systemID].FindSdc("SdcGUID", sdcGUID)
	if err != nil {
		return "", fmt.Errorf("error finding SDC from GUID: %s, err: %s",
			sdcGUID, err.Error())
	}

	return id.Sdc.ID, nil
}

// getSDCIPs returns SDC IPs from the given sdc GUID and system ID.
func (s *service) getSDCIPs(sdcGUID string, systemID string) ([]string, error) { // name change
	sdcGUID = strings.ToUpper(sdcGUID)

	if s.systems[systemID] == nil {
		return nil, fmt.Errorf("getSDCIPs error systemID not found: %s", systemID)
	}
	id, err := s.systems[systemID].FindSdc("SdcGUID", sdcGUID)
	if err != nil {
		return nil, fmt.Errorf("error finding SDC from GUID: %s, err: %s",
			sdcGUID, err.Error())
	}

	return id.Sdc.SdcIPs, nil
}

var findStoragePoolFunc = func(adminClient *goscaleio.Client, id, name, storagePoolID, protectionDomain string) (*siotypes.StoragePool, error) {
	return adminClient.FindStoragePool(id, name, storagePoolID, protectionDomain)
}

// getStoragePoolID returns pool ID from the given name, system ID, and protectionDomain name
func (s *service) getStoragePoolID(name, systemID, pdID string) (string, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return "", fmt.Errorf("admin client not found for system %s", systemID)
	}
	// Need to lookup ID from the gateway, with respect to PD if provided
	pool, err := findStoragePoolFunc(adminClient, "", name, "", pdID)
	if err != nil {
		return "", err
	}

	return pool.ID, nil
}

// getCSIVolume converts the given siotypes.Volume to a CSI volume
func (s *service) getCSIVolume(vol *siotypes.Volume, systemID string) *csi.Volume {
	// Get storage pool name; add to cache of ID to Name if not present
	storagePoolName := s.getStoragePoolNameFromID(systemID, vol.StoragePoolID)
	installationID, err := s.getArrayInstallationID(systemID)
	if err != nil {
		csmlog.Infof("getCSIVolume error system not found: %s with error: %v\n", systemID, err)
	}

	// Make the additional volume attributes
	attributes := map[string]string{
		"Name":            vol.Name,
		"StoragePoolID":   vol.StoragePoolID,
		"StoragePoolName": storagePoolName,
		"StorageSystem":   systemID,
		"CreationTime":    time.Unix(int64(vol.CreationTime), 0).String(),
		"InstallationID":  installationID,
	}
	dash := "-"
	vi := &csi.Volume{
		VolumeId:      systemID + dash + vol.ID,
		CapacityBytes: int64(vol.SizeInKb * bytesInKiB),
		VolumeContext: attributes,
	}

	return vi
}

// getCSIVolumeFromFilesystem converts the given siotypes.FileSystem to a CSI volume
func (s *service) getCSIVolumeFromFilesystem(fs *siotypes.FileSystem, systemID string) *csi.Volume {
	// Get storage pool name; add to cache of ID to Name if not present
	storagePoolName := s.getStoragePoolNameFromID(systemID, fs.StoragePoolID)
	installationID, err := s.getArrayInstallationID(systemID)
	if err != nil {
		csmlog.Infof("getCSIVolumeFromFilesystem error system not found: %s with error: %v\n", systemID, err)
	}

	// Make the additional volume attributes
	creationTime, _ := strconv.Atoi(fs.CreationTimestamp)
	attributes := map[string]string{
		"Name":            fs.Name,
		"StoragePoolID":   fs.StoragePoolID,
		"StoragePoolName": storagePoolName,
		"StorageSystem":   systemID,
		"CreationTime":    time.Unix(int64(creationTime), 0).String(),
		"InstallationID":  installationID,
		"NasServerID":     fs.NasServerID,
		"fsType":          "nfs",
	}
	hyphen := "/"

	vi := &csi.Volume{
		VolumeId:      systemID + hyphen + fs.ID,
		CapacityBytes: int64(fs.SizeTotal),
		VolumeContext: attributes,
	}

	return vi
}

// getArryaInstallationID returns installation ID for the given system ID
func (s *service) getArrayInstallationID(systemID string) (string, error) {
	system, err := s.adminClients[systemID].FindSystem(systemID, "", "")
	if err != nil {
		return "", err
	}
	return system.System.InstallID, nil
}

// Convert an SIO Volume into a CSI Snapshot object suitable for return.
func (s *service) getCSISnapshot(vol *siotypes.Volume, systemID string) *csi.Snapshot {
	dash := "-"
	snapshot := &csi.Snapshot{
		SizeBytes:      int64(vol.SizeInKb) * bytesInKiB,
		SnapshotId:     systemID + dash + vol.ID,
		SourceVolumeId: systemID + dash + vol.AncestorVolumeID,
		ReadyToUse:     true,
	}
	// Convert array timestamp to CSI timestamp and add
	csiTimestamp := timestamppb.New(time.Unix(int64(vol.CreationTime), 0))
	if csiTimestamp != nil {
		snapshot.CreationTime = csiTimestamp
	}
	return snapshot
}

func (s *service) getCSISnapshotFromFileSystem(fs *siotypes.FileSystem, systemID string) *csi.Snapshot {
	slash := "/"
	snapshot := &csi.Snapshot{
		SizeBytes:      int64(fs.SizeTotal),
		SnapshotId:     systemID + slash + fs.ID,
		SourceVolumeId: fs.ParentID,
		ReadyToUse:     true,
	}
	creationTime, _ := strconv.Atoi(fs.CreationTimestamp)
	// Convert array timestamp to CSI timestamp and add
	csiTimestamp := timestamppb.New(time.Unix(int64(creationTime), 0))
	if csiTimestamp != nil {
		snapshot.CreationTime = csiTimestamp
	}
	return snapshot
}

// Returns storage pool name from the given storage pool ID and system ID
func (s *service) getStoragePoolNameFromID(systemID, id string) string {
	storagePoolName := s.storagePoolIDToName[id]
	if storagePoolName == "" {
		adminClient := s.adminClients[systemID]
		pool, err := adminClient.FindStoragePool(id, "", "", "")
		if err == nil {
			storagePoolName = pool.Name
			s.storagePoolIDToName[id] = pool.Name
		} else {
			csmlog.Infof("Could not found StoragePool: %s on system %s", id, systemID)
		}
	}
	return storagePoolName
}

// Provide periodic logging of statistics like goroutines and memory
func (s *service) logStatistics() {
	if s.statisticsCounter = s.statisticsCounter + 1; (s.statisticsCounter % 100) == 0 {
		goroutines := runtime.NumGoroutine()
		memstats := new(runtime.MemStats)
		runtime.ReadMemStats(memstats)
		fields := map[string]interface{}{
			"GoRoutines":   goroutines,
			"HeapAlloc":    memstats.HeapAlloc,
			"HeapReleased": memstats.HeapReleased,
			"StackSys":     memstats.StackSys,
		}
		csmlog.WithFields(fields).Infof("resource statistics counter: %d", s.statisticsCounter)
	}
}

func getArrayConfig(ctx context.Context) (map[string]*ArrayConnectionData, error) {
	arrays := make(map[string]*ArrayConnectionData)

	_, err := os.Stat(ArrayConfigFile)
	if err != nil {
		csmlog.WithContext(ctx).Errorf("Found error %v while checking stat of file %s ", err, ArrayConfigFile)
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file %s does not exist", ArrayConfigFile)
		}
	}

	config, err := os.ReadFile(filepath.Clean(ArrayConfigFile))
	if err != nil {
		return nil, fmt.Errorf("file %s errors: %v", ArrayConfigFile, err)
	}

	if string(config) != "" {
		creds := make([]ArrayConnectionData, 0)
		// support backward compatibility
		config, _ = yaml.JSONToYAML(config)
		err = yaml.Unmarshal(config, &creds)
		if err != nil {
			return nil, fmt.Errorf("unable to parse the credentials: %v", err)
		}

		if len(creds) == 0 {
			return nil, fmt.Errorf("%s", "no arrays are provided in vxflexos-creds secret")
		}

		noOfDefaultArray := 0
		crossSystemZones := make(map[ZoneName]string)
		for i, c := range creds {
			systemID := c.SystemID
			if _, ok := arrays[systemID]; ok {
				return nil, fmt.Errorf("duplicate system ID %s found at index %d", systemID, i)
			}
			if systemID == "" {
				return nil, fmt.Errorf("invalid value for system name at index %d", i)
			}
			if c.Username == "" {
				return nil, fmt.Errorf("invalid value for Username at index %d", i)
			}
			if c.Password == "" {
				return nil, fmt.Errorf("invalid value for Password at index %d", i)
			}
			if c.Endpoint == "" {
				return nil, fmt.Errorf("invalid value for Endpoint at index %d", i)
			}
			if strings.TrimSpace(c.BlockProtocol) == "" {
				csmlog.WithContext(ctx).Infof("BlockProtocol is not set, defaulting to auto")
				c.BlockProtocol = "auto"
			}
			// ArrayConnectionData
			if c.AllSystemNames != "" {
				names := strings.Split(c.AllSystemNames, ",")
				csmlog.WithContext(ctx).Infof("Powerflex systemID %s AllSytemNames given %#v\n", systemID, names)
			}

			// for PowerFlex v4.0
			if strings.TrimSpace(c.NasName) == "" {
				c.NasName = ""
			}

			skipCertificateValidation := c.SkipCertificateValidation || c.Insecure

			fields := map[string]interface{}{
				"endpoint":                  c.Endpoint,
				"user":                      c.Username,
				"password":                  "********",
				"skipCertificateValidation": skipCertificateValidation,
				"isDefault":                 c.IsDefault,
				"systemID":                  c.SystemID,
				"allSystemNames":            c.AllSystemNames,
				"nasName":                   c.NasName,
				"blockProtocol":             c.BlockProtocol,
			}

			csmlog.WithFields(fields).Infof("configured %s", c.SystemID)

			if c.IsDefault {
				noOfDefaultArray++
			}

			if noOfDefaultArray > 1 {
				return nil, fmt.Errorf("'isDefault' parameter presents more than once in storage array list")
			}

			// copy in the arrayConnectionData to arrays
			copyOfCred := ArrayConnectionData{}
			copyOfCred = c

			// normalize legacy singular "zone" to "zones[]"
			if err := normalizeZoneConfig(&copyOfCred); err != nil {
				return nil, err
			}

			// validate per-system PD uniqueness within zones[]
			if err := validateZonePDUniqueness(copyOfCred.SystemID, copyOfCred.Zones); err != nil {
				return nil, err
			}

			// validate that a zone name is not used by multiple systems
			for _, z := range copyOfCred.Zones {
				if prevSystem, ok := crossSystemZones[z.Name]; ok {
					return nil, fmt.Errorf("zone %s is defined on multiple systems (%s and %s)", z.Name, prevSystem, copyOfCred.SystemID)
				}
				crossSystemZones[z.Name] = copyOfCred.SystemID
			}

			arrays[c.SystemID] = &copyOfCred
		}
	} else {
		return nil, fmt.Errorf("arrays details are not provided in vxflexos-creds secret")
	}

	// Log zone config summary at startup for diagnostics
	csmlog.WithContext(ctx).Info(getZoneConfigSummary(arrays))

	return arrays, nil
}

// normalizeZoneConfig normalizes the legacy singular "zone" field into the
// "zones[]" model. If both "zone" and "zones[]" are present, it returns an
// error. If only "zone" is present, it is promoted to a single-element
// "zones[]". AvailabilityZone is retained until preinit.go is migrated.
func normalizeZoneConfig(a *ArrayConnectionData) error {
	hasLegacy := a.AvailabilityZone != nil
	hasZones := len(a.Zones) > 0

	if hasLegacy && hasZones {
		return fmt.Errorf("system %s: both 'zone' and 'zones' are present; use only 'zones'", a.SystemID)
	}

	if hasLegacy {
		a.Zones = []AvailabilityZone{*a.AvailabilityZone}
		// TODO(<ECSDF-XXXX>): preinit.go still reads AvailabilityZone; clear it once migrated.
	}

	return nil
}

// zonePoolLookup abstracts the PowerFlex API calls needed for default pool
// resolution, enabling unit testing without a live array connection.
type zonePoolLookup interface {
	// FindPoolsForPD returns the storage pool names for the given PD name.
	FindPoolsForPD(pdName string) ([]string, error)
}

// goscaleioPoolLookup implements zonePoolLookup using the goscaleio SDK.
type goscaleioPoolLookup struct {
	system *sio.System
}

func (g *goscaleioPoolLookup) FindPoolsForPD(pdName string) ([]string, error) {
	pd, err := g.system.FindProtectionDomain("", pdName, "")
	if err != nil {
		return nil, err
	}
	pdEx, err := g.system.GetProtectionDomainEx(pd.ID)
	if err != nil {
		return nil, err
	}
	pools, err := pdEx.GetStoragePool("")
	if err != nil {
		return nil, err
	}
	names := make([]string, len(pools))
	for i, p := range pools {
		names[i] = p.Name
	}
	return names, nil
}

// resolveDefaultPools fills in the default storage pool for any zone entry
// whose ProtectionDomains[0].Pools is empty. It queries the PowerFlex API
// via the provided zonePoolLookup and assigns the first available pool.
func resolveDefaultPools(systemID string, zones []AvailabilityZone, lookup zonePoolLookup) error {
	for i := range zones {
		if len(zones[i].ProtectionDomains) == 0 {
			continue
		}
		if len(zones[i].ProtectionDomains) > 1 {
			return fmt.Errorf("system %s zone %s: only one protection domain per zone is supported", systemID, zones[i].Name)
		}
		pd := &zones[i].ProtectionDomains[0]
		if len(pd.Pools) > 0 {
			continue
		}
		pools, err := lookup.FindPoolsForPD(string(pd.Name))
		if err != nil {
			return fmt.Errorf("system %s zone %s: failed to resolve pools for PD %q: %w", systemID, zones[i].Name, pd.Name, err)
		}
		if len(pools) == 0 {
			return fmt.Errorf("system %s zone %s: no storage pools found for PD %q", systemID, zones[i].Name, pd.Name)
		}
		pd.Pools = []PoolName{PoolName(pools[0])}
	}
	return nil
}

// validateZonePDUniqueness rejects duplicate Protection Domain names within a
// single system's zones[]. The same PD name on different systems is allowed.
func validateZonePDUniqueness(systemID string, zones []AvailabilityZone) error {
	seen := map[ProtectionDomainName]ZoneName{}
	for _, z := range zones {
		if len(z.ProtectionDomains) == 0 {
			continue
		}
		if len(z.ProtectionDomains) > 1 {
			return fmt.Errorf("system %s zone %s: only one protection domain per zone is supported", systemID, z.Name)
		}
		pd := z.ProtectionDomains[0].Name
		if prev, ok := seen[pd]; ok {
			return fmt.Errorf("system %s: duplicate protection domain %q used by both zone %q and zone %q", systemID, pd, prev, z.Name)
		}
		seen[pd] = z.Name
	}
	return nil
}

// getVolumeIDFromCsiVolumeId returns PowerFlex volume ID from CSI volume ID
func getVolumeIDFromCsiVolumeID(csiVolID string) string {
	if csiVolID == "" {
		return ""
	}
	i := strings.LastIndex(csiVolID, "-")
	if i == -1 {
		return csiVolID
	}
	tokens := strings.Split(csiVolID, "-")
	return tokens[len(tokens)-1]
}

// getFilesystemIDFromCsiVolumeID returns PowerFlex filesystem ID from CSI volume ID
func getFilesystemIDFromCsiVolumeID(csiVolID string) string {
	if csiVolID == "" {
		return ""
	}
	containsHyphen := strings.Contains(csiVolID, "/")
	if containsHyphen {
		tokens := strings.Split(csiVolID, "/")
		return tokens[len(tokens)-1]
	}
	err := errors.New("csiVolID unexpected string")
	csmlog.Errorf("%s format error: %v", csiVolID, err)
	return ""
}

// getNFSExport method returns the NFSExport for a given filesystem
// and returns a not found error if the NFSExport does not exist for filesystem.
func (s *service) getNFSExport(fs *siotypes.FileSystem, client *goscaleio.Client) (*siotypes.NFSExport, error) {
	nfsExportList, err := client.GetNFSExport()
	if err != nil {
		return nil, err
	}

	for _, nfsExport := range nfsExportList {
		if nfsExport.FileSystemID == fs.ID {
			return &nfsExport, nil
		}
	}

	return nil, status.Errorf(codes.NotFound, "NFS Export for the NFS volume: %s not found", fs.Name)
}

// getFileInterface method returns the FileInterface for the given filesytem.
func (s *service) getFileInterface(systemID string, fs *siotypes.FileSystem, client *goscaleio.Client) (*siotypes.FileInterface, error) {
	system, err := client.FindSystem(systemID, "", "")
	if err != nil {
		return nil, err
	}

	nas, err := system.GetNASByIDName(fs.NasServerID, "")
	if err != nil {
		return nil, err
	}

	fileInterface, err := system.GetFileInterface(nas.CurrentPreferredIPv4InterfaceID)
	if err != nil {
		return nil, err
	}
	return fileInterface, err
}

// getSystemIDFromCsiVolumeId returns PowerFlex volume ID from CSI volume ID
func (s *service) getSystemIDFromCsiVolumeID(csiVolID string) string {
	containsSlash := strings.Contains(csiVolID, "/")
	if containsSlash {
		i := strings.LastIndex(csiVolID, "/")
		tokens := strings.Split(csiVolID, "/")
		// expected format: sysId/fsId
		if len(tokens) == 2 {
			sys := csiVolID[:i]
			if id, ok := s.connectedSystemNameToID[sys]; ok {
				return id
			}
			return sys
		}
	} else {
		i := strings.LastIndex(csiVolID, "-")
		if i == -1 {
			return ""
		}
		tokens := strings.Split(csiVolID, "-")
		// expected format: sysId-volId
		if len(tokens) == 2 {
			sys := csiVolID[:i]
			if id, ok := s.connectedSystemNameToID[sys]; ok {
				return id
			}
			return sys
		}
	}
	// There is only volume ID in csi volume ID
	return ""
}

// Contains checks if the a string is present in a slice of strings
func Contains(slice []string, element string) bool {
	for _, a := range slice {
		if a == element {
			return true
		}
	}
	return false
}

// parseMask converts the subnet mask from CIDR notation to the dotted-decimal format
// An input of x.x.x.x/32 will return 255.255.255.255
func parseMask(ipaddr string) (mask string, err error) {
	removeExtra := regexp.MustCompile(`^(.*[\\/])`)
	asd := ipaddr[len(ipaddr)-3:]
	findSubnet := removeExtra.ReplaceAll([]byte(asd), []byte(""))
	subnet, err := strconv.ParseInt(string(findSubnet), 10, 64)
	if err != nil {
		return "", errors.New("parse mask: error parsing mask")
	}
	if subnet < 0 || subnet > 32 {
		return "", errors.New("invalid subnet mask")
	}
	var buff bytes.Buffer
	for i := 0; i < int(subnet); i++ {
		buff.WriteString("1")
	}
	for i := subnet; i < 32; i++ {
		buff.WriteString("0")
	}
	masker := buff.String()
	a, _ := strconv.ParseUint(masker[:8], 2, 64)
	b, _ := strconv.ParseUint(masker[8:16], 2, 64)
	c, _ := strconv.ParseUint(masker[16:24], 2, 64)
	d, _ := strconv.ParseUint(masker[24:32], 2, 64)
	resultMask := fmt.Sprintf("%v.%v.%v.%v", a, b, c, d)
	return resultMask, nil
}

// GetIPListWithMaskFromString returns ip and mask in string form found in input string
// A return value of nil indicates no match
func GetIPListWithMaskFromString(input string) (string, error) {
	// Split the IP address and subnet mask if present
	parts := strings.Split(input, "/")
	ip := parts[0]
	result := net.ParseIP(ip)
	if result == nil {
		return "", errors.New("doesn't seem to be a valid IP")
	}
	if len(parts) > 1 {
		// ideally there will be only 2 substrings for a valid IP/SubnetMask
		if len(parts) > 2 {
			return "", errors.New("doesn't seem to be a valid IP")
		}
		mask, err := parseMask(input)
		if err != nil {
			return "", errors.New("doesn't seem to be a valid IP")
		}
		ip = ip + "/" + mask
	}
	return ip, nil
}

// ParseCIDR parses the CIDR address to the valid start IP range with Mask
func ParseCIDR(externalAccessCIDR string) (string, error) {
	// check if externalAccess has netmask bit or not
	if !strings.Contains(externalAccessCIDR, "/") {
		// if externalAccess is a plane ip we can add /32 from our end
		externalAccessCIDR += "/32"
		csmlog.Debugf("externalAccess after appending netMask bit:  %v", externalAccessCIDR)
	}
	ip, ipnet, err := net.ParseCIDR(externalAccessCIDR)
	if err != nil {
		return "", err
	}
	csmlog.Debugf("Parsed CIDR: %s -> ip: %v net: %v", externalAccessCIDR, ip, ipnet)
	start, _ := cidr.AddressRange(ipnet)
	fromString, err := GetIPListWithMaskFromString(externalAccessCIDR)
	if err != nil {
		return "", err
	}
	csmlog.Debugf("IP with Mask:  %v", fromString)
	part := strings.Split(fromString, "/")

	// ExernalAccess IP consists of Starting range IP of CIDR+Mask and hence concatenating the same to remove from the array
	externalAccess := start.String() + "/" + part[1]
	return externalAccess, nil
}

// ExternalAccessAlreadyAdded return true if externalAccess is present on ARRAY in any access mode type
func externalAccessAlreadyAdded(export *siotypes.NFSExport, externalAccess string) bool {
	if Contains(export.ReadWriteRootHosts, externalAccess) || Contains(export.ReadWriteHosts, externalAccess) || Contains(export.ReadOnlyRootHosts, externalAccess) || Contains(export.ReadOnlyHosts, externalAccess) {
		csmlog.Debugf("ExternalAccess is already added into Host Access list on array:  %v", externalAccess)
		return true
	}
	csmlog.Debugf("Going to add externalAccess into Host Access list on array:  %v", externalAccess)
	return false
}

func (s *service) unexportFilesystem(ctx context.Context, _ *csi.ControllerUnpublishVolumeRequest, client *goscaleio.Client, fs *siotypes.FileSystem, volumeContextID string, nodeIPs []string, nodeID string) error {
	nfsExportName := NFSExportNamePrefix + fs.Name
	nfsExportExists := false
	var nfsExportID string
	// Check if nfs export exists for the File system
	nfsExportList, err := client.GetNFSExport()
	if err != nil {
		return err
	}

	for _, nfsExport := range nfsExportList {
		if nfsExport.FileSystemID == fs.ID {
			nfsExportExists = true
			nfsExportID = nfsExport.ID
		}
	}

	if !nfsExportExists {
		csmlog.WithContext(ctx).Infof("NFS Share: %s not found on array.", nfsExportName)
		return nil
	}

	// remove host access from NFS Export
	nfsExportResp, err := client.GetNFSExportByIDName(nfsExportID, "")
	if err != nil {
		return status.Errorf(codes.NotFound, "Could not find NFS Export: %s", err)
	}

	fmt.Printf("%#v\n", nfsExportResp)

	var modifyParam *siotypes.NFSExportModify = &siotypes.NFSExportModify{}

	sort.Strings(nfsExportResp.ReadOnlyHosts)
	index := 0
	for _, nodeIP := range nodeIPs {
		index = sort.SearchStrings(nfsExportResp.ReadOnlyHosts, nodeIP)
		if len(nfsExportResp.ReadOnlyHosts) > 0 {
			if index >= 0 {
				modifyParam.RemoveReadOnlyHosts = append(modifyParam.RemoveReadOnlyHosts, nodeIP+"/255.255.255.255") // we can't remove without netmask
				csmlog.WithContext(ctx).Debugf("Going to remove IP from ROHosts:  %v", nodeIP)
			}
		}
	}

	sort.Strings(nfsExportResp.ReadOnlyRootHosts)
	for _, nodeIP := range nodeIPs {
		index = sort.SearchStrings(nfsExportResp.ReadOnlyRootHosts, nodeIP)
		if len(nfsExportResp.ReadOnlyRootHosts) > 0 {
			if index >= 0 {
				modifyParam.RemoveReadOnlyRootHosts = append(modifyParam.RemoveReadOnlyRootHosts, nodeIP+"/255.255.255.255") // we can't remove without netmask
				csmlog.WithContext(ctx).Debugf("Going to remove IP from RORootHosts:  %v", nodeIP)
			}
		}
	}

	for _, nodeIP := range nodeIPs {
		if Contains(nfsExportResp.ReadWriteHosts, nodeIP+"/255.255.255.255") {
			modifyParam.RemoveReadWriteHosts = append(modifyParam.RemoveReadWriteHosts, nodeIP+"/255.255.255.255") // we can't remove without netmask
			csmlog.WithContext(ctx).Debugf("Going to remove IP from RWHosts:  %v", nodeIP)
		}
	}

	for _, nodeIP := range nodeIPs {
		if Contains(nfsExportResp.ReadWriteRootHosts, nodeIP+"/255.255.255.255") {
			modifyParam.RemoveReadWriteRootHosts = append(modifyParam.RemoveReadWriteRootHosts, nodeIP+"/255.255.255.255") // we can't remove without netmask
			csmlog.WithContext(ctx).Debugf("Going to remove IP from RWRootHosts:  %v", nodeIP)
		}
	}

	err = client.ModifyNFSExport(modifyParam, nfsExportID)
	if err != nil {
		return status.Errorf(codes.NotFound, "Allocating host %s access to NFS Export failed. Error: %v", nodeID, err)
	}
	csmlog.WithContext(ctx).Debugf("Host: %s access is removed from NFS Share: %s", nodeID, nfsExportID)
	csmlog.WithContext(ctx).Debugf("ControllerUnpublishVolume successful for volid: [%s]", volumeContextID)

	return nil
}

// exportFilesystem - Method to export filesystem with idempotency
func (s *service) exportFilesystem(ctx context.Context, _ *csi.ControllerPublishVolumeRequest, client *goscaleio.Client, fs *siotypes.FileSystem, nodeIPs []string, externalAccess string, nodeID string, pContext map[string]string, am *csi.VolumeCapability_AccessMode) (*csi.ControllerPublishVolumeResponse, error) {
	for i, nodeIP := range nodeIPs {
		nodeIPs[i] = nodeIP + "/255.255.255.255"
	}
	var nfsExportName string
	nfsExportName = NFSExportNamePrefix + fs.Name

	nfsExportExists := false
	var nfsExportID string

	// Check if nfs export exists for the File system
	nfsExportList, err := client.GetNFSExport()
	if err != nil {
		return nil, err
	}

	for _, nfsExport := range nfsExportList {
		if nfsExport.FileSystemID == fs.ID {
			nfsExportExists = true
			nfsExportID = nfsExport.ID
			nfsExportName = nfsExport.Name
		}
	}

	// Create NFS export if it doesn't exist
	if !nfsExportExists {
		csmlog.WithContext(ctx).Debugf("NFS Export does not exist for fs: %s ,proceeding to create NFS Export", fs.Name)
		resp, err := client.CreateNFSExport(&siotypes.NFSExportCreate{
			Name:         nfsExportName,
			FileSystemID: fs.ID,
			Path:         NFSExportLocalPath + fs.Name,
		})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "create NFS Export failed. Error:%v", err)
		}

		nfsExportID = resp.ID
	}

	nfsExportResp, err := client.GetNFSExportByIDName(nfsExportID, "")
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Could not find NFS Export: %s", err)
	}

	readOnlyHosts := nfsExportResp.ReadOnlyHosts
	readWriteHosts := nfsExportResp.ReadWriteHosts
	readOnlyRootHosts := nfsExportResp.ReadOnlyRootHosts
	readWriteRootHosts := nfsExportResp.ReadWriteRootHosts

	foundIncompatible := false
	foundIdempotent := false
	otherHostsWithAccess := len(readOnlyHosts)

	var readHostList, readWriteHostList []string

	for _, host := range readOnlyHosts {
		if Contains(nodeIPs, host) {
			foundIncompatible = true
			break
		}
	}

	otherHostsWithAccess += len(readWriteHosts)
	if !foundIncompatible {
		for _, host := range readWriteHosts {
			if Contains(nodeIPs, host) {
				foundIncompatible = true
				break
			}
		}
	}

	otherHostsWithAccess += len(readOnlyRootHosts)
	if !foundIncompatible {
		for _, host := range readOnlyRootHosts {
			if Contains(nodeIPs, host) {
				if am.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
					foundIdempotent = true
				} else {
					foundIncompatible = true
				}
			}
		}
	}
	otherHostsWithAccess += len(readWriteRootHosts)

	if !foundIncompatible && !foundIdempotent {
		for _, host := range readWriteRootHosts {
			if Contains(nodeIPs, host) {
				if am.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
					foundIncompatible = true
				} else {
					foundIdempotent = true
					otherHostsWithAccess--
				}
			}
		}
	}

	if foundIncompatible {
		return nil, status.Errorf(codes.NotFound, "Host: %s has access on NFS Export: %s with incompatible access mode.", nodeID, nfsExportID)
	}

	if (am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER || am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER || am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER) && otherHostsWithAccess > 0 {
		return nil, status.Errorf(codes.NotFound, "Other hosts have access on NFS Share: %s", nfsExportID)
	}

	// Idempotent case
	if foundIdempotent {
		csmlog.WithContext(ctx).Info("Host has access to the given host and exists in the required state.")
		return &csi.ControllerPublishVolumeResponse{PublishContext: pContext}, nil
	}

	// Check and remove the default host if given in external access
	if Contains(nodeIPs, externalAccess) {
		csmlog.WithContext(ctx).Debug("Setting externalAccess to empty as it contains the host ip")
		externalAccess = ""
	}

	// Allocate host access to NFS Share with appropriate access mode
	if am.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
		readHostList = append(readHostList, nodeIPs...)
		if externalAccess != "" && !externalAccessAlreadyAdded(nfsExportResp, externalAccess) {
			readHostList = append(readHostList, externalAccess)
		}
		err := client.ModifyNFSExport(&siotypes.NFSExportModify{AddReadOnlyRootHosts: readHostList}, nfsExportID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Allocating host access failed with the error: %v", err)
		}
	} else {
		readWriteHostList = append(readWriteHostList, nodeIPs...)
		if externalAccess != "" && !externalAccessAlreadyAdded(nfsExportResp, externalAccess) {
			readWriteHostList = append(readWriteHostList, externalAccess)
		}
		err := client.ModifyNFSExport(&siotypes.NFSExportModify{AddReadWriteRootHosts: readWriteHostList}, nfsExportID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Allocating host access failed with the error: %v", err)
		}
	}

	csmlog.WithContext(ctx).Debugf("NFS Export: %s is accessible to host: %s with access mode: %s", nfsExportID, nodeID, am.Mode)
	csmlog.WithContext(ctx).Debugf("ControllerPublishVolume successful for volid: [%s]", pContext["volumeContextId"])

	return &csi.ControllerPublishVolumeResponse{PublishContext: pContext}, nil
}

// this function updates volumePrefixToSystems, a map of volume ID prefixes -> system IDs
// this is needed for checkSystemVolumes, a function that verifies that any legacy vol ID
// is found on the default system, only
func (s *service) UpdateVolumePrefixToSystemsMap(systemID string) error {
	// get one vol from system
	vols, _, err := s.listVolumes(systemID, 0, 1, true, false, "", "")
	if err != nil {

		csmlog.Errorf("failed to list vols for array %s : %s ", systemID, err.Error())
		return fmt.Errorf("failed to list vols for array %s : %s ", systemID, err.Error())

	}

	if len(vols) == 0 {
		// if system has no volumes, then there can't be a legacy vol on it
		csmlog.Infof("systemID: %s  has no volumes, not adding to volumePrefixToSystems map. \n", systemID)
		return nil

	}
	volID := vols[0].ID

	csmlog.Infof("vol id in UpdateVolumePrefixToSystemsMap is: %s  from systemID: %s \n", volID, systemID)

	// use first 24 bit from volume id as a key and system id as a value, and add this entry to the map

	key := s.calcKeyForMap(volID)

	if _, ok := s.volumePrefixToSystems[key]; ok {

		// if key found:
		// make sure systemID isn't already added for the specific key
		if contains(s.volumePrefixToSystems[key], systemID) {
			csmlog.Infof("volumePrefixToSystems: systemID: %s  already added for key %s. Not adding for key again. \n", systemID, key)
			return nil
		}
		// systemID has not been added to key before, add it
		csmlog.Infof("volumePrefixToSystems: Adding systemID %s to key %s \n", systemID, key)
		s.volumePrefixToSystems[key] = append(s.volumePrefixToSystems[key], systemID)

	} else {
		// if key not found:
		csmlog.Infof("volumePrefixToSystems: adding new key, value pair: key %s, systemID: %s \n", key, systemID)
		s.volumePrefixToSystems[key] = []string{systemID}
	}

	return nil
}

func (s *service) checkVolumesMap(volumeID string) error {
	systemID := s.getSystemIDFromCsiVolumeID(volumeID)

	// ID is legacy, so we  ensure it's only found on default system
	if systemID == "" {

		csmlog.Infof("volume id in checkVolumesMap is: %s \n", volumeID)
		csmlog.Infof("volume %s ,assumed to be on default system. \n", volumeID)

		if len(volumeID) < 3 {
			err := errors.New("vol ID too short")
			csmlog.Errorf("volume id %s is shorter than 3 chars, returning error: %v", volumeID, err)
			return fmt.Errorf("volume id %s is shorter than 3 chars, returning error", volumeID)
		}

		key := s.calcKeyForMap(volumeID)

		if _, ok := s.volumePrefixToSystems[key]; ok {
			// key found, make sure vol isn't on non-default system
			// For each systemID in s.volumePrefixToSystems[key], read all volumes from the system
			for _, systemID := range s.volumePrefixToSystems[key] {
				vols, _, err := s.listVolumes(systemID, 0, 0, true, false, "", "")
				if err != nil {
					csmlog.Errorf("failed to list vols for array %s : %s ", systemID, err.Error())
					return fmt.Errorf("failed to list vols for array %s : %s ", systemID, err.Error())
				}
				for _, vol := range vols {
					if vol.ID == volumeID {
						// legacy volume found on non-default system, this is an error
						csmlog.Errorf("found volume id %s on non-default system %s. expecting this volume id only on default system. aborting operation: %v", volumeID, systemID, err)
						return fmt.Errorf("found volume id %s on non-default system %s. expecting this volume id only on default system. aborting operation ", volumeID, systemID)
					}
				}
			}
		}

		// volume was not found on a non default system.
		csmlog.Infof("checkVolumesMap returns OK")
		return nil
	}

	// volume was not legacy
	csmlog.Infof("Volume ID: %s contains system ID: %s. checkVolumesMap passed", volumeID, systemID)
	return nil
}

// needs to get first 24 bits of VOlID, this is equivalent to first 3 bytes
func (s *service) calcKeyForMap(volumeID string) string {
	bytes := []byte(volumeID)
	key := string(bytes[0:3])
	return key
}

var getProtectionDomainIDFromNameFunc = func(adminClient *goscaleio.Client, systemID, protectionDomainName string) (string, error) {
	if protectionDomainName == "" {
		csmlog.Infof("Protection Domain not provided; there could be conflicts if two storage pools share a name")
		return "", nil
	}
	system, err := adminClient.FindSystem(systemID, "", "")
	if err != nil {
		return "", err
	}
	pd, err := system.FindProtectionDomain("", protectionDomainName, "")
	if err != nil {
		return "", err
	}
	return pd.ID, nil
}

func (s *service) getProtectionDomainIDFromName(systemID, protectionDomainName string) (string, error) {
	if protectionDomainName == "" {
		return "", nil
	}
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return "", fmt.Errorf("admin client not found for system %s", systemID)
	}
	return getProtectionDomainIDFromNameFunc(adminClient, systemID, protectionDomainName)
}

func (s *service) getSystem(systemID string) (*siotypes.System, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	// Gets the desired system content. Needed for remote replication.
	systems, err := adminClient.GetSystems()
	if err != nil {
		return nil, err
	}
	for _, system := range systems {
		if system.ID == systemID {
			return system, nil
		}
	}
	return nil, fmt.Errorf("system %s not found", systemID)
}

func (s *service) getPeerMdms(systemID string) ([]*siotypes.PeerMDM, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	mdms, err := adminClient.GetPeerMDMs()
	if err != nil {
		return nil, err
	}
	return mdms, nil
}

func (s *service) getProtectionDomain(systemID string, pdName string) (string, error) {
	pdID, err := s.getProtectionDomainIDFromName(systemID, pdName)
	if err != nil {
		return "", err
	}

	if pdID != "" {
		return pdID, nil
	}

	system, err := s.adminClients[systemID].FindSystem(systemID, "", "")
	if err != nil {
		return "", err
	}

	pd, err := system.GetProtectionDomain("")
	if err != nil {
		return "", err
	}

	if len(pd) == 0 {
		return "", errors.New("no protection domains found")
	}

	csmlog.Infof("[getProtectionDomain] - PD not provived, using: %s, System: %s", pd[0].Name, systemID)

	pdID = pd[0].ID

	return pdID, nil
}

func (s *service) removeVolumeFromReplicationPair(systemID string, volumeID string) (*siotypes.ReplicationPair, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	repPair, err := s.findReplicationPairByVolID(systemID, volumeID)
	if err != nil {
		return nil, err
	}

	pair := goscaleio.NewReplicationPair(adminClient)
	pair.ReplicaitonPair = repPair

	resp, err := pair.RemoveReplicationPair(true)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *service) findReplicationPairByVolID(systemID, volumeID string) (*siotypes.ReplicationPair, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	// Gets a list of all replication pairs.
	pairs, err := adminClient.GetAllReplicationPairs()
	if err != nil {
		return nil, err
	}

	for _, pair := range pairs {
		if volumeID == pair.LocalVolumeID {
			return pair, nil
		}
	}

	return nil, fmt.Errorf("replication pair for volume ID: %s, not found", volumeID)
}

func (s *service) expandReplicationPair(ctx context.Context, req *csi.ControllerExpandVolumeRequest, systemID, volumeID string) error {
	csmlog.WithContext(ctx).Infof("[expandReplicationPair] starting expansion for system %s, volume %s", systemID, volumeID)
	pair, err := s.findReplicationPairByVolID(systemID, volumeID)
	if err != nil {
		return err
	}

	csmlog.WithContext(ctx).Infof("[expandReplicationPair] found replication pair: %+v", pair)
	group, err := s.getReplicationConsistencyGroupByID(systemID, pair.ReplicationConsistencyGroupID)
	if err != nil {
		return err
	}

	csmlog.WithContext(ctx).Infof("[expandReplicationPair] found replication consistency group: %+v", group)
	// Avoid getting in a expand attempt cycle.
	if group.ReplicationDirection == "RemoteToLocal" {
		csmlog.WithContext(ctx).Info("[expandReplicationPair] skipping expansion because replication direction is RemoteToLocal")
		return nil
	}

	req.VolumeId = group.RemoteMdmID + "-" + pair.RemoteVolumeID

	resp, err := s.ControllerExpandVolume(ctx, req)
	if err != nil {
		return err
	}

	csmlog.WithContext(ctx).Infof("[expandReplicationPair] remote volume expanded successfully: %+v", resp)
	csmlog.WithContext(ctx).Info("[expandReplicationPair] verifying that the remote volume expansion is complete")

	// Use cached PlatformInfo for genType so rounding is consistent with the
	// outer ControllerExpandVolume call that already populated the cache.
	// Propagate the error — a missing system entry must not silently fall back
	// to Gen1 granularity and produce incorrect sizing on a Gen2/EC array.
	//
	// ASSUMPTION: This polling loop assumes the local and remote arrays have the
	// same genType (same granularity). The remote volume is expanded via the mutated
	// req (line 2393), but we poll the LOCAL volume (volumeID/systemID) and compare
	// its size against requestedSize computed from the LOCAL system's genType.
	// If the local array is Gen1 ("") but the remote is Gen2/EC ("EC"), the poll
	// will time out waiting for a local volume size that matches the remote's 1 GiB
	// rounding. Mixed Gen1/Gen2 replication topologies are not currently supported.
	// TODO: Add explicit genType comparison between local and remote systems, or
	// document this constraint in the replication feature specification.
	expandPlatformInfo, err := s.GetPlatformInfo(systemID)
	if err != nil {
		return err
	}
	// Per validateVolSize contract: CALLERS MUST validate genType with isKnownGenType.
	// If a future PFMP version returns a new genType, we must abort rather than
	// silently applying Gen1 granularity to the polling-loop comparison.
	if !isKnownGenType(expandPlatformInfo.GenType) {
		csmlog.WithContext(ctx).Warnf("expandReplicationPair: unrecognized genType %q for system %s; aborting.", expandPlatformInfo.GenType, systemID)
		return fmt.Errorf("unrecognised array generation type %q for system %s", expandPlatformInfo.GenType, systemID)
	}
	requestedSize, err := validateVolSize(req.CapacityRange, expandPlatformInfo.GenType)
	if err != nil {
		return err
	}

	vol, _ := s.getVolByID(volumeID, systemID)

	attempts := 0
	maxVolRetrievalRetries := 100

	for int64(vol.SizeInKb) != requestedSize && attempts < maxVolRetrievalRetries {
		time.Sleep(3 * time.Millisecond)
		vol, _ = s.getVolByID(volumeID, systemID)
		attempts++
	}

	return nil
}

func (s *service) getNASServerIDFromName(systemID, nasName string) (string, error) {
	if nasName == "" {
		csmlog.Infof("NAS server not provided.")
		return "", errors.New("NAS server not provided")
	}
	system, err := s.adminClients[systemID].FindSystem(systemID, "", "")
	if err != nil {
		return "", err
	}
	nas, err := system.GetNASByIDName("", nasName)
	if err != nil {
		return "", err
	}
	return nas.ID, nil
}

func (s *service) GetNfsTopology(systemID string) []*csi.Topology {
	nfsTopology := new(csi.Topology)
	nfsTopology.Segments = map[string]string{Name + "/" + systemID + "-nfs": "true"}
	return []*csi.Topology{nfsTopology}
}

func (s *service) GetNodeLabels(ctx context.Context) (map[string]string, error) {
	if K8sClientset == nil {
		err := k8sutils.CreateKubeClientSet()
		if err != nil {
			return nil, status.Error(codes.Internal, GetMessage("init client failed with error: %v", err))
		}
		K8sClientset = k8sutils.Clientset
	}

	nodeName := s.opts.KubeNodeName
	if nodeName == "" {
		csmlog.WithContext(ctx).Infof("Using env variable for node name")
		nodeName = os.Getenv("NODENAME")
	}

	csmlog.WithContext(ctx).Infof("Using: %s as nodeName", nodeName)

	// access the API to fetch node object
	node, err := K8sClientset.CoreV1().Nodes().Get(context.TODO(), nodeName, v1.GetOptions{})
	if err != nil {
		return nil, status.Error(codes.Internal, GetMessage("Unable to fetch the node labels. Error: %v", err))
	}
	csmlog.WithContext(ctx).Debugf("Node labels: %v\n", node.Labels)
	return node.Labels, nil
}

// GetNodeIPByCSINodeID returns cluster IP of the node corresponding to the given CSI nodeID
func (s *service) GetNodeIPByCSINodeID(nodeID string) string {
	// 1. List CSINodes
	csiNodes, err := K8sClientset.StorageV1().CSINodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		csmlog.Errorf("Error listing CSINodes: %v", err)
		return ""
	}

	var kubeNodeName string
	for _, csiNode := range csiNodes.Items {
		for _, driver := range csiNode.Spec.Drivers {
			if driver.Name == Name && driver.NodeID == nodeID {
				kubeNodeName = csiNode.Name
				break
			}
		}
		if kubeNodeName != "" {
			break
		}
	}

	if kubeNodeName == "" {
		csmlog.Warnf("No Kubernetes node found for CSI nodeID: %s", nodeID)
		return ""
	}

	// 2. Get Node object
	node, err := s.getNode(context.TODO(), kubeNodeName)
	if err != nil {
		return ""
	}

	// 3. Extract InternalIP
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address
		}
	}
	return ""
}

// QueryArrayStatus make API call to the specified url to retrieve connection status
func (s *service) QueryArrayStatus(ctx context.Context, url string) (bool, error) {
	defer func() {
		if err := recover(); err != nil {
			csmlog.WithContext(ctx).Debugf("Panic occurred while querying array status: %v", err)
		}
	}()
	client := http.Client{
		Timeout: Timeout,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		csmlog.WithContext(ctx).Errorf("Failed to create array status request for %s: %v", url, err)
		return false, err
	}
	if PodmonAPIToken != "" {
		req.Header.Set("Authorization", "Bearer "+PodmonAPIToken)
	}
	resp, err := client.Do(req)

	csmlog.WithContext(ctx).Debugf("Received response %+v for URL %s", resp, url)
	if err != nil {
		csmlog.WithContext(ctx).Errorf("Failed to call array status API %s: %v", url, err)
		return false, err
	}
	defer resp.Body.Close() // #nosec G307
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		csmlog.WithContext(ctx).Errorf("Failed to read array status API response: %v", err)
		return false, err
	}
	if resp.StatusCode != 200 {
		csmlog.WithContext(ctx).Errorf("Received unexpected status code while fetching array status: %d", resp.StatusCode)
		return false, fmt.Errorf("unexpected response from the server")
	}
	var statusResponse ArrayConnectivityStatus
	err = json.Unmarshal(bodyBytes, &statusResponse)
	if err != nil {
		csmlog.WithContext(ctx).Errorf("Failed to parse array status response: %v", err)
		return false, err
	}
	csmlog.WithContext(ctx).Infof("Array status response: %+v", statusResponse)
	// responseObject has last success and last attempt timestamp in Unix format
	timeDiff := statusResponse.LastAttempt - statusResponse.LastSuccess
	tolerance := SetPollingFrequency(ctx)
	currTime := time.Now().Unix()
	// checking if the status response is stale and connectivity test is still running
	// since nodeProbe is run at frequency tolerance/2, ideally below check should never be true
	if (currTime - statusResponse.LastAttempt) > tolerance*2 {
		csmlog.WithContext(ctx).Errorf("Connectivity test appears stale; current time is %d and the last attempt was at %d", currTime, statusResponse.LastAttempt)
		// considering connectivity is broken
		return false, nil
	}
	csmlog.WithContext(ctx).Debugf("Last connectivity check was %d seconds ago; tolerance is %d seconds", timeDiff, tolerance)
	// give 2s leeway for tolerance check
	if timeDiff <= tolerance+2 {
		return true, nil
	}
	return false, nil
}

func (s *service) SetPodZoneLabel(ctx context.Context, zoneLabel map[string]string) error {
	if K8sClientset == nil {
		err := k8sutils.CreateKubeClientSet()
		if err != nil {
			return status.Error(codes.Internal, GetMessage("init client failed with error: %v", err))
		}
		K8sClientset = k8sutils.Clientset
	}

	// access the API to fetch node object
	pods, err := K8sClientset.CoreV1().Pods(DriverNamespace).List(ctx, v1.ListOptions{})
	if err != nil {
		return status.Error(codes.Internal, GetMessage("Unable to fetch the node labels. Error: %v", err))
	}

	podName := ""
	for _, pod := range pods.Items {
		if pod.Spec.NodeName == s.opts.KubeNodeName && pod.Labels["app"] != "" {
			// only add labels to node pods. Controller pod is not restricted to a zone
			if strings.Contains(pod.Labels["app"], "node") {
				podName = pod.Name
			}
		}
	}

	if podName == "" {
		return status.Errorf(codes.NotFound, "no node pod found for node %s in namespace %s", s.opts.KubeNodeName, DriverNamespace)
	}

	pod, err := K8sClientset.CoreV1().Pods(DriverNamespace).Get(ctx, podName, v1.GetOptions{})
	if err != nil {
		return status.Error(codes.Internal, GetMessage("Unable to fetch the node labels. Error: %v", err))
	}

	for key, value := range zoneLabel {
		csmlog.WithContext(ctx).Infof("Setting label %s=%s on pod %s", key, value, podName)
		pod.Labels[key] = value
	}

	_, err = K8sClientset.CoreV1().Pods(DriverNamespace).Update(ctx, pod, v1.UpdateOptions{})
	if err != nil {
		return status.Error(codes.Internal, GetMessage("Unable to update the node labels. Error: %v", err))
	}

	return nil
}

// getNode returns node corresponding to the s.opts.KubeNodeName
func (s *service) getNode(ctx context.Context, nodeName string) (*corev1.Node, error) {
	if nodeName == "" {
		return nil, status.Error(codes.InvalidArgument, "node name is empty")
	}
	node, err := K8sClientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, status.Error(codes.Internal, GetMessage("unable to fetch node %q: %v", nodeName, err))
	}
	return node, nil
}

// GetNodeIP returns cluster IP of the node corresponding to the s.opts.KubeNodeName
func (s *service) GetNodeIP(ctx context.Context) (string, error) {
	node, err := s.getNode(ctx, s.opts.KubeNodeName)
	if err != nil {
		return "", err
	}

	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address, nil
		}
	}

	return "", fmt.Errorf("no InternalIP found for node %q", s.opts.KubeNodeName)
}

func (s *service) GetNodeUID(ctx context.Context) (string, error) {
	node, err := s.getNode(ctx, s.opts.KubeNodeName)
	if err != nil {
		return "", err
	}

	return string(node.UID), nil
}

func hashNodeID(input string) string {
	hash := sha256.Sum256([]byte(input))
	encoded := hex.EncodeToString(hash[:])
	return encoded
}

// GetMessage - Get message
func GetMessage(format string, args ...interface{}) string {
	str := fmt.Sprintf(format, args...)
	return str
}

// ParseInt64FromContext parses an environment variable into an int64 value.
func ParseInt64FromContext(ctx context.Context, key string) (int64, error) {
	if val, ok := LookupEnv(ctx, key); ok {
		i, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid int64 value '%v' specified for '%s'", val, key)
		}
		return i, nil
	}
	return 0, nil
}

func lookupEnv(ctx context.Context, key string) (string, bool) {
	return csictx.LookupEnv(ctx, key)
}

func getZoneKeyLabelFromSecret(arrays map[string]*ArrayConnectionData) (string, error) {
	zoneKeyLabel := ""

	for _, array := range arrays {
		// Check Zones[] slice first (multi-zone support)
		for _, z := range array.Zones {
			if z.LabelKey == "" {
				csmlog.Warnf("array %s zone %s has no labelKey defined; node zone labels will use the default key", array.SystemID, z.Name)
				continue
			}
			if zoneKeyLabel == "" {
				zoneKeyLabel = z.LabelKey
			} else if zoneKeyLabel != z.LabelKey {
				csmlog.Warnf("array %s zone %s key %s does not match %s", array.SystemID, z.Name, z.LabelKey, zoneKeyLabel)
				return "", fmt.Errorf("array %s zone %s key %s does not match %s", array.SystemID, z.Name, z.LabelKey, zoneKeyLabel)
			}
		}
	}

	return zoneKeyLabel, nil
}

// isNodeMode returns true if the mode property of service s is set to "node", false otherwise.
func (s *service) isNodeMode() bool {
	return strings.EqualFold(s.mode, "node")
}

// isControllerMode returns true if the mode property of service s is set to "controller", false otherwise.
func (s *service) isControllerMode() bool {
	return strings.EqualFold(s.mode, "controller")
}

// configuredZoneNames returns a list of zone names configured for this array.
func (array *ArrayConnectionData) configuredZoneNames() []string {
	if len(array.Zones) == 0 {
		return nil
	}
	names := make([]string, len(array.Zones))
	for i, z := range array.Zones {
		names[i] = string(z.Name)
	}
	return names
}

// getZoneConfigSummary returns a human-readable summary of all zone-to-PD
// mappings across arrays, suitable for startup diagnostic logging.
// Credential fields (username, password, token) are never included.
func getZoneConfigSummary(arrays map[string]*ArrayConnectionData) string {
	hasAnyZone := false
	for _, arr := range arrays {
		if len(arr.Zones) > 0 {
			hasAnyZone = true
			break
		}
	}
	if !hasAnyZone {
		return "zone config summary: no zone configuration detected"
	}

	var sb strings.Builder
	sb.WriteString("zone config summary:")

	// Sort system IDs for deterministic output
	systemIDs := make([]string, 0, len(arrays))
	for sysID := range arrays {
		systemIDs = append(systemIDs, sysID)
	}
	sort.Strings(systemIDs)

	for _, sysID := range systemIDs {
		arr := arrays[sysID]
		if len(arr.Zones) == 0 {
			continue
		}
		for _, z := range arr.Zones {
			for _, pd := range z.ProtectionDomains {
				pools := make([]string, len(pd.Pools))
				for i, p := range pd.Pools {
					pools[i] = string(p)
				}
				sb.WriteString(fmt.Sprintf(" [system=%s zone=%s PD=%s pools=[%s]]",
					sysID, z.Name, pd.Name, strings.Join(pools, ",")))
			}
		}
	}

	return sb.String()
}

// formatNodeZoneAssociation returns a log message describing a node's zone
// association for diagnostic logging during NodeGetInfo.
func formatNodeZoneAssociation(nodeID, zoneName, labelKey string) string {
	return fmt.Sprintf("node %s associated with zone %s (label=%s)", nodeID, zoneName, labelKey)
}

// hasZoneConfig returns true if the array has any zone configuration.
func (array *ArrayConnectionData) hasZoneConfig() bool {
	return len(array.Zones) > 0
}

// isInZone returns true if the array is configured for use in the provided zoneName, false otherwise.
func (array *ArrayConnectionData) isInZone(zoneName string) bool {
	for _, z := range array.Zones {
		if z.Name == ZoneName(zoneName) {
			return true
		}
	}
	return false
}

func (s *service) initConnectors() {
	if s.nvmeConnector == nil {
		nvmeConnectorParams := gobrick.NVMeConnectorParams{
			Chroot: s.opts.NodeChrootPath,
		}
		s.nvmeConnector = gobrick.NewNVMeConnector(nvmeConnectorParams)
	}

	if s.nvmeLib == nil {
		nvmeOptions := map[string]string{
			"chrootDirectory": s.opts.NodeChrootPath,
		}
		s.nvmeLib = gonvme.NewNVMe(nvmeOptions)
	}
}

func (s *service) getInitiators() ([]string, error) {
	ctx := context.Background()

	var nvmeAvailable bool

	nvmeInitiators, err := s.nvmeConnector.GetInitiatorName(ctx)
	if err != nil {
		csmlog.Error("nodeStartup could not get Initiator NQNs")
	} else if len(nvmeInitiators) == 0 {
		csmlog.Error("NVMe initiators not found on node")
	} else {
		csmlog.Debug("NVMe initiators found on node")
		nvmeAvailable = true
	}

	if !nvmeAvailable {
		// If we haven't found any initiators we still can use NFS
		csmlog.Info("NVMe initiators not found on node")
	}

	return nvmeInitiators, nil
}

func (s *service) GetPlatformInfo(systemID string) (*PlatformInfo, error) {
	// CG-RC-001: use a per-systemID mutex so that the first concurrent write to
	// s.platformInfos is serialised without blocking unrelated systemIDs.
	lock := s.getProbeLock(systemID)
	lock.Lock()
	defer lock.Unlock()

	platformInfo, ok := s.platformInfos[systemID]
	if !ok {
		csmlog.Debugf("Start: Retrieving Platform Info from Array using SystemId: %s", systemID)

		platformInfo = &PlatformInfo{
			SystemID: systemID,
		}

		version, err := s.GetPlatformVersion(systemID)
		if err != nil {
			return nil, err
		}

		platformInfo.ArrayVersion = version

		genType, err := s.GetGenType(systemID)
		if err != nil {
			return nil, err
		}

		platformInfo.GenType = genType

		s.platformInfos[systemID] = platformInfo

		// FR-6: INFO-level structured log on fresh detection so operators can
		// confirm which granularity boundary the driver will apply.
		granularity := "8 GiB (Gen1)"
		if genType == "EC" {
			granularity = "1 GiB (Gen2/EC)"
		}
		csmlog.Infof("Array generation detected: systemID=%s version=%v genType=%q granularity=%s",
			systemID, version, genType, granularity)

		csmlog.Debugf("End: Retrieved Platform Info from Array using SystemId: %s Version: %v GenType: %s", systemID, version, genType)
	}

	return platformInfo, nil
}

func (s *service) GetGenType(systemID string) (string, error) {
	system := s.systems[systemID]
	if system == nil {
		return "", fmt.Errorf("GetGenType: system %q not found in systems map", systemID)
	}

	// Query all ProtectionDomains for this system and return genType of first one
	pds, err := system.GetProtectionDomain("")
	if err != nil {
		return "", err
	}

	if len(pds) > 0 {
		return pds[0].GenType, nil
	}

	return "", fmt.Errorf("GetGenType: system %q has no protection domains — cannot determine generation type", systemID)
}

func (s *service) GetPlatformVersion(systemID string) (float64, error) {
	c := s.adminClients[systemID]
	if c == nil {
		return 0, nil
	}

	version, err := c.GetVersion()
	if err != nil {
		return 0, err
	}

	ver, err := strconv.ParseFloat(version, 64)
	if err != nil {
		return 0, err
	}

	return ver, nil
}

func (s *service) getArrayVersion(ctx context.Context, systemID string) (float64, error) {
	if err := s.systemProbeAll(ctx); err != nil {
		return 0, err
	}

	c := s.adminClients[systemID]
	if c == nil {
		return 0, fmt.Errorf("unable to get admin client of the array: %s", systemID)
	}

	platformInfo, err := s.GetPlatformInfo(systemID)
	if err != nil {
		return 0, err
	}

	return platformInfo.ArrayVersion, nil
}

func (s *service) getHostIDAndType(systemID, nodeID string) (string, string, error) {
	hostID := ""
	hostType := ""

	sdcID, err := s.getSDCID(nodeID, systemID)
	if err != nil {
		csmlog.Infof("No SDC host found for nodeID %s: %v", nodeID, err)
	}
	if sdcID != "" {
		csmlog.Infof("SDC Host with ID %s found for nodeID %s", sdcID, nodeID)
		hostID = sdcID
		hostType = SDC
	} else {
		nvmeHost, err := s.systems[systemID].FindSdc("Name", nodeID)
		if err != nil {
			csmlog.Infof("No NVME host found for nodeID %s: %v", nodeID, err)
		}
		if nvmeHost != nil {
			csmlog.Infof("NVME Host with ID %s found for nodeID %s", nvmeHost.Sdc.ID, nodeID)
			hostID = nvmeHost.Sdc.ID
			hostType = NVMeTCP
		} else {
			return hostID, hostType, err
		}
	}
	return hostID, hostType, nil
}

// startMetricsServer starts the shared HTTP metrics server and stores it in s.metricsServer.
// It is called on every controller regardless of leader election status so that each
// controller pod always exposes a /metrics endpoint. When MetricsTLSCertFile and
// MetricsTLSKeyFile are both configured, the endpoint is served over HTTPS.
func (s *service) startMetricsServer() {
	csmlog.Infof("Starting metrics server on port %s", s.opts.MetricsPort)
	srv := svcmetrics.NewSharedMetricsServer()

	var startErr error
	if s.opts.MetricsTLSCertFile != "" && s.opts.MetricsTLSKeyFile != "" {
		csmlog.Infof("Metrics server TLS enabled (cert: %s, key: %s)", s.opts.MetricsTLSCertFile, s.opts.MetricsTLSKeyFile)
		startErr = srv.StartTLS(s.opts.MetricsPort, s.opts.MetricsTLSCertFile, s.opts.MetricsTLSKeyFile)
	} else {
		startErr = srv.Start(s.opts.MetricsPort)
	}

	if startErr != nil {
		csmlog.Errorf("failed to start metrics server: %v", startErr)
		return
	}
	s.metricsServer = srv
	csmlog.Infof("Metrics server started; endpoint available at %s/metrics", srv.GetAddr())
}

// startGatewayMonitor starts the gateway polling loop using the provided context.
// It assumes s.metricsServer is already running and registers metrics into the
// server's shared Prometheus registry.  It is called only on the leader.
func (s *service) startGatewayMonitor(ctx context.Context) {
	if s.metricsServer == nil {
		csmlog.WithContext(ctx).Error("Cannot start gateway monitor because the metrics server is not running")
		return
	}

	// Build an entries map from existing adminClients (already authenticated).
	gwEntries := make(map[string]svcmetrics.GatewayEntry, len(s.adminClients))
	for id, c := range s.adminClients {
		// ip is used solely as a Prometheus label on gateway metrics; the
		// actual gateway communication uses the already-authenticated client.
		// If host extraction fails we fall back to the raw endpoint so the
		// metric label still identifies the gateway, and continue monitoring.
		ip := ""
		if arr, ok := s.opts.arrays[id]; ok {
			if host, err := ExtractHost(arr.Endpoint); err == nil {
				ip = host
			} else {
				csmlog.WithContext(ctx).Warnf("Could not extract gateway address for system %s from endpoint %s: %v; using the endpoint as the metric label", id, arr.Endpoint, err)
				ip = arr.Endpoint
			}
		}
		gwEntries[id] = svcmetrics.GatewayEntry{Client: c, IP: ip}
	}

	pollInterval := s.opts.GatewayMonitoringInterval
	if pollInterval == 0 {
		pollInterval = 30 * time.Second
	}

	gm := svcmetrics.NewGatewayMonitor(gwEntries, svcmetrics.Config{
		PollInterval: pollInterval,
	})
	if gmErr := gm.Start(ctx); gmErr != nil {
		csmlog.WithContext(ctx).Errorf("Failed to start gateway monitor: %v", gmErr)
		return
	}
	s.gatewayMonitor = gm
	csmlog.WithContext(ctx).Infof("Gateway monitoring started; metrics are available at %s/metrics", s.metricsServer.GetAddr())
}

// startGatewayMonitoring starts both the metrics server and the gateway monitor.
// It is called directly when gateway monitoring leader election is disabled (i.e.
// a single controller is responsible for both serving metrics and polling gateways).
func (s *service) startGatewayMonitoring(ctx context.Context) {
	s.startMetricsServer()
	if s.metricsServer == nil {
		// startMetricsServer already logged the error.
		return
	}
	s.startGatewayMonitor(ctx)
}

// startGatewayMonitoringWithLeaderElection participates in leader election for the gateway
// monitoring lease. The metrics server is already running on every controller; only the
// controller that holds the lease will run the gateway polling loop. The parent ctx is
// used to stop the leader election loop when the driver itself is shutting down.
func (s *service) startGatewayMonitoringWithLeaderElection(ctx context.Context) {
	if K8sClientset == nil {
		if err := k8sutils.CreateKubeClientSet(KubeConfig); err != nil {
			csmlog.WithContext(ctx).Errorf("Gateway monitoring leader election failed to create the Kubernetes clientset: %v", err)
			return
		}
		K8sClientset = k8sutils.Clientset
	}

	lockName := "gateway-monitor-" + strings.ReplaceAll(Name, ".", "-")

	runFunc := func(leCtx context.Context) {
		// Merge the leader-election context with the parent driver context so
		// that losing the lease OR the driver shutting down both stop monitoring.
		monCtx, cancel := context.WithCancel(leCtx)
		go func() {
			select {
			case <-ctx.Done():
				cancel()
			case <-leCtx.Done():
				cancel()
			}
		}()
		defer cancel()

		csmlog.WithContext(ctx).Infof("Gateway monitoring acquired leader election lease %q", lockName)
		s.startGatewayMonitor(monCtx)

		// Block until the merged context is cancelled so the lease is held while
		// monitoring is running. When cancelled the gateway monitor's own context
		// will also be cancelled, stopping all polling goroutines.
		<-monCtx.Done()
		csmlog.WithContext(ctx).Infof("Gateway monitoring released leader election lease %q; stopping the gateway monitor", lockName)
		// Stop only the gateway monitor; the metrics server keeps running on this
		// controller so that it continues to serve (now-empty) metrics until it
		// either re-acquires the lease or the pod is terminated.
		if s.gatewayMonitor != nil {
			s.gatewayMonitor.Stop(context.Background()) //nolint:errcheck
			s.gatewayMonitor = nil
		}
	}

	if err := k8sutils.LeaderElectionFunc(&K8sClientset, lockName, DriverNamespace, runFunc); err != nil {
		csmlog.WithContext(ctx).Errorf("Gateway monitoring leader election failed: %v", err)
	}
}

// OperationInterceptor returns the gRPC unary interceptor that records CSI operation
// metrics. It is nil until startCollectors has been called. Intended for use by the
// provider when gocsi supports injecting server options at startup.
func (s *service) OperationInterceptor() grpc.UnaryServerInterceptor {
	return s.operationInterceptor
}

// GetOperationInterceptor returns the global operation interceptor for use by the provider
func GetOperationInterceptor() grpc.UnaryServerInterceptor {
	return globalOperationInterceptor
}

// startCollectors initializes and starts all PowerFlex collectors.
func (s *service) startCollectors(ctx context.Context) {
	csmlog.WithContext(ctx).Infof("startCollectors: starting collector initialization")
	if s.metricsServer == nil {
		csmlog.WithContext(ctx).Warn("startCollectors: metrics server not available, skipping collectors")
		return
	}

	reg := s.metricsServer.GetRegistry()
	baseManager := collectors.NewCollectorManager()
	collectorInterval := metricsCollectionInterval()
	runtimeConfig := metricsRuntimeConfig()

	// Register the cross-array stale indicator metric.
	s.metricsStale = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_metrics_stale",
		Help: "Set to 1 when an array's metrics may be stale.",
	}, []string{"system_id"})
	if err := reg.Register(s.metricsStale); err != nil {
		csmlog.WithContext(ctx).Warnf("startCollectors: could not register dell_powerflex_metrics_stale: %v", err)
	}

	// Register ER-K8S-BR64714-001-powerflex-gen2-1gb-granularity granularity metrics (FR-7).
	if s.granularityMetrics == nil {
		if promReg, ok := reg.(*prometheus.Registry); ok {
			gm, err := svcmetrics.NewGranularityMetrics(promReg)
			if err != nil {
				csmlog.WithContext(ctx).Warnf("startCollectors: failed to register granularity metrics: %v", err)
			} else {
				s.granularityMetrics = gm
			}
		} else {
			csmlog.WithContext(ctx).Warn("startCollectors: metrics registry is not a *prometheus.Registry, skipping granularity metrics")
		}
	}

	// Build a single operation interceptor shared across all arrays.
	// startCollectors is invoked from BeforeServe, which runs before the gRPC
	// server begins handling requests, so globalOperationInterceptor will be
	// set before any RPC is processed. The provider wrapper also handles a nil
	// interceptor defensively in case the ordering changes in the future.
	// The global_id label identifies the driver, while the legacy system_id
	// series remains stable for backward-compatible dashboards.
	legacySystemID := s.opts.defaultSystemID
	if legacySystemID == "" {
		legacySystemID = "unknown"
	}
	driverID := Name
	if driverID == "" {
		driverID = "unknown"
	}
	s.operationInterceptor = NewOperationInterceptor(reg, legacySystemID, driverID)
	globalOperationInterceptor = s.operationInterceptor
	csmlog.WithContext(ctx).Infof("startCollectors: gRPC operation interceptor metrics registered with system_id=%s and global_id=%s", legacySystemID, driverID)

	type arrayCollectorConfig struct {
		systemID           string
		client             *sio.Client
		managementEndpoint string
		metadataClient     kubernetes.Interface
		volumeMetadata     collectors.VolumeMetadataProvider
		metricsRuntime     *collectors.MetricsRuntime
	}

	arrayConfigs := make([]arrayCollectorConfig, 0, len(s.adminClients))

	csmlog.WithContext(ctx).Infof("startCollectors: starting collector initialization loop, adminClients count: %d", len(s.adminClients))
	for sid, client := range s.adminClients {
		managementEndpoint := "unknown"
		if arrayCfg, ok := s.opts.arrays[sid]; ok && arrayCfg != nil && arrayCfg.Endpoint != "" {
			managementEndpoint = arrayCfg.Endpoint
		}

		csmlog.WithContext(ctx).Infof("startCollectors[%s]: initializing metadata client", sid)
		metadataClient := K8sClientset
		if metadataClient == nil && k8sutils.Clientset != nil {
			metadataClient = k8sutils.Clientset
		}
		if metadataClient == nil {
			if err := k8sutils.CreateKubeClientSet(KubeConfig); err == nil {
				metadataClient = k8sutils.Clientset
				K8sClientset = metadataClient
				csmlog.WithContext(ctx).Infof("startCollectors[%s]: initialized Kubernetes client for K8sMetadataChecker", sid)
			} else {
				csmlog.WithContext(ctx).Warnf("startCollectors[%s]: failed to initialize Kubernetes client for K8sMetadataChecker: %v", sid, err)
			}
		}
		volumeMetadata := collectors.NewK8sMetadataChecker(metadataClient, Name)
		if volumeMetadata.Available() {
			csmlog.WithContext(ctx).Infof("startCollectors[%s]: K8sMetadataChecker available for PV validation", sid)
		} else {
			csmlog.WithContext(ctx).Warnf("startCollectors[%s]: K8sMetadataChecker not available, PV validation disabled", sid)
		}

		runtimeCfg := runtimeConfig
		runtimeCfg.StaleReporter = func(id string, stale bool) {
			if stale {
				s.metricsStale.WithLabelValues(id).Set(1)
				return
			}
			s.metricsStale.WithLabelValues(id).Set(0)
		}
		metricsRuntime := collectors.NewMetricsRuntimeWithConfig(sid, runtimeCfg)
		baseHealth, err := collectors.NewDriverHealthCollector(reg, sid)
		if err != nil {
			csmlog.WithContext(ctx).Warnf("startCollectors[%s]: DriverHealthCollector register error: %v", sid, err)
		} else {
			baseManager.Register(baseHealth)
		}

		arrayConfigs = append(arrayConfigs, arrayCollectorConfig{
			systemID:           sid,
			client:             client,
			managementEndpoint: managementEndpoint,
			metadataClient:     metadataClient,
			volumeMetadata:     volumeMetadata,
			metricsRuntime:     metricsRuntime,
		})
	}

	if len(baseManager.Collectors()) > 0 {
		s.collectorManager = baseManager
		s.collectorManager.Start(ctx, collectorInterval)
		go func() {
			<-ctx.Done()
			if s.collectorManager != nil {
				s.collectorManager.Stop()
			}
		}()
	} else {
		csmlog.WithContext(ctx).Warn("startCollectors: no driver health collectors could be registered; metrics will be incomplete")
	}

	startArrayCollectors := func(arrayCtx context.Context) {
		arrayManager := collectors.NewCollectorManager()
		for _, cfg := range arrayConfigs {
			apiObserver, err := collectors.NewPowerFlexAPIObserver(reg, cfg.systemID)
			if err != nil {
				csmlog.WithContext(arrayCtx).Warnf("startCollectors[%s]: PowerFlexAPIObserver create error: %v", cfg.systemID, err)
			} else {
				cfg.client.SetRequestObserver(apiObserver)
			}

			metricsClient := collectors.NewPowerFlexMetricsClient(cfg.client, cfg.systemID)

			sp, err := collectors.NewStoragePoolCollector(metricsClient, reg, cfg.systemID)
			if err != nil {
				csmlog.WithContext(arrayCtx).Warnf("startCollectors[%s]: StoragePoolCollector register error: %v", cfg.systemID, err)
			} else {
				sp.SetRuntime(cfg.metricsRuntime)
				arrayManager.Register(sp)
			}

			rcg, err := collectors.NewRCGCollector(metricsClient, reg, cfg.systemID)
			if err != nil {
				csmlog.WithContext(arrayCtx).Warnf("startCollectors[%s]: RCGCollector register error: %v", cfg.systemID, err)
			} else {
				rcg.SetRuntime(cfg.metricsRuntime)
				arrayManager.Register(rcg)
			}

			pfVolumeClient := collectors.NewPowerFlexVolumeClient(cfg.client, cfg.metadataClient, cfg.volumeMetadata, cfg.systemID)
			vol, err := collectors.NewVolumeCollector(pfVolumeClient, reg, cfg.systemID)
			if err != nil {
				csmlog.WithContext(arrayCtx).Warnf("startCollectors[%s]: VolumeCollector register error: %v", cfg.systemID, err)
			} else {
				vol.SetRuntime(cfg.metricsRuntime)
				arrayManager.Register(vol)
			}

			arrayHealth, err := collectors.NewArrayHealthCollector(collectors.NewPowerFlexArrayHealthClient(cfg.client), reg, cfg.systemID, cfg.managementEndpoint)
			if err != nil {
				csmlog.WithContext(arrayCtx).Warnf("startCollectors[%s]: ArrayHealthCollector register error: %v", cfg.systemID, err)
			} else {
				arrayHealth.SetRuntime(cfg.metricsRuntime)
				arrayManager.Register(arrayHealth)
			}
		}

		if len(arrayManager.Collectors()) == 0 {
			csmlog.WithContext(arrayCtx).Warn("startCollectors: no array collectors could be registered; metrics will be incomplete")
			return
		}

		arrayManager.Start(arrayCtx, collectorInterval)
		go func() {
			<-arrayCtx.Done()
			arrayManager.Stop()
		}()
		csmlog.WithContext(arrayCtx).Infof("startCollectors: started %d array collectors across %d arrays", len(arrayManager.Collectors()), len(arrayConfigs))
	}

	if strings.EqualFold(s.mode, "controller") {
		if metricsLeaderElectionEnabled() {
			if k8sutils.Kubeclient != nil && k8sutils.Kubeclient.Clientset != nil {
				go func() {
					if err := k8sutils.LeaderElectionForMetrics(ctx, k8sutils.Kubeclient.Clientset, "powerflex-metrics", metricsLeaderElectionNamespace(), metricsLeaderElectionRenewDeadline(), metricsLeaderElectionLeaseDuration(), metricsLeaderElectionRetryPeriod(), startArrayCollectors); err != nil && ctx.Err() == nil {
						csmlog.WithContext(ctx).Errorf("metrics leader election failed, falling back to local array metrics collection: %v", err)
						startArrayCollectors(ctx)
					}
				}()
			} else {
				csmlog.WithContext(ctx).Warn("metrics leader election enabled but Kubernetes client is unavailable; starting array collectors locally")
				startArrayCollectors(ctx)
			}
		} else {
			startArrayCollectors(ctx)
		}
	} else {
		csmlog.WithContext(ctx).Infof("startCollectors: skipping array collectors in node mode")
	}

	csmlog.WithContext(ctx).Infof("startCollectors: started %d driver health collectors across %d arrays", len(baseManager.Collectors()), len(s.adminClients))
}

func metricsLeaderElectionEnabled() bool {
	if raw, ok := csictx.LookupEnv(context.Background(), EnvMetricsLeaderElectionEnabled); ok {
		return strings.EqualFold(raw, "true")
	}
	return false
}

func metricsLeaderElectionNamespace() string {
	if ns, ok := csictx.LookupEnv(context.Background(), EnvDriverNamespace); ok && ns != "" {
		return ns
	}
	return DriverNamespace
}

func metricsLeaderElectionLeaseDuration() time.Duration {
	return durationFromEnvOrDefault(EnvMetricsLeaderElectionLeaseDuration, 60*time.Second)
}

func metricsLeaderElectionRenewDeadline() time.Duration {
	return durationFromEnvOrDefault(EnvMetricsLeaderElectionRenewDeadline, 40*time.Second)
}

func metricsLeaderElectionRetryPeriod() time.Duration {
	return durationFromEnvOrDefault(EnvMetricsLeaderElectionRetryPeriod, 5*time.Second)
}

func durationFromEnvOrDefault(env string, defaultValue time.Duration) time.Duration {
	if raw, ok := csictx.LookupEnv(context.Background(), env); ok {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
		csmlog.Warnf("invalid value %q for %s, defaulting to %s", raw, env, defaultValue)
	}
	return defaultValue
}

func intFromEnvOrDefault(env string, defaultValue int) int {
	if raw, ok := csictx.LookupEnv(context.Background(), env); ok {
		if v, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && v > 0 {
			return v
		}
		csmlog.Warnf("invalid value %q for %s, defaulting to %d", raw, env, defaultValue)
	}
	return defaultValue
}

func metricsCollectionInterval() time.Duration {
	return durationFromEnvOrDefault(EnvMetricsCollectionInterval, 30*time.Second)
}

func metricsRuntimeConfig() collectors.RuntimeConfig {
	return collectors.RuntimeConfig{
		Timeout:        durationFromEnvOrDefault(EnvMetricsArrayTimeout, 30*time.Second),
		CacheTTL:       durationFromEnvOrDefault(EnvMetricsCollectionCacheTTL, 25*time.Second),
		RateLimit:      intFromEnvOrDefault(EnvMetricsArrayRateLimit, 100),
		CBThreshold:    intFromEnvOrDefault(EnvMetricsArrayCBThreshold, 3),
		CBResetTimeout: durationFromEnvOrDefault(EnvMetricsArrayCBResetTimeout, 30*time.Second),
	}
}
