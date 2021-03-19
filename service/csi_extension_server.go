package service

import (
	"context"
	"fmt"

	podmon "github.com/dell/dell-csi-extensions/podmon"
	sio "github.com/dell/goscaleio"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

	// The NodeID for the VxFlex OS is the SdcGuid field.
	if req.GetNodeId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "The NodeID is a required field")
	}

	systemID := req.GetArrayId()
	if systemID == "" {
		if len(req.GetVolumeIds()) > 0 {
			systemID = getSystemIDFromCsiVolumeID(req.GetVolumeIds()[0])
		}
		if systemID == "" {
			systemID = s.opts.defaultSystemID
		}
	}

	// Do a probe of the requested system
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	// First- check to see if the SDC is Connected or Disconnected.
	// Then retrieve the SDC and seet the connection state
	sdc, err := s.systems[systemID].FindSdc("SdcGuid", req.GetNodeId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "NodeID is invalid: %s - there is no corresponding SDC", req.GetNodeId())
	}
	connectionState := sdc.Sdc.MdmConnectionState
	rep.Messages = append(rep.Messages, fmt.Sprintf("SDC connection state: %s", connectionState))
	rep.Connected = (connectionState == "Connected")

	// Second- check to see if the Volumes have any I/O in the recent past.
	for _, volID := range req.GetVolumeIds() {
		// Probe system
		prevSystemID := systemID
		systemID = getSystemIDFromCsiVolumeID(volID)
		if systemID == "" {
			systemID = s.opts.defaultSystemID
		}
		if prevSystemID != systemID {
			if err := s.requireProbe(ctx, systemID); err != nil {
				rep.Messages = append(rep.Messages, fmt.Sprintf("Could not probe system: %s", volID))
				continue
			}
		}
		// Get the Volume
		vol, err := s.getVolByID(getVolumeIDFromCsiVolumeID(volID), systemID)
		if err != nil {
			rep.Messages = append(rep.Messages, fmt.Sprintf("Could not retrieve volume: %s", volID))
			continue
		}
		// Get the volume statistics
		volume := sio.NewVolume(s.adminClients[systemID])
		volume.Volume = vol
		stats, err := volume.GetVolumeStatistics()
		if err != nil {
			rep.Messages = append(rep.Messages, fmt.Sprintf("Could not retrieve volume statistics: %s", volID))
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

	log.Infof("ValidateVolumeHostConnectivity reply %+v", rep)
	return rep, nil
}
