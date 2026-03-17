// Copyright © 2021-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"fmt"

	podmon "github.com/dell/dell-csi-extensions/podmon"
	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// ExistingGroupID group id on powerflex array
	ExistingGroupID = "existingSnapshotGroupID"
	// ArrayStatus is the endPoint for polling to check array status
	ArrayStatus = "/array-status"
)

func (s *service) ValidateVolumeHostConnectivity(ctx context.Context, req *podmon.ValidateVolumeHostConnectivityRequest) (*podmon.ValidateVolumeHostConnectivityResponse, error) {
	log.Infof("ValidateVolumeHostConnectivity called %+v", req)
	rep := &podmon.ValidateVolumeHostConnectivityResponse{
		Messages: make([]string, 0),
	}

	if (len(req.GetVolumeIds()) == 0 || len(req.GetVolumeIds()) == 0) && req.GetNodeId() == "" {
		// This is a nop call just testing the interface is present
		rep.Messages = append(rep.Messages, "ValidateVolumeHostConnectivity is implemented")
		return rep, nil
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "The NodeID is a required field")
	}

	systemID := req.GetArrayId()
	if systemID == "" {
		if len(req.GetVolumeIds()) > 0 {
			systemID = s.getSystemIDFromCsiVolumeID(req.GetVolumeIds()[0])
		}
		if systemID == "" {
			systemID = s.opts.defaultSystemID
		}
	}

	// Do a probe of the requested system
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	// First- check to see if the host is Connected or Disconnected.
	_, hostType, err := s.getHostIDAndType(systemID, nodeID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error getting host ID and type for nodeID %s: %s", nodeID, err.Error())
	}
	if hostType == NVMeTCP {
		var message string
		rep.Connected = false
		nodeIP := s.GetNodeIPByCSINodeID(nodeID)
		if len(nodeIP) == 0 {
			log.Errorf("could not resolve IP address for nodeID=%s", nodeID)
			return nil, fmt.Errorf("failed to resolve IP address for nodeID=%s", nodeID)
		}

		// Query the podmon API on the node to get the connectivity status
		url := "http://" + nodeIP + s.opts.PodmonPort + ArrayStatus + "/" + systemID
		connected, err := s.QueryArrayStatus(ctx, url)
		if err != nil {
			message = fmt.Sprintf("connectivity unknown for array %s to node %s due to %s", systemID, nodeID, err)
			log.Error(message)
			rep.Messages = append(rep.Messages, message)
			log.Errorf("%s", err.Error())
		}

		if connected {
			rep.Connected = true
			message = fmt.Sprintf("array %s is connected to node %s", systemID, nodeID)
		} else {
			message = fmt.Sprintf("array %s is not connected to node %s", systemID, nodeID)
		}
		log.Info(message)
		rep.Messages = append(rep.Messages, message)
	} else {
		sdc, err := s.systems[systemID].FindSdc("SdcGUID", req.GetNodeId())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "NodeID is invalid: %s - there is no corresponding SDC, error: %s", req.GetNodeId(), err.Error())
		}
		connectionState := sdc.Sdc.MdmConnectionState
		rep.Messages = append(rep.Messages, fmt.Sprintf("SDC connection state: %s", connectionState))
		rep.Connected = (connectionState == "Connected")
	}

	// Second- check to see if the Volumes have any I/O in the recent past.
	for _, volID := range req.GetVolumeIds() {
		// Probe system
		prevSystemID := systemID
		systemID = s.getSystemIDFromCsiVolumeID(volID)
		if systemID == "" {
			systemID = s.opts.defaultSystemID
		}
		if prevSystemID != systemID {
			if err := s.requireProbe(ctx, systemID); err != nil {
				rep.Messages = append(rep.Messages, fmt.Sprintf("Could not probe system: %s, error: %s", volID, err.Error()))
				continue
			}
		}
		// Get the Volume
		vol, err := s.getVolByID(getVolumeIDFromCsiVolumeID(volID), systemID)
		if err != nil {
			rep.Messages = append(rep.Messages, fmt.Sprintf("Could not retrieve volume: %s, error: %s", volID, err.Error()))
			continue
		}

		adminClient := s.adminClients[systemID]

		// Get the volume statistics
		volume := sio.NewVolume(adminClient)
		volume.Volume = vol

		platformInfo, err := s.GetPlatformInfo(systemID)
		if err != nil {
			return nil, err
		}

		if platformInfo.GenType == siotypes.GenTypeEC {
			log.Infof("Found Gentype EC system %s", systemID)
			metrics, err := adminClient.GetMetrics("volume", []string{volID})
			if err != nil {
				rep.Messages = append(rep.Messages, fmt.Sprintf("Could not retrieve volume statistics: %s, error: %s", volID, err.Error()))
				continue
			}

			if len(metrics.Resources) == 0 {
				rep.Messages = append(rep.Messages, fmt.Sprintf("No metrics found for volume: %s", volID))
				continue
			}
			writeBW := getMetric(metrics.Resources[0].Metrics, "host_write_bandwidth")
			readBW := getMetric(metrics.Resources[0].Metrics, "host_read_bandwidth")
			writeIOPS := getMetric(metrics.Resources[0].Metrics, "host_write_iops")
			readIOPS := getMetric(metrics.Resources[0].Metrics, "host_read_iops")

			rep.Messages = append(rep.Messages, fmt.Sprintf("Volume %s writeBW %.2f readBw %.2f readIOPS %.2f writeIOPS %.2f",
				volID, writeBW, readBW, readIOPS, writeIOPS))
			if ((writeBW > 0) || (readBW > 0)) || ((writeIOPS > 0) || (readIOPS > 0)) {
				rep.IosInProgress = true
			}
		} else {
			log.Infof("Found Legacy system %s", systemID)
			stats, err := volume.GetVolumeStatistics()
			if err != nil {
				rep.Messages = append(rep.Messages, fmt.Sprintf("Could not retrieve volume statistics: %s, error: %s", volID, err.Error()))
				continue
			}
			readCount := stats.UserDataReadBwc.NumOccured
			writeCount := stats.UserDataWriteBwc.NumOccured
			sampleSeconds := stats.UserDataWriteBwc.NumSeconds
			rep.Messages = append(rep.Messages, fmt.Sprintf("Volume %s writes %d reads %d for %d seconds",
				volID, writeCount, readCount, sampleSeconds))
			if (readCount + writeCount) > 0 {
				rep.IosInProgress = true
			}
		}
	}

	log.Infof("ValidateVolumeHostConnectivity reply %+v", rep)
	return rep, nil
}

func getMetric(metrics []siotypes.Metric, name string) float64 {
	for _, m := range metrics {
		if m.Name == name {
			return m.Values[0]
		}
	}
	return 0
}
