package providers

import (
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
)

// CallbackWaitSSHReady callback to test if ssh become ready or return timeout error
type CallbackWaitSSHReady interface {
	WaitSSHReady(name, address string) error
}

type ProviderConfiguration interface {
	GetTestMode() bool
	SetTestMode(value bool)
	GetTimeout() time.Duration
	AvailableGpuTypes() map[string]string
	NodeGroupName() string
	Copy() ProviderConfiguration
	Clone(nodeIndex int) (ProviderConfiguration, error)
	ConfigureNetwork(network v1alpha1.ManagedNetworkConfig)
	AttachInstance(instanceName string) error
	RetrieveNetworkInfos(name, vmuuid string, nodeIndex int) error
	UpdateMacAddressTable(nodeIndex int)
	GenerateProviderID(vmuuid string) string
	GetTopologyLabels() map[string]string
	WaitForVMReady(callback CallbackWaitSSHReady) (*string, error)
	UUID(name string) (string, error)
	InstanceExists(name string) bool
	InstanceAutoStart(name string) error
	PowerOn(name string) error
	PowerOff(name string) error
	Delete(name string) error
	WaitForToolsRunning(name string) (bool, error)
}
