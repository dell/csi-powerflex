// Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"net"
	"sort"
	"strings"

	"github.com/dell/gobrick"
	"github.com/dell/gonvme"
	"github.com/dell/goscaleio"
)

const (
	oui           = "64b94e" // Dell/PowerFlex OUI
	volumeIDLen   = 16       // 8 bytes
	clusterLSBLen = 10       // 5 bytes
	nguidLen      = 32       // 16 bytes
)

// NVMEConnector is wrapper of gobrick.NVMEConnector interface.
// It allows to connect NVMe volumes to the node.
type NVMEConnector interface {
	ConnectVolume(ctx context.Context, info gobrick.NVMeVolumeInfo, useFC bool) (gobrick.Device, error)
	DisconnectVolumeByDeviceName(ctx context.Context, name string) error
	GetInitiatorName(ctx context.Context) ([]string, error)
}

// GetNVMETCPTargetsInfoFromStorage returns list of gobrick compatible NVME TCP targets by querying PowerFlex array
func getNVMETCPTargetsInfoFromStorage(system *goscaleio.System) ([]string, error) {
	allSdt, err := system.GetAllSdts()
	if err != nil {
		log.Infof("failed to get SDT from system %s : %v", system.System.ID, err)
		return nil, err
	} else if len(allSdt) == 0 {
		log.Infof("system %s returned empty SDT: %v", system.System.ID, allSdt)
		return nil, fmt.Errorf("system %s returned empty SDT", system.System.ID)
	}
	// sort data by id
	sort.Slice(allSdt, func(i, j int) bool {
		return allSdt[i].ID < allSdt[j].ID
	})
	var portals []string
	for _, t := range allSdt {
		if len(t.IPList) == 0 || t.IPList[0].IP == "" {
			log.Debugf("SDT %s has no IPs; skipping", t.ID)
			continue
		}
		portals = append(portals, net.JoinHostPort(t.IPList[0].IP, "4420"))
	}
	return portals, nil
}

func (s *service) discoverNVMeTargets(system *goscaleio.System) ([]gonvme.NVMeTarget, error) {
	portals, err := getNVMETCPTargetsInfoFromStorage(system)
	if err != nil {
		return nil, fmt.Errorf("failed to get targets from array: %w", err)
	}

	discoveredTargets := make(map[string]gonvme.NVMeTarget)
	for _, portal := range portals {
		var ip string
		ip, _, err = net.SplitHostPort(portal)
		if err != nil {
			log.Infof("Invalid portal format, skipping: %s (%v)", portal, err)
			continue
		}
		if _, exists := discoveredTargets[ip]; exists {
			continue
		}
		log.Infof("Trying to discover NVMe target from portal %s", ip)
		var targets []gonvme.NVMeTarget
		targets, err = s.nvmeLib.DiscoverNVMeTCPTargets(ip, false)
		if err != nil {
			log.Infof("couldn't discover targets with portal %s: %s", ip, err)
			continue
		}
		for _, target := range targets {
			discoveredTargets[target.Portal] = target
			s.nvmeTargetNqn[target.Portal] = target.TargetNqn
		}
	}

	if len(discoveredTargets) == 0 {
		log.Warnf("failed to discover NVMe targets for array %s", system.System.ID)
	}

	targets := make([]gonvme.NVMeTarget, 0, len(discoveredTargets))
	for _, target := range discoveredTargets {
		targets = append(targets, target)
	}

	return targets, err
}

func (s *service) connectToNVMeTargets(system *goscaleio.System, targets []gonvme.NVMeTarget) error {
	connected := false
	for _, t := range targets {
		log.Infof("Connecting to NVMe target %v", t)
		if err := s.nvmeLib.NVMeTCPConnect(t, false); err != nil {
			log.Errorf("couldn't connect to the nvme target %v: %s", t, err)
			continue
		}
		connected = true
	}
	if connected {
		return nil
	}
	return fmt.Errorf("failed to connect to any NVMe targets for array %s", system.System.ID)
}

// discoverAndConnectNVMeTargets handles the discovery and connection to NVMe targets for a given array.
func (s *service) discoverAndConnectNVMeTargets(system *goscaleio.System) error {
	targets, err := s.discoverNVMeTargets(system)
	if err != nil {
		return err
	}
	return s.connectToNVMeTargets(system, targets)
}

// BuildNGUID creates NGUID as:
// [VolumeID (16 chars)] + [OUI (6 chars)] + [ClusterID last 10 chars]
func buildNGUID(volumeID, clusterID string) (string, error) {
	vol := normalize(volumeID)
	cluster := normalize(clusterID)

	if len(vol) != volumeIDLen {
		return "", fmt.Errorf("volumeID must be %d characters, got %q", volumeIDLen, vol)
	}

	if len(cluster) < clusterLSBLen {
		return "", fmt.Errorf("clusterID must be at least %d characters, got %q", clusterLSBLen, cluster)
	}

	clusterLSB := cluster[len(cluster)-clusterLSBLen:]
	nguid := vol + oui + clusterLSB

	return nguid, nil
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
