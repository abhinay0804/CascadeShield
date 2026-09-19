# Phase 3: Metrics Engine — The Bookkeeper
## A Beginner's Guide

---

## Summary of Previous Phase (Phase 2 Recap)

Phase 2 built the **Topology Engine**. It takes raw kernel events (PIDs, IP addresses, ports) and turns them into a **Service Dependency DAG** — a live graph showing which microservices are talking to which other microservices.

After Phase 2, the DAG looks like this:

```
orders ──────────► payments
  │
  └──────────────► inventory
```

Each arrow (edge) in the graph stores raw counters like:
- How many total TCP connections were made
- How many bytes were sent total
- How many retransmit events occurred
- How long (in ns) connections lasted

But these are **totals** — they don't tell you the current rate. "10,000 connections" doesn't tell you if orders is hitting payments 100 times/second or 10 times/second right now.

**Phase 3 fixes this.** It adds a "bookkeeper" that continuously computes:
- Requests per second (current rate)
- P50 / P95 / P99 latency (percentile latency — more on this below)
- Error rate (how often connections fail)
- Throughput (bytes per second)

These become the `EdgeMetrics` fields that Phase 4 (The Oracle) will use to predict cascading failures.

---

## What Was Done in Phase 3

We built **3 major pieces**:

1. **Sliding Window** — Tracks the *current rate* of events using time buckets
2. **HDR Histogram** — Tracks latency percentiles efficiently
3. **Annotator + Prometheus** — Orchestrates everything and exposes metrics via HTTP

---

## Why It Was Done

### The Problem: Totals Are Useless for Prediction

Imagine you're monitoring your heart rate. Your total lifetime heartbeat count is about 2.5 billion. That number is useless for predicting a heart attack. What matters is: what is your heart rate RIGHT NOW? And what is your average over the last 60 seconds?

The same applies to microservices:

```
Without Phase 3:
  edge.TotalConnections = 1,000,000    ← useless for risk prediction

With Phase 3:
  edge.Metrics.RequestRate = 847.3     ← connections/sec right now
  edge.Metrics.LatencyP99  = 312.7ms  ← the slowest 1% of connections
  edge.Metrics.ErrorRate   = 0.023    ← 2.3% connection failure rate
```

Phase 4 needs these current-rate metrics to run its simulation.

### Why P50/P95/P99 and Not Just "Average"?

**Averages lie.** Consider these two scenarios:

```
Scenario A: 99 connections at 10ms + 1 connection at 10,000ms (10 seconds!)
  Average = (99×10 + 10000) / 100 = 109.9ms

Scenario B: All 100 connections at 100ms
  Average = 100ms
```

Both scenarios have nearly the same average (about 100ms), but Scenario A has a connection that took **10 seconds** — a user experienced a 10-second hang. The average hides this.

Percentiles tell the truth:
- **P50 (median)**: The "typical" user experience — 50% of connections are faster than this
- **P95**: 95% of connections are faster than this — covers most users
- **P99**: 99% of connections are faster than this — the "worst-case-but-not-the-absolute-worst"

SREs (Site Reliability Engineers) care most about P99 because it's where users actually start experiencing problems.

---

## How It Was Done — Concept First

### Concept 1: The Sliding Window 🪟

**Analogy:** Imagine tracking how many cars pass under a bridge per minute. You could count all cars since the bridge was built (like Phase 2's `TotalConnections`). But what you really want is: "How many cars passed in the last 60 seconds?"

A **sliding window** does exactly this. It keeps a "moving frame" of time:

```
                            Time ─────────────────────▶

    60-second window
  ┌───────────────────────────────────────────────────┐
  │  B1  │  B2  │  B3  │  B4  │  B5  │  ...  │  B12 │
  │  5s  │  5s  │  5s  │  5s  │  5s  │       │  5s  │
  └───────────────────────────────────────────────────┘
    ↑ Oldest (dropped)               New bucket ↑

  Every 5 seconds:
    1. Old bucket B1 is DISCARDED (it's > 60 seconds old)
    2. New empty bucket B13 is CREATED
    3. Sum all 12 buckets → current stats
```

This is called **tumbling buckets**. The window "slides" by discarding old buckets. CascadeShield uses 12 buckets × 5 seconds each = a 60-second window by default.

**Key insight**: Instead of a background ticker resetting buckets, the window uses **lazy advancement**. Every time you call `RecordConnect()` or `Aggregate()`, it first calls `advance(time.Now())` to check if any buckets have expired and replaces them with fresh ones. This means there's no extra goroutine needed per-edge.

### Concept 2: The HDR Histogram 📊

**Analogy:** A histogram is like a row of bins where you throw balls based on their size. "1ms–2ms" bin, "2ms–4ms" bin, etc.

Regular histograms use **equal-width bins**:
```
[0–100ms] [100–200ms] [200–300ms] ...
```
Problem: If all your connections are 1–5ms, you're wasting 99% of your bins on ranges that never have any data.

**HDR (High Dynamic Range) Histograms** use **logarithmically-spaced bins**:
```
[0–1ms] [1–2ms] [2–4ms] [4–8ms] [8–16ms] ... [8s–16s]
```
Each bin is 2× the width of the previous one. This means you get:
- **High precision for small values** (0–1ms: 1000 sub-bins for 3 significant figures)
- **Low precision for large values** (8–16s: who cares about sub-second accuracy for 8-second connections)

The result: great percentile accuracy with very little memory.

**How percentiles work:**
```
Say you've recorded 1000 observations. P99 = the 990th value when sorted.

Without sorting (you can't sort millions of measurements in real-time):
1. Walk through bins from smallest to largest
2. Accumulate counts: bin1=50, bin2=200, bin3=400... running total
3. When running total hits 990 → you're in the P99 bin
4. Return that bin's upper bound

This is O(number_of_bins) = O(1000) regardless of how many measurements you've taken!
```

### Concept 3: The Annotator 🎭

The Annotator is a **goroutine** (a lightweight Go thread) that:
1. Reads from the same event channel as the DAG Builder (via a "tee")
2. For each event, updates the sliding window and histogram for that edge
3. Every 5 seconds, calls `ComputeMetrics()` and writes the results to the DAG

**What is a "tee"?** In Unix, `tee` is a command that splits one stream into two. In our main loop:

```go
// main.go — tee pattern
resolvedEvt := res.Resolve(evt)

// Send to DAG Builder (topology)
select {
case builderEvents <- resolvedEvt:
default: logger.Warn("dropping event") // non-blocking!
}

// ALSO send to Metrics Annotator
select {
case metricsEvents <- resolvedEvt:
default: logger.Warn("dropping event") // non-blocking!
}
```

Both goroutines get the same events, but process them independently. The DAG Builder tracks connections/edges, the Annotator tracks statistics.

The `select { case ch <- evt: default: ... }` pattern is critical:
- **Regular send** (`ch <- evt`): blocks forever if the channel is full
- **Non-blocking send** (`select { case ch <- evt: default: }`): gives up immediately if channel is full

This prevents a slow metrics goroutine from blocking the kernel event loop.

---

## Code Walkthrough

### `pkg/metrics/config.go` — Zero Hardcoding

```go
type Config struct {
    WindowDuration   time.Duration  // How long the sliding window spans (default: 60s)
    BucketCount      int            // How many tumbling buckets (default: 12)
    HistogramMinMs   int64          // Smallest trackable latency (default: 0ms)
    HistogramMaxMs   int64          // Largest trackable latency (default: 30000ms = 30s)
    HistogramSigFigs int            // Precision (default: 3 → 0.1% error)
    AnnotateInterval time.Duration  // How often to flush metrics to DAG (default: 5s)
    MetricsAddr      string         // Prometheus HTTP address (default: ":9090")
    MaxEdgesTracked  int            // Memory safety cap (default: 10000)
}
```

Every value is configurable. No magic numbers. This follows the **Zero Hardcoding Policy** that applies to every phase of CascadeShield.

### `pkg/metrics/window.go` — The Sliding Window

```go
// Bucket = one 5-second time slot
type Bucket struct {
    StartTime       time.Time
    ConnectionCount int64
    TotalDurationNs int64   // Sum of all connection lifetimes in this slot
    TotalBytesSent  int64
    TotalBytesRecv  int64
    RetransmitCount int64
    CloseCount      int64
}

// Window = ring of 12 Buckets
type Window struct {
    mu             sync.Mutex
    buckets        []Bucket       // ring buffer of time slots
    bucketCount    int
    slideInterval  time.Duration  // 5s (= 60s / 12 buckets)
    windowDuration time.Duration  // 60s
    currentIdx     int            // which bucket we're writing to now
}
```

**The `advance()` method** (called before every read or write):
```go
func (w *Window) advance(now time.Time) {
    // Is the current bucket's time slot expired?
    nextSliceEnd := w.buckets[w.currentIdx].StartTime.Add(w.slideInterval)

    for now.After(nextSliceEnd) {
        // Move to next slot (ring: wrap around when we hit the end)
        w.currentIdx = (w.currentIdx + 1) % w.bucketCount
        // Clear the slot we just overwrote (it was the oldest data)
        w.buckets[w.currentIdx] = Bucket{StartTime: nextSliceEnd}
        nextSliceEnd = nextSliceEnd.Add(w.slideInterval)
    }
}
```

**Why ring buffer (not a list that you append/delete from)?**
- Ring buffers are O(1) for writes — you just move the index pointer
- No memory allocation after initialization — arrays are pre-allocated
- Cache-friendly — all buckets are contiguous in memory

### `pkg/metrics/histogram.go` — Percentile Magic

```go
// NewHistogram builds log-spaced bucket boundaries
func NewHistogram(minMs, maxMs int64, sigFigs int) *Histogram {
    subBuckets := int64(math.Pow10(sigFigs))  // sigFigs=3 → 1000 sub-buckets per power-of-2 range

    var boundaries []int64
    value := minNs
    for value <= maxNs {
        boundaries = append(boundaries, value)
        // Each step is proportional to the current value (logarithmic spacing)
        step := max64(1, value/subBuckets)
        value += step
    }
    // ...
}

// RecordNs: binary search to find the right bucket, increment count
func (h *Histogram) RecordNs(ns int64) {
    idx := h.bucketIndex(ns)  // binary search: O(log n_buckets)
    h.counts[idx]++
    h.totalCount++
}

// Percentile: walk bins accumulating count until we reach the target rank
func (h *Histogram) Percentile(p float64) float64 {
    target := int64(math.Ceil(float64(h.totalCount) * p / 100.0))
    cumulative := int64(0)
    for i, count := range h.counts {
        cumulative += count
        if cumulative >= target {
            return float64(h.bucketBoundaries[i]) / 1_000_000.0  // ns → ms
        }
    }
    return ...
}
```

### `pkg/metrics/annotator.go` — The Orchestrator

```go
// Annotator holds the map of per-edge state
type Annotator struct {
    cfg        Config
    dag        *graph.DAG
    mu         sync.RWMutex
    edgeStates map[EdgeKey]*EdgeState  // one entry per (source, target) pair
}

// Run is the main goroutine
func (a *Annotator) Run(ctx context.Context, events <-chan resolver.ResolvedEvent) {
    annotateTicker := time.NewTicker(a.cfg.AnnotateInterval)  // fires every 5s

    for {
        select {
        case evt := <-events:
            a.processEvent(evt)    // update sliding window + histogram

        case <-annotateTicker.C:
            a.flush()              // compute metrics + write to DAG

        case <-ctx.Done():
            return                 // graceful shutdown
        }
    }
}
```

**Go Pattern — select statement:** In Go, `select` is like a `switch` for channels. It blocks until one of the listed channels has data (or a timer fires). This is how goroutines wait for multiple events simultaneously without spinning (wasting CPU).

**Go Pattern — sync.RWMutex:** The `edgeStates` map is accessed by:
- The event goroutine (writes when new edges appear)
- The flush goroutine (reads all states)

`sync.RWMutex` allows many concurrent readers OR one writer — never both. This is faster than a plain `Mutex` when reads dominate (which they do once all edges are discovered).

### `pkg/metrics/prometheus.go` — The HTTP Exporter

```go
type PrometheusServer struct {
    // Gauge vectors: one value per (source_service, target_service) label pair
    reqRateGauge   *prometheus.GaugeVec  // cascadeshield_edge_request_rate
    p50Gauge       *prometheus.GaugeVec  // cascadeshield_edge_latency_p50_ms
    // ... etc.
}

// On each GET /metrics request from Prometheus:
func handler(w, r) {
    ps.updateGauges()    // read all edges from DAG → set gauge values
    stdHandler(w, r)     // Prometheus library renders the text format
}
```

**What does the /metrics output look like?**

```
# HELP cascadeshield_edge_request_rate TCP connections per second
# TYPE cascadeshield_edge_request_rate gauge
cascadeshield_edge_request_rate{source_service="orders",target_service="payments"} 847.3
cascadeshield_edge_request_rate{source_service="orders",target_service="inventory"} 231.7

# HELP cascadeshield_edge_latency_p99_ms 99th percentile latency in ms
cascadeshield_edge_latency_p99_ms{source_service="orders",target_service="payments"} 312.7
```

Prometheus reads this every 15–30 seconds and stores it in its time-series database. Grafana can then plot these values over time.

---

## How It All Connects

```
BEFORE Phase 3:
  Edge has raw counters: TotalConnections=50,000, TotalDurationNs=2,500,000,000,000
  These are growing accumulators — useless for predicting failures

AFTER Phase 3:
  Edge.Metrics.RequestRate = 847.3 req/s    ← feeds Phase 4 thread pool model
  Edge.Metrics.LatencyP99  = 312.7 ms       ← feeds Phase 4 timeout chain model
  Edge.Metrics.ErrorRate   = 0.023          ← feeds Phase 4 retry amplification model
  Edge.Metrics.BytesPerSec = 1,234,567 B/s  ← feeds Phase 4 connection pool model

  Also visible externally:
  GET http://localhost:9090/metrics → Prometheus can scrape and store all of this
```

**Phase 4 (The Oracle)** will:
1. Call `dag.GetSnapshot()` to get an immutable copy of the full graph (with all EdgeMetrics)
2. Run 1000 Monte Carlo simulations: "what if payments latency suddenly increases by 200ms?"
3. Apply the thread pool model: `active_threads = requestRate × (latencyP99 + perturbation_ms) / 1000`
4. If `active_threads > pool_size` → thread pool exhausted → cascade predicted

Without Phase 3's real-time metrics, Phase 4 would have nothing meaningful to simulate with.

---

## Benchmark Results

| Operation | Speed | Allocs/op | vs 100K events/s target |
|---|---|---|---|
| `Window.RecordConnect()` | ~9.0M ops/sec (111.2 ns/op) | 0 B/op | **90× headroom** |
| `Window.Aggregate()` | ~16.9M ops/sec (58.98 ns/op) | 0 B/op | **169× headroom** |
| `Histogram.RecordNs()` | ~12.9M ops/sec (77.40 ns/op) | 0 B/op | **129× headroom** |

**Zero allocations per operation (`0 B/op, 0 allocs/op`)** means the garbage collector is never triggered by the hot path. This is essential for low-latency eBPF event processing — GC pauses would cause event backpressure and ring buffer drops.

---

## Key Concepts Glossary

| Concept | Simple Explanation |
|---|---|
| **Sliding window** | A "moving snapshot" of recent activity — shows what's happening NOW, not since the beginning of time |
| **Tumbling bucket** | A fixed-size time slot in the sliding window; old ones are discarded, new ones opened as time advances |
| **HDR histogram** | A memory-efficient structure for tracking percentiles; uses log-spaced bins so small values get high precision |
| **Percentile** | "P99 = 50ms" means 99% of connections were faster than 50ms; the P99 value is the cut-off above which only 1% of data falls |
| **Tee pattern** | Sending the same event to two goroutines simultaneously (like a Y-junction in a pipe) |
| **Non-blocking send** | `select { case ch <- v: default: }` — if the channel is full, skip and continue; don't wait |
| **Gauge** | A Prometheus metric type that can go up or down (vs Counter which only goes up) — request rate and latency are Gauges |
| **GaugeVec** | A set of Gauges distinguished by labels — `cascadeshield_edge_request_rate{source="A", target="B"}` |
| **Custom registry** | An isolated Prometheus registry (not the global default) — prevents test conflicts and controls which metrics are exported |
