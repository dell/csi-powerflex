package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/dell/csi-vxflexos/v2/k8sutils"
	"github.com/dell/gocsi"
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes"
	fake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func Test_main(t *testing.T) {
	// capture defaults and reset after each test for a clean testing env.
	defaultRunFunc := driverRunFunc
	defaultRunLoopFunc := driverRunLoopFunc
	defaultInitFlagsFunc := initFlagsFunc
	defaultCheckConfigsFunc := checkConfigsFunc
	defaultPreInitCheckFunc := preInitCheckFunc
	defaultK8sConfigFunc := k8sutils.InClusterConfigFunc
	defaultK8sClientsetFunc := k8sutils.NewForConfigFunc
	defaultK8sLEFunc := k8sutils.LeaderElectionFunc

	// UT will always fail if os.Exit(1) is called, so override it with a do-nothing by default
	// make sure to override this with an appropriate channel output or similar if testing a failure!
	defaultForceExit := func() {}

	afterEach := func() {
		flags.arrayConfigfile = nil
		flags.driverConfigParamsfile = nil
		flags.enableLeaderElection = nil
		flags.leaderElectionNamespace = nil
		flags.kubeconfig = nil
		driverRunFunc = defaultRunFunc
		driverRunLoopFunc = defaultRunLoopFunc
		initFlagsFunc = defaultInitFlagsFunc
		checkConfigsFunc = defaultCheckConfigsFunc
		preInitCheckFunc = defaultPreInitCheckFunc
		forceExit = defaultForceExit
		k8sutils.InClusterConfigFunc = defaultK8sConfigFunc
		k8sutils.NewForConfigFunc = defaultK8sClientsetFunc
		k8sutils.LeaderElectionFunc = defaultK8sLEFunc
	}

	beforeEach := func() {
		k8sutils.InClusterConfigFunc = func() (*rest.Config, error) {
			return &rest.Config{}, nil
		}
		k8sutils.NewForConfigFunc = func(_ *rest.Config) (kubernetes.Interface, error) {
			return fake.NewClientset(), nil
		}
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
				driverRunLoopFunc = func(_ context.Context) {
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
				checkConfigsFunc = func() error {
					return nil
				}
				preInitCheckFunc = func() (bool, error) {
					return false, nil
				}
				k8sutils.LeaderElectionFunc = func(_ *kubernetes.Interface, _ string, _ string, _ func(_ context.Context)) error {
					return nil
				}
			},
			want: "running",
		},
		{
			name: "execute main() with leader election",
			setup: func() {
				driverRunLoopFunc = func(_ context.Context) {
				}
				initFlagsFunc = func() {
					LEEnabled := true
					LENamespace := "vxflexos"
					kubeconfig := ""
					flags.enableLeaderElection = &LEEnabled
					flags.leaderElectionNamespace = &LENamespace
					flags.kubeconfig = &kubeconfig
				}
				checkConfigsFunc = func() error {
					return nil
				}
				preInitCheckFunc = func() (bool, error) {
					return false, nil
				}
				k8sutils.LeaderElectionFunc = func(_ *kubernetes.Interface, _ string, _ string, _ func(_ context.Context)) error {
					runningCh <- "running"
					return nil
				}

			},
			want: "running",
		},
		{
			name: "fail main() at checkConfigsFunc",
			setup: func() {
				driverRunLoopFunc = func(_ context.Context) {
				}
				initFlagsFunc = func() {
					LEEnabled := true
					LENamespace := "vxflexos"
					kubeconfig := ""
					flags.enableLeaderElection = &LEEnabled
					flags.leaderElectionNamespace = &LENamespace
					flags.kubeconfig = &kubeconfig
				}
				checkConfigsFunc = func() error {
					runningCh <- "fail"
					return errors.New("some error")
				}
				preInitCheckFunc = func() (bool, error) {
					return false, nil
				}
				k8sutils.LeaderElectionFunc = func(_ *kubernetes.Interface, _ string, _ string, _ func(_ context.Context)) error {
					return nil
				}

			},
			want: "fail",
		},
		{
			name: "fail main() at preinitCheckFunc",
			setup: func() {
				driverRunLoopFunc = func(_ context.Context) {
				}
				initFlagsFunc = func() {
					LEEnabled := true
					LENamespace := "vxflexos"
					kubeconfig := ""
					flags.enableLeaderElection = &LEEnabled
					flags.leaderElectionNamespace = &LENamespace
					flags.kubeconfig = &kubeconfig
				}
				checkConfigsFunc = func() error {
					return nil
				}
				preInitCheckFunc = func() (bool, error) {
					runningCh <- "fail"
					return false, errors.New("some error")
				}
				k8sutils.LeaderElectionFunc = func(_ *kubernetes.Interface, _ string, _ string, _ func(_ context.Context)) error {
					return nil
				}

			},
			want: "fail",
		},
		{
			name: "fail main() at driverRun",
			setup: func() {
				driverRunFunc = func() error {
					runningCh <- "not running"
					return errors.New("some error")
				}
				initFlagsFunc = func() {
					LEEnabled := true
					LENamespace := "vxflexos"
					kubeconfig := ""
					flags.enableLeaderElection = &LEEnabled
					flags.leaderElectionNamespace = &LENamespace
					flags.kubeconfig = &kubeconfig
				}

				checkConfigsFunc = func() error {
					return nil
				}
				preInitCheckFunc = func() (bool, error) {
					return false, errors.New("some error")
				}
				k8sutils.LeaderElectionFunc = func(_ *kubernetes.Interface, _ string, _ string, _ func(_ context.Context)) error {
					return nil
				}

			},
			want: "not running",
		},
		{
			name: "end main() at preinitCheckFunc",
			setup: func() {
				driverRunLoopFunc = func(_ context.Context) {
				}
				initFlagsFunc = func() {
					LEEnabled := true
					LENamespace := "vxflexos"
					kubeconfig := ""
					flags.enableLeaderElection = &LEEnabled
					flags.leaderElectionNamespace = &LENamespace
					flags.kubeconfig = &kubeconfig
				}
				checkConfigsFunc = func() error {
					return nil
				}
				preInitCheckFunc = func() (bool, error) {
					runningCh <- "graceful stop"
					return true, nil
				}
				k8sutils.LeaderElectionFunc = func(_ *kubernetes.Interface, _ string, _ string, _ func(_ context.Context)) error {
					return nil
				}

			},
			want: "graceful stop",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beforeEach()
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
	defaultRunFunc := driverRunLoopFunc
	defaultK8sConfigFunc := k8sutils.InClusterConfigFunc
	defaultK8sClientsetFunc := k8sutils.NewForConfigFunc
	defaultK8sLEFunc := k8sutils.LeaderElectionFunc

	afterEach := func() {
		flags.enableLeaderElection = nil
		flags.leaderElectionNamespace = nil
		flags.kubeconfig = nil
		driverRunLoopFunc = defaultRunFunc
		k8sutils.InClusterConfigFunc = defaultK8sConfigFunc
		k8sutils.NewForConfigFunc = defaultK8sClientsetFunc
		k8sutils.LeaderElectionFunc = defaultK8sLEFunc

	}
	beforeEach := func() {
		k8sutils.InClusterConfigFunc = func() (*rest.Config, error) {
			return &rest.Config{}, nil
		}
		k8sutils.NewForConfigFunc = func(_ *rest.Config) (kubernetes.Interface, error) {
			return fake.NewClientset(), nil
		}
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
				driverRunLoopFunc = func(_ context.Context) {
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
				LENamespace := "vxflexos"
				kubeconfigFilepath := "./some/path"

				// assign values to necessary flags
				flags.enableLeaderElection = &enableLE
				flags.leaderElectionNamespace = &LENamespace
				flags.kubeconfig = &kubeconfigFilepath

				driverRunLoopFunc = func(_ context.Context) {
					runningCh <- "running"
				}
			},
			want:    "running",
			wantErr: false,
			errMsg:  "",
		},
		{
			name: "run the driver with leader election but fail LE",
			setup: func() {
				enableLE := true
				LENamespace := "vxflexos"
				kubeconfigFilepath := "./some/path"

				// assign values to necessary flags
				flags.enableLeaderElection = &enableLE
				flags.leaderElectionNamespace = &LENamespace
				flags.kubeconfig = &kubeconfigFilepath

				driverRunLoopFunc = func(_ context.Context) {
				}

				k8sutils.LeaderElectionFunc = func(clientSet *kubernetes.Interface, lockName string, namespace string, runFunc func(ctx context.Context)) error {
					runningCh <- "fail"
					return errors.New("injected k8s error")
				}
			},
			want:    "fail",
			wantErr: true,
			errMsg:  "",
		},
		{
			name: "fail to create a kube clientset",
			setup: func() {
				enableLE := true
				LENamespace := "vxflexos"
				kubeconfigFilepath := "./some/path"

				// assign values to necessary flags
				flags.enableLeaderElection = &enableLE
				flags.leaderElectionNamespace = &LENamespace
				flags.kubeconfig = &kubeconfigFilepath
				driverRunLoopFunc = func(_ context.Context) {
					runningCh <- "fail"
				}

				// make kubeclientset fail
				k8sutils.NewForConfigFunc = func(config *rest.Config) (kubernetes.Interface, error) {
					return nil, errors.New("kubeclientset fail")
				}
			},
			want:    "fail",
			wantErr: true,
			errMsg:  "kubeclientset fail",
		},
		{
			name: "leader election fails",
			setup: func() {
				enableLE := true
				LENamespace := "vxflexos"
				kubeconfigFilepath := "./some/path"
				k8sutils.LeaderElectionFunc = func(_ *kubernetes.Interface, _ string, _ string, _ func(context.Context)) error {
					return errors.New("error, leader election failed")
				}

				// assign values to necessary flags
				flags.enableLeaderElection = &enableLE
				flags.leaderElectionNamespace = &LENamespace
				flags.kubeconfig = &kubeconfigFilepath

				driverRunLoopFunc = func(_ context.Context) {
					runningCh <- "should not be running"
				}
			},
			want:    "should not be running",
			wantErr: true,
			errMsg:  "error, leader election failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beforeEach()
			defer afterEach()

			tt.setup()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()

			err := make(chan error)
			go func() {
				err <- driverRunFunc()
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
			flags.arrayConfigfile = nil
			flags.driverConfigParamsfile = nil
			flags.enableLeaderElection = nil
			flags.leaderElectionNamespace = nil
			flags.kubeconfig = nil
			initFlagsFunc()

			assert.Equal(t, tt.want.enableLeaderElection, *flags.enableLeaderElection)
			assert.Equal(t, tt.want.leaderElectionNamespace, *flags.leaderElectionNamespace)
			assert.Equal(t, tt.want.kubeconfig, *flags.kubeconfig)
		})
	}
}

func Test_preinitCheckFunc(t *testing.T) {
	afterEach := func() {
	}

	beforeEach := func() {
	}

	tests := []struct {
		name     string
		setup    func()
		wantStop bool
		wantErr  bool
	}{
		{
			name: "execute preinit function without setting mdm-info",
			setup: func() {
			},
			wantStop: false,
			wantErr:  false,
		},
		{
			name: "execute preinit function with setting mdm-info/failing service preinit",
			setup: func() {
				os.Setenv(gocsi.EnvVarMode, "mdm-info")
			},
			wantStop: false,
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beforeEach()
			defer afterEach()
			tt.setup()

			stop, err := preInitCheckFunc()
			if stop != tt.wantStop {
				t.Errorf("main() = %v, want %v", stop, tt.wantStop)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("main() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Cannot be run in a vacuum, must be run as part of suite.
// Flags are initialized once and only once. Reinitialization causes failure.
func Test_checkConfigsFunc(t *testing.T) {
	afterEach := func() {
	}

	beforeEach := func() {
	}

	tests := []struct {
		name    string
		setup   func()
		wantErr bool
	}{
		{
			name: "execute checkConfigs function without setting flags",
			setup: func() {
			},
			wantErr: true,
		},
		{
			name: "execute checkConfigs function with only setting arrayConfigfile flag",
			setup: func() {
				*flags.arrayConfigfile = "not-empty"
				flag.Parse()
			},
			wantErr: true,
		},
		{
			name: "execute checkConfigs function with all flags set",
			setup: func() {
				*flags.arrayConfigfile = "not-empty"
				*flags.driverConfigParamsfile = "not-empty"
				flag.Parse()
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beforeEach()
			defer afterEach()
			tt.setup()

			err := checkConfigsFunc()
			if (err != nil) != tt.wantErr {
				t.Errorf("main() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_driverRunLoopFunc(t *testing.T) {

}
