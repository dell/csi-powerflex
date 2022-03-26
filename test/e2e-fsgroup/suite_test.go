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
	// k8s.io/kubernetes/tests/e2e/framework requires env KUBECONFIG to be set
	// it does not fall back to defaults
	if os.Getenv(kubeconfigEnvVar) == "" {
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		os.Setenv(kubeconfigEnvVar, kubeconfig)
	}
	os.Setenv(scParamStoragePool, "pool1")
	os.Setenv(scParamStorageSystem, "4d4a2e5a36080e0f")

	framework.TestContext.Provider = "local"

	framework.TestContext.MaxNodesToGather = 0

	t := framework.TestContextType{
		Host:             "https://10.247.98.123:6443",
		MaxNodesToGather: 0,
	}

	//timeouts := framework.NewTimeoutContextWithDefaults()

	framework.AfterReadingAllFlags(&t)
}

func TestE2E(t *testing.T) {
	handleFlags()
	RegisterFailHandler(Fail)
	junitReporter := reporters.NewJUnitReporter("junit.xml")
	RunSpecsWithDefaultAndCustomReporters(t, "CSI Driver End-to-End Tests", []Reporter{junitReporter})
}

func handleFlags() {
	config.CopyFlags(config.Flags, flag.CommandLine)
	framework.RegisterCommonFlags(flag.CommandLine)
	framework.RegisterClusterFlags(flag.CommandLine)
	flag.Parse()
}
