package graph

import (
	"time"
)

// EdgeMetrics holds calculated performance indicators for a service dependency link.
// Phase 3 (Metrics Pipeline) will continuously compute these using sliding windows.
type EdgeMetrics struct {
	RequestRate    float64 `json:"request_rate"`    // connections/sec
	LatencyP50     float64 `json:"latency_p50_ms"`  // median lifetime latency (ms)
	LatencyP95     float64 `json:"latency_p95_ms"`  // 95th percentile latency (ms)
	LatencyP99     float64 `json:"latency_p99_ms"`  // 99th percentile latency (ms)
	ErrorRate      float64 `json:"error_rate"`      // 0.0 - 1.0 (connection failures / total)
	RetransmitRate float64 `json:"retransmit_rate"` // retransmits per second
	BytesPerSec    float64 `json:"bytes_per_sec"`   // combined throughput
}

// Edge represents a directed TCP traffic link from a Source service to a Target service.
type Edge struct {
	Source    string    `json:"source"`     // Source service ID
	Target    string    `json:"target"`     // Target service ID
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Active    bool      `json:"active"`     // true if traffic seen within StaleEdgeTimeout

	// Cumulative traffic counters populated directly by Phase 2 eBPF events
	TotalConnections  uint64 `json:"total_connections"`
	ActiveConnections int64  `json:"active_connections"` // incremented on CONNECT/ACCEPT, decremented on CLOSE
	TotalRetransmits  uint64 `json:"total_retransmits"`
	TotalBytesSent    uint64 `json:"total_bytes_sent"`
	TotalBytesRecv    uint64 `json:"total_bytes_recv"`
	TotalDurationNs   uint64 `json:"total_duration_ns"`  // accumulated lifetime duration of closed sockets
	ConnectionCount   uint64 `json:"connection_count"`   // number of completed CLOSE events

	// Computed metrics (updated by Phase 3 sliding window analyzer)
	Metrics EdgeMetrics `json:"metrics"`
}

// NewEdge creates a directed Edge between source and target services.
func NewEdge(source, target string) *Edge {
	now := time.Now()
	return &Edge{
		Source:    source,
		Target:    target,
		FirstSeen: now,
		LastSeen:  now,
		Active:    true,
	}
}

// UpdateLastSeen updates the edge activity timestamp and marks it as active.
func (e *Edge) UpdateLastSeen(t time.Time) {
	if t.After(e.LastSeen) {
		e.LastSeen = t
	}
	e.Active = true
}

// AvgDurationNs returns the average socket lifetime in nanoseconds across closed connections.
func (e *Edge) AvgDurationNs() float64 {
	if e.ConnectionCount == 0 {
		return 0
	}
	return float64(e.TotalDurationNs) / float64(e.ConnectionCount)
}
