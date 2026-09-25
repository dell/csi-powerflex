// Copyright © 2024 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"io/fs"
	"net"
	"os"
	"strings"

	csmlog "github.com/Ecosystems/container-storage-modules/src/csmlog"
)

const (
	defaultNodeMdmsFile = "/data/node_mdms.txt"
)

// PreInitService is the interface for running pre-initialization service logic.
type PreInitService interface {
	PreInit() error
}

// NewPreInitService returns a new PreInitService instance.
func NewPreInitService() PreInitService {
	return &service{}
}

// ArrayConfigurationProvider provides array configuration data.
type ArrayConfigurationProvider interface {
	GetArrayConfiguration() ([]*ArrayConnectionData, error)
}

// FileWriterProvider provides an interface for writing files.
type FileWriterProvider interface {
	WriteFile(filename string, data []byte, perm os.FileMode) error
}

// DefaultArrayConfigurationProvider is the default implementation of ArrayConfigurationProvider.
type DefaultArrayConfigurationProvider struct{}

// GetArrayConfiguration returns the array configuration data.
func (s *DefaultArrayConfigurationProvider) GetArrayConfiguration() ([]*ArrayConnectionData, error) {
	arrayConfig, err := getArrayConfig(nil)
	if err != nil {
		return nil, err
	}

	connectionData := make([]*ArrayConnectionData, 0)
	for _, v := range arrayConfig {
		connectionData = append(connectionData, v)
	}

	return connectionData, nil
}

// DefaultFileWriterProvider is the default implementation of FileWriterProvider.
type DefaultFileWriterProvider struct{}

// WriteFile writes data to a file with the specified permissions.
func (s *DefaultFileWriterProvider) WriteFile(filename string, data []byte, perm os.FileMode) error {
	return os.WriteFile(filename, data, perm)
}

var (
	arrayConfigurationProviderImpl ArrayConfigurationProvider = &DefaultArrayConfigurationProvider{}
	fileWriterProviderImpl         FileWriterProvider         = &DefaultFileWriterProvider{}
	nodeMdmsFile                                              = defaultNodeMdmsFile
)

func (s *service) PreInit() error {
	csmlog.Infof("PreInit running")

	arrayConfig, err := arrayConfigurationProviderImpl.GetArrayConfiguration()
	if err != nil {
		return fmt.Errorf("unable to get array configuration: %v", err)
	}

	labelKey, err := getLabelKey(arrayConfig)
	if err != nil {
		return fmt.Errorf("unable to get zone label key: %v", err)
	}

	// Validate and trim MDM IPs for all arrays.
	for i := range arrayConfig {
		if arrayConfig[i].Mdm != "" {
			validated, err := validateAndTrimMDM(i, arrayConfig[i].SystemID, arrayConfig[i].Mdm)
			if err != nil {
				return err
			}
			arrayConfig[i].Mdm = validated
		} else {
			// Validate that arrays requiring MDM (SDC/auto protocol) have it configured
			proto := strings.ToLower(arrayConfig[i].BlockProtocol)
			if proto == "sdc" || proto == "auto" || proto == "" {
				errMsg := fmt.Sprintf("array at index %d (systemID: %s) requires MDM for blockProtocol %q but the 'mdm' field is not set in the config secret", i, arrayConfig[i].SystemID, arrayConfig[i].BlockProtocol)
				csmlog.Errorf(errMsg)
				return fmt.Errorf("%s", errMsg)
			}
		}
	}

	var mdmData string

	if labelKey == "" {
		csmlog.Debug("No zone key found, will configure all MDMs")
		sb := strings.Builder{}
		for _, connectionData := range arrayConfig {
			if connectionData.Mdm != "" {
				if sb.Len() > 0 {
					sb.WriteString("\\&")
				}
				sb.WriteString(connectionData.Mdm)
			}
		}
		mdmData = sb.String()
	} else {
		csmlog.Infof("Zone key detected, will configure MDMs for this node, key: %s", labelKey)
		nodeLabels, err := s.GetNodeLabels(context.Background())
		if err != nil {
			return fmt.Errorf("unable to get node labels: %v", err)
		}

		zone, ok := nodeLabels[labelKey]

		if ok && zone == "" {
			csmlog.Errorf("node key found but zone is missing, will not configure MDMs for this node, key: %s", labelKey)
		}

		if zone != "" {
			csmlog.Infof("zone found, will configure MDMs for this node, zone: %s", zone)
			mdmData, err = getMdmList(arrayConfig, labelKey, zone)
			if err != nil {
				return fmt.Errorf("unable to get MDM list: %v", err)
			}
		}
	}

	csmlog.Infof("Saving MDM list to %s for %d array(s)", nodeMdmsFile, len(arrayConfig))
	err = fileWriterProviderImpl.WriteFile(nodeMdmsFile, []byte(fmt.Sprintf("MDM=%s\n", mdmData)), fs.FileMode(0o444))
	return err
}

// Returns a string with the list of MDM addresses given a
// key and zone. The MDMs for each array is separated by an ampersand.
// The ordering of the MDM addresses is not guaranteed. An error is
// returned if either the key or zone are empty.
func getMdmList(connectionData []*ArrayConnectionData, key, zone string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("key is empty")
	}
	if zone == "" {
		return "", fmt.Errorf("zone is empty")
	}

	sb := &strings.Builder{}
	for _, connectionData := range connectionData {
		if connectionData.Mdm != "" && connectionData.AvailabilityZone != nil && connectionData.AvailabilityZone.LabelKey == key && string(connectionData.AvailabilityZone.Name) == zone {
			if sb.Len() > 0 {
				sb.WriteString("\\&")
			}
			sb.WriteString(connectionData.Mdm)
		}
	}

	return sb.String(), nil
}

// validateAndTrimMDM validates that each IP in the comma-separated MDM string
// is a valid IPv4 address. Leading/trailing whitespace is trimmed from each IP
// . Returns the trimmed MDM string or an error identifying the array
// index and invalid value.
func validateAndTrimMDM(index int, systemID, mdm string) (string, error) {
	ips := strings.Split(mdm, ",")
	trimmed := make([]string, 0, len(ips))
	for _, raw := range ips {
		ip := strings.TrimSpace(raw)
		if ip == "" {
			continue
		}
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.To4() == nil {
			errMsg := fmt.Sprintf("array at index %d (systemID: %s) has invalid MDM value %q; only numeric IPv4 addresses are accepted", index, systemID, ip)
			csmlog.Errorf(errMsg)
			return "", fmt.Errorf("%s", errMsg)
		}
		trimmed = append(trimmed, ip)
	}
	if len(trimmed) == 0 {
		csmlog.Errorf("array at index %d (systemID: %s) has no valid MDM addresses; only whitespace found", index, systemID)
		return "", fmt.Errorf("array at index %d (systemID: %s) has no valid MDM addresses; only whitespace found", index, systemID)
	}
	return strings.Join(trimmed, ","), nil
}

// Returns the label key for the given set of array configurations.
// It is expected that the value for labelKey is the same for all arrays.
// An empty string is returned if the labelKey is not present in all arrays.
// An error is returned if the key cannot be determined.
func getLabelKey(connectionData []*ArrayConnectionData) (string, error) {
	if len(connectionData) == 0 {
		return "", fmt.Errorf("array connection data is empty")
	}

	labelKey := ""
	if connectionData[0].AvailabilityZone != nil {
		labelKey = connectionData[0].AvailabilityZone.LabelKey
	}

	for _, v := range connectionData {
		if v.AvailabilityZone != nil && v.AvailabilityZone.LabelKey != labelKey {
			return "", fmt.Errorf("zone label key is not the same for all arrays")
		}
	}

	return labelKey, nil
}
