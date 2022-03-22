package e2e

import (
        "flag"
        "os"
        "path/filepath"

        "k8s.io/kubernetes/test/e2e/framework"
        k8sConfig "k8s.io/kubernetes/test/e2e/framework/config"
)

const kubeconfigEnvVar = "KUBECONFIG"

func initializeFramework() {
        // k8s.io/kubernetes/tests/e2e/framework requires env KUBECONFIG to be set
        // it does not fall back to defaults
        if os.Getenv(kubeconfigEnvVar) == "" {
                kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
                os.Setenv(kubeconfigEnvVar, kubeconfig)
        }
        framework.AfterReadingAllFlags(&framework.TestContext)

        k8sConfig.CopyFlags(k8sConfig.Flags, flag.CommandLine)
        framework.RegisterCommonFlags(flag.CommandLine)
        flag.Parse()
}

