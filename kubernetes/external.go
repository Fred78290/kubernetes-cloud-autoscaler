package kubernetes

import (
	"fmt"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
)

type externalProvider struct {
	kubernetesCommon
}

func (provider *externalProvider) agentConfig() any {
	config := provider.configuration
	external := config.External
	externalConfig := map[string]any{
		"max-pods":  provider.maxPods,
		"node-name": provider.nodeName,
		"server":    external.Address,
		"token":     external.Token,
	}

	if config.UseControllerManager() && !config.UseCloudInitToConfigure() {
		externalConfig["provider-id"] = provider.providerID
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
			externalConfig["datastore-cafile"] = fmt.Sprintf("%s/ca.pem", config.ExtDestinationEtcdSslDir)
			externalConfig["datastore-certfile"] = fmt.Sprintf("%s/etcd.pem", config.ExtDestinationEtcdSslDir)
			externalConfig["datastore-keyfile"] = fmt.Sprintf("%s/etcd-key.pem", config.ExtDestinationEtcdSslDir)
		}
	}

	return externalConfig
}

func (provider *externalProvider) JoinCluster(c types.ClientGenerator) (err error) {
	return provider.joinClusterWithConfig(provider.agentConfig(), provider.configuration.External.ConfigPath, c, false, provider.configuration.External.JoinCommand)
}

func (provider *externalProvider) PutConfigInCloudInit(cloudInit cloudinit.CloudInit) (err error) {
	config := provider.configuration

	if err = cloudInit.AddObjectToWriteFile(provider.agentConfig(), config.External.ConfigPath, *config.CloudInitFileOwner, *config.CloudInitFileMode); err == nil {
		cloudInit.AddRunCommand(config.External.JoinCommand)
	}

	return
}
