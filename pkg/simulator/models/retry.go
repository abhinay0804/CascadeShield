package models

// RetryAmplification models the positive feedback loop where retries increase
// load, which increases errors, which triggers more retries.
//
// Real-world example:
//   Service C fails 50% of requests. B retries failed requests 3 times.
//   Normal load on C: 1000 req/s
//   With retries: 1000 + (500 failures × 3 retries) = 2500 req/s
//   C is now at 2.5× load → more failures → more retries → death spiral
//
// The mathematical model:
//   amplification = 1 + (error_rate × max_retries)
//
// But this is a simplification! In reality, the amplified load INCREASES the
// error rate (overloaded services fail more), creating a feedback loop:
//
//   iteration 0: error_rate = 0.5, amplification = 1 + 0.5×3 = 2.5
//   iteration 1: new load = 2500, new error_rate ≈ 0.65 (higher load → more errors)
//   iteration 2: amplification = 1 + 0.65×3 = 2.95
//   ... until convergence or MaxIterations
//
// We model the error-rate increase under load using a simple logistic curve:
//   new_error_rate = base_error_rate + (1 - base_error_rate) × (1 - capacity/load)
//   where capacity is bounded by pool size.
//
// Parameters:
//   - baseRequestRate: current requests/sec (from EdgeMetrics.RequestRate)
//   - baseErrorRate: current error fraction 0.0-1.0 (from EdgeMetrics.ErrorRate)
//   - params: EdgeParams containing MaxRetries
//   - capacity: effective capacity in req/s (derived from pool size / avg latency)
//   - maxIterations: cap on fixed-point iterations to prevent infinite loops
func RetryAmplification(baseRequestRate, baseErrorRate float64, params EdgeParams, capacity float64, maxIterations int) ModelResult {
	if params.MaxRetries <= 0 || baseRequestRate <= 0 {
		return ModelResult{
			TargetState:    StateHealthy,
			TimeToFailure:  -1,
			AdditionalLoad: 1.0,
		}
	}

	if maxIterations <= 0 {
		maxIterations = 10
	}

	currentRate := baseRequestRate
	currentErrorRate := baseErrorRate
	maxRetries := float64(params.MaxRetries)

	// Fixed-point iteration: compute amplified load → new error rate → repeat
	converged := false
	for i := 0; i < maxIterations; i++ {
		// Amplified load = base rate × (1 + error_rate × retries)
		amplification := 1.0 + (currentErrorRate * maxRetries)
		newRate := baseRequestRate * amplification

		// Model error rate increase under amplified load
		// If load exceeds capacity, error rate increases proportionally
		var newErrorRate float64
		if capacity > 0 && newRate > capacity {
			// Overloaded: errors increase based on how much over capacity we are
			overloadFactor := (newRate - capacity) / capacity
			newErrorRate = baseErrorRate + (1.0-baseErrorRate)*clamp(overloadFactor, 0, 1)
		} else {
			newErrorRate = baseErrorRate
		}

		// Check convergence: if load and error rate stopped changing significantly
		rateDelta := abs(newRate - currentRate)
		errorDelta := abs(newErrorRate - currentErrorRate)
		if rateDelta < 0.01*baseRequestRate && errorDelta < 0.001 {
			converged = true
		}

		currentRate = newRate
		currentErrorRate = clamp(newErrorRate, 0, 1.0)

		if converged {
			break
		}
	}

	amplificationFactor := 1.0
	if baseRequestRate > 0 {
		amplificationFactor = currentRate / baseRequestRate
	}

	// Classify result based on amplification and error rate
	if currentErrorRate > 0.9 || (capacity > 0 && currentRate > capacity*2) {
		// Extreme amplification or near-total failure → exhausted
		return ModelResult{
			TargetState:    StateExhausted,
			TimeToFailure:  -1, // Already in failure state
			AdditionalLoad: amplificationFactor,
		}
	}

	if amplificationFactor > 1.5 || currentErrorRate > 0.5 {
		// Significant amplification — degraded
		return ModelResult{
			TargetState:    StateDegraded,
			TimeToFailure:  -1,
			AdditionalLoad: amplificationFactor,
		}
	}

	return ModelResult{
		TargetState:    StateHealthy,
		TimeToFailure:  -1,
		AdditionalLoad: amplificationFactor,
	}
}

// clamp restricts v to the range [lo, hi].
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// abs returns the absolute value of a float64.
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
