package cloudstack

import "github.com/Fred78290/kubernetes-cloud-autoscaler/providers"

type cloudstackNetwork struct {
	*providers.Network
}

func (net *cloudstackNetwork) Clone(controlPlane bool, nodeIndex int) (copy *cloudstackNetwork) {
	copy = &cloudstackNetwork{
		Network: net.Network.Clone(controlPlane, nodeIndex),
	}

	return
}
