package openstack

import (
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/dns/v2/recordsets"
	"github.com/gophercloud/gophercloud/v2/openstack/dns/v2/zones"
	"github.com/gophercloud/gophercloud/v2/pagination"
)

type openStackNetwork struct {
	*providers.Network
	dnsZoneID           *string
	dnsEntryID          *string
	floatingIP          *string
	OpenstackInterfaces []openstackNetworkInterface
}

type openstackNetworkInterface struct {
	*providers.NetworkInterface
	networkID string
}

func (net *openStackNetwork) ConfigurationDns(ctx *context.Context, client *gophercloud.ServiceClient) (err error) {
	var allPages pagination.Page
	var allZones []zones.Zone

	if allPages, err = zones.List(client, zones.ListOpts{Name: net.Domain + "."}).AllPages(ctx); err == nil {
		if allZones, err = zones.ExtractZones(allPages); err == nil && len(allZones) > 0 {
			net.dnsZoneID = aws.String(allZones[0].ID)
		}
	}

	return
}

func (net *openStackNetwork) Clone(controlPlane bool, nodeIndex int) (copy *openStackNetwork) {
	copy = &openStackNetwork{
		Network:             net.Network.Clone(controlPlane, nodeIndex),
		dnsZoneID:           net.dnsZoneID,
		OpenstackInterfaces: make([]openstackNetworkInterface, 0, len(net.Interfaces)),
	}

	for index, inf := range copy.Interfaces {
		openstackInterface := openstackNetworkInterface{
			NetworkInterface: inf,
			networkID:        net.OpenstackInterfaces[index].networkID,
		}

		copy.OpenstackInterfaces = append(copy.OpenstackInterfaces, openstackInterface)
	}

	return
}

func (net *openStackNetwork) registerDNS(ctx *context.Context, client *gophercloud.ServiceClient, name, address string) (err error) {
	if client != nil && net.dnsZoneID != nil {
		var record *recordsets.RecordSet

		opts := recordsets.CreateOpts{
			Name: name,
			Records: []string{
				address,
			},
			Type: "A",
		}

		if record, err = recordsets.Create(ctx, client, *net.dnsZoneID, opts).Extract(); err == nil {
			net.dnsEntryID = aws.String(record.ID)
		}
	}

	return
}

func (net *openStackNetwork) unregisterDNS(ctx *context.Context, client *gophercloud.ServiceClient, name, _ string) (err error) {
	if client != nil && net.dnsZoneID != nil {
		var allPages pagination.Page
		var allRecords []recordsets.RecordSet

		if net.dnsEntryID != nil {
			err = recordsets.Delete(ctx, client, *net.dnsZoneID, *net.dnsEntryID).ExtractErr()
		} else if allPages, err = recordsets.ListByZone(client, *net.dnsZoneID, recordsets.ListOpts{Name: name + "."}).AllPages(ctx); err == nil {
			if allRecords, err = recordsets.ExtractRecordSets(allPages); err == nil {
				for _, record := range allRecords {
					err = recordsets.Delete(ctx, client, *net.dnsZoneID, record.ID).ExtractErr()
				}
			}
		}

		net.dnsEntryID = nil
	}

	return
}

func (net *openStackNetwork) toOpenstackNetwork() (network []servers.Network) {
	network = make([]servers.Network, 0, len(net.Interfaces))

	for _, inf := range net.OpenstackInterfaces {
		if inf.IsEnabled() {
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
