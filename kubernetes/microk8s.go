package kubernetes

import (
	"fmt"
	"strings"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
)

type microk8sProvider struct {
	kubernetesCommon
}

func (provider *microk8sProvider) joinCommand() []string {
	microk8s := provider.configuration.MicroK8S
	//worker := " --worker"

	//if provider.controlPlane {
	//	worker = ""
	//}

	return []string{
		fmt.Sprintf("snap install microk8s --classic --channel=%s", microk8s.Channel),
		//	"microk8s status --wait-ready",
		//		fmt.Sprintf("microk8s join %s/%s%s", microk8s.Address, microk8s.Token, worker),
	}
}

func (provider *microk8sProvider) agentConfig() any {
	config := provider.configuration
	microk8s := config.MicroK8S
	extraKubeAPIServerArgs := map[string]any{}
	extraKubeletArgs := map[string]any{
		"--max-pods":       provider.maxPods,
		"--cloud-provider": config.GetCloudProviderName(),
		"--node-ip":        provider.address(),
	}

	microk8sConfig := map[string]any{
		"version":                "0.1.0",
		"persistentClusterToken": microk8s.Token,
		"join": map[string]any{
			"url":    fmt.Sprintf("%s/%s", microk8s.Address, microk8s.Token),
			"worker": !provider.controlPlane,
		},
		"extraMicroK8sAPIServerProxyArgs": map[string]any{
			"--refresh-interval": "0",
		},
	}

	if config.UseImageCredentialProviderConfig() {
		extraKubeletArgs["--image-credential-provider-config"] = *config.ImageCredentialProviderConfig
		extraKubeletArgs["--image-credential-provider-bin-dir"] = *config.ImageCredentialProviderBinDir
	}

	if provider.controlPlane {
		microk8sConfig["addons"] = []map[string]string{
			{
				"name": "dns",
			},
			{
				"name": "rbac",
			},
			{
				"name": "hostpath-storage",
			},
		}

		extraKubeAPIServerArgs["--advertise-address"] = provider.address()
		extraKubeAPIServerArgs["--authorization-mode"] = "RBAC,Node"

		if config.UseExternalEtdcServer() {
			extraKubeAPIServerArgs["--etcd-servers"] = microk8s.DatastoreEndpoint
			extraKubeAPIServerArgs["--etcd-cafile"] = fmt.Sprintf("%s/ca.pem", config.ExtDestinationEtcdSslDir)
			extraKubeAPIServerArgs["--etcd-certfile"] = fmt.Sprintf("%s/etcd.pem", config.ExtDestinationEtcdSslDir)
			extraKubeAPIServerArgs["--etcd-keyfile"] = fmt.Sprintf("%s/etcd-key.pem", config.ExtDestinationEtcdSslDir)
		}
	}

	if microk8s.OverrideConfig != nil {
		for k, v := range microk8s.OverrideConfig {
			if k == "extraKubeletArgs" {
				extra := v.(map[string]any)

				for k1, v1 := range extra {
					extraKubeletArgs[k1] = v1
				}
			} else if k == "extraKubeAPIServerArgs" {
				extra := v.(map[string]any)

				for k1, v1 := range extra {
					extraKubeAPIServerArgs[k1] = v1
				}

			} else {
				microk8sConfig[k] = v
			}
		}
	}

	if len(extraKubeletArgs) > 0 {
		microk8sConfig["extraKubeletArgs"] = extraKubeletArgs
	}

	if len(extraKubeAPIServerArgs) > 0 {
		microk8sConfig["extraKubeAPIServerArgs"] = extraKubeAPIServerArgs
	}

	return microk8sConfig
}

func (provider *microk8sProvider) JoinCluster(c types.ClientGenerator) (err error) {
	return provider.joinClusterWithConfig(provider.agentConfig(), "/var/snap/microk8s/common/.microk8s.yaml", c, false, provider.joinCommand()...)
}

func (provider *microk8sProvider) PutConfigInCloudInit(cloudInit cloudinit.CloudInit) (err error) {
	config := provider.configuration

	joinClustercript := []string{
		"#!/bin/bash",
		"export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin",
	}

	joinClustercript = append(joinClustercript, provider.joinCommand()...)

	cloudInit.AddTextToWriteFile(strings.Join(joinClustercript, "\n"), "/usr/local/bin/join-cluster.sh", *config.CloudInitFileOwner, 0755)

	if err = cloudInit.AddObjectToWriteFile(provider.agentConfig(), "/var/snap/microk8s/common/.microk8s.yaml", *config.CloudInitFileOwner, *config.CloudInitFileMode); err == nil {
		cloudInit.AddRunCommand("nohup /usr/local/bin/join-cluster.sh > /var/log/join-cluster.log &")
	}

	return
}
