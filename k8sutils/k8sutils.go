package k8sutils

import (
	"context"
	"fmt"
	"github.com/kubernetes-csi/csi-lib-utils/leaderelection"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"os"
)

type leaderElection interface {
	Run() error
	WithNamespace(namespace string)
}

// CreateKubeClientSet - Returns kubeclient set
func CreateKubeClientSet(kubeconfig string) (*kubernetes.Clientset, error) {
	var clientset *kubernetes.Clientset
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	// creates the clientset
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

// LeaderElection
func LeaderElection(clientset *kubernetes.Clientset, lockName string, namespace string, runFunc func(ctx context.Context)) {
	le := leaderelection.NewLeaderElection(clientset, lockName, runFunc)
	le.WithNamespace(namespace)
	if err := le.Run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to initialize leader election: %v", err)
		os.Exit(1)
	}
}
