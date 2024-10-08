package client

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	apiv1 "k8s.io/api/core/v1"

	managednodeClientset "github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/generated/clientset/versioned"
	apiextensionClientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"

	"github.com/linki/instrumented_http"
	glog "github.com/sirupsen/logrus"
	policy "k8s.io/api/policy/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	typesv1 "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Default pod eviction settings.
const (
	conditionDrainedScheduled = "DrainScheduled"
	retrySleep                = time.Millisecond * 250
)

// singletonClientGenerator provides clients
type singletonClientGenerator struct {
	KubeConfig           string
	APIServerURL         string
	RequestTimeout       time.Duration
	DeletionTimeout      time.Duration
	MaxGracePeriod       time.Duration
	NodeReadyTimeout     time.Duration
	kubeClient           kubernetes.Interface
	nodeManagerClientset managednodeClientset.Interface
	apiExtensionClient   apiextensionClientset.Interface
	kubeOnce             sync.Once
}

// getRestConfig returns the rest clients config to get automatically
// data if you run inside a cluster or by passing flags.
func getRestConfig(kubeConfig, apiServerURL string) (*rest.Config, error) {
	if kubeConfig == "" {
		if _, err := os.Stat(clientcmd.RecommendedHomeFile); err == nil {
			kubeConfig = clientcmd.RecommendedHomeFile
		}
	}

	glog.Debugf("apiServerURL: %s", apiServerURL)
	glog.Debugf("kubeConfig: %s", kubeConfig)

	// evaluate whether to use kubeConfig-file or serviceaccount-token
	var (
		config *rest.Config
		err    error
	)
	if kubeConfig == "" {
		glog.Infof("Using inCluster-config based on serviceaccount-token")
		config, err = rest.InClusterConfig()
	} else {
		glog.Infof("Using kubeConfig")
		config, err = clientcmd.BuildConfigFromFlags(apiServerURL, kubeConfig)
	}
	if err != nil {
		return nil, err
	}

	return config, nil
}

// newKubeClient returns a new Kubernetes client object. It takes a Config and
// uses APIServerURL and KubeConfig attributes to connect to the cluster. If
// KubeConfig isn't provided it defaults to using the recommended default.
func newKubeClient(kubeConfig, apiServerURL string, requestTimeout time.Duration) (kubernetes.Interface, managednodeClientset.Interface, apiextensionClientset.Interface, error) {
	glog.Infof("Instantiating new Kubernetes client")

	config, err := getRestConfig(kubeConfig, apiServerURL)
	if err != nil {
		return nil, nil, nil, err
	}

	config.Timeout = requestTimeout * time.Second

	config.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		return instrumented_http.NewTransport(rt, &instrumented_http.Callbacks{
			PathProcessor: func(path string) string {
				parts := strings.Split(path, "/")
				return parts[len(parts)-1]
			},
		})
	}

	client, err := kubernetes.NewForConfig(config)

	if err != nil {
		return nil, nil, nil, err
	}

	nodeManagerClientset, err := managednodeClientset.NewForConfig(config)

	if err != nil {
		return client, nil, nil, err
	}

	apiExtensionClient, err := apiextensionClientset.NewForConfig(config)

	if err != nil {
		return client, nodeManagerClientset, nil, err
	}

	glog.Infof("Created Kubernetes client %s", config.Host)

	return client, nodeManagerClientset, apiExtensionClient, err
}

func (p *singletonClientGenerator) newRequestContext() *context.Context {
	return utils.NewRequestContext(p.RequestTimeout)
}

// KubeClient generates a kube client if it was not created before
func (p *singletonClientGenerator) KubeClient() (kubernetes.Interface, error) {
	var err error
	p.kubeOnce.Do(func() {
		p.kubeClient, p.nodeManagerClientset, p.apiExtensionClient, err = newKubeClient(p.KubeConfig, p.APIServerURL, p.RequestTimeout)
	})
	return p.kubeClient, err
}

// NodeManagerClient generates node manager client if it was not created before
func (p *singletonClientGenerator) NodeManagerClient() (managednodeClientset.Interface, error) {
	var err error
	p.kubeOnce.Do(func() {
		p.kubeClient, p.nodeManagerClientset, p.apiExtensionClient, err = newKubeClient(p.KubeConfig, p.APIServerURL, p.RequestTimeout)
	})
	return p.nodeManagerClientset, err
}

// ApiExtentionClient generates an api extension client if it was not created before
func (p *singletonClientGenerator) ApiExtentionClient() (apiextensionClientset.Interface, error) {
	var err error
	p.kubeOnce.Do(func() {
		p.kubeClient, p.nodeManagerClientset, p.apiExtensionClient, err = newKubeClient(p.KubeConfig, p.APIServerURL, p.RequestTimeout)
	})
	return p.apiExtensionClient, err
}

func (p *singletonClientGenerator) WaitNodeToBeReady(nodeName string) error {
	var nodeInfo *apiv1.Node
	kubeclient, err := p.KubeClient()

	if err != nil {
		return err
	}

	ctx := p.newRequestContext()
	defer ctx.Cancel()

	glog.Infof("Wait kubernetes node %s to be ready", nodeName)

	if err = context.PollImmediate(time.Second, p.NodeReadyTimeout, func() (bool, error) {
		nodeInfo, err = kubeclient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})

		if err != nil {
			return false, err
		}

		for _, status := range nodeInfo.Status.Conditions {
			if status.Type == apiv1.NodeReady {
				if b, e := strconv.ParseBool(string(status.Status)); e == nil {
					if b {
						return true, nil
					}
				}
			}
		}

		glog.Debugf("The kubernetes node: %s is not ready", nodeName)

		return false, nil
	}); err == nil {
		glog.Infof("The kubernetes node %s is Ready", nodeName)
		return nil
	}

	return fmt.Errorf(constantes.ErrNodeIsNotReady, nodeName)
}

func (p *singletonClientGenerator) awaitDeletion(pod apiv1.Pod, timeout time.Duration) error {
	kubeclient, err := p.KubeClient()

	if err != nil {
		return err
	}

	ctx := p.newRequestContext()
	defer ctx.Cancel()

	return context.PollImmediate(time.Second, timeout, func() (bool, error) {
		got, err := kubeclient.CoreV1().Pods(pod.GetNamespace()).Get(ctx, pod.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf(constantes.ErrUndefinedPod, pod.GetNamespace(), pod.GetName(), err)
		}
		if got.GetUID() != pod.GetUID() {
			return true, nil
		}
		return false, nil
	})
}

func (p *singletonClientGenerator) evictPod(pod apiv1.Pod, abort <-chan struct{}, e chan<- error) {
	gracePeriod := int64(p.MaxGracePeriod.Seconds())

	if pod.Spec.TerminationGracePeriodSeconds != nil && *pod.Spec.TerminationGracePeriodSeconds < gracePeriod {
		gracePeriod = *pod.Spec.TerminationGracePeriodSeconds
	}

	kubeclient, err := p.KubeClient()

	if err != nil {
		e <- err
		return
	}

	ctx := context.NewContext(time.Duration(gracePeriod))
	defer ctx.Cancel()

	for {
		select {
		case <-abort:
			e <- fmt.Errorf(constantes.ErrPodEvictionAborted)
			return
		default:
			err := kubeclient.CoreV1().Pods(pod.GetNamespace()).Evict(ctx, &policy.Eviction{
				ObjectMeta:    metav1.ObjectMeta{Namespace: pod.GetNamespace(), Name: pod.GetName()},
				DeleteOptions: &metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod},
			})
			switch {
			// The eviction API returns 429 Too Many Requests if a pod
			// cannot currently be evicted, for example due to a pod
			// disruption budget.
			case apierrors.IsTooManyRequests(err):
				time.Sleep(5 * time.Second)
			case apierrors.IsNotFound(err):
				e <- nil
				return
			case err != nil:
				e <- fmt.Errorf(constantes.ErrCannotEvictPod, pod.GetNamespace(), pod.GetName(), err)
				return
			default:
				if err = p.awaitDeletion(pod, p.DeletionTimeout); err != nil {
					e <- fmt.Errorf(constantes.ErrUnableToConfirmPodEviction, pod.GetNamespace(), pod.GetName(), err)
				} else {
					e <- nil
				}
				return
			}
		}
	}
}

// PodList return list of pods hosted on named node
func (p *singletonClientGenerator) PodList(nodeName string, podFilter PodFilterFunc) ([]apiv1.Pod, error) {
	var pods *apiv1.PodList

	kubeclient, err := p.KubeClient()

	if err != nil {
		return nil, err
	}

	ctx := p.newRequestContext()
	defer ctx.Cancel()

	if pods, err = kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(fields.Set{"spec.nodeName": nodeName}).String(),
	}); err != nil {
		return nil, fmt.Errorf(constantes.ErrPodListReturnError, nodeName, err)
	}

	include := make([]apiv1.Pod, 0, len(pods.Items))

	for _, pod := range pods.Items {
		passes, err := podFilter(pod)
		if err != nil {
			return nil, fmt.Errorf("cannot filter pods, reason: %v", err)
		}
		if passes {
			include = append(include, pod)
		}
	}

	return include, nil
}

// NodeList return node list from cluster
func (p *singletonClientGenerator) NodeList() (*apiv1.NodeList, error) {

	kubeclient, err := p.KubeClient()

	if err != nil {
		return nil, err
	}

	ctx := p.newRequestContext()
	defer ctx.Cancel()

	return kubeclient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
}

func (p *singletonClientGenerator) cordonOrUncordonNode(nodeName string, flag bool) error {
	ctx := p.newRequestContext()
	defer ctx.Cancel()

	return context.PollImmediate(retrySleep, time.Duration(p.RequestTimeout)*time.Second, func() (bool, error) {
		var node *apiv1.Node
		kubeclient, err := p.KubeClient()

		if err != nil {
			return false, err
		}

		if node, err = kubeclient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{}); err != nil {
			return false, err
		}

		if node.Spec.Unschedulable == flag {
			return true, nil
		}

		node.Spec.Unschedulable = flag

		if _, err = kubeclient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{}); err != nil {
			glog.Warnf("Unschedulable node: %s is not ready, err = %s", nodeName, err)
			return false, nil
		}

		return true, nil
	})
}

func (p *singletonClientGenerator) UncordonNode(nodeName string) error {
	return p.cordonOrUncordonNode(nodeName, false)
}

func (p *singletonClientGenerator) CordonNode(nodeName string) error {
	return p.cordonOrUncordonNode(nodeName, true)
}

func (p *singletonClientGenerator) SetProviderID(nodeName, providerID string) error {
	ctx := p.newRequestContext()
	defer ctx.Cancel()

	return context.PollImmediate(retrySleep, time.Duration(p.RequestTimeout)*time.Second, func() (bool, error) {
		var node *apiv1.Node
		kubeclient, err := p.KubeClient()

		if err != nil {
			return false, err
		}

		if node, err = kubeclient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{}); err != nil {
			return false, err
		}

		if node.Spec.ProviderID == providerID {
			return true, nil
		}

		patch := utils.ToYAML(map[string]any{
			"kind":       "Node",
			"apiVersion": "v1",
			"spec": map[string]string{
				"providerID": providerID,
			},
		})

		patchOptions := metav1.PatchOptions{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Node",
				APIVersion: "v1",
			},
			FieldManager: "application/apply-patch",
		}

		if _, err = kubeclient.CoreV1().Nodes().Patch(ctx, nodeName, typesv1.ApplyPatchType, []byte(patch), patchOptions); err != nil {
			glog.Warnf("Set providerID node: %s is not ready, err = %s", nodeName, err)
			return false, nil
		}

		return true, nil
	})
}

func (p *singletonClientGenerator) MarkDrainNode(nodeName string) error {
	ctx := p.newRequestContext()
	defer ctx.Cancel()

	return context.PollImmediate(retrySleep, time.Duration(p.RequestTimeout)*time.Second, func() (bool, error) {
		var node *apiv1.Node
		kubeclient, err := p.KubeClient()

		if err != nil {
			return false, err
		}

		now := metav1.Time{Time: time.Now()}

		if node, err = kubeclient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}

		conditionStatus := apiv1.ConditionTrue

		// Create or update the condition associated to the monitor
		conditionUpdated := false

		for i, condition := range node.Status.Conditions {
			if string(condition.Type) == conditionDrainedScheduled {
				node.Status.Conditions[i].LastHeartbeatTime = now
				node.Status.Conditions[i].Message = "Drain activity scheduled " + now.Time.Format(time.RFC3339)
				node.Status.Conditions[i].Status = conditionStatus
				conditionUpdated = true
				break
			}
		}

		if !conditionUpdated { // There was no condition found, let's create one
			node.Status.Conditions = append(node.Status.Conditions,
				apiv1.NodeCondition{
					Type:               apiv1.NodeConditionType(conditionDrainedScheduled),
					Status:             conditionStatus,
					LastHeartbeatTime:  now,
					LastTransitionTime: now,
					Reason:             "Draino",
					Message:            "Drain activity scheduled " + now.Format(time.RFC3339),
				},
			)
		}

		if _, err = kubeclient.CoreV1().Nodes().UpdateStatus(ctx, node, metav1.UpdateOptions{}); err != nil {
			glog.Warnf("Drain node: %s is not ready, err = %s", nodeName, err)
			return false, nil
		}

		return true, nil
	})
}

func (p *singletonClientGenerator) DrainNode(nodeName string, ignoreDaemonSet, deleteLocalData bool) error {
	ctx := p.newRequestContext()
	defer ctx.Cancel()

	pf := []PodFilterFunc{
		MirrorPodFilter,
	}

	if ignoreDaemonSet {
		pf = append(pf, NewDaemonSetPodFilter(ctx, p.kubeClient))
	}

	if !deleteLocalData {
		pf = append(pf, LocalStoragePodFilter)
	}

	pods, err := p.PodList(nodeName, NewPodFilters(pf...))
	if err != nil {
		return fmt.Errorf(constantes.ErrUnableToGetPodListOnNode, nodeName, err)
	}

	abort := make(chan struct{})
	errs := make(chan error, 1)

	defer close(abort)

	for _, pod := range pods {
		go p.evictPod(pod, abort, errs)
	}

	deadline := time.After(p.RequestTimeout)

	for range pods {
		select {
		case err := <-errs:
			if err != nil {
				return fmt.Errorf(constantes.ErrUnableEvictAllPodsOnNode, nodeName, err)
			}
		case <-deadline:
			return fmt.Errorf(constantes.ErrTimeoutWhenWaitingEvictions, nodeName)
		}
	}

	return nil
}

func (p *singletonClientGenerator) GetNode(nodeName string) (*apiv1.Node, error) {
	kubeclient, err := p.KubeClient()

	if err != nil {
		return nil, err
	}

	ctx := p.newRequestContext()
	defer ctx.Cancel()

	return kubeclient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})

}

func (p *singletonClientGenerator) DeleteNode(nodeName string) error {
	kubeclient, err := p.KubeClient()

	if err != nil {
		return err
	}

	ctx := p.newRequestContext()
	defer ctx.Cancel()

	return kubeclient.CoreV1().Nodes().Delete(ctx, nodeName, metav1.DeleteOptions{})
}

// AnnoteNode set annotation on node
func (p *singletonClientGenerator) AnnoteNode(nodeName string, annotations map[string]string) error {
	ctx := p.newRequestContext()
	defer ctx.Cancel()

	return context.PollImmediate(retrySleep, p.RequestTimeout, func() (bool, error) {
		var nodeInfo *apiv1.Node

		kubeclient, err := p.KubeClient()

		if err != nil {
			return false, err
		}

		if nodeInfo, err = kubeclient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{}); err != nil {
			return false, err
		}

		if len(nodeInfo.Annotations) == 0 {
			nodeInfo.Annotations = annotations
		} else {
			for k, v := range annotations {
				nodeInfo.Annotations[k] = v
			}
		}

		if _, err = kubeclient.CoreV1().Nodes().Update(ctx, nodeInfo, metav1.UpdateOptions{}); err != nil {
			glog.Warnf("Annote node: %s is not ready, err = %s", nodeName, err)
			return false, nil
		}

		return true, nil
	})
}

// LabelNode set label on node
func (p *singletonClientGenerator) LabelNode(nodeName string, labels map[string]string) error {
	ctx := p.newRequestContext()
	defer ctx.Cancel()

	return context.PollImmediate(retrySleep, p.RequestTimeout, func() (bool, error) {
		var nodeInfo *apiv1.Node
		kubeclient, err := p.KubeClient()

		if err != nil {
			return false, err
		}

		if nodeInfo, err = kubeclient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{}); err != nil {
			return false, err
		}

		if len(nodeInfo.Labels) == 0 {
			nodeInfo.Labels = labels
		} else {
			for k, v := range labels {
				nodeInfo.Labels[k] = v
			}
		}

		if _, err = kubeclient.CoreV1().Nodes().Update(ctx, nodeInfo, metav1.UpdateOptions{}); err != nil {
			glog.Warnf("Label node: %s is not ready, err = %s", nodeName, err)
			return false, nil
		}

		return true, nil
	})
}

func containTaint(key string, taints *[]apiv1.Taint) (int, bool) {
	for i, t := range *taints {
		if t.Key == key {
			return i, true
		}
	}

	return -1, false
}

// TaintNode set annotation on node
func (p *singletonClientGenerator) TaintNode(nodeName string, taints ...apiv1.Taint) error {
	ctx := p.newRequestContext()
	defer ctx.Cancel()

	return context.PollImmediate(retrySleep, p.RequestTimeout, func() (bool, error) {
		var nodeInfo *apiv1.Node
		kubeclient, err := p.KubeClient()

		if err != nil {
			return false, err
		}

		if nodeInfo, err = kubeclient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{}); err != nil {
			return false, err
		}

		if nodeInfo.Spec.Taints == nil {
			nodeInfo.Spec.Taints = taints
		} else {
			mergedTaints := make([]apiv1.Taint, 0, len(taints))

			for _, taint := range taints {
				if index, found := containTaint(taint.Key, &nodeInfo.Spec.Taints); found {
					// Replace taint
					nodeInfo.Spec.Taints[index] = taint
				} else {
					// Merge it later
					mergedTaints = append(mergedTaints, taint)
				}
			}

			if len(mergedTaints) > 0 {
				nodeInfo.Spec.Taints = append(nodeInfo.Spec.Taints, mergedTaints...)
			}
		}

		if _, err = kubeclient.CoreV1().Nodes().Update(ctx, nodeInfo, metav1.UpdateOptions{}); err != nil {
			glog.Warnf("Label node: %s is not ready, err = %s", nodeName, err)
			return false, nil
		}

		return true, nil
	})
}

func (p *singletonClientGenerator) GetSecret(secretName, namespace string) (*apiv1.Secret, error) {
	ctx := p.newRequestContext()
	defer ctx.Cancel()

	if kubeclient, err := p.KubeClient(); err != nil {
		return nil, err
	} else {
		return kubeclient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	}
}

func (p *singletonClientGenerator) DeleteSecret(secretName, namespace string) error {
	ctx := p.newRequestContext()
	defer ctx.Cancel()

	if kubeclient, err := p.KubeClient(); err != nil {
		return err
	} else {
		return kubeclient.CoreV1().Secrets(namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	}
}

func NewClientGenerator(cfg ClientConfig) ClientGenerator {
	return &singletonClientGenerator{
		KubeConfig:       cfg.GetKubeConfig(),
		APIServerURL:     cfg.GetAPIServerURL(),
		RequestTimeout:   cfg.GetRequestTimeout(),
		NodeReadyTimeout: cfg.GetNodeReadyTimeout(),
		DeletionTimeout:  cfg.GetDeletionTimeout(),
		MaxGracePeriod:   cfg.GetMaxGracePeriod(),
	}
}
