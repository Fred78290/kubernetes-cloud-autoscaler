package kubernetes

import (
	"fmt"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/client"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
)

type K3SJoinConfig struct {
	CommonJoinConfig
	ExtraCommands []string `json:"extras-commands,omitempty"`
}

type k3sProvider struct {
	kubernetesCommon
}

func (provider *k3sProvider) joinCommand() []string {
	return []string{
		"systemctl enable k3s.service",
		"systemctl start k3s.service --no-block",
		"journalctl --no-pager -xeu k3s.service",
	}
}

func (provider *k3sProvider) agentCommand() []string {
	config := provider.configuration
	k3s := config.GetK3SJoinConfig()
	command := make([]string, 0, 5)

	if config.UseControllerManager() && !config.UseCloudInitToConfigure() {
		command = append(command, fmt.Sprintf("echo K3S_ARGS='--kubelet-arg=max-pods=%d --node-name=%s --server=https://%s --token=%s --kubelet-arg=provider-id=%s ' > /etc/systemd/system/k3s.service.env", provider.maxPods, provider.nodeName(), k3s.Address, k3s.Token, provider.providerID()))
	} else {
		command = append(command, fmt.Sprintf("echo K3S_ARGS='--kubelet-arg=max-pods=%d --node-name=%s --server=https://%s --token=%s' > /etc/systemd/system/k3s.service.env", provider.maxPods, provider.nodeName(), k3s.Address, k3s.Token))
	}

	if provider.controlPlane {
		command = append(command, "echo 'K3S_MODE=server' > /etc/default/k3s")

		if config.DisableCloudController() {
			command = append(command, "echo K3S_DISABLE_ARGS='--disable-cloud-controller --disable=servicelb --disable=traefik --disable=metrics-server' > /etc/systemd/system/k3s.disabled.env")
		} else {
			command = append(command, "echo K3S_DISABLE_ARGS='--disable=servicelb --disable=traefik --disable=metrics-server' > /etc/systemd/system/k3s.disabled.env")
		}

		if config.UseExternalEtdcServer() {
			command = append(command, fmt.Sprintf("echo K3S_SERVER_ARGS='--datastore-endpoint=%s --datastore-cafile=%s/ca.pem --datastore-certfile=%s/etcd.pem --datastore-keyfile=%s/etcd-key.pem' > /etc/systemd/system/k3s.server.env", k3s.DatastoreEndpoint, config.GetExtDestinationEtcdSslDir(), config.GetExtDestinationEtcdSslDir(), config.GetExtDestinationEtcdSslDir()))
		}
	} else {
		command = append(command, "echo 'K3S_MODE=agent' > /etc/default/k3s")
	}

	// Append extras arguments
	if len(k3s.ExtraCommands) > 0 {
		command = append(command, k3s.ExtraCommands...)
	}

	return append(command, provider.joinCommand()...)
}

func (provider *k3sProvider) PrepareNodeCreation(c client.ClientGenerator) (err error) {
	passwordNode := fmt.Sprintf(k3sPasswordNodeSecret, provider.nodeName())

	if secret, e := c.GetSecret(passwordNode, kubeSystemNamespace); e == nil && secret != nil {
		err = c.DeleteSecret(passwordNode, kubeSystemNamespace)
	}

	return
}

func (provider *k3sProvider) PrepareNodeDeletion(c client.ClientGenerator, powered bool) (err error) {
	return provider.PrepareNodeCreation(c)
}

func (provider *k3sProvider) JoinCluster(c client.ClientGenerator) (err error) {
	return provider.executeCommands(provider.agentCommand(), false, c)
}

func (provider *k3sProvider) PutConfigInCloudInit(cloudInit cloudinit.CloudInit) (err error) {
	return cloudInit.AddRunCommand(provider.agentCommand()...)
}
