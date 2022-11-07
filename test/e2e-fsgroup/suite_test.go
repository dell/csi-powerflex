/*
 Copyright © 2022 Dell Inc. or its subsidiaries. All Rights Reserved.
 
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

package e2e

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
)

func init() {

	var yamlError error

	testParameters, yamlError = readYaml("e2e-values.yaml")
	if yamlError != nil {

		framework.Failf("Unable to read yaml e2e-values.yaml: %s", yamlError.Error())
	}

	// k8s.io/kubernetes/tests/e2e/framework requires env KUBECONFIG to be set
	// it does not fall back to defaults
	if os.Getenv(testParameters["kubeconfigEnvVar"]) == "" {
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		os.Setenv(testParameters["kubeconfigEnvVar"], kubeconfig)
	}

	framework.TestContext.Provider = "local"

	t := framework.TestContextType{}

	framework.AfterReadingAllFlags(&t)
}

func TestE2E(t *testing.T) {
	handleFlags()
	RegisterFailHandler(Fail)

	// pass/fail/skip results summarized to this file

	junitReporter := reporters.NewJUnitReporter("junit.xml")

	// dont dump huge logs of node / pods on error
	framework.TestContext.DumpLogsOnFailure = false

	//framework.TestContext.DeleteNamespace = false

	// runs all ginkgo tests in go files
	RunSpecsWithDefaultAndCustomReporters(t, "CSI Driver End-to-End Tests", []Reporter{junitReporter})
}

func handleFlags() {
	config.CopyFlags(config.Flags, flag.CommandLine)
	framework.RegisterCommonFlags(flag.CommandLine)
	framework.RegisterClusterFlags(flag.CommandLine)
	flag.Parse()
}
