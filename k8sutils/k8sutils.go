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
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Ecosystems/container-storage-modules/src/csmlog"
	"github.com/kubernetes-csi/csi-lib-utils/leaderelection"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	k8sleaderelection "k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsv1beta1api "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
)

// Clientset - Interface to kubernetes
var Clientset kubernetes.Interface

// Kubeclient - Global K8sClient instance
var Kubeclient *K8sClient

// K8sClient holds Kubernetes client instances
type K8sClient struct {
	Clientset     kubernetes.Interface
	MetricsClient *metricsv1beta1api.MetricsV1beta1Client
}

var log = csmlog.GetLogger()

type leaderElection interface {
	Run() error
	WithNamespace(namespace string)
}

type metricsLeaderElector interface {
	Run(ctx context.Context)
}

// CreateKubeClientSet - Returns kubeclient set
func CreateKubeClientSet(kubeConfig ...string) error {
	Kubeclient = &K8sClient{}

	config, err := InClusterConfigFunc()
	if err != nil {
		if len(kubeConfig) == 0 {
			return err
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig[0])
		if err != nil {
			return err
		}
	}

	// creates the clientset
	Kubeclient.Clientset, err = NewForConfigFunc(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %s", err.Error())
	}

	// Set backward compatibility variable
	Clientset = Kubeclient.Clientset

	// Initialize metrics client (optional, may fail if metrics-server not installed)
	Kubeclient.MetricsClient, err = NewMetricsForConfigFunc(config)
	if err != nil {
		log.Warnf("Failed to create metrics client (metrics-server may not be installed): %v", err)
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

var NewMetricsForConfigFunc = func(config *rest.Config) (*metricsv1beta1api.MetricsV1beta1Client, error) {
	return metricsv1beta1api.NewForConfig(config)
}

// LeaderElection - Initializes Leader election
var LeaderElectionFunc = func(clientset *kubernetes.Interface, lockName string, namespace string, runFunc func(ctx context.Context)) error {
	le := leaderelection.NewLeaderElection(*clientset, lockName, runFunc)
	le.WithNamespace(namespace)
	return le.Run()
}

// LeaderElectionForMetrics initializes leader election for array-level metrics collection.
// It uses the POD_NAME environment variable when available and falls back to the hostname.
// If hostname lookup fails, the lock name is used as the identity.
var LeaderElectionForMetrics = func(ctx context.Context, clientset kubernetes.Interface, lockName string, namespace string,
	leaderElectionRenewDeadline, leaderElectionLeaseDuration, leaderElectionRetryPeriod time.Duration, runFunc func(ctx context.Context),
) error {
	identity := os.Getenv("POD_NAME")
	if identity == "" {
		identity = os.Getenv("X_CSI_POD_NAME")
	}
	if identity == "" {
		var err error
		identity, err = os.Hostname()
		if err != nil {
			identity = lockName
		}
	}

	log.Infof("Starting metrics leader election for %s in namespace %s with identity %s", lockName, namespace, identity)

	rl, err := resourcelock.New(resourcelock.LeasesResourceLock, namespace, lockName, clientset.CoreV1(), clientset.CoordinationV1(), resourcelock.ResourceLockConfig{
		Identity: identity,
	})
	if err != nil {
		return fmt.Errorf("failed to create resource lock: %w", err)
	}

	leConfig := k8sleaderelection.LeaderElectionConfig{
		Lock:            rl,
		LeaseDuration:   leaderElectionLeaseDuration,
		RenewDeadline:   leaderElectionRenewDeadline,
		RetryPeriod:     leaderElectionRetryPeriod,
		ReleaseOnCancel: true,
		Callbacks: k8sleaderelection.LeaderCallbacks{
			OnStartedLeading: func(runCtx context.Context) {
				log.Info("Started leading metrics collection")
				runFunc(runCtx)
			},
			OnStoppedLeading: func() {
				log.Info("Stopped leading metrics collection")
			},
			OnNewLeader: func(newLeader string) {
				log.Infof("New metrics leader elected: %s", newLeader)
			},
		},
	}

	le, err := newMetricsLeaderElectorFunc(leConfig)
	if err != nil {
		return fmt.Errorf("failed to create leader elector: %w", err)
	}

	le.Run(ctx)
	return nil
}

var newMetricsLeaderElectorFunc = func(cfg k8sleaderelection.LeaderElectionConfig) (metricsLeaderElector, error) {
	return k8sleaderelection.NewLeaderElector(cfg)
}

// GetPodMetrics retrieves metrics for the specified pod
func (k8s *K8sClient) GetPodMetrics(ctx context.Context, namespace, podName string) (*metricsv1beta1.PodMetrics, error) {
	if k8s.MetricsClient == nil {
		return nil, errors.New("metrics client is uninitialized")
	}

	return k8s.MetricsClient.PodMetricses(namespace).Get(ctx, podName, metav1.GetOptions{})
}

// GetPodRestartCount retrieves the restart count for a specific container in a pod
func (k8s *K8sClient) GetPodRestartCount(ctx context.Context, namespace, podName, containerName string) (int32, error) {
	if k8s.Clientset == nil {
		return 0, errors.New("kubernetes client is uninitialized")
	}

	pod, err := k8s.Clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to get pod %s in namespace %s: %w", podName, namespace, err)
	}

	// Find the specified container and return its restart count
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name == containerName {
			return containerStatus.RestartCount, nil
		}
	}

	return 0, fmt.Errorf("container %s not found in pod %s", containerName, podName)
}

// GetPodRestartCountAuto retrieves the restart count for the CSI driver container by auto-detecting the container name
func (k8s *K8sClient) GetPodRestartCountAuto(ctx context.Context, namespace, podName string) (int32, error) {
	if k8s.Clientset == nil {
		return 0, errors.New("kubernetes client is uninitialized")
	}

	pod, err := k8s.Clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to get pod %s in namespace %s: %w", podName, namespace, err)
	}

	// Auto-detect the CSI driver container by looking for common CSI driver container names
	// Priority: "csi-vxflexos", "driver", or the first container
	containerNames := []string{"csi-vxflexos", "driver"}

	// First try known container names
	for _, containerStatus := range pod.Status.ContainerStatuses {
		for _, knownName := range containerNames {
			if strings.Contains(containerStatus.Name, knownName) {
				return containerStatus.RestartCount, nil
			}
		}
	}

	// Fallback to first container if known names not found
	if len(pod.Status.ContainerStatuses) > 0 {
		return pod.Status.ContainerStatuses[0].RestartCount, nil
	}

	return 0, fmt.Errorf("no containers found in pod %s", podName)
}
