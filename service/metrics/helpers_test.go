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

package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// U-PFX-20: DefaultMetricsPort must equal :9090 (migrated from :8443)
func TestDefaultMetricsPort_Is9090(t *testing.T) {
	assert.Equal(t, ":9090", DefaultMetricsPort,
		"DefaultMetricsPort must be :9090 to align with all other CSM components (migrated from :8443)")
}
