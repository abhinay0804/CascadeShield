package metrics

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	"github.com/abhinay0804/cascadeshield/pkg/probe"
	"github.com/abhinay0804/cascadeshield/pkg/resolver"
)

// makeTestEvent creates a synthetic ResolvedEvent for annotator tests.
func makeTestEvent(src, dst string, evtType probe.EventType, durationNs, bytesSent, bytesRecv uint64) resolver.ResolvedEvent {
	return resolver.ResolvedEvent{
		SourceService: src,
		TargetService: dst,
		Resolved:      true,
		Raw: probe.Event{
			Type:          evtType,
			DurationNs:    durationNs,
			BytesSent:     bytesSent,
			BytesReceived: bytesRecv,
		},
	}
}

func TestAnnotator_RecordsEvents(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AnnotateInterval = 50 * time.Millisecond
	cfg.WindowDuration = 5 * time.Second
	cfg.BucketCount = 5

	dag := graph.NewDAG(graph.DefaultConfig(), slog.Default())
	// Pre-create the edge in the DAG so UpdateEdgeMetrics has somewhere to write
	dag.RecordConnect("svc-a", "svc-b", "default")

	annotator := NewAnnotator(cfg, dag, slog.Default())
	events := make(chan resolver.ResolvedEvent, 32)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		annotator.Run(ctx, events)
	}()

	// Send CONNECT events
	for i := 0; i < 10; i++ {
		events <- makeTestEvent("svc-a", "svc-b", probe.EventConnect, 0, 0, 0)
	}
	// Send CLOSE events with 50ms duration
	for i := 0; i < 5; i++ {
		events <- makeTestEvent("svc-a", "svc-b", probe.EventClose, 50_000_000, 1024, 512)
	}
	// Send RETRANSMIT events
	for i := 0; i < 2; i++ {
		events <- makeTestEvent("svc-a", "svc-b", probe.EventRetransmit, 0, 0, 0)
	}

	// Wait for the annotator to flush
	time.Sleep(200 * time.Millisecond)
	cancel()
	wg.Wait()

	if annotator.TrackedEdgeCount() != 1 {
		t.Errorf("expected 1 tracked edge, got %d", annotator.TrackedEdgeCount())
	}

	edge, ok := dag.GetEdge("svc-a", "svc-b")
	if !ok {
		t.Fatal("edge svc-a→svc-b not found in DAG")
	}

	if edge.Metrics.RequestRate <= 0 {
		t.Errorf("expected positive request rate, got %f", edge.Metrics.RequestRate)
	}
	if edge.Metrics.ErrorRate <= 0 {
		t.Errorf("expected positive error rate (retransmits recorded), got %f", edge.Metrics.ErrorRate)
	}
	if edge.Metrics.BytesPerSec <= 0 {
		t.Errorf("expected positive bytes/sec, got %f", edge.Metrics.BytesPerSec)
	}
	// P50 latency should be near 50ms (±10ms)
	if edge.Metrics.LatencyP50 < 1 {
		t.Errorf("expected P50 latency > 1ms, got %f", edge.Metrics.LatencyP50)
	}

	t.Logf("Edge metrics: req/s=%.2f p50=%.2fms p95=%.2fms p99=%.2fms err=%.4f bps=%.0f",
		edge.Metrics.RequestRate,
		edge.Metrics.LatencyP50,
		edge.Metrics.LatencyP95,
		edge.Metrics.LatencyP99,
		edge.Metrics.ErrorRate,
		edge.Metrics.BytesPerSec,
	)
}

func TestAnnotator_IgnoresUnresolved(t *testing.T) {
	cfg := DefaultConfig()
	dag := graph.NewDAG(graph.DefaultConfig(), slog.Default())
	annotator := NewAnnotator(cfg, dag, slog.Default())

	events := make(chan resolver.ResolvedEvent, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		annotator.Run(ctx, events)
	}()

	// Send events with empty service names (unresolved)
	events <- resolver.ResolvedEvent{SourceService: "", TargetService: "svc-b"}
	events <- resolver.ResolvedEvent{SourceService: "svc-a", TargetService: ""}

	<-ctx.Done()
	wg.Wait()

	if annotator.TrackedEdgeCount() != 0 {
		t.Errorf("expected 0 tracked edges for unresolved events, got %d", annotator.TrackedEdgeCount())
	}
}

func TestAnnotator_MaxEdgesTracked(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxEdgesTracked = 2
	cfg.AnnotateInterval = time.Hour // don't flush during test

	dag := graph.NewDAG(graph.DefaultConfig(), slog.Default())
	annotator := NewAnnotator(cfg, dag, slog.Default())

	events := make(chan resolver.ResolvedEvent, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		annotator.Run(ctx, events)
	}()

	// Try to create 5 edges — only 2 should be tracked
	for i := 0; i < 5; i++ {
		src := string(rune('a' + i))
		events <- makeTestEvent(src, "svc-x", probe.EventConnect, 0, 0, 0)
		time.Sleep(10 * time.Millisecond) // pace events
	}

	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()

	if annotator.TrackedEdgeCount() > 2 {
		t.Errorf("expected at most 2 tracked edges (limit), got %d", annotator.TrackedEdgeCount())
	}
}
