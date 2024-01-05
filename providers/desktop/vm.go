package desktop

import (
	"github.com/Fred78290/kubernetes-cloud-autoscaler/api"
)

// VirtualMachine virtual machine wrapper
type VirtualMachine struct {
	Name   string
	Uuid   string
	Vmx    string
	Vcpus  int32
	Memory int64
}

func BuildNetworkInterface(interfaces []*NetworkInterface, nodeIndex int) []*api.NetworkInterface {
	result := make([]*api.NetworkInterface, 0, len(interfaces))

	for _, inf := range interfaces {
		result = append(result, &api.NetworkInterface{
			Macaddress:  inf.GetMacAddress(nodeIndex),
			Vnet:        inf.VNet,
			Type:        inf.ConnectionType,
			Device:      inf.VirtualDev,
			BsdName:     inf.BsdName,
			DisplayName: inf.DisplayName,
		})
	}
	return result
}
