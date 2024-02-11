package providers

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/sshutils"
	"github.com/drone/envsubst"
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
	MultipassProviderName         = "multipass"
)

var SupportedKubernetesDistribution = []string{
	RKE2DistributionName,
	K3SDistributionName,
	KubeAdmDistributionName,
	ExternalDistributionName,
}

var SupportedCloudProviders = []string{
	AwsCloudProviderName,
	VSphereCloudProviderName,
	VMWareWorkstationProviderName,
	MultipassProviderName,
}

type BasicConfiguration struct {
	CloudInit    cloudinit.CloudInit              `json:"cloud-init"`
	SSH          sshutils.AutoScalerServerSSH     `json:"ssh"`
	NodeGroup    string                           `json:"nodegroup"`
	InstanceName string                           `json:"instance-name"`
	InstanceType string                           `json:"instance-type"`
	DiskSize     int                              `default:"10240" json:"disk-size"`
	DiskType     string                           `default:"gp3" json:"disk-type"`
	Machines     map[string]MachineCharacteristic `json:"machines"`
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
	Price    float64 `json:"price"`                    // VM price in usd
	Memory   int     `json:"memsize"`                  // VM Memory size in megabytes
	Vcpu     int     `json:"vcpus"`                    // VM number of cpus
	DiskSize int     `default:"10240" json:"disksize"` // VM disk size in megabytes
}

type MachineCharacteristics map[string]*MachineCharacteristic

type ProviderConfiguration interface {
	AttachInstance(instanceName string, nodeIndex int) (ProviderHandler, error)
	CreateInstance(instanceName, instanceType string, nodeIndex int) (ProviderHandler, error)
	GetAvailableGpuTypes() map[string]string
	InstanceExists(name string) bool
	UUID(name string) (string, error)
}

type InstanceCreateInput struct {
	NodeGroup string
	UserName  string
	AuthKey   string
	CloudInit cloudinit.CloudInit
	Machine   *MachineCharacteristic
}

type ProviderHandler interface {
	GetTimeout() time.Duration
	ConfigureNetwork(network v1alpha1.ManagedNetworkConfig)
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
	InstanceStatus() (InstanceStatus, error)
	InstanceWaitForPowered() error
	InstanceWaitForToolsRunning() (bool, error)
	InstanceMaxPods(desiredMaxPods int) (int, error)
	PrivateDNSName() (string, error)
	RegisterDNS(address string) error
	UnregisterDNS(address string) error
	UUID(name string) (string, error)
}

func LoadTextEnvSubst(fileName string) (string, error) {
	if buf, err := os.ReadFile(fileName); err != nil {
		return "", err
	} else {
		return envsubst.EvalEnv(string(buf))
	}
}

func LoadConfig(fileName string, config any) error {
	if content, err := LoadTextEnvSubst(fileName); err != nil {
		return err
	} else {
		reader := strings.NewReader(content)

		return json.NewDecoder(reader).Decode(config)
	}
}
