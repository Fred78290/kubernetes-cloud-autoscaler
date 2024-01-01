package aws

import (
	"fmt"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
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
	TestMode  bool          `json:"test-mode"`
	ec2Client *ec2.EC2
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
	CniPlugin       string             `json:"cni-plugin,omitempty"`
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

type awsHandler struct {
	config          *Configuration
	distribution    string
	nodeIndex       int
	runningInstance *Ec2Instance
	desiredENI      *UserDefinedNetworkInterface
}

type awsWrapper struct {
	config Configuration
}

func NewAwsProviderConfiguration(fileName string) (providers.ProviderConfiguration, error) {
	var wrapper awsWrapper

	if err := providers.LoadConfig(fileName, &wrapper.config); err != nil {
		glog.Errorf("Failed to open file:%s, error:%v", fileName, err)

		return nil, err
	}

	return &wrapper, nil
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

func (conf *awsHandler) GetTimeout() time.Duration {
	return conf.config.Timeout
}

func (conf *awsHandler) NodeGroupName() string {
	return conf.config.NodeGroup
}

func (conf *awsHandler) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
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

func (conf *awsHandler) RetrieveNetworkInfos() error {
	return nil
}

func (conf *awsHandler) UpdateMacAddressTable() error {
	return nil
}

func (conf *awsHandler) GenerateProviderID() string {
	return fmt.Sprintf("aws://%s/%s", *conf.runningInstance.Zone, *conf.runningInstance.InstanceID)
}

func (conf *awsHandler) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion: *conf.runningInstance.Region,
		constantes.NodeLabelTopologyZone:   *conf.runningInstance.Zone,
	}
}

// InstanceCreate will create a named VM not powered
// memory and disk are in megabytes
func (conf *awsHandler) InstanceCreate(nodeName string, nodeIndex int, instanceType, userName, authKey string, cloudInit interface{}, machine *providers.MachineCharacteristic) (string, error) {
	var err error

	if conf.runningInstance, err = conf.config.newEc2Instance(nodeName); err != nil {
		return "", err
	}

	if err = conf.runningInstance.Create(nodeIndex, conf.NodeGroupName(), instanceType, nil, machine.DiskType, machine.DiskSize, conf.desiredENI); err != nil {
		return "", err
	}

	return *conf.runningInstance.InstanceID, nil
}

func (conf *awsHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	if conf.runningInstance == nil {
		return "", fmt.Errorf("instance not attached when calling WaitForVMReady")
	}

	return conf.runningInstance.WaitForIP(callback)
}

func (conf *awsHandler) InstanceID() (string, error) {
	if conf.runningInstance == nil {
		return "", fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return *conf.runningInstance.InstanceID, nil
}

func (conf *awsHandler) InstanceExists(name string) bool {
	return conf.config.Exists(name)
}

func (conf *awsHandler) InstanceAutoStart() error {
	return nil
}

func (conf *awsHandler) InstancePowerOn() error {
	if conf.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return conf.runningInstance.PowerOn()
}

func (conf *awsHandler) InstancePowerOff() error {
	if conf.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return conf.runningInstance.PowerOff()
}

func (conf *awsHandler) InstanceShutdownGuest() error {
	if conf.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return conf.runningInstance.ShutdownGuest()
}

func (conf *awsHandler) InstanceDelete() error {
	if conf.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return conf.runningInstance.Delete()
}

func (conf *awsHandler) InstanceStatus() (providers.InstanceStatus, error) {
	if conf.runningInstance == nil {
		return nil, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return conf.runningInstance.Status()
}

func (conf *awsHandler) InstanceWaitForPowered() error {
	if conf.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return conf.runningInstance.WaitForPowered()
}

func (conf *awsHandler) InstanceWaitForToolsRunning() (bool, error) {
	return true, nil
}

func (conf *awsHandler) InstanceMaxPods(instanceType string, desiredMaxPods int) (int, error) {
	if client, err := conf.config.createClient(); err != nil {
		return 0, err
	} else {
		input := ec2.DescribeInstanceTypesInput{
			InstanceTypes: []*string{
				aws.String(instanceType),
			},
		}

		if result, err := client.DescribeInstanceTypes(&input); err != nil {
			return 0, err
		} else {
			networkInfos := result.InstanceTypes[0].NetworkInfo

			return int(*networkInfos.MaximumNetworkInterfaces*(*networkInfos.Ipv4AddressesPerInterface-1) + 2), nil
		}
	}
}

func (conf *awsHandler) RegisterDNS(address string) error {
	var err error

	if conf.runningInstance != nil && len(conf.config.Network.ZoneID) > 0 {
		vm := conf.runningInstance
		hostname := fmt.Sprintf("%s.%s", vm.InstanceName, conf.config.Network.PrivateZoneName)

		glog.Infof("Register route53 entry for instance %s, node group: %s, hostname: %s with IP:%s", vm.InstanceName, conf.config.NodeGroup, hostname, address)

		err = vm.RegisterDNS(conf.config, hostname, address, conf.config.TestMode)
	}

	return err

}

func (conf *awsHandler) UnregisterDNS(address string) error {
	var err error

	if conf.runningInstance != nil && len(conf.config.Network.ZoneID) > 0 {
		vm := conf.runningInstance
		hostname := fmt.Sprintf("%s.%s", vm.InstanceName, conf.config.Network.PrivateZoneName)

		glog.Infof("Unregister route53 entry for instance %s, node group: %s, hostname: %s with IP:%s", vm.InstanceName, conf.config.NodeGroup, hostname, address)

		err = vm.UnRegisterDNS(conf.config, hostname, false)
	}

	return err
}

func (conf *awsHandler) UUID(instanceName string) (string, error) {
	if conf.runningInstance != nil && conf.runningInstance.InstanceName == instanceName {
		return *conf.runningInstance.InstanceID, nil
	}

	if ec2, err := GetEc2Instance(conf.config, instanceName); err != nil {
		return "", err
	} else {
		return *ec2.InstanceID, nil
	}
}

func (wrapper *awsWrapper) AttachInstance(instanceName string, nodeIndex int) (providers.ProviderHandler, error) {
	if instance, err := GetEc2Instance(&wrapper.config, instanceName); err != nil {
		return nil, err
	} else {
		return &awsHandler{
			config:          &wrapper.config,
			runningInstance: instance,
			nodeIndex:       nodeIndex,
		}, nil
	}
}

func (wrapper *awsWrapper) CreateInstance(instanceName string, nodeIndex int) (providers.ProviderHandler, error) {
	if wrapper.InstanceExists(instanceName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, instanceName)
		return nil, fmt.Errorf(constantes.ErrVMAlreadyExists, instanceName)
	}

	return &awsHandler{
		config:    &wrapper.config,
		nodeIndex: nodeIndex,
	}, nil
}

func (handler *awsWrapper) InstanceExists(name string) bool {
	return handler.config.Exists(name)
}

func (wrapper *awsWrapper) NodeGroupName() string {
	return wrapper.config.NodeGroup
}

func (conf *awsWrapper) GetAvailableGpuTypes() map[string]string {
	return availableGPUTypes
}

func (conf *Configuration) createClient() (*ec2.EC2, error) {
	if conf.ec2Client == nil {
		var err error
		var sess *session.Session

		if sess, err = newSessionWithOptions(conf.AccessKey, conf.SecretKey, conf.Token, conf.Filename, conf.Profile, conf.Region); err != nil {
			return nil, err
		}

		// Create EC2 service client
		if glog.GetLevel() >= glog.DebugLevel {
			conf.ec2Client = ec2.New(sess, aws.NewConfig().WithLogger(conf).WithLogLevel(aws.LogDebugWithHTTPBody).WithLogLevel(aws.LogDebugWithSigning))
		} else {
			conf.ec2Client = ec2.New(sess)
		}
	}

	return conf.ec2Client, nil
}

// Log logging
func (conf *Configuration) Log(args ...interface{}) {
	glog.Infoln(args...)
}

func (conf *Configuration) GetFileName() string {
	return conf.Filename
}

func (conf *Configuration) Exists(name string) bool {
	if _, err := GetEc2Instance(conf, name); err != nil {
		return true
	}

	return false
}

func (conf *Configuration) UUID(instanceName string) (string, error) {
	if ec2, err := GetEc2Instance(conf, instanceName); err != nil {
		return "", err
	} else {
		return *ec2.InstanceID, nil
	}
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

func (conf *Configuration) newEc2Instance(instanceName string) (*Ec2Instance, error) {
	if client, err := conf.createClient(); err != nil {
		return nil, err
	} else {
		return &Ec2Instance{
			client:       client,
			config:       conf,
			InstanceName: instanceName,
		}, nil
	}
}
