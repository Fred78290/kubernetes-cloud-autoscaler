package vsphere

import (
	"fmt"
	"net/url"
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
	//	NodeGroup         string            `json:"nodegroup"`
	URL               string            `json:"url"`
	UserName          string            `json:"uid"`
	Password          string            `json:"password"`
	Insecure          bool              `json:"insecure"`
	DataCenter        string            `json:"dc"`
	DataStore         string            `json:"datastore"`
	Resource          string            `json:"resource-pool"`
	VMBasePath        string            `json:"vmFolder"`
	Timeout           time.Duration     `json:"timeout"`
	Annotation        string            `json:"annotation"`
	TemplateName      string            `json:"template-name"`
	Template          bool              `json:"template"`
	LinkedClone       bool              `json:"linked"`
	AllowUpgrade      bool              `default:"true" json:"allow-upgrade"`
	Customization     string            `json:"customization"`
	Network           providers.Network `json:"network"`
	AvailableGPUTypes map[string]string `json:"gpu-types"`
	VMWareRegion      string            `json:"csi-region"`
	VMWareZone        string            `json:"csi-zone"`
	TestMode          bool              `json:"test-mode"`
}

type vsphereWrapper struct {
	Configuration
}

type vsphereHandler struct {
	*vsphereWrapper
	network      *vsphereNetwork
	instanceType string
	instanceName string
	instanceID   string
	controlPlane bool
	nodeIndex    int
}

func NewVSphereProviderConfiguration(fileName string) (providers.ProviderConfiguration, error) {
	var wrapper vsphereWrapper

	if err := providers.LoadConfig(fileName, &wrapper.Configuration); err != nil {
		glog.Errorf("Failed to open file: %s, error: %v", fileName, err)

		return nil, err
	} else if _, err = wrapper.UUID(wrapper.TemplateName); err != nil {
		return nil, err
	}

	wrapper.Configuration.Network.ConfigurationDidLoad()

	return &wrapper, nil
}

// Status shortened vm status
type Status struct {
	Interfaces []providers.NetworkInterface
	Powered    bool
}

type VmStatus struct {
	Status
	address string
}

type CreateInput struct {
	*providers.InstanceCreateInput
	NodeName        string
	NodeIndex       int
	AllowUpgrade    bool
	ExpandHardDrive bool
	Annotation      string
	VSphereNetwork  *vsphereNetwork
}

func (status *VmStatus) Address() string {
	return status.address
}

func (status *VmStatus) Powered() bool {
	return status.Status.Powered
}

func (handler *vsphereHandler) GetTimeout() time.Duration {
	return handler.Timeout
}

func (handler *vsphereHandler) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
	handler.network.ConfigureNetwork(network)
}

func (handler *vsphereHandler) RetrieveNetworkInfos() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	return handler.RetrieveNetworkInfosWithContext(ctx, handler.instanceName, handler.nodeIndex, handler.network)
}

func (handler *vsphereHandler) UpdateMacAddressTable() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.network.UpdateMacAddressTable()
}

func (handler *vsphereHandler) GenerateProviderID() string {
	if len(handler.instanceID) > 0 {
		return fmt.Sprintf("vsphere://%s", handler.instanceID)
	} else {
		return ""
	}
}

func (handler *vsphereHandler) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion:  handler.VMWareRegion,
		constantes.NodeLabelTopologyZone:    handler.VMWareZone,
		constantes.NodeLabelVMWareCSIRegion: handler.VMWareRegion,
		constantes.NodeLabelVMWareCSIZone:   handler.VMWareZone,
	}
}

func (handler *vsphereHandler) InstanceCreate(input *providers.InstanceCreateInput) (string, error) {
	createInput := &CreateInput{
		InstanceCreateInput: input,
		NodeName:            handler.instanceName,
		NodeIndex:           handler.nodeIndex,
		AllowUpgrade:        handler.AllowUpgrade,
		ExpandHardDrive:     true,
		Annotation:          handler.Annotation,
		VSphereNetwork:      handler.network,
	}

	if vm, err := handler.Create(createInput); err != nil {
		return "", err
	} else {
		ctx := context.NewContext(handler.Timeout)
		defer ctx.Cancel()

		handler.instanceID = vm.UUID(ctx)

		return handler.instanceID, err
	}
}

func (handler *vsphereHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	if handler.instanceName == "" {
		return "", fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	if ip, err := handler.WaitForIP(handler.instanceName); err != nil {
		return ip, err
	} else {
		if err := context.PollImmediate(time.Second, handler.Timeout*time.Second, func() (bool, error) {
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

func (handler *vsphereHandler) InstancePrimaryAddressIP() string {
	return handler.network.PrimaryAddressIP()
}

func (handler *vsphereHandler) InstanceID() (string, error) {
	if handler.instanceName == "" {
		return "", fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.UUID(handler.instanceName)
}

func (handler *vsphereHandler) InstanceExists(name string) bool {
	return handler.Exists(handler.instanceName)
}

func (handler *vsphereHandler) InstanceAutoStart() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	if hostsystem, err := handler.GetHostSystem(handler.instanceName); err != nil {
		return err
	} else if err = handler.SetAutoStart(hostsystem, handler.instanceName, -1); err != nil {
		return err
	}

	return nil
}

func (handler *vsphereHandler) InstancePowerOn() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.PowerOn(handler.instanceName)
}

func (handler *vsphereHandler) InstancePowerOff() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.PowerOff(handler.instanceName)
}

func (handler *vsphereHandler) InstanceShutdownGuest() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.ShutdownGuest(handler.instanceName)
}

func (handler *vsphereHandler) InstanceDelete() error {
	if handler.instanceName == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.Delete(handler.instanceName)
}

func (handler *vsphereHandler) InstanceStatus() (providers.InstanceStatus, error) {
	if handler.instanceName == "" {
		return nil, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	if status, err := handler.Status(handler.instanceName); err != nil {
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

	return handler.WaitForPowered(handler.instanceName)
}

func (handler *vsphereHandler) InstanceWaitForToolsRunning() (bool, error) {
	if handler.instanceName == "" {
		return false, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.WaitForToolsRunning(handler.instanceName)
}

func (handler *vsphereHandler) InstanceMaxPods(desiredMaxPods int) (int, error) {
	if desiredMaxPods == 0 {
		desiredMaxPods = 110
	}

	return desiredMaxPods, nil
}

func (handler *vsphereHandler) PrivateDNSName() (string, error) {
	return handler.instanceName, nil
}

func (handler *vsphereHandler) RegisterDNS(address string) error {
	return nil
}

func (handler *vsphereHandler) UnregisterDNS(address string) error {
	return nil
}

func (handler *vsphereHandler) findPreferredIPAddress(interfaces []providers.NetworkInterface) string {
	address := ""

	for _, inf := range interfaces {
		if declaredInf := handler.network.InterfaceByName(inf.NetworkName); declaredInf != nil {
			if declaredInf.Primary {
				return inf.IPAddress
			}
		}
	}

	return address
}

func (wrapper *vsphereWrapper) AttachInstance(instanceName string, controlPlane bool, nodeIndex int) (providers.ProviderHandler, error) {
	if vmuuid, err := wrapper.UUID(instanceName); err != nil {
		return nil, err
	} else {
		return &vsphereHandler{
			vsphereWrapper: wrapper,
			network:        newVSphereNetwork(&wrapper.Network, controlPlane, nodeIndex),
			instanceName:   instanceName,
			instanceID:     vmuuid,
			controlPlane:   controlPlane,
			nodeIndex:      nodeIndex,
		}, nil
	}
}

func (wrapper *vsphereWrapper) CreateInstance(instanceName, instanceType string, controlPlane bool, nodeIndex int) (providers.ProviderHandler, error) {
	if wrapper.InstanceExists(instanceName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, instanceName)
		return nil, fmt.Errorf(constantes.ErrVMAlreadyExists, instanceName)
	}

	return &vsphereHandler{
		vsphereWrapper: wrapper,
		network:        newVSphereNetwork(&wrapper.Network, controlPlane, nodeIndex),
		instanceType:   instanceType,
		instanceName:   instanceName,
		nodeIndex:      nodeIndex,
	}, nil
}

func (wrapper *vsphereWrapper) InstanceExists(name string) bool {
	return wrapper.Exists(name)
}

func (wrapper *vsphereWrapper) GetAvailableGpuTypes() map[string]string {
	return wrapper.AvailableGPUTypes
}

func (wrapper *vsphereWrapper) UUIDWithContext(ctx *context.Context, name string) (string, error) {
	vm, err := wrapper.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return "", err
	}

	return vm.UUID(ctx), nil
}

func (wrapper *vsphereWrapper) UUID(name string) (string, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.UUIDWithContext(ctx, name)
}

// GetClient create a new govomi client
func (wrapper *vsphereWrapper) GetClient(ctx *context.Context) (*Client, error) {
	var u *url.URL
	var sURL string
	var err error
	var c *govmomi.Client

	if sURL, err = wrapper.getURL(); err == nil {
		if u, err = soap.ParseURL(sURL); err == nil {
			// Connect and log in to ESX or vCenter
			if c, err = govmomi.NewClient(ctx, u, wrapper.Insecure); err == nil {
				return &Client{
					Client: c,
				}, nil
			}
		}
	}
	return nil, err
}

// CreateWithContext will create a named VM not powered
// memory and disk are in megabytes
func (wrapper *vsphereWrapper) CreateWithContext(ctx *context.Context, input *CreateInput) (*VirtualMachine, error) {
	var err error
	var client *Client
	var dc *Datacenter
	var ds *Datastore
	var vm *VirtualMachine

	createInput := &CreateVirtualMachineInput{
		CreateInput:   input,
		TemplateName:  wrapper.TemplateName,
		VMFolder:      wrapper.VMBasePath,
		ResourceName:  wrapper.Resource,
		Customization: wrapper.Customization,
		Template:      wrapper.Template,
		LinkedClone:   wrapper.LinkedClone,
	}

	if client, err = wrapper.GetClient(ctx); err == nil {
		if dc, err = client.GetDatacenter(ctx, wrapper.DataCenter); err == nil {
			if ds, err = dc.GetDatastore(ctx, wrapper.DataStore); err == nil {
				if vm, err = ds.CreateVirtualMachine(ctx, createInput); err == nil {
					err = vm.Configure(ctx, input)
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
func (wrapper *vsphereWrapper) Create(input *CreateInput) (*VirtualMachine, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.CreateWithContext(ctx, input)
}

// DeleteWithContext a VM by name
func (wrapper *vsphereWrapper) DeleteWithContext(ctx *context.Context, name string) error {
	vm, err := wrapper.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.Delete(ctx)
}

// Delete a VM by name
func (wrapper *vsphereWrapper) Delete(name string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.DeleteWithContext(ctx, name)
}

// VirtualMachineWithContext  Retrieve VM by name
func (wrapper *vsphereWrapper) VirtualMachineWithContext(ctx *context.Context, name string) (*VirtualMachine, error) {
	var err error
	var client *Client
	var dc *Datacenter
	var ds *Datastore

	if client, err = wrapper.GetClient(ctx); err == nil {
		if dc, err = client.GetDatacenter(ctx, wrapper.DataCenter); err == nil {
			if ds, err = dc.GetDatastore(ctx, wrapper.DataStore); err == nil {
				return ds.VirtualMachine(ctx, name)
			}
		}
	}

	return nil, err
}

// VirtualMachine  Retrieve VM by name
func (wrapper *vsphereWrapper) VirtualMachine(name string) (*VirtualMachine, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.VirtualMachineWithContext(ctx, name)
}

// VirtualMachineListWithContext return all VM for the current datastore
func (wrapper *vsphereWrapper) VirtualMachineListWithContext(ctx *context.Context) ([]*VirtualMachine, error) {
	var err error
	var client *Client
	var dc *Datacenter
	var ds *Datastore

	if client, err = wrapper.GetClient(ctx); err == nil {
		if dc, err = client.GetDatacenter(ctx, wrapper.DataCenter); err == nil {
			if ds, err = dc.GetDatastore(ctx, wrapper.DataStore); err == nil {
				return ds.List(ctx)
			}
		}
	}

	return nil, err
}

// VirtualMachineList return all VM for the current datastore
func (wrapper *vsphereWrapper) VirtualMachineList() ([]*VirtualMachine, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.VirtualMachineListWithContext(ctx)
}

// UUID get VM UUID by name
func (handler *vsphereHandler) UUID(name string) (string, error) {
	return handler.vsphereWrapper.UUID(name)
}

func (wrapper *vsphereWrapper) getURL() (string, error) {
	u, err := url.Parse(wrapper.URL)

	if err != nil {
		return "", err
	}

	u.User = url.UserPassword(wrapper.UserName, wrapper.Password)

	return u.String(), err
}

// WaitForIPWithContext wait ip a VM by name
func (wrapper *vsphereWrapper) WaitForIPWithContext(ctx *context.Context, name string) (string, error) {

	if wrapper.TestMode {
		return "127.0.0.1", nil
	}

	vm, err := wrapper.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return "", err
	}

	return vm.WaitForIP(ctx)
}

// WaitForIP wait ip a VM by name
func (wrapper *vsphereWrapper) WaitForIP(name string) (string, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.WaitForIPWithContext(ctx, name)
}

// SetAutoStartWithContext set autostart for the VM
func (wrapper *vsphereWrapper) SetAutoStartWithContext(ctx *context.Context, esxi, name string, startOrder int) error {
	var err error = nil

	if !wrapper.TestMode {
		var client *Client
		var dc *Datacenter
		var host *HostAutoStartManager

		if client, err = wrapper.GetClient(ctx); err == nil {
			if dc, err = client.GetDatacenter(ctx, wrapper.DataCenter); err == nil {
				if host, err = dc.GetHostAutoStartManager(ctx, esxi); err == nil {
					return host.SetAutoStart(ctx, wrapper.DataStore, name, startOrder)
				}
			}
		}
	}

	return err
}

// WaitForToolsRunningWithContext wait vmware tools is running a VM by name
func (wrapper *vsphereWrapper) WaitForToolsRunningWithContext(ctx *context.Context, name string) (bool, error) {
	if wrapper.TestMode {
		return true, nil
	}

	vm, err := wrapper.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return false, err
	}

	return vm.WaitForToolsRunning(ctx)
}

// WaitForToolsRunningWithContext wait vmware tools is running a VM by name
func (wrapper *vsphereWrapper) WaitForPoweredWithContext(ctx *context.Context, name string) error {
	if wrapper.TestMode {
		return nil
	}

	vm, err := wrapper.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.WaitForPowered(ctx)
}

func (wrapper *vsphereWrapper) GetHostSystemWithContext(ctx *context.Context, name string) (string, error) {
	vm, err := wrapper.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return "*", err
	}

	return vm.HostSystem(ctx)
}

// GetHostSystem return the host where the virtual machine leave
func (wrapper *vsphereWrapper) GetHostSystem(name string) (string, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.GetHostSystemWithContext(ctx, name)
}

// SetAutoStart set autostart for the VM
func (wrapper *vsphereWrapper) SetAutoStart(esxi, name string, startOrder int) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.SetAutoStartWithContext(ctx, esxi, name, startOrder)
}

// WaitForToolsRunning wait vmware tools is running a VM by name
func (wrapper *vsphereWrapper) WaitForToolsRunning(name string) (bool, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.WaitForToolsRunningWithContext(ctx, name)
}

// WaitForToolsRunning wait vmware tools is running a VM by name
func (wrapper *vsphereWrapper) WaitForPowered(name string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.WaitForPoweredWithContext(ctx, name)
}

// PowerOnWithContext power on a VM by name
func (wrapper *vsphereWrapper) PowerOnWithContext(ctx *context.Context, name string) error {
	vm, err := wrapper.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.PowerOn(ctx)
}

// PowerOn power on a VM by name
func (wrapper *vsphereWrapper) PowerOn(name string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.PowerOnWithContext(ctx, name)
}

// PowerOffWithContext power off a VM by name
func (wrapper *vsphereWrapper) PowerOffWithContext(ctx *context.Context, name string) error {
	vm, err := wrapper.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.PowerOff(ctx)
}

// ShutdownGuestWithContext power off a VM by name
func (wrapper *vsphereWrapper) ShutdownGuestWithContext(ctx *context.Context, name string) error {
	vm, err := wrapper.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.ShutdownGuest(ctx)
}

// PowerOff power off a VM by name
func (wrapper *vsphereWrapper) PowerOff(name string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.PowerOffWithContext(ctx, name)
}

// ShutdownGuest power off a VM by name
func (wrapper *vsphereWrapper) ShutdownGuest(name string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.ShutdownGuestWithContext(ctx, name)
}

// StatusWithContext return the current status of VM by name
func (wrapper *vsphereWrapper) StatusWithContext(ctx *context.Context, name string) (*Status, error) {
	vm, err := wrapper.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return nil, err
	}

	return vm.Status(ctx)
}

// Status return the current status of VM by name
func (wrapper *vsphereWrapper) Status(name string) (*Status, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.StatusWithContext(ctx, name)
}

func (wrapper *vsphereWrapper) RetrieveNetworkInfosWithContext(ctx *context.Context, name string, nodeIndex int, network *vsphereNetwork) error {
	vm, err := wrapper.VirtualMachineWithContext(ctx, name)

	if err != nil {
		return err
	}

	return vm.collectNetworkInfos(ctx, network)
}

func (wrapper *vsphereWrapper) RetrieveNetworkInfos(name string, nodeIndex int, network *vsphereNetwork) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.RetrieveNetworkInfosWithContext(ctx, name, nodeIndex, network)
}

// ExistsWithContext return the current status of VM by name
func (wrapper *vsphereWrapper) ExistsWithContext(ctx *context.Context, name string) bool {
	if _, err := wrapper.VirtualMachineWithContext(ctx, name); err == nil {
		return true
	}

	return false
}

func (wrapper *vsphereWrapper) Exists(name string) bool {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	return wrapper.ExistsWithContext(ctx, name)
}
