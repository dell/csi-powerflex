// Copyright © 2020-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"fmt"
	"os"

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
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	// creates the clientset
	Clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	return nil
}

// LeaderElection - Initializes Leader election
func LeaderElection(clientset *kubernetes.Interface, lockName string, namespace string, runFunc func(ctx context.Context)) {
	le := leaderelection.NewLeaderElection(*clientset, lockName, runFunc)
	le.WithNamespace(namespace)
	if err := le.Run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to initialize leader election: %v", err)
		os.Exit(1)
	}
}
