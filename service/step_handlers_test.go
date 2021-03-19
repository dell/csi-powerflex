package service

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	types "github.com/dell/goscaleio/types/v1"
	"github.com/gorilla/mux"
	codes "google.golang.org/grpc/codes"
)

var (
	debug         bool
	sdcMappings   []types.MappedSdcInfo
	sdcMappingsID string

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
		GetSdcInstancesError          bool
		MapSdcError                   bool
		RemoveMappedSdcError          bool
		SIOGatewayVolumeNotFoundError bool
		GetStatisticsError            bool
		CreateSnapshotError           bool
		RemoveVolumeError             bool
		VolumeInstancesError          bool
		BadVolIDError                 bool
		WrongVolIDError               bool
		WrongSystemError              bool
		NoEndpointError               bool
		NoUserError                   bool
		NoPasswordError               bool
		NoSysNameError                bool
		NoAdminError                  bool
		WrongSysNameError             bool
		NoVolumeIDError               bool
		SetVolumeSizeError            bool
		systemNameMatchingError       bool
		LegacyVolumeConflictError     bool
		VolumeIDTooShortError         bool
	}
)

// This file contains HTTP handlers for mocking to the ScaleIO API.
// This allows unit testing with a Scale IO but still provides some coverage in the goscaleio library.
var scaleioRouter http.Handler
var testControllerHasNoConnection bool
var count int

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
	volumeIDToAncestorID = make(map[string]string)
	volumeNameToID = make(map[string]string)
	volumeIDToConsistencyGroupID = make(map[string]string)
	debug = false
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
	stepHandlersErrors.GetStatisticsError = false
	stepHandlersErrors.GetSystemSdcError = false
	stepHandlersErrors.CreateSnapshotError = false
	stepHandlersErrors.RemoveVolumeError = false
	stepHandlersErrors.VolumeInstancesError = false
	stepHandlersErrors.BadVolIDError = false
	stepHandlersErrors.WrongVolIDError = false
	stepHandlersErrors.WrongSystemError = false
	stepHandlersErrors.NoEndpointError = false
	stepHandlersErrors.NoUserError = false
	stepHandlersErrors.NoPasswordError = false
	stepHandlersErrors.NoSysNameError = false
	stepHandlersErrors.NoAdminError = false
	stepHandlersErrors.WrongSysNameError = false
	stepHandlersErrors.NoVolumeIDError = false
	stepHandlersErrors.SetVolumeSizeError = false
	stepHandlersErrors.LegacyVolumeConflictError = false
	stepHandlersErrors.VolumeIDTooShortError = false
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
	scaleioRouter.HandleFunc("/api/types/Volume/instances", handleVolumeInstances)
	scaleioRouter.HandleFunc("/api/types/StoragePool/instances", handleStoragePoolInstances)
	scaleioRouter.HandleFunc("{Volume}/relationship/Statistics", handleVolumeStatistics)
	scaleioRouter.HandleFunc("/api/Volume/relationship/Statistics", handleVolumeStatistics)
	scaleioRouter.HandleFunc("{SdcGuid}/relationships/Sdc", handleSystemSdc)
	return scaleioRouter
}

// handle implements GET /api/types/StoragePool/instances
func handleVolumeStatistics(w http.ResponseWriter, r *http.Request) {
	if stepHandlersErrors.PodmonVolumeStatisticsError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}
	returnJSONFile("features", "get_volume_statistics.json", w, nil)
}

func handleSystemSdc(w http.ResponseWriter, r *http.Request) {
	if stepHandlersErrors.GetSystemSdcError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}
	returnJSONFile("features", "get_sdc_instances.json", w, nil)
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
func handleVersion(w http.ResponseWriter, r *http.Request) {
	if testControllerHasNoConnection {
		w.WriteHeader(http.StatusRequestTimeout)
		return
	}
	w.Write([]byte("2.5"))
}

// handleSystemInstances implements GET /api/types/System/instances
func handleSystemInstances(w http.ResponseWriter, r *http.Request) {
	if stepHandlersErrors.PodmonNodeProbeError {
		writeError(w, "PodmonNodeProbeError", http.StatusRequestTimeout, codes.Internal)
		return
	}
	if stepHandlersErrors.PodmonControllerProbeError {
		writeError(w, "PodmonControllerProbeError", http.StatusRequestTimeout, codes.Internal)
		return
	}
	if stepHandlersErrors.systemNameMatchingError {
		count++
	}
	if count == 2 {
		fmt.Printf("DEBUG send bad system\n")
		returnJSONFile("features", "bad_system.json", w, nil)
		count = 0
	} else {
		returnJSONFile("features", "get_system_instances.json", w, nil)
	}
}

// handleStoragePoolInstances implements GET /api/types/StoragePool/instances
func handleStoragePoolInstances(w http.ResponseWriter, r *http.Request) {
	if stepHandlersErrors.GetStoragePoolsError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}
	returnJSONFile("features", "get_storage_pool_instances.json", w, nil)
}

func returnJSONFile(directory, filename string, w http.ResponseWriter, replacements map[string]string) (jsonBytes []byte) {
	jsonBytes, err := ioutil.ReadFile(filepath.Join(directory, filename))
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

// Map of volume name to ID
var volumeNameToID map[string]string

// Map of volume ID to ancestor ID
var volumeIDToAncestorID map[string]string

// Map of volume ID to consistency group ID
var volumeIDToConsistencyGroupID map[string]string

// handleVolumeInstances handles listing all volumes or creating a volume
func handleVolumeInstances(w http.ResponseWriter, r *http.Request) {
	if volumeIDToName == nil {
		volumeIDToName = make(map[string]string)
		volumeIDToAncestorID = make(map[string]string)
		volumeNameToID = make(map[string]string)
		volumeIDToConsistencyGroupID = make(map[string]string)
	}
	if stepHandlersErrors.VolumeInstancesError {
		writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
		return
	}
	switch r.Method {

	// Post is CreateVolume; here just return a volume id encoded from the name
	case http.MethodPost:
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
		if debug {
			log.Printf("request name: %s id: %s\n", req.Name, resp.ID)
		}
		encoder := json.NewEncoder(w)
		err = encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}

		log.Printf("end make volumes")
		break
	// Read all the Volumes
	case http.MethodGet:
		instances := make([]*types.Volume, 0)
		for id, name := range volumeIDToName {
			replacementMap := make(map[string]string)
			replacementMap["__ID__"] = id
			replacementMap["__NAME__"] = name
			replacementMap["__MAPPED_SDC_INFO__"] = getSdcMappings(id)
			replacementMap["__ANCESTOR_ID__"] = volumeIDToAncestorID[id]
			replacementMap["__CONSISTENCY_GROUP_ID__"] = volumeIDToConsistencyGroupID[id]
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
		break
	}
}

func handleAction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	from := vars["from"]
	id := vars["id"]
	action := vars["action"]
	log.Printf("action from %s id %s action %s", from, id, action)
	switch action {
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
			sdcMappings = append(sdcMappings, types.MappedSdcInfo{SdcID: req.SdcID, SdcIP: "10.247.102.218"})
		}
		fmt.Printf("SdcID: %s\n", req.SdcID)
		if req.SdcID == "d0f055aa00000001" {
			sdcMappings = append(sdcMappings, types.MappedSdcInfo{SdcID: req.SdcID, SdcIP: "10.247.102.151"})
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
			// For now3, only a single snapshot ID is supported

			id := "9999"
			if snapParam.SnapshotName == "clone" {
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
			volumeIDToConsistencyGroupID[id] = "null"
		}

		if stepHandlersErrors.WrongVolIDError {
			returnJSONFile("features", "create_snapshot2.json", w, nil)
		}

		returnJSONFile("features", "create_snapshot.json", w, nil)
	case "removeVolume":
		if stepHandlersErrors.RemoveVolumeError {
			writeError(w, "inducedError", http.StatusRequestTimeout, codes.Internal)
		}
		name := volumeIDToName[id]
		volumeIDToName[id] = ""
		volumeIDToAncestorID[id] = ""
		volumeIDToConsistencyGroupID[id] = ""
		if name != "" {
			volumeNameToID[name] = ""
		}
	case "setVolumeSize":
		if stepHandlersErrors.SetVolumeSizeError {
			writeError(w, "induced error", http.StatusRequestTimeout, codes.Internal)
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

	if stepHandlersErrors.SIOGatewayVolumeNotFoundError {
		writeError(w, "Could not find the volume", http.StatusRequestTimeout, codes.Internal)
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
			log.Printf("Found id %s for %s\n", id, objType)
			log.Printf("Found name = %s\n", volumeIDToName[id])
			replacementMap := make(map[string]string)
			replacementMap["__ID__"] = id
			replacementMap["__NAME__"] = volumeIDToName[id]
			replacementMap["__MAPPED_SDC_INFO__"] = getSdcMappings(id)
			replacementMap["__ANCESTOR_ID__"] = volumeIDToAncestorID[id]
			replacementMap["__CONSISTENCY_GROUP_ID__"] = volumeIDToConsistencyGroupID[id]
			returnJSONFile("features", "volume.json.template", w, replacementMap)
		} else {
			log.Printf("Did not find id %s for %s\n", id, objType)
			writeError(w, "volume not found: "+id, http.StatusNotFound, codes.NotFound)
		}
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

	req := new(types.VolumeQeryIdByKeyParam)
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
		err := encoder.Encode(resp)
		if err != nil {
			log.Printf("error encoding json: %s\n", err.Error())
		}
	} else {
		log.Printf("did not find volume %s\n", req.Name)
		volumeNameToID[req.Name] = ""
		writeError(w, fmt.Sprintf("Volume not found %s", req.Name), http.StatusNotFound, codes.NotFound)
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
