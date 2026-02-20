package kube

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Client struct {
	Clientset *kubernetes.Clientset
}

func New() (*Client, error) {
	// 1. Try to find the Kubeconfig path from environment or default home
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	// 2. Build configuration
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		// 3. Fallback: Try in-cluster config (if running inside a pod)
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("could not find kubeconfig: %v", err)
		}
	}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &Client{Clientset: cs}, nil
}
