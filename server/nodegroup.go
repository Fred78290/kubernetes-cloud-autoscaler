package server

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/client"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	apigrpc "github.com/Fred78290/kubernetes-cloud-autoscaler/grpc"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/kubernetes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha2"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	glog "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	uid "k8s.io/apimachinery/pkg/types"
)

// NodeGroupState describe the nodegroup status
type NodeGroupState int32

const (
	// NodegroupNotCreated not created state
	NodegroupNotCreated = iota

	// NodegroupCreating creating state
	NodegroupCreating

	// NodegroupCreated create state
	NodegroupCreated

	// NodegroupDeleting deleting status
	NodegroupDeleting

	// NodegroupDeleted deleted status
	NodegroupDeleted
)

type ServerNodeState int

const (
	ServerNodeStateNotRunning = iota
	ServerNodeStateDeleted
	ServerNodeStateCreating
	ServerNodeStateRunning
	ServerNodeStateDeleting
)

// AutoScalerServerNodeGroup Group all AutoScaler VM created inside a NodeGroup
// Each node have name like <node group name>-vm-<vm index>
type AutoScalerServerNodeGroup struct {
	sync.Mutex
	NodeGroupIdentifier        string                           `json:"identifier"`
	ServiceIdentifier          string                           `json:"service"`
	ProvisionnedNodeNamePrefix string                           `default:"autoscaled" json:"node-name-prefix"`
	ManagedNodeNamePrefix      string                           `default:"worker" json:"managed-name-prefix"`
	ControlPlaneNamePrefix     string                           `default:"master" json:"controlplane-name-prefix"`
	InstanceType               string                           `json:"instance-type"`
	Status                     NodeGroupState                   `json:"status"`
	MinNodeSize                int                              `json:"minSize"`
	MaxNodeSize                int                              `json:"maxSize"`
	Nodes                      map[string]*AutoScalerServerNode `json:"nodes"`
	NodeLabels                 types.KubernetesLabel            `json:"nodeLabels"`
	SystemLabels               types.KubernetesLabel            `json:"systemLabels"`
	AutoProvision              bool                             `json:"auto-provision"`
	LastCreatedNodeIndex       int                              `json:"node-index"`
	RunningNodes               map[int]ServerNodeState          `json:"running-nodes-state"`
	pendingNodes               map[string]*AutoScalerServerNode
	pendingNodesWG             sync.WaitGroup
	numOfControlPlanes         int
	numOfExternalNodes         int
	numOfProvisionnedNodes     int
	numOfManagedNodes          int
	configuration              *types.AutoScalerServerConfig
	machines                   providers.MachineCharacteristics
}

func NewAutoScalerServerNodeGroup(s *nodegroupCreateInput) (nodeGroup *AutoScalerServerNodeGroup) {
	return &AutoScalerServerNodeGroup{
		ServiceIdentifier:          s.configuration.ServiceIdentifier,
		ProvisionnedNodeNamePrefix: s.configuration.ProvisionnedNodeNamePrefix,
		ManagedNodeNamePrefix:      s.configuration.ManagedNodeNamePrefix,
		ControlPlaneNamePrefix:     s.configuration.ControlPlaneNamePrefix,
		NodeGroupIdentifier:        s.nodeGroupID,
		InstanceType:               s.machineType,
		Status:                     NodegroupNotCreated,
		pendingNodes:               make(map[string]*AutoScalerServerNode),
		Nodes:                      make(map[string]*AutoScalerServerNode),
		MinNodeSize:                s.minNodeSize,
		MaxNodeSize:                s.maxNodeSize,
		NodeLabels:                 s.labels,
		SystemLabels:               s.systemLabels,
		AutoProvision:              s.autoProvision,
		configuration:              s.configuration,
		machines:                   s.machines,
	}
}

func CreateLabelOrAnnotation(values []string) types.KubernetesLabel {
	result := types.KubernetesLabel{}

	for _, value := range values {
		if len(value) > 0 {
			parts := strings.Split(value, "=")

			if len(parts) == 1 {
				result[parts[0]] = ""
			} else {
				result[parts[0]] = parts[1]
			}
		}
	}

	return result
}

func (g *AutoScalerServerNodeGroup) Machine() *providers.MachineCharacteristic {
	return g.machines[g.InstanceType]
}

func (g *AutoScalerServerNodeGroup) findNextNodeIndex() int {

	for index := 0; index < g.MaxNodeSize; index++ {
		if run, found := g.RunningNodes[index]; !found || run < ServerNodeStateCreating {
			return index
		}
	}

	g.LastCreatedNodeIndex++

	return g.LastCreatedNodeIndex
}

func (g *AutoScalerServerNodeGroup) cleanup(c client.ClientGenerator) error {
	glog.Debugf("AutoScalerServerNodeGroup::cleanup, nodeGroupID: %s", g.NodeGroupIdentifier)

	var lastError error

	g.Status = NodegroupDeleting

	g.pendingNodesWG.Wait()

	glog.Debugf("AutoScalerServerNodeGroup::cleanup, nodeGroupID: %s, iterate node to delete", g.NodeGroupIdentifier)

	for _, node := range g.AllNodes() {
		if node.NodeType == AutoScalerServerNodeAutoscaled {
			if lastError = node.deleteVM(c); lastError != nil {
				glog.Errorf(constantes.ErrNodeGroupCleanupFailOnVM, g.NodeGroupIdentifier, node.InstanceName, lastError)
				// Not fatal on cleanup
				if lastError.Error() == fmt.Sprintf(constantes.ErrVMNotFound, node.InstanceName) {
					lastError = nil
				}
			}
		}
	}

	g.RunningNodes = make(map[int]ServerNodeState)
	g.Nodes = make(map[string]*AutoScalerServerNode)
	g.pendingNodes = make(map[string]*AutoScalerServerNode)
	g.numOfControlPlanes = 0
	g.numOfExternalNodes = 0
	g.numOfManagedNodes = 0
	g.numOfProvisionnedNodes = 0
	g.Status = NodegroupDeleted

	return lastError
}

func (g *AutoScalerServerNodeGroup) targetSize() int {
	glog.Debugf("AutoScalerServerNodeGroup::targetSize, nodeGroupID: %s", g.NodeGroupIdentifier)

	return len(g.pendingNodes) + len(g.Nodes)
}

func (g *AutoScalerServerNodeGroup) AllNodes() []*AutoScalerServerNode {
	return append(utils.Values(g.Nodes), utils.Values(g.pendingNodes)...)
}

func (g *AutoScalerServerNodeGroup) setNodeGroupSize(c client.ClientGenerator, newSize int, prepareOnly bool) ([]*AutoScalerServerNode, error) {
	glog.Debugf("AutoScalerServerNodeGroup::setNodeGroupSize, nodeGroupID: %s", g.NodeGroupIdentifier)

	g.Lock()
	defer g.Unlock()

	delta := newSize - g.targetSize()

	if delta < 0 {
		if prepareOnly {
			return g.prepareDeleteNodes(delta), nil
		} else {
			return g.deleteNodes(c, delta)
		}
	} else if delta > 0 {
		if prepareOnly {
			return g.prepareNodes(delta)
		} else {
			return g.addNodes(c, delta)
		}
	}

	return []*AutoScalerServerNode{}, nil
}

func (g *AutoScalerServerNodeGroup) refresh() {
	glog.Debugf("AutoScalerServerNodeGroup::refresh, nodeGroupID: %s", g.NodeGroupIdentifier)

	for _, node := range g.AllNodes() {
		if node.State > AutoScalerServerNodeStateCreating {
			if _, err := node.statusVM(); err != nil {
				glog.Infof("status VM return an error: %v", err)
			}
		}
	}
}

func (g *AutoScalerServerNodeGroup) removeNamedNode(nodeName string) {
	delete(g.Nodes, nodeName)
	delete(g.pendingNodes, nodeName)
}

func (g *AutoScalerServerNodeGroup) findNamedNode(nodeName string) *AutoScalerServerNode {
	var node *AutoScalerServerNode = nil

	if node = g.Nodes[nodeName]; node == nil {
		node = g.pendingNodes[nodeName]
	}

	return node
}

func (g *AutoScalerServerNodeGroup) prepareDeleteNodes(delta int) []*AutoScalerServerNode {
	pendingNodes := utils.Values(g.pendingNodes)
	startIndex := len(pendingNodes) - 1
	tempNodes := make([]*AutoScalerServerNode, 0, -delta)

	for index := startIndex; index > 0; index-- {
		node := pendingNodes[index]

		// Don't delete not owned node
		if node.NodeType == AutoScalerServerNodeAutoscaled {
			delta++
			tempNodes = append(tempNodes, node)
		}

		if delta == 0 {
			break
		}
	}

	return tempNodes
}

func (g *AutoScalerServerNodeGroup) destroyNode(c client.ClientGenerator, node *AutoScalerServerNode) error {
	g.RunningNodes[node.NodeIndex] = ServerNodeStateDeleting

	e := node.deleteVM(c)

	g.RunningNodes[node.NodeIndex] = ServerNodeStateDeleted
	g.removeNamedNode(node.InstanceName)

	return e
}

func (g *AutoScalerServerNodeGroup) destroyNodes(c client.ClientGenerator, nodes []*AutoScalerServerNode) ([]*AutoScalerServerNode, error) {
	var err error
	deletedNodes := make([]*AutoScalerServerNode, 0, len(nodes))

	for _, node := range nodes {
		if err = g.destroyNode(c, node); err != nil {
			glog.Errorf(constantes.ErrUnableToDeleteVM, node.InstanceName, err)
		} else {
			deletedNodes = append(deletedNodes, node)
		}
	}

	return deletedNodes, err
}

// delta must be negative and will delete nodes in pending!!!!
func (g *AutoScalerServerNodeGroup) deleteNodes(c client.ClientGenerator, delta int) ([]*AutoScalerServerNode, error) {
	glog.Debugf("AutoScalerServerNodeGroup::deleteNodes, nodeGroupID: %s", g.NodeGroupIdentifier)

	return g.destroyNodes(c, g.prepareDeleteNodes(-delta))
}

func (g *AutoScalerServerNodeGroup) addManagedNode(crd *v1alpha2.ManagedNode) (*AutoScalerServerNode, error) {
	controlPlane := crd.Spec.ControlPlane
	nodeName, nodeIndex := g.nodeName(g.findNextNodeIndex(), controlPlane, true)
	instanceType := crd.Spec.InstanceType

	if len(instanceType) == 0 {
		instanceType = g.InstanceType
	}

	if machine, found := g.machines[instanceType]; found {
		// Clone the config to allow increment IP address for vmware
		if providerHandler, err := g.configuration.GetCloudConfiguration().CreateInstance(nodeName, instanceType, crd.Spec.ControlPlane, nodeIndex); err == nil {
			g.RunningNodes[nodeIndex] = ServerNodeStateCreating

			resLimit := g.configuration.ManagedNodeResourceLimiter
			diskSize := crd.Spec.DiskSizeInMB

			if diskSize == 0 {
				diskSize = machine.GetDiskSize()
			}

			diskSize = utils.MaxInt(utils.MinInt(diskSize, resLimit.GetMaxValue(constantes.ResourceNameManagedNodeDisk, types.ManagedNodeMaxDiskSize)),
				resLimit.GetMinValue(constantes.ResourceNameManagedNodeDisk, types.ManagedNodeMinDiskSize))

			// Change network if asked
			providerHandler.ConfigureNetwork(crd.Spec.Networking)

			node := &AutoScalerServerNode{
				NodeGroup:        g.NodeGroupIdentifier,
				NodeName:         nodeName,
				NodeIndex:        nodeIndex,
				InstanceName:     nodeName,
				Memory:           machine.Memory,
				CPU:              machine.Vcpu,
				DiskSize:         diskSize,
				NodeType:         AutoScalerServerNodeManaged,
				ControlPlaneNode: controlPlane,
				AllowDeployment:  crd.Spec.AllowDeployment,
				ExtraLabels:      CreateLabelOrAnnotation(crd.Spec.Labels),
				ExtraAnnotations: CreateLabelOrAnnotation(crd.Spec.Annotations),
				CRDUID:           crd.GetUID(),
				IPAddress:        providerHandler.InstancePrimaryAddressIP(),
				providerHandler:  providerHandler,
				serverConfig:     g.configuration,
			}

			annoteMaster := ""

			if g.configuration.Distribution != nil && *g.configuration.Distribution != kubernetes.KubeAdmDistributionName {
				annoteMaster = "true"
			}

			// Add system labels
			if controlPlane {
				node.ExtraLabels[constantes.NodeLabelMasterRole] = annoteMaster
				node.ExtraLabels[constantes.NodeLabelControlPlaneRole] = annoteMaster
				node.ExtraLabels["master"] = "true"
			} else {
				node.ExtraLabels[constantes.NodeLabelWorkerRole] = annoteMaster
				node.ExtraLabels["worker"] = "true"
			}

			g.pendingNodes[node.InstanceName] = node

			return node, nil
		} else {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf(constantes.ErrMachineTypeNotFound, instanceType)
	}
}

func (g *AutoScalerServerNodeGroup) prepareNodes(delta int) ([]*AutoScalerServerNode, error) {
	tempNodes := make([]*AutoScalerServerNode, 0, delta)
	config := g.configuration.GetCloudConfiguration()
	annoteMaster := ""
	machine := g.Machine()

	if g.configuration.Distribution != nil && *g.configuration.Distribution != kubernetes.KubeAdmDistributionName {
		annoteMaster = "true"
	}

	if g.Status != NodegroupCreated {
		glog.Debugf("AutoScalerServerNodeGroup::addNodes, nodeGroupID: %s -> g.status != nodegroupCreated", g.NodeGroupIdentifier)
		return []*AutoScalerServerNode{}, fmt.Errorf(constantes.ErrNodeGroupNotFound, g.NodeGroupIdentifier)
	}

	for {
		nodeName, nodeIndex := g.nodeName(g.findNextNodeIndex(), false, false)

		// Clone the vsphere config to allow increment IP address
		if providerHandler, err := config.CreateInstance(nodeName, g.InstanceType, false, nodeIndex); err == nil {

			g.RunningNodes[nodeIndex] = ServerNodeStateCreating

			extraAnnotations := types.KubernetesLabel{}
			extraLabels := types.KubernetesLabel{
				constantes.NodeLabelWorkerRole: annoteMaster,
				"worker":                       "true",
			}

			node := &AutoScalerServerNode{
				NodeGroup:        g.NodeGroupIdentifier,
				NodeName:         nodeName,
				NodeIndex:        nodeIndex,
				InstanceName:     nodeName,
				Memory:           machine.Memory,
				CPU:              machine.Vcpu,
				DiskSize:         machine.GetDiskSize(),
				NodeType:         AutoScalerServerNodeAutoscaled,
				ExtraAnnotations: extraAnnotations,
				ExtraLabels:      extraLabels,
				ControlPlaneNode: false,
				AllowDeployment:  true,
				IPAddress:        providerHandler.InstancePrimaryAddressIP(),
				providerHandler:  providerHandler,
				serverConfig:     g.configuration,
			}

			tempNodes = append(tempNodes, node)

			g.pendingNodes[node.InstanceName] = node

			delta--

			if delta == 0 {
				break
			}
		} else {
			g.pendingNodes = make(map[string]*AutoScalerServerNode)
			return []*AutoScalerServerNode{}, err
		}
	}

	return tempNodes, nil
}

func (g *AutoScalerServerNodeGroup) addNodes(c client.ClientGenerator, delta int) ([]*AutoScalerServerNode, error) {
	glog.Debugf("AutoScalerServerNodeGroup::addNodes, nodeGroupID: %s", g.NodeGroupIdentifier)

	if tempNodes, err := g.prepareNodes(delta); err == nil {
		return g.createNodes(c, tempNodes)
	} else {
		return tempNodes, err
	}
}

// return the list of successfuly created nodes
func (g *AutoScalerServerNodeGroup) createNodes(c client.ClientGenerator, nodes []*AutoScalerServerNode) ([]*AutoScalerServerNode, error) {
	var mu sync.Mutex
	createdNodes := make([]*AutoScalerServerNode, 0, len(nodes))

	glog.Debugf("AutoScalerServerNodeGroup::addNodes, nodeGroupID: %s", g.NodeGroupIdentifier)

	numberOfNodesToCreate := len(nodes)

	createNode := func(node *AutoScalerServerNode) error {
		var err error

		defer g.pendingNodesWG.Done()

		if g.Status != NodegroupCreated {
			g.RunningNodes[node.NodeIndex] = ServerNodeStateNotRunning
			glog.Debugf("AutoScalerServerNodeGroup::addNodes, nodeGroupID: %s -> g.status != nodegroupCreated", g.NodeGroupIdentifier)
			return fmt.Errorf(constantes.ErrUnableToLaunchVMNodeGroupNotReady, node.InstanceName)
		}

		if err = node.launchVM(c, g.NodeLabels, g.SystemLabels); err != nil {
			node.cleanOnLaunchError(c, err)

			mu.Lock()
			defer mu.Unlock()

			g.RunningNodes[node.NodeIndex] = ServerNodeStateDeleted
		} else {
			mu.Lock()
			defer mu.Unlock()

			createdNodes = append(createdNodes, node)

			g.Nodes[node.InstanceName] = node
			g.RunningNodes[node.NodeIndex] = ServerNodeStateRunning

			if node.NodeType == AutoScalerServerNodeAutoscaled {
				g.numOfProvisionnedNodes++
			} else if node.NodeType == AutoScalerServerNodeManaged {
				g.numOfManagedNodes++

				if node.ControlPlaneNode {
					g.numOfControlPlanes++
				}
			}
		}

		delete(g.pendingNodes, node.InstanceName)

		return err
	}

	var result error = nil
	var successful int

	g.pendingNodesWG.Add(numberOfNodesToCreate)

	// Do sync if one node only
	if numberOfNodesToCreate == 1 {
		if err := createNode(nodes[0]); err == nil {
			successful++
		}
	} else {
		var maxCreatedNodePerCycle int

		if g.configuration.MaxCreatedNodePerCycle <= 0 {
			maxCreatedNodePerCycle = numberOfNodesToCreate
		} else {
			maxCreatedNodePerCycle = g.configuration.MaxCreatedNodePerCycle
		}

		glog.Debugf("Launch node group %s of %d VM by %d nodes per cycle", g.NodeGroupIdentifier, numberOfNodesToCreate, maxCreatedNodePerCycle)

		createNodeAsync := func(currentNode *AutoScalerServerNode, wg *sync.WaitGroup, err chan error) {
			e := createNode(currentNode)
			wg.Done()
			err <- e
		}

		totalLoop := numberOfNodesToCreate / maxCreatedNodePerCycle

		if numberOfNodesToCreate%maxCreatedNodePerCycle > 0 {
			totalLoop++
		}

		currentNodeIndex := 0

		for numberOfCycle := 0; numberOfCycle < totalLoop; numberOfCycle++ {
			// WaitGroup per segment
			numberOfNodeInCycle := utils.MinInt(maxCreatedNodePerCycle, numberOfNodesToCreate-currentNodeIndex)

			glog.Debugf("Launched cycle: %d, node per cycle is: %d", numberOfCycle, numberOfNodeInCycle)

			if numberOfNodeInCycle > 1 {
				ch := make([]chan error, numberOfNodeInCycle)
				wg := sync.WaitGroup{}

				for nodeInCycle := 0; nodeInCycle < numberOfNodeInCycle; nodeInCycle++ {
					node := nodes[currentNodeIndex]
					cherr := make(chan error)
					ch[nodeInCycle] = cherr

					currentNodeIndex++

					wg.Add(1)

					go createNodeAsync(node, &wg, cherr)
				}

				// Wait this segment to launch
				glog.Debugf("Wait cycle to finish: %d", numberOfCycle)

				wg.Wait()

				glog.Debugf("Launched cycle: %d, collect result", numberOfCycle)

				for _, chError := range ch {
					if chError != nil {
						var err = <-chError

						if err == nil {
							successful++
						}
					}
				}

				glog.Debugf("Finished cycle: %d", numberOfCycle)
			} else if numberOfNodeInCycle > 0 {
				node := nodes[currentNodeIndex]
				currentNodeIndex++
				if err := createNode(node); err == nil {
					successful++
				}
			} else {
				break
			}
		}
	}

	g.pendingNodesWG.Wait()

	if g.Status != NodegroupCreated {
		result = fmt.Errorf(constantes.ErrUnableToLaunchNodeGroupNotCreated, g.NodeGroupIdentifier)

		glog.Errorf("Launched node group %s of %d VM got an error because was destroyed", g.NodeGroupIdentifier, numberOfNodesToCreate)
	} else if successful == 0 {
		result = fmt.Errorf(constantes.ErrUnableToLaunchNodeGroup, g.NodeGroupIdentifier)
		glog.Infof("Launched node group %s of %d VM failed", g.NodeGroupIdentifier, numberOfNodesToCreate)
	} else {
		glog.Infof("Launched node group %s of %d/%d VM successful", g.NodeGroupIdentifier, successful, numberOfNodesToCreate)
	}

	return createdNodes, result
}

func (g *AutoScalerServerNodeGroup) nodeAllowDeployment(nodeInfo *apiv1.Node) bool {

	if nodeInfo.Spec.Taints != nil {
		for _, taint := range nodeInfo.Spec.Taints {
			if taint.Key == constantes.NodeLabelMasterRole || taint.Key == constantes.NodeLabelControlPlaneRole {
				if taint.Effect == apiv1.TaintEffectNoSchedule {
					return true
				}
			}
		}
	}

	return true
}

func (g *AutoScalerServerNodeGroup) hasInstance(nodeName string) (bool, error) {
	glog.Debugf("AutoScalerServerNodeGroup::hasInstance, nodeGroupID: %s, nodeName: %s", g.NodeGroupIdentifier, nodeName)

	if node := g.findNamedNode(nodeName); node != nil {
		if status, err := node.statusVM(); err != nil {
			return false, err
		} else {
			return status == AutoScalerServerNodeStateRunning, nil
		}
	}

	return false, fmt.Errorf(constantes.ErrNodeNotFoundInNodeGroup, nodeName, g.NodeGroupIdentifier)
}

func (g *AutoScalerServerNodeGroup) findManagedNodeDeleted(client client.ClientGenerator, formerNodes map[string]*AutoScalerServerNode) {

	for nodeName, formerNode := range formerNodes {
		if found := g.findNamedNode(nodeName); found == nil {
			if _, err := formerNode.statusVM(); err == nil {
				glog.Infof("Node '%s' is deleted, delete VM", nodeName)
				if err := formerNode.deleteVM(client); err != nil {
					glog.Errorf(constantes.ErrUnableToDeleteVM, nodeName, err)
				}
			}
		}
	}
}

func (g *AutoScalerServerNodeGroup) autoDiscoveryNodes(client client.ClientGenerator, includeExistingNode bool) (map[string]*AutoScalerServerNode, error) {
	var lastNodeIndex = 0
	var nodeInfos *apiv1.NodeList
	var err error

	if nodeInfos, err = client.NodeList(); err != nil {
		return nil, err
	}

	formerNodes := g.Nodes

	g.Nodes = make(map[string]*AutoScalerServerNode)
	g.pendingNodes = make(map[string]*AutoScalerServerNode)
	g.RunningNodes = make(map[int]ServerNodeState)
	g.LastCreatedNodeIndex = 0
	g.numOfExternalNodes = 0
	g.numOfManagedNodes = 0
	g.numOfProvisionnedNodes = 0
	g.numOfControlPlanes = 0

	for _, nodeInfo := range nodeInfos.Items {
		if nodegroupName, found := nodeInfo.Annotations[constantes.AnnotationNodeGroupName]; found {
			var instanceName string
			var nodeType AutoScalerServerNodeType
			var crdUID uid.UID
			var vmUUID string
			var nodeIndex string
			var handler providers.ProviderHandler

			autoProvisionned, _ := strconv.ParseBool(nodeInfo.Annotations[constantes.AnnotationNodeAutoProvisionned])
			managedNode, _ := strconv.ParseBool(nodeInfo.Annotations[constantes.AnnotationNodeManaged])
			controlPlane := false

			if _, found = nodeInfo.Labels[constantes.NodeLabelControlPlaneRole]; found {
				controlPlane = true
			} else if _, found = nodeInfo.Labels[constantes.NodeLabelMasterRole]; found {
				controlPlane = true
			}

			if autoProvisionned {
				nodeType = AutoScalerServerNodeAutoscaled
			} else if managedNode {
				nodeType = AutoScalerServerNodeManaged
			} else {
				nodeType = AutoScalerServerNodeExternal
			}

			// Ignore nodes not handled by autoscaler if option includeExistingNode == false
			if nodegroupName == g.NodeGroupIdentifier && (autoProvisionned || includeExistingNode) {
				glog.Infof("Discover kubernetes node: %s matching nodegroup: %s", nodeInfo.Name, g.NodeGroupIdentifier)

				if instanceName, found = nodeInfo.Annotations[constantes.AnnotationInstanceName]; found {
					node := formerNodes[instanceName]
					runningIP := ""

					for _, address := range nodeInfo.Status.Addresses {
						if address.Type == apiv1.NodeInternalIP {
							runningIP = address.Address
							break
						}
					}

					if vmUUID, found = nodeInfo.Annotations[constantes.AnnotationInstanceID]; !found {
						vmUUID = node.findInstanceUUID()
					}

					if nodeIndex, found = nodeInfo.Annotations[constantes.AnnotationNodeIndex]; found {
						lastNodeIndex, _ = strconv.Atoi(nodeIndex)
					}

					glog.Infof("Handle kubernetes node: %s with IP: %s into nodegroup: %s", nodeInfo.Name, runningIP, g.NodeGroupIdentifier)

					g.LastCreatedNodeIndex = utils.MaxInt(g.LastCreatedNodeIndex, lastNodeIndex)

					if ownerRef := metav1.GetControllerOf(&nodeInfo); ownerRef != nil {
						if ownerRef.Kind == v1alpha2.SchemeGroupVersionKind.Kind {
							crdUID = ownerRef.UID
						}
					}

					// Node name and instance name could be differ when using AWS cloud provider
					if handler, err = g.configuration.GetCloudConfiguration().AttachInstance(instanceName, controlPlane, lastNodeIndex); err == nil {
						if node == nil {
							glog.Infof("Create node: %s with IP: %s to nodegroup: %s", instanceName, runningIP, g.NodeGroupIdentifier)

							node = &AutoScalerServerNode{
								NodeGroup:        g.NodeGroupIdentifier,
								NodeName:         nodeInfo.Name,
								NodeIndex:        lastNodeIndex,
								InstanceName:     instanceName,
								State:            AutoScalerServerNodeStateRunning,
								NodeType:         nodeType,
								VMUUID:           vmUUID,
								CRDUID:           crdUID,
								ControlPlaneNode: controlPlane,
								AllowDeployment:  g.nodeAllowDeployment(&nodeInfo),
								CPU:              int(nodeInfo.Status.Capacity.Cpu().Value()),
								Memory:           int(nodeInfo.Status.Capacity.Memory().Value() / (1024 * 1024)),
								DiskSize:         int(nodeInfo.Status.Capacity.Storage().Value() / (1024 * 1024)),
								IPAddress:        runningIP,
								providerHandler:  handler,
								serverConfig:     g.configuration,
							}

							err = client.AnnoteNode(nodeInfo.Name, map[string]string{
								constantes.AnnotationNodeGroupName:        g.NodeGroupIdentifier,
								constantes.AnnotationInstanceName:         instanceName,
								constantes.AnnotationInstanceID:           vmUUID,
								constantes.AnnotationScaleDownDisabled:    strconv.FormatBool(nodeType != AutoScalerServerNodeAutoscaled),
								constantes.AnnotationNodeAutoProvisionned: strconv.FormatBool(autoProvisionned),
								constantes.AnnotationNodeManaged:          strconv.FormatBool(managedNode),
								constantes.AnnotationNodeIndex:            strconv.Itoa(node.NodeIndex),
							})

							if err != nil {
								glog.Errorf(constantes.ErrAnnoteNodeReturnError, nodeInfo.Name, err)
							}

							err = client.LabelNode(nodeInfo.Name, map[string]string{
								constantes.AnnotationNodeGroupName: g.NodeGroupIdentifier,
							})

							if err != nil {
								glog.Errorf(constantes.ErrLabelNodeReturnError, nodeInfo.Name, err)
							}
						} else {
							node.providerHandler = handler
							glog.Infof("Attach existing node: %s with IP: %s to nodegroup: %s", instanceName, runningIP, g.NodeGroupIdentifier)
						}

						if err = node.retrieveNetworkInfos(); err != nil {
							glog.Errorf("an error occured during retrieve network infos: %v", err)
						}

						g.Nodes[nodeInfo.Name] = node
						g.RunningNodes[lastNodeIndex] = ServerNodeStateRunning

						if controlPlane {
							if managedNode {
								g.numOfManagedNodes++
							} else {
								g.numOfExternalNodes++
							}

							g.numOfControlPlanes++

						} else if autoProvisionned {
							g.numOfProvisionnedNodes++
						} else if managedNode {
							g.numOfManagedNodes++
						} else {
							g.numOfExternalNodes++
						}

						lastNodeIndex++

						_, _ = node.statusVM()
					} else {
						glog.Errorf("Can not add node: %s with IP: %s to nodegroup: %s, reason: %v", instanceName, runningIP, g.NodeGroupIdentifier, err)
					}
				}
			} else {
				glog.Infof("Ignore kubernetes node %s not handled by me", nodeInfo.Name)
			}
		}
	}

	return formerNodes, nil
}

func (g *AutoScalerServerNodeGroup) deleteNode(c client.ClientGenerator, node *AutoScalerServerNode) error {
	var err error

	if node.ControlPlaneNode {
		err = fmt.Errorf(constantes.ErrUnableToDeleteControlPlaneNode, node.NodeName)
	} else {
		g.RunningNodes[node.NodeIndex] = ServerNodeStateDeleting

		if err = node.deleteVM(c); err != nil {
			glog.Errorf(constantes.ErrUnableToDeleteVM, node.InstanceName, err)
		}

		g.RunningNodes[node.NodeIndex] = ServerNodeStateDeleted
		g.removeNamedNode(node.InstanceName)

		if node.NodeType == AutoScalerServerNodeAutoscaled {
			g.numOfProvisionnedNodes--
		} else {
			g.numOfManagedNodes--
		}
	}

	return err
}

func (g *AutoScalerServerNodeGroup) deleteNodeByName(c client.ClientGenerator, nodeName string) error {
	glog.Debugf("AutoScalerServerNodeGroup::deleteNodeByName, nodeGroupID: %s, nodeName: %s", g.NodeGroupIdentifier, nodeName)

	if node := g.findNamedNode(nodeName); node != nil {

		return g.deleteNode(c, node)
	}

	return fmt.Errorf(constantes.ErrNodeNotFoundInNodeGroup, nodeName, g.NodeGroupIdentifier)
}

func (g *AutoScalerServerNodeGroup) setConfiguration(config *types.AutoScalerServerConfig, machines providers.MachineCharacteristics) (err error) {
	glog.Debugf("AutoScalerServerNodeGroup::setConfiguration, nodeGroupID: %s", g.NodeGroupIdentifier)

	g.configuration = config
	g.machines = machines

	for _, node := range g.AllNodes() {
		if err = node.setServerConfiguration(config); err != nil {
			glog.Errorf("unable to set configuration to node: %s, %v", node.InstanceName, err)
			return err
		}
	}

	return err
}

func (g *AutoScalerServerNodeGroup) deleteNodeGroup(c client.ClientGenerator) error {
	glog.Debugf("AutoScalerServerNodeGroup::deleteNodeGroup, nodeGroupID: %s", g.NodeGroupIdentifier)

	return g.cleanup(c)
}

func (g *AutoScalerServerNodeGroup) getProvisionnedNodePrefix() string {
	if len(g.ProvisionnedNodeNamePrefix) == 0 {
		return "autoscaled"
	}

	return g.ProvisionnedNodeNamePrefix
}

func (g *AutoScalerServerNodeGroup) getControlPlanePrefix() string {
	if len(g.ControlPlaneNamePrefix) == 0 {
		return "master"
	}

	return g.ControlPlaneNamePrefix
}

func (g *AutoScalerServerNodeGroup) getManagedNodePrefix() string {
	if len(g.ManagedNodeNamePrefix) == 0 {
		return "worker"
	}

	return g.ManagedNodeNamePrefix
}

func (g *AutoScalerServerNodeGroup) nodeName(vmIndex int, controlplane, managed bool) (string, int) {
	var start int
	config := g.configuration.GetCloudConfiguration()

	if controlplane {
		start = 2
	} else {
		start = 1
	}

	for index := start; index <= g.MaxNodeSize; index++ {
		var nodeName string

		if controlplane {
			nodeName = fmt.Sprintf(nodeNameTemplate, g.NodeGroupIdentifier, g.getControlPlanePrefix(), index)
		} else if managed {
			nodeName = fmt.Sprintf(nodeNameTemplate, g.NodeGroupIdentifier, g.getManagedNodePrefix(), index)
		} else {
			nodeName = fmt.Sprintf(nodeNameTemplate, g.NodeGroupIdentifier, g.getProvisionnedNodePrefix(), index)
		}

		if found := g.findNamedNode(nodeName); found == nil {
			if !config.InstanceExists(nodeName) {
				return nodeName, vmIndex
			} else {
				glog.Warnf(constantes.ErrVMAlreadyExists, nodeName)
				g.RunningNodes[vmIndex] = ServerNodeStateRunning
				vmIndex++
			}
		}
	}

	// Should never reach this code
	if controlplane {
		return fmt.Sprintf(nodeNameTemplate, g.NodeGroupIdentifier, g.getControlPlanePrefix(), vmIndex-g.numOfExternalNodes-g.numOfProvisionnedNodes-g.numOfManagedNodes+g.numOfControlPlanes+1), vmIndex
	} else if managed {
		return fmt.Sprintf(nodeNameTemplate, g.NodeGroupIdentifier, g.getManagedNodePrefix(), vmIndex-g.numOfExternalNodes-g.numOfProvisionnedNodes+1), vmIndex
	} else {
		return fmt.Sprintf(nodeNameTemplate, g.NodeGroupIdentifier, g.getProvisionnedNodePrefix(), vmIndex-g.numOfExternalNodes-g.numOfManagedNodes+1), vmIndex
	}
}

func (g *AutoScalerServerNodeGroup) findNodeByCRDUID(uid uid.UID) (*AutoScalerServerNode, error) {
	for _, node := range g.AllNodes() {
		if node.CRDUID == uid {
			return node, nil
		}
	}
	return nil, fmt.Errorf(constantes.ErrManagedNodeNotFound, uid)
}

func (g *AutoScalerServerNodeGroup) GetOptions(defaults *apigrpc.AutoscalingOptions) (*apigrpc.AutoscalingOptions, error) {
	if options, err := g.configuration.GetAutoScalingOptions(); options != nil {
		return options, err
	}

	return defaults, nil
}

func (g *AutoScalerServerNodeGroup) havingDeletingNodes() bool {
	for _, node := range g.Nodes {
		if node.State == AutoScalerServerNodeStateDeleting {
			return true
		}
	}

	return false
}
