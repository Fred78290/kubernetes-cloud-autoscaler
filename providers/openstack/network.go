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

	opts := zones.ListOpts{
		Name: net.Domain + ".",
	}

	if allPages, err = zones.List(client, opts).AllPages(ctx); err == nil {
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
		var dnsEntryID string

		if dnsEntryID, err = net.findDNSEntry(ctx, client, name, "A"); err == nil {
			opts := recordsets.UpdateOpts{
				Records: []string{
					address,
				},
			}

			if record, err = recordsets.Update(ctx, client, *net.dnsZoneID, dnsEntryID, opts).Extract(); err == nil {
				net.dnsEntryID = aws.String(record.ID)
			}
		} else {
			opts := recordsets.CreateOpts{
				Name: name + "." + net.Domain + ".",
				Type: "A",
				Records: []string{
					address,
				},
			}

			if record, err = recordsets.Create(ctx, client, *net.dnsZoneID, opts).Extract(); err == nil {
				net.dnsEntryID = aws.String(record.ID)
			}
		}
	}

	return
}

func (net *openStackNetwork) findDNSEntry(ctx *context.Context, client *gophercloud.ServiceClient, name string, recordType string) (dnsEntryID string, err error) {
	var allPages pagination.Page
	var allRecords []recordsets.RecordSet

	opts := recordsets.ListOpts{
		Name:   name + "." + net.Domain + ".",
		ZoneID: *net.dnsZoneID,
		Type:   recordType,
	}

	if allPages, err = recordsets.ListByZone(client, *net.dnsZoneID, opts).AllPages(ctx); err == nil {
		if allRecords, err = recordsets.ExtractRecordSets(allPages); err == nil {
			for _, record := range allRecords {
				dnsEntryID = record.ID
				break
			}
		}
	}

	return
}

func (net *openStackNetwork) unregisterDNS(ctx *context.Context, client *gophercloud.ServiceClient, name, _ string) (err error) {
	if client != nil && net.dnsZoneID != nil {
		var dnsEntryID string

		if net.dnsEntryID != nil {
			dnsEntryID = *net.dnsEntryID
		} else if dnsEntryID, err = net.findDNSEntry(ctx, client, name, "A"); err != nil {
			return
		}

		net.dnsEntryID = nil

		err = recordsets.Delete(ctx, client, *net.dnsZoneID, dnsEntryID).ExtractErr()
	}

	return
}

func (net *openStackNetwork) retrieveNetworkInfos(ctx *context.Context, client *gophercloud.ServiceClient, name string) (err error) {
	if client != nil && net.dnsZoneID != nil {
		var dnsEntryID string

		if dnsEntryID, err = net.findDNSEntry(ctx, client, name, "A"); err != nil {
			return
		}

		net.dnsEntryID = aws.String(dnsEntryID)
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
