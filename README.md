[![Build Status](https://github.com/Fred78290/kubernetes-cloud-autoscaler/actions/workflows/build.yml/badge.svg)](https://github.com/Fred78290/kubernetes-cloud-autoscaler/actions/workflows/build.yml)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=Fred78290_kubernetes-cloud-autoscaler&metric=alert_status)](https://sonarcloud.io/dashboard?id=Fred78290_kubernetes-cloud-autoscaler)
[![Licence](https://img.shields.io/hexpm/l/plug.svg)](https://github.com/Fred78290/kubernetes-cloud-autoscaler/blob/master/LICENSE)

# kubernetes-cloud-autoscaler

Kubernetes cloud autoscaler for **vsphere,aws,multipass,openstack,cloudstack,lxd and vmware workstation and vmware fusion** provider including a custom resource controller to create managed node without code

### This project replace [kubernetes-vmware-autoscaler](https://github.com/Fred78290/kubernetes-vmware-autoscaler), [kubernetes-aws-autoscaler](https://github.com/Fred78290/kubernetes-aws-autoscaler), [kubernetes-multipass-autoscaler](https://github.com/Fred78290/kubernetes-multipass-autoscaler), [kubernetes-desktop-autoscaler](https://github.com/Fred78290/kubernetes-desktop-autoscaler).

The autoscaler allow to use different running plateform with different kubernetes distribution

## Supported plateform with cloud provider

* vSphere
* AWS
* VMWare Workstation
* VMWare Fusion
* Multipass
* OpenStack
* CloudStack
* LXD

## Supported kube engine with kubernetes distribution

* Kubeadm
* K3S
* RKE2
* Microk8s
* External

## Supported kubernetes release

* 1.30.0
  * This version is supported kubernetes v1.30
* 1.31.0
  * This version is supported kubernetes v1.31

## How it works

This tool will drive a cloud provider to deploy VM at the demand. The cluster autoscaler deployment use vanilla cluster-autoscaler or then enhanced version of [cluster-autoscaler](https://github.com/Fred78290/autoscaler).

This version use grpc to communicate with the cloud provider hosted outside the pod. A docker image is available here [cluster-autoscaler](https://hub.docker.com/r/fred78290/cluster-autoscaler)

Some samples of the cluster-autoscaler deployment are available at `examples/<plateform>/<kube engine>/autoscaler.yaml`. You must fill value between <>

Example of vsphere deployment with k3s: [examples/vsphere/k3s/autoscaler.yaml](https://raw.githubusercontent.com/Fred78290/kubernetes-cloud-autoscaler/main/examples/vsphere/k3s/autoscaler.yaml)

### Before you must create a kubernetes cluster on your plateform

You can do it from scrash or you can use script from project [autoscaled-masterkube-multipass](https://github.com/Fred78290/autoscaled-masterkube-multipass) to create a kubernetes cluster in single control plane or in HA mode with 3 control planes.

## Commandline arguments

| Parameter | Description |
| --- | --- |
| `version` | Display version and exit |
| `save` | Tell the tool to save state in this file |
| `config` |The the tool to use config file |
| `log-format` | The format in which log messages are printed (default: text, options: text, json)|
| `log-level` | Set the level of logging. (default: info, options: panic, debug, info, warning, error, fatal)|
| `debug` | Debug mode|
| `distribution` | Which kubernetes distribution to use: kubeadm, k3s, k3s, external|
| `use-vanilla-grpc` | Tell we use vanilla autoscaler externalgrpc cloudprovider|
| `use-controller-manager` | Tell we use vsphere controller manager|
| `use-external-etcd` | Tell we use an external etcd service (overriden by config file if defined)|
| `src-etcd-ssl-dir` | Locate the source etcd ssl files (overriden by config file if defined)|
| `dst-etcd-ssl-dir` | Locate the destination etcd ssl files (overriden by config file if defined)|
| `kubernetes-pki-srcdir` | Locate the source kubernetes pki files (overriden by config file if defined)|
| `kubernetes-pki-dstdir` | Locate the destination kubernetes pki files (overriden by config file if defined)|
| `server` | The Kubernetes API server to connect to (default: auto-detect)|
| `kubeconfig` | Retrieve target cluster configuration from a Kubernetes configuration file (default: auto-detect)|
| `request-timeout` | Request timeout when calling Kubernetes APIs. 0s means no timeout|
| `deletion-timeout` | Deletion timeout when delete node. 0s means no timeout|
| `node-ready-timeout` | Node ready timeout to wait for a node to be ready. 0s means no timeout|
| `max-grace-period` | Maximum time evicted pods will be given to terminate gracefully.|
| `min-cpus` | Limits: minimum cpu (default: 1)|
| `max-cpus` | Limits: max cpu (default: 24)|
| `min-memory` | Limits: minimum memory in MB (default: 1G)|
| `max-memory` | Limits: max memory in MB (default: 24G)|
| `min-managednode-cpus` | Managed node: minimum cpu (default: 2)|
| `max-managednode-cpus` | Managed node: max cpu (default: 32)|
| `min-managednode-memory` | Managed node: minimum memory in MB (default: 2G)|
| `max-managednode-memory` | Managed node: max memory in MB (default: 24G)|
| `min-managednode-disksize` | Managed node: minimum disk size in MB (default: 10MB)|
| `max-managednode-disksize` | Managed node: max disk size in MB (default: 1T)|

## Build

The build process use make file. The simplest way to build is `make container`

# Supported kube engine

## Use k3s, rke2, microk8s or external as kubernetes distribution method

Instead using **kubeadm** as kubernetes distribution method, it is possible to use **k3s**, **rke2**, **microk8s** or **external**

**external** allow to use custom shell script to join cluster

Samples provided here

* [external](./examples/multipass/external/autoscaler-custom.yaml)
* [k3s](./examples/multipass/k3s/autoscaler-custom.yaml)
* [kubeadm](./examples/multipass/kubeadm/autoscaler-custom.yaml)
* [microk8s](./examples/multipass/kubeadm/autoscaler-custom.yaml)
* [rke2](./examples/multipass/k3s/autoscaler-custom.yaml)

## Use the vanilla autoscaler with extern gRPC cloud provider

You can also use the vanilla autoscaler with the [externalgrpc cloud provider](https://github.com/kubernetes/autoscaler/tree/master/cluster-autoscaler/cloudprovider/externalgrpc)

#### Samples of the cluster-autoscaler deployment with vanilla autoscaler. You must fill value between <>

* [external](./examples/multipass/external/autoscaler.yaml)
* [k3s](./examples/multipass/k3s/autoscaler.yaml)
* [kubeadm](./examples/multipass/kubeadm/autoscaler.yaml)
* [microk8s](./examples/multipass/microk8s/autoscaler.yaml)
* [rke2](./examples/multipass/k3s/autoscaler.yaml)

## Use external kubernetes distribution

When you use a custom method to create your cluster, you must provide a shell script to autoscaler to join the cluster. The script use a yaml config created by autoscaler at the given path.
Eventually you can provide also a script called before node delettion

config: /etc/default/kubernetes-cloud-autoscaler-config.yaml

```yaml
provider-id: vsphere://42373f8d-b72d-21c0-4299-a667a18c9fce
max-pods: 110
node-name: vsphere-dev-k3s-woker-01
server: 192.168.1.120:9345
token: K1060b887525bbfa7472036caa8a3c36b550fbf05e6f8e3dbdd970739cbd7373537
disable-cloud-controller: false
````

If you declare to use an external etcd service

```yaml
datastore-endpoint: https://1.2.3.4:2379
datastore-cafile: /etc/ssl/etcd/ca.pem
datastore-certfile: /etc/ssl/etcd/etcd.pem
datastore-keyfile: /etc/ssl/etcd/etcd-key.pem
```

You can also provide extras config onto this file

```json
{
  "external": {
    "join-command": "/usr/local/bin/join-cluster.sh",
    "delete-command": "/usr/local/bin/delete-node.sh",
    "config-path": "/etc/default/kubernetes-cloud-autoscaler-config.yaml",
    "extra-config": {
        "mydata": {
          "extra": "ball"
        },
        "...": "..."
    }
  }
}
```

Your script is responsible to set the correct kubelet flags such as max-pods=110, provider-id=vsphere://42373f8d-b72d-21c0-4299-a667a18c9fce, cloud-provider=external, ...

## Annotations requirements

If you expected to use cloud-autoscaler on already deployed kubernetes cluster, you must add some node annotations to existing node

Also don't forget to create an image usable by autoscaler to scale up the cluster [create-image.sh](https://raw.githubusercontent.com/Fred78290/autoscaled-masterkube-multipass/master/bin/create-image.sh)

| Annotation | Description | Value |
| --- | --- | --- |
| `cluster-autoscaler.kubernetes.io/scale-down-disabled` | Avoid scale down for this node |true|
| `cluster.autoscaler.nodegroup/name` | Node group name |vsphere-dev-k3s|
| `cluster.autoscaler.nodegroup/autoprovision` | Tell if the node is provisionned by autoscaler |false|
| `cluster.autoscaler.nodegroup/instance-id` | The vm UUID |42373f8d-b72d-21c0-4299-a667a18c9fce|
| `cluster.autoscaler.nodegroup/instance-name` | The vm name |vsphere-dev-k3s-masterkube|
| `cluster.autoscaler.nodegroup/managed` | Tell if the node is managed by autoscaler not autoscaled |false|
| `cluster.autoscaler.nodegroup/node-index` | The node index, will be set if missing |0|

Sample master node

```text
    cluster-autoscaler.kubernetes.io/scale-down-disabled: "true"
    cluster.autoscaler.nodegroup/autoprovision: "false"
    cluster.autoscaler.nodegroup/instance-id: 42373f8d-b72d-21c0-4299-a667a18c9fce
    cluster.autoscaler.nodegroup/instance-name: vsphere-dev-k3s-masterkube
    cluster.autoscaler.nodegroup/managed: "false" 
    cluster.autoscaler.nodegroup/name: vsphere-dev-k3s
    cluster.autoscaler.nodegroup/node-index: "0"
```

Sample first worker node

```text
    cluster-autoscaler.kubernetes.io/scale-down-disabled: "true"
    cluster.autoscaler.nodegroup/autoprovision: "false"
    cluster.autoscaler.nodegroup/instance-id: 42370879-d4f7-eab0-a1c2-918a97ac6856
    cluster.autoscaler.nodegroup/instance-name: vsphere-dev-k3s-worker-01
    cluster.autoscaler.nodegroup/managed: "false"
    cluster.autoscaler.nodegroup/name: vsphere-dev-k3s
    cluster.autoscaler.nodegroup/node-index: "1"
```

Sample autoscaled worker node

```text
    cluster-autoscaler.kubernetes.io/scale-down-disabled: "false"
    cluster.autoscaler.nodegroup/autoprovision: "true"
    cluster.autoscaler.nodegroup/instance-id: 3d25c629-3f1d-46b3-be9f-b95db2a64859
    cluster.autoscaler.nodegroup/instance-name: vsphere-dev-k3s-autoscaled-01
    cluster.autoscaler.nodegroup/managed: "false"
    cluster.autoscaler.nodegroup/name: vsphere-dev-k3s
    cluster.autoscaler.nodegroup/node-index: "2"
```

## Node labels

These labels will be added

| Label | Description | Value |
| --- | --- | --- |
|`node-role.kubernetes.io/control-plane`|Tell if the node is control-plane |true|
|`node-role.kubernetes.io/master`|Tell if the node is master |true|
|`node-role.kubernetes.io/worker`|Tell if the node is worker |true|

## Cloud controller provider compliant

autoscaler will set correctly the node provider id when you use platform cloud controller.

## CRD controller

This release include a CRD controller allowing to create kubernetes node without use of cli or code. Just by apply a configuration file, you have the ability to create nodes on the fly.

As example you can take a look on [artifacts/examples](https://github.com/Fred78290/kubernetes-cloud-autoscaler/tree/main/artifacts/examples) on execute the following command to create a new node

|File|Description|
| --- | --- |
|`managed-addr.yaml`|Create a managed node with a fixed IP address|
|`managed-control-plane.yaml`|Create a control-plane node with a fixed IP address |
|`managed-dhcp.yaml`|Create a managed node with a DHCP address|
|`managed-nodes.yaml`|Create two managed nodes with a fixed IP address|

#### Create managed node on aws with k3s engine

```bash
kubectl apply -f artifacts/examples/aws/k3s/managed-addr.yaml
kubectl apply -f artifacts/examples/aws/k3s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/aws/k3s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/aws/k3s/managed-nodes.yaml
````

#### Create managed node on aws with kubeadm engine

```bash
kubectl apply -f artifacts/examples/aws/kubeadm/managed-addr.yaml
kubectl apply -f artifacts/examples/aws/kubeadm/managed-control-plane.yaml
kubectl apply -f artifacts/examples/aws/kubeadm/managed-dhcp.yaml
kubectl apply -f artifacts/examples/aws/kubeadm/managed-nodes.yaml
```

#### Create managed node on aws with microk8s engine

```bash
kubectl apply -f artifacts/examples/aws/microk8s/managed-addr.yaml
kubectl apply -f artifacts/examples/aws/microk8s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/aws/microk8s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/aws/microk8s/managed-nodes.yaml
```

#### Create managed node on aws with rke2 engine

```bash
kubectl apply -f artifacts/examples/aws/rke2/managed-addr.yaml
kubectl apply -f artifacts/examples/aws/rke2/managed-control-plane.yaml
kubectl apply -f artifacts/examples/aws/rke2/managed-dhcp.yaml
kubectl apply -f artifacts/examples/aws/rke2/managed-nodes.yaml
```

#### Create managed node on cloudstack with k3s engine

```bash
kubectl apply -f artifacts/examples/cloudstack/k3s/managed-addr.yaml
kubectl apply -f artifacts/examples/cloudstack/k3s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/cloudstack/k3s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/cloudstack/k3s/managed-nodes.yaml
```

#### Create managed node on cloudstack with kubeadm engine

```bash
kubectl apply -f artifacts/examples/cloudstack/kubeadm/managed-addr.yaml
kubectl apply -f artifacts/examples/cloudstack/kubeadm/managed-control-plane.yaml
kubectl apply -f artifacts/examples/cloudstack/kubeadm/managed-dhcp.yaml
kubectl apply -f artifacts/examples/cloudstack/kubeadm/managed-nodes.yaml
```

#### Create managed node on cloudstack with microks8 engine

```bash
kubectl apply -f artifacts/examples/cloudstack/microk8s/managed-addr.yaml
kubectl apply -f artifacts/examples/cloudstack/microk8s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/cloudstack/microk8s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/cloudstack/microk8s/managed-nodes.yaml
```

#### Create managed node on cloudstack with rke2 engine

```bash
kubectl apply -f artifacts/examples/cloudstack/rke2/managed-addr.yaml
kubectl apply -f artifacts/examples/cloudstack/rke2/managed-control-plane.yaml
kubectl apply -f artifacts/examples/cloudstack/rke2/managed-dhcp.yaml
kubectl apply -f artifacts/examples/cloudstack/rke2/managed-nodes.yaml
```

#### Create managed node on vmware desktop with k3s engine

```bash
kubectl apply -f artifacts/examples/desktop/k3s/managed-addr.yaml
kubectl apply -f artifacts/examples/desktop/k3s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/desktop/k3s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/desktop/k3s/managed-nodes.yaml
```

#### Create managed node on vmware desktop with kubeadm engine

```bash
kubectl apply -f artifacts/examples/desktop/kubeadm/managed-addr.yaml
kubectl apply -f artifacts/examples/desktop/kubeadm/managed-control-plane.yaml
kubectl apply -f artifacts/examples/desktop/kubeadm/managed-dhcp.yaml
kubectl apply -f artifacts/examples/desktop/kubeadm/managed-nodes.yaml
```

#### Create managed node on vmware desktop with microk8s engine

```bash
kubectl apply -f artifacts/examples/desktop/microk8s/managed-addr.yaml
kubectl apply -f artifacts/examples/desktop/microk8s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/desktop/microk8s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/desktop/microk8s/managed-nodes.yaml
```

#### Create managed node on vmware desktop with rke2 engine

```bash
kubectl apply -f artifacts/examples/desktop/rke2/managed-addr.yaml
kubectl apply -f artifacts/examples/desktop/rke2/managed-control-plane.yaml
kubectl apply -f artifacts/examples/desktop/rke2/managed-dhcp.yaml
kubectl apply -f artifacts/examples/desktop/rke2/managed-nodes.yaml
```

#### Create managed node on multipass with k3s engine

```bash
kubectl apply -f artifacts/examples/multipass/k3s/managed-addr.yaml
kubectl apply -f artifacts/examples/multipass/k3s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/multipass/k3s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/multipass/k3s/managed-nodes.yaml
```

#### Create managed node on multipass with kubeadm engine

```bash
kubectl apply -f artifacts/examples/multipass/kubeadm/managed-addr.yaml
kubectl apply -f artifacts/examples/multipass/kubeadm/managed-control-plane.yaml
kubectl apply -f artifacts/examples/multipass/kubeadm/managed-dhcp.yaml
kubectl apply -f artifacts/examples/multipass/kubeadm/managed-nodes.yaml
```

#### Create managed node on multipass with microk8s engine

```bash
kubectl apply -f artifacts/examples/multipass/microk8s/managed-addr.yaml
kubectl apply -f artifacts/examples/multipass/microk8s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/multipass/microk8s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/multipass/microk8s/managed-nodes.yaml
```

#### Create managed node on multipass with rke2 engine

```bash
kubectl apply -f artifacts/examples/multipass/rke2/managed-addr.yaml
kubectl apply -f artifacts/examples/multipass/rke2/managed-control-plane.yaml
kubectl apply -f artifacts/examples/multipass/rke2/managed-dhcp.yaml
kubectl apply -f artifacts/examples/multipass/rke2/managed-nodes.yaml
```

#### Create managed node on openstack with k3s engine

```bash
kubectl apply -f artifacts/examples/openstack/k3s/managed-addr.yaml
kubectl apply -f artifacts/examples/openstack/k3s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/openstack/k3s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/openstack/k3s/managed-nodes.yaml
```

#### Create managed node on openstack with kubeadm engine

```bash
kubectl apply -f artifacts/examples/openstack/kubeadm/managed-addr.yaml
kubectl apply -f artifacts/examples/openstack/kubeadm/managed-control-plane.yaml
kubectl apply -f artifacts/examples/openstack/kubeadm/managed-dhcp.yaml
kubectl apply -f artifacts/examples/openstack/kubeadm/managed-nodes.yaml
```

#### Create managed node on openstack with microk8s engine

```bash
kubectl apply -f artifacts/examples/openstack/microk8s/managed-addr.yaml
kubectl apply -f artifacts/examples/openstack/microk8s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/openstack/microk8s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/openstack/microk8s/managed-nodes.yaml
```

#### Create managed node on openstack with rke2 engine

```bash
kubectl apply -f artifacts/examples/openstack/rke2/managed-addr.yaml
kubectl apply -f artifacts/examples/openstack/rke2/managed-control-plane.yaml
kubectl apply -f artifacts/examples/openstack/rke2/managed-dhcp.yaml
kubectl apply -f artifacts/examples/openstack/rke2/managed-nodes.yaml
```

#### Create managed node on vsphere with k3s engine

```bash
kubectl apply -f artifacts/examples/vsphere/k3s/managed-addr.yaml
kubectl apply -f artifacts/examples/vsphere/k3s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/vsphere/k3s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/vsphere/k3s/managed-nodes.yaml
```

#### Create managed node on vsphere with kubeadm engine

```bash
kubectl apply -f artifacts/examples/vsphere/kubeadm/managed-addr.yaml
kubectl apply -f artifacts/examples/vsphere/kubeadm/managed-control-plane.yaml
kubectl apply -f artifacts/examples/vsphere/kubeadm/managed-dhcp.yaml
kubectl apply -f artifacts/examples/vsphere/kubeadm/managed-nodes.yaml
```

#### Create managed node on vsphere with microk8s engine

```bash
kubectl apply -f artifacts/examples/vsphere/microk8s/managed-addr.yaml
kubectl apply -f artifacts/examples/vsphere/microk8s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/vsphere/microk8s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/vsphere/microk8s/managed-nodes.yaml
```

#### Create managed node on vsphere with rke2 engine

```bash
kubectl apply -f artifacts/examples/vsphere/rke2/managed-addr.yaml
kubectl apply -f artifacts/examples/vsphere/rke2/managed-control-plane.yaml
kubectl apply -f artifacts/examples/vsphere/rke2/managed-dhcp.yaml
kubectl apply -f artifacts/examples/vsphere/rke2/managed-nodes.yaml
```

#### Create managed node on lxd with k3s engine

```bash
kubectl apply -f artifacts/examples/lxd/k3s/managed-addr.yaml
kubectl apply -f artifacts/examples/lxd/k3s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/lxd/k3s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/lxd/k3s/managed-nodes.yaml
```

#### Create managed node on lxd with kubeadm engine

```bash
kubectl apply -f artifacts/examples/lxd/kubeadm/managed-addr.yaml
kubectl apply -f artifacts/examples/lxd/kubeadm/managed-control-plane.yaml
kubectl apply -f artifacts/examples/lxd/kubeadm/managed-dhcp.yaml
kubectl apply -f artifacts/examples/lxd/kubeadm/managed-nodes.yaml
```

#### Create managed node on lxd with microk8s engine

```bash
kubectl apply -f artifacts/examples/lxd/microk8s/managed-addr.yaml
kubectl apply -f artifacts/examples/lxd/microk8s/managed-control-plane.yaml
kubectl apply -f artifacts/examples/lxd/microk8s/managed-dhcp.yaml
kubectl apply -f artifacts/examples/lxd/microk8s/managed-nodes.yaml
```

#### Create managed node on lxd with rke2 engine

```bash
kubectl apply -f artifacts/examples/lxd/rke2/managed-addr.yaml
kubectl apply -f artifacts/examples/lxd/rke2/managed-control-plane.yaml
kubectl apply -f artifacts/examples/lxd/rke2/managed-dhcp.yaml
kubectl apply -f artifacts/examples/lxd/rke2/managed-nodes.yaml
```

If you want delete the node just delete the CRD with the call

```bash
kubectl delete -f artifacts/examples/vsphere/rke2/managed-dhcp.yaml
```

You have the ability also to create a control plane as instead a worker

```bash
kubectl apply -f artifacts/examples/vsphere/rke2/managed-control-plane.yaml
```

The resource is cluster scope so you don't need a namespace. The name of the resource is not the name of the managed node.

The minimal resource declaration

```yaml
apiVersion: "nodemanager.aldunelabs.com/v1alpha2"
kind: "ManagedNode"
metadata:
  name: "vsphere-dev-k3s-managed-01"
spec:
  nodegroup: vsphere-dev-k3s
  instanceType: small
  diskSizeInMb: 10240
  diskType: gp3
```

The full qualified resource including networks declaration to override the default controller network management and adding some node labels & annotations. If you specify the managed node as controller, you can also allows the controlplane to support deployment as a worker node

```yaml
apiVersion: "nodemanager.aldunelabs.com/v1alpha2"
kind: "ManagedNode"
metadata:
  name: "vsphere-dev-k3s-managed-01"
spec:
  nodegroup: vsphere-dev-k3s
  controlPlane: false
  allowDeployment: true
  instanceType: small
  diskSizeInMB: 10240
  diskType: gp3
  labels:
  - demo-label.acme.com=demo
  - sample-label.acme.com=sample
  annotations:
  - demo-annotation.acme.com=demo
  - sample-annotation.acme.com=sample
  network:
    vmware:
      -
        network: "VM Network"
        address: 10.0.0.85
        netmask: 255.255.255.0
        use-dhcp-routes: false
        routes:
          - to: default
            via: 10.0.0.1
            metric: 100
          - to: x.x.0.0/16
            via: 10.0.0.1
            metric: 100
          - to: y.y.y.y/8
            via: 10.0.0.1
            metric: 500
      -
        network: "VLAN20"
        address: 192.168.2.80
        netmask: 255.255.255.0
        use-dhcp-routes: false
```

As example of use generated by autoscaled-masterkube-multipass scripts

```json
{
    "use-external-etcd": false,
    "src-etcd-ssl-dir": "/etc/etcd/ssl",
    "dst-etcd-ssl-dir": "/etc/kubernetes/pki/etcd",
    "distribution": "k3s",
    "plateform": "vsphere",
    "kubernetes-pki-srcdir": "/etc/kubernetes/pki",
    "kubernetes-pki-dstdir": "/etc/kubernetes/pki",
    "image-credential-provider-bin-dir": "/var/lib/rancher/credentialprovider/bin",
    "image-credential-provider-config": "/var/lib/rancher/credentialprovider/config.yaml",
    "listen": "unix:/var/run/cluster-autoscaler/autoscaler.sock",
    "secret": "vsphere",
    "minNode": 0,
    "maxNode": 9,
    "maxPods": 110,
    "maxNode-per-cycle": 2,
    "nodegroup": "vsphere-dev-k3s",
    "node-name-prefix": "autoscaled",
    "managed-name-prefix": "managed",
    "controlplane-name-prefix": "master",
    "nodePrice": 0,
    "podPrice": 0,
    "use-cloudinit-config": false,
    "cloudinit-file-owner": "root:adm",
    "cloudinit-file-mode": 420,
    "optionals": {
        "pricing": false,
        "getAvailableMachineTypes": false,
        "newNodeGroup": false,
        "templateNodeInfo": false,
        "createNodeGroup": false,
        "deleteNodeGroup": false
    },
    "k3s": {
        "address": "192.168.2.80:6443",
        "token": "...",
        "ca": "sha256:...",
        "extras-args": [
            "--ignore-preflight-errors=All"
        ],
        "datastore-endpoint": "",
        "extras-commands": []
    },
    "default-machine": "medium",
    "cloud-init": {
        "package_update": false,
        "package_upgrade": false,
        "growpart": {
            "ignore_growroot_disabled": false,
            "mode": "auto",
            "devices": [
                "/"
            ]
        },
        "runcmd": [
            "echo '192.168.2.80 vsphere-dev-k3s-masterkube vsphere-dev-k3s-masterkube.acme.com' >> /etc/hosts"
        ]
    },
    "ssh-infos": {
        "wait-ssh-ready-seconds": 180,
        "user": "kubernetes",
        "ssh-private-key": "/etc/ssh/id_rsa"
    },
    "autoscaling-options": {
        "scaleDownUtilizationThreshold": 0.5,
        "scaleDownGpuUtilizationThreshold": 0.5,
        "scaleDownUnneededTime": "1m",
        "scaleDownUnreadyTime": "1m",
        "maxNodeProvisionTime": "15m",
        "zeroOrMaxNodeScaling": false,
        "ignoreDaemonSetsUtilization": true
    },
    "credential-provider-config": {
        "apiVersion": "kubelet.config.k8s.io/v1",
        "kind": "CredentialProviderConfig",
        "providers": [
            {
                "name": "ecr-credential-provider",
                "matchImages": [
                    "*.dkr.ecr.*.amazonaws.com",
                    "*.dkr.ecr.*.amazonaws.cn",
                    "*.dkr.ecr-fips.*.amazonaws.com",
                    "*.dkr.ecr.us-iso-east-1.c2s.ic.gov",
                    "*.dkr.ecr.us-isob-east-1.sc2s.sgov.gov"
                ],
                "defaultCacheDuration": "12h",
                "apiVersion": "credentialprovider.kubelet.k8s.io/v1",
                "args": [
                    "get-credentials"
                ],
                "env": [
                    {
                        "name": "AWS_ACCESS_KEY_ID",
                        "value": "...."
                    },
                    {
                        "name": "AWS_SECRET_ACCESS_KEY",
                        "value": "...."
                    }
                ]
            }
        ]
    }
}
```

```json
{
    "url": "https://administrator@vsphere.acme.com:mypassword@vsphere.acme.com/sdk",
    "uid": "administrator@vsphere.acme.com",
    "password": "mypassword",
    "insecure": true,
    "dc": "DC01",
    "datastore": "datastore",
    "resource-pool": "ACME/Resources/FR",
    "vmFolder": "HOME",
    "timeout": 300,
    "template-name": "jammy-kubernetes-k3s-v1.29.1+k3s2-amd64",
    "template": false,
    "linked": false,
    "allow-upgrade": false,
    "customization": "",
    "region": "home",
    "zone": "office",
    "network": {
        "domain": "acme.com",
        "dns": {
            "search": [
                "acme.com"
            ],
            "nameserver": [
                "10.0.0.1"
            ]
        },
        "interfaces": [
            {
                "enabled": true,
                "primary": true,
                "exists": true,
                "network": "VLAN20",
                "adapter": "vmxnet3",
                "mac-address": "generate",
                "nic": "eth0",
                "dhcp": true,
                "use-dhcp-routes": false,
                "address": "192.168.2.83",
                "netmask": "255.255.255.0",
                "routes": [
                    {
                        "to": "default",
                        "via": "192.168.2.254",
                        "metric": 100
                    }
                ]
            },
            {
                "enabled": true,
                "primary": false,
                "exists": false,
                "network": "VM Network",
                "adapter": "vmxnet3",
                "mac-address": "generate",
                "nic": "eth1",
                "dhcp": true,
                "use-dhcp-routes": true,
                "routes": [
                    {
                        "to": "172.30.0.0/16",
                        "via": "10.0.0.1",
                        "metric": 500
                    }
                ]
            }
        ]
    }
}
````

