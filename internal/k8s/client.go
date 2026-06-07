// Package k8s builds Kubernetes manifests and applies them using client-go.
package k8s

import (
	"fmt"
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClientset creates a typed Kubernetes clientset from kubeconfig or in-cluster config.
func NewClientset(explicitKubeconfig string) (*kubernetes.Clientset, error) {
	cfg, err := restConfig(explicitKubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

// restConfig prefers in-cluster credentials when running inside a pod; otherwise loads kubeconfig.
func restConfig(explicitKubeconfig string) (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if explicitKubeconfig != "" {
		loadingRules.ExplicitPath = explicitKubeconfig
	} else if path := os.Getenv("KUBECONFIG"); path != "" {
		loadingRules.ExplicitPath = path
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	return cfg, nil
}
