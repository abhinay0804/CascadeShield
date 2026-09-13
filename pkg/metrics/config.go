// Package metrics implements the Phase 3 Metrics Engine for CascadeShield.
//
// This package attaches real-time statistical metrics to every edge in the
// service dependency DAG: request rate, latency percentiles (P50/P95/P99),
// error rate, and throughput — computed using sliding window aggregation over
// raw eBPF event counters.
package metrics

import (
	"time"
)

// Config holds all tunables for the Metrics Engine.
//
// ZERO HARDCODING POLICY: Every threshold, interval, and capacity is
// configurable here. No magic numbers in implementation files.
type Config struct {
	// WindowDuration is the total span of the sliding window.
	// Metrics represent a rolling average over this time period.
	// Default: 60 seconds.
	WindowDuration time.Duration

	// BucketCount is the number of tumbling buckets the window is divided into.
	// A larger count gives finer granularity but uses more memory.
	// The slide interval is derived as WindowDuration / BucketCount.
	// Default: 12 (one bucket per 5 seconds).
	BucketCount int

	// HistogramMinMs is the minimum value (in milliseconds) tracked by the
	// HDR histogram. Values below this are clamped to the minimum bucket.
	// Default: 0 (sub-millisecond connections)
	HistogramMinMs int64

	// HistogramMaxMs is the maximum value (in milliseconds) tracked.
	// Values above this are clamped to the maximum bucket.
	// Default: 30000 (30 seconds — covers even the slowest connections)
	HistogramMaxMs int64

	// HistogramSigFigs controls histogram precision (number of significant figures).
	// Higher values = more buckets, more memory, more accuracy.
	// Default: 3 (0.1% relative error)
	HistogramSigFigs int

	// AnnotateInterval is how often the Annotator rewrites EdgeMetrics into the DAG.
	// Should equal SlideInterval (WindowDuration / BucketCount) in most cases.
	// Default: derived from WindowDuration / BucketCount.
	AnnotateInterval time.Duration

	// MetricsAddr is the TCP address for the Prometheus HTTP exposition endpoint.
	// Default: ":9090"
	MetricsAddr string

	// MaxEdgesTracked is the maximum number of distinct edges for which metric
	// state is maintained. Protects against unbounded memory growth.
	// Default: 10000
	MaxEdgesTracked int
}

// DefaultConfig returns a Config pre-filled with production-safe defaults.
// All values are intentionally exposed so operators can tune without recompiling.
func DefaultConfig() Config {
	windowDuration := 60 * time.Second
	bucketCount := 12

	return Config{
		WindowDuration:   windowDuration,
		BucketCount:      bucketCount,
		HistogramMinMs:   0,
		HistogramMaxMs:   30000, // 30 seconds max connection lifetime
		HistogramSigFigs: 3,
		// SlideInterval derived; annotator runs every slide interval
		AnnotateInterval: windowDuration / time.Duration(bucketCount),
		MetricsAddr:      ":9090",
		MaxEdgesTracked:  10000,
	}
}

// SlideInterval returns the duration of each individual bucket.
// This is the smallest granularity at which the window "slides".
func (c Config) SlideInterval() time.Duration {
	if c.BucketCount <= 0 {
		return 5 * time.Second
	}
	return c.WindowDuration / time.Duration(c.BucketCount)
}
