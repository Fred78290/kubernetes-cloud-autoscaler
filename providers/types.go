package providers

import (
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha2"
)

const (
	AwsCloudProviderName          = "aws"
	VSphereCloudProviderName      = "vsphere"
	VMWareWorkstationProviderName = "desktop"
	MultipassProviderName         = "multipass"
	OpenStackProviderName         = "openstack"
)

var SupportedCloudProviders = []string{
	AwsCloudProviderName,
	VSphereCloudProviderName,
	VMWareWorkstationProviderName,
	MultipassProviderName,
	OpenStackProviderName,
}

// CallbackWaitSSHReady callback to test if ssh become ready or return timeout error
type CallbackWaitSSHReady interface {
	WaitSSHReady(name, address string) error
}

type InstanceStatus interface {
	Address() string
	Powered() bool
}

// MachineCharacteristic defines VM kind
type MachineCharacteristic struct {
	Price    float64 `json:"price"`              // VM price in usd
	Memory   int     `json:"memsize"`            // VM Memory size in megabytes
	Vcpu     int     `json:"vcpus"`              // VM number of cpus
	DiskSize *int    `json:"disksize,omitempty"` // VM disk size in megabytes
}

func (m *MachineCharacteristic) GetDiskSize() int {
	if m.DiskSize != nil {
		return *m.DiskSize
	}

	return 10240
}

type MachineCharacteristics map[string]*MachineCharacteristic

type ProviderConfiguration interface {
	AttachInstance(instanceName string, controlPlane bool, nodeIndex int) (ProviderHandler, error)
	CreateInstance(instanceName, instanceType string, controlPlane bool, nodeIndex int) (ProviderHandler, error)
	GetAvailableGpuTypes() map[string]string
	InstanceExists(name string) bool
	UUID(name string) (string, error)
	SetMode(test bool)
}

type InstanceCreateInput struct {
	ControlPlane bool
	AllowUpgrade bool
	NodeGroup    string
	UserName     string
	AuthKey      string
	CloudInit    cloudinit.CloudInit
	Machine      *MachineCharacteristic
}

type ProviderHandler interface {
	GetTimeout() time.Duration
	ConfigureNetwork(network v1alpha2.ManagedNetworkConfig)
	RetrieveNetworkInfos() error
	UpdateMacAddressTable() error
	GenerateProviderID() string
	GetTopologyLabels() map[string]string
	InstanceCreate(input *InstanceCreateInput) (string, error)
	InstanceWaitReady(callback CallbackWaitSSHReady) (string, error)
	InstancePrimaryAddressIP() string
	InstanceID() (string, error)
	InstanceAutoStart() error
	InstancePowerOn() error
	InstancePowerOff() error
	InstanceShutdownGuest() error
	InstanceDelete() error
	InstanceCreated() bool
	InstanceStatus() (InstanceStatus, error)
	InstanceWaitForPowered() error
	InstanceWaitForToolsRunning() (bool, error)
	InstanceMaxPods(desiredMaxPods int) (int, error)
	PrivateDNSName() (string, error)
	RegisterDNS(address string) error
	UnregisterDNS(address string) error
	UUID(name string) (string, error)
}
