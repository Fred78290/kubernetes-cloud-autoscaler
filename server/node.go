package server

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/vsphere"
	glog "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	uid "k8s.io/apimachinery/pkg/types"
)

// AutoScalerServerNodeState VM state
type AutoScalerServerNodeState int32

// AutoScalerServerNodeType node class (external, autoscaled, managed)
type AutoScalerServerNodeType int32

// autoScalerServerNodeStateString strings
var autoScalerServerNodeStateString = []string{
	"AutoScalerServerNodeStateNotCreated",
	"AutoScalerServerNodeStateCreating",
	"AutoScalerServerNodeStateRunning",
	"AutoScalerServerNodeStateStopped",
	"AutoScalerServerNodeStateDeleted",
	"AutoScalerServerNodeStateUndefined",
}

const (
	// AutoScalerServerNodeStateNotCreated not created state
	AutoScalerServerNodeStateNotCreated = iota

	// AutoScalerServerNodeStateCreating running state
	AutoScalerServerNodeStateCreating

	// AutoScalerServerNodeStateRunning running state
	AutoScalerServerNodeStateRunning

	// AutoScalerServerNodeStateStopped stopped state
	AutoScalerServerNodeStateStopped

	// AutoScalerServerNodeStateDeleted deleted state
	AutoScalerServerNodeStateDeleted

	// AutoScalerServerNodeStateDeleting deleting state
	AutoScalerServerNodeStateDeleting

	// AutoScalerServerNodeStateUndefined undefined state
	AutoScalerServerNodeStateUndefined
)

const (
	// AutoScalerServerNodeExternal is a node create out of autoscaler
	AutoScalerServerNodeExternal = iota
	// AutoScalerServerNodeAutoscaled is a node create by autoscaler
	AutoScalerServerNodeAutoscaled
	// AutoScalerServerNodeManaged is a node managed by controller
	AutoScalerServerNodeManaged
)

const (
	joinClusterInfo          = "Join cluster for node:%s for nodegroup: %s"
	unableToExecuteCmdError  = "unable to execute command: %s, output: %s, reason:%v"
	nodeNameTemplate         = "%s-%s-%02d"
	errWriteFileErrorMsg     = "unable to write file: %s, reason: %v"
	tmpConfigDestinationFile = "/tmp/config.yaml"
)

// AutoScalerServerNode Describe a AutoScaler VM
// Node name and instance name could be differ when using AWS cloud provider
type AutoScalerServerNode struct {
	NodeGroupID      string                       `json:"group"`
	InstanceName     string                       `json:"instance-name"`
	NodeName         string                       `json:"node-name"`
	NodeIndex        int                          `json:"index"`
	VMUUID           string                       `json:"vm-uuid"`
	CRDUID           uid.UID                      `json:"crd-uid"`
	Memory           int                          `json:"memory"`
	CPU              int                          `json:"cpu"`
	DiskSize         int                          `json:"diskSize"`
	DiskType         string                       `default:"standard" json:"diskType"`
	InstanceType     string                       `json:"instance-Type"`
	IPAddress        string                       `json:"address"`
	State            AutoScalerServerNodeState    `json:"state"`
	NodeType         AutoScalerServerNodeType     `json:"type"`
	ControlPlaneNode bool                         `json:"control-plane,omitempty"`
	AllowDeployment  bool                         `json:"allow-deployment,omitempty"`
	ExtraLabels      types.KubernetesLabel        `json:"labels,omitempty"`
	ExtraAnnotations types.KubernetesLabel        `json:"annotations,omitempty"`
	CloudConfig      providers.CloudConfiguration `json:"cloud-config"`
	serverConfig     *types.AutoScalerServerConfig
}

func (s AutoScalerServerNodeState) String() string {
	return autoScalerServerNodeStateString[s]
}

func (vm *AutoScalerServerNode) waitReady(c types.ClientGenerator) error {
	glog.Debugf("AutoScalerNode::waitReady, node:%s", vm.NodeName)

	return c.WaitNodeToBeReady(vm.NodeName)
}

func (vm *AutoScalerServerNode) recopyEtcdSslFilesIfNeeded() error {
	var err error

	if (vm.serverConfig.Distribution == nil || *vm.serverConfig.Distribution != types.RKE2DistributionName) && (vm.ControlPlaneNode || *vm.serverConfig.UseExternalEtdc) {
		glog.Infof("Recopy Etcd ssl files for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroupID)

		timeout := vm.CloudConfig.GetTimeout()

		if err = utils.Scp(vm.serverConfig.SSH, vm.IPAddress, vm.serverConfig.ExtSourceEtcdSslDir, "."); err != nil {
			glog.Errorf("scp failed: %v", err)
		} else if _, err = utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, timeout, fmt.Sprintf("mkdir -p %s", vm.serverConfig.ExtDestinationEtcdSslDir)); err != nil {
			glog.Errorf("mkdir failed: %v", err)
		} else if _, err = utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, timeout, fmt.Sprintf("cp -r %s/* %s", filepath.Base(vm.serverConfig.ExtSourceEtcdSslDir), vm.serverConfig.ExtDestinationEtcdSslDir)); err != nil {
			glog.Errorf("mv failed: %v", err)
		} else if _, err = utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, timeout, fmt.Sprintf("chown -R root:root %s", vm.serverConfig.ExtDestinationEtcdSslDir)); err != nil {
			glog.Errorf("chown failed: %v", err)
		}
	}

	return err
}

func (vm *AutoScalerServerNode) recopyKubernetesPKIIfNeeded() error {
	var err error

	if (vm.serverConfig.Distribution == nil || *vm.serverConfig.Distribution != types.RKE2DistributionName) && vm.ControlPlaneNode {
		glog.Infof("Recopy PKI for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroupID)

		timeout := vm.CloudConfig.GetTimeout()

		if err = utils.Scp(vm.serverConfig.SSH, vm.IPAddress, vm.serverConfig.KubernetesPKISourceDir, "."); err != nil {
			glog.Errorf("scp failed: %v", err)
		} else if _, err = utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, timeout, fmt.Sprintf("mkdir -p %s", vm.serverConfig.KubernetesPKIDestDir)); err != nil {
			glog.Errorf("mkdir failed: %v", err)
		} else if _, err = utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, timeout, fmt.Sprintf("cp -r %s/* %s", filepath.Base(vm.serverConfig.KubernetesPKISourceDir), vm.serverConfig.KubernetesPKIDestDir)); err != nil {
			glog.Errorf("mv failed: %v", err)
		} else if _, err = utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, timeout, fmt.Sprintf("chown -R root:root %s", vm.serverConfig.KubernetesPKIDestDir)); err != nil {
			glog.Errorf("chown failed: %v", err)
		}
	}

	return err
}

func (vm *AutoScalerServerNode) executeCommands(args []string, restartKubelet bool, c types.ClientGenerator) error {
	glog.Infof(joinClusterInfo, vm.NodeName, vm.NodeGroupID)

	command := fmt.Sprintf("sh -c \"%s\"", strings.Join(args, " && "))

	timeout := vm.CloudConfig.GetTimeout()

	if out, err := utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, timeout, command); err != nil {
		return fmt.Errorf(unableToExecuteCmdError, command, out, err)
	} else {

		if restartKubelet {
			// To be sure, with kubeadm 1.26.1, the kubelet is not correctly restarted
			time.Sleep(5 * time.Second)
		}

		return context.PollImmediate(5*time.Second, time.Duration(vm.serverConfig.SSH.WaitSshReadyInSeconds)*time.Second, func() (done bool, err error) {
			if node, err := c.GetNode(vm.NodeName); err == nil && node != nil {
				return true, nil
			}

			if restartKubelet {
				glog.Infof("Restart kubelet for node:%s for nodegroup: %s", vm.NodeName, vm.NodeGroupID)

				if out, err := utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, timeout, "systemctl restart kubelet"); err != nil {
					return false, fmt.Errorf("unable to restart kubelet, output: %s, reason:%v", out, err)
				}
			}

			return false, nil
		})
	}
}

func (vm *AutoScalerServerNode) kubeAdmJoin(c types.ClientGenerator) error {
	kubeAdm := vm.serverConfig.KubeAdm

	args := []string{
		"kubeadm",
		"join",
		kubeAdm.Address,
		"--node-name",
		vm.NodeName,
		"--token",
		kubeAdm.Token,
		"--discovery-token-ca-cert-hash",
		kubeAdm.CACert,
		"--apiserver-advertise-address",
		vm.IPAddress,
	}

	if vm.ControlPlaneNode {
		args = append(args, "--control-plane")
	}

	// Append extras arguments
	if len(kubeAdm.ExtraArguments) > 0 {
		args = append(args, kubeAdm.ExtraArguments...)
	}

	command := []string{
		strings.Join(args, " "),
	}

	return vm.executeCommands(command, true, c)
}

func (vm *AutoScalerServerNode) externalAgentJoin(c types.ClientGenerator) error {
	var result error

	external := vm.serverConfig.External
	config := map[string]interface{}{
		"provider-id":              vm.generateProviderID(),
		"max-pods":                 vm.serverConfig.MaxPods,
		"node-name":                vm.NodeName,
		"server":                   external.Address,
		"token":                    external.Token,
		"disable-cloud-controller": vm.ControlPlaneNode && vm.serverConfig.UseControllerManager != nil && *vm.serverConfig.UseControllerManager,
	}

	if external.ExtraConfig != nil {
		for k, v := range external.ExtraConfig {
			config[k] = v
		}
	}

	if vm.ControlPlaneNode && vm.serverConfig.UseExternalEtdc != nil && *vm.serverConfig.UseExternalEtdc {
		config["datastore-endpoint"] = external.DatastoreEndpoint
		config["datastore-cafile"] = fmt.Sprintf("%s/ca.pem", vm.serverConfig.ExtDestinationEtcdSslDir)
		config["datastore-certfile"] = fmt.Sprintf("%s/etcd.pem", vm.serverConfig.ExtDestinationEtcdSslDir)
		config["datastore-keyfile"] = fmt.Sprintf("%s/etcd-key.pem", vm.serverConfig.ExtDestinationEtcdSslDir)
	}

	if f, err := os.CreateTemp(os.TempDir(), "config.*.yaml"); err != nil {
		result = fmt.Errorf("unable to create %s, reason: %v", external.ConfigPath, err)
	} else {
		defer os.Remove(f.Name())

		if _, err = f.WriteString(utils.ToYAML(config)); err != nil {
			f.Close()
			result = fmt.Errorf(errWriteFileErrorMsg, f.Name(), err)
		} else {
			f.Close()

			if err = utils.Scp(vm.serverConfig.SSH, vm.IPAddress, f.Name(), tmpConfigDestinationFile); err != nil {
				result = fmt.Errorf("unable to transfer file: %s to %s, reason: %v", f.Name(), tmpConfigDestinationFile, err)
			} else {
				args := []string{
					fmt.Sprintf("mkdir -p %s", path.Dir(external.ConfigPath)),
					fmt.Sprintf("cp %s %s", tmpConfigDestinationFile, external.ConfigPath),
					external.JoinCommand,
				}

				result = vm.executeCommands(args, false, c)
			}
		}
	}

	return result
}

func (vm *AutoScalerServerNode) rke2AgentJoin(c types.ClientGenerator) error {
	var result error

	rke2 := vm.serverConfig.RKE2
	service := "rke2-server"
	config := map[string]interface{}{
		"kubelet-arg": []string{
			"cloud-provider=external",
			"fail-swap-on=false",
			fmt.Sprintf("provider-id=%s", vm.generateProviderID()),
			fmt.Sprintf("max-pods=%d", vm.serverConfig.MaxPods),
		},
		"node-name": vm.NodeName,
		"server":    fmt.Sprintf("https://%s", rke2.Address),
		"token":     rke2.Token,
	}

	if vm.ControlPlaneNode {
		if vm.serverConfig.UseControllerManager != nil && *vm.serverConfig.UseControllerManager {
			config["disable-cloud-controller"] = true
			config["cloud-provider-name"] = "external"
		}

		config["disable"] = []string{
			"rke2-ingress-nginx",
			"rke2-metrics-server",
		}
	}

	// Append extras arguments
	if len(rke2.ExtraCommands) > 0 {
		for _, cmd := range rke2.ExtraCommands {
			args := strings.Split(cmd, "=")
			config[args[0]] = args[1]
		}
	}

	if vm.ControlPlaneNode {
		service = "rke2-server"
	} else {
		service = "rke2-agent"
	}

	const dstFile = "/etc/rancher/rke2/config.yaml"

	if f, err := os.CreateTemp(os.TempDir(), "config.*.yaml"); err != nil {
		result = fmt.Errorf("unable to create %s, reason: %v", dstFile, err)
	} else {
		defer os.Remove(f.Name())

		if _, err = f.WriteString(utils.ToYAML(config)); err != nil {
			f.Close()
			result = fmt.Errorf(errWriteFileErrorMsg, f.Name(), err)
		} else {
			f.Close()

			if err = utils.Scp(vm.serverConfig.SSH, vm.IPAddress, f.Name(), tmpConfigDestinationFile); err != nil {
				result = fmt.Errorf("unable to transfer file: %s to %s, reason: %v", f.Name(), tmpConfigDestinationFile, err)
			} else {
				args := make([]string, 0, 5)

				if rke2.DeleteCredentialsProvider {
					args = append(args, "rm -rf /var/lib/rancher/credentialprovider")
				}

				args = append(args,
					fmt.Sprintf("cp %s %s", tmpConfigDestinationFile, dstFile),
					fmt.Sprintf("systemctl enable %s.service", service),
					fmt.Sprintf("systemctl start %s.service", service))

				result = vm.executeCommands(args, false, c)
			}
		}
	}

	return result
}

func (vm *AutoScalerServerNode) retrieveNodeInfo(c types.ClientGenerator) error {
	if nodeInfo, err := c.GetNode(vm.NodeName); err != nil {
		return err
	} else {
		vm.CPU = int(nodeInfo.Status.Capacity.Cpu().Value())
		vm.Memory = int(nodeInfo.Status.Capacity.Memory().Value() / (1024 * 1024))
		vm.DiskSize = int(nodeInfo.Status.Capacity.Storage().Value() / (1024 * 1024))
	}

	return nil
}

func (vm *AutoScalerServerNode) k3sAgentJoin(c types.ClientGenerator) error {
	k3s := vm.serverConfig.K3S
	args := []string{
		fmt.Sprintf("echo K3S_ARGS='--kubelet-arg=provider-id=%s --kubelet-arg=max-pods=%d --node-name=%s --server=https://%s --token=%s' > /etc/systemd/system/k3s.service.env", vm.generateProviderID(), vm.serverConfig.MaxPods, vm.NodeName, k3s.Address, k3s.Token),
	}

	if vm.ControlPlaneNode {
		if vm.serverConfig.UseControllerManager != nil && *vm.serverConfig.UseControllerManager {
			args = append(args, "echo 'K3S_MODE=server' > /etc/default/k3s", "echo K3S_DISABLE_ARGS='--disable-cloud-controller --disable=servicelb --disable=traefik --disable=metrics-server' > /etc/systemd/system/k3s.disabled.env")
		} else {
			args = append(args, "echo 'K3S_MODE=server' > /etc/default/k3s", "echo K3S_DISABLE_ARGS='--disable=servicelb --disable=traefik --disable=metrics-server' > /etc/systemd/system/k3s.disabled.env")
		}

		if vm.serverConfig.UseExternalEtdc != nil && *vm.serverConfig.UseExternalEtdc {
			args = append(args, fmt.Sprintf("echo K3S_SERVER_ARGS='--datastore-endpoint=%s --datastore-cafile=%s/ca.pem --datastore-certfile=%s/etcd.pem --datastore-keyfile=%s/etcd-key.pem' > /etc/systemd/system/k3s.server.env", k3s.DatastoreEndpoint, vm.serverConfig.ExtDestinationEtcdSslDir, vm.serverConfig.ExtDestinationEtcdSslDir, vm.serverConfig.ExtDestinationEtcdSslDir))
		}
	}

	// Append extras arguments
	if len(k3s.ExtraCommands) > 0 {
		args = append(args, k3s.ExtraCommands...)
	}

	if k3s.DeleteCredentialsProvider {
		args = append(args, "rm -rf /var/lib/rancher/credentialprovider")
	}

	args = append(args, "systemctl enable k3s.service", "systemctl start k3s.service")

	return vm.executeCommands(args, false, c)
}

func (vm *AutoScalerServerNode) joinCluster(c types.ClientGenerator) error {
	glog.Infof("Register node in cluster for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroupID)

	if vm.serverConfig.Distribution != nil {
		if *vm.serverConfig.Distribution == types.K3SDistributionName {
			return vm.k3sAgentJoin(c)
		} else if *vm.serverConfig.Distribution == types.RKE2DistributionName {
			return vm.rke2AgentJoin(c)
		} else if *vm.serverConfig.Distribution == types.ExternalDistributionName {
			return vm.externalAgentJoin(c)
		}
	}

	return vm.kubeAdmJoin(c)
}

func (vm *AutoScalerServerNode) setNodeLabels(c types.ClientGenerator, nodeLabels, systemLabels types.KubernetesLabel) error {
	topology := vm.CloudConfig.GetTopologyLabels()
	labels := types.MergeKubernetesLabel(nodeLabels, topology, systemLabels, vm.ExtraLabels)

	if err := c.LabelNode(vm.NodeName, labels); err != nil {
		return fmt.Errorf(constantes.ErrLabelNodeReturnError, vm.NodeName, err)
	}

	annotations := types.KubernetesLabel{
		constantes.AnnotationNodeGroupName:        vm.NodeGroupID,
		constantes.AnnotationScaleDownDisabled:    strconv.FormatBool(vm.NodeType != AutoScalerServerNodeAutoscaled),
		constantes.AnnotationNodeAutoProvisionned: strconv.FormatBool(vm.NodeType == AutoScalerServerNodeAutoscaled),
		constantes.AnnotationNodeManaged:          strconv.FormatBool(vm.NodeType == AutoScalerServerNodeManaged),
		constantes.AnnotationNodeIndex:            strconv.Itoa(vm.NodeIndex),
		constantes.AnnotationInstanceName:         vm.InstanceName,
		constantes.AnnotationInstanceID:           vm.VMUUID,
	}

	annotations = types.MergeKubernetesLabel(annotations, vm.ExtraAnnotations)

	if err := c.AnnoteNode(vm.NodeName, annotations); err != nil {
		return fmt.Errorf(constantes.ErrAnnoteNodeReturnError, vm.NodeName, err)
	}

	if vm.ControlPlaneNode && vm.AllowDeployment {
		if err := c.TaintNode(vm.NodeName,
			apiv1.Taint{
				Key:    constantes.NodeLabelControlPlaneRole,
				Effect: apiv1.TaintEffectNoSchedule,
				TimeAdded: &metav1.Time{
					Time: time.Now(),
				},
			},
			apiv1.Taint{
				Key:    constantes.NodeLabelMasterRole,
				Effect: apiv1.TaintEffectNoSchedule,
				TimeAdded: &metav1.Time{
					Time: time.Now(),
				},
			}); err != nil {
			return fmt.Errorf(constantes.ErrTaintNodeReturnError, vm.NodeName, err)
		}
	}

	return nil
}

func (vm *AutoScalerServerNode) waitForSshReady() error {
	return context.PollImmediate(time.Second, time.Duration(vm.serverConfig.SSH.WaitSshReadyInSeconds)*time.Second, func() (done bool, err error) {
		if _, err := utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, vm.CloudConfig.GetTimeout(), "ls"); err != nil {
			if strings.HasSuffix(err.Error(), "connection refused") {
				glog.Warnf("Wait ssh ready for node: %s, address: %s.", vm.NodeName, vm.IPAddress)
				return false, nil
			}

			return false, fmt.Errorf("unable to ssh: %s, reason:%v", vm.NodeName, err)
		}

		return true, nil
	})
}

func (vm *AutoScalerServerNode) launchVM(c types.ClientGenerator, nodeLabels, systemLabels types.KubernetesLabel) error {
	glog.Debugf("AutoScalerNode::launchVM, node:%s", vm.NodeName)

	var err error
	var status AutoScalerServerNodeState
	var hostsystem string

	vsphere := vm.CloudConfig
	network := vsphere.Network
	userInfo := vm.serverConfig.SSH

	glog.Infof("Launch VM:%s for nodegroup: %s", vm.InstanceName, vm.NodeGroupID)

	if vm.State != AutoScalerServerNodeStateNotCreated {
		return fmt.Errorf(constantes.ErrVMAlreadyCreated, vm.InstanceName)
	}

	if vsphere.Exists(vm.NodeName) {
		glog.Warnf(constantes.ErrVMAlreadyExists, vm.InstanceName)
		return fmt.Errorf(constantes.ErrVMAlreadyExists, vm.InstanceName)
	}

	vm.State = AutoScalerServerNodeStateCreating

	if vm.NodeType != AutoScalerServerNodeAutoscaled && vm.NodeType != AutoScalerServerNodeManaged {

		err = fmt.Errorf(constantes.ErrVMNotProvisionnedByMe, vm.InstanceName)

	} else if _, err = vsphere.Create(vm.InstanceName, userInfo.GetUserName(), userInfo.GetAuthKeys(), vm.serverConfig.CloudInit, network, "", true, vm.Memory, vm.CPU, vm.DiskSize, vm.NodeIndex); err != nil {

		err = fmt.Errorf(constantes.ErrUnableToLaunchVM, vm.InstanceName, err)

	} else if vm.VMUUID, err = vsphere.UUID(vm.InstanceName); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if err = vsphere.PowerOn(vm.InstanceName); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if hostsystem, err = vsphere.GetHostSystem(vm.InstanceName); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if err = vsphere.SetAutoStart(hostsystem, vm.InstanceName, -1); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if _, err = vsphere.WaitForToolsRunning(vm.InstanceName); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if _, err = vsphere.WaitForIP(vm.InstanceName); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if status, err = vm.statusVM(); err != nil {

		err = fmt.Errorf(constantes.ErrGetVMInfoFailed, vm.InstanceName, err)

	} else if status != AutoScalerServerNodeStateRunning {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if err = vm.waitForSshReady(); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if err = vm.recopyKubernetesPKIIfNeeded(); err != nil {

		err = fmt.Errorf(constantes.ErrRecopyKubernetesPKIFailed, vm.InstanceName, err)

	} else if err = vm.recopyEtcdSslFilesIfNeeded(); err != nil {

		err = fmt.Errorf(constantes.ErrUpdateEtcdSslFailed, vm.InstanceName, err)

	} else if err = vm.joinCluster(c); err != nil {

		err = fmt.Errorf(constantes.ErrKubeAdmJoinFailed, vm.InstanceName, err)

	} else if err = vm.setProviderID(c); err != nil {

		err = fmt.Errorf(constantes.ErrProviderIDNotConfigured, vm.InstanceName, err)

	} else if err = vm.waitReady(c); err != nil {

		err = fmt.Errorf(constantes.ErrNodeIsNotReady, vm.InstanceName)

	} else {
		err = vm.setNodeLabels(c, nodeLabels, systemLabels)
	}

	if err == nil {
		glog.Infof("Launched VM:%s for nodegroup: %s", vm.InstanceName, vm.NodeGroupID)
	} else {
		glog.Errorf("Unable to launch VM:%s for nodegroup: %s. Reason: %v", vm.InstanceName, vm.NodeGroupID, err.Error())
	}

	return err
}

// WaitSSHReady method SSH test IP
func (vm *AutoScalerServerNode) WaitSSHReady(nodename, address string) error {
	return context.PollImmediate(time.Second, time.Duration(vm.serverConfig.SSH.WaitSshReadyInSeconds)*time.Second, func() (bool, error) {
		// Set hostname
		if _, err := utils.Sudo(vm.serverConfig.SSH, address, time.Second, fmt.Sprintf("hostnamectl set-hostname %s", nodename)); err != nil {
			if strings.HasSuffix(err.Error(), "connection refused") || strings.HasSuffix(err.Error(), "i/o timeout") {
				return false, nil
			}

			return false, err
		}

		// Node name and instance name could be differ when using AWS cloud provider
		if *vm.serverConfig.CloudProvider == "aws" {

			if nodeName, err := utils.Sudo(vm.serverConfig.SSH, address, 1, "curl -s http://169.254.169.254/latest/meta-data/local-hostname"); err == nil {
				vm.NodeName = nodeName

				glog.Debugf("Launch VM:%s set to nodeName: %s", nodename, nodeName)
			} else {
				return false, err
			}
		}

		return true, nil
	})

}

func (vm *AutoScalerServerNode) WaitForIP() (*string, error) {
	glog.Infof("Wait IP ready for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroupID)

	return vm.CloudConfig.WaitForVMReady(vm)
}

func (vm *AutoScalerServerNode) startVM(c types.ClientGenerator) error {
	glog.Debugf("AutoScalerNode::startVM, node:%s", vm.InstanceName)

	var err error
	var state AutoScalerServerNodeState

	glog.Infof("Start VM:%s", vm.InstanceName)

	vsphere := vm.CloudConfig

	if vm.NodeType != AutoScalerServerNodeAutoscaled && vm.NodeType != AutoScalerServerNodeManaged {

		err = fmt.Errorf(constantes.ErrVMNotProvisionnedByMe, vm.InstanceName)

	} else if state, err = vm.statusVM(); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if state == AutoScalerServerNodeStateStopped {

		if err = vsphere.PowerOn(vm.NodeName); err != nil {

			err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

		} else if _, err = vsphere.WaitForToolsRunning(vm.NodeName); err != nil {

			err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

		} else if _, err = vsphere.WaitForIP(vm.NodeName); err != nil {

			err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

		} else if state, err = vm.statusVM(); err != nil {

			err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

		} else if state != AutoScalerServerNodeStateRunning {

			err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

		} else {
			if err = c.UncordonNode(vm.NodeName); err != nil {
				glog.Errorf(constantes.ErrUncordonNodeReturnError, vm.NodeName, err)

				err = nil
			}

			vm.State = AutoScalerServerNodeStateRunning
		}
	} else if state != AutoScalerServerNodeStateRunning {
		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, fmt.Sprintf("Unexpected state: %d", state))
	}

	if err == nil {
		glog.Infof("Started VM:%s", vm.InstanceName)
	} else {
		glog.Errorf("Unable to start VM:%s. Reason: %v", vm.InstanceName, err)
	}

	return err
}

func (vm *AutoScalerServerNode) stopVM(c types.ClientGenerator) error {
	glog.Debugf("AutoScalerNode::stopVM, node:%s", vm.InstanceName)

	var err error
	var state AutoScalerServerNodeState

	glog.Infof("Stop VM:%s", vm.InstanceName)

	vsphere := vm.CloudConfig

	if vm.NodeType != AutoScalerServerNodeAutoscaled && vm.NodeType != AutoScalerServerNodeManaged {

		err = fmt.Errorf(constantes.ErrVMNotProvisionnedByMe, vm.InstanceName)

	} else if state, err = vm.statusVM(); err != nil {

		err = fmt.Errorf(constantes.ErrStopVMFailed, vm.InstanceName, err)

	} else if state == AutoScalerServerNodeStateRunning {
		if err = c.CordonNode(vm.NodeName); err != nil {
			glog.Errorf(constantes.ErrCordonNodeReturnError, vm.NodeName, err)
		}

		if err = vsphere.PowerOff(vm.NodeName); err == nil {
			vm.State = AutoScalerServerNodeStateStopped
		} else {
			err = fmt.Errorf(constantes.ErrStopVMFailed, vm.InstanceName, err)
		}

	} else if state != AutoScalerServerNodeStateStopped {

		err = fmt.Errorf(constantes.ErrStopVMFailed, vm.InstanceName, fmt.Sprintf("Unexpected state: %d", state))

	}

	if err == nil {
		glog.Infof("Stopped VM:%s", vm.InstanceName)
	} else {
		glog.Errorf("Could not stop VM:%s. Reason: %s", vm.InstanceName, err)
	}

	return err
}

func (vm *AutoScalerServerNode) deleteVM(c types.ClientGenerator) error {
	glog.Debugf("AutoScalerNode::deleteVM, node:%s", vm.InstanceName)

	var err error
	var status *vsphere.Status

	if vm.NodeType != AutoScalerServerNodeAutoscaled && vm.NodeType != AutoScalerServerNodeManaged {
		err = fmt.Errorf(constantes.ErrVMNotProvisionnedByMe, vm.InstanceName)
	} else {
		vsphere := vm.CloudConfig

		if status, err = vsphere.Status(vm.NodeName); err == nil {
			if status.Powered {
				// Delete kubernetes node only is alive
				if _, err = c.GetNode(vm.NodeName); err == nil {
					if err = c.MarkDrainNode(vm.NodeName); err != nil {
						glog.Errorf(constantes.ErrCordonNodeReturnError, vm.NodeName, err)
					}

					if err = c.DrainNode(vm.NodeName, true, true); err != nil {
						glog.Errorf(constantes.ErrDrainNodeReturnError, vm.NodeName, err)
					}

					if err = c.DeleteNode(vm.NodeName); err != nil {
						glog.Errorf(constantes.ErrDeleteNodeReturnError, vm.NodeName, err)
					}
				}

				if err = vsphere.PowerOff(vm.NodeName); err != nil {
					err = fmt.Errorf(constantes.ErrStopVMFailed, vm.InstanceName, err)
				} else {
					vm.State = AutoScalerServerNodeStateStopped

					if err = vsphere.Delete(vm.NodeName); err != nil {
						err = fmt.Errorf(constantes.ErrDeleteVMFailed, vm.InstanceName, err)
					}
				}
			} else if err = vsphere.Delete(vm.NodeName); err != nil {
				err = fmt.Errorf(constantes.ErrDeleteVMFailed, vm.InstanceName, err)
			}
		}
	}

	if err == nil {
		glog.Infof("Deleted VM:%s", vm.InstanceName)
		vm.State = AutoScalerServerNodeStateDeleted
	} else {
		glog.Errorf("Could not delete VM:%s. Reason: %s", vm.InstanceName, err)
	}

	return err
}

func (vm *AutoScalerServerNode) statusVM() (AutoScalerServerNodeState, error) {
	glog.Debugf("AutoScalerNode::statusVM, node:%s", vm.InstanceName)

	// Get VM infos
	var status *vsphere.Status
	var err error

	if status, err = vm.CloudConfig.Status(vm.NodeName); err != nil {
		glog.Errorf(constantes.ErrGetVMInfoFailed, vm.InstanceName, err)
		return AutoScalerServerNodeStateUndefined, err
	}

	if status != nil {
		vm.IPAddress = vm.CloudConfig.FindPreferredIPAddress(status.Interfaces)

		if status.Powered {
			vm.State = AutoScalerServerNodeStateRunning
		} else {
			vm.State = AutoScalerServerNodeStateStopped
		}

		return vm.State, nil
	}

	return AutoScalerServerNodeStateUndefined, fmt.Errorf(constantes.ErrAutoScalerInfoNotFound, vm.InstanceName)
}

func (vm *AutoScalerServerNode) setProviderID(c types.ClientGenerator) error {
	if vm.serverConfig.UseControllerManager != nil && !*vm.serverConfig.UseControllerManager {
		return c.SetProviderID(vm.NodeName, vm.generateProviderID())
	}

	return nil
}

func (vm *AutoScalerServerNode) generateProviderID() string {
	if vm.serverConfig.UseControllerManager != nil && *vm.serverConfig.UseControllerManager {
		return vm.CloudConfig.GenerateProviderID(vm.VMUUID)
	}

	if vm.serverConfig.Distribution != nil {
		if *vm.serverConfig.Distribution == types.K3SDistributionName {
			return fmt.Sprintf("k3s://%s", vm.NodeName)
		} else if *vm.serverConfig.Distribution == types.RKE2DistributionName {
			return fmt.Sprintf("k3s://%s", vm.NodeName)
		}
	}

	return ""
}

func (vm *AutoScalerServerNode) findInstanceUUID() string {
	if vmUUID, err := vm.CloudConfig.UUID(vm.NodeName); err == nil {
		vm.VMUUID = vmUUID

		return vmUUID
	}

	return ""
}

func (vm *AutoScalerServerNode) setServerConfiguration(config *types.AutoScalerServerConfig) {
	vm.CloudConfig.UpdateMacAddressTable(vm.NodeIndex)
	vm.serverConfig = config
}

func (vm *AutoScalerServerNode) retrieveNetworkInfos() error {
	return vm.CloudConfig.RetrieveNetworkInfos(vm.NodeName, vm.VMUUID, vm.NodeIndex)
}

// cleanOnLaunchError called when error occurs during launch
func (vm *AutoScalerServerNode) cleanOnLaunchError(c types.ClientGenerator, err error) {
	glog.Errorf(constantes.ErrUnableToLaunchVM, vm.InstanceName, err)

	exists := fmt.Sprintf(constantes.ErrVMAlreadyExists, vm.InstanceName)

	if err.Error() != exists {
		if *vm.serverConfig.DebugMode {
			glog.Warningf("Debug mode enabled, don't delete VM: %s for inspection", vm.InstanceName)
		} else if status, _ := vm.statusVM(); status != AutoScalerServerNodeStateNotCreated {
			if e := vm.deleteVM(c); e != nil {
				glog.Errorf(constantes.ErrUnableToDeleteVM, vm.NodeName, e)
			}
		} else {
			glog.Warningf(constantes.WarnFailedVMNotDeleted, vm.NodeName, status)
		}
	}

}
