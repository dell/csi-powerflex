// Copyright © 2022-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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

package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/cucumber/godog"
	"github.com/dell/csi-vxflexos/v2/provider"
	"github.com/dell/csi-vxflexos/v2/service"
	"github.com/dell/gocsi/utils"
	"google.golang.org/grpc"
)

const (
	datafile = "/tmp/datafile"
	datadir  = "/tmp/datadir"
	// The array configuration provided by the test user
	baseConfigFile = "../../config.json"
	// The auto-generated array configuration file for the driver
	// that is based on the user provided config in baseConfigFile,
	// env variables and specific test scenarios requirements
	configFile = "./secret.json"
)

var grpcClient *grpc.ClientConn
var stopDriver func()

func readConfigFile(filePath string) {
	/* load array config and give proper errors if not ok*/
	if _, err := os.Stat(filePath); err == nil {
		if _, err := os.ReadFile(filePath); err != nil {
			msg := fmt.Sprintf("Failed to read multi array configuration from file %s: %v", filePath, err)
			panic(msg)
		}
		f, err := os.Open(filePath)
		r := bufio.NewReader(f)
		mdms := make([]string, 0)
		line, isPrefix, err := r.ReadLine()
		for err == nil && !isPrefix {
			line, isPrefix, err = r.ReadLine()
			if strings.Contains(string(line), "127.0.0.1") {
				msg := fmt.Sprintf("Integration test pre-requisite powerflex array endpoint %s is not ok, setup ../../config.json", string(line))
				panic(msg)
			}
			if strings.Contains(string(line), "mdm") {
				mdms = append(mdms, string(line))
			}
		}
		if len(mdms) < 1 {
			msg := fmt.Sprintf("Integration test pre-requisite config file ../../config.json must have mdm key set with working ip %#v", mdms)
			panic(msg)
		}
	} else if os.IsNotExist(err) {
		msg := fmt.Sprintf("Integration test pre-requisite needs a valid config.json located here: %s", filePath)
		panic(msg)
	}
}

func buildArrayConfig(withDefaultSysName, withAltSysName, withZone bool) error {
	fmt.Println("Building array config")

	protectionDomain := os.Getenv("PROTECTION_DOMAIN")
	storagePool := os.Getenv("STORAGE_POOL")

	if withZone && (protectionDomain == "" || storagePool == "") {
		return fmt.Errorf("PROTECTION_DOMAIN or STORAGE_POOL env variable is not set")
	}

	systemName := os.Getenv("SYSTEM_NAME")
	altSystemName := os.Getenv("ALT_SYSTEM_NAME")

	if withDefaultSysName && systemName == "" {
		return fmt.Errorf("SYSTEM_NAME env variable is not set")
	}

	if withAltSysName && altSystemName == "" {
		return fmt.Errorf("ALT_SYSTEM_NAME env variable is not set")
	}

	// Read the JSON file
	file, err := os.ReadFile(baseConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read array config file: %v", err)
	}

	// Unmarshal the JSON with arrays config
	var arrays []*ArrayConnectionData
	err = json.Unmarshal(file, &arrays)
	if err != nil {
		return fmt.Errorf("failed to unmarshal array config JSON: %v", err)
	}

	// Update the default array with the zone config and system name from env
	// Update the alternative (non-default) array with the alternative system name from env
	foundDefault := false
	for _, a := range arrays {
		if a.IsDefault {
			foundDefault = true
			if withZone {
				fmt.Printf("Adding zone config to default system %s: protectionDomain=%s storagePool=%s\n", a.SystemID, protectionDomain, storagePool)
				a.AvailabilityZone = &AvailabilityZone{
					Name:     "zoneA",
					LabelKey: "zone.csi-vxflexos.dellemc.com",
					ProtectionDomains: []ProtectionDomain{
						{
							Name:  protectionDomain,
							Pools: []string{storagePool},
						},
					},
				}
			}
			if withDefaultSysName {
				fmt.Printf("Using name %s for default system %s in driver config\n", systemName, a.SystemID)
				a.SystemID = systemName
			}
		} else {
			if withZone {
				fmt.Printf("Adding dummy zone config to alternative system %s\n", a.SystemID)
				a.AvailabilityZone = &AvailabilityZone{
					Name:     "",
					LabelKey: "zone.csi-vxflexos.dellemc.com",
					ProtectionDomains: []ProtectionDomain{
						{
							Name:  "",
							Pools: []string{""},
						},
					},
				}
			}
			if withAltSysName {
				fmt.Printf("Using name %s for alternative system %s in driver config\n", altSystemName, a.SystemID)
				a.SystemID = altSystemName
			}
		}
	}
	if !foundDefault {
		return fmt.Errorf("no default array found in array config %s", baseConfigFile)
	}

	// Marshal the updated config back to JSON
	updatedJSON, err := json.MarshalIndent(arrays, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal updated array config: %v", err)
	}

	// Write the updated JSON to a new file
	err = os.WriteFile(configFile, updatedJSON, 0644)
	if err != nil {
		return fmt.Errorf("failed to write updated array config: %v", err)
	}
	return nil
}

func TestIntegration(t *testing.T) {
	err := buildArrayConfig(false, false, false)
	if err != nil {
		t.Fatalf("Failed to setup array config file: %v", err)
	}
	readConfigFile(configFile)

	fmt.Printf("Starting PowerFlex CSI driver service...\n")
	err = startDriver(configFile)
	if err != nil {
		t.Fatalf("Driver failed to start: %v", err)
	}

	// Make the directory and file needed for NodePublish, these are:
	//  /tmp/datadir    -- for file system mounts
	//  /tmp/datafile   -- for block bind mounts
	fmt.Printf("Checking %s\n", datadir)
	var fileMode os.FileMode
	fileMode = 0o777
	err = os.Mkdir(datadir, fileMode)
	if err != nil && !os.IsExist(err) {
		t.Fatalf("Dir mount point %s creation error: %v", datadir, err)
	}
	fmt.Printf("Checking %s\n", datafile)
	file, err := os.Create(datafile)
	if err != nil && !os.IsExist(err) {
		t.Fatalf("File mount point %s creation error: %v", datafile, err)
	}
	if file != nil {
		file.Close()
	}

	tags := os.Getenv("TEST_TAGS")

	opts := godog.Options{
		Paths:         []string{"features"},
		Tags:          tags,
		Format:        "pretty", //,junit:integration.xml",
		StopOnFailure: true,
	}

	exitVal := godog.TestSuite{
		Name:                 "CSI PowerFlex Integration Tests",
		TestSuiteInitializer: FeatureContext,
		Options:              &opts,
	}.Run()

	stopDriver()
	if exitVal != 0 {
		t.Fatalf("[TestIntegration] godog exited with %d", exitVal)
	}
}

func TestIdentityGetPluginInfo(t *testing.T) {
	ctx := context.Background()
	fmt.Printf("Testing GetPluginInfo\n")
	client := csi.NewIdentityClient(grpcClient)
	info, err := client.GetPluginInfo(ctx, &csi.GetPluginInfoRequest{})
	if err != nil {
		fmt.Printf("GetPluginInfo %s:\n", err.Error())
		t.Error("GetPluginInfo failed")
	} else {
		fmt.Printf("Testing GetPluginInfo passed: %s\n", info.GetName())
	}
}

func restartDriver() error {
	fmt.Println("Restarting driver service")
	stopDriver()
	err := startDriver(configFile)
	if err != nil {
		fmt.Printf("Failed to start driver service: %v\n", err)
	}
	return err
}

func startDriver(cFile string) error {
	ctx := context.Background()

	// Create a new SP instance and serve it with a piped connection.
	service.ArrayConfigFile = cFile
	sp := provider.New()
	lis, err := utils.GetCSIEndpointListener()
	if err != nil {
		return fmt.Errorf("couldn't open listener: %v", err)
	}
	fmt.Printf("Listener created at: %s\n", lis.Addr().String())

	go func() {
		fmt.Println("Starting driver service...")
		if err := sp.Serve(ctx, lis); err != nil {
			fmt.Printf("http: Server closed")
		}
	}()

	_, addr, err := utils.GetCSIEndpoint()
	if err != nil {
		return fmt.Errorf("failed to get CSI endpoint: %v", err)
	}

	clientOpts := []grpc.DialOption{
		grpc.WithInsecure(),
	}

	// Create a client for the piped connection.
	fmt.Printf("Creating GRPC client: addr %s\n", addr)

	client, err := grpc.DialContext(ctx, "unix:"+addr, clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to connect to driver: %v", err)
	}
	fmt.Println("Connected to driver")

	grpcClient = client

	stopDriver = func() {
		fmt.Println("Stopping GRPS client and driver service...")
		client.Close()
		sp.GracefulStop(ctx)
	}

	fmt.Println("Driver is initializing...")
	time.Sleep(10 * time.Second)

	return nil
}
