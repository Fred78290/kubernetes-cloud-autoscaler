package kubernetes

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/client"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/sshutils"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	glog "github.com/sirupsen/logrus"
)

const (
	RKE2DistributionName     = "rke2"
	K3SDistributionName      = "k3s"
	KubeAdmDistributionName  = "kubeadm"
	MicroK8SDistributionName = "microk8s"
	ExternalDistributionName = "external"
)

const (
	unableToExecuteCmdError  = "unable to execute command: %s, output: %s, reason: %v"
	errWriteFileErrorMsg     = "unable to write file: %s, reason: %v"
	tmpConfigDestinationFile = "/tmp/config.yaml"
	mkdirCmd                 = "mkdir -p %s"
	kubeSystemNamespace      = "kube-system"
	k3sPasswordNodeSecret    = "%s.node-password.k3s"
	rke2PasswordNodeSecret   = "%s.node-password.rke2"
)

var SupportedKubernetesDistribution = []string{
	RKE2DistributionName,
	K3SDistributionName,
	KubeAdmDistributionName,
	MicroK8SDistributionName,
	ExternalDistributionName,
}

type CommonJoinConfig struct {
	Address           string `json:"address,omitempty"`
	Token             string `json:"token,omitempty"`
	DatastoreEndpoint string `json:"datastore-endpoint,omitempty"`
}

type MapAny map[string]any

type GetStringFunc func() string
type KubernetesProvider interface {
	PrepareNodeCreation(c client.ClientGenerator) error
	PrepareNodeDeletion(c client.ClientGenerator, powered bool) error

	JoinCluster(c client.ClientGenerator) error
	UploadImageCredentialProviderConfig() error

	PutConfigInCloudInit(cloudInit cloudinit.CloudInit) error
	PutImageCredentialProviderConfigInCloudInit(cloudInit cloudinit.CloudInit) error
}

type KubernetesServerConfig interface {
	GetExternalConfig() *ExternalJoinConfig
	GetK3SJoinConfig() *K3SJoinConfig
	GetKubeAdmConfig() *KubeJoinConfig
	GetMicroK8SConfig() *MicroK8SJoinConfig
	GetRKE2Config() *RKE2JoinConfig

	GetCloudInitFileOwner() string
	GetCloudInitFileMode() uint
	KubernetesDistribution() string
	UseCloudInitToConfigure() bool
	UseControllerManager() bool
	DisableCloudController() bool
	GetCloudProviderName() string
	UseImageCredentialProviderConfig() bool
	UseExternalEtdcServer() bool
	GetExtDestinationEtcdSslDir() string
	GetExtSourceEtcdSslDir() string
	GetKubernetesPKISourceDir() string
	GetKubernetesPKIDestDir() string
	GetSSHConfig() *sshutils.AutoScalerServerSSH
	GetCredentialProviderConfig() any
	GetImageCredentialProviderConfig() string
	GetImageCredentialProviderBinDir() string
}

type kubernetesCommon struct {
	configuration KubernetesServerConfig
	controlPlane  bool
	maxPods       int
	nodeName      GetStringFunc
	address       GetStringFunc
	providerID    GetStringFunc
	timeout       time.Duration
}

func (provider *kubernetesCommon) runCommands(args ...string) (string, error) {
	config := provider.configuration
	timeout := provider.timeout
	command := fmt.Sprintf("sh -c \"%s\"", strings.Join(args, " && "))

	if out, err := utils.Sudo(config.GetSSHConfig(), provider.address(), timeout, command); err != nil {
		return "", fmt.Errorf(unableToExecuteCmdError, command, out, err)
	} else {
		return out, err
	}
}

func (provider *kubernetesCommon) executeCommands(args []string, restartKubelet bool, c client.ClientGenerator) error {
	config := provider.configuration
	timeout := provider.timeout

	if _, err := provider.runCommands(args...); err != nil {
		return err
	} else if c != nil {

		if restartKubelet {
			// To be sure, with kubeadm 1.26.1, the kubelet is not correctly restarted
			time.Sleep(5 * time.Second)
		}

		return context.PollImmediate(5*time.Second, time.Duration(config.GetSSHConfig().WaitSshReadyInSeconds)*time.Second, func() (done bool, err error) {
			if node, err := c.GetNode(provider.nodeName()); err == nil && node != nil {
				return true, nil
			}

			if restartKubelet {
				glog.Infof("Restart kubelet for node: %s", provider.nodeName())

				if out, err := utils.Sudo(config.GetSSHConfig(), provider.address(), timeout, "systemctl restart kubelet"); err != nil {
					return false, fmt.Errorf("unable to restart kubelet, output: %s, reason: %v", out, err)
				}
			}

			return false, nil
		})
	}

	return nil
}

func (provider *kubernetesCommon) joinClusterWithConfig(content any, destinationFile string, c client.ClientGenerator, restartKubelet bool, extraCommand ...string) error {
	var result error

	if f, err := os.CreateTemp(os.TempDir(), "config.*.yaml"); err != nil {
		result = fmt.Errorf("unable to create %s, reason: %v", destinationFile, err)
	} else {
		defer os.Remove(f.Name())

		content := utils.ToYAML(content)

		if glog.GetLevel() >= glog.DebugLevel {
			fmt.Fprintf(os.Stderr, "\n%s:\n%s\n", destinationFile, content)
		}

		if _, err = f.WriteString(content); err != nil {
			f.Close()
			result = fmt.Errorf(errWriteFileErrorMsg, f.Name(), err)
		} else {
			f.Close()

			if err = utils.Scp(provider.configuration.GetSSHConfig(), provider.address(), f.Name(), tmpConfigDestinationFile); err != nil {
				result = fmt.Errorf(constantes.ErrCantCopyFileToNode, f.Name(), tmpConfigDestinationFile, err)
			} else {
				args := []string{
					fmt.Sprintf(mkdirCmd, path.Dir(destinationFile)),
					fmt.Sprintf("cp %s %s", tmpConfigDestinationFile, destinationFile),
					fmt.Sprintf("rm %s", tmpConfigDestinationFile),
				}

				if len(extraCommand) > 0 {
					args = append(args, extraCommand...)
				}

				result = provider.executeCommands(args, restartKubelet, c)
			}
		}
	}

	return result
}

func (provider *kubernetesCommon) uploadFile(content any, destinationFile string) error {
	return provider.joinClusterWithConfig(content, destinationFile, nil, false)
}

func (provider *kubernetesCommon) PrepareNodeCreation(c client.ClientGenerator) (err error) {
	return
}

func (provider *kubernetesCommon) PrepareNodeDeletion(c client.ClientGenerator, powered bool) (err error) {
	return
}

func (provider *kubernetesCommon) UploadImageCredentialProviderConfig() (err error) {
	config := provider.configuration

	if config.UseImageCredentialProviderConfig() {
		err = provider.uploadFile(config.GetCredentialProviderConfig(), config.GetImageCredentialProviderConfig())
	}

	return
}

func (provider *kubernetesCommon) PutImageCredentialProviderConfigInCloudInit(cloudInit cloudinit.CloudInit) (err error) {
	config := provider.configuration

	if config.UseImageCredentialProviderConfig() {
		err = cloudInit.AddObjectToWriteFile(config.GetCredentialProviderConfig(), config.GetImageCredentialProviderConfig(), config.GetCloudInitFileOwner(), config.GetCloudInitFileMode())
	}

	return
}

func NewKubernetesProvider(configuration KubernetesServerConfig, controlPlane bool, maxPods int, nodeName, providerID, address GetStringFunc, timeout time.Duration) (provider KubernetesProvider) {
	switch configuration.KubernetesDistribution() {
	case K3SDistributionName:
		provider = &k3sProvider{
			kubernetesCommon: kubernetesCommon{
				configuration: configuration,
				controlPlane:  controlPlane,
				maxPods:       maxPods,
				nodeName:      nodeName,
				address:       address,
				providerID:    providerID,
				timeout:       timeout,
			},
		}

	case RKE2DistributionName:
		provider = &rke2Provider{
			kubernetesCommon: kubernetesCommon{
				configuration: configuration,
				controlPlane:  controlPlane,
				maxPods:       maxPods,
				nodeName:      nodeName,
				address:       address,
				providerID:    providerID,
				timeout:       timeout,
			},
		}

	case MicroK8SDistributionName:
		provider = &microk8sProvider{
			kubernetesCommon: kubernetesCommon{
				configuration: configuration,
				controlPlane:  controlPlane,
				maxPods:       maxPods,
				nodeName:      nodeName,
				address:       address,
				providerID:    providerID,
				timeout:       timeout,
			},
		}

	case ExternalDistributionName:
		provider = &externalProvider{
			kubernetesCommon: kubernetesCommon{
				configuration: configuration,
				controlPlane:  controlPlane,
				maxPods:       maxPods,
				nodeName:      nodeName,
				address:       address,
				providerID:    providerID,
				timeout:       timeout,
			},
		}

	default:
		provider = &kubeadmProvider{
			kubernetesCommon: kubernetesCommon{
				configuration: configuration,
				controlPlane:  controlPlane,
				maxPods:       maxPods,
				nodeName:      nodeName,
				address:       address,
				providerID:    providerID,
				timeout:       timeout,
			},
		}
	}

	return
}
