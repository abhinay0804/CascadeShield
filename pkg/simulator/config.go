// Package simulator implements Phase 4: The Oracle — a Monte Carlo cascade
// failure simulator for CascadeShield.
//
// The Oracle takes a snapshot of the live Service Dependency DAG (with real-time
// EdgeMetrics from Phase 3), and runs thousands of Monte Carlo simulations to
// predict which services are most likely to trigger cascading failures, how fast
// those cascades would propagate, and what the cascade paths look like.
//
// This file defines all configurable parameters for the simulation engine.
package simulator

import (
	"runtime"
	"time"
)

// Config holds all tunables for the Monte Carlo cascade simulator.
//
// ZERO HARDCODING POLICY: Every threshold, pool size, distribution parameter,
// and concurrency limit is configurable here with sensible defaults.
type Config struct {
	// --- Simulation Parameters ---

	// NumSimulations is the number of Monte Carlo runs per origin node.
	// Higher = more accurate P(cascade) estimates, but slower.
	// Default: 1000
	NumSimulations int

	// SimulationInterval is how often The Oracle runs a full simulation cycle
	// on the latest DAG snapshot.
	// Default: 30 seconds
	SimulationInterval time.Duration

	// MaxConcurrentNodes is the maximum number of origin nodes simulated in parallel.
	// Bounded by a semaphore to avoid overwhelming the CPU.
	// Default: runtime.NumCPU()
	MaxConcurrentNodes int

	// --- Perturbation Parameters ---

	// PerturbationStdDev controls the spread of the normal distribution used
	// to sample latency and error rate perturbations. Expressed as a fraction
	// of the current metric value.
	//   - 0.3 means perturbations are drawn from N(0, (0.3 × currentValue)²)
	//   - Higher values model more volatile / unpredictable environments
	// Default: 0.3
	PerturbationStdDev float64

	// MinPerturbationMs is the minimum absolute latency perturbation injected,
	// ensuring even low-latency edges get meaningful perturbations.
	// Default: 10ms
	MinPerturbationMs float64

	// --- Default Service Parameters ---
	// These are used when K8s ResourceLimits are not available (bare-metal mode).

	// DefaultThreadPoolSize is the assumed thread pool capacity for services
	// where we can't discover the actual value from K8s resource limits.
	//
	// Rationale: 200 matches the Apache Tomcat default (the most common
	// Java microservice container) and is reasonable for Go services which
	// use goroutines but still have practical concurrency limits from
	// downstream dependencies.
	// Default: 200
	DefaultThreadPoolSize int

	// DefaultConnPoolSize is the assumed connection pool capacity.
	//
	// Rationale: Database connection pools (HikariCP default = 10,
	// PgBouncer default = 100) are fundamentally smaller than thread pools.
	// 50 is a generous middle-ground that avoids false positives while
	// still catching real saturation.
	// Default: 50
	DefaultConnPoolSize int

	// DefaultTimeout is the per-edge request timeout when not explicitly configured.
	// Default: 5 seconds
	DefaultTimeout time.Duration

	// DefaultMaxRetries is the number of retry attempts per edge.
	// Default: 3
	DefaultMaxRetries int

	// DefaultRetryBackoff is the base delay between retries.
	// Default: 100ms
	DefaultRetryBackoff time.Duration

	// --- Risk Classification Thresholds ---

	// RiskThresholdLow: scores below this are classified as LOW risk.
	// Default: 0.3
	RiskThresholdLow float64

	// RiskThresholdMedium: scores below this are classified as MEDIUM risk.
	// Default: 0.6
	RiskThresholdMedium float64

	// RiskThresholdHigh: scores below this are classified as HIGH risk.
	// Scores at or above this are CRITICAL.
	// Default: 0.85
	RiskThresholdHigh float64

	// --- Retry Model Safety ---

	// MaxRetryIterations caps the fixed-point iteration loop in the retry
	// amplification model to prevent divergence in positive feedback loops.
	// Default: 10
	MaxRetryIterations int

	// TopCascadePathsCount is how many top cascade paths to keep per node
	// in the simulation report (sorted by frequency).
	// Default: 3
	TopCascadePathsCount int
}

// DefaultConfig returns a Config pre-filled with production-safe defaults.
func DefaultConfig() Config {
	return Config{
		NumSimulations:     1000,
		SimulationInterval: 30 * time.Second,
		MaxConcurrentNodes: runtime.NumCPU(),

		PerturbationStdDev: 0.3,
		MinPerturbationMs:  10.0,

		DefaultThreadPoolSize: 200,
		DefaultConnPoolSize:   50,
		DefaultTimeout:        5 * time.Second,
		DefaultMaxRetries:     3,
		DefaultRetryBackoff:   100 * time.Millisecond,

		RiskThresholdLow:    0.3,
		RiskThresholdMedium: 0.6,
		RiskThresholdHigh:   0.85,

		MaxRetryIterations:   10,
		TopCascadePathsCount: 3,
	}
}
