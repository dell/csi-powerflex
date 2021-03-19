package service

import (
	"context"
	"encoding/json"
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

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	podmon "github.com/dell/dell-csi-extensions/podmon"
	"github.com/dell/gocsi"
	csictx "github.com/dell/gocsi/context"
	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	ptypes "github.com/golang/protobuf/ptypes"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	"github.com/dell/csi-vxflexos/core"
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
)

// ArrayConfig is file name with array connection data
var ArrayConfig string

// ArrayConnectionData contains data required to connect to array
type ArrayConnectionData struct {
	SystemID  string `json:"systemID"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Endpoint  string `json:"endpoint"`
	Insecure  bool   `json:"insecure,omitempty"`
	IsDefault bool   `json:"isDefault,omitempty"`
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
	volumePrefixToSystems map[string][]string
}

// New returns a new Service.
func New() Service {
	return &service{
		storagePoolIDToName:   map[string]string{},
		volumePrefixToSystems: map[string][]string{},
	}
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
		}

		log.WithFields(fields).Infof("configured %s", Name)
	}()

	// Get the SP's operating mode.
	s.mode = csictx.Getenv(ctx, gocsi.EnvVarMode)

	opts := Opts{}

	var err error

	// Process configuration file and initialize system clients
	opts.arrays, err = getArrayConfig(ctx)
	if err != nil {
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
	if s.privDir == "" {
		s.privDir = defaultPrivDir
	}

	// pb parses an environment variable into a boolean value. If an error
	// is encountered, default is set to false, and error is logged
	pb := func(n string) bool {
		if v, ok := csictx.LookupEnv(ctx, n); ok {
			b, err := strconv.ParseBool(v)
			if err != nil {
				log.WithField(n, v).Debug(
					"invalid boolean value. defaulting to false")
				return false
			}
			return b
		}
		return false
	}

	opts.Thick = pb(EnvThick)
	opts.AutoProbe = pb(EnvAutoProbe)

	s.opts = opts
	s.adminClients = make(map[string]*sio.Client)
	s.systems = make(map[string]*sio.System)

	if _, ok := csictx.LookupEnv(ctx, "X_CSI_VXFLEXOS_NO_PROBE_ON_START"); !ok {
		// Do a controller probe
		if !strings.EqualFold(s.mode, "node") {
			if err := s.systemProbeAll(ctx); err != nil {
				return err
			}
		}

		// Do a node probe
		if !strings.EqualFold(s.mode, "controller") {
			if err := s.nodeProbe(ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

// RegisterAdditionalServers registers any additional grpc services that use the CSI socket.
func (s *service) RegisterAdditionalServers(server *grpc.Server) {
	log.Info("Registering additional GRPC servers")
	podmon.RegisterPodmonServer(server, s)
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
			log.Warnf("invalid boolean received `%s`=(%v) in params",
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
	// The `GetVolume` API returns a slice of volumes, but when only passing
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

	// Need to translate sdcGUID to sdcID
	id, err := s.systems[systemID].FindSdc("SdcGuid", sdcGUID)
	if err != nil {
		return "", fmt.Errorf("error finding SDC from GUID: %s, err: %s",
			sdcGUID, err.Error())
	}

	return id.Sdc.ID, nil
}

// getStoragePoolID returns pool ID from the given name and system ID.
func (s *service) getStoragePoolID(name string, systemID string) (string, error) {

	// Need to lookup ID from the gateway
	pool, err := s.adminClients[systemID].FindStoragePool("", name, "")
	if err != nil {
		return "", err
	}

	return pool.ID, nil
}

// getCSIVolume converts the given siotypes.Volume to a CSI volume
func (s *service) getCSIVolume(vol *siotypes.Volume, systemID string) *csi.Volume {

	// Get storage pool name; add to cache of ID to Name if not present
	storagePoolName := s.getStoragePoolNameFromID(systemID, vol.StoragePoolID)

	// Make the additional volume attributes
	attributes := map[string]string{
		"Name":            vol.Name,
		"StoragePoolID":   vol.StoragePoolID,
		"StoragePoolName": storagePoolName,
		"CreationTime":    time.Unix(int64(vol.CreationTime), 0).String(),
	}
	dash := "-"
	vi := &csi.Volume{
		VolumeId:      systemID + dash + vol.ID,
		CapacityBytes: int64(vol.SizeInKb * bytesInKiB),
		VolumeContext: attributes,
	}

	return vi
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
		fmt.Printf("Could not convert time %v to ptypes.Timestamp %v\n", vol.CreationTime, csiTimestamp)
	}
	if csiTimestamp != nil {
		snapshot.CreationTime = csiTimestamp
	}
	return snapshot
}

// Returns storage pool name from the given storage pool ID and system ID
func (s *service) getStoragePoolNameFromID(systemID string, id string) string {
	storagePoolName := s.storagePoolIDToName[id]
	if storagePoolName == "" {
		adminClient := s.adminClients[systemID]
		pool, err := adminClient.FindStoragePool(id, "", "")
		if err == nil {
			storagePoolName = pool.Name
			s.storagePoolIDToName[id] = pool.Name
		} else {
			log.Printf("Could not found StoragePool: %s on system %s", id, systemID)
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
		log.WithFields(fields).Infof("resource statistics counter: %d", s.statisticsCounter)
	}
}

func getArrayConfig(ctx context.Context) (map[string]*ArrayConnectionData, error) {
	arrays := make(map[string]*ArrayConnectionData)

	if _, err := os.Stat(ArrayConfig); os.IsNotExist(err) {
		return nil, fmt.Errorf(fmt.Sprintf("File %s does not exist", ArrayConfig))
	}

	config, err := ioutil.ReadFile(filepath.Clean(ArrayConfig))
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("File %s errors: %v", ArrayConfig, err))
	}

	if string(config) != "" {
		jsonCreds := make([]ArrayConnectionData, 0)
		err := json.Unmarshal(config, &jsonCreds)
		if err != nil {
			return nil, fmt.Errorf(fmt.Sprintf("Unable to parse the credentials: %v", err))
		}

		if len(jsonCreds) == 0 {
			return nil, fmt.Errorf("no arrays are provided in vxflexos-creds secret")
		}

		noOfDefaultArray := 0
		for i, c := range jsonCreds {
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

			fields := map[string]interface{}{
				"endpoint":  c.Endpoint,
				"user":      c.Username,
				"password":  "********",
				"insecure":  c.Insecure,
				"isDefault": c.IsDefault,
				"systemID":  c.SystemID,
			}

			log.WithFields(fields).Infof("configured %s", c.SystemID)

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

	tokens := strings.Split(csiVolID, "-")
	if len(tokens) == 1 {
		// Only one token found, which means volume created using csi powerflex from v1.0 to v1.3
		return tokens[0]
	} else if len(tokens) == 2 {
		return tokens[1]
	}

	return ""
}

// getSystemIDFromCsiVolumeId returns PowerFlex volume ID from CSI volume ID
func getSystemIDFromCsiVolumeID(csiVolID string) string {
	tokens := strings.Split(csiVolID, "-")
	if len(tokens) == 2 {
		return tokens[0]
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

		log.WithError(err).Errorf("failed to list vols for array %s : %s ", systemID, err.Error())
		return fmt.Errorf("failed to list vols for array %s : %s ", systemID, err.Error())

	}

	if len(vols) == 0 {
		//if system has no volumes, then there can't be a legacy vol on it
		fmt.Printf("systemID: %s  has no volumes, not adding to volumePrefixToSystems map. \n", systemID)
		return nil

	}
	volID := vols[0].ID

	fmt.Printf("vol id in UpdateVolumePrefixToSystemsMap is: %s  from systemID: %s \n", volID, systemID)

	// use first 24 bit from volume id as a key and system id as a value, and add this entry to the map

	key := s.calcKeyForMap(volID)

	if _, ok := s.volumePrefixToSystems[key]; ok {

		//if key found:
		//make sure systemID isn't already added for the specific key
		if contains(s.volumePrefixToSystems[key], systemID) {
			fmt.Printf("volumePrefixToSystems: systemID: %s  already added for key %s. Not adding for key again. \n", systemID, key)
			return nil
		}
		//systemID has not been added to key before, add it
		fmt.Printf("volumePrefixToSystems: Adding systemID %s to key %s \n", systemID, key)
		s.volumePrefixToSystems[key] = append(s.volumePrefixToSystems[key], systemID)

	} else {
		//if key not found:
		fmt.Printf("volumePrefixToSystems: adding new key, value pair: key %s, systemID: %s \n", key, systemID)
		s.volumePrefixToSystems[key] = []string{systemID}
	}

	return nil

}

func (s *service) checkVolumesMap(volumeID string) error {

	systemID := getSystemIDFromCsiVolumeID(volumeID)

	//ID is legacy, so we  ensure it's only found on default system
	if systemID == "" {

		fmt.Printf("volume id in checkVolumesMap is: %s \n", volumeID)
		fmt.Printf("volume %s ,assumed to be on default system. \n", volumeID)

		if len(volumeID) < 3 {
			err := errors.New("vol ID too short")
			log.WithError(err).Errorf("volume id %s is shorter than 3 chars, returning error", volumeID)
			return fmt.Errorf("volume id %s is shorter than 3 chars, returning error", volumeID)
		}

		key := s.calcKeyForMap(volumeID)

		if _, ok := s.volumePrefixToSystems[key]; ok {

			//key found, make sure vol isn't on non-default system
			//For each systemID in s.volumePrefixToSystems[key], read all volumes from the system
			for _, systemID := range s.volumePrefixToSystems[key] {
				vols, _, err := s.listVolumes(systemID, 0, 0, true, false, "", "")
				if err != nil {
					log.WithError(err).Errorf("failed to list vols for array %s : %s ", systemID, err.Error())
					return fmt.Errorf("failed to list vols for array %s : %s ", systemID, err.Error())
				}
				for _, vol := range vols {
					if vol.ID == volumeID {
						//legacy volume found on non-default system, this is an error
						log.WithError(err).Errorf("Found volume id %s on non-default system %s. Expecting this volume id only on default system.  Aborting operation ", volumeID, systemID)
						return fmt.Errorf("Found volume id %s on non-default system %s. Expecting this volume id only on default system.  Aborting operation ", volumeID, systemID)
					}
				}
			}

		}

		//volume was not found on a non default system.
		log.Infof("checkVolumesMap returns OK")
		return nil
	}

	//volume was not legacy
	fmt.Printf("Volume ID: %s contains system ID: %s. checkVolumesMap passed  \n", volumeID, systemID)
	return nil

}

//needs to get first 24 bits of VOlID, this is equivalent to first 3 bytes
func (s *service) calcKeyForMap(volumeID string) string {
	bytes := []byte(volumeID)
	key := string(bytes[0:3])
	return key

}
