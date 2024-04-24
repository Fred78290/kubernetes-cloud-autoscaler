package kubernetes

import (
	"fmt"
	"strings"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
)

type rke2Provider struct {
	kubernetesCommon
}

func (provider *rke2Provider) joinCommand() []string {
	var service string

	if provider.controlPlane {
		service = "rke2-server"
	} else {
		service = "rke2-agent"
	}

	return []string{
		fmt.Sprintf("systemctl enable %s.service", service),
		fmt.Sprintf("systemctl start %s.service", service),
		fmt.Sprintf("journalctl --no-pager -xeu %s.service", service),
	}
}

func (provider *rke2Provider) agentConfig() any {
	config := provider.configuration
	rke2 := config.RKE2
	address := provider.address()
	kubeletArgs := []string{
		"fail-swap-on=false",
		fmt.Sprintf("max-pods=%d", provider.maxPods),
	}

	if config.UseControllerManager() && !config.UseCloudInitToConfigure() {
		kubeletArgs = append(kubeletArgs, fmt.Sprintf("provider-id=%s", provider.providerID()))
	}

	if config.CloudProvider != nil && len(*config.CloudProvider) > 0 {
		kubeletArgs = append(kubeletArgs, fmt.Sprintf("cloud-provider=%s", *config.CloudProvider))
	}

	rke2Config := map[string]any{
		"kubelet-arg": kubeletArgs,
		"node-name":   provider.nodeName(),
		"server":      fmt.Sprintf("https://%s", rke2.Address),
		"token":       rke2.Token,
	}

	if len(address) > 0 {
		rke2Config["advertise-address"] = address
	}

	if provider.controlPlane {
		if config.UseControllerManager() {
			rke2Config["disable-cloud-controller"] = config.DisableCloudController()
			rke2Config["cloud-provider-name"] = config.GetCloudProviderName()
		}

		rke2Config["disable"] = []string{
			"rke2-ingress-nginx",
			"rke2-metrics-server",
		}
	}

	// Append extras arguments
	if len(rke2.ExtraCommands) > 0 {
		for _, cmd := range rke2.ExtraCommands {
			args := strings.Split(cmd, "=")
			rke2Config[args[0]] = args[1]
		}
	}

	return rke2Config
}

func (provider *rke2Provider) PrepareNodeCreation(c types.ClientGenerator) (err error) {
	passwordNode := fmt.Sprintf(rke2PasswordNodeSecret, provider.nodeName())

	if secret, e := c.GetSecret(passwordNode, kubeSystemNamespace); e == nil && secret != nil {
		err = c.DeleteSecret(passwordNode, kubeSystemNamespace)
	}

	return
}

func (provider *rke2Provider) PrepareNodeDeletion(c types.ClientGenerator, powered bool) (err error) {
	return provider.PrepareNodeCreation(c)
}

func (provider *rke2Provider) JoinCluster(c types.ClientGenerator) (err error) {
	return provider.joinClusterWithConfig(provider.agentConfig(), "/etc/rancher/rke2/config.yaml", c, false, provider.joinCommand()...)
}

func (provider *rke2Provider) PutConfigInCloudInit(cloudInit cloudinit.CloudInit) (err error) {
	config := provider.configuration

	if err = cloudInit.AddObjectToWriteFile(provider.agentConfig(), "/etc/rancher/rke2/config.yaml", *config.CloudInitFileOwner, *config.CloudInitFileMode); err == nil {
		cloudInit.AddRunCommand(provider.joinCommand()...)
	}

	return
}
