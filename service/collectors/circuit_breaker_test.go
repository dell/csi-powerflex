// Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package collectors

import (
	"context"
	"errors"
	"fmt"
	"testing"

	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

// steppedFailingRegisterer succeeds for the first failAfter-1 Register calls,
// then returns an error. This lets us exercise each metric-registration error
// branch inside collector constructors.
type steppedFailingRegisterer struct {
	failAfter int
	count     int
}

func (r *steppedFailingRegisterer) Register(prometheus.Collector) error {
	r.count++
	if r.count >= r.failAfter {
		return fmt.Errorf("register failed at call %d", r.count)
	}
	return nil
}

func (r *steppedFailingRegisterer) MustRegister(...prometheus.Collector) {}
func (r *steppedFailingRegisterer) Unregister(prometheus.Collector) bool { return false }

func TestNewStoragePoolCollector_RegistrationErrorBranches(t *testing.T) {
	// NewStoragePoolCollector registers 5 metrics, so failAfter runs 1..5.
	for failAfter := 1; failAfter <= 5; failAfter++ {
		t.Run(fmt.Sprintf("fail_after_%d", failAfter), func(t *testing.T) {
			reg := &steppedFailingRegisterer{failAfter: failAfter}
			_, err := NewStoragePoolCollector(&mockStoragePoolClient{}, reg, "system-1")
			assert.Error(t, err)
		})
	}
}

type mockStoragePoolClient struct{}

func (m *mockStoragePoolClient) GetStoragePools(context.Context) ([]StoragePoolStats, error) {
	return nil, nil
}

func TestNewVolumeCollector_RegistrationErrorBranches(t *testing.T) {
	// NewVolumeCollector registers 7 metrics.
	for failAfter := 1; failAfter <= 7; failAfter++ {
		t.Run(fmt.Sprintf("fail_after_%d", failAfter), func(t *testing.T) {
			reg := &steppedFailingRegisterer{failAfter: failAfter}
			_, err := NewVolumeCollector(&mockVolumeClient{}, reg, "system-1")
			assert.Error(t, err)
		})
	}
}

type mockVolumeClient struct{}

func (m *mockVolumeClient) GetVolumes(context.Context) ([]VolumeInfo, error) {
	return nil, nil
}

func TestNewRCGCollector_RegistrationErrorBranches(t *testing.T) {
	// NewRCGCollector registers 16 metrics.
	for failAfter := 1; failAfter <= 16; failAfter++ {
		t.Run(fmt.Sprintf("fail_after_%d", failAfter), func(t *testing.T) {
			reg := &steppedFailingRegisterer{failAfter: failAfter}
			_, err := NewRCGCollector(&mockRCGClient{}, reg, "system-1")
			assert.Error(t, err)
		})
	}
}

type mockRCGClient struct{}

func (m *mockRCGClient) GetRCGStats(context.Context) ([]RCGStats, error) {
	return nil, nil
}

func TestNewArrayHealthCollector_RegistrationErrorBranches(t *testing.T) {
	// NewArrayHealthCollector registers 4 metrics.
	for failAfter := 1; failAfter <= 4; failAfter++ {
		t.Run(fmt.Sprintf("fail_after_%d", failAfter), func(t *testing.T) {
			reg := &steppedFailingRegisterer{failAfter: failAfter}
			_, err := NewArrayHealthCollector(&mockArrayHealthClient{}, reg, "system-1", "https://gateway.example")
			assert.Error(t, err)
		})
	}
}

type mockArrayHealthClient struct{}

func (m *mockArrayHealthClient) GetVersion(context.Context) (string, error) {
	return "", errors.New("not implemented")
}

func (m *mockArrayHealthClient) GetMDMClusterDetails(context.Context) (*siotypes.MdmCluster, error) {
	return nil, errors.New("not implemented")
}

func TestNewDriverHealthCollector_RegistrationErrorBranches(t *testing.T) {
	// NewDriverHealthCollector registers 8 metrics.
	for failAfter := 1; failAfter <= 8; failAfter++ {
		t.Run(fmt.Sprintf("fail_after_%d", failAfter), func(t *testing.T) {
			reg := &steppedFailingRegisterer{failAfter: failAfter}
			_, err := NewDriverHealthCollector(reg, "system-1")
			assert.Error(t, err)
		})
	}
}
