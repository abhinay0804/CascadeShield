package simulator

import (
	"context"
	"log/slog"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
)

// Engine is the Monte Carlo cascade simulation orchestrator.
//
// It runs on a configurable interval (default 30s), takes an immutable
// DAGSnapshot, and for each node in the graph:
//   1. Runs N simulations (default 1000) with random perturbations
//   2. Propagates failures through the DAG using all 4 failure models
//   3. Aggregates results into risk scores and cascade paths
//
// The simulation is parallelized across nodes using a goroutine pool bounded
// by MaxConcurrentNodes (default: NumCPU).
//
// Go Pattern: semaphore channel for bounded concurrency, WaitGroup for join.
type Engine struct {
	cfg    Config
	dag    *graph.DAG
	logger *slog.Logger

	// latestReport holds the most recent simulation result (thread-safe read)
	latestReport *SimulationReport
	reportMu     sync.RWMutex
}

// NewEngine creates a new simulation engine.
func NewEngine(cfg Config, dag *graph.DAG, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		cfg:    cfg,
		dag:    dag,
		logger: logger,
	}
}

// LatestReport returns the most recent simulation result, or nil if no
// simulation has completed yet.
func (e *Engine) LatestReport() *SimulationReport {
	e.reportMu.RLock()
	defer e.reportMu.RUnlock()
	return e.latestReport
}

// Run starts the simulation loop. It blocks until ctx is cancelled.
// Simulations run on a ticker at SimulationInterval.
func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.SimulationInterval)
	defer ticker.Stop()

	e.logger.Info("Oracle simulation engine started",
		"interval", e.cfg.SimulationInterval,
		"num_simulations", e.cfg.NumSimulations,
		"max_concurrent", e.cfg.MaxConcurrentNodes,
	)

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("Oracle simulation engine shutting down")
			return
		case <-ticker.C:
			snap := e.dag.Snapshot()
			if len(snap.Nodes) == 0 {
				e.logger.Debug("Oracle: no nodes in DAG, skipping simulation")
				continue
			}
			report := e.RunSimulation(snap)

			e.reportMu.Lock()
			e.latestReport = report
			e.reportMu.Unlock()

			e.logger.Info("Oracle simulation complete", "summary", report.Summary())
		}
	}
}

// RunSimulation executes a full Monte Carlo simulation cycle on the given snapshot.
// This is the core computation — it can also be called directly for testing.
func (e *Engine) RunSimulation(snap *graph.DAGSnapshot) *SimulationReport {
	start := time.Now()

	report := &SimulationReport{
		SnapshotTime:   snap.Timestamp,
		NumSimulations: e.cfg.NumSimulations,
		NodeCount:      len(snap.Nodes),
		NodeResults:    make(map[string]*NodeSimResult, len(snap.Nodes)),
	}

	// Count edges
	for _, targets := range snap.Edges {
		report.EdgeCount += len(targets)
	}

	// Parallel simulation: one goroutine per origin node, bounded by semaphore
	sem := make(chan struct{}, e.cfg.MaxConcurrentNodes)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for nodeID := range snap.Nodes {
		sem <- struct{}{} // Acquire semaphore slot
		wg.Add(1)

		go func(originID string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore slot

			nodeResult := e.simulateNode(snap, originID)

			mu.Lock()
			report.NodeResults[originID] = nodeResult
			mu.Unlock()
		}(nodeID)
	}
	wg.Wait()

	report.Timestamp = time.Now()
	report.Duration = time.Since(start)

	e.logger.Debug("Oracle simulation timing",
		"duration", report.Duration,
		"nodes", report.NodeCount,
		"edges", report.EdgeCount,
	)

	return report
}

// simulateNode runs NumSimulations Monte Carlo iterations for a single origin node.
// Each iteration injects a random perturbation and propagates failures.
func (e *Engine) simulateNode(snap *graph.DAGSnapshot, originID string) *NodeSimResult {
	// Create a per-goroutine RNG to avoid contention on global rand.
	// Go 1.20+ rand sources are already per-goroutine, but being explicit.
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(hashString(originID))))

	runs := make([]CascadeRun, e.cfg.NumSimulations)

	for i := 0; i < e.cfg.NumSimulations; i++ {
		// Sample perturbation from normal distribution.
		// The perturbation simulates a sudden increase in latency or error rate.
		//
		// Latency perturbation: N(0, σ²) where σ = PerturbationStdDev × currentP99
		// We only take the positive tail (negative perturbation = improvement, not interesting).
		latencyPert := e.samplePerturbation(rng, originID, snap)
		errorPert := e.sampleErrorPerturbation(rng)

		runs[i] = PropagateCascade(snap, originID, latencyPert, errorPert, e.cfg)
	}

	riskScore := ComputeRiskScore(originID, runs, snap)
	return BuildNodeResult(originID, runs, e.cfg, riskScore)
}

// samplePerturbation samples a latency perturbation (in ms) for the origin node.
// Uses the current max outgoing edge latency as the base for the distribution.
func (e *Engine) samplePerturbation(rng *rand.Rand, originID string, snap *graph.DAGSnapshot) float64 {
	// Find the maximum P99 latency on any outgoing edge from this node
	maxLatencyMs := 0.0
	if targets, ok := snap.Edges[originID]; ok {
		for _, edge := range targets {
			if edge.Metrics.LatencyP99 > maxLatencyMs {
				maxLatencyMs = edge.Metrics.LatencyP99
			}
		}
	}

	// If no outgoing edges or no metrics yet, use a minimum perturbation
	if maxLatencyMs < e.cfg.MinPerturbationMs {
		maxLatencyMs = e.cfg.MinPerturbationMs
	}

	// Sample from abs(N(0, σ²)) — only positive perturbations
	sigma := e.cfg.PerturbationStdDev * maxLatencyMs
	perturbation := math.Abs(rng.NormFloat64() * sigma)

	// Ensure minimum perturbation
	if perturbation < e.cfg.MinPerturbationMs {
		perturbation = e.cfg.MinPerturbationMs
	}

	return perturbation
}

// sampleErrorPerturbation samples an error rate increase (0.0-0.5 range).
func (e *Engine) sampleErrorPerturbation(rng *rand.Rand) float64 {
	// Sample from abs(N(0, 0.1²)) — small error rate perturbations
	perturbation := math.Abs(rng.NormFloat64() * 0.1)
	if perturbation > 0.5 {
		perturbation = 0.5 // Cap at 50% additional error rate
	}
	return perturbation
}

// hashString returns a simple hash of a string for RNG seeding.
// This ensures different goroutines get different random sequences.
func hashString(s string) int64 {
	h := int64(0)
	for _, c := range s {
		h = h*31 + int64(c)
	}
	return h
}
