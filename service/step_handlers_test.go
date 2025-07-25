// Copyright © 2019-2023 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dell/goscaleio"
	types "github.com/dell/goscaleio/types/v1"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	codes "google.golang.org/grpc/codes"
)

var (
	debug              bool
	sdcMappings        []types.MappedSdcInfo
	sdcMappingsID      string
	setSdcNameSuccess  bool
	sdcIDToName        map[string]string
	isQuotaEnabled     bool
	sdcDependencyOnNFS bool

	stepHandlersErrors struct {
		FindVolumeIDError             bool
		GetVolByIDError               bool
		GetStoragePoolsError          bool
		PodmonFindSdcError            bool
		PodmonVolumeStatisticsError   bool
		PodmonNoNodeIDError           bool
		PodmonNoSystemError           bool
		PodmonNoVolumeNoNodeIDError   bool
		PodmonControllerProbeError    bool
		PodmonNodeProbeError          bool
		PodmonVolumeError             bool
		GetSystemSdcError             bool
		GetSystemLimitError           bool
		GetSdcInstancesError          bool
		MapSdcError                   bool
		ApproveSdcError               bool
		RemoveMappedSdcError          bool
		SDCLimitsError                bool
		SIOGatewayVolumeNotFoundError bool
		GetStatisticsError            bool
		CreateSnapshotError           bool
		RemoveVolumeError             bool
		VolumeInstancesError          bool
		FileSystemInstancesError      bool
		GetFileSystemsByIDError       bool
		NoFileSystemIDError           bool
		NFSExportInstancesError       bool
		NasServerNotFoundError        bool
		FileInterfaceNotFoundError    bool
		BadVolIDError                 bool
		NoCsiVolIDError               bool
		WrongVolIDError               bool
		WrongFileSystemIDError        bool
		WrongSystemError              bool
		NoEndpointError               bool
		NoUserError                   bool
		NoPasswordError               bool
		NoSysNameError                bool
		NoAdminError                  bool
		WrongSysNameError             bool
		WrongSystemIDError            bool
		BadCapacityError              bool
		NoVolumeIDError               bool
		SetVolumeSizeError            bool
		systemNameMatchingError       bool
		LegacyVolumeConflictError     bool
		VolumeIDTooShortError         bool
		EmptyEphemeralID              bool
		IncorrectEphemeralID          bool
		TooManyDashesVolIDError       bool
		CorrectFormatBadCsiVolID      bool
		EmptySysID                    bool
		VolIDListEmptyError           bool
		CreateVGSNoNameError          bool
		CreateVGSNameTooLongError     bool
		CreateVGSLegacyVol            bool
		CreateVGSAcrossTwoArrays      bool
		CreateVGSBadTimeError         bool
		CreateSplitVGSError           bool
		BadVolIDJSON                  bool
		BadMountPathError             bool
		NoMountPathError              bool
		NoVolIDError                  bool
		NoVolIDSDCError               bool
		NoVolError                    bool
		SetSdcNameError               bool
		UpdateConfigMapUnmarshalError bool
		GetIPAddressByInterfaceError  bool
		UpdateConfigK8sClientError    bool
		UpdateConfigFormatError       bool
		ConfigMapNotFoundError        bool
	}
)

// This file contains HTTP handlers for mocking to the ScaleIO API.
// This allows unit testing with a Scale IO but still provides some coverage in the goscaleio library.
var (
	scaleioRouter                 http.Handler
	testControllerHasNoConnection bool
	count                         int
)

var inducedError error

const (
	remoteRCGID            = "d303184900000001"
	unmarkedForReplication = "UnmarkedForReplication"
	defaultVolumeSize      = "33554432"
	defaultConsistencyMode = goscaleio.Consistent
)

// getFileHandler returns an http.Handler that
func getHandler() http.Handler {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			log.Printf("handler called: %s %s", r.Method, r.URL)
			if scaleioRouter == nil {
				getRouter().ServeHTTP(w, r)
			}
		})
	log.Printf("Clearing volume caches\n")
	volumeIDToName = make(map[string]string)
	fileSystemIDName = make(map[string]string)
	fileSystemIDToSizeTotal = make(map[string]string)
	fileSystemIDParentID = make(map[string]string)
	nfsExportIDName = make(map[string]string)
	fileSystemNameToID = make(map[string]string)
	nfsExportNameID = make(map[string]string)
	nfsExportIDtoFsID = make(map[string]string)
	nfsExportIDPath = make(map[string]string)
	volumeIDToAncestorID = make(map[string]string)
	volumeNameToID = make(map[string]string)
	volumeIDToConsistencyGroupID = make(map[string]string)
	volumeIDToReplicationState = make(map[string]string)
	volumeIDToSizeInKB = make(map[string]string)
	nfsExportIDReadOnlyRootHosts = make(map[string][]string)
	nfsExportIDReadWriteRootHosts = make(map[string][]string)
	nfsExportIDReadWriteHosts = make(map[string][]string)
	nfsExportIDReadOnlyHosts = make(map[string][]string)
	treeQuotaID = make(map[string]string)
	treeQuotaIDToPath = make(map[string]string)
	treeQuotaIDToSoftLimit = make(map[string]string)
	treeQuotaIDToGracePeriod = make(map[string]string)
	treeQuotaIDToHardLimit = make(map[string]string)
	debug = false
	sdcDependencyOnNFS = false
	stepHandlersErrors.FindVolumeIDError = false
	stepHandlersErrors.GetVolByIDError = false
	stepHandlersErrors.SIOGatewayVolumeNotFoundError = false
	stepHandlersErrors.GetStoragePoolsError = false
	stepHandlersErrors.PodmonFindSdcError = false
	stepHandlersErrors.PodmonVolumeStatisticsError = false
	stepHandlersErrors.PodmonNoVolumeNoNodeIDError = false
	stepHandlersErrors.PodmonNoNodeIDError = false
	stepHandlersErrors.PodmonNoSystemError = false
	stepHandlersErrors.PodmonControllerProbeError = false
	stepHandlersErrors.PodmonNodeProbeError = false
	stepHandlersErrors.PodmonVolumeError = false
	stepHandlersErrors.GetSdcInstancesError = false
	stepHandlersErrors.MapSdcError = false
	stepHandlersErrors.RemoveMappedSdcError = false
	stepHandlersErrors.SDCLimitsError = false
	stepHandlersErrors.GetStatisticsError = false
	stepHandlersErrors.GetSystemSdcError = false
	stepHandlersErrors.CreateSnapshotError = false
	stepHandlersErrors.RemoveVolumeError = false
	stepHandlersErrors.VolumeInstancesError = false
	stepHandlersErrors.NasServerNotFoundError = false
	stepHandlersErrors.FileInterfaceNotFoundError = false
	stepHandlersErrors.FileSystemInstancesError = false
	stepHandlersErrors.NFSExportInstancesError = false
	stepHandlersErrors.NasServerNotFoundError = false
	stepHandlersErrors.BadCapacityError = false
	stepHandlersErrors.BadVolIDError = false
	stepHandlersErrors.GetFileSystemsByIDError = false
	stepHandlersErrors.NoCsiVolIDError = false
	stepHandlersErrors.WrongVolIDError = false
	stepHandlersErrors.WrongFileSystemIDError = false
	stepHandlersErrors.NoFileSystemIDError = false
	stepHandlersErrors.WrongSystemError = false
	stepHandlersErrors.NoEndpointError = false
	stepHandlersErrors.NoUserError = false
	stepHandlersErrors.NoPasswordError = false
	stepHandlersErrors.NoSysNameError = false
	stepHandlersErrors.NoAdminError = false
	stepHandlersErrors.WrongSysNameError = false
	stepHandlersErrors.WrongSystemIDError = false
	stepHandlersErrors.NoVolumeIDError = false
	stepHandlersErrors.SetVolumeSizeError = false
	stepHandlersErrors.systemNameMatchingError = false
	stepHandlersErrors.LegacyVolumeConflictError = false
	stepHandlersErrors.VolumeIDTooShortError = false
	stepHandlersErrors.EmptyEphemeralID = false
	stepHandlersErrors.IncorrectEphemeralID = false
	stepHandlersErrors.TooManyDashesVolIDError = false
	stepHandlersErrors.CorrectFormatBadCsiVolID = false
	stepHandlersErrors.EmptySysID = false
	stepHandlersErrors.VolIDListEmptyError = false
	stepHandlersErrors.CreateVGSNoNameError = false
	stepHandlersErrors.CreateVGSNameTooLongError = false
	stepHandlersErrors.CreateVGSLegacyVol = false
	stepHandlersErrors.CreateVGSAcrossTwoArrays = false
	stepHandlersErrors.CreateVGSBadTimeError = false
	stepHandlersErrors.CreateSplitVGSError = false
	stepHandlersErrors.BadVolIDJSON = false
	stepHandlersErrors.BadMountPathError = false
	stepHandlersErrors.NoMountPathError = false
	stepHandlersErrors.NoVolIDError = false
	stepHandlersErrors.NoVolIDSDCError = false
	stepHandlersErrors.NoVolError = false
	stepHandlersErrors.SetSdcNameError = false
	stepHandlersErrors.ApproveSdcError = false
	stepHandlersErrors.UpdateConfigMapUnmarshalError = false
	stepHandlersErrors.GetIPAddressByInterfaceError = false
	stepHandlersErrors.UpdateConfigK8sClientError = false
	stepHandlersErrors.UpdateConfigFormatError = false
	stepHandlersErrors.ConfigMapNotFoundError = false
	sdcMappings = sdcMappings[:0]
	sdcMappingsID = ""
	return handler
}

func getRouter() http.Handler {
	scaleioRouter := mux.NewRouter()
	scaleioRouter.HandleFunc("/api/instances/{from}::{id}/action/{action}", handleAction)
	scaleioRouter.HandleFunc("/api/instances/{from}::{id}/relationships/{to}", handleRelationships)
	scaleioRouter.HandleFunc("/api/types/Volume/instances/action/queryIdByKey", handleQueryVolumeIDByKey)
	scaleioRouter.HandleFunc("/api/instances/{type}::{id}", handleInstances)
	scaleioRouter.HandleFunc("/api/login", handleLogin)
	scaleioRouter.HandleFunc("/api/version", handleVersion)
	scaleioRouter.HandleFunc("/api/types/System/instances", handleSystemInstances)
	scaleioRouter.HandleFunc("/rest/v1/nas-servers", handleNasInstances)
	scaleioRouter.HandleFunc("/rest/v1/nas-servers/{id}", handleGetNasInstances)
	scaleioRouter.HandleFunc("/rest/v1/file-systems", handleFileSystems)
	scaleioRouter.HandleFunc("/rest/v1/nfs-exports", handleNFSExports)
	scaleioRouter.HandleFunc("/rest/v1/file-systems/{id}", handleGetFileSystems)
	scaleioRouter.HandleFunc("/rest/v1/nfs-exports/{id}", handleGetNFSExports)
	scaleioRouter.HandleFunc("/rest/v1/file-systems/{id}/restore", handleRestoreSnapshotNFS)
	scaleioRouter.HandleFunc("/rest/v1/file-interfaces/{id}", handleGetFileInterface)
	scaleioRouter.HandleFunc("/rest/v1/file-systems/{id}/snapshot", handleNFSSnapshots)
	scaleioRouter.HandleFunc("/api/types/Volume/instances", handleVolumeInstances)
	scaleioRouter.HandleFunc("/api/types/StoragePool/instances", handleStoragePoolInstances)
	scaleioRouter.HandleFunc("{Volume}/relationship/Statistics", handleVolumeStatistics)
	scaleioRouter.HandleFunc("/api/Volume/relationship/Statistics", handleVolumeStatistics)
	scaleioRouter.HandleFunc("{SdcGUID}/relationships/Sdc", handleSystemSdc)
	scaleioRouter.HandleFunc("/api/types/PeerMdm/instances", handlePeerMdmInstances)
	scaleioRouter.HandleFunc("/api/types/ReplicationConsistencyGroup/instances", handleReplicationConsistencyGroupInstances)
	scaleioRouter.HandleFunc("/api/types/ReplicationPair/instances", handleReplicationPairInstances)
	scaleioRouter.HandleFunc("/rest/v1/file-tree-quotas", handleFileTreeQuotas)
	scaleioRouter.HandleFunc("/rest/v1/file-tree-quotas/{id}", handleGetFileTreeQuotas)
	scaleioRouter.HandleFunc("/api/instances/System/action/querySystemLimits", handleGetSystemLimits)
	scaleioRouter.HandleFunc("/rest/v1/nfs-servers", handleIsNFSEnabled)
	return scaleioRouter
}

// handle implements GET /api/types/StoragePool/instances
func handleVolumeStatistics(w http.ResponseWriter, _ *http.Request) {
	if stepHandlersErrors.PodmonVolumeStatisticsError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}
	returnJSONFile("features", "get_volume_statistics.json", w, nil)
}

func handleSystemSdc(w http.ResponseWriter, _ *http.Request) {
	if stepHandlersErrors.GetSystemSdcError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}
	returnJSONFile("features", "get_sdc_instances.json", w, nil)
}

func handleGetSystemLimits(w http.ResponseWriter, _ *http.Request) {
	if stepHandlersErrors.GetSystemLimitError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}
	returnJSONFile("features", "get_system_limits.json", w, nil)
}

// handleLogin implements GET /api/login
func handleLogin(w http.ResponseWriter, r *http.Request) {
	u, p, ok := r.BasicAuth()
	if !ok || len(strings.TrimSpace(u)) < 1 || len(strings.TrimSpace(p)) < 1 {
		w.Header().Set("WWW-Authenticate", "Basic realm=Restricted")
		w.WriteHeader(http.StatusUnauthorized)
		returnJSONFile("features", "authorization_failure.json", w, nil)
		return
	}
	if testControllerHasNoConnection {
		w.WriteHeader(http.StatusRequestTimeout)
		return
	}
	w.Write([]byte("YWRtaW46MTU0MTU2MjIxOTI5MzpmODkxNDVhN2NkYzZkNGNkYjYxNGE0OGRkZGE3Zjk4MA"))
}

// handleLogin implements GET /api/version
func handleVersion(w http.ResponseWriter, _ *http.Request) {
	if testControllerHasNoConnection {
		w.WriteHeader(http.StatusRequestTimeout)
		return
	}
	w.Write([]byte("4.0"))
}

// handleSystemInstances implements GET /api/types/System/instances
func handleSystemInstances(w http.ResponseWriter, _ *http.Request) {
	if stepHandlersErrors.PodmonNodeProbeError {
		writeError(w, "PodmonNodeProbeError", http.StatusRequestTimeout, codes.Internal)
		return
	}
	if stepHandlersErrors.PodmonControllerProbeError {
		writeError(w, "PodmonControllerProbeError", http.StatusRequestTimeout, codes.Internal)
		return
	}
	if inducedError.Error() == "BadRemoteSystemIDError" {
		returnJSONFile("features", "get_primary_system_instance.json", w, nil)
		return
	}
	if stepHandlersErrors.systemNameMatchingError {
		count++
	}
	if count == 2 || stepHandlersErrors.WrongSysNameError || stepHandlersErrors.WrongSystemIDError {
		fmt.Printf("DEBUG send bad system\n")
		returnJSONFile("features", "bad_system.json", w, nil)
		count = 0
	} else {
		returnJSONFile("features", "get_system_instances.json", w, nil)
	}
}

func handleNFSSnapshots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:

		if inducedError.Error() == "CreateSnapshotsError" {
			writeError(w, "error creating snapshot", http.StatusRequestTimeout, codes.Internal)
			return
		}
		vars := mux.Vars(r)
		id := vars["id"]
		req := types.CreateFileSystemSnapshotParam{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		resp := types.CreateFileSystemSnapshotResponse{}
		resp.ID = hex.EncodeToString([]byte(req.Name))
		fileSystemIDName[resp.ID] = req.Name
		fileSystemNameToID[req.Name] = resp.ID
		fileSystemIDParentID[resp.ID] = id
		sizeTotal := fileSystemIDToSizeTotal[id]
		fileSystemIDToSizeTotal[resp.ID] = sizeTotal

		if array, ok := systemArrays[r.Host]; ok {
			fmt.Printf("Host Endpoint %s\n", r.Host)
			array.fileSystems[resp.ID] = make(map[string]string)
			array.fileSystems[resp.ID]["name"] = req.Name
			array.fileSystems[resp.ID]["id"] = resp.ID
			array.fileSystems[resp.ID]["parent_id"] = id
			array.fileSystems[resp.ID]["size_total"] = sizeTotal
		}
		if debug {
			log.Printf("request name: %s id: %s\n", req.Name, resp.ID)
		}
		encoder := json.NewEncoder(w)
		err = encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}

		log.Printf("end make fileSystemSnapshots")
	}
}

// handleNasInstances implements GET /api/types/System/instances
func handleNasInstances(w http.ResponseWriter, _ *http.Request) {
	if stepHandlersErrors.NasServerNotFoundError {
		writeError(w, "nas server not found", http.StatusNotFound, codes.NotFound)
		return
	}

	returnJSONFile("features", "get_nas_servers.json", w, nil)
}

// handleIsNFSEnabled implements GET rest/v1/nfs-servers?select=*
func handleIsNFSEnabled(w http.ResponseWriter, _ *http.Request) {
	returnJSONFile("features", "get_nfs_server.json", w, nil)
}

func handleGetNasInstances(w http.ResponseWriter, _ *http.Request) {
	if stepHandlersErrors.NasServerNotFoundError {
		writeError(w, "nas server not found", http.StatusNotFound, codes.NotFound)
		return
	}

	returnJSONFile("features", "get_nas_server_id.json", w, nil)
}

func handleGetFileInterface(w http.ResponseWriter, _ *http.Request) {
	if stepHandlersErrors.FileInterfaceNotFoundError {
		writeError(w, "file interace not found", http.StatusNotFound, codes.NotFound)
		return
	}

	returnJSONFile("features", "get_file_interface.json", w, nil)
}

func handleNFSExports(w http.ResponseWriter, r *http.Request) {
	if nfsExportIDName == nil {
		nfsExportIDName = make(map[string]string)
		nfsExportNameID = make(map[string]string)
	}

	switch r.Method {

	// Post is CreateVolume; here just return a volume id encoded from the name
	case http.MethodPost:
		if inducedError.Error() == "nfsExportError" {
			writeError(w, "create NFS Export failed", http.StatusRequestTimeout, codes.Internal)
			return
		}

		req := types.NFSExportCreate{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}

		// good response
		resp := new(types.NFSExportCreateResponse)
		resp.ID = hex.EncodeToString([]byte(req.Name))
		nfsExportIDName[resp.ID] = req.Name
		nfsExportIDtoFsID[req.Name] = resp.ID
		nfsExportIDtoFsID[resp.ID] = req.FileSystemID
		nfsExportIDPath[resp.ID] = req.Path

		if array, ok := systemArrays[r.Host]; ok {
			fmt.Printf("Host Endpoint %s\n", r.Host)
			array.nfsExports[resp.ID] = make(map[string]string)
			array.nfsExports[resp.ID]["name"] = req.Name
			array.nfsExports[resp.ID]["id"] = resp.ID
			array.nfsExports[resp.ID]["path"] = req.Path
			array.nfsExports[resp.ID]["file_system_id"] = req.FileSystemID

			if len(req.ReadOnlyRootHosts) != 0 {
				array.nfsExports[resp.ID]["read_root_hosts"] = req.ReadOnlyRootHosts[0]
			} else if len(req.ReadWriteRootHosts) != 0 {
				array.nfsExports[resp.ID]["write_root_hosts"] = req.ReadWriteRootHosts[0]
			} else if len(req.ReadOnlyHosts) != 0 {
				array.nfsExports[resp.ID]["read_hosts"] = req.ReadOnlyHosts[0]
			} else if len(req.ReadWriteHosts) != 0 {
				array.nfsExports[resp.ID]["write_hosts"] = req.ReadWriteHosts[0]
			} else {
				array.nfsExports[resp.ID]["read_hosts"] = ""
				array.nfsExports[resp.ID]["write_hosts"] = ""
				array.nfsExports[resp.ID]["read_root_hosts"] = ""
				array.nfsExports[resp.ID]["write_root_hosts"] = ""
			}

		}

		if debug {
			log.Printf("request name: %s id: %s\n", req.Name, resp.ID)
		}
		encoder := json.NewEncoder(w)
		err = encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}

		log.Printf("end make nfsExports")
	// Read all the Volumes
	case http.MethodGet:
		if stepHandlersErrors.NFSExportInstancesError {
			writeError(w, "error getting the NFS Exports", http.StatusInternalServerError, codes.Internal)
			return
		}
		instances := make([]*types.NFSExport, 0)
		nfsExports := make(map[string]map[string]string)

		if array, ok := systemArrays[r.Host]; ok {
			nfsExports = array.nfsExports

			for _, nfsExp := range nfsExports {

				replacementMap := make(map[string]string)
				replacementMap["__ID__"] = nfsExp["id"]
				replacementMap["__NAME__"] = nfsExp["name"]
				replacementMap["__PATH__"] = nfsExp["path"]
				replacementMap["__FS_ID__"] = nfsExp["file_system_id"]
				replacementMap["__READ_HOSTS__"] = nfsExp["read_hosts"]
				replacementMap["__WRITE_HOSTS__"] = nfsExp["write_hosts"]
				replacementMap["__READ_ROOT_HOSTS__"] = nfsExp["read_root_hosts"]
				replacementMap["__WRITE_ROOT_HOSTS__"] = nfsExp["write_root_hosts"]
				data := returnJSONFile("features", "nfsexport.json.template", nil, replacementMap)
				nfsExp := new(types.NFSExport)
				err := json.Unmarshal(data, nfsExp)
				if err != nil {
					log.Printf("error unmarshalling json: %s\n", string(data))
				}
				instances = append(instances, nfsExp)
			}
		}

		// Add none-created volumes (old)
		for id, name := range nfsExportIDName {
			if _, ok := nfsExports[id]; ok {
				continue
			}

			name = id
			replacementMap := make(map[string]string)
			replacementMap["__ID__"] = id
			replacementMap["__NAME__"] = name
			replacementMap["__PATH__"] = nfsExportIDPath[id]
			replacementMap["__FS_ID__"] = nfsExportIDtoFsID[id]
			if len(nfsExportIDReadOnlyRootHosts[id]) != 0 {
				replacementMap["__READ_HOSTS__"] = nfsExportIDReadOnlyHosts[id][0]
			} else if len(nfsExportIDReadWriteRootHosts[id]) != 0 {
				replacementMap["__WRITE_HOSTS__"] = nfsExportIDReadWriteHosts[id][0]
			} else if len(nfsExportIDReadWriteRootHosts[id]) != 0 {
				replacementMap["__WRITE_ROOT_HOSTS__"] = nfsExportIDReadWriteRootHosts[id][0]
			} else if len(nfsExportIDReadOnlyRootHosts[id]) != 0 {
				replacementMap["__READ_ROOT_HOSTS__"] = nfsExportIDReadOnlyRootHosts[id][0]
			} else {
				replacementMap["__READ_HOSTS__"] = ""
				replacementMap["__WRITE_HOSTS__"] = ""
				replacementMap["__READ_ROOT_HOSTS__"] = ""
				replacementMap["__WRITE_ROOT_HOSTS__"] = ""
			}
			data := returnJSONFile("features", "nfsexport.json.template", nil, replacementMap)
			nfsExp := new(types.NFSExport)
			err := json.Unmarshal(data, nfsExp)
			if err != nil {
				log.Printf("error unmarshalling json: %s\n", string(data))
			}
			instances = append(instances, nfsExp)
		}

		encoder := json.NewEncoder(w)
		err := encoder.Encode(instances)
		if err != nil {
			log.Printf("error encoding json: %s\n", err)
		}
	}
}

func handleGetNFSExports(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:

		vars := mux.Vars(r)
		id := vars["id"]

		fmt.Println("id:", id)
		fmt.Println("fsidname", nfsExportIDName[id])

		if inducedError.Error() == "nfsExportNotFoundError" {
			writeError(w, "Could not find NFS Export", http.StatusNotFound, codes.NotFound)
			return
		}

		// Insert to map if it doesn't exist.
		if nfsExportIDName[id] == "" {
			log.Printf("Did not find id %s \n", id)
			writeError(w, "could not find nfsExport ", http.StatusNotFound, codes.NotFound)
			return
		}

		replacementMap := make(map[string]string)
		nfsExp := make(map[string]string)
		if array, ok := systemArrays[r.Host]; ok {
			nfsExp = array.nfsExports[id]
		}

		log.Printf("Get id %s\n", id)
		if nfsExp != nil {
			replacementMap["__ID__"] = nfsExp["id"]
			replacementMap["__NAME__"] = nfsExp["name"]
			replacementMap["__PATH__"] = nfsExp["path"]
			replacementMap["__FS_ID__"] = nfsExp["file_system_id"]
			replacementMap["__READ_HOSTS__"] = nfsExp["read_hosts"]
			replacementMap["__WRITE_HOSTS__"] = nfsExp["write_hosts"]
			replacementMap["__READ_ROOT_HOSTS__"] = nfsExp["read_root_hosts"]
			replacementMap["__WRITE_ROOT_HOSTS__"] = nfsExp["write_root_hosts"]
		} else {
			replacementMap["__ID__"] = id
			replacementMap["__NAME__"] = nfsExportIDName[id]
			replacementMap["__PATH__"] = nfsExportIDPath[id]
			replacementMap["__FS_ID__"] = nfsExportIDtoFsID[id]
			if len(nfsExportIDReadOnlyRootHosts["id"]) != 0 {
				replacementMap["__READ_HOSTS__"] = nfsExportIDReadOnlyHosts["id"][0]
			} else if len(nfsExportIDReadWriteRootHosts["id"]) != 0 {
				replacementMap["__WRITE_HOSTS__"] = nfsExportIDReadWriteHosts["id"][0]
			} else if len(nfsExportIDReadWriteRootHosts["id"]) != 0 {
				replacementMap["__WRITE_ROOT_HOSTS__"] = nfsExportIDReadWriteRootHosts["id"][0]
			} else if len(nfsExportIDReadOnlyRootHosts["id"]) != 0 {
				replacementMap["__READ_ROOT_HOSTS__"] = nfsExportIDReadOnlyRootHosts["id"][0]
			} else {
				replacementMap["__READ_HOSTS__"] = ""
				replacementMap["__WRITE_HOSTS__"] = ""
				replacementMap["__READ_ROOT_HOSTS__"] = ""
				replacementMap["__WRITE_ROOT_HOSTS__"] = ""
			}
		}

		if inducedError.Error() == "readHostsIncompatible" {
			replacementMap["__READ_HOSTS__"] = "127.1.1.11/255.255.255.255"
		}

		if inducedError.Error() == "writeHostsIncompatible" {
			replacementMap["__WRITE_HOSTS__"] = "127.1.1.11/255.255.255.255"
		}

		data := returnJSONFile("features", "nfsexport.json.template", nil, replacementMap)
		nfsExp1 := new(types.NFSExport)
		err := json.Unmarshal(data, nfsExp1)
		if err != nil {
			log.Printf("error unmarshalling json: %s\n", string(data))
		}

		encoder := json.NewEncoder(w)
		err = encoder.Encode(nfsExp1)
		if err != nil {
			log.Printf("error encoding json: %s\n", err)
		}
	case http.MethodDelete:
		vars := mux.Vars(r)
		id := vars["id"]

		// Insert to map if it doesn't exist.
		if nfsExportIDName[id] == "" {
			log.Printf("Did not find id %s \n", id)
			writeError(w, "could not find nfsExport ", http.StatusNotFound, codes.NotFound)
			return
		}

		nfsExp := make(map[string]string)

		if array, ok := systemArrays[r.Host]; ok {
			nfsExp = array.nfsExports[id]
			delete(array.nfsExports, id)
		}
		nfsExportIDName[id] = ""
		nfsExportNameID[nfsExp["name"]] = ""
		nfsExportIDPath[id] = ""
		nfsExportIDtoFsID[id] = ""
	case http.MethodPatch:
		vars := mux.Vars(r)
		id := vars["id"]
		fmt.Println("id:", id)
		fmt.Println("fsidname", nfsExportIDName[id])

		if nfsExportIDName[id] == "" {
			log.Printf("Did not find id %s \n", id)
			writeError(w, "could not find nfsExport ", http.StatusNotFound, codes.NotFound)
			return
		}

		if inducedError.Error() == "nfsExportModifyError" {
			writeError(w, "Allocating host access failed", http.StatusGatewayTimeout, codes.Internal)
			return
		}

		req := types.NFSExportModify{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		fmt.Printf("patchReq:%#v\n", req)
		if len(req.AddReadOnlyRootHosts) != 0 {
			nfsExportIDReadOnlyRootHosts[id] = req.AddReadOnlyRootHosts
		} else if len(req.AddReadWriteRootHosts) != 0 {
			nfsExportIDReadWriteRootHosts[id] = req.AddReadWriteRootHosts
		}
		if array, ok := systemArrays[r.Host]; ok {
			if len(req.AddReadOnlyRootHosts) != 0 {
				array.nfsExports[id]["read_root_hosts"] = req.AddReadOnlyRootHosts[0]
			} else if len(req.AddReadWriteRootHosts) != 0 {
				array.nfsExports[id]["write_root_hosts"] = req.AddReadWriteRootHosts[0]
			} else if len(req.AddReadOnlyHosts) != 0 {
				array.nfsExports[id]["read_hosts"] = req.AddReadOnlyHosts[0]
			} else if len(req.AddReadWriteHosts) != 0 {
				array.nfsExports[id]["write_hosts"] = req.AddReadWriteHosts[0]
			} else {
				array.nfsExports[id]["read_hosts"] = ""
				array.nfsExports[id]["write_hosts"] = ""
				array.nfsExports[id]["read_root_hosts"] = ""
				array.nfsExports[id]["write_root_hosts"] = ""
			}
		}

		w.WriteHeader(http.StatusNoContent)

	}
}

func handleFileSystems(w http.ResponseWriter, r *http.Request) {
	if fileSystemIDName == nil {
		fileSystemIDName = make(map[string]string)
		fileSystemNameToID = make(map[string]string)
		fileSystemIDToSizeTotal = make(map[string]string)
	}

	if stepHandlersErrors.FileSystemInstancesError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}

	if stepHandlersErrors.BadCapacityError {
		writeError(w, "bad capacity error", http.StatusBadRequest, codes.InvalidArgument)
		return
	}

	switch r.Method {

	// Post is CreateVolume; here just return a volume id encoded from the name
	case http.MethodPost:
		if inducedError.Error() == "CreateVolumeError" {
			writeError(w, "create volume induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}

		req := types.FsCreate{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}

		// good response
		resp := new(types.FileSystemResp)
		resp.ID = hex.EncodeToString([]byte(req.Name))
		fileSystemIDName[resp.ID] = req.Name
		fileSystemNameToID[req.Name] = resp.ID
		fileSystemIDToSizeTotal[resp.ID] = strconv.Itoa(req.SizeTotal)

		if array, ok := systemArrays[r.Host]; ok {
			fmt.Printf("Host Endpoint %s\n", r.Host)
			// array.fileSystems = make(map[string]map[string]string)
			array.fileSystems[resp.ID] = make(map[string]string)
			array.fileSystems[resp.ID]["name"] = req.Name
			array.fileSystems[resp.ID]["id"] = resp.ID
			array.fileSystems[resp.ID]["size_total"] = strconv.Itoa(req.SizeTotal)
		}

		if debug {
			log.Printf("request name: %s id: %s\n", req.Name, resp.ID)
		}
		encoder := json.NewEncoder(w)
		err = encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}

		log.Printf("end make fileSystems")
	// Read all the Volumes
	case http.MethodGet:
		instances := make([]*types.FileSystem, 0)
		fileSystems := make(map[string]map[string]string)

		if array, ok := systemArrays[r.Host]; ok {
			fileSystems = array.fileSystems

			for _, fs := range fileSystems {
				replacementMap := make(map[string]string)
				replacementMap["__ID__"] = fs["id"]
				replacementMap["__NAME__"] = fs["name"]
				replacementMap["__SIZE_IN_Total__"] = fs["size_total"]
				replacementMap["__PARENT_ID__"] = fs["parent_id"]
				replacementMap["__IS_QUOTA_ENABLED__"] = strconv.FormatBool(isQuotaEnabled)
				data := returnJSONFile("features", "filesystem.json.template", nil, replacementMap)
				fs := new(types.FileSystem)
				err := json.Unmarshal(data, fs)
				if err != nil {
					log.Printf("error unmarshalling json: %s\n", string(data))
				}
				instances = append(instances, fs)
			}
		}

		// Add none-created volumes (old)
		for id, name := range fileSystemIDName {
			if _, ok := fileSystems[id]; ok {
				continue
			}

			name = id
			replacementMap := make(map[string]string)
			replacementMap["__ID__"] = id
			replacementMap["__NAME__"] = name
			replacementMap["__SIZE_IN_Total__"] = fileSystemIDToSizeTotal[id]
			replacementMap["__PARENT_ID__"] = fileSystemIDParentID[id]
			replacementMap["__IS_QUOTA_ENABLED__"] = strconv.FormatBool(isQuotaEnabled)
			data := returnJSONFile("features", "filesystem.json.template", nil, replacementMap)
			fs := new(types.FileSystem)
			err := json.Unmarshal(data, fs)
			if err != nil {
				log.Printf("error unmarshalling json: %s\n", string(data))
			}
			instances = append(instances, fs)
		}

		encoder := json.NewEncoder(w)
		err := encoder.Encode(instances)
		if err != nil {
			log.Printf("error encoding json: %s\n", err)
		}
	}
}

func handleRestoreSnapshotNFS(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	// Post is CreateVolume; here just return a volume id encoded from the name
	case http.MethodPost:
		if inducedError.Error() == "restoreVolumeError" {
			writeError(w, "create volume induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}

		req := types.RestoreFsSnapParam{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}

		// good response
		resp := new(types.RestoreFsSnapResponse)
		resp.ID = req.SnapshotID
		if debug {
			log.Printf("response id: %s\n", resp.ID)
		}
		encoder := json.NewEncoder(w)
		err = encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}

		log.Printf("end make restore fs from snaspshot")
	}
}

func handleGetFileSystems(w http.ResponseWriter, r *http.Request) {
	switch r.Method {

	// Post is CreateVolume; here just return a volume id encoded from the name
	case http.MethodGet:
		vars := mux.Vars(r)
		id := vars["id"]

		if stepHandlersErrors.GetFileSystemsByIDError {
			writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}

		// Insert to map if it doesn't exist.
		if fileSystemIDName[id] == "" {
			log.Printf("Did not find id %s \n", id)
			writeError(w, "could not find filesystem ", http.StatusNotFound, codes.NotFound)
			return
		}

		replacementMap := make(map[string]string)
		fs := make(map[string]string)
		if array, ok := systemArrays[r.Host]; ok {
			fs = array.fileSystems[id]
		}

		log.Printf("Get id %s\n", id)
		if fs != nil {
			replacementMap["__ID__"] = fs["id"]
			replacementMap["__NAME__"] = fs["name"]
			replacementMap["__SIZE_IN_Total__"] = fs["size_total"]
			replacementMap["__IS_QUOTA_ENABLED__"] = strconv.FormatBool(isQuotaEnabled)
			replacementMap["__PARENT_ID__"] = fs["parent_id"]
			if fs["parent_id"] != "" {
				if inducedError.Error() == "GetSnashotByIdError" {
					writeError(w, "could not find snapshot id", http.StatusNotFound, codes.NotFound)
					return
				}
			}
		} else {
			replacementMap["__ID__"] = id
			replacementMap["__NAME__"] = fileSystemIDName[id]
			replacementMap["__SIZE_IN_Total__"] = fileSystemIDToSizeTotal[id]
			replacementMap["__PARENT_ID__"] = fileSystemIDParentID[id]
			replacementMap["__IS_QUOTA_ENABLED__"] = strconv.FormatBool(isQuotaEnabled)
			if fileSystemIDParentID[id] != "" {
				if inducedError.Error() == "GetSnashotByIdError" {
					writeError(w, "could not find snapshot id", http.StatusNotFound, codes.NotFound)
					return
				}
			}
		}

		data := returnJSONFile("features", "filesystem.json.template", nil, replacementMap)
		fs1 := new(types.FileSystem)
		err := json.Unmarshal(data, fs1)
		if err != nil {
			log.Printf("error unmarshalling json: %s\n", string(data))
		}

		encoder := json.NewEncoder(w)
		err = encoder.Encode(fs1)
		if err != nil {
			log.Printf("error encoding json: %s\n", err)
		}
	case http.MethodDelete:
		if inducedError.Error() == "DeleteSnapshotError" {
			writeError(w, "error while deleting the filesystem snapshot", http.StatusGatewayTimeout, codes.Internal)
			return
		}
		vars := mux.Vars(r)
		id := vars["id"]

		// Insert to map if it doesn't exist.
		if fileSystemIDName[id] == "" {
			log.Printf("Did not find id %s \n", id)
			writeError(w, "could not find filesystem ", http.StatusNotFound, codes.NotFound)
			return
		}

		fs := make(map[string]string)

		if array, ok := systemArrays[r.Host]; ok {
			fs = array.fileSystems[id]
			delete(array.fileSystems, id)
		}
		fileSystemIDName[id] = ""
		fileSystemNameToID[fs["name"]] = ""
		fileSystemIDToSizeTotal[id] = ""
	case http.MethodPatch:
		req := types.FSModify{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		if inducedError.Error() == "ModifyFSError" {
			writeError(w, "Modify filesystem failed with error:", http.StatusRequestTimeout, codes.Internal)
			return
		}
		vars := mux.Vars(r)
		id := vars["id"]
		fmt.Println("id:", id)

		fmt.Printf("patchReq:%#v\n", req)
		fmt.Printf("req.IsQuotaEnabled:%#v\n", req.IsQuotaEnabled)
		if array, ok := systemArrays[r.Host]; ok {
			array.fileSystems[id]["size"] = strconv.Itoa(req.Size)
			array.fileSystems[id]["description"] = req.Description
			array.fileSystems[id]["isquotaenabled"] = strconv.FormatBool(req.IsQuotaEnabled)
			array.fileSystems[id]["hardlimit"] = strconv.Itoa(req.DefaultHardLimit)
			array.fileSystems[id]["softlimit"] = strconv.Itoa(req.DefaultSoftLimit)
			array.fileSystems[id]["graceperiod"] = strconv.Itoa(req.GracePeriod)
		}
		w.WriteHeader(http.StatusNoContent)
		log.Printf("end modify file systems")
	}

	// returnJSONFile("features", "get_file_system_response.json", w, nil)
}

// handleStoragePoolInstances implements GET /api/types/StoragePool/instances
func handleStoragePoolInstances(w http.ResponseWriter, _ *http.Request) {
	if stepHandlersErrors.GetStoragePoolsError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}
	returnJSONFile("features", "get_storage_pool_instances.json", w, nil)
}

func handlePeerMdmInstances(w http.ResponseWriter, _ *http.Request) {
	if inducedError.Error() == "PeerMdmError" {
		writeError(w, "PeerMdmError", http.StatusRequestTimeout, codes.Internal)
		return
	}
	returnJSONFile("features", "get_peer_mdms.json", w, nil)
}

func returnJSONFile(directory, filename string, w http.ResponseWriter, replacements map[string]string) (jsonBytes []byte) {
	jsonBytes, err := os.ReadFile(filepath.Join(directory, filename))
	if err != nil {
		log.Printf("Couldn't read %s/%s\n", directory, filename)
		if w != nil {
			w.WriteHeader(http.StatusNotFound)
		}
		return make([]byte, 0)
	}
	if replacements != nil {
		jsonString := string(jsonBytes)
		for key, value := range replacements {
			if value == "" {
				if key == "__READ_HOSTS__" {
					jsonString = strings.ReplaceAll(jsonString, `"read_only_hosts": ["__READ_HOSTS__"],`, "")
					continue
				} else if key == "__READ_ROOT_HOSTS__" {
					jsonString = strings.ReplaceAll(jsonString, `"read_only_root_hosts": ["__READ_ROOT_HOSTS__"],`, "")
					continue
				} else if key == "__WRITE_HOSTS__" {
					jsonString = strings.ReplaceAll(jsonString, `"read_write_hosts": ["__WRITE_HOSTS__"],`, "")
					continue
				} else if key == "__WRITE_ROOT_HOSTS__" {
					jsonString = strings.ReplaceAll(jsonString, `"read_write_root_hosts": ["__WRITE_ROOT_HOSTS__"],`, "")
					continue
				}
			}
			jsonString = strings.Replace(jsonString, key, value, -1)
		}

		if debug {
			log.Printf("Edited payload:\n%s\n", jsonString)
		}
		jsonBytes = []byte(jsonString)
	}
	if debug {
		log.Printf("jsonBytes:\n%s\n", jsonBytes)
	}
	if w != nil {
		_, err = w.Write(jsonBytes)
		if err != nil {
			log.Printf("Couldn't write to ResponseWriter")
			w.WriteHeader(http.StatusInternalServerError)
			return make([]byte, 0)
		}
	}
	return jsonBytes
}

// Map of volume ID to name
var volumeIDToName map[string]string

// Map of FileSystem ID to name
var fileSystemIDName map[string]string

// Map of FileSystem ID to parentID
var fileSystemIDParentID map[string]string

// Map of NFSExport ID to name
var nfsExportIDName map[string]string

// Map of volume name to ID
var volumeNameToID map[string]string

// Map of FileSystem Name to ID
var fileSystemNameToID map[string]string

// Map of NFSExport Name to ID
var nfsExportNameID map[string]string

// Map of NFSExport ID to FilesystemID
var nfsExportIDtoFsID map[string]string

// Map of NFSExport ID to path
var nfsExportIDPath map[string]string

var (
	nfsExportIDReadWriteRootHosts map[string][]string
	nfsExportIDReadOnlyRootHosts  map[string][]string
	nfsExportIDReadOnlyHosts      map[string][]string
	nfsExportIDReadWriteHosts     map[string][]string
)

// Map of volume ID to ancestor ID
var volumeIDToAncestorID map[string]string

// Map of volume ID to consistency group ID
var volumeIDToConsistencyGroupID map[string]string

// Replication group mode to replace for.
var replicationGroupConsistMode string

// Map of volume ID to Replication State
var volumeIDToReplicationState map[string]string

// Map of volume ID to size in KB
var volumeIDToSizeInKB map[string]string

// Map of FileSystem ID to size Total
var fileSystemIDToSizeTotal map[string]string

// Replication group state to replace for.
var replicationGroupState string

// Map of Tree quota ID
var treeQuotaID map[string]string

// Map of Tree quota ID to Path
var treeQuotaIDToPath map[string]string

// Map of Tree quota ID to soft limit
var treeQuotaIDToSoftLimit map[string]string

// Map of Tree quota ID to grace period
var treeQuotaIDToGracePeriod map[string]string

// Map of Tree quota ID to hard limit
var treeQuotaIDToHardLimit map[string]string

// Possible rework, every systemID should have a instances similar to an array.
type systemArray struct {
	ID                           string
	replicationSystem            *systemArray
	volumes                      map[string]map[string]string
	fileSystems                  map[string]map[string]string
	nfsExports                   map[string]map[string]string
	replicationConsistencyGroups map[string]map[string]string
	replicationPairs             map[string]map[string]string
	treeQuotas                   map[string]map[string]string
}

func (s *systemArray) Init() {
	s.volumes = make(map[string]map[string]string)
	s.fileSystems = make(map[string]map[string]string)
	s.nfsExports = make(map[string]map[string]string)
	s.replicationConsistencyGroups = make(map[string]map[string]string)
	s.replicationPairs = make(map[string]map[string]string)
	s.treeQuotas = make(map[string]map[string]string)
}

func (s *systemArray) Link(remoteSystem *systemArray) {
	s.replicationSystem = remoteSystem
	remoteSystem.replicationSystem = s
}

var systemArrays map[string]*systemArray

// handleVolumeInstances handles listing all volumes or creating a volume
func handleVolumeInstances(w http.ResponseWriter, r *http.Request) {
	if volumeIDToName == nil {
		volumeIDToName = make(map[string]string)
		volumeIDToAncestorID = make(map[string]string)
		volumeNameToID = make(map[string]string)
		volumeIDToConsistencyGroupID = make(map[string]string)
		volumeIDToReplicationState = make(map[string]string)
		volumeIDToSizeInKB = make(map[string]string)
	}

	if stepHandlersErrors.VolumeInstancesError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}

	if inducedError.Error() == "SnapshotCreationError" {
		writeError(w, "RCG snapshot not created", http.StatusRequestTimeout, codes.Internal)
		return
	}

	switch r.Method {

	// Post is CreateVolume; here just return a volume id encoded from the name
	case http.MethodPost:
		if inducedError.Error() == "CreateVolumeError" {
			writeError(w, "create volume induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}

		req := types.VolumeParam{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		if volumeNameToID[req.Name] != "" {
			w.WriteHeader(http.StatusInternalServerError)
			// duplicate volume name response
			log.Printf("request for volume creation of duplicate name: %s\n", req.Name)
			resp := new(types.Error)
			resp.Message = sioGatewayVolumeNameInUse
			resp.HTTPStatusCode = http.StatusInternalServerError
			resp.ErrorCode = 6
			encoder := json.NewEncoder(w)
			err = encoder.Encode(resp)
			if err != nil {
				log.Printf("error encoding json: %s\n", err.Error())
			}
			return
		}
		// good response
		resp := new(types.VolumeResp)
		resp.ID = hex.EncodeToString([]byte(req.Name))
		volumeIDToName[resp.ID] = req.Name
		volumeNameToID[req.Name] = resp.ID
		volumeIDToAncestorID[resp.ID] = "null"
		volumeIDToConsistencyGroupID[resp.ID] = "null"
		volumeIDToReplicationState[resp.ID] = unmarkedForReplication
		volumeIDToSizeInKB[resp.ID] = req.VolumeSizeInKb

		if array, ok := systemArrays[r.Host]; ok {
			fmt.Printf("Host Endpoint %s\n", r.Host)
			array.volumes[resp.ID] = make(map[string]string)
			array.volumes[resp.ID]["name"] = req.Name
			array.volumes[resp.ID]["id"] = resp.ID
			array.volumes[resp.ID]["sizeInKb"] = req.VolumeSizeInKb
			array.volumes[resp.ID]["volumeReplicationState"] = unmarkedForReplication
			array.volumes[resp.ID]["consistencyGroupID"] = "null"
			array.volumes[resp.ID]["ancestorVolumeId"] = "null"
		}

		if debug {
			log.Printf("request name: %s id: %s\n", req.Name, resp.ID)
		}
		encoder := json.NewEncoder(w)
		err = encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}

		log.Printf("end make volumes")
	// Read all the Volumes
	case http.MethodGet:
		instances := make([]*types.Volume, 0)
		volumes := make(map[string]map[string]string)

		if array, ok := systemArrays[r.Host]; ok {
			volumes = array.volumes

			for id, vol := range volumes {
				replacementMap := make(map[string]string)
				replacementMap["__ID__"] = vol["id"]
				replacementMap["__NAME__"] = vol["name"]
				replacementMap["__MAPPED_SDC_INFO__"] = getSdcMappings(id)
				replacementMap["__ANCESTOR_ID__"] = vol["ancestorVolumeId"]
				replacementMap["__CONSISTENCY_GROUP_ID__"] = vol["consistencyGroupID"]
				replacementMap["__SIZE_IN_KB__"] = vol["sizeInKb"]
				replacementMap["__VOLUME_REPLICATION_STATE__"] = vol["volumeReplicationState"]
				data := returnJSONFile("features", "volume.json.template", nil, replacementMap)
				vol := new(types.Volume)
				err := json.Unmarshal(data, vol)
				if err != nil {
					log.Printf("error unmarshalling json: %s\n", string(data))
				}
				instances = append(instances, vol)
			}
		}

		// Add none-created volumes (old)
		for id, name := range volumeIDToName {
			if _, ok := volumes[id]; ok {
				continue
			}

			name = id
			replacementMap := make(map[string]string)
			replacementMap["__ID__"] = id
			replacementMap["__NAME__"] = name
			replacementMap["__MAPPED_SDC_INFO__"] = getSdcMappings(id)
			replacementMap["__ANCESTOR_ID__"] = volumeIDToAncestorID[id]
			replacementMap["__CONSISTENCY_GROUP_ID__"] = volumeIDToConsistencyGroupID[id]
			replacementMap["__SIZE_IN_KB__"] = volumeIDToSizeInKB[id]
			replacementMap["__VOLUME_REPLICATION_STATE__"] = volumeIDToReplicationState[id]
			data := returnJSONFile("features", "volume.json.template", nil, replacementMap)
			vol := new(types.Volume)
			err := json.Unmarshal(data, vol)
			if err != nil {
				log.Printf("error unmarshalling json: %s\n", string(data))
			}
			instances = append(instances, vol)
		}

		encoder := json.NewEncoder(w)
		err := encoder.Encode(instances)
		if err != nil {
			log.Printf("error encoding json: %s\n", err)
		}
	}
}

func handleAction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	from := vars["from"]
	id := vars["id"]
	action := vars["action"]
	log.Printf("action from %s id %s action %s", from, id, action)
	switch action {
	case "setSdcName":
		if stepHandlersErrors.SetSdcNameError {
			writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}
		req := types.RenameSdcParam{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		fmt.Printf("SdcName: %s\n", req.SdcName)
		sdcIDToName = make(map[string]string, 0)
		if id == "d0f055a700000000" {
			sdcIDToName[id] = req.SdcName
		}

		if id == "d0f055aa00000001" {
			sdcIDToName[id] = req.SdcName
		}

		setSdcNameSuccess = true

	case "approveSdc":
		errMsg := "The given GUID is invalid.Please specify GUID in the following format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
		if stepHandlersErrors.ApproveSdcError {
			writeError(w, errMsg, http.StatusInternalServerError, codes.Internal)
		}
		req := types.ApproveSdcParam{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		resp := types.ApproveSdcByGUIDResponse{SdcID: "d0f055a700000000"}
		encoder := json.NewEncoder(w)
		err = encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}

	case "addMappedSdc":
		if stepHandlersErrors.MapSdcError {
			writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}
		req := types.MapVolumeSdcParam{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		fmt.Printf("SdcID: %s\n", req.SdcID)
		if req.SdcID == "d0f055a700000000" {
			sdcMappings = append(sdcMappings, types.MappedSdcInfo{SdcID: req.SdcID, SdcIP: "127.1.1.11"})
		}
		fmt.Printf("SdcID: %s\n", req.SdcID)
		if req.SdcID == "d0f055aa00000001" {
			sdcMappings = append(sdcMappings, types.MappedSdcInfo{SdcID: req.SdcID, SdcIP: "127.1.1.10"})
		}
	case "removeMappedSdc":
		if stepHandlersErrors.RemoveMappedSdcError {
			writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}
		req := types.UnmapVolumeSdcParam{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		for i, val := range sdcMappings {
			if val.SdcID == req.SdcID {
				copy(sdcMappings[i:], sdcMappings[i+1:])
				sdcMappings = sdcMappings[:len(sdcMappings)-1]
			}
		}
	case "setMappedSdcLimits":
		if stepHandlersErrors.SDCLimitsError {
			writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}
		req := types.SetMappedSdcLimitsParam{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		fmt.Printf("SdcID: %s\n", req.SdcID)
		if req.SdcID == "d0f055a700000000" {
			sdcMappings = append(sdcMappings, types.MappedSdcInfo{SdcID: req.SdcID})
		}
		fmt.Printf("BandwidthLimitInKbps: %s\n", req.BandwidthLimitInKbps)
		if req.BandwidthLimitInKbps == "10240" {
			sdcMappings = append(sdcMappings, types.MappedSdcInfo{SdcID: req.SdcID, LimitBwInMbps: 10})
		}
		fmt.Printf("IopsLimit: %s\n", req.IopsLimit)
		if req.IopsLimit == "11" {
			sdcMappings = append(sdcMappings, types.MappedSdcInfo{SdcID: req.SdcID, LimitIops: 11})
		}
	case "snapshotVolumes":
		if stepHandlersErrors.CreateSnapshotError {
			writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		}
		req := types.SnapshotVolumesParam{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		for _, snapParam := range req.SnapshotDefs {
			// For now, only a single snapshot ID is supported

			id := snapParam.VolumeID

			cgValue := "f30216fb00000001"

			if snapParam.SnapshotName == "clone" || snapParam.SnapshotName == "volumeFromSnap" {
				id = "72cee42500000003"
			}
			if snapParam.SnapshotName == "invalid-clone" {
				writeError(w, "inducedError Volume not found", http.StatusRequestTimeout, codes.Internal)
				return
			}

			if stepHandlersErrors.WrongVolIDError {
				id = "72cee42500000002"
			}
			if stepHandlersErrors.FindVolumeIDError {
				id = "72cee42500000002"
				writeError(w, "inducedError Volume not found", http.StatusRequestTimeout, codes.Internal)
				return
			}

			volumeIDToName[id] = snapParam.SnapshotName
			volumeNameToID[snapParam.SnapshotName] = id
			volumeIDToAncestorID[id] = snapParam.VolumeID
			volumeIDToConsistencyGroupID[id] = cgValue
			volumeIDToSizeInKB[id] = defaultVolumeSize
			volumeIDToReplicationState[id] = unmarkedForReplication

			if array, ok := systemArrays[r.Host]; ok {
				array.volumes[id] = make(map[string]string)
				array.volumes[id]["name"] = snapParam.SnapshotName
				array.volumes[id]["id"] = id
				array.volumes[id]["sizeInKb"] = defaultVolumeSize
				array.volumes[id]["volumeReplicationState"] = unmarkedForReplication
				array.volumes[id]["consistencyGroupID"] = cgValue
				array.volumes[id]["ancestorVolumeId"] = snapParam.VolumeID
			}
		}

		if stepHandlersErrors.WrongVolIDError {
			returnJSONFile("features", "create_snapshot2.json", w, nil)
		}
		returnJSONFile("features", "create_snapshot.json", w, nil)
	case "removeVolume":
		if stepHandlersErrors.RemoveVolumeError {
			writeError(w, "inducedError", http.StatusRequestTimeout, codes.Internal)
		}

		if name, ok := volumeIDToName[id]; ok {
			volumeIDToName[id] = ""
			volumeIDToAncestorID[id] = ""
			volumeIDToConsistencyGroupID[id] = ""
			volumeIDToSizeInKB[id] = ""
			volumeIDToSizeInKB[id] = defaultVolumeSize
			volumeIDToReplicationState[id] = ""
			if name != "" {
				volumeNameToID[name] = ""
			}
		}

		if array, ok := systemArrays[r.Host]; ok {
			delete(array.volumes, id)
		}

	case "setVolumeSize":
		if stepHandlersErrors.SetVolumeSizeError {
			writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}
		req := types.SetVolumeSizeParam{}
		decoder := json.NewDecoder(r.Body)
		_ = decoder.Decode(&req)
		intValue, _ := strconv.Atoi(req.SizeInGB)
		volumeIDToSizeInKB[id] = strconv.Itoa(intValue / 1024)
	case "setVolumeName":
		// volumeIDToName[id] = snapParam.Name
		req := types.SetVolumeNameParam{}
		decoder := json.NewDecoder(r.Body)
		_ = decoder.Decode(&req)
		fmt.Printf("set volume name %s", req.NewName)
		volumeIDToName[id] = req.NewName
	case "removeReplicationConsistencyGroup":
		if inducedError.Error() == "RemoveRCGError" {
			writeError(w, "coule not remove RCG", http.StatusRequestTimeout, codes.Internal)
			return
		}

		groups := systemArrays[r.Host].replicationConsistencyGroups
		delete(groups, id)

	case "removeReplicationPair":
		if inducedError.Error() == "NoDeleteReplicationPair" {
			writeError(w, "pairs exist", http.StatusRequestTimeout, codes.Internal)
			return
		}
		pairs := systemArrays[r.Host].replicationPairs

		// Set replication state
		targetSystem := systemArrays[r.Host].replicationSystem
		remoteVolume := pairs[id]["remoteVolumeId"]
		targetSystem.volumes[remoteVolume]["volumeReplicationState"] = unmarkedForReplication

		delete(pairs, id)

		fmt.Printf("volumeIDToReplicationState %+v\n", volumeIDToReplicationState)

	case "createReplicationConsistencyGroupSnapshots":
		if inducedError.Error() == "ExecuteActionError" {
			writeError(w, "could not execute RCG action", http.StatusRequestTimeout, codes.Internal)
			return
		}

		snapshotGroupID := uuid.New().String()
		resp := types.CreateReplicationConsistencyGroupSnapshotResp{}
		resp.SnapshotGroupID = snapshotGroupID

		remoteConsistencyGroup := systemArrays[r.Host].replicationConsistencyGroups[id]["remoteId"]
		remoteSystem := systemArrays[r.Host].replicationSystem

		for _, pair := range remoteSystem.replicationPairs {
			if pair["replicationConsistencyGroupId"] == remoteConsistencyGroup {
				volID := uuid.New().String()
				volName := "snapshot-" + pair["localVolumeId"]
				remoteSystem.volumes[volID] = make(map[string]string)
				remoteSystem.volumes[volID]["name"] = volName
				remoteSystem.volumes[volID]["id"] = volID
				remoteSystem.volumes[volID]["sizeInKb"] = remoteSystem.volumes[pair["localVolumeId"]]["sizeInKb"]
				remoteSystem.volumes[volID]["volumeReplicationState"] = unmarkedForReplication
				remoteSystem.volumes[volID]["consistencyGroupID"] = snapshotGroupID
				remoteSystem.volumes[volID]["ancestorVolumeId"] = pair["localVolumeId"]
			}
		}

		encoder := json.NewEncoder(w)
		err := encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err)
		}
	case "switchoverReplicationConsistencyGroup":
		fallthrough
	case "failoverReplicationConsistencyGroup":
		fallthrough
	case "restoreReplicationConsistencyGroup":
		fallthrough
	case "reverseReplicationConsistencyGroup":
		fallthrough
	case "resumeReplicationConsistencyGroup":
		fallthrough
	case "pauseReplicationConsistencyGroup":
		fallthrough
	case "syncNowReplicationConsistencyGroup":
		if inducedError.Error() == "ExecuteActionError" {
			writeError(w, "could not execute RCG action", http.StatusRequestTimeout, codes.Internal)
			return
		}
	}
}

func getSdcMappings(volumeID string) string {
	var bytes []byte
	var err error
	if sdcMappingsID == "" || volumeID == sdcMappingsID {
		bytes, err = json.Marshal(&sdcMappings)
	} else {
		var emptyMappings []types.MappedSdcInfo
		bytes, err = json.Marshal(&emptyMappings)
	}
	if err != nil {
		log.Printf("Json marshalling error: %s", err.Error())
		return ""
	}
	if debug {
		fmt.Printf("sdcMappings: %s\n", string(bytes))
	}
	return string(bytes)
}

func handleRelationships(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	from := vars["from"]
	id := vars["id"]
	to := vars["to"]
	log.Printf("relationship from %s id %s to %s", from, id, to)
	switch to {
	case "Sdc":
		if stepHandlersErrors.GetSdcInstancesError {
			writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		} else if stepHandlersErrors.PodmonFindSdcError {
			writeError(w, "PodmonFindSdcError", http.StatusRequestTimeout, codes.Internal)
		} else if stepHandlersErrors.PodmonNoSystemError {
			writeError(w, "PodmonNoSystemError", http.StatusRequestTimeout, codes.Internal)
		} else if stepHandlersErrors.PodmonControllerProbeError {
			writeError(w, "PodmonControllerProbeError", http.StatusRequestTimeout, codes.Internal)
			return
		} else if stepHandlersErrors.PodmonNodeProbeError {
			writeError(w, "PodmonNodeProbeError", http.StatusRequestTimeout, codes.Internal)
			return
		}
		if setSdcNameSuccess {
			instances := make([]*types.Sdc, 0)
			for id, name := range sdcIDToName {
				replacementMap := make(map[string]string)
				replacementMap["__ID__"] = id
				replacementMap["__NAME__"] = name
				data := returnJSONFile("features", "get_sdc_instances.json.template", nil, replacementMap)
				sdc := new(types.Sdc)
				err := json.Unmarshal(data, sdc)
				if err != nil {
					log.Printf("error unmarshalling json: %s\n", string(data))
				}
				instances = append(instances, sdc)
			}
			encoder := json.NewEncoder(w)
			err := encoder.Encode(instances)
			if err != nil {
				log.Printf("error encoding json: %s\n", err)
			}
			setSdcNameSuccess = false
			return
		}
		returnJSONFile("features", "get_sdc_instances.json", w, nil)
	case "Statistics":
		if stepHandlersErrors.GetStatisticsError {
			writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}
		if from == "System" {
			returnJSONFile("features", "get_system_statistics.json", w, nil)
		} else if from == "StoragePool" {
			returnJSONFile("features", "get_storage_pool_statistics.json", w, nil)
		} else if from == "Volume" {
			if stepHandlersErrors.PodmonVolumeStatisticsError {
				writeError(w, "PodmonVolumeStatisticsError", http.StatusRequestTimeout, codes.Internal)
				return
			}
			returnJSONFile("features", "get_volume_statistics.json", w, nil)
		} else {
			writeError(w, "Unsupported relationship from type", http.StatusRequestTimeout, codes.Internal)
		}
	case "ProtectionDomain":
		if inducedError.Error() == "NoProtectionDomainError" {
			writeError(w, "induced error NoProtectionDomainError", http.StatusRequestTimeout, codes.Internal)
			return
		}

		if from == "System" {
			returnJSONFile("features", "get_system_instances.json", w, nil)
		}
	case "ReplicationPair":
		if inducedError.Error() == "GetReplicationPairError" {
			writeError(w, "GET ReplicationPair induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}

		instances := make([]*types.ReplicationPair, 0)

		for _, pair := range systemArrays[r.Host].replicationPairs {
			replacementMap := make(map[string]string)
			replacementMap["__ID__"] = pair["id"]
			replacementMap["__NAME__"] = pair["name"]
			replacementMap["__SOURCE_VOLUME__"] = pair["localVolumeId"]
			replacementMap["__DESTINATION_VOLUME__"] = pair["remoteVolumeId"]
			replacementMap["__RP_GROUP__"] = pair["replicationConsistencyGroupId"]

			data := returnJSONFile("features", "replication_pair.template", nil, replacementMap)
			pair := new(types.ReplicationPair)
			err := json.Unmarshal(data, pair)
			if err != nil {
				log.Printf("error unmarshalling json: %s\n", string(data))
			}
			log.Printf("pair +%v", pair)
			instances = append(instances, pair)
		}

		encoder := json.NewEncoder(w)
		err := encoder.Encode(instances)
		if err != nil {
			log.Printf("error encoding json: %s\n", err)
		}
	default:
		writeError(w, "Unsupported relationship to type", http.StatusRequestTimeout, codes.Internal)
	}
}

// handleInstances will retrieve specific instances
func handleInstances(w http.ResponseWriter, r *http.Request) {
	if stepHandlersErrors.BadVolIDError {
		writeError(w, "id must be a hexadecimal number", http.StatusRequestTimeout, codes.InvalidArgument)
		return
	}

	if stepHandlersErrors.GetVolByIDError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}
	if stepHandlersErrors.NoVolumeIDError {
		writeError(w, "volume ID is required", http.StatusRequestTimeout, codes.InvalidArgument)
		return
	}

	if stepHandlersErrors.SIOGatewayVolumeNotFoundError || inducedError.Error() == "TargetVolumeAlreadyDeleted" {
		writeError(w, sioGatewayVolumeNotFound, http.StatusRequestTimeout, codes.Internal)
		return
	}

	vars := mux.Vars(r)
	objType := vars["type"]
	id := vars["id"]
	id = extractIDFromStruct(id)
	if true {
		log.Printf("handle instances type %s id %s\n", objType, id)
	}
	switch objType {
	case "Volume":
		if id != "9999" {
			// Insert to map if it doesn't exist.
			if volumeIDToName[id] == "" {
				volumeIDToName[id] = "vol" + id
				volumeIDToSizeInKB[id] = defaultVolumeSize
				volumeIDToReplicationState[id] = unmarkedForReplication
			}

			replacementMap := make(map[string]string)
			vol := make(map[string]string)
			if array, ok := systemArrays[r.Host]; ok {
				vol = array.volumes[id]
			}

			log.Printf("Get id %s for %s\n", id, objType)
			if vol != nil {
				replacementMap["__ID__"] = vol["id"]
				replacementMap["__NAME__"] = vol["name"]
				replacementMap["__MAPPED_SDC_INFO__"] = getSdcMappings(id)
				replacementMap["__ANCESTOR_ID__"] = vol["ancestorVolumeId"]
				replacementMap["__CONSISTENCY_GROUP_ID__"] = vol["consistencyGroupID"]
				replacementMap["__SIZE_IN_KB__"] = vol["sizeInKb"]
				replacementMap["__VOLUME_REPLICATION_STATE__"] = vol["volumeReplicationState"]
			} else {
				replacementMap["__ID__"] = id
				replacementMap["__NAME__"] = volumeIDToName[id]
				replacementMap["__MAPPED_SDC_INFO__"] = getSdcMappings(id)
				replacementMap["__ANCESTOR_ID__"] = volumeIDToAncestorID[id]
				replacementMap["__CONSISTENCY_GROUP_ID__"] = volumeIDToConsistencyGroupID[id]
				replacementMap["__SIZE_IN_KB__"] = volumeIDToSizeInKB[id]
				replacementMap["__VOLUME_REPLICATION_STATE__"] = volumeIDToReplicationState[id]
			}
			returnJSONFile("features", "volume.json.template", w, replacementMap)
		} else {
			log.Printf("Did not find id %s for %s\n", id, objType)
			writeError(w, "volume not found: "+id, http.StatusNotFound, codes.NotFound)
		}

	case "ReplicationConsistencyGroup":
		if inducedError.Error() == "GetRCGByIdError" {
			writeError(w, "could not GET RCG by ID", http.StatusRequestTimeout, codes.Internal)
			return
		}

		group := systemArrays[r.Host].replicationConsistencyGroups[id]

		replacementMap := make(map[string]string)
		replacementMap["__ID__"] = group["id"]
		replacementMap["__NAME__"] = group["name"]
		replacementMap["__MODE__"] = replicationGroupConsistMode
		replacementMap["__PROTECTION_DOMAIN__"] = group["protectionDomainId"]
		replacementMap["__RM_PROTECTION_DOMAIN__"] = group["remoteProtectionDomainId"]
		replacementMap["__REP_DIR__"] = group["replicationDirection"]

		if replicationGroupState == "Normal" {
			replacementMap["__STATE__"] = "Ok"
		} else {
			replacementMap["__STATE__"] = "StoppedByUser"
			if replicationGroupState == "Failover" {
				replacementMap["__FO_TYPE__"] = "Failover"
				replacementMap["__FO_STATE__"] = "Done"
			} else if replicationGroupState == "Paused" {
				replacementMap["__P_MODE__"] = "Paused"
			}
		}

		returnJSONFile("features", "replication_consistency_group.template", w, replacementMap)

	}
}

// There are times when a struct {"id":"01234567890"} is sent for an id.
// This function extracts the id value
func extractIDFromStruct(id string) string {
	if !strings.HasPrefix(id, "{") {
		return id
	}
	// handle {"id":"012345678"} which seems to be passed in for this at times
	id = strings.Replace(id, "\"id\"", "", 1)
	id = strings.Replace(id, "{", "", 1)
	id = strings.Replace(id, "}", "", 1)
	id = strings.Replace(id, ":", "", 1)
	id = strings.Replace(id, "\"", "", -1)
	id = strings.Replace(id, "\n", "", -1)
	id = strings.Replace(id, " ", "", -1)
	return id
}

// Retrieve a volume by name
func handleQueryVolumeIDByKey(w http.ResponseWriter, r *http.Request) {
	if stepHandlersErrors.FindVolumeIDError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}
	req := new(types.VolumeQeryIDByKeyParam)
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&req)
	if err != nil {
		log.Printf("error decoding json: %s\n", err.Error())
	}
	if volumeNameToID[req.Name] != "" {
		resp := new(types.VolumeResp)
		resp.ID = volumeNameToID[req.Name]
		log.Printf("found volume %s id %s\n", req.Name, volumeNameToID[req.Name])
		encoder := json.NewEncoder(w)
		if stepHandlersErrors.BadVolIDJSON {
			err = encoder.Encode("thisWill://causeUnmarshalErr")
		} else {
			err = encoder.Encode(resp.ID)
		}
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}
	} else {
		log.Printf("did not find volume %s\n", req.Name)
		volumeNameToID[req.Name] = ""
		writeError(w, fmt.Sprintf("Volume not found %s", req.Name), http.StatusNotFound, codes.NotFound)

	}
}

func handleReplicationConsistencyGroupInstances(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if inducedError.Error() == "ReplicationConsistencyGroupError" {
			writeError(w, "create rcg induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}

		req := types.ReplicationConsistencyGroupCreatePayload{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}

		fmt.Printf("POST to ReplicationConsistencyGroup %s\n", req.Name)
		for _, ctx := range systemArrays[r.Host].replicationConsistencyGroups {
			if ctx["name"] == req.Name {
				w.WriteHeader(http.StatusInternalServerError)
				log.Printf("request for rcg creation of duplicate name: %s\n", req.Name)
				resp := types.Error{
					Message:        "The Replication Consistency Group already exists",
					HTTPStatusCode: http.StatusInternalServerError, ErrorCode: 6,
				}
				encoder := json.NewEncoder(w)
				err = encoder.Encode(resp)
				if err != nil {
					log.Printf("error encoding json: %s\n", err.Error())
				}
				return
			}
		}

		resp := new(types.ReplicationConsistencyGroup)
		resp.ID = hex.EncodeToString([]byte(req.Name))
		fmt.Printf("Generated rcg ID %s Name %s\n", resp.ID, req.Name)

		var array *systemArray

		// Add local rcg.
		array = systemArrays[r.Host]
		array.replicationConsistencyGroups[resp.ID] = make(map[string]string)
		array.replicationConsistencyGroups[resp.ID]["name"] = req.Name
		array.replicationConsistencyGroups[resp.ID]["id"] = resp.ID
		array.replicationConsistencyGroups[resp.ID]["remoteId"] = remoteRCGID
		array.replicationConsistencyGroups[resp.ID]["protectionDomainId"] = req.ProtectionDomainID
		array.replicationConsistencyGroups[resp.ID]["remoteProtectionDomainId"] = req.RemoteProtectionDomainID
		array.replicationConsistencyGroups[resp.ID]["rpoInSeconds"] = req.RpoInSeconds
		array.replicationConsistencyGroups[resp.ID]["remoteMdmId"] = req.DestinationSystemID
		array.replicationConsistencyGroups[resp.ID]["replicationDirection"] = "LocalToRemote"

		array = array.replicationSystem
		array.replicationConsistencyGroups[remoteRCGID] = make(map[string]string)
		array.replicationConsistencyGroups[remoteRCGID]["name"] = "rem-" + req.Name
		array.replicationConsistencyGroups[remoteRCGID]["id"] = remoteRCGID
		array.replicationConsistencyGroups[remoteRCGID]["remoteId"] = resp.ID
		array.replicationConsistencyGroups[remoteRCGID]["protectionDomainId"] = req.RemoteProtectionDomainID
		array.replicationConsistencyGroups[remoteRCGID]["remoteProtectionDomainId"] = req.ProtectionDomainID
		array.replicationConsistencyGroups[remoteRCGID]["rpoInSeconds"] = req.RpoInSeconds
		array.replicationConsistencyGroups[remoteRCGID]["remoteMdmId"] = array.ID
		array.replicationConsistencyGroups[remoteRCGID]["replicationDirection"] = "RemoteToLocal"

		if debug {
			log.Printf("request name: %s id: %s\n", req.Name, resp.ID)
		}

		if inducedError.Error() == "StorageGroupAlreadyExists" || inducedError.Error() == "StorageGroupAlreadyExistsUnretriavable" {
			writeError(w, "The Replication Consistency Group already exists", http.StatusRequestTimeout, codes.Internal)
			return
		}

		encoder := json.NewEncoder(w)
		err = encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}
	case http.MethodGet:
		if inducedError.Error() == "GetReplicationConsistencyGroupsError" {
			writeError(w, "could not GET ReplicationConsistencyGroups", http.StatusRequestTimeout, codes.Internal)
			return
		}

		instances := make([]*types.ReplicationConsistencyGroup, 0)
		for _, group := range systemArrays[r.Host].replicationConsistencyGroups {
			if inducedError.Error() == "StorageGroupAlreadyExistsUnretriavable" {
				continue
			}

			replacementMap := make(map[string]string)
			replacementMap["__ID__"] = group["id"]

			if inducedError.Error() == "RemoteRCGBadNameError" {
				replacementMap["__NAME__"] = "xxx"
			} else {
				replacementMap["__NAME__"] = group["name"]
			}

			replacementMap["__MODE__"] = replicationGroupConsistMode
			replacementMap["__PROTECTION_DOMAIN__"] = group["protectionDomainId"]
			replacementMap["__RM_PROTECTION_DOMAIN__"] = group["remoteProtectionDomainId"]
			replacementMap["__REP_DIR__"] = group["replicationDirection"]

			data := returnJSONFile("features", "replication_consistency_group.template", nil, replacementMap)

			fmt.Printf("RCG data %s\n", string(data))
			rcg := new(types.ReplicationConsistencyGroup)
			err := json.Unmarshal(data, rcg)
			if err != nil {
				log.Printf("error unmarshalling json: %s\n", string(data))
			}

			instances = append(instances, rcg)
		}

		encoder := json.NewEncoder(w)
		err := encoder.Encode(instances)
		if err != nil {
			log.Printf("error encoding json: %s\n", err)
		}

	}
}

func handleReplicationPairInstances(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if inducedError.Error() == "ReplicationPairError" {
			writeError(w, "POST ReplicationPair induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}
		req := types.QueryReplicationPair{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		fmt.Printf("POST to ReplicationPair %s Request %+v\n", req.Name, req)
		for _, ctx := range systemArrays[r.Host].replicationPairs {
			if ctx["name"] == req.Name {
				w.WriteHeader(http.StatusInternalServerError)
				log.Printf("request for replication pair creation of duplicate name: %s\n", req.Name)

				resp := new(types.Error)
				resp.Message = "Replication Pair name already in use"
				resp.HTTPStatusCode = http.StatusInternalServerError
				resp.ErrorCode = 6
				encoder := json.NewEncoder(w)
				err = encoder.Encode(resp)
				if err != nil {
					log.Printf("error encoding json: %s\n", err.Error())
				}
				return
			}
		}

		resp := new(types.ReplicationPair)
		resp.ID = hex.EncodeToString([]byte(req.Name))
		fmt.Printf("Generated replicationPair ID %s Name %s Struct %+v\n", resp.ID, req.Name, req)

		var array *systemArray
		array = systemArrays[r.Host]
		array.replicationPairs[resp.ID] = make(map[string]string)
		array.replicationPairs[resp.ID]["name"] = req.Name
		array.replicationPairs[resp.ID]["id"] = resp.ID
		array.replicationPairs[resp.ID]["localVolumeId"] = req.SourceVolumeID
		array.replicationPairs[resp.ID]["remoteVolumeId"] = req.DestinationVolumeID
		array.replicationPairs[resp.ID]["replicationConsistencyGroupId"] = req.ReplicationConsistencyGroupID

		// set replicated on volumes.
		array.volumes[req.SourceVolumeID]["volumeReplicationState"] = "Replicated"

		array = array.replicationSystem
		array.replicationPairs[resp.ID] = make(map[string]string)
		array.replicationPairs[resp.ID]["name"] = "rp-" + req.Name
		array.replicationPairs[resp.ID]["id"] = resp.ID
		array.replicationPairs[resp.ID]["localVolumeId"] = req.DestinationVolumeID
		array.replicationPairs[resp.ID]["remoteVolumeId"] = req.SourceVolumeID
		array.replicationPairs[resp.ID]["replicationConsistencyGroupId"] = array.replicationConsistencyGroups[remoteRCGID]["id"]

		array.volumes[req.DestinationVolumeID]["volumeReplicationState"] = "Replicated"

		volumeIDToReplicationState[req.SourceVolumeID] = "Replicated"
		volumeIDToReplicationState[req.DestinationVolumeID] = "Replicated"

		if debug {
			log.Printf("request name: %s id: %s sourceVolume %s\n", req.Name, resp.ID, req.SourceVolumeID)
		}

		if inducedError.Error() == "ReplicationPairAlreadyExists" || inducedError.Error() == "ReplicationPairAlreadyExistsUnretrievable" {
			writeError(w, "A Replication Pair for the specified local volume already exists", http.StatusRequestTimeout, codes.Internal)
			return
		}

		encoder := json.NewEncoder(w)
		err = encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}
	case http.MethodGet:
		if inducedError.Error() == "GetReplicationPairError" {
			writeError(w, "GET ReplicationPair induced error", http.StatusRequestTimeout, codes.Internal)
			return
		}

		instances := make([]*types.ReplicationPair, 0)
		for _, pair := range systemArrays[r.Host].replicationPairs {
			if inducedError.Error() == "ReplicationPairAlreadyExistsUnretrievable" {
				continue
			}

			replacementMap := make(map[string]string)
			replacementMap["__ID__"] = pair["id"]
			replacementMap["__NAME__"] = pair["name"]
			replacementMap["__SOURCE_VOLUME__"] = pair["localVolumeId"]
			replacementMap["__DESTINATION_VOLUME__"] = pair["remoteVolumeId"]
			replacementMap["__RP_GROUP__"] = pair["replicationConsistencyGroupId"]

			log.Printf("replicatPair replacementMap %v\n", replacementMap)
			data := returnJSONFile("features", "replication_pair.template", nil, replacementMap)

			log.Printf("replication-pair-data %s\n", string(data))
			pair := new(types.ReplicationPair)
			err := json.Unmarshal(data, pair)
			if err != nil {
				log.Printf("error unmarshalling json: %s\n", string(data))
			}

			log.Printf("replication-pair +%v", pair)
			instances = append(instances, pair)
		}

		encoder := json.NewEncoder(w)
		err := encoder.Encode(instances)
		if err != nil {
			log.Printf("error encoding json: %s\n", err)
		}
	}
}

func handleFileTreeQuotas(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if inducedError.Error() == "CreateQuotaError" {
			writeError(w, "error creating tree quota", http.StatusRequestTimeout, codes.Internal)
			return
		}
		req := types.TreeQuotaCreate{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}

		// good response
		resp := new(types.TreeQuotaCreateResponse)
		resp.ID = hex.EncodeToString([]byte("dummy-name"))
		treeQuotaID[resp.ID] = resp.ID
		treeQuotaIDToPath[resp.ID] = req.Path
		treeQuotaIDToSoftLimit[resp.ID] = strconv.Itoa(req.SoftLimit)
		treeQuotaIDToGracePeriod[resp.ID] = strconv.Itoa(req.GracePeriod)
		treeQuotaIDToHardLimit[resp.ID] = strconv.Itoa(req.HardLimit)

		if array, ok := systemArrays[r.Host]; ok {
			fmt.Printf("Host Endpoint %s\n", r.Host)
			array.treeQuotas[resp.ID] = make(map[string]string)
			array.treeQuotas[resp.ID]["id"] = resp.ID
			array.treeQuotas[resp.ID]["path"] = req.Path
			array.treeQuotas[resp.ID]["description"] = req.Description
			array.treeQuotas[resp.ID]["softlimit"] = strconv.Itoa(req.SoftLimit)
			array.treeQuotas[resp.ID]["graceperiod"] = strconv.Itoa(req.GracePeriod)
			array.treeQuotas[resp.ID]["hardlimit"] = strconv.Itoa(req.HardLimit)
		}
		if debug {
			log.Printf("request \"dummy-name\" id: %s\n", resp.ID)
		}
		encoder := json.NewEncoder(w)
		err = encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}
		log.Printf("end make tree quotas")
	case http.MethodGet:
		if inducedError.Error() == "GetQuotaByFSIDError" {
			writeError(w, "Fetching tree quota for filesystem failed, error:", http.StatusRequestTimeout, codes.Internal)
			return
		}
		instances := make([]*types.TreeQuota, 0)
		treeQuotas := make(map[string]map[string]string)

		if array, ok := systemArrays[r.Host]; ok {
			treeQuotas = array.treeQuotas

			for _, tq := range treeQuotas {
				replacementMap := make(map[string]string)
				replacementMap["__ID__"] = tq["id"]
				replacementMap["__PATH__"] = tq["path"]
				replacementMap["__HARD_LIMIT_SIZE__"] = tq["hardlimit"]
				replacementMap["__SOFT_LIMIT_SIZE__"] = tq["softlimit"]
				replacementMap["__GRACE_PERIOD__"] = tq["graceperiod"]
				data := returnJSONFile("features", "treequota.json.template", nil, replacementMap)
				tq := new(types.TreeQuota)
				err := json.Unmarshal(data, tq)
				if err != nil {
					log.Printf("error unmarshalling json: %s\n", string(data))
				}
				instances = append(instances, tq)
			}
		}

		// Add none-created volumes (old)
		for id := range treeQuotaID {
			if _, ok := treeQuotaID[id]; ok {
				continue
			}

			replacementMap := make(map[string]string)
			replacementMap["__ID__"] = id
			replacementMap["__PATH__"] = treeQuotaIDToPath["path"]
			replacementMap["__HARD_LIMIT_SIZE__"] = treeQuotaIDToHardLimit["hardlimit"]
			replacementMap["__SOFT_LIMIT_SIZE__"] = treeQuotaIDToSoftLimit["softlimit"]
			replacementMap["__GRACE_PERIOD__"] = treeQuotaIDToGracePeriod["graceperiod"]
			data := returnJSONFile("features", "filesystem.json.template", nil, replacementMap)
			tq := new(types.TreeQuota)
			err := json.Unmarshal(data, tq)
			if err != nil {
				log.Printf("error unmarshalling json: %s\n", string(data))
			}
			instances = append(instances, tq)
		}

		encoder := json.NewEncoder(w)
		err := encoder.Encode(instances)
		if err != nil {
			log.Printf("error encoding json: %s\n", err)
		}
		log.Printf("end get tree quotas")
	}
}

func handleGetFileTreeQuotas(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPatch:
		if inducedError.Error() == "ModifyQuotaError" {
			writeError(w, "Modifying tree quota for filesystem failed, error:", http.StatusRequestTimeout, codes.Internal)
			return
		}
		vars := mux.Vars(r)
		id := vars["id"]
		fmt.Println("id:", id)

		req := types.TreeQuotaModify{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&req)
		if err != nil {
			log.Printf("error decoding json: %s\n", err.Error())
		}
		fmt.Printf("patchReq:%#v\n", req)
		if array, ok := systemArrays[r.Host]; ok {
			array.treeQuotas[id]["description"] = req.Description
			array.treeQuotas[id]["softlimit"] = strconv.Itoa(req.SoftLimit)
			array.treeQuotas[id]["graceperiod"] = strconv.Itoa(req.GracePeriod)
			array.treeQuotas[id]["hardlimit"] = strconv.Itoa(req.HardLimit)
		}
		w.WriteHeader(http.StatusNoContent)
		log.Printf("end modify tree quotas")
	}
}

// Write an error code to the response writer
func writeError(w http.ResponseWriter, message string, httpStatus int, errorCode codes.Code) {
	w.WriteHeader(httpStatus)
	resp := new(types.Error)
	resp.Message = message
	resp.HTTPStatusCode = http.StatusNotFound
	resp.ErrorCode = int(errorCode)
	encoder := json.NewEncoder(w)
	err := encoder.Encode(resp)
	if err != nil {
		log.Printf("error encoding json: %s\n", err.Error())
	}
}
