package graph

import (
	"time"
)

// Config holds configuration options for the live Service Dependency DAG.
// ZERO HARDCODING POLICY: All graph parameters, pruning intervals, and safety limits
// must be explicitly declared here with defaults provided by DefaultConfig().
type Config struct {
	// StaleEdgeTimeout is the duration after which an inactive edge is pruned (default: 5 minutes).
	StaleEdgeTimeout time.Duration

	// PruneInterval controls how often the stale edge pruning background goroutine runs (default: 30 seconds).
	PruneInterval time.Duration

	// MaxNodes is the maximum allowed nodes in the graph to prevent memory exhaustion (default: 1000).
	MaxNodes int

	// MaxEdgesPerNode limits outbound connections per node (default: 100).
	MaxEdgesPerNode int

	// TopologyLogInterval is how often the builder logs a summary of the current DAG (default: 30 seconds).
	TopologyLogInterval time.Duration
}

// DefaultConfig returns a Config struct initialized with production-tested defaults.
func DefaultConfig() Config {
	return Config{
		StaleEdgeTimeout:    5 * time.Minute,
		PruneInterval:       30 * time.Second,
		MaxNodes:            1000,
		MaxEdgesPerNode:     100,
		TopologyLogInterval: 30 * time.Second,
	}
}
