package metrics

import (
	"sync"
)

// EdgeKey uniquely identifies a directed service-to-service edge.
type EdgeKey struct {
	Source string
	Target string
}

// EdgeState holds all sliding window and histogram state for a single DAG edge.
//
// For beginners: each service-to-service edge in the dependency graph has its
// own "metrics accumulator" here. As eBPF events flow in, they are recorded
// into this struct. Periodically, the Annotator reads from here to compute
// the actual metric values and writes them back to the DAG edge.
type EdgeState struct {
	// Window tracks connection rates, bytes, retransmits via tumbling buckets.
	Window *Window

	// Histogram tracks latency percentiles over connection lifetime (duration_ns).
	// It is kept in sync with the Window — when the window advances, a new
	// rolling histogram is maintained by accumulating across all current buckets.
	Histogram *Histogram

	// mu protects the per-bucket histograms (one per bucket, for rotation).
	mu sync.Mutex

	// perBucketHistograms stores one histogram per window bucket so that when
	// a bucket expires, its latency contribution can also be removed.
	// This implements a "sliding histogram" using a ring of histograms.
	perBucketHists []*Histogram
	currentHistIdx int
	cfg            Config
}

// NewEdgeState creates a fresh EdgeState for a newly observed edge.
func NewEdgeState(cfg Config) *EdgeState {
	perBucketHists := make([]*Histogram, cfg.BucketCount)
	for i := range perBucketHists {
		perBucketHists[i] = NewHistogram(cfg.HistogramMinMs, cfg.HistogramMaxMs, cfg.HistogramSigFigs)
	}
	return &EdgeState{
		Window:         NewWindow(cfg.BucketCount, cfg.WindowDuration),
		Histogram:      NewHistogram(cfg.HistogramMinMs, cfg.HistogramMaxMs, cfg.HistogramSigFigs),
		perBucketHists: perBucketHists,
		currentHistIdx: cfg.BucketCount - 1,
		cfg:            cfg,
	}
}

// RecordConnect records a new TCP connection for this edge.
func (s *EdgeState) RecordConnect() {
	s.Window.RecordConnect()
}

// RecordClose records a completed connection with its duration and byte counts.
func (s *EdgeState) RecordClose(durationNs, bytesSent, bytesRecv int64) {
	s.Window.RecordClose(durationNs, bytesSent, bytesRecv)
	// Record into the global rolling histogram for percentile queries
	s.Histogram.RecordNs(durationNs)
}

// RecordRetransmit records a TCP retransmit event for this edge.
func (s *EdgeState) RecordRetransmit() {
	s.Window.RecordRetransmit()
}

// ComputeMetrics derives EdgeMetrics from the current window state and histogram.
// This is the main function the Annotator calls on a ticker to compute the
// values written back to the DAG's EdgeMetrics struct.
//
// Returns (requestRate, p50ms, p95ms, p99ms, errorRate, bytesPerSec).
func (s *EdgeState) ComputeMetrics() (requestRate, p50ms, p95ms, p99ms, errorRate, bytesPerSec float64) {
	stats := s.Window.Aggregate()

	windowSecs := stats.WindowDurationSecs
	if windowSecs <= 0 {
		windowSecs = 1
	}

	// Request rate = connections per second
	requestRate = float64(stats.TotalConnections) / windowSecs

	// Latency percentiles from the rolling histogram
	p50ms = s.Histogram.Percentile(50)
	p95ms = s.Histogram.Percentile(95)
	p99ms = s.Histogram.Percentile(99)

	// Error rate = retransmits / total connections (fraction 0..1)
	if stats.TotalConnections > 0 {
		errorRate = float64(stats.TotalRetransmits) / float64(stats.TotalConnections)
		if errorRate > 1.0 {
			errorRate = 1.0 // clamp: can't exceed 100%
		}
	}

	// Throughput = total bytes (both directions) / window duration
	totalBytes := stats.TotalBytesSent + stats.TotalBytesRecv
	bytesPerSec = float64(totalBytes) / windowSecs

	return
}
