package models

import (
	"time"
)

// ConnPoolSaturation models the failure mode where a downstream service's
// elevated latency causes the caller's connection pool to saturate.
//
// Real-world example:
//   Service A has a pool of 50 connections to its database.
//   Each query takes 10ms (checkout + hold). At 1000 req/s:
//   active_conns = 1000 × 0.010 = 10 — healthy.
//   If query latency jumps to 100ms:
//   active_conns = 1000 × 0.100 = 100 — exceeds pool of 50.
//   Requests start queuing for a free connection → latency spikes further.
//
// The key difference from ThreadPool: connection pools are typically much
// smaller (50 vs 200), so they saturate faster and produce queuing delays
// that cascade upstream.
//
// Formula:
//   active_conns = request_rate × hold_duration_seconds
//   if active_conns > pool_size → SATURATED
//   queuing_delay = (active_conns - pool_size) × avg_hold_time / pool_size
//
// Parameters:
//   - requestRate: current requests/sec on this edge
//   - currentLatencyMs: current connection hold duration in ms (proxy for actual hold time)
//   - injectedLatencyMs: additional latency perturbation in ms
//   - params: EdgeParams containing ConnPoolSize
func ConnPoolSaturation(requestRate, currentLatencyMs, injectedLatencyMs float64, params EdgeParams) ModelResult {
	if requestRate <= 0 || params.ConnPoolSize <= 0 {
		return ModelResult{TargetState: StateHealthy, TimeToFailure: -1}
	}

	totalHoldMs := currentLatencyMs + injectedLatencyMs
	if totalHoldMs <= 0 {
		return ModelResult{TargetState: StateHealthy, TimeToFailure: -1}
	}

	// active_conns = request_rate × hold_duration_in_seconds
	activeConns := requestRate * (totalHoldMs / 1000.0)
	poolSize := float64(params.ConnPoolSize)

	if activeConns <= poolSize*0.8 {
		return ModelResult{TargetState: StateHealthy, TimeToFailure: -1}
	}

	if activeConns <= poolSize {
		// Approaching saturation — degraded with some queuing
		utilization := activeConns / poolSize
		// M/M/c queuing approximation for wait time
		queueDelayMs := (utilization / (1 - utilization + 0.01)) * totalHoldMs * 0.1
		return ModelResult{
			TargetState:   StateDegraded,
			TimeToFailure: -1,
			QueueingDelay: time.Duration(queueDelayMs * float64(time.Millisecond)),
		}
	}

	// Pool saturated — EXHAUSTED
	// Excess connections are waiting in queue
	excessConns := activeConns - poolSize
	// Queuing delay = how long each excess request waits for a free connection
	queueDelayMs := (excessConns / poolSize) * totalHoldMs
	// TTF = time to fill the pool from current state
	ttfSeconds := poolSize / requestRate
	ttf := time.Duration(ttfSeconds * float64(time.Second))

	return ModelResult{
		TargetState:   StateExhausted,
		TimeToFailure: ttf,
		QueueingDelay: time.Duration(queueDelayMs * float64(time.Millisecond)),
	}
}
