package constantes

const (
	// ResourceNameCores is string name for cores. It's used by ResourceLimiter.
	ResourceNameCores = "cpu"
	// ResourceNameMemory is string name for memory. It's used by ResourceLimiter.
	// Memory should always be provided in bytes.
	ResourceNameMemory = "memory"
	// ResourceNameNodes is string name for node. It's used by ResourceLimiter.
	ResourceNameNodes = "nodes"

	// ResourceNameManagedNodeDisk
	ResourceNameManagedNodeDisk = "disk"
	// ResourceNameManagedNodeMemory
	ResourceNameManagedNodeMemory = "memory"
	// ResourceNameManagedNodeCores
	ResourceNameManagedNodeCores = "cpus"
)

const (
	// NodeLabelControlPlaneRole k8s annotation
	NodeLabelControlPlaneRole = "node-role.kubernetes.io/control-plane"

	// NodeLabelMasterRole k8s annotation
	NodeLabelMasterRole = "node-role.kubernetes.io/master"

	// NodeLabelWorkerRole k8s annotation
	NodeLabelWorkerRole = "node-role.kubernetes.io/worker"

	// NodeLabelTopologyRegion topology label
	NodeLabelTopologyRegion = "topology.kubernetes.io/region"

	// NodeLabelTopologyZone topology label
	NodeLabelTopologyZone = "topology.kubernetes.io/zone"

	// NodeLabelVMWareCSIRegion k8s annotation
	NodeLabelVMWareCSIRegion = "topology.csi.vmware.com/k8s-region"

	// NodeLabelVMWareCSIZone k8s annotation
	NodeLabelVMWareCSIZone = "topology.csi.vmware.com/k8s-zone"

	// AnnotationNodeGroupName k8s annotation
	AnnotationNodeGroupName = "cluster.autoscaler.nodegroup/name"

	// AnnotationNodeIndex k8s annotation
	AnnotationNodeIndex = "cluster.autoscaler.nodegroup/node-index"

	// AnnotationInstanceName k8s annotation
	AnnotationInstanceName = "cluster.autoscaler.nodegroup/instance-name"

	// AnnotationInstanceID k8s annotation
	AnnotationInstanceID = "cluster.autoscaler.nodegroup/instance-id"

	// AnnotationNodeAutoProvisionned k8s annotation
	AnnotationNodeAutoProvisionned = "cluster.autoscaler.nodegroup/autoprovision"

	// AnnotationNodeManaged k8s annotation
	AnnotationNodeManaged = "cluster.autoscaler.nodegroup/managed"

	// AnnotationScaleDownDisabled k8s annotation
	AnnotationScaleDownDisabled = "cluster-autoscaler.kubernetes.io/scale-down-disabled"
)
