package e2e

import (
        "fmt"
        "os"
        "testing"
        "time"

        //csmv1 "github.com/dell/csm-operator/api/v1alpha1"
        step "github.com/dell/csi-powerflex/tests/e2e/steps"

        "k8s.io/client-go/kubernetes"
        "sigs.k8s.io/controller-runtime/pkg/client"
        "sigs.k8s.io/controller-runtime/pkg/client/config"

        . "github.com/onsi/ginkgo"
        . "github.com/onsi/gomega"
        "k8s.io/kubernetes/test/e2e/framework"
)

const (
        timeout  = time.Minute * 10
        interval = time.Second * 10
)

var (
        testResources []step.Resource
        stepRunner    *step.Runner
        beautify      string
)

// TestE2E -
func TestE2E(t *testing.T) {
        if testing.Short() {
                t.Skip("skipping testing in short mode")
        }

        initializeFramework()
        RegisterFailHandler(Fail)
        RunSpecs(t, "CSI-PowerFlex End-to-End Tests")
}

var _ = BeforeSuite(func() {
        By("Getting test environment variables")
        valuesFile := os.Getenv("E2E_VALUES_FILE")
        Expect(valuesFile).NotTo(BeEmpty(), "Missing environment variable required for tests. E2E_VALUES_FILE must both be set.")

        By("Reading values file")
        res, err := step.GetTestResources(valuesFile)
        if err != nil {
                framework.Failf("Failed to read values file: %v", err)
        }
        testResources = res

        By("Getting a k8s client")
        ctrlClient, err := client.New(config.GetConfigOrDie(), client.Options{})
        if err != nil {
                framework.Failf("Failed to create controll runtime client: %v", err)
        }
	fmt.Printf("config is: %v \n", config.GetConfigOrDie().Host)

        clientSet, err := kubernetes.NewForConfig(config.GetConfigOrDie())
        if err != nil {
                framework.Failf("Failed to create kubernetes  clientset : %v", err)
        }

        stepRunner = &step.Runner{}
        step.StepRunnerInit(stepRunner, ctrlClient, clientSet)

        beautify = "    "

})

var _ = Describe("[run-e2e-test]E2E Testing", func() {
        It("Running all test Given Test Scenarios", func() {
                for _, test := range testResources {
                        By(fmt.Sprintf("Starting: %s ", test.Scenario.Scenario))

                        for _, stepName := range test.Scenario.Steps {
                                By(fmt.Sprintf("%s Executing  %s", beautify, stepName))
                                Eventually(func() error {
                                        return stepRunner.RunStep(stepName, test)
                                }, timeout, interval).Should(BeNil())
                        }
                        By(fmt.Sprintf("Ending: %s ", test.Scenario.Scenario))
                        By("")
                        By("")
                        time.Sleep(5 * time.Second)
                }
        })
})

