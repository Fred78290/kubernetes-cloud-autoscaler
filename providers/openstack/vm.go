package openstack

import (
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	glog "github.com/sirupsen/logrus"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/pagination"
)

type instanceStatus struct {
	address string
	powered bool
}

type OpenStackServer struct {
	servers.Server
}

type ServerInstance struct {
	*openstackWrapper
	NodeIndex      int
	InstanceName   string
	PrivateDNSName string
	InstanceID     string
	Region         string
	Zone           string
	AddressIP      string
}

func (status *instanceStatus) Address() string {
	return status.address
}

func (status *instanceStatus) Powered() bool {
	return status.powered
}

// WaitForIP wait ip a VM by name
func (instance *ServerInstance) WaitForIP(callback providers.CallbackWaitSSHReady) (address string, err error) {
	glog.Debugf("WaitForIP: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	if err = context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
		var server *servers.Server

		ctx := context.NewContext(instance.Timeout)
		defer ctx.Cancel()

		if server, err = instance.getServer(ctx); err != nil {
			return false, err
		}

		status := strings.ToUpper(server.Status)

		if status == "ACTIVE" {

			glog.Debugf("WaitForIP: instance %s id (%s), using IP: %s", instance.InstanceName, server.ID, instance.AddressIP)

			if err = callback.WaitSSHReady(instance.InstanceName, instance.AddressIP); err != nil {
				return false, err
			}

			return true, nil
		} else if status != "IN_PROGRESS" {
			return false, fmt.Errorf(constantes.ErrWrongStateMachine, server.Status, instance.InstanceName)
		}

		return false, nil
	}); err != nil {
		return "", err
	}

	return
}

func (instance *ServerInstance) PowerOn(ctx *context.Context) (err error) {
	glog.Debugf("PowerOn: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	if err = servers.Start(ctx, instance.computeClient, instance.InstanceID).ExtractErr(); err == nil {
		err = context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
			var server *servers.Server

			if server, err = instance.getServer(ctx); err != nil {
				return false, err
			}

			status := strings.ToUpper(server.Status)

			if status == "ACTIVE" {
				return true, nil
			} else if status != "IN_PROGRESS" {
				return false, fmt.Errorf(constantes.ErrWrongStateMachine, server.Status, instance.InstanceName)
			}

			return false, nil
		})
	}

	return err
}

func (instance *ServerInstance) PowerOff(ctx *context.Context) (err error) {
	glog.Debugf("PowerOff: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	if err = servers.Stop(ctx, instance.computeClient, instance.InstanceID).ExtractErr(); err == nil {
		err = context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
			var server *servers.Server

			if server, err = instance.getServer(ctx); err != nil {
				return false, err
			}

			status := strings.ToUpper(server.Status)

			if status == "SHUTOFF" {
				return true, nil
			} else if status != "IN_PROGRESS" {
				return false, fmt.Errorf(constantes.ErrWrongStateMachine, server.Status, instance.InstanceName)
			}

			return false, nil
		})
	}

	return err
}

func (instance *ServerInstance) ShutdownGuest() (err error) {
	ctx := context.NewContext(instance.Timeout)
	defer ctx.Cancel()

	return instance.PowerOff(ctx)
}

func (instance *ServerInstance) Delete() (err error) {
	glog.Debugf("Delete: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	ctx := context.NewContext(instance.Timeout)
	defer ctx.Cancel()

	if err = servers.Delete(ctx, instance.computeClient, instance.InstanceID).ExtractErr(); err == nil {
		if instance.network.floatingIP != nil {
			err = floatingips.Delete(ctx, instance.computeClient, *instance.network.floatingIP).ExtractErr()
		}
	}

	return
}

func (instance *ServerInstance) Status() (status providers.InstanceStatus, err error) {
	glog.Debugf("Status: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	var server *servers.Server
	var addressIP string

	ctx := context.NewContext(instance.Timeout)
	defer ctx.Cancel()

	if server, err = instance.getServer(ctx); err != nil {
		return
	}

	if addressIP, err = instance.getAddress(ctx, server); err != nil {
		return
	}

	status = &instanceStatus{
		address: addressIP,
		powered: strings.ToUpper(server.Status) == "ACTIVE",
	}

	return
}

// WaitForPowered wait ip a VM by name
func (instance *ServerInstance) WaitForPowered() error {
	glog.Debugf("WaitForPowered: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	return context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
		var err error
		var server *servers.Server

		ctx := context.NewContext(instance.Timeout)
		defer ctx.Cancel()

		if server, err = instance.getServer(ctx); err != nil {
			glog.Debugf("WaitForPowered: instance %s id (%s), got an error %v", instance.InstanceName, instance.InstanceID, err)

			return false, err
		}

		status := strings.ToUpper(server.Status)

		if status != "ACTIVE" {
			if status == "IN_PROGRESS" {
				return false, nil
			}

			glog.Debugf("WaitForPowered: instance %s id (%s), unexpected state: %s", instance.InstanceName, instance.InstanceID, status)

			return false, fmt.Errorf(constantes.ErrWrongStateMachine, instance.InstanceName, instance.InstanceName)
		}

		glog.Debugf("WaitForPowered: ready instance %s id (%s)", instance.InstanceName, instance.InstanceID)

		return true, nil
	})
}

func (instance *ServerInstance) getServer(ctx *context.Context) (*servers.Server, error) {
	return servers.Get(ctx, instance.computeClient, instance.InstanceID).Extract()
}

// Create will create a named VM not powered
// memory and disk are in megabytes
func (instance *ServerInstance) Create(controlPlane bool, nodeGroup, flavorRef string, userData string, diskSize int) (err error) {
	var server *servers.Server
	var securityGroup string
	var useFloatingIP bool
	var floatingNetworkID string

	ctx := context.NewContext(instance.Timeout)
	defer ctx.Cancel()

	if controlPlane {
		securityGroup = instance.Configuration.Network.SecurityGroup.ControlPlaneNode
	} else {
		securityGroup = instance.Configuration.Network.SecurityGroup.WorkerNode
	}

	if instance.Configuration.Network.FloatingInfos != nil {
		floatingInfos := instance.Configuration.Network.FloatingInfos
		floatingNetworkID = floatingInfos.FloatingIPNetwork

		if controlPlane && floatingInfos.ControlPlaneNode {
			useFloatingIP = true
		} else if !controlPlane && floatingInfos.WorkerNode {
			useFloatingIP = true
		}
	}

	opts := servers.CreateOpts{
		Name:             instance.InstanceName,
		FlavorRef:        flavorRef,
		UserData:         []byte(userData),
		ImageRef:         instance.Configuration.Image,
		AvailabilityZone: instance.Configuration.OpenStackZone,
		AccessIPv4:       instance.network.PrimaryAddressIP(),
		Networks:         instance.network.toOpenstackNetwork(),
		SecurityGroups:   []string{securityGroup},
		Min:              1,
		Max:              1,
	}

	if server, err = servers.Create(ctx, instance.computeClient, opts).Extract(); err != nil {
		err = fmt.Errorf("server creation failed for: %s, reason: %v", instance.InstanceName, err)
	} else if instance.AddressIP, err = instance.getAddress(ctx, server); err != nil {
		err = fmt.Errorf("unable to get ip address for server: %s, reason: %v", instance.InstanceName, err)
	} else {
		instance.InstanceID = server.ID

		if useFloatingIP {

			var floatingIP *floatingips.FloatingIP
			var allPages pagination.Page
			var allPorts []ports.Port

			glog.Debugf("create floating ip for server: %s", instance.InstanceName)

			opts := ports.ListOpts{
				DeviceID: server.ID,
			}

			if allPages, err = ports.List(instance.computeClient, opts).AllPages(ctx); err != nil {
				err = fmt.Errorf("unable to list port for server: %s, named: %s. Reason: %v", instance.InstanceName, server.ID, err)
			} else if allPorts, err = ports.ExtractPorts(allPages); err != nil {
				err = fmt.Errorf("unable to extract port for server: %s, named: %s. Reason: %v", instance.InstanceName, server.ID, err)
			} else if len(allPorts) == 0 {
				err = fmt.Errorf("unable to find port for server: %s, named: %s. Reason: %v", instance.InstanceName, server.ID, err)
			} else {
				opts := floatingips.CreateOpts{
					Description:       fmt.Sprintf("Floating ip for: %s", instance.InstanceName),
					FloatingNetworkID: floatingNetworkID,
					PortID:            allPorts[0].ID,
				}

				if floatingIP, err = floatingips.Create(ctx, instance.computeClient, opts).Extract(); err != nil {
					err = fmt.Errorf("unable to create floating ip for server: %s, named: %s. Reason: %v", instance.InstanceName, server.ID, err)
				} else {
					instance.network.floatingIP = aws.String(floatingIP.ID)
					instance.AddressIP = floatingIP.FloatingIP
				}
			}
		}
	}

	return
}
