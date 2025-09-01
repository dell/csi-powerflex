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
	"strconv"
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
	testNamespace   = "vxflexos-test"
	storageClass    = "vxflexos-az-wait"
)

type feature struct {
	errs             []error
	zoneNodeMapping  map[string]string
	zoneKey          string
	supportedZones   []string
	cordonedNode     string
	zoneReplicaCount int32
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
		return fmt.Errorf("namespace %s or %s not found", driverNamespace, testNamespace)
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
	createStsCmd := "kubectl apply -f templates/" + fileLocation + "/sts.yaml"
	_, err := execLocalCommand(createStsCmd)
	if err != nil {
		return err
	}

	time.Sleep(10 * time.Second)

	log.Println("[createZoneVolumes] Created volumes and pods...")
	return nil
}

func (f *feature) deleteZoneVolumes(fileLocation string) error {
	deleteStsCmd := "kubectl delete -f templates/" + fileLocation + "/sts.yaml"
	_, err := execLocalCommand(deleteStsCmd)
	if err != nil {
		return err
	}

	time.Sleep(10 * time.Second)

	log.Println("[deleteZoneVolumes] Deleted volumes and pods...")
	return nil
}

func (f *feature) checkStatfulSetStatus() error {
	err := f.isStatefulSetReady()
	if err != nil {
		return err
	}

	log.Println("[checkStatfulSetStatus] Statefulset and zone pods are ready")

	return nil
}

func (f *feature) isStatefulSetReady() error {
	ready := false
	attempts := 0

	for attempts < 15 {
		getStsCmd := "kubectl get sts -n " + testNamespace + " vxflextest-az -o json"
		result, err := execLocalCommand(getStsCmd)
		if err != nil {
			return err
		}

		sts := v1.StatefulSet{}
		err = json.Unmarshal(result, &sts)
		if err != nil {
			return err
		}

		// Everything should be ready.
		if *sts.Spec.Replicas == sts.Status.ReadyReplicas {
			ready = true
			f.zoneReplicaCount = sts.Status.ReadyReplicas
			break
		}

		attempts++
		time.Sleep(10 * time.Second)
	}

	if !ready {
		return fmt.Errorf("statefulset not ready, check statefulset status and then try again")
	}

	return nil
}

func (f *feature) getStatefulSetPods() ([]v1Core.Pod, error) {
	getZonePods := "kubectl get pods -n " + testNamespace + " -l app=vxflextest-az -o jsonpath='{.items}'"
	result, err := execLocalCommand(getZonePods)
	if err != nil {
		return nil, err
	}

	pods := []v1Core.Pod{}
	err = json.Unmarshal(result, &pods)
	if err != nil {
		return nil, err
	}

	log.Println("[getStatefulSetPods] Pods found: ", len(pods))

	return pods, nil
}

func (f *feature) cordonNode() error {
	nodeToCordon := ""

	// Get the first node in the zone
	for key := range f.zoneNodeMapping {
		log.Printf("[cordonNode] Cordoning node: %s\n", key)
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

	log.Println("[cordonNode] Cordoned node correctly")
	f.cordonedNode = nodeToCordon

	return nil
}

func (f *feature) checkPodsForCordonRun() error {
	log.Println("[checkPodsForCordonRun] checking pods status")

	pods, err := f.getStatefulSetPods()
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

func (f *feature) createZoneSnapshotsAndRestore(location string) error {
	log.Println("[createZoneSnapshotsAndRestore] Creating snapshots and restores")
	templateFile := "templates/" + location + "/snapshot.yaml"
	updatedTemplateFile := "templates/" + location + "/snapshot-updated.yaml"

	for i := 0; i < int(f.zoneReplicaCount); i++ {
		time.Sleep(10 * time.Second)

		cpCmd := "cp " + templateFile + " " + updatedTemplateFile
		b, err := execLocalCommand(cpCmd)
		if err != nil {
			return fmt.Errorf("failed to copy template file: %v\nErrMessage:\n%s", err, string(b))
		}

		// Update iteration and apply...
		err = replaceInFile("ITERATION", strconv.Itoa(i), updatedTemplateFile)
		if err != nil {
			return err
		}

		createSnapshot := "kubectl apply -f " + updatedTemplateFile
		_, err = execLocalCommand(createSnapshot)
		if err != nil {
			return err
		}
	}

	log.Println("[createZoneSnapshotsAndRestore] Snapshots and restores created")

	return nil
}

func (f *feature) createZoneClonesAndRestore(location string) error {
	log.Println("[createZoneClonesAndRestore] Creating clones and restores")
	templateFile := "templates/" + location + "/clone.yaml"
	updatedTemplateFile := "templates/" + location + "/clone-updated.yaml"

	for i := 0; i < int(f.zoneReplicaCount); i++ {
		time.Sleep(10 * time.Second)

		cpCmd := "cp " + templateFile + " " + updatedTemplateFile
		b, err := execLocalCommand(cpCmd)
		if err != nil {
			return fmt.Errorf("failed to copy template file: %v\nErrMessage:\n%s", err, string(b))
		}

		// Update iteration and apply...
		err = replaceInFile("ITERATION", strconv.Itoa(i), updatedTemplateFile)
		if err != nil {
			return err
		}

		createClone := "kubectl apply -f " + updatedTemplateFile
		_, err = execLocalCommand(createClone)
		if err != nil {
			return err
		}
	}

	log.Println("[createZoneClonesAndRestore] Clones and restores created")

	return nil
}

func (f *feature) deleteZoneSnapshotsAndRestore(location string) error {
	log.Println("[deleteZoneSnapshotsAndRestore] Deleting restores and snapshots")
	templateFile := "templates/" + location + "/snapshot.yaml"
	updatedTemplateFile := "templates/" + location + "/snapshot-updated.yaml"

	for i := 0; i < int(f.zoneReplicaCount); i++ {
		time.Sleep(10 * time.Second)

		cpCmd := "cp " + templateFile + " " + updatedTemplateFile
		b, err := execLocalCommand(cpCmd)
		if err != nil {
			return fmt.Errorf("failed to copy template file: %v\nErrMessage:\n%s", err, string(b))
		}

		// Update iteration and apply...
		err = replaceInFile("ITERATION", strconv.Itoa(i), updatedTemplateFile)
		if err != nil {
			return err
		}

		deleteSnapshot := "kubectl delete -f " + updatedTemplateFile
		_, err = execLocalCommand(deleteSnapshot)
		if err != nil {
			return err
		}
	}

	log.Println("[deleteZoneSnapshotsAndRestore] Snapshots and restores deleted")

	return nil
}

func (f *feature) deleteZoneClonesAndRestore(location string) error {
	log.Println("[deleteZoneClonesAndRestore] Deleting restores and clones")
	templateFile := "templates/" + location + "/clone.yaml"
	updatedTemplateFile := "templates/" + location + "/clone-updated.yaml"

	for i := 0; i < int(f.zoneReplicaCount); i++ {
		time.Sleep(10 * time.Second)

		cpCmd := "cp " + templateFile + " " + updatedTemplateFile
		b, err := execLocalCommand(cpCmd)
		if err != nil {
			return fmt.Errorf("failed to copy template file: %v\nErrMessage:\n%s", err, string(b))
		}

		// Update iteration and apply...
		err = replaceInFile("ITERATION", strconv.Itoa(i), updatedTemplateFile)
		if err != nil {
			return err
		}

		deleteClone := "kubectl delete -f " + updatedTemplateFile
		_, err = execLocalCommand(deleteClone)
		if err != nil {
			return err
		}
	}

	log.Println("[deleteZoneClonesAndRestore] Clones and restores deleted")

	return nil
}

func (f *feature) areAllRestoresRunning() error {
	log.Println("[areAllRestoresRunning] Checking if all restores are running")

	complete := false
	attempts := 0
	for attempts < 15 {
		getZonePods := "kubectl get pods -n " + testNamespace + " -o jsonpath='{.items}'"
		result, err := execLocalCommand(getZonePods)
		if err != nil {
			return err
		}

		pods := []v1Core.Pod{}
		err = json.Unmarshal(result, &pods)
		if err != nil {
			return err
		}

		runningCount := 0
		for _, pod := range pods {
			if !strings.Contains(pod.ObjectMeta.Name, "maz-restore") {
				continue
			}

			if pod.Status.Phase == "Running" {
				runningCount++
			}
		}

		if runningCount != int(f.zoneReplicaCount) {
			time.Sleep(10 * time.Second)
			continue
		}

		complete = true
		break

	}

	if !complete {
		return fmt.Errorf("all restores not running, check pods status containing maz-restore and then try again")
	}

	return nil
}

func replaceInFile(oldString, newString, templateFile string) error {
	cmdString := "s|" + oldString + "|" + newString + "|g"
	replaceCmd := fmt.Sprintf("sed -i '%s' %s", cmdString, templateFile)
	_, err := execLocalCommand(replaceCmd)
	if err != nil {
		return fmt.Errorf("failed to substitute %s with %s in file %s: %s", oldString, newString, templateFile, err.Error())
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
	s.Step(`^check the statefulset for zones$`, f.checkStatfulSetStatus)
	s.Step(`^cordon one node$`, f.cordonNode)
	s.Step(`^ensure pods aren't scheduled incorrectly and still running$`, f.checkPodsForCordonRun)
	s.Step(`^create snapshots for zone volumes and restore in "([^"]*)"$`, f.createZoneSnapshotsAndRestore)
	s.Step(`^delete snapshots for zone volumes and restore in "([^"]*)"$`, f.deleteZoneSnapshotsAndRestore)
	s.Step(`^all zone restores are running$`, f.areAllRestoresRunning)
	s.Step(`^create clones for zone volumes and restore in "([^"]*)"$`, f.createZoneClonesAndRestore)
	s.Step(`^delete clones for zone volumes and restore in "([^"]*)"$`, f.deleteZoneClonesAndRestore)
}
