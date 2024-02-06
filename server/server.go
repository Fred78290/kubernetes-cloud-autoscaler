package server

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	apigrpc "github.com/Fred78290/kubernetes-cloud-autoscaler/grpc"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/signals"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	glog "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

type cloudProviderRequest interface {
	GetProviderID() string
}

type cloudProviderServer interface {
	RegisterCloudProviderServer(server *grpc.Server)
}

type applicationInterface interface {
	getNodeGroup(nodegroup string) (*AutoScalerServerNodeGroup, error)
	isNodegroupDiscovered() bool
	getResourceLimiter() *types.ResourceLimiter
	syncState()
	client() types.ClientGenerator
	getMachineType(instanceType string) *providers.MachineCharacteristic
}

// AutoScalerServerApp declare AutoScaler grpc server
type AutoScalerServerApp struct {
	apigrpc.UnimplementedCloudProviderServiceServer
	apigrpc.UnimplementedNodeGroupServiceServer
	apigrpc.UnimplementedPricingModelServiceServer
	Machines        providers.MachineCharacteristics      `default:"{\"standard\": {}}" json:"machines"` // Mandatory, Available machines
	ResourceLimiter *types.ResourceLimiter                `json:"limits"`
	Groups          map[string]*AutoScalerServerNodeGroup `json:"groups"`
	NodesDefinition []*apigrpc.NodeGroupDef               `json:"nodedefs"`
	AutoProvision   bool                                  `json:"auto"`
	configuration   *types.AutoScalerServerConfig
	running         bool
	kubeClient      types.ClientGenerator
	requestTimeout  time.Duration
}

var phSavedState = ""
var phSaveState bool

func (s *AutoScalerServerApp) generateNodeGroupName() string {
	return fmt.Sprintf("ng-%d", time.Now().Unix())
}

func (s *AutoScalerServerApp) isNodegroupDiscovered() bool {
	return len(s.Groups) > 0
}

func (s *AutoScalerServerApp) getResourceLimiter() *types.ResourceLimiter {
	return s.ResourceLimiter
}

func (s *AutoScalerServerApp) getNodeGroup(nodegroupName string) (*AutoScalerServerNodeGroup, error) {
	if ng, found := s.Groups[nodegroupName]; found {
		return ng, nil
	}

	return nil, fmt.Errorf(constantes.ErrNodeGroupNotFound, nodegroupName)
}

type nodegroupCreateInput struct {
	nodeGroupID   string
	minNodeSize   int
	maxNodeSize   int
	machineType   string
	labels        types.KubernetesLabel
	systemLabels  types.KubernetesLabel
	autoProvision bool
}

func (s *AutoScalerServerApp) newNodeGroup(intput *nodegroupCreateInput) (*AutoScalerServerNodeGroup, error) {

	machine := s.Machines[intput.machineType]

	if machine == nil {
		return nil, fmt.Errorf(constantes.ErrMachineTypeNotFound, intput.machineType)
	}

	if nodeGroup := s.Groups[intput.nodeGroupID]; nodeGroup != nil {
		glog.Errorf(constantes.ErrNodeGroupAlreadyExists, intput.nodeGroupID)

		return nil, fmt.Errorf(constantes.ErrNodeGroupAlreadyExists, intput.nodeGroupID)
	}

	intput.labels = types.MergeKubernetesLabel(s.configuration.NodeLabels, intput.labels)

	glog.Infof("New node group, ID:%s minSize:%d, maxSize:%d, machineType:%s, node labels:%v, %v", input.nodeGroupID, input.minNodeSize, input.maxNodeSize, input.machineType, input.labels, input.systemLabels)

	nodeGroup := &AutoScalerServerNodeGroup{
		ServiceIdentifier:          s.configuration.ServiceIdentifier,
		ProvisionnedNodeNamePrefix: s.configuration.ProvisionnedNodeNamePrefix,
		ManagedNodeNamePrefix:      s.configuration.ManagedNodeNamePrefix,
		ControlPlaneNamePrefix:     s.configuration.ControlPlaneNamePrefix,
		NodeGroupIdentifier:        intput.nodeGroupID,
		InstanceType:               intput.machineType,
		Status:                     NodegroupNotCreated,
		pendingNodes:               make(map[string]*AutoScalerServerNode),
		Nodes:                      make(map[string]*AutoScalerServerNode),
		MinNodeSize:                intput.minNodeSize,
		MaxNodeSize:                intput.maxNodeSize,
		NodeLabels:                 intput.labels,
		SystemLabels:               intput.systemLabels,
		AutoProvision:              intput.autoProvision,
		configuration:              s.configuration,
		machines:                   s.Machines,
	}

	s.Groups[intput.nodeGroupID] = nodeGroup

	return nodeGroup, nil
}

func (s *AutoScalerServerApp) deleteNodeGroup(nodeGroupID string) error {
	nodeGroup := s.Groups[nodeGroupID]

	if nodeGroup == nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, nodeGroupID)
		return fmt.Errorf(constantes.ErrNodeGroupNotFound, nodeGroupID)
	}

	glog.Infof("Delete node group, ID:%s", nodeGroupID)

	if err := nodeGroup.deleteNodeGroup(s.kubeClient); err != nil {
		glog.Errorf(constantes.ErrUnableToDeleteNodeGroup, nodeGroupID, err)
		return err
	}

	delete(s.Groups, nodeGroupID)

	return nil
}

func (s *AutoScalerServerApp) createNodeGroup(nodeGroupID string) (*AutoScalerServerNodeGroup, error) {
	nodeGroup := s.Groups[nodeGroupID]

	if nodeGroup == nil {
		glog.Errorf(constantes.ErrNodeGroupNotFound, nodeGroupID)
		return nil, fmt.Errorf(constantes.ErrNodeGroupNotFound, nodeGroupID)
	}

	if nodeGroup.Status == NodegroupNotCreated {
		numberOfNodesToCreate := nodeGroup.MinNodeSize - len(nodeGroup.Nodes)
		nodeGroup.Status = NodegroupCreating

		// Must launch minNode VM
		if numberOfNodesToCreate > 0 {

			glog.Infof("Create node group, ID:%s", nodeGroupID)

			if _, err := nodeGroup.addNodes(s.kubeClient, numberOfNodesToCreate); err != nil {
				glog.Errorf(err.Error())

				nodeGroup.Status = NodegroupNotCreated

				return nodeGroup, err
			}
		}

		nodeGroup.Status = NodegroupCreated
	}

	return nodeGroup, nil
}

func (s *AutoScalerServerApp) doAutoProvision() error {
	glog.Debug("Call server doAutoProvision")

	var ng *AutoScalerServerNodeGroup
	var formerNodes map[string]*AutoScalerServerNode
	var err error

	for _, nodeGroupDef := range s.NodesDefinition {
		nodeGroupIdentifier := nodeGroupDef.GetNodeGroupID()

		if len(nodeGroupIdentifier) > 0 {
			ng = s.Groups[nodeGroupIdentifier]

			if ng == nil {
				systemLabels := types.KubernetesLabel{}
				labels := types.KubernetesLabel{}

				// Default labels
				if nodeGroupDef.GetLabels() != nil {
					for k, v := range nodeGroupDef.GetLabels() {
						labels[k] = v
					}
				}

				glog.Infof("Auto provision for nodegroup:%s, minSize:%d, maxSize:%d", nodeGroupIdentifier, nodeGroupDef.MinSize, nodeGroupDef.MaxSize)

				input := nodegroupCreateInput{
					nodeGroupID:   nodeGroupIdentifier,
					minNodeSize:   int(nodeGroupDef.MinSize),
					maxNodeSize:   int(nodeGroupDef.MaxSize),
					machineType:   s.configuration.DefaultMachineType,
					labels:        labels,
					systemLabels:  systemLabels,
					autoProvision: true,
				}

				if _, err = s.newNodeGroup(&input); err != nil {
					break
				}

				if ng, err = s.createNodeGroup(nodeGroupIdentifier); err != nil {
					break
				}

				if formerNodes, err = ng.autoDiscoveryNodes(s.kubeClient, nodeGroupDef.GetIncludeExistingNode()); err != nil {
					break
				}

				// If the nodegroup already exists, reparse nodes
			} else if formerNodes, err = ng.autoDiscoveryNodes(s.kubeClient, nodeGroupDef.GetIncludeExistingNode()); err != nil {
				break
			}

			// Drop VM if kubernetes nodes removed
			ng.findManagedNodeDeleted(s.kubeClient, formerNodes)
		}
	}

	return err
}

func (s *AutoScalerServerApp) syncState() {
	for _, ng := range s.Groups {
		ng.refresh()
	}

	if phSaveState {
		if err := s.Save(phSavedState); err != nil {
			glog.Errorf(constantes.ErrFailedToSaveServerState, err)
		}
	}
}

// Save state to file
func (s *AutoScalerServerApp) Save(fileName string) error {
	file, err := os.Create(fileName)

	if err != nil {
		glog.Errorf("Failed to open file:%s, error:%v", fileName, err)

		return err
	}

	defer file.Close()

	encoder := json.NewEncoder(file)
	err = encoder.Encode(s)

	if err != nil {
		glog.Errorf("failed to encode AutoScalerServerApp to file:%s, error:%v", fileName, err)

		return err
	}

	return nil
}

// Load saved state from file
func (s *AutoScalerServerApp) Load(fileName string) error {
	file, err := os.Open(fileName)

	if err != nil {
		glog.Errorf("Failed to open file:%s, error:%v", fileName, err)

		return err
	}

	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(s)

	if err != nil {
		glog.Errorf("failed to decode AutoScalerServerApp file:%s, error:%v", fileName, err)
		return err
	}

	for _, ng := range s.Groups {
		if err = ng.setConfiguration(s.configuration); err != nil {
			return err
		}
	}

	if s.AutoProvision {
		if err := s.doAutoProvision(); err != nil {
			glog.Errorf(constantes.ErrUnableToAutoProvisionNodeGroup, err)

			return err
		}
	}

	return nil
}

func (s *AutoScalerServerApp) getMachineType(instanceType string) *providers.MachineCharacteristic {

	if machineSpec, ok := s.Machines[instanceType]; ok {
		return machineSpec
	}

	return nil
}

func (s *AutoScalerServerApp) client() types.ClientGenerator {
	return s.kubeClient
}

func (s *AutoScalerServerApp) startController() error {
	var err error

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	if _, err = s.kubeClient.KubeClient(); err == nil {
		if _, err = s.kubeClient.NodeManagerClient(); err == nil {
			var controller *Controller

			if controller, err = NewController(s, stopCh); err == nil {
				err = controller.Run()
			} else {
				glog.Errorf("Create CRD failed, reason: %v", err)
			}
		} else {
			glog.Errorf("can't get manager node client interface, reason: %v", err)
		}
	} else {
		glog.Errorf("can't get kubeclient interface, reason: %v", err)
	}

	return err
}

func (s *AutoScalerServerApp) runServer(config *types.AutoScalerServerConfig, registerService func(*grpc.Server)) error {
	var server *grpc.Server

	if config.CertCA == "" || config.CertPrivateKey == "" || config.CertPublicKey == "" {
		server = grpc.NewServer()
	} else {
		certPool := x509.NewCertPool()

		if certificate, err := tls.LoadX509KeyPair(config.CertPublicKey, config.CertPrivateKey); err != nil {
			return fmt.Errorf("failed to read certificate files: %s", err)
		} else if bs, err := os.ReadFile(config.CertCA); err != nil {
			return fmt.Errorf("failed to read client ca cert: %s", err)
		} else if ok := certPool.AppendCertsFromPEM(bs); !ok {
			return fmt.Errorf("failed to append client certs")
		} else {

			transportCreds := credentials.NewTLS(&tls.Config{
				ClientAuth:   tls.RequireAndVerifyClientCert,
				Certificates: []tls.Certificate{certificate},
				ClientCAs:    certPool,
			})

			server = grpc.NewServer(grpc.Creds(transportCreds))
		}
	}

	defer func() {
		s.running = false
		server.Stop()
	}()

	registerService(server)
	reflection.Register(server)

	if u, err := url.Parse(*config.Listen); err != nil {
		return err
	} else {
		var listen string

		if u.Scheme == "unix" {
			listen = u.Path
		} else if u.Scheme == "tcp" {
			listen = u.Host
		} else {
			return fmt.Errorf("unsupported scheme:%s, %s", u.Scheme, *config.Listen)
		}

		if len(listen) == 0 {
			return fmt.Errorf("endpoint is empty, %s", *config.Listen)
		}

		if listener, err := net.Listen(u.Scheme, listen); err != nil {
			return fmt.Errorf("failed to listen: %v", err)
		} else if err = server.Serve(listener); err != nil {
			return fmt.Errorf("failed to serve: %v", err)
		}

		glog.Infof("End listening server")

		return nil
	}
}

func (s *AutoScalerServerApp) createGrpListener() (cloudProviderServer, error) {
	if *s.configuration.GrpcProvider == "externalgrpc" {
		return NewExternalgrpcServerApp(s)
	} else {
		return NewGrpcServerApp(s)
	}
}
func (s *AutoScalerServerApp) run(config *types.AutoScalerServerConfig) {
	if err := s.runServer(config, func(server *grpc.Server) {
		if grpcListener, err := s.createGrpListener(); err != nil {
			glog.Fatalf("failed to create externalgrpc: %v", err)
		} else {
			grpcListener.RegisterCloudProviderServer(server)
		}
	}); err != nil {
		glog.Fatalf("failed to start server: %v", err)
	}
}

func (s *AutoScalerServerApp) checkPrivateKeyExists() bool {
	if len(s.configuration.SSH.Password) > 0 {
		return true
	}

	if len(s.configuration.SSH.AuthKeys) == 0 {
		return false
	}

	return utils.FileExistAndReadable(s.configuration.SSH.AuthKeys)
}

func (s *AutoScalerServerApp) checkKubernetesPKIReadable() bool {
	return utils.DirExistAndReadable(s.configuration.KubernetesPKISourceDir)
}

func (s *AutoScalerServerApp) checkEtcdSslReadable() bool {
	if *s.configuration.UseExternalEtdc {
		return utils.DirExistAndReadable(s.configuration.ExtSourceEtcdSslDir)
	}

	return true
}

// StartServer start the service
func StartServer(kubeClient types.ClientGenerator, c *types.Config) {
	var config types.AutoScalerServerConfig
	var autoScalerServer *AutoScalerServerApp
	var machines providers.MachineCharacteristics

	saveState := c.SaveLocation
	configFileName := c.Config

	if len(saveState) > 0 {
		phSavedState = saveState
		phSaveState = true
	}

	content, err := providers.LoadTextEnvSubst(configFileName)
	if err != nil {
		glog.Fatalf("failed to open config file:%s, error:%v", configFileName, err)
	}

	decoder := json.NewDecoder(strings.NewReader(content))
	err = decoder.Decode(&config)
	if err != nil {
		glog.Fatalf("failed to decode config file:%s, error:%v", configFileName, err)
	}

	if _, err = kubeClient.KubeClient(); err != nil {
		glog.Fatalf("failed to get kubernetes client, error:%v", err)
	}

	if config.Optionals == nil {
		config.Optionals = &types.AutoScalerServerOptionals{
			Pricing:                  false,
			GetAvailableMachineTypes: false,
			NewNodeGroup:             false,
			TemplateNodeInfo:         false,
			Create:                   false,
			Delete:                   false,
		}
	}

	if config.Listen == nil {
		config.Listen = &c.Listen
	}

	if config.NodeGroup == nil {
		config.NodeGroup = &c.Nodegroup
	}

	if config.MachineConfig == nil {
		config.MachineConfig = &c.MachineConfig
	}

	if config.Distribution == nil {
		config.Distribution = &c.Distribution
	}

	if config.Plateform == nil {
		config.Plateform = &c.Plateform
	}

	if len(*config.NodeGroup) == 0 {
		glog.Fatal("Nodegroup is not defined")
	}

	if err = config.SetupCloudConfiguration(c.ProviderConfig); err != nil {
		glog.Fatalf("Can't setup cloud provider, reason:%s", err)
	}

	if _, err = config.GetAutoScalingOptions(); err != nil {
		glog.Fatalf("Can't setup autoscaling options, reason:%s", err)
	}

	switch *config.Distribution {
	case providers.KubeAdmDistributionName:
		if config.KubeAdm == nil {
			glog.Fatal("KubeAdm configuration is not defined")
		}
	case providers.K3SDistributionName:
		if config.K3S == nil {
			glog.Fatal("K3S configuration is not defined")
		}
	case providers.RKE2DistributionName:
		if config.RKE2 == nil {
			glog.Fatal("RKE2 configuration is not defined")
		}
	case providers.ExternalDistributionName:
		if config.External == nil {
			glog.Fatal("External configuration is not defined")
		}
	default:
		glog.Fatalf("Unsupported kubernetes distribution: %s", *config.Distribution)
	}

	if config.MinNode == nil {
		config.MinNode = &c.MinNode
	}

	if config.MaxNode == nil {
		config.MaxNode = &c.MaxNode
	}

	if config.MaxPods == nil {
		config.MaxPods = &c.MaxPods
	}

	if config.DebugMode == nil {
		config.DebugMode = &c.DebugMode
	}

	if config.CloudProvider == nil {
		config.CloudProvider = &c.CloudProvider
	}

	if config.UseExternalEtdc == nil {
		config.UseExternalEtdc = &c.UseExternalEtdc
	}

	if config.GrpcProvider == nil {
		config.GrpcProvider = &c.GrpcProvider
	}

	if len(config.ExtDestinationEtcdSslDir) == 0 {
		config.ExtDestinationEtcdSslDir = c.ExtDestinationEtcdSslDir
	}

	if len(config.ExtSourceEtcdSslDir) == 0 {
		config.ExtSourceEtcdSslDir = c.ExtSourceEtcdSslDir
	}

	if len(config.KubernetesPKISourceDir) == 0 {
		config.KubernetesPKISourceDir = c.KubernetesPKISourceDir
	}

	if len(config.KubernetesPKIDestDir) == 0 {
		config.KubernetesPKIDestDir = c.KubernetesPKIDestDir
	}

	if err = providers.LoadConfig(*config.MachineConfig, &machines); err != nil {
		log.Fatalf(constantes.ErrMachineSpecsNotFound, *config.MachineConfig, err)
	}

	config.ManagedNodeResourceLimiter = c.GetManagedNodeResourceLimiter()

	if !phSaveState || !utils.FileExists(phSavedState) {
		autoScalerServer = &AutoScalerServerApp{
			kubeClient:      kubeClient,
			requestTimeout:  c.RequestTimeout,
			ResourceLimiter: c.GetResourceLimiter(),
			configuration:   &config,
			Groups:          make(map[string]*AutoScalerServerNodeGroup),
			Machines:        machines,
		}

		autoScalerServer.ResourceLimiter.SetMaxValue64(constantes.ResourceNameNodes, *config.MaxNode)
		autoScalerServer.ResourceLimiter.SetMinValue64(constantes.ResourceNameNodes, *config.MinNode)

		if phSaveState {
			if err = autoScalerServer.Save(phSavedState); err != nil {
				log.Fatalf(constantes.ErrFailedToSaveServerState, err)
			}
		}
	} else {
		autoScalerServer = &AutoScalerServerApp{
			kubeClient:     kubeClient,
			requestTimeout: c.RequestTimeout,
			configuration:  &config,
			Machines:       machines,
		}

		if err := autoScalerServer.Load(phSavedState); err != nil {
			log.Fatalf(constantes.ErrFailedToLoadServerState, err)
		}
	}

	if !autoScalerServer.checkPrivateKeyExists() {
		glog.Fatalf(constantes.ErrFatalMissingSSHKey, autoScalerServer.configuration.SSH.AuthKeys)
	}

	if !autoScalerServer.checkKubernetesPKIReadable() {
		glog.Fatalf(constantes.ErrFatalKubernetesPKIMissingOrUnreadable, autoScalerServer.configuration.KubernetesPKISourceDir)
	}

	if !autoScalerServer.checkEtcdSslReadable() {
		glog.Fatalf(constantes.ErrFatalEtcdMissingOrUnreadable, autoScalerServer.configuration.ExtSourceEtcdSslDir)
	}

	if err = autoScalerServer.startController(); err != nil {
		glog.Fatalf("Can't start controller, reason:%s", err)
	}

	autoScalerServer.run(&config)
}
