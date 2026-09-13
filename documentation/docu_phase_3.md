# Phase 3 Technical Completion Document: Metrics Engine

**Phase:** Phase 3 (Metrics Engine — Sliding Windows, HDR Histograms & Prometheus)
**Status:** Completed
**Implementing Agent:** Claude Sonnet 4.6 (Thinking)
**Date:** September 10, 2026

---

## 1. Executive Summary

Phase 3 implements the **Metrics Engine** for CascadeShield. It transforms raw cumulative counters on DAG edges (built by Phase 2) into real-time statistical metrics: request rate, latency percentiles (P50/P95/P99), error rate, and throughput.

The engine runs as two concurrent components:
1. **Annotator** — a goroutine that consumes the same `ResolvedEvent` stream as the DAG Builder (via a tee'd channel in `main.go`) and maintains per-edge sliding windows + histograms.
2. **PrometheusServer** — an HTTP server on `:9090` that exposes all edge and DAG metrics in Prometheus text format on every scrape.

These `EdgeMetrics` values become the primary simulation inputs for Phase 4 (The Oracle).

---

## 2. Files Created & Modified

### `pkg/metrics/` (all new)

| File | Purpose |
|---|---|
| `config.go` | `Config` struct + `DefaultConfig()` — all tunables (window duration, bucket count, histogram bounds, metrics addr, max tracked edges) |
| `window.go` | Tumbling-bucket sliding window — ring of `Bucket` structs; `RecordConnect`, `RecordClose`, `RecordRetransmit`, `Aggregate()` |
| `histogram.go` | Log-linear HDR-style histogram — power-of-two bucketing with configurable sub-bucket precision; `RecordNs(ns)`, `Percentile(p)`, `Reset()` |
| `edge_state.go` | Per-edge composite state (Window + Histogram); `ComputeMetrics()` returns all 6 derived values |
| `annotator.go` | Main Phase 3 orchestrator — event consumer goroutine + periodic flush to DAG via `UpdateEdgeMetrics()` |
| `prometheus.go` | Prometheus HTTP exposition server — custom registry, GaugeVec per edge-level metric, `updateGauges()` on each scrape |
| `window_test.go` | Unit tests: record/aggregate, slide eviction, concurrent race, benchmarks |
| `histogram_test.go` | Unit tests: percentile accuracy (<5ms error for 100-value uniform dist), clamp, reset, single-value edge case |
| `annotator_test.go` | Integration tests: event pipeline → EdgeMetrics flushed into DAG, unresolved events ignored, MaxEdgesTracked limit enforced |

### `pkg/graph/dag.go` (modified)

Added `UpdateEdgeMetrics(source, target string, m EdgeMetrics)` — a write-locked method allowing the Annotator to atomically push computed metrics into live DAG edges without the Annotator needing direct pointer access to `*Edge`.

### `cmd/cascadeshield/main.go` (modified)

- Added Phase 3 CLI flags: `--metrics-addr`, `--window-duration`, `--window-buckets`
- Changed single `resolvedEvents` channel to two channels (`builderEvents`, `metricsEvents`) — events are **tee'd** in the main loop with non-blocking sends
- Initializes `Annotator` and `PrometheusServer`, starts both before the main loop

---

## 3. Architecture & Design Decisions

### 3.1 Tumbling-Bucket Sliding Window (Not Exponential Decay)

The architecture plan calls for a sliding window with configurable buckets. We implement **tumbling buckets** rather than exponential decay or a circular sample buffer because:
- Tumbling buckets have O(1) insert and O(BucketCount) aggregate — predictable cost
- They match what Prometheus itself uses internally
- Bucket boundaries make it easy to explain what "60s window" means

**Implementation detail:** The `Window` maintains a ring of `BucketCount` `Bucket` structs. The `advance()` method (called on every write and aggregate) checks if the current bucket's time slice has expired and if so, clears old buckets by overwriting them with empty ones. This means buckets are evicted lazily (no background ticker needed).

### 3.2 HDR-Style Log-Linear Histogram (No External Library)

The task tracker suggested `github.com/HdrHistogram/hdrhistogram-go`. We implemented an equivalent **log-linear histogram** internally because:
- Zero new external dependencies for this package
- The full HDR library has features (recordValueWithCount, iterators, encoding) we don't need
- A pure binary-search over power-of-two sub-bucket boundaries achieves the same accuracy profile

**Accuracy:** With `HistogramSigFigs=3`, relative error is ≤0.1% across the full dynamic range. Unit tests confirm P50/P95/P99 are within 5ms of actual for a uniform 1–100ms distribution.

### 3.3 Tee Pattern (Fan-out to Multiple Consumers)

Instead of a single channel shared between the DAG Builder and Annotator:
- `main.go` sends each event to **both** `builderEvents` and `metricsEvents` channels
- Non-blocking sends (`select { case ch <- evt: default: ... }`) prevent either consumer from blocking the main event loop
- This is the standard Go fan-out pattern — each consumer processes events independently and at its own pace

### 3.4 Prometheus Custom Registry

`PrometheusServer` uses `prometheus.NewRegistry()` (not `prometheus.DefaultRegisterer`) for:
- Test isolation — multiple test instances can run without "metric already registered" panics
- Explicit control over which metrics are exposed — no automatic Go runtime metrics (which would confuse CascadeShield dashboards with unrelated data)

---

## 4. Data Flow

```
eBPF Probes (kernel)
      │
      ▼
probe.Reader (ring buffer consumer)
      │
      ▼ probe.Event channel
main.go (main loop)
      │
      ├─────────────────────────────────────┐
      │  res.Resolve(evt)                   │
      │       ▼ resolver.ResolvedEvent      │
      ├──► builderEvents channel            │
      │         ▼                           │
      │    graph.Builder.Run()              │
      │         ▼                           │
      │    DAG.RecordConnect/Close/Retransmit│ (Phase 2: topology structure)
      │                                     │
      └──► metricsEvents channel            │
               ▼                           │
          metrics.Annotator.Run()           │
               ├── Window.RecordConnect/Close/Retransmit
               └── Histogram.RecordNs(duration_ns)
                         ▼
              every AnnotateInterval (5s)
                         ▼
              EdgeState.ComputeMetrics()
                         ▼
              DAG.UpdateEdgeMetrics(source, target, m) ◄─ (write lock)
                         │
                         ▼
              Edge.Metrics.RequestRate, LatencyP50..P99,
              ErrorRate, BytesPerSec (Phase 4 reads these)

Prometheus scrape (every 15-30s):
GET /metrics → PrometheusServer.updateGauges() → reads DAG.GetAllEdges()
             → writes GaugeVec per (source_service, target_service)
```

---

## 5. Key Interfaces

```go
// pkg/metrics/config.go
func DefaultConfig() Config
func (c Config) SlideInterval() time.Duration

// pkg/metrics/annotator.go
func NewAnnotator(cfg Config, dag *graph.DAG, logger *slog.Logger) *Annotator
func (a *Annotator) Run(ctx context.Context, events <-chan resolver.ResolvedEvent)
func (a *Annotator) TrackedEdgeCount() int

// pkg/metrics/prometheus.go
func NewPrometheusServer(cfg Config, dag *graph.DAG, annotator *Annotator, logger *slog.Logger) *PrometheusServer
func (ps *PrometheusServer) Start(ctx context.Context) error

// pkg/graph/dag.go (new method)
func (d *DAG) UpdateEdgeMetrics(source, target string, m EdgeMetrics)
```

---

## 6. Prometheus Metrics Exported

| Metric Name | Type | Labels | Description |
|---|---|---|---|
| `cascadeshield_edge_request_rate` | Gauge | source_service, target_service | Connections/sec (sliding window) |
| `cascadeshield_edge_latency_p50_ms` | Gauge | source_service, target_service | Median latency in ms |
| `cascadeshield_edge_latency_p95_ms` | Gauge | source_service, target_service | 95th pct latency in ms |
| `cascadeshield_edge_latency_p99_ms` | Gauge | source_service, target_service | 99th pct latency in ms |
| `cascadeshield_edge_error_rate` | Gauge | source_service, target_service | Fraction 0.0–1.0 |
| `cascadeshield_edge_bytes_per_sec` | Gauge | source_service, target_service | Combined throughput in bytes/s |
| `cascadeshield_dag_nodes_total` | Gauge | (none) | Live node count |
| `cascadeshield_dag_edges_total` | Gauge | (none) | Live edge count |

---

## 7. Configuration (All Tunables)

| Flag | Env Default | Description |
|---|---|---|
| `--metrics-addr` | `:9090` | Prometheus HTTP listen address |
| `--window-duration` | `60s` | Sliding window total span |
| `--window-buckets` | `12` | Number of tumbling buckets |
| `Config.HistogramMinMs` | `0` | Histogram min value (ms) |
| `Config.HistogramMaxMs` | `30000` | Histogram max value (ms, = 30s) |
| `Config.HistogramSigFigs` | `3` | Histogram precision significant figures |
| `Config.MaxEdgesTracked` | `10000` | Memory protection: max tracked edges |

---

## 8. Testing Results

```bash
go test -v -race -timeout 60s ./pkg/metrics/...
```

**Result:** ALL PASS — 11 tests, 0 failures, 0 data races

| Test | Result | Notes |
|---|---|---|
| `TestWindow_RecordAndAggregate` | ✅ PASS | Basic counters verify correctly |
| `TestWindow_SlideEviction` | ✅ PASS | 2-bucket 100ms window, old data evicted after expiry |
| `TestWindow_ConcurrentRace` | ✅ PASS | 20 goroutines × 100 ops = 0 races |
| `TestHistogram_Percentiles` | ✅ PASS | P50=50.02ms, P95=95.02ms, P99=99.09ms (all within 5ms of actual) |
| `TestHistogram_EmptyReturnsZero` | ✅ PASS | |
| `TestHistogram_Clamp` | ✅ PASS | |
| `TestHistogram_Reset` | ✅ PASS | |
| `TestHistogram_SingleValue` | ✅ PASS | |
| `TestAnnotator_RecordsEvents` | ✅ PASS | req/s=2.00, p50=50ms, err=0.20, bps=1536 |
| `TestAnnotator_IgnoresUnresolved` | ✅ PASS | |
| `TestAnnotator_MaxEdgesTracked` | ✅ PASS | |

```bash
go test -v -race ./pkg/graph/... ./pkg/resolver/...
```

**Result:** ALL PASS — Phase 2 tests unaffected by new `UpdateEdgeMetrics` method

---

## 9. Gotchas & Lessons

1. **Prometheus imports**: `go get github.com/prometheus/client_golang@latest` registers the module but you also need `go get github.com/prometheus/client_golang/prometheus github.com/prometheus/client_golang/prometheus/promhttp` to pull the sub-package transitive deps. Run `go mod tidy` after.

2. **Histogram binary search**: The `bucketIndex()` method must use `<` not `<=` to avoid off-by-one on bucket boundaries. Values at exactly a boundary should go into the upper bucket for correct percentile computation.

3. **Lazy bucket advance**: Calling `advance(time.Now())` at the start of every `Record*` and `Aggregate()` call (instead of a background ticker) means the window auto-advances without any goroutine management. This simplifies the code and avoids the complexity of a ticker goroutine per edge.

4. **Non-blocking tee in main.go**: The `select { case ch <- evt: default: logger.Warn(...) }` pattern is critical. If either the builder or annotator goroutine is temporarily slow, the main event loop must NOT block — eBPF ring buffer events are time-sensitive.

---

## 10. Notes for Phase 4 (The Oracle)

Phase 4 reads `edge.Metrics` from the live DAG to use as simulation inputs:

- `LatencyP99` → current 99th percentile latency for that service dependency
- `RequestRate` → current traffic rate (connection/s)
- `ErrorRate` → current fraction of failed connections

Phase 4 should call `dag.GetSnapshot()` (from Phase 2) to get an immutable copy of the full graph state (including all EdgeMetrics) before running each Monte Carlo simulation batch. This avoids lock contention between the live annotator and the simulation goroutines.
