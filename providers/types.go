package providers

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
)

const (
	RKE2DistributionName     = "rke2"
	K3SDistributionName      = "k3s"
	KubeAdmDistributionName  = "kubeadm"
	ExternalDistributionName = "external"
)

const (
	AwsCloudProviderName          = "aws"
	VSphereCloudProviderName      = "vsphere"
	VMWareWorkstationProviderName = "desktop"
)

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
	Memory   int    `json:"memsize"`                // VM Memory size in megabytes
	Vcpu     int    `json:"vcpus"`                  // VM number of cpus
	DiskSize int    `json:"disksize"`               // VM disk size in megabytes
	DiskType string `default:"gp2" json:"diskType"` // VM disk type
}

type MachineCharacteristics map[string]*MachineCharacteristic

type ProviderConfiguration interface {
	GetTestMode() bool
	SetTestMode(value bool)
	GetTimeout() time.Duration
	GetAvailableGpuTypes() map[string]string
	NodeGroupName() string
	//	Copy() ProviderConfiguration
	Clone(nodeIndex int) (ProviderConfiguration, error)
	ConfigureNetwork(network v1alpha1.ManagedNetworkConfig)
	AttachInstance(instanceName string) (ProviderConfiguration, error)
	RetrieveNetworkInfos(name, vmuuid string, nodeIndex int) error
	UpdateMacAddressTable(nodeIndex int) error
	GenerateProviderID(vmuuid string) string
	GetTopologyLabels() map[string]string
	InstanceCreate(nodeName string, nodeIndex int, instanceType, userName, authKey string, cloudInit interface{}, machine *MachineCharacteristic) (string, error)
	InstanceWaitReady(callback CallbackWaitSSHReady) (string, error)
	InstanceID(name string) (string, error)
	InstanceExists(name string) bool
	InstanceAutoStart(name string) error
	InstancePowerOn(name string) error
	InstancePowerOff(name string) error
	InstanceShutdownGuest(name string) error
	InstanceDelete(name string) error
	InstanceStatus(name string) (InstanceStatus, error)
	InstanceWaitForPowered(name string) error
	InstanceWaitForToolsRunning(name string) (bool, error)
	InstanceMaxPods(instanceType string, desiredMaxPods int) (int, error)
	RegisterDNS(address string) error
	UnregisterDNS(address string) error
}

// Copy Make a deep copy from src into dst.
func Copy(dst interface{}, src interface{}) error {
	if dst == nil {
		return fmt.Errorf("dst cannot be nil")
	}

	if src == nil {
		return fmt.Errorf("src cannot be nil")
	}

	bytes, err := json.Marshal(src)

	if err != nil {
		return fmt.Errorf("unable to marshal src: %s", err)
	}

	err = json.Unmarshal(bytes, dst)

	if err != nil {
		return fmt.Errorf("unable to unmarshal into dst: %s", err)
	}

	return nil
}
