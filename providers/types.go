package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	glog "github.com/sirupsen/logrus"
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
	AttachInstance(instanceName string, nodeIndex int) (ProviderHandler, error)
	CreateInstance(instanceName string, nodeIndex int) (ProviderHandler, error)
	NodeGroupName() string
	GetAvailableGpuTypes() map[string]string
	InstanceExists(name string) bool
	UUID(name string) (string, error)
}

type InstanceCreateInput struct {
	NodeName     string
	NodeIndex    int
	InstanceType string
	UserName     string
	AuthKey      string
	CloudInit    interface{}
	Machine      *MachineCharacteristic
}

type ProviderHandler interface {
	GetTimeout() time.Duration
	NodeGroupName() string
	ConfigureNetwork(network v1alpha1.ManagedNetworkConfig)
	RetrieveNetworkInfos() error
	UpdateMacAddressTable() error
	GenerateProviderID() string
	GetTopologyLabels() map[string]string
	InstanceCreate(input *InstanceCreateInput) (string, error)
	InstanceWaitReady(callback CallbackWaitSSHReady) (string, error)
	InstanceID() (string, error)
	InstanceAutoStart() error
	InstancePowerOn() error
	InstancePowerOff() error
	InstanceShutdownGuest() error
	InstanceDelete() error
	InstanceStatus() (InstanceStatus, error)
	InstanceWaitForPowered() error
	InstanceWaitForToolsRunning() (bool, error)
	InstanceMaxPods(instanceType string, desiredMaxPods int) (int, error)
	RegisterDNS(address string) error
	UnregisterDNS(address string) error
	UUID(name string) (string, error)
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

func LoadConfig(fileName string, config any) error {
	file, err := os.Open(fileName)

	if err != nil {
		glog.Errorf("Failed to open file:%s, error:%v", fileName, err)

		return err
	}

	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(config)

	if err != nil {
		glog.Errorf("failed to decode AutoScalerServerApp file:%s, error:%v", fileName, err)
		return err
	}

	return nil
}
