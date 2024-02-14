package desktop

import (
	"github.com/Fred78290/kubernetes-cloud-autoscaler/api"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
)

// VirtualMachine virtual machine wrapper
type VirtualMachine struct {
	Name   string
	Uuid   string
	Vmx    string
	Vcpus  int32
	Memory int64
}

func BuildNetworkInterface(network *providers.Network) []*api.NetworkInterface {

	result := make([]*api.NetworkInterface, 0, len(network.Interfaces))

	for _, inf := range network.Interfaces {
		if inf.Enabled {
			result = append(result, &api.NetworkInterface{
				Macaddress:  inf.GetMacAddress(),
				Vnet:        inf.NetworkName,
				Type:        inf.ConnectionType,
				Device:      inf.Adapter,
				BsdName:     inf.BsdName,
				DisplayName: inf.DisplayName,
			})
		}
	}
	return result
}
