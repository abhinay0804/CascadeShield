package graph

import (
	"context"
	"testing"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/probe"
	"github.com/abhinay0804/cascadeshield/pkg/resolver"
)

func TestBuilder_ProcessEvents(t *testing.T) {
	cfg := DefaultConfig()
	dag := NewDAG(cfg, nil)
	builder := NewBuilder(dag, cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := make(chan resolver.ResolvedEvent, 10)

	// Start builder in background goroutine
	go builder.Run(ctx, eventsCh)

	// Send CONNECT event
	eventsCh <- resolver.ResolvedEvent{
		Raw: probe.Event{
			Type: probe.EventConnect,
			PID:  1234,
		},
		SourceService: "orders",
		TargetService: "payments",
		Namespace:     "default",
	}

	// Wait for builder to process
	time.Sleep(50 * time.Millisecond)

	if dag.NodeCount() != 2 {
		t.Fatalf("expected 2 nodes, got %d", dag.NodeCount())
	}
	edge, ok := dag.GetEdge("orders", "payments")
	if !ok || edge.TotalConnections != 1 || edge.ActiveConnections != 1 {
		t.Fatalf("expected edge with 1 active connection, got %+v", edge)
	}

	// Send CLOSE event
	eventsCh <- resolver.ResolvedEvent{
		Raw: probe.Event{
			Type:          probe.EventClose,
			PID:           1234,
			BytesSent:     1024,
			BytesReceived: 2048,
			DurationNs:    5000000,
		},
		SourceService: "orders",
		TargetService: "payments",
		Namespace:     "default",
	}

	time.Sleep(50 * time.Millisecond)

	edge, _ = dag.GetEdge("orders", "payments")
	if edge.ActiveConnections != 0 {
		t.Errorf("expected 0 active connections after close, got %d", edge.ActiveConnections)
	}
	if edge.TotalBytesSent != 1024 || edge.TotalBytesRecv != 2048 {
		t.Errorf("bytes mismatch on edge: sent=%d recv=%d", edge.TotalBytesSent, edge.TotalBytesRecv)
	}

	cancel()
}
