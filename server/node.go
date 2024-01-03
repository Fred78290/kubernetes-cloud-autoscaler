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
	glog "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	uid "k8s.io/apimachinery/pkg/types"
	kubelet "k8s.io/kubelet/config/v1beta1"
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
	mkdirCmd                 = "mkdir -p %s"
)

// AutoScalerServerNode Describe a AutoScaler VM
// Node name and instance name could be differ when using AWS cloud provider
type AutoScalerServerNode struct {
	NodeGroup    string  `json:"group"`
	NodeName     string  `json:"node-name"`
	NodeIndex    int     `json:"index"`
	InstanceName string  `json:"instance-name"`
	VMUUID       string  `json:"vm-uuid"`
	CRDUID       uid.UID `json:"crd-uid"`
	Memory       int     `json:"memory"`
	CPU          int     `json:"cpu"`
	DiskSize     int     `json:"diskSize"`
	//InstanceType     string                    `json:"instance-Type"`
	IPAddress        string                    `json:"address"`
	State            AutoScalerServerNodeState `json:"state"`
	NodeType         AutoScalerServerNodeType  `json:"type"`
	ControlPlaneNode bool                      `json:"control-plane,omitempty"`
	AllowDeployment  bool                      `json:"allow-deployment,omitempty"`
	ExtraLabels      types.KubernetesLabel     `json:"labels,omitempty"`
	ExtraAnnotations types.KubernetesLabel     `json:"annotations,omitempty"`
	providerHandler  providers.ProviderHandler
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

	if (vm.serverConfig.Distribution == nil || *vm.serverConfig.Distribution != providers.RKE2DistributionName) && (vm.ControlPlaneNode || *vm.serverConfig.UseExternalEtdc) {
		glog.Infof("Recopy Etcd ssl files for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroup)

		timeout := vm.providerHandler.GetTimeout()

		if err = utils.Scp(vm.serverConfig.SSH, vm.IPAddress, vm.serverConfig.ExtSourceEtcdSslDir, "."); err != nil {
			glog.Errorf("scp failed: %v", err)
		} else if _, err = utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, timeout, fmt.Sprintf(mkdirCmd, vm.serverConfig.ExtDestinationEtcdSslDir)); err != nil {
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

	if (vm.serverConfig.Distribution == nil || *vm.serverConfig.Distribution != providers.RKE2DistributionName) && vm.ControlPlaneNode {
		glog.Infof("Recopy PKI for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroup)

		timeout := vm.providerHandler.GetTimeout()

		if err = utils.Scp(vm.serverConfig.SSH, vm.IPAddress, vm.serverConfig.KubernetesPKISourceDir, "."); err != nil {
			glog.Errorf("scp failed: %v", err)
		} else if _, err = utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, timeout, fmt.Sprintf(mkdirCmd, vm.serverConfig.KubernetesPKIDestDir)); err != nil {
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
	glog.Infof(joinClusterInfo, vm.NodeName, vm.NodeGroup)

	command := fmt.Sprintf("sh -c \"%s\"", strings.Join(args, " && "))

	timeout := vm.providerHandler.GetTimeout()

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
				glog.Infof("Restart kubelet for node:%s for nodegroup: %s", vm.NodeName, vm.NodeGroup)

				if out, err := utils.Sudo(vm.serverConfig.SSH, vm.IPAddress, timeout, "systemctl restart kubelet"); err != nil {
					return false, fmt.Errorf("unable to restart kubelet, output: %s, reason:%v", out, err)
				}
			}

			return false, nil
		})
	}
}

func (vm *AutoScalerServerNode) writeKubeletMergeConfig(c types.ClientGenerator, maxPods int) error {
	if f, err := os.CreateTemp(os.TempDir(), "kubeletconfiguration0.*.yaml"); err != nil {
		return err
	} else {
		defer os.Remove(f.Name())

		kubeletConfiguration := kubelet.KubeletConfiguration{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KubeletConfiguration",
				APIVersion: kubelet.SchemeGroupVersion.Identifier(),
			},
			MaxPods: int32(maxPods),
		}

		if _, err = f.WriteString(utils.ToYAML(&kubeletConfiguration)); err != nil {
			defer f.Close()
			return fmt.Errorf(errWriteFileErrorMsg, f.Name(), err)
		} else {
			f.Close()

			tmpConfigDestinationFile := "/tmp/kubeletconfiguration0+merge.yaml"

			if err = utils.Scp(vm.serverConfig.SSH, vm.IPAddress, f.Name(), tmpConfigDestinationFile); err != nil {
				return fmt.Errorf(constantes.ErrCantCopyFileToNode, f.Name(), tmpConfigDestinationFile, err)
			} else {
				args := []string{
					"mkdir -p /etc/kubernetes/patches",
					fmt.Sprintf("cp %s /etc/kubernetes/patches", tmpConfigDestinationFile),
				}

				if err = vm.executeCommands(args, false, c); err != nil {
					return fmt.Errorf(constantes.ErrCantCopyFileToNode, f.Name(), tmpConfigDestinationFile, err)
				}
			}
		}
	}

	return nil
}

func (vm *AutoScalerServerNode) kubeAdmJoin(c types.ClientGenerator, maxPods int) error {
	kubeAdm := vm.serverConfig.KubeAdm

	if err := vm.writeKubeletMergeConfig(c, maxPods); err != nil {
		return err
	}

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
		"--patches",
		"/etc/kubernetes/patches",
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

func (vm *AutoScalerServerNode) externalAgentJoin(c types.ClientGenerator, maxPods int) error {
	var result error

	external := vm.serverConfig.External
	config := map[string]interface{}{
		"provider-id":              vm.generateProviderID(),
		"max-pods":                 maxPods,
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
				result = fmt.Errorf(constantes.ErrCantCopyFileToNode, f.Name(), tmpConfigDestinationFile, err)
			} else {
				args := []string{
					fmt.Sprintf(mkdirCmd, path.Dir(external.ConfigPath)),
					fmt.Sprintf("cp %s %s", tmpConfigDestinationFile, external.ConfigPath),
					external.JoinCommand,
				}

				result = vm.executeCommands(args, false, c)
			}
		}
	}

	return result
}

func (vm *AutoScalerServerNode) rke2AgentJoin(c types.ClientGenerator, maxPods int) error {
	var result error

	rke2 := vm.serverConfig.RKE2
	service := "rke2-server"
	kubeletArgs := []string{"fail-swap-on=false", fmt.Sprintf("provider-id=%s", vm.generateProviderID()), fmt.Sprintf("max-pods=%d", maxPods)}

	if vm.serverConfig.UseControllerManager != nil && *vm.serverConfig.UseControllerManager {
		kubeletArgs = append(kubeletArgs, "cloud-provider=external")
	}

	config := map[string]interface{}{
		"kubelet-arg": kubeletArgs,
		"node-name":   vm.NodeName,
		"server":      fmt.Sprintf("https://%s", rke2.Address),
		"token":       rke2.Token,
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
				result = fmt.Errorf(constantes.ErrCantCopyFileToNode, f.Name(), tmpConfigDestinationFile, err)
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

func (vm *AutoScalerServerNode) k3sAgentJoin(c types.ClientGenerator, maxPods int) error {
	k3s := vm.serverConfig.K3S
	args := []string{
		fmt.Sprintf("echo K3S_ARGS='--kubelet-arg=provider-id=%s --kubelet-arg=max-pods=%d --node-name=%s --server=https://%s --token=%s' > /etc/systemd/system/k3s.service.env", vm.generateProviderID(), maxPods, vm.NodeName, k3s.Address, k3s.Token),
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

func (vm *AutoScalerServerNode) joinCluster(c types.ClientGenerator, maxPods int) error {
	glog.Infof("Register node in cluster for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroup)

	if vm.serverConfig.Distribution != nil {
		if *vm.serverConfig.Distribution == providers.K3SDistributionName {
			return vm.k3sAgentJoin(c, maxPods)
		} else if *vm.serverConfig.Distribution == providers.RKE2DistributionName {
			return vm.rke2AgentJoin(c, maxPods)
		} else if *vm.serverConfig.Distribution == providers.ExternalDistributionName {
			return vm.externalAgentJoin(c, maxPods)
		}
	}

	return vm.kubeAdmJoin(c, maxPods)
}

func (vm *AutoScalerServerNode) setNodeLabels(c types.ClientGenerator, nodeLabels, systemLabels types.KubernetesLabel) error {
	topology := vm.providerHandler.GetTopologyLabels()
	labels := types.MergeKubernetesLabel(nodeLabels, topology, systemLabels, vm.ExtraLabels)

	if err := c.LabelNode(vm.NodeName, labels); err != nil {
		return fmt.Errorf(constantes.ErrLabelNodeReturnError, vm.NodeName, err)
	}

	annotations := types.KubernetesLabel{
		constantes.AnnotationNodeGroupName:        vm.NodeGroup,
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

func (vm *AutoScalerServerNode) WaitForIP() (string, error) {
	glog.Infof("Wait IP ready for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroup)

	return vm.providerHandler.InstanceWaitReady(vm)
}

func (vm *AutoScalerServerNode) launchVM(c types.ClientGenerator, nodeLabels, systemLabels types.KubernetesLabel) error {
	glog.Debugf("AutoScalerNode::launchVM, node:%s", vm.InstanceName)

	var err error
	var status AutoScalerServerNodeState
	var maxPods int

	providerHandler := vm.providerHandler
	userInfo := vm.serverConfig.SSH
	glog.Infof("Launch VM:%s for nodegroup: %s", vm.InstanceName, vm.NodeGroup)

	if vm.State != AutoScalerServerNodeStateNotCreated {
		return fmt.Errorf(constantes.ErrVMAlreadyCreated, vm.InstanceName)
	}

	vm.State = AutoScalerServerNodeStateCreating

	desiredMachine := &providers.MachineCharacteristic{
		Memory: vm.Memory,
		Vcpu:   vm.CPU,
	}

	createInput := &providers.InstanceCreateInput{
		NodeGroup: vm.NodeGroup,
		DiskSize:  vm.DiskSize,
		UserName:  userInfo.GetUserName(),
		AuthKey:   userInfo.GetAuthKeys(),
		CloudInit: vm.serverConfig.CloudInit,
		Machine:   desiredMachine,
	}

	if vm.NodeType != AutoScalerServerNodeAutoscaled && vm.NodeType != AutoScalerServerNodeManaged {

		err = fmt.Errorf(constantes.ErrVMNotProvisionnedByMe, vm.InstanceName)

	} else if maxPods, err = providerHandler.InstanceMaxPods(vm.serverConfig.MaxPods); err != nil {

		err = fmt.Errorf(constantes.ErrUnableToRetrieveMaxPodsForInstanceType, vm.InstanceName, err)

	} else if vm.VMUUID, err = providerHandler.InstanceCreate(createInput); err != nil {

		err = fmt.Errorf(constantes.ErrUnableToLaunchVM, vm.InstanceName, err)

	} else if vm.VMUUID, err = providerHandler.InstanceID(); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if err = providerHandler.InstancePowerOn(); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if err = providerHandler.InstanceAutoStart(); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if _, err = providerHandler.InstanceWaitForToolsRunning(); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if vm.IPAddress, err = vm.WaitForIP(); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if err = providerHandler.RegisterDNS(vm.IPAddress); err != nil {

		err = fmt.Errorf(constantes.ErrRegisterDNSVMFailed, vm.InstanceName, err)

	} else if status, err = vm.statusVM(); err != nil {

		err = fmt.Errorf(constantes.ErrGetVMInfoFailed, vm.InstanceName, err)

	} else if status != AutoScalerServerNodeStateRunning {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if err = vm.recopyKubernetesPKIIfNeeded(); err != nil {

		err = fmt.Errorf(constantes.ErrRecopyKubernetesPKIFailed, vm.InstanceName, err)

	} else if err = vm.recopyEtcdSslFilesIfNeeded(); err != nil {

		err = fmt.Errorf(constantes.ErrUpdateEtcdSslFailed, vm.InstanceName, err)

	} else if err = vm.joinCluster(c, maxPods); err != nil {

		err = fmt.Errorf(constantes.ErrKubeAdmJoinFailed, vm.InstanceName, err)

	} else if err = vm.setProviderID(c); err != nil {

		err = fmt.Errorf(constantes.ErrProviderIDNotConfigured, vm.InstanceName, err)

	} else if err = vm.waitReady(c); err != nil {

		err = fmt.Errorf(constantes.ErrNodeIsNotReady, vm.InstanceName)

	} else if err = vm.retrieveNodeInfo(c); err != nil {

		err = fmt.Errorf(constantes.ErrNodeIsNotReady, vm.InstanceName)

	} else {
		err = vm.setNodeLabels(c, nodeLabels, systemLabels)
	}

	if err == nil {
		glog.Infof("Launched VM:%s nodename:%s for nodegroup: %s", vm.InstanceName, vm.NodeName, vm.NodeGroup)
	} else {
		glog.Errorf("Unable to launch VM:%s for nodegroup: %s. Reason: %v", vm.InstanceName, vm.NodeGroup, err.Error())
	}

	return err
}

func (vm *AutoScalerServerNode) startVM(c types.ClientGenerator) error {
	glog.Debugf("AutoScalerNode::startVM, node:%s", vm.InstanceName)

	var err error
	var state AutoScalerServerNodeState

	glog.Infof("Start VM:%s", vm.InstanceName)

	providerHandler := vm.providerHandler

	if vm.NodeType != AutoScalerServerNodeAutoscaled && vm.NodeType != AutoScalerServerNodeManaged {

		err = fmt.Errorf(constantes.ErrVMNotProvisionnedByMe, vm.InstanceName)

	} else if state, err = vm.statusVM(); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if state == AutoScalerServerNodeStateStopped {

		if err = providerHandler.InstancePowerOn(); err != nil {

			err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

		} else if _, err = providerHandler.InstanceWaitForToolsRunning(); err != nil {

			err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

		} else if vm.IPAddress, err = vm.WaitForIP(); err != nil {

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

	providerHandler := vm.providerHandler

	if vm.NodeType != AutoScalerServerNodeAutoscaled && vm.NodeType != AutoScalerServerNodeManaged {

		err = fmt.Errorf(constantes.ErrVMNotProvisionnedByMe, vm.InstanceName)

	} else if state, err = vm.statusVM(); err != nil {

		err = fmt.Errorf(constantes.ErrStopVMFailed, vm.InstanceName, err)

	} else if state == AutoScalerServerNodeStateRunning {

		if err = c.CordonNode(vm.NodeName); err != nil {
			glog.Errorf(constantes.ErrCordonNodeReturnError, vm.NodeName, err)
		}

		if err = providerHandler.InstancePowerOff(); err == nil {
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
	var status providers.InstanceStatus

	providerHandler := vm.providerHandler

	if vm.NodeType != AutoScalerServerNodeAutoscaled && vm.NodeType != AutoScalerServerNodeManaged {
		err = fmt.Errorf(constantes.ErrVMNotProvisionnedByMe, vm.InstanceName)
	} else if vm.State != AutoScalerServerNodeStateDeleting {
		if status, err = providerHandler.InstanceStatus(); err == nil {
			vm.State = AutoScalerServerNodeStateDeleting

			if err = providerHandler.UnregisterDNS(status.Address()); err != nil {
				glog.Warnf("unable to unregister DNS entry, reason: %v", err)
			}

			if status.Powered() {
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

				if err = providerHandler.InstancePowerOff(); err != nil {
					err = fmt.Errorf(constantes.ErrStopVMFailed, vm.InstanceName, err)
				} else {
					vm.State = AutoScalerServerNodeStateStopped

					if err = providerHandler.InstanceDelete(); err != nil {
						err = fmt.Errorf(constantes.ErrDeleteVMFailed, vm.InstanceName, err)
					}
				}
			} else if err = providerHandler.InstanceDelete(); err != nil {
				err = fmt.Errorf(constantes.ErrDeleteVMFailed, vm.InstanceName, err)
			}
		}
	} else {
		err = fmt.Errorf(constantes.ErrVMAlreadyDeleting, vm.InstanceName)
	}

	if err == nil {
		glog.Infof("Deleted VM:%s", vm.InstanceName)
		vm.State = AutoScalerServerNodeStateDeleted
	} else if !strings.HasPrefix(err.Error(), "InvalidInstanceID.NotFound: The instance ID") {
		glog.Errorf("Could not delete VM:%s. Reason: %s", vm.InstanceName, err)
	} else {
		glog.Warnf("Could not delete VM:%s. does not exist", vm.InstanceName)
		err = fmt.Errorf(constantes.ErrVMNotFound, vm.InstanceName)
	}

	return err
}

func (vm *AutoScalerServerNode) statusVM() (AutoScalerServerNodeState, error) {
	glog.Debugf("AutoScalerNode::statusVM, node:%s", vm.InstanceName)

	// Get VM infos
	var status providers.InstanceStatus
	var err error

	if status, err = vm.providerHandler.InstanceStatus(); err != nil {
		glog.Errorf(constantes.ErrGetVMInfoFailed, vm.InstanceName, err)
		return AutoScalerServerNodeStateUndefined, err
	}

	if status != nil {
		vm.IPAddress = status.Address()

		if status.Powered() {
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
		providerID := vm.generateProviderID()

		if len(providerID) > 0 {
			return c.SetProviderID(vm.NodeName, providerID)
		}
	}

	return nil
}

func (vm *AutoScalerServerNode) generateProviderID() string {
	if vm.serverConfig.UseControllerManager != nil && *vm.serverConfig.UseControllerManager {
		return vm.providerHandler.GenerateProviderID()
	}

	if vm.serverConfig.Distribution != nil {
		if *vm.serverConfig.Distribution == providers.K3SDistributionName {
			return fmt.Sprintf("k3s://%s", vm.NodeName)
		} else if *vm.serverConfig.Distribution == providers.RKE2DistributionName {
			return fmt.Sprintf("rke2://%s", vm.NodeName)
		}
	}

	return ""
}

func (vm *AutoScalerServerNode) findInstanceUUID() string {
	if vmUUID, err := vm.providerHandler.InstanceID(); err == nil {
		vm.VMUUID = vmUUID

		return vmUUID
	}

	return ""
}

func (vm *AutoScalerServerNode) setServerConfiguration(config *types.AutoScalerServerConfig) error {
	var err error

	if vm.providerHandler, err = config.GetCloudConfiguration().AttachInstance(vm.InstanceName, vm.NodeIndex); err == nil {
		vm.serverConfig = config
		vm.providerHandler.UpdateMacAddressTable()
	}

	return err
}

func (vm *AutoScalerServerNode) retrieveNetworkInfos() error {
	return vm.providerHandler.RetrieveNetworkInfos()
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
				glog.Errorf(constantes.ErrUnableToDeleteVM, vm.InstanceName, e)
			}
		} else {
			glog.Warningf(constantes.WarnFailedVMNotDeleted, vm.InstanceName, status)
		}
	}

}
