package resolver

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/cache"
)

// KubeResolver wraps client-go SharedInformers to watch Kubernetes Pods and Services.
// Informers provide local, watch-based O(1) in-memory cache lookups without overloading the API server.
type KubeResolver struct {
	client          kubernetes.Interface
	informerFactory informers.SharedInformerFactory
	podLister       listerv1.PodLister
	serviceLister   listerv1.ServiceLister
	stopCh          chan struct{}
	started         bool
	mu              sync.Mutex
	logger          *slog.Logger
}

// NewKubeResolver creates and initializes a Kubernetes resolver using client-go.
// It auto-detects in-cluster vs local kubeconfig environments.
func NewKubeResolver(kubeconfigPath string, resyncPeriod time.Duration, logger *slog.Logger) (*KubeResolver, error) {
	if logger == nil {
		logger = slog.Default()
	}

	config, err := getKubeConfig(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	factory := informers.NewSharedInformerFactory(clientset, resyncPeriod)
	podInformer := factory.Core().V1().Pods()
	serviceInformer := factory.Core().V1().Services()

	return &KubeResolver{
		client:          clientset,
		informerFactory: factory,
		podLister:       podInformer.Lister(),
		serviceLister:   serviceInformer.Lister(),
		stopCh:          make(chan struct{}),
		logger:          logger,
	}, nil
}

// Start begins watching K8s API events and waits for the initial cache to sync.
func (kr *KubeResolver) Start(ctx context.Context) error {
	kr.mu.Lock()
	if kr.started {
		kr.mu.Unlock()
		return nil
	}
	kr.started = true
	kr.mu.Unlock()

	kr.logger.Info("starting Kubernetes SharedInformers")
	kr.informerFactory.Start(kr.stopCh)

	// Wait for cache sync with context timeout safety
	synced := kr.informerFactory.WaitForCacheSync(kr.stopCh)
	for typ, ok := range synced {
		if !ok {
			return fmt.Errorf("failed to sync informer cache for type %v", typ)
		}
	}

	kr.logger.Info("Kubernetes informer cache fully synchronized")
	return nil
}

// ResolveContainerID maps a container runtime ID (e.g. docker/containerd ID) to its K8s Pod and Namespace.
func (kr *KubeResolver) ResolveContainerID(containerID string) (podName, namespace string, err error) {
	if containerID == "" {
		return "", "", fmt.Errorf("empty container ID")
	}

	// Walk all pods in local cache to match container ID
	pods, err := kr.podLister.List(labels.Everything())
	if err != nil {
		return "", "", fmt.Errorf("failed to list pods from cache: %w", err)
	}

	for _, pod := range pods {
		// Check regular containers
		for _, cStatus := range pod.Status.ContainerStatuses {
			if matchesContainerID(cStatus.ContainerID, containerID) {
				return pod.Name, pod.Namespace, nil
			}
		}
		// Check init containers
		for _, cStatus := range pod.Status.InitContainerStatuses {
			if matchesContainerID(cStatus.ContainerID, containerID) {
				return pod.Name, pod.Namespace, nil
			}
		}
	}

	return "", "", fmt.Errorf("container ID %s not found in K8s pod cache", containerID)
}

// ResolveIP maps an IP address (pod IP or host IP) to a K8s Pod and Namespace.
func (kr *KubeResolver) ResolveIP(ip string) (podName, namespace string, err error) {
	if ip == "" {
		return "", "", fmt.Errorf("empty IP address")
	}

	pods, err := kr.podLister.List(labels.Everything())
	if err != nil {
		return "", "", fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods {
		if pod.Status.PodIP == ip {
			return pod.Name, pod.Namespace, nil
		}
		for _, podIP := range pod.Status.PodIPs {
			if podIP.IP == ip {
				return pod.Name, pod.Namespace, nil
			}
		}
	}

	return "", "", fmt.Errorf("IP address %s not found in K8s pod cache", ip)
}

// ResolvePodToService maps a Pod name and Namespace to its logical Service identity.
// Priority:
// 1. K8s Service selector match
// 2. Pod Owner Reference (Deployment / ReplicaSet / StatefulSet name)
// 3. Pod name prefix fallback (e.g., "orders-7f8d9b-abc" -> "orders")
func (kr *KubeResolver) ResolvePodToService(podName, namespace string) (string, error) {
	pod, err := kr.podLister.Pods(namespace).Get(podName)
	if err != nil {
		return "", fmt.Errorf("failed to get pod %s/%s: %w", namespace, podName, err)
	}

	// 1. Try K8s Service selector matching
	services, err := kr.serviceLister.Services(namespace).List(labels.Everything())
	if err == nil && len(services) > 0 {
		for _, svc := range services {
			if len(svc.Spec.Selector) == 0 {
				continue
			}
			selector := labels.Set(svc.Spec.Selector).AsSelector()
			if selector.Matches(labels.Set(pod.Labels)) {
				return svc.Name, nil
			}
		}
	}

	// 2. Fallback to OwnerReference (Deployment/ReplicaSet name)
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "Deployment" || owner.Kind == "StatefulSet" || owner.Kind == "DaemonSet" {
			return owner.Name, nil
		}
		if owner.Kind == "ReplicaSet" {
			// ReplicaSet names are typically "<deployment-name>-<hash>"
			return stripHashSuffix(owner.Name), nil
		}
	}

	// 3. Fallback to Pod name prefix (strip random hash suffix)
	return stripHashSuffix(podName), nil
}

// GetPodInfo extracts Pod metadata (IP, container ID, labels) for graph nodes.
func (kr *KubeResolver) GetPodInfo(podName, namespace string) (*corev1.Pod, error) {
	return kr.podLister.Pods(namespace).Get(podName)
}

// Stop halts informers cleanly.
func (kr *KubeResolver) Stop() {
	kr.mu.Lock()
	defer kr.mu.Unlock()
	if kr.started {
		close(kr.stopCh)
		kr.started = false
		kr.logger.Info("stopped Kubernetes SharedInformers")
	}
}

// Helper functions:

func matchesContainerID(fullContainerID, targetID string) bool {
	// Full container ID in K8s is e.g. "containerd://1a2b3c4d..." or "docker://1a2b3c4d..."
	parts := strings.Split(fullContainerID, "://")
	id := fullContainerID
	if len(parts) > 1 {
		id = parts[1]
	}
	return strings.HasPrefix(id, targetID) || strings.HasPrefix(targetID, id)
}

func stripHashSuffix(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) > 2 {
		return strings.Join(parts[:len(parts)-2], "-")
	} else if len(parts) > 1 {
		return parts[0]
	}
	return name
}

func getKubeConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}

	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fallback to default user kubeconfig path (~/.kube/config)
	homeDir, err := os.UserHomeDir()
	if err == nil {
		defaultPath := filepath.Join(homeDir, ".kube", "config")
		if _, err := os.Stat(defaultPath); err == nil {
			return clientcmd.BuildConfigFromFlags("", defaultPath)
		}
	}

	return nil, fmt.Errorf("no valid kubeconfig found (in-cluster or local)")
}

// Silence unused variable warning for cache import if necessary
var _ = cache.MetaNamespaceKeyFunc
