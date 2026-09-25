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
//

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeEphemeralCreateVolumeParams(t *testing.T) {
	t.Parallel()

	input := map[string]string{
		"systemID":    "attacker-system",
		"storagePool": "attacker-pool",
		"storagepool": "attacker-pool-legacy",
		"size":        "1Gi",
		"volumeName":  "ephemeral-vol",
	}

	got := sanitizeEphemeralCreateVolumeParams(input)

	assert.Equal(t, "1Gi", got["size"])
	assert.Equal(t, "ephemeral-vol", got["volumeName"])
	assert.NotContains(t, got, "systemID")
	assert.Contains(t, got, "storagePool")
	assert.Contains(t, got, "storagepool")
}
