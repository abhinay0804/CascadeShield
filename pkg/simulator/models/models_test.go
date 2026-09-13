package models

import (
	"testing"
	"time"
)

// =============================================================================
// Model 1: Thread Pool Exhaustion
// =============================================================================

func TestThreadPool_Healthy(t *testing.T) {
	// 100 req/s × 50ms = 5 active threads. Pool = 200. Well under capacity.
	result := ThreadPoolExhaustion(100, 50, 0, EdgeParams{ThreadPoolSize: 200})
	if result.TargetState != StateHealthy {
		t.Errorf("expected HEALTHY, got %s", result.TargetState)
	}
}

func TestThreadPool_Degraded(t *testing.T) {
	// 1000 req/s × 180ms = 180 active threads. Pool = 200. 90% = degraded.
	result := ThreadPoolExhaustion(1000, 80, 100, EdgeParams{ThreadPoolSize: 200})
	if result.TargetState != StateDegraded {
		t.Errorf("expected DEGRADED, got %s", result.TargetState)
	}
}

func TestThreadPool_Exhausted(t *testing.T) {
	// 1000 req/s × 250ms = 250 active threads. Pool = 200. EXHAUSTED.
	result := ThreadPoolExhaustion(1000, 50, 200, EdgeParams{ThreadPoolSize: 200})
	if result.TargetState != StateExhausted {
		t.Errorf("expected EXHAUSTED, got %s", result.TargetState)
	}
	// TTF = pool_size / rate = 200 / 1000 = 0.2s = 200ms
	expectedTTF := 200 * time.Millisecond
	if result.TimeToFailure < expectedTTF-10*time.Millisecond || result.TimeToFailure > expectedTTF+10*time.Millisecond {
		t.Errorf("expected TTF ~200ms, got %v", result.TimeToFailure)
	}
}

func TestThreadPool_ZeroRate(t *testing.T) {
	result := ThreadPoolExhaustion(0, 100, 50, EdgeParams{ThreadPoolSize: 200})
	if result.TargetState != StateHealthy {
		t.Errorf("expected HEALTHY for zero rate, got %s", result.TargetState)
	}
}

// =============================================================================
// Model 2: Connection Pool Saturation
// =============================================================================

func TestConnPool_Healthy(t *testing.T) {
	// 100 req/s × 10ms = 1 active conn. Pool = 50. Healthy.
	result := ConnPoolSaturation(100, 10, 0, EdgeParams{ConnPoolSize: 50})
	if result.TargetState != StateHealthy {
		t.Errorf("expected HEALTHY, got %s", result.TargetState)
	}
}

func TestConnPool_Saturated(t *testing.T) {
	// 1000 req/s × 100ms = 100 active conns. Pool = 50. EXHAUSTED.
	result := ConnPoolSaturation(1000, 50, 50, EdgeParams{ConnPoolSize: 50})
	if result.TargetState != StateExhausted {
		t.Errorf("expected EXHAUSTED, got %s", result.TargetState)
	}
	if result.QueueingDelay <= 0 {
		t.Errorf("expected positive queuing delay, got %v", result.QueueingDelay)
	}
}

func TestConnPool_ZeroPool(t *testing.T) {
	result := ConnPoolSaturation(1000, 100, 0, EdgeParams{ConnPoolSize: 0})
	if result.TargetState != StateHealthy {
		t.Errorf("expected HEALTHY for zero pool, got %s", result.TargetState)
	}
}

// =============================================================================
// Model 3: Timeout Chain Propagation
// =============================================================================

func TestTimeout_NoTimeout(t *testing.T) {
	// No timeout configured → caller waits forever
	result := TimeoutChainPropagation(5000, EdgeParams{Timeout: 0}, 0)
	if result.TimedOut {
		t.Error("expected no timeout when timeout = 0")
	}
}

func TestTimeout_WithinTimeout(t *testing.T) {
	// Downstream responds in 2s, timeout is 5s → no timeout
	result := TimeoutChainPropagation(2000, EdgeParams{Timeout: 5 * time.Second}, 0)
	if result.TimedOut {
		t.Error("expected no timeout when latency < timeout")
	}
}

func TestTimeout_ExceedsTimeout(t *testing.T) {
	// Downstream responds in 8s, timeout is 3s → timeout!
	result := TimeoutChainPropagation(8000, EdgeParams{Timeout: 3 * time.Second}, 0)
	if !result.TimedOut {
		t.Error("expected timeout when latency > timeout")
	}
	// Effective latency should be the timeout duration, not actual
	if result.EffectiveLatency != 3*time.Second {
		t.Errorf("expected effective latency = 3s, got %v", result.EffectiveLatency)
	}
}

func TestTimeout_WithRetries(t *testing.T) {
	// Downstream at 4s, timeout = 3s, 2 retries, 100ms backoff
	// Total wait = 3s (first) + 0.1s + 3s (retry1) + 0.1s + 3s (retry2) = 9.2s
	result := TimeoutChainPropagation(4000, EdgeParams{
		Timeout:      3 * time.Second,
		RetryBackoff: 100 * time.Millisecond,
	}, 2)
	if !result.TimedOut {
		t.Error("expected timeout with retries")
	}
	expectedWait := 9200 * time.Millisecond
	tolerance := 100 * time.Millisecond
	if result.WaitDuration < expectedWait-tolerance || result.WaitDuration > expectedWait+tolerance {
		t.Errorf("expected total wait ~9.2s, got %v", result.WaitDuration)
	}
}

func TestTimeout_EvaluateTimeout(t *testing.T) {
	// Test the convenience wrapper
	result := EvaluateTimeout(8000, EdgeParams{
		Timeout:      3 * time.Second,
		MaxRetries:   2,
		RetryBackoff: 100 * time.Millisecond,
	})
	if result.TargetState != StateDegraded {
		t.Errorf("expected DEGRADED for timed-out edge, got %s", result.TargetState)
	}
}

// =============================================================================
// Model 4: Retry Amplification
// =============================================================================

func TestRetry_NoRetries(t *testing.T) {
	result := RetryAmplification(1000, 0.5, EdgeParams{MaxRetries: 0}, 2000, 10)
	if result.AdditionalLoad != 1.0 {
		t.Errorf("expected no amplification with 0 retries, got %.2f", result.AdditionalLoad)
	}
}

func TestRetry_BasicAmplification(t *testing.T) {
	// 50% error rate, 3 retries, unlimited capacity
	// amplification = 1 + 0.5 × 3 = 2.5
	result := RetryAmplification(1000, 0.5, EdgeParams{MaxRetries: 3}, 0, 10)
	expected := 2.5
	tolerance := 0.1
	if result.AdditionalLoad < expected-tolerance || result.AdditionalLoad > expected+tolerance {
		t.Errorf("expected amplification ~2.5, got %.2f", result.AdditionalLoad)
	}
}

func TestRetry_FeedbackLoop(t *testing.T) {
	// High error rate + limited capacity → should show > 2.5× amplification
	// because the amplified load causes more errors which cause more retries
	result := RetryAmplification(1000, 0.5, EdgeParams{MaxRetries: 3}, 1500, 10)
	if result.AdditionalLoad <= 2.0 {
		t.Errorf("expected significant amplification with feedback loop, got %.2f", result.AdditionalLoad)
	}
	t.Logf("Retry amplification with feedback: %.2f× (state: %s)", result.AdditionalLoad, result.TargetState)
}

func TestRetry_Convergence(t *testing.T) {
	// Even with extreme inputs, the model should converge within MaxIterations
	result := RetryAmplification(10000, 0.9, EdgeParams{MaxRetries: 10}, 5000, 10)
	if result.AdditionalLoad <= 0 {
		t.Error("expected positive amplification factor")
	}
	t.Logf("Extreme retry scenario: %.2f× amplification, state=%s", result.AdditionalLoad, result.TargetState)
}

func TestRetry_ZeroErrorRate(t *testing.T) {
	// No errors → no retries → no amplification
	result := RetryAmplification(1000, 0.0, EdgeParams{MaxRetries: 3}, 2000, 10)
	if result.AdditionalLoad != 1.0 {
		t.Errorf("expected 1.0× with zero error rate, got %.2f", result.AdditionalLoad)
	}
	if result.TargetState != StateHealthy {
		t.Errorf("expected HEALTHY with zero error rate, got %s", result.TargetState)
	}
}
