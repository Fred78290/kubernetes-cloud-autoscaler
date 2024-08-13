package aws

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	glog "github.com/sirupsen/logrus"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

const (
	route53_UpsertCmd = "UPSERT"
	route53_DeleteCmd = "DELETE"
)

// instanceStatus shortened vm status
type instanceStatus struct {
	address string
	powered bool
}

// Ec2Instance Running instance
type Ec2Instance struct {
	*awsWrapper
	client         *ec2.Client
	NodeIndex      int
	InstanceName   string
	PrivateDNSName string
	InstanceID     string
	Region         string
	Zone           string
	AddressIP      *string
}

func userHomeDir() string {
	if runtime.GOOS == "windows" { // Windows
		return os.Getenv("USERPROFILE")
	}

	// *nix
	return os.Getenv("HOME")
}

func credentialsFileExists(filename string) bool {
	if isNullOrEmpty(filename) {
		if filename = os.Getenv("AWS_SHARED_CREDENTIALS_FILE"); isNullOrEmpty(filename) {
			filename = filepath.Join(userHomeDir(), ".aws", "credentials")
		}
	}

	if _, err := os.Stat(filename); err != nil {
		return false
	}

	return true
}

func isAwsProfileValid(filename, profile string) bool {
	if isNullOrEmpty(profile) {
		return false
	}

	return credentialsFileExists(filename)
}

func newSessionWithOptions(accessKey, secretKey, token, filename, profile, region string) (cfg aws.Config, err error) {
	// Unset this variables because LoadDefaultConfig use it when it's not necessary
	os.Unsetenv("AWS_PROFILE")
	os.Unsetenv("AWS_SHARED_CREDENTIALS_FILE")
	os.Unsetenv("AWS_CONFIG_FILE")

	if !isNullOrEmpty(accessKey) && !isNullOrEmpty(secretKey) {
		glog.Debugf("aws credentials with accesskey: %s, secret: %s", accessKey, secretKey)

		cfg, err = config.LoadDefaultConfig(context.TODO(), config.WithRegion(region), config.WithCredentialsProvider(aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(accessKey, secretKey, token))))
	} else if isAwsProfileValid(filename, profile) {
		glog.Debugf("aws credentials with profile: %s, credentials: %s", profile, filename)

		cfg, err = config.LoadDefaultConfig(context.TODO(), config.WithSharedConfigProfile(profile), config.WithSharedConfigFiles([]string{filename}))
	} else {
		glog.Debugf("aws credentials with ec2rolecreds")

		cfg, err = config.LoadDefaultConfig(context.TODO(), config.WithRegion(region), config.WithCredentialsProvider(aws.NewCredentialsCache(ec2rolecreds.New())))
	}
}

func (status *instanceStatus) Address() string {
	return status.address
}

func (status *instanceStatus) Powered() bool {
	return status.powered
}

func (instance *Ec2Instance) getTimeout() time.Duration {
	return instance.Timeout * time.Second
}

func (instance *Ec2Instance) getInstanceID() string {
	return instance.InstanceID
}

func (instance *Ec2Instance) getEc2Instance() (*types.Instance, error) {
	var err error
	var result *ec2.DescribeInstancesOutput

	ctx := instance.NewContext()
	defer ctx.Cancel()

	input := &ec2.DescribeInstancesInput{
		InstanceIds: []string{
			instance.InstanceID,
		},
	}

	if result, err = instance.client.DescribeInstances(ctx, input); err != nil {
		return nil, err
	}

	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		return nil, fmt.Errorf(constantes.ErrVMNotFound, instance.InstanceName)
	}

	return &result.Reservations[0].Instances[0], nil
}

// NewContext create instance context
func (instance *Ec2Instance) NewContext() *context.Context {
	return context.NewContext(instance.Timeout)
}

func (instance *Ec2Instance) NewContextWithTimeout(timeout time.Duration) *context.Context {
	return context.NewContext(timeout)
}

// WaitForIP wait ip a VM by name
func (instance *Ec2Instance) WaitForIP(callback providers.CallbackWaitSSHReady) (string, error) {
	glog.Debugf("WaitForIP: instance %s id (%s)", instance.InstanceName, instance.getInstanceID())

	if err := context.PollImmediate(time.Second, instance.getTimeout(), func() (bool, error) {
		var err error
		var ec2Instance *types.Instance

		if ec2Instance, err = instance.getEc2Instance(); err != nil {
			return false, err
		}

		code := *ec2Instance.State.Code

		if code == 16 {

			if ec2Instance.PublicIpAddress != nil {
				instance.AddressIP = ec2Instance.PublicIpAddress
			} else {
				instance.AddressIP = ec2Instance.PrivateIpAddress
			}

			glog.Debugf("WaitForIP: instance %s id (%s), using IP: %s", instance.InstanceName, instance.getInstanceID(), *instance.AddressIP)

			if err = callback.WaitSSHReady(instance.InstanceName, *instance.AddressIP); err != nil {
				return false, err
			}

			return true, nil
		} else if code != 0 {
			return false, fmt.Errorf(constantes.ErrWrongStateMachine, ec2Instance.State.Name, instance.InstanceName, "pending")
		}

		return false, nil
	}); err != nil {
		return "", err
	}

	return *instance.AddressIP, nil
}

// WaitForPowered wait ip a VM by name
func (instance *Ec2Instance) WaitForPowered() error {
	glog.Debugf("WaitForPowered: instance %s id (%s)", instance.InstanceName, instance.getInstanceID())

	return ec2.NewInstanceRunningWaiter(instance.client).Wait(context.Background(), &ec2.DescribeInstancesInput{
		InstanceIds: []string{
			instance.InstanceID,
		},
	},
		instance.getTimeout(),
	)
}

func (instance *Ec2Instance) buildNetworkInterfaces(desiredENI *UserDefinedNetworkInterface) (interfaces []types.InstanceNetworkInterfaceSpecification, err error) {
	if desiredENI != nil {
		var privateIPAddress *string
		var subnetID *string
		var securityGroup []string
		var deleteOnTermination bool
		var networkInterfaceId *string

		if len(desiredENI.PrivateAddress) > 0 {
			privateIPAddress = &desiredENI.PrivateAddress

			var output *ec2.DescribeNetworkInterfacesOutput
			var input = ec2.DescribeNetworkInterfacesInput{
				Filters: []types.Filter{
					{
						Name: aws.String("addresses.private-ip-address"),
						Values: []string{
							desiredENI.PrivateAddress,
						},
					},
				},
			}

			// Check if associated with ENI
			if output, err = instance.client.DescribeNetworkInterfaces(context.TODO(), &input); err != nil {
				if len(output.NetworkInterfaces) > 0 {
					privateIPAddress = nil
					desiredENI.NetworkInterfaceID = *output.NetworkInterfaces[0].NetworkInterfaceId
				}
			}
		}

		if len(desiredENI.NetworkInterfaceID) > 0 {
			deleteOnTermination = false
			networkInterfaceId = aws.String(desiredENI.NetworkInterfaceID)
		} else {
			deleteOnTermination = true

			if len(desiredENI.SubnetID) > 0 {
				subnetID = aws.String(desiredENI.SubnetID)
			} else {
				subnetID = aws.String(instance.Network.ENI[0].GetNextSubnetsID(instance.NodeIndex))
			}

			if len(desiredENI.SecurityGroupID) > 0 {
				securityGroup = []string{
					desiredENI.SecurityGroupID,
				}
			} else if len(instance.Network.ENI[0].SecurityGroupID) > 0 {
				securityGroup = []string{
					instance.Network.ENI[0].SecurityGroupID,
				}
			}
		}

		interfaces = []types.InstanceNetworkInterfaceSpecification{
			{
				AssociatePublicIpAddress: aws.Bool(desiredENI.PublicIP),
				DeleteOnTermination:      aws.Bool(deleteOnTermination),
				Description:              aws.String(instance.InstanceName),
				DeviceIndex:              aws.Int32(0),
				SubnetId:                 subnetID,
				NetworkInterfaceId:       networkInterfaceId,
				PrivateIpAddress:         privateIPAddress,
				Groups:                   securityGroup,
			},
		}

	} else if len(instance.Network.ENI) > 0 {
		interfaces = make([]types.InstanceNetworkInterfaceSpecification, len(instance.Network.ENI))

		for index, eni := range instance.Network.ENI {
			var securityGroup []string

			if len(eni.SecurityGroupID) > 0 {
				securityGroup = []string{
					eni.SecurityGroupID,
				}
			}

			interfaces[index] = types.InstanceNetworkInterfaceSpecification{
				AssociatePublicIpAddress: aws.Bool(eni.PublicIP),
				DeleteOnTermination:      aws.Bool(true),
				Description:              aws.String(instance.InstanceName),
				DeviceIndex:              aws.Int32(int32(index)),
				SubnetId:                 aws.String(eni.GetNextSubnetsID(instance.NodeIndex)),
				Groups:                   securityGroup,
			}
		}
	} else {
		err = fmt.Errorf("unable create worker node, any network interface defined")
	}

	return
}

func (instance *Ec2Instance) buildBlockDeviceMappings(diskType types.VolumeType, diskSize int) (devices []types.BlockDeviceMapping) {
	if diskSize > 0 || len(diskType) > 0 {
		if diskSize == 0 {
			diskSize = 10
		} else {
			diskSize = diskSize / 1024
		}

		if len(diskType) == 0 {
			diskType = "gp3"
		}

		devices = []types.BlockDeviceMapping{
			{
				DeviceName: aws.String("/dev/sda1"),
				Ebs: &types.EbsBlockDevice{
					DeleteOnTermination: aws.Bool(true),
					VolumeType:          diskType,
					VolumeSize:          aws.Int32(int32(diskSize)),
				},
			},
		}
	}

	return
}

func (instance *Ec2Instance) buildTagSpecifications(nodeGroup string) []types.TagSpecification {
	instanceTags := make([]types.Tag, 0, len(instance.Tags)+3)

	instanceTags = append(instanceTags, types.Tag{
		Key:   aws.String("Name"),
		Value: aws.String(instance.InstanceName),
	})

	instanceTags = append(instanceTags, types.Tag{
		Key:   aws.String("NodeGroup"),
		Value: aws.String(nodeGroup),
	})

	instanceTags = append(instanceTags, types.Tag{
		Key:   aws.String("NodeIndex"),
		Value: aws.String(strconv.Itoa(instance.NodeIndex)),
	})

	// Add tags
	if instance.Tags != nil && len(instance.Tags) > 0 {
		for _, tag := range instance.Tags {
			instanceTags = append(instanceTags, types.Tag{
				Key:   aws.String(tag.Key),
				Value: aws.String(tag.Value),
			})
		}
	}

	return []types.TagSpecification{
		{
			ResourceType: types.ResourceTypeInstance,
			Tags:         instanceTags,
		},
	}

}

// Create will create a named VM not powered
// memory and disk are in megabytes
func (instance *Ec2Instance) Create(nodeGroup string, instanceType types.InstanceType, userData string, diskType types.VolumeType, diskSize int, desiredENI *UserDefinedNetworkInterface) (err error) {
	var result *ec2.RunInstancesOutput
	var pUserData *string

	if userData != "" {
		pUserData = &userData
	}

	glog.Debugf("Create: instance name %s in node group %s", instance.InstanceName, nodeGroup)

	// Check if instance is not already created
	if instance.Exists(instance.InstanceName) {
		glog.Debugf("Create: instance name %s already exists", instance.InstanceName)

		err = fmt.Errorf(constantes.ErrCantCreateVMAlreadyExist, instance.InstanceName)
	} else {
		ctx := instance.NewContext()
		defer ctx.Cancel()

		metadataOptions := &types.InstanceMetadataOptionsRequest{
			HttpEndpoint:            instance.MetadataOptions.HttpEndpoint,
			HttpTokens:              instance.MetadataOptions.HttpTokens,
			HttpPutResponseHopLimit: &instance.MetadataOptions.HttpPutResponseHopLimit,
			InstanceMetadataTags:    instance.MetadataOptions.InstanceMetadataTags,
		}

		iamInstanceProfile := &types.IamInstanceProfileSpecification{
			Arn: &instance.IamRole,
		}

		input := &ec2.RunInstancesInput{
			InstanceType:                      instanceType,
			ImageId:                           aws.String(instance.ImageID),
			KeyName:                           aws.String(instance.KeyName),
			InstanceInitiatedShutdownBehavior: types.ShutdownBehaviorStop,
			MaxCount:                          aws.Int32(1),
			MinCount:                          aws.Int32(1),
			UserData:                          pUserData,
			MetadataOptions:                   metadataOptions,
			IamInstanceProfile:                iamInstanceProfile,
			TagSpecifications:                 instance.buildTagSpecifications(nodeGroup),
			BlockDeviceMappings:               instance.buildBlockDeviceMappings(diskType, diskSize),
		}

		// Add ENI
		if input.NetworkInterfaces, err = instance.buildNetworkInterfaces(desiredENI); err == nil {

			if result, err = instance.client.RunInstances(ctx, input); err == nil {
				instance.Region = instance.awsWrapper.Region
				instance.Zone = *result.Instances[0].Placement.AvailabilityZone
				instance.InstanceID = *result.Instances[0].InstanceId
				instance.PrivateDNSName = *result.Instances[0].PrivateDnsName
			}
		}
	}

	return
}

func (instance *Ec2Instance) delete(wait bool) (err error) {
	glog.Debugf("Delete: instance %s id (%s)", instance.InstanceName, instance.getInstanceID())

	input := &ec2.TerminateInstancesInput{
		InstanceIds: []string{
			instance.InstanceID,
		},
	}

	if _, err = instance.client.TerminateInstances(context.Background(), input); err == nil {
		if wait {
			err = ec2.NewInstanceTerminatedWaiter(instance.client).Wait(context.Background(), &ec2.DescribeInstancesInput{
				InstanceIds: []string{
					instance.InstanceID,
				},
			},
				instance.getTimeout(),
			)
		}
	}

	return
}

// Delete a VM by name and don't wait for terminated status
func (instance *Ec2Instance) Delete() error {
	return instance.delete(false)
}

// Terminate a VM by name and wait until status is terminated
func (instance *Ec2Instance) Terminate() error {
	return instance.delete(true)
}

// PowerOn power on a VM by name
func (instance *Ec2Instance) PowerOn() error {
	var err error

	ctx := instance.NewContext()
	input := &ec2.StartInstancesInput{
		InstanceIds: []string{
			instance.InstanceID,
		},
	}

	defer ctx.Cancel()

	glog.Debugf("PowerOn: instance %s id (%s)", instance.InstanceName, instance.getInstanceID())

	if _, err = instance.client.StartInstances(ctx, input); err == nil {
		// Wait start is effective
		input := &ec2.DescribeInstancesInput{
			InstanceIds: []string{
				instance.InstanceID,
			},
		}

		if err = ec2.NewInstanceRunningWaiter(instance.client).Wait(context.Background(), input, instance.getTimeout()); err == nil {
			if ec2Instance, err := instance.getEc2Instance(); err == nil {
				if ec2Instance.PublicIpAddress != nil {
					instance.AddressIP = ec2Instance.PublicIpAddress
				} else {
					instance.AddressIP = ec2Instance.PrivateIpAddress
				}
			}
		}

	} else {
		glog.Debugf("powerOn: instance %s id (%s), got error %v", instance.InstanceName, instance.getInstanceID(), err)
	}

	return err
}

func (instance *Ec2Instance) powerOff(force bool) (err error) {
	input := &ec2.StopInstancesInput{
		Force: &force,
		InstanceIds: []string{
			instance.InstanceID,
		},
	}

	glog.Debugf("powerOff: instance %s id (%s)", instance.InstanceName, instance.getInstanceID())

	if _, err = instance.client.StopInstances(context.Background(), input); err == nil {
		input := &ec2.DescribeInstancesInput{
			InstanceIds: []string{
				instance.InstanceID,
			},
		}

		err = ec2.NewInstanceStoppedWaiter(instance.client).Wait(context.Background(), input, instance.getTimeout())
	} else {
		glog.Debugf("powerOff: instance %s id (%s), got error %v", instance.InstanceName, instance.getInstanceID(), err)
	}

	return
}

// PowerOff power off a VM by name
func (instance *Ec2Instance) PowerOff() error {
	return instance.powerOff(true)
}

// ShutdownGuest power off a VM by name
func (instance *Ec2Instance) ShutdownGuest() error {
	return instance.powerOff(false)
}

// Status return the current status of VM by name
func (instance *Ec2Instance) Status() (status providers.InstanceStatus, err error) {
	var ec2Instance *types.Instance

	glog.Debugf("Status: instance %s id (%s)", instance.InstanceName, instance.getInstanceID())

	if ec2Instance, err = instance.getEc2Instance(); err == nil {
		var address *string

		code := ec2Instance.State.Code

		if code == nil || *code == 48 {
			glog.Debugf("Status: instance %s id (%s) is terminated", instance.InstanceName, instance.getInstanceID())

			err = fmt.Errorf("EC2 Instance %s is terminated", instance.InstanceName)
		} else if *code == 16 || *code == 0 {
			if ec2Instance.PublicIpAddress != nil {
				address = ec2Instance.PublicIpAddress
			} else {
				address = ec2Instance.PrivateIpAddress
			}

			status = &instanceStatus{
				address: *address,
				powered: *code == 16 || *code == 0,
			}
		} else {
			status = &instanceStatus{
				powered: false,
			}
		}
	}

	return
}

func (instance *Ec2Instance) route53Client() (client *route53.Client, err error) {
	var cfg aws.Config

	if cfg, err = newSessionWithOptions(instance.GetRoute53AccessKey(), instance.GetRoute53SecretKey(), instance.GetRoute53AccessToken(), instance.GetFileName(), instance.GetRoute53Profile(), instance.GetRoute53Region()); err == nil {
		client = route53.NewFromConfig(cfg, func(o *route53.Options) {
			if glog.GetLevel() >= glog.TraceLevel {
				o.Logger = instance
				o.ClientLogMode = aws.LogSigning | aws.LogRequest | aws.LogResponse
			}
		})
	}

	return
}

func (instance *Ec2Instance) getRegisteredRecordSetAddress(name string) (address *string, err error) {
	var client *route53.Client

	if client, err = instance.route53Client(); err == nil {
		input := &route53.ListResourceRecordSetsInput{
			HostedZoneId:    aws.String(instance.Network.Route53.ZoneID),
			MaxItems:        aws.Int32(1),
			StartRecordName: aws.String(name),
			StartRecordType: route53types.RRTypeA,
		}

		if output, err := client.ListResourceRecordSets(context.TODO(), input); err == nil {
			if len(output.ResourceRecordSets) > 0 && len(output.ResourceRecordSets[0].ResourceRecords) > 0 {
				address = output.ResourceRecordSets[0].ResourceRecords[0].Value
			} else {
				err = fmt.Errorf("route53 entry: %s not found", name)
			}
		}
	}

	return
}

func (instance *Ec2Instance) changeResourceRecordSetsInput(cmd route53types.ChangeAction, name, address string, wait bool) (err error) {
	var client *route53.Client

	if client, err = instance.route53Client(); err == nil {
		var result *route53.ChangeResourceRecordSetsOutput

		input := &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: aws.String(instance.Network.Route53.ZoneID),
			ChangeBatch: &route53types.ChangeBatch{
				Comment: aws.String("Kubernetes worker node"),
				Changes: []route53types.Change{
					{
						Action: cmd,
						ResourceRecordSet: &route53types.ResourceRecordSet{
							Name: aws.String(name),
							TTL:  aws.Int64(60),
							Type: route53types.RRTypeA,
							ResourceRecords: []route53types.ResourceRecord{
								{
									Value: aws.String(address),
								},
							},
						},
					},
				},
			},
		}

		if result, err = client.ChangeResourceRecordSets(context.Background(), input); err == nil {
			if wait {
				input := &route53.GetChangeInput{
					Id: result.ChangeInfo.Id,
				}

				err = route53.NewResourceRecordSetsChangedWaiter(client).Wait(context.Background(), input, instance.getTimeout())
			}
		}
	}

	return
}

// RegisterDNS register EC2 instance in Route53
func (instance *Ec2Instance) RegisterDNS(name, address string, wait bool) error {
	return instance.changeResourceRecordSetsInput(route53_UpsertCmd, name, address, wait)
}

// UnRegisterDNS unregister EC2 instance in Route53
func (instance *Ec2Instance) UnRegisterDNS(name string, wait bool) error {
	if address, err := instance.getRegisteredRecordSetAddress(name); err == nil {
		return instance.changeResourceRecordSetsInput(route53_DeleteCmd, name, *address, wait)
	}

	return nil
}
