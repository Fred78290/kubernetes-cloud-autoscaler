package lxd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha2"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/rfc2136"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	golxd "github.com/canonical/lxd/client"
	"github.com/canonical/lxd/shared/api"
	glog "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type Remote struct {
	Addr     string `yaml:"addr"`
	AuthType string `yaml:"auth_type,omitempty"`
	Project  string `yaml:"project,omitempty"`
	Protocol string `yaml:"protocol,omitempty"`
	Public   bool   `yaml:"public"`
	Global   bool   `yaml:"-"`
	Static   bool   `yaml:"-"`
}

type NetworkInterface struct {
	Enabled     *bool  `json:"enabled,omitempty" yaml:"primary,omitempty"`
	Primary     bool   `json:"primary,omitempty" yaml:"primary,omitempty"`
	NicName     string `json:"nic,omitempty" yaml:"primary,omitempty"`
	NetworkName string `json:"network,omitempty" yaml:"network,omitempty"`
	DHCP        bool   `json:"dhcp,omitempty" yaml:"dhcp,omitempty"`
	IPAddress   string `json:"address,omitempty" yaml:"address,omitempty"`
}

type Network struct {
	Domain     string              `json:"domain,omitempty" yaml:"domain,omitempty"`
	Interfaces []*NetworkInterface `json:"interfaces,omitempty" yaml:"interfaces,omitempty"`
}

type Configuration struct {
	LxdServerURL      string            `json:"lxd-server-url,omitempty"`
	LxdConfigLocation string            `default:"/etc/lxd" json:"lxd-config-location,omitempty"`
	TLSServerCert     string            `json:"tls-server-cert,omitempty"`
	TLSClientCert     string            `json:"tls-client-cert,omitempty"`
	TLSClientKey      string            `json:"tls-client-key,omitempty"`
	TLSCA             string            `json:"tls-ca,omitempty"`
	ContainerType     api.InstanceType  `default:"container" json:"container-type,omitempty"`
	StoragePool       string            `default:"default" json:"storage-pool,omitempty"`
	Profiles          []string          `json:"profiles,omitempty"`
	Project           string            `json:"project,omitempty"`
	LxdRegion         string            `default:"home" json:"region"`
	LxdZone           string            `default:"office" json:"zone"`
	TemplateName      string            `json:"template-name,omitempty"`
	Timeout           time.Duration     `json:"timeout"`
	AvailableGPUTypes map[string]string `json:"gpu-types"`
	UseBind9          bool              `json:"use-bind9"`
	Bind9Host         string            `json:"bind9-host"`
	RndcKeyFile       string            `json:"rndc-key-file"`
	Network           Network           `json:"network"`
	Remotes           map[string]Remote `json:"remotes,omitempty"`
}

type lxdWrapper struct {
	Configuration
	client           golxd.InstanceServer
	network          *lxdNetwork
	bind9Provider    *rfc2136.RFC2136Provider
	imageFingerPrint string
	testMode         bool
}

type lxdHandler struct {
	*lxdWrapper
	attachedNetwork *lxdNetwork
	runningInstance *ServerInstance
	instanceName    string
	instanceType    string
	instanceID      string
	controlPlane    bool
	nodeIndex       int
}

func NewLxdProviderConfiguration(fileName string) (providers.ProviderConfiguration, error) {
	var wrapper lxdWrapper
	var err error

	if err = utils.LoadConfig(fileName, &wrapper.Configuration); err != nil {
		glog.Errorf("Failed to open file: %s, error: %v", fileName, err)

		return nil, err
	}

	if err = wrapper.ConfigurationDidLoad(); err != nil {
		return nil, err
	}

	return &wrapper, nil
}

func (wrapper *lxdWrapper) SetMode(test bool) {
	wrapper.testMode = test
}

func (wrapper *lxdWrapper) GetMode() bool {
	return wrapper.testMode
}

func (wrapper *lxdWrapper) AttachInstance(instanceName string, controlPlane bool, nodeIndex int) (handler providers.ProviderHandler, err error) {
	var instanceID string

	if instanceID, err = wrapper.UUID(instanceName); err == nil {
		network := wrapper.network.Clone(controlPlane, nodeIndex)
		handler = &lxdHandler{
			lxdWrapper:      wrapper,
			attachedNetwork: network,
			instanceName:    instanceName,
			instanceID:      instanceID,
			controlPlane:    controlPlane,
			nodeIndex:       nodeIndex,
			runningInstance: wrapper.newServerInstance(instanceName, instanceID, network, nodeIndex),
		}
	}

	return
}

func (wrapper *lxdWrapper) CreateInstance(instanceName, instanceType string, controlPlane bool, nodeIndex int) (handler providers.ProviderHandler, err error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if wrapper.InstanceExists(instanceName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, instanceName)
		err = fmt.Errorf(constantes.ErrVMAlreadyExists, instanceName)
	} else {
		handler = &lxdHandler{
			lxdWrapper:      wrapper,
			attachedNetwork: wrapper.network.Clone(controlPlane, nodeIndex),
			instanceType:    instanceType,
			instanceName:    instanceName,
			controlPlane:    controlPlane,
			nodeIndex:       nodeIndex,
		}
	}

	return
}

func (wrapper *lxdWrapper) findImageByName(imageServer golxd.ImageServer, name string) (fingerprint string, err error) {
	var alias *api.ImageAliasesEntry

	if alias, _, err = imageServer.GetImageAlias(name); err != nil || alias == nil {
		var images []api.Image

		if images, err = imageServer.GetImages(); err != nil {
			err = fmt.Errorf("image: %s not found, reason: %v", name, err)
		} else {
			for _, image := range images {
				if image.Properties["name"] == name {
					fingerprint = images[0].Fingerprint
					break
				}
			}
		}
	} else {
		fingerprint = alias.Target
	}

	if len(fingerprint) == 0 {
		err = fmt.Errorf("image: %s not found", name)
	}

	return
}

func (wrapper *lxdWrapper) copyImage(imageServer golxd.ImageServer, remote, name string) (fingerprint string, err error) {
	var image *api.Image
	var op golxd.RemoteOperation

	if fingerprint, err = wrapper.findImageByName(imageServer, name); err == nil {
		if _, _, err = wrapper.client.GetImage(fingerprint); err != nil {
			if image, _, err = imageServer.GetImage(fingerprint); err == nil {
				glog.Infof("Copy remote image %s:%s to local", remote, name)

				if op, err = wrapper.client.CopyImage(imageServer, *image, nil); err == nil {
					err = op.Wait()
				}

				glog.Infof("Copy done %s:%s, err: %v", remote, name, err)
			}
		} else {
			glog.Infof("remote image %s:%s found locally", remote, name)
		}
	}

	return
}

func (wrapper *lxdWrapper) findImage(name string) (fingerprint string, err error) {
	if strings.Contains(name, ":") {
		var imageServer golxd.ImageServer
		remote := providers.StringBefore(name, ":")
		name = providers.StringAfter(name, ":")

		glog.Infof("Get remote image information for %s:%s", remote, name)

		if remoteServer, found := wrapper.Remotes[remote]; found {
			if imageServer, err = golxd.ConnectSimpleStreams(remoteServer.Addr, nil); err != nil {
				err = fmt.Errorf("remote image server %s failed: reason: %v", remoteServer.Addr, err)
			} else if fingerprint, err = wrapper.copyImage(imageServer, remote, name); err != nil {
				err = fmt.Errorf("copy image server %s failed: reason: %v", remoteServer.Addr, err)
			}
		} else {
			err = fmt.Errorf("remote image server: %s not found", remote)
		}
	} else {
		fingerprint, err = wrapper.findImageByName(wrapper.client, name)
	}

	return
}

func (wrapper *lxdWrapper) readLxdPEM(pem string) (content string, err error) {
	if pem != "" {
		var b []byte

		if b, err = os.ReadFile(path.Join(wrapper.LxdConfigLocation, pem)); err == nil {
			content = string(b)
		}
	}

	return
}

func (wrapper *lxdWrapper) ConfigurationDidLoad() (err error) {
	if wrapper.Configuration.UseBind9 {
		if wrapper.bind9Provider, err = rfc2136.NewDNSRFC2136ProviderCredentials(wrapper.Configuration.Bind9Host, wrapper.Configuration.RndcKeyFile); err != nil {
			return err
		}
	}

	if strings.HasPrefix(wrapper.LxdServerURL, "unix:") {
		if wrapper.client, err = golxd.ConnectLXDUnix(wrapper.LxdServerURL[5:], nil); err != nil {
			return err
		}
	} else {
		var ca string
		var serverCert string
		var clientCert string
		var clientKey string

		if ca, err = wrapper.readLxdPEM(wrapper.TLSCA); err != nil {
			return err
		}

		if serverCert, err = wrapper.readLxdPEM(wrapper.TLSServerCert); err != nil {
			return err
		}

		if clientCert, err = wrapper.readLxdPEM(wrapper.TLSClientCert); err != nil {
			return err
		}

		if clientKey, err = wrapper.readLxdPEM(wrapper.TLSClientKey); err != nil {
			return err
		}

		args := golxd.ConnectionArgs{
			TLSServerCert: serverCert,
			TLSClientCert: clientCert,
			TLSClientKey:  clientKey,
			TLSCA:         ca,
		}

		if wrapper.client, err = golxd.ConnectLXD(wrapper.LxdServerURL, &args); err != nil {
			return err
		}
	}

	if wrapper.client == nil {
		return errors.New("no cloud provider config given")
	}

	wrapper.client = wrapper.client.UseProject(wrapper.Project)

	if wrapper.imageFingerPrint, err = wrapper.findImage(wrapper.TemplateName); err != nil {
		return
	}

	if len(wrapper.imageFingerPrint) == 0 {
		return fmt.Errorf("image: %s not found", wrapper.TemplateName)
	}

	network := wrapper.Configuration.Network
	wrapper.network = &lxdNetwork{
		Network: &providers.Network{
			Domain:     network.Domain,
			Interfaces: make([]*providers.NetworkInterface, 0, len(network.Interfaces)),
		},
	}

	for _, inf := range network.Interfaces {
		lxdInterface := &lxdNetworkInterface{
			NetworkInterface: &providers.NetworkInterface{
				Enabled:     inf.Enabled,
				MacAddress:  "ignore",
				Primary:     inf.Primary,
				NetworkName: inf.NetworkName,
				NicName:     inf.NicName,
				DHCP:        inf.DHCP,
				IPAddress:   inf.IPAddress,
				Netmask:     "255.255.255.255",
			},
		}

		wrapper.network.LxdInterfaces = append(wrapper.network.LxdInterfaces, lxdInterface)
		wrapper.network.Interfaces = append(wrapper.network.Interfaces, lxdInterface.NetworkInterface)

		if lxdInterface.network, _, err = wrapper.client.GetNetwork(inf.NetworkName); err != nil {
			return fmt.Errorf("unable to find network: %s, reason: %v", inf.NetworkName, err)
		}

		if !lxdInterface.network.Managed {
			return fmt.Errorf("unable to use network: %s, reason: network is not managed", inf.NetworkName)
		}

		lxdInterface.gateway = providers.StringBefore(lxdInterface.network.Config["ipv4.address"], "/")
	}

	wrapper.network.Network.ConfigurationDidLoad()

	return
}

func (wrapper *lxdWrapper) getAddress(server *api.InstanceFull, failIfMissingNetwork bool) (addressIP string, err error) {
	if server.State != nil {
		inf := wrapper.network.PrimaryInterface()

		if nic, found := server.State.Network[inf.NicName]; found {
			for _, address := range nic.Addresses {
				if address.Family == "inet" {
					return address.Address, nil
				}
			}

			if failIfMissingNetwork {
				err = fmt.Errorf("instance: %s doesn't have address", server.Name)
			}
		} else if failIfMissingNetwork {
			err = fmt.Errorf("instance: %s doesn't have nic", server.Name)
		}
	}

	return
}

func (wrapper *lxdWrapper) getServerInstance(name string) (vm *ServerInstance, err error) {
	var instance *api.InstanceFull
	var addressIP string

	if instance, _, err = wrapper.client.GetInstanceFull(name); err != nil {
		return
	}

	if addressIP, err = wrapper.getAddress(instance, false); err == nil {
		vm = &ServerInstance{
			lxdWrapper:     wrapper,
			InstanceName:   instance.Name,
			InstanceID:     instance.Config["volatile.uuid"],
			AddressIP:      addressIP,
			Location:       instance.Location,
			PrivateDNSName: instance.Name,
		}
	}
	return
}

func (wrapper *lxdWrapper) newServerInstance(instanceName, instanceID string, network *lxdNetwork, nodeIndex int) *ServerInstance {
	return &ServerInstance{
		lxdWrapper:      wrapper,
		InstanceName:    instanceName,
		InstanceID:      instanceID,
		NodeIndex:       nodeIndex,
		PrivateDNSName:  instanceName,
		attachedNetwork: network,
	}
}

func (wrapper *lxdWrapper) GetAvailableGpuTypes() map[string]string {
	return wrapper.AvailableGPUTypes
}

func (wrapper *lxdWrapper) InstanceExists(name string) (exists bool) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if _, err := wrapper.getServerInstance(name); err != nil {
		return false
	}

	return true
}

func (wrapper *lxdWrapper) UUID(name string) (vmuuid string, err error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if server, err := wrapper.getServerInstance(name); err != nil {
		return "", err
	} else {
		return server.InstanceID, nil
	}
}

func (wrapper *lxdWrapper) RetrieveNetworkInfos(vmuuid string, network *providers.Network) (err error) {
	return
}

func (handler *lxdHandler) GetTimeout() time.Duration {
	return handler.Timeout
}

func (handler *lxdHandler) ConfigureNetwork(network v1alpha2.ManagedNetworkConfig) {
	handler.attachedNetwork.ConfigureManagedNetwork(network.Lxd.Managed())
}

func (handler *lxdHandler) RetrieveNetworkInfos() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	return handler.lxdWrapper.RetrieveNetworkInfos(handler.instanceID, handler.attachedNetwork.Network)
}

func (handler *lxdHandler) UpdateMacAddressTable() error {
	if handler.instanceID == "" {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.attachedNetwork.UpdateMacAddressTable()
}

func (handler *lxdHandler) GenerateProviderID() string {
	if len(handler.instanceID) > 0 {
		return handler.instanceID
	} else {
		return ""
	}
}

func (handler *lxdHandler) GetTopologyLabels() map[string]string {
	return map[string]string{
		constantes.NodeLabelTopologyRegion:  handler.LxdRegion,
		constantes.NodeLabelTopologyZone:    handler.LxdZone,
		constantes.NodeLabelVMWareCSIRegion: handler.LxdRegion,
		constantes.NodeLabelVMWareCSIZone:   handler.LxdZone,
	}
}

func (handler *lxdHandler) InstanceCreate(input *providers.InstanceCreateInput) (vmuuid string, err error) {
	var userData string

	handler.runningInstance = handler.newServerInstance(handler.instanceName, "", handler.attachedNetwork, handler.nodeIndex)

	if userData, err = handler.encodeCloudInit(input.CloudInit); err != nil {
		return "", err
	}

	if err = handler.runningInstance.Create(input.ControlPlane, input.NodeGroup, userData, input.Machine); err != nil {
		return "", err
	}

	handler.instanceID = handler.runningInstance.InstanceID

	return handler.runningInstance.InstanceID, nil
}

func (handler *lxdHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (address string, err error) {
	if handler.runningInstance == nil {
		return "", fmt.Errorf("instance not attached when calling WaitForVMReady")
	}

	return handler.runningInstance.WaitForIP(callback)
}

func (handler *lxdHandler) InstancePrimaryAddressIP() (address string) {
	return handler.attachedNetwork.PrimaryAddressIP()
}

func (handler *lxdHandler) InstanceID() (vmuuid string, err error) {
	if handler.runningInstance == nil {
		return "", fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.InstanceID, nil
}

func (handler *lxdHandler) InstanceAutoStart() (err error) {
	return
}

func (handler *lxdHandler) InstancePowerOn() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	return handler.runningInstance.PowerOn(ctx)
}

func (handler *lxdHandler) InstancePowerOff() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	return handler.runningInstance.PowerOff(ctx)
}

func (handler *lxdHandler) InstanceShutdownGuest() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.ShutdownGuest()
}

func (handler *lxdHandler) InstanceDelete() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.Delete()
}

func (handler *lxdHandler) InstanceCreated() bool {
	if handler.runningInstance == nil {
		return false
	}

	return handler.runningInstance.InstanceExists(handler.instanceName)
}

func (handler *lxdHandler) InstanceStatus() (status providers.InstanceStatus, err error) {
	if handler.runningInstance == nil {
		return nil, fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.Status()
}

func (handler *lxdHandler) InstanceWaitForPowered() (err error) {
	if handler.runningInstance == nil {
		return fmt.Errorf(constantes.ErrInstanceIsNotAttachedToCloudProvider)
	}

	return handler.runningInstance.WaitForPowered()
}

func (handler *lxdHandler) InstanceWaitForToolsRunning() (bool, error) {
	return true, context.PollImmediate(250*time.Millisecond, handler.Timeout*time.Second, func() (bool, error) {
		if instance, _, err := handler.client.GetInstanceFull(handler.instanceName); err != nil {
			return false, err
		} else if handler.runningInstance.AddressIP, err = handler.getAddress(instance, true); err != nil {
			return false, nil
		}

		return true, nil
	})
}

func (handler *lxdHandler) InstanceMaxPods(desiredMaxPods int) (int, error) {
	if desiredMaxPods == 0 {
		desiredMaxPods = 110
	}

	return desiredMaxPods, nil
}

func (handler *lxdHandler) PrivateDNSName() (string, error) {
	return handler.instanceName, nil
}

func (handler *lxdHandler) RegisterDNS(address string) (err error) {
	if handler.bind9Provider != nil {
		err = handler.bind9Provider.AddRecord(handler.instanceName+"."+handler.Network.Domain, handler.Network.Domain, address)
	}

	return
}

func (handler *lxdHandler) UnregisterDNS(address string) (err error) {
	if handler.bind9Provider != nil {
		err = handler.bind9Provider.RemoveRecord(handler.instanceName+"."+handler.Network.Domain, handler.Network.Domain, address)
	}

	return
}

func (handler *lxdHandler) UUID(name string) (string, error) {
	if handler.runningInstance != nil && handler.runningInstance.InstanceName == name {
		return handler.runningInstance.InstanceID, nil
	}

	ctx := context.NewContext(handler.Timeout)
	defer ctx.Cancel()

	if server, err := handler.getServerInstance(name); err != nil {
		return "", err
	} else {
		return server.InstanceID, nil
	}
}

func (handler *lxdHandler) encodeCloudInit(object any) (result string, err error) {
	if object == nil {
		return
	}

	var out bytes.Buffer

	fmt.Fprintln(&out, "#cloud-config")

	wr := yaml.NewEncoder(&out)
	err = wr.Encode(object)
	wr.Close()

	if err != nil {
		return
	}

	result = out.String()
	return
}
