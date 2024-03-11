package providers

import (
	"crypto/rand"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/praserx/ipconv"
	glog "github.com/sirupsen/logrus"
)

type NetworkInterface struct {
	Enabled        bool                     `default:true json:"enabled,omitempty" yaml:"primary,omitempty"`
	Primary        bool                     `json:"primary,omitempty" yaml:"primary,omitempty"`
	Existing       bool                     `default:true json:"exists,omitempty" yaml:"exists,omitempty"`
	ConnectionType string                   `default:"nat" json:"type,omitempty" yaml:"type,omitempty"`
	BsdName        string                   `json:"bsd-name,omitempty" yaml:"bsd-name,omitempty"`
	DisplayName    string                   `json:"display-name,omitempty" yaml:"display-name,omitempty"`
	NetworkName    string                   `json:"network,omitempty" yaml:"network,omitempty"`
	Adapter        string                   `json:"adapter,omitempty" yaml:"adapter,omitempty"`
	MacAddress     string                   `json:"mac-address,omitempty" yaml:"mac-address,omitempty"`
	NicName        string                   `json:"nic,omitempty" yaml:"nic,omitempty"`
	DHCP           bool                     `json:"dhcp,omitempty" yaml:"dhcp,omitempty"`
	UseRoutes      bool                     `default:true json:"use-dhcp-routes,omitempty" yaml:"use-dhcp-routes,omitempty"`
	IPAddress      string                   `json:"address,omitempty" yaml:"address,omitempty"`
	Netmask        string                   `json:"netmask,omitempty" yaml:"netmask,omitempty"`
	Gateway        string                   `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	Routes         []v1alpha1.NetworkRoutes `json:"routes,omitempty" yaml:"routes,omitempty"`
	nodeIndex      int
}

type Network struct {
	Domain       string                   `json:"domain,omitempty" yaml:"domain,omitempty"`
	Interfaces   []*NetworkInterface      `json:"interfaces,omitempty" yaml:"interfaces,omitempty"`
	DNS          *cloudinit.NetworkResolv `json:"dns,omitempty" yaml:"dns,omitempty"`
	nodeIndex    int
	controlPlane bool
}

type MacAddress struct {
	sync.Mutex

	Addresses map[string]string `json:"mac-addresses,omitempty"`
}

var macAddreses = newMacAddress()

func newMacAddress() *MacAddress {
	return &MacAddress{
		Addresses: map[string]string{},
	}
}

func stringBefore(str string, char string) string {
	if index := strings.Index(str, char); index >= 0 {
		return str[:index]
	} else {
		return str
	}
}

func stringAfter(str string, char string) string {
	if index := strings.LastIndex(str, char); index >= 0 {
		return str[index+1:]
	} else {
		return ""
	}
}

func (addr *MacAddress) setMacAddress(netName, address string) {
	addr.Lock()
	defer addr.Unlock()

	addr.Addresses[netName] = address

	glog.Debugf("setMacAddress: %s = %s", netName, address)
}

func (addr *MacAddress) generateMacAddress(netName string) string {
	var address string
	var found bool

	addr.Lock()
	defer addr.Unlock()

	if address, found = addr.Addresses[netName]; !found {
		buf := make([]byte, 3)

		if _, err := rand.Read(buf); err != nil {
			return ""
		}

		address = fmt.Sprintf("00:16:3e:%02x:%02x:%02x", buf[0], buf[1], buf[2])

		addr.Addresses[netName] = address

		glog.Debugf("generateMacAddress: %s = %s", netName, address)
	}

	return address
}

func (vnet *Network) Clone(controlPlane bool, nodeIndex int) *Network {
	copy := &Network{
		Domain:       vnet.Domain,
		DNS:          vnet.DNS,
		Interfaces:   make([]*NetworkInterface, len(vnet.Interfaces)),
		controlPlane: controlPlane,
		nodeIndex:    nodeIndex,
	}

	for index, inet := range vnet.Interfaces {
		address := inet.IPAddress

		if !controlPlane && nodeIndex > 0 {
			if !inet.DHCP || len(address) > 0 {
				if ip := net.ParseIP(address).To4(); ip != nil {
					if ipv4, err := ipconv.IPv4ToInt(net.ParseIP(address).To4()); err == nil {
						address = ipconv.IntToIPv4(ipv4 + uint32(nodeIndex-1)).String()
					}
				}
			}
		}

		copy.Interfaces[index] = &NetworkInterface{
			Enabled:        inet.Enabled,
			Primary:        inet.Primary,
			Existing:       inet.Existing,
			ConnectionType: inet.ConnectionType,
			BsdName:        inet.BsdName,
			DisplayName:    inet.DisplayName,
			NetworkName:    inet.NetworkName,
			Adapter:        inet.Adapter,
			MacAddress:     inet.MacAddress,
			NicName:        inet.NicName,
			DHCP:           inet.DHCP,
			UseRoutes:      inet.UseRoutes,
			IPAddress:      address,
			Netmask:        inet.Netmask,
			Gateway:        inet.Gateway,
			Routes:         inet.Routes,
			nodeIndex:      nodeIndex,
		}
	}

	return copy
}

func (vnet *Network) PrimaryAddressIP() (address string) {
	for _, n := range vnet.Interfaces {
		if n.Enabled && n.Primary {
			if !n.DHCP || len(n.IPAddress) > 0 {
				address = n.IPAddress
			}
			break
		}
	}
	return address
}

// GetCloudInitNetwork create cloud-init object
func (vnet *Network) GetCloudInitNetwork(useMacAddress bool) *cloudinit.NetworkDeclare {

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
				macAddress := ""
				address := n.IPAddress

				if useMacAddress {
					macAddress = n.GetMacAddress()
				}
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

				if len(address) > 0 {
					if len(label) > 0 {
						addr := map[string]any{}
						addr[cloudinit.ToCIDR(address, n.Netmask)] = map[string]any{
							"label": n.NicName,
						}

						ethernet = &cloudinit.NetworkAdapter{
							Addresses: &[]any{
								addr,
							},
						}

					} else {
						ethernet = &cloudinit.NetworkAdapter{
							Addresses: &[]any{
								cloudinit.ToCIDR(address, n.Netmask),
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
	for _, inet := range vnet.Interfaces {
		if inet.Enabled && inet.Existing {
			infs = append(infs, inet)
		}
	}

	return infs
}

func (vnet *Network) UpdateMacAddressTable() error {
	for _, inet := range vnet.Interfaces {
		inet.nodeIndex = vnet.nodeIndex

		if inet.Enabled {
			inet.updateMacAddressTable()
		}
	}

	return nil
}

func (vnet *Network) InterfaceByName(networkName string) *NetworkInterface {
	for _, inet := range vnet.Interfaces {
		if inet.Enabled && inet.NetworkName == networkName {
			return inet
		}
	}

	return nil
}

func (vnet *Network) ConfigurationDidLoad() {
	for _, inet := range vnet.Interfaces {
		if strings.ToLower(inet.IPAddress) == "none" {
			inet.IPAddress = ""
			inet.Enabled = false
		} else if strings.ToLower(inet.IPAddress) == "dhcp" {
			inet.DHCP = true
			inet.IPAddress = ""
		}
	}
}

func (vnet *Network) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
	if len(network.VMWare) > 0 {
		for _, network := range network.VMWare {
			if inet := vnet.InterfaceByName(network.NetworkName); inet != nil {
				inet.Enabled = network.Enabled

				if inet.Enabled {
					inet.DHCP = network.DHCP
					inet.UseRoutes = network.UseRoutes
					inet.Routes = network.Routes

					if strings.ToLower(network.IPV4Address) == "none" {
						inet.Enabled = false
						inet.IPAddress = ""
					} else if strings.ToLower(network.IPV4Address) == "dhcp" {
						inet.DHCP = true
						inet.IPAddress = ""
					} else if len(network.IPV4Address) > 0 {
						inet.IPAddress = network.IPV4Address
					}

					if len(network.Adapter) > 0 {
						inet.Adapter = network.Adapter
					}

					if len(network.Netmask) > 0 {
						inet.Netmask = network.Netmask
					}

					if len(network.Gateway) > 0 {
						inet.Gateway = network.Gateway
					}

					if len(network.MacAddress) > 0 {
						inet.MacAddress = network.MacAddress
					}
				} else {
					glog.Warnf(constantes.WarnNetworkInterfaceIsDisabled, network.NetworkName)
				}
			} else {
				glog.Errorf(constantes.ErrNetworkInterfaceNotFoundToConfigure, network.NetworkName)
			}
		}
	}
}

func (inet *NetworkInterface) netName() string {
	return fmt.Sprintf("%s-%d", inet.NicName, inet.nodeIndex)
}

func (inet *NetworkInterface) updateMacAddressTable() {
	address := inet.MacAddress

	if len(address) > 0 && strings.ToLower(address) != "generate" && strings.ToLower(address) != "ignore" {
		macAddreses.setMacAddress(inet.netName(), address)
	}
}

func (inet *NetworkInterface) AttachMacAddress(address string) {
	macAddreses.setMacAddress(inet.netName(), address)
}

// GetMacAddress return a macaddress
func (inet *NetworkInterface) GetMacAddress() string {
	if inet.nodeIndex < 0 || !inet.Enabled {
		return ""
	}

	address := inet.MacAddress

	if strings.ToLower(address) == "generate" {
		address = macAddreses.generateMacAddress(inet.netName())
	} else if strings.ToLower(address) == "ignore" {
		address = ""
	}

	inet.MacAddress = address

	return address
}
