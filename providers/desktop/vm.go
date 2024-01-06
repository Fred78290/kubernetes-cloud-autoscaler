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

func BuildNetworkInterface(interfaces []*providers.NetworkInterface, nodeIndex int) []*api.NetworkInterface {
	result := make([]*api.NetworkInterface, 0, len(interfaces))

	for _, inf := range interfaces {
		result = append(result, &api.NetworkInterface{
			Macaddress:  inf.GetMacAddress(nodeIndex),
			Vnet:        inf.NetworkName,
			Type:        inf.ConnectionType,
			Device:      inf.Adapter,
			BsdName:     inf.BsdName,
			DisplayName: inf.DisplayName,
		})
	}
	return result
}
