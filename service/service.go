package service

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	ptypes "github.com/golang/protobuf/ptypes"
	"github.com/rexray/gocsi"
	csictx "github.com/rexray/gocsi/context"
	log "github.com/sirupsen/logrus"

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
)

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
}

// Opts defines service configuration options.
type Opts struct {
	Endpoint                   string
	User                       string
	Password                   string
	SystemName                 string
	SdcGUID                    string
	Insecure                   bool
	Thick                      bool
	AutoProbe                  bool
	DisableCerts               bool   // used for unit testing only
	Lsmod                      string // used for unit testing only
	EnableSnapshotCGDelete     bool   // when snapshot deleted, enable deleting of all snaps in the CG of the snapshot
	EnableListVolumesSnapshots bool   // when listing volumes, include snapshots and volumes
}

type service struct {
	opts                Opts
	mode                string
	adminClient         *sio.Client
	system              *sio.System
	volCache            []*siotypes.Volume
	volCacheRWL         sync.RWMutex
	snapCache           []*siotypes.Volume
	snapCacheRWL        sync.RWMutex
	sdcMap              map[string]string
	sdcMapRWL           sync.RWMutex
	spCache             map[string]string
	spCacheRWL          sync.RWMutex
	privDir             string
	storagePoolIDToName map[string]string
	statisticsCounter   int
}

// New returns a new Service.
func New() Service {
	return &service{
		sdcMap:              map[string]string{},
		spCache:             map[string]string{},
		storagePoolIDToName: map[string]string{},
	}
}

func (s *service) BeforeServe(
	ctx context.Context, sp *gocsi.StoragePlugin, lis net.Listener) error {

	defer func() {
		fields := map[string]interface{}{
			"endpoint":       s.opts.Endpoint,
			"user":           s.opts.User,
			"password":       "",
			"systemname":     s.opts.SystemName,
			"sdcGUID":        s.opts.SdcGUID,
			"insecure":       s.opts.Insecure,
			"thickprovision": s.opts.Thick,
			"privatedir":     s.privDir,
			"autoprobe":      s.opts.AutoProbe,
			"mode":           s.mode,
		}

		if s.opts.Password != "" {
			fields["password"] = "******"
		}
		
		//censor user for logging purposes
                fields["user"] = "******"
		log.WithFields(fields).Infof("configured %s", Name)
		fields["user"] =  s.opts.User
	}()

	// Get the SP's operating mode.
	s.mode = csictx.Getenv(ctx, gocsi.EnvVarMode)

	opts := Opts{}

	if ep, ok := csictx.LookupEnv(ctx, EnvEndpoint); ok {
		opts.Endpoint = ep
	}
	if user, ok := csictx.LookupEnv(ctx, EnvUser); ok {
		opts.User = user
	}
	if opts.User == "" {
		opts.User = "admin"
	}
	if pw, ok := csictx.LookupEnv(ctx, EnvPassword); ok {
		opts.Password = pw
	}
	if name, ok := csictx.LookupEnv(ctx, EnvSystemName); ok {
		opts.SystemName = name
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

	opts.Insecure = pb(EnvInsecure)
	opts.Thick = pb(EnvThick)
	opts.AutoProbe = pb(EnvAutoProbe)

	s.opts = opts

	if _, ok := csictx.LookupEnv(ctx, "X_CSI_VXFLEXOS_NO_PROBE_ON_START"); !ok {
		// Do a controller probe
		if !strings.EqualFold(s.mode, "node") {
			if err := s.controllerProbe(ctx); err != nil {
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

func (s *service) getVolByID(id string) (*siotypes.Volume, error) {

	// The `GetVolume` API returns a slice of volumes, but when only passing
	// in a volume ID, the response will be just the one volume
	vols, err := s.adminClient.GetVolume("", strings.TrimSpace(id), "", "", false)
	if err != nil {
		return nil, err
	}
	return vols[0], nil
}

func (s *service) getSDCID(sdcGUID string) (string, error) {
	sdcGUID = strings.ToUpper(sdcGUID)

	// check if ID is already in cache
	f := func() string {
		s.sdcMapRWL.RLock()
		defer s.sdcMapRWL.RUnlock()

		if id, ok := s.sdcMap[sdcGUID]; ok {
			return id
		}
		return ""
	}
	if id := f(); id != "" {
		return id, nil
	}

	// Need to translate sdcGUID to sdcID
	id, err := s.system.FindSdc("SdcGuid", sdcGUID)
	if err != nil {
		return "", fmt.Errorf("error finding SDC from GUID: %s, err: %s",
			sdcGUID, err.Error())
	}

	s.sdcMapRWL.Lock()
	defer s.sdcMapRWL.Unlock()

	s.sdcMap[sdcGUID] = id.Sdc.ID

	return id.Sdc.ID, nil
}

func (s *service) getStoragePoolID(name string) (string, error) {
	// check if ID is already in cache
	f := func() string {
		s.spCacheRWL.RLock()
		defer s.spCacheRWL.RUnlock()

		if id, ok := s.spCache[name]; ok {
			return id
		}
		return ""
	}
	if id := f(); id != "" {
		return id, nil
	}

	// Need to lookup ID from the gateway
	pool, err := s.adminClient.FindStoragePool("", name, "")
	if err != nil {
		return "", err
	}

	s.spCacheRWL.Lock()
	defer s.spCacheRWL.Unlock()
	s.spCache[name] = pool.ID

	return pool.ID, nil
}

func (s *service) getCSIVolume(vol *siotypes.Volume) *csi.Volume {
	// Get storage pool name; add to cache of ID to Name if not present
	storagePoolName := s.getStoragePoolNameFromID(vol.StoragePoolID)

	// Make the additional volume attributes
	attributes := map[string]string{
		"Name":            vol.Name,
		"StoragePoolID":   vol.StoragePoolID,
		"StoragePoolName": storagePoolName,
		"CreationTime":    time.Unix(int64(vol.CreationTime), 0).String(),
	}

	vi := &csi.Volume{
		VolumeId:      vol.ID,
		CapacityBytes: int64(vol.SizeInKb * bytesInKiB),
		VolumeContext: attributes,
	}

	return vi
}

// Convert an SIO Volume into a CSI Snapshot object suitable for return.
func (s *service) getCSISnapshot(vol *siotypes.Volume) *csi.Snapshot {
	snapshot := &csi.Snapshot{
		SizeBytes:      int64(vol.SizeInKb) * bytesInKiB,
		SnapshotId:     vol.ID,
		SourceVolumeId: vol.AncestorVolumeID,
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

func (s *service) getStoragePoolNameFromID(id string) string {
	storagePoolName := s.storagePoolIDToName[id]
	if storagePoolName == "" {
		pool, err := s.adminClient.FindStoragePool(id, "", "")
		if err == nil {
			storagePoolName = pool.Name
			s.storagePoolIDToName[id] = pool.Name
		} else {
			log.Printf("Could not found StoragePool: %s", id)
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
