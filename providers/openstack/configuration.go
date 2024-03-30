package openstack

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/images"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/gophercloud/utils/openstack/clientconfig"
	glog "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type Configuration struct {
	Cloud             string            `json:"cloud"`
	Image             string            `json:"image"`
	Timeout           time.Duration     `json:"timeout"`
	Network           *OpenStackNetwork `json:"network"`
	AvailableGPUTypes map[string]string `json:"gpu-types"`
	AllowUpgrade      bool              `default:true json:"allow-upgrade"`
	OpenStackRegion   string            `default:"home" json:"csi-region"`
	OpenStackZone     string            `default:"office" json:"csi-zone"`
}

type openstackWrapper struct {
	Configuration
	providerClient *gophercloud.ProviderClient
	computeClient  *gophercloud.ServiceClient
	networkClient  *gophercloud.ServiceClient
	imageClient    *gophercloud.ServiceClient
	dnsClient      *gophercloud.ServiceClient
	testMode       bool
}

type openstackHandler struct {
	*openstackWrapper
	network *OpenStackNetwork

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

	if wrapper.providerClient, err = clientconfig.AuthenticatedClient(&clientconfig.ClientOpts{Cloud: wrapper.Configuration.Cloud}); err != nil {
		return nil, err
	}

	if wrapper.computeClient, err = clientconfig.NewServiceClient("compute", &clientconfig.ClientOpts{Cloud: wrapper.Configuration.Cloud}); err != nil {
		return nil, err
	}

	if wrapper.networkClient, err = clientconfig.NewServiceClient("network", &clientconfig.ClientOpts{Cloud: wrapper.Configuration.Cloud}); err != nil {
		return nil, err
	}

	if wrapper.imageClient, err = clientconfig.NewServiceClient("image", &clientconfig.ClientOpts{Cloud: wrapper.Configuration.Cloud}); err != nil {
		return nil, err
	}

	if wrapper.dnsClient, _ = clientconfig.NewServiceClient("dns", &clientconfig.ClientOpts{Cloud: wrapper.Configuration.Cloud}); err != nil {
		glog.Warnf("service DNS got error: %v. Ignoring", err)
		wrapper.dnsClient = nil
	}

	if wrapper.Image, err = wrapper.getFlavor(wrapper.Image); err != nil {
		return nil, err
	}

	if err = wrapper.Configuration.Network.ConfigurationDidLoad(wrapper.networkClient); err != nil {
		return nil, err
	}

	if wrapper.dnsClient != nil {
		if err = wrapper.Configuration.Network.ConfigurationDns(wrapper.dnsClient); err != nil {
			return nil, err
		}
	}

	return &wrapper, nil
}

func (wrapper *openstackWrapper) SetMode(test bool) {
	wrapper.testMode = test
}

func (wrapper *openstackWrapper) GetMode() bool {
	return wrapper.testMode
}

func (wrapper *openstackWrapper) AttachInstance(instanceName string, controlPlane bool, nodeIndex int) (providers.ProviderHandler, error) {
	if vmuuid, err := wrapper.UUID(instanceName); err != nil {
		return nil, err
	} else {
		return &openstackHandler{
			openstackWrapper: wrapper,
			network:          wrapper.Network.Clone(controlPlane, nodeIndex),
			instanceName:     instanceName,
			instanceID:       vmuuid,
			controlPlane:     controlPlane,
			nodeIndex:        nodeIndex,
		}, nil
	}

}

func (wrapper *openstackWrapper) CreateInstance(instanceName, instanceType string, controlPlane bool, nodeIndex int) (handler providers.ProviderHandler, err error) {
	if wrapper.InstanceExists(instanceName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, instanceName)
		err = fmt.Errorf(constantes.ErrVMAlreadyExists, instanceName)
	} else if instanceType, err = wrapper.getFlavor(instanceType); err == nil {
		handler = &openstackHandler{
			openstackWrapper: wrapper,
			network:          wrapper.Network.Clone(controlPlane, nodeIndex),
			instanceType:     instanceType,
			instanceName:     instanceName,
			controlPlane:     controlPlane,
			nodeIndex:        nodeIndex,
		}
	}

	return
}

func (wrapper *openstackWrapper) getFlavor(name string) (string, error) {
	if pages, err := images.ListDetail(wrapper.imageClient, images.ListOpts{Name: name}).AllPages(); err != nil {
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

func (wrapper *openstackWrapper) getAddress(server *servers.Server) (addressIP string, err error) {
	var allPages pagination.Page
	var addresses map[string][]servers.Address

	if len(server.AccessIPv4) > 0 {
		addressIP = server.AccessIPv4
	} else {
		if allPages, err = servers.ListAddresses(wrapper.computeClient, server.ID).AllPages(); err != nil {
			return "", err
		}

		if addresses, err = servers.ExtractAddresses(allPages); err != nil {
			return "", err
		}

		for _, addr := range addresses {
			addressIP = addr[0].Address
			break
		}
	}

	return
}

func (wrapper *openstackWrapper) getServer(name string) (vm *ServerInstance, err error) {
	var allServers []OpenStackServer
	var allPages pagination.Page

	if allPages, err = servers.List(wrapper.computeClient, servers.ListOpts{Name: name}).AllPages(); err == nil {
		if err = servers.ExtractServersInto(allPages, &allServers); err == nil {
			if len(allServers) == 0 {
				err = fmt.Errorf("server: %s not found", name)
			} else {
				var addressIP string
				server := allServers[0]

				if addressIP, err = wrapper.getAddress(&server.Server); err == nil {
					vm = &ServerInstance{
						openstackWrapper: wrapper,
						InstanceName:     server.Name,
						InstanceID:       server.ID,
						AddressIP:        addressIP,
						Region:           wrapper.OpenStackRegion,
						Zone:             server.AvailabilityZone.ZoneName,
					}
				}
			}
		}
	}

	return
}

func (wrapper *openstackWrapper) newServer(instanceName string, nodeIndex int) (*ServerInstance, error) {
	vm := &ServerInstance{
		openstackWrapper: wrapper,
		InstanceName:     instanceName,
		NodeIndex:        nodeIndex,
		PrivateDNSName:   instanceName,
		Region:           wrapper.OpenStackRegion,
		Zone:             wrapper.OpenStackZone,
	}

	return vm, nil
}

func (wrapper *openstackWrapper) GetAvailableGpuTypes() map[string]string {
	return wrapper.AvailableGPUTypes
}

func (wrapper *openstackWrapper) InstanceExists(name string) (exists bool) {
	if _, err := wrapper.getServer(name); err != nil {
		return false
	}

	return true
}

func (wrapper *openstackWrapper) UUID(name string) (vmuuid string, err error) {
	if server, err := wrapper.getServer(name); err != nil {
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

func (handler *openstackHandler) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
	handler.network.ConfigureNetwork(network)
}

func (handler *openstackHandler) RetrieveNetworkInfos() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

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
		return fmt.Sprintf("openstack://%s", handler.instanceID)
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

	if userData, err = handler.encodeCloudInit(input.CloudInit); err != nil {
		return "", err
	}

	if handler.runningInstance, err = handler.newServer(handler.instanceName, handler.nodeIndex); err != nil {
		return "", err
	}

	if err = handler.runningInstance.Create(input.ControlPlane, input.NodeGroup, handler.instanceType, userData, input.Machine.DiskSize); err != nil {
		return "", err
	}

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

	return handler.runningInstance.PowerOn()
}

func (handler *openstackHandler) InstancePowerOff() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.PowerOff()
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
	return handler.network.registerDNS(handler.dnsClient, handler.instanceName, address)
}

func (handler *openstackHandler) UnregisterDNS(address string) (err error) {
	return handler.network.unregisterDNS(handler.dnsClient, handler.instanceName, address)
}

func (handler *openstackHandler) UUID(name string) (string, error) {
	if handler.runningInstance != nil && handler.runningInstance.InstanceName == name {
		return handler.runningInstance.InstanceID, nil
	}

	if server, err := handler.getServer(name); err != nil {
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
