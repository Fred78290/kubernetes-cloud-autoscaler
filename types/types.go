package types

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	apigrpc "github.com/Fred78290/kubernetes-cloud-autoscaler/grpc"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/aws"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/desktop"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/multipass"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/vsphere"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/sshutils"
	"github.com/alecthomas/kingpin"
	glog "github.com/sirupsen/logrus"

	clientset "github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/generated/clientset/versioned"
	apiv1 "k8s.io/api/core/v1"
	apiextension "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
)

const (
	DefaultMaxGracePeriod    time.Duration = 120 * time.Second
	DefaultMaxRequestTimeout time.Duration = 120 * time.Second
	DefaultMaxDeletionPeriod time.Duration = 300 * time.Second
	DefaultNodeReadyTimeout  time.Duration = 300 * time.Second
	DefaultMinNodes                        = 0
	DefaultMaxNodes                        = 24
	DefaultMaxPods                         = 110
	DefaultMinCpus                         = 2
	DefaultMaxCpus                         = 24
	DefaultMinMemoryInMB                   = 1024
	DefaultMaxMemoryInMB                   = 24 * 1024
)

const (
	ManagedNodeMinMemory   = 2 * 1024
	ManagedNodeMaxMemory   = 128 * 1024
	ManagedNodeMinCores    = 2
	ManagedNodeMaxCores    = 32
	ManagedNodeMinDiskSize = 10 * 1024
	ManagedNodeMaxDiskSize = 1024 * 1024
)

// KubernetesLabel labels
type KubernetesLabel map[string]string

// MergeKubernetesLabel merge kubernetes map in one
func MergeKubernetesLabel(labels ...KubernetesLabel) KubernetesLabel {
	merged := KubernetesLabel{}

	for _, label := range labels {
		for k, v := range label {
			merged[k] = v
		}
	}

	return merged
}

type Config struct {
	APIServerURL             string
	KubeConfig               string
	ProviderConfig           string
	MachineConfig            string
	ExtDestinationEtcdSslDir string
	ExtSourceEtcdSslDir      string
	KubernetesPKISourceDir   string
	KubernetesPKIDestDir     string
	Distribution             string
	UseExternalEtdc          bool
	UseVanillaGrpcProvider   bool
	UseControllerManager     bool
	RequestTimeout           time.Duration
	DeletionTimeout          time.Duration
	MaxGracePeriod           time.Duration
	NodeReadyTimeout         time.Duration
	CloudProvider            string
	Config                   string
	SaveLocation             string
	DisplayVersion           bool
	DebugMode                bool
	LogFormat                string
	LogLevel                 string
	MinNode                  int64
	MaxNode                  int64
	MaxPods                  int64
	MinCpus                  int64
	MinMemory                int64
	MaxCpus                  int64
	MaxMemory                int64
	ManagedNodeMinCpus       int64
	ManagedNodeMinMemory     int64
	ManagedNodeMaxCpus       int64
	ManagedNodeMaxMemory     int64
	ManagedNodeMinDiskSize   int64
	ManagedNodeMaxDiskSize   int64
}

func (c *Config) GetResourceLimiter() *ResourceLimiter {
	return &ResourceLimiter{
		MinLimits: map[string]int64{
			constantes.ResourceNameCores:  c.MinCpus,
			constantes.ResourceNameMemory: c.MinMemory * 1024 * 1024,
			constantes.ResourceNameNodes:  int64(c.MinNode),
		},
		MaxLimits: map[string]int64{
			constantes.ResourceNameCores:  c.MaxCpus,
			constantes.ResourceNameMemory: c.MaxMemory * 1024 * 1024,
			constantes.ResourceNameNodes:  int64(c.MaxNode),
		},
	}
}

func (c *Config) GetManagedNodeResourceLimiter() *ResourceLimiter {
	return &ResourceLimiter{
		MinLimits: map[string]int64{
			constantes.ResourceNameManagedNodeDisk:   c.ManagedNodeMinDiskSize,
			constantes.ResourceNameManagedNodeMemory: c.ManagedNodeMinMemory,
			constantes.ResourceNameManagedNodeCores:  c.ManagedNodeMinCpus,
		},
		MaxLimits: map[string]int64{
			constantes.ResourceNameManagedNodeDisk:   c.ManagedNodeMaxDiskSize,
			constantes.ResourceNameManagedNodeMemory: c.ManagedNodeMaxMemory,
			constantes.ResourceNameManagedNodeCores:  c.ManagedNodeMaxCpus,
		},
	}
}

// A PodFilterFunc returns true if the supplied pod passes the filter.
type PodFilterFunc func(p apiv1.Pod) (bool, error)

// ClientGenerator provides clients
type ClientGenerator interface {
	KubeClient() (kubernetes.Interface, error)
	NodeManagerClient() (clientset.Interface, error)
	ApiExtentionClient() (apiextension.Interface, error)

	PodList(nodeName string, podFilter PodFilterFunc) ([]apiv1.Pod, error)
	NodeList() (*apiv1.NodeList, error)
	GetNode(nodeName string) (*apiv1.Node, error)
	SetProviderID(nodeName, providerID string) error
	UncordonNode(nodeName string) error
	CordonNode(nodeName string) error
	MarkDrainNode(nodeName string) error
	DrainNode(nodeName string, ignoreDaemonSet, deleteLocalData bool) error
	DeleteNode(nodeName string) error
	AnnoteNode(nodeName string, annotations map[string]string) error
	LabelNode(nodeName string, labels map[string]string) error
	TaintNode(nodeName string, taints ...apiv1.Taint) error
	WaitNodeToBeReady(nodeName string) error
}

// ResourceLimiter define limit, not really used
type ResourceLimiter struct {
	MinLimits map[string]int64 `json:"min"`
	MaxLimits map[string]int64 `json:"max"`
}

// KubeJoinConfig give element to join kube master
type KubeJoinConfig struct {
	Address        string   `json:"address,omitempty"`
	Token          string   `json:"token,omitempty"`
	CACert         string   `json:"ca,omitempty"`
	ExtraArguments []string `json:"extras-args,omitempty"`
}

type K3SJoinConfig struct {
	Address                   string   `json:"address,omitempty"`
	Token                     string   `json:"token,omitempty"`
	ExtraCommands             []string `json:"extras-commands,omitempty"`
	DatastoreEndpoint         string   `json:"datastore-endpoint,omitempty"`
	DeleteCredentialsProvider bool     `json:"delete-credentials-provider,omitempty"`
}

type RKE2JoinConfig struct {
	Address                   string   `json:"address,omitempty"`
	Token                     string   `json:"token,omitempty"`
	ExtraCommands             []string `json:"extras-commands,omitempty"`
	DeleteCredentialsProvider bool     `json:"delete-credentials-provider,omitempty"`
}

type ExternalJoinConfig struct {
	Address           string         `json:"address,omitempty"`
	Token             string         `json:"token,omitempty"`
	DatastoreEndpoint string         `json:"datastore-endpoint,omitempty"`
	JoinCommand       string         `json:"join-command,omitempty"`
	ConfigPath        string         `json:"config-path,omitempty"`
	ExtraConfig       map[string]any `json:"extra-config,omitempty"`
}

// AutoScalerServerOptionals declare wich features must be optional
type AutoScalerServerOptionals struct {
	Pricing                  bool `json:"pricing"`
	GetAvailableMachineTypes bool `json:"getAvailableMachineTypes"`
	NewNodeGroup             bool `json:"newNodeGroup"`
	TemplateNodeInfo         bool `json:"templateNodeInfo"`
	Create                   bool `json:"create"`
	Delete                   bool `json:"delete"`
}

type NodeGroupAutoscalingOptions struct {
	// ScaleDownUtilizationThreshold sets threshold for nodes to be considered for scale down
	// if cpu or memory utilization is over threshold.
	ScaleDownUtilizationThreshold float64 `json:"scaleDownUtilizationThreshold,omitempty"`

	// ScaleDownGpuUtilizationThreshold sets threshold for gpu nodes to be
	// considered for scale down if gpu utilization is over threshold.
	ScaleDownGpuUtilizationThreshold float64 `json:"scaleDownGpuUtilizationThreshold,omitempty"`

	// ScaleDownUnneededTime sets the duration CA expects a node to be
	// unneeded/eligible for removal before scaling down the node.
	ScaleDownUnneededTime time.Duration `json:"scaleDownUnneededTime,omitempty"`

	// ScaleDownUnreadyTime represents how long an unready node should be
	// unneeded before it is eligible for scale down.
	ScaleDownUnreadyTime time.Duration `json:"scaleDownUnreadyTime,omitempty"`
}

// AutoScalerServerConfig is contains configuration
type AutoScalerServerConfig struct {
	Distribution               *string                         `default:"kubeadm" json:"distribution"`
	CloudProvider              *string                         `default:"vsphere" json:"machines"`
	MachineConfig              *string                         `json:"cloud-provider"`
	UseExternalEtdc            *bool                           `json:"use-external-etcd"`
	UseVanillaGrpcProvider     *bool                           `json:"use-vanilla-grpc"`
	UseControllerManager       *bool                           `json:"use-controller-manager"`
	ExtDestinationEtcdSslDir   string                          `default:"/etc/etcd/ssl" json:"dst-etcd-ssl-dir"`
	ExtSourceEtcdSslDir        string                          `default:"/etc/etcd/ssl" json:"src-etcd-ssl-dir"`
	KubernetesPKISourceDir     string                          `default:"/etc/kubernetes/pki" json:"kubernetes-pki-srcdir"`
	KubernetesPKIDestDir       string                          `default:"/etc/kubernetes/pki" json:"kubernetes-pki-dstdir"`
	Network                    string                          `default:"tcp" json:"network"`                     // Mandatory, Network to listen (see grpc doc) to listen
	Listen                     string                          `default:"0.0.0.0:5200" json:"listen"`             // Mandatory, Address to listen
	CertPrivateKey             string                          `json:"cert-private-key,omitempty"`                // Optional to secure grcp channel
	CertPublicKey              string                          `json:"cert-public-key,omitempty"`                 // Optional to secure grcp channel
	CertCA                     string                          `json:"cert-ca,omitempty"`                         // Optional to secure grcp channel
	ServiceIdentifier          string                          `json:"secret"`                                    // Mandatory, secret Identifier, client must match this
	NodeGroup                  string                          `json:"nodegroup"`                                 // Mandatory, the nodegroup
	MinNode                    *int64                          `json:"minNode"`                                   // Mandatory, Min AutoScaler VM
	MaxNode                    *int64                          `json:"maxNode"`                                   // Mandatory, Max AutoScaler VM
	MaxPods                    *int64                          `default:"110" json:"maxPods"`                     // Mandatory, Max pod per node
	MaxCreatedNodePerCycle     int                             `json:"maxNode-per-cycle" default:"2"`             // Optional, the max number VM to create in //
	ProvisionnedNodeNamePrefix string                          `default:"autoscaled" json:"node-name-prefix"`     // Optional, the created node name prefix
	ManagedNodeNamePrefix      string                          `default:"worker" json:"managed-name-prefix"`      // Optional, the created node name prefix
	ControlPlaneNamePrefix     string                          `default:"master" json:"controlplane-name-prefix"` // Optional, the created node name prefix
	NodePrice                  float64                         `json:"nodePrice"`                                 // Optional, The VM price
	PodPrice                   float64                         `json:"podPrice"`                                  // Optional, The pod price
	KubeAdm                    *KubeJoinConfig                 `json:"kubeadm"`
	K3S                        *K3SJoinConfig                  `json:"k3s,omitempty"`
	RKE2                       *RKE2JoinConfig                 `json:"rke2,omitempty"`
	External                   *ExternalJoinConfig             `json:"external,omitempty"`
	DefaultMachineType         string                          `default:"standard" json:"default-machine"`
	DiskSizeInMB               int                             `default:"10240" json:"disk-size"`
	NodeLabels                 KubernetesLabel                 `json:"nodeLabels"`
	CloudInit                  cloudinit.CloudInit             `json:"cloud-init"` // Optional, The cloud init conf file
	Optionals                  *AutoScalerServerOptionals      `json:"optionals"`
	ManagedNodeResourceLimiter *ResourceLimiter                `json:"managednodes-limits"`
	SSH                        *sshutils.AutoScalerServerSSH   `json:"ssh-infos"`
	AutoScalingOptions         *NodeGroupAutoscalingOptions    `json:"autoscaling-options,omitempty"`
	DebugMode                  *bool                           `json:"debug,omitempty"`
	providerConfiguration      providers.ProviderConfiguration `json:"-"`
}

func (limits *ResourceLimiter) MergeRequestResourceLimiter(limiter *apigrpc.ResourceLimiter) {
	if limits.MaxLimits == nil {
		limits.MaxLimits = limiter.MaxLimits
	} else {
		for k, v := range limiter.MaxLimits {
			limits.MaxLimits[k] = v
		}
	}

	if limits.MinLimits == nil {
		limits.MinLimits = limiter.MinLimits
	} else {
		for k, v := range limiter.MinLimits {
			limits.MinLimits[k] = v
		}
	}
}

func (limits *ResourceLimiter) SetMaxValue64(key string, value int64) {
	if limits.MaxLimits == nil {
		limits.MaxLimits = make(map[string]int64)
	}

	limits.MaxLimits[key] = value
}

func (limits *ResourceLimiter) SetMinValue64(key string, value int64) {
	if limits.MinLimits == nil {
		limits.MinLimits = make(map[string]int64)
	}

	limits.MinLimits[key] = value
}

func (limits *ResourceLimiter) GetMaxValue64(key string, defaultValue int64) int64 {
	if limits.MaxLimits != nil {
		if value, found := limits.MaxLimits[key]; found {
			return value
		}
	}
	return defaultValue
}

func (limits *ResourceLimiter) GetMinValue64(key string, defaultValue int64) int64 {
	if limits.MinLimits != nil {
		if value, found := limits.MinLimits[key]; found {
			return value
		}
	}
	return defaultValue
}

func (limits *ResourceLimiter) SetMaxValue(key string, value int) {
	if limits.MaxLimits == nil {
		limits.MaxLimits = make(map[string]int64)
	}

	limits.MaxLimits[key] = int64(value)
}

func (limits *ResourceLimiter) SetMinValue(key string, value int) {
	if limits.MinLimits == nil {
		limits.MinLimits = make(map[string]int64)
	}

	limits.MinLimits[key] = int64(value)
}

func (limits *ResourceLimiter) GetMaxValue(key string, defaultValue int) int {
	if limits.MaxLimits != nil {
		if value, found := limits.MaxLimits[key]; found {
			return int(value)
		}
	}
	return defaultValue
}

func (limits *ResourceLimiter) GetMinValue(key string, defaultValue int) int {
	if limits.MinLimits != nil {
		if value, found := limits.MinLimits[key]; found {
			return int(value)
		}
	}
	return defaultValue
}

// SetupCloudConfiguration returns the cloud configuration
func (conf *AutoScalerServerConfig) SetupCloudConfiguration(configFile string) error {
	var err error

	if conf.providerConfiguration == nil {
		if conf.CloudProvider == nil {
			conf.providerConfiguration, err = vsphere.NewVSphereProviderConfiguration(configFile)
		} else {
			switch *conf.CloudProvider {
			case providers.AwsCloudProviderName:
				conf.providerConfiguration, err = aws.NewAwsProviderConfiguration(configFile)
			case providers.VSphereCloudProviderName:
				conf.providerConfiguration, err = vsphere.NewVSphereProviderConfiguration(configFile)
			case providers.VMWareWorkstationProviderName:
				conf.providerConfiguration, err = desktop.NewDesktopProviderConfiguration(configFile)
			case providers.MultipassProviderName:
				conf.providerConfiguration, err = multipass.NewMultipassProviderConfiguration(configFile)
			default:
				glog.Fatalf("Unsupported cloud provider: %s", *conf.CloudProvider)
			}
		}
	}

	return err
}

func (conf *AutoScalerServerConfig) GetCloudConfiguration() providers.ProviderConfiguration {
	return conf.providerConfiguration
}

// NewConfig returns new Config object
func NewConfig() *Config {
	return &Config{
		APIServerURL:             "",
		KubeConfig:               "",
		ProviderConfig:           "/etc/cluster/provider.json",
		MachineConfig:            "/etc/cluster/machines.json",
		Config:                   "/etc/cluster/autoscaler.json",
		Distribution:             providers.KubeAdmDistributionName,
		UseExternalEtdc:          false,
		UseVanillaGrpcProvider:   false,
		UseControllerManager:     true,
		ExtDestinationEtcdSslDir: "/etc/etcd/ssl",
		ExtSourceEtcdSslDir:      "/etc/etcd/ssl",
		KubernetesPKISourceDir:   "/etc/kubernetes/pki",
		KubernetesPKIDestDir:     "/etc/kubernetes/pki",
		RequestTimeout:           DefaultMaxRequestTimeout,
		DeletionTimeout:          DefaultMaxDeletionPeriod,
		MaxGracePeriod:           DefaultMaxGracePeriod,
		NodeReadyTimeout:         DefaultNodeReadyTimeout,
		CloudProvider:            "vsphere",
		DisplayVersion:           false,
		MinNode:                  DefaultMinNodes,
		MaxNode:                  DefaultMaxNodes,
		MaxPods:                  DefaultMaxPods,
		MinCpus:                  DefaultMinCpus,
		MinMemory:                DefaultMinMemoryInMB,
		MaxCpus:                  DefaultMaxCpus,
		MaxMemory:                DefaultMaxMemoryInMB,
		ManagedNodeMinCpus:       ManagedNodeMinCores,
		ManagedNodeMaxCpus:       ManagedNodeMaxCores,
		ManagedNodeMinMemory:     ManagedNodeMinMemory,
		ManagedNodeMaxMemory:     ManagedNodeMaxMemory,
		ManagedNodeMinDiskSize:   ManagedNodeMinDiskSize,
		ManagedNodeMaxDiskSize:   ManagedNodeMaxDiskSize,
		LogFormat:                "text",
		LogLevel:                 glog.InfoLevel.String(),
	}
}

// allLogLevelsAsStrings returns all logrus levels as a list of strings
func allLogLevelsAsStrings() []string {
	var levels []string
	for _, level := range glog.AllLevels {
		levels = append(levels, level.String())
	}
	return levels
}

func (cfg *Config) ParseFlags(args []string, version string) error {
	app := kingpin.New("vmware-autoscaler", "Kubernetes VMWare autoscaler create VM instances at demand for autoscaling.\n\nNote that all flags may be replaced with env vars - `--flag` -> `VMWARE_AUTOSCALER_FLAG=1` or `--flag value` -> `VMWARE_AUTOSCALER_FLAG=value`")

	//	app.Version(version)
	app.HelpFlag.Short('h')
	app.DefaultEnvars()

	app.Flag("log-format", "The format in which log messages are printed (default: text, options: text, json)").Default(cfg.LogFormat).EnumVar(&cfg.LogFormat, "text", "json")
	app.Flag("log-level", "Set the level of logging. (default: info, options: panic, debug, info, warning, error, fatal").Default(cfg.LogLevel).EnumVar(&cfg.LogLevel, allLogLevelsAsStrings()...)

	app.Flag("debug", "Debug mode").Default("false").BoolVar(&cfg.DebugMode)

	app.Flag("cloud-provider", "Which cloud provider used: vsphere, aws, desktop, multipass").Default(providers.VSphereCloudProviderName).EnumVar(&cfg.CloudProvider, providers.SupportedCloudProviders...)
	app.Flag("cloud-provider-config", "Cloud provider config file").Default(cfg.ProviderConfig).StringVar(&cfg.ProviderConfig)

	app.Flag("distribution", "Which kubernetes distribution to use: kubeadm, k3s, rke2, external").Default(providers.K3SDistributionName).EnumVar(&cfg.Distribution, providers.SupportedKubernetesDistribution...)
	app.Flag("use-vanilla-grpc", "Tell we use vanilla autoscaler externalgrpc cloudprovider").Default("false").BoolVar(&cfg.UseVanillaGrpcProvider)
	app.Flag("use-controller-manager", "Tell we use vsphere controller manager").Default("true").BoolVar(&cfg.UseControllerManager)

	// External Etcd
	app.Flag("use-external-etcd", "Tell we use an external etcd service (overriden by config file if defined)").Default("false").BoolVar(&cfg.UseExternalEtdc)
	app.Flag("src-etcd-ssl-dir", "Locate the source etcd ssl files (overriden by config file if defined)").Default(cfg.ExtSourceEtcdSslDir).StringVar(&cfg.ExtSourceEtcdSslDir)
	app.Flag("dst-etcd-ssl-dir", "Locate the destination etcd ssl files (overriden by config file if defined)").Default(cfg.ExtDestinationEtcdSslDir).StringVar(&cfg.ExtDestinationEtcdSslDir)

	app.Flag("kubernetes-pki-srcdir", "Locate the source kubernetes pki files (overriden by config file if defined)").Default(cfg.KubernetesPKISourceDir).StringVar(&cfg.KubernetesPKISourceDir)
	app.Flag("kubernetes-pki-dstdir", "Locate the destination kubernetes pki files (overriden by config file if defined)").Default(cfg.KubernetesPKIDestDir).StringVar(&cfg.KubernetesPKIDestDir)

	// Flags related to Kubernetes
	app.Flag("server", "The Kubernetes API server to connect to (default: auto-detect)").Default(cfg.APIServerURL).StringVar(&cfg.APIServerURL)
	app.Flag("kubeconfig", "Retrieve target cluster configuration from a Kubernetes configuration file (default: auto-detect)").Default(cfg.KubeConfig).StringVar(&cfg.KubeConfig)
	app.Flag("request-timeout", "Request timeout when calling Kubernetes APIs. 0s means no timeout").Default(DefaultMaxRequestTimeout.String()).DurationVar(&cfg.RequestTimeout)
	app.Flag("deletion-timeout", "Deletion timeout when delete node. 0s means no timeout").Default(DefaultMaxDeletionPeriod.String()).DurationVar(&cfg.DeletionTimeout)
	app.Flag("node-ready-timeout", "Node ready timeout to wait for a node to be ready. 0s means no timeout").Default(DefaultNodeReadyTimeout.String()).DurationVar(&cfg.NodeReadyTimeout)
	app.Flag("max-grace-period", "Maximum time evicted pods will be given to terminate gracefully.").Default(DefaultMaxGracePeriod.String()).DurationVar(&cfg.MaxGracePeriod)

	app.Flag("min-nodes", "Limits: minimum nodes (default: 0)").Default(strconv.FormatInt(cfg.MinNode, 10)).Int64Var(&cfg.MinNode)
	app.Flag("max-nodes", "Limits: max nodes (default: 24)").Default(strconv.FormatInt(cfg.MaxCpus, 10)).Int64Var(&cfg.MaxCpus)
	app.Flag("min-cpus", "Limits: minimum cpu (default: 1)").Default(strconv.FormatInt(cfg.MinCpus, 10)).Int64Var(&cfg.MinCpus)
	app.Flag("max-cpus", "Limits: max cpu (default: 24)").Default(strconv.FormatInt(cfg.MaxCpus, 10)).Int64Var(&cfg.MaxCpus)
	app.Flag("min-memory", "Limits: minimum memory in MB (default: 1G)").Default(strconv.FormatInt(cfg.MinMemory, 10)).Int64Var(&cfg.MinMemory)
	app.Flag("max-memory", "Limits: max memory in MB (default: 24G)").Default(strconv.FormatInt(cfg.MaxMemory, 10)).Int64Var(&cfg.MaxMemory)

	app.Flag("min-managednode-cpus", "Managed node: minimum cpu (default: 2)").Default(strconv.FormatInt(cfg.MinCpus, 10)).Int64Var(&cfg.ManagedNodeMinCpus)
	app.Flag("max-managednode-cpus", "Managed node: max cpu (default: 32)").Default(strconv.FormatInt(cfg.ManagedNodeMaxCpus, 10)).Int64Var(&cfg.ManagedNodeMaxCpus)
	app.Flag("min-managednode-memory", "Managed node: minimum memory in MB (default: 2G)").Default(strconv.FormatInt(cfg.MinMemory, 10)).Int64Var(&cfg.ManagedNodeMinMemory)
	app.Flag("max-managednode-memory", "Managed node: max memory in MB (default: 24G)").Default(strconv.FormatInt(cfg.ManagedNodeMaxMemory, 10)).Int64Var(&cfg.ManagedNodeMaxMemory)
	app.Flag("min-managednode-disksize", "Managed node: minimum disk size in MB (default: 10MB)").Default(strconv.FormatInt(cfg.ManagedNodeMinDiskSize, 10)).Int64Var(&cfg.ManagedNodeMinDiskSize)
	app.Flag("max-managednode-disksize", "Managed node: max disk size in MB (default: 1T)").Default(strconv.FormatInt(cfg.ManagedNodeMaxDiskSize, 10)).Int64Var(&cfg.ManagedNodeMaxDiskSize)

	app.Flag("version", "Display version and exit").BoolVar(&cfg.DisplayVersion)

	app.Flag("config", "The config for the server").Default(cfg.Config).StringVar(&cfg.Config)
	app.Flag("machines", "The machine specs").Default(cfg.MachineConfig).StringVar(&cfg.MachineConfig)
	app.Flag("save", "The file to persists the server").Default(cfg.SaveLocation).StringVar(&cfg.SaveLocation)

	_, err := app.Parse(args)
	if err != nil {
		return err
	}

	return nil
}

func (cfg *Config) String() string {
	return fmt.Sprintf("APIServerURL:%s KubeConfig:%s RequestTimeout:%s Config:%s SaveLocation:%s DisplayVersion:%s", cfg.APIServerURL, cfg.KubeConfig, cfg.RequestTimeout, cfg.Config, cfg.SaveLocation, strconv.FormatBool(cfg.DisplayVersion))
}
