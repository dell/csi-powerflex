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

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apparentlymart/go-cidr/cidr"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-vxflexos/v2/core"
	"github.com/dell/csi-vxflexos/v2/k8sutils"
	"github.com/dell/dell-csi-extensions/podmon"
	"github.com/dell/dell-csi-extensions/replication"
	volumeGroupSnapshot "github.com/dell/dell-csi-extensions/volumeGroupSnapshot"
	"github.com/dell/gocsi"
	csictx "github.com/dell/gocsi/context"
	"github.com/dell/goscaleio"
	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	"github.com/fsnotify/fsnotify"
	"github.com/golang/protobuf/ptypes"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Name is the name of the CSI plug-in.
	Name = "csi-vxflexos.dellemc.com"

	// KeyThickProvisioning is the key used to get a flag indicating that
	// a volume should be thick provisioned from the volume create params
	KeyThickProvisioning = "thickprovisioning"

	thinProvisioned  = "ThinProvisioned"
	thickProvisioned = "ThickProvisioned"
	defaultPrivDir   = "/dev/disk/csi-vxflexos"

	// SystemTopologySystemValue is the supported topology key
	SystemTopologySystemValue string = "csi-vxflexos.dellemc.com"

	//DefaultLogLevel for csi logs
	DefaultLogLevel = logrus.DebugLevel

	//ParamCSILogLevel csi driver log level
	ParamCSILogLevel = "CSI_LOG_LEVEL"
)

var mx = sync.Mutex{}
var px = sync.Mutex{}

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

// Log controlls the logger
// give default value, will be overwritten by configmap
var Log = logrus.New()

// ArrayConnectionData contains data required to connect to array
type ArrayConnectionData struct {
	SystemID                  string `json:"systemID"`
	Username                  string `json:"username"`
	Password                  string `json:"password"`
	Endpoint                  string `json:"endpoint"`
	SkipCertificateValidation bool   `json:"skipCertificateValidation,omitempty"`
	Insecure                  bool   `json:"insecure,omitempty"`
	IsDefault                 bool   `json:"isDefault,omitempty"`
	AllSystemNames            string `json:"allSystemNames"`
	NasName                   string `json:"nasName"`
}

// Manifest is the SP's manifest.
var Manifest = map[string]string{
	"url":    "http://github.com/dell/csi-vxflexos",
	"semver": core.SemVer,
	"commit": core.CommitSha32,
	"formed": core.CommitTime.Format(time.RFC1123),
}

// Service is the CSI Mock service provider.
type Service interface {
	csi.ControllerServer
	csi.IdentityServer
	csi.NodeServer
	BeforeServe(context.Context, *gocsi.StoragePlugin, net.Listener) error
	RegisterAdditionalServers(server *grpc.Server)
	ProcessMapSecretChange() error
}

// Opts defines service configuration options.
type Opts struct {
	// map from system name to ArrayConnectionData
	arrays                     map[string]*ArrayConnectionData
	defaultSystemID            string // ID of default system
	SdcGUID                    string
	Thick                      bool
	AutoProbe                  bool
	DisableCerts               bool   // used for unit testing only
	Lsmod                      string // used for unit testing only
	drvCfgQueryMDM             string // used for testing only
	EnableSnapshotCGDelete     bool   // when snapshot deleted, enable deleting of all snaps in the CG of the snapshot
	EnableListVolumesSnapshots bool   // when listing volumes, include snapshots and volumes
	AllowRWOMultiPodAccess     bool   // allow multiple pods to access a RWO volume on the same node
	IsHealthMonitorEnabled     bool   // allow driver to make use of the alpha feature gate, CSIVolumeHealth
	IsSdcRenameEnabled         bool   // allow driver to enable renaming SDC
	SdcPrefix                  string // prefix to be set for SDC name
	IsApproveSDCEnabled        bool
	replicationContextPrefix   string
	replicationPrefix          string
	MaxVolumesPerNode          int64
	IsQuotaEnabled             bool   // allow driver to enable quota limits for NFS volumes
	ExternalAccess             string // used for adding extra IP/IP range to the NFS export
	KubeNodeName               string
}

type service struct {
	opts                Opts
	adminClients        map[string]*sio.Client
	systems             map[string]*sio.System
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
	//maps the first 24 bits of a volume ID to the volume's systemID
	volumePrefixToSystems   map[string][]string
	connectedSystemNameToID map[string]string
}

// Process dynamic changes to configMap or Secret.
func (s *service) ProcessMapSecretChange() error {

	//Update dynamic config params
	vc := viper.New()
	vc.AutomaticEnv()
	Log.WithField("file", DriverConfigParamsFile).Info("driver configuration file ")
	vc.SetConfigFile(DriverConfigParamsFile)
	if err := vc.ReadInConfig(); err != nil {
		Log.WithError(err).Error("unable to read config file, using default values")
	}
	if err := s.updateDriverConfigParams(Log, vc); err != nil {
		return err
	}
	vc.WatchConfig()
	vc.OnConfigChange(func(e fsnotify.Event) {
		// Putting in mutex to allow tests to pass with race flag
		mx.Lock()
		defer mx.Unlock()
		Log.WithField("file", DriverConfigParamsFile).Info("log configuration file changed")
		if err := s.updateDriverConfigParams(Log, vc); err != nil {
			Log.Warn(err)
		}
	})

	// dynamic array secret change
	va := viper.New()
	va.SetConfigFile(ArrayConfigFile)
	Log.WithField("file", ArrayConfigFile).Info("array configuration file")

	va.WatchConfig()

	va.OnConfigChange(func(e fsnotify.Event) {
		// Putting in mutex to allow tests to pass with race flag
		mx.Lock()
		defer mx.Unlock()
		Log.WithField("file", ArrayConfigFile).Info("array configuration file changed")
		var err error
		s.opts.arrays, err = getArrayConfig(context.Background())
		if err != nil {
			Log.WithError(err).Error("unable to reload multi array config file")
		}
		err = s.doProbe(context.Background())
		if err != nil {
			Log.WithError(err).Error("unable to probe array in multi array config")
		}
		// log csiNode topology keys
		if err = s.logCsiNodeTopologyKeys(); err != nil {
			Log.WithError(err).Error("unable to log csiNode topology keys")
		}
	})
	return nil
}

func (s *service) logCsiNodeTopologyKeys() error {
	if K8sClientset == nil {
		err := k8sutils.CreateKubeClientSet(KubeConfig)
		if err != nil {
			Log.WithError(err).Error("unable to create k8s clientset for query")
			return err
		}
		K8sClientset = k8sutils.Clientset
	}

	csiNodes, err := K8sClientset.StorageV1().CSINodes().List(context.TODO(), metav1.ListOptions{})
	node, err := s.NodeGetInfo(context.Background(), nil)
	if node != nil {
		Log.WithField("node info", node.NodeId).Info("NodeInfo ID")
		segMap := node.AccessibleTopology.Segments

		for key := range segMap {
			Log.WithField("node info key", key).Info("NodeInfo topologykeys")
		}

		if err == nil {
			for i, csiNode := range csiNodes.Items {
				if len(csiNode.Spec.Drivers) > 0 {
					csinodeID := csiNode.Spec.Drivers[i].NodeID
					csiNodeName := csiNode.Spec.Drivers[i].Name
					if csinodeID == node.NodeId && csiNodeName == Name {
						csinodeID := csiNode.Spec.Drivers[i].NodeID
						Log.WithField("csinode", csiNode.Name).Info("csiNode name")
						Log.WithField("csinode ID", csinodeID).Info("csiNode id")
						tkeys := csiNode.Spec.Drivers[i].TopologyKeys
						if tkeys != nil {
							Log.WithField("csinode topologykeys", len(tkeys)).Info("count")
							needMap := make(map[string]string)
							for key := range segMap {
								for _, tkey := range tkeys {
									if tkey != key {
										needMap[key] = "missing"
									} else {
										Log.WithField("csinode topologykeys", "ok").Info("found")
									}
								}
							}
							for akey := range needMap {
								Log.WithField("csinode missing topology key", akey).Info("node key")
							}
						}
					}
				}
			}
		} else {
			Log.WithError(err).Error("unable to list csiNodes in cluster")
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

func (s *service) updateDriverConfigParams(logger *logrus.Logger, v *viper.Viper) error {

	logFormat := v.GetString("CSI_LOG_FORMAT")
	logFormat = strings.ToLower(logFormat)
	logger.WithField("format", logFormat).Info("Read CSI_LOG_FORMAT from log configuration file")
	if strings.EqualFold(logFormat, "json") {
		logger.SetFormatter(&logrus.JSONFormatter{})
	} else {
		// use text formatter by defualt
		if logFormat != "text" {
			logger.WithField("format", logFormat).Info("CSI_LOG_FORMAT value not recognized, setting to text")
		}
		logger.SetFormatter(&logrus.TextFormatter{})
	}

	level := DefaultLogLevel
	if v.IsSet(ParamCSILogLevel) {
		logLevel := v.GetString(ParamCSILogLevel)
		if logLevel != "" {
			logLevel = strings.ToLower(logLevel)
			logger.WithField("level", logLevel).Info("Read CSI_LOG_LEVEL from log configuration file")
			var err error
			level, err = logrus.ParseLevel(logLevel)
			if err != nil {
				Log.WithError(err).Errorf("CSI_LOG_LEVEL %s value not recognized, setting to debug error: %s ", logLevel, err.Error())
				logger.SetLevel(DefaultLogLevel)
				return fmt.Errorf("input log level %q is not valid", logLevel)
			}
		}
	}
	logger.SetLevel(level)
	// set X_CSI_LOG_LEVEL so that gocsi doesn't overwrite the loglevel set by us
	_ = os.Setenv(gocsi.EnvVarLogLevel, level.String())
	return nil
}

func (s *service) BeforeServe(
	ctx context.Context, sp *gocsi.StoragePlugin, lis net.Listener) error {
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
		}

		Log.WithFields(fields).Infof("configured %s", Name)
	}()

	// Get the SP's operating mode.
	s.mode = csictx.Getenv(ctx, gocsi.EnvVarMode)

	opts := Opts{}

	var err error

	// Process configuration file and initialize system clients
	opts.arrays, err = getArrayConfig(ctx)
	if err != nil {
		Log.Warnf("unable to get arrays from config: %s", err.Error())
		return err
	}

	if err = s.ProcessMapSecretChange(); err != nil {
		Log.Warnf("unable to configure dynamic configMap secret change detection : %s", err.Error())
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
	if approveSDC, ok := csictx.LookupEnv(ctx, EnvIsApproveSDCEnabled); ok {
		if approveSDC == "true" {
			opts.IsApproveSDCEnabled = true
		}
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
		Log.Warnf("error while parsing env variable '%s', %s, defaulting to 0", EnvMaxVolumesPerNode, err)
		opts.MaxVolumesPerNode = 0
	} else {
		opts.MaxVolumesPerNode = MaxVolumesPerNode
	}
	if externalAccess, ok := csictx.LookupEnv(ctx, EnvExternalAccess); ok {
		// Trimming spaces if any
		externalAccess = strings.TrimSpace(externalAccess)
		if externalAccess == "" {
			Log.Infof("externalAccess is not provided")
			opts.ExternalAccess = ""
		} else {
			opts.ExternalAccess, err = ParseCIDR(externalAccess)
			if err != nil {
				Log.Warnf("error while parsing the externalAccess : %s, defaulting to empty", err)
				opts.ExternalAccess = ""
			}
		}
	}
	if kubeNodeName, ok := csictx.LookupEnv(ctx, EnvKubeNodeName); ok {
		opts.KubeNodeName = kubeNodeName
	}

	// log csiNode topology keys
	if err = s.logCsiNodeTopologyKeys(); err != nil {
		Log.WithError(err).Error("unable to log csiNode topology keys")
	}

	// pb parses an environment variable into a boolean value. If an error
	// is encountered, default is set to false, and error is logged
	pb := func(n string) bool {
		if v, ok := csictx.LookupEnv(ctx, n); ok {
			b, err := strconv.ParseBool(v)
			if err != nil {
				Log.WithField(n, v).Debug(
					"invalid boolean value. defaulting to false")
				return false
			}
			return b
		}
		return false
	}

	opts.Thick = pb(EnvThick)
	opts.AutoProbe = true

	s.opts = opts
	s.adminClients = make(map[string]*sio.Client)
	s.systems = make(map[string]*sio.System)

	if _, ok := csictx.LookupEnv(ctx, "X_CSI_VXFLEXOS_NO_PROBE_ON_START"); !ok {
		return s.doProbe(ctx)
	}
	return nil
}

func (s *service) checkNFS(ctx context.Context, systemID string) (bool, error) {

	err := s.systemProbeAll(ctx)
	if err != nil {
		return false, err
	}

	c := s.adminClients[systemID]
	if c == nil {
		return false, nil
	}
	version, err := c.GetVersion()
	if err != nil {
		return false, err
	}
	ver, err := strconv.ParseFloat(version, 64)
	if err != nil {
		return false, err
	}
	if ver >= 4.0 {
		arrayConData, err := getArrayConfig(ctx)
		if err != nil {
			return false, err
		}
		array := arrayConData[systemID]
		if strings.TrimSpace(array.NasName) == "" {
			Log.Warnf("nasName value not found in secret, it is mandatory parameter for NFS volume operations")
		}
		// even though NasName is not present but PowerFlex version is >=4.0 we support NFS.
		return true, nil
	}
	return false, nil
}

// Probe all systems managed by driver
func (s *service) doProbe(ctx context.Context) error {

	// Putting in mutex to allow tests to pass with race flag
	px.Lock()
	defer px.Unlock()

	if !strings.EqualFold(s.mode, "node") {
		if err := s.systemProbeAll(ctx); err != nil {
			return err
		}
	}

	// Do a node probe
	if !strings.EqualFold(s.mode, "controller") {
		// Probe all systems managed by driver
		if err := s.systemProbeAll(ctx); err != nil {
			return err
		}

		if err := s.nodeProbe(ctx); err != nil {
			return err
		}
	}
	return nil
}

// RegisterAdditionalServers registers any additional grpc services that use the CSI socket.
func (s *service) RegisterAdditionalServers(server *grpc.Server) {
	Log.Info("Registering additional GRPC servers")
	podmon.RegisterPodmonServer(server, s)
	volumeGroupSnapshot.RegisterVolumeGroupSnapshotServer(server, s)
	replication.RegisterReplicationServer(server, s)
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
			Log.Warnf("invalid boolean received %s=(%#v) in params",
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
func (s *service) getSDCIPs(sdcGUID string, systemID string) ([]string, error) { //name change
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

// getStoragePoolID returns pool ID from the given name, system ID, and protectionDomain name
func (s *service) getStoragePoolID(name, systemID, pdID string) (string, error) {

	// Need to lookup ID from the gateway, with respect to PD if provided
	pool, err := s.adminClients[systemID].FindStoragePool("", name, "", pdID)
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
		Log.Printf("getCSIVolume error system not found: %s with error: %v\n", systemID, err)
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
		Log.Printf("getCSIVolumeFromFilesystem error system not found: %s with error: %v\n", systemID, err)
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
		SourceVolumeId: vol.AncestorVolumeID,
		ReadyToUse:     true,
	}
	// Convert array timestamp to CSI timestamp and add
	csiTimestamp, err := ptypes.TimestampProto(time.Unix(int64(vol.CreationTime), 0))
	if err != nil {
		Log.Printf("Could not convert time %v to ptypes.Timestamp %v\n", vol.CreationTime, csiTimestamp)
	}
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
	csiTimestamp, err := ptypes.TimestampProto(time.Unix(int64(creationTime), 0))
	if err != nil {
		Log.Printf("Could not convert time %v to ptypes.Timestamp %v\n", creationTime, csiTimestamp)
	}
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
			Log.Printf("Could not found StoragePool: %s on system %s", id, systemID)
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
		Log.WithFields(fields).Infof("resource statistics counter: %d", s.statisticsCounter)
	}
}

func getArrayConfig(ctx context.Context) (map[string]*ArrayConnectionData, error) {
	arrays := make(map[string]*ArrayConnectionData)

	_, err := os.Stat(ArrayConfigFile)
	if err != nil {
		Log.Errorf("Found error %v while checking stat of file %s ", err, ArrayConfigFile)
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(fmt.Sprintf("File %s does not exist", ArrayConfigFile))
		}
	}

	config, err := os.ReadFile(filepath.Clean(ArrayConfigFile))
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("File %s errors: %v", ArrayConfigFile, err))
	}

	if string(config) != "" {
		creds := make([]ArrayConnectionData, 0)
		// support backward compatibility
		config, _ = yaml.JSONToYAML(config)
		err = yaml.Unmarshal(config, &creds)
		if err != nil {
			return nil, fmt.Errorf(fmt.Sprintf("Unable to parse the credentials: %v", err))
		}

		if len(creds) == 0 {
			return nil, fmt.Errorf("no arrays are provided in vxflexos-creds secret")
		}

		noOfDefaultArray := 0
		for i, c := range creds {
			systemID := c.SystemID
			if _, ok := arrays[systemID]; ok {
				return nil, fmt.Errorf(fmt.Sprintf("duplicate system ID %s found at index %d", systemID, i))
			}
			if systemID == "" {
				return nil, fmt.Errorf(fmt.Sprintf("invalid value for system name at index %d", i))
			}
			if c.Username == "" {
				return nil, fmt.Errorf(fmt.Sprintf("invalid value for Username at index %d", i))
			}
			if c.Password == "" {
				return nil, fmt.Errorf(fmt.Sprintf("invalid value for Password at index %d", i))
			}
			if c.Endpoint == "" {
				return nil, fmt.Errorf(fmt.Sprintf("invalid value for Endpoint at index %d", i))
			}
			// ArrayConnectionData
			if c.AllSystemNames != "" {
				names := strings.Split(c.AllSystemNames, ",")
				Log.Printf("Powerflex systemID %s AllSytemNames given %#v\n", systemID, names)
			}

			// for PowerFlex v4.0
			str := ""
			if strings.TrimSpace(c.NasName) == "" {
				c.NasName = str
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
			}

			Log.WithFields(fields).Infof("configured %s", c.SystemID)

			if c.IsDefault {
				noOfDefaultArray++
			}

			if noOfDefaultArray > 1 {
				return nil, fmt.Errorf("'isDefault' parameter presents more than once in storage array list")
			}

			// copy in the arrayConnectionData to arrays
			copy := ArrayConnectionData{}
			copy = c
			arrays[c.SystemID] = &copy
		}
	} else {
		return nil, fmt.Errorf("arrays details are not provided in vxflexos-creds secret")
	}

	return arrays, nil
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
	index := len(tokens)
	if index > 0 {
		return tokens[index-1]
	}
	err := errors.New("csiVolID unexpected string")
	Log.WithError(err).Errorf("%s format error", csiVolID)
	return ""
}

// getFilesystemIDFromCsiVolumeID returns PowerFlex filesystem ID from CSI volume ID
func getFilesystemIDFromCsiVolumeID(csiVolID string) string {
	if csiVolID == "" {
		return ""
	}
	containsHyphen := strings.Contains(csiVolID, "/")
	if containsHyphen {
		i := strings.LastIndex(csiVolID, "/")
		if i == -1 {
			return csiVolID
		}
		tokens := strings.Split(csiVolID, "/")
		index := len(tokens)
		if index > 0 {
			return tokens[index-1]
		}
	}
	err := errors.New("csiVolID unexpected string")
	Log.WithError(err).Errorf("%s format error", csiVolID)
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
	containsHyphen := strings.Contains(csiVolID, "/")
	if containsHyphen {
		i := strings.LastIndex(csiVolID, "/")
		if i == -1 {
			return ""
		}
		tokens := strings.Split(csiVolID, "/")
		if len(tokens) > 1 {
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
		if len(tokens) > 1 {
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
	removeExtra := regexp.MustCompile("^(.*[\\/])")
	asd := ipaddr[len(ipaddr)-3:]
	findSubnet := removeExtra.ReplaceAll([]byte(asd), []byte(""))
	subnet, err := strconv.ParseInt(string(findSubnet), 10, 64)
	if err != nil {
		return "", errors.New("Parse Mask: Error parsing mask")
	}
	if subnet < 0 || subnet > 32 {
		return "", errors.New("Invalid subnet mask")
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
		Log.Debug("externalAccess after appending netMask bit:", externalAccessCIDR)
	}
	ip, ipnet, err := net.ParseCIDR(externalAccessCIDR)
	if err != nil {
		return "", err
	}
	Log.Debug("Parsed CIDR:", externalAccessCIDR, "-> ip:", ip, " net:", ipnet)
	start, _ := cidr.AddressRange(ipnet)
	fromString, err := GetIPListWithMaskFromString(externalAccessCIDR)
	if err != nil {
		return "", err
	}
	Log.Debug("IP with Mask:", fromString)
	part := strings.Split(fromString, "/")

	// ExernalAccess IP consists of Starting range IP of CIDR+Mask and hence concatenating the same to remove from the array
	externalAccess := start.String() + "/" + part[1]
	return externalAccess, nil
}

// ExternalAccessAlreadyAdded return true if externalAccess is present on ARRAY in any access mode type
func externalAccessAlreadyAdded(export *siotypes.NFSExport, externalAccess string) bool {
	if Contains(export.ReadWriteRootHosts, externalAccess) || Contains(export.ReadWriteHosts, externalAccess) || Contains(export.ReadOnlyRootHosts, externalAccess) || Contains(export.ReadOnlyHosts, externalAccess) {
		Log.Debug("ExternalAccess is already added into Host Access list on array: ", externalAccess)
		return true
	}
	Log.Debug("Going to add externalAccess into Host Access list on array: ", externalAccess)
	return false
}

func (s *service) unexportFilesystem(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest, client *goscaleio.Client, fs *siotypes.FileSystem, volumeContextID string, nodeIPs []string, nodeID string) error {

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
		Log.Infof("NFS Share: %s not found on array.", nfsExportName)
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
				Log.Debug("Going to remove IP from ROHosts: ", nodeIP)
			}
		}
	}

	sort.Strings(nfsExportResp.ReadOnlyRootHosts)
	for _, nodeIP := range nodeIPs {
		index = sort.SearchStrings(nfsExportResp.ReadOnlyRootHosts, nodeIP)
		if len(nfsExportResp.ReadOnlyRootHosts) > 0 {
			if index >= 0 {
				modifyParam.RemoveReadOnlyRootHosts = append(modifyParam.RemoveReadOnlyRootHosts, nodeIP+"/255.255.255.255") // we can't remove without netmask
				Log.Debug("Going to remove IP from RORootHosts: ", nodeIP)
			}
		}
	}

	for _, nodeIP := range nodeIPs {
		if Contains(nfsExportResp.ReadWriteHosts, nodeIP+"/255.255.255.255") {
			modifyParam.RemoveReadWriteHosts = append(modifyParam.RemoveReadWriteHosts, nodeIP+"/255.255.255.255") // we can't remove without netmask
			Log.Debug("Going to remove IP from RWHosts: ", nodeIP)
		}
	}

	for _, nodeIP := range nodeIPs {
		if Contains(nfsExportResp.ReadWriteRootHosts, nodeIP+"/255.255.255.255") {
			modifyParam.RemoveReadWriteRootHosts = append(modifyParam.RemoveReadWriteRootHosts, nodeIP+"/255.255.255.255") // we can't remove without netmask
			Log.Debug("Going to remove IP from RWRootHosts: ", nodeIP)
		}
	}

	err = client.ModifyNFSExport(modifyParam, nfsExportID)

	if err != nil {
		return status.Errorf(codes.NotFound, "Allocating host %s access to NFS Export failed. Error: %v", nodeID, err)

	}
	Log.Debugf("Host: %s access is removed from NFS Share: %s", nodeID, nfsExportID)
	Log.Debugf("ControllerUnpublishVolume successful for volid: [%s]", volumeContextID)

	return nil

}

// exportFilesystem - Method to export filesystem with idempotency
func (s *service) exportFilesystem(ctx context.Context, req *csi.ControllerPublishVolumeRequest, client *goscaleio.Client, fs *siotypes.FileSystem, nodeIPs []string, externalAccess string, nodeID string, pContext map[string]string, am *csi.VolumeCapability_AccessMode) (*csi.ControllerPublishVolumeResponse, error) {

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
		Log.Debugf("NFS Export does not exist for fs: %s ,proceeding to create NFS Export", fs.Name)
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

	//Idempotent case
	if foundIdempotent {
		Log.Info("Host has access to the given host and exists in the required state.")
		return &csi.ControllerPublishVolumeResponse{PublishContext: pContext}, nil
	}

	// Check and remove the default host if given in external access
	if Contains(nodeIPs, externalAccess) {
		Log.Debug("Setting externalAccess to empty as it contains the host ip")
		externalAccess = ""
	}

	//Allocate host access to NFS Share with appropriate access mode
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

	Log.Debugf("NFS Export: %s is accessible to host: %s with access mode: %s", nfsExportID, nodeID, am.Mode)
	Log.Debugf("ControllerPublishVolume successful for volid: [%s]", pContext["volumeContextId"])

	return &csi.ControllerPublishVolumeResponse{PublishContext: pContext}, nil
}

// this function updates volumePrefixToSystems, a map of volume ID prefixes -> system IDs
// this is needed for checkSystemVolumes, a function that verifies that any legacy vol ID
// is found on the default system, only
func (s *service) UpdateVolumePrefixToSystemsMap(systemID string) error {
	//get one vol from system
	vols, _, err := s.listVolumes(systemID, 0, 1, true, false, "", "")

	if err != nil {

		Log.WithError(err).Errorf("failed to list vols for array %s : %s ", systemID, err.Error())
		return fmt.Errorf("failed to list vols for array %s : %s ", systemID, err.Error())

	}

	if len(vols) == 0 {
		//if system has no volumes, then there can't be a legacy vol on it
		Log.Printf("systemID: %s  has no volumes, not adding to volumePrefixToSystems map. \n", systemID)
		return nil

	}
	volID := vols[0].ID

	Log.Printf("vol id in UpdateVolumePrefixToSystemsMap is: %s  from systemID: %s \n", volID, systemID)

	// use first 24 bit from volume id as a key and system id as a value, and add this entry to the map

	key := s.calcKeyForMap(volID)

	if _, ok := s.volumePrefixToSystems[key]; ok {

		//if key found:
		//make sure systemID isn't already added for the specific key
		if contains(s.volumePrefixToSystems[key], systemID) {
			Log.Printf("volumePrefixToSystems: systemID: %s  already added for key %s. Not adding for key again. \n", systemID, key)
			return nil
		}
		//systemID has not been added to key before, add it
		Log.Printf("volumePrefixToSystems: Adding systemID %s to key %s \n", systemID, key)
		s.volumePrefixToSystems[key] = append(s.volumePrefixToSystems[key], systemID)

	} else {
		//if key not found:
		Log.Printf("volumePrefixToSystems: adding new key, value pair: key %s, systemID: %s \n", key, systemID)
		s.volumePrefixToSystems[key] = []string{systemID}
	}

	return nil

}

func (s *service) checkVolumesMap(volumeID string) error {

	systemID := s.getSystemIDFromCsiVolumeID(volumeID)

	// ID is legacy, so we  ensure it's only found on default system
	if systemID == "" {

		Log.Printf("volume id in checkVolumesMap is: %s \n", volumeID)
		Log.Printf("volume %s ,assumed to be on default system. \n", volumeID)

		if len(volumeID) < 3 {
			err := errors.New("vol ID too short")
			Log.WithError(err).Errorf("volume id %s is shorter than 3 chars, returning error", volumeID)
			return fmt.Errorf("volume id %s is shorter than 3 chars, returning error", volumeID)
		}

		key := s.calcKeyForMap(volumeID)

		if _, ok := s.volumePrefixToSystems[key]; ok {

			// key found, make sure vol isn't on non-default system
			// For each systemID in s.volumePrefixToSystems[key], read all volumes from the system
			for _, systemID := range s.volumePrefixToSystems[key] {
				vols, _, err := s.listVolumes(systemID, 0, 0, true, false, "", "")
				if err != nil {
					Log.WithError(err).Errorf("failed to list vols for array %s : %s ", systemID, err.Error())
					return fmt.Errorf("failed to list vols for array %s : %s ", systemID, err.Error())
				}
				for _, vol := range vols {
					if vol.ID == volumeID {
						// legacy volume found on non-default system, this is an error
						Log.WithError(err).Errorf("Found volume id %s on non-default system %s. Expecting this volume id only on default system.  Aborting operation ", volumeID, systemID)
						return fmt.Errorf("Found volume id %s on non-default system %s. Expecting this volume id only on default system.  Aborting operation ", volumeID, systemID)
					}
				}
			}

		}

		// volume was not found on a non default system.
		Log.Infof("checkVolumesMap returns OK")
		return nil
	}

	// volume was not legacy
	Log.Printf("Volume ID: %s contains system ID: %s. checkVolumesMap passed", volumeID, systemID)
	return nil

}

// needs to get first 24 bits of VOlID, this is equivalent to first 3 bytes
func (s *service) calcKeyForMap(volumeID string) string {
	bytes := []byte(volumeID)
	key := string(bytes[0:3])
	return key

}

func (s *service) getProtectionDomainIDFromName(systemID, protectionDomainName string) (string, error) {
	if protectionDomainName == "" {
		Log.Printf("Protection Domain not provided; there could be conflicts if two storage pools share a name")
		return "", nil
	}
	system, err := s.adminClients[systemID].FindSystem(systemID, "", "")
	if err != nil {
		return "", err
	}
	pd, err := system.FindProtectionDomain("", protectionDomainName, "")
	if err != nil {
		return "", err
	}
	return pd.ID, nil
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
	return nil, fmt.Errorf("System %s not found", systemID)
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

	Log.Printf("[getProtectionDomain] - PD not provived, using: %s, System: %s", pd[0].Name, systemID)

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
	Log.Printf("[expandReplicationPair] - Start: %s, %s", systemID, volumeID)
	pair, err := s.findReplicationPairByVolID(systemID, volumeID)
	if err != nil {
		return err
	}

	Log.Printf("[expandReplicationPair] - Pair Found: %+v", pair)
	group, err := s.getReplicationConsistencyGroupByID(systemID, pair.ReplicationConsistencyGroupID)
	if err != nil {
		return err
	}

	Log.Printf("[expandReplicationPair] - Group Found: %+v", group)
	// Avoid getting in a expand attempt cycle.
	if group.ReplicationDirection == "RemoteToLocal" {
		Log.Printf("[expandReplicationPair] - Only want to expand from LocalToRemote, if first call, there might be an issue.")
		return nil
	}

	req.VolumeId = group.RemoteMdmID + "-" + pair.RemoteVolumeID

	resp, err := s.ControllerExpandVolume(ctx, req)
	if err != nil {
		return err
	}

	Log.Printf("[expandReplicationPair] - ControllerExpandVolume expanded the remote volume first: %+v", resp)
	Log.Printf("[expandReplicationPair] - Ensuring remote has expanded...")

	requestedSize, err := validateVolSize(req.CapacityRange)
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
		Log.Printf("NAS server not provided.")
		return "", nil
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
		err := k8sutils.CreateKubeClientSet(KubeConfig)
		if err != nil {
			return nil, status.Error(codes.Internal, GetMessage("init client failed with error: %v", err))
		}
		K8sClientset = k8sutils.Clientset
	}

	// access the API to fetch node object
	node, err := K8sClientset.CoreV1().Nodes().Get(context.TODO(), s.opts.KubeNodeName, v1.GetOptions{})
	if err != nil {
		return nil, status.Error(codes.Internal, GetMessage("Unable to fetch the node labels. Error: %v", err))
	}
	Log.Debugf("Node labels: %v\n", node.Labels)
	return node.Labels, nil
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
