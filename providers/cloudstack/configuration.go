package cloudstack

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha2"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/rfc2136"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	"github.com/apache/cloudstack-go/v2/cloudstack"
	glog "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type SecurityGroup struct {
	ControlPlaneNode string `default:"default" json:"control-plane"`
	WorkerNode       string `default:"default" json:"worker-node"`
}

type NetworkInfos struct {
	PublicControlPlaneNode bool `json:"public-control-plane,omitempty"`
	PublicWorkerNode       bool `json:"public-worker-node,omitempty"`
}

type NetworkInterface struct {
	Enabled     *bool  `json:"enabled,omitempty" yaml:"primary,omitempty"`
	Primary     bool   `json:"primary,omitempty" yaml:"primary,omitempty"`
	NetworkName string `json:"network,omitempty" yaml:"network,omitempty"`
	DHCP        bool   `json:"dhcp,omitempty" yaml:"dhcp,omitempty"`
	IPAddress   string `json:"address,omitempty" yaml:"address,omitempty"`
	Netmask     string `json:"netmask,omitempty" yaml:"netmask,omitempty"`
}

type Network struct {
	Domain                 string              `json:"domain,omitempty" yaml:"domain,omitempty"`
	Interfaces             []*NetworkInterface `json:"interfaces,omitempty" yaml:"interfaces,omitempty"`
	PublicControlPlaneNode bool                `json:"public-control-plane,omitempty"`
	PublicWorkerNode       bool                `json:"public-worker-node,omitempty"`
	SecurityGroup          SecurityGroup       `json:"security-group"`
}

type Configuration struct {
	ApiUrl            string            `json:"api-url,omitempty"`
	ApiKey            string            `json:"api-key,omitempty"`
	SecretKey         string            `json:"secret-key,omitempty"`
	SSLNoVerify       bool              `json:"ssl-no-verify,omitempty"`
	SshKeyName        string            `json:"ssh-key-name,omitempty"`
	ProjectID         string            `json:"project-id,omitempty"`
	ZoneId            string            `json:"zone-id,omitempty"`
	PodId             string            `json:"pod-id,omitempty"`
	ClusterId         string            `json:"cluster-id,omitempty"`
	HostId            string            `json:"host-id,omitempty"`
	VpcId             string            `json:"vpc-id,omitempty"`
	Hypervisor        string            `json:"hypervisor,omitempty"`
	TemplateId        string            `json:"template,omitempty"`
	Timeout           time.Duration     `json:"timeout"`
	AvailableGPUTypes map[string]string `json:"gpu-types"`
	UseBind9          bool              `json:"use-bind9"`
	Bind9Host         string            `json:"bind9-host"`
	RndcKeyFile       string            `json:"rndc-key-file"`
	Network           Network           `json:"network"`
}

type CloudStackOptions []cloudstack.OptionFunc

type cloudstackWrapper struct {
	Configuration
	client        *cloudstack.CloudStackClient
	network       *cloudstackNetwork
	bind9Provider *rfc2136.RFC2136Provider
	options       CloudStackOptions
	testMode      bool
}

type cloudstackHandler struct {
	*cloudstackWrapper
	attachedNetwork *cloudstackNetwork
	runningInstance *ServerInstance
	instanceName    string
	instanceType    string
	instanceID      string
	controlPlane    bool
	nodeIndex       int
}

type HypervisorSetter interface {
	SetHypervisor(string)
}

type PodIDSetter interface {
	SetPodid(string)
}

type HostIDSetter interface {
	SetHostid(string)
}

type ProjectIDSetter interface {
	SetProjectid(string)
}

type TemplateIDSetter interface {
	SetTemplateid(string)
}

type ClusterIDSetter interface {
	SetClusterid(string)
}

type VpcIDSetter interface {
	SetVpcid(string)
}

type SetKeypairSetter interface {
	SetKeypair(string)
}

func NewCloudStackProviderConfiguration(fileName string) (providers.ProviderConfiguration, error) {
	var wrapper cloudstackWrapper
	var err error

	if err = utils.LoadConfig(fileName, &wrapper.Configuration); err != nil {
		glog.Errorf("Failed to open file: %s, error: %v", fileName, err)

		return nil, err
	}

	if err = wrapper.ConfigurationDidLoad(); err != nil {
		return nil, err
	}

	return &wrapper, nil
}

func (opts CloudStackOptions) ApplyOptions(cs *cloudstack.CloudStackClient, params interface{}) (err error) {
	for _, fn := range opts {
		if err = fn(cs, params); err != nil {
			return
		}
	}

	return
}

func (wrapper *cloudstackWrapper) SetMode(test bool) {
	wrapper.testMode = test
}

func (wrapper *cloudstackWrapper) GetMode() bool {
	return wrapper.testMode
}

func (wrapper *cloudstackWrapper) AttachInstance(instanceName string, controlPlane bool, nodeIndex int) (handler providers.ProviderHandler, err error) {
	var instanceID string

	if instanceID, err = wrapper.UUID(instanceName); err == nil {
		network := wrapper.network.Clone(controlPlane, nodeIndex)
		handler = &cloudstackHandler{
			cloudstackWrapper: wrapper,
			attachedNetwork:   network,
			instanceName:      instanceName,
			instanceID:        instanceID,
			controlPlane:      controlPlane,
			nodeIndex:         nodeIndex,
			runningInstance:   wrapper.newServerInstance(instanceName, instanceID, network, nodeIndex),
		}
	}

	return
}

func (wrapper *cloudstackWrapper) CreateInstance(instanceName, instanceType string, controlPlane bool, nodeIndex int) (handler providers.ProviderHandler, err error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if wrapper.InstanceExists(instanceName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, instanceName)
		err = fmt.Errorf(constantes.ErrVMAlreadyExists, instanceName)
	} else if instanceType, err = wrapper.getServiceOffering(instanceType); err == nil {
		handler = &cloudstackHandler{
			cloudstackWrapper: wrapper,
			attachedNetwork:   wrapper.network.Clone(controlPlane, nodeIndex),
			instanceType:      instanceType,
			instanceName:      instanceName,
			controlPlane:      controlPlane,
			nodeIndex:         nodeIndex,
		}
	}

	return
}

func (wrapper *cloudstackWrapper) defaultOptions() CloudStackOptions {
	if len(wrapper.options) == 0 {
		wrapper.options = append(wrapper.options, cloudstack.WithZone(wrapper.ZoneId), cloudstack.WithProject(wrapper.ProjectID),
			func(cs *cloudstack.CloudStackClient, p interface{}) error {
				if ps, ok := p.(HypervisorSetter); ok && len(wrapper.Hypervisor) > 0 {
					ps.SetHypervisor(wrapper.Hypervisor)
				}
				return nil
			},
			func(cs *cloudstack.CloudStackClient, p interface{}) error {
				if ps, ok := p.(TemplateIDSetter); ok && len(wrapper.TemplateId) > 0 {
					ps.SetTemplateid(wrapper.TemplateId)
				}
				return nil
			},
			func(cs *cloudstack.CloudStackClient, p interface{}) error {
				if ps, ok := p.(PodIDSetter); ok && len(wrapper.PodId) > 0 {
					ps.SetPodid(wrapper.PodId)
				}
				return nil
			},
			func(cs *cloudstack.CloudStackClient, p interface{}) error {
				if ps, ok := p.(ClusterIDSetter); ok && len(wrapper.ClusterId) > 0 {
					ps.SetClusterid(wrapper.ClusterId)
				}
				return nil
			},
			func(cs *cloudstack.CloudStackClient, p interface{}) error {
				if ps, ok := p.(HostIDSetter); ok && len(wrapper.HostId) > 0 {
					ps.SetHostid(wrapper.HostId)
				}
				return nil
			},
			func(cs *cloudstack.CloudStackClient, p interface{}) error {
				if ps, ok := p.(ProjectIDSetter); ok && len(wrapper.ProjectID) > 0 {
					ps.SetProjectid(wrapper.ProjectID)
				}
				return nil
			},
			func(cs *cloudstack.CloudStackClient, p interface{}) error {
				if ps, ok := p.(VpcIDSetter); ok && len(wrapper.VpcId) > 0 {
					ps.SetVpcid(wrapper.VpcId)
				}
				return nil
			},
			func(cs *cloudstack.CloudStackClient, p interface{}) error {
				if ps, ok := p.(SetKeypairSetter); ok && len(wrapper.SshKeyName) > 0 {
					ps.SetKeypair(wrapper.SshKeyName)
				}
				return nil
			})
	}

	return wrapper.options
}

func (wrapper *cloudstackWrapper) ConfigurationDidLoad() (err error) {
	if wrapper.Configuration.UseBind9 {
		if wrapper.bind9Provider, err = rfc2136.NewDNSRFC2136ProviderCredentials(wrapper.Configuration.Bind9Host, wrapper.Configuration.RndcKeyFile); err != nil {
			return err
		}
	}

	wrapper.client = cloudstack.NewAsyncClient(wrapper.ApiUrl, wrapper.ApiKey, wrapper.SecretKey, !wrapper.SSLNoVerify)

	if wrapper.client == nil {
		return errors.New("no cloud provider config given")
	}

	//wrapper.client.DefaultOptions(wrapper.defaultOptions()...)

	network := wrapper.Configuration.Network
	onet := &cloudstackNetwork{
		Network: &providers.Network{
			Domain:     network.Domain,
			Interfaces: make([]*providers.NetworkInterface, 0, len(network.Interfaces)),
		},
	}

	wrapper.network = onet

	for _, inf := range network.Interfaces {
		var network *cloudstack.Network
		var count int

		cloudstackInterface := &cloudstackNetworkInterface{
			NetworkInterface: &providers.NetworkInterface{
				Enabled:     inf.Enabled,
				MacAddress:  "ignore",
				Primary:     inf.Primary,
				NetworkName: inf.NetworkName,
				NicName:     "ens3",
				DHCP:        inf.DHCP,
				IPAddress:   inf.IPAddress,
				Netmask:     inf.Netmask,
			},
		}

		onet.CloudStackInterfaces = append(onet.CloudStackInterfaces, cloudstackInterface)
		onet.Interfaces = append(onet.Interfaces, cloudstackInterface.NetworkInterface)

		if network, count, err = wrapper.client.Network.GetNetworkByName(inf.NetworkName, wrapper.defaultOptions()...); err != nil {
			return fmt.Errorf("unable to find network: %s, reason: %v", inf.NetworkName, err)
		} else if count == 0 {
			return fmt.Errorf("network: %s not found", inf.NetworkName)
		} else if count == 1 {
			cloudstackInterface.networkID = network.Id
		} else {
			return fmt.Errorf("%d named networks: %s", count, inf.NetworkName)
		}
	}

	onet.Network.ConfigurationDidLoad()

	return
}

func (wrapper *cloudstackWrapper) getServiceOffering(name string) (id string, err error) {
	id, _, err = wrapper.client.ServiceOffering.GetServiceOfferingID(name, wrapper.defaultOptions()...)

	return
}

func (wrapper *cloudstackWrapper) getAddress(server *cloudstack.VirtualMachine) (addressIP string, err error) {
	if len(server.Nic) == 0 {
		err = fmt.Errorf("instance: %s doesn't have nic", server.Name)
	} else {
		addressIP = server.Nic[0].Ipaddress
	}

	return
}

func (wrapper *cloudstackWrapper) getServerInstance(name string) (vm *ServerInstance, err error) {
	var response *cloudstack.VirtualMachine
	var addressIP string

	if response, _, err = wrapper.client.VirtualMachine.GetVirtualMachineByName(name, cloudstack.WithZone(wrapper.ZoneId), cloudstack.WithProject(wrapper.ProjectID)); err != nil {
		return
	}

	if addressIP, err = wrapper.getAddress(response); err == nil {
		vm = &ServerInstance{
			cloudstackWrapper: wrapper,
			InstanceName:      response.Name,
			InstanceID:        response.Id,
			AddressIP:         addressIP,
			PublicAddress:     response.Publicip,
			PublicAddressID:   response.Publicipid,
			PrivateDNSName:    response.Instancename,
			ZoneName:          response.Zonename,
			HostName:          response.Hostname,
		}
	}
	return
}

func (wrapper *cloudstackWrapper) newServerInstance(instanceName, instanceID string, network *cloudstackNetwork, nodeIndex int) *ServerInstance {
	return &ServerInstance{
		cloudstackWrapper: wrapper,
		InstanceName:      instanceName,
		InstanceID:        instanceID,
		NodeIndex:         nodeIndex,
		PrivateDNSName:    instanceName,
		attachedNetwork:   network,
	}
}

func (wrapper *cloudstackWrapper) GetAvailableGpuTypes() map[string]string {
	return wrapper.AvailableGPUTypes
}

func (wrapper *cloudstackWrapper) InstanceExists(name string) (exists bool) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if _, err := wrapper.getServerInstance(name); err != nil {
		return false
	}

	return true
}

func (wrapper *cloudstackWrapper) UUID(name string) (vmuuid string, err error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if server, err := wrapper.getServerInstance(name); err != nil {
		return "", err
	} else {
		return server.InstanceID, nil
	}
}

func (wrapper *cloudstackWrapper) RetrieveNetworkInfos(vmuuid string, network *providers.Network) (err error) {
	return
}

func (handler *cloudstackHandler) GetTimeout() time.Duration {
	return handler.Timeout
}

func (handler *cloudstackHandler) ConfigureNetwork(network v1alpha2.ManagedNetworkConfig) {
	handler.attachedNetwork.ConfigureManagedNetwork(network.CloudStack.Managed())
}

func (handler *cloudstackHandler) RetrieveNetworkInfos() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	return handler.cloudstackWrapper.RetrieveNetworkInfos(handler.instanceID, handler.attachedNetwork.Network)
}

func (handler *cloudstackHandler) UpdateMacAddressTable() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.attachedNetwork.UpdateMacAddressTable()
}

func (handler *cloudstackHandler) GenerateProviderID() string {
	if len(handler.instanceID) > 0 {
		return handler.instanceID
	} else {
		return ""
	}
}

func (handler *cloudstackHandler) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion:  handler.runningInstance.ZoneName,
		constantes.NodeLabelTopologyZone:    handler.runningInstance.HostName,
		constantes.NodeLabelVMWareCSIRegion: handler.runningInstance.ZoneName,
		constantes.NodeLabelVMWareCSIZone:   handler.runningInstance.HostName,
	}
}

func (handler *cloudstackHandler) InstanceCreate(input *providers.InstanceCreateInput) (vmuuid string, err error) {
	var userData string

	handler.runningInstance = handler.newServerInstance(handler.instanceName, "", handler.attachedNetwork, handler.nodeIndex)

	if userData, err = handler.encodeCloudInit(input.CloudInit); err != nil {
		return "", err
	}

	if err = handler.runningInstance.Create(input.ControlPlane, input.NodeGroup, handler.instanceType, userData, input.Machine.GetDiskSize()); err != nil {
		return "", err
	}

	handler.instanceID = handler.runningInstance.InstanceID

	return handler.runningInstance.InstanceID, nil
}

func (handler *cloudstackHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (address string, err error) {
	if handler.runningInstance == nil {
		return "", fmt.Errorf("instance not attached when calling WaitForVMReady")
	}

	return handler.runningInstance.WaitForIP(callback)
}

func (handler *cloudstackHandler) InstancePrimaryAddressIP() (address string) {
	return handler.attachedNetwork.PrimaryAddressIP()
}

func (handler *cloudstackHandler) InstanceID() (vmuuid string, err error) {
	if handler.runningInstance == nil {
		return "", fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.InstanceID, nil
}

func (handler *cloudstackHandler) InstanceAutoStart() (err error) {
	return
}

func (handler *cloudstackHandler) InstancePowerOn() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	return handler.runningInstance.PowerOn(ctx)
}

func (handler *cloudstackHandler) InstancePowerOff() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	return handler.runningInstance.PowerOff(ctx)
}

func (handler *cloudstackHandler) InstanceShutdownGuest() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.ShutdownGuest()
}

func (handler *cloudstackHandler) InstanceDelete() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.Delete()
}

func (handler *cloudstackHandler) InstanceCreated() bool {
	if handler.runningInstance == nil {
		return false
	}

	return handler.runningInstance.InstanceExists(handler.instanceName)
}

func (handler *cloudstackHandler) InstanceStatus() (status providers.InstanceStatus, err error) {
	if handler.runningInstance == nil {
		return nil, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.Status()
}

func (handler *cloudstackHandler) InstanceWaitForPowered() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.WaitForPowered()
}

func (handler *cloudstackHandler) InstanceWaitForToolsRunning() (bool, error) {
	return true, nil
}

func (handler *cloudstackHandler) InstanceMaxPods(desiredMaxPods int) (int, error) {
	if desiredMaxPods == 0 {
		desiredMaxPods = 110
	}

	return desiredMaxPods, nil
}

func (handler *cloudstackHandler) PrivateDNSName() (string, error) {
	return handler.instanceName, nil
}

func (handler *cloudstackHandler) RegisterDNS(address string) (err error) {
	if handler.bind9Provider != nil {
		err = handler.bind9Provider.AddRecord(handler.instanceName+"."+handler.Network.Domain, handler.Network.Domain, address)
	}

	return
}

func (handler *cloudstackHandler) UnregisterDNS(address string) (err error) {
	if handler.bind9Provider != nil {
		err = handler.bind9Provider.RemoveRecord(handler.instanceName+"."+handler.Network.Domain, handler.Network.Domain, address)
	}

	return
}

func (handler *cloudstackHandler) UUID(name string) (string, error) {
	if handler.runningInstance != nil && handler.runningInstance.InstanceName == name {
		return handler.runningInstance.InstanceID, nil
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	if server, err := handler.getServerInstance(name); err != nil {
		return "", err
	} else {
		return server.InstanceID, nil
	}
}

func (handler *cloudstackHandler) encodeCloudInit(object any) (result string, err error) {
	if object == nil {
		return
	}

	var out bytes.Buffer

	fmt.Fprintln(&out, "#cloud-config")

	wr := yaml.NewEncoder(&out)

	err = wr.Encode(object)
	wr.Close()

	if err != nil {
		return
	}

	result = base64.StdEncoding.EncodeToString(out.Bytes())

	return
}
