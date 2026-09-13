package graph

import (
	"context"
	"log/slog"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/probe"
	"github.com/abhinay0804/cascadeshield/pkg/resolver"
)

// Builder consumes resolved kernel events and continuously updates the live Service Dependency DAG.
type Builder struct {
	dag    *DAG
	config Config
	logger *slog.Logger
}

// NewBuilder creates a new Graph Builder wrapping the target DAG and configuration.
func NewBuilder(dag *DAG, cfg Config, logger *slog.Logger) *Builder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Builder{
		dag:    dag,
		config: cfg,
		logger: logger,
	}
}

// DAG returns the underlying live DAG instance.
func (b *Builder) DAG() *DAG {
	return b.dag
}

// Run starts the event worker loop, stale edge pruner ticker, and topology summary logger.
// It blocks until the provided context is cancelled or the input channel is closed.
func (b *Builder) Run(ctx context.Context, events <-chan resolver.ResolvedEvent) {
	pruneTicker := time.NewTicker(b.config.PruneInterval)
	defer pruneTicker.Stop()

	logTicker := time.NewTicker(b.config.TopologyLogInterval)
	defer logTicker.Stop()

	b.logger.Info("Graph Builder worker started listening for resolved events")

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("Graph Builder shutting down on context cancellation",
				"total_nodes", b.dag.NodeCount(),
				"total_edges", b.dag.EdgeCount())
			return

		case <-pruneTicker.C:
			b.dag.PruneStaleEdges()

		case <-logTicker.C:
			b.logTopologySummary()

		case evt, ok := <-events:
			if !ok {
				b.logger.Info("Resolved events channel closed, Graph Builder stopping")
				return
			}
			b.processEvent(evt)
		}
	}
}

// processEvent routes a resolved eBPF event to mutate nodes and edges in the DAG.
func (b *Builder) processEvent(evt resolver.ResolvedEvent) {
	if evt.SourceService == "" || evt.TargetService == "" {
		return // Skip unidentifiable traffic
	}

	switch evt.Raw.Type {
	case probe.EventConnect, probe.EventAccept:
		b.dag.RecordConnect(evt.SourceService, evt.TargetService, evt.Namespace)

	case probe.EventClose:
		b.dag.RecordClose(
			evt.SourceService,
			evt.TargetService,
			evt.Raw.BytesSent,
			evt.Raw.BytesReceived,
			evt.Raw.DurationNs,
		)

	case probe.EventRetransmit:
		b.dag.RecordRetransmit(evt.SourceService, evt.TargetService, evt.Raw.Retransmits)
	}
}

// logTopologySummary logs periodic snapshot metrics of the live dependency graph.
func (b *Builder) logTopologySummary() {
	nodesCount := b.dag.NodeCount()
	edgesCount := b.dag.EdgeCount()

	b.logger.Info("DAG Topology Summary",
		"nodes", nodesCount,
		"edges", edgesCount)

	edges := b.dag.GetAllEdges()
	for _, e := range edges {
		b.logger.Debug("DAG Edge Detail",
			"source", e.Source,
			"target", e.Target,
			"total_conn", e.TotalConnections,
			"active_conn", e.ActiveConnections,
			"retransmits", e.TotalRetransmits)
	}
}
