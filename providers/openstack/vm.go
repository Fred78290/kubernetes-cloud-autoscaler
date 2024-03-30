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
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/availabilityzones"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/extendedserverattributes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/extendedstatus"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/startstop"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/pagination"
)

type instanceStatus struct {
	address string
	powered bool
}

type OpenStackServer struct {
	servers.Server
	extendedstatus.ServerExtendedStatusExt
	availabilityzones.AvailabilityZone
	extendedserverattributes.ServerAttributesExt
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

		if server, err = instance.getServer(); err != nil {
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

func (instance *ServerInstance) PowerOn() (err error) {
	glog.Debugf("PowerOn: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	if err = startstop.Start(instance.computeClient, instance.InstanceID).ExtractErr(); err == nil {
		err = context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
			var server *servers.Server

			if server, err = instance.getServer(); err != nil {
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

func (instance *ServerInstance) PowerOff() (err error) {
	glog.Debugf("PowerOff: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	if err = startstop.Start(instance.computeClient, instance.InstanceID).ExtractErr(); err == nil {
		err = context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
			var server *servers.Server

			if server, err = instance.getServer(); err != nil {
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
	return instance.PowerOff()
}

func (instance *ServerInstance) Delete() (err error) {
	glog.Debugf("Delete: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	if err = servers.Delete(instance.computeClient, instance.InstanceID).ExtractErr(); err == nil {
		if instance.Network.floatingIP != nil {
			err = floatingips.Delete(instance.computeClient, *instance.Network.floatingIP).ExtractErr()
		}
	}

	return
}

func (instance *ServerInstance) Status() (status providers.InstanceStatus, err error) {
	glog.Debugf("Status: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	var server *servers.Server
	var addressIP string

	if server, err = instance.getServer(); err != nil {
		return
	}

	if addressIP, err = instance.getAddress(server); err != nil {
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

		if server, err = instance.getServer(); err != nil {
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

func (instance *ServerInstance) getServer() (*servers.Server, error) {
	return servers.Get(instance.computeClient, instance.InstanceID).Extract()
}

// Create will create a named VM not powered
// memory and disk are in megabytes
func (instance *ServerInstance) Create(controlPlane bool, nodeGroup, flavorRef string, userData string, diskSize int) (err error) {
	var server *servers.Server
	var securityGroup string
	var useFloatingIP bool
	var floatingNetworkID string

	if instance.Network.FloatingInfos != nil {
		floatingInfos := instance.Network.FloatingInfos
		floatingNetworkID = floatingInfos.FloatingIPNetwork

		if controlPlane {
			securityGroup = floatingInfos.ControlPlaneNode.SecurityGroup
		} else {
			securityGroup = floatingInfos.WorkerNode.SecurityGroup
		}

		if controlPlane && floatingInfos.ControlPlaneNode.UseFloatingIP {
			useFloatingIP = true
		} else if !controlPlane && floatingInfos.WorkerNode.UseFloatingIP {
			useFloatingIP = true
		}
	}

	opts := servers.CreateOpts{
		Name:             instance.InstanceName,
		FlavorRef:        flavorRef,
		UserData:         []byte(userData),
		ImageRef:         instance.Configuration.Image,
		AvailabilityZone: instance.Configuration.OpenStackZone,
		AccessIPv4:       instance.Network.PrimaryAddressIP(),
		Networks:         instance.Network.toOpenstackNetwork(),
		SecurityGroups:   []string{securityGroup},
		Min:              1,
		Max:              1,
	}

	if server, err = servers.Create(instance.computeClient, opts).Extract(); err != nil {
		err = fmt.Errorf("server creation failed for: %s, reason: %v", instance.InstanceName, err)
	} else if instance.AddressIP, err = instance.getAddress(server); err != nil {
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

			if allPages, err = ports.List(instance.computeClient, opts).AllPages(); err != nil {
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

				if floatingIP, err = floatingips.Create(instance.computeClient, opts).Extract(); err != nil {
					err = fmt.Errorf("unable to create floating ip for server: %s, named: %s. Reason: %v", instance.InstanceName, server.ID, err)
				} else {
					instance.Network.floatingIP = aws.String(floatingIP.ID)
					instance.AddressIP = floatingIP.FloatingIP
				}
			}
		}
	}

	return
}
