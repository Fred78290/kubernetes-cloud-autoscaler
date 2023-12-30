package desktop

import (
	"fmt"
	"net"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/desktop/api"
)

// Configuration declares desktop connection info
type Configuration struct {
	//Configuration *api.Configuration `json:"configuration"`
	NodeGroup    string        `json:"nodegroup"`
	Timeout      time.Duration `json:"timeout"`
	TemplateUUID string        `json:"template"`
	TimeZone     string        `json:"time-zone"`
	LinkedClone  bool          `json:"linked"`
	Autostart    bool          `json:"autostart"`
	Network      *Network      `json:"network"`
	AllowUpgrade bool          `json:"allow-upgrade"`
	testMode     bool
	instanceName string
	instanceID   string
	apiclient    api.VMWareDesktopAutoscalerServiceClient
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

func (conf *Configuration) GetTestMode() bool {
	return conf.testMode
}

func (conf *Configuration) SetTestMode(value bool) {
	conf.testMode = value
}

func (conf *Configuration) GetTimeout() time.Duration {
	return conf.Timeout
}

func (conf *Configuration) GetAvailableGpuTypes() map[string]string {
	return map[string]string{}
}

func (conf *Configuration) NodeGroupName() string {
	return conf.NodeGroup
}

// Create a shadow copy
func (conf *Configuration) Copy() providers.ProviderConfiguration {
	var dup Configuration

	_ = providers.Copy(&dup, conf)

	dup.apiclient = conf.apiclient

	return &dup
}

// Clone duplicate the conf, change ip address in network config if needed
func (conf *Configuration) Clone(nodeIndex int) (providers.ProviderConfiguration, error) {
	dup := conf.Copy().(*Configuration)

	if dup.Network != nil {
		for _, inf := range dup.Network.Interfaces {
			if !inf.DHCP {
				ip := net.ParseIP(inf.IPAddress)
				address := ip.To4()
				address[3] += byte(nodeIndex)

				inf.IPAddress = ip.String()
			}
		}
	}

	return dup, nil
}

func (conf *Configuration) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
	if len(network.VMWare) > 0 {
		for _, network := range network.VMWare {
			if inf := conf.FindVMNet(network.NetworkName); inf != nil {
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

func (conf *Configuration) AttachInstance(instanceName string) error {
	if instanceID, err := conf.UUID(instanceName); err != nil {
		return err
	} else {
		conf.instanceName = instanceName
		conf.instanceID = instanceID
	}

	return nil
}

func (conf *Configuration) RetrieveNetworkInfos(name, vmuuid string, nodeIndex int) error {
	return conf.retrieveNetworkInfos(vmuuid, nodeIndex)
}

func (conf *Configuration) UpdateMacAddressTable(nodeIndex int) error {
	return conf.Network.UpdateMacAddressTable(nodeIndex)
}

func (conf *Configuration) GenerateProviderID(vmuuid string) string {
	return fmt.Sprintf("desktop://%s", vmuuid)
}

func (conf *Configuration) GetTopologyLabels() map[string]string {
	return map[string]string{}
}

func (conf *Configuration) InstanceCreate(nodeName string, nodeIndex int, instanceType, userName, authKey string, cloudInit interface{}, machine *providers.MachineCharacteristic) (string, error) {
	if vmuuid, err := conf.Create(nodeName, nodeIndex, userName, authKey, cloudInit, conf.Network, machine); err != nil {
		return "", err
	} else {
		conf.instanceName = nodeName
		conf.instanceID = vmuuid

		return conf.instanceID, err
	}
}

func (conf *Configuration) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	if ip, err := conf.WaitForIP(conf.instanceName, conf.Timeout); err != nil {
		return ip, err
	} else {
		if err := context.PollImmediate(time.Second, conf.Timeout*time.Second, func() (bool, error) {
			var err error

			if err = callback.WaitSSHReady(conf.instanceName, ip); err != nil {
				return false, err
			}

			return true, nil
		}); err != nil {
			return ip, err
		}

		return ip, nil
	}
}

func (conf *Configuration) InstanceID(name string) (string, error) {
	if name == conf.instanceName {
		return conf.instanceID, nil
	}

	return conf.UUID(name)
}

func (conf *Configuration) InstanceExists(name string) bool {
	return conf.Exists(name)
}

func (conf *Configuration) InstanceAutoStart(name string) error {
	return nil
}

func (conf *Configuration) InstancePowerOn(name string) error {
	if vmuuid, err := conf.InstanceID(name); err != nil {
		return err
	} else {
		return conf.PowerOn(vmuuid)
	}
}

func (conf *Configuration) InstancePowerOff(name string) error {
	if vmuuid, err := conf.InstanceID(name); err != nil {
		return err
	} else {
		return conf.PowerOff(vmuuid, "soft")
	}
}

func (conf *Configuration) InstanceDelete(name string) error {
	if vmuuid, err := conf.InstanceID(name); err != nil {
		return err
	} else {
		return conf.Delete(vmuuid)
	}
}

func (conf *Configuration) InstanceStatus(name string) (providers.InstanceStatus, error) {
	if vmuuid, err := conf.InstanceID(name); err != nil {
		return nil, err
	} else if status, err := conf.Status(vmuuid); err != nil {
		return nil, err
	} else {
		return &VmStatus{
			Status:  *status,
			address: conf.FindPreferredIPAddress(status.Ethernet),
		}, nil
	}
}

func (conf *Configuration) InstanceWaitForToolsRunning(name string) (bool, error) {
	if vmuuid, err := conf.InstanceID(name); err != nil {
		return false, err
	} else {
		return true, conf.WaitForPowerState(vmuuid, true)
	}
}

func (conf *Configuration) RegisterDNS(address string) error {
	return nil
}

func (conf *Configuration) UnregisterDNS(address string) error {
	return nil
}

func (conf *Configuration) SetClient(apiclient api.VMWareDesktopAutoscalerServiceClient) {
	conf.apiclient = apiclient
}

func (conf *Configuration) GetClient() (api.VMWareDesktopAutoscalerServiceClient, error) {
	return conf.apiclient, nil
}

func (conf *Configuration) FindVNetWithContext(ctx *context.Context, name string) (*NetworkDevice, error) {
	if client, err := conf.GetClient(); err != nil {
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

func (conf *Configuration) FindVNet(name string) (*NetworkDevice, error) {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.FindVNetWithContext(ctx, name)
}

func (conf *Configuration) FindPreferredIPAddress(devices []VNetDevice) string {
	address := ""

	for _, ether := range devices {
		if declaredInf := conf.FindInterface(&ether); declaredInf != nil {
			if declaredInf.Primary {
				return ether.Address
			}
		}
	}

	return address
}

func (conf *Configuration) FindVMNet(name string) *NetworkInterface {
	if conf.Network != nil {
		for _, inf := range conf.Network.Interfaces {
			if name == inf.VNet {
				return inf
			}
		}
	}

	return nil
}

func (conf *Configuration) FindInterface(ether *VNetDevice) *NetworkInterface {
	if conf.Network != nil {
		for _, inf := range conf.Network.Interfaces {
			if inf.Same(ether.ConnectionType, ether.VNet) {
				return inf
			}
		}
	}

	return nil
}

// CreateWithContext will create a named VM not powered
// memory and disk are in megabytes
// Return vm UUID
func (conf *Configuration) CreateWithContext(ctx *context.Context, name string, nodeIndex int, userName, authKey string, cloudInit interface{}, network *Network, machine *providers.MachineCharacteristic) (string, error) {
	var err error

	request := &api.CreateRequest{
		Template:     conf.TemplateUUID,
		Name:         name,
		Vcpus:        int32(machine.Vcpu),
		Memory:       int64(machine.Memory),
		DiskSizeInMb: int32(machine.DiskSize),
		Linked:       conf.LinkedClone,
		Networks:     BuildNetworkInterface(conf.Network.Interfaces, nodeIndex),
		Register:     false,
		Autostart:    conf.Autostart,
	}

	if request.GuestInfos, err = BuildCloudInit(name, userName, authKey, conf.TimeZone, cloudInit, network, nodeIndex, conf.AllowUpgrade); err != nil {
		return "", fmt.Errorf(constantes.ErrCloudInitFailCreation, name, err)
	} else if client, err := conf.GetClient(); err != nil {
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
func (conf *Configuration) Create(name string, nodeIndex int, userName, authKey string, cloudInit interface{}, network *Network, machine *providers.MachineCharacteristic) (string, error) {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.CreateWithContext(ctx, name, nodeIndex, userName, authKey, cloudInit, network, machine)
}

// DeleteWithContext a VM by UUID
func (conf *Configuration) DeleteWithContext(ctx *context.Context, vmuuid string) error {
	if client, err := conf.GetClient(); err != nil {
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
func (conf *Configuration) Delete(vmuuid string) error {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.DeleteWithContext(ctx, vmuuid)
}

// VirtualMachineWithContext  Retrieve VM by name
func (conf *Configuration) VirtualMachineByNameWithContext(ctx *context.Context, name string) (*VirtualMachine, error) {
	if client, err := conf.GetClient(); err != nil {
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
func (conf *Configuration) VirtualMachineByName(name string) (*VirtualMachine, error) {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.VirtualMachineByNameWithContext(ctx, name)
}

// VirtualMachineWithContext  Retrieve VM by vmuuid
func (conf *Configuration) VirtualMachineByUUIDWithContext(ctx *context.Context, vmuuid string) (*VirtualMachine, error) {
	if client, err := conf.GetClient(); err != nil {
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
func (conf *Configuration) VirtualMachineByUUID(vmuuid string) (*VirtualMachine, error) {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.VirtualMachineByUUIDWithContext(ctx, vmuuid)
}

// VirtualMachineListWithContext return all VM for the current datastore
func (conf *Configuration) VirtualMachineListWithContext(ctx *context.Context) ([]*VirtualMachine, error) {
	if client, err := conf.GetClient(); err != nil {
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
func (conf *Configuration) VirtualMachineList() ([]*VirtualMachine, error) {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.VirtualMachineListWithContext(ctx)
}

// UUID get VM UUID by name
func (conf *Configuration) UUIDWithContext(ctx *context.Context, name string) (string, error) {
	if vm, err := conf.VirtualMachineByNameWithContext(ctx, name); err != nil {
		return "", err
	} else {
		return vm.Uuid, nil
	}
}

// UUID get VM UUID by name
func (conf *Configuration) UUID(name string) (string, error) {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.UUIDWithContext(ctx, name)
}

// WaitForIPWithContext wait ip a VM by vmuuid
func (conf *Configuration) WaitForIPWithContext(ctx *context.Context, vmuuid string, timeout time.Duration) (string, error) {
	if client, err := conf.GetClient(); err != nil {
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
func (conf *Configuration) WaitForIP(vmuuid string, timeout time.Duration) (string, error) {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.WaitForIPWithContext(ctx, vmuuid, timeout)
}

// SetAutoStartWithContext set autostart for the VM
func (conf *Configuration) SetAutoStartWithContext(ctx *context.Context, vmuuid string, autostart bool) error {
	if client, err := conf.GetClient(); err != nil {
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
func (conf *Configuration) SetAutoStart(vmuuid string, autostart bool) error {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.SetAutoStartWithContext(ctx, vmuuid, autostart)
}

// WaitForToolsRunningWithContext wait vmware tools is running a VM by vmuuid
func (conf *Configuration) WaitForToolsRunningWithContext(ctx *context.Context, vmuuid string, timeout time.Duration) (bool, error) {
	if client, err := conf.GetClient(); err != nil {
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
func (conf *Configuration) WaitForToolsRunning(vmuuid string, timeout time.Duration) (bool, error) {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.WaitForToolsRunningWithContext(ctx, vmuuid, timeout)
}

// PowerOnWithContext power on a VM by vmuuid
func (conf *Configuration) PowerOnWithContext(ctx *context.Context, vmuuid string) error {
	if client, err := conf.GetClient(); err != nil {
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
func (conf *Configuration) PowerOn(vmuuid string) error {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.PowerOnWithContext(ctx, vmuuid)
}

// PowerOffWithContext power off a VM by vmuuid
func (conf *Configuration) PowerOffWithContext(ctx *context.Context, vmuuid, mode string) error {
	if client, err := conf.GetClient(); err != nil {
		return err
	} else if response, err := client.PowerOff(ctx, &api.PowerOffRequest{Identifier: vmuuid, Mode: mode}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		return nil
	}
}

func (conf *Configuration) WaitForPowerStateWithContenxt(ctx *context.Context, vmuuid string, wanted bool) error {
	if client, err := conf.GetClient(); err != nil {
		return err
	} else {
		return context.PollImmediate(time.Second, conf.Timeout, func() (bool, error) {
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

func (conf *Configuration) WaitForPowerState(vmuuid string, wanted bool) error {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.WaitForPowerStateWithContenxt(ctx, vmuuid, wanted)
}

// PowerOff power off a VM by name
func (conf *Configuration) PowerOff(vmuuid, mode string) error {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.PowerOffWithContext(ctx, vmuuid, mode)
}

// ShutdownGuestWithContext power off a VM by vmuuid
func (conf *Configuration) ShutdownGuestWithContext(ctx *context.Context, vmuuid string) error {
	if client, err := conf.GetClient(); err != nil {
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
func (conf *Configuration) ShutdownGuest(vmuuid string) error {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.ShutdownGuestWithContext(ctx, vmuuid)
}

// StatusWithContext return the current status of VM by vmuuid
func (conf *Configuration) StatusWithContext(ctx *context.Context, vmuuid string) (*Status, error) {
	if client, err := conf.GetClient(); err != nil {
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
func (conf *Configuration) Status(vmuuid string) (*Status, error) {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.StatusWithContext(ctx, vmuuid)
}

func (conf *Configuration) retrieveNetworkInfosWithContext(ctx *context.Context, vmuuid string, nodeIndex int) error {
	if client, err := conf.GetClient(); err != nil {
		return err
	} else if response, err := client.Status(ctx, &api.VirtualMachineRequest{Identifier: vmuuid}); err != nil {
		return err
	} else if response.GetError() != nil {
		return api.NewApiError(response.GetError())
	} else {
		for _, ether := range response.GetResult().GetEthernet() {
			for _, inf := range conf.Network.Interfaces {
				if (inf.VNet == ether.Vnet) || (inf.ConnectionType == ether.ConnectionType && inf.ConnectionType != "custom") {
					inf.AttachMacAddress(ether.GeneratedAddress, nodeIndex)
				}
			}
		}

		return nil
	}
}

func (conf *Configuration) retrieveNetworkInfos(vmuuid string, nodeIndex int) error {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.retrieveNetworkInfosWithContext(ctx, vmuuid, nodeIndex)
}

// ExistsWithContext return the current status of VM by name
func (conf *Configuration) ExistsWithContext(ctx *context.Context, name string) bool {
	if _, err := conf.VirtualMachineByNameWithContext(ctx, name); err == nil {
		return true
	}

	return false
}

func (conf *Configuration) Exists(name string) bool {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.ExistsWithContext(ctx, name)
}
