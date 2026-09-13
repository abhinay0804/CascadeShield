package graph

import (
	"sync"
	"testing"
	"time"
)

func TestDAG_AddNodeAndEdge(t *testing.T) {
	cfg := DefaultConfig()
	dag := NewDAG(cfg, nil)

	dag.RecordConnect("orders", "payments", "default")

	if dag.NodeCount() != 2 {
		t.Errorf("expected 2 nodes, got %d", dag.NodeCount())
	}
	if dag.EdgeCount() != 1 {
		t.Errorf("expected 1 edge, got %d", dag.EdgeCount())
	}

	// Verify upstream / downstream
	downstream := dag.GetDownstream("orders")
	if len(downstream) != 1 || downstream[0].Target != "payments" {
		t.Errorf("expected downstream payments from orders")
	}

	upstream := dag.GetUpstream("payments")
	if len(upstream) != 1 || upstream[0].Source != "orders" {
		t.Errorf("expected upstream orders for payments")
	}
}

func TestDAG_PruneStaleEdges(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StaleEdgeTimeout = 50 * time.Millisecond
	dag := NewDAG(cfg, nil)

	dag.RecordConnect("svc-a", "svc-b", "default")
	edge, ok := dag.GetEdge("svc-a", "svc-b")
	if !ok {
		t.Fatalf("expected edge svc-a -> svc-b")
	}

	// Wait past timeout
	time.Sleep(70 * time.Millisecond)

	pruned := dag.PruneStaleEdges()
	if pruned != 1 {
		t.Fatalf("expected 1 pruned edge, got %d (edge lastSeen=%v)", pruned, edge.LastSeen)
	}
	if dag.EdgeCount() != 0 {
		t.Errorf("expected 0 edges after prune, got %d", dag.EdgeCount())
	}
}

func TestDAG_ConcurrentRace(t *testing.T) {
	cfg := DefaultConfig()
	dag := NewDAG(cfg, nil)

	var wg sync.WaitGroup

	// Launch concurrent writers and readers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			src := "service-A"
			tgt := "service-B"
			dag.RecordConnect(src, tgt, "ns")
			dag.RecordClose(src, tgt, 100, 200, 5000)
			dag.RecordRetransmit(src, tgt, 1)
			_ = dag.GetAllNodes()
			_ = dag.GetAllEdges()
		}(i)
	}

	wg.Wait()
	if dag.EdgeCount() != 1 {
		t.Errorf("expected 1 unique edge, got %d", dag.EdgeCount())
	}
}
