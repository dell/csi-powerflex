package e2e

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	//"github.com/onsi/ginkgo/v2/reporters"
	. "github.com/onsi/gomega"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
)

func init() {
	// k8s.io/kubernetes/tests/e2e/framework requires env KUBECONFIG to be set
	// it does not fall back to defaults
	if os.Getenv(kubeconfigEnvVar) == "" {
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		os.Setenv(kubeconfigEnvVar, kubeconfig)
	}

	framework.TestContext.Provider = "local"

	t := framework.TestContextType{
		Host: "https://10.247.98.123:6443",
	}

	framework.AfterReadingAllFlags(&t)
}

func TestE2E(t *testing.T) {
	handleFlags()
	RegisterFailHandler(Fail)

	// pass/fail/skip results summarized to this file

	//junitReporter := reporters.NewJUnitReporter("junit.xml")

	// dont dump huge logs of node / pods on error
	framework.TestContext.DumpLogsOnFailure = false

	// runs all ginkgo tests in go files
	//RunSpecsWithDefaultAndCustomReporters(t, "CSI Driver End-to-End Tests", []Reporter{junitReporter})

	// fetch the current config
	suiteConfig, reporterConfig := GinkgoConfiguration()
	// adjust it
	reporterConfig.FullTrace = true
	RunSpecs(t, "CSI Driver End-to-End Tests", suiteConfig, reporterConfig)
}

func handleFlags() {
	config.CopyFlags(config.Flags, flag.CommandLine)
	framework.RegisterCommonFlags(flag.CommandLine)
	framework.RegisterClusterFlags(flag.CommandLine)
	flag.Parse()
	if flag.Lookup("seed") != nil {
		fmt.Printf("debug found flag %s ", flag.Lookup("seed"))
		var foo string
        	flag.StringVar(&foo, "seed", "", "this is seed")
    	}
	flag.Parse()
}
