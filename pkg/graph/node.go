package graph

import (
	"time"
)

// PodInfo represents a single Kubernetes pod backing a service node.
type PodInfo struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	IP          string            `json:"ip"`
	ContainerID string            `json:"container_id"`
	Labels      map[string]string `json:"labels"`
	Ready       bool              `json:"ready"`
}

// ResourceLimits captures Kubernetes resource requests and limits for capacity modeling.
// Phase 4 (The Oracle) uses these parameters to simulate thread/connection pool depletion.
type ResourceLimits struct {
	CPURequestMillis int64 `json:"cpu_request_millis"` // millicores requested (e.g. 500m -> 500)
	CPULimitMillis   int64 `json:"cpu_limit_millis"`   // millicores limit (e.g. 2000m -> 2000)
	MemRequestBytes  int64 `json:"mem_request_bytes"`  // bytes requested (e.g. 512Mi -> 536870912)
	MemLimitBytes    int64 `json:"mem_limit_bytes"`    // bytes limit
}

// Node represents a logical microservice or host process within the dependency graph.
type Node struct {
	ID             string            `json:"id"`        // Logical service name (e.g., "orders", "payments")
	Namespace      string            `json:"namespace"` // K8s namespace or "host" / "external"
	Pods           []PodInfo         `json:"pods"`      // Backing pods if running in K8s
	Metadata       map[string]string `json:"metadata"`  // Arbitrary annotations/tags
	ResourceLimits ResourceLimits    `json:"resource_limits"`
	FirstSeen      time.Time         `json:"first_seen"`
	LastSeen       time.Time         `json:"last_seen"`
	InDegree       int               `json:"in_degree"`  // Number of incoming edges
	OutDegree      int               `json:"out_degree"` // Number of outgoing edges
}

// NewNode constructs a new Node instance.
func NewNode(id, namespace string) *Node {
	now := time.Now()
	return &Node{
		ID:        id,
		Namespace: namespace,
		Pods:      make([]PodInfo, 0),
		Metadata:  make(map[string]string),
		FirstSeen: now,
		LastSeen:  now,
	}
}

// AddPod appends a pod to the node's list of backing pod replicas if not already present.
func (n *Node) AddPod(pod PodInfo) {
	for i, existing := range n.Pods {
		if existing.Name == pod.Name && existing.Namespace == pod.Namespace {
			n.Pods[i] = pod // Update existing
			return
		}
	}
	n.Pods = append(n.Pods, pod)
}

// UpdateLastSeen updates the node's activity timestamp.
func (n *Node) UpdateLastSeen(t time.Time) {
	if t.After(n.LastSeen) {
		n.LastSeen = t
	}
}
