package desktop

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/desktop/api"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
)

// Configuration declares desktop connection info
type Configuration struct {
	//Configuration *api.Configuration `json:"configuration"`
	NodeGroup    string                                   `json:"nodegroup"`
	Timeout      time.Duration                            `json:"timeout"`
	TemplateUUID string                                   `json:"template"`
	TimeZone     string                                   `json:"time-zone"`
	LinkedClone  bool                                     `json:"linked"`
	Autostart    bool                                     `json:"autostart"`
	Network      *Network                                 `json:"network"`
	AllowUpgrade bool                                     `json:"allow-upgrade"`
	TestMode     bool                                     `json:"-"`
	apiclient    api.VMWareDesktopAutoscalerServiceClient `json:"-"`
}

func (conf *Configuration) GetTestMode() bool {
	return conf.TestMode
}

func (conf *Configuration) SetTestMode(value bool) {
	conf.TestMode = value
}

func (conf *Configuration) GetTimeout() time.Duration {
	return conf.Timeout
}

func (conf *Configuration) AvailableGpuTypes() map[string]string {
	return map[string]string{}
}

func (conf *Configuration) NodeGroupName() string {
	return conf.NodeGroup
}

func (conf *Configuration) AttachInstance(instanceName string) error {
	return nil
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

func (conf *Configuration) UpdateMacAddressTable(nodeIndex int) {
	conf.Network.UpdateMacAddressTable(nodeIndex)
}

func (conf *Configuration) GenerateProviderID(vmuuid string) string {
	return fmt.Sprintf("desktop://%s", vmuuid)
}

func (conf *Configuration) GetTopologyLabels() map[string]string {
	return map[string]string{}
}

// Copy Make a deep copy from src into dst.
func Copy(dst interface{}, src interface{}) error {
	if dst == nil {
		return fmt.Errorf("dst cannot be nil")
	}

	if src == nil {
		return fmt.Errorf("src cannot be nil")
	}

	bytes, err := json.Marshal(src)

	if err != nil {
		return fmt.Errorf("unable to marshal src: %s", err)
	}

	err = json.Unmarshal(bytes, dst)

	if err != nil {
		return fmt.Errorf("unable to unmarshal into dst: %s", err)
	}

	return nil
}

func (conf *Configuration) SetClient(apiclient api.VMWareDesktopAutoscalerServiceClient) {
	conf.apiclient = apiclient
}

func (conf *Configuration) GetClient() (api.VMWareDesktopAutoscalerServiceClient, error) {
	return conf.apiclient, nil
}

// Create a shadow copy
func (conf *Configuration) Copy() *Configuration {
	var dup Configuration

	_ = Copy(&dup, conf)

	dup.apiclient = conf.apiclient

	return &dup
}

// Clone duplicate the conf, change ip address in network config if needed
func (conf *Configuration) Clone(nodeIndex int) (*Configuration, error) {
	dup := conf.Copy()

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
func (conf *Configuration) CreateWithContext(ctx *context.Context, name, userName, authKey string, cloudInit interface{}, network *Network, memory, cpus, disk, nodeIndex int) (string, error) {
	var err error

	request := &api.CreateRequest{
		Template:     conf.TemplateUUID,
		Name:         name,
		Vcpus:        int32(cpus),
		Memory:       int64(memory),
		DiskSizeInMb: int32(disk),
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
func (conf *Configuration) Create(name, userName, authKey string, cloudInit interface{}, network *Network, memory, cpus, disk, nodeIndex int) (string, error) {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.CreateWithContext(ctx, name, userName, authKey, cloudInit, network, memory, cpus, disk, nodeIndex)
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
func (conf *Configuration) PowerOff(name, mode string) error {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.PowerOffWithContext(ctx, name, mode)
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

func (conf *Configuration) RetrieveNetworkInfosWithContext(ctx *context.Context, vmuuid string, nodeIndex int) error {
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

func (conf *Configuration) RetrieveNetworkInfos(name, vmuuid string, nodeIndex int) error {
	ctx := context.NewContext(conf.Timeout)
	defer ctx.Cancel()

	return conf.RetrieveNetworkInfosWithContext(ctx, vmuuid, nodeIndex)
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
