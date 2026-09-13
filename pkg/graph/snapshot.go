package graph

import (
	"encoding/json"
	"fmt"
	"time"
)

// DAGSnapshot is a thread-safe, immutable point-in-time deep copy of the Service Dependency DAG.
// Phase 4 (The Oracle) and Phase 6 (API/UI) use snapshots to execute failure simulations
// and render UI graphs without holding mutex locks on the live DAG.
type DAGSnapshot struct {
	Timestamp time.Time                  `json:"timestamp"`
	Nodes     map[string]Node            `json:"nodes"`
	Edges     map[string]map[string]Edge `json:"edges"` // source -> target -> Edge
}

// Snapshot creates an isolated deep copy of the current DAG state.
// It acquires an RLock on the DAG to ensure consistency across nodes and edges.
func (d *DAG) Snapshot() *DAGSnapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()

	snap := &DAGSnapshot{
		Timestamp: time.Now(),
		Nodes:     make(map[string]Node, len(d.nodes)),
		Edges:     make(map[string]map[string]Edge, len(d.edges)),
	}

	// Deep-copy nodes
	for id, node := range d.nodes {
		nodeCopy := *node
		// Copy slice of PodInfo
		if len(node.Pods) > 0 {
			nodeCopy.Pods = make([]PodInfo, len(node.Pods))
			copy(nodeCopy.Pods, node.Pods)
		}
		// Copy map of Metadata
		if len(node.Metadata) > 0 {
			nodeCopy.Metadata = make(map[string]string, len(node.Metadata))
			for k, v := range node.Metadata {
				nodeCopy.Metadata[k] = v
			}
		}
		snap.Nodes[id] = nodeCopy
	}

	// Deep-copy edges
	for source, targets := range d.edges {
		targetMap := make(map[string]Edge, len(targets))
		for target, edge := range targets {
			edgeCopy := *edge
			targetMap[target] = edgeCopy
		}
		snap.Edges[source] = targetMap
	}

	return snap
}

// ToJSON serializes the DAGSnapshot to JSON formatted bytes.
func (s *DAGSnapshot) ToJSON() ([]byte, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DAGSnapshot to JSON: %w", err)
	}
	return data, nil
}

// FromJSON deserializes a DAGSnapshot from JSON bytes.
func FromJSON(data []byte) (*DAGSnapshot, error) {
	var snap DAGSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DAGSnapshot from JSON: %w", err)
	}
	return &snap, nil
}
