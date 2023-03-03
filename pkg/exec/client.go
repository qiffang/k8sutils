package exec

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Clientset knows how to interact with a K8s cluster. It has a reference to
// user configuration stored in viper.

type Client struct {
	kubernetes.Interface
	*ClientOpt
}

type ClientOpt struct {
	K8sConfig *rest.Config

	PodName       string
	ContainerName string
	Namespace     string

	CurrentContext string
}

// NewClient returns a new Clientset for the given config.
func NewClient(opt *ClientOpt) (*Client, error) {
	k8sClientset, err := kubernetes.NewForConfig(opt.K8sConfig)
	if err != nil {
		return nil, err
	}

	return &Client{
		ClientOpt: opt,
		Interface: k8sClientset,
	}, nil
}
