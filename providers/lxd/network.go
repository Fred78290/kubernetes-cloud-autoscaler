package lxd

import (
	"bytes"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/canonical/lxd/shared/api"
	"gopkg.in/yaml.v2"
)

type TypeConfig struct {
	Type string `default:"physical" json:"type,omitempty" yaml:"type,omitempty"`
}

type SubnetConfig struct {
	Type    string `default:"dhcp" json:"type,omitempty" yaml:"type,omitempty"`
	Address string `json:"address,omitempty" yaml:"address,omitempty"`
	Gateway string `json:"gateway,omitempty" yaml:"gateway,omitempty"`
}

type NetworkConfig struct {
	TypeConfig
	Name    string
	Subnets []SubnetConfig
}

type RouteConfig struct {
	TypeConfig
	Destination string `json:"destination,omitempty" yaml:"destination,omitempty"`
	Gateway     string `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	Metric      *int   `json:"metric,omitempty" yaml:"metric,omitempty"`
}

type NetworkDeclare struct {
	Version int   `default:"1" json:"version,omitempty" yaml:"version,omitempty"`
	Config  []any `json:"config,omitempty" yaml:"ethernets,omitempty"`
}

type lxdNetwork struct {
	*providers.Network
	LxdInterfaces []*lxdNetworkInterface
}

type lxdNetworkInterface struct {
	*providers.NetworkInterface
	gateway string
	network *api.Network
}

func (net *lxdNetwork) Clone(controlPlane bool, nodeIndex int) (copy *lxdNetwork) {
	copy = &lxdNetwork{
		Network:       net.Network.Clone(controlPlane, nodeIndex),
		LxdInterfaces: make([]*lxdNetworkInterface, 0, len(net.Interfaces)),
	}

	for index, inf := range copy.Interfaces {
		openstackInterface := &lxdNetworkInterface{
			NetworkInterface: inf,
			gateway:          net.LxdInterfaces[index].gateway,
			network:          net.LxdInterfaces[index].network,
		}

		copy.LxdInterfaces = append(copy.LxdInterfaces, openstackInterface)
	}

	return
}

func (vnet *lxdNetwork) PrimaryInterface() *lxdNetworkInterface {
	for _, n := range vnet.LxdInterfaces {
		if n.IsEnabled() && n.Primary {
			return n
		}
	}
	return nil
}

// GetCloudInitNetwork create cloud-init object
func (vnet *lxdNetwork) GetCloudInitNetwork(useMacAddress bool) *NetworkDeclare {
	declare := &NetworkDeclare{
		Version: 1,
	}

	for _, n := range vnet.LxdInterfaces {
		if n.IsEnabled() {
			if len(n.NicName) > 0 {
				ethernet := NetworkConfig{
					TypeConfig: TypeConfig{
						Type: "physical",
					},
					Name: n.NetworkName,
				}

				if n.DHCP || len(n.IPAddress) == 0 {
					ethernet.Subnets = append(ethernet.Subnets, SubnetConfig{
						Type: "dhcp",
					})
				} else {
					ethernet.Subnets = append(ethernet.Subnets, SubnetConfig{
						Type:    "static",
						Address: cloudinit.ToCIDR(n.IPAddress, n.Netmask),
						Gateway: n.gateway,
					})
				}

				declare.Config = append(declare.Config, &ethernet)
			}

			for _, r := range n.Routes {
				route := RouteConfig{
					TypeConfig: TypeConfig{
						Type: "route",
					},
					Destination: r.To,
					Gateway:     r.Via,
					Metric:      &r.Metric,
				}

				declare.Config = append(declare.Config, &route)
			}
		}
	}

	return declare
}

func (net *NetworkDeclare) encodeCloudInit() string {
	var out bytes.Buffer

	wr := yaml.NewEncoder(&out)
	err := wr.Encode(net)
	wr.Close()

	if err != nil {
		return ""
	}

	return out.String()
}
