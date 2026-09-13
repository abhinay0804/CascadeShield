package simulator

import (
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	"github.com/abhinay0804/cascadeshield/pkg/simulator/models"
)

// CascadeRun captures the result of a single Monte Carlo simulation run
// where one origin node is perturbed and failures propagate through the DAG.
type CascadeRun struct {
	// Path is the ordered list of nodes that failed (cascade chain).
	// First element is the origin; subsequent elements failed because of the cascade.
	Path []string

	// NodeStates records the final health state of every node after propagation.
	NodeStates map[string]models.NodeState

	// TimeToFailure records how long each exhausted/degraded node took to fail.
	TimeToFailure map[string]time.Duration
}

// PropagateCascade performs BFS failure propagation from a degraded origin node
// through the DAG, applying all 4 failure models at each hop.
//
// Design choice: propagation goes UPSTREAM. If "payments" degrades, "orders"
// (which calls payments) is affected. Then "gateway" (which calls orders)
// is affected. This models real cascade behavior: downstream failures
// propagate through the call chain to callers.
//
// Parameters:
//   - snap: immutable DAG snapshot (from Phase 2's dag.Snapshot())
//   - originID: the node that was perturbed (the "root cause")
//   - injectedLatencyMs: additional latency injected at the origin
//   - injectedErrorRate: additional error rate injected at the origin (0.0-1.0)
//   - cfg: simulator config (provides default pool sizes, timeouts, etc.)
func PropagateCascade(snap *graph.DAGSnapshot, originID string, injectedLatencyMs, injectedErrorRate float64, cfg Config) CascadeRun {
	run := CascadeRun{
		Path:          []string{originID},
		NodeStates:    make(map[string]models.NodeState, len(snap.Nodes)),
		TimeToFailure: make(map[string]time.Duration),
	}

	// Initialize all nodes as healthy
	for id := range snap.Nodes {
		run.NodeStates[id] = models.StateHealthy
	}

	// The origin node is degraded (we injected a perturbation)
	run.NodeStates[originID] = models.StateDegraded

	// Build a reverse adjacency list: for each node, find all callers (upstream nodes).
	// In the DAG, edges go source→target (caller→callee). We need target→callers.
	upstreamOf := make(map[string][]string)
	for source, targets := range snap.Edges {
		for target := range targets {
			upstreamOf[target] = append(upstreamOf[target], source)
		}
	}

	// BFS queue: start from origin, propagate to callers
	type queueItem struct {
		nodeID          string
		effectiveLatMs  float64 // latency as seen by callers of this node
		effectiveErrRate float64 // error rate as seen by callers
	}

	queue := []queueItem{{
		nodeID:          originID,
		effectiveLatMs:  injectedLatencyMs,
		effectiveErrRate: injectedErrorRate,
	}}
	visited := map[string]bool{originID: true}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// For each service that CALLS the current (degraded/exhausted) node
		for _, callerID := range upstreamOf[current.nodeID] {
			if visited[callerID] {
				continue
			}

			// Get the edge from caller → current
			edge, edgeExists := snap.Edges[callerID][current.nodeID]
			if !edgeExists {
				continue
			}

			// Build edge parameters (from K8s ResourceLimits or defaults)
			params := buildEdgeParams(snap, callerID, cfg)

			// The downstream node (current) has elevated latency.
			// The caller sees: current P99 latency + the injected/cascaded extra latency.
			downstreamLatencyMs := edge.Metrics.LatencyP99 + current.effectiveLatMs
			requestRate := edge.Metrics.RequestRate
			errorRate := edge.Metrics.ErrorRate + current.effectiveErrRate

			// Apply all 4 failure models
			threadResult := models.ThreadPoolExhaustion(requestRate, downstreamLatencyMs, 0, params)
			connResult := models.ConnPoolSaturation(requestRate, downstreamLatencyMs, 0, params)
			timeoutResult := models.EvaluateTimeout(downstreamLatencyMs, params)

			// Retry amplification: capacity = pool_size / (latency_s)
			capacity := float64(params.ThreadPoolSize) / (downstreamLatencyMs / 1000.0 + 0.001)
			retryResult := models.RetryAmplification(requestRate, errorRate, params, capacity, cfg.MaxRetryIterations)

			// Take the WORST outcome across all 4 models
			worstState := models.StateHealthy
			var worstTTF time.Duration
			var additionalLatMs float64
			var additionalErrRate float64

			results := []models.ModelResult{threadResult, connResult, timeoutResult, retryResult}
			for _, r := range results {
				if r.TargetState > worstState {
					worstState = r.TargetState
					if r.TimeToFailure > 0 {
						worstTTF = r.TimeToFailure
					}
				}
				if r.QueueingDelay > 0 {
					additionalLatMs += float64(r.QueueingDelay.Milliseconds())
				}
				if r.AdditionalLoad > 1.0 {
					// Retry amplification increases effective error rate for upstream
					additionalErrRate += (r.AdditionalLoad - 1.0) * 0.1
				}
			}

			if worstState > models.StateHealthy {
				visited[callerID] = true
				run.NodeStates[callerID] = worstState
				run.Path = append(run.Path, callerID)

				if worstTTF > 0 {
					run.TimeToFailure[callerID] = worstTTF
				}

				// Cascade continues: enqueue this caller so ITS callers are also evaluated
				queue = append(queue, queueItem{
					nodeID:          callerID,
					effectiveLatMs:  additionalLatMs + current.effectiveLatMs*0.5, // dampened propagation
					effectiveErrRate: clampF(additionalErrRate+current.effectiveErrRate*0.5, 0, 1.0),
				})
			}
		}
	}

	return run
}

// buildEdgeParams constructs EdgeParams for a caller node, using K8s
// ResourceLimits when available, falling back to config defaults.
func buildEdgeParams(snap *graph.DAGSnapshot, callerID string, cfg Config) models.EdgeParams {
	params := models.EdgeParams{
		ThreadPoolSize: cfg.DefaultThreadPoolSize,
		ConnPoolSize:   cfg.DefaultConnPoolSize,
		Timeout:        cfg.DefaultTimeout,
		MaxRetries:     cfg.DefaultMaxRetries,
		RetryBackoff:   cfg.DefaultRetryBackoff,
	}

	// If K8s ResourceLimits are populated, derive pool sizes from CPU limits.
	// Heuristic: 1 CPU core ≈ 50 concurrent request capacity.
	if node, ok := snap.Nodes[callerID]; ok {
		if node.ResourceLimits.CPULimitMillis > 0 {
			cpuCores := float64(node.ResourceLimits.CPULimitMillis) / 1000.0
			estimatedPool := int(cpuCores * 50)
			if estimatedPool > 0 {
				params.ThreadPoolSize = estimatedPool
				params.ConnPoolSize = max(10, estimatedPool/4)
			}
		}
	}

	return params
}

// clampF restricts v to the range [lo, hi].
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// max returns the larger of two ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
