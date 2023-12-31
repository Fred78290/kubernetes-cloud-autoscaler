package vsphere

import (
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
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
}

type vsphereConfiguration struct {
	config       *Configuration
	distribution string
	network      Network
	testMode     bool
	instanceName string
	instanceID   string
}

func NewVSphereProviderConfiguration(distribution string, config *Configuration) (providers.ProviderConfiguration, error) {
	var network Network

	providers.Copy(&network, config.Network)

	return &vsphereConfiguration{
		config:       config,
		distribution: distribution,
		network:      network,
	}, nil
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

func (conf *vsphereConfiguration) GetTestMode() bool {
	return conf.testMode
}

func (conf *vsphereConfiguration) SetTestMode(value bool) {
	conf.testMode = value
}

func (conf *vsphereConfiguration) GetTimeout() time.Duration {
	return conf.config.Timeout
}

func (conf *vsphereConfiguration) GetAvailableGpuTypes() map[string]string {
	return map[string]string{}
}

func (conf *vsphereConfiguration) NodeGroupName() string {
	return conf.config.NodeGroup
}

// Create a shadow copy
func (conf *vsphereConfiguration) copy() *vsphereConfiguration {
	var network Network

	providers.Copy(&network, conf.config.Network)

	return &vsphereConfiguration{
		config:       conf.config,
		distribution: conf.distribution,
		network:      network,
		testMode:     conf.testMode,
		instanceName: conf.instanceName,
		instanceID:   conf.instanceID,
	}
}

// Clone duplicate the conf, change ip address in network config if needed
func (conf *vsphereConfiguration) Clone(nodeIndex int) (providers.ProviderConfiguration, error) {
	dup := conf.copy()

	for _, inf := range dup.network.Interfaces {
		if !inf.DHCP {
			ip := net.ParseIP(inf.IPAddress)
			address := ip.To4()
			address[3] += byte(nodeIndex)

			inf.IPAddress = ip.String()
		}
	}

	return dup, nil
}

func (conf *vsphereConfiguration) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
	if len(network.VMWare) > 0 {
		for _, network := range network.VMWare {
			if inf := conf.FindInterfaceByName(network.NetworkName); inf != nil {
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

func (conf *vsphereConfiguration) AttachInstance(instanceName string) (providers.ProviderConfiguration, error) {
	clone := conf.copy()
	var err error

	clone.instanceID, err = conf.UUID(instanceName)
	clone.instanceName = instanceName

	return clone, err
}

func (conf *vsphereConfiguration) RetrieveNetworkInfos(name, vmuuid string, nodeIndex int) error {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.RetrieveNetworkInfosWithContext(ctx, name, nodeIndex)
}

func (conf *vsphereConfiguration) UpdateMacAddressTable(nodeIndex int) error {
	return conf.network.UpdateMacAddressTable(nodeIndex)
}

func (conf *vsphereConfiguration) GenerateProviderID(vmuuid string) string {
	return fmt.Sprintf("vsphere://%s", vmuuid)
}

func (conf *vsphereConfiguration) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion:  conf.config.VMWareRegion,
		constantes.NodeLabelTopologyZone:    conf.config.VMWareZone,
		constantes.NodeLabelVMWareCSIRegion: conf.config.VMWareRegion,
		constantes.NodeLabelVMWareCSIZone:   conf.config.VMWareZone,
	}
}

func (conf *vsphereConfiguration) InstanceCreate(nodeName string, nodeIndex int, instanceType, userName, authKey string, cloudInit interface{}, machine *providers.MachineCharacteristic) (string, error) {
	if vm, err := conf.Create(nodeName, nodeIndex, userName, authKey, cloudInit, &conf.network, machine); err != nil {
		return "", err
	} else {
		ctx := context.NewContext(conf.config.Timeout)
		defer ctx.Cancel()

		conf.instanceName = nodeName
		conf.instanceID = vm.UUID(ctx)

		return conf.instanceID, err
	}
}

func (conf *vsphereConfiguration) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	if ip, err := conf.WaitForIP(conf.instanceName); err != nil {
		return ip, err
	} else {
		if err := context.PollImmediate(time.Second, conf.config.Timeout*time.Second, func() (bool, error) {
			var err error

			if err = callback.WaitSSHReady(conf.instanceName, ip); err != nil {
				return false, err
			}

			return true, nil
		}); err != nil {
			return ip, err
		}

		return ip, nil
	}
}

func (conf *vsphereConfiguration) InstanceID(name string) (string, error) {
	return conf.UUID(name)
}

func (conf *vsphereConfiguration) InstanceExists(name string) bool {
	return conf.Exists(name)
}

func (conf *vsphereConfiguration) InstanceAutoStart(name string) error {
	if hostsystem, err := conf.GetHostSystem(name); err != nil {
		return err
	} else if err = conf.SetAutoStart(hostsystem, name, -1); err != nil {
		return err
	}

	return nil
}

func (conf *vsphereConfiguration) InstancePowerOn(name string) error {
	return conf.PowerOn(name)
}

func (conf *vsphereConfiguration) InstancePowerOff(name string) error {
	return conf.PowerOff(name)
}

func (conf *vsphereConfiguration) InstanceShutdownGuest(name string) error {
	return conf.ShutdownGuest(name)
}

func (conf *vsphereConfiguration) InstanceDelete(name string) error {
	return conf.Delete(name)
}

func (conf *vsphereConfiguration) InstanceStatus(name string) (providers.InstanceStatus, error) {
	if status, err := conf.Status(name); err != nil {
		return nil, err
	} else {
		return &VmStatus{
			Status:  *status,
			address: conf.FindPreferredIPAddress(status.Interfaces),
		}, nil
	}
}

func (conf *vsphereConfiguration) InstanceWaitForPowered(name string) error {
	if vmuuid, err := conf.InstanceID(name); err != nil {
		return err
	} else {
		return conf.WaitForPowered(vmuuid)
	}
}

func (conf *vsphereConfiguration) InstanceWaitForToolsRunning(name string) (bool, error) {
	return conf.WaitForToolsRunning(name)
}

func (conf *vsphereConfiguration) InstanceMaxPods(instanceType string, desiredMaxPods int) (int, error) {
	if desiredMaxPods == 0 {
		desiredMaxPods = 110
	}

	return desiredMaxPods, nil
}

func (conf *vsphereConfiguration) RegisterDNS(address string) error {
	return nil
}

func (conf *vsphereConfiguration) UnregisterDNS(address string) error {
	return nil
}

func (conf *vsphereConfiguration) getURL() (string, error) {
	u, err := url.Parse(conf.config.URL)

	if err != nil {
		return "", err
	}

	u.User = url.UserPassword(conf.config.UserName, conf.config.Password)

	return u.String(), err
}

func (conf *vsphereConfiguration) FindPreferredIPAddress(interfaces []NetworkInterface) string {
	address := ""

	for _, inf := range interfaces {
		if declaredInf := conf.FindInterfaceByName(inf.NetworkName); declaredInf != nil {
			if declaredInf.Primary {
				return inf.IPAddress
			}
		}
	}

	return address
}

func (conf *vsphereConfiguration) FindInterfaceByName(networkName string) *NetworkInterface {
	for _, inf := range conf.network.Interfaces {
		if inf.NetworkName == networkName {
			return inf
		}
	}

	return nil
}

// GetClient create a new govomi client
func (conf *vsphereConfiguration) GetClient(ctx *context.Context) (*Client, error) {
	var u *url.URL
	var sURL string
	var err error
	var c *govmomi.Client

	if sURL, err = conf.getURL(); err == nil {
		if u, err = soap.ParseURL(sURL); err == nil {
			// Connect and log in to ESX or vCenter
			if c, err = govmomi.NewClient(ctx, u, conf.config.Insecure); err == nil {
				return &Client{
					Client:        c,
					Configuration: conf.config,
				}, nil
			}
		}
	}
	return nil, err
}

// CreateWithContext will create a named VM not powered
// memory and disk are in megabytes
func (conf *vsphereConfiguration) CreateWithContext(ctx *context.Context, name string, nodeIndex int, userName, authKey string, cloudInit interface{}, network *Network, machine *providers.MachineCharacteristic) (*VirtualMachine, error) {
	var err error
	var client *Client
	var dc *Datacenter
	var ds *Datastore
	var vm *VirtualMachine

	if client, err = conf.GetClient(ctx); err == nil {
		if dc, err = client.GetDatacenter(ctx, conf.config.DataCenter); err == nil {
			if ds, err = dc.GetDatastore(ctx, conf.config.DataStore); err == nil {
				if vm, err = ds.CreateVirtualMachine(ctx, name, conf.config.TemplateName, conf.config.VMBasePath, conf.config.Resource, conf.config.Template, conf.config.LinkedClone, network, conf.config.Customization, nodeIndex); err == nil {
					err = vm.Configure(ctx, userName, authKey, cloudInit, network, "", true, machine.Memory, machine.Vcpu, machine.DiskSize, nodeIndex, conf.config.AllowUpgrade)
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
func (conf *vsphereConfiguration) Create(name string, nodeIndex int, userName, authKey string, cloudInit interface{}, network *Network, machine *providers.MachineCharacteristic) (*VirtualMachine, error) {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.CreateWithContext(ctx, name, nodeIndex, userName, authKey, cloudInit, network, machine)
}

// DeleteWithContext a VM by name
func (conf *vsphereConfiguration) DeleteWithContext(ctx *context.Context, name string) error {
	vm, err := conf.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.Delete(ctx)
}

// Delete a VM by name
func (conf *vsphereConfiguration) Delete(name string) error {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.DeleteWithContext(ctx, name)
}

// VirtualMachineWithContext  Retrieve VM by name
func (conf *vsphereConfiguration) VirtualMachineWithContext(ctx *context.Context, name string) (*VirtualMachine, error) {
	var err error
	var client *Client
	var dc *Datacenter
	var ds *Datastore

	if client, err = conf.GetClient(ctx); err == nil {
		if dc, err = client.GetDatacenter(ctx, conf.config.DataCenter); err == nil {
			if ds, err = dc.GetDatastore(ctx, conf.config.DataStore); err == nil {
				return ds.VirtualMachine(ctx, name)
			}
		}
	}

	return nil, err
}

// VirtualMachine  Retrieve VM by name
func (conf *vsphereConfiguration) VirtualMachine(name string) (*VirtualMachine, error) {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.VirtualMachineWithContext(ctx, name)
}

// VirtualMachineListWithContext return all VM for the current datastore
func (conf *vsphereConfiguration) VirtualMachineListWithContext(ctx *context.Context) ([]*VirtualMachine, error) {
	var err error
	var client *Client
	var dc *Datacenter
	var ds *Datastore

	if client, err = conf.GetClient(ctx); err == nil {
		if dc, err = client.GetDatacenter(ctx, conf.config.DataCenter); err == nil {
			if ds, err = dc.GetDatastore(ctx, conf.config.DataStore); err == nil {
				return ds.List(ctx)
			}
		}
	}

	return nil, err
}

// VirtualMachineList return all VM for the current datastore
func (conf *vsphereConfiguration) VirtualMachineList() ([]*VirtualMachine, error) {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.VirtualMachineListWithContext(ctx)
}

// UUID get VM UUID by name
func (conf *vsphereConfiguration) UUIDWithContext(ctx *context.Context, name string) (string, error) {
	vm, err := conf.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return "", err
	}

	return vm.UUID(ctx), nil
}

// UUID get VM UUID by name
func (conf *vsphereConfiguration) UUID(name string) (string, error) {
	if name == conf.instanceName {
		return conf.instanceID, nil
	}

	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.UUIDWithContext(ctx, name)
}

// WaitForIPWithContext wait ip a VM by name
func (conf *vsphereConfiguration) WaitForIPWithContext(ctx *context.Context, name string) (string, error) {

	if conf.testMode {
		return "127.0.0.1", nil
	}

	vm, err := conf.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return "", err
	}

	return vm.WaitForIP(ctx)
}

// WaitForIP wait ip a VM by name
func (conf *vsphereConfiguration) WaitForIP(name string) (string, error) {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.WaitForIPWithContext(ctx, name)
}

// SetAutoStartWithContext set autostart for the VM
func (conf *vsphereConfiguration) SetAutoStartWithContext(ctx *context.Context, esxi, name string, startOrder int) error {
	var err error = nil

	if !conf.testMode {
		var client *Client
		var dc *Datacenter
		var host *HostAutoStartManager

		if client, err = conf.GetClient(ctx); err == nil {
			if dc, err = client.GetDatacenter(ctx, conf.config.DataCenter); err == nil {
				if host, err = dc.GetHostAutoStartManager(ctx, esxi); err == nil {
					return host.SetAutoStart(ctx, conf.config.DataStore, name, startOrder)
				}
			}
		}
	}

	return err
}

// WaitForToolsRunningWithContext wait vmware tools is running a VM by name
func (conf *vsphereConfiguration) WaitForToolsRunningWithContext(ctx *context.Context, name string) (bool, error) {
	if conf.testMode {
		return true, nil
	}

	vm, err := conf.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return false, err
	}

	return vm.WaitForToolsRunning(ctx)
}

// WaitForToolsRunningWithContext wait vmware tools is running a VM by name
func (conf *vsphereConfiguration) WaitForPoweredWithContext(ctx *context.Context, name string) error {
	if conf.testMode {
		return nil
	}

	vm, err := conf.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.WaitForPowered(ctx)
}

func (conf *vsphereConfiguration) GetHostSystemWithContext(ctx *context.Context, name string) (string, error) {
	vm, err := conf.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return "*", err
	}

	return vm.HostSystem(ctx)
}

// GetHostSystem return the host where the virtual machine leave
func (conf *vsphereConfiguration) GetHostSystem(name string) (string, error) {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.GetHostSystemWithContext(ctx, name)
}

// SetAutoStart set autostart for the VM
func (conf *vsphereConfiguration) SetAutoStart(esxi, name string, startOrder int) error {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.SetAutoStartWithContext(ctx, esxi, name, startOrder)
}

// WaitForToolsRunning wait vmware tools is running a VM by name
func (conf *vsphereConfiguration) WaitForToolsRunning(name string) (bool, error) {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.WaitForToolsRunningWithContext(ctx, name)
}

// WaitForToolsRunning wait vmware tools is running a VM by name
func (conf *vsphereConfiguration) WaitForPowered(name string) error {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.WaitForPoweredWithContext(ctx, name)
}

// PowerOnWithContext power on a VM by name
func (conf *vsphereConfiguration) PowerOnWithContext(ctx *context.Context, name string) error {
	vm, err := conf.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.PowerOn(ctx)
}

// PowerOn power on a VM by name
func (conf *vsphereConfiguration) PowerOn(name string) error {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.PowerOnWithContext(ctx, name)
}

// PowerOffWithContext power off a VM by name
func (conf *vsphereConfiguration) PowerOffWithContext(ctx *context.Context, name string) error {
	vm, err := conf.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.PowerOff(ctx)
}

// ShutdownGuestWithContext power off a VM by name
func (conf *vsphereConfiguration) ShutdownGuestWithContext(ctx *context.Context, name string) error {
	vm, err := conf.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.ShutdownGuest(ctx)
}

// PowerOff power off a VM by name
func (conf *vsphereConfiguration) PowerOff(name string) error {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.PowerOffWithContext(ctx, name)
}

// ShutdownGuest power off a VM by name
func (conf *vsphereConfiguration) ShutdownGuest(name string) error {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.ShutdownGuestWithContext(ctx, name)
}

// StatusWithContext return the current status of VM by name
func (conf *vsphereConfiguration) StatusWithContext(ctx *context.Context, name string) (*Status, error) {
	vm, err := conf.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return nil, err
	}

	return vm.Status(ctx)
}

// Status return the current status of VM by name
func (conf *vsphereConfiguration) Status(name string) (*Status, error) {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.StatusWithContext(ctx, name)
}

func (conf *vsphereConfiguration) RetrieveNetworkInfosWithContext(ctx *context.Context, name string, nodeIndex int) error {
	vm, err := conf.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.collectNetworkInfos(ctx, &conf.network, nodeIndex)
}

// ExistsWithContext return the current status of VM by name
func (conf *vsphereConfiguration) ExistsWithContext(ctx *context.Context, name string) bool {
	if _, err := conf.VirtualMachineWithContext(ctx, name); err == nil {
		return true
	}

	return false
}

func (conf *vsphereConfiguration) Exists(name string) bool {
	ctx := context.NewContext(conf.config.Timeout)
	defer ctx.Cancel()

	return conf.ExistsWithContext(ctx, name)
}
