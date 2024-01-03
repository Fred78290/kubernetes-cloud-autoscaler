package aws

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	glog "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
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

// Configuration declares aws connection info
type Configuration struct {
	AccessKey         string            `json:"accessKey,omitempty"`
	SecretKey         string            `json:"secretKey,omitempty"`
	Token             string            `json:"token,omitempty"`
	Filename          string            `json:"filename,omitempty"`
	Profile           string            `json:"profile,omitempty"`
	Region            string            `json:"region,omitempty"`
	Timeout           time.Duration     `json:"timeout"`
	ImageID           string            `json:"ami"`
	IamRole           string            `json:"iam-role-arn"`
	KeyName           string            `json:"keyName"`
	DiskType          string            `default:"gp2" json:"disk-type"`
	Tags              []Tag             `json:"tags,omitempty"`
	Network           Network           `json:"network"`
	AvailableGPUTypes map[string]string `json:"gpu-types"`
	TestMode          bool              `json:"test-mode"`
	ec2Client         *ec2.EC2
}

// Tag aws tag
type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Network declare network Configuration
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

type CreateInput struct {
	*providers.InstanceCreateInput
}
type awsWrapper struct {
	Configuration
}

type awsHandler struct {
	*awsWrapper
	instanceName    string
	instanceType    string
	nodeIndex       int
	runningInstance *Ec2Instance
	desiredENI      *UserDefinedNetworkInterface
}

func NewAwsProviderConfiguration(fileName string) (providers.ProviderConfiguration, error) {
	var wrapper awsWrapper

	if err := providers.LoadConfig(fileName, &wrapper.Configuration); err != nil {
		glog.Errorf("Failed to open file:%s, error:%v", fileName, err)

		return nil, err
	}

	if !wrapper.AmiExists(wrapper.ImageID) {
		return nil, fmt.Errorf("ami: %s not found", wrapper.ImageID)
	}

	return &wrapper, nil
}

func isNullOrEmpty(s string) bool {
	return len(strings.TrimSpace(s)) == 0
}

func (handler *awsHandler) GetTimeout() time.Duration {
	return handler.Timeout
}

func (handler *awsHandler) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
	if network.ENI != nil {
		eni := network.ENI
		if len(eni.SubnetID)+len(eni.SecurityGroupID) > 0 {
			handler.desiredENI = &UserDefinedNetworkInterface{
				NetworkInterfaceID: eni.NetworkInterfaceID,
				SubnetID:           eni.SubnetID,
				SecurityGroupID:    eni.SecurityGroupID,
				PrivateAddress:     eni.PrivateAddress,
				PublicIP:           eni.PublicIP,
			}
		}
	}
}

func (handler *awsHandler) RetrieveNetworkInfos() error {
	return nil
}

func (handler *awsHandler) UpdateMacAddressTable() error {
	return nil
}

func (handler *awsHandler) GenerateProviderID() string {
	return fmt.Sprintf("aws://%s/%s", *handler.runningInstance.Zone, *handler.runningInstance.InstanceID)
}

func (handler *awsHandler) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion: *handler.runningInstance.Region,
		constantes.NodeLabelTopologyZone:   *handler.runningInstance.Zone,
	}
}

func (handler *awsHandler) encodeCloudInit(object interface{}) (*string, error) {
	if object == nil {
		return nil, nil
	}

	var out bytes.Buffer

	fmt.Fprintln(&out, "#cloud-config")

	wr := yaml.NewEncoder(&out)

	err := wr.Encode(object)
	wr.Close()

	if err != nil {
		return nil, err
	}

	result := out.String()

	return &result, nil
}

// InstanceCreate will create a named VM not powered
// memory and disk are in megabytes
func (handler *awsHandler) InstanceCreate(input *providers.InstanceCreateInput) (string, error) {
	var err error
	var userData *string

	if userData, err = handler.encodeCloudInit(input.CloudInit); err != nil {
		return "", err
	}

	if handler.runningInstance, err = handler.newEc2Instance(handler.instanceName); err != nil {
		return "", err
	}

	if err = handler.runningInstance.Create(handler.nodeIndex, input.NodeGroup, handler.instanceType, userData, handler.DiskType, input.DiskSize, handler.desiredENI); err != nil {
		return "", err
	}

	return *handler.runningInstance.InstanceID, nil
}

func (handler *awsHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	if handler.runningInstance == nil {
		return "", fmt.Errorf("instance not attached when calling WaitForVMReady")
	}

	return handler.runningInstance.WaitForIP(callback)
}

func (handler *awsHandler) InstanceID() (string, error) {
	if handler.runningInstance == nil {
		return "", fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return *handler.runningInstance.InstanceID, nil
}

func (handler *awsHandler) InstanceAutoStart() error {
	return nil
}

func (handler *awsHandler) InstancePowerOn() error {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.PowerOn()
}

func (handler *awsHandler) InstancePowerOff() error {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.PowerOff()
}

func (handler *awsHandler) InstanceShutdownGuest() error {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.ShutdownGuest()
}

func (handler *awsHandler) InstanceDelete() error {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.Delete()
}

func (handler *awsHandler) InstanceStatus() (providers.InstanceStatus, error) {
	if handler.runningInstance == nil {
		return nil, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.Status()
}

func (handler *awsHandler) InstanceWaitForPowered() error {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.WaitForPowered()
}

func (handler *awsHandler) InstanceWaitForToolsRunning() (bool, error) {
	return true, nil
}

func (handler *awsHandler) InstanceMaxPods(desiredMaxPods int) (int, error) {
	if client, err := handler.createClient(); err != nil {
		return 0, err
	} else {
		input := ec2.DescribeInstanceTypesInput{
			InstanceTypes: []*string{
				aws.String(handler.instanceType),
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

func (handler *awsHandler) RegisterDNS(address string) error {
	var err error

	if handler.runningInstance != nil && len(handler.Network.ZoneID) > 0 {
		vm := handler.runningInstance
		hostname := fmt.Sprintf("%s.%s", vm.InstanceName, handler.Network.PrivateZoneName)

		glog.Infof("Register route53 entry for instance %s, hostname: %s with IP:%s", vm.InstanceName, hostname, address)

		err = vm.RegisterDNS(hostname, address, handler.TestMode)
	}

	return err

}

func (handler *awsHandler) UnregisterDNS(address string) error {
	var err error

	if handler.runningInstance != nil && len(handler.Network.ZoneID) > 0 {
		vm := handler.runningInstance
		hostname := fmt.Sprintf("%s.%s", vm.InstanceName, handler.Network.PrivateZoneName)

		glog.Infof("Unregister route53 entry for instance %s, hostname: %s with IP:%s", vm.InstanceName, hostname, address)

		err = vm.UnRegisterDNS(hostname, false)
	}

	return err
}

func (handler *awsHandler) UUID(instanceName string) (string, error) {
	if handler.runningInstance != nil && handler.runningInstance.InstanceName == instanceName {
		return *handler.runningInstance.InstanceID, nil
	}

	if ec2, err := handler.GetEc2Instance(instanceName); err != nil {
		return "", err
	} else {
		return *ec2.InstanceID, nil
	}
}

func (wrapper *awsWrapper) AttachInstance(instanceName string, nodeIndex int) (providers.ProviderHandler, error) {
	if instance, err := wrapper.GetEc2Instance(instanceName); err != nil {
		return nil, err
	} else {
		return &awsHandler{
			awsWrapper:      wrapper,
			runningInstance: instance,
			instanceName:    instanceName,
			nodeIndex:       nodeIndex,
		}, nil
	}
}

func (wrapper *awsWrapper) CreateInstance(instanceName, instanceType string, nodeIndex int) (providers.ProviderHandler, error) {
	if wrapper.InstanceExists(instanceName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, instanceName)
		return nil, fmt.Errorf(constantes.ErrVMAlreadyExists, instanceName)
	}

	return &awsHandler{
		awsWrapper:   wrapper,
		instanceType: instanceType,
		instanceName: instanceName,
		nodeIndex:    nodeIndex,
	}, nil
}

func (wrapper *awsWrapper) InstanceExists(name string) bool {
	return wrapper.Exists(name)
}

func (wrapper *awsWrapper) GetAvailableGpuTypes() map[string]string {
	return wrapper.AvailableGPUTypes
}

func (wrapper *awsWrapper) createClient() (*ec2.EC2, error) {
	if wrapper.ec2Client == nil {
		var err error
		var sess *session.Session

		if sess, err = newSessionWithOptions(wrapper.AccessKey, wrapper.SecretKey, wrapper.Token, wrapper.Filename, wrapper.Profile, wrapper.Region); err != nil {
			return nil, err
		}

		// Create EC2 service client
		if glog.GetLevel() >= glog.DebugLevel {
			wrapper.ec2Client = ec2.New(sess, aws.NewConfig().WithLogger(wrapper).WithLogLevel(aws.LogDebugWithHTTPBody).WithLogLevel(aws.LogDebugWithSigning))
		} else {
			wrapper.ec2Client = ec2.New(sess)
		}
	}

	return wrapper.ec2Client, nil
}

// Log logging
func (wrapper *awsWrapper) Log(args ...interface{}) {
	glog.Infoln(args...)
}

func (wrapper *awsWrapper) GetFileName() string {
	return wrapper.Filename
}

func (wrapper *awsWrapper) Exists(name string) bool {
	if _, err := wrapper.GetEc2Instance(name); err != nil {
		return true
	}

	return false
}

func (wrapper *awsWrapper) AmiExists(ami string) bool {
	if client, err := wrapper.createClient(); err != nil {
		return false
	} else {
		input := ec2.DescribeImagesInput{
			MaxResults: aws.Int64(1),
			ImageIds: []*string{
				aws.String(ami),
			},
		}

		if _, err = client.DescribeImages(&input); err != nil {
			return false
		}
	}

	return true
}

// GetEc2Instance return an existing instance from name
func (wrapper *awsWrapper) GetEc2Instance(instanceName string) (*Ec2Instance, error) {
	if client, err := wrapper.createClient(); err != nil {
		return nil, err
	} else {
		var result *ec2.DescribeInstancesOutput

		input := &ec2.DescribeInstancesInput{
			Filters: []*ec2.Filter{
				{
					Name: aws.String("tag:Name"),
					Values: []*string{
						aws.String(instanceName),
					},
				},
			},
		}

		ctx := context.NewContext(wrapper.Timeout)
		defer ctx.Cancel()

		if result, err = client.DescribeInstancesWithContext(ctx, input); err != nil {
			return nil, err
		}

		if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
			return nil, fmt.Errorf(constantes.ErrVMNotFound, instanceName)
		}

		for _, reservation := range result.Reservations {
			for _, instance := range reservation.Instances {
				// Assume EC2 shutting-down is terminated after
				if *instance.State.Code != 48 && *instance.State.Code != 32 {
					var address *string

					if instance.PublicIpAddress != nil {
						address = instance.PublicIpAddress
					} else {
						address = instance.PrivateIpAddress
					}

					return &Ec2Instance{
						awsWrapper:   wrapper,
						client:       client,
						InstanceName: instanceName,
						InstanceID:   instance.InstanceId,
						Region:       &wrapper.Region,
						Zone:         instance.Placement.AvailabilityZone,
						AddressIP:    address,
					}, nil
				}
			}
		}

		return nil, fmt.Errorf(constantes.ErrVMNotFound, instanceName)
	}
}

func (wrapper *awsWrapper) UUID(instanceName string) (string, error) {
	if ec2, err := wrapper.GetEc2Instance(instanceName); err != nil {
		return "", err
	} else {
		return *ec2.InstanceID, nil
	}
}

// GetRoute53AccessKey return route53 access key or default
func (wrapper *awsWrapper) GetRoute53AccessKey() string {
	if !isNullOrEmpty(wrapper.Network.AccessKey) {
		return wrapper.Network.AccessKey
	}

	return wrapper.AccessKey
}

// GetRoute53SecretKey return route53 secret key or default
func (wrapper *awsWrapper) GetRoute53SecretKey() string {
	if !isNullOrEmpty(wrapper.Network.SecretKey) {
		return wrapper.Network.SecretKey
	}

	return wrapper.SecretKey
}

// GetRoute53AccessToken return route53 token or default
func (wrapper *awsWrapper) GetRoute53AccessToken() string {
	if !isNullOrEmpty(wrapper.Network.Token) {
		return wrapper.Network.Token
	}

	return wrapper.Token
}

// GetRoute53Profile return route53 profile or default
func (wrapper *awsWrapper) GetRoute53Profile() string {
	if !isNullOrEmpty(wrapper.Network.Profile) {
		return wrapper.Network.Profile
	}

	return wrapper.Profile
}

// GetRoute53Profile return route53 region or default
func (wrapper *awsWrapper) GetRoute53Region() string {
	if !isNullOrEmpty(wrapper.Network.Region) {
		return wrapper.Network.Region
	}

	return wrapper.Region
}

func (wrapper *awsWrapper) newEc2Instance(instanceName string) (*Ec2Instance, error) {
	if client, err := wrapper.createClient(); err != nil {
		return nil, err
	} else {
		return &Ec2Instance{
			awsWrapper:   wrapper,
			client:       client,
			InstanceName: instanceName,
		}, nil
	}
}
