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

//go:build integration
// +build integration

package e2e

import (
	"testing"

	"github.com/cucumber/godog"
)

func TestZoneVolumes(t *testing.T) {
	godogOptions := godog.Options{
		Format:        "pretty,junit:zone-volumes-test-report.xml",
		Paths:         []string{"features"},
		Tags:          "zone",
		TestingT:      t,
		StopOnFailure: true,
	}

	status := godog.TestSuite{
		Name:                "zone-volumes",
		ScenarioInitializer: InitializeScenario,
		Options:             &godogOptions,
	}.Run()

	if status != 0 {
		t.Fatalf("error: there were failed zone volumes tests. status: %d", status)
	}
}
