package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/dell/csi-vxflexos/v2/k8sutils"
	"github.com/dell/gocsi"
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func Test_main(t *testing.T) {
	defaultRunFunc := driverRunFunc
	defaultGetKubeClientSetFunc := getKubeClientSetFunc
	defaultRunLeaderElectionFunc := runWithLeaderElectionFunc
	defaultInitFlagsFunc := initFlagsFunc
	defaultCheckConfigsFunc := checkConfigsFunc
	defaultPreInitCheckFunc := preInitCheckFunc
	defaultK8sConfigFunc := k8sutils.InClusterConfigFunc
	defaultK8sClientsetFunc := k8sutils.NewForConfigFunc
	defaultK8sLeaderElectionFunc := k8sutils.LeaderElectionFunc

	afterEach := func() {
		flags.enableLeaderElection = nil
		flags.leaderElectionNamespace = nil
		flags.kubeconfig = nil
		driverRunFunc = defaultRunFunc
		getKubeClientSetFunc = defaultGetKubeClientSetFunc
		runWithLeaderElectionFunc = defaultRunLeaderElectionFunc
		initFlagsFunc = defaultInitFlagsFunc
		checkConfigsFunc = defaultCheckConfigsFunc
		preInitCheckFunc = defaultPreInitCheckFunc
		k8sutils.InClusterConfigFunc = defaultK8sConfigFunc
		k8sutils.NewForConfigFunc = defaultK8sClientsetFunc
		k8sutils.LeaderElectionFunc = defaultK8sLeaderElectionFunc
	}

	runningCh := make(chan string)
	tests := []struct {
		name  string
		setup func()
		want  string
	}{
		{
			name: "execute main() without leader election",
			setup: func() {
				driverRunFunc = func(_ context.Context) {
					runningCh <- "running"
				}
				initFlagsFunc = func() {
					LEEnabled := false
					LENamespace := ""
					kubeconfig := ""
					flags.enableLeaderElection = &LEEnabled
					flags.leaderElectionNamespace = &LENamespace
					flags.kubeconfig = &kubeconfig
				}
				checkConfigsFunc = func() {
				}
				preInitCheckFunc = func() {
				}
				runWithLeaderElectionFunc = func(_ kubernetes.Interface, _ string, _ string, _ func(_ context.Context)) error {
					return nil
				}
				k8sutils.InClusterConfigFunc = func() (*rest.Config, error) {
					return &rest.Config{}, nil
				}
				k8sutils.NewForConfigFunc = func(config *rest.Config) (*kubernetes.Clientset, error) {
					return kubernetes.NewForConfig(config)
				}
				k8sutils.LeaderElectionFunc = func(clientset *kubernetes.Interface, lockName string, namespace string, runFunc func(ctx context.Context)) error {
					return nil
				}
			},
			want: "running",
		}, /*
			{
				name: "execute main() with leader election",
				setup: func() {
					driverRunFunc = func(_ context.Context) {
						runningCh <- "running"
					}
					initFlagsFunc = func() {
						LEEnabled := true
						LENamespace := ""
						kubeconfig := ""
						flags.enableLeaderElection = &LEEnabled
						flags.leaderElectionNamespace = &LENamespace
						flags.kubeconfig = &kubeconfig
					}
					getKubeClientSetFunc = func() error {
						k8sutils.Clientset = fake.NewClientset()
						return nil
					}
					checkConfigsFunc = func() {
					}
					preInitCheckFunc = func() {
					}
					runWithLeaderElectionFunc = func(_ kubernetes.Interface, _ string, _ string, _ func(_ context.Context)) error {
						return nil
					}
					k8sutils.GetInClusterConfig = func() (*rest.Config, error) {
						return &rest.Config{}, nil
					}
					k8sutils.GetK8sClientset = func(config *rest.Config) (*kubernetes.Clientset, error) {
						return kubernetes.NewForConfig(config)
					}

				},
				want: "running",
			},*/
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer afterEach()
			tt.setup()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			go main()

			select {
			case running := <-runningCh:
				if running != tt.want {
					t.Errorf("main() = %v, want %v", running, tt.want)
				}
			case <-ctx.Done():
				t.Errorf("timed out waiting for driver to run")
			}
		})
	}
}

func Test_driverRun(t *testing.T) {
	// capture defaults and reset after each test for a clean testing env.
	defaultRunFunc := driverRunFunc
	defaultGetKubeClientSetFunc := getKubeClientSetFunc
	defaultRunLeaderElectionFunc := runWithLeaderElectionFunc
	afterEach := func() {
		flags.enableLeaderElection = nil
		flags.leaderElectionNamespace = nil
		flags.kubeconfig = nil
		driverRunFunc = defaultRunFunc
		getKubeClientSetFunc = defaultGetKubeClientSetFunc
		runWithLeaderElectionFunc = defaultRunLeaderElectionFunc
	}

	runningCh := make(chan string)
	tests := []struct {
		name    string
		setup   func()
		want    string
		wantErr bool
		errMsg  string
	}{
		{
			name: "run the driver without leader election",
			setup: func() {
				enableLE := false

				// assign values to necessary flags
				flags.enableLeaderElection = &enableLE
				driverRunFunc = func(_ context.Context) {
					runningCh <- "running"
				}
			},
			want:    "running",
			wantErr: false,
			errMsg:  "",
		},
		{
			name: "run the driver with leader election",
			setup: func() {
				enableLE := true
				LENamespace := "powermax"
				kubeconfigFilepath := "./some/path"

				getKubeClientSetFunc = func() error {
					k8sutils.Clientset = fake.NewClientset()
					return nil
				}

				// assign values to necessary flags
				flags.enableLeaderElection = &enableLE
				flags.leaderElectionNamespace = &LENamespace
				flags.kubeconfig = &kubeconfigFilepath

				driverRunFunc = func(_ context.Context) {
					runningCh <- "running"
				}
			},
			want:    "running",
			wantErr: false,
			errMsg:  "",
		},
		{
			name: "fail to create a kube clientset",
			setup: func() {
				enableLE := true
				LENamespace := "powermax"
				kubeconfigFilepath := "./some/path"

				// assign values to necessary flags
				flags.enableLeaderElection = &enableLE
				flags.leaderElectionNamespace = &LENamespace
				flags.kubeconfig = &kubeconfigFilepath
				driverRunFunc = func(_ context.Context) {
					runningCh <- "should not be running"
				}
			},
			want:    "",
			wantErr: true,
			errMsg:  "",
		},
		{
			name: "leader election fails",
			setup: func() {
				enableLE := true
				LENamespace := "powermax"
				kubeconfigFilepath := "./some/path"

				getKubeClientSetFunc = func() error {
					k8sutils.Clientset = fake.NewClientset()
					return nil
				}
				runWithLeaderElectionFunc = func(_ kubernetes.Interface, _ string, _ string, _ func(context.Context)) error {
					return errors.New("error, leader election failed")
				}

				// assign values to necessary flags
				flags.enableLeaderElection = &enableLE
				flags.leaderElectionNamespace = &LENamespace
				flags.kubeconfig = &kubeconfigFilepath

				driverRunFunc = func(_ context.Context) {
					runningCh <- "should not be running"
				}
			},
			want:    "",
			wantErr: true,
			errMsg:  "error, leader election failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer afterEach()

			tt.setup()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()

			err := make(chan error)
			go func() {
				err <- driverRun()
			}()

			select {
			case running := <-runningCh:
				if running != tt.want {
					t.Errorf("driverRun() = %v, want %v", running, tt.want)
				}
			case e := <-err:
				if (e != nil) != tt.wantErr {
					t.Errorf("driverRun() error = %v, wantErr %v", e, tt.wantErr)
				} else {
					assert.ErrorContains(t, e, tt.errMsg)
				}
			case <-ctx.Done():
				t.Errorf("driverRun() timed out")
			}
		})
	}
}

func Test_setEnvs(t *testing.T) {
	tests := []struct {
		name string
		want map[string]string
	}{
		{
			name: "execute setEnvs()",
			want: map[string]string{
				gocsi.EnvVarDebug:      "false",
				gocsi.EnvVarReqLogging: "true",
				gocsi.EnvVarRepLogging: "true",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnvsFunc()

			for envVar, expected := range tt.want {
				assert.Equal(t, expected, os.Getenv(envVar))
			}
		})
	}
}

func Test_initFlags(t *testing.T) {
	type testFlags struct {
		enableLeaderElection    bool
		leaderElectionNamespace string
		kubeconfig              string
	}
	tests := []struct {
		name string
		want testFlags
	}{
		{
			name: "execute initFlags()",
			want: testFlags{
				enableLeaderElection:    false,
				leaderElectionNamespace: "",
				kubeconfig:              "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initFlagsFunc()

			assert.Equal(t, tt.want.enableLeaderElection, *flags.enableLeaderElection)
			assert.Equal(t, tt.want.leaderElectionNamespace, *flags.leaderElectionNamespace)
			assert.Equal(t, tt.want.kubeconfig, *flags.kubeconfig)
		})
	}
}
