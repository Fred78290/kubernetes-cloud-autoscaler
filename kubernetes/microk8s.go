package kubernetes

import (
	"fmt"
	"strings"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/client"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
)

type MicroK8SJoinConfig struct {
	CommonJoinConfig
	UseLoadBalancer bool   `json:"use-nlb"`
	Channel         string `json:"channel,omitempty"`
	OverrideConfig  MapAny `json:"override-config,omitempty"`
}

type microk8sProvider struct {
	kubernetesCommon
}

func (provider *microk8sProvider) joinCommand() []string {
	microk8s := provider.configuration.GetMicroK8SConfig()

	return []string{
		fmt.Sprintf("snap install microk8s --classic --channel=%s", microk8s.Channel),
	}
}

func (provider *microk8sProvider) agentConfig() any {
	config := provider.configuration
	microk8s := config.GetMicroK8SConfig()
	extraKubeAPIServerArgs := MapAny{}
	extraKubeletArgs := MapAny{
		"--max-pods":       provider.maxPods,
		"--cloud-provider": config.GetCloudProviderName(),
		"--node-ip":        provider.address(),
	}

	microk8sConfig := MapAny{
		"version":                "0.1.0",
		"persistentClusterToken": microk8s.Token,
		"join": MapAny{
			"url":    fmt.Sprintf("%s/%s", microk8s.Address, microk8s.Token),
			"worker": !provider.controlPlane,
		},
		"extraMicroK8sAPIServerProxyArgs": MapAny{
			"--refresh-interval": "0",
		},
	}

	if config.UseImageCredentialProviderConfig() {
		extraKubeletArgs["--image-credential-provider-config"] = config.GetImageCredentialProviderConfig()
		extraKubeletArgs["--image-credential-provider-bin-dir"] = config.GetImageCredentialProviderBinDir()
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
			extraKubeAPIServerArgs["--etcd-cafile"] = fmt.Sprintf("%s/ca.pem", config.GetExtDestinationEtcdSslDir())
			extraKubeAPIServerArgs["--etcd-certfile"] = fmt.Sprintf("%s/etcd.pem", config.GetExtDestinationEtcdSslDir())
			extraKubeAPIServerArgs["--etcd-keyfile"] = fmt.Sprintf("%s/etcd-key.pem", config.GetExtDestinationEtcdSslDir())
		}
	} else if microk8s.UseLoadBalancer {
		microk8sConfig["extraMicroK8sAPIServerProxyArgs"] = MapAny{
			"--refresh-interval": "0",
			"--traefik-config":   "/usr/local/etc/microk8s/traefik.yaml",
		}
	}

	if microk8s.OverrideConfig != nil {
		for k, v := range microk8s.OverrideConfig {
			if k == "extraKubeletArgs" {
				extra := v.(MapAny)

				for k1, v1 := range extra {
					extraKubeletArgs[k1] = v1
				}
			} else if k == "extraKubeAPIServerArgs" {
				extra := v.(MapAny)

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

func (provider *microk8sProvider) JoinCluster(c client.ClientGenerator) (err error) {
	return provider.joinClusterWithConfig(provider.agentConfig(), "/var/snap/microk8s/common/.microk8s.yaml", c, false, provider.joinCommand()...)
}

func (provider *microk8sProvider) PutConfigInCloudInit(cloudInit cloudinit.CloudInit) (err error) {
	config := provider.configuration
	microk8s := config.GetMicroK8SConfig()

	joinClustercript := []string{
		"#!/bin/bash",
		"export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin",
	}

	joinClustercript = append(joinClustercript, provider.joinCommand()...)

	cloudInit.AddTextToWriteFile(strings.Join(joinClustercript, "\n"), "/usr/local/bin/join-cluster.sh", config.GetCloudInitFileOwner(), 0755)

	if !provider.controlPlane && microk8s.UseLoadBalancer {
		traefik := MapAny{
			"entryPoints": MapAny{
				"apiserver": MapAny{
					"address": "':16443'",
				},
			},
			"providers": MapAny{
				"file": MapAny{
					"filename": "/usr/local/etc/microk8s/provider.yaml",
					"watch":    true,
				},
			},
		}

		provider := MapAny{
			"tcp": MapAny{
				"routers": MapAny{
					"Router-1": MapAny{
						"rule":    "HostSNI(`*`)",
						"service": "kube-apiserver",
						"tls": MapAny{
							"passthrough": true,
						},
					},
				},
			},
			"services": MapAny{
				"kube-apiserver": MapAny{
					"loadBalancer": MapAny{
						"servers": []MapAny{
							{
								"address": microk8s.Address,
							},
						},
					},
				},
			},
		}

		cloudInit.AddObjectToWriteFile(traefik, "/usr/local/etc/microk8s/traefik.yaml", config.GetCloudInitFileOwner(), config.GetCloudInitFileMode())
		cloudInit.AddObjectToWriteFile(provider, "/usr/local/etc/microk8s/provider.yaml", config.GetCloudInitFileOwner(), config.GetCloudInitFileMode())
	}

	if err = cloudInit.AddObjectToWriteFile(provider.agentConfig(), "/var/snap/microk8s/common/.microk8s.yaml", config.GetCloudInitFileOwner(), config.GetCloudInitFileMode()); err == nil {
		cloudInit.AddRunCommand("nohup /usr/local/bin/join-cluster.sh > /var/log/join-cluster.log &")
	}

	return
}
