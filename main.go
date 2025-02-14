//Copyright © 2019-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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
//go:generate go generate ./core

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/dell/csi-vxflexos/v2/k8sutils"
	"github.com/dell/csi-vxflexos/v2/provider"
	"github.com/dell/csi-vxflexos/v2/service"
	"github.com/dell/gocsi"
	"github.com/sirupsen/logrus"
)

var flags struct {
	arrayConfigfile         *string
	driverConfigParamsfile  *string
	enableLeaderElection    *bool
	leaderElectionNamespace *string
	kubeconfig              *string
}

// main is ignored when this package is built as a go plug-in
func main() {
	logger := logrus.New()
	service.Log = logger
	setEnvsFunc()
	initFlagsFunc()

	err := checkConfigsFunc()
	if err != nil {
		forceExit()
	}

	done, err := preInitCheckFunc()
	if err != nil {
		forceExit()
	}
	if done {
		return // main loop is finished, return here
	}

	err = driverRunFunc()
	if err != nil {
		forceExit()
	}
}

var checkConfigsFunc = func() error {
	if *flags.arrayConfigfile == "" {
		fmt.Fprintf(os.Stderr, "array-config argument is mandatory")
		return errors.New("missing param")
	}

	if *flags.driverConfigParamsfile == "" {
		fmt.Fprintf(os.Stderr, "driver-config-params argument is mandatory")
		return errors.New("missing param")
	}
	service.ArrayConfigFile = *flags.arrayConfigfile
	service.DriverConfigParamsFile = *flags.driverConfigParamsfile
	service.KubeConfig = *flags.kubeconfig
	return nil
}

var preInitCheckFunc = func() (bool, error) {
	// Run the service as a pre-init step.
	if os.Getenv(gocsi.EnvVarMode) == "mdm-info" {
		fmt.Fprintf(os.Stdout, "PowerFlex Container Storage Interface (CSI) Plugin starting in pre-init mode.")
		svc := service.NewPreInitService()
		err := svc.PreInit()
		return (err == nil), err
	}
	return false, nil
}

var driverRunFunc = func() error {
	run := driverRunLoopFunc
	if !*flags.enableLeaderElection {
		run(context.Background())
	} else {
		driverName := strings.Replace(service.Name, ".", "-", -1)
		lockName := fmt.Sprintf("driver-%s", driverName)
		err := k8sutils.CreateKubeClientSet()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "failed to create clientset for leader election: %v", err)
			return err
		}
		service.K8sClientset = k8sutils.Clientset
		// Attempt to become leader and start the driver
		err = k8sutils.LeaderElectionFunc(&k8sutils.Clientset, lockName, *flags.leaderElectionNamespace, run)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "failed to become leader: %v", err)
			return err
		}
	}
	return nil
}

var driverRunLoopFunc = func(ctx context.Context) {
	gocsi.Run(ctx, service.Name, "A PowerFlex Container Storage Interface (CSI) Plugin",
		usage, provider.New())
}

// sets environment variables
var setEnvsFunc = func() {
	// Always set X_CSI_DEBUG to false irrespective of what user has specified
	_ = os.Setenv(gocsi.EnvVarDebug, "false")
	// We always want to enable Request and Response logging(no reason for users to control this)
	_ = os.Setenv(gocsi.EnvVarReqLogging, "true")
	_ = os.Setenv(gocsi.EnvVarRepLogging, "true")
}

// initializes all driver flags
var initFlagsFunc = func() {
	flags.arrayConfigfile = flag.String("array-config", "", "yaml file with array(s) configuration")
	flags.driverConfigParamsfile = flag.String("driver-config-params", "", "yaml file with driver config params")
	flags.enableLeaderElection = flag.Bool("leader-election", false, "boolean to enable leader election")
	flags.leaderElectionNamespace = flag.String("leader-election-namespace", "", "namespace where leader election lease will be created")
	flags.kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	flag.Parse()
}

// allows for override in UT to test codepaths that end in force-quit
var forceExit = func() {
	os.Exit(1)
}

const usage = `    X_CSI_VXFLEXOS_SDCGUID
        Specifies the GUID of the SDC. This is only used by the Node Service,
        and removes a need for calling an external binary to retrieve the GUID.
        If not set, the external binary will be invoked.

        The default value is empty.

    X_CSI_VXFLEXOS_THICKPROVISIONING
        Specifies whether thick provisiong should be used when creating volumes.

        The default value is false.

    X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE
        When a snapshot is deleted, if it is a member of a Consistency Group, enable automatic deletion
        of all snapshots in the consistency group.

        The default value is false.

    X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS
        When listing volumes, if this option is is enabled, then volumes and snapshots will be returned.
        Otherwise only volumes are returned.

        The default value is false.
`
