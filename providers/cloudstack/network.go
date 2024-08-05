package cloudstack

import "github.com/Fred78290/kubernetes-cloud-autoscaler/providers"

type cloudstackNetwork struct {
	*providers.Network
	CloudStackInterfaces []*cloudstackNetworkInterface
}

type cloudstackNetworkInterface struct {
	*providers.NetworkInterface
	networkID string
}

func (net *cloudstackNetwork) Clone(controlPlane bool, nodeIndex int) (copy *cloudstackNetwork) {
	copy = &cloudstackNetwork{
		Network:              net.Network.Clone(controlPlane, nodeIndex),
		CloudStackInterfaces: make([]*cloudstackNetworkInterface, 0, len(net.Interfaces)),
	}

	for index, inf := range copy.Interfaces {
		openstackInterface := &cloudstackNetworkInterface{
			NetworkInterface: inf,
			networkID:        net.CloudStackInterfaces[index].networkID,
		}

		copy.CloudStackInterfaces = append(copy.CloudStackInterfaces, openstackInterface)
	}

	return
}

func (vnet *cloudstackNetwork) PrimaryInterface() *cloudstackNetworkInterface {
	for _, n := range vnet.CloudStackInterfaces {
		if n.IsEnabled() && n.Primary {
			return n
		}
	}
	return nil
}
