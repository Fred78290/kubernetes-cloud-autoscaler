package providers

import (
	"crypto/rand"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/praserx/ipconv"
)

type NetworkInterface struct {
	Enabled        bool                     `default:"true" json:"enabled,omitempty" yaml:"primary,omitempty"`
	Primary        bool                     `json:"primary,omitempty" yaml:"primary,omitempty"`
	Existing       bool                     `default:"true" json:"exists,omitempty" yaml:"exists,omitempty"`
	ConnectionType string                   `default:"nat" json:"type,omitempty" yaml:"type,omitempty"`
	BsdName        string                   `json:"bsd-name,omitempty" yaml:"bsd-name,omitempty"`
	DisplayName    string                   `json:"display-name,omitempty" yaml:"display-name,omitempty"`
	NetworkName    string                   `json:"network,omitempty" yaml:"network,omitempty"`
	Adapter        string                   `json:"adapter,omitempty" yaml:"adapter,omitempty"`
	MacAddress     string                   `json:"mac-address,omitempty" yaml:"mac-address,omitempty"`
	NicName        string                   `json:"nic,omitempty" yaml:"nic,omitempty"`
	DHCP           bool                     `json:"dhcp,omitempty" yaml:"dhcp,omitempty"`
	UseRoutes      bool                     `default:"true" json:"use-dhcp-routes,omitempty" yaml:"use-dhcp-routes,omitempty"`
	IPAddress      string                   `json:"address,omitempty" yaml:"address,omitempty"`
	Netmask        string                   `json:"netmask,omitempty" yaml:"netmask,omitempty"`
	Gateway        string                   `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	Routes         []v1alpha1.NetworkRoutes `json:"routes,omitempty" yaml:"routes,omitempty"`
}

type Network struct {
	Domain     string                   `json:"domain,omitempty" yaml:"domain,omitempty"`
	Interfaces []*NetworkInterface      `json:"interfaces,omitempty" yaml:"interfaces,omitempty"`
	DNS        *cloudinit.NetworkResolv `json:"dns,omitempty" yaml:"dns,omitempty"`
}

var macAddresesLock sync.Mutex
var macAddreses = make(map[string]string)

func stringBefore(str string, char string) string {
	if index := strings.Index(str, char); index >= 0 {
		return str[:index]
	} else {
		return str
	}
}

func stringAfter(str string, char string) string {
	if index := strings.LastIndex(str, char); index >= 0 {
		return str[index:]
	} else {
		return ""
	}
}

func attachMacAddress(netName, address string) {
	macAddresesLock.Lock()
	defer macAddresesLock.Unlock()

	macAddreses[netName] = address
}

func generateMacAddress(netName string) string {
	var address string
	var found bool

	macAddresesLock.Lock()
	defer macAddresesLock.Unlock()

	if address, found = macAddreses[netName]; !found {
		buf := make([]byte, 3)

		if _, err := rand.Read(buf); err != nil {
			return ""
		}

		address = fmt.Sprintf("00:16:3e:%02x:%02x:%02x", buf[0], buf[1], buf[2])

		macAddreses[netName] = address
	}

	return address
}

func (vnet *Network) Clone(nodeIndex int) *Network {
	copy := &Network{
		Domain:     vnet.Domain,
		DNS:        vnet.DNS,
		Interfaces: make([]*NetworkInterface, len(vnet.Interfaces)),
	}

	for index, inf := range vnet.Interfaces {
		address := inf.IPAddress
		if !inf.DHCP {
			if ip := net.ParseIP(inf.IPAddress).To4(); ip != nil {
				if ipv4, err := ipconv.IPv4ToInt(net.ParseIP(inf.IPAddress).To4()); err == nil {
					address = ipconv.IntToIPv4(ipv4 + uint32(nodeIndex)).String()
				}
			}
		}

		copy.Interfaces[index] = &NetworkInterface{
			Enabled:        inf.Enabled,
			Primary:        inf.Primary,
			Existing:       inf.Existing,
			ConnectionType: inf.ConnectionType,
			BsdName:        inf.BsdName,
			DisplayName:    inf.DisplayName,
			NetworkName:    inf.NetworkName,
			Adapter:        inf.Adapter,
			MacAddress:     inf.MacAddress,
			NicName:        inf.NicName,
			DHCP:           inf.DHCP,
			UseRoutes:      inf.UseRoutes,
			IPAddress:      address,
			Netmask:        inf.Netmask,
			Gateway:        inf.Gateway,
			Routes:         inf.Routes,
		}
	}

	return copy
}

func (vnet *Network) PrimaryAddressIP() (address string) {
	for _, n := range vnet.Interfaces {
		if n.Enabled && n.Primary {
			if !n.DHCP {
				address = n.IPAddress
			}
			break
		}
	}
	return address
}

// GetCloudInitNetwork create cloud-init object
func (vnet *Network) GetCloudInitNetwork(nodeIndex int) *cloudinit.NetworkDeclare {

	declare := &cloudinit.NetworkDeclare{
		Version:   2,
		Ethernets: make(map[string]*cloudinit.NetworkAdapter, len(vnet.Interfaces)),
	}

	for _, n := range vnet.Interfaces {
		if n.Enabled {
			nicName := stringBefore(n.NicName, ":")
			label := stringAfter(n.NicName, ":")

			if len(nicName) > 0 {
				var ethernet *cloudinit.NetworkAdapter
				var macAddress = n.GetMacAddress(nodeIndex)

				if n.DHCP || len(n.IPAddress) == 0 {
					ethernet = &cloudinit.NetworkAdapter{
						DHCP4: n.DHCP,
					}

					if !n.UseRoutes {
						ethernet.DHCPOverrides = map[string]any{
							"use-routes": false,
						}
					} else if len(n.Gateway) > 0 {
						ethernet.Gateway4 = &n.Gateway
					}

				}

				if len(n.IPAddress) > 0 {
					if len(label) > 0 {
						addr := map[string]any{}
						addr[cloudinit.ToCIDR(n.IPAddress, n.Netmask)] = map[string]any{
							"label": label,
						}

						ethernet = &cloudinit.NetworkAdapter{
							Addresses: &[]any{
								addr,
							},
						}

					} else {
						ethernet = &cloudinit.NetworkAdapter{
							Addresses: &[]any{
								cloudinit.ToCIDR(n.IPAddress, n.Netmask),
							},
						}
					}

					if len(n.Gateway) > 0 {
						ethernet.Gateway4 = &n.Gateway
					}
				}

				if len(macAddress) != 0 {
					ethernet.Match = &map[string]string{
						"macaddress": macAddress,
					}

					if len(nicName) > 0 {
						ethernet.NicName = &nicName
					}
				}

				if len(n.Routes) != 0 {
					ethernet.Routes = &n.Routes
				}

				if vnet.DNS != nil {
					ethernet.Nameservers = &cloudinit.Nameserver{
						Addresses: vnet.DNS.Nameserver,
						Search:    vnet.DNS.Search,
					}
				}

				declare.Ethernets[nicName] = ethernet
			}
		}
	}

	return declare
}

// GetDeclaredExistingInterfaces return the declared existing interfaces
func (vnet *Network) GetDeclaredExistingInterfaces() []*NetworkInterface {

	infs := make([]*NetworkInterface, 0, len(vnet.Interfaces))
	for _, inf := range vnet.Interfaces {
		if inf.Enabled && inf.Existing {
			infs = append(infs, inf)
		}
	}

	return infs
}

func (vnet *Network) UpdateMacAddressTable(nodeIndex int) error {
	for _, inf := range vnet.Interfaces {
		if inf.Enabled {
			inf.updateMacAddressTable(nodeIndex)
		}
	}

	return nil
}

func (vnet *Network) InterfaceByName(networkName string) *NetworkInterface {
	for _, inf := range vnet.Interfaces {
		if inf.Enabled && inf.NetworkName == networkName {
			return inf
		}
	}

	return nil
}

func (vnet *NetworkInterface) netName(nodeIndex int) string {
	return fmt.Sprintf("%s[%d]", vnet.NicName, nodeIndex)
}

func (vnet *NetworkInterface) updateMacAddressTable(nodeIndex int) {
	address := vnet.MacAddress

	if len(address) > 0 && strings.ToLower(address) != "generate" && strings.ToLower(address) != "ignore" {
		attachMacAddress(vnet.netName(nodeIndex), address)
	}
}

func (vnet *NetworkInterface) AttachMacAddress(address string, nodeIndex int) {
	attachMacAddress(vnet.netName(nodeIndex), address)
}

// GetMacAddress return a macaddress
func (vnet *NetworkInterface) GetMacAddress(nodeIndex int) string {
	if nodeIndex < 0 || !vnet.Enabled {
		return ""
	}

	address := vnet.MacAddress

	if strings.ToLower(address) == "generate" {
		address = generateMacAddress(vnet.netName(nodeIndex))
	} else if strings.ToLower(address) == "ignore" {
		address = ""
	}

	vnet.MacAddress = address

	return address
}
