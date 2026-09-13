package graph

import (
	"log/slog"
	"sync"
	"time"
)

// DAG represents a live, thread-safe Service Dependency Directed Acyclic Graph.
//
// Go Pattern Note for Beginners (RWMutex Synchronization & Thread Safety):
// - Multiple goroutines can read the graph simultaneously using RLock() / RUnlock().
// - Only one goroutine can write to/modify the graph at a time using Lock() / Unlock().
// - Centralizing edge mutations (RecordConnect, RecordClose, RecordRetransmit) inside
//   write locks prevents data races when Phase 2 builder updates metrics while other components read.
type DAG struct {
	nodes  map[string]*Node
	edges  map[string]map[string]*Edge // source -> target -> *Edge
	config Config
	mu     sync.RWMutex
	logger *slog.Logger
}

// NewDAG creates a new thread-safe DAG instance with the given configuration.
func NewDAG(cfg Config, logger *slog.Logger) *DAG {
	if logger == nil {
		logger = slog.Default()
	}
	return &DAG{
		nodes:  make(map[string]*Node),
		edges:  make(map[string]map[string]*Edge),
		config: cfg,
		logger: logger,
	}
}

// --- Read Operations (Acquire RLock) ---

// GetNode retrieves a copy of a node by ID under RLock.
func (d *DAG) GetNode(id string) (Node, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	node, exists := d.nodes[id]
	if !exists {
		return Node{}, false
	}
	return *node, true
}

// GetEdge retrieves a thread-safe copy of an edge between source and target services.
func (d *DAG) GetEdge(source, target string) (Edge, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if targets, exists := d.edges[source]; exists {
		if edge, found := targets[target]; found {
			return *edge, true
		}
	}
	return Edge{}, false
}

// GetUpstream returns all incoming edges pointing to nodeID (services that call nodeID).
func (d *DAG) GetUpstream(nodeID string) []Edge {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var upstream []Edge
	for _, targets := range d.edges {
		if edge, exists := targets[nodeID]; exists {
			upstream = append(upstream, *edge)
		}
	}
	return upstream
}

// GetDownstream returns all outgoing edges originating from nodeID (services called by nodeID).
func (d *DAG) GetDownstream(nodeID string) []Edge {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var downstream []Edge
	if targets, exists := d.edges[nodeID]; exists {
		for _, edge := range targets {
			downstream = append(downstream, *edge)
		}
	}
	return downstream
}

// GetAllNodes returns a slice of all node copies in the DAG.
func (d *DAG) GetAllNodes() []Node {
	d.mu.RLock()
	defer d.mu.RUnlock()

	nodes := make([]Node, 0, len(d.nodes))
	for _, n := range d.nodes {
		nodes = append(nodes, *n)
	}
	return nodes
}

// GetAllEdges returns a slice of all edge copies in the DAG.
func (d *DAG) GetAllEdges() []Edge {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var edges []Edge
	for _, targets := range d.edges {
		for _, e := range targets {
			edges = append(edges, *e)
		}
	}
	return edges
}

// NodeCount returns the total number of nodes in the graph.
func (d *DAG) NodeCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.nodes)
}

// EdgeCount returns the total number of edges in the graph.
func (d *DAG) EdgeCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	count := 0
	for _, targets := range d.edges {
		count += len(targets)
	}
	return count
}

// --- Write / Mutation Operations (Acquire Write Lock) ---

// EnsureNode retrieves an existing node or creates a new one if it doesn't exist.
func (d *DAG) EnsureNode(id, namespace string) *Node {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ensureNodeUnlocked(id, namespace)
}

func (d *DAG) ensureNodeUnlocked(id, namespace string) *Node {
	if node, exists := d.nodes[id]; exists {
		node.UpdateLastSeen(time.Now())
		return node
	}

	if len(d.nodes) >= d.config.MaxNodes {
		d.logger.Warn("DAG node capacity reached, skipping new node creation", "max", d.config.MaxNodes, "node_id", id)
		return nil
	}

	node := NewNode(id, namespace)
	d.nodes[id] = node
	return node
}

// RecordConnect handles a CONNECT or ACCEPT event: ensures nodes exist and updates edge connection counters under write lock.
func (d *DAG) RecordConnect(source, target, namespace string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	_ = d.ensureNodeUnlocked(source, namespace)
	_ = d.ensureNodeUnlocked(target, namespace)

	targets, exists := d.edges[source]
	if !exists {
		targets = make(map[string]*Edge)
		d.edges[source] = targets
	}

	edge, found := targets[target]
	if !found {
		if len(targets) >= d.config.MaxEdgesPerNode {
			d.logger.Warn("Max outbound edges reached for node", "source", source, "limit", d.config.MaxEdgesPerNode)
			return
		}

		edge = NewEdge(source, target)
		targets[target] = edge

		if srcNode, ok := d.nodes[source]; ok {
			srcNode.OutDegree++
		}
		if tgtNode, ok := d.nodes[target]; ok {
			tgtNode.InDegree++
		}
	}

	edge.TotalConnections++
	edge.ActiveConnections++
	edge.UpdateLastSeen(time.Now())
}

// RecordClose handles a CLOSE event: updates active connections, byte counts, and lifetime duration under write lock.
func (d *DAG) RecordClose(source, target string, bytesSent, bytesRecv, durationNs uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if targets, exists := d.edges[source]; exists {
		if edge, found := targets[target]; found {
			if edge.ActiveConnections > 0 {
				edge.ActiveConnections--
			}
			edge.TotalBytesSent += bytesSent
			edge.TotalBytesRecv += bytesRecv
			edge.TotalDurationNs += durationNs
			edge.ConnectionCount++
			edge.UpdateLastSeen(time.Now())
		}
	}
}

// RecordRetransmit handles a RETRANSMIT event: increments retransmit counter under write lock.
func (d *DAG) RecordRetransmit(source, target string, count uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if targets, exists := d.edges[source]; exists {
		if edge, found := targets[target]; found {
			edge.TotalRetransmits += uint64(count)
			edge.UpdateLastSeen(time.Now())
		}
	}
}

// UpdateEdgeMetrics writes computed Phase 3 metrics into an existing edge under write lock.
// This is the primary entry point for the Metrics Annotator to push computed values
// (request rate, latency percentiles, error rate, throughput) into the live DAG.
//
// If the edge does not exist (it may have been pruned), the update is silently dropped.
func (d *DAG) UpdateEdgeMetrics(source, target string, m EdgeMetrics) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if targets, exists := d.edges[source]; exists {
		if edge, found := targets[target]; found {
			edge.Metrics = m
		}
	}
}

// PruneStaleEdges removes inactive edges where time.Since(LastSeen) > StaleEdgeTimeout.
// It also cleans up orphaned nodes and updates node degrees. Returns the number of edges pruned.
func (d *DAG) PruneStaleEdges() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	prunedCount := 0

	for source, targets := range d.edges {
		for target, edge := range targets {
			if now.Sub(edge.LastSeen) > d.config.StaleEdgeTimeout {
				delete(targets, target)
				prunedCount++

				if srcNode, ok := d.nodes[source]; ok && srcNode.OutDegree > 0 {
					srcNode.OutDegree--
				}
				if tgtNode, ok := d.nodes[target]; ok && tgtNode.InDegree > 0 {
					tgtNode.InDegree--
				}
			}
		}
		if len(targets) == 0 {
			delete(d.edges, source)
		}
	}

	if prunedCount > 0 {
		remainingEdges := 0
		for _, targets := range d.edges {
			remainingEdges += len(targets)
		}
		d.logger.Info("pruned stale DAG edges", "pruned", prunedCount, "remaining_edges", remainingEdges)
	}

	return prunedCount
}
