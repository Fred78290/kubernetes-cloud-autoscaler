package v1alpha2

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManagedNode is a specification for a ManagedNode resource
type ManagedNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ManagedNodeSpec   `json:"spec,omitempty"`
	Status            ManagedNodeStatus `json:"status,omitempty"`
}

// ManagedNodeNetwork is a specification for a network ManagedNode resource
type AwsManagedNodeNetwork struct {
	SubnetID           string `json:"subnetID,omitempty"`
	SecurityGroupID    string `json:"securityGroup,omitempty"`
	NetworkInterfaceID string `json:"networkInterfaceID,omitempty"`
	PrivateAddress     string `json:"privateAddress,omitempty"`
	PublicIP           bool   `json:"publicIP,omitempty"`
}

// NetworkRoutes is a specification for a network route ManagedNode resource
type NetworkRoutes struct {
	To     string `json:"to,omitempty" yaml:"to,omitempty"`
	Via    string `json:"via,omitempty" yaml:"via,omitempty"`
	Metric int    `json:"metric,omitempty" yaml:"metric,omitempty"`
}

// ComonManagedNodeNetwork is a specification for a common network ManagedNode resource
type CommonManagedNodeNetwork struct {
	NetworkName string `json:"network,omitempty"` //vnet for desktop
	DHCP        bool   `json:"dhcp,omitempty"`
	IPV4Address string `json:"address,omitempty"`
	Netmask     string `json:"netmask,omitempty"`
}

// ManagedNodeNetwork is a specification for a network ManagedNode resource
type VMWareManagedNodeNetwork struct {
	CommonManagedNodeNetwork
	Adapter    string          `json:"adapter,omitempty" yaml:"adapter,omitempty"`
	UseRoutes  *bool           `json:"use-dhcp-routes,omitempty" yaml:"use-dhcp-routes,omitempty"`
	MacAddress string          `json:"mac-address,omitempty" yaml:"mac-address,omitempty"`
	Routes     []NetworkRoutes `json:"routes,omitempty" yaml:"routes,omitempty"`
}

type MultipassManagedNodeNetwork struct {
	CommonManagedNodeNetwork
	UseRoutes  *bool           `json:"use-dhcp-routes,omitempty" yaml:"use-dhcp-routes,omitempty"`
	MacAddress string          `json:"mac-address,omitempty" yaml:"mac-address,omitempty"`
	Routes     []NetworkRoutes `json:"routes,omitempty" yaml:"routes,omitempty"`
}

type OpenStackManagedNodeNetwork struct {
	CommonManagedNodeNetwork
}

type LxdManagedNodeNetwork struct {
	CommonManagedNodeNetwork
	Routes []NetworkRoutes `json:"routes,omitempty" yaml:"routes,omitempty"`
}

type CloudStackManagedNodeNetwork struct {
	CommonManagedNodeNetwork
}

type OpenStackManagedNodeNetworks []OpenStackManagedNodeNetwork

func (m OpenStackManagedNodeNetworks) Managed() (managed []ManagedNetworkInterface) {
	for _, net := range m {
		managed = append(managed, &net)
	}

	return
}

type CloudStackManagedNodeNetworks []CloudStackManagedNodeNetwork

func (m CloudStackManagedNodeNetworks) Managed() (managed []ManagedNetworkInterface) {
	for _, net := range m {
		managed = append(managed, &net)
	}

	return
}

type VMWareManagedNodeNetworks []VMWareManagedNodeNetwork

func (m VMWareManagedNodeNetworks) Managed() (managed []ManagedNetworkInterface) {
	for _, net := range m {
		managed = append(managed, &net)
	}

	return
}

type MultipassManagedNodeNetworks []MultipassManagedNodeNetwork

func (m MultipassManagedNodeNetworks) Managed() (managed []ManagedNetworkInterface) {
	for _, net := range m {
		managed = append(managed, &net)
	}

	return
}

type LxdManagedNodeNetworks []LxdManagedNodeNetwork

func (m LxdManagedNodeNetworks) Managed() (managed []ManagedNetworkInterface) {
	for _, net := range m {
		managed = append(managed, &net)
	}

	return
}

type ManagedNetworkConfig struct {
	OpenStack  OpenStackManagedNodeNetworks  `json:"openstack,omitempty"`
	CloudStack CloudStackManagedNodeNetworks `json:"cloudstack,omitempty"`
	VMWare     VMWareManagedNodeNetworks     `json:"vmware,omitempty"`
	Multipass  MultipassManagedNodeNetworks  `json:"multipass,omitempty"`
	Lxd        LxdManagedNodeNetworks        `json:"lxd,omitempty"`
	ENI        *AwsManagedNodeNetwork        `json:"eni,omitempty"`
}

type ManagedNetworkInterface interface {
	IsDHCP() bool
	GetAdapter() string
	GetNetworkName() string
	GetIPV4Address() string
	GetNetmask() string
	GetUseDhcpRoutes() *bool
	GetRoutes() []NetworkRoutes
	GetMacAddress() string
}

// ManagedNodeSpec is the spec for a ManagedNode resource
type ManagedNodeSpec struct {
	Nodegroup       string               `default:"vmware-ca-k8s" json:"nodegroup,omitempty"`
	ControlPlane    bool                 `json:"controlPlane,omitempty"`
	AllowDeployment bool                 `json:"allowDeployment,omitempty"`
	InstanceType    string               `default:"t2.micro" json:"instanceType"`
	DiskSizeInMB    int                  `default:"10240" json:"diskSizeInMB"`
	DiskType        string               `default:"gp3" json:"diskType"`
	Labels          []string             `json:"labels,omitempty"`
	Annotations     []string             `json:"annotations,omitempty"`
	Networking      ManagedNetworkConfig `json:"network,omitempty"`
}

// ManagedNodeStatus is the status for a ManagedNode resource
type ManagedNodeStatus struct {
	// The last time this status was updated.
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// The node name created
	NodeName string `json:"nodename,omitempty"`
	// The instance created
	InstanceName string `json:"instancename,omitempty"`
	InstanceID   string `json:"instanceid,omitempty"`
	// A human-readable description of the status of this operation.
	// +optional
	Message string `json:"message,omitempty"`
	// A machine-readable description of why this operation is in the
	// "Failure" status. If this value is empty there
	// is no information available. A Reason clarifies an HTTP status
	// code but does not override it.
	// +optional
	Reason metav1.StatusReason `json:"reason,omitempty"`
	// Suggested HTTP return code for this status, 0 if not set.
	// +optional
	Code int32 `json:"code,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManagedNodeList is a list of ManagedNode resources
type ManagedNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ManagedNode `json:"items"`
}

func (mn *ManagedNode) GetNodegroup() string {
	return mn.Spec.Nodegroup
}

// ** CommonManagedNodeNetwork
func (m *CommonManagedNodeNetwork) IsDHCP() bool {
	return m.DHCP
}

func (m *CommonManagedNodeNetwork) GetAdapter() string {
	return ""
}

func (m *CommonManagedNodeNetwork) GetNetworkName() string {
	return m.NetworkName
}

func (m *CommonManagedNodeNetwork) GetIPV4Address() string {
	return m.IPV4Address
}

func (m *CommonManagedNodeNetwork) GetNetmask() string {
	return m.IPV4Address
}

func (m *CommonManagedNodeNetwork) GetUseDhcpRoutes() *bool {
	return aws.Bool(true)
}

func (m *CommonManagedNodeNetwork) GetRoutes() []NetworkRoutes {
	return nil
}

func (m *CommonManagedNodeNetwork) GetMacAddress() string {
	return ""
}

// ** VMWareManagedNodeNetwork
func (m *VMWareManagedNodeNetwork) GetAdapter() string {
	return m.Adapter
}

func (m *VMWareManagedNodeNetwork) GetUseDhcpRoutes() *bool {
	return m.UseRoutes
}

func (m *VMWareManagedNodeNetwork) GetRoutes() []NetworkRoutes {
	return m.Routes
}

func (m *VMWareManagedNodeNetwork) GetMacAddress() string {
	return m.MacAddress
}

// ** MultipassManagedNodeNetwork
func (m *MultipassManagedNodeNetwork) GetUseDhcpRoutes() *bool {
	return m.UseRoutes
}

func (m *MultipassManagedNodeNetwork) GetRoutes() []NetworkRoutes {
	return m.Routes
}

func (m *MultipassManagedNodeNetwork) GetMacAddress() string {
	return m.MacAddress
}

// ** LxdManagedNodeNetwork
func (m *LxdManagedNodeNetwork) GetRoutes() []NetworkRoutes {
	return m.Routes
}
