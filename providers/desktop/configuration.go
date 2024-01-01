package desktop

import (
	"fmt"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/desktop/api"
	glog "github.com/sirupsen/logrus"
)

// Configuration declares desktop connection info
type VMWareDesktopConfiguration struct {
	NodeGroup    string        `json:"nodegroup"`
	Timeout      time.Duration `json:"timeout"`
	TimeZone     string        `json:"time-zone"`
	TemplateName string        `json:"template-name"`
	LinkedClone  bool          `json:"linked"`
	Autostart    bool          `json:"autostart"`
	Network      *Network      `json:"network"`
	AllowUpgrade bool          `json:"allow-upgrade"`
	TestMode     bool          `json:"test-mode"`
}

type Configuration struct {
	DesktopConfig *VMWareDesktopConfiguration `json:"config"`
	ApiConfig     *api.Configuration          `json:"api"`
	templateUUID  string
}

type desktopHandler struct {
	config       *Configuration
	network      Network
	instanceName string
	instanceID   string
	nodeIndex    int
}

type desktopWrapper struct {
	config Configuration
}

func NewDesktopProviderConfiguration(fileName string) (providers.ProviderConfiguration, error) {
	var wrapper desktopWrapper
	var err error

	if err = providers.LoadConfig(fileName, &wrapper.config); err != nil {
		return nil, err
	} else if wrapper.config.templateUUID, err = wrapper.config.UUID(wrapper.config.DesktopConfig.TemplateName); err != nil {
		wrapper.config.templateUUID = wrapper.config.DesktopConfig.TemplateName
		wrapper.config.DesktopConfig.TemplateName, err = wrapper.config.Name(wrapper.config.templateUUID)
	}

	return &wrapper, err
}

type VmStatus struct {
	Status
	address string
}

func (status *VmStatus) Address() string {
	return status.address
}

func (status *VmStatus) Powered() bool {
	return status.Status.Powered
}

func (handler *desktopHandler) GetTimeout() time.Duration {
	return handler.config.DesktopConfig.Timeout
}

func (handler *desktopHandler) NodeGroupName() string {
	return handler.config.DesktopConfig.NodeGroup
}

func (handler *desktopHandler) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
	if len(network.VMWare) > 0 {
		for _, network := range network.VMWare {
			if inf := handler.findVMNet(network.NetworkName); inf != nil {
				inf.DHCP = network.DHCP
				inf.UseRoutes = network.UseRoutes
				inf.Routes = network.Routes

				if len(network.IPV4Address) > 0 {
					inf.IPAddress = network.IPV4Address
				}

				if len(network.Netmask) > 0 {
					inf.Netmask = network.Netmask
				}

				if len(network.Gateway) > 0 {
					inf.Gateway = network.Gateway
				}

				if len(network.MacAddress) > 0 {
					inf.MacAddress = network.MacAddress
				}
			}
		}
	}
}

func (handler *desktopHandler) RetrieveNetworkInfos() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.RetrieveNetworkInfos(handler.instanceID, handler.nodeIndex, &handler.network)
}

func (handler *desktopHandler) UpdateMacAddressTable() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.network.UpdateMacAddressTable(handler.nodeIndex)
}

func (handler *desktopHandler) GenerateProviderID() string {
	return fmt.Sprintf("desktop://%s", handler.instanceID)
}

func (handler *desktopHandler) GetTopologyLabels() map[string]string {
	return map[string]string{}
}

func (handler *desktopHandler) InstanceCreate(nodeName string, nodeIndex int, instanceType, userName, authKey string, cloudInit interface{}, machine *providers.MachineCharacteristic) (string, error) {
	if vmuuid, err := handler.config.Create(nodeName, nodeIndex, userName, authKey, cloudInit, &handler.network, machine); err != nil {
		return "", err
	} else {
		handler.instanceName = nodeName
		handler.instanceID = vmuuid

		return vmuuid, err
	}
}

func (handler *desktopHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	if ip, err := handler.config.WaitForIP(handler.instanceName, handler.config.DesktopConfig.Timeout); err != nil {
		return ip, err
	} else {
		if err := context.PollImmediate(time.Second, handler.config.DesktopConfig.Timeout*time.Second, func() (bool, error) {
			var err error

			if err = callback.WaitSSHReady(handler.instanceName, ip); err != nil {
				return false, err
			}

			return true, nil
		}); err != nil {
			return ip, err
		}

		return ip, nil
	}
}

func (handler *desktopHandler) InstanceID() (string, error) {
	if handler.instanceID == "" {
		return "", fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.instanceID, nil
}

func (handler *desktopHandler) InstanceExists(name string) bool {
	return handler.config.Exists(name)
}

func (handler *desktopHandler) InstanceAutoStart() error {
	return nil
}

func (handler *desktopHandler) InstancePowerOn() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.PowerOn(handler.instanceID)
}

func (handler *desktopHandler) InstancePowerOff() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.PowerOff(handler.instanceID, "soft")
}

func (handler *desktopHandler) InstanceShutdownGuest() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.ShutdownGuest(handler.instanceID)
}

func (handler *desktopHandler) InstanceDelete() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.Delete(handler.instanceID)
}

func (handler *desktopHandler) InstanceStatus() (providers.InstanceStatus, error) {
	if handler.instanceID == "" {
		return nil, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	if status, err := handler.config.Status(handler.instanceID); err != nil {
		return nil, err
	} else {
		return &VmStatus{
			Status:  *status,
			address: handler.findPreferredIPAddress(status.Ethernet),
		}, nil
	}
}

func (handler *desktopHandler) InstanceWaitForPowered() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.WaitForPowerState(handler.instanceID, true)
}

func (handler *desktopHandler) InstanceWaitForToolsRunning() (bool, error) {
	if handler.instanceID == "" {
		return false, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return true, handler.config.WaitForPowerState(handler.instanceID, true)
}

func (handler *desktopHandler) InstanceMaxPods(instanceType string, desiredMaxPods int) (int, error) {
	if desiredMaxPods == 0 {
		desiredMaxPods = 110
	}

	return desiredMaxPods, nil
}

func (handler *desktopHandler) UUID(name string) (string, error) {
	return handler.config.UUID(name)
}

func (handler *desktopHandler) RegisterDNS(address string) error {
	return nil
}

func (handler *desktopHandler) UnregisterDNS(address string) error {
	return nil
}

func (handler *desktopHandler) findPreferredIPAddress(devices []VNetDevice) string {
	address := ""

	for _, ether := range devices {
		if declaredInf := handler.findInterface(&ether); declaredInf != nil {
			if declaredInf.Primary {
				return ether.Address
			}
		}
	}

	return address
}

func (handler *desktopHandler) findVMNet(name string) *NetworkInterface {
	for _, inf := range handler.network.Interfaces {
		if name == inf.VNet {
			return inf
		}
	}

	return nil
}

func (handler *desktopHandler) findInterface(ether *VNetDevice) *NetworkInterface {
	for _, inf := range handler.network.Interfaces {
		if inf.Same(ether.ConnectionType, ether.VNet) {
			return inf
		}
	}

	return nil
}

func (wrapper *desktopWrapper) AttachInstance(instanceName string, nodeIndex int) (providers.ProviderHandler, error) {
	var network Network

	providers.Copy(&network, wrapper.config.DesktopConfig.Network)

	if vmuuid, err := wrapper.config.UUID(instanceName); err != nil {
		return nil, err
	} else {
		return &desktopHandler{
			config:       &wrapper.config,
			network:      network,
			instanceName: instanceName,
			instanceID:   vmuuid,
			nodeIndex:    nodeIndex,
		}, nil
	}
}

func (wrapper *desktopWrapper) CreateInstance(instanceName string, nodeIndex int) (providers.ProviderHandler, error) {
	if wrapper.InstanceExists(instanceName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, instanceName)
		return nil, fmt.Errorf(constantes.ErrVMAlreadyExists, instanceName)
	}

	var network Network

	providers.Copy(&network, wrapper.config.DesktopConfig.Network)

	return &desktopHandler{
		config:       &wrapper.config,
		network:      network,
		instanceName: instanceName,
		nodeIndex:    nodeIndex,
	}, nil
}

func (wrapper *desktopWrapper) InstanceExists(name string) bool {
	return wrapper.config.Exists(name)
}

func (wrapper *desktopWrapper) NodeGroupName() string {
	return wrapper.config.DesktopConfig.NodeGroup
}

func (handler *desktopWrapper) GetAvailableGpuTypes() map[string]string {
	return map[string]string{}
}

func (config *Configuration) FindVNetWithContext(ctx *context.Context, name string) (*NetworkDevice, error) {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return nil, err
	} else if response, err := client.ListNetwork(ctx, &api.NetworkRequest{}); err != nil {
		return nil, err
	} else if response.GetError() != nil {
		return nil, api.NewApiError(response.GetError())
	} else {
		vmnets := response.GetResult().Vmnets

		for _, vmnet := range vmnets {
			if vmnet.Name == name {
				return &NetworkDevice{
					Name:   vmnet.Name,
					Type:   vmnet.Type,
					Dhcp:   vmnet.Dhcp,
					Subnet: vmnet.Subnet,
					Mask:   vmnet.Mask,
				}, nil
			}
		}

		return nil, nil
	}
}

func (config *Configuration) FindVNet(name string) (*NetworkDevice, error) {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.FindVNetWithContext(ctx, name)
}

// CreateWithContext will create a named VM not powered
// memory and disk are in megabytes
// Return vm UUID
func (config *Configuration) CreateWithContext(ctx *context.Context, name string, nodeIndex int, userName, authKey string, cloudInit interface{}, network *Network, machine *providers.MachineCharacteristic) (string, error) {
	var err error

	request := &api.CreateRequest{
		Template:     config.DesktopConfig.TemplateName,
		Name:         name,
		Vcpus:        int32(machine.Vcpu),
		Memory:       int64(machine.Memory),
		DiskSizeInMb: int32(machine.DiskSize),
		Linked:       config.DesktopConfig.LinkedClone,
		Networks:     BuildNetworkInterface(network.Interfaces, nodeIndex),
		Register:     false,
		Autostart:    config.DesktopConfig.Autostart,
	}

	if request.GuestInfos, err = BuildCloudInit(name, userName, authKey, config.DesktopConfig.TimeZone, cloudInit, network, nodeIndex, config.DesktopConfig.AllowUpgrade); err != nil {
		return "", fmt.Errorf(constantes.ErrCloudInitFailCreation, name, err)
	} else if client, err := config.ApiConfig.GetClient(); err != nil {
		return "", err
	} else if response, err := client.Create(ctx, request); err != nil {
		return "", err
	} else if response.GetError() != nil {
		return "", api.NewApiError(response.GetError())
	} else {
		return response.GetResult().Machine.Uuid, nil
	}
}

// Create will create a named VM not powered
// memory and disk are in megabytes
func (config *Configuration) Create(name string, nodeIndex int, userName, authKey string, cloudInit interface{}, network *Network, machine *providers.MachineCharacteristic) (string, error) {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.CreateWithContext(ctx, name, nodeIndex, userName, authKey, cloudInit, network, machine)
}

// DeleteWithContext a VM by UUID
func (config *Configuration) DeleteWithContext(ctx *context.Context, vmuuid string) error {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return err
	} else if response, err := client.Delete(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

// Delete a VM by vmuuid
func (config *Configuration) Delete(vmuuid string) error {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.DeleteWithContext(ctx, vmuuid)
}

// VirtualMachineWithContext  Retrieve VM by name
func (config *Configuration) VirtualMachineByNameWithContext(ctx *context.Context, name string) (*VirtualMachine, error) {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return nil, err
	} else if response, err := client.VirtualMachineByName(ctx, &api.VirtualMachineRequest{Identifier: name}); err != nil {
		return nil, err
	} else if response.GetError() != nil {
		return nil, api.NewApiError(response.GetError())
	} else {
		vm := response.GetResult()

		return &VirtualMachine{
			Name:   vm.GetName(),
			Uuid:   vm.GetUuid(),
			Vmx:    vm.GetVmx(),
			Vcpus:  vm.GetVcpus(),
			Memory: vm.GetMemory(),
		}, nil
	}
}

// VirtualMachine  Retrieve VM by vmuuid
func (config *Configuration) VirtualMachineByName(name string) (*VirtualMachine, error) {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.VirtualMachineByNameWithContext(ctx, name)
}

// VirtualMachineWithContext  Retrieve VM by vmuuid
func (config *Configuration) VirtualMachineByUUIDWithContext(ctx *context.Context, vmuuid string) (*VirtualMachine, error) {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return nil, err
	} else if response, err := client.VirtualMachineByUUID(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
		return nil, err
	} else if response.GetError() != nil {
		return nil, api.NewApiError(response.GetError())
	} else {
		vm := response.GetResult()

		return &VirtualMachine{
			Name:   vm.GetName(),
			Uuid:   vm.GetUuid(),
			Vmx:    vm.GetVmx(),
			Vcpus:  vm.GetVcpus(),
			Memory: vm.GetMemory(),
		}, nil
	}
}

// VirtualMachine  Retrieve VM by vmuuid
func (config *Configuration) VirtualMachineByUUID(vmuuid string) (*VirtualMachine, error) {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.VirtualMachineByUUIDWithContext(ctx, vmuuid)
}

// VirtualMachineListWithContext return all VM for the current datastore
func (config *Configuration) VirtualMachineListWithContext(ctx *context.Context) ([]*VirtualMachine, error) {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return nil, err
	} else if response, err := client.ListVirtualMachines(ctx, &api.VirtualMachinesRequest{}); err != nil {
		return nil, err
	} else if response.GetError() != nil {
		return nil, api.NewApiError(response.GetError())
	} else {
		vms := response.GetResult()
		result := make([]*VirtualMachine, 0, len(vms.Machines))

		for _, vm := range vms.Machines {
			result = append(result, &VirtualMachine{
				Name:   vm.GetName(),
				Uuid:   vm.GetUuid(),
				Vmx:    vm.GetVmx(),
				Vcpus:  vm.GetVcpus(),
				Memory: vm.GetMemory(),
			})
		}

		return result, nil
	}
}

// VirtualMachineList return all VM for the current datastore
func (config *Configuration) VirtualMachineList() ([]*VirtualMachine, error) {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.VirtualMachineListWithContext(ctx)
}

// UUID get VM UUID by name
func (config *Configuration) UUIDWithContext(ctx *context.Context, name string) (string, error) {
	if vm, err := config.VirtualMachineByNameWithContext(ctx, name); err != nil {
		return "", err
	} else {
		return vm.Uuid, nil
	}
}

// UUID get VM UUID by name
func (config *Configuration) UUID(name string) (string, error) {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.UUIDWithContext(ctx, name)
}

// Name get VM Name by uuid
func (config *Configuration) NameWithContext(ctx *context.Context, name string) (string, error) {
	if vm, err := config.VirtualMachineByUUIDWithContext(ctx, name); err != nil {
		return "", err
	} else {
		return vm.Name, nil
	}
}

func (config *Configuration) Name(vmuuid string) (string, error) {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.NameWithContext(ctx, vmuuid)
}

// WaitForIPWithContext wait ip a VM by vmuuid
func (config *Configuration) WaitForIPWithContext(ctx *context.Context, vmuuid string, timeout time.Duration) (string, error) {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return "", err
	} else if response, err := client.WaitForIP(ctx, &api.WaitForIPRequest{Identifier: vmuuid, TimeoutInSeconds: int32(timeout / time.Second)}); err != nil {
		return "", err
	} else if response.GetError() != nil {
		return "", api.NewApiError(response.GetError())
	} else {
		return response.GetResult().GetAddress(), nil
	}
}

// WaitForIP wait ip a VM by vmuuid
func (config *Configuration) WaitForIP(vmuuid string, timeout time.Duration) (string, error) {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.WaitForIPWithContext(ctx, vmuuid, timeout)
}

// SetAutoStartWithContext set autostart for the VM
func (config *Configuration) SetAutoStartWithContext(ctx *context.Context, vmuuid string, autostart bool) error {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return err
	} else if response, err := client.SetAutoStart(ctx, &api.AutoStartRequest{Uuid: vmuuid, Autostart: autostart}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

// SetAutoStart set autostart for the VM
func (config *Configuration) SetAutoStart(vmuuid string, autostart bool) error {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.SetAutoStartWithContext(ctx, vmuuid, autostart)
}

// WaitForToolsRunningWithContext wait vmware tools is running a VM by vmuuid
func (config *Configuration) WaitForToolsRunningWithContext(ctx *context.Context, vmuuid string, timeout time.Duration) (bool, error) {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return false, err
	} else if response, err := client.WaitForToolsRunning(ctx, &api.WaitForToolsRunningRequest{Identifier: vmuuid, TimeoutInSeconds: int32(timeout / time.Second)}); err != nil {
		return false, err
	} else if response.GetError() != nil {
		return false, api.NewApiError(response.GetError())
	} else {
		return response.GetResult().GetRunning(), nil
	}
}

// WaitForToolsRunning wait vmware tools is running a VM by vmuuid
func (config *Configuration) WaitForToolsRunning(vmuuid string, timeout time.Duration) (bool, error) {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.WaitForToolsRunningWithContext(ctx, vmuuid, timeout)
}

// PowerOnWithContext power on a VM by vmuuid
func (config *Configuration) PowerOnWithContext(ctx *context.Context, vmuuid string) error {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return err
	} else if response, err := client.PowerOn(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

// PowerOn power on a VM by vmuuid
func (config *Configuration) PowerOn(vmuuid string) error {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.PowerOnWithContext(ctx, vmuuid)
}

// PowerOffWithContext power off a VM by vmuuid
func (config *Configuration) PowerOffWithContext(ctx *context.Context, vmuuid, mode string) error {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return err
	} else if response, err := client.PowerOff(ctx, &api.PowerOffRequest{Identifier: vmuuid, Mode: mode}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

func (config *Configuration) WaitForPowerStateWithContenxt(ctx *context.Context, vmuuid string, wanted bool) error {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return err
	} else {
		return context.PollImmediate(time.Second, config.DesktopConfig.Timeout, func() (bool, error) {
			if response, err := client.PowerState(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
				return false, err
			} else if response.GetError() != nil {
				return false, api.NewApiError(response.GetError())
			} else {
				return response.GetPowered() == wanted, nil
			}
		})
	}
}

func (config *Configuration) WaitForPowerState(vmuuid string, wanted bool) error {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.WaitForPowerStateWithContenxt(ctx, vmuuid, wanted)
}

// PowerOff power off a VM by name
func (config *Configuration) PowerOff(vmuuid, mode string) error {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.PowerOffWithContext(ctx, vmuuid, mode)
}

// ShutdownGuestWithContext power off a VM by vmuuid
func (config *Configuration) ShutdownGuestWithContext(ctx *context.Context, vmuuid string) error {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return err
	} else if response, err := client.ShutdownGuest(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

// ShutdownGuest power off a VM by vmuuid
func (config *Configuration) ShutdownGuest(vmuuid string) error {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.ShutdownGuestWithContext(ctx, vmuuid)
}

// StatusWithContext return the current status of VM by vmuuid
func (config *Configuration) StatusWithContext(ctx *context.Context, vmuuid string) (*Status, error) {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return nil, err
	} else if response, err := client.Status(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
		return nil, err
	} else if response.GetError() != nil {
		return nil, api.NewApiError(response.GetError())
	} else {
		ethernet := make([]VNetDevice, 0, len(response.GetResult().GetEthernet()))

		for _, ether := range response.GetResult().GetEthernet() {
			ethernet = append(ethernet, VNetDevice{
				AddressType:            ether.AddressType,
				BsdName:                ether.BsdName,
				ConnectionType:         ether.ConnectionType,
				DisplayName:            ether.DisplayName,
				GeneratedAddress:       ether.GeneratedAddress,
				GeneratedAddressOffset: ether.GeneratedAddressOffset,
				Address:                ether.Address,
				LinkStatePropagation:   ether.LinkStatePropagation,
				PciSlotNumber:          ether.PciSlotNumber,
				Present:                ether.Present,
				VirtualDevice:          ether.VirtualDev,
				VNet:                   ether.Vnet,
			})
		}

		return &Status{
			Powered:  response.GetResult().GetPowered(),
			Ethernet: ethernet,
		}, nil
	}
}

// Status return the current status of VM by vmuuid
func (config *Configuration) Status(vmuuid string) (*Status, error) {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.StatusWithContext(ctx, vmuuid)
}

func (config *Configuration) RetrieveNetworkInfosWithContext(ctx *context.Context, vmuuid string, nodeIndex int, network *Network) error {
	if client, err := config.ApiConfig.GetClient(); err != nil {
		return err
	} else if response, err := client.Status(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		for _, ether := range response.GetResult().GetEthernet() {
			for _, inf := range network.Interfaces {
				if (inf.VNet == ether.Vnet) || (inf.ConnectionType == ether.ConnectionType && inf.ConnectionType != "custom") {
					inf.AttachMacAddress(ether.GeneratedAddress, nodeIndex)
				}
			}
		}

		return nil
	}
}

func (config *Configuration) RetrieveNetworkInfos(vmuuid string, nodeIndex int, network *Network) error {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.RetrieveNetworkInfosWithContext(ctx, vmuuid, nodeIndex, network)
}

// ExistsWithContext return the current status of VM by name
func (config *Configuration) ExistsWithContext(ctx *context.Context, name string) bool {
	if _, err := config.VirtualMachineByNameWithContext(ctx, name); err == nil {
		return true
	}

	return false
}

func (config *Configuration) Exists(name string) bool {
	ctx := context.NewContext(config.DesktopConfig.Timeout)
	defer ctx.Cancel()

	return config.ExistsWithContext(ctx, name)
}
