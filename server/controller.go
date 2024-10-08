/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"time"

	glog "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	uid "k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	nodelisters "k8s.io/client-go/listers/core/v1"

	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/client"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	nodemanager "github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha2"
	schemeclientset "github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/generated/clientset/versioned/scheme"
	informers "github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/generated/informers/externalversions"
	listers "github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/generated/listers/nodemanager/v1alpha2"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
)

// resourceLimitsStatus describe the limit status when controller try to add a new node
type resourceLimitsStatus int32

const (
	resourceLimitsNice = iota
	resourceLimitsMaxCpuReached
	resourceLimitsMaxMemoryReached
	resourceLimitsMaxNodesReached
)

const controllerAgentName = "nodemanager-controller"
const warnNodeDeletionErr = "an error occured during node deletion of %s, reason: %v"

const (
	FailedEvent  = "Failed"
	SuccessEvent = "Success"
	WarningEvent = "Warning"
	ErrorEvent   = "Error"
)

// Controller is the controller implementation for Foo resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	client            client.ClientGenerator
	nodesLister       nodelisters.NodeLister
	nodesSynced       cache.InformerSynced
	managedNodeLister listers.ManagedNodeLister
	managedNodeSynced cache.InformerSynced
	application       applicationInterface

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder

	stopCh <-chan struct{}
}

// NewController returns a new sample controller
func NewController(application applicationInterface, stopCh <-chan struct{}) (controller *Controller, err error) {

	client := application.client()
	kubeclientset, _ := client.KubeClient()
	nodeManagerClientset, _ := client.NodeManagerClient()

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeclientset, time.Second*30)
	managedNodeInformerFactory := informers.NewSharedInformerFactory(nodeManagerClientset, time.Second*30)

	nodesInformer := kubeInformerFactory.Core().V1().Nodes()
	managedNodeInformer := managedNodeInformerFactory.Nodemanager().V1alpha2().ManagedNodes()

	// Create event broadcaster
	// Add sample-controller types to the default Kubernetes Scheme so Events can be
	// logged for sample-controller types.
	utilruntime.Must(schemeclientset.AddToScheme(scheme.Scheme))
	glog.Debugf("Creating event broadcaster")

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})

	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller = &Controller{
		client:            client,
		stopCh:            stopCh,
		application:       application,
		nodesLister:       nodesInformer.Lister(),
		nodesSynced:       nodesInformer.Informer().HasSynced,
		managedNodeLister: managedNodeInformer.Lister(),
		managedNodeSynced: managedNodeInformer.Informer().HasSynced,
		workqueue:         workqueue.NewNamedRateLimitingQueue(workqueue.NewItemFastSlowRateLimiter(5*time.Second, 30*time.Second, 10), "ManagedNodes"),
		recorder:          recorder,
	}

	glog.Info("Setting up event handlers")

	// Set up an event handler for when ManagedNode resources change
	managedNodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueManagedNode,
		UpdateFunc: func(old, new any) {
			controller.enqueueManagedNode(new)
		},
		DeleteFunc: controller.deleteManagedNode,
	})

	// Set up an event handler for when Node resources change
	nodesInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleNode,
		UpdateFunc: func(old, new any) {
			newDepl := new.(*corev1.Node)
			oldDepl := old.(*corev1.Node)

			if newDepl.ResourceVersion != oldDepl.ResourceVersion {
				controller.handleNode(new)
			}
		},
		DeleteFunc: controller.handleNode,
	})

	if err = controller.CreateCRD(); err == nil {
		// notice that there is no need to run Start methods in a separate goroutine. (i.e. go kubeInformerFactory.Start(stopCh)
		// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
		kubeInformerFactory.Start(stopCh)
		cache.WaitForCacheSync(stopCh, controller.nodesSynced)
		managedNodeInformerFactory.Start(stopCh)
		cache.WaitForCacheSync(stopCh, controller.managedNodeSynced)
	}

	return controller, err
}

func (c *Controller) waitCRDAccepted() error {
	apiextensionClientset, _ := c.client.ApiExtentionClient()

	err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, 10*time.Second, true, func(context.Context) (bool, error) {
		if crd, err := apiextensionClientset.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), nodemanager.FullCRDName, metav1.GetOptions{}); err == nil {
			accepted := false
			etablished := false

			if len(crd.Status.Conditions) == 0 {
				return false, nil
			}

			for _, condition := range crd.Status.Conditions {
				switch condition.Type {
				case apiextensionv1.NamesAccepted:
					accepted = condition.Status == apiextensionv1.ConditionTrue
				case apiextensionv1.Established:
					etablished = condition.Status == apiextensionv1.ConditionTrue
				}
			}

			if accepted {
				return etablished, nil
			} else {
				return false, fmt.Errorf("CRD is not accepted")
			}
		} else {
			return false, err
		}
	})

	return err
}

func (c *Controller) CreateCRD() error {
	//var TRUE bool = true
	var err error

	var XPreserveUnknownFields *bool = nil

	apiextensionClientset, _ := c.client.ApiExtentionClient()

	crd := &apiextensionv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: nodemanager.FullCRDName},
		Spec: apiextensionv1.CustomResourceDefinitionSpec{
			Group:                 nodemanager.GroupName,
			Scope:                 apiextensionv1.ClusterScoped,
			PreserveUnknownFields: false,
			Versions: []apiextensionv1.CustomResourceDefinitionVersion{
				{
					Name:    nodemanager.GroupVersion,
					Served:  true,
					Storage: true,
					Subresources: &apiextensionv1.CustomResourceSubresources{
						Status: &apiextensionv1.CustomResourceSubresourceStatus{},
					},
					Schema: &apiextensionv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionv1.JSONSchemaProps{
							Type:                   "object",
							XPreserveUnknownFields: XPreserveUnknownFields,
							Properties: map[string]apiextensionv1.JSONSchemaProps{
								"status": {
									Type:                   "object",
									XPreserveUnknownFields: XPreserveUnknownFields,
									Properties: map[string]apiextensionv1.JSONSchemaProps{
										"lastUpdateTime": {
											Type: "string",
										},
										"nodename": {
											Type: "string",
										},
										"instancename": {
											Type: "string",
										},
										"instanceid": {
											Type: "string",
										},
										"message": {
											Type: "string",
										},
										"reason": {
											Type: "string",
										},
										"code": {
											Type: "integer",
										},
									},
								},
								"spec": {
									Type:                   "object",
									XPreserveUnknownFields: XPreserveUnknownFields,
									Properties: map[string]apiextensionv1.JSONSchemaProps{
										"nodegroup": {
											Type:        "string",
											Description: "Nodegroup to join",
										},
										"controlPlane": {
											Type:        "boolean",
											Description: "Tell if the managed is a control plane",
										},
										"allowDeployment": {
											Type:        "boolean",
											Description: "Tell the managed as control plane allows pods deployment",
										},
										"instanceType": {
											Type:        "string",
											Description: "Instance type",
											Default: &apiextensionv1.JSON{
												Raw: []byte("\"t2.micro\""),
											},
										},
										"diskSizeInMB": {
											Type:        "integer",
											Description: "The disk size in MiB",
											Default: &apiextensionv1.JSON{
												Raw: []byte("10240"),
											},
										},
										"diskType": {
											Type:        "string",
											Description: "Disk type on AWS",
											Default: &apiextensionv1.JSON{
												Raw: []byte("\"gp3\""),
											},
										},
										"labels": {
											Type:        "array",
											Description: "Apprend node labels",
											Items: &apiextensionv1.JSONSchemaPropsOrArray{
												Schema: &apiextensionv1.JSONSchemaProps{
													Type: "string",
												},
											},
										},
										"annotations": {
											Type:        "array",
											Description: "Append node annotations",
											Items: &apiextensionv1.JSONSchemaPropsOrArray{
												Schema: &apiextensionv1.JSONSchemaProps{
													Type: "string",
												},
											},
										},
										"network": {
											Type:                   "object",
											Description:            "Describe network interface for this node, allow to override configuration",
											XPreserveUnknownFields: XPreserveUnknownFields,
											Properties: map[string]apiextensionv1.JSONSchemaProps{
												"openstack": {
													Type: "array",
													Items: &apiextensionv1.JSONSchemaPropsOrArray{
														Schema: &apiextensionv1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionv1.JSONSchemaProps{
																"network": {
																	Type:        "string",
																	Description: "Network name for vsphere or local interface for multipas and desktop, vmnetX...",
																},
																"dhcp": {
																	Type:        "boolean",
																	Description: "Tell if interface use dhcp to obtain address",
																	Default: &apiextensionv1.JSON{
																		Raw: []byte("true"),
																	},
																},
																"address": {
																	Type:        "string",
																	Description: "Valid ip v4 address or DHCP or NONE",
																},
																"netmask": {
																	Type:        "string",
																	Description: "Netmask in format xxx.xxx.xxx.xxx",
																},
															},
														},
													},
												},
												"cloudstack": {
													Type: "array",
													Items: &apiextensionv1.JSONSchemaPropsOrArray{
														Schema: &apiextensionv1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionv1.JSONSchemaProps{
																"network": {
																	Type:        "string",
																	Description: "Network name for vsphere or local interface for multipass and desktop, vmnetX...",
																},
																"dhcp": {
																	Type:        "boolean",
																	Description: "Tell if interface use dhcp to obtain address",
																	Default: &apiextensionv1.JSON{
																		Raw: []byte("true"),
																	},
																},
																"address": {
																	Type:        "string",
																	Description: "Valid ip v4 address or DHCP or NONE",
																},
																"netmask": {
																	Type:        "string",
																	Description: "Netmask in format xxx.xxx.xxx.xxx",
																},
															},
														},
													},
												},
												"vmware": {
													Type: "array",
													Items: &apiextensionv1.JSONSchemaPropsOrArray{
														Schema: &apiextensionv1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionv1.JSONSchemaProps{
																"network": {
																	Type:        "string",
																	Description: "Network name for vsphere or local interface for multipass and desktop, vmnetX...",
																},
																"device": {
																	Type:        "string",
																	Description: "Adapter type",
																	Default: &apiextensionv1.JSON{
																		Raw: []byte("\"vmxnet3\""),
																	},
																},
																"dhcp": {
																	Type:        "boolean",
																	Description: "Tell if interface use dhcp to obtain address",
																	Default: &apiextensionv1.JSON{
																		Raw: []byte("true"),
																	},
																},
																"use-dhcp-routes": {
																	Type:        "boolean",
																	Description: "Tell if we use routes declared by dhcp server",
																	Default: &apiextensionv1.JSON{
																		Raw: []byte("true"),
																	},
																},
																"mac-address": {
																	Type:        "string",
																	Description: "Give a mac address, empty to let hypervisor manage mac address or generate to generate mac address by controller",
																},
																"address": {
																	Type:        "string",
																	Description: "Valid ip v4 address or DHCP or NONE",
																},
																"netmask": {
																	Type:        "string",
																	Description: "Netmask in format xxx.xxx.xxx.xxx",
																},
																"routes": {
																	Type:        "array",
																	Description: "Override network routes",
																	Items: &apiextensionv1.JSONSchemaPropsOrArray{
																		Schema: &apiextensionv1.JSONSchemaProps{
																			Type: "object",
																			Properties: map[string]apiextensionv1.JSONSchemaProps{
																				"to": {
																					Type: "string",
																				},
																				"via": {
																					Type: "string",
																				},
																				"metric": {
																					Type: "integer",
																				},
																			},
																		},
																	},
																},
															},
														},
													},
												},
												"multipass": {
													Type: "array",
													Items: &apiextensionv1.JSONSchemaPropsOrArray{
														Schema: &apiextensionv1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionv1.JSONSchemaProps{
																"network": {
																	Type:        "string",
																	Description: "Network name for vsphere or local interface for multipass and desktop, vmnetX...",
																},
																"dhcp": {
																	Type:        "boolean",
																	Description: "Tell if interface use dhcp to obtain address",
																	Default: &apiextensionv1.JSON{
																		Raw: []byte("true"),
																	},
																},
																"use-dhcp-routes": {
																	Type:        "boolean",
																	Description: "Tell if we use routes declared by dhcp server",
																	Default: &apiextensionv1.JSON{
																		Raw: []byte("true"),
																	},
																},
																"mac-address": {
																	Type:        "string",
																	Description: "Give a mac address, empty to let hypervisor manage mac address or generate to generate mac address by controller",
																},
																"address": {
																	Type:        "string",
																	Description: "Valid ip v4 address or DHCP or NONE",
																},
																"netmask": {
																	Type:        "string",
																	Description: "Netmask in format xxx.xxx.xxx.xxx",
																},
																"routes": {
																	Type:        "array",
																	Description: "Override network routes",
																	Items: &apiextensionv1.JSONSchemaPropsOrArray{
																		Schema: &apiextensionv1.JSONSchemaProps{
																			Type: "object",
																			Properties: map[string]apiextensionv1.JSONSchemaProps{
																				"to": {
																					Type: "string",
																				},
																				"via": {
																					Type: "string",
																				},
																				"metric": {
																					Type: "integer",
																				},
																			},
																		},
																	},
																},
															},
														},
													},
												},
												"lxd": {
													Type: "array",
													Items: &apiextensionv1.JSONSchemaPropsOrArray{
														Schema: &apiextensionv1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionv1.JSONSchemaProps{
																"network": {
																	Type:        "string",
																	Description: "Network name for lxd",
																},
																"nic": {
																	Type:        "string",
																	Description: "Interface name in container",
																},
																"dhcp": {
																	Type:        "boolean",
																	Description: "Tell if interface use dhcp to obtain address",
																	Default: &apiextensionv1.JSON{
																		Raw: []byte("true"),
																	},
																},
																"address": {
																	Type:        "string",
																	Description: "Valid ip v4 address or DHCP or NONE",
																},
															},
														},
													},
												},
												"eni": {
													Type:        "object",
													Description: "Create or use existing ENI",
													Properties: map[string]apiextensionv1.JSONSchemaProps{
														"subnetID": {
															Type:        "string",
															Description: "Existing VPC subnet identifier",
														},
														"securityGroup": {
															Type:        "string",
															Description: "Existing security group identifier",
														},
														"networkInterfaceID": {
															Type:        "string",
															Description: "Existing ENI identifier",
														},
														"privateAddress": {
															Type:        "string",
															Description: "Set ip v4 address for this interface",
														},
														"publicIP": {
															Type:        "boolean",
															Description: "Use a public ip v4 address",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			Names: apiextensionv1.CustomResourceDefinitionNames{
				Singular:   nodemanager.CRDSingular,
				Plural:     nodemanager.CRDPlural,
				Kind:       reflect.TypeOf(v1alpha2.ManagedNode{}).Name(),
				ShortNames: []string{nodemanager.CRDShortName},
			},
		},
	}

	if _, err = apiextensionClientset.ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			err = nil
		}
	} else {
		err = c.waitCRDAccepted()
	}

	return err
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run() error {
	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting node manager controller")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")

	if ok := cache.WaitForCacheSync(c.stopCh, c.nodesSynced, c.managedNodeSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	go func() {
		defer utilruntime.HandleCrash()
		defer c.workqueue.ShutDown()

		glog.Info("Starting workers")

		go wait.Until(c.runWorker, time.Second, c.stopCh)

		glog.Info("Started workers")
		<-c.stopCh
		glog.Info("Shutting down workers")
	}()

	return nil
}

func (c *Controller) newControllerRef(owner metav1.Object) *metav1.OwnerReference {
	blockOwnerDeletion := false
	isController := true

	return &metav1.OwnerReference{
		APIVersion:         v1alpha2.SchemeGroupVersionKind.GroupVersion().String(),
		Kind:               v1alpha2.SchemeGroupVersionKind.Kind,
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		BlockOwnerDeletion: &blockOwnerDeletion,
		Controller:         &isController,
	}
}

// create vm node for each cdr managed node
func (c *Controller) startManagedNodes(managedNodesByUID map[uid.UID]string, nodesInCreationByNodegroup map[string][]*AutoScalerServerNode) {
	kubeclientset, _ := c.client.KubeClient()

	for nodeGroupName, nodesList := range nodesInCreationByNodegroup {
		if nodeGroup, err := c.application.getNodeGroup(nodeGroupName); err == nil {
			if _, err := nodeGroup.createNodes(c.client, nodesList); err != nil {
				glog.Errorf("could not create all nodes, %v", err)
			}

			for _, node := range nodesList {
				if key, found := managedNodesByUID[node.CRDUID]; found {
					if managedNode, err := c.getManagedNodeFromKey(key); err == nil {
						newStatus := managedNode.Status
						key := c.generateKey(managedNode)

						if node.State == AutoScalerServerNodeStateRunning {
							newStatus.Code = nodemanager.StatusManagedNodeCreated
							newStatus.Reason = nodemanager.StatusManagedNodeReason(nodemanager.StatusManagedNodeCreated)
							newStatus.Message = fmt.Sprintf("Node %s creation successful", node.NodeName)
							newStatus.NodeName = node.NodeName
							newStatus.InstanceName = node.InstanceName
							newStatus.InstanceID = node.VMUUID

							c.recorder.Event(managedNode, corev1.EventTypeNormal, SuccessEvent, newStatus.Message)

							cache.WaitForCacheSync(c.stopCh, c.nodesSynced, c.managedNodeSynced)

							if workerNode, err := c.client.GetNode(node.NodeName); err != nil {
								err = fmt.Errorf("unable to find core node %s, reason: %v", newStatus.NodeName, err)
								glog.Error(err.Error())
								c.recorder.Event(managedNode, corev1.EventTypeWarning, ErrorEvent, err.Error())
							} else {
								ownerReferences := workerNode.GetOwnerReferences()
								owerRef := c.newControllerRef(managedNode)

								ownerReferences = append(ownerReferences, *owerRef)
								workerNode.SetOwnerReferences(ownerReferences)

								if workerNode, err = kubeclientset.CoreV1().Nodes().Update(context.TODO(), workerNode, metav1.UpdateOptions{}); err != nil {
									err = fmt.Errorf("failed to update owner reference for core node: %s, reason: %v", workerNode.GetName(), err)
									glog.Error(err.Error())

									c.recorder.Event(managedNode, corev1.EventTypeWarning, ErrorEvent, err.Error())
								}
							}

						} else {
							newStatus.NodeName = node.NodeName
							newStatus.InstanceName = node.NodeName
							newStatus.Code = nodemanager.StatusManagedNodeCreationFailed
							newStatus.Reason = nodemanager.StatusManagedNodeReason(nodemanager.StatusManagedNodeCreationFailed)
							newStatus.Message = fmt.Sprintf("Node %s creation failed", node.NodeName)

							c.recorder.Event(managedNode, corev1.EventTypeWarning, FailedEvent, newStatus.Message)
						}

						if err := c.updateManagedNodeStatus(managedNode, newStatus); err != nil {
							glog.Errorf(constantes.ErrUpdateStatusNodeFailed, key, err)
						}
					} else {
						glog.Errorf("managed node by key: %s is not found, reason: %v", key, err)
					}
				} else {
					glog.Errorf("managed node by UID: %s is not found", node.CRDUID)
				}
			}
		}
	}
}

// Find managed node deleted at launch
func (c *Controller) findManagedNodeDeleted() {
	deleted := 0

	if nodeList, err := c.nodesLister.List(labels.Everything()); err == nil {
		for _, nodeInfo := range nodeList {
			if ownerRef := metav1.GetControllerOf(nodeInfo); ownerRef != nil {
				// If this object is not owned by a ManagedNode, we should not do anything more with it.
				if ownerRef.Kind == v1alpha2.SchemeGroupVersionKind.Kind {
					if _, err := c.getManagedNodeFromKey(ownerRef.Name); apierrors.IsNotFound(err) {
						if nodegroup, found := nodeInfo.Annotations[constantes.AnnotationNodeGroupName]; found {
							if ng, err := c.application.getNodeGroup(nodegroup); err == nil {
								if node, err := ng.findNodeByCRDUID(ownerRef.UID); err == nil {
									if err = ng.deleteNode(c.client, node); err != nil {
										glog.Warnf(warnNodeDeletionErr, node.NodeName, err)
									}

									glog.Infof(constantes.InfoManagedNodeIsDeleted, ownerRef.Name, node.NodeName)

									deleted++
								} else {
									glog.Errorf(constantes.ErrNodeNotFoundInNodeGroup, ownerRef.UID, nodegroup)
								}
							} else {
								glog.Errorf(constantes.ErrNodeGroupNotFound, nodegroup)
							}
						}
					}
				}
			}
		}
	}

	// For each managed node find kubernetes nodes deleted
	if managedNodeList, err := c.managedNodeLister.List(labels.Everything()); err == nil {
		// Each ManagedNode
		for _, managedNode := range managedNodeList {
			if managedNode.Status.Code == nodemanager.StatusManagedNodeCreated && len(managedNode.Status.NodeName) > 0 {
				// Try to find node
				if _, err := c.nodesLister.Get(managedNode.Status.NodeName); apierrors.IsNotFound(err) {

					// MArk ManagedNode as deleted
					newStatus := managedNode.Status

					newStatus.Code = nodemanager.StatusManagedNodeDeleted
					newStatus.Message = fmt.Sprintf("the node %s hosted by instance %s was deleted", newStatus.NodeName, newStatus.InstanceName)
					newStatus.Reason = nodemanager.StatusManagedNodeReason(nodemanager.StatusManagedNodeDeleted)

					c.recorder.Event(managedNode, corev1.EventTypeWarning, WarningEvent, newStatus.Message)

					glog.Infof("node %s hosted by instance %s owned by managed node %s was deleted", newStatus.NodeName, newStatus.InstanceName, c.generateKey(managedNode))

					// Try to find the node group and delete node
					if ng, err := c.application.getNodeGroup(managedNode.GetNodegroup()); err == nil {
						if node, err := ng.findNodeByCRDUID(managedNode.GetUID()); err == nil {
							if err = ng.deleteNode(c.client, node); err != nil {
								glog.Warnf(warnNodeDeletionErr, node.NodeName, err)
							}

							deleted++
						} else {
							glog.Errorf(constantes.ErrNodeNotFoundInNodeGroup, managedNode.Status.NodeName, managedNode.GetNodegroup())
						}
					} else {
						glog.Errorf(constantes.ErrNodeGroupNotFound, managedNode.GetNodegroup())
					}
				}
			}
		}
	}

	if deleted > 0 {
		c.application.syncState()
	}
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	c.findManagedNodeDeleted()

	for {
		if managedNodesByUID, nodesInCreationByNodegroup, ok := c.processAllItems(); ok {
			if len(nodesInCreationByNodegroup) > 0 {

				cache.WaitForCacheSync(c.stopCh, c.nodesSynced, c.managedNodeSynced)

				c.startManagedNodes(managedNodesByUID, nodesInCreationByNodegroup)

				c.application.syncState()
			}

			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}
}

// processAllItems will read a all items off the workqueue and attempt to process it, by calling the syncHandler.
func (c *Controller) processAllItems() (map[uid.UID]string, map[string][]*AutoScalerServerNode, bool) {
	managedNodesByUID := make(map[uid.UID]string)
	nodesInCreationByNodegroup := make(map[string][]*AutoScalerServerNode)
	recycledToQueue := make([]string, 0, c.workqueue.Len())

	for c.workqueue.Len() > 0 {
		var ok bool
		var err error
		var key string

		obj, shutdown := c.workqueue.Get()

		if shutdown {
			return managedNodesByUID, nodesInCreationByNodegroup, false
		}

		if key, ok = obj.(string); !ok {
			glog.Errorf("expected string in workqueue but got %#v", obj)
			c.workqueue.Forget(obj)
		} else if ok, err = c.handleManagedNode(key, managedNodesByUID, nodesInCreationByNodegroup); err != nil && ok {
			recycledToQueue = append(recycledToQueue, key)
			glog.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		} else {
			c.workqueue.Forget(obj)
		}

		c.workqueue.Done(obj)
	}

	// Recycle delayed item
	for _, obj := range recycledToQueue {
		c.workqueue.AddRateLimited(obj)
	}

	return managedNodesByUID, nodesInCreationByNodegroup, true
}

// check if the new node doesn't break cluster limits
func (c *Controller) checkRessourceLimits(crd *v1alpha2.ManagedNode, machineSpec *providers.MachineCharacteristic) resourceLimitsStatus {
	var currentAllocatedMemorySize int64 = 0
	var currentAllocatedCores = 0

	resourceLimiter := c.application.getResourceLimiter()
	status := resourceLimitsNice

	if glog.GetLevel() > glog.InfoLevel {
		fmt.Fprintf(os.Stderr, "resourceLimiter: %s", utils.ToYAML(resourceLimiter))
	}

	if nodeList, err := c.nodesLister.List(labels.Everything()); err == nil {
		maxNodes := resourceLimiter.GetMaxValue(constantes.ResourceNameNodes, 32)

		if maxNodes < len(nodeList)+1 {
			glog.Warnf("Max nodes limit reached for managed node: %s, max expected: %d, needed: %d",
				crd.GetName(),
				maxNodes,
				len(nodeList)+1)

			status = resourceLimitsMaxNodesReached
		} else {
			maxCores := resourceLimiter.GetMaxValue(constantes.ResourceNameCores, 128)
			maxMemory := resourceLimiter.GetMaxValue64(constantes.ResourceNameMemory, 1024*1024*1024)

			for _, node := range nodeList {
				currentCores := node.Status.Capacity.Cpu()
				currentMemory := node.Status.Capacity.Memory()

				currentAllocatedMemorySize += currentMemory.Value()
				currentAllocatedCores += int(currentCores.Value())
			}

			if maxCores < machineSpec.Vcpu+currentAllocatedCores {
				glog.Warnf("Max cpus limit reached for managed node: %s, max expected: %d, needed: %d",
					crd.GetName(),
					maxCores,
					machineSpec.Vcpu+currentAllocatedCores)

				status = resourceLimitsMaxCpuReached
			} else if maxMemory < int64(machineSpec.Memory)+currentAllocatedMemorySize {
				glog.Warnf("Max memory limit reached for managed node: %s, max expected: %s, needed: %s",
					crd.GetName(),
					resource.NewQuantity(maxMemory, resource.BinarySI).String(),
					resource.NewQuantity(int64(machineSpec.Memory)+currentAllocatedMemorySize, resource.BinarySI).String())

				status = resourceLimitsMaxMemoryReached
			}
		}
	} else {
		glog.Errorf("can't check limit for managed node: %s, reason: %v", crd.GetName(), err)
	}

	return resourceLimitsStatus(status)
}

// handle the CRD managed node, create/delete/update...
func (c *Controller) handleManagedNode(key string, managedNodesByUID map[uid.UID]string, nodesInCreationByNodegroup map[string][]*AutoScalerServerNode) (bool, error) {
	var err error
	var updateErr error
	var managedNode *v1alpha2.ManagedNode
	var nodeGroup *AutoScalerServerNodeGroup
	var node *AutoScalerServerNode

	recycled := false

	if managedNode, err = c.getManagedNodeFromKey(key); err != nil {

		// The ManagedNode resource may no longer exist, in which case we delete associated Node.
		if apierrors.IsNotFound(err) {
			// Delete node eventually (probably dead code)
			if managedNode != nil && managedNode.Status.Code == nodemanager.StatusManagedNodeCreated {
				if nodeGroup, err = c.application.getNodeGroup(managedNode.GetNodegroup()); err == nil {
					if node, err = nodeGroup.findNodeByCRDUID(managedNode.GetUID()); err == nil {
						glog.Infof(constantes.InfoManagedNodeIsDeleted, key, node.NodeName)

						if err = nodeGroup.deleteNode(c.client, node); err != nil {
							glog.Warnf(warnNodeDeletionErr, node.NodeName, err)
						}

						c.application.syncState()
					} else {
						glog.Errorf(constantes.ErrNodeNotFoundInNodeGroup, managedNode.GetNodegroup(), managedNode.GetUID())
					}
				} else {
					glog.Errorf(constantes.ErrNodeGroupNotFound, managedNode.GetNodegroup())
				}
			}
		} else {
			glog.Errorf("ManagedNode '%s' in work queue no longer exists", key)
		}

		err = nil
	} else {
		nodegroup := managedNode.GetNodegroup()
		oldStatus := managedNode.Status
		newStatus := oldStatus

		managedNodesByUID[managedNode.GetUID()] = c.generateKey(managedNode)

		if nodeGroup, err = c.application.getNodeGroup(nodegroup); err != nil {
			glog.Errorf(constantes.ErrNodeGroupNotFound, nodegroup)

			if c.application.isNodegroupDiscovered() {
				newStatus.Code = nodemanager.StatusManagedNodeCreationFailed
				newStatus.Message = fmt.Sprintf(constantes.ErrNodeGroupNotFound, managedNode.GetNodegroup())
				newStatus.Reason = nodemanager.StatusManagedNodeReason(nodemanager.StatusManagedNodeCreationFailed)
			} else {
				newStatus.Code = nodemanager.StatusManagedNodeGroupNotFound
				newStatus.Message = fmt.Sprintf(constantes.ErrNodeGroupNotFound, managedNode.GetNodegroup())
				newStatus.Reason = nodemanager.StatusManagedNodeReason(nodemanager.StatusManagedNodeGroupNotFound)
			}

			if oldStatus.Code != newStatus.Code {
				if newStatus.Code == nodemanager.StatusManagedNodeGroupNotFound {
					c.recorder.Event(managedNode, corev1.EventTypeWarning, WarningEvent, newStatus.Message)
				} else {
					c.recorder.Event(managedNode, corev1.EventTypeWarning, ErrorEvent, newStatus.Message)
				}

				if updateErr = c.updateManagedNodeStatus(managedNode, newStatus); updateErr != nil {
					glog.Errorf(constantes.ErrUpdateStatusNodeFailed, key, updateErr)
				}
			}

		} else if node, _ = nodeGroup.findNodeByCRDUID(managedNode.GetUID()); node == nil {

			if oldStatus.Code == nodemanager.StatusManagedNodeNeedToCreated || oldStatus.Code == nodemanager.StatusManagedNodeGroupNotFound || oldStatus.Code == nodemanager.StatusManagedNodeNiceLimitReached {

				if machineSpec := c.application.getMachineType(managedNode.Spec.InstanceType); machineSpec == nil {
					newStatus.Code = nodemanager.StatusManagedNodeInstanceTypeNotRegistred
					newStatus.Reason = nodemanager.StatusManagedNodeReason(nodemanager.StatusManagedNodeInstanceTypeNotRegistred)
					newStatus.Message = fmt.Sprintf("managed node %s use unregistered instance type: %s", key, managedNode.Spec.InstanceType)

					glog.Infof(newStatus.Message)

					c.recorder.Eventf(managedNode, corev1.EventTypeWarning, ErrorEvent, newStatus.Message)

				} else if limitStatus := c.checkRessourceLimits(managedNode, machineSpec); limitStatus != resourceLimitsNice {

					// Push event only if the first
					if oldStatus.Code != nodemanager.StatusManagedNodeNiceLimitReached {

						switch limitStatus {
						case resourceLimitsMaxCpuReached:
							newStatus.Message = "Cpus resource limit reached"
						case resourceLimitsMaxMemoryReached:
							newStatus.Message = "Memory resource limit reached"
						case resourceLimitsMaxNodesReached:
							newStatus.Message = "Nodes resource limit reached"
						}

						newStatus.Code = nodemanager.StatusManagedNodeNiceLimitReached
						newStatus.Reason = nodemanager.StatusManagedNodeReason(nodemanager.StatusManagedNodeNiceLimitReached)

						glog.Infof("managed node %s break resource limits", key)

						c.recorder.Eventf(managedNode, corev1.EventTypeWarning, ErrorEvent, newStatus.Message)
					}

				} else if node, err = nodeGroup.addManagedNode(managedNode); err == nil {

					var nodesListByNodegroup []*AutoScalerServerNode
					var found bool

					// Create managedNode
					glog.Infof("create managed node %s in group: %s, node name: %s", key, node.NodeGroup, node.NodeName)

					if nodesListByNodegroup, found = nodesInCreationByNodegroup[managedNode.GetNodegroup()]; !found {
						nodesListByNodegroup = make([]*AutoScalerServerNode, 0, 5)
						nodesListByNodegroup = append(nodesListByNodegroup, node)
					} else {
						nodesListByNodegroup = append(nodesListByNodegroup, node)
					}

					nodesInCreationByNodegroup[managedNode.GetNodegroup()] = nodesListByNodegroup

					newStatus.NodeName = node.NodeName
					newStatus.InstanceName = node.InstanceName
					newStatus.Code = nodemanager.StatusManagedNodeCreation
					newStatus.Reason = nodemanager.StatusManagedNodeReason(nodemanager.StatusManagedNodeCreation)
					newStatus.Message = fmt.Sprintf("Node %s in creation", node.NodeName)

					c.recorder.Event(managedNode, corev1.EventTypeNormal, SuccessEvent, newStatus.Message)

				} else {

					glog.Errorf("creation managed node %s failed, reason: %v", key, err)

					newStatus.Code = nodemanager.StatusManagedNodeCreationFailed
					newStatus.Message = err.Error()
					newStatus.Reason = nodemanager.StatusManagedNodeReason(nodemanager.StatusManagedNodeCreationFailed)

					c.recorder.Eventf(managedNode, corev1.EventTypeWarning, ErrorEvent, newStatus.Message)
				}

			} else if oldStatus.Code == nodemanager.StatusManagedNodeCreated {

				glog.Infof("node %s hosted by instance %s owned by managed node %s was deleted", oldStatus.NodeName, oldStatus.InstanceName, key)

				newStatus.Code = nodemanager.StatusManagedNodeDeleted
				newStatus.Message = fmt.Sprintf("the node %s hosted by instance %s was deleted", oldStatus.NodeName, oldStatus.InstanceName)
				newStatus.Reason = nodemanager.StatusManagedNodeReason(nodemanager.StatusManagedNodeDeleted)

				c.recorder.Event(managedNode, corev1.EventTypeWarning, WarningEvent, newStatus.Message)
			}

			if oldStatus.Code != newStatus.Code {
				if updateErr = c.updateManagedNodeStatus(managedNode, newStatus); updateErr != nil {
					glog.Errorf(constantes.ErrUpdateStatusNodeFailed, key, updateErr)
				}
			}
		}

		recycled = newStatus.Code == nodemanager.StatusManagedNodeGroupNotFound || newStatus.Code == nodemanager.StatusManagedNodeNiceLimitReached
	}

	return recycled, err
}

func (c *Controller) updateManagedNodeStatus(managedNode *v1alpha2.ManagedNode, newStatus v1alpha2.ManagedNodeStatus) error {
	nodeManagerClientset, _ := c.client.NodeManagerClient()
	newStatus.LastUpdateTime = metav1.Now()

	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	if managedNodeCopy, err := nodeManagerClientset.NodemanagerV1alpha2().ManagedNodes().Get(context.TODO(), managedNode.Name, metav1.GetOptions{}); err != nil {
		return err
	} else {
		managedNodeCopy.Status = newStatus

		// If the CustomResourceSubresources feature gate is not enabled,
		// we must use Update instead of UpdateStatus to update the Status block of the Foo resource.
		// UpdateStatus will not allow changes to the Spec of the resource,
		// which is ideal for ensuring nothing other than resource status has been updated.
		_, err := nodeManagerClientset.NodemanagerV1alpha2().
			ManagedNodes().
			UpdateStatus(context.TODO(), managedNodeCopy, metav1.UpdateOptions{})

		return err
	}
}

func (c *Controller) getManagedNodeFromKey(key string) (*v1alpha2.ManagedNode, error) {
	if _, name, err := cache.SplitMetaNamespaceKey(key); err == nil {
		return c.managedNodeLister.Get(name)
	} else {
		return nil, err
	}
}

func (c *Controller) generateKey(obj any) string {
	if key, err := cache.MetaNamespaceKeyFunc(obj); err == nil {
		return key
	} else {
		glog.Error(err.Error())
	}

	return ""
}

func (c *Controller) deleteManagedNode(obj any) {
	if managedNode, ok := obj.(*v1alpha2.ManagedNode); ok {
		if nodeGroup, err := c.application.getNodeGroup(managedNode.GetNodegroup()); err == nil {
			if node, err := nodeGroup.findNodeByCRDUID(managedNode.GetUID()); err == nil {
				glog.Infof(constantes.InfoManagedNodeIsDeleted, c.generateKey(managedNode), node.NodeName)

				if err = nodeGroup.deleteNode(c.client, node); err != nil {
					glog.Warnf(warnNodeDeletionErr, node.NodeName, err)
				}

				c.application.syncState()
			} else {
				glog.Errorf(constantes.ErrNodeNotFoundInNodeGroup, managedNode.GetUID(), managedNode.GetNodegroup())
			}
		} else {
			glog.Errorf(constantes.ErrNodeGroupNotFound, managedNode.GetNodegroup())
		}
	}
}

func (c *Controller) enqueueManagedNode(obj any) {
	if managedNode, ok := obj.(*v1alpha2.ManagedNode); ok {
		c.workqueue.Add(c.generateKey(managedNode))
	}
}

func (c *Controller) deleteOwnerRef(ownerRef *metav1.OwnerReference) {
	if managedNode, err := c.getManagedNodeFromKey(ownerRef.Name); err == nil {
		c.deleteManagedNode(managedNode)

		if ownerRef.BlockOwnerDeletion != nil && !*ownerRef.BlockOwnerDeletion {
			if clientset, err := c.application.client().NodeManagerClient(); err == nil {
				if err = clientset.NodemanagerV1alpha2().
					ManagedNodes().
					Delete(context.TODO(), managedNode.GetName(), metav1.DeleteOptions{}); err != nil {
					glog.Errorf("Unable to delete ManagedNode: %s, reason: %v", ownerRef.Name, err)
				}
			}
		}
	}
}

func (c *Controller) handleNode(obj any) {
	var object metav1.Object
	var ok bool

	if object, ok = obj.(metav1.Object); !ok {
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			if object, ok = tombstone.Obj.(metav1.Object); ok {
				glog.Infof("Recovered deleted node '%s' from tombstone", c.generateKey(object))
			} else {
				glog.Errorf("error decoding object tombstone, invalid type")
			}
		} else {
			glog.Errorf("error decoding object, invalid type")
		}
	}

	if object != nil {
		glog.Debugf("Processing node: %s", c.generateKey(object))

		if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
			// If this object is not owned by a ManagedNode, we should not do anything more with it.
			if ownerRef.Kind == v1alpha2.SchemeGroupVersionKind.Kind {
				if managedNode, err := c.getManagedNodeFromKey(ownerRef.Name); err == nil {
					if _, err := c.client.GetNode(managedNode.Status.NodeName); apierrors.IsNotFound(err) {
						c.deleteOwnerRef(ownerRef)
					}
				} else {
					glog.Debugf("ignoring orphaned node '%s' of ManagedNode '%s'", object.GetSelfLink(), ownerRef.Name)
				}
			}
		}
	}
}
