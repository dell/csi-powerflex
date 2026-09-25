// Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package k8sutils

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8sleaderelection "k8s.io/client-go/tools/leaderelection"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsv1beta1api "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
)

func Test_CreateKubeClientSet(t *testing.T) {
	var tempConfigFunc func() (*rest.Config, error)                               // must return getInClusterConfig to its original value
	var tempClientsetFunc func(config *rest.Config) (kubernetes.Interface, error) // must return getK8sClientset to its original value

	// Save original KUBECONFIG for restoration
	originalKubeConfig := os.Getenv("KUBECONFIG")

	tests := []struct {
		name    string
		before  func() error
		after   func()
		wantErr bool
	}{
		{
			name: "success: manually set InClusterConfig with mock",
			before: func() error {
				Clientset = nil // reset Clientset before each run
				tempConfigFunc = InClusterConfigFunc
				InClusterConfigFunc = func() (*rest.Config, error) { return &rest.Config{}, nil }
				return nil
			},
			after:   func() { InClusterConfigFunc = tempConfigFunc },
			wantErr: false,
		},
		{
			name: "failure: unmocked config function",
			before: func() error {
				Clientset = nil // reset Clientset before each run
				tempConfigFunc = InClusterConfigFunc
				// Mock InClusterConfigFunc to return an error to simulate failure
				InClusterConfigFunc = func() (*rest.Config, error) {
					return nil, errors.New("unable to load in-cluster configuration")
				}
				// Clear KUBECONFIG to ensure fallback also fails
				os.Unsetenv("KUBECONFIG")
				return nil
			},
			after: func() {
				InClusterConfigFunc = tempConfigFunc
				// Restore original KUBECONFIG
				if originalKubeConfig != "" {
					os.Setenv("KUBECONFIG", originalKubeConfig)
				}
			},
			wantErr: true,
		},
		{
			name: "failure: error returned by kubernetes.NewForConfig",
			before: func() error { // overrides to get past a mock and inject a failure
				Clientset = nil // reset Clientset before each run
				tempConfigFunc = InClusterConfigFunc
				tempClientsetFunc = NewForConfigFunc
				InClusterConfigFunc = func() (*rest.Config, error) { return &rest.Config{}, nil }
				NewForConfigFunc = func(_ *rest.Config) (kubernetes.Interface, error) {
					return nil, assert.AnError
				}
				return nil
			},
			after: func() { // restore functions to their defaults
				InClusterConfigFunc = tempConfigFunc
				NewForConfigFunc = tempClientsetFunc
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.before()
			defer tt.after()

			// Test 1: Call CreateKubeClientSet() without parameters
			err := CreateKubeClientSet()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, Clientset)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, Clientset)
			}

			// Reset Clientset for the second test call
			Clientset = nil

			// Test 2: Call CreateKubeClientSet(kubeConfig) with parameters
			// For the failure test case, we need to ensure this also fails
			if tt.name == "failure: unmocked config function" {
				// For this test case, pass an invalid kubeconfig path to ensure failure
				err = CreateKubeClientSet("/invalid/path/to/kubeconfig")
			} else {
				// For other tests, use the original kubeconfig (if any)
				if originalKubeConfig != "" {
					err = CreateKubeClientSet(originalKubeConfig)
				} else {
					// If no original kubeconfig, test without parameters again
					err = CreateKubeClientSet()
				}
			}

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, Clientset)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, Clientset)
			}
		})
	}
}

func Test_LeaderElection(t *testing.T) {
	type args struct {
		clientSet kubernetes.Interface
		lockName  string
		namespace string
		runFunc   func(ctx context.Context)
	}

	type test struct {
		name    string
		args    args
		wantErr bool
	}

	testCh := make(chan bool) // channel on which the runFunc should respond
	tests := []test{
		{
			// When the leader is elected, it should call the runFunc, at which point
			// the func should return a 'true' value to the testCh channel.
			name: "successfully starts leader election",
			args: args{
				clientSet: fake.NewClientset(),
				lockName:  "driver-csi-powermax-dellemc-com",
				namespace: "powermax",
				runFunc: func(_ context.Context) {
					t.Log("leader is elected and run func is running")
					testCh <- true
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// leaderElection.Run() func never exits during normal operation.
			// If the runFunc does not write to the testCh channel within 30 seconds,
			// consider it a failed run and cancel the context.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			errCh := make(chan error)
			go func() {
				errCh <- LeaderElectionFunc(&tt.args.clientSet, tt.args.lockName, tt.args.namespace, tt.args.runFunc)
			}()

			select {
			case err := <-errCh:
				// should only reach here if there is a config error when starting the
				// leaderElector via the leaderElector.Run() func. This is difficult to achieve in this context.
				if (err != nil) != tt.wantErr {
					t.Errorf("LeaderElection failed. err: %s", err.Error())
				}
			case pass := <-testCh:
				if pass == tt.wantErr {
					t.Errorf("failed to elect a leader and call the run func")
				}
			case <-ctx.Done():
				t.Error("timed out waiting for leader election to start")
			}
		})
	}
}

type fakeMetricsLeaderElector struct {
	run func(context.Context)
}

func (f *fakeMetricsLeaderElector) Run(ctx context.Context) {
	if f.run != nil {
		f.run(ctx)
	}
}

func Test_LeaderElectionForMetrics(t *testing.T) {
	originalFactory := newMetricsLeaderElectorFunc
	defer func() {
		newMetricsLeaderElectorFunc = originalFactory
	}()

	t.Run("uses POD_NAME and triggers callbacks", func(t *testing.T) {
		t.Setenv("POD_NAME", "powerflex-controller-0")
		called := false
		newMetricsLeaderElectorFunc = func(cfg k8sleaderelection.LeaderElectionConfig) (metricsLeaderElector, error) {
			assert.Equal(t, 2*time.Second, cfg.LeaseDuration)
			assert.Equal(t, 3*time.Second, cfg.RenewDeadline)
			assert.Equal(t, 4*time.Second, cfg.RetryPeriod)
			assert.True(t, cfg.ReleaseOnCancel)
			return &fakeMetricsLeaderElector{run: func(ctx context.Context) {
				called = true
				cfg.Callbacks.OnStartedLeading(ctx)
				cfg.Callbacks.OnNewLeader("leader-1")
				cfg.Callbacks.OnStoppedLeading()
			}}, nil
		}

		clientset := fake.NewClientset()
		err := LeaderElectionForMetrics(context.Background(), clientset, "powerflex-metrics", "vxflexos", 3*time.Second, 2*time.Second, 4*time.Second, func(runCtx context.Context) {
			assert.NotNil(t, runCtx)
		})
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("falls back to X_CSI_POD_NAME", func(t *testing.T) {
		t.Setenv("POD_NAME", "")
		t.Setenv("X_CSI_POD_NAME", "powerflex-controller-1")
		newMetricsLeaderElectorFunc = func(_ k8sleaderelection.LeaderElectionConfig) (metricsLeaderElector, error) {
			return &fakeMetricsLeaderElector{}, nil
		}

		err := LeaderElectionForMetrics(context.Background(), fake.NewClientset(), "powerflex-metrics", "vxflexos", time.Second, time.Second, time.Second, func(context.Context) {})
		assert.NoError(t, err)
	})

	t.Run("returns error when elector creation fails", func(t *testing.T) {
		newMetricsLeaderElectorFunc = func(_ k8sleaderelection.LeaderElectionConfig) (metricsLeaderElector, error) {
			return nil, errors.New("leader elector creation failed")
		}

		err := LeaderElectionForMetrics(context.Background(), fake.NewClientset(), "powerflex-metrics", "vxflexos", time.Second, time.Second, time.Second, func(context.Context) {})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create leader elector")
	})
}

func TestCreateKubeClientSet_MetricsClientFailure(t *testing.T) {
	origInCluster := InClusterConfigFunc
	origNewForConfig := NewForConfigFunc
	origNewMetricsForConfig := NewMetricsForConfigFunc
	defer func() {
		InClusterConfigFunc = origInCluster
		NewForConfigFunc = origNewForConfig
		NewMetricsForConfigFunc = origNewMetricsForConfig
	}()

	Clientset = nil
	Kubeclient = nil
	InClusterConfigFunc = func() (*rest.Config, error) {
		return &rest.Config{}, nil
	}
	NewForConfigFunc = func(_ *rest.Config) (kubernetes.Interface, error) {
		return fake.NewClientset(), nil
	}
	NewMetricsForConfigFunc = func(_ *rest.Config) (*metricsv1beta1api.MetricsV1beta1Client, error) {
		return nil, errors.New("metrics client unavailable")
	}

	err := CreateKubeClientSet()
	require.NoError(t, err)
	require.NotNil(t, Clientset)
	require.NotNil(t, Kubeclient)
	assert.Nil(t, Kubeclient.MetricsClient)
}

func TestGetPodMetrics(t *testing.T) {
	t.Run("returns error when metrics client is uninitialized", func(t *testing.T) {
		k8s := &K8sClient{}
		metrics, err := k8s.GetPodMetrics(context.Background(), "default", "pod-1")
		assert.Nil(t, metrics)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "uninitialized")
	})

	t.Run("returns pod metrics when the API server responds", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/apis/metrics.k8s.io/v1beta1/namespaces/default/pods/pod-1", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(&metricsv1beta1.PodMetrics{
				TypeMeta:   metav1.TypeMeta{Kind: "PodMetrics", APIVersion: "metrics.k8s.io/v1beta1"},
				ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
				Timestamp:  metav1.NewTime(time.Unix(1700000000, 0)),
				Window:     metav1.Duration{Duration: 30 * time.Second},
				Containers: []metricsv1beta1.ContainerMetrics{{
					Name: "driver",
					Usage: v1.ResourceList{
						v1.ResourceCPU:    resourceMustParse(t, "10m"),
						v1.ResourceMemory: resourceMustParse(t, "64Mi"),
					},
				}},
			})
		}))
		defer server.Close()

		client, err := metricsv1beta1api.NewForConfig(&rest.Config{Host: server.URL})
		require.NoError(t, err)
		k8s := &K8sClient{MetricsClient: client}

		metrics, err := k8s.GetPodMetrics(context.Background(), "default", "pod-1")
		require.NoError(t, err)
		require.NotNil(t, metrics)
		assert.Equal(t, "pod-1", metrics.Name)
		assert.Equal(t, "default", metrics.Namespace)
		assert.Len(t, metrics.Containers, 1)
		assert.Equal(t, "driver", metrics.Containers[0].Name)
	})
}

func TestGetPodRestartCount(t *testing.T) {
	t.Run("returns error when clientset is uninitialized", func(t *testing.T) {
		k8s := &K8sClient{}
		count, err := k8s.GetPodRestartCount(context.Background(), "default", "pod-1", "driver")
		assert.Equal(t, int32(0), count)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "uninitialized")
	})

	t.Run("returns restart count for the requested container", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{Name: "driver", RestartCount: 7},
					{Name: "sidecar", RestartCount: 2},
				},
			},
		})
		k8s := &K8sClient{Clientset: clientset}
		count, err := k8s.GetPodRestartCount(context.Background(), "default", "pod-1", "driver")
		require.NoError(t, err)
		assert.Equal(t, int32(7), count)
	})

	t.Run("returns error when the requested container is missing", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "default"},
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{{Name: "driver", RestartCount: 1}},
			},
		})
		k8s := &K8sClient{Clientset: clientset}
		count, err := k8s.GetPodRestartCount(context.Background(), "default", "pod-2", "missing")
		assert.Equal(t, int32(0), count)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetPodRestartCountAuto(t *testing.T) {
	t.Run("returns restart count for known CSI driver container names", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-3", Namespace: "default"},
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{Name: "driver", RestartCount: 4},
					{Name: "sidecar", RestartCount: 1},
				},
			},
		})
		k8s := &K8sClient{Clientset: clientset}
		count, err := k8s.GetPodRestartCountAuto(context.Background(), "default", "pod-3")
		require.NoError(t, err)
		assert.Equal(t, int32(4), count)
	})

	t.Run("falls back to the first container when no known CSI driver name matches", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-4", Namespace: "default"},
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{Name: "app", RestartCount: 9},
					{Name: "helper", RestartCount: 3},
				},
			},
		})
		k8s := &K8sClient{Clientset: clientset}
		count, err := k8s.GetPodRestartCountAuto(context.Background(), "default", "pod-4")
		require.NoError(t, err)
		assert.Equal(t, int32(9), count)
	})

	t.Run("returns error when pod has no containers", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-5", Namespace: "default"},
		})
		k8s := &K8sClient{Clientset: clientset}
		count, err := k8s.GetPodRestartCountAuto(context.Background(), "default", "pod-5")
		assert.Equal(t, int32(0), count)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no containers found")
	})
}

func resourceMustParse(t *testing.T, value string) resource.Quantity {
	t.Helper()
	qty, err := resource.ParseQuantity(value)
	require.NoError(t, err)
	return qty
}
