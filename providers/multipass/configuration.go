package multipass

import (
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/api"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	glog "github.com/sirupsen/logrus"
)

const (
	multipassCommandLine      = "multipass"
	errCloudInitWriteError    = "can't write cloud-init, reason: %v"
	errCloudInitMarshallError = "can't marshall cloud-init, reason: %v"
	errTempFile               = "can't create temp file, reason: %v"
)

type MountPoint struct {
	LocalPath    string
	InstancePath string
}

// Configuration declares multipass connection info
type Configuration struct {
	Address           string             `json:"address"` // external cluster autoscaler provider address of the form "host:port", "host%zone:port", "[host]:port" or "[host%zone]:port"
	Key               string             `json:"key"`     // path to file containing the tls key
	Cert              string             `json:"cert"`    // path to file containing the tls certificate
	Cacert            string             `json:"cacert"`  // path to file containing the CA certificate
	Timeout           time.Duration      `json:"timeout"`
	TemplateName      string             `json:"template-name"`
	NetplanFileName   string             `default:"10-custom.yaml" json:"netplan-name"`
	AvailableGPUTypes map[string]string  `json:"gpu-types"`
	VMWareRegion      string             `default:"home" json:"region"`
	VMWareZone        string             `default:"office" json:"zone"`
	Network           *providers.Network `json:"network" `
	Mounts            []MountPoint       `json:"mounts"`
}

type VMStatus struct {
	CPUCount string `json:"cpu_count"`
	Disks    struct {
		Sda1 struct {
			Total string `json:"total"`
			Used  string `json:"used"`
		} `json:"sda1"`
	} `json:"disks"`
	ImageHash    string    `json:"image_hash"`
	ImageRelease string    `json:"image_release"`
	Ipv4         []string  `json:"ipv4"`
	Load         []float64 `json:"load"`
	Memory       struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
	} `json:"memory"`
	Mounts struct {
		Home struct {
			GidMappings []string `json:"gid_mappings"`
			SourcePath  string   `json:"source_path"`
			UIDMappings []string `json:"uid_mappings"`
		} `json:"Home"`
	} `json:"mounts"`
	Release string `json:"release"`
	State   string `json:"state"`
}

type MultipassVMInfos struct {
	Errors []any               `json:"errors"`
	Info   map[string]VMStatus `json:"info"`
}

func NewMultipassProviderConfiguration(fileName string) (providers.ProviderConfiguration, error) {
	var config Configuration
	var err error

	if err = providers.LoadConfig(fileName, &config); err != nil {
		glog.Errorf("Failed to open file: %s, error: %v", fileName, err)

		return nil, err
	}

	if config.Address == "multipass" || len(config.Address) == 0 {
		return &hostMultipassWrapper{
			baseMultipassWrapper: baseMultipassWrapper{
				Configuration: &config,
			},
		}, nil
	}

	wrapper := remoteMultipassWrapper{
		baseMultipassWrapper: baseMultipassWrapper{
			Configuration: &config,
		},
	}

	if wrapper.client, err = api.NewApiClient(config.Address, config.Key, config.Cert, config.Cacert); err != nil {
		return nil, err
	}

	wrapper.Configuration.Network.ConfigurationDidLoad()

	return &wrapper, nil
}

func (status *VMStatus) Address() string {
	if len(status.Ipv4) > 1 {
		return status.Ipv4[1]
	}

	if len(status.Ipv4) > 0 {
		return status.Ipv4[0]
	}

	return ""
}

func (status *VMStatus) Powered() bool {
	return strings.ToUpper(status.State) == "RUNNING"
}
