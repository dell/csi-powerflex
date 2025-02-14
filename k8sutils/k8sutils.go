// Copyright © 2020-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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

package k8sutils

import (
	"context"

	"github.com/kubernetes-csi/csi-lib-utils/leaderelection"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Clientset - Interface to kubernetes
var Clientset kubernetes.Interface

type leaderElection interface {
	Run() error
	WithNamespace(namespace string)
}

// CreateKubeClientSet - Returns kubeclient set
func CreateKubeClientSet() error {
	config, err := InClusterConfigFunc()
	if err != nil {
		return err
	}

	// creates the clientset
	Clientset, err = NewForConfigFunc(config)
	if err != nil {
		return err
	}
	return nil
}

// used for unit testing -
// allows CreateKubeClientSet to be mocked
var InClusterConfigFunc = func() (*rest.Config, error) {
	return rest.InClusterConfig()
}

var NewForConfigFunc = func(config *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(config)
}

// LeaderElection - Initializes Leader election
var LeaderElectionFunc = func(clientset *kubernetes.Interface, lockName string, namespace string, runFunc func(ctx context.Context)) error {
	le := leaderelection.NewLeaderElection(*clientset, lockName, runFunc)
	le.WithNamespace(namespace)
	return le.Run()
}
