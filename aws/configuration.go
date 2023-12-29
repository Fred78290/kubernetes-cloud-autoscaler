package aws

import (
	"encoding/json"
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
	NodeGroup       string        `json:"nodegroup"`
	AccessKey       string        `json:"accessKey,omitempty"`
	SecretKey       string        `json:"secretKey,omitempty"`
	Token           string        `json:"token,omitempty"`
	Filename        string        `json:"filename,omitempty"`
	Profile         string        `json:"profile,omitempty"`
	Region          string        `json:"region,omitempty"`
	Timeout         time.Duration `json:"timeout"`
	ImageID         string        `json:"ami"`
	IamRole         string        `json:"iam-role-arn"`
	KeyName         string        `json:"keyName"`
	Tags            []Tag         `json:"tags,omitempty"`
	Network         Network       `json:"network"`
	DiskType        string        `default:"standard" json:"diskType"`
	DiskSize        int           `default:"10" json:"diskSize"`
	TestMode        bool          `json:"-"`
	runningInstance *Ec2Instance
	desiredENI      *UserDefinedNetworkInterface
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
	Address string
	Powered bool
}

func isNullOrEmpty(s string) bool {
	return len(strings.TrimSpace(s)) == 0
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

func (conf *Configuration) GetTestMode() bool {
	return conf.TestMode
}

func (conf *Configuration) SetTestMode(value bool) {
	conf.TestMode = value
}

func (conf *Configuration) GetTimeout() time.Duration {
	return conf.Timeout
}

func (conf *Configuration) AvailableGpuTypes() map[string]string {
	return availableGPUTypes
}

func (conf *Configuration) NodeGroupName() string {
	return conf.NodeGroup
}

func (conf *Configuration) AttachInstance(instanceName string) error {

	if ec2Instance, err := GetEc2Instance(conf, instanceName); err == nil {
		conf.runningInstance = ec2Instance
	}

	return nil
}

func (conf *Configuration) RetrieveNetworkInfos(name, vmuuid string, nodeIndex int) error {
	return nil
}

func (conf *Configuration) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
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

func (conf *Configuration) UpdateMacAddressTable(nodeIndex int) {
}

func (conf *Configuration) GenerateProviderID(vmuuid string) string {
	return fmt.Sprintf("aws://%s/%s", *conf.runningInstance.Zone, *conf.runningInstance.InstanceID)
}

func (conf *Configuration) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion: *conf.runningInstance.Region,
		constantes.NodeLabelTopologyZone:   *conf.runningInstance.Zone,
	}
}

func (conf *Configuration) WaitForVMReady(callback providers.CallbackWaitSSHReady) (*string, error) {
	return conf.runningInstance.WaitForIP(callback)
}

func (conf *Configuration) Copy() *Configuration {
	var dup Configuration

	_ = Copy(&dup, conf)

	dup.TestMode = conf.TestMode

	return &dup
}

func (conf *Configuration) Clone(nodeIndex int) (*Configuration, error) {
	dup := conf.Copy()

	dup.runningInstance = nil
	dup.desiredENI = nil

	return dup, nil
}

func (eni *NetworkInterface) GetNextSubnetsID(nodeIndex int) string {
	numOfEnis := len(eni.SubnetsID)

	if numOfEnis == 1 {
		return eni.SubnetsID[0]
	}

	index := nodeIndex % numOfEnis

	return eni.SubnetsID[index]
}

// Log logging
func (conf *Configuration) Log(args ...interface{}) {
	glog.Infoln(args...)
}

// GetInstanceID return aws instance id from named ec2 instance
func (conf *Configuration) GetInstanceID(name string) (*Ec2Instance, error) {
	return GetEc2Instance(conf, name)
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

// Create will create a named VM not powered
// memory and disk are in megabytes
func (conf *Configuration) Create(nodeIndex int, nodeGroup, name, instanceType string, diskType string, diskSize int, userData *string, desiredENI *UserDefinedNetworkInterface) (*Ec2Instance, error) {
	var err error
	var instance *Ec2Instance

	if instance, err = NewEc2Instance(conf, name); err != nil {
		return nil, err
	}

	if err = instance.Create(nodeIndex, nodeGroup, instanceType, userData, diskType, diskSize, desiredENI); err != nil {
		return nil, err
	}

	return instance, nil
}

func (conf *Configuration) Exists(name string) bool {
	if _, err := GetEc2Instance(conf, name); err == nil {
		return true
	}

	return false
}
