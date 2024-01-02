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
	Address           string            `json:"address"` // external cluster autoscaler provider address of the form "host:port", "host%zone:port", "[host]:port" or "[host%zone]:port"
	Key               string            `json:"key"`     // path to file containing the tls key
	Cert              string            `json:"cert"`    // path to file containing the tls certificate
	Cacert            string            `json:"cacert"`  // path to file containing the CA certificate
	NodeGroup         string            `json:"nodegroup"`
	Timeout           time.Duration     `json:"timeout"`
	TimeZone          string            `json:"time-zone"`
	TemplateName      string            `json:"template-name"`
	LinkedClone       bool              `json:"linked"`
	Autostart         bool              `json:"autostart"`
	Network           *Network          `json:"network"`
	AvailableGPUTypes map[string]string `json:"gpu-types"`
	AllowUpgrade      bool              `json:"allow-upgrade"`
	TestMode          bool              `json:"test-mode"`
}

type desktopWrapper struct {
	Configuration

	client       api.VMWareDesktopAutoscalerServiceClient
	templateUUID string
}

type desktopHandler struct {
	*desktopWrapper
	network      Network
	instanceName string
	instanceID   string
	nodeIndex    int
}

type CreateInput struct {
	*providers.InstanceCreateInput

	AllowUpgrade bool
	TimeZone     string
	Network      *Network
}

func NewDesktopProviderConfiguration(fileName string) (providers.ProviderConfiguration, error) {
	var wrapper desktopWrapper
	var err error

	if err = providers.LoadConfig(fileName, &wrapper.Configuration); err != nil {
		return nil, err
	} else if wrapper.client, err = api.NewApiClient(wrapper.Address, wrapper.Key, wrapper.Cert, wrapper.Cacert); err != nil {
		return nil, err
	} else if wrapper.templateUUID, err = wrapper.UUID(wrapper.TemplateName); err != nil {
		wrapper.templateUUID = wrapper.TemplateName
		wrapper.TemplateName, err = wrapper.Name(wrapper.templateUUID)
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
	return handler.Timeout
}

func (handler *desktopHandler) NodeGroupName() string {
	return handler.NodeGroup
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

	return handler.desktopWrapper.RetrieveNetworkInfos(handler.instanceID, handler.nodeIndex, &handler.network)
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

func (handler *desktopHandler) InstanceCreate(input *providers.InstanceCreateInput) (string, error) {

	createInput := &CreateInput{
		InstanceCreateInput: input,
		AllowUpgrade:        handler.AllowUpgrade,
		TimeZone:            handler.TimeZone,
		Network:             &handler.network,
	}

	if vmuuid, err := handler.Create(createInput); err != nil {
		return "", err
	} else {
		handler.instanceName = input.NodeName
		handler.nodeIndex = input.NodeIndex
		handler.instanceID = vmuuid

		return vmuuid, err
	}
}

func (handler *desktopHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	if ip, err := handler.WaitForIP(handler.instanceName, handler.Timeout); err != nil {
		return ip, err
	} else {
		if err := context.PollImmediate(time.Second, handler.Timeout*time.Second, func() (bool, error) {
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
	return handler.Exists(name)
}

func (handler *desktopHandler) InstanceAutoStart() error {
	return nil
}

func (handler *desktopHandler) InstancePowerOn() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.PowerOn(handler.instanceID)
}

func (handler *desktopHandler) InstancePowerOff() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.PowerOff(handler.instanceID, "soft")
}

func (handler *desktopHandler) InstanceShutdownGuest() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.ShutdownGuest(handler.instanceID)
}

func (handler *desktopHandler) InstanceDelete() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.Delete(handler.instanceID)
}

func (handler *desktopHandler) InstanceStatus() (providers.InstanceStatus, error) {
	if handler.instanceID == "" {
		return nil, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	if status, err := handler.Status(handler.instanceID); err != nil {
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

	return handler.WaitForPowerState(handler.instanceID, true)
}

func (handler *desktopHandler) InstanceWaitForToolsRunning() (bool, error) {
	if handler.instanceID == "" {
		return false, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return true, handler.WaitForPowerState(handler.instanceID, true)
}

func (handler *desktopHandler) InstanceMaxPods(instanceType string, desiredMaxPods int) (int, error) {
	if desiredMaxPods == 0 {
		desiredMaxPods = 110
	}

	return desiredMaxPods, nil
}

func (handler *desktopHandler) UUID(name string) (string, error) {
	return handler.desktopWrapper.UUID(name)
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

	providers.Copy(&network, wrapper.Network)

	if vmuuid, err := wrapper.UUID(instanceName); err != nil {
		return nil, err
	} else {
		return &desktopHandler{
			desktopWrapper: wrapper,
			network:        network,
			instanceName:   instanceName,
			instanceID:     vmuuid,
			nodeIndex:      nodeIndex,
		}, nil
	}
}

func (wrapper *desktopWrapper) CreateInstance(instanceName string, nodeIndex int) (providers.ProviderHandler, error) {
	if wrapper.InstanceExists(instanceName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, instanceName)
		return nil, fmt.Errorf(constantes.ErrVMAlreadyExists, instanceName)
	}

	var network Network

	providers.Copy(&network, wrapper.Network)

	return &desktopHandler{
		desktopWrapper: wrapper,
		network:        network,
		instanceName:   instanceName,
		nodeIndex:      nodeIndex,
	}, nil
}

func (wrapper *desktopWrapper) InstanceExists(name string) bool {
	return wrapper.Exists(name)
}

func (wrapper *desktopWrapper) NodeGroupName() string {
	return wrapper.NodeGroup
}

func (wrapper *desktopWrapper) GetAvailableGpuTypes() map[string]string {
	return wrapper.AvailableGPUTypes
}

func (wrapper *desktopWrapper) FindVNetWithContext(ctx *context.Context, name string) (*NetworkDevice, error) {
	if response, err := wrapper.client.ListNetwork(ctx, &api.NetworkRequest{}); err != nil {
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

func (wrapper *desktopWrapper) FindVNet(name string) (*NetworkDevice, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.FindVNetWithContext(ctx, name)
}

// CreateWithContext will create a named VM not powered
// memory and disk are in megabytes
// Return vm UUID
func (wrapper *desktopWrapper) CreateWithContext(ctx *context.Context, input *CreateInput) (string, error) {
	var err error

	request := &api.CreateRequest{
		Template:     wrapper.TemplateName,
		Name:         input.NodeName,
		Vcpus:        int32(input.Machine.Vcpu),
		Memory:       int64(input.Machine.Memory),
		DiskSizeInMb: int32(input.Machine.DiskSize),
		Linked:       wrapper.LinkedClone,
		Networks:     BuildNetworkInterface(input.Network.Interfaces, input.NodeIndex),
		Register:     false,
		Autostart:    wrapper.Autostart,
	}

	if request.GuestInfos, err = BuildCloudInit(input.NodeName, input.UserName, input.AuthKey, wrapper.TimeZone, input.CloudInit, input.Network, input.NodeIndex, wrapper.AllowUpgrade); err != nil {
		return "", fmt.Errorf(constantes.ErrCloudInitFailCreation, input.NodeName, err)
	} else if response, err := wrapper.client.Create(ctx, request); err != nil {
		return "", err
	} else if response.GetError() != nil {
		return "", api.NewApiError(response.GetError())
	} else {
		return response.GetResult().Machine.Uuid, nil
	}
}

// Create will create a named VM not powered
// memory and disk are in megabytes
func (wrapper *desktopWrapper) Create(input *CreateInput) (string, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.CreateWithContext(ctx, input)
}

// DeleteWithContext a VM by UUID
func (wrapper *desktopWrapper) DeleteWithContext(ctx *context.Context, vmuuid string) error {
	if response, err := wrapper.client.Delete(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

// Delete a VM by vmuuid
func (wrapper *desktopWrapper) Delete(vmuuid string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.DeleteWithContext(ctx, vmuuid)
}

// VirtualMachineWithContext  Retrieve VM by name
func (wrapper *desktopWrapper) VirtualMachineByNameWithContext(ctx *context.Context, name string) (*VirtualMachine, error) {
	if response, err := wrapper.client.VirtualMachineByName(ctx, &api.VirtualMachineRequest{Identifier: name}); err != nil {
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
func (wrapper *desktopWrapper) VirtualMachineByName(name string) (*VirtualMachine, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.VirtualMachineByNameWithContext(ctx, name)
}

// VirtualMachineWithContext  Retrieve VM by vmuuid
func (wrapper *desktopWrapper) VirtualMachineByUUIDWithContext(ctx *context.Context, vmuuid string) (*VirtualMachine, error) {
	if response, err := wrapper.client.VirtualMachineByUUID(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
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
func (wrapper *desktopWrapper) VirtualMachineByUUID(vmuuid string) (*VirtualMachine, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.VirtualMachineByUUIDWithContext(ctx, vmuuid)
}

// VirtualMachineListWithContext return all VM for the current datastore
func (wrapper *desktopWrapper) VirtualMachineListWithContext(ctx *context.Context) ([]*VirtualMachine, error) {
	if response, err := wrapper.client.ListVirtualMachines(ctx, &api.VirtualMachinesRequest{}); err != nil {
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
func (wrapper *desktopWrapper) VirtualMachineList() ([]*VirtualMachine, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.VirtualMachineListWithContext(ctx)
}

// UUID get VM UUID by name
func (wrapper *desktopWrapper) UUIDWithContext(ctx *context.Context, name string) (string, error) {
	if vm, err := wrapper.VirtualMachineByNameWithContext(ctx, name); err != nil {
		return "", err
	} else {
		return vm.Uuid, nil
	}
}

// UUID get VM UUID by name
func (wrapper *desktopWrapper) UUID(name string) (string, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.UUIDWithContext(ctx, name)
}

// Name get VM Name by uuid
func (wrapper *desktopWrapper) NameWithContext(ctx *context.Context, name string) (string, error) {
	if vm, err := wrapper.VirtualMachineByUUIDWithContext(ctx, name); err != nil {
		return "", err
	} else {
		return vm.Name, nil
	}
}

func (wrapper *desktopWrapper) Name(vmuuid string) (string, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.NameWithContext(ctx, vmuuid)
}

// WaitForIPWithContext wait ip a VM by vmuuid
func (wrapper *desktopWrapper) WaitForIPWithContext(ctx *context.Context, vmuuid string, timeout time.Duration) (string, error) {
	if response, err := wrapper.client.WaitForIP(ctx, &api.WaitForIPRequest{Identifier: vmuuid, TimeoutInSeconds: int32(timeout / time.Second)}); err != nil {
		return "", err
	} else if response.GetError() != nil {
		return "", api.NewApiError(response.GetError())
	} else {
		return response.GetResult().GetAddress(), nil
	}
}

// WaitForIP wait ip a VM by vmuuid
func (wrapper *desktopWrapper) WaitForIP(vmuuid string, timeout time.Duration) (string, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.WaitForIPWithContext(ctx, vmuuid, timeout)
}

// SetAutoStartWithContext set autostart for the VM
func (wrapper *desktopWrapper) SetAutoStartWithContext(ctx *context.Context, vmuuid string, autostart bool) error {
	if response, err := wrapper.client.SetAutoStart(ctx, &api.AutoStartRequest{Uuid: vmuuid, Autostart: autostart}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

// SetAutoStart set autostart for the VM
func (wrapper *desktopWrapper) SetAutoStart(vmuuid string, autostart bool) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.SetAutoStartWithContext(ctx, vmuuid, autostart)
}

// WaitForToolsRunningWithContext wait vmware tools is running a VM by vmuuid
func (wrapper *desktopWrapper) WaitForToolsRunningWithContext(ctx *context.Context, vmuuid string, timeout time.Duration) (bool, error) {
	if response, err := wrapper.client.WaitForToolsRunning(ctx, &api.WaitForToolsRunningRequest{Identifier: vmuuid, TimeoutInSeconds: int32(timeout / time.Second)}); err != nil {
		return false, err
	} else if response.GetError() != nil {
		return false, api.NewApiError(response.GetError())
	} else {
		return response.GetResult().GetRunning(), nil
	}
}

// WaitForToolsRunning wait vmware tools is running a VM by vmuuid
func (wrapper *desktopWrapper) WaitForToolsRunning(vmuuid string, timeout time.Duration) (bool, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.WaitForToolsRunningWithContext(ctx, vmuuid, timeout)
}

// PowerOnWithContext power on a VM by vmuuid
func (wrapper *desktopWrapper) PowerOnWithContext(ctx *context.Context, vmuuid string) error {
	if response, err := wrapper.client.PowerOn(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

// PowerOn power on a VM by vmuuid
func (wrapper *desktopWrapper) PowerOn(vmuuid string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.PowerOnWithContext(ctx, vmuuid)
}

// PowerOffWithContext power off a VM by vmuuid
func (wrapper *desktopWrapper) PowerOffWithContext(ctx *context.Context, vmuuid, mode string) error {
	if response, err := wrapper.client.PowerOff(ctx, &api.PowerOffRequest{Identifier: vmuuid, Mode: mode}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

func (wrapper *desktopWrapper) WaitForPowerStateWithContenxt(ctx *context.Context, vmuuid string, wanted bool) error {
	return context.PollImmediate(time.Second, wrapper.Timeout, func() (bool, error) {
		if response, err := wrapper.client.PowerState(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
			return false, err
		} else if response.GetError() != nil {
			return false, api.NewApiError(response.GetError())
		} else {
			return response.GetPowered() == wanted, nil
		}
	})
}

func (wrapper *desktopWrapper) WaitForPowerState(vmuuid string, wanted bool) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.WaitForPowerStateWithContenxt(ctx, vmuuid, wanted)
}

// PowerOff power off a VM by name
func (wrapper *desktopWrapper) PowerOff(vmuuid, mode string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.PowerOffWithContext(ctx, vmuuid, mode)
}

// ShutdownGuestWithContext power off a VM by vmuuid
func (wrapper *desktopWrapper) ShutdownGuestWithContext(ctx *context.Context, vmuuid string) error {
	if response, err := wrapper.client.ShutdownGuest(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

// ShutdownGuest power off a VM by vmuuid
func (wrapper *desktopWrapper) ShutdownGuest(vmuuid string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.ShutdownGuestWithContext(ctx, vmuuid)
}

// StatusWithContext return the current status of VM by vmuuid
func (wrapper *desktopWrapper) StatusWithContext(ctx *context.Context, vmuuid string) (*Status, error) {
	if response, err := wrapper.client.Status(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
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
func (wrapper *desktopWrapper) Status(vmuuid string) (*Status, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.StatusWithContext(ctx, vmuuid)
}

func (wrapper *desktopWrapper) RetrieveNetworkInfosWithContext(ctx *context.Context, vmuuid string, nodeIndex int, network *Network) error {
	if response, err := wrapper.client.Status(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
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

func (wrapper *desktopWrapper) RetrieveNetworkInfos(vmuuid string, nodeIndex int, network *Network) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.RetrieveNetworkInfosWithContext(ctx, vmuuid, nodeIndex, network)
}

// ExistsWithContext return the current status of VM by name
func (wrapper *desktopWrapper) ExistsWithContext(ctx *context.Context, name string) bool {
	if _, err := wrapper.VirtualMachineByNameWithContext(ctx, name); err == nil {
		return true
	}

	return false
}

func (wrapper *desktopWrapper) Exists(name string) bool {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.ExistsWithContext(ctx, name)
}
