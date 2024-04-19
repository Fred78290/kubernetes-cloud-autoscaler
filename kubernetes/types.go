package kubernetes

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	glog "github.com/sirupsen/logrus"
)

const (
	unableToExecuteCmdError  = "unable to execute command: %s, output: %s, reason: %v"
	errWriteFileErrorMsg     = "unable to write file: %s, reason: %v"
	tmpConfigDestinationFile = "/tmp/config.yaml"
	mkdirCmd                 = "mkdir -p %s"
)

type KubernetesProvider interface {
	JoinCluster(c types.ClientGenerator) error
	UploadImageCredentialProviderConfig() error

	PutConfigInCloudInit(cloudInit cloudinit.CloudInit) error
	PutImageCredentialProviderConfigInCloudInit(cloudInit cloudinit.CloudInit) error
}

type kubernetesCommon struct {
	configuration *types.AutoScalerServerConfig
	controlPlane  bool
	maxPods       int
	nodeName      string
	address       string
	providerID    string
	timeout       time.Duration
}

func (provider *kubernetesCommon) runCommands(args ...string) (string, error) {
	config := provider.configuration
	timeout := provider.timeout
	command := fmt.Sprintf("sh -c \"%s\"", strings.Join(args, " && "))

	if out, err := utils.Sudo(config.SSH, provider.address, timeout, command); err != nil {
		return "", fmt.Errorf(unableToExecuteCmdError, command, out, err)
	} else {
		return out, err
	}
}

func (provider *kubernetesCommon) executeCommands(args []string, restartKubelet bool, c types.ClientGenerator) error {
	config := provider.configuration
	timeout := provider.timeout

	if _, err := provider.runCommands(args...); err != nil {
		return err
	} else if c != nil {

		if restartKubelet {
			// To be sure, with kubeadm 1.26.1, the kubelet is not correctly restarted
			time.Sleep(5 * time.Second)
		}

		return context.PollImmediate(5*time.Second, time.Duration(config.SSH.WaitSshReadyInSeconds)*time.Second, func() (done bool, err error) {
			if node, err := c.GetNode(provider.nodeName); err == nil && node != nil {
				return true, nil
			}

			if restartKubelet {
				glog.Infof("Restart kubelet for node: %s", provider.nodeName)

				if out, err := utils.Sudo(config.SSH, provider.address, timeout, "systemctl restart kubelet"); err != nil {
					return false, fmt.Errorf("unable to restart kubelet, output: %s, reason: %v", out, err)
				}
			}

			return false, nil
		})
	}

	return nil
}

func (provider *kubernetesCommon) joinClusterWithConfig(content any, destinationFile string, c types.ClientGenerator, restartKubelet bool, extraCommand ...string) error {
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

			if err = utils.Scp(provider.configuration.SSH, provider.address, f.Name(), tmpConfigDestinationFile); err != nil {
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

func (provider *kubernetesCommon) UploadImageCredentialProviderConfig() (err error) {
	config := provider.configuration

	if config.UseImageCredentialProviderConfig() {
		err = provider.uploadFile(config.CredentialProviderConfig, *config.ImageCredentialProviderConfig)
	}

	return
}

func (provider *kubernetesCommon) PutImageCredentialProviderConfigInCloudInit(cloudInit cloudinit.CloudInit) (err error) {
	config := provider.configuration

	if config.UseImageCredentialProviderConfig() {
		err = cloudInit.AddObjectToWriteFile(config.CredentialProviderConfig, *config.ImageCredentialProviderConfig, *config.CloudInitFileOwner, *config.CloudInitFileMode)
	}

	return
}

func NewKubernetesProvider(configuration *types.AutoScalerServerConfig, controlPlane bool, maxPods int, nodeName, providerID, address string, timeout time.Duration) (provider KubernetesProvider) {
	switch configuration.KubernetesDistribution() {
	case providers.K3SDistributionName:
		provider = &k3sProvider{
			kubernetesCommon: kubernetesCommon{
				configuration: configuration,
				controlPlane:  controlPlane,
				nodeName:      nodeName,
				address:       address,
				providerID:    providerID,
				timeout:       timeout,
			},
		}

	case providers.RKE2DistributionName:
		provider = &rke2Provider{
			kubernetesCommon: kubernetesCommon{
				configuration: configuration,
				controlPlane:  controlPlane,
				nodeName:      nodeName,
				address:       address,
				providerID:    providerID,
				timeout:       timeout,
			},
		}

	case providers.MicroK8SDistributionName:
		provider = &microk8sProvider{
			kubernetesCommon: kubernetesCommon{
				configuration: configuration,
				controlPlane:  controlPlane,
				nodeName:      nodeName,
				address:       address,
				providerID:    providerID,
				timeout:       timeout,
			},
		}

	case providers.ExternalDistributionName:
		provider = &externalProvider{
			kubernetesCommon: kubernetesCommon{
				configuration: configuration,
				controlPlane:  controlPlane,
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
				nodeName:      nodeName,
				address:       address,
				providerID:    providerID,
				timeout:       timeout,
			},
		}
	}

	return
}
