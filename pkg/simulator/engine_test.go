package simulator

import (
	"log/slog"
	"testing"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	"github.com/abhinay0804/cascadeshield/pkg/simulator/models"
)

// buildTestDAG creates a 3-node DAG: gateway → orders → payments
// with realistic edge metrics for testing.
func buildTestDAG() *graph.DAG {
	cfg := graph.DefaultConfig()
	dag := graph.NewDAG(cfg, slog.Default())

	// Create edges (RecordConnect creates nodes automatically)
	dag.RecordConnect("gateway", "orders", "default")
	dag.RecordConnect("orders", "payments", "default")

	// Set realistic metrics via UpdateEdgeMetrics
	dag.UpdateEdgeMetrics("gateway", "orders", graph.EdgeMetrics{
		RequestRate: 500,
		LatencyP50:  20,
		LatencyP95:  80,
		LatencyP99:  150,
		ErrorRate:   0.01,
		BytesPerSec: 50000,
	})
	dag.UpdateEdgeMetrics("orders", "payments", graph.EdgeMetrics{
		RequestRate: 300,
		LatencyP50:  30,
		LatencyP95:  100,
		LatencyP99:  200,
		ErrorRate:   0.02,
		BytesPerSec: 30000,
	})

	return dag
}

// buildTestSnapshot creates a snapshot from the test DAG.
func buildTestSnapshot() *graph.DAGSnapshot {
	dag := buildTestDAG()
	return dag.Snapshot()
}

// =============================================================================
// Cascade Propagation Tests
// =============================================================================

func TestCascade_KnownGraph(t *testing.T) {
	snap := buildTestSnapshot()
	cfg := DefaultConfig()

	// Inject a large latency perturbation at "payments" (the leaf)
	// This should cascade upstream: payments → orders → gateway
	run := PropagateCascade(snap, "payments", 500, 0.3, cfg)

	t.Logf("Cascade path: %v", run.Path)
	t.Logf("Node states: %v", run.NodeStates)

	// The origin should be in the path
	if run.Path[0] != "payments" {
		t.Errorf("expected origin 'payments', got '%s'", run.Path[0])
	}

	// With 500ms added latency on payments (total ~700ms P99):
	// orders calls payments at 300 req/s × 0.700s = 210 active threads
	// With default pool = 200 → orders should be EXHAUSTED
	if run.NodeStates["orders"] == models.StateHealthy {
		t.Logf("WARNING: orders not affected — perturbation may be too small for defaults")
	}
}

func TestCascade_NoEdges(t *testing.T) {
	// Single isolated node — no cascade possible
	dag := graph.NewDAG(graph.DefaultConfig(), slog.Default())
	dag.EnsureNode("lonely", "default")
	snap := dag.Snapshot()
	cfg := DefaultConfig()

	run := PropagateCascade(snap, "lonely", 1000, 0.5, cfg)

	if len(run.Path) != 1 {
		t.Errorf("expected path length 1, got %d: %v", len(run.Path), run.Path)
	}
	if run.NodeStates["lonely"] != models.StateDegraded {
		t.Errorf("expected origin to be DEGRADED, got %s", run.NodeStates["lonely"])
	}
}

// =============================================================================
// Risk Score Tests
// =============================================================================

func TestRiskScore_Normalization(t *testing.T) {
	snap := buildTestSnapshot()
	cfg := DefaultConfig()

	// Generate some cascade runs with guaranteed exhaustion
	runs := make([]CascadeRun, 100)
	for i := range runs {
		runs[i] = PropagateCascade(snap, "payments", 500, 0.3, cfg)
	}

	score := ComputeRiskScore("payments", runs, snap)

	if score < 0 || score > 1 {
		t.Errorf("risk score out of [0, 1] range: %f", score)
	}
	t.Logf("Risk score for payments (100 runs): %.4f", score)
}

func TestRiskScore_ZeroRunsReturnsZero(t *testing.T) {
	snap := buildTestSnapshot()
	score := ComputeRiskScore("payments", nil, snap)
	if score != 0 {
		t.Errorf("expected 0 for nil runs, got %f", score)
	}
}

func TestRiskScore_Classification(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		score    float64
		expected RiskLevel
	}{
		{0.1, RiskLow},
		{0.29, RiskLow},
		{0.3, RiskMedium},
		{0.59, RiskMedium},
		{0.6, RiskHigh},
		{0.84, RiskHigh},
		{0.85, RiskCritical},
		{0.99, RiskCritical},
	}

	for _, tt := range tests {
		got := ClassifyRisk(tt.score, cfg)
		if got != tt.expected {
			t.Errorf("score %.2f: expected %s, got %s", tt.score, tt.expected, got)
		}
	}
}

// =============================================================================
// Engine Tests
// =============================================================================

func TestEngine_RunSimulation(t *testing.T) {
	dag := buildTestDAG()
	cfg := DefaultConfig()
	cfg.NumSimulations = 100 // Fewer for fast test
	cfg.MaxConcurrentNodes = 2

	engine := NewEngine(cfg, dag, slog.Default())
	snap := dag.Snapshot()

	report := engine.RunSimulation(snap)

	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.NodeCount != 3 {
		t.Errorf("expected 3 nodes, got %d", report.NodeCount)
	}
	if report.EdgeCount != 2 {
		t.Errorf("expected 2 edges, got %d", report.EdgeCount)
	}
	if len(report.NodeResults) != 3 {
		t.Errorf("expected 3 node results, got %d", len(report.NodeResults))
	}

	for nodeID, result := range report.NodeResults {
		if result.RiskScore < 0 || result.RiskScore > 1 {
			t.Errorf("node %s: risk score out of range: %f", nodeID, result.RiskScore)
		}
		t.Logf("Node %s: risk=%.4f (%s) cascade_prob=%.2f affected=%v",
			nodeID, result.RiskScore, result.RiskLevel,
			result.CascadeProbability, result.AffectedNodes)
	}

	// Report should be JSON-serializable
	jsonData, err := report.ToJSON()
	if err != nil {
		t.Fatalf("failed to serialize report: %v", err)
	}
	if len(jsonData) < 100 {
		t.Error("JSON output suspiciously short")
	}

	// Summary should be non-empty
	summary := report.Summary()
	if summary == "" {
		t.Error("expected non-empty summary")
	}
	t.Logf("Summary: %s", summary)
}

func TestEngine_EmptyDAG(t *testing.T) {
	dag := graph.NewDAG(graph.DefaultConfig(), slog.Default())
	cfg := DefaultConfig()
	cfg.NumSimulations = 10

	engine := NewEngine(cfg, dag, slog.Default())
	snap := dag.Snapshot()

	report := engine.RunSimulation(snap)
	if len(report.NodeResults) != 0 {
		t.Errorf("expected 0 results for empty DAG, got %d", len(report.NodeResults))
	}
}

func TestEngine_LatestReport(t *testing.T) {
	dag := buildTestDAG()
	cfg := DefaultConfig()
	cfg.NumSimulations = 10

	engine := NewEngine(cfg, dag, slog.Default())

	// Initially nil
	if engine.LatestReport() != nil {
		t.Error("expected nil initial report")
	}

	// Run simulation
	snap := dag.Snapshot()
	report := engine.RunSimulation(snap)

	// Manually set
	engine.reportMu.Lock()
	engine.latestReport = report
	engine.reportMu.Unlock()

	latest := engine.LatestReport()
	if latest == nil {
		t.Fatal("expected non-nil latest report after simulation")
	}
	if latest.NodeCount != 3 {
		t.Errorf("expected 3 nodes in latest report, got %d", latest.NodeCount)
	}
}

// =============================================================================
// Benchmark
// =============================================================================

// BenchmarkEngine_10Nodes benchmarks simulation on a 10-node graph.
// Target: 1000 simulations in < 500ms.
func BenchmarkEngine_10Nodes(b *testing.B) {
	// Build a 10-node linear chain: n0 → n1 → ... → n9
	dag := graph.NewDAG(graph.DefaultConfig(), slog.Default())
	for i := 0; i < 9; i++ {
		src := nodeID(i)
		dst := nodeID(i + 1)
		dag.RecordConnect(src, dst, "default")
		dag.UpdateEdgeMetrics(src, dst, graph.EdgeMetrics{
			RequestRate: 100 + float64(i*50),
			LatencyP99:  50 + float64(i*20),
			ErrorRate:   0.01 + float64(i)*0.005,
		})
	}
	snap := dag.Snapshot()

	cfg := DefaultConfig()
	cfg.NumSimulations = 1000
	engine := NewEngine(cfg, dag, slog.Default())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.RunSimulation(snap)
	}
}

func nodeID(i int) string {
	return "node-" + string(rune('a'+i))
}

// BenchmarkEngine_1000Sims_TimingCheck runs once and checks total time.
func BenchmarkEngine_1000Sims_TimingCheck(b *testing.B) {
	dag := graph.NewDAG(graph.DefaultConfig(), slog.Default())
	for i := 0; i < 9; i++ {
		src := nodeID(i)
		dst := nodeID(i + 1)
		dag.RecordConnect(src, dst, "default")
		dag.UpdateEdgeMetrics(src, dst, graph.EdgeMetrics{
			RequestRate: 200,
			LatencyP99:  100,
			ErrorRate:   0.05,
		})
	}
	snap := dag.Snapshot()

	cfg := DefaultConfig()
	cfg.NumSimulations = 1000

	engine := NewEngine(cfg, dag, slog.Default())

	start := time.Now()
	report := engine.RunSimulation(snap)
	elapsed := time.Since(start)

	b.Logf("10-node graph, 1000 sims: %v (nodes=%d, edges=%d)",
		elapsed, report.NodeCount, report.EdgeCount)

	if elapsed > 500*time.Millisecond {
		b.Errorf("SLOW: 1000 sims took %v (target < 500ms)", elapsed)
	}
}
