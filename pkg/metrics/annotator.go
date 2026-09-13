package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	"github.com/abhinay0804/cascadeshield/pkg/probe"
	"github.com/abhinay0804/cascadeshield/pkg/resolver"
)

// Annotator is the Phase 3 orchestrator. It:
//   1. Consumes a stream of resolved eBPF events (same events that go to the DAG Builder).
//   2. Maintains per-edge sliding windows and histograms.
//   3. On each tick (AnnotateInterval), computes derived EdgeMetrics and writes them
//      into the live DAG via UpdateEdgeMetrics.
//
// For beginners: think of the Annotator as a "statistical bookkeeper" running in its own
// goroutine. The DAG Builder cares about TOPOLOGY (which services talk to which).
// The Annotator cares about PERFORMANCE (how fast, how reliable are those connections).
// Both workers read from the same event stream (tee'd in main.go) and write to the DAG —
// the Builder writes structural edges, the Annotator writes metric values on those edges.
type Annotator struct {
	cfg    Config
	dag    *graph.DAG
	logger *slog.Logger

	// mu protects the edgeStates map
	mu         sync.RWMutex
	edgeStates map[EdgeKey]*EdgeState
}

// NewAnnotator creates a new Metrics Annotator.
//
// Parameters:
//   - cfg: Metrics config (window duration, histogram bounds, etc.)
//   - dag: The live service dependency DAG to write EdgeMetrics into
//   - logger: Structured logger
func NewAnnotator(cfg Config, dag *graph.DAG, logger *slog.Logger) *Annotator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Annotator{
		cfg:        cfg,
		dag:        dag,
		logger:     logger,
		edgeStates: make(map[EdgeKey]*EdgeState, 64),
	}
}

// Run starts the Annotator event loop.
//
// It blocks until ctx is cancelled. Internally it runs two concurrent operations:
//   1. A goroutine that reads from `events` and updates per-edge state.
//   2. A ticker that fires every AnnotateInterval to compute and write EdgeMetrics.
//
// Go Pattern: context cancellation propagates to both loops.
func (a *Annotator) Run(ctx context.Context, events <-chan resolver.ResolvedEvent) {
	annotateTicker := time.NewTicker(a.cfg.AnnotateInterval)
	defer annotateTicker.Stop()

	a.logger.Info("Metrics Annotator started",
		"window_duration", a.cfg.WindowDuration,
		"bucket_count", a.cfg.BucketCount,
		"annotate_interval", a.cfg.AnnotateInterval,
	)

	for {
		select {
		case <-ctx.Done():
			a.logger.Info("Metrics Annotator shutting down on context cancellation")
			return

		case evt, ok := <-events:
			if !ok {
				a.logger.Info("Metrics Annotator: events channel closed")
				return
			}
			a.processEvent(evt)

		case <-annotateTicker.C:
			a.flush()
		}
	}
}

// processEvent routes a resolved event to the appropriate EdgeState counter.
func (a *Annotator) processEvent(evt resolver.ResolvedEvent) {
	// We only track events with both sides resolved to service names
	if evt.SourceService == "" || evt.TargetService == "" {
		return
	}

	key := EdgeKey{Source: evt.SourceService, Target: evt.TargetService}
	state := a.getOrCreateEdgeState(key)
	if state == nil {
		return // capacity limit reached
	}

	switch evt.Raw.Type {
	case probe.EventConnect, probe.EventAccept:
		state.RecordConnect()

	case probe.EventClose:
		// Duration is in nanoseconds from the eBPF event.
		// Bytes are uint64 from the event struct — cast to int64 for window counters.
		// Overflow is practically impossible (max TCP bytes per connection < 2^63).
		state.RecordClose(
			int64(evt.Raw.DurationNs),
			int64(evt.Raw.BytesSent),
			int64(evt.Raw.BytesReceived),
		)

	case probe.EventRetransmit:
		state.RecordRetransmit()
	}
}

// flush computes EdgeMetrics for every tracked edge and writes them into the DAG.
// This runs on the AnnotateInterval ticker (default every 5 seconds).
func (a *Annotator) flush() {
	a.mu.RLock()
	// Snapshot the keys so we don't hold the read lock while writing to the DAG
	type kv struct {
		key   EdgeKey
		state *EdgeState
	}
	entries := make([]kv, 0, len(a.edgeStates))
	for k, v := range a.edgeStates {
		entries = append(entries, kv{k, v})
	}
	a.mu.RUnlock()

	for _, entry := range entries {
		reqRate, p50, p95, p99, errRate, bps := entry.state.ComputeMetrics()

		m := graph.EdgeMetrics{
			RequestRate:    reqRate,
			LatencyP50:     p50,
			LatencyP95:     p95,
			LatencyP99:     p99,
			ErrorRate:      errRate,
			RetransmitRate: errRate, // Currently same signal as error rate
			BytesPerSec:    bps,
		}

		a.dag.UpdateEdgeMetrics(entry.key.Source, entry.key.Target, m)
	}

	if len(entries) > 0 {
		a.logger.Debug("Metrics Annotator flush complete",
			"edges_annotated", len(entries),
		)
	}
}

// getOrCreateEdgeState returns the existing EdgeState for a key, or creates one.
// Returns nil if the MaxEdgesTracked limit is reached.
func (a *Annotator) getOrCreateEdgeState(key EdgeKey) *EdgeState {
	// Fast path: read lock
	a.mu.RLock()
	if state, ok := a.edgeStates[key]; ok {
		a.mu.RUnlock()
		return state
	}
	a.mu.RUnlock()

	// Slow path: write lock to create new state
	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have created it)
	if state, ok := a.edgeStates[key]; ok {
		return state
	}

	if len(a.edgeStates) >= a.cfg.MaxEdgesTracked {
		a.logger.Warn("Metrics Annotator reached max edge tracking capacity",
			"max", a.cfg.MaxEdgesTracked,
			"source", key.Source,
			"target", key.Target,
		)
		return nil
	}

	state := NewEdgeState(a.cfg)
	a.edgeStates[key] = state
	a.logger.Debug("Metrics Annotator: tracking new edge",
		"source", key.Source,
		"target", key.Target,
		"total_tracked", len(a.edgeStates),
	)
	return state
}

// TrackedEdgeCount returns the number of edges currently being tracked.
// Used for testing and Prometheus exposition.
func (a *Annotator) TrackedEdgeCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.edgeStates)
}
