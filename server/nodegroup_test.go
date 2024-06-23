package server

import (
	"fmt"
	"os"
	"testing"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/client"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	managednodeClientset "github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/generated/clientset/versioned"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	glog "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	apiv1 "k8s.io/api/core/v1"
	apiextension "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type autoScalerServerConfigTest struct {
	types.AutoScalerServerConfig
	machines providers.MachineCharacteristics
}

type baseTest struct {
	config        *autoScalerServerConfigTest
	parentTest    *testing.T
	childTest     *testing.T
	testMode      bool
	stopOnFailure bool
}

func (b *baseTest) Child(t *testing.T) *baseTest {
	b.childTest = t
	return b
}

func (b *baseTest) RunningTest() *testing.T {
	return b.childTest
}

func (b *baseTest) Errorf(format string, args ...any) {
	if b.stopOnFailure && b.childTest != nil {
		b.parentTest.Fatalf(format, args...)
	} else {
		b.childTest.Errorf(format, args...)
	}
}

type nodegroupTest struct {
	baseTest
}

func (ng *nodegroupTest) Child(t *testing.T) *nodegroupTest {
	ng.baseTest.Child(t)

	return ng
}

type autoScalerServerNodeGroupTest struct {
	*AutoScalerServerNodeGroup
	*baseTest
}

func (ng *autoScalerServerNodeGroupTest) Child(t *testing.T) *autoScalerServerNodeGroupTest {
	ng.baseTest.Child(t)

	return ng
}

func (ng *autoScalerServerNodeGroupTest) createTestNode(nodeName string, controlPlane bool, desiredState ...AutoScalerServerNodeState) (node *AutoScalerServerNode) {
	var state AutoScalerServerNodeState = AutoScalerServerNodeStateNotCreated
	var providerHandler providers.ProviderHandler
	var err error
	var found bool

	if node, found = ng.Nodes[nodeName]; found {
		return
	}

	if len(desiredState) > 0 {
		state = desiredState[0]
	}

	machine := ng.Machine()

	if ng.config.GetCloudConfiguration().InstanceExists(nodeName) {
		providerHandler, err = ng.config.GetCloudConfiguration().AttachInstance(nodeName, false, 1)
	} else {
		providerHandler, err = ng.config.GetCloudConfiguration().CreateInstance(nodeName, ng.InstanceType, false, 1)
	}

	if err != nil {
		ng.RunningTest().Fatalf("unable to createTestNode: %s, reason: %v", nodeName, err)
	} else {
		node = &AutoScalerServerNode{
			NodeGroup:        testGroupID,
			NodeName:         nodeName,
			InstanceName:     nodeName,
			ControlPlaneNode: controlPlane,
			VMUUID:           testVMUUID,
			CRDUID:           testCRDUID,
			Memory:           machine.Memory,
			CPU:              machine.Vcpu,
			DiskSize:         machine.GetDiskSize(),
			IPAddress:        "127.0.0.1",
			State:            state,
			NodeType:         AutoScalerServerNodeAutoscaled,
			NodeIndex:        1,
			providerHandler:  providerHandler,
			serverConfig:     ng.configuration,
		}

		if vmuuid := node.findInstanceUUID(); len(vmuuid) > 0 {
			node.VMUUID = vmuuid
		}

		ng.Nodes[nodeName] = node
		ng.RunningNodes[len(ng.RunningNodes)+1] = ServerNodeStateRunning
	}

	return
}

func (m *nodegroupTest) launchVM() {
	ng, testNode, err := m.newTestNode(true, launchVMName)

	if assert.NoError(m.RunningTest(), err) {
		if err := testNode.launchVM(m, ng.NodeLabels, ng.SystemLabels); err != nil {
			m.Errorf("AutoScalerNode.launchVM() error = %v", err)
		}
	}
}

func (m *nodegroupTest) startVM() {
	_, testNode, err := m.newTestNode(true, launchVMName)

	if assert.NoError(m.RunningTest(), err) {
		if err := testNode.startVM(m); err != nil {
			m.Errorf("AutoScalerNode.startVM() error = %v", err)
		}
	}
}

func (m *nodegroupTest) stopVM() {
	_, testNode, err := m.newTestNode(true, launchVMName)

	if assert.NoError(m.RunningTest(), err) {
		if err := testNode.stopVM(m); err != nil {
			m.Errorf("AutoScalerNode.stopVM() error = %v", err)
		}
	}
}

func (m *nodegroupTest) deleteVM() {
	_, testNode, err := m.newTestNode(true, launchVMName)

	if assert.NoError(m.RunningTest(), err) {
		if err := testNode.deleteVM(m); err != nil {
			m.Errorf("AutoScalerNode.deleteVM() error = %v", err)
		}
	}
}

func (m *nodegroupTest) statusVM() {
	_, testNode, err := m.newTestNode(true, launchVMName)

	if assert.NoError(m.RunningTest(), err) {
		if got, err := testNode.statusVM(); err != nil {
			m.Errorf("AutoScalerNode.statusVM() error = %v", err)
		} else if got != AutoScalerServerNodeStateRunning {
			m.Errorf("AutoScalerNode.statusVM() = %v, want %v", got, AutoScalerServerNodeStateRunning)
		}
	}
}

func (m *nodegroupTest) addNode() {
	ng, err := m.newTestNodeGroup()

	if assert.NoError(m.RunningTest(), err) {
		if _, err := ng.addNodes(m, 1); err != nil {
			m.Errorf("AutoScalerServerNodeGroup.addNode() error = %v", err)
		}
	}
}

func (m *nodegroupTest) deleteNode() {
	ng, testNode, err := m.newTestNode(false, launchVMName)

	if assert.NoError(m.RunningTest(), err) {
		if err := ng.deleteNodeByName(m, testNode.NodeName); err != nil {
			m.Errorf("AutoScalerServerNodeGroup.deleteNode() error = %v", err)
		}
	}
}

func (m *nodegroupTest) deleteNodeGroup() {
	ng, err := m.newTestNodeGroup()

	if assert.NoError(m.RunningTest(), err) {
		if err := ng.deleteNodeGroup(m); err != nil {
			m.Errorf("AutoScalerServerNodeGroup.deleteNodeGroup() error = %v", err)
		}
	}
}

func (m *baseTest) KubeClient() (kubernetes.Interface, error) {
	return nil, nil
}

func (m *baseTest) NodeManagerClient() (managednodeClientset.Interface, error) {
	return nil, nil
}

func (m *baseTest) ApiExtentionClient() (apiextension.Interface, error) {
	return nil, nil
}

func (m *baseTest) PodList(nodeName string, podFilter client.PodFilterFunc) ([]apiv1.Pod, error) {
	return nil, nil
}

func (m *baseTest) NodeList() (*apiv1.NodeList, error) {
	node := apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNodeName,
			UID:  testCRDUID,
			Annotations: map[string]string{
				constantes.AnnotationNodeGroupName:        testGroupID,
				constantes.AnnotationNodeIndex:            "0",
				constantes.AnnotationInstanceID:           testVMUUID,
				constantes.AnnotationNodeAutoProvisionned: "true",
				constantes.AnnotationScaleDownDisabled:    "false",
				constantes.AnnotationNodeManaged:          "false",
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: fmt.Sprintf("vsphere://%s", testVMUUID),
		},
		Status: apiv1.NodeStatus{
			Phase: apiv1.NodeRunning,
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeDiskPressure,
					Status: apiv1.ConditionFalse,
				},
				{
					Type:   apiv1.NodeMemoryPressure,
					Status: apiv1.ConditionFalse,
				},
				{
					Type:   apiv1.NodeNetworkUnavailable,
					Status: apiv1.ConditionFalse,
				},
			},
		},
	}

	return &apiv1.NodeList{
		Items: []apiv1.Node{
			node,
		},
	}, nil
}

func (m *baseTest) UncordonNode(nodeName string) error {
	return nil
}

func (m *baseTest) CordonNode(nodeName string) error {
	return nil
}

func (m *baseTest) SetProviderID(nodeName, providerID string) error {
	return nil
}

func (m *baseTest) MarkDrainNode(nodeName string) error {
	return nil
}

func (m *baseTest) DrainNode(nodeName string, ignoreDaemonSet, deleteLocalData bool) error {
	return nil
}

func (m *baseTest) GetNode(nodeName string) (*apiv1.Node, error) {
	node := &apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			UID:  testCRDUID,
			Annotations: map[string]string{
				constantes.AnnotationNodeGroupName:        testGroupID,
				constantes.AnnotationNodeIndex:            "0",
				constantes.AnnotationInstanceID:           findInstanceID(m.config.GetCloudConfiguration(), nodeName),
				constantes.AnnotationNodeAutoProvisionned: "true",
				constantes.AnnotationScaleDownDisabled:    "false",
				constantes.AnnotationNodeManaged:          "false",
			},
		},
	}

	return node, nil
}

func (m *baseTest) DeleteNode(nodeName string) error {
	return nil
}

func (m *baseTest) AnnoteNode(nodeName string, annotations map[string]string) error {
	return nil
}

func (m *baseTest) LabelNode(nodeName string, labels map[string]string) error {
	return nil
}

func (m *baseTest) TaintNode(nodeName string, taints ...apiv1.Taint) error {
	return nil
}

func (m *baseTest) WaitNodeToBeReady(nodeName string) error {
	return nil
}

func (p *baseTest) GetSecret(secretName, namespace string) (*apiv1.Secret, error) {
	return nil, fmt.Errorf("Not implemented")
}

func (m *baseTest) DeleteSecret(secretName, namespace string) error {
	return nil
}

func (m *baseTest) newTestNodeNamedWithState(nodeName string, controlPlane bool, _ AutoScalerServerNodeState) (*autoScalerServerNodeGroupTest, *AutoScalerServerNode, error) {

	if ng, err := m.newTestNodeGroup(); err == nil {
		vm := ng.createTestNode(nodeName, controlPlane)

		return ng, vm, err
	} else {
		return nil, nil, err
	}
}

func (m *baseTest) newTestNode(controlPlane bool, name ...string) (*autoScalerServerNodeGroupTest, *AutoScalerServerNode, error) {
	nodeName := testNodeName

	if len(name) > 0 {
		nodeName = name[0]
	}

	return m.newTestNodeNamedWithState(nodeName, controlPlane, AutoScalerServerNodeStateNotCreated)
}

func (m *baseTest) newTestNodeGroup() (ng *autoScalerServerNodeGroupTest, err error) {
	if err = m.loadTestConfig(m.testMode); err == nil {
		if _, ok := m.config.machines[m.config.DefaultMachineType]; ok {
			ng = &autoScalerServerNodeGroupTest{
				baseTest: m,
				AutoScalerServerNodeGroup: &AutoScalerServerNodeGroup{
					AutoProvision:              true,
					ServiceIdentifier:          m.config.ServiceIdentifier,
					NodeGroupIdentifier:        testGroupID,
					ProvisionnedNodeNamePrefix: m.config.ProvisionnedNodeNamePrefix,
					ManagedNodeNamePrefix:      m.config.ManagedNodeNamePrefix,
					ControlPlaneNamePrefix:     m.config.ControlPlaneNamePrefix,
					Status:                     NodegroupCreated,
					MinNodeSize:                int(*m.config.MinNode),
					MaxNodeSize:                int(*m.config.MaxNode),
					SystemLabels:               types.KubernetesLabel{},
					Nodes:                      make(map[string]*AutoScalerServerNode),
					RunningNodes:               make(map[int]ServerNodeState),
					pendingNodes:               make(map[string]*AutoScalerServerNode),
					configuration:              &m.config.AutoScalerServerConfig,
					NodeLabels:                 m.config.NodeLabels,
					InstanceType:               m.config.DefaultMachineType,
					machines:                   m.config.machines,
				},
			}
		} else {
			m.RunningTest().Fatalf("Unable to find machine definition for type: %s", m.config.DefaultMachineType)
		}
	}

	return
}

func (m *baseTest) loadTestConfig(testMode bool) (err error) {
	var config autoScalerServerConfigTest

	fileName := utils.GetServerConfigFile()
	machineConfig := utils.GetMachinesConfigFile()
	providerConfig := utils.GetProviderConfigFile()

	if os.Getenv("TESTLOGLEVEL") == "DEBUG" {
		glog.SetLevel(glog.DebugLevel)
	} else if os.Getenv("TESTLOGLEVEL") == "TRACE" {
		glog.SetLevel(glog.TraceLevel)
	}

	if err = utils.LoadConfig(fileName, &config.AutoScalerServerConfig); err != nil {
		glog.Errorf("failed to decode config file: %s, error: %v", fileName, err)
		return
	}

	if err = utils.LoadConfig(machineConfig, &config.machines); err != nil {
		glog.Errorf("failed to decode machines config file: %s, error: %v", machineConfig, err)
		return
	}

	if err = config.SetupCloudConfiguration(providerConfig); err != nil {
		glog.Errorf("failed to decode provider config file: %s, error: %v", providerConfig, err)
		return
	}

	config.GetCloudConfiguration().SetMode(testMode)
	config.SSH.SetMode(testMode)

	m.config = &config

	return
}

func (m *baseTest) ssh() {
	err := m.loadTestConfig(false)

	if assert.NoError(m.RunningTest(), err) {
		if _, err = utils.Sudo(m.config.SSH, "127.0.0.1", 1, "ls"); err != nil {
			m.Errorf("SSH error = %v", err)
		}
	}
}

func findInstanceID(config providers.ProviderConfiguration, nodeName string) string {
	if vmUUID, err := config.UUID(nodeName); err == nil {
		return vmUUID
	}

	return testVMUUID
}

func Test_SSH(t *testing.T) {
	createTestNodegroup(t, false).ssh()
}

func createTestNodegroup(t *testing.T, stopOnFailure bool) *nodegroupTest {
	if os.Getenv("TESTLOGLEVEL") == "DEBUG" {
		glog.SetLevel(glog.DebugLevel)
	} else if os.Getenv("TESTLOGLEVEL") == "TRACE" {
		glog.SetLevel(glog.TraceLevel)
	}

	return &nodegroupTest{
		baseTest: baseTest{
			parentTest:    t,
			childTest:     t,
			stopOnFailure: stopOnFailure,
			testMode:      utils.GetTestMode(),
		},
	}
}

func TestNodeGroup_launchVM(t *testing.T) {
	createTestNodegroup(t, false).launchVM()
}

func TestNodeGroup_startVM(t *testing.T) {
	createTestNodegroup(t, false).startVM()
}

func TestNodeGroup_stopVM(t *testing.T) {
	createTestNodegroup(t, false).stopVM()
}

func TestNodeGroup_deleteVM(t *testing.T) {
	createTestNodegroup(t, false).deleteVM()
}

func TestNodeGroup_statusVM(t *testing.T) {
	createTestNodegroup(t, false).statusVM()
}

func TestNodeGroupGroup_addNode(t *testing.T) {
	createTestNodegroup(t, false).addNode()
}

func TestNodeGroupGroup_deleteNode(t *testing.T) {
	createTestNodegroup(t, false).deleteNode()
}

func TestNodeGroupGroup_deleteNodeGroup(t *testing.T) {
	createTestNodegroup(t, false).deleteNodeGroup()
}
