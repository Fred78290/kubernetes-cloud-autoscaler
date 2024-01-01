package vsphere

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	glog "github.com/sirupsen/logrus"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/vim25/soap"
)

// Configuration declares vsphere connection info
type Configuration struct {
	NodeGroup     string        `json:"nodegroup"`
	URL           string        `json:"url"`
	UserName      string        `json:"uid"`
	Password      string        `json:"password"`
	Insecure      bool          `json:"insecure"`
	DataCenter    string        `json:"dc"`
	DataStore     string        `json:"datastore"`
	Resource      string        `json:"resource-pool"`
	VMBasePath    string        `json:"vmFolder"`
	Timeout       time.Duration `json:"timeout"`
	TemplateName  string        `json:"template-name"`
	Template      bool          `json:"template"`
	LinkedClone   bool          `json:"linked"`
	AllowUpgrade  bool          `json:"allow-upgrade"`
	Customization string        `json:"customization"`
	Network       Network       `json:"network"`
	VMWareRegion  string        `json:"csi-region"`
	VMWareZone    string        `json:"csi-zone"`
	TestMode      bool          `json:"test-mode"`
}

type vsphereHandler struct {
	config       *Configuration
	network      Network
	instanceName string
	instanceID   string
	nodeIndex    int
}

type vsphereWrapper struct {
	config Configuration
}

func loadConfig(fileName string) (*Configuration, error) {
	var config Configuration
	file, err := os.Open(fileName)

	if err != nil {
		glog.Errorf("Failed to open file:%s, error:%v", fileName, err)

		return nil, err
	}

	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)

	if err != nil {
		glog.Errorf("failed to decode AutoScalerServerApp file:%s, error:%v", fileName, err)
		return nil, err
	}

	return &config, nil
}

func NewVSphereProviderConfiguration(fileName string) (providers.ProviderConfiguration, error) {
	var wrapper vsphereWrapper

	if err := providers.LoadConfig(fileName, &wrapper.config); err != nil {
		glog.Errorf("Failed to open file:%s, error:%v", fileName, err)

		return nil, err
	} else if _, err = wrapper.config.UUID(wrapper.config.TemplateName); err != nil {
		return nil, err
	}

	return &wrapper, nil
}

// Status shortened vm status
type Status struct {
	Interfaces []NetworkInterface
	Powered    bool
}

type VmStatus struct {
	Status
	address string
}

func (status *VmStatus) Address() string {
	return status.address
}

func (status *VmStatus) Powered() bool {
	return status.Status.Powered
}

func (handler *vsphereHandler) GetTimeout() time.Duration {
	return handler.config.Timeout
}

func (handler *vsphereHandler) NodeGroupName() string {
	return handler.config.NodeGroup
}

func (handler *vsphereHandler) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
	if len(network.VMWare) > 0 {
		for _, network := range network.VMWare {
			if inf := handler.findInterfaceByName(network.NetworkName); inf != nil {
				inf.DHCP = network.DHCP
				inf.UseRoutes = network.UseRoutes
				inf.Routes = network.Routes

				if len(network.IPV4Address) > 0 {
					inf.IPAddress = network.IPV4Address
				}

				if len(network.Netmask) > 0 {
					inf.Netmask = network.Netmask
				}

				if len(network.Gateway) > 0 {
					inf.Gateway = network.Gateway
				}

				if len(network.MacAddress) > 0 {
					inf.MacAddress = network.MacAddress
				}
			}
		}
	}
}

func (handler *vsphereHandler) RetrieveNetworkInfos() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	ctx := context.NewContext(handler.config.Timeout)
	defer ctx.Cancel()

	return handler.config.RetrieveNetworkInfosWithContext(ctx, handler.instanceName, handler.nodeIndex, &handler.network)
}

func (handler *vsphereHandler) UpdateMacAddressTable() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.network.UpdateMacAddressTable(handler.nodeIndex)
}

func (handler *vsphereHandler) GenerateProviderID() string {
	return fmt.Sprintf("vsphere://%s", handler.instanceID)
}

func (handler *vsphereHandler) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion:  handler.config.VMWareRegion,
		constantes.NodeLabelTopologyZone:    handler.config.VMWareZone,
		constantes.NodeLabelVMWareCSIRegion: handler.config.VMWareRegion,
		constantes.NodeLabelVMWareCSIZone:   handler.config.VMWareZone,
	}
}

func (handler *vsphereHandler) InstanceCreate(nodeName string, nodeIndex int, instanceType, userName, authKey string, cloudInit interface{}, machine *providers.MachineCharacteristic) (string, error) {
	if vm, err := handler.config.Create(nodeName, nodeIndex, userName, authKey, cloudInit, &handler.network, machine); err != nil {
		return "", err
	} else {
		ctx := context.NewContext(handler.config.Timeout)
		defer ctx.Cancel()

		handler.instanceName = nodeName
		handler.instanceID = vm.UUID(ctx)
		handler.nodeIndex = nodeIndex

		return handler.instanceID, err
	}
}

func (handler *vsphereHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	if handler.instanceName == "" {
		return "", fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	if ip, err := handler.config.WaitForIP(handler.instanceName); err != nil {
		return ip, err
	} else {
		if err := context.PollImmediate(time.Second, handler.config.Timeout*time.Second, func() (bool, error) {
			var err error

			if err = callback.WaitSSHReady(handler.instanceName, ip); err != nil {
				return false, err
			}

			return true, nil
		}); err != nil {
			return ip, err
		}

		return ip, nil
	}
}

func (handler *vsphereHandler) InstanceID() (string, error) {
	if handler.instanceName == "" {
		return "", fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.UUID(handler.instanceName)
}

func (handler *vsphereHandler) InstanceExists(name string) bool {
	return handler.config.Exists(handler.instanceName)
}

func (handler *vsphereHandler) InstanceAutoStart() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	if hostsystem, err := handler.config.GetHostSystem(handler.instanceName); err != nil {
		return err
	} else if err = handler.config.SetAutoStart(hostsystem, handler.instanceName, -1); err != nil {
		return err
	}

	return nil
}

func (handler *vsphereHandler) InstancePowerOn() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.PowerOn(handler.instanceName)
}

func (handler *vsphereHandler) InstancePowerOff() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.PowerOff(handler.instanceName)
}

func (handler *vsphereHandler) InstanceShutdownGuest() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.ShutdownGuest(handler.instanceName)
}

func (handler *vsphereHandler) InstanceDelete() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.Delete(handler.instanceName)
}

func (handler *vsphereHandler) InstanceStatus() (providers.InstanceStatus, error) {
	if handler.instanceName == "" {
		return nil, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	if status, err := handler.config.Status(handler.instanceName); err != nil {
		return nil, err
	} else {
		return &VmStatus{
			Status:  *status,
			address: handler.findPreferredIPAddress(status.Interfaces),
		}, nil
	}
}

func (handler *vsphereHandler) InstanceWaitForPowered() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.WaitForPowered(handler.instanceName)
}

func (handler *vsphereHandler) InstanceWaitForToolsRunning() (bool, error) {
	if handler.instanceName == "" {
		return false, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.config.WaitForToolsRunning(handler.instanceName)
}

func (handler *vsphereHandler) InstanceMaxPods(instanceType string, desiredMaxPods int) (int, error) {
	if desiredMaxPods == 0 {
		desiredMaxPods = 110
	}

	return desiredMaxPods, nil
}

func (handler *vsphereHandler) RegisterDNS(address string) error {
	return nil
}

func (handler *vsphereHandler) UnregisterDNS(address string) error {
	return nil
}

func (handler *vsphereHandler) findPreferredIPAddress(interfaces []NetworkInterface) string {
	address := ""

	for _, inf := range interfaces {
		if declaredInf := handler.findInterfaceByName(inf.NetworkName); declaredInf != nil {
			if declaredInf.Primary {
				return inf.IPAddress
			}
		}
	}

	return address
}

func (handler *vsphereHandler) findInterfaceByName(networkName string) *NetworkInterface {
	for _, inf := range handler.network.Interfaces {
		if inf.NetworkName == networkName {
			return inf
		}
	}

	return nil
}

func (wrapper *vsphereWrapper) AttachInstance(instanceName string, nodeIndex int) (providers.ProviderHandler, error) {
	var network Network

	providers.Copy(&network, wrapper.config.Network)

	if vmuuid, err := wrapper.config.UUID(instanceName); err != nil {
		return nil, err
	} else {
		return &vsphereHandler{
			config:       &wrapper.config,
			network:      network,
			instanceName: instanceName,
			instanceID:   vmuuid,
			nodeIndex:    nodeIndex,
		}, nil
	}
}

func (wrapper *vsphereWrapper) CreateInstance(instanceName string, nodeIndex int) (providers.ProviderHandler, error) {
	if wrapper.InstanceExists(instanceName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, instanceName)
		return nil, fmt.Errorf(constantes.ErrVMAlreadyExists, instanceName)
	}

	var network Network

	providers.Copy(&network, wrapper.config.Network)

	return &vsphereHandler{
		config:       &wrapper.config,
		network:      network,
		instanceName: instanceName,
		nodeIndex:    nodeIndex,
	}, nil
}

func (wrapper *vsphereWrapper) InstanceExists(name string) bool {
	return wrapper.config.Exists(name)
}

func (wrapper *vsphereWrapper) NodeGroupName() string {
	return wrapper.config.NodeGroup
}

func (handler *vsphereWrapper) GetAvailableGpuTypes() map[string]string {
	return map[string]string{}
}

func (config *Configuration) UUIDWithContext(ctx *context.Context, name string) (string, error) {
	vm, err := config.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return "", err
	}

	return vm.UUID(ctx), nil
}

func (config *Configuration) UUID(name string) (string, error) {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.UUIDWithContext(ctx, name)
}

// GetClient create a new govomi client
func (config *Configuration) GetClient(ctx *context.Context) (*Client, error) {
	var u *url.URL
	var sURL string
	var err error
	var c *govmomi.Client

	if sURL, err = config.getURL(); err == nil {
		if u, err = soap.ParseURL(sURL); err == nil {
			// Connect and log in to ESX or vCenter
			if c, err = govmomi.NewClient(ctx, u, config.Insecure); err == nil {
				return &Client{
					Client:        c,
					Configuration: config,
				}, nil
			}
		}
	}
	return nil, err
}

// CreateWithContext will create a named VM not powered
// memory and disk are in megabytes
func (config *Configuration) CreateWithContext(ctx *context.Context, name string, nodeIndex int, userName, authKey string, cloudInit interface{}, network *Network, machine *providers.MachineCharacteristic) (*VirtualMachine, error) {
	var err error
	var client *Client
	var dc *Datacenter
	var ds *Datastore
	var vm *VirtualMachine

	if client, err = config.GetClient(ctx); err == nil {
		if dc, err = client.GetDatacenter(ctx, config.DataCenter); err == nil {
			if ds, err = dc.GetDatastore(ctx, config.DataStore); err == nil {
				if vm, err = ds.CreateVirtualMachine(ctx, name, config.TemplateName, config.VMBasePath, config.Resource, config.Template, config.LinkedClone, network, config.Customization, nodeIndex); err == nil {
					err = vm.Configure(ctx, userName, authKey, cloudInit, network, "", true, machine.Memory, machine.Vcpu, machine.DiskSize, nodeIndex, config.AllowUpgrade)
				}
			}
		}
	}

	// If an error occured delete VM
	if err != nil && vm != nil {
		_ = vm.Delete(ctx)
	}

	return vm, err
}

// Create will create a named VM not powered
// memory and disk are in megabytes
func (config *Configuration) Create(name string, nodeIndex int, userName, authKey string, cloudInit interface{}, network *Network, machine *providers.MachineCharacteristic) (*VirtualMachine, error) {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.CreateWithContext(ctx, name, nodeIndex, userName, authKey, cloudInit, network, machine)
}

// DeleteWithContext a VM by name
func (config *Configuration) DeleteWithContext(ctx *context.Context, name string) error {
	vm, err := config.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.Delete(ctx)
}

// Delete a VM by name
func (config *Configuration) Delete(name string) error {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.DeleteWithContext(ctx, name)
}

// VirtualMachineWithContext  Retrieve VM by name
func (config *Configuration) VirtualMachineWithContext(ctx *context.Context, name string) (*VirtualMachine, error) {
	var err error
	var client *Client
	var dc *Datacenter
	var ds *Datastore

	if client, err = config.GetClient(ctx); err == nil {
		if dc, err = client.GetDatacenter(ctx, config.DataCenter); err == nil {
			if ds, err = dc.GetDatastore(ctx, config.DataStore); err == nil {
				return ds.VirtualMachine(ctx, name)
			}
		}
	}

	return nil, err
}

// VirtualMachine  Retrieve VM by name
func (config *Configuration) VirtualMachine(name string) (*VirtualMachine, error) {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.VirtualMachineWithContext(ctx, name)
}

// VirtualMachineListWithContext return all VM for the current datastore
func (config *Configuration) VirtualMachineListWithContext(ctx *context.Context) ([]*VirtualMachine, error) {
	var err error
	var client *Client
	var dc *Datacenter
	var ds *Datastore

	if client, err = config.GetClient(ctx); err == nil {
		if dc, err = client.GetDatacenter(ctx, config.DataCenter); err == nil {
			if ds, err = dc.GetDatastore(ctx, config.DataStore); err == nil {
				return ds.List(ctx)
			}
		}
	}

	return nil, err
}

// VirtualMachineList return all VM for the current datastore
func (config *Configuration) VirtualMachineList() ([]*VirtualMachine, error) {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.VirtualMachineListWithContext(ctx)
}

// UUID get VM UUID by name
func (handler *vsphereHandler) UUID(name string) (string, error) {
	return handler.config.UUID(name)
}

func (config *Configuration) getURL() (string, error) {
	u, err := url.Parse(config.URL)

	if err != nil {
		return "", err
	}

	u.User = url.UserPassword(config.UserName, config.Password)

	return u.String(), err
}

// WaitForIPWithContext wait ip a VM by name
func (config *Configuration) WaitForIPWithContext(ctx *context.Context, name string) (string, error) {

	if config.TestMode {
		return "127.0.0.1", nil
	}

	vm, err := config.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return "", err
	}

	return vm.WaitForIP(ctx)
}

// WaitForIP wait ip a VM by name
func (config *Configuration) WaitForIP(name string) (string, error) {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.WaitForIPWithContext(ctx, name)
}

// SetAutoStartWithContext set autostart for the VM
func (config *Configuration) SetAutoStartWithContext(ctx *context.Context, esxi, name string, startOrder int) error {
	var err error = nil

	if !config.TestMode {
		var client *Client
		var dc *Datacenter
		var host *HostAutoStartManager

		if client, err = config.GetClient(ctx); err == nil {
			if dc, err = client.GetDatacenter(ctx, config.DataCenter); err == nil {
				if host, err = dc.GetHostAutoStartManager(ctx, esxi); err == nil {
					return host.SetAutoStart(ctx, config.DataStore, name, startOrder)
				}
			}
		}
	}

	return err
}

// WaitForToolsRunningWithContext wait vmware tools is running a VM by name
func (config *Configuration) WaitForToolsRunningWithContext(ctx *context.Context, name string) (bool, error) {
	if config.TestMode {
		return true, nil
	}

	vm, err := config.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return false, err
	}

	return vm.WaitForToolsRunning(ctx)
}

// WaitForToolsRunningWithContext wait vmware tools is running a VM by name
func (config *Configuration) WaitForPoweredWithContext(ctx *context.Context, name string) error {
	if config.TestMode {
		return nil
	}

	vm, err := config.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.WaitForPowered(ctx)
}

func (config *Configuration) GetHostSystemWithContext(ctx *context.Context, name string) (string, error) {
	vm, err := config.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return "*", err
	}

	return vm.HostSystem(ctx)
}

// GetHostSystem return the host where the virtual machine leave
func (config *Configuration) GetHostSystem(name string) (string, error) {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.GetHostSystemWithContext(ctx, name)
}

// SetAutoStart set autostart for the VM
func (config *Configuration) SetAutoStart(esxi, name string, startOrder int) error {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.SetAutoStartWithContext(ctx, esxi, name, startOrder)
}

// WaitForToolsRunning wait vmware tools is running a VM by name
func (config *Configuration) WaitForToolsRunning(name string) (bool, error) {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.WaitForToolsRunningWithContext(ctx, name)
}

// WaitForToolsRunning wait vmware tools is running a VM by name
func (config *Configuration) WaitForPowered(name string) error {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.WaitForPoweredWithContext(ctx, name)
}

// PowerOnWithContext power on a VM by name
func (config *Configuration) PowerOnWithContext(ctx *context.Context, name string) error {
	vm, err := config.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.PowerOn(ctx)
}

// PowerOn power on a VM by name
func (config *Configuration) PowerOn(name string) error {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.PowerOnWithContext(ctx, name)
}

// PowerOffWithContext power off a VM by name
func (config *Configuration) PowerOffWithContext(ctx *context.Context, name string) error {
	vm, err := config.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.PowerOff(ctx)
}

// ShutdownGuestWithContext power off a VM by name
func (config *Configuration) ShutdownGuestWithContext(ctx *context.Context, name string) error {
	vm, err := config.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.ShutdownGuest(ctx)
}

// PowerOff power off a VM by name
func (config *Configuration) PowerOff(name string) error {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.PowerOffWithContext(ctx, name)
}

// ShutdownGuest power off a VM by name
func (config *Configuration) ShutdownGuest(name string) error {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.ShutdownGuestWithContext(ctx, name)
}

// StatusWithContext return the current status of VM by name
func (config *Configuration) StatusWithContext(ctx *context.Context, name string) (*Status, error) {
	vm, err := config.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return nil, err
	}

	return vm.Status(ctx)
}

// Status return the current status of VM by name
func (config *Configuration) Status(name string) (*Status, error) {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.StatusWithContext(ctx, name)
}

func (config *Configuration) RetrieveNetworkInfosWithContext(ctx *context.Context, name string, nodeIndex int, network *Network) error {
	vm, err := config.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.collectNetworkInfos(ctx, network, nodeIndex)
}

func (config *Configuration) RetrieveNetworkInfos(name string, nodeIndex int, network *Network) error {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.RetrieveNetworkInfosWithContext(ctx, name, nodeIndex, network)
}

// ExistsWithContext return the current status of VM by name
func (config *Configuration) ExistsWithContext(ctx *context.Context, name string) bool {
	if _, err := config.VirtualMachineWithContext(ctx, name); err == nil {
		return true
	}

	return false
}

func (config *Configuration) Exists(name string) bool {
	ctx := context.NewContext(config.Timeout)
	defer ctx.Cancel()

	return config.ExistsWithContext(ctx, name)
}
