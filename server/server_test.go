package server

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	apigrpc "github.com/Fred78290/kubernetes-cloud-autoscaler/grpc"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testServiceIdentifier = "vmware"
	testGroupID           = "vmware-ca-k8s"
	testNodeName          = "DC0_H0_VM0"
	testVMUUID            = "265104de-1472-547c-b873-6dc7883fb6cb"
	testCRDUID            = "96cb1c71-1d2e-4c55-809f-72e874fc4b2c"
	launchVMName          = "vmware-ca-k8s-autoscaled-01"
)

type autoScalerServerAppTest struct {
	AutoScalerServerApp
	*grpcServerApp
	ng           *autoScalerServerNodeGroupTest
	createdGroup string
}

func (s *autoScalerServerAppTest) newFakeNode(nodeName string) apiv1.Node {
	return apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			UID:  testCRDUID,
			Annotations: map[string]string{
				constantes.AnnotationNodeGroupName:        testGroupID,
				constantes.AnnotationNodeIndex:            "0",
				constantes.AnnotationInstanceID:           findInstanceID(s.ng.config.GetCloudConfiguration(), nodeName),
				constantes.AnnotationInstanceName:         nodeName,
				constantes.AnnotationNodeAutoProvisionned: "true",
			},
		},
	}
}

func (s *autoScalerServerAppTest) createFakeNode(nodeName string) apiv1.Node {
	nodes := s.ng.AllNodes()

	if len(nodes) > 0 {
		node := nodes[0]

		return apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: node.NodeName,
				UID:  node.CRDUID,
				Annotations: map[string]string{
					constantes.AnnotationNodeGroupName:        node.NodeGroup,
					constantes.AnnotationNodeIndex:            fmt.Sprintf("%d", node.NodeIndex),
					constantes.AnnotationInstanceID:           node.VMUUID,
					constantes.AnnotationInstanceName:         node.InstanceName,
					constantes.AnnotationNodeAutoProvisionned: "true",
				},
			},
		}
	}

	return s.newFakeNode(nodeName)
}

type serverTest struct {
	baseTest
	appTest *autoScalerServerAppTest
}

func (m *serverTest) Child(t *testing.T) *serverTest {
	m.baseTest.Child(t)

	return m
}

func (m *serverTest) NodeGroups() {
	s, err := m.newTestServer(true, false, false)

	expected := []string{
		testGroupID,
	}

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.CloudProviderServiceRequest{
			ProviderID: s.configuration.ServiceIdentifier,
		}

		if got, err := s.NodeGroups(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.NodeGroups() error = %v", err)
		} else if !reflect.DeepEqual(m.extractNodeGroup(got.GetNodeGroups()), expected) {
			m.Errorf("AutoScalerServerApp.NodeGroups() = %v, want %v", m.extractNodeGroup(got.GetNodeGroups()), expected)
		}
	}
}

func (m *serverTest) NodeGroupForNode() {
	s, err := m.newTestServer(true, false, true)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupForNodeRequest{
			ProviderID: s.configuration.ServiceIdentifier,
			Node:       utils.ToJSON(s.createFakeNode(testNodeName)),
		}

		if got, err := s.NodeGroupForNode(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.NodeGroupForNode() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.NodeGroupForNode() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		} else if !reflect.DeepEqual(got.GetNodeGroup().GetId(), testGroupID) {
			m.Errorf("AutoScalerServerApp.NodeGroupForNode() = %v, want %v", got.GetNodeGroup().GetId(), testGroupID)
		}
	}
}

func (m *serverTest) HasInstance() {
	s, err := m.newTestServer(true, true, false, AutoScalerServerNodeStateRunning)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.HasInstanceRequest{
			ProviderID: s.configuration.ServiceIdentifier,
			Node:       utils.ToJSON(s.createFakeNode(testNodeName)),
		}

		if got, err := s.HasInstance(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.HasInstance() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.HasInstance() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		} else if got.GetHasInstance() == false {
			m.RunningTest().Error("AutoScalerServerApp.HasInstance() not found")
		}
	}
}

func (m *serverTest) Pricing() {
	s, err := m.newTestServer(true, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.CloudProviderServiceRequest{
			ProviderID: s.configuration.ServiceIdentifier,
		}

		if got, err := s.Pricing(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.Pricing() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.Pricing() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		} else if !reflect.DeepEqual(got.GetPriceModel().GetId(), s.configuration.ServiceIdentifier) {
			m.Errorf("AutoScalerServerApp.Pricing() = %v, want %v", got.GetPriceModel().GetId(), s.configuration.ServiceIdentifier)
		}
	}
}

func (m *serverTest) GetAvailableMachineTypes() {
	s, err := m.newTestServer(true, false, false)

	expected := []string{
		"2xlarge",
		"4xlarge",
		"large",
		"medium",
		"small",
		"tiny",
		"xlarge",
	}

	sort.Strings(expected)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.CloudProviderServiceRequest{
			ProviderID: s.configuration.ServiceIdentifier,
		}

		if got, err := s.GetAvailableMachineTypes(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.GetAvailableMachineTypes() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.GetAvailableMachineTypes() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		} else if !reflect.DeepEqual(m.extractAvailableMachineTypes(got.GetAvailableMachineTypes()), expected) {
			m.Errorf("AutoScalerServerApp.GetAvailableMachineTypes() = %v, want %v", m.extractAvailableMachineTypes(got.GetAvailableMachineTypes()), expected)
		}
	}
}

func (m *serverTest) NewNodeGroup() {
	s, err := m.newTestServer(false, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NewNodeGroupRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			MachineType: s.configuration.DefaultMachineType,
			Labels:      s.configuration.NodeLabels,
		}

		if got, err := s.NewNodeGroup(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.NewNodeGroup() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.NewNodeGroup() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		} else {
			m.appTest.createdGroup = got.GetNodeGroup().GetId()
			m.RunningTest().Logf("AutoScalerServerApp.NewNodeGroup() return node group created : %v", m.appTest.createdGroup)
		}
	}
}

func (m *serverTest) GetResourceLimiter() {
	s, err := m.newTestServer(true, false, false)

	expected := &types.ResourceLimiter{
		MinLimits: map[string]int64{constantes.ResourceNameCores: 1, constantes.ResourceNameMemory: 10000000},
		MaxLimits: map[string]int64{constantes.ResourceNameCores: 5, constantes.ResourceNameMemory: 100000000},
	}

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.CloudProviderServiceRequest{
			ProviderID: s.configuration.ServiceIdentifier,
		}

		if got, err := s.GetResourceLimiter(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.GetResourceLimiter() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.GetResourceLimiter() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		} else if !reflect.DeepEqual(m.extractResourceLimiter(got.GetResourceLimiter()), expected) {
			m.Errorf("AutoScalerServerApp.GetResourceLimiter() = %v, want %v", got.GetResourceLimiter(), expected)
		}
	}
}

func (m *serverTest) Cleanup() {
	s, err := m.newTestServer(true, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.CloudProviderServiceRequest{
			ProviderID: s.configuration.ServiceIdentifier,
		}

		if got, err := s.Cleanup(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.Cleanup() error = %v", err)
		} else if got.GetError() != nil && strings.HasSuffix(got.GetError().GetReason(), "is not provisionned by me") == false {
			m.Errorf("AutoScalerServerApp.Cleanup() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		}
	}
}

func (m *serverTest) Refresh() {
	s, err := m.newTestServer(true, false, true)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.CloudProviderServiceRequest{
			ProviderID: s.configuration.ServiceIdentifier,
		}

		if got, err := s.Refresh(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.Refresh() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.Refresh() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		}
	}
}

func (m *serverTest) MaxSize() {
	s, err := m.newTestServer(true, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupServiceRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
		}

		if got, err := s.MaxSize(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.MaxSize() error = %v", err)
		} else if got.GetMaxSize() != int32(*s.configuration.MaxNode) {
			m.Errorf("AutoScalerServerApp.MaxSize() = %v, want %v", got.GetMaxSize(), s.configuration.MaxNode)
		}
	}
}

func (m *serverTest) MinSize() {
	s, err := m.newTestServer(true, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupServiceRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
		}

		if got, err := s.MinSize(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.MinSize() error = %v", err)
		} else if got.GetMinSize() != int32(*s.configuration.MinNode) {
			m.Errorf("AutoScalerServerApp.MinSize() = %v, want %v", got.GetMinSize(), s.configuration.MinNode)
		}
	}
}

func (m *serverTest) TargetSize() {
	s, err := m.newTestServer(true, false, true)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupServiceRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
		}

		if got, err := s.TargetSize(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.TargetSize() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.TargetSize() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		} else if got.GetTargetSize() != 1 {
			m.Errorf("AutoScalerServerApp.TargetSize() = %v, want %v", got.GetTargetSize(), 1)
		}
	}
}

func (m *serverTest) IncreaseSize() {
	s, err := m.newTestServer(true, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.IncreaseSizeRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
			Delta:       1,
		}

		if got, err := s.IncreaseSize(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.IncreaseSize() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.IncreaseSize() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		}
	}
}

func (m *serverTest) DeleteNodes() {
	s, err := m.newTestServer(true, false, true)

	if assert.NoError(m.RunningTest(), err) {
		nodes := []string{utils.ToJSON(s.createFakeNode(testNodeName))}
		request := &apigrpc.DeleteNodesRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
			Node:        nodes,
		}

		if got, err := s.DeleteNodes(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.DeleteNodes() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.DeleteNodes() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		}
	}
}

func (m *serverTest) DecreaseTargetSize() {
	s, err := m.newTestServer(true, false, true)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.DecreaseTargetSizeRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
			Delta:       -1,
		}

		if got, err := s.DecreaseTargetSize(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.DecreaseTargetSize() error = %v", err)
		} else if got.GetError() != nil && !strings.HasPrefix(got.GetError().GetReason(), "attempt to delete existing nodes") {
			m.Errorf("AutoScalerServerApp.DecreaseTargetSize() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		}
	}
}

func (m *serverTest) Id() {
	s, err := m.newTestServer(true, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupServiceRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
		}

		if got, err := s.Id(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.Id() error = %v", err)
		} else if got.GetResponse() != testGroupID {
			m.Errorf("AutoScalerServerApp.Id() = %v, want %v", got, testGroupID)
		}
	}
}

func (m *serverTest) Debug() {
	s, err := m.newTestServer(true, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupServiceRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
		}

		if _, err := s.Debug(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.Debug() error = %v", err)
		}
	}
}

func (m *serverTest) Nodes() {
	s, err := m.newTestServer(true, false, true)
	nodes := s.ng.AllNodes()
	expected := make([]string, 0, len(nodes))

	for _, node := range nodes {
		expected = append(expected, node.generateProviderID())
	}

	sort.Strings(expected)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupServiceRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
		}

		if got, err := s.Nodes(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.Nodes() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.Nodes() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		} else if !reflect.DeepEqual(m.sortedInstanceID(got.GetInstances()), expected) {
			m.Errorf("AutoScalerServerApp.Nodes() = %v, want %v", m.sortedInstanceID(got.GetInstances()), expected)
		}
	}
}

func (m *serverTest) TemplateNodeInfo() {
	s, err := m.newTestServer(true, false, true)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupServiceRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
		}

		if got, err := s.TemplateNodeInfo(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.TemplateNodeInfo() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.TemplateNodeInfo() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		}
	}
}

func (m *serverTest) Exist() {
	s, err := m.newTestServer(true, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupServiceRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
		}

		if got, err := s.Exist(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.Exist() error = %v", err)
		} else if got.GetExists() == false {
			m.Errorf("AutoScalerServerApp.Exist() = %v", got.GetExists())
		}
	}
}

func (m *serverTest) Create() {
	s, err := m.newTestServer(true, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupServiceRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: m.appTest.createdGroup,
		}

		if got, err := s.Create(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.Create() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.Create() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		} else if got.GetNodeGroup().GetId() != m.appTest.createdGroup {
			m.Errorf("AutoScalerServerApp.Create() = %v, want %v", got.GetNodeGroup().GetId(), m.appTest.createdGroup)
		}
	}
}

func (m *serverTest) Delete() {
	s, err := m.newTestServer(true, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupServiceRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: m.appTest.createdGroup,
		}

		if got, err := s.Delete(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.Delete() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.Delete() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		}
	}
}

func (m *serverTest) Autoprovisioned() {
	s, err := m.newTestServer(true, false, false)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodeGroupServiceRequest{
			ProviderID:  s.configuration.ServiceIdentifier,
			NodeGroupID: testGroupID,
		}

		if got, err := s.Autoprovisioned(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.Autoprovisioned() error = %v", err)
		} else if got.GetAutoprovisioned() == false {
			m.Errorf("AutoScalerServerApp.Autoprovisioned() = %v, want true", got.GetAutoprovisioned())
		}
	}
}

func (m *serverTest) Belongs() {
	s, err := m.newTestServer(true, true, true)

	tests := []struct {
		name    string
		request *apigrpc.BelongsRequest
		want    bool
		wantErr bool
	}{
		{
			name: "Belongs",
			want: true,
			request: &apigrpc.BelongsRequest{
				ProviderID:  s.configuration.ServiceIdentifier,
				NodeGroupID: testGroupID,
				Node:        utils.ToJSON(s.newFakeNode(testNodeName)),
			},
		},
		{
			name:    "NotBelongs",
			want:    false,
			wantErr: false,
			request: &apigrpc.BelongsRequest{
				ProviderID:  s.configuration.ServiceIdentifier,
				NodeGroupID: testGroupID,
				Node:        utils.ToJSON(s.newFakeNode("wrong-name")),
			},
		},
	}

	if assert.NoError(m.RunningTest(), err) {
		for _, test := range tests {

			got, err := s.Belongs(context.TODO(), test.request)

			if (err != nil) != test.wantErr {
				m.Errorf("AutoScalerServerApp.Belongs() error = %v", err)
			} else if got.GetError() != nil {
				m.Errorf("AutoScalerServerApp.Belongs() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
			} else if got.GetBelongs() != test.want {
				m.Errorf("AutoScalerServerApp.Belongs() = %v, want %v", got.GetBelongs(), test.want)
			}
		}
	}
}

func (m *serverTest) NodePrice() {
	s, err := m.newTestServer(true, false, true)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.NodePriceRequest{
			ProviderID: s.configuration.ServiceIdentifier,
			StartTime:  time.Now().Unix(),
			EndTime:    time.Now().Add(time.Hour).Unix(),
			Node:       utils.ToJSON(s.createFakeNode(testNodeName)),
		}

		if got, err := s.NodePrice(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.NodePrice() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.NodePrice() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		} else if got.GetPrice() != 0 {
			m.Errorf("AutoScalerServerApp.NodePrice() = %v, want %v", got.GetPrice(), 0)
		}
	}
}

func (m *serverTest) PodPrice() {
	s, err := m.newTestServer(true, false, true)

	if assert.NoError(m.RunningTest(), err) {
		request := &apigrpc.PodPriceRequest{
			ProviderID: s.configuration.ServiceIdentifier,
			StartTime:  time.Now().Unix(),
			EndTime:    time.Now().Add(time.Hour).Unix(),
			Pod:        utils.ToJSON(s.createFakeNode(testNodeName)),
		}

		if got, err := s.PodPrice(context.TODO(), request); err != nil {
			m.Errorf("AutoScalerServerApp.PodPrice() error = %v", err)
		} else if got.GetError() != nil {
			m.Errorf("AutoScalerServerApp.PodPrice() return an error, code = %v, reason = %s", got.GetError().GetCode(), got.GetError().GetReason())
		} else if got.GetPrice() != 0 {
			m.Errorf("AutoScalerServerApp.PodPrice() = %v, want %v", got.GetPrice(), 0)
		}
	}
}

func (m *serverTest) extractInstanceID(instances *apigrpc.Instances) []string {
	r := make([]string, 0, len(instances.GetItems()))

	for _, n := range instances.GetItems() {
		r = append(r, n.GetId())
	}

	return r
}

func (m *serverTest) sortedInstanceID(instances *apigrpc.Instances) []string {
	r := m.extractInstanceID(instances)

	sort.Strings(r)

	return r
}

func (m *serverTest) extractNodeGroup(nodeGroups []*apigrpc.NodeGroup) []string {
	r := make([]string, len(nodeGroups))

	for i, n := range nodeGroups {
		r[i] = n.Id
	}

	return r
}

func (m *serverTest) extractResourceLimiter(res *apigrpc.ResourceLimiter) *types.ResourceLimiter {
	r := &types.ResourceLimiter{
		MinLimits: res.MinLimits,
		MaxLimits: res.MaxLimits,
	}

	return r
}

func (m *serverTest) extractAvailableMachineTypes(availableMachineTypes *apigrpc.AvailableMachineTypes) []string {
	r := make([]string, len(availableMachineTypes.MachineType))

	copy(r, availableMachineTypes.MachineType)

	sort.Strings(r)

	return r
}

func (m *serverTest) newTestServer(addNodeGroup, addTestNode, controlPlane bool, desiredState ...AutoScalerServerNodeState) (*autoScalerServerAppTest, error) {
	var ng *autoScalerServerNodeGroupTest
	var err error

	if m.appTest == nil {
		if ng, err = m.newTestNodeGroup(); err != nil {
			return nil, err
		}

		m.appTest = &autoScalerServerAppTest{
			ng:           ng,
			createdGroup: testGroupID,
			AutoScalerServerApp: AutoScalerServerApp{
				ResourceLimiter: &types.ResourceLimiter{
					MinLimits: map[string]int64{constantes.ResourceNameCores: 1, constantes.ResourceNameMemory: 10000000},
					MaxLimits: map[string]int64{constantes.ResourceNameCores: 5, constantes.ResourceNameMemory: 100000000},
				},
				NodesDefinition: []*apigrpc.NodeGroupDef{
					{
						NodeGroupID:  ng.NodeGroupIdentifier,
						Provisionned: ng.AutoProvision,
						MinSize:      int32(ng.MinNodeSize),
						MaxSize:      int32(ng.MaxNodeSize),
					},
				},
				Groups:        map[string]*AutoScalerServerNodeGroup{},
				kubeClient:    m,
				configuration: &m.config.AutoScalerServerConfig,
				machines:      m.config.machines,
				running:       true,
			},
		}

		if m.appTest.grpcServerApp, err = NewGrpcServerApp(&m.appTest.AutoScalerServerApp); err != nil {
			return nil, err
		}
	} else {
		ng = m.appTest.ng
	}

	if addNodeGroup {
		m.appTest.Groups[ng.NodeGroupIdentifier] = ng.AutoScalerServerNodeGroup

		if addTestNode && len(ng.AllNodes()) == 0 {
			ng.createTestNode(testNodeName, controlPlane, desiredState...)
		}
	}

	return m.appTest, err
}

func getTestMode() bool {
	if testMode := strings.ToLower(os.Getenv("TEST_MODE")); testMode != "" {
		return testMode == "true" || testMode == "1"
	}

	return false
}

func createServerTest(t *testing.T, stopOnFailure bool) *serverTest {
	return &serverTest{
		baseTest: baseTest{
			parentTest:    t,
			childTest:     t,
			stopOnFailure: stopOnFailure,
			testMode:      getTestMode(),
		},
	}
}

func TestServer_NodeGroups(t *testing.T) {
	createServerTest(t, false).NodeGroups()
}

func TestServer_NodeGroupForNode(t *testing.T) {
	createServerTest(t, false).NodeGroupForNode()
}

func TestServer_Pricing(t *testing.T) {
	createServerTest(t, false).Pricing()
}

func TestServer_GetAvailableMachineTypes(t *testing.T) {
	createServerTest(t, false).GetAvailableMachineTypes()
}

func TestServer_NewNodeGroup(t *testing.T) {
	createServerTest(t, false).NewNodeGroup()
}

func TestServer_GetResourceLimiter(t *testing.T) {
	createServerTest(t, false).GetResourceLimiter()
}

func TestServer_Cleanup(t *testing.T) {
	createServerTest(t, false).Cleanup()
}

func TestServer_Refresh(t *testing.T) {
	createServerTest(t, false).Refresh()
}

func TestServer_MaxSize(t *testing.T) {
	createServerTest(t, false).MaxSize()
}

func TestServer_MinSize(t *testing.T) {
	createServerTest(t, false).MinSize()
}

func TestServer_TargetSize(t *testing.T) {
	createServerTest(t, false).TargetSize()
}

func TestServer_IncreaseSize(t *testing.T) {
	createServerTest(t, false).IncreaseSize()
}

func TestServer_DecreaseTargetSize(t *testing.T) {
	createServerTest(t, false).DecreaseTargetSize()
}

func TestServer_DeleteNodes(t *testing.T) {
	createServerTest(t, false).DeleteNodes()
}

func TestServer_Id(t *testing.T) {
	createServerTest(t, false).Id()
}

func TestServer_Debug(t *testing.T) {
	createServerTest(t, false).Debug()
}

func TestServer_Nodes(t *testing.T) {
	createServerTest(t, false).Nodes()
}

func TestServer_TemplateNodeInfo(t *testing.T) {
	createServerTest(t, false).TemplateNodeInfo()
}

func TestServer_Exist(t *testing.T) {
	createServerTest(t, false).Exist()
}

func TestServer_Create(t *testing.T) {
	createServerTest(t, false).Create()
}

func TestServer_Delete(t *testing.T) {
	createServerTest(t, false).Delete()
}

func TestServer_Autoprovisioned(t *testing.T) {
	createServerTest(t, false).Autoprovisioned()
}

func TestServer_Belongs(t *testing.T) {
	createServerTest(t, false).Belongs()
}

func TestServer_NodePrice(t *testing.T) {
	createServerTest(t, false).NodePrice()
}

func TestServer_PodPrice(t *testing.T) {
	createServerTest(t, false).PodPrice()
}
