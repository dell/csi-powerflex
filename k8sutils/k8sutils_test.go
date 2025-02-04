package k8sutils

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// following the structure of csi-powermax test, which has more test cases
// because csi-powermax allows a
func Test_CreateKubeClientSet(t *testing.T) {
	tests := []struct {
		name    string
		before  func(string) error
		after   func()
		wantErr bool
	}{
		{
			name:    "failure: must create cert/key under /var, requires root",
			before:  func(_ string) error { return nil },
			after:   func() {},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				errCh <- LeaderElection(&tt.args.clientSet, tt.args.lockName, tt.args.namespace, tt.args.runFunc)
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
