package client

import (
	"time"

	managednodeClientset "github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/generated/clientset/versioned"
	apiv1 "k8s.io/api/core/v1"
	apiextension "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
)

// ClientGenerator provides clients
type ClientGenerator interface {
	KubeClient() (kubernetes.Interface, error)
	NodeManagerClient() (managednodeClientset.Interface, error)
	ApiExtentionClient() (apiextension.Interface, error)

	PodList(nodeName string, podFilter PodFilterFunc) ([]apiv1.Pod, error)
	NodeList() (*apiv1.NodeList, error)
	GetNode(nodeName string) (*apiv1.Node, error)
	SetProviderID(nodeName, providerID string) error
	UncordonNode(nodeName string) error
	CordonNode(nodeName string) error
	MarkDrainNode(nodeName string) error
	DrainNode(nodeName string, ignoreDaemonSet, deleteLocalData bool) error
	DeleteNode(nodeName string) error
	AnnoteNode(nodeName string, annotations map[string]string) error
	LabelNode(nodeName string, labels map[string]string) error
	TaintNode(nodeName string, taints ...apiv1.Taint) error
	GetSecret(secretName, namespace string) (*apiv1.Secret, error)
	DeleteSecret(secretName, namespace string) error
	WaitNodeToBeReady(nodeName string) error
}

type ClientConfig interface {
	GetKubeConfig() string
	GetAPIServerURL() string
	GetRequestTimeout() time.Duration
	GetNodeReadyTimeout() time.Duration
	GetDeletionTimeout() time.Duration
	GetMaxGracePeriod() time.Duration
}
