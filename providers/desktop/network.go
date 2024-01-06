package desktop

import (
	"strings"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
)

// VMNetDevice declare single interface
type VNetDevice struct {
	AddressType            string `json:"addressType,omitempty" yaml:"addressType,omitempty"`
	BsdName                string `json:"bsdName,omitempty" yaml:"bsdName,omitempty"`
	ConnectionType         string `json:"connectionType,omitempty" yaml:"connectionType,omitempty"`
	DisplayName            string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	GeneratedAddress       string `json:"generatedAddress,omitempty" yaml:"generatedAddress,omitempty"`
	GeneratedAddressOffset int32  `json:"generatedAddressOffset,omitempty" yaml:"generatedAddressOffset,omitempty"`
	LinkStatePropagation   bool   `json:"linkStatePropagation,omitempty" yaml:"linkStatePropagation,omitempty"`
	PciSlotNumber          int32  `json:"pciSlotNumber,omitempty" yaml:"pciSlotNumber,omitempty"`
	Present                bool   `json:"present,omitempty" yaml:"present,omitempty"`
	VirtualDevice          string `json:"virtualDev,omitempty" yaml:"virtualDev,omitempty"`
	VNet                   string `json:"vnet,omitempty" yaml:"vnet,omitempty"`
	Address                string `json:"address,omitempty" yaml:"address,omitempty"`
}

// Status shortened vm status
type Status struct {
	Ethernet []VNetDevice
	Powered  bool
}

type NetworkDevice struct {
	Name   string `json:"name,omitempty" yaml:"name,omitempty"`
	Type   string `json:"type,omitempty" yaml:"type,omitempty"`
	Dhcp   bool   `json:"dhcp,omitempty" yaml:"dhcp,omitempty"`
	Subnet string `json:"subnet,omitempty" yaml:"subnet,omitempty"`
	Mask   string `json:"mask,omitempty" yaml:"mask,omitempty"`
}

func (vnet *VNetDevice) Same(inf *providers.NetworkInterface) bool {
	if (strings.EqualFold(inf.ConnectionType, "custom") && strings.EqualFold(vnet.ConnectionType, "custom")) || (strings.EqualFold(inf.ConnectionType, "bridged") && strings.EqualFold(vnet.ConnectionType, "bridged")) {
		return strings.EqualFold(inf.NetworkName, vnet.VNet)
	} else {
		return strings.EqualFold(inf.ConnectionType, vnet.ConnectionType)
	}
}
