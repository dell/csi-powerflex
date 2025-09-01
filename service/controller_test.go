/*
 *
 * Copyright © 2021-2024 Dell Inc. or its subsidiaries. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

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
	"errors"
	"sync"
	"testing"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	"golang.org/x/net/context"
)

func Test_service_getZoneFromZoneLabelKey(t *testing.T) {
	type fields struct {
		opts                    Opts
		adminClients            map[string]*sio.Client
		systems                 map[string]*sio.System
		mode                    string
		volCache                []*siotypes.Volume
		volCacheSystemID        string
		snapCache               []*siotypes.Volume
		snapCacheSystemID       string
		privDir                 string
		storagePoolIDToName     map[string]string
		statisticsCounter       int
		volumePrefixToSystems   map[string][]string
		connectedSystemNameToID map[string]string
	}

	type args struct {
		ctx          context.Context
		zoneLabelKey string
	}

	const validTopologyKey = "topology.kubernetes.io/zone"
	const validZone = "zoneA"

	tests := map[string]struct {
		fields           fields
		args             args
		wantZone         string
		wantErr          bool
		getNodeLabelFunc func(ctx context.Context, s *service) (map[string]string, error)
	}{
		"success": {
			// happy path test
			args: args{
				ctx:          context.Background(),
				zoneLabelKey: validTopologyKey,
			},
			wantZone: "zoneA",
			wantErr:  false,
			getNodeLabelFunc: func(_ context.Context, _ *service) (map[string]string, error) {
				nodeLabels := map[string]string{validTopologyKey: validZone}
				return nodeLabels, nil
			},
		},
		"use bad zone label key": {
			// The key args.zoneLabelKey will not be found in the map returned by getNodeLabelFunc
			args: args{
				ctx:          context.Background(),
				zoneLabelKey: "badkey",
			},
			wantZone: "",
			wantErr:  true,
			getNodeLabelFunc: func(_ context.Context, _ *service) (map[string]string, error) {
				return nil, nil
			},
		},
		"fail to get node labels": {
			// getNodeLabelFunc will return an error, triggering failure to get the labels
			args: args{
				ctx:          context.Background(),
				zoneLabelKey: "unimportant",
			},
			wantZone: "",
			wantErr:  true,
			getNodeLabelFunc: func(_ context.Context, _ *service) (map[string]string, error) {
				return nil, errors.New("")
			},
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			s := &service{
				opts:                    tt.fields.opts,
				adminClients:            tt.fields.adminClients,
				systems:                 tt.fields.systems,
				mode:                    tt.fields.mode,
				volCache:                tt.fields.volCache,
				volCacheRWL:             sync.RWMutex{},
				volCacheSystemID:        tt.fields.volCacheSystemID,
				snapCache:               tt.fields.snapCache,
				snapCacheRWL:            sync.RWMutex{},
				snapCacheSystemID:       tt.fields.snapCacheSystemID,
				privDir:                 tt.fields.privDir,
				storagePoolIDToName:     tt.fields.storagePoolIDToName,
				statisticsCounter:       tt.fields.statisticsCounter,
				volumePrefixToSystems:   tt.fields.volumePrefixToSystems,
				connectedSystemNameToID: tt.fields.connectedSystemNameToID,
			}
			GetNodeLabels = tt.getNodeLabelFunc
			gotZone, err := s.getZoneFromZoneLabelKey(tt.args.ctx, tt.args.zoneLabelKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("service.getZoneFromZoneLabelKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotZone != tt.wantZone {
				t.Errorf("service.getZoneFromZoneLabelKey() = %v, want %v", gotZone, tt.wantZone)
			}
		})
	}
}

func Test_service_getSystemIDFromZoneLabelKey(t *testing.T) {
	type fields struct {
		opts                    Opts
		adminClients            map[string]*sio.Client
		systems                 map[string]*sio.System
		mode                    string
		volCache                []*siotypes.Volume
		volCacheSystemID        string
		snapCache               []*siotypes.Volume
		snapCacheSystemID       string
		privDir                 string
		storagePoolIDToName     map[string]string
		statisticsCounter       int
		volumePrefixToSystems   map[string][]string
		connectedSystemNameToID map[string]string
	}

	type args struct {
		req *csi.GetCapacityRequest
	}

	const validSystemID = "valid-id"
	const validTopologyKey = "topology.kubernetes.io/zone"
	const validZone = "zoneA"

	tests := map[string]struct {
		fields       fields
		args         args
		wantSystemID string
		wantErr      bool
	}{
		"success": {
			// happy path test
			wantErr:      false,
			wantSystemID: validSystemID,
			args: args{
				req: &csi.GetCapacityRequest{
					AccessibleTopology: &csi.Topology{
						Segments: map[string]string{
							validTopologyKey: validZone,
						},
					},
				},
			},
			fields: fields{
				opts: Opts{
					zoneLabelKey: validTopologyKey,
					arrays: map[string]*ArrayConnectionData{
						"array1": {
							SystemID: validSystemID,
							AvailabilityZone: &AvailabilityZone{
								Name: validZone,
							},
						},
					},
				},
			},
		},
		"topology not passed with csi request": {
			// should return an empty string if no topology info is passed
			// with the csi request
			wantErr:      false,
			wantSystemID: "",
			args: args{
				req: &csi.GetCapacityRequest{
					AccessibleTopology: &csi.Topology{
						// don't pass any topology info with the request
						Segments: map[string]string{},
					},
				},
			},
			fields: fields{
				opts: Opts{
					zoneLabelKey: validTopologyKey,
				},
			},
		},
		"zone name missing in secret": {
			// topology information in the csi request does not match
			// any of the arrays in the secret
			wantErr:      true,
			wantSystemID: "",
			args: args{
				req: &csi.GetCapacityRequest{
					AccessibleTopology: &csi.Topology{
						Segments: map[string]string{
							validTopologyKey: validZone,
						},
					},
				},
			},
			fields: fields{
				opts: Opts{
					zoneLabelKey: validTopologyKey,
					arrays: map[string]*ArrayConnectionData{
						"array1": {
							SystemID: validSystemID,
							AvailabilityZone: &AvailabilityZone{
								// ensure the zone name will not match the topology key value
								// in the request
								Name: validZone + "no-match",
							},
						},
					},
				},
			},
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			s := &service{
				opts:                    tt.fields.opts,
				adminClients:            tt.fields.adminClients,
				systems:                 tt.fields.systems,
				mode:                    tt.fields.mode,
				volCache:                tt.fields.volCache,
				volCacheRWL:             sync.RWMutex{},
				volCacheSystemID:        tt.fields.volCacheSystemID,
				snapCache:               tt.fields.snapCache,
				snapCacheRWL:            sync.RWMutex{},
				snapCacheSystemID:       tt.fields.snapCacheSystemID,
				privDir:                 tt.fields.privDir,
				storagePoolIDToName:     tt.fields.storagePoolIDToName,
				statisticsCounter:       tt.fields.statisticsCounter,
				volumePrefixToSystems:   tt.fields.volumePrefixToSystems,
				connectedSystemNameToID: tt.fields.connectedSystemNameToID,
			}
			gotSystemID, err := s.getSystemIDFromZoneLabelKey(tt.args.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("service.getSystemIDFromZoneLabelKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotSystemID != tt.wantSystemID {
				t.Errorf("service.getSystemIDFromZoneLabelKey() = %v, want %v", gotSystemID, tt.wantSystemID)
			}
		})
	}
}
