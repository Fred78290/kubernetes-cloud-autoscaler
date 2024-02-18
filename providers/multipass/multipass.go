package multipass

import (
	"fmt"
	"sync"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	glog "github.com/sirupsen/logrus"
)

type multipassHandler struct {
	multipassWrapper
	network      *providers.Network
	instanceName string
	controlPlane bool
	nodeIndex    int
}

type multipassWrapper interface {
	providers.ProviderConfiguration

	getConfiguration() *Configuration

	powerOn(instanceName string) error
	powerOff(instanceName string) error
	delete(instanceName string) error
	status(instanceName string) (providers.InstanceStatus, error)
	create(input *createInstanceInput) (string, error)
	waitForIP(instanceName string, status multipassWrapper, callback providers.CallbackWaitSSHReady) (string, error)
	waitForPowered(instanceName string, status multipassWrapper) (err error)
}

type baseMultipassWrapper struct {
	*Configuration
	sync.Mutex
}

type createInstanceInput struct {
	*providers.InstanceCreateInput
	network      *providers.Network
	netplanFile  string
	instanceName string
	template     string
	nodeIndex    int
}

func (wrapper *baseMultipassWrapper) waitForIP(instanceName string, status multipassWrapper, callback providers.CallbackWaitSSHReady) (string, error) {
	address := ""

	if err := context.PollImmediate(time.Second, wrapper.Timeout*time.Second, func() (bool, error) {
		if status, err := status.status(instanceName); err != nil {
			return false, err
		} else if status.Powered() && len(status.Address()) > 0 {
			glog.Debugf("WaitForIP: instance %s, using IP: %s", instanceName, status.Address())

			if err = callback.WaitSSHReady(instanceName, status.Address()); err != nil {
				return false, err
			}
			address = status.Address()
			return true, nil
		} else {
			return false, nil
		}
	}); err != nil {
		return "", err
	}

	return address, nil
}

func (wrapper *baseMultipassWrapper) waitForPowered(instanceName string, status multipassWrapper) (err error) {
	return context.PollImmediate(time.Second, wrapper.Timeout*time.Second, func() (bool, error) {
		if status, err := status.status(instanceName); err != nil {
			return false, err
		} else if status.Powered() {
			return true, nil
		} else {
			return false, nil
		}
	})
}

func (handler *multipassHandler) GetTimeout() time.Duration {
	return handler.getConfiguration().Timeout
}

func (handler *multipassHandler) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
	handler.network.ConfigureNetwork(network)
}

func (handler *multipassHandler) RetrieveNetworkInfos() error {
	return nil
}

func (handler *multipassHandler) UpdateMacAddressTable() error {
	return nil
}

func (handler *multipassHandler) GenerateProviderID() string {
	return fmt.Sprintf("multipass://%s", handler.instanceName)
}

func (handler *multipassHandler) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion:  handler.getConfiguration().VMWareRegion,
		constantes.NodeLabelTopologyZone:    handler.getConfiguration().VMWareZone,
		constantes.NodeLabelVMWareCSIRegion: handler.getConfiguration().VMWareRegion,
		constantes.NodeLabelVMWareCSIZone:   handler.getConfiguration().VMWareZone,
	}
}

func (handler *multipassHandler) InstanceCreate(input *providers.InstanceCreateInput) (string, error) {
	createInstanceInput := createInstanceInput{
		InstanceCreateInput: input,
		network:             handler.network,
		instanceName:        handler.instanceName,
		template:            handler.getConfiguration().TemplateName,
		nodeIndex:           handler.nodeIndex,
		netplanFile:         handler.getConfiguration().NetplanFileName,
	}

	return handler.create(&createInstanceInput)
}

func (handler *multipassHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	return handler.waitForIP(handler.instanceName, handler, callback)
}

func (handler *multipassHandler) InstancePrimaryAddressIP() string {
	return handler.network.PrimaryAddressIP()
}

func (handler *multipassHandler) InstanceID() (string, error) {
	return handler.instanceName, nil
}

func (handler *multipassHandler) InstanceAutoStart() error {
	return nil
}

func (handler *multipassHandler) InstancePowerOn() error {
	return handler.powerOn(handler.instanceName)
}

func (handler *multipassHandler) InstancePowerOff() error {
	return handler.powerOff(handler.instanceName)
}

func (handler *multipassHandler) InstanceShutdownGuest() error {
	return handler.powerOff(handler.instanceName)

}

func (handler *multipassHandler) InstanceDelete() error {
	return handler.delete(handler.instanceName)
}

func (handler *multipassHandler) InstanceStatus() (providers.InstanceStatus, error) {
	return handler.status(handler.instanceName)
}

func (handler *multipassHandler) InstanceWaitForPowered() error {
	return handler.waitForPowered(handler.instanceName, handler)
}

func (handler *multipassHandler) InstanceWaitForToolsRunning() (bool, error) {
	return true, nil
}

func (handler *multipassHandler) InstanceMaxPods(desiredMaxPods int) (int, error) {
	if desiredMaxPods == 0 {
		desiredMaxPods = 110
	}

	return desiredMaxPods, nil
}

func (handler *multipassHandler) PrivateDNSName() (string, error) {
	return handler.instanceName, nil
}

func (handler *multipassHandler) RegisterDNS(address string) error {
	return nil
}

func (handler *multipassHandler) UnregisterDNS(address string) error {
	return nil
}

func (handler *multipassHandler) UUID(name string) (string, error) {
	return name, nil
}
