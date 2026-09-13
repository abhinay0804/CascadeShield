// Package chaos implements Phase 6: Chaos Arena — chaos injection, pre-built
// scenario definitions, and prediction outcome validation for CascadeShield.
package chaos

import (
	"time"
)

// Config holds all tunables for chaos injection and validation.
//
// ZERO HARDCODING POLICY: Every HTTP timeout, scenario step duration, and risk
// tolerance threshold is configurable here with sensible production defaults.
type Config struct {
	// HTTPTimeout is the request timeout when calling microservice /chaos endpoints.
	// Default: 5 seconds
	HTTPTimeout time.Duration

	// ScenarioDuration is the default execution window for a chaos experiment.
	// Default: 30 seconds
	ScenarioDuration time.Duration

	// ValidationLatencyToleranceMs is the max delta between predicted and measured latency.
	// Default: 50ms
	ValidationLatencyToleranceMs float64

	// MinimumAccuracyRatio is the minimum accuracy ratio (0.0 to 1.0) required for validation pass.
	// Default: 0.8 (80% accuracy)
	MinimumAccuracyRatio float64
}

// DefaultConfig returns safe defaults.
func DefaultConfig() Config {
	return Config{
		HTTPTimeout:                  5 * time.Second,
		ScenarioDuration:             30 * time.Second,
		ValidationLatencyToleranceMs: 50.0,
		MinimumAccuracyRatio:         0.80,
	}
}
