// Copyright © 2026 Dell Inc. or its subsidiaries. All Rights Reserved.
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

package service

import (
	"os"
	"strings"
	"testing"

	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestBuildSDCName covers all branches of the buildSDCName helper.
func TestBuildSDCName(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		hostName    string
		trimEnabled bool
		want        string
		wantLen     int
	}{
		{
			name:        "no prefix, short name, trim disabled",
			prefix:      "",
			hostName:    "worker-node-1",
			trimEnabled: false,
			want:        "worker-node-1",
		},
		{
			name:        "no prefix, short name, trim enabled — no truncation needed",
			prefix:      "",
			hostName:    "worker-node-1",
			trimEnabled: true,
			want:        "worker-node-1",
		},
		{
			name:        "no prefix, exactly 31 chars, trim enabled — no truncation",
			prefix:      "",
			hostName:    "1234567890123456789012345678901", // exactly 31 chars
			trimEnabled: true,
			want:        "1234567890123456789012345678901",
			wantLen:     31,
		},
		{
			name:        "no prefix, 32 chars, trim disabled — not truncated",
			prefix:      "",
			hostName:    "12345678901234567890123456789012", // 32 chars
			trimEnabled: false,
			want:        "12345678901234567890123456789012",
			wantLen:     32,
		},
		{
			name:        "no prefix, 32 chars, trim enabled — truncated to 31",
			prefix:      "",
			hostName:    "12345678901234567890123456789012", // 32 chars
			trimEnabled: true,
			want:        "1234567890123456789012345678901",
			wantLen:     31,
		},
		{
			name:        "no prefix, long hostname, trim enabled — truncated to 31",
			prefix:      "",
			hostName:    "very-long-kubernetes-worker-node-hostname-abc",
			trimEnabled: true,
			wantLen:     31,
		},
		{
			name:        "with prefix, short total, trim disabled",
			prefix:      "pfx",
			hostName:    "node1",
			trimEnabled: false,
			want:        "pfx-node1",
		},
		{
			name:        "with prefix, short total, trim enabled — no truncation",
			prefix:      "pfx",
			hostName:    "node1",
			trimEnabled: true,
			want:        "pfx-node1",
		},
		{
			name:        "with prefix, combined exactly 31 chars, trim enabled — no truncation",
			prefix:      "prefix",
			hostName:    "123456789012345678901234", // "prefix-" (7) + 24 = 31
			trimEnabled: true,
			wantLen:     31,
		},
		{
			name:        "with prefix, combined 32 chars, trim enabled — truncated to 31",
			prefix:      "prefix",
			hostName:    "1234567890123456789012345", // "prefix-" (7) + 25 = 32
			trimEnabled: true,
			wantLen:     31,
		},
		{
			name:        "with prefix, combined long, trim disabled — not truncated",
			prefix:      "datacenter-west",
			hostName:    "kubernetes-worker-node-42",
			trimEnabled: false,
			want:        "datacenter-west-kubernetes-worker-node-42",
		},
		{
			name:        "with prefix, combined long, trim enabled — truncated to 31",
			prefix:      "datacenter-west",
			hostName:    "kubernetes-worker-node-42",
			trimEnabled: true,
			wantLen:     31,
		},
		{
			name:        "prefix separator is hyphen",
			prefix:      "myprefix",
			hostName:    "myhost",
			trimEnabled: false,
			want:        "myprefix-myhost",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildSDCName(tc.prefix, tc.hostName, tc.trimEnabled)

			if tc.want != "" {
				assert.Equal(t, tc.want, got)
			}
			if tc.wantLen > 0 {
				assert.Equal(t, tc.wantLen, len(got),
					"expected length %d but got %d (name=%q)", tc.wantLen, len(got), got)
			}
			if tc.trimEnabled {
				assert.LessOrEqual(t, len(got), 31,
					"trim enabled: name must not exceed 31 chars, got %q (%d chars)", got, len(got))
			}
			// Verify prefix is honoured when set
			if tc.prefix != "" {
				assert.True(t, strings.HasPrefix(got, tc.prefix+"-"),
					"expected name to start with prefix %q, got %q", tc.prefix+"-", got)
			}
		})
	}
}

// TestBuildSDCName_TrimPreservesPrefix verifies that after truncation the
// result still starts with the prefix (truncation is applied to the combined string).
func TestBuildSDCName_TrimPreservesPrefix(t *testing.T) {
	// "pfx-" (4 chars) + 27-char hostname = 31: no truncation
	result := buildSDCName("pfx", "123456789012345678901234567", true)
	assert.Equal(t, 31, len(result))
	assert.True(t, strings.HasPrefix(result, "pfx-"))

	// "pfx-" (4 chars) + 28-char hostname = 32: truncated
	result = buildSDCName("pfx", "1234567890123456789012345678", true)
	assert.Equal(t, 31, len(result))
	assert.True(t, strings.HasPrefix(result, "pfx-"))
}

// TestBuildSDCName_Deterministic verifies the same inputs always produce
// the same output (idempotent).
func TestBuildSDCName_Deterministic(t *testing.T) {
	prefix := "cluster-east"
	host := "node-with-very-long-hostname-in-kubernetes"
	r1 := buildSDCName(prefix, host, true)
	r2 := buildSDCName(prefix, host, true)
	assert.Equal(t, r1, r2)
	assert.Equal(t, 31, len(r1))
}

// TestRenameSDC_MissingHostname verifies renameSDC returns a FailedPrecondition
// error when the HOSTNAME env variable is not set.
func TestRenameSDC_MissingHostname(t *testing.T) {
	// Ensure HOSTNAME is unset for this test
	prev, wasSet := os.LookupEnv("HOSTNAME")
	os.Unsetenv("HOSTNAME")
	defer func() {
		if wasSet {
			os.Setenv("HOSTNAME", prev)
		}
	}()

	svc := &service{}
	err := svc.renameSDC(Opts{IsSdcRenameEnabled: true})

	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "HOSTNAME")
}

// TestRenameSDC_EmptyConnectedSystems verifies renameSDC is a no-op (returns nil)
// when connectedSystemID is empty, regardless of TrimSDCNameEnabled.
func TestRenameSDC_EmptyConnectedSystems(t *testing.T) {
	os.Setenv("HOSTNAME", "test-node")
	defer os.Unsetenv("HOSTNAME")

	// Save and restore connectedSystemID
	saved := connectedSystemID
	connectedSystemID = nil
	defer func() { connectedSystemID = saved }()

	svc := &service{}

	// trim disabled
	err := svc.renameSDC(Opts{IsSdcRenameEnabled: true, TrimSDCNameEnabled: false})
	assert.NoError(t, err)

	// trim enabled — still no-op since no systems are connected
	err = svc.renameSDC(Opts{IsSdcRenameEnabled: true, TrimSDCNameEnabled: true})
	assert.NoError(t, err)
}

// TestRenameSDC_NilSystemSkipped verifies that a nil system entry in the
// systems map is skipped without error, even when connectedSystemID is non-empty.
func TestRenameSDC_NilSystemSkipped(t *testing.T) {
	os.Setenv("HOSTNAME", "test-node")
	defer os.Unsetenv("HOSTNAME")

	saved := connectedSystemID
	connectedSystemID = []string{"sysID1"}
	defer func() { connectedSystemID = saved }()

	svc := &service{
		systems: map[string]*sio.System{
			"sysID1": nil, // explicitly nil — should be skipped
		},
	}

	err := svc.renameSDC(Opts{IsSdcRenameEnabled: true, TrimSDCNameEnabled: true})
	assert.NoError(t, err)
}
