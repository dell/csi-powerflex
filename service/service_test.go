// Copyright © 2019-2024 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cucumber/godog"
	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestMain(m *testing.M) {
	server := &http.Server{
		Addr:              "localhost:6060",
		ReadHeaderTimeout: 60 * time.Second,
	}

	go server.ListenAndServe()
	fmt.Printf("starting godog...\n")

	opts := godog.Options{
		Format: "pretty",
		Paths:  []string{"features"},
		Tags:   "",
	}

	status := godog.TestSuite{
		Name:                "godog",
		ScenarioInitializer: FeatureContext,
		Options:             &opts,
	}.Run()

	fmt.Printf("godog finished\n")

	if st := m.Run(); st > status {
		fmt.Printf("godog.TestSuite status %d\n", status)
		fmt.Printf("m.Run status %d\n", st)
		status = st
	}

	fmt.Printf("status %d\n", status)

	os.Exit(status)
}

func Test_service_SetPodZoneLabel(t *testing.T) {
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
		ctx       context.Context
		zoneLabel map[string]string
	}

	const validZoneName = "zoneA"
	const validZoneLabelKey = "topology.kubernetes.io/zone"
	const validAppName = "test-node-pod"
	const validAppLabelKey = "app"
	const validNodeName = "kube-node-name"
	validAppLabels := map[string]string{validAppLabelKey: validAppName}

	tests := []struct {
		name     string
		fields   fields
		args     args
		initTest func(s *service)
		wantErr  bool
	}{
		{
			// happy path test
			name:    "add zone labels to a pod",
			wantErr: false,
			args: args{
				ctx: context.Background(),
				zoneLabel: map[string]string{
					validZoneLabelKey: validZoneName,
				},
			},
			fields: fields{
				opts: Opts{
					KubeNodeName: validNodeName,
				},
			},
			initTest: func(s *service) {
				// setup fake k8s client and create a pod to perform tests against
				K8sClientset = fake.NewSimpleClientset()
				podClient := K8sClientset.CoreV1().Pods(DriverNamespace)

				// create test pod
				_, err := podClient.Create(context.Background(), &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:   validAppName,
						Labels: validAppLabels,
					},
					Spec: v1.PodSpec{
						NodeName: s.opts.KubeNodeName,
					},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("error creating test pod error = %v", err)
				}
			},
		},
		{
			// Attempt to set pod labels when the k8s client cannot get pods
			name:    "when 'list pods' k8s client request fails",
			wantErr: true,
			args: args{
				ctx: context.Background(),
				zoneLabel: map[string]string{
					validZoneLabelKey: validZoneName,
				},
			},
			fields: fields{
				opts: Opts{
					KubeNodeName: validNodeName,
				},
			},
			initTest: func(_ *service) {
				// create a client, but do not create any pods so the request
				// to list pods fails
				K8sClientset = fake.NewSimpleClientset()
			},
		},
		{
			name:    "clientset is nil and fails to create one",
			wantErr: true,
			args: args{
				ctx: context.Background(),
				zoneLabel: map[string]string{
					validZoneLabelKey: validZoneName,
				},
			},
			fields: fields{
				opts: Opts{
					KubeNodeName: validNodeName,
				},
			},
			initTest: func(_ *service) {
				// setup clientset to nil to force creation
				// Creation should fail because tests are not run in a cluster
				K8sClientset = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			tt.initTest(s)
			err := s.SetPodZoneLabel(tt.args.ctx, tt.args.zoneLabel)
			if (err != nil) != tt.wantErr {
				t.Errorf("service.SetPodZoneLabel() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestArrayConnectionData_isInZone(t *testing.T) {
	type fields struct {
		SystemID                  string
		Username                  string
		Password                  string
		Endpoint                  string
		SkipCertificateValidation bool
		Insecure                  bool
		IsDefault                 bool
		AllSystemNames            string
		NasName                   string
		AvailabilityZone          *AvailabilityZone
	}
	type args struct {
		zoneName string
	}
	tests := map[string]struct {
		fields fields
		args   args
		want   bool
	}{
		"array is in the zone": {
			want: true,
			fields: fields{
				AvailabilityZone: &AvailabilityZone{
					LabelKey: "topology.kubernetes.io/zone",
					Name:     "zoneA",
				},
			},
			args: args{
				zoneName: "zoneA",
			},
		},
		"availability zone is not used": {
			want:   false,
			fields: fields{},
			args: args{
				zoneName: "zoneA",
			},
		},
		"zone names do not match": {
			want: false,
			fields: fields{
				AvailabilityZone: &AvailabilityZone{
					LabelKey: "topology.kubernetes.io/zone",
					Name:     "zoneA",
				},
			},
			args: args{
				zoneName: "zoneB",
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			array := &ArrayConnectionData{
				SystemID:                  tt.fields.SystemID,
				Username:                  tt.fields.Username,
				Password:                  tt.fields.Password,
				Endpoint:                  tt.fields.Endpoint,
				SkipCertificateValidation: tt.fields.SkipCertificateValidation,
				Insecure:                  tt.fields.Insecure,
				IsDefault:                 tt.fields.IsDefault,
				AllSystemNames:            tt.fields.AllSystemNames,
				NasName:                   tt.fields.NasName,
				AvailabilityZone:          tt.fields.AvailabilityZone,
			}
			if got := array.isInZone(tt.args.zoneName); got != tt.want {
				t.Errorf("ArrayConnectionData.isInZone() = %v, want %v", got, tt.want)
			}
		})
	}
}
