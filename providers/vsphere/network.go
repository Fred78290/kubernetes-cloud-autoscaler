package vsphere

import (
	"strings"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type vsphereNetwork struct {
	*providers.Network
	VSphereInterfaces []vsphereNetworkInterface
}

// NetworkInterface declare single interface
type vsphereNetworkInterface struct {
	*providers.NetworkInterface
	networkReference object.NetworkReference
	networkBacking   types.BaseVirtualDeviceBackingInfo
}

func newVSphereNetwork(net *providers.Network, controlPlane bool, nodeIndex int) *vsphereNetwork {
	result := &vsphereNetwork{
		Network:           net.Clone(controlPlane, nodeIndex),
		VSphereInterfaces: make([]vsphereNetworkInterface, len(net.Interfaces)),
	}

	for index, inf := range result.Interfaces {
		result.VSphereInterfaces[index] = vsphereNetworkInterface{
			NetworkInterface: inf,
		}
	}

	return result
}

// Devices return all devices
func (net *vsphereNetwork) Devices(ctx *context.Context, devices object.VirtualDeviceList, dc *Datacenter) (object.VirtualDeviceList, error) {
	var err error
	var device types.BaseVirtualDevice

	for _, n := range net.VSphereInterfaces {
		if n.CreateIt() {
			if device, err = n.Device(ctx, dc); err == nil {
				devices = append(devices, n.SetMacAddress(device))
			} else {
				break
			}
		}
	}

	return devices, err
}

// See func (p DistributedVirtualPortgroup) EthernetCardBackingInfo(ctx context.Context) (types.BaseVirtualDeviceBackingInfo, error)
// Lack permissions workaround
func distributedVirtualPortgroupEthernetCardBackingInfo(ctx *context.Context, p *object.DistributedVirtualPortgroup) (string, error) {
	var dvp mo.DistributedVirtualPortgroup

	prop := "config.distributedVirtualSwitch"

	if err := p.Properties(ctx, p.Reference(), []string{"key", prop}, &dvp); err != nil {
		return "", err
	}

	return dvp.Key, nil
}

// MatchInterface return if this interface match the virtual device
// Due missing read permission, I can't create BackingInfo network card, so I use collected info to construct backing info
func (net *vsphereNetworkInterface) MatchInterface(ctx *context.Context, dc *Datacenter, card *types.VirtualEthernetCard) (bool, error) {

	equal := false

	if network, err := net.Reference(ctx, dc); err == nil {

		ref := network.Reference()

		if ref.Type == "Network" {
			if backing, ok := card.Backing.(*types.VirtualEthernetCardNetworkBackingInfo); ok {
				if c, err := network.EthernetCardBackingInfo(ctx); err == nil {
					if cc, ok := c.(*types.VirtualEthernetCardNetworkBackingInfo); ok {
						equal = backing.DeviceName == cc.DeviceName

						if equal {
							net.networkBacking = &types.VirtualEthernetCardNetworkBackingInfo{
								VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{
									DeviceName: backing.DeviceName,
								},
							}
						}
					}
				} else {
					return false, err
				}
			}
		} else if ref.Type == "OpaqueNetwork" {
			if backing, ok := card.Backing.(*types.VirtualEthernetCardOpaqueNetworkBackingInfo); ok {
				if c, err := network.EthernetCardBackingInfo(ctx); err == nil {
					if cc, ok := c.(*types.VirtualEthernetCardOpaqueNetworkBackingInfo); ok {
						equal = backing.OpaqueNetworkId == cc.OpaqueNetworkId && backing.OpaqueNetworkType == cc.OpaqueNetworkType

						if equal {
							net.networkBacking = &types.VirtualEthernetCardOpaqueNetworkBackingInfo{
								OpaqueNetworkId:   backing.OpaqueNetworkId,
								OpaqueNetworkType: backing.OpaqueNetworkType,
							}
						}
					}
				} else {
					return false, err
				}
			}
		} else if ref.Type == "DistributedVirtualPortgroup" {
			if backing, ok := card.Backing.(*types.VirtualEthernetCardDistributedVirtualPortBackingInfo); ok {
				if portgroupKey, err := distributedVirtualPortgroupEthernetCardBackingInfo(ctx, network.(*object.DistributedVirtualPortgroup)); err == nil {
					equal = backing.Port.PortgroupKey == portgroupKey

					if equal {
						net.networkBacking = &types.VirtualEthernetCardDistributedVirtualPortBackingInfo{
							Port: types.DistributedVirtualSwitchPortConnection{
								SwitchUuid:   backing.Port.SwitchUuid,
								PortgroupKey: backing.Port.PortgroupKey,
							},
						}
					}
				}
			} else {
				return false, err
			}
		}
	}

	return equal, nil
}

// SetMacAddress put mac address in the device
func (net *vsphereNetworkInterface) SetMacAddress(device types.BaseVirtualDevice) types.BaseVirtualDevice {
	adress := net.GetMacAddress()

	if len(adress) != 0 {
		card := device.(types.BaseVirtualEthernetCard).GetVirtualEthernetCard()
		card.AddressType = string(types.VirtualEthernetCardMacTypeManual)
		card.MacAddress = adress
	}

	return device
}

// Reference return the network reference
func (net *vsphereNetworkInterface) Reference(ctx *context.Context, dc *Datacenter) (object.NetworkReference, error) {
	var err error

	if net.networkReference == nil {

		f := dc.NewFinder(ctx)

		net.networkReference, err = f.NetworkOrDefault(ctx, net.NetworkName)
	}

	return net.networkReference, err
}

// Device return a device
func (net *vsphereNetworkInterface) Device(ctx *context.Context, dc *Datacenter) (types.BaseVirtualDevice, error) {
	network, err := net.Reference(ctx, dc)

	if err != nil {
		return nil, err
	}

	networkReference := network.Reference()
	net.networkBacking, err = network.EthernetCardBackingInfo(ctx)

	if err != nil {
		if strings.Contains(err.Error(), "no System.Read privilege on:") {
			if false {
				net.networkBacking = &types.VirtualEthernetCardOpaqueNetworkBackingInfo{
					OpaqueNetworkType: networkReference.Type,
					OpaqueNetworkId:   networkReference.Value,
				}
			} else {
				net.networkBacking = &types.VirtualEthernetCardNetworkBackingInfo{
					Network: &networkReference,
					VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{
						DeviceName: net.NetworkName,
					},
				}
			}
		} else {
			return nil, err
		}
	}

	device, err := object.EthernetCardTypes().CreateEthernetCard(net.Adapter, net.networkBacking)
	if err != nil {
		return nil, err
	}

	// Connect the device
	enabled := net.IsEnabled()
	device.GetVirtualDevice().Connectable = &types.VirtualDeviceConnectInfo{
		StartConnected:    enabled,
		AllowGuestControl: enabled,
		Connected:         enabled,
	}

	macAddress := net.GetMacAddress()

	if len(macAddress) != 0 {
		card := device.(types.BaseVirtualEthernetCard).GetVirtualEthernetCard()
		card.AddressType = string(types.VirtualEthernetCardMacTypeManual)
		card.MacAddress = macAddress
	}

	return device, nil
}

// Change applies update backing and hardware address changes to the given network device.
func (net *vsphereNetworkInterface) Change(device types.BaseVirtualDevice, update types.BaseVirtualDevice) {
	current := device.(types.BaseVirtualEthernetCard).GetVirtualEthernetCard()
	changed := update.(types.BaseVirtualEthernetCard).GetVirtualEthernetCard()

	current.Backing = changed.Backing

	if len(changed.MacAddress) > 0 {
		current.MacAddress = changed.MacAddress
	}

	if len(changed.AddressType) > 0 {
		current.AddressType = changed.AddressType
	}
}

// ChangeAddress just the mac adress
func (net *vsphereNetworkInterface) ChangeAddress(card *types.VirtualEthernetCard) bool {
	macAddress := net.GetMacAddress()

	if len(macAddress) != 0 {
		card.Backing = net.networkBacking
		card.AddressType = string(types.VirtualEthernetCardMacTypeManual)
		card.MacAddress = macAddress

		return true
	}

	return false
}

func (net *vsphereNetworkInterface) ConfigureEthernetCard(ctx *context.Context, dc *Datacenter, card *types.VirtualEthernetCard) error {
	if network, err := net.Reference(ctx, dc); err != nil {
		return err
	} else {
		macAddress := net.GetMacAddress()
		networkReference := network.Reference()

		if _, ok := network.(*object.DistributedVirtualSwitch); ok {
			net.networkBacking = card.Backing
		} else {
			net.networkBacking = &types.VirtualEthernetCardNetworkBackingInfo{
				Network: &networkReference,
				VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{
					DeviceName: net.NetworkName,
				},
			}
			card.Backing = net.networkBacking
		}

		if len(macAddress) > 0 {
			card.AddressType = string(types.VirtualEthernetCardMacTypeManual)
			card.MacAddress = macAddress
		}
	}

	return nil
}

// NeedToReconfigure tell that we must set the mac address
func (net *vsphereNetworkInterface) NeedToReconfigure() bool {
	return len(net.GetMacAddress()) != 0 && net.Usable()
}
