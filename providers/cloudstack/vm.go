package cloudstack

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/apache/cloudstack-go/v2/cloudstack"
	glog "github.com/sirupsen/logrus"
)

type instanceStatus struct {
	address string
	powered bool
}

type ServerInstance struct {
	*cloudstackWrapper
	attachedNetwork *cloudstackNetwork
	NodeIndex       int
	InstanceName    string
	PrivateDNSName  string
	InstanceID      string
	AddressIP       string
	PublicAddress   string
	PublicAddressID string
	ZoneName        string
	HostName        string
}

func (status *instanceStatus) Address() string {
	return status.address
}

func (status *instanceStatus) Powered() bool {
	return status.powered
}

func (instance *ServerInstance) expectStatus(expected, initial string) (bool, error) {
	var server *cloudstack.VirtualMachine
	var err error

	if server, err = instance.getServer(); err != nil {
		glog.Debugf("get instance %s id (%s), got an error %v", instance.InstanceName, instance.InstanceID, err)

		return false, err
	}

	status := strings.ToUpper(server.State)

	if status == initial {
		return false, nil
	}

	if status == expected {
		glog.Debugf("ready instance %s id (%s)", instance.InstanceName, instance.InstanceID)

		return true, nil
	} else if status != "STARTING" && status != "BUILD" {
		glog.Debugf("instance %s id (%s), unexpected state: %s", instance.InstanceName, instance.InstanceID, status)

		return false, fmt.Errorf(constantes.ErrWrongStateMachine, server.State, instance.InstanceName)
	}

	return false, nil
}

// WaitForIP wait ip a VM by name
func (instance *ServerInstance) WaitForIP(callback providers.CallbackWaitSSHReady) (address string, err error) {
	glog.Debugf("WaitForIP: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	ctx := context.NewContext(instance.Timeout)
	defer ctx.Cancel()

	address = instance.AddressIP

	if err = context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (ready bool, err error) {
		if ready, err = instance.expectStatus("STARTED", ""); ready {
			glog.Debugf("WaitForIP: instance %s id (%s), using IP: %s", instance.InstanceName, instance.InstanceID, instance.AddressIP)

			if err = callback.WaitSSHReady(instance.InstanceName, instance.AddressIP); err == nil {
				return true, nil
			}
		}

		return false, err
	}); err != nil {
		return "", err
	}

	return
}

func (instance *ServerInstance) isPowered() (powered bool, err error) {
	var server *cloudstack.VirtualMachine

	if server, err = instance.getServer(); err != nil {
		return false, err
	}

	status := strings.ToUpper(server.State)

	if status == "STARTED" || status == "STARTING" {
		powered = true
	}

	return
}

func (instance *ServerInstance) PowerOn(ctx *context.Context) (err error) {
	glog.Debugf("PowerOn: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	var powered bool

	if powered, err = instance.isPowered(); err != nil || powered {
		return
	}

	if _, err = instance.client.VirtualMachine.StartVirtualMachine(instance.client.VirtualMachine.NewStartVirtualMachineParams(instance.InstanceID)); err == nil {
		err = context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
			return instance.expectStatus("STARTED", "STOPPED")
		})
	}

	return err
}

func (instance *ServerInstance) PowerOff(ctx *context.Context) (err error) {
	glog.Debugf("PowerOff: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	var powered bool

	if powered, err = instance.isPowered(); err != nil || !powered {
		return
	}

	if _, err = instance.client.VirtualMachine.StopVirtualMachine(instance.client.VirtualMachine.NewStopVirtualMachineParams(instance.InstanceID)); err == nil {
		err = context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
			return instance.expectStatus("STOPPED", "STARTED")
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

	p := instance.client.VirtualMachine.NewDestroyVirtualMachineParams(instance.InstanceID)

	p.SetExpunge(true)

	if _, err = instance.client.VirtualMachine.DestroyVirtualMachine(p); err == nil {
		if instance.PublicAddressID != "" {
			_, err = instance.client.Address.DisassociateIpAddress(instance.client.Address.NewDisassociateIpAddressParams(instance.PublicAddressID))
		}
	}

	return
}

func (instance *ServerInstance) Status() (status providers.InstanceStatus, err error) {
	glog.Debugf("Status: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	var server *cloudstack.VirtualMachine
	var addressIP string

	ctx := context.NewContext(instance.Timeout)
	defer ctx.Cancel()

	if server, err = instance.getServer(); err != nil {
		return
	}

	if addressIP, err = instance.getAddress(server); err != nil {
		return
	}

	status = &instanceStatus{
		address: addressIP,
		powered: strings.ToUpper(server.State) == "STARTED",
	}

	return
}

// WaitForPowered wait ip a VM by name
func (instance *ServerInstance) WaitForPowered() error {
	glog.Debugf("WaitForPowered: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	ctx := context.NewContext(instance.Timeout)
	defer ctx.Cancel()

	return context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
		return instance.expectStatus("STARTED", "STOPPED")
	})
}

func (instance *ServerInstance) getServer() (vm *cloudstack.VirtualMachine, err error) {
	vm, _, err = instance.client.VirtualMachine.GetVirtualMachineByID(instance.InstanceID)
	return
}

// Create will create a named VM not powered
// memory and disk are in megabytes
func (instance *ServerInstance) Create(controlPlane bool, nodeGroup, serviceOfferingId, userData string, diskSize int) (err error) {
	var server *cloudstack.DeployVirtualMachineResponse
	var securityGroup string
	var useFloatingIP bool

	config := instance.Configuration
	network := instance.Configuration.Network
	primaryInterface := instance.attachedNetwork.PrimaryInterface()
	ctx := context.NewContext(instance.Timeout)
	defer ctx.Cancel()

	if controlPlane {
		securityGroup = network.SecurityGroup.ControlPlaneNode
		useFloatingIP = network.PublicControlPlaneNode
	} else {
		securityGroup = network.SecurityGroup.WorkerNode
		useFloatingIP = network.PublicWorkerNode
	}

	p := instance.client.VirtualMachine.NewDeployVirtualMachineParams(serviceOfferingId, config.TemplateId, config.ZoneId)

	p.SetDisplayname(instance.InstanceName)
	p.SetName(instance.InstanceName)
	p.SetKeypair(config.SshKeyName)
	p.SetUserdata(base64.StdEncoding.EncodeToString([]byte(userData)))
	p.SetIpaddress(instance.attachedNetwork.PrimaryAddressIP())
	p.SetNetworkids([]string{primaryInterface.NetworkName})

	if len(config.VpcId) == 0 {
		p.SetSecuritygroupids([]string{securityGroup})
	}

	instance.client.VirtualMachine.DeployVirtualMachine(p)

	if server, err = instance.client.VirtualMachine.DeployVirtualMachine(p); err != nil {
		err = fmt.Errorf("server creation failed for: %s, reason: %v", instance.InstanceName, err)
	} else {
		instance.InstanceID = server.Id

		if err = instance.WaitForPowered(); err != nil {
			err = fmt.Errorf("server powered failed for: %s, reason: %v", instance.InstanceName, err)
		} else if len(server.Nic) == 0 {
			err = fmt.Errorf("unable to get ip address for server: %s", instance.InstanceName)
		} else {
			instance.AddressIP = server.Nic[0].Ipaddress

			glog.Debugf("instance: %s with ID: %s started. Got IP: %s", instance.InstanceName, instance.InstanceID, instance.AddressIP)

			if useFloatingIP {
				var response *cloudstack.AssociateIpAddressResponse
				var nat *cloudstack.EnableStaticNatResponse

				p := instance.client.Address.NewAssociateIpAddressParams()

				p.SetNetworkid(primaryInterface.NetworkName)

				if response, err = instance.client.Address.AssociateIpAddress(p); err != nil {
					err = fmt.Errorf("unable to associate address for instance: %s. Reason: %v", instance.InstanceName, err)
				} else if nat, err = instance.client.NAT.EnableStaticNat(instance.client.NAT.NewEnableStaticNatParams(response.Id, server.Id)); err != nil {
					err = fmt.Errorf("unable to enable static nat for instance: %s. Reason: %v", instance.InstanceName, err)
				} else if !nat.Success {
					err = fmt.Errorf("unable to start static nat for instance: %s. failed: %v", instance.InstanceName, nat.Displaytext)
				} else {
					instance.PublicAddress = response.Ipaddress
					instance.PublicAddressID = response.Id
					instance.ZoneName = response.Zonename
				}
			}
		}
	}

	return
}