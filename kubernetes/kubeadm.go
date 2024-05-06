package kubernetes

import (
	"fmt"
	"strings"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/client"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
)

// KubeJoinConfig give element to join kube master
type KubeJoinConfig struct {
	CommonJoinConfig
	CACert         string   `json:"ca,omitempty"`
	ExtraArguments []string `json:"extras-args,omitempty"`
}

type kubeadmProvider struct {
	kubernetesCommon
}

func (provider *kubeadmProvider) joinCommand() []string {
	config := provider.configuration
	commands := make([]string, 0, 2)
	kubeAdm := config.GetKubeAdmConfig()
	address := provider.address()

	if config.UseImageCredentialProviderConfig() {
		commands = append(commands, fmt.Sprintf("echo KUBELET_EXTRA_ARGS='--image-credential-provider-config=%s --image-credential-provider-bin-dir=%s' > /etc/default/kubelet", config.GetImageCredentialProviderConfig(), config.GetImageCredentialProviderBinDir()))
	}

	join := []string{
		"kubeadm",
		"join",
		kubeAdm.Address,
		"--node-name",
		provider.nodeName(),
		"--token",
		kubeAdm.Token,
		"--discovery-token-ca-cert-hash",
		kubeAdm.CACert,
		"--patches",
		"/etc/kubernetes/patches",
	}

	if provider.controlPlane {
		join = append(join, "--control-plane")

		if len(address) > 0 {
			join = append(join, "--apiserver-advertise-address", address)
		}
	}

	// Append extras arguments
	if len(kubeAdm.ExtraArguments) > 0 {
		join = append(join, kubeAdm.ExtraArguments...)
	}

	return append(commands, strings.Join(join, " "))
}

func (provider *kubeadmProvider) agentConfig() any {
	if provider.configuration.UseCloudInitToConfigure() {
		return MapAny{
			"address": provider.address(),
			"maxPods": provider.maxPods,
		}
	} else {
		return MapAny{
			"address":    provider.address(),
			"providerID": provider.providerID(),
			"maxPods":    provider.maxPods,
		}
	}
}

func (provider *kubeadmProvider) JoinCluster(c client.ClientGenerator) (err error) {
	return provider.joinClusterWithConfig(provider.agentConfig(), "/etc/kubernetes/patches/kubeletconfiguration0+merge.yaml", c, true, provider.joinCommand()...)
}

func (provider *kubeadmProvider) PutConfigInCloudInit(cloudInit cloudinit.CloudInit) (err error) {
	config := provider.configuration

	if err = cloudInit.AddObjectToWriteFile(provider.agentConfig(), "/etc/kubernetes/patches/kubeletconfiguration0+merge.yaml", config.GetCloudInitFileOwner(), config.GetCloudInitFileMode()); err == nil {
		cloudInit.AddRunCommand(provider.joinCommand()...)
	}

	return
}
