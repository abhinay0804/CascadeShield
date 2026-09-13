// Package models implements the 4 failure propagation models used by The Oracle
// to simulate cascading failures across a service dependency graph.
//
// Each model takes observed edge metrics (from Phase 3) plus a perturbation
// (injected latency or error rate increase) and predicts whether the target
// service's resources would be exhausted, and if so, how quickly.
//
// This file defines shared types used across all 4 models.
package models

import (
	"time"
)

// NodeState tracks the health of a service node during a simulation run.
//
// For beginners: each node starts Healthy. As the simulation injects
// perturbations and propagates them through the DAG, nodes transition:
//
//	Healthy → Degraded (slower, but still serving)
//	Degraded → Exhausted (resource pool depleted, cannot serve any requests)
//
// Once Exhausted, all upstream callers are affected — this is how cascades happen.
type NodeState int

const (
	// StateHealthy means the service is functioning normally.
	StateHealthy NodeState = iota

	// StateDegraded means the service has elevated latency or error rate
	// but can still process requests (just slower).
	StateDegraded

	// StateExhausted means the service's thread/connection pool is fully
	// depleted — it CANNOT serve ANY requests. This is the cascade trigger.
	StateExhausted
)

// String returns a human-readable label for the node state.
func (s NodeState) String() string {
	switch s {
	case StateHealthy:
		return "HEALTHY"
	case StateDegraded:
		return "DEGRADED"
	case StateExhausted:
		return "EXHAUSTED"
	default:
		return "UNKNOWN"
	}
}

// ModelResult is the output of applying a single failure model to one edge.
//
// Multiple models are applied to the same edge and their worst-case results
// are combined by the cascade propagator.
type ModelResult struct {
	// TargetState is the predicted state of the target node after applying this model.
	TargetState NodeState

	// TimeToFailure is how long until the target transitions to Exhausted.
	// Zero means already exhausted (instantaneous failure).
	// Negative means the model doesn't predict exhaustion (healthy/degraded only).
	TimeToFailure time.Duration

	// AdditionalLoad is the amplified request rate caused by this model.
	// Used by the retry model to capture feedback loops.
	// A value of 1.0 means no amplification; 2.5 means 2.5× the original load.
	AdditionalLoad float64

	// QueueingDelay is extra latency added by resource contention (e.g.,
	// waiting for a connection from a saturated pool).
	QueueingDelay time.Duration
}

// EdgeParams holds the per-edge configuration parameters used by failure models.
//
// In a full K8s deployment, these would be discovered from service annotations
// or configuration. For now, they are populated from DefaultConfig() values.
type EdgeParams struct {
	// ThreadPoolSize is the maximum number of concurrent worker threads
	// (or goroutines) the calling service can dedicate to this dependency.
	ThreadPoolSize int

	// ConnPoolSize is the maximum number of simultaneous connections
	// to this downstream service (e.g., database connection pool).
	ConnPoolSize int

	// Timeout is the per-call timeout for requests on this edge.
	Timeout time.Duration

	// MaxRetries is how many times the caller will retry a failed request.
	MaxRetries int

	// RetryBackoff is the base delay between retry attempts.
	RetryBackoff time.Duration
}
