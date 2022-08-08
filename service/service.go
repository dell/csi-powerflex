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
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-vxflexos/v2/core"
	"github.com/dell/csi-vxflexos/v2/k8sutils"
	"github.com/dell/dell-csi-extensions/podmon"
	"github.com/dell/dell-csi-extensions/replication"
	volumeGroupSnapshot "github.com/dell/dell-csi-extensions/volumeGroupSnapshot"
	"github.com/dell/gocsi"
	csictx "github.com/dell/gocsi/context"
	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	"github.com/fsnotify/fsnotify"
	"github.com/golang/protobuf/ptypes"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// ArrayConfigFile is file name with array connection data
var ArrayConfigFile string

// DriverConfigParamsFile is the name of the input driver config params file
var DriverConfigParamsFile string

// KubeConfig is the kube config
var KubeConfig string

// K8sClientset is the client to query k8s
var K8sClientset kubernetes.Interface

//Log controlls the logger
//give default value, will be overwritten by configmap
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

//New returns a handle to service
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
	} else {
		opts.IsHealthMonitorEnabled = false
	}
	if s.privDir == "" {
		s.privDir = defaultPrivDir
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
	replication.RegisterReplicationServer(server, s)
	volumeGroupSnapshot.RegisterVolumeGroupSnapshotServer(server, s)
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

func (s *service) getSystem(systemID string) ([]*siotypes.System, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	// Gets the desired system content. Needed for remote replication.
	system, err := adminClient.GetSystems()
	if err != nil {
		return nil, err
	}
	return system, nil
}

func (s *service) getProtectionDomain(systemID string, system *siotypes.System) ([]*siotypes.ProtectionDomain, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	// Gets the desired system content. Needed for remote replication.
	theSystem, err := adminClient.FindSystem(system.ID, "", "")
	if err != nil {
		return nil, err
	}

	pd, err := theSystem.GetProtectionDomain("")
	if err != nil {
		return nil, err
	}

	return pd, nil
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

	if _, err := os.Stat(ArrayConfigFile); os.IsNotExist(err) {
		return nil, fmt.Errorf(fmt.Sprintf("File %s does not exist", ArrayConfigFile))
	}

	config, err := ioutil.ReadFile(filepath.Clean(ArrayConfigFile))
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

			skipCertificateValidation := c.SkipCertificateValidation || c.Insecure

			fields := map[string]interface{}{
				"endpoint":                  c.Endpoint,
				"user":                      c.Username,
				"password":                  "********",
				"skipCertificateValidation": skipCertificateValidation,
				"isDefault":                 c.IsDefault,
				"systemID":                  c.SystemID,
				"allSystemNames":            c.AllSystemNames,
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

// getSystemIDFromCsiVolumeId returns PowerFlex volume ID from CSI volume ID
func (s *service) getSystemIDFromCsiVolumeID(csiVolID string) string {
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

	// There is only volume ID in csi volume ID
	return ""
}

//this function updates volumePrefixToSystems, a map of volume ID prefixes -> system IDs
//this is needed for checkSystemVolumes, a function that verifies that any legacy vol ID
//is found on the default system, only
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
