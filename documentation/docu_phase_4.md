# Phase 4 Technical Completion Document: The Oracle — Monte Carlo Cascade Simulator

**Phase:** Phase 4 (The Oracle — Monte Carlo Cascade Simulator)
**Status:** Completed
**Implementing Agent:** Claude Opus 4.6 (Thinking)
**Date:** September 12, 2026

---

## 1. Executive Summary

Phase 4 implements **The Oracle** — a Monte Carlo cascade failure simulator for CascadeShield. Given a snapshot of the live Service Dependency DAG (with real-time EdgeMetrics from Phase 3), The Oracle runs thousands of stochastic simulations to predict:

- **Which services** are most likely to trigger cascading failures
- **How fast** those cascades would propagate (Time-To-Failure)
- **What paths** the cascades would follow through the dependency graph
- **Risk scores** (0.0–1.0) classifying each node as LOW/MEDIUM/HIGH/CRITICAL

The Oracle runs every 30 seconds (configurable) and produces a `SimulationReport` that feeds Phase 4b (The Shield) for autonomous remediation and Phase 5 for dashboard display.

---

## 2. Files Created & Modified

### `pkg/simulator/` (all new)

| File | Purpose |
|---|---|
| `config.go` | Config struct + DefaultConfig() — all tunables (num simulations, intervals, pool sizes, risk thresholds) |
| `models/types.go` | Shared types: `NodeState` enum, `ModelResult`, `EdgeParams` |
| `models/threadpool.go` | Model 1: Thread pool exhaustion (active_threads = rate × latency) |
| `models/connpool.go` | Model 2: Connection pool saturation (smaller pools, queuing delays) |
| `models/timeout.go` | Model 3: Timeout chain propagation (multi-hop timeout cascades) |
| `models/retry.go` | Model 4: Retry amplification (iterative fixed-point convergence with feedback loop detection) |
| `cascade.go` | BFS upstream cascade propagation applying all 4 models at each hop |
| `risk.go` | Risk score computation: P(cascade) × Impact × Urgency, normalized to [0, 1] |
| `report.go` | Structured simulation output: `SimulationReport`, `NodeSimResult`, `CascadePath` |
| `engine.go` | Monte Carlo orchestrator: goroutine pool + normal distribution perturbations + ticker |
| `models/models_test.go` | 17 unit tests for all 4 failure models |
| `engine_test.go` | 8 tests for cascade BFS, risk scores, engine integration, benchmarks |

### `cmd/cascadeshield/main.go` (modified)

- Added `--sim-interval` and `--num-simulations` CLI flags
- Initializes `simulator.Engine` and starts it in a goroutine
- Logs Oracle's latest risk assessment on graceful shutdown

---

## 3. The Four Failure Models

### Model 1: Thread Pool Exhaustion

```
active_threads = request_rate × (current_latency_ms + injected_latency_ms) / 1000
if active_threads > pool_size → EXHAUSTED
TTF = pool_size / request_rate
```

Default pool size: **200** (matches Tomcat default — most common Java microservice container).

### Model 2: Connection Pool Saturation

Same mechanics as thread pool, but models smaller pools (default: **50**) with queuing delays. Uses M/M/c queuing approximation for wait time under saturation.

### Model 3: Timeout Chain Propagation

Given a chain A → B → C → D with per-edge timeouts: if D's latency exceeds C→D timeout, C returns error after `timeout` duration (not actual latency). With retries, total wait = `(timeout + backoff) × (1 + retries)`.

### Model 4: Retry Amplification

```
for iteration in 0..MaxIterations:
    amplification = 1 + (error_rate × max_retries)
    new_rate = base_rate × amplification
    new_error_rate = f(new_rate, capacity)  // logistic increase under load
    if converged: break
```

Detects positive feedback loops. Capped at 10 iterations to prevent divergence.

---

## 4. Monte Carlo Simulation Flow

```
Every SimulationInterval (30s):
  snap = dag.Snapshot()           ← immutable deep copy (from Phase 2)

  for each node N in snap (in parallel, bounded by semaphore):
    for i in 1..NumSimulations (1000):
      1. Sample perturbation:
         latencyDelta = |N(0, σ²)|  where σ = 0.3 × max_outgoing_P99
         errorDelta   = |N(0, 0.1²)|
      2. PropagateCascade(snap, N, latencyDelta, errorDelta):
         BFS upstream through callers, applying all 4 models at each hop
      3. Record: cascade path, node states, TTF
    Aggregate:
      RiskScore = Σ P(cascade) × Impact × Urgency
      TopPaths  = top 3 cascade paths by frequency

  Output: SimulationReport with per-node NodeSimResult
```

---

## 5. Risk Score Formula

```
Risk(node) = Σ over affected nodes D:
    P(cascade to D) × Impact(D) × Urgency(D)

P(cascade)  = (runs where D exhausted) / total_runs
Impact(D)   = InDegree(D) / maxInDegree  (normalized fan-in)
Urgency(D)  = min(10, 1 / mean_TTF(D).Seconds())

Normalized by: raw_score / (num_nodes × 10)
Clamped to [0.0, 1.0]
```

Classification thresholds:
| Range | Level |
|---|---|
| < 0.3 | LOW |
| 0.3–0.6 | MEDIUM |
| 0.6–0.85 | HIGH |
| ≥ 0.85 | CRITICAL |

---

## 6. Pool Size Design Decision

| Pool Type | Default | Rationale |
|---|---|---|
| Thread Pool | 200 | Matches Apache Tomcat default (most common Java microservice container). Reasonable for Go services with practical downstream concurrency limits. |
| Connection Pool | 50 | Database connection pools (HikariCP default=10, PgBouncer default=100) are fundamentally smaller. 50 is a generous middle-ground. |

When K8s `ResourceLimits` are populated, pool sizes are derived from CPU limits:
- `ThreadPoolSize = CPULimitMillis / 1000 × 50` (1 core ≈ 50 concurrent reqs)
- `ConnPoolSize = max(10, ThreadPoolSize / 4)`

---

## 7. Testing Results

```bash
go test -v -race -timeout 120s ./pkg/simulator/...
```

**Result:** ALL PASS — 25 tests, 0 failures, 0 data races

### Model Tests (17)

| Test | Result | Notes |
|---|---|---|
| `TestThreadPool_Healthy` | ✅ | 5 active threads / 200 pool = healthy |
| `TestThreadPool_Degraded` | ✅ | 180/200 = 90% utilization |
| `TestThreadPool_Exhausted` | ✅ | 250/200 = EXHAUSTED, TTF=200ms ±10ms |
| `TestThreadPool_ZeroRate` | ✅ | Edge case: zero rate = healthy |
| `TestConnPool_Healthy` | ✅ | 1 active / 50 pool |
| `TestConnPool_Saturated` | ✅ | 100/50 = EXHAUSTED, positive queuing delay |
| `TestConnPool_ZeroPool` | ✅ | Edge case: zero pool = healthy |
| `TestTimeout_NoTimeout` | ✅ | No timeout configured |
| `TestTimeout_WithinTimeout` | ✅ | 2s < 5s timeout |
| `TestTimeout_ExceedsTimeout` | ✅ | 8s > 3s timeout, effective latency = 3s |
| `TestTimeout_WithRetries` | ✅ | Total wait = 9.2s with 2 retries |
| `TestTimeout_EvaluateTimeout` | ✅ | Convenience wrapper → DEGRADED |
| `TestRetry_NoRetries` | ✅ | 0 retries → 1.0× amplification |
| `TestRetry_BasicAmplification` | ✅ | 50% errors × 3 retries → 2.5× |
| `TestRetry_FeedbackLoop` | ✅ | With capacity limit → 4.0× (EXHAUSTED) |
| `TestRetry_Convergence` | ✅ | Extreme inputs → 11.0× (converges within 10 iterations) |
| `TestRetry_ZeroErrorRate` | ✅ | 0% errors → 1.0× (HEALTHY) |

### Engine Tests (8)

| Test | Result | Notes |
|---|---|---|
| `TestCascade_KnownGraph` | ✅ | payments→orders→gateway cascade correctly predicted |
| `TestCascade_NoEdges` | ✅ | Isolated node, no cascade |
| `TestRiskScore_Normalization` | ✅ | Score always in [0, 1] |
| `TestRiskScore_ZeroRunsReturnsZero` | ✅ | |
| `TestRiskScore_Classification` | ✅ | All 8 threshold boundaries verified |
| `TestEngine_RunSimulation` | ✅ | 3-node DAG: payments=MEDIUM, orders=LOW, gateway=LOW |
| `TestEngine_EmptyDAG` | ✅ | 0 results for empty graph |
| `TestEngine_LatestReport` | ✅ | Thread-safe report access |

### Benchmark

```
10-node graph, 1000 simulations: 5.6ms
Target: < 500ms
Result: 89× faster than target 🚀
```

---

## 8. Configuration (All Tunables)

| Flag / Config | Default | Description |
|---|---|---|
| `--sim-interval` | 30s | Simulation cycle interval |
| `--num-simulations` | 1000 | Monte Carlo runs per origin node |
| `MaxConcurrentNodes` | NumCPU | Goroutine pool bound |
| `PerturbationStdDev` | 0.3 | Normal distribution σ (fraction of current value) |
| `MinPerturbationMs` | 10ms | Floor for latency perturbation |
| `DefaultThreadPoolSize` | 200 | Thread pool when K8s limits unavailable |
| `DefaultConnPoolSize` | 50 | Connection pool when K8s limits unavailable |
| `DefaultTimeout` | 5s | Per-edge request timeout |
| `DefaultMaxRetries` | 3 | Retry attempts per edge |
| `DefaultRetryBackoff` | 100ms | Base delay between retries |
| `RiskThresholdLow` | 0.3 | LOW/MEDIUM boundary |
| `RiskThresholdMedium` | 0.6 | MEDIUM/HIGH boundary |
| `RiskThresholdHigh` | 0.85 | HIGH/CRITICAL boundary |
| `MaxRetryIterations` | 10 | Fixed-point iteration cap for retry model |
| `TopCascadePathsCount` | 3 | Cascade paths kept per node in report |

---

## 9. Notes for Phase 4b (The Shield)

Phase 4b reads `SimulationReport` via `oracle.LatestReport()`:

```go
report := oracle.LatestReport()
for nodeID, result := range report.NodeResults {
    if result.RiskLevel == simulator.RiskCritical {
        // Find the cut edge on the top cascade path
        // Apply load-shedding
    }
}
```

The `TopCascadePaths[0].Path` identifies the most likely cascade chain. The "cut edge" is the edge whose shedding maximally reduces cascade probability.
