/*
 Copyright © 2019-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
 
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at
      http://www.apache.org/licenses/LICENSE-2.0
 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package service

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"testing"

	"github.com/cucumber/godog"
)

func TestMain(m *testing.M) {

	go http.ListenAndServe("localhost:6060", nil)
	fmt.Printf("starting godog...\n")

	opts := godog.Options{
		Format: "pretty",
		Paths:  []string{"features"},
		//Tags:   "wip",
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
