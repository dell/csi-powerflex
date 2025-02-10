package k8sutils

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func Test_CreateKubeClientSet(t *testing.T) {
	var tempConfigFunc func() (*rest.Config, error)                               // must return getInClusterConfig to its original value
	var tempClientsetFunc func(config *rest.Config) (kubernetes.Interface, error) // must return getK8sClientset to its original value
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
				return nil
			},
			after:   func() {},
			wantErr: true,
		},
		{
			name: "failure: error returned by kubernetes.NewForConfig",
			before: func() error { // overrides to get past a mock and inject a failure
				Clientset = nil // reset Clientset before each run
				tempConfigFunc = InClusterConfigFunc
				tempClientsetFunc = NewForConfigFunc
				InClusterConfigFunc = func() (*rest.Config, error) { return &rest.Config{}, nil }
				NewForConfigFunc = func(config *rest.Config) (kubernetes.Interface, error) {
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

			err := CreateKubeClientSet()
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
				runFunc: func(ctx context.Context) {
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
