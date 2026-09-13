package models

import (
	"time"
)

// TimeoutChainResult captures the outcome of timeout analysis for a single edge.
type TimeoutChainResult struct {
	// TimedOut is true if the caller would time out waiting for this downstream.
	TimedOut bool

	// WaitDuration is the actual time the caller waits before either getting
	// a response or hitting its timeout.
	WaitDuration time.Duration

	// EffectiveLatency is the latency experienced by the caller.
	// If timed out: = Timeout duration (the caller gave up).
	// If not timed out: = actual downstream latency.
	EffectiveLatency time.Duration
}

// TimeoutChainPropagation models the failure mode where timeouts propagate
// through a multi-hop service chain.
//
// Real-world example:
//   A → B → C → D, each with configured timeouts.
//   If D becomes slow (latency = 8s):
//     C's timeout for D = 3s → C times out after 3s, returns error to B
//     B's timeout for C = 5s → B waited 3s (C's timeout) → still within B's 5s
//     But if B retries C: total wait = 3s + 3s = 6s > B's timeout → B times out
//     A's timeout for B = 10s → depends on B's retry behavior
//
// Key insight: Timeouts DON'T prevent cascades — they change the cascade SPEED.
// A timeout converts a "slow" failure into a "fast error" which then triggers
// retry amplification (Model 4).
//
// Parameters:
//   - downstreamLatencyMs: the actual response time of the downstream service (including perturbation)
//   - params: EdgeParams containing Timeout for this edge
//   - retriesAttempted: how many times the caller has retried (affects total wait)
func TimeoutChainPropagation(downstreamLatencyMs float64, params EdgeParams, retriesAttempted int) TimeoutChainResult {
	if params.Timeout <= 0 {
		// No timeout configured — caller waits forever (no timeout-based failure)
		return TimeoutChainResult{
			TimedOut:         false,
			WaitDuration:     time.Duration(downstreamLatencyMs * float64(time.Millisecond)),
			EffectiveLatency: time.Duration(downstreamLatencyMs * float64(time.Millisecond)),
		}
	}

	timeoutMs := float64(params.Timeout.Milliseconds())

	// Total time the caller waits = downstream latency × (1 + retries)
	// Each retry waits for the full downstream latency (or until timeout)
	perAttemptMs := downstreamLatencyMs
	if perAttemptMs > timeoutMs {
		// Each attempt is capped by the timeout
		perAttemptMs = timeoutMs
	}

	// Total wait = first attempt + (retries × (per-attempt time + backoff))
	backoffMs := float64(params.RetryBackoff.Milliseconds())
	totalWaitMs := perAttemptMs // first attempt
	for i := 0; i < retriesAttempted; i++ {
		totalWaitMs += backoffMs + perAttemptMs
	}

	if downstreamLatencyMs > timeoutMs {
		// The downstream is slower than our timeout — we WILL time out
		return TimeoutChainResult{
			TimedOut:         true,
			WaitDuration:     time.Duration(totalWaitMs * float64(time.Millisecond)),
			EffectiveLatency: params.Timeout, // caller experiences timeout duration, not actual
		}
	}

	// Downstream responded within timeout — no timeout, but latency may be elevated
	return TimeoutChainResult{
		TimedOut:         false,
		WaitDuration:     time.Duration(totalWaitMs * float64(time.Millisecond)),
		EffectiveLatency: time.Duration(totalWaitMs * float64(time.Millisecond)),
	}
}

// EvaluateTimeout is a convenience wrapper that returns a ModelResult
// from timeout analysis, suitable for combining with other model results.
func EvaluateTimeout(downstreamLatencyMs float64, params EdgeParams) ModelResult {
	result := TimeoutChainPropagation(downstreamLatencyMs, params, params.MaxRetries)

	if result.TimedOut {
		return ModelResult{
			TargetState:   StateDegraded,
			TimeToFailure: result.WaitDuration,
			QueueingDelay: result.EffectiveLatency,
		}
	}

	if result.EffectiveLatency > params.Timeout/2 {
		// Over 50% of timeout consumed — degraded
		return ModelResult{
			TargetState:   StateDegraded,
			TimeToFailure: -1,
			QueueingDelay: result.EffectiveLatency,
		}
	}

	return ModelResult{
		TargetState:   StateHealthy,
		TimeToFailure: -1,
	}
}
