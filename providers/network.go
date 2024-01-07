package providers

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
)

type NetworkInterface struct {
	Primary        bool                     `json:"primary,omitempty" yaml:"primary,omitempty"`
	Existing       bool                     `json:"exists,omitempty" yaml:"exists,omitempty"`
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

func (net *Network) Clone() *Network {
	copy := &Network{
		Domain:     net.Domain,
		DNS:        net.DNS,
		Interfaces: make([]*NetworkInterface, len(net.Interfaces)),
	}

	for index, inf := range net.Interfaces {
		copy.Interfaces[index] = &NetworkInterface{
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
			IPAddress:      inf.IPAddress,
			Netmask:        inf.Netmask,
			Gateway:        inf.Gateway,
			Routes:         inf.Routes,
		}
	}

	return copy
}

// GetCloudInitNetwork create cloud-init object
func (net *Network) GetCloudInitNetwork(nodeIndex int) *cloudinit.NetworkDeclare {

	declare := &cloudinit.NetworkDeclare{
		Version:   2,
		Ethernets: make(map[string]*cloudinit.NetworkAdapter, len(net.Interfaces)),
	}

	for _, n := range net.Interfaces {
		if len(n.NicName) > 0 {
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

			} else {
				ethernet = &cloudinit.NetworkAdapter{
					Addresses: &[]string{
						cloudinit.ToCIDR(n.IPAddress, n.Netmask),
					},
				}

				if len(n.Gateway) > 0 {
					ethernet.Gateway4 = &n.Gateway
				}
			}

			if len(macAddress) != 0 {
				ethernet.Match = &map[string]string{
					"macaddress": macAddress,
				}
			}

			if len(n.NicName) > 0 {
				ethernet.NicName = &n.NicName
			}

			if len(n.Routes) != 0 {
				ethernet.Routes = &n.Routes
			}

			if net.DNS != nil {
				ethernet.Nameservers = &cloudinit.Nameserver{
					Addresses: net.DNS.Nameserver,
					Search:    net.DNS.Search,
				}
			}

			declare.Ethernets[n.NicName] = ethernet
		}
	}

	return declare
}

// GetDeclaredExistingInterfaces return the declared existing interfaces
func (net *Network) GetDeclaredExistingInterfaces() []*NetworkInterface {

	infs := make([]*NetworkInterface, 0, len(net.Interfaces))
	for _, inf := range net.Interfaces {
		if inf.Existing {
			infs = append(infs, inf)
		}
	}

	return infs
}

func (net *Network) UpdateMacAddressTable(nodeIndex int) error {
	for _, inf := range net.Interfaces {
		inf.updateMacAddressTable(nodeIndex)
	}

	return nil
}

func (net *Network) InterfaceByName(networkName string) *NetworkInterface {
	for _, inf := range net.Interfaces {
		if inf.NetworkName == networkName {
			return inf
		}
	}

	return nil
}

func (net *NetworkInterface) netName(nodeIndex int) string {
	return fmt.Sprintf("%s[%d]", net.NicName, nodeIndex)
}

func (net *NetworkInterface) updateMacAddressTable(nodeIndex int) {
	address := net.MacAddress

	if len(address) > 0 && strings.ToLower(address) != "generate" && strings.ToLower(address) != "ignore" {
		attachMacAddress(net.netName(nodeIndex), address)
	}
}

func (net *NetworkInterface) AttachMacAddress(address string, nodeIndex int) {
	attachMacAddress(net.netName(nodeIndex), address)
}

// GetMacAddress return a macaddress
func (net *NetworkInterface) GetMacAddress(nodeIndex int) string {
	if nodeIndex < 0 {
		return ""
	}

	address := net.MacAddress

	if strings.ToLower(address) == "generate" {
		address = generateMacAddress(net.netName(nodeIndex))
	} else if strings.ToLower(address) == "ignore" {
		address = ""
	}

	net.MacAddress = address

	return address
}
