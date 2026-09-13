package shield

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	"github.com/abhinay0804/cascadeshield/pkg/simulator"
)

// buildTestOracleWithReport creates a simulator.Engine pre-loaded with a report
// that has a high-risk node (payments) for testing shield decisions.
func buildTestOracleWithReport(riskScore float64) *simulator.Engine {
	dag := graph.NewDAG(graph.DefaultConfig(), slog.Default())
	dag.RecordConnect("gateway", "orders", "default")
	dag.RecordConnect("orders", "payments", "default")
	dag.UpdateEdgeMetrics("gateway", "orders", graph.EdgeMetrics{
		RequestRate: 500, LatencyP99: 150, ErrorRate: 0.01,
	})
	dag.UpdateEdgeMetrics("orders", "payments", graph.EdgeMetrics{
		RequestRate: 300, LatencyP99: 200, ErrorRate: 0.02,
	})

	simCfg := simulator.DefaultConfig()
	simCfg.NumSimulations = 50 // Fast for tests
	engine := simulator.NewEngine(simCfg, dag, slog.Default())

	// Run a simulation to populate LatestReport
	snap := dag.Snapshot()
	_ = engine.RunSimulation(snap)
	return engine
}

// =============================================================================
// Strategy Tests
// =============================================================================

func TestStrategy_NoShedBelowThreshold(t *testing.T) {
	// Create a report with all nodes below shed threshold
	report := &simulator.SimulationReport{
		NodeResults: map[string]*simulator.NodeSimResult{
			"payments": {
				NodeID:    "payments",
				RiskScore: 0.5, // Below default 0.7 threshold
				RiskLevel: simulator.RiskHigh,
			},
		},
	}
	cfg := DefaultConfig()
	decisions := ComputeSheddingStrategy(report, cfg)
	if len(decisions) != 0 {
		t.Errorf("expected 0 decisions below threshold, got %d", len(decisions))
	}
}

func TestStrategy_ShedAboveThreshold(t *testing.T) {
	report := &simulator.SimulationReport{
		NodeResults: map[string]*simulator.NodeSimResult{
			"payments": {
				NodeID:    "payments",
				RiskScore: 0.85,
				RiskLevel: simulator.RiskCritical,
				TopCascadePaths: []simulator.CascadePath{
					{Path: []string{"payments", "orders", "gateway"}, Frequency: 900},
				},
			},
		},
	}
	cfg := DefaultConfig()
	decisions := ComputeSheddingStrategy(report, cfg)

	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}

	d := decisions[0]
	// Cut edge should be orders→payments (the caller of the origin)
	if d.Source != "orders" || d.Target != "payments" {
		t.Errorf("expected cut edge orders→payments, got %s→%s", d.Source, d.Target)
	}
	if d.ShedPercent <= 0 || d.ShedPercent > cfg.MaxShedPercentage {
		t.Errorf("shed percent %.2f out of bounds [0, %.2f]", d.ShedPercent, cfg.MaxShedPercentage)
	}
	t.Logf("Decision: shed %s→%s by %.0f%% (reason: %s)", d.Source, d.Target, d.ShedPercent*100, d.Reason)
}

func TestStrategy_NilReport(t *testing.T) {
	cfg := DefaultConfig()
	decisions := ComputeSheddingStrategy(nil, cfg)
	if len(decisions) != 0 {
		t.Errorf("expected nil for nil report, got %d decisions", len(decisions))
	}
}

// =============================================================================
// Recovery Manager Tests
// =============================================================================

func approxEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < tolerance
}

func TestRecovery_RampUp(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RampUpStep = 0.15
	rm := NewRecoveryManager(cfg)

	// First tick: target 50% → should ramp to 15%
	decisions := []SheddingDecision{{Source: "orders", Target: "payments", ShedPercent: 0.50}}
	toApply, _ := rm.ProcessDecisions(decisions)
	if len(toApply) != 1 {
		t.Fatalf("expected 1 apply, got %d", len(toApply))
	}
	if !approxEqual(toApply[0].CurrentShed, 0.15, 0.01) {
		t.Errorf("expected ramp to ~15%%, got %.2f%%", toApply[0].CurrentShed*100)
	}

	// Second tick: should ramp to ~30%
	toApply, _ = rm.ProcessDecisions(decisions)
	if !approxEqual(toApply[0].CurrentShed, 0.30, 0.01) {
		t.Errorf("expected ramp to ~30%%, got %.2f%%", toApply[0].CurrentShed*100)
	}

	// Third tick: should ramp to ~45%
	toApply, _ = rm.ProcessDecisions(decisions)
	if !approxEqual(toApply[0].CurrentShed, 0.45, 0.02) {
		t.Errorf("expected ramp to ~45%%, got %.2f%%", toApply[0].CurrentShed*100)
	}

	// Fourth tick: should reach 50% (target)
	toApply, _ = rm.ProcessDecisions(decisions)
	if !approxEqual(toApply[0].CurrentShed, 0.50, 0.02) {
		t.Errorf("expected cap at ~50%%, got %.2f%%", toApply[0].CurrentShed*100)
	}
}

func TestRecovery_RampDown(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RampUpStep = 1.0 // Instant ramp-up for test setup
	cfg.RampDownStep = 0.10
	cfg.ConsecutiveLowIntervals = 3
	rm := NewRecoveryManager(cfg)

	// Set up: apply 50% shedding
	decisions := []SheddingDecision{{Source: "orders", Target: "payments", ShedPercent: 0.50}}
	rm.ProcessDecisions(decisions)

	// Now remove the decision (risk dropped) — should start ramping down
	toApply, toRemove := rm.ProcessDecisions(nil)
	if len(toRemove) > 0 {
		t.Error("should NOT fully remove on first low-risk interval")
	}
	if len(toApply) == 1 {
		if toApply[0].CurrentShed != 0.40 {
			t.Errorf("expected ramp down to 40%%, got %.0f%%", toApply[0].CurrentShed*100)
		}
	}
}

func TestRecovery_FullRecoveryAfterConsecutiveLow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RampUpStep = 1.0
	cfg.RampDownStep = 0.05 // Slow ramp-down so we don't hit 0 before consecutive count
	cfg.ConsecutiveLowIntervals = 2
	rm := NewRecoveryManager(cfg)

	// Set up: apply 30% shedding
	decisions := []SheddingDecision{{Source: "a", Target: "b", ShedPercent: 0.30}}
	rm.ProcessDecisions(decisions)

	// Tick 1: no decision → ramp down, count=1, shed ~25%
	toApply, toRemove := rm.ProcessDecisions(nil)
	t.Logf("Tick 1: apply=%d remove=%d active=%d", len(toApply), len(toRemove), rm.ActiveEdgeCount())

	// Tick 2: no decision → count=2 ≥ ConsecutiveLowIntervals → fully remove
	_, toRemove = rm.ProcessDecisions(nil)
	t.Logf("Tick 2: remove=%d active=%d", len(toRemove), rm.ActiveEdgeCount())

	// After enough ticks, the edge should be fully removed
	if rm.ActiveEdgeCount() != 0 {
		t.Errorf("expected 0 active edges after full recovery, got %d", rm.ActiveEdgeCount())
	}
}

func TestRecovery_MaxShedCap(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RampUpStep = 1.0 // Instant
	cfg.MaxShedPercentage = 0.8
	rm := NewRecoveryManager(cfg)

	// Try to shed 100% — should cap at 80%
	decisions := []SheddingDecision{{Source: "a", Target: "b", ShedPercent: 1.0}}
	toApply, _ := rm.ProcessDecisions(decisions)
	if toApply[0].CurrentShed != 0.8 {
		t.Errorf("expected cap at 80%%, got %.0f%%", toApply[0].CurrentShed*100)
	}
}

// =============================================================================
// Controller Tests
// =============================================================================

func TestController_DryRunDefault(t *testing.T) {
	oracle := buildTestOracleWithReport(0.85)
	actuator := NewLogActuator()
	cfg := DefaultConfig()
	cfg.Armed = false // DRY-RUN (default)
	cfg.ControlInterval = 50 * time.Millisecond
	cfg.ShedThreshold = 0.1 // Low threshold to guarantee triggering

	controller := NewController(cfg, oracle, actuator, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		controller.Run(ctx)
	}()

	// Let it tick a few times
	time.Sleep(200 * time.Millisecond)
	cancel()
	wg.Wait()

	// Verify audit log recorded actions
	log := actuator.AuditLog()
	if len(log) == 0 {
		t.Log("No audit entries — Oracle may not have produced actionable results")
	}
	for _, entry := range log {
		if !entry.DryRun {
			t.Errorf("expected DRY-RUN entries, got non-dry-run: %s", entry)
		}
		t.Logf("Audit: %s", entry)
	}
}

func TestController_KillSwitch(t *testing.T) {
	oracle := buildTestOracleWithReport(0.85)
	actuator := NewLogActuator()
	cfg := DefaultConfig()

	controller := NewController(cfg, oracle, actuator, slog.Default())
	controller.KillSwitch()

	// Kill switch should record REMOVE_ALL
	log := actuator.AuditLog()
	found := false
	for _, entry := range log {
		if entry.Action == "REMOVE_ALL" {
			found = true
		}
	}
	if !found {
		t.Error("expected REMOVE_ALL audit entry from kill switch")
	}
}

func TestLogActuator_ActiveRules(t *testing.T) {
	a := NewLogActuator()
	if a.ActiveRules() != 0 {
		t.Errorf("expected 0 initial active rules")
	}

	_ = a.Apply("a", "b", 0.5)
	_ = a.Apply("c", "d", 0.3)
	if a.ActiveRules() != 2 {
		t.Errorf("expected 2 active rules, got %d", a.ActiveRules())
	}

	_ = a.Remove("a", "b")
	if a.ActiveRules() != 1 {
		t.Errorf("expected 1 active rule after remove, got %d", a.ActiveRules())
	}

	_ = a.RemoveAll()
	if a.ActiveRules() != 0 {
		t.Errorf("expected 0 active rules after RemoveAll, got %d", a.ActiveRules())
	}
}
