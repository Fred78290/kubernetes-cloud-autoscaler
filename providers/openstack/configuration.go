package openstack

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha2"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/gophercloud/gophercloud/v2"
	openstackapi "github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/config"
	"github.com/gophercloud/gophercloud/v2/openstack/config/clouds"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/pagination"
	glog "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type FloatingNetworkInfos struct {
	FloatingIPNetwork string `json:"network,omitempty"`
	ControlPlaneNode  bool   `json:"control-plane,omitempty"`
	WorkerNode        bool   `json:"worker-node,omitempty"`
}

type SecurityGroup struct {
	ControlPlaneNode string `default:"default" json:"control-plane"`
	WorkerNode       string `default:"default" json:"worker-node"`
}
type NetworkInterface struct {
	Enabled     *bool  `json:"enabled,omitempty" yaml:"primary,omitempty"`
	Primary     bool   `json:"primary,omitempty" yaml:"primary,omitempty"`
	NetworkName string `json:"network,omitempty" yaml:"network,omitempty"`
	NicName     string `json:"nic,omitempty" yaml:"nic,omitempty"`
	DHCP        bool   `json:"dhcp,omitempty" yaml:"dhcp,omitempty"`
	IPAddress   string `json:"address,omitempty" yaml:"address,omitempty"`
	Netmask     string `json:"netmask,omitempty" yaml:"netmask,omitempty"`
	Gateway     string `json:"gateway,omitempty" yaml:"gateway,omitempty"`
}

type Network struct {
	Domain        string                   `json:"domain,omitempty" yaml:"domain,omitempty"`
	Interfaces    []*NetworkInterface      `json:"interfaces,omitempty" yaml:"interfaces,omitempty"`
	DNS           *cloudinit.NetworkResolv `json:"dns,omitempty" yaml:"dns,omitempty"`
	FloatingInfos *FloatingNetworkInfos    `json:"floating-ip,omitempty"`
	SecurityGroup SecurityGroup            `json:"security-group"`
}
type Configuration struct {
	Cloud             string            `json:"cloud"`
	Image             string            `json:"image"`
	Timeout           time.Duration     `json:"timeout"`
	Network           Network           `json:"network"`
	AvailableGPUTypes map[string]string `json:"gpu-types"`
	KeyName           string            `json:"keyName"`
	OpenStackRegion   string            `default:"home" json:"region"`
	OpenStackZone     string            `default:"office" json:"zone"`
}

type openstackWrapper struct {
	Configuration
	network        *openStackNetwork
	providerClient *gophercloud.ProviderClient
	computeClient  *gophercloud.ServiceClient
	networkClient  *gophercloud.ServiceClient
	imageClient    *gophercloud.ServiceClient
	dnsClient      *gophercloud.ServiceClient
	testMode       bool
}

type openstackHandler struct {
	*openstackWrapper
	attachedNetwork *openStackNetwork

	runningInstance *ServerInstance
	instanceType    string
	instanceName    string
	instanceID      string
	controlPlane    bool
	nodeIndex       int
}

func NewOpenStackProviderConfiguration(fileName string) (providers.ProviderConfiguration, error) {
	var wrapper openstackWrapper
	var err error

	if err = providers.LoadConfig(fileName, &wrapper.Configuration); err != nil {
		glog.Errorf("Failed to open file: %s, error: %v", fileName, err)

		return nil, err
	}

	if err = wrapper.ConfigurationDidLoad(); err != nil {
		return nil, err
	}

	return &wrapper, nil
}

func (wrapper *openstackWrapper) SetMode(test bool) {
	wrapper.testMode = test
}

func (wrapper *openstackWrapper) GetMode() bool {
	return wrapper.testMode
}

func (wrapper *openstackWrapper) AttachInstance(instanceName string, controlPlane bool, nodeIndex int) (handler providers.ProviderHandler, err error) {
	var instanceID string

	if instanceID, err = wrapper.UUID(instanceName); err == nil {
		network := wrapper.network.Clone(controlPlane, nodeIndex)
		handler = &openstackHandler{
			openstackWrapper: wrapper,
			attachedNetwork:  network,
			instanceName:     instanceName,
			instanceID:       instanceID,
			controlPlane:     controlPlane,
			nodeIndex:        nodeIndex,
			runningInstance:  wrapper.newServerInstance(instanceName, instanceID, network, nodeIndex),
		}
	}

	return
}

func (wrapper *openstackWrapper) CreateInstance(instanceName, instanceType string, controlPlane bool, nodeIndex int) (handler providers.ProviderHandler, err error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if wrapper.InstanceExists(instanceName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, instanceName)
		err = fmt.Errorf(constantes.ErrVMAlreadyExists, instanceName)
	} else if instanceType, err = wrapper.getFlavor(ctx, instanceType); err == nil {
		handler = &openstackHandler{
			openstackWrapper: wrapper,
			attachedNetwork:  wrapper.network.Clone(controlPlane, nodeIndex),
			instanceType:     instanceType,
			instanceName:     instanceName,
			controlPlane:     controlPlane,
			nodeIndex:        nodeIndex,
		}
	}

	return
}

// This is duplicated from https://github.com/gophercloud/utils
// so that Gophercloud "core" doesn't have a dependency on the
// complementary utils repository.
func (wrapper *openstackWrapper) IDFromName(name string) (string, error) {
	count := 0
	id := ""

	listOpts := networks.ListOpts{
		Name: name,
	}

	pages, err := networks.List(wrapper.networkClient, listOpts).AllPages(context.TODO())
	if err != nil {
		return "", err
	}

	all, err := networks.ExtractNetworks(pages)
	if err != nil {
		return "", err
	}

	for _, s := range all {
		if s.Name == name {
			count++
			id = s.ID
		}
	}

	switch count {
	case 0:
		return "", gophercloud.ErrResourceNotFound{Name: name, ResourceType: "network"}
	case 1:
		return id, nil
	default:
		return "", gophercloud.ErrMultipleResourcesFound{Name: name, Count: count, ResourceType: "network"}
	}
}

func (wrapper *openstackWrapper) ConfigurationDidLoad() (err error) {
	var authOptions gophercloud.AuthOptions
	var endpointOptions gophercloud.EndpointOpts
	var tlsConfig *tls.Config

	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if authOptions, endpointOptions, tlsConfig, err = clouds.Parse(); err != nil {
		if authOptions, err = openstackapi.AuthOptionsFromEnv(); err != nil {
			return err
		}
	}

	authOptions.AllowReauth = true

	if wrapper.providerClient, err = config.NewProviderClient(ctx, authOptions, config.WithTLSConfig(tlsConfig)); err != nil {
		return err
	}

	if wrapper.computeClient, err = openstackapi.NewComputeV2(wrapper.providerClient, endpointOptions); err != nil {
		return err
	}

	if wrapper.networkClient, err = openstackapi.NewNetworkV2(wrapper.providerClient, endpointOptions); err != nil {
		return err
	}

	if wrapper.imageClient, err = openstackapi.NewImageV2(wrapper.providerClient, endpointOptions); err != nil {
		return err
	}

	if wrapper.dnsClient, err = openstackapi.NewDNSV2(wrapper.providerClient, endpointOptions); err != nil {
		glog.Warnf("service DNS got error: %v. Ignoring", err)
		wrapper.dnsClient = nil
	}

	if wrapper.Image, err = wrapper.getImage(ctx, wrapper.Image); err != nil {
		return err
	}

	network := wrapper.Configuration.Network
	onet := &openStackNetwork{
		OpenstackInterfaces: make([]openstackNetworkInterface, 0, len(wrapper.Configuration.Network.Interfaces)),
		Network: &providers.Network{
			Domain:     network.Domain,
			DNS:        network.DNS,
			Interfaces: make([]*providers.NetworkInterface, 0, len(network.Interfaces)),
		},
	}

	if network.FloatingInfos != nil {
		floatingInfos := network.FloatingInfos

		if floatingInfos.FloatingIPNetwork == "" && (floatingInfos.ControlPlaneNode || floatingInfos.WorkerNode) {
			return fmt.Errorf("floating network is not defined")
		}

		if floatingInfos.FloatingIPNetwork != "" {
			floatingIPNetwork := ""

			if floatingIPNetwork, err = wrapper.IDFromName(floatingInfos.FloatingIPNetwork); err != nil {
				return fmt.Errorf("the floating ip network: %s not found, reason: %v", floatingInfos.FloatingIPNetwork, err)
			}

			floatingInfos.FloatingIPNetwork = floatingIPNetwork
		}
	}

	for _, inf := range network.Interfaces {
		openstackInterface := openstackNetworkInterface{
			NetworkInterface: &providers.NetworkInterface{
				Enabled:     inf.Enabled,
				MacAddress:  "ignore",
				Primary:     inf.Primary,
				NetworkName: inf.NetworkName,
				NicName:     inf.NicName,
				DHCP:        inf.DHCP,
				IPAddress:   inf.IPAddress,
				Gateway:     inf.Gateway,
			},
		}

		if openstackInterface.networkID, err = wrapper.IDFromName(inf.NetworkName); err != nil {
			return fmt.Errorf("unable to find network: %s, reason: %v", inf.NetworkName, err)
		}

		onet.OpenstackInterfaces = append(onet.OpenstackInterfaces, openstackInterface)
		onet.Interfaces = append(onet.Interfaces, openstackInterface.NetworkInterface)
	}

	onet.Network.ConfigurationDidLoad()

	wrapper.network = onet

	if wrapper.dnsClient != nil {
		if err = onet.ConfigurationDns(ctx, wrapper.dnsClient); err != nil {
			return err
		}
	}

	return
}

func (wrapper *openstackWrapper) getFlavor(ctx *context.Context, name string) (string, error) {
	if pages, err := flavors.ListDetail(wrapper.computeClient, flavors.ListOpts{}).AllPages(ctx); err != nil {
		return "", err
	} else if flavors, err := flavors.ExtractFlavors(pages); err != nil {
		return "", err
	} else {
		for _, flavor := range flavors {
			if flavor.Name == name {
				return flavor.ID, nil
			}
		}
	}

	return "", fmt.Errorf("flavor: %s, no found", name)
}

func (wrapper *openstackWrapper) getImage(ctx *context.Context, name string) (string, error) {
	if pages, err := images.List(wrapper.imageClient, images.ListOpts{Name: name}).AllPages(ctx); err != nil {
		return "", err
	} else if images, err := images.ExtractImages(pages); err != nil {
		return "", err
	} else {
		for _, image := range images {
			if image.Name == name {
				return image.ID, nil
			}
		}
	}

	return "", fmt.Errorf("image: %s, no found", name)
}

func (wrapper *openstackWrapper) getAddress(ctx *context.Context, server *servers.Server) (addressIP string, err error) {
	var allPages pagination.Page
	var addresses []servers.Address

	if len(server.AccessIPv4) > 0 {
		return server.AccessIPv4, nil
	}

	primaryInterface := wrapper.network.PrimaryInterface()

	if allPages, err = servers.ListAddressesByNetwork(wrapper.computeClient, server.ID, primaryInterface.NetworkName).AllPages(ctx); err != nil {
		return
	}

	if addresses, err = servers.ExtractNetworkAddresses(allPages); err != nil {
		return
	}

	if len(addresses) > 0 {
		addressIP = addresses[0].Address
	}

	return
}

func (wrapper *openstackWrapper) getServer(ctx *context.Context, name string) (vm *ServerInstance, err error) {
	var allServers []OpenStackServer
	var allPages pagination.Page

	if allPages, err = servers.List(wrapper.computeClient, servers.ListOpts{Name: name}).AllPages(ctx); err == nil {
		if err = servers.ExtractServersInto(allPages, &allServers); err == nil {
			if len(allServers) == 0 {
				err = fmt.Errorf("server: %s not found", name)
			} else {
				var addressIP string
				server := allServers[0]

				if addressIP, err = wrapper.getAddress(ctx, &server.Server); err == nil {
					vm = &ServerInstance{
						openstackWrapper: wrapper,
						InstanceName:     server.Name,
						InstanceID:       server.ID,
						AddressIP:        addressIP,
						Region:           wrapper.OpenStackRegion,
						Zone:             server.AvailabilityZone,
					}
				}
			}
		}
	}

	return
}

func (wrapper *openstackWrapper) newServerInstance(instanceName, instanceID string, network *openStackNetwork, nodeIndex int) *ServerInstance {
	return &ServerInstance{
		openstackWrapper: wrapper,
		InstanceName:     instanceName,
		InstanceID:       instanceID,
		NodeIndex:        nodeIndex,
		PrivateDNSName:   instanceName,
		Region:           wrapper.OpenStackRegion,
		Zone:             wrapper.OpenStackZone,
		attachedNetwork:  network,
	}
}

func (wrapper *openstackWrapper) GetAvailableGpuTypes() map[string]string {
	return wrapper.AvailableGPUTypes
}

func (wrapper *openstackWrapper) InstanceExists(name string) (exists bool) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if _, err := wrapper.getServer(ctx, name); err != nil {
		return false
	}

	return true
}

func (wrapper *openstackWrapper) UUID(name string) (vmuuid string, err error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if server, err := wrapper.getServer(ctx, name); err != nil {
		return "", err
	} else {
		return server.InstanceID, nil
	}
}

func (wrapper *openstackWrapper) RetrieveNetworkInfos(vmuuid string, network *providers.Network) (err error) {
	return
}

func (handler *openstackHandler) GetTimeout() time.Duration {
	return handler.Timeout
}

func (handler *openstackHandler) ConfigureNetwork(network v1alpha2.ManagedNetworkConfig) {
	handler.network.ConfigureOpenStackNetwork(network.OpenStack)
}

func (handler *openstackHandler) RetrieveNetworkInfos() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	handler.attachedNetwork.retrieveNetworkInfos(ctx, handler.dnsClient, handler.instanceName)

	return handler.openstackWrapper.RetrieveNetworkInfos(handler.instanceID, handler.network.Network)
}

func (handler *openstackHandler) UpdateMacAddressTable() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.network.UpdateMacAddressTable()
}

func (handler *openstackHandler) GenerateProviderID() string {
	if len(handler.instanceID) > 0 {
		return fmt.Sprintf("openstack://%s/%s", handler.Configuration.OpenStackRegion, handler.instanceID)
	} else {
		return ""
	}
}

func (handler *openstackHandler) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion:  handler.runningInstance.Region,
		constantes.NodeLabelTopologyZone:    handler.runningInstance.Zone,
		constantes.NodeLabelVMWareCSIRegion: handler.runningInstance.Region,
		constantes.NodeLabelVMWareCSIZone:   handler.runningInstance.Zone,
	}
}

func (handler *openstackHandler) InstanceCreate(input *providers.InstanceCreateInput) (vmuuid string, err error) {
	var userData string

	handler.runningInstance = handler.newServerInstance(handler.instanceName, "", handler.network, handler.nodeIndex)

	if userData, err = handler.encodeCloudInit(input.CloudInit); err != nil {
		return "", err
	}

	if err = handler.runningInstance.Create(input.ControlPlane, input.NodeGroup, handler.instanceType, userData, input.Machine.GetDiskSize()); err != nil {
		return "", err
	}

	handler.instanceID = handler.runningInstance.InstanceID

	return handler.runningInstance.InstanceID, nil
}

func (handler *openstackHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (address string, err error) {
	if handler.runningInstance == nil {
		return "", fmt.Errorf("instance not attached when calling WaitForVMReady")
	}

	return handler.runningInstance.WaitForIP(callback)
}

func (handler *openstackHandler) InstancePrimaryAddressIP() (address string) {
	return handler.network.PrimaryAddressIP()
}

func (handler *openstackHandler) InstanceID() (vmuuid string, err error) {
	if handler.runningInstance == nil {
		return "", fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.InstanceID, nil
}

func (handler *openstackHandler) InstanceAutoStart() (err error) {
	return
}

func (handler *openstackHandler) InstancePowerOn() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	return handler.runningInstance.PowerOn(ctx)
}

func (handler *openstackHandler) InstancePowerOff() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	return handler.runningInstance.PowerOff(ctx)
}

func (handler *openstackHandler) InstanceShutdownGuest() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.ShutdownGuest()
}

func (handler *openstackHandler) InstanceDelete() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.Delete()
}

func (handler *openstackHandler) InstanceStatus() (status providers.InstanceStatus, err error) {
	if handler.runningInstance == nil {
		return nil, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.Status()
}

func (handler *openstackHandler) InstanceWaitForPowered() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.WaitForPowered()
}

func (handler *openstackHandler) InstanceWaitForToolsRunning() (bool, error) {
	return true, nil
}

func (handler *openstackHandler) InstanceMaxPods(desiredMaxPods int) (int, error) {
	if desiredMaxPods == 0 {
		desiredMaxPods = 110
	}

	return desiredMaxPods, nil
}

func (handler *openstackHandler) PrivateDNSName() (string, error) {
	return handler.instanceName, nil
}

func (handler *openstackHandler) RegisterDNS(address string) (err error) {
	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	return handler.network.registerDNS(ctx, handler.dnsClient, handler.instanceName, address)
}

func (handler *openstackHandler) UnregisterDNS(address string) (err error) {
	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	return handler.network.unregisterDNS(ctx, handler.dnsClient, handler.instanceName, address)
}

func (handler *openstackHandler) UUID(name string) (string, error) {
	if handler.runningInstance != nil && handler.runningInstance.InstanceName == name {
		return handler.runningInstance.InstanceID, nil
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	if server, err := handler.getServer(ctx, name); err != nil {
		return "", err
	} else {
		return server.InstanceID, nil
	}
}

func (handler *openstackHandler) encodeCloudInit(object any) (result string, err error) {
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
