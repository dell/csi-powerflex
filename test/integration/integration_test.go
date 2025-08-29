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
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/cucumber/godog"
	"github.com/dell/csi-vxflexos/v2/provider"
	"github.com/dell/csi-vxflexos/v2/service"
	csiutils "github.com/dell/gocsi/utils/csi"
	"google.golang.org/grpc"
)

const (
	datafile       = "/tmp/datafile"
	datadir        = "/tmp/datadir"
	configFile     = "../../config.json"
	zoneConfigFile = "features/array-config/multi-az"
)

var grpcClient *grpc.ClientConn

func readConfigFile(filePath string) {
	/* load array config and give proper errors if not ok*/
	if _, err := os.Stat(filePath); err == nil {
		if _, err := os.ReadFile(filePath); err != nil {
			err = fmt.Errorf("DEBUG integration pre requisites missing %s with multi array configuration file ", filePath)
			panic(err)
		}
		f, err := os.Open(filePath)
		r := bufio.NewReader(f)
		mdms := make([]string, 0)
		line, isPrefix, err := r.ReadLine()
		for err == nil && !isPrefix {
			line, isPrefix, err = r.ReadLine()
			if strings.Contains(string(line), "127.0.0.1") {
				err := fmt.Errorf("Integration test pre-requisite powerflex array endpoint %s is not ok, setup ../../config.json", string(line))
				panic(err)
			}
			if strings.Contains(string(line), "mdm") {
				mdms = append(mdms, string(line))
			}
		}
		if len(mdms) < 1 {
			err := fmt.Errorf("Integration Test pre-requisite config file ../../config.json  must have mdm key set with working ip %#v", mdms)
			panic(err)
		}
	} else if os.IsNotExist(err) {
		err := fmt.Errorf("Integration Test pre-requisite needs  a valid config.json located here :  %s\"", err)
		panic(err)
	}
}

func TestIntegration(t *testing.T) {
	readConfigFile(configFile)
	var stop func()
	ctx := context.Background()
	fmt.Printf("calling startServer")
	grpcClient, stop = startServer(ctx, "")
	fmt.Printf("back from startServer")
	time.Sleep(5 * time.Second)

	// Make the directory and file needed for NodePublish, these are:
	//  /tmp/datadir    -- for file system mounts
	//  /tmp/datafile   -- for block bind mounts
	fmt.Printf("Checking %s\n", datadir)
	var fileMode os.FileMode
	fileMode = 0o777
	err := os.Mkdir(datadir, fileMode)
	if err != nil && !os.IsExist(err) {
		fmt.Printf("%s: %s\n", datadir, err)
	}
	fmt.Printf("Checking %s\n", datafile)
	file, err := os.Create(datafile)
	if err != nil && !os.IsExist(err) {
		fmt.Printf("%s %s\n", datafile, err)
	}
	if file != nil {
		file.Close()
	}

	outputfile, err := os.Create("integration.xml")

	opts := godog.Options{
		Format: "junit",
		Output: outputfile,
		Paths:  []string{"features"},
		// Tags:   "wip",
	}

	exitVal := godog.TestSuite{
		Name:                "godog",
		ScenarioInitializer: FeatureContext,
		Options:             &opts,
	}.Run()

	stop()
	if exitVal != 0 {
		t.Fatalf("[TestIntegration] godog exited with %d", exitVal)
	}
}

func TestZoneIntegration(t *testing.T) {
	readConfigFile(zoneConfigFile)
	var stop func()
	ctx := context.Background()
	fmt.Printf("calling startServer")
	grpcClient, stop = startServer(ctx, zoneConfigFile)
	fmt.Printf("back from startServer")
	time.Sleep(5 * time.Second)

	fmt.Printf("Checking %s\n", datadir)
	err := os.Mkdir(datadir, 0o777)
	if err != nil && !os.IsExist(err) {
		fmt.Printf("%s: %s\n", datadir, err)
	}

	fmt.Printf("Checking %s\n", datafile)
	file, err := os.Create(datafile)
	if err != nil && !os.IsExist(err) {
		fmt.Printf("%s %s\n", datafile, err)
	}

	if file != nil {
		file.Close()
	}

	godogOptions := godog.Options{
		Format:        "pretty,junit:zone-volumes-test-report.xml",
		Paths:         []string{"features"},
		Tags:          "zone-integration",
		TestingT:      t,
		StopOnFailure: true,
	}

	exitVal := godog.TestSuite{
		Name:                "zone-integration",
		ScenarioInitializer: FeatureContext,
		Options:             &godogOptions,
	}.Run()

	stop()
	if exitVal != 0 {
		t.Fatalf("[TestZoneIntegration] godog exited with %d", exitVal)
	}
}

func TestIdentityGetPluginInfo(t *testing.T) {
	ctx := context.Background()
	fmt.Printf("testing GetPluginInfo\n")
	client := csi.NewIdentityClient(grpcClient)
	info, err := client.GetPluginInfo(ctx, &csi.GetPluginInfoRequest{})
	if err != nil {
		fmt.Printf("GetPluginInfo %s:\n", err.Error())
		t.Error("GetPluginInfo failed")
	} else {
		fmt.Printf("testing GetPluginInfo passed: %s\n", info.GetName())
	}
}

func startServer(ctx context.Context, cFile string) (*grpc.ClientConn, func()) {
	if cFile == "" {
		cFile = configFile
	}
	// Create a new SP instance and serve it with a piped connection.
	service.ArrayConfigFile = cFile
	sp := provider.New()
	lis, err := csiutils.GetCSIEndpointListener()
	if err != nil {
		fmt.Printf("couldn't open listener: %s\n", err.Error())
		return nil, nil
	}
	fmt.Printf("lis: %v\n", lis)
	go func() {
		fmt.Printf("starting server\n")
		if err := sp.Serve(ctx, lis); err != nil {
			fmt.Printf("http: Server closed")
		}
	}()
	network, addr, err := csiutils.GetCSIEndpoint()
	if err != nil {
		return nil, nil
	}
	fmt.Printf("network %v addr %v\n", network, addr)

	clientOpts := []grpc.DialOption{
		grpc.WithInsecure(),
	}

	// Create a client for the piped connection.
	fmt.Printf("calling gprc.DialContext, ctx %v, addr %s, clientOpts %v\n", ctx, addr, clientOpts)
	client, err := grpc.DialContext(ctx, "unix:"+addr, clientOpts...)
	if err != nil {
		fmt.Printf("DialContext returned error: %s", err.Error())
	}
	fmt.Printf("grpc.DialContext returned ok\n")

	return client, func() {
		client.Close()
		sp.GracefulStop(ctx)
	}
}
