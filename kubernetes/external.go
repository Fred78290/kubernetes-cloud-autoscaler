package kubernetes

import (
	"fmt"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/client"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
)

type ExternalJoinConfig struct {
	CommonJoinConfig
	JoinCommand  string `json:"join-command,omitempty"`
	LeaveCommand string `json:"leave-command,omitempty"`
	ConfigPath   string `json:"config-path,omitempty"`
	ExtraConfig  MapAny `json:"extra-config,omitempty"`
}

type externalProvider struct {
	kubernetesCommon
}

func (provider *externalProvider) agentConfig() any {
	config := provider.configuration
	external := config.GetExternalConfig()
	externalConfig := MapAny{
		"max-pods":  provider.maxPods,
		"node-name": provider.nodeName(),
		"server":    external.Address,
		"token":     external.Token,
	}

	if config.UseControllerManager() && !config.UseCloudInitToConfigure() {
		externalConfig["provider-id"] = provider.providerID()
	}

	if external.ExtraConfig != nil {
		for k, v := range external.ExtraConfig {
			externalConfig[k] = v
		}
	}

	if provider.controlPlane {
		if config.UseControllerManager() {
			externalConfig["disable-cloud-controller"] = config.DisableCloudController()
			externalConfig["cloud-provider-name"] = config.GetCloudProviderName()
		}

		if config.UseExternalEtdcServer() {
			externalConfig["datastore-endpoint"] = external.DatastoreEndpoint
			externalConfig["datastore-cafile"] = fmt.Sprintf("%s/ca.pem", config.GetExtDestinationEtcdSslDir())
			externalConfig["datastore-certfile"] = fmt.Sprintf("%s/etcd.pem", config.GetExtDestinationEtcdSslDir())
			externalConfig["datastore-keyfile"] = fmt.Sprintf("%s/etcd-key.pem", config.GetExtDestinationEtcdSslDir())
		}
	}

	return externalConfig
}

func (provider *externalProvider) PrepareNodeDeletion(c client.ClientGenerator, powered bool) (err error) {
	config := provider.configuration
	external := config.GetExternalConfig()

	if powered && len(external.LeaveCommand) > 0 {
		_, err = provider.runCommands(external.LeaveCommand)
	}

	return err
}

func (provider *externalProvider) JoinCluster(c client.ClientGenerator) (err error) {
	external := provider.configuration.GetExternalConfig()

	return provider.joinClusterWithConfig(provider.agentConfig(), external.ConfigPath, c, false, external.JoinCommand)
}

func (provider *externalProvider) PutConfigInCloudInit(cloudInit cloudinit.CloudInit) (err error) {
	config := provider.configuration
	external := config.GetExternalConfig()

	if err = cloudInit.AddObjectToWriteFile(provider.agentConfig(), external.ConfigPath, config.GetCloudInitFileOwner(), config.GetCloudInitFileMode()); err == nil {
		cloudInit.AddRunCommand(external.JoinCommand)
	}

	return
}
