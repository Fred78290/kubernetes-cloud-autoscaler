package server

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
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
	joinClusterInfo          = "Join cluster for node: %s for nodegroup: %s"
	unableToExecuteCmdError  = "unable to execute command: %s, output: %s, reason: %v"
	nodeNameTemplate         = "%s-%s-%02d"
	errWriteFileErrorMsg     = "unable to write file: %s, reason: %v"
	tmpConfigDestinationFile = "/tmp/config.yaml"
	mkdirCmd                 = "mkdir -p %s"
	scpFailed                = "scp failed: %v"
	mkdirFailed              = "mkdir failed: %v"
	moveFailed               = "mv failed: %v"
	chownFailed              = "chown failed: %v"
	copyFiles                = "cp -r /tmp/%s/* %s"
	changeOwner              = "chown -R %s %s"
)

// AutoScalerServerNode Describe a AutoScaler VM
// Node name and instance name could be differ when using AWS cloud provider
type AutoScalerServerNode struct {
	NodeGroup        string                    `json:"group"`
	NodeName         string                    `json:"node-name"`
	NodeIndex        int                       `json:"index"`
	InstanceName     string                    `json:"instance-name"`
	VMUUID           string                    `json:"vm-uuid"`
	CRDUID           uid.UID                   `json:"crd-uid"`
	Memory           int                       `json:"memory"`
	CPU              int                       `json:"cpu"`
	DiskSize         int                       `json:"diskSize"`
	IPAddress        string                    `json:"address"`
	State            AutoScalerServerNodeState `json:"state"`
	NodeType         AutoScalerServerNodeType  `json:"type"`
	ControlPlaneNode bool                      `json:"control-plane,omitempty"`
	AllowDeployment  bool                      `json:"allow-deployment,omitempty"`
	ExtraLabels      types.KubernetesLabel     `json:"labels,omitempty"`
	ExtraAnnotations types.KubernetesLabel     `json:"annotations,omitempty"`
	CloudInit        cloudinit.CloudInit       `json:"cloud-init,omitempty"`
	MaxPods          int                       `json:"max-pods,omitempty"`
	providerHandler  providers.ProviderHandler
	serverConfig     *types.AutoScalerServerConfig
}

func (s AutoScalerServerNodeState) String() string {
	return autoScalerServerNodeStateString[s]
}

func (vm *AutoScalerServerNode) waitReady(c types.ClientGenerator) error {

	if vm.serverConfig.UseCloudInitToConfigure() {
		glog.Infof("Wait node: %s joined the cluster", vm.NodeName)

		if err := context.PollImmediate(5*time.Second, time.Duration(vm.serverConfig.SSH.WaitSshReadyInSeconds)*time.Second, func() (done bool, err error) {
			if nodeInfo, err := c.GetNode(vm.NodeName); err == nil && nodeInfo != nil {
				vm.CPU = int(nodeInfo.Status.Capacity.Cpu().Value())
				vm.Memory = int(nodeInfo.Status.Capacity.Memory().Value() / (1024 * 1024))
				vm.DiskSize = int(nodeInfo.Status.Capacity.Storage().Value() / (1024 * 1024))

				for _, address := range nodeInfo.Status.Addresses {
					if address.Type == apiv1.NodeInternalIP {
						vm.State = AutoScalerServerNodeStateRunning
						vm.IPAddress = address.Address
						break
					}
				}

				return true, nil
			}

			return false, nil
		}); err != nil {
			return fmt.Errorf(constantes.ErrNodeIsNotReady, vm.NodeName)
		}
	}

	glog.Infof("Wait node: %s to be ready", vm.NodeName)

	return c.WaitNodeToBeReady(vm.NodeName)
}

func (vm *AutoScalerServerNode) recopyDirectory(srcDir, dstDir string) (err error) {
	config := vm.serverConfig
	timeout := vm.providerHandler.GetTimeout()
	dirName := filepath.Base(srcDir)

	if err = utils.Scp(config.SSH, vm.IPAddress, srcDir, "/tmp"); err != nil {
		glog.Errorf(scpFailed, err)
	} else if _, err = utils.Sudo(config.SSH, vm.IPAddress, timeout, fmt.Sprintf(mkdirCmd, dstDir)); err != nil {
		glog.Errorf(mkdirFailed, err)
	} else if _, err = utils.Sudo(config.SSH, vm.IPAddress, timeout, fmt.Sprintf(copyFiles, dirName, dstDir)); err != nil {
		glog.Errorf(moveFailed, err)
	} else if _, err = utils.Sudo(config.SSH, vm.IPAddress, timeout, fmt.Sprintf(changeOwner, *config.CloudInitFileOwner, dstDir)); err != nil {
		glog.Errorf(chownFailed, err)
	}

	return err
}

func (vm *AutoScalerServerNode) recopyEtcdSslFilesIfNeeded() (err error) {
	config := vm.serverConfig

	if !vm.serverConfig.UseCloudInitToConfigure() {
		if config.KubernetesDistribution() != providers.RKE2DistributionName && vm.ControlPlaneNode && config.UseExternalEtdcServer() {
			glog.Infof("Recopy Etcd ssl files for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroup)

			err = vm.recopyDirectory(config.ExtSourceEtcdSslDir, config.ExtDestinationEtcdSslDir)
		}
	}

	return err
}

func (vm *AutoScalerServerNode) recopyKubernetesPKIIfNeeded() (err error) {
	config := vm.serverConfig

	if !config.UseCloudInitToConfigure() {
		if config.KubernetesDistribution() == providers.KubeAdmDistributionName && vm.ControlPlaneNode {
			glog.Infof("Recopy PKI for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroup)

			err = vm.recopyDirectory(config.KubernetesPKISourceDir, config.KubernetesPKIDestDir)
		}
	}

	return err
}

func (vm *AutoScalerServerNode) executeCommands(args []string, restartKubelet bool, c types.ClientGenerator) error {
	config := vm.serverConfig
	command := fmt.Sprintf("sh -c \"%s\"", strings.Join(args, " && "))

	timeout := vm.providerHandler.GetTimeout()

	if out, err := utils.Sudo(config.SSH, vm.IPAddress, timeout, command); err != nil {
		return fmt.Errorf(unableToExecuteCmdError, command, out, err)
	} else if c != nil {

		if restartKubelet {
			// To be sure, with kubeadm 1.26.1, the kubelet is not correctly restarted
			time.Sleep(5 * time.Second)
		}

		return context.PollImmediate(5*time.Second, time.Duration(config.SSH.WaitSshReadyInSeconds)*time.Second, func() (done bool, err error) {
			if node, err := c.GetNode(vm.NodeName); err == nil && node != nil {
				return true, nil
			}

			if restartKubelet {
				glog.Infof("Restart kubelet for node: %s for nodegroup: %s", vm.NodeName, vm.NodeGroup)

				if out, err := utils.Sudo(config.SSH, vm.IPAddress, timeout, "systemctl restart kubelet"); err != nil {
					return false, fmt.Errorf("unable to restart kubelet, output: %s, reason: %v", out, err)
				}
			}

			return false, nil
		})
	}

	return nil
}

func (vm *AutoScalerServerNode) joinClusterWithConfig(content any, destinationFile string, c types.ClientGenerator, restartKubelet bool, extraCommand ...string) error {
	var result error

	if f, err := os.CreateTemp(os.TempDir(), "config.*.yaml"); err != nil {
		result = fmt.Errorf("unable to create %s, reason: %v", destinationFile, err)
	} else {
		defer os.Remove(f.Name())

		content := utils.ToYAML(content)

		if glog.GetLevel() >= glog.DebugLevel {
			fmt.Fprintf(os.Stderr, "\n%s:\n%s\n", destinationFile, content)
		}

		if _, err = f.WriteString(content); err != nil {
			f.Close()
			result = fmt.Errorf(errWriteFileErrorMsg, f.Name(), err)
		} else {
			f.Close()

			if err = utils.Scp(vm.serverConfig.SSH, vm.IPAddress, f.Name(), tmpConfigDestinationFile); err != nil {
				result = fmt.Errorf(constantes.ErrCantCopyFileToNode, f.Name(), tmpConfigDestinationFile, err)
			} else {
				args := []string{
					fmt.Sprintf(mkdirCmd, path.Dir(destinationFile)),
					fmt.Sprintf("cp %s %s", tmpConfigDestinationFile, destinationFile),
					fmt.Sprintf("rm %s", tmpConfigDestinationFile),
				}

				if len(extraCommand) > 0 {
					args = append(args, extraCommand...)
				}

				result = vm.executeCommands(args, false, c)
			}
		}
	}

	return result
}

func (vm *AutoScalerServerNode) putFile(content any, destinationFile string) error {
	return vm.joinClusterWithConfig(content, destinationFile, nil, false)
}

func (vm *AutoScalerServerNode) kubeAdmConfig() any {
	if vm.serverConfig.UseCloudInitToConfigure() {
		return map[string]any{
			"address": vm.IPAddress,
			"maxPods": vm.MaxPods,
		}
	} else {
		return map[string]any{
			"address":    vm.IPAddress,
			"providerID": vm.generateProviderID(),
			"maxPods":    vm.MaxPods,
		}
	}
}

func (vm *AutoScalerServerNode) kubeAdmJoinCommand() []string {
	config := vm.serverConfig
	commands := make([]string, 0, 2)
	kubeAdm := vm.serverConfig.KubeAdm

	if config.UseImageCredentialProviderConfig() {
		commands = append(commands, fmt.Sprintf("echo KUBELET_EXTRA_ARGS='--image-credential-provider-config=%s --image-credential-provider-bin-dir=%s' > /etc/default/kubelet", *config.ImageCredentialProviderConfig, *config.ImageCredentialProviderBinDir))
	}

	join := []string{
		"kubeadm",
		"join",
		kubeAdm.Address,
		"--node-name",
		vm.NodeName,
		"--token",
		kubeAdm.Token,
		"--discovery-token-ca-cert-hash",
		kubeAdm.CACert,
		"--patches",
		"/etc/kubernetes/patches",
	}

	if vm.ControlPlaneNode {
		join = append(join, "--control-plane")

		if len(vm.IPAddress) > 0 {
			join = append(join, "--apiserver-advertise-address", vm.IPAddress)
		}
	}

	// Append extras arguments
	if len(kubeAdm.ExtraArguments) > 0 {
		join = append(join, kubeAdm.ExtraArguments...)
	}

	return append(commands, strings.Join(join, " "))
}

func (vm *AutoScalerServerNode) kubeAdmJoin(c types.ClientGenerator) error {
	return vm.joinClusterWithConfig(vm.kubeAdmConfig(), "/etc/kubernetes/patches/kubeletconfiguration0+merge.yaml", c, true, vm.kubeAdmJoinCommand()...)
}

func (vm *AutoScalerServerNode) externalAgentConfig() any {
	config := vm.serverConfig
	external := config.External
	externalConfig := map[string]any{
		"max-pods":  vm.MaxPods,
		"node-name": vm.NodeName,
		"server":    external.Address,
		"token":     external.Token,
	}

	if config.UseControllerManager() && !config.UseCloudInitToConfigure() {
		externalConfig["provider-id"] = vm.generateProviderID()
	}

	if external.ExtraConfig != nil {
		for k, v := range external.ExtraConfig {
			externalConfig[k] = v
		}
	}

	if vm.ControlPlaneNode {
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

func (vm *AutoScalerServerNode) externalAgentJoin(c types.ClientGenerator) error {
	return vm.joinClusterWithConfig(vm.externalAgentConfig(), vm.serverConfig.External.ConfigPath, c, false, vm.serverConfig.External.JoinCommand)
}

func (vm *AutoScalerServerNode) rke2AgentConfig() any {
	config := vm.serverConfig
	rke2 := config.RKE2
	kubeletArgs := []string{
		"fail-swap-on=false",
		fmt.Sprintf("max-pods=%d", vm.MaxPods),
	}

	if config.UseControllerManager() && !config.UseCloudInitToConfigure() {
		kubeletArgs = append(kubeletArgs, fmt.Sprintf("provider-id=%s", vm.generateProviderID()))
	}

	if config.CloudProvider != nil && len(*config.CloudProvider) > 0 {
		kubeletArgs = append(kubeletArgs, fmt.Sprintf("cloud-provider=%s", *config.CloudProvider))
	}

	rke2Config := map[string]any{
		"kubelet-arg": kubeletArgs,
		"node-name":   vm.NodeName,
		"server":      fmt.Sprintf("https://%s", rke2.Address),
		"token":       rke2.Token,
	}

	if len(vm.IPAddress) > 0 {
		rke2Config["advertise-address"] = vm.IPAddress
	}

	if vm.ControlPlaneNode {
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

func (vm *AutoScalerServerNode) rke2AgentJoin(c types.ClientGenerator) error {
	var service string

	if vm.ControlPlaneNode {
		service = "rke2-server"
	} else {
		service = "rke2-agent"
	}

	return vm.joinClusterWithConfig(vm.rke2AgentConfig(), "/etc/rancher/rke2/config.yaml", c, false, fmt.Sprintf("systemctl enable %s.service", service), fmt.Sprintf("systemctl start %s.service", service))
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

func (vm *AutoScalerServerNode) k3sAgentCommand() []string {
	config := vm.serverConfig
	k3s := config.K3S
	command := make([]string, 0, 5)

	if config.UseControllerManager() && !config.UseCloudInitToConfigure() {
		command = append(command, fmt.Sprintf("echo K3S_ARGS='--kubelet-arg=max-pods=%d --node-name=%s --server=https://%s --token=%s --kubelet-arg=provider-id=%s ' > /etc/systemd/system/k3s.service.env", vm.MaxPods, vm.NodeName, k3s.Address, k3s.Token, vm.generateProviderID()))
	} else {
		command = append(command, fmt.Sprintf("echo K3S_ARGS='--kubelet-arg=max-pods=%d --node-name=%s --server=https://%s --token=%s' > /etc/systemd/system/k3s.service.env", vm.MaxPods, vm.NodeName, k3s.Address, k3s.Token))
	}

	if vm.ControlPlaneNode {
		command = append(command, "echo 'K3S_MODE=server' > /etc/default/k3s")

		if config.DisableCloudController() {
			command = append(command, "echo K3S_DISABLE_ARGS='--disable-cloud-controller --disable=servicelb --disable=traefik --disable=metrics-server' > /etc/systemd/system/k3s.disabled.env")
		} else {
			command = append(command, "echo K3S_DISABLE_ARGS='--disable=servicelb --disable=traefik --disable=metrics-server' > /etc/systemd/system/k3s.disabled.env")
		}

		if config.UseExternalEtdcServer() {
			command = append(command, fmt.Sprintf("echo K3S_SERVER_ARGS='--datastore-endpoint=%s --datastore-cafile=%s/ca.pem --datastore-certfile=%s/etcd.pem --datastore-keyfile=%s/etcd-key.pem' > /etc/systemd/system/k3s.server.env", k3s.DatastoreEndpoint, config.ExtDestinationEtcdSslDir, config.ExtDestinationEtcdSslDir, config.ExtDestinationEtcdSslDir))
		}
	} else {
		command = append(command, "echo 'K3S_MODE=agent' > /etc/default/k3s")
	}

	// Append extras arguments
	if len(k3s.ExtraCommands) > 0 {
		command = append(command, k3s.ExtraCommands...)
	}

	return append(command, "systemctl enable k3s.service", "systemctl start k3s.service")
}

func (vm *AutoScalerServerNode) k3sAgentJoin(c types.ClientGenerator) error {
	return vm.executeCommands(vm.k3sAgentCommand(), false, c)
}

func (vm *AutoScalerServerNode) joinCluster(c types.ClientGenerator) (err error) {
	glog.Infof("Register node in cluster for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroup)

	config := vm.serverConfig

	if config.UseImageCredentialProviderConfig() {
		err = vm.putFile(config.CredentialProviderConfig, *config.ImageCredentialProviderConfig)
	}

	if err == nil {
		glog.Infof(joinClusterInfo, vm.NodeName, vm.NodeGroup)

		switch config.KubernetesDistribution() {
		case providers.K3SDistributionName:
			err = vm.k3sAgentJoin(c)
		case providers.RKE2DistributionName:
			err = vm.rke2AgentJoin(c)
		case providers.ExternalDistributionName:
			err = vm.externalAgentJoin(c)
		default:
			err = vm.kubeAdmJoin(c)
		}
	}

	return err
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
	config := vm.serverConfig

	// Node name and instance name could be differ when using AWS cloud provider
	//	if nodeName, err := vm.providerHandler.PrivateDNSName(); err == nil {
	//		vm.NodeName = nodeName
	//
	//		glog.Debugf("Launch VM: %s set to nodeName: %s", nodename, nodeName)
	//	} else {
	//		return err
	//	}

	// We never use ssh in cloud-init mode
	if config.UseCloudInitToConfigure() {
		return nil
	}

	return context.PollImmediate(time.Second, time.Duration(config.SSH.WaitSshReadyInSeconds)*time.Second, func() (bool, error) {
		// Set hostname
		if _, err := utils.Sudo(config.SSH, address, time.Second, fmt.Sprintf("hostnamectl set-hostname %s", nodename)); err != nil {
			if strings.HasSuffix(err.Error(), "connection refused") || strings.HasSuffix(err.Error(), "i/o timeout") {
				return false, nil
			}

			return false, err
		}

		return true, nil
	})
}

func (vm *AutoScalerServerNode) WaitForIP() (string, error) {
	glog.Infof("Wait IP ready for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroup)

	return vm.providerHandler.InstanceWaitReady(vm)
}

func (vm *AutoScalerServerNode) appendEtcdSslFilesIfNeededInCloudInit() error {
	config := vm.serverConfig

	if config.KubernetesDistribution() != providers.RKE2DistributionName && (vm.ControlPlaneNode && config.UseExternalEtdcServer()) {
		glog.Infof("Put in cloud-init Etcd ssl files for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroup)

		return vm.CloudInit.AddDirectoryToWriteFile(config.ExtSourceEtcdSslDir, config.ExtDestinationEtcdSslDir, *config.CloudInitFileOwner)
	}

	return nil
}

func (vm *AutoScalerServerNode) appendKubernetesPKIIfNeededInCloudInit() error {
	config := vm.serverConfig

	if (config.KubernetesDistribution() != providers.RKE2DistributionName) && vm.ControlPlaneNode {
		glog.Infof("Put in cloud-init PKI for instance: %s in node group: %s", vm.InstanceName, vm.NodeGroup)

		return vm.CloudInit.AddDirectoryToWriteFile(config.KubernetesPKISourceDir, config.KubernetesPKIDestDir, *config.CloudInitFileOwner)
	}

	return nil
}

func (vm *AutoScalerServerNode) appendKubeAdmConfigInCloudInit() (err error) {
	config := vm.serverConfig

	if err = vm.CloudInit.AddObjectToWriteFile(vm.kubeAdmConfig(), "/etc/kubernetes/patches/kubeletconfiguration0+merge.yaml", *config.CloudInitFileOwner, *config.CloudInitFileMode); err == nil {
		vm.CloudInit.AddRunCommand(vm.kubeAdmJoinCommand()...)
	}

	return err
}

func (vm *AutoScalerServerNode) appendExternalAgentConfigInCloudInit() (err error) {
	config := vm.serverConfig

	if err = vm.CloudInit.AddObjectToWriteFile(vm.externalAgentConfig(), vm.serverConfig.External.ConfigPath, *config.CloudInitFileOwner, *config.CloudInitFileMode); err == nil {
		vm.CloudInit.AddRunCommand(vm.serverConfig.External.JoinCommand)
	}

	return err
}

func (vm *AutoScalerServerNode) appendRKE22AgentConfigInCloudInit() (err error) {
	config := vm.serverConfig

	if err = vm.CloudInit.AddObjectToWriteFile(vm.rke2AgentConfig(), "/etc/rancher/rke2/config.yaml", *config.CloudInitFileOwner, *config.CloudInitFileMode); err == nil {
		var service string

		if vm.ControlPlaneNode {
			service = "rke2-server"
		} else {
			service = "rke2-agent"
		}

		vm.CloudInit.AddRunCommand(fmt.Sprintf("systemctl enable %s.service", service), fmt.Sprintf("systemctl start %s.service", service))
	}

	return err
}

func (vm *AutoScalerServerNode) appendK3SAgentConfigInCloudInit() error {
	vm.CloudInit.AddRunCommand(vm.k3sAgentCommand()...)

	return nil
}

func (vm *AutoScalerServerNode) prepareCloudInit() (err error) {
	config := vm.serverConfig

	if vm.MaxPods, err = vm.providerHandler.InstanceMaxPods(int(*config.MaxPods)); err != nil {
		return fmt.Errorf(constantes.ErrUnableToRetrieveMaxPodsForInstanceType, vm.InstanceName, err)
	}

	if vm.serverConfig.CloudInit == nil {
		vm.CloudInit = cloudinit.CloudInit{
			"package_update":  false,
			"package_upgrade": false,
		}
	} else if vm.CloudInit, err = vm.serverConfig.CloudInit.Clone(); err != nil {
		return err
	}

	if config.UseCloudInitToConfigure() {
		if err = vm.appendEtcdSslFilesIfNeededInCloudInit(); err != nil {
			return err
		}

		if err = vm.appendKubernetesPKIIfNeededInCloudInit(); err != nil {
			return err
		}

		if config.UseImageCredentialProviderConfig() {
			if err = vm.CloudInit.AddObjectToWriteFile(config.CredentialProviderConfig, *config.ImageCredentialProviderConfig, *config.CloudInitFileOwner, *config.CloudInitFileMode); err != nil {
				return err
			}
		}

		switch config.KubernetesDistribution() {
		case providers.K3SDistributionName:
			return vm.appendK3SAgentConfigInCloudInit()
		case providers.RKE2DistributionName:
			return vm.appendRKE22AgentConfigInCloudInit()
		case providers.ExternalDistributionName:
			return vm.appendExternalAgentConfigInCloudInit()
		default:
			return vm.appendKubeAdmConfigInCloudInit()
		}
	}

	return err
}

func (vm *AutoScalerServerNode) createInstance(c types.ClientGenerator) (err error) {
	providerHandler := vm.providerHandler
	userInfo := vm.serverConfig.SSH

	if err = vm.prepareCloudInit(); err != nil {
		return fmt.Errorf("prepare cloud-init failed: %v", err)
	}

	createInput := &providers.InstanceCreateInput{
		NodeGroup: vm.NodeGroup,
		UserName:  userInfo.GetUserName(),
		AuthKey:   userInfo.GetAuthKeys(),
		CloudInit: vm.CloudInit,
		Machine: &providers.MachineCharacteristic{
			Memory:   vm.Memory,
			Vcpu:     vm.CPU,
			DiskSize: vm.DiskSize,
		},
	}

	vm.State = AutoScalerServerNodeStateCreating

	if vm.NodeType != AutoScalerServerNodeAutoscaled && vm.NodeType != AutoScalerServerNodeManaged {

		err = fmt.Errorf(constantes.ErrVMNotProvisionnedByMe, vm.InstanceName)

	} else if vm.VMUUID, err = providerHandler.InstanceCreate(createInput); err != nil {

		err = fmt.Errorf(constantes.ErrUnableToLaunchVM, vm.InstanceName, err)

	} else if vm.VMUUID, err = providerHandler.InstanceID(); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if err = providerHandler.InstancePowerOn(); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	} else if err = providerHandler.InstanceAutoStart(); err != nil {

		err = fmt.Errorf(constantes.ErrStartVMFailed, vm.InstanceName, err)

	}

	return err
}

func (vm *AutoScalerServerNode) postCloudInitConfiguration(c types.ClientGenerator) (err error) {
	providerHandler := vm.providerHandler

	if err = vm.waitReady(c); err != nil {

		err = fmt.Errorf(constantes.ErrNodeJoinClusterFailed, vm.NodeName, err)

	} else if vm.State != AutoScalerServerNodeStateRunning {

		err = fmt.Errorf(constantes.ErrNodeInternalIPNotFound, vm.NodeName)

	} else if err = vm.setProviderID(c); err != nil {

		err = fmt.Errorf(constantes.ErrProviderIDNotConfigured, vm.InstanceName, err)

	} else if err = providerHandler.RegisterDNS(vm.IPAddress); err != nil {

		err = fmt.Errorf(constantes.ErrRegisterDNSVMFailed, vm.InstanceName, err)

	}

	return err
}

func (vm *AutoScalerServerNode) postSshConfiguration(c types.ClientGenerator) (err error) {
	var status AutoScalerServerNodeState

	providerHandler := vm.providerHandler

	if _, err = providerHandler.InstanceWaitForToolsRunning(); err != nil {

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

	} else if err = vm.joinCluster(c); err != nil {

		err = fmt.Errorf(constantes.ErrNodeJoinClusterFailed, vm.InstanceName, err)

	} else if err = vm.waitReady(c); err != nil {

		err = fmt.Errorf(constantes.ErrNodeIsNotReady, vm.InstanceName)

	} else if err = vm.retrieveNodeInfo(c); err != nil {

		err = fmt.Errorf(constantes.ErrNodeIsNotReady, vm.InstanceName)

	} else if err = vm.setProviderID(c); err != nil {

		err = fmt.Errorf(constantes.ErrProviderIDNotConfigured, vm.InstanceName, err)
	}

	return err
}

func (vm *AutoScalerServerNode) launchVM(c types.ClientGenerator, nodeLabels, systemLabels types.KubernetesLabel) (err error) {
	glog.Infof("Launch VM: %s for nodegroup: %s", vm.InstanceName, vm.NodeGroup)

	if vm.State != AutoScalerServerNodeStateNotCreated {
		return fmt.Errorf(constantes.ErrVMAlreadyCreated, vm.InstanceName)
	}

	if err = vm.createInstance(c); err == nil {
		if vm.serverConfig.UseCloudInitToConfigure() {
			err = vm.postCloudInitConfiguration(c)
		} else {
			err = vm.postSshConfiguration(c)
		}

		if err == nil {
			err = vm.setNodeLabels(c, nodeLabels, systemLabels)
		}

	}

	if err == nil {
		glog.Infof("Launched VM: %s nodename: %s for nodegroup: %s", vm.InstanceName, vm.NodeName, vm.NodeGroup)
	} else {
		glog.Errorf("Unable to launch VM: %s for nodegroup: %s. Reason: %v", vm.InstanceName, vm.NodeGroup, err.Error())
	}

	return err
}

func (vm *AutoScalerServerNode) startVM(c types.ClientGenerator) error {
	glog.Infof("Start VM: %s", vm.InstanceName)

	var err error
	var state AutoScalerServerNodeState

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
		glog.Infof("Started VM: %s", vm.InstanceName)
	} else {
		glog.Errorf("Unable to start VM: %s. Reason: %v", vm.InstanceName, err)
	}

	return err
}

func (vm *AutoScalerServerNode) stopVM(c types.ClientGenerator) error {
	glog.Infof("Stop VM: %s", vm.InstanceName)

	var err error
	var state AutoScalerServerNodeState

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
		glog.Infof("Stopped VM: %s", vm.InstanceName)
	} else {
		glog.Errorf("Could not stop VM: %s. Reason: %s", vm.InstanceName, err)
	}

	return err
}

func (vm *AutoScalerServerNode) prepareNodeDeletion(c types.ClientGenerator) error {
	config := vm.serverConfig

	switch config.KubernetesDistribution() {
	case providers.K3SDistributionName:
		return c.DeleteSecret(fmt.Sprintf("%s.node-password.k3s", vm.NodeName), "kube-system")

	case providers.RKE2DistributionName:
		return c.DeleteSecret(fmt.Sprintf("%s.node-password.rke2", vm.NodeName), "kube-system")

	case providers.ExternalDistributionName:
		if len(config.External.DeleteCommand) > 0 {
			return vm.executeCommands([]string{config.External.DeleteCommand}, false, c)
		}
	}

	return nil
}

func (vm *AutoScalerServerNode) deleteVM(c types.ClientGenerator) error {
	glog.Infof("Delete VM: %s", vm.InstanceName)

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
					if err = vm.prepareNodeDeletion(c); err != nil {
						glog.Errorf(constantes.ErrPrepareNodeDeletionFailed, vm.NodeName, err)
					}

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
		glog.Infof("Deleted VM: %s", vm.InstanceName)
		vm.State = AutoScalerServerNodeStateDeleted
	} else if !strings.HasPrefix(err.Error(), "InvalidInstanceID.NotFound: The instance ID") {
		glog.Errorf("Could not delete VM: %s. Reason: %s", vm.InstanceName, err)
	} else {
		glog.Warnf("Could not delete VM: %s. does not exist", vm.InstanceName)
		err = fmt.Errorf(constantes.ErrVMNotFound, vm.InstanceName)
	}

	return err
}

func (vm *AutoScalerServerNode) statusVM() (AutoScalerServerNodeState, error) {
	glog.Debugf("AutoScalerNode::statusVM, node: %s", vm.InstanceName)

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
	// provider is set by config in ssh config mode
	if vm.serverConfig.DisableCloudController() && vm.serverConfig.UseCloudInitToConfigure() {
		providerID := vm.providerHandler.GenerateProviderID()

		// Well ignore error, the controller can set it earlier
		if len(providerID) > 0 {
			c.SetProviderID(vm.NodeName, providerID)
		}
	}

	return nil
}

func (vm *AutoScalerServerNode) generateProviderID() string {
	if vm.serverConfig.DisableCloudController() {
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

	if vm.providerHandler, err = config.GetCloudConfiguration().AttachInstance(vm.InstanceName, vm.ControlPlaneNode, vm.NodeIndex); err == nil {
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
