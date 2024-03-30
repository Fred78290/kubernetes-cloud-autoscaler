package openstack

import (
	"fmt"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/dns/v2/recordsets"
	"github.com/gophercloud/gophercloud/openstack/dns/v2/zones"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/gophercloud/utils/openstack/networking/v2/networks"
)

type FloatingNetworkInfo struct {
	SecurityGroup string `json:"security-group,omitempty"`
	UseFloatingIP bool   `json:"use-floating-ip,omitempty"`
}

type FloatingNetworkInfos struct {
	FloatingIPNetwork string              `json:"floating-ip-network,omitempty"`
	ControlPlaneNode  FloatingNetworkInfo `json:"floating-control-plane,omitempty"`
	WorkerNode        FloatingNetworkInfo `json:"floating-worker-node,omitempty"`
}

type OpenStackNetwork struct {
	*providers.Network
	FloatingInfos       *FloatingNetworkInfos `json:"floating-ip,omitempty"`
	dnsZoneID           *string
	dnsEntryID          *string
	floatingIP          *string
	openstackInterfaces []openstackNetworkInterface
}

type openstackNetworkInterface struct {
	*providers.NetworkInterface
	networkID string
}

func (net *OpenStackNetwork) ConfigurationDns(client *gophercloud.ServiceClient) (err error) {
	var allPages pagination.Page
	var allZones []zones.Zone

	if allPages, err = zones.List(client, zones.ListOpts{Name: net.Domain}).AllPages(); err == nil {
		if allZones, err = zones.ExtractZones(allPages); err == nil && len(allZones) > 0 {
			net.dnsZoneID = aws.String(allZones[0].ID)
		}
	}

	return
}

func (net *OpenStackNetwork) ConfigurationDidLoad(client *gophercloud.ServiceClient) (err error) {
	net.Network.ConfigurationDidLoad()

	if net.FloatingInfos != nil {
		floatingInfos := net.FloatingInfos

		if floatingInfos.FloatingIPNetwork == "" && (floatingInfos.ControlPlaneNode.UseFloatingIP || floatingInfos.WorkerNode.UseFloatingIP) {
			return fmt.Errorf("floating network is not defined")
		}

		if floatingInfos.FloatingIPNetwork != "" {
			floatingIPNetwork := ""

			if floatingIPNetwork, err = networks.IDFromName(client, floatingInfos.FloatingIPNetwork); err != nil {
				return fmt.Errorf("the floating ip network: %s not found, reason: %v", floatingInfos.FloatingIPNetwork, err)
			}

			floatingInfos.FloatingIPNetwork = floatingIPNetwork
		}
	}

	net.openstackInterfaces = make([]openstackNetworkInterface, 0, len(net.Network.Interfaces))

	for _, inf := range net.Network.Interfaces {
		openstackInterface := openstackNetworkInterface{
			NetworkInterface: inf,
		}

		if openstackInterface.networkID, err = networks.IDFromName(client, inf.NetworkName); err != nil {
			err = fmt.Errorf("unable to find network: %s, reason: %v", inf.NetworkName, err)
			break
		}

		net.openstackInterfaces = append(net.openstackInterfaces, openstackInterface)
	}

	return
}

func (net *OpenStackNetwork) Clone(controlPlane bool, nodeIndex int) (copy *OpenStackNetwork) {
	copy = &OpenStackNetwork{
		Network:             net.Network.Clone(controlPlane, nodeIndex),
		FloatingInfos:       net.FloatingInfos,
		dnsZoneID:           net.dnsZoneID,
		openstackInterfaces: make([]openstackNetworkInterface, 0, len(net.openstackInterfaces)),
	}

	for index, inf := range net.Interfaces {
		openstackInterface := openstackNetworkInterface{
			NetworkInterface: inf,
			networkID:        net.openstackInterfaces[index].networkID,
		}

		net.openstackInterfaces = append(net.openstackInterfaces, openstackInterface)
	}

	return
}

func (net *OpenStackNetwork) registerDNS(client *gophercloud.ServiceClient, name, address string) (err error) {
	if client != nil && net.dnsZoneID != nil {
		var record *recordsets.RecordSet

		opts := recordsets.CreateOpts{
			Name: name,
			Records: []string{
				address,
			},
			Type: "A",
		}

		if record, err = recordsets.Create(client, *net.dnsZoneID, opts).Extract(); err == nil {
			net.dnsEntryID = aws.String(record.ID)
		}
	}

	return
}

func (net *OpenStackNetwork) unregisterDNS(client *gophercloud.ServiceClient, name, address string) (err error) {
	if client != nil && net.dnsZoneID != nil && net.dnsEntryID != nil {
		err = recordsets.Delete(client, *net.dnsZoneID, *net.dnsEntryID).ExtractErr()

		net.dnsEntryID = nil
	}

	return
}

func (net *OpenStackNetwork) toOpenstackNetwork() (network []servers.Network) {
	network = make([]servers.Network, 0, len(net.Interfaces))

	for _, inf := range net.openstackInterfaces {
		if inf.Enabled {
			address := ""

			if !inf.DHCP {
				address = inf.IPAddress
			}

			network = append(network, servers.Network{
				UUID:    inf.networkID,
				FixedIP: address,
			})
		}
	}

	return
}
