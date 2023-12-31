package aws

import (
	"fmt"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	glog "github.com/sirupsen/logrus"
)

// VirtualMachinePowerState alias string
type VirtualMachinePowerState string

const (
	// VirtualMachinePowerStatePoweredOff state
	VirtualMachinePowerStatePoweredOff = VirtualMachinePowerState("poweredOff")

	// VirtualMachinePowerStatePoweredOn state
	VirtualMachinePowerStatePoweredOn = VirtualMachinePowerState("poweredOn")

	// VirtualMachinePowerStateSuspended state
	VirtualMachinePowerStateSuspended = VirtualMachinePowerState("suspended")
)

var availableGPUTypes = map[string]string{
	"nvidia-tesla-k80":  "",
	"nvidia-tesla-p100": "",
	"nvidia-tesla-v100": "",
}

// Configuration declares aws connection info
type Configuration struct {
	NodeGroup string        `json:"nodegroup"`
	AccessKey string        `json:"accessKey,omitempty"`
	SecretKey string        `json:"secretKey,omitempty"`
	Token     string        `json:"token,omitempty"`
	Filename  string        `json:"filename,omitempty"`
	Profile   string        `json:"profile,omitempty"`
	Region    string        `json:"region,omitempty"`
	Timeout   time.Duration `json:"timeout"`
	ImageID   string        `json:"ami"`
	IamRole   string        `json:"iam-role-arn"`
	KeyName   string        `json:"keyName"`
	Tags      []Tag         `json:"tags,omitempty"`
	Network   Network       `json:"network"`
	DiskType  string        `default:"standard" json:"diskType"`
	DiskSize  int           `default:"10" json:"diskSize"`
}

// Tag aws tag
type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Network declare network configuration
type Network struct {
	ZoneID          string             `json:"route53,omitempty"`
	PrivateZoneName string             `json:"privateZoneName,omitempty"`
	AccessKey       string             `json:"accessKey,omitempty"`
	SecretKey       string             `json:"secretKey,omitempty"`
	Token           string             `json:"token,omitempty"`
	Profile         string             `json:"profile,omitempty"`
	Region          string             `json:"region,omitempty"`
	ENI             []NetworkInterface `json:"eni,omitempty"`
}

// NetworkInterface declare ENI interface
type NetworkInterface struct {
	SubnetsID       []string `json:"subnets"`
	SecurityGroupID string   `json:"securityGroup"`
	PublicIP        bool     `json:"publicIP"`
}

func (eni *NetworkInterface) GetNextSubnetsID(nodeIndex int) string {
	numOfEnis := len(eni.SubnetsID)

	if numOfEnis == 1 {
		return eni.SubnetsID[0]
	}

	index := nodeIndex % numOfEnis

	return eni.SubnetsID[index]
}

// UserDefinedNetworkInterface declare a network interface interface overriding default Eni
type UserDefinedNetworkInterface struct {
	NetworkInterfaceID string `json:"networkInterfaceId"`
	SubnetID           string `json:"subnets"`
	SecurityGroupID    string `json:"securityGroup"`
	PrivateAddress     string `json:"privateAddress,omitempty"`
	PublicIP           bool   `json:"publicIP"`
}

// Status shortened vm status
type Status struct {
	address string
	powered bool
}

type awsConfiguration struct {
	config          *Configuration
	testMode        bool
	runningInstance *Ec2Instance
	desiredENI      *UserDefinedNetworkInterface
}

func NewAwsProviderConfiguration(config *Configuration) providers.ProviderConfiguration {
	return &awsConfiguration{
		config: config,
	}
}

func (status *Status) Address() string {
	return status.address
}

func (status *Status) Powered() bool {
	return status.powered
}

func isNullOrEmpty(s string) bool {
	return len(strings.TrimSpace(s)) == 0
}

func (conf *awsConfiguration) GetTestMode() bool {
	return conf.testMode
}

func (conf *awsConfiguration) SetTestMode(value bool) {
	conf.testMode = value
}

func (conf *awsConfiguration) GetTimeout() time.Duration {
	return conf.config.Timeout
}

func (conf *awsConfiguration) GetAvailableGpuTypes() map[string]string {
	return availableGPUTypes
}

func (conf *awsConfiguration) NodeGroupName() string {
	return conf.config.NodeGroup
}

func (conf *awsConfiguration) copy() *awsConfiguration {
	var dup awsConfiguration

	_ = providers.Copy(&dup, conf)

	dup.testMode = conf.testMode

	return &dup
}

func (conf *awsConfiguration) Clone(nodeIndex int) (providers.ProviderConfiguration, error) {
	dup := conf.copy()

	dup.runningInstance = nil
	dup.desiredENI = nil

	return dup, nil
}

func (conf *awsConfiguration) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
	if network.ENI != nil {
		eni := network.ENI
		if len(eni.SubnetID)+len(eni.SecurityGroupID) > 0 {
			conf.desiredENI = &UserDefinedNetworkInterface{
				NetworkInterfaceID: eni.NetworkInterfaceID,
				SubnetID:           eni.SubnetID,
				SecurityGroupID:    eni.SecurityGroupID,
				PrivateAddress:     eni.PrivateAddress,
				PublicIP:           eni.PublicIP,
			}
		}
	}
}

func (conf *awsConfiguration) AttachInstance(instanceName string) (providers.ProviderConfiguration, error) {
	clone := conf.copy()

	if ec2Instance, err := GetEc2Instance(conf.config, instanceName); err != nil {
		return nil, err
	} else {
		clone.runningInstance = ec2Instance
	}

	return clone, nil
}

func (conf *awsConfiguration) RetrieveNetworkInfos(name, vmuuid string, nodeIndex int) error {
	return nil
}

func (conf *awsConfiguration) UpdateMacAddressTable(nodeIndex int) error {
	return nil
}

func (conf *awsConfiguration) GenerateProviderID(vmuuid string) string {
	return fmt.Sprintf("aws://%s/%s", *conf.runningInstance.Zone, *conf.runningInstance.InstanceID)
}

func (conf *awsConfiguration) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion: *conf.runningInstance.Region,
		constantes.NodeLabelTopologyZone:   *conf.runningInstance.Zone,
	}
}

// InstanceCreate will create a named VM not powered
// memory and disk are in megabytes
func (conf *awsConfiguration) InstanceCreate(nodeName string, nodeIndex int, instanceType, userName, authKey string, cloudInit interface{}, machine *providers.MachineCharacteristic) (string, error) {
	var err error

	if conf.runningInstance, err = NewEc2Instance(conf.config, nodeName); err != nil {
		return "", err
	}

	if err = conf.runningInstance.Create(nodeIndex, conf.NodeGroupName(), instanceType, nil, machine.DiskType, machine.DiskSize, conf.desiredENI); err != nil {
		return "", err
	}

	return *conf.runningInstance.InstanceID, nil
}

func (conf *awsConfiguration) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	if conf.runningInstance == nil {
		return "", fmt.Errorf("instance not attached when calling WaitForVMReady")
	}

	return conf.runningInstance.WaitForIP(callback)
}

func (conf *awsConfiguration) InstanceID(name string) (string, error) {
	if instance, err := conf.GetInstanceID(name); err != nil {
		return "", err
	} else {
		return *instance.InstanceID, nil
	}
}

func (conf *awsConfiguration) InstanceExists(name string) bool {
	if _, err := conf.GetInstanceID(name); err == nil {
		return true
	}

	return false
}

func (conf *awsConfiguration) InstanceAutoStart(name string) error {
	return nil
}

func (conf *awsConfiguration) InstancePowerOn(name string) error {
	if instance, err := conf.GetInstanceID(name); err != nil {
		return err
	} else {
		return instance.PowerOn()
	}

}

func (conf *awsConfiguration) InstancePowerOff(name string) error {
	if instance, err := conf.GetInstanceID(name); err != nil {
		return err
	} else {
		return instance.PowerOff()
	}
}

func (conf *awsConfiguration) InstanceDelete(name string) error {
	if instance, err := conf.GetInstanceID(name); err != nil {
		return err
	} else {
		return instance.Delete()
	}
}

func (conf *awsConfiguration) InstanceStatus(name string) (providers.InstanceStatus, error) {
	if instance, err := conf.GetInstanceID(name); err != nil {
		return nil, err
	} else {
		return instance.Status()
	}
}

func (conf *awsConfiguration) InstanceWaitForToolsRunning(name string) (bool, error) {
	return true, nil
}

func (conf *awsConfiguration) RegisterDNS(address string) error {
	var err error

	if conf.runningInstance != nil && len(conf.config.Network.ZoneID) > 0 {
		vm := conf.runningInstance
		hostname := fmt.Sprintf("%s.%s", vm.InstanceName, conf.config.Network.PrivateZoneName)

		glog.Infof("Register route53 entry for instance %s, node group: %s, hostname: %s with IP:%s", vm.InstanceName, conf.config.NodeGroup, hostname, address)

		err = vm.RegisterDNS(conf.config, hostname, address, conf.testMode)
	}

	return err

}

func (conf *awsConfiguration) UnregisterDNS(address string) error {
	var err error

	if conf.runningInstance != nil && len(conf.config.Network.ZoneID) > 0 {
		vm := conf.runningInstance
		hostname := fmt.Sprintf("%s.%s", vm.InstanceName, conf.config.Network.PrivateZoneName)

		glog.Infof("Unregister route53 entry for instance %s, node group: %s, hostname: %s with IP:%s", vm.InstanceName, conf.config.NodeGroup, hostname, address)

		err = vm.UnRegisterDNS(conf.config, hostname, false)
	}

	return err
}

// GetInstanceID return aws instance id from named ec2 instance
func (conf *awsConfiguration) GetInstanceID(instanceName string) (*Ec2Instance, error) {
	if conf.runningInstance != nil && conf.runningInstance.InstanceName == instanceName {
		return conf.runningInstance, nil
	}

	return GetEc2Instance(conf.config, instanceName)
}

// Log logging
func (conf *Configuration) Log(args ...interface{}) {
	glog.Infoln(args...)
}

func (conf *Configuration) GetFileName() string {
	return conf.Filename
}

// GetRoute53AccessKey return route53 access key or default
func (conf *Configuration) GetRoute53AccessKey() string {
	if !isNullOrEmpty(conf.Network.AccessKey) {
		return conf.Network.AccessKey
	}

	return conf.AccessKey
}

// GetRoute53SecretKey return route53 secret key or default
func (conf *Configuration) GetRoute53SecretKey() string {
	if !isNullOrEmpty(conf.Network.SecretKey) {
		return conf.Network.SecretKey
	}

	return conf.SecretKey
}

// GetRoute53AccessToken return route53 token or default
func (conf *Configuration) GetRoute53AccessToken() string {
	if !isNullOrEmpty(conf.Network.Token) {
		return conf.Network.Token
	}

	return conf.Token
}

// GetRoute53Profile return route53 profile or default
func (conf *Configuration) GetRoute53Profile() string {
	if !isNullOrEmpty(conf.Network.Profile) {
		return conf.Network.Profile
	}

	return conf.Profile
}

// GetRoute53Profile return route53 region or default
func (conf *Configuration) GetRoute53Region() string {
	if !isNullOrEmpty(conf.Network.Region) {
		return conf.Network.Region
	}

	return conf.Region
}
