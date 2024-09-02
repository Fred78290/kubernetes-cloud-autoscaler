package lxd

import (
	"fmt"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	golxd "github.com/canonical/lxd/client"
	"github.com/canonical/lxd/shared/api"
	glog "github.com/sirupsen/logrus"
)

type instanceStatus struct {
	address string
	powered bool
}

type ServerInstance struct {
	*lxdWrapper
	attachedNetwork *lxdNetwork
	NodeIndex       int
	InstanceName    string
	PrivateDNSName  string
	InstanceID      string
	Location        string
	AddressIP       string
}

func (status *instanceStatus) Address() string {
	return status.address
}

func (status *instanceStatus) Powered() bool {
	return status.powered
}

func (instance *ServerInstance) expectStatus(expected, initial string) (bool, error) {
	var container *api.InstanceFull
	var err error

	if container, err = instance.getServer(); err != nil {
		glog.Debugf("get instance %s id (%s), got an error %v", instance.InstanceName, instance.InstanceID, err)

		return false, err
	}

	status := strings.ToUpper(container.Status)

	if status == initial {
		return false, nil
	}

	if status == expected {
		glog.Debugf("ready instance %s id (%s)", instance.InstanceName, instance.InstanceID)

		return true, nil
	} else if status == "FROZEN" || status == "ERROR" {
		glog.Debugf("instance %s id (%s), unexpected state: %s", instance.InstanceName, instance.InstanceID, status)

		return false, fmt.Errorf(constantes.ErrWrongStateMachine, status, instance.InstanceName, expected)
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
		if ready, err = instance.expectStatus("RUNNING", ""); ready {
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
	var container *api.InstanceFull

	if container, err = instance.getServer(); err != nil {
		return false, err
	}

	status := strings.ToUpper(container.Status)

	if status == "RUNNING" {
		powered = true
	}

	return
}

func (instance *ServerInstance) PowerOn(ctx *context.Context) (err error) {
	glog.Debugf("PowerOn: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	var powered bool
	var op golxd.Operation

	if powered, err = instance.isPowered(); err != nil || powered {
		return
	}

	if op, err = instance.client.UpdateInstanceState(instance.InstanceName, api.InstanceStatePut{
		Action:  "start",
		Timeout: int(instance.Timeout),
	}, ""); err != nil {

	}

	if err = op.Wait(); err == nil {
		err = context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
			return instance.expectStatus("RUNNING", "STOPPED")
		})
	}

	return err
}

func (instance *ServerInstance) PowerOff(ctx *context.Context) (err error) {
	glog.Debugf("PowerOff: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	var powered bool
	var op golxd.Operation

	if powered, err = instance.isPowered(); err != nil || !powered {
		return
	}

	if op, err = instance.client.UpdateInstanceState(instance.InstanceName, api.InstanceStatePut{
		Action:  "stop",
		Timeout: int(instance.Timeout),
	}, ""); err != nil {

	}

	if err = op.Wait(); err == nil {
		err = context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
			return instance.expectStatus("STOPPED", "RUNNING")
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

	var op golxd.Operation

	if op, err = instance.client.DeleteInstance(instance.InstanceName); err != nil {
		return
	}

	err = op.Wait()

	return
}

func (instance *ServerInstance) Status() (status providers.InstanceStatus, err error) {
	glog.Debugf("Status: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	var container *api.InstanceFull
	var addressIP string

	ctx := context.NewContext(instance.Timeout)
	defer ctx.Cancel()

	if container, err = instance.getServer(); err != nil {
		return
	}

	if addressIP, err = instance.getAddress(container); err != nil {
		return
	}

	status = &instanceStatus{
		address: addressIP,
		powered: strings.ToUpper(container.Status) == "RUNNING",
	}

	return
}

// WaitForPowered wait ip a VM by name
func (instance *ServerInstance) WaitForPowered() error {
	glog.Debugf("WaitForPowered: instance %s id (%s)", instance.InstanceName, instance.InstanceID)

	ctx := context.NewContext(instance.Timeout)
	defer ctx.Cancel()

	return context.PollImmediate(time.Second, instance.Timeout*time.Second, func() (bool, error) {
		return instance.expectStatus("RUNNING", "STOPPED")
	})
}

func (instance *ServerInstance) getServer() (vm *api.InstanceFull, err error) {
	vm, _, err = instance.client.GetInstanceFull(instance.InstanceName)

	return
}

// Create will create a named VM not powered
// memory and disk are in megabytes
func (instance *ServerInstance) Create(controlPlane bool, nodeGroup, userData string, machine *providers.MachineCharacteristic) (err error) {
	var op golxd.Operation
	var container *api.InstanceFull

	devices := make(map[string]map[string]string)
	config := make(map[string]string)

	for _, inf := range instance.network.LxdInterfaces {
		if inf.IsEnabled() {
			device := map[string]string{
				"type":    "nic",
				"network": inf.NetworkName,
				"name":    inf.NetworkName,
			}

			if !inf.DHCP && len(inf.IPAddress) > 0 {
				device["ipv4.address"] = cloudinit.ToCIDR(inf.IPAddress, inf.Netmask)
			}

			devices[inf.NicName] = device
		}
	}

	devices["root"] = map[string]string{
		"type": "disk",
		"path": "/",
		"pool": instance.StoragePool,
		"size": fmt.Sprintf("%dMiB", machine.GetDiskSize()),
	}

	config["limits.cpu"] = fmt.Sprintf("%d", machine.Vcpu)
	config["limits.memory"] = fmt.Sprintf("%dMiB", machine.Memory)
	config["cloud-init.user-data"] = userData
	config["cloud-init.network-config"] = instance.network.GetCloudInitNetwork(false).encodeCloudInit()

	req := api.InstancesPost{
		InstancePut: api.InstancePut{
			Profiles:    instance.Profiles,
			Devices:     devices,
			Config:      config,
			Description: instance.InstanceName,
		},
		Source: api.InstanceSource{
			Type:        "image",
			Fingerprint: instance.imageFingerPrint,
		},
		Name:  instance.InstanceName,
		Type:  instance.ContainerType,
		Start: false,
	}

	if op, err = instance.client.CreateInstance(req); err != nil {
		err = fmt.Errorf("server creation failed for: %s, reason: %v", instance.InstanceName, err)
	} else if err = op.Wait(); err != nil {
		err = fmt.Errorf("operation creation failed for: %s, reason: %v", instance.InstanceName, err)
	} else if container, _, err = instance.client.GetInstanceFull(instance.InstanceName); err != nil {
		err = fmt.Errorf("get instance failed for: %s, reason: %v", instance.InstanceName, err)
	} else {
		instance.InstanceID = container.Config["volatile.uuid"]
	}

	return
}
