package graph

import (
	"testing"
)

func TestDAG_Snapshot(t *testing.T) {
	cfg := DefaultConfig()
	dag := NewDAG(cfg, nil)

	n1 := dag.EnsureNode("orders", "default")
	n1.AddPod(PodInfo{Name: "orders-pod-1", Namespace: "default", IP: "10.0.0.1"})

	_ = dag.EnsureNode("payments", "default")
	dag.RecordConnect("orders", "payments", "default")

	// Take snapshot
	snap := dag.Snapshot()

	if len(snap.Nodes) != 2 {
		t.Fatalf("expected 2 snapshot nodes, got %d", len(snap.Nodes))
	}
	if len(snap.Edges["orders"]) != 1 {
		t.Fatalf("expected 1 snapshot edge for orders, got %d", len(snap.Edges["orders"]))
	}

	// Mutate live DAG after snapshot taken
	dag.RecordConnect("orders", "inventory", "default")

	// Verify snapshot remains unchanged (isolated deep copy)
	if len(snap.Nodes) != 2 {
		t.Errorf("snapshot was mutated! Expected 2 nodes, got %d", len(snap.Nodes))
	}

	// Test JSON serialization
	jsonBytes, err := snap.ToJSON()
	if err != nil || len(jsonBytes) == 0 {
		t.Fatalf("failed to serialize snapshot to JSON: %v", err)
	}

	deserialized, err := FromJSON(jsonBytes)
	if err != nil {
		t.Fatalf("failed to deserialize snapshot from JSON: %v", err)
	}
	if len(deserialized.Nodes) != 2 {
		t.Errorf("deserialized snapshot expected 2 nodes, got %d", len(deserialized.Nodes))
	}
}
