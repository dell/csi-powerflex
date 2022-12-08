// Copyright © 2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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

package integration_test

import (
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/akutz/memconn"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/cucumber/godog"
	"github.com/dell/csi-vxflexos/v2/provider"
	"github.com/dell/csi-vxflexos/v2/service"
	"github.com/dell/gocsi/utils"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

const (
	datafile   = "/tmp/datafile"
	datadir    = "/tmp/datadir"
	configFile = "../../config.json"
)

var grpcClient *grpc.ClientConn

func init() {
	/* load array config and give proper errors if not ok*/
	if _, err := os.Stat(configFile); err == nil {
		if _, err := ioutil.ReadFile(configFile); err != nil {
			err = fmt.Errorf("DEBUG integration pre requisites missing %s with multi array configuration file ", configFile)
			panic(err)
		}
		f, err := os.Open(configFile)
		r := bufio.NewReader(f)
		mdms := make([]string, 0)
		line, isPrefix, err := r.ReadLine()
		for err == nil && !isPrefix {
			line, isPrefix, err = r.ReadLine()
			if strings.Contains(string(line), "127.0.0.1") {
				err := fmt.Errorf("Integration test pre-requisite powerflex array endpoint %s is not ok, setup ../../config.json \n", string(line))
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

func TestMain(m *testing.M) {
	var stop func()
	ctx := context.Background()
	fmt.Printf("calling startServer")
	grpcClient, stop = startServer(ctx)
	fmt.Printf("back from startServer")
	time.Sleep(5 * time.Second)

	// Make the directory and file needed for NodePublish, these are:
	//  /tmp/datadir    -- for file system mounts
	//  /tmp/datafile   -- for block bind mounts
	fmt.Printf("Checking %s\n", datadir)
	var fileMode os.FileMode
	fileMode = 0777
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

	opts := godog.Options{
		Format: "pretty",
		Paths:  []string{"features"},
		//Tags:   "wip",
	}

	exitVal := godog.TestSuite{
		Name:                "godog",
		ScenarioInitializer: FeatureContext,
		Options:             &opts,
	}.Run()

	if st := m.Run(); st > exitVal {
		exitVal = st
	}
	stop()
	os.Exit(exitVal)
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

func startServer(ctx context.Context) (*grpc.ClientConn, func()) {
	// Create a new SP instance and serve it with a piped connection.
	service.ArrayConfigFile = configFile
	sp := provider.New()
	lis, err := utils.GetCSIEndpointListener()
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
	network, addr, err := utils.GetCSIEndpoint()
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

func startServerX(ctx context.Context, t *testing.T) (*grpc.ClientConn, func()) {
	// Create a new SP instance and serve it with a piped connection.
	sp := provider.New()
	lis, err := memconn.Listen("memu", "csi-test")
	assert.NoError(t, err)
	go func() {
		if err := sp.Serve(ctx, lis); err != nil {
			assert.EqualError(t, err, "http: Server closed")
		}
	}()

	clientOpts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithDialer(func(string, time.Duration) (net.Conn, error) {
			return memconn.Dial("memu", "csi-test")
		}),
	}

	// Create a client for the piped connection.
	client, err := grpc.DialContext(ctx, "unix:./unix_sock", clientOpts...)
	if err != nil {
		fmt.Printf("DialContext error: %s\n", err.Error())
	}

	return client, func() {
		client.Close()
		sp.GracefulStop(ctx)
	}
}
