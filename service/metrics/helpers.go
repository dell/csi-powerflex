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

// Package metrics provides Prometheus metrics infrastructure for the
// CSI PowerFlex driver, including a shared metrics HTTP server and gateway
// health monitoring. It is designed to be extensible so that any driver
// component can register its own Prometheus collectors on the shared endpoint.
package metrics

import (
	"strings"
)

// DefaultMetricsPort is the default HTTP port for the Prometheus metrics endpoint.
const DefaultMetricsPort = ":9090"

// FormatMetricsAddr formats a metrics address for use in log messages and status
// reporting. It normalises the port string so a plain "9090" becomes ":9090".
func FormatMetricsAddr(addr string) string {
	if addr == "" {
		return DefaultMetricsPort
	}
	if !strings.HasPrefix(addr, ":") {
		return ":" + addr
	}
	return addr
}
