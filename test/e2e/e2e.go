// Copyright © 2024 Dell Inc. or its subsidiaries. All Rights Reserved.
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

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/dell/csi-vxflexos/v2/service"
	v1 "k8s.io/api/apps/v1"
	v1Core "k8s.io/api/core/v1"
	v1Storage "k8s.io/api/storage/v1"
	"sigs.k8s.io/yaml"
)

const (
	driverNamespace = "vxflexos"
	testNamespace   = "reptest"
	storageClass    = "vxflexos-az-wait"
)

type feature struct {
	errs            []error
	zoneNodeMapping map[string]string
	zoneKey         string
	supportedZones  []string
	cordonedNode    string
}

func (f *feature) aVxFlexOSService() error {
	f.errs = make([]error, 0)
	f.zoneNodeMapping = make(map[string]string)
	f.supportedZones = make([]string, 0)
	f.cordonedNode = ""
	f.zoneKey = ""
	return nil
}

func (f *feature) isEverythingWorking() error {
	log.Println("[isEverythingWorking] Checking if everything is working...")
	checkNamespace := "kubectl get ns -A | grep -e " + driverNamespace + " -e " + testNamespace
	result, err := execLocalCommand(checkNamespace)
	if err != nil {
		return err
	}

	if !strings.Contains(string(result), driverNamespace) || !strings.Contains(string(result), testNamespace) {
		return fmt.Errorf("namespace vxflexos or reptest not found")
	}

	checkDeployment := "kubectl get deployment -n " + driverNamespace + " vxflexos-controller -o json"
	result, err = execLocalCommand(checkDeployment)
	if err != nil {
		return err
	}

	deploymentInfo := v1.Deployment{}
	err = json.Unmarshal(result, &deploymentInfo)
	if err != nil {
		return err
	}

	if deploymentInfo.Status.Replicas != deploymentInfo.Status.ReadyReplicas {
		return fmt.Errorf("deployment not ready, check deployment status and then try again")
	}

	return nil
}

func (f *feature) verifyZoneInfomation(secret, namespace string) error {
	arrays, err := f.getZoneFromSecret(secret, namespace)
	if err != nil {
		return err
	}

	for _, array := range arrays {
		if array.AvailabilityZone == nil {
			continue
		}

		// Find the first zone label and assume all others are the same..
		f.zoneKey = array.AvailabilityZone.LabelKey
		break
	}

	if f.zoneKey == "" {
		return fmt.Errorf("no labelKey found in secret %s", secret)
	}

	getNodeLabels := []string{"kubectl", "get", "nodes", "-A", "-o", "jsonpath='{.items}'"}
	justString := strings.Join(getNodeLabels, " ")

	result, err := execLocalCommand(justString)
	if err != nil {
		return nil
	}

	nodes := []v1Core.Node{}
	err = json.Unmarshal(result, &nodes)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if strings.Contains(node.ObjectMeta.Name, "master") {
			continue
		}

		if val, ok := node.ObjectMeta.Labels[f.zoneKey]; ok {
			f.zoneNodeMapping[node.ObjectMeta.Name] = val
		}
	}

	if len(f.zoneNodeMapping) == 0 {
		return fmt.Errorf("no nodes found for zone: %s", f.zoneKey)
	}

	getStorageClassCmd := "kubectl get sc " + storageClass
	_, err = execLocalCommand(getStorageClassCmd)
	if err != nil {
		return fmt.Errorf("storage class %s not found", storageClass)
	}

	scInfo := v1Storage.StorageClass{}
	getStorageClassInfo := "kubectl get sc " + storageClass + " -o json"
	result, err = execLocalCommand(getStorageClassInfo)
	if err != nil {
		return fmt.Errorf("storage class %s not found", storageClass)
	}
	err = json.Unmarshal(result, &scInfo)
	if err != nil {
		return err
	}

	if scInfo.AllowedTopologies == nil {
		return fmt.Errorf("no topologies found for storage class %s not found", storageClass)
	}

	if scInfo.AllowedTopologies[0].MatchLabelExpressions[0].Key != f.zoneKey {
		return fmt.Errorf("storage class %s does not have the proper zone lablel %s", storageClass, f.zoneKey)
	}

	// Add supported zones from the test storage class.
	f.supportedZones = scInfo.AllowedTopologies[0].MatchLabelExpressions[0].Values

	return nil
}

func (f *feature) getZoneFromSecret(secretName, namespace string) ([]service.ArrayConnectionData, error) {
	getSecretInformation := []string{"kubectl", "get", "secrets", "-n", namespace, secretName, "-o", "jsonpath='{.data.config}'"}
	justString := strings.Join(getSecretInformation, " ")

	result, err := execLocalCommand(justString)
	if err != nil {
		return nil, err
	}

	dec, err := base64.StdEncoding.DecodeString(string(result))
	if err != nil {
		return nil, err
	}

	arrayConnection := make([]service.ArrayConnectionData, 0)
	err = yaml.Unmarshal(dec, &arrayConnection)
	if err != nil {
		return nil, err
	}

	return arrayConnection, nil
}

func (f *feature) createZoneVolumes(fileLocation string) error {
	createPvcCmd := "kubectl apply -f templates/" + fileLocation + "/pvc.yaml"
	_, err := execLocalCommand(createPvcCmd)
	if err != nil {
		return err
	}

	createPodCmd := "kubectl apply -f templates/" + fileLocation + "/pod.yaml"
	_, err = execLocalCommand(createPodCmd)
	if err != nil {
		return err
	}

	time.Sleep(10 * time.Second)

	log.Println("[createZoneVolumes] Created volumes and pods...")
	return nil
}

func (f *feature) deleteZoneVolumes(fileLocation string) error {
	deletePodCmd := "kubectl delete -f templates/" + fileLocation + "/pod.yaml"
	_, err := execLocalCommand(deletePodCmd)
	if err != nil {
		return err
	}

	deletePvcCmd := "kubectl delete -f templates/" + fileLocation + "/pvc.yaml"
	_, err = execLocalCommand(deletePvcCmd)
	if err != nil {
		return err
	}

	time.Sleep(10 * time.Second)

	log.Println("[deleteZoneVolumes] Deleted volumes and pods...")
	return nil
}

func (f *feature) checkPodsStatus() error {
	log.Println("[checkPodsStatus] checking pods status")

	_, err := f.areAllPodsRunning()
	if err != nil {
		return fmt.Errorf("pods not ready, check pods status and then try again")
	}

	return nil
}

func (f *feature) areAllPodsRunning() ([]v1Core.Pod, error) {
	podInfo := []v1Core.Pod{}
	attempts := 0
	ready := false

	for attempts < 15 {
		getZonePods := "kubectl get pods -n " + testNamespace + " -o jsonpath='{.items}'"
		result, err := execLocalCommand(getZonePods)
		if err != nil {
			return nil, err
		}

		pods := []v1Core.Pod{}
		err = json.Unmarshal(result, &pods)
		if err != nil {
			return nil, err
		}

		for _, pod := range pods {
			if pod.Status.Phase != "Running" {
				attempts++
				time.Sleep(10 * time.Second)
				continue
			}
		}

		ready = true
		podInfo = pods
		break
	}

	if !ready {
		return nil, fmt.Errorf("pods not ready, check pods status and then try again")
	}

	log.Println("[areAllPodsRunning] All pods are ready")

	return podInfo, nil
}

func (f *feature) cordonNode() error {
	log.Println("[cordonNode] cordoning node")
	nodeToCordon := ""

	// Get the first node in the zone
	for key := range f.zoneNodeMapping {
		log.Printf("Cordoning node: %s\n", key)
		nodeToCordon = key
		break
	}

	if nodeToCordon == "" {
		return fmt.Errorf("no node to cordon found")
	}

	cordonNodeCommand := "kubectl cordon " + nodeToCordon
	_, err := execLocalCommand(cordonNodeCommand)
	if err != nil {
		return err
	}

	// When cordon, the NoSchedule taint is found
	checkNodeStatus := "kubectl get node " + nodeToCordon + " -o json | grep NoSchedule"
	result, err := execLocalCommand(checkNodeStatus)
	if err != nil {
		return err
	}

	if !strings.Contains(string(result), "NoSchedule") {
		return fmt.Errorf("node %s not cordoned", nodeToCordon)
	}

	log.Println("Cordoned node correctly")
	f.cordonedNode = nodeToCordon

	return nil
}

func (f *feature) checkPodsForCordonRun() error {
	log.Println("[checkPodsForCordonRun] checking pods status")

	pods, err := f.areAllPodsRunning()
	if err != nil {
		return fmt.Errorf("pods not ready, check pods status and then try again")
	}

	for _, pod := range pods {
		if pod.Spec.NodeName == f.cordonedNode {
			return fmt.Errorf("pod %s scheduled incorrectly", pod.ObjectMeta.Name)
		}
	}

	log.Println("[checkPodsForCordonRun] Pods scheduled correctly, reseting node...")

	// Reset node since scheduled correctly
	uncordonNodeCommand := "kubectl uncordon " + f.cordonedNode
	_, err = execLocalCommand(uncordonNodeCommand)
	if err != nil {
		return err
	}

	return nil
}

func execLocalCommand(command string) ([]byte, error) {
	var buf bytes.Buffer
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	if err != nil {
		return buf.Bytes(), err
	}

	return buf.Bytes(), nil
}

func InitializeScenario(s *godog.ScenarioContext) {
	f := &feature{}

	s.Step(`^a VxFlexOS service$`, f.aVxFlexOSService)
	s.Step(`^verify driver is configured and running correctly$`, f.isEverythingWorking)
	s.Step(`^verify zone information from secret "([^"]*)" in namespace "([^"]*)"$`, f.verifyZoneInfomation)
	s.Step(`^create zone volume and pod in "([^"]*)"$`, f.createZoneVolumes)
	s.Step(`^delete zone volume and pod in "([^"]*)"$`, f.deleteZoneVolumes)
	s.Step(`^check pods to be running on desired zones$`, f.checkPodsStatus)
	s.Step(`^cordon one node$`, f.cordonNode)
	s.Step(`^ensure pods aren't scheduled incorrectly and still running$`, f.checkPodsForCordonRun)
}
