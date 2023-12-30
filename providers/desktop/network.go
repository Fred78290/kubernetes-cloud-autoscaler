package desktop

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
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

// NetworkInterface declare single interface
type NetworkInterface struct {
	Primary        bool                     `json:"primary,omitempty" yaml:"primary,omitempty"`
	Existing       bool                     `json:"exists,omitempty" yaml:"exists,omitempty"`
	ConnectionType string                   `default:"nat" json:"type,omitempty" yaml:"type,omitempty"`
	VNet           string                   `json:"vnet,omitempty" yaml:"vnet,omitempty"`
	VirtualDev     string                   `default:"vmxnet3" json:"device,omitempty" yaml:"device,omitempty"`
	BsdName        string                   `json:"bsd-name,omitempty" yaml:"bsd-name,omitempty"`
	DisplayName    string                   `json:"display-name,omitempty" yaml:"display-name,omitempty"`
	MacAddress     string                   `json:"mac-address,omitempty" yaml:"mac-address,omitempty"`
	NicName        string                   `json:"nic,omitempty" yaml:"nic,omitempty"`
	DHCP           bool                     `json:"dhcp,omitempty" yaml:"dhcp,omitempty"`
	UseRoutes      bool                     `default:"true" json:"use-dhcp-routes,omitempty" yaml:"use-dhcp-routes,omitempty"`
	IPAddress      string                   `json:"address,omitempty" yaml:"address,omitempty"`
	Netmask        string                   `json:"netmask,omitempty" yaml:"netmask,omitempty"`
	Gateway        string                   `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	Routes         []v1alpha1.NetworkRoutes `json:"routes,omitempty" yaml:"routes,omitempty"`
}

type NetworkDevice struct {
	Name   string `json:"name,omitempty" yaml:"name,omitempty"`
	Type   string `json:"type,omitempty" yaml:"type,omitempty"`
	Dhcp   bool   `json:"dhcp,omitempty" yaml:"dhcp,omitempty"`
	Subnet string `json:"subnet,omitempty" yaml:"subnet,omitempty"`
	Mask   string `json:"mask,omitempty" yaml:"mask,omitempty"`
}

// NetworkResolv /etc/resolv.conf
type NetworkResolv struct {
	Search     []string `json:"search,omitempty" yaml:"search,omitempty"`
	Nameserver []string `json:"nameserver,omitempty" yaml:"nameserver,omitempty"`
}

// Network describes a card adapter
type Network struct {
	Domain     string              `json:"domain,omitempty" yaml:"domain,omitempty"`
	Interfaces []*NetworkInterface `json:"interfaces,omitempty" yaml:"interfaces,omitempty"`
	DNS        *NetworkResolv      `json:"dns,omitempty" yaml:"dns,omitempty"`
}

// Nameserver declaration
type Nameserver struct {
	Search    []string `json:"search,omitempty" yaml:"search,omitempty"`
	Addresses []string `json:"addresses,omitempty" yaml:"addresses,omitempty"`
}

// NetworkAdapter wrapper
type NetworkAdapter struct {
	DHCP4         bool                      `json:"dhcp4,omitempty" yaml:"dhcp4,omitempty"`
	NicName       *string                   `json:"set-name,omitempty" yaml:"set-name,omitempty"`
	Match         *map[string]string        `json:"match,omitempty" yaml:"match,omitempty"`
	Gateway4      *string                   `json:"gateway4,omitempty" yaml:"gateway4,omitempty"`
	Addresses     *[]string                 `json:"addresses,omitempty" yaml:"addresses,omitempty"`
	Nameservers   *Nameserver               `json:"nameservers,omitempty" yaml:"nameservers,omitempty"`
	DHCPOverrides *map[string]interface{}   `json:"dhcp4-overrides,omitempty" yaml:"dhcp4-overrides,omitempty"`
	Routes        *[]v1alpha1.NetworkRoutes `json:"routes,omitempty" yaml:"routes,omitempty"`
}

// NetworkDeclare wrapper
type NetworkDeclare struct {
	Version   int                        `json:"version,omitempty" yaml:"version,omitempty"`
	Ethernets map[string]*NetworkAdapter `json:"ethernets,omitempty" yaml:"ethernets,omitempty"`
}

// NetworkConfig wrapper
type NetworkConfig struct {
	InstanceID    string          `json:"instance-id,omitempty" yaml:"instance-id,omitempty"`
	LocalHostname string          `json:"local-hostname,omitempty" yaml:"local-hostname,omitempty"`
	Hostname      string          `json:"hostname,omitempty" yaml:"hostname,omitempty"`
	Network       *NetworkDeclare `json:"network,omitempty" yaml:"network,omitempty"`
}

// Converts IP mask to 16 bit unsigned integer.
func addressToInteger(mask net.IP) uint32 {
	var i uint32

	buf := bytes.NewReader(mask)

	_ = binary.Read(buf, binary.BigEndian, &i)

	return i
}

// ToCIDR returns address in cidr format ww.xx.yy.zz/NN
func ToCIDR(address, netmask string) string {

	if len(netmask) == 0 {
		mask := net.ParseIP(address).DefaultMask()
		netmask = net.IPv4(mask[0], mask[1], mask[2], mask[3]).To4().String()
	}

	mask := net.ParseIP(netmask)
	netmask = strconv.FormatUint(uint64(addressToInteger(mask.To4())), 2)

	return fmt.Sprintf("%s/%d", address, strings.Count(netmask, "1"))
}

func (inf *NetworkInterface) Same(connectionType, vnet string) bool {
	if (strings.EqualFold(inf.ConnectionType, "custom") && strings.EqualFold(connectionType, "custom")) || (strings.EqualFold(inf.ConnectionType, "bridged") && strings.EqualFold(connectionType, "bridged")) {
		return strings.EqualFold(inf.VNet, vnet)
	} else {
		return strings.EqualFold(inf.ConnectionType, connectionType)
	}
}

// GetCloudInitNetwork create cloud-init object
func (net *Network) GetCloudInitNetwork(nodeIndex int) *NetworkDeclare {

	declare := &NetworkDeclare{
		Version:   2,
		Ethernets: make(map[string]*NetworkAdapter, len(net.Interfaces)),
	}

	for _, n := range net.Interfaces {
		if len(n.NicName) > 0 {
			var ethernet *NetworkAdapter
			var macAddress = n.GetMacAddress(nodeIndex)

			if n.DHCP || len(n.IPAddress) == 0 {
				ethernet = &NetworkAdapter{
					DHCP4: n.DHCP,
				}

				if !n.UseRoutes {
					dhcpOverrides := map[string]interface{}{
						"use-routes": false,
					}
					ethernet.DHCPOverrides = &dhcpOverrides
				} else if len(n.Gateway) > 0 {
					ethernet.Gateway4 = &n.Gateway
				}

			} else {
				ethernet = &NetworkAdapter{
					Addresses: &[]string{
						ToCIDR(n.IPAddress, n.Netmask),
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

				if len(n.NicName) > 0 {
					ethernet.NicName = &n.NicName
				}
			} else {
				ethernet.NicName = nil
			}

			if len(n.Routes) != 0 {
				ethernet.Routes = &n.Routes
			}

			if net.DNS != nil {
				ethernet.Nameservers = &Nameserver{
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
	address := net.MacAddress

	if strings.ToLower(address) == "generate" {
		address = generateMacAddress(net.netName(nodeIndex))
	} else if strings.ToLower(address) == "ignore" {
		address = ""
	}

	net.MacAddress = address

	return address
}
