

package steps

import (
	//"context"
	"fmt"
	"io/ioutil"
	"os"
	//"strings"

	csmv1 "github.com/dell/csm-operator/api/v1alpha1"
	//"github.com/dell/csm-operator/pkg/constants"
	//"github.com/dell/csm-operator/pkg/modules"
	//appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	//"k8s.io/apimachinery/pkg/api/errors"
	//confv1 "k8s.io/client-go/applyconfigurations/apps/v1"
	//acorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/kubernetes"
	//"k8s.io/kubernetes/test/e2e/framework"
	fpod "k8s.io/kubernetes/test/e2e/framework/pod"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"


)

// CustomTest -
type CustomTest struct {
        Name string `json:"name" yaml:"name"`
        Run  string `json:"run" yaml:"run"`
}

// Scenario -
type Scenario struct {
        Scenario   string     `json:"scenario" yaml:"scenario"`
        Path       string     `json:"path" yaml:"path"`
        Steps      []string   `json:"steps" yaml:"steps"`
        CustomTest CustomTest `json:"customTest,omitempty" yaml:"customTest"`
}

// Resource -
type Resource struct {
        Scenario       Scenario
        CustomResource csmv1.ContainerStorageModule
}

// Step -
type Step struct {
        ctrlClient client.Client
        clientSet  *kubernetes.Clientset
}

var (
        authString        = "karavi-authorization-proxy"
        driverNamespace = "vxflexos"
)

// GetTestResources -- parse values file
func GetTestResources(valuesFilePath string) ([]Resource, error) {
        b, err := ioutil.ReadFile(valuesFilePath)
        if err != nil {
                return nil, fmt.Errorf("failed to read values file: %v", err)
        }

        scenarios := []Scenario{}
        err = yaml.Unmarshal(b, &scenarios)
        if err != nil {
                return nil, fmt.Errorf("failed to read unmarshal values file: %v", err)
        }

        recourses := []Resource{}
        for _, scene := range scenarios {
                b, err := ioutil.ReadFile(scene.Path)
                if err != nil {
                        return nil, fmt.Errorf("failed to read testdata: %v", err)
                }

                customResource := csmv1.ContainerStorageModule{}
                err = yaml.Unmarshal(b, &customResource)
                if err != nil {
                        return nil, fmt.Errorf("failed to read unmarshal CSM custom resource: %v", err)
                }

                recourses = append(recourses, Resource{
                        Scenario:       scene,
                        CustomResource: customResource,
                })
        }

        return recourses, nil
}




func (step *Step) validateTestEnvironment(_ Resource) error {
        if os.Getenv("DRIVER_NAMESPACE") != "" {
                driverNamespace = os.Getenv("DRIVER_NAMESPACE")
        }

	fmt.Printf("driverNamespace is: %s \n", driverNamespace)
        pods, err := fpod.GetPodsInNamespace(step.clientSet, driverNamespace, map[string]string{})
        if err != nil {
                return err
        }
	
	numOfPods := len(pods)

        if numOfPods == 0 {
		fmt.Printf("Returning error!\n")
                return fmt.Errorf("no pods were found in %s namespace", driverNamespace)
        }

        notReadyMessage := ""
        allReady := true
        for _, pod := range pods {
                if pod.Status.Phase != corev1.PodRunning {
                        allReady = false
                        notReadyMessage += fmt.Sprintf("\nThe pod(%s) is %s", pod.Name, pod.Status.Phase)
                }
        }

        if !allReady {
		fmt.Printf("%s\n", notReadyMessage)
                return fmt.Errorf(notReadyMessage)
        }

        return nil
}

func (step *Step) createPodWithFsGroup(_ Resource) error {
	fmt.Printf("Called createPodWithFsGroup! \n")
	return nil 

}

