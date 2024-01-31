package vsphere

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	glog "github.com/sirupsen/logrus"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// VirtualMachine virtual machine wrapper
type VirtualMachine struct {
	Ref       types.ManagedObjectReference
	Name      string
	Datastore *Datastore
}

func (vm *VirtualMachine) UUID(ctx *context.Context) string {
	virtualMachine := vm.VirtualMachine(ctx)

	return virtualMachine.UUID(ctx)
}

func (vm *VirtualMachine) HostSystem(ctx *context.Context) (string, error) {

	host, err := vm.VirtualMachine(ctx).HostSystem(ctx)

	if err != nil {
		return "*", err
	}

	return host.ObjectName(ctx)
}

// VirtualMachine return govmomi virtual machine
func (vm *VirtualMachine) VirtualMachine(ctx *context.Context) *object.VirtualMachine {
	key := vm.Ref.String()

	if v := ctx.Value(key); v != nil {
		return v.(*object.VirtualMachine)
	}

	f := vm.Datastore.Datacenter.NewFinder(ctx)

	v, err := f.ObjectReference(ctx, vm.Ref)

	if err != nil {
		glog.Fatalf("Can't find virtual machine:%s", vm.Name)
	}

	//	v := object.NewVirtualMachine(vm.VimClient(), vm.Ref)

	ctx.WithValue(key, v)
	ctx.WithValue(fmt.Sprintf("[%s] %s", vm.Datastore.Name, vm.Name), v)

	return v.(*object.VirtualMachine)
}

// VimClient return the VIM25 client
func (vm *VirtualMachine) VimClient() *vim25.Client {
	return vm.Datastore.VimClient()
}

func (vm *VirtualMachine) collectNetworkInfos(ctx *context.Context, network *vsphereNetwork, nodeIndex int) error {
	virtualMachine := vm.VirtualMachine(ctx)
	devices, err := virtualMachine.Device(ctx)

	if err == nil {
		for _, device := range devices {
			// It's an ether device?
			if ethernet, ok := device.(types.BaseVirtualEthernetCard); ok {
				// Match my network?
				for _, inf := range network.VSphereInterfaces {
					if inf.Enabled {
						card := ethernet.GetVirtualEthernetCard()
						if match, err := inf.MatchInterface(ctx, vm.Datastore.Datacenter, card); match && err == nil {
							inf.AttachMacAddress(card.MacAddress, nodeIndex)
						}
					}
				}
			}
		}
	}

	return err
}

func (vm *VirtualMachine) addNetwork(ctx *context.Context, network *vsphereNetwork, devices object.VirtualDeviceList, nodeIndex int) (object.VirtualDeviceList, error) {
	var err error

	if network != nil && len(network.VSphereInterfaces) > 0 {
		devices, err = network.Devices(ctx, devices, vm.Datastore.Datacenter, nodeIndex)
	}

	return devices, err
}

func (vm *VirtualMachine) findHardDrive(ctx *context.Context, list object.VirtualDeviceList) (*types.VirtualDisk, error) {
	var disks []*types.VirtualDisk

	for _, device := range list {
		switch md := device.(type) {
		case *types.VirtualDisk:
			disks = append(disks, md)
		default:
			continue
		}
	}

	switch len(disks) {
	case 0:
		return nil, errors.New("no disk found using the given values")
	case 1:
		return disks[0], nil
	}

	return nil, errors.New("the given disk values match multiple disks")
}

func (vm *VirtualMachine) addOrExpandHardDrive(ctx *context.Context, virtualMachine *object.VirtualMachine, diskSize int, expandHardDrive bool, devices object.VirtualDeviceList) (object.VirtualDeviceList, error) {
	var err error
	var existingDevices object.VirtualDeviceList
	var controller types.BaseVirtualController

	if diskSize > 0 {
		drivePath := fmt.Sprintf("[%s] %s/harddrive.vmdk", vm.Datastore.Name, vm.Name)

		if existingDevices, err = virtualMachine.Device(ctx); err == nil {
			if expandHardDrive {
				var task *object.Task
				var disk *types.VirtualDisk

				if disk, err = vm.findHardDrive(ctx, existingDevices); err == nil {
					diskSizeInKB := int64(diskSize * 1024)

					if diskSizeInKB > disk.CapacityInKB {
						disk.CapacityInKB = diskSizeInKB

						spec := types.VirtualMachineConfigSpec{
							DeviceChange: []types.BaseVirtualDeviceConfigSpec{
								&types.VirtualDeviceConfigSpec{
									Device:    disk,
									Operation: types.VirtualDeviceConfigSpecOperationEdit,
								},
							},
						}

						if task, err = virtualMachine.Reconfigure(ctx, spec); err == nil {
							err = task.Wait(ctx)
						}
					}
				}
			} else {
				if controller, err = existingDevices.FindDiskController(""); err == nil {

					disk := existingDevices.CreateDisk(controller, vm.Datastore.Ref, drivePath)

					if len(existingDevices.SelectByBackingInfo(disk.Backing)) != 0 {

						err = fmt.Errorf("disk %s already exists", drivePath)

					} else {

						backing := disk.Backing.(*types.VirtualDiskFlatVer2BackingInfo)

						backing.ThinProvisioned = types.NewBool(true)
						backing.DiskMode = string(types.VirtualDiskModePersistent)
						backing.Sharing = string(types.VirtualDiskSharingSharingNone)

						disk.CapacityInKB = int64(diskSize) * 1024

						devices = append(devices, disk)
					}
				}
			}
		}
	}

	return devices, err
}

// Configure set characteristic of VM a virtual machine
func (vm *VirtualMachine) Configure(ctx *context.Context, input *CreateInput) error {
	var devices object.VirtualDeviceList
	var err error
	var task *object.Task

	virtualMachine := vm.VirtualMachine(ctx)

	vmConfigSpec := types.VirtualMachineConfigSpec{
		NumCPUs:      int32(input.Machine.Vcpu),
		MemoryMB:     int64(input.Machine.Memory),
		Annotation:   input.Annotation,
		InstanceUuid: virtualMachine.UUID(ctx),
		Uuid:         virtualMachine.UUID(ctx),
	}

	if devices, err = vm.addOrExpandHardDrive(ctx, virtualMachine, input.DiskSize, input.ExpandHardDrive, devices); err != nil {

		err = fmt.Errorf(constantes.ErrUnableToAddHardDrive, vm.Name, err)

	} else if devices, err = vm.addNetwork(ctx, input.VSphereNetwork, devices, input.NodeIndex); err != nil {

		err = fmt.Errorf(constantes.ErrUnableToAddNetworkCard, vm.Name, err)

	} else if vmConfigSpec.DeviceChange, err = devices.ConfigSpec(types.VirtualDeviceConfigSpecOperationAdd); err != nil {

		err = fmt.Errorf(constantes.ErrUnableToCreateDeviceChangeOp, vm.Name, err)

	} else if vmConfigSpec.ExtraConfig, err = vm.SetGuestInfo(ctx, input); err != nil {

		err = fmt.Errorf(constantes.ErrCloudInitFailCreation, vm.Name, err)

	} else if task, err = virtualMachine.Reconfigure(ctx, vmConfigSpec); err != nil {

		err = fmt.Errorf(constantes.ErrUnableToReconfigureVM, vm.Name, err)

	} else if err = task.Wait(ctx); err != nil {

		err = fmt.Errorf(constantes.ErrUnableToReconfigureVM, vm.Name, err)

	}

	return err
}

func (vm VirtualMachine) isSimulatorRunning(ctx *context.Context, v *object.VirtualMachine) bool {
	var o mo.VirtualMachine

	err := v.Properties(ctx, v.Reference(), []string{"config.extraConfig"}, &o)
	if err != nil {
		return false
	}

	for _, extra := range o.Config.ExtraConfig {
		extraValue := extra.GetOptionValue()

		if extraValue.Key == "govcsim" && strings.ToLower(fmt.Sprint(extraValue.Value)) == "true" {
			return true
		}
	}

	return false
}

func (vm VirtualMachine) isToolsRunning(ctx *context.Context, v *object.VirtualMachine) (bool, error) {
	var o mo.VirtualMachine

	if vm.isSimulatorRunning(ctx, v) {
		return true, nil
	}

	running, err := v.IsToolsRunning(ctx)
	if err != nil {
		return false, err
	}

	if running {
		return running, nil
	}

	err = v.Properties(ctx, v.Reference(), []string{"guest"}, &o)
	if err != nil {
		return false, err
	}

	return o.Guest.ToolsRunningStatus == string(types.VirtualMachineToolsRunningStatusGuestToolsRunning), nil
}

// IsToolsRunning returns true if VMware Tools is currently running in the guest OS, and false otherwise.
func (vm VirtualMachine) IsToolsRunning(ctx *context.Context) (bool, error) {
	v := vm.VirtualMachine(ctx)

	return vm.isToolsRunning(ctx, v)
}

// IsSimulatorRunning returns true if VMware Tools is currently running in the guest OS, and false otherwise.
func (vm VirtualMachine) IsSimulatorRunning(ctx *context.Context) bool {
	v := vm.VirtualMachine(ctx)

	return vm.isSimulatorRunning(ctx, v)
}

func (vm *VirtualMachine) waitForPowered(ctx *context.Context, v *object.VirtualMachine) error {
	var running bool

	p := property.DefaultCollector(vm.VimClient())

	return property.Wait(ctx, p, v.Reference(), []string{object.PropRuntimePowerState}, func(pc []types.PropertyChange) bool {
		for _, c := range pc {
			if c.Name != object.PropRuntimePowerState {
				continue
			}
			if c.Op != types.PropertyChangeOpAssign {
				continue
			}
			if c.Val == nil {
				continue
			}

			running = c.Val.(string) == string(types.VirtualMachinePowerStatePoweredOn)

			return running
		}

		return false
	})
}

func (vm *VirtualMachine) waitForToolsRunning(ctx *context.Context, v *object.VirtualMachine) (bool, error) {
	var running bool

	p := property.DefaultCollector(vm.VimClient())

	err := property.Wait(ctx, p, v.Reference(), []string{"guest.toolsRunningStatus"}, func(pc []types.PropertyChange) bool {
		for _, c := range pc {
			if c.Name != "guest.toolsRunningStatus" {
				continue
			}
			if c.Op != types.PropertyChangeOpAssign {
				continue
			}
			if c.Val == nil {
				continue
			}

			running = c.Val.(string) == string(types.VirtualMachineToolsRunningStatusGuestToolsRunning)

			return running
		}

		return false
	})

	if err != nil {
		return false, err
	}

	return running, nil
}

func (vm *VirtualMachine) ListAddresses(ctx *context.Context) ([]providers.NetworkInterface, error) {
	var o mo.VirtualMachine

	v := vm.VirtualMachine(ctx)

	if err := v.Properties(ctx, v.Reference(), []string{"guest"}, &o); err != nil {
		return nil, err
	}

	addresses := make([]providers.NetworkInterface, 0, len(o.Guest.Net))

	for _, net := range o.Guest.Net {
		if net.Connected {
			ip := ""

			// vcsim bug
			if len(net.IpAddress) > 0 {
				ip = net.IpAddress[0]
			}

			addresses = append(addresses, providers.NetworkInterface{
				NetworkName: net.Network,
				MacAddress:  net.MacAddress,
				IPAddress:   ip,
			})
		}
	}

	return addresses, nil
}

// WaitForToolsRunning wait vmware tool starts
func (vm *VirtualMachine) WaitForPowered(ctx *context.Context) error {
	var powerState types.VirtualMachinePowerState
	var err error

	v := vm.VirtualMachine(ctx)

	if powerState, err = v.PowerState(ctx); err == nil {
		if powerState == types.VirtualMachinePowerStatePoweredOff {
			return vm.waitForPowered(ctx, v)
		}
	}

	return nil
}

// WaitForToolsRunning wait vmware tool starts
func (vm *VirtualMachine) WaitForToolsRunning(ctx *context.Context) (bool, error) {
	var powerState types.VirtualMachinePowerState
	var err error
	var running bool

	v := vm.VirtualMachine(ctx)

	if powerState, err = v.PowerState(ctx); err == nil {
		if powerState == types.VirtualMachinePowerStatePoweredOn {
			running, err = vm.waitForToolsRunning(ctx, v)
		} else {
			err = fmt.Errorf("the VM: %s is not powered", v.InventoryPath)
		}
	}

	return running, err
}

func (vm *VirtualMachine) waitForIP(ctx *context.Context, v *object.VirtualMachine) (string, error) {
	if running, err := vm.isToolsRunning(ctx, v); running {
		return v.WaitForIP(ctx)
	} else if err != nil {
		return "", err
	}

	return "", fmt.Errorf("VMWare tools is not running on the VM:%s, unable to retrieve IP", vm.Name)
}

// WaitForIP wait ip
func (vm *VirtualMachine) WaitForIP(ctx *context.Context) (string, error) {
	var powerState types.VirtualMachinePowerState
	var err error
	var ip string

	v := vm.VirtualMachine(ctx)

	if !vm.isSimulatorRunning(ctx, v) {
		if powerState, err = v.PowerState(ctx); err == nil {
			if powerState == types.VirtualMachinePowerStatePoweredOn {
				ip, err = vm.waitForIP(ctx, v)
			} else {
				err = fmt.Errorf("the VM: %s is not powered", v.InventoryPath)
			}
		}
	}

	return ip, err
}

// PowerOn power on a virtual machine
func (vm *VirtualMachine) PowerOn(ctx *context.Context) error {
	var powerState types.VirtualMachinePowerState
	var err error
	var task *object.Task

	v := vm.VirtualMachine(ctx)

	if powerState, err = v.PowerState(ctx); err == nil {
		if powerState != types.VirtualMachinePowerStatePoweredOn {
			if task, err = v.PowerOn(ctx); err == nil {
				err = task.Wait(ctx)
			}
		} else {
			err = fmt.Errorf("the VM: %s is already powered", v.InventoryPath)
		}
	}

	return err
}

// PowerOff power off a virtual machine
func (vm *VirtualMachine) PowerOff(ctx *context.Context) error {
	var powerState types.VirtualMachinePowerState
	var err error
	var task *object.Task

	v := vm.VirtualMachine(ctx)

	if powerState, err = v.PowerState(ctx); err == nil {
		if powerState == types.VirtualMachinePowerStatePoweredOn {
			if task, err = v.PowerOff(ctx); err == nil {
				err = task.Wait(ctx)
			}
		} else {
			err = fmt.Errorf("the VM: %s is already off", v.InventoryPath)
		}
	}

	return err
}

// ShutdownGuest power off a virtual machine
func (vm *VirtualMachine) ShutdownGuest(ctx *context.Context) error {
	var powerState types.VirtualMachinePowerState
	var err error
	var task *object.Task

	v := vm.VirtualMachine(ctx)

	if powerState, err = v.PowerState(ctx); err == nil {
		if powerState == types.VirtualMachinePowerStatePoweredOn {
			if err = v.ShutdownGuest(ctx); err != nil {
				if task, err = v.PowerOff(ctx); err == nil {
					err = task.Wait(ctx)
				}
			}
		} else {
			err = fmt.Errorf("the VM: %s is already power off", v.InventoryPath)
		}
	}

	return err
}

// Delete delete the virtual machine
func (vm *VirtualMachine) Delete(ctx *context.Context) error {
	var powerState types.VirtualMachinePowerState
	var err error
	var task *object.Task

	v := vm.VirtualMachine(ctx)

	if powerState, err = v.PowerState(ctx); err == nil {
		if powerState != types.VirtualMachinePowerStatePoweredOn {
			if task, err = v.Destroy(ctx); err == nil {
				err = task.Wait(ctx)
			}
		} else {
			err = fmt.Errorf("the VM: %s is powered", v.InventoryPath)
		}
	}

	return err
}

// Status refresh status virtual machine
func (vm *VirtualMachine) Status(ctx *context.Context) (*Status, error) {
	var powerState types.VirtualMachinePowerState
	var err error
	var status *Status = &Status{}
	var interfaces []providers.NetworkInterface
	v := vm.VirtualMachine(ctx)

	if powerState, err = v.PowerState(ctx); err == nil {
		if interfaces, err = vm.ListAddresses(ctx); err == nil {
			status.Interfaces = interfaces
			status.Powered = powerState == types.VirtualMachinePowerStatePoweredOn
		}
	}

	return status, err
}

// SetGuestInfo change guest ingos
func (vm *VirtualMachine) SetGuestInfo(ctx *context.Context, input *CreateInput) ([]types.BaseOptionValue, error) {
	tz, _ := time.Now().Zone()
	v := vm.VirtualMachine(ctx)

	cloundInitInput := cloudinit.CloudInitInput{
		InstanceName: input.NodeName,
		InstanceID:   v.UUID(ctx),
		UserName:     input.UserName,
		AuthKey:      input.AuthKey,
		DomainName:   input.VSphereNetwork.Domain,
		CloudInit:    input.CloudInit,
		AllowUpgrade: input.AllowUpgrade,
		TimeZone:     tz,
		Network:      nil,
	}

	if input.VSphereNetwork != nil && len(input.VSphereNetwork.Interfaces) > 0 {
		cloundInitInput.Network = input.VSphereNetwork.GetCloudInitNetwork(input.NodeIndex)
	}

	if guestInfos, err := cloundInitInput.BuildGuestInfos(); err != nil {
		return nil, err
	} else {
		extraConfig := make([]types.BaseOptionValue, len(guestInfos))

		for k, v := range guestInfos {
			extraConfig = append(extraConfig,
				&types.OptionValue{
					Key:   fmt.Sprintf("guestinfo.%s", k),
					Value: v,
				})
		}

		return extraConfig, nil
	}
}
