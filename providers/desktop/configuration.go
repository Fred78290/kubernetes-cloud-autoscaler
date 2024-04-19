package desktop

import (
	"fmt"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/api"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha2"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"

	glog "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/uuid"
)

// Configuration declares desktop connection info
type Configuration struct {
	Address           string             `json:"address"` // external cluster autoscaler provider address of the form "host:port", "host%zone:port", "[host]:port" or "[host%zone]:port"
	Key               string             `json:"key"`     // path to file containing the tls key
	Cert              string             `json:"cert"`    // path to file containing the tls certificate
	Cacert            string             `json:"cacert"`  // path to file containing the CA certificate
	Timeout           time.Duration      `json:"timeout"`
	TimeZone          string             `default:"UTC" json:"time-zone"`
	TemplateName      string             `json:"template-name"`
	LinkedClone       bool               `json:"linked"`
	Autostart         bool               `json:"autostart"`
	Network           *providers.Network `json:"network"`
	AvailableGPUTypes map[string]string  `json:"gpu-types"`
	VMWareRegion      string             `default:"home" json:"region"`
	VMWareZone        string             `default:"office" json:"zone"`
}

type desktopWrapper struct {
	Configuration

	client       api.DesktopAutoscalerServiceClient
	templateUUID string
	testMode     bool
}

type desktopHandler struct {
	*desktopWrapper
	network      *providers.Network
	instanceType string
	instanceName string
	instanceID   string
	controlPlane bool
	nodeIndex    int
}

type CreateInput struct {
	*providers.InstanceCreateInput
	NodeName string
	TimeZone string
	Network  *providers.Network
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

	wrapper.Configuration.Network.ConfigurationDidLoad()

	return &wrapper, err
}

type vmStatus struct {
	Status
	address string
}

func (status *vmStatus) Address() string {
	return status.address
}

func (status *vmStatus) Powered() bool {
	return status.Status.Powered
}

func (handler *desktopHandler) GetTimeout() time.Duration {
	return handler.Timeout
}

func (handler *desktopHandler) ConfigureNetwork(network v1alpha2.ManagedNetworkConfig) {
	handler.network.ConfigureVMWareNetwork(network.VMWare)
}

func (handler *desktopHandler) RetrieveNetworkInfos() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.desktopWrapper.RetrieveNetworkInfos(handler.instanceID, handler.network)
}

func (handler *desktopHandler) UpdateMacAddressTable() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.network.UpdateMacAddressTable()
}

func (handler *desktopHandler) GenerateProviderID() string {
	if len(handler.instanceID) > 0 {
		return fmt.Sprintf("desktop://%s", handler.instanceID)
	} else {
		return ""
	}
}

func (handler *desktopHandler) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion:  handler.VMWareRegion,
		constantes.NodeLabelTopologyZone:    handler.VMWareZone,
		constantes.NodeLabelVMWareCSIRegion: handler.VMWareRegion,
		constantes.NodeLabelVMWareCSIZone:   handler.VMWareZone,
	}
}

func (handler *desktopHandler) InstanceCreate(input *providers.InstanceCreateInput) (string, error) {
	createInput := &CreateInput{
		InstanceCreateInput: input,
		NodeName:            handler.instanceName,
		TimeZone:            handler.TimeZone,
		Network:             handler.network,
	}

	if vmuuid, err := handler.Create(createInput); err != nil {
		return "", err
	} else {
		handler.instanceID = vmuuid

		return vmuuid, err
	}
}

func (handler *desktopHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	if handler.instanceID == "" {
		return "", fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	if ip, err := handler.WaitForIP(handler.instanceID, handler.Timeout); err != nil {
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

func (handler *desktopHandler) InstancePrimaryAddressIP() string {
	return handler.network.PrimaryAddressIP()
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
		return &vmStatus{
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

func (handler *desktopHandler) InstanceMaxPods(desiredMaxPods int) (int, error) {
	if desiredMaxPods == 0 {
		desiredMaxPods = 110
	}

	return desiredMaxPods, nil
}

func (handler *desktopHandler) UUID(name string) (string, error) {
	return handler.desktopWrapper.UUID(name)
}

func (handler *desktopHandler) PrivateDNSName() (string, error) {
	return handler.instanceName, nil
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

func (handler *desktopHandler) findInterface(ether *VNetDevice) *providers.NetworkInterface {
	for _, inf := range handler.network.Interfaces {
		if ether.Same(inf) {
			return inf
		}
	}

	return nil
}

func (wrapper *desktopWrapper) SetMode(test bool) {
	wrapper.testMode = test
}

func (wrapper *desktopWrapper) GetMode() bool {
	return wrapper.testMode
}

func (wrapper *desktopWrapper) AttachInstance(instanceName string, controlPlane bool, nodeIndex int) (providers.ProviderHandler, error) {
	if vmuuid, err := wrapper.UUID(instanceName); err != nil {
		return nil, err
	} else {
		return &desktopHandler{
			desktopWrapper: wrapper,
			network:        wrapper.Network.Clone(controlPlane, nodeIndex),
			instanceName:   instanceName,
			instanceID:     vmuuid,
			controlPlane:   controlPlane,
			nodeIndex:      nodeIndex,
		}, nil
	}
}

func (wrapper *desktopWrapper) CreateInstance(instanceName, instanceType string, controlPlane bool, nodeIndex int) (providers.ProviderHandler, error) {
	if wrapper.InstanceExists(instanceName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, instanceName)
		return nil, fmt.Errorf(constantes.ErrVMAlreadyExists, instanceName)
	}

	return &desktopHandler{
		desktopWrapper: wrapper,
		network:        wrapper.Network.Clone(controlPlane, nodeIndex),
		instanceType:   instanceType,
		instanceName:   instanceName,
		controlPlane:   controlPlane,
		nodeIndex:      nodeIndex,
	}, nil
}

func (wrapper *desktopWrapper) InstanceExists(name string) bool {
	return wrapper.Exists(name)
}

func (wrapper *desktopWrapper) GetAvailableGpuTypes() map[string]string {
	return wrapper.AvailableGPUTypes
}

func (wrapper *desktopWrapper) FindVNetWithContext(ctx *context.Context, name string) (*NetworkDevice, error) {
	if response, err := wrapper.client.VMWareListNetwork(ctx, &api.NetworkRequest{}); err != nil {
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
		Template:     wrapper.templateUUID,
		Name:         input.NodeName,
		Vcpus:        int32(input.Machine.Vcpu),
		Memory:       int64(input.Machine.Memory),
		DiskSizeInMb: int32(input.Machine.GetDiskSize()),
		Linked:       wrapper.LinkedClone,
		Networks:     BuildNetworkInterface(input.Network),
		Register:     false,
		Autostart:    wrapper.Autostart,
	}

	cloundInitInput := cloudinit.CloudInitInput{
		InstanceName: input.NodeName,
		InstanceID:   string(uuid.NewUUID()),
		UserName:     input.UserName,
		AuthKey:      input.AuthKey,
		DomainName:   input.Network.Domain,
		CloudInit:    input.CloudInit,
		AllowUpgrade: input.AllowUpgrade,
		TimeZone:     wrapper.TimeZone,
		Network:      nil,
	}

	if input.Network != nil && len(input.Network.Interfaces) > 0 {
		cloundInitInput.Network = input.Network.GetCloudInitNetwork(true)
	}

	if request.GuestInfos, err = cloundInitInput.BuildGuestInfos(); err != nil {
		return "", fmt.Errorf(constantes.ErrCloudInitFailCreation, input.NodeName, err)
	} else if response, err := wrapper.client.VMWareCreate(ctx, request); err != nil {
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
	if response, err := wrapper.client.VMWareDelete(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
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
	if response, err := wrapper.client.VMWareVirtualMachineByName(ctx, &api.VirtualMachineRequest{Identifier: name}); err != nil {
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
	if response, err := wrapper.client.VMWareVirtualMachineByUUID(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
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
	if response, err := wrapper.client.VMWareListVirtualMachines(ctx, &api.VirtualMachinesRequest{}); err != nil {
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
	if response, err := wrapper.client.VMWareWaitForIP(ctx, &api.WaitForIPRequest{Identifier: vmuuid, TimeoutInSeconds: int32(timeout / time.Second)}); err != nil {
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
	if response, err := wrapper.client.VMWareSetAutoStart(ctx, &api.AutoStartRequest{Uuid: vmuuid, Autostart: autostart}); err != nil {
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
	if response, err := wrapper.client.VMWareWaitForToolsRunning(ctx, &api.WaitForToolsRunningRequest{Identifier: vmuuid, TimeoutInSeconds: int32(timeout / time.Second)}); err != nil {
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
	if response, err := wrapper.client.VMWarePowerOn(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
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
	if response, err := wrapper.client.VMWarePowerOff(ctx, &api.PowerOffRequest{Identifier: vmuuid, Mode: mode}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

func (wrapper *desktopWrapper) WaitForPowerStateWithContenxt(ctx *context.Context, vmuuid string, wanted bool) error {
	return context.PollImmediate(time.Second, wrapper.Timeout, func() (bool, error) {
		if response, err := wrapper.client.VMWarePowerState(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
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
	if response, err := wrapper.client.VMWareShutdownGuest(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
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
	if response, err := wrapper.client.VMWareStatus(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
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

func (wrapper *desktopWrapper) RetrieveNetworkInfosWithContext(ctx *context.Context, vmuuid string, network *providers.Network) error {
	if response, err := wrapper.client.VMWareStatus(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		for _, ether := range response.GetResult().GetEthernet() {
			for _, inf := range network.Interfaces {
				if (inf.NetworkName == ether.Vnet) || (inf.ConnectionType == ether.ConnectionType && inf.ConnectionType != "custom") {
					inf.AttachMacAddress(ether.GeneratedAddress)
				}
			}
		}

		return nil
	}
}

func (wrapper *desktopWrapper) RetrieveNetworkInfos(vmuuid string, network *providers.Network) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.RetrieveNetworkInfosWithContext(ctx, vmuuid, network)
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
