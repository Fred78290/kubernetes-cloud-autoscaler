package server

import (
	"context"
	"fmt"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	apigrpc "github.com/Fred78290/kubernetes-cloud-autoscaler/grpc"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	glog "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type grpcServerApp struct {
	apigrpc.UnimplementedCloudProviderServiceServer
	apigrpc.UnimplementedNodeGroupServiceServer
	apigrpc.UnimplementedPricingModelServiceServer

	appServer *AutoScalerServerApp
}

func NewGrpcServerApp(appServer *AutoScalerServerApp) (*grpcServerApp, error) {
	external := &grpcServerApp{
		appServer: appServer,
	}

	return external, nil
}

func (s *grpcServerApp) RegisterCloudProviderServer(server *grpc.Server) {
	apigrpc.RegisterCloudProviderServiceServer(server, s)
	apigrpc.RegisterNodeGroupServiceServer(server, s)
	apigrpc.RegisterPricingModelServiceServer(server, s)
}

func (s *grpcServerApp) isCallDenied(request cloudProviderRequest) bool {
	return request.GetProviderID() != s.appServer.configuration.ServiceIdentifier
}

// Connect allows client to connect
func (s *grpcServerApp) Connect(ctx context.Context, request *apigrpc.ConnectRequest) (*apigrpc.ConnectReply, error) {
	glog.Debugf("Call server Connect: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)

		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if request.GetResourceLimiter() != nil {
		if s.appServer.ResourceLimiter != nil {
			s.appServer.ResourceLimiter.MergeRequestResourceLimiter(request.GetResourceLimiter())
		} else {
			s.appServer.ResourceLimiter = &types.ResourceLimiter{
				MinLimits: request.ResourceLimiter.MinLimits,
				MaxLimits: request.ResourceLimiter.MaxLimits,
			}

			s.appServer.ResourceLimiter.SetMaxValue64(constantes.ResourceNameNodes, *s.appServer.configuration.MaxNode)
			s.appServer.ResourceLimiter.SetMinValue64(constantes.ResourceNameNodes, *s.appServer.configuration.MinNode)
		}
	}

	s.appServer.NodesDefinition = request.GetNodes()
	s.appServer.AutoProvision = request.GetAutoProvisionned()

	if s.appServer.AutoProvision {
		if err := s.appServer.doAutoProvision(); err != nil {
			glog.Errorf(constantes.ErrUnableToAutoProvisionNodeGroup, err)

			return nil, err
		}
	}

	return &apigrpc.ConnectReply{
		Response: &apigrpc.ConnectReply_Connected{
			Connected: true,
		},
	}, nil
}

// Name returns name of the cloud provider.
func (s *grpcServerApp) Name(ctx context.Context, request *apigrpc.CloudProviderServiceRequest) (*apigrpc.NameReply, error) {
	glog.Debugf("Call server Name: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	return &apigrpc.NameReply{
		Name: constantes.ProviderName,
	}, nil
}

// NodeGroups returns all node groups configured for this cloud provider.
func (s *grpcServerApp) NodeGroups(ctx context.Context, request *apigrpc.CloudProviderServiceRequest) (*apigrpc.NodeGroupsReply, error) {
	glog.Debugf("Call server NodeGroups: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	nodeGroups := make([]*apigrpc.NodeGroup, 0, len(s.appServer.Groups))

	for name, nodeGroup := range s.appServer.Groups {
		// Return node group if created
		if nodeGroup.Status == NodegroupCreated {
			nodeGroups = append(nodeGroups, &apigrpc.NodeGroup{
				Id: name,
			})
		}
	}

	return &apigrpc.NodeGroupsReply{
		NodeGroups: nodeGroups,
	}, nil
}

// NodeGroupForNode returns the node group for the given node, nil if the node
// should not be processed by cluster autoscaler, or non-nil error if such
// occurred. Must be implemented.
func (s *grpcServerApp) NodeGroupForNode(ctx context.Context, request *apigrpc.NodeGroupForNodeRequest) (*apigrpc.NodeGroupForNodeReply, error) {
	glog.Debugf("Call server NodeGroupForNode: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	node, err := utils.NodeFromJSON(request.GetNode())

	if err != nil {
		glog.Errorf(constantes.ErrCantUnmarshallNodeWithReason, request.GetNode(), err)

		return &apigrpc.NodeGroupForNodeReply{
			Response: &apigrpc.NodeGroupForNodeReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: err.Error(),
				},
			},
		}, nil
	}

	if nodegroupName, found := node.Annotations[constantes.AnnotationNodeGroupName]; found {
		nodeGroup, err := s.appServer.getNodeGroup(nodegroupName)

		if err != nil {
			return &apigrpc.NodeGroupForNodeReply{
				Response: &apigrpc.NodeGroupForNodeReply_Error{
					Error: &apigrpc.Error{
						Code:   constantes.CloudProviderError,
						Reason: err.Error(),
					},
				},
			}, nil
		}

		if nodeGroup == nil {
			glog.Errorf("Nodegroup not found for node: %s", node.Name)

			return &apigrpc.NodeGroupForNodeReply{
				Response: &apigrpc.NodeGroupForNodeReply_NodeGroup{
					NodeGroup: &apigrpc.NodeGroup{},
				},
			}, nil
		}

		return &apigrpc.NodeGroupForNodeReply{
			Response: &apigrpc.NodeGroupForNodeReply_NodeGroup{
				NodeGroup: &apigrpc.NodeGroup{
					Id: nodeGroup.NodeGroupIdentifier,
				},
			},
		}, nil
	} else {
		return &apigrpc.NodeGroupForNodeReply{
			Response: &apigrpc.NodeGroupForNodeReply_NodeGroup{
				NodeGroup: &apigrpc.NodeGroup{},
			},
		}, nil
	}

}

// Pricing returns pricing model for this cloud provider or error if not available.
// Implementation optional.
func (s *grpcServerApp) Pricing(ctx context.Context, request *apigrpc.CloudProviderServiceRequest) (*apigrpc.PricingModelReply, error) {
	glog.Debugf("Call server Pricing: %v", request)

	if s.appServer.configuration.Optionals != nil && s.appServer.configuration.Optionals.Pricing {
		return nil, fmt.Errorf(constantes.ErrNotImplemented)
	}

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	return &apigrpc.PricingModelReply{
		Response: &apigrpc.PricingModelReply_PriceModel{
			PriceModel: &apigrpc.PricingModel{
				Id: s.appServer.configuration.ServiceIdentifier,
			},
		},
	}, nil
}

// GetAvailableMachineTypes get all machine types that can be requested from the cloud provider.
// Implementation optional.
func (s *grpcServerApp) GetAvailableMachineTypes(ctx context.Context, request *apigrpc.CloudProviderServiceRequest) (*apigrpc.AvailableMachineTypesReply, error) {
	glog.Debugf("Call server GetAvailableMachineTypes: %v", request)

	if s.appServer.configuration.Optionals != nil && s.appServer.configuration.Optionals.GetAvailableMachineTypes {
		return nil, fmt.Errorf(constantes.ErrNotImplemented)
	}

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	machineTypes := make([]string, 0, len(s.appServer.machines))

	for n := range s.appServer.machines {
		machineTypes = append(machineTypes, n)
	}

	return &apigrpc.AvailableMachineTypesReply{
		Response: &apigrpc.AvailableMachineTypesReply_AvailableMachineTypes{
			AvailableMachineTypes: &apigrpc.AvailableMachineTypes{
				MachineType: machineTypes,
			},
		},
	}, nil
}

// NewNodeGroup builds a theoretical node group based on the node definition provided. The node group is not automatically
// created on the cloud provider side. The node group is not returned by NodeGroups() until it is created.
// Implementation optional.
func (s *grpcServerApp) NewNodeGroup(ctx context.Context, request *apigrpc.NewNodeGroupRequest) (*apigrpc.NewNodeGroupReply, error) {
	glog.Debugf("Call server NewNodeGroup: %v", request)

	if s.appServer.configuration.Optionals != nil && s.appServer.configuration.Optionals.NewNodeGroup {
		return nil, fmt.Errorf(constantes.ErrNotImplemented)
	}

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	instanceType := request.GetMachineType()

	if len(instanceType) == 0 {
		instanceType = s.appServer.configuration.DefaultMachineType
	}

	machineType := s.appServer.machines[instanceType]

	if machineType == nil {
		glog.Errorf(constantes.ErrMachineTypeNotFound, request.GetMachineType())

		return &apigrpc.NewNodeGroupReply{
			Response: &apigrpc.NewNodeGroupReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: fmt.Sprintf(constantes.ErrMachineTypeNotFound, request.GetMachineType()),
				},
			},
		}, nil
	}

	var nodeGroupIdentifier string

	labels := make(types.KubernetesLabel)
	systemLabels := make(types.KubernetesLabel)

	if reqLabels := request.GetLabels(); reqLabels != nil {
		for k2, v2 := range reqLabels {
			labels[k2] = v2
		}
	}

	if reqSystemLabels := request.GetSystemLabels(); reqSystemLabels != nil {
		for k2, v2 := range reqSystemLabels {
			systemLabels[k2] = v2
		}
	}

	if len(request.GetNodeGroupID()) == 0 {
		nodeGroupIdentifier = s.appServer.generateNodeGroupName()
	} else {
		nodeGroupIdentifier = request.GetNodeGroupID()
	}

	input := nodegroupCreateInput{
		nodeGroupID:   nodeGroupIdentifier,
		minNodeSize:   int(request.GetMinNodeSize()),
		maxNodeSize:   int(request.GetMaxNodeSize()),
		machineType:   instanceType,
		labels:        labels,
		systemLabels:  systemLabels,
		autoProvision: false,
	}

	nodeGroup, err := s.appServer.newNodeGroup(&input)

	if err != nil {
		glog.Errorf(constantes.ErrUnableToCreateNodeGroup, nodeGroupIdentifier, err)

		return &apigrpc.NewNodeGroupReply{
			Response: &apigrpc.NewNodeGroupReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: fmt.Sprintf(constantes.ErrUnableToCreateNodeGroup, nodeGroupIdentifier, err),
				},
			},
		}, nil
	}

	return &apigrpc.NewNodeGroupReply{
		Response: &apigrpc.NewNodeGroupReply_NodeGroup{
			NodeGroup: &apigrpc.NodeGroup{
				Id: nodeGroup.NodeGroupIdentifier,
			},
		},
	}, nil
}

// GetResourceLimiter returns struct containing limits (max, min) for resources (cores, memory etc.).
func (s *grpcServerApp) GetResourceLimiter(ctx context.Context, request *apigrpc.CloudProviderServiceRequest) (*apigrpc.ResourceLimiterReply, error) {
	glog.Debugf("Call server GetResourceLimiter: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	return &apigrpc.ResourceLimiterReply{
		Response: &apigrpc.ResourceLimiterReply_ResourceLimiter{
			ResourceLimiter: &apigrpc.ResourceLimiter{
				MinLimits: s.appServer.ResourceLimiter.MinLimits,
				MaxLimits: s.appServer.ResourceLimiter.MaxLimits,
			},
		},
	}, nil
}

// GPULabel returns the label added to nodes with GPU resource.
func (s *grpcServerApp) GPULabel(ctx context.Context, request *apigrpc.CloudProviderServiceRequest) (*apigrpc.GPULabelReply, error) {
	glog.Debugf("Call server GPULabel: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	return &apigrpc.GPULabelReply{
		Response: &apigrpc.GPULabelReply_Gpulabel{
			Gpulabel: "",
		},
	}, nil
}

// GetAvailableGPUTypes return all available GPU types cloud provider supports.
func (s *grpcServerApp) GetAvailableGPUTypes(ctx context.Context, request *apigrpc.CloudProviderServiceRequest) (*apigrpc.GetAvailableGPUTypesReply, error) {
	glog.Debugf("Call server GetAvailableGPUTypes: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	return &apigrpc.GetAvailableGPUTypesReply{
		AvailableGpuTypes: s.appServer.configuration.GetCloudConfiguration().GetAvailableGpuTypes(),
	}, nil
}

// Cleanup cleans up open resources before the cloud provider is destroyed, i.e. go routines etc.
func (s *grpcServerApp) Cleanup(ctx context.Context, request *apigrpc.CloudProviderServiceRequest) (*apigrpc.CleanupReply, error) {
	glog.Debugf("Call server Cleanup: %v", request)

	var lastError *apigrpc.Error

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	for _, nodeGroup := range s.appServer.Groups {
		if err := nodeGroup.cleanup(s.appServer.kubeClient); err != nil {
			lastError = &apigrpc.Error{
				Code:   constantes.CloudProviderError,
				Reason: err.Error(),
			}
		}
	}

	glog.Debug("Leave server Cleanup, done")

	s.appServer.Groups = make(map[string]*AutoScalerServerNodeGroup)

	return &apigrpc.CleanupReply{
		Error: lastError,
	}, nil
}

// Refresh is called before every main loop and can be used to dynamically update cloud provider state.
// In particular the list of node groups returned by NodeGroups can change as a result of CloudProvider.Refresh().
func (s *grpcServerApp) Refresh(ctx context.Context, request *apigrpc.CloudProviderServiceRequest) (*apigrpc.RefreshReply, error) {
	glog.Debugf("Call server Refresh: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	for _, ng := range s.appServer.Groups {
		ng.refresh()
	}

	if phSaveState {
		if err := s.appServer.Save(phSavedState); err != nil {
			glog.Errorf(constantes.ErrFailedToSaveServerState, err)
		}
	}

	return &apigrpc.RefreshReply{
		Error: nil,
	}, nil
}

func (s *grpcServerApp) HasInstance(ctx context.Context, request *apigrpc.HasInstanceRequest) (*apigrpc.HasInstanceReply, error) {

	glog.Debugf("Call server HasInstance: %v", request)

	if request.GetProviderID() != s.appServer.configuration.ServiceIdentifier {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	node, err := utils.NodeFromJSON(request.GetNode())

	if err != nil {
		glog.Errorf(constantes.ErrCantUnmarshallNodeWithReason, request.GetNode(), err)

		return &apigrpc.HasInstanceReply{
			Response: &apigrpc.HasInstanceReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: err.Error(),
				},
			},
		}, nil
	}

	if nodegroupName, found := node.Annotations[constantes.AnnotationNodeGroupName]; found {
		if nodeGroup, err := s.appServer.getNodeGroup(nodegroupName); err != nil {
			return &apigrpc.HasInstanceReply{
				Response: &apigrpc.HasInstanceReply_Error{
					Error: &apigrpc.Error{
						Code:   constantes.CloudProviderError,
						Reason: err.Error(),
					},
				},
			}, nil
		} else if nodeGroup == nil {
			glog.Infof("Nodegroup not found for node: %s", node.Name)

			return &apigrpc.HasInstanceReply{
				Response: &apigrpc.HasInstanceReply_Error{
					Error: &apigrpc.Error{
						Code:   constantes.CloudProviderError,
						Reason: fmt.Sprintf(constantes.ErrNodeGroupForNodeNotFound, nodegroupName, node.Name),
					},
				},
			}, nil
		} else {

			var hasInstance bool

			if hasInstance, err = nodeGroup.hasInstance(node.Name); err != nil {
				return &apigrpc.HasInstanceReply{
					Response: &apigrpc.HasInstanceReply_Error{
						Error: &apigrpc.Error{
							Code:   constantes.CloudProviderError,
							Reason: err.Error(),
						},
					},
				}, nil
			}

			return &apigrpc.HasInstanceReply{
				Response: &apigrpc.HasInstanceReply_HasInstance{
					HasInstance: hasInstance,
				},
			}, nil
		}
	} else {
		return &apigrpc.HasInstanceReply{
			Response: &apigrpc.HasInstanceReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: fmt.Sprintf(constantes.ErrMissingNodeAnnotationError, node.Name),
				},
			},
		}, nil
	}

}

// MaxSize returns maximum size of the node group.
func (s *grpcServerApp) MaxSize(ctx context.Context, request *apigrpc.NodeGroupServiceRequest) (*apigrpc.MaxSizeReply, error) {
	glog.Debugf("Call server MaxSize: %v", request)

	var maxSize int

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if nodeGroup, err := s.appServer.getNodeGroup(request.GetNodeGroupID()); err != nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())
	} else {
		maxSize = nodeGroup.MaxNodeSize
	}

	return &apigrpc.MaxSizeReply{
		MaxSize: int32(maxSize),
	}, nil
}

// MinSize returns minimum size of the node group.
func (s *grpcServerApp) MinSize(ctx context.Context, request *apigrpc.NodeGroupServiceRequest) (*apigrpc.MinSizeReply, error) {
	glog.Debugf("Call server MinSize: %v", request)

	var minSize int

	if s.isCallDenied(request) {
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if nodeGroup, err := s.appServer.getNodeGroup(request.GetNodeGroupID()); err != nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())
	} else {
		minSize = nodeGroup.MinNodeSize
	}

	return &apigrpc.MinSizeReply{
		MinSize: int32(minSize),
	}, nil
}

// TargetSize returns the current target size of the node group. It is possible that the
// number of nodes in Kubernetes is different at the moment but should be equal
// to Size() once everything stabilizes (new nodes finish startup and registration or
// removed nodes are deleted completely). Implementation required.
func (s *grpcServerApp) TargetSize(ctx context.Context, request *apigrpc.NodeGroupServiceRequest) (*apigrpc.TargetSizeReply, error) {
	glog.Debugf("Call server TargetSize: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if nodeGroup, err := s.appServer.getNodeGroup(request.GetNodeGroupID()); err != nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())

		return &apigrpc.TargetSizeReply{
			Response: &apigrpc.TargetSizeReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: fmt.Sprintf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID()),
				},
			},
		}, nil
	} else {
		return &apigrpc.TargetSizeReply{
			Response: &apigrpc.TargetSizeReply_TargetSize{
				TargetSize: int32(nodeGroup.targetSize()),
			},
		}, nil
	}
}

// IncreaseSize increases the size of the node group. To delete a node you need
// to explicitly name it and use DeleteNode. This function should wait until
// node group size is updated. Implementation required.
func (s *grpcServerApp) IncreaseSize(ctx context.Context, request *apigrpc.IncreaseSizeRequest) (*apigrpc.IncreaseSizeReply, error) {
	glog.Debugf("Call server IncreaseSize: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if nodeGroup, err := s.appServer.getNodeGroup(request.GetNodeGroupID()); err != nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())

		return &apigrpc.IncreaseSizeReply{
			Error: &apigrpc.Error{
				Code:   constantes.CloudProviderError,
				Reason: fmt.Sprintf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID()),
			},
		}, nil
	} else {

		if request.GetDelta() <= 0 {
			glog.Errorf(constantes.ErrIncreaseSizeMustBePositive)

			return &apigrpc.IncreaseSizeReply{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: constantes.ErrIncreaseSizeMustBePositive,
				},
			}, nil
		}

		newSize := nodeGroup.targetSize() + int(request.GetDelta())

		if newSize > nodeGroup.MaxNodeSize {
			glog.Errorf(constantes.ErrIncreaseSizeTooLarge, newSize, nodeGroup.MaxNodeSize)

			return &apigrpc.IncreaseSizeReply{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: fmt.Sprintf(constantes.ErrIncreaseSizeTooLarge, newSize, nodeGroup.MaxNodeSize),
				},
			}, nil
		}

		if _, err := nodeGroup.setNodeGroupSize(s.appServer.kubeClient, newSize, false); err != nil {
			return &apigrpc.IncreaseSizeReply{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: err.Error(),
				},
			}, nil
		}
	}

	return &apigrpc.IncreaseSizeReply{
		Error: nil,
	}, nil
}

// DeleteNodes deletes nodes from this node group. Error is returned either on
// failure or if the given node doesn't belong to this node group. This function
// should wait until node group size is updated. Implementation required.
func (s *grpcServerApp) DeleteNodes(ctx context.Context, request *apigrpc.DeleteNodesRequest) (*apigrpc.DeleteNodesReply, error) {
	glog.Debugf("Call server DeleteNodes: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if nodeGroup, err := s.appServer.getNodeGroup(request.GetNodeGroupID()); err != nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())

		return &apigrpc.DeleteNodesReply{
			Error: &apigrpc.Error{
				Code:   constantes.CloudProviderError,
				Reason: fmt.Sprintf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID()),
			},
		}, nil
	} else {

		if nodeGroup.targetSize()-len(request.GetNode()) < nodeGroup.MinNodeSize {
			return &apigrpc.DeleteNodesReply{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: fmt.Sprintf(constantes.ErrMinSizeReached, request.GetNodeGroupID()),
				},
			}, nil
		}

		// Iterate over each requested node to delete
		for idx, sNode := range request.GetNode() {
			node, err := utils.NodeFromJSON(sNode)

			// Can't deserialize
			if node == nil || err != nil {
				glog.Errorf(constantes.ErrCantUnmarshallNodeWithReason, sNode, err)

				return &apigrpc.DeleteNodesReply{
					Error: &apigrpc.Error{
						Code:   constantes.CloudProviderError,
						Reason: fmt.Sprintf(constantes.ErrCantUnmarshallNode, idx, request.GetNodeGroupID()),
					},
				}, nil
			}

			// Check node group owner
			if nodegroupName, found := node.Annotations[constantes.AnnotationNodeGroupName]; found {
				if nodeGroup, err = s.appServer.getNodeGroup(nodegroupName); err != nil {
					glog.Errorf(constantes.ErrNodeGroupNotFound, nodegroupName)

					return &apigrpc.DeleteNodesReply{
						Error: &apigrpc.Error{
							Code:   constantes.CloudProviderError,
							Reason: err.Error(),
						},
					}, nil
				}

				// Delete the node in the group
				if err = nodeGroup.deleteNodeByName(s.appServer.kubeClient, node.Name); err != nil {
					return &apigrpc.DeleteNodesReply{
						Error: &apigrpc.Error{
							Code:   constantes.CloudProviderError,
							Reason: err.Error(),
						},
					}, nil
				}

			} else {
				return &apigrpc.DeleteNodesReply{
					Error: &apigrpc.Error{
						Code:   constantes.CloudProviderError,
						Reason: fmt.Sprintf(constantes.ErrUnableToDeleteNode, node.Name, nodeGroup.NodeGroupIdentifier),
					},
				}, nil
			}
		}
	}

	return &apigrpc.DeleteNodesReply{
		Error: nil,
	}, nil
}

// DecreaseTargetSize decreases the target size of the node group. This function
// doesn't permit to delete any existing node and can be used only to reduce the
// request for new nodes that have not been yet fulfilled. Delta should be negative.
// It is assumed that cloud provider will not delete the existing nodes when there
// is an option to just decrease the target. Implementation required.
func (s *grpcServerApp) DecreaseTargetSize(ctx context.Context, request *apigrpc.DecreaseTargetSizeRequest) (*apigrpc.DecreaseTargetSizeReply, error) {
	glog.Debugf("Call server DecreaseTargetSize: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if nodeGroup, err := s.appServer.getNodeGroup(request.GetNodeGroupID()); err != nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())

		return &apigrpc.DecreaseTargetSizeReply{
			Error: &apigrpc.Error{
				Code:   constantes.CloudProviderError,
				Reason: fmt.Sprintf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID()),
			},
		}, nil
	} else {

		if request.GetDelta() >= 0 {
			glog.Errorf(constantes.ErrDecreaseSizeMustBeNegative)

			return &apigrpc.DecreaseTargetSizeReply{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: constantes.ErrDecreaseSizeMustBeNegative,
				},
			}, nil
		}

		newSize := nodeGroup.targetSize() + int(request.GetDelta())

		if newSize < len(nodeGroup.Nodes) {
			if nodeGroup.havingDeletingNodes() {
				return &apigrpc.DecreaseTargetSizeReply{
					Error: nil,
				}, nil
			}

			glog.Errorf(constantes.ErrDecreaseSizeAttemptDeleteNodes, nodeGroup.targetSize(), request.GetDelta(), newSize)

			return &apigrpc.DecreaseTargetSizeReply{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: fmt.Sprintf(constantes.ErrDecreaseSizeAttemptDeleteNodes, nodeGroup.targetSize(), request.GetDelta(), newSize),
				},
			}, nil
		}

		if _, err := nodeGroup.setNodeGroupSize(s.appServer.kubeClient, newSize, false); err != nil {
			return &apigrpc.DecreaseTargetSizeReply{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: err.Error(),
				},
			}, nil
		}
	}

	return &apigrpc.DecreaseTargetSizeReply{
		Error: nil,
	}, nil
}

// Id returns an unique identifier of the node group.
func (s *grpcServerApp) Id(ctx context.Context, request *apigrpc.NodeGroupServiceRequest) (*apigrpc.IdReply, error) {
	glog.Debugf("Call server Id: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if nodeGroup, err := s.appServer.getNodeGroup(request.GetNodeGroupID()); err != nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())
		return nil, fmt.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())
	} else {
		return &apigrpc.IdReply{
			Response: nodeGroup.NodeGroupIdentifier,
		}, nil
	}
}

// Debug returns a string containing all information regarding this node group.
func (s *grpcServerApp) Debug(ctx context.Context, request *apigrpc.NodeGroupServiceRequest) (*apigrpc.DebugReply, error) {
	glog.Debugf("Call server Debug: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if nodeGroup, err := s.appServer.getNodeGroup(request.GetNodeGroupID()); err != nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())

		return nil, fmt.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())
	} else {
		return &apigrpc.DebugReply{
			Response: fmt.Sprintf("%s-%s", request.GetProviderID(), nodeGroup.NodeGroupIdentifier),
		}, nil
	}
}

// Nodes returns a list of all nodes that belong to this node group.
// It is required that Instance objects returned by this method have Id field set.
// Other fields are optional.
func (s *grpcServerApp) Nodes(ctx context.Context, request *apigrpc.NodeGroupServiceRequest) (*apigrpc.NodesReply, error) {
	glog.Debugf("Call server Nodes: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if nodeGroup, err := s.appServer.getNodeGroup(request.GetNodeGroupID()); err != nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())

		return &apigrpc.NodesReply{
			Response: &apigrpc.NodesReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: fmt.Sprintf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID()),
				},
			},
		}, nil
	} else {

		instances := make([]*apigrpc.Instance, 0, nodeGroup.targetSize())

		for _, node := range nodeGroup.AllNodes() {
			status := apigrpc.InstanceState_STATE_UNDEFINED

			switch node.State {
			case AutoScalerServerNodeStateRunning:
				status = apigrpc.InstanceState_STATE_RUNNING
			case AutoScalerServerNodeStateCreating:
				status = apigrpc.InstanceState_STATE_BEING_CREATED
			case AutoScalerServerNodeStateDeleted:
				status = apigrpc.InstanceState_STATE_BEING_DELETED
			}

			instances = append(instances, &apigrpc.Instance{
				Id: node.generateProviderID(),
				Status: &apigrpc.InstanceStatus{
					State:     status,
					ErrorInfo: nil,
				},
			})
		}

		return &apigrpc.NodesReply{
			Response: &apigrpc.NodesReply_Instances{
				Instances: &apigrpc.Instances{
					Items: instances,
				},
			},
		}, nil
	}
}

// TemplateNodeInfo returns a schedulercache.NodeInfo structure of an empty
// (as if just started) node. This will be used in scale-up simulations to
// predict what would a new node look like if a node group was expanded. The returned
// NodeInfo is expected to have a fully populated Node object, with all of the labels,
// capacity and allocatable information as well as all pods that are started on
// the node by default, using manifest (most likely only kube-proxy). Implementation optional.
func (s *grpcServerApp) TemplateNodeInfo(ctx context.Context, request *apigrpc.NodeGroupServiceRequest) (*apigrpc.TemplateNodeInfoReply, error) {
	glog.Debugf("Call server TemplateNodeInfo: %v", request)

	if s.appServer.configuration.Optionals != nil && s.appServer.configuration.Optionals.TemplateNodeInfo {
		return nil, fmt.Errorf(constantes.ErrNotImplemented)
	}

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if nodeGroup, err := s.appServer.getNodeGroup(request.GetNodeGroupID()); err != nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())

		return &apigrpc.TemplateNodeInfoReply{
			Response: &apigrpc.TemplateNodeInfoReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: fmt.Sprintf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID()),
				},
			},
		}, nil
	} else {

		labels := types.MergeKubernetesLabel(nodeGroup.NodeLabels, nodeGroup.SystemLabels)
		annotations := types.KubernetesLabel{
			constantes.AnnotationNodeGroupName:        request.GetNodeGroupID(),
			constantes.AnnotationScaleDownDisabled:    "false",
			constantes.AnnotationNodeAutoProvisionned: "true",
			constantes.AnnotationNodeManaged:          "false",
		}

		node := &apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: apiv1.NodeSpec{
				Unschedulable: false,
			},
		}

		return &apigrpc.TemplateNodeInfoReply{
			Response: &apigrpc.TemplateNodeInfoReply_NodeInfo{NodeInfo: &apigrpc.NodeInfo{
				Node: utils.ToJSON(node),
			}},
		}, nil
	}
}

// Exist checks if the node group really exists on the cloud provider side. Allows to tell the
// theoretical node group from the real one. Implementation required.
func (s *grpcServerApp) Exist(ctx context.Context, request *apigrpc.NodeGroupServiceRequest) (*apigrpc.ExistReply, error) {
	glog.Debugf("Call server Exist: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	nodeGroup, _ := s.appServer.getNodeGroup(request.GetNodeGroupID())

	return &apigrpc.ExistReply{
		Exists: nodeGroup != nil,
	}, nil
}

// Create creates the node group on the cloud provider side. Implementation optional.
func (s *grpcServerApp) Create(ctx context.Context, request *apigrpc.NodeGroupServiceRequest) (*apigrpc.CreateReply, error) {
	glog.Debugf("Call server Create: %v", request)

	if s.appServer.configuration.Optionals != nil && s.appServer.configuration.Optionals.Create {
		return nil, fmt.Errorf(constantes.ErrNotImplemented)
	}

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	nodeGroup, err := s.appServer.createNodeGroup(request.GetNodeGroupID())

	if err != nil {
		glog.Errorf(constantes.ErrUnableToCreateNodeGroup, request.GetNodeGroupID(), err)

		return &apigrpc.CreateReply{
			Response: &apigrpc.CreateReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: err.Error(),
				},
			},
		}, nil
	}

	return &apigrpc.CreateReply{
		Response: &apigrpc.CreateReply_NodeGroup{
			NodeGroup: &apigrpc.NodeGroup{
				Id: nodeGroup.NodeGroupIdentifier,
			},
		},
	}, nil
}

// Delete deletes the node group on the cloud provider side.
// This will be executed only for autoprovisioned node groups, once their size drops to 0.
// Implementation optional.
func (s *grpcServerApp) Delete(ctx context.Context, request *apigrpc.NodeGroupServiceRequest) (*apigrpc.DeleteReply, error) {
	glog.Debugf("Call server Delete: %v", request)

	if s.appServer.configuration.Optionals != nil && s.appServer.configuration.Optionals.Delete {
		return nil, fmt.Errorf(constantes.ErrNotImplemented)
	}

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	err := s.appServer.deleteNodeGroup(request.GetNodeGroupID())

	if err != nil {
		glog.Errorf(constantes.ErrUnableToDeleteNodeGroup, request.GetNodeGroupID(), err)
		return &apigrpc.DeleteReply{
			Error: &apigrpc.Error{
				Code:   constantes.CloudProviderError,
				Reason: err.Error(),
			},
		}, nil
	}

	return &apigrpc.DeleteReply{
		Error: nil,
	}, nil
}

// Autoprovisioned returns true if the node group is autoprovisioned. An autoprovisioned group
// was created by CA and can be deleted when scaled to 0.
func (s *grpcServerApp) Autoprovisioned(ctx context.Context, request *apigrpc.NodeGroupServiceRequest) (*apigrpc.AutoprovisionedReply, error) {
	glog.Debugf("Call server Autoprovisioned: %v", request)

	var b bool

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	if ng, _ := s.appServer.getNodeGroup(request.GetNodeGroupID()); ng != nil {
		b = ng.AutoProvision
	}

	return &apigrpc.AutoprovisionedReply{
		Autoprovisioned: b,
	}, nil
}

// Belongs returns true if the given node belongs to the NodeGroup.
func (s *grpcServerApp) Belongs(ctx context.Context, request *apigrpc.BelongsRequest) (*apigrpc.BelongsReply, error) {
	glog.Debugf("Call server Belongs: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	node, err := utils.NodeFromJSON(request.GetNode())

	if err != nil {
		glog.Errorf(constantes.ErrCantUnmarshallNodeWithReason, request.GetNode(), err)

		return &apigrpc.BelongsReply{
			Response: &apigrpc.BelongsReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: err.Error(),
				},
			},
		}, nil
	}

	if nodegroupName, found := node.Annotations[constantes.AnnotationNodeGroupName]; found {
		var nodeGroup *AutoScalerServerNodeGroup

		if nodeGroup, err = s.appServer.getNodeGroup(nodegroupName); err != nil {
			return &apigrpc.BelongsReply{
				Response: &apigrpc.BelongsReply_Error{
					Error: &apigrpc.Error{
						Code:   constantes.CloudProviderError,
						Reason: err.Error(),
					},
				},
			}, nil
		}

		return &apigrpc.BelongsReply{
			Response: &apigrpc.BelongsReply_Belongs{
				Belongs: nodeGroup.findNamedNode(node.Name) != nil,
			},
		}, nil
	}

	return &apigrpc.BelongsReply{
		Response: &apigrpc.BelongsReply_Error{
			Error: &apigrpc.Error{
				Code:   constantes.CloudProviderError,
				Reason: fmt.Sprintf("Node annotation[%s] is empty", constantes.AnnotationNodeGroupName),
			},
		},
	}, nil
}

func (s *grpcServerApp) GetOptions(ctx context.Context, request *apigrpc.GetOptionsRequest) (*apigrpc.GetOptionsReply, error) {
	glog.Debugf("Call server GetOptions: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	pbDefaults := request.GetDefaults()

	if pbDefaults == nil {
		return &apigrpc.GetOptionsReply{
			Response: &apigrpc.GetOptionsReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: "request fields were nil",
				},
			},
		}, nil
	}

	if nodeGroup, err := s.appServer.getNodeGroup(request.GetNodeGroupID()); err != nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID())

		return &apigrpc.GetOptionsReply{
			Response: &apigrpc.GetOptionsReply_Error{
				Error: &apigrpc.Error{
					Code:   constantes.CloudProviderError,
					Reason: fmt.Sprintf(constantes.ErrNodeGroupNotFound, request.GetNodeGroupID()),
				},
			},
		}, nil
	} else {

		defaults := &apigrpc.AutoscalingOptions{
			ScaleDownUtilizationThreshold:    pbDefaults.GetScaleDownGpuUtilizationThreshold(),
			ScaleDownGpuUtilizationThreshold: pbDefaults.GetScaleDownGpuUtilizationThreshold(),
			ScaleDownUnneededTime:            pbDefaults.GetScaleDownUnneededTime(),
			ScaleDownUnreadyTime:             pbDefaults.GetScaleDownUnneededTime(),
		}

		opts, err := nodeGroup.GetOptions(defaults)

		if err != nil {
			return &apigrpc.GetOptionsReply{
				Response: &apigrpc.GetOptionsReply_Error{
					Error: &apigrpc.Error{
						Code:   constantes.CloudProviderError,
						Reason: err.Error(),
					},
				},
			}, nil
		}

		return &apigrpc.GetOptionsReply{
			Response: &apigrpc.GetOptionsReply_NodeGroupAutoscalingOptions{
				NodeGroupAutoscalingOptions: &apigrpc.AutoscalingOptions{
					ScaleDownUtilizationThreshold:    opts.ScaleDownUtilizationThreshold,
					ScaleDownGpuUtilizationThreshold: opts.ScaleDownGpuUtilizationThreshold,
					ScaleDownUnneededTime:            opts.ScaleDownUnneededTime,
					ScaleDownUnreadyTime:             opts.ScaleDownUnreadyTime,
				},
			},
		}, nil
	}
}

// NodePrice returns a price of running the given node for a given period of time.
// All prices returned by the structure should be in the same currency.
func (s *grpcServerApp) NodePrice(ctx context.Context, request *apigrpc.NodePriceRequest) (*apigrpc.NodePriceReply, error) {
	glog.Debugf("Call server NodePrice: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	return &apigrpc.NodePriceReply{
		Response: &apigrpc.NodePriceReply_Price{
			Price: s.appServer.configuration.NodePrice,
		},
	}, nil
}

// PodPrice returns a theoretical minimum price of running a pod for a given
// period of time on a perfectly matching machine.
func (s *grpcServerApp) PodPrice(ctx context.Context, request *apigrpc.PodPriceRequest) (*apigrpc.PodPriceReply, error) {
	glog.Debugf("Call server PodPrice: %v", request)

	if s.isCallDenied(request) {
		glog.Errorf(constantes.ErrMismatchingProvider)
		return nil, fmt.Errorf(constantes.ErrMismatchingProvider)
	}

	return &apigrpc.PodPriceReply{
		Response: &apigrpc.PodPriceReply_Price{
			Price: s.appServer.configuration.PodPrice,
		},
	}, nil
}
