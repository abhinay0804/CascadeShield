package models

import (
	"time"
)

// ThreadPoolExhaustion models the failure mode where a downstream service's
// elevated latency causes the caller's thread pool to fill up.
//
// Real-world example:
//   Service A calls Service B at 1000 req/s. Each call blocks a thread.
//   If B's latency jumps from 50ms to 250ms, the number of threads blocked
//   waiting for B increases from 50 to 250 — exceeding A's pool of 200.
//   Now A can't serve ANY requests, even ones that don't involve B.
//
// Formula:
//   active_threads = request_rate × (latency_seconds)
//   if active_threads > pool_size → EXHAUSTED
//   TTF = pool_size / request_rate (seconds until pool fills from empty)
//
// Parameters:
//   - requestRate: current requests/sec on this edge (from EdgeMetrics.RequestRate)
//   - currentLatencyMs: current P99 latency in ms (from EdgeMetrics.LatencyP99)
//   - injectedLatencyMs: additional latency perturbation in ms
//   - params: EdgeParams containing ThreadPoolSize
func ThreadPoolExhaustion(requestRate, currentLatencyMs, injectedLatencyMs float64, params EdgeParams) ModelResult {
	if requestRate <= 0 || params.ThreadPoolSize <= 0 {
		return ModelResult{TargetState: StateHealthy, TimeToFailure: -1}
	}

	totalLatencyMs := currentLatencyMs + injectedLatencyMs
	if totalLatencyMs <= 0 {
		return ModelResult{TargetState: StateHealthy, TimeToFailure: -1}
	}

	// active_threads = request_rate × latency_in_seconds
	// e.g., 1000 req/s × 0.250s = 250 threads busy at any instant
	activeThreads := requestRate * (totalLatencyMs / 1000.0)

	poolSize := float64(params.ThreadPoolSize)

	if activeThreads <= poolSize*0.8 {
		// Well within capacity — healthy
		return ModelResult{TargetState: StateHealthy, TimeToFailure: -1}
	}

	if activeThreads <= poolSize {
		// Close to capacity — degraded but not yet exhausted
		return ModelResult{
			TargetState:   StateDegraded,
			TimeToFailure: -1,
			// Queuing delay begins as pool approaches saturation
			// Little's Law: delay ≈ (utilization / (1 - utilization)) × service_time
			QueueingDelay: time.Duration(
				(activeThreads / (poolSize - activeThreads + 1)) * totalLatencyMs * float64(time.Millisecond),
			),
		}
	}

	// Pool exceeded — EXHAUSTED
	// TTF = how long from empty to full = pool_size / arrival_rate
	ttfSeconds := poolSize / requestRate
	ttf := time.Duration(ttfSeconds * float64(time.Second))

	return ModelResult{
		TargetState:   StateExhausted,
		TimeToFailure: ttf,
	}
}
