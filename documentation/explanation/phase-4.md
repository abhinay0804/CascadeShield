# Phase 4: The Oracle — A Beginner's Guide
## Monte Carlo Cascade Failure Simulation

---

## Summary of Previous Phases

| Phase | What It Does |
|---|---|
| Phase 1 (Kernel Eye) | Captures every TCP connection event from the Linux kernel using eBPF probes |
| Phase 2 (Topology Engine) | Maps PIDs → Containers → Pods → Services, builds a live dependency graph (DAG) |
| Phase 3 (Metrics Engine) | Computes real-time statistics on each edge: requests/sec, P99 latency, error rate |

After Phase 3, each edge in the DAG has live metrics:
```
orders ──── RequestRate: 500 req/s ────► payments
             LatencyP99: 150ms
             ErrorRate:  0.02
```

**Phase 4 asks:** "What happens if payments suddenly gets slow? Does orders crash? Does gateway crash? How fast?"

---

## What Was Done

We built **The Oracle** — a Monte Carlo cascade failure simulator.

In plain English: **we run thousands of "what-if" scenarios** on the live service graph. For every service, we ask: "If THIS service gets slow, what breaks?"

---

## Why It Was Done

### The Problem: You Can't Wait for Cascading Failures to Happen

Cascading failures are the #1 cause of large-scale outages in microservice architectures. The pattern:

```
1. One service gets slow (database query takes 10x longer)
2. Its callers' thread pools fill up (waiting for slow responses)
3. The callers can't serve ANY requests (even ones unrelated to the slow service)
4. THEIR callers get affected too
5. Within seconds, the entire cluster is down
```

By the time you detect step 1, steps 2-5 have already happened. **You need to predict the cascade BEFORE it starts.**

### The Solution: Monte Carlo Simulation

Monte Carlo simulation is a technique where you:
1. Take the current real state of the system
2. Inject random perturbations ("what if payments gets 200ms slower?")
3. Simulate what would happen mathematically
4. Repeat 1000 times to get statistical confidence
5. Report: "If payments degrades, there's a 92% chance orders crashes in 1.2 seconds"

**Analogy:** It's like a weather forecast. Meteorologists don't wait for hurricanes — they run thousands of simulations with slightly different starting conditions and report the probability of bad weather.

---

## The Four Failure Models

The Oracle uses 4 models that represent real-world failure propagation mechanisms. Each model answers a different "how does failure spread?" question.

### Model 1: Thread Pool Exhaustion 🧵

**Analogy:** A restaurant has 20 waiters. Normally each table takes 10 minutes. But the kitchen is slow today — each table takes 30 minutes. Now waiters are stuck waiting at the kitchen window. After a while, all 20 waiters are waiting, and NO new customers can be served.

```
Formula:
    active_threads = request_rate × latency_in_seconds
    
Example:
    orders → payments at 1000 req/s, latency = 50ms
    active_threads = 1000 × 0.050 = 50   ← well within 200 pool

    Now payments gets slow: latency = 250ms
    active_threads = 1000 × 0.250 = 250  ← EXCEEDS 200 pool! EXHAUSTED!
    
    Time to exhaustion = pool_size / request_rate = 200 / 1000 = 0.2 seconds
```

### Model 2: Connection Pool Saturation 🔌

Same idea as thread pools, but for **database connections**. Connection pools are typically much smaller (50 vs 200), so they saturate faster.

```
Default connection pool: 50 connections
If each query holds a connection for 100ms at 1000 queries/sec:
    active_connections = 1000 × 0.100 = 100  ← 2x the pool size! Saturated!
    
Result: New queries QUEUE for a free connection → latency spikes → cascades upstream
```

### Model 3: Timeout Chain Propagation ⏱️

**Analogy:** A phone chain: Alice calls Bob (3 min timeout), Bob calls Charlie (2 min timeout), Charlie calls Dave (1 min timeout). If Dave is slow:

```
Dave is slow (takes 5 minutes):
  Charlie's timeout = 1 min → Charlie gives up after 1 min
  Bob was waiting for Charlie → Bob waited 1 min (still within Bob's 2 min timeout)
  BUT if Bob retries Charlie → Bob waits 1 min + 1 min = 2 min → Bob times out!
  Alice was waiting for Bob → Alice times out too

Key insight: Timeouts DON'T prevent cascades. They just change the cascade SPEED.
```

### Model 4: Retry Amplification 🔄

**Analogy:** A highway with a traffic jam. Cars (requests) get stuck. Impatient drivers (retries) take detours — but the detours all lead back to the same jammed intersection, making it WORSE.

```
Normal:  1000 req/s to service C
C fails 50% of requests
Caller B retries failed requests 3 times:
    Amplified load = 1000 + (500 failures × 3 retries) = 2500 req/s
    
But 2500 req/s makes C fail MORE → say 65% now:
    Amplified load = 1000 + (650 × 3) = 2950 req/s
    
And 2950 causes 80% failure:
    Amplified load = 1000 + (800 × 3) = 3400 req/s
    
This is a POSITIVE FEEDBACK LOOP — a "death spiral"
```

The model runs iteratively until it converges (or hits a cap of 10 iterations).

---

## How The Simulation Works — Step by Step

### Step 1: Take a Snapshot

```go
snap := dag.Snapshot()  // Immutable deep copy of the live DAG
```

This freezes the current state of the graph — all nodes, edges, and their metrics. The simulation works on this frozen copy while the live DAG continues updating.

**Why a copy?** If we simulated on the live DAG, our simulated perturbations would corrupt the real data. Also, the simulation takes a few milliseconds and we don't want to hold locks that long.

### Step 2: For Each Node, Run 1000 Simulations

```
for each node N in the graph:
    for sim = 1 to 1000:
        1. Roll the dice: how much does N's latency increase?
           latencyDelta = |Normal(0, σ²)|  where σ = 30% of current P99
        
        2. Propagate the failure through the graph (BFS upstream)
        
        3. Record: which nodes failed, how fast, what path
```

**What is Normal(0, σ²)?** A bell curve centered at zero. Most of the time the perturbation is small (near zero). Occasionally it's large. This models real-world behavior — most slowdowns are small, but occasionally you get a major spike.

### Step 3: BFS Cascade Propagation

**BFS = Breadth-First Search** — explore one "hop" at a time.

```
Origin: payments is slow (+200ms)
    │
    ▼ Who CALLS payments? → orders
    Apply 4 models to the orders→payments edge:
        Thread pool: 300 req/s × 0.350s = 105 active (pool=200) → ok
        But orders is now slow too (+50ms from queuing)
    │
    ▼ Who CALLS orders? → gateway
    Apply 4 models to the gateway→orders edge:
        Thread pool: 500 req/s × 0.200s = 100 active (pool=200) → ok
    
    Cascade path: [payments, orders, gateway]
    Result: payments DEGRADED, orders DEGRADED, gateway HEALTHY
```

**Key: propagation goes UPSTREAM.** If the leaf service (payments) gets slow, the services that CALL it (orders) are affected, then THEIR callers (gateway).

### Step 4: Compute Risk Score

```
Risk(payments) = 
    Σ for each affected node D:
        P(cascade to D) × Impact(D) × Urgency(D)

P(cascade to orders) = 920/1000 = 0.92  (92% of simulations)
Impact(orders) = InDegree(orders) / maxInDegree = 1/1 = 1.0
Urgency(orders) = 1 / mean_TTF = 1 / 1.2s = 0.83

Contribution from orders = 0.92 × 1.0 × 0.83 = 0.76
... plus contributions from other affected nodes ...

Final: normalized and clamped to [0, 1] → Risk = 0.87 (CRITICAL)
```

### Step 5: Build Report

```json
{
  "node_results": {
    "payments": {
      "risk_score": 0.367,
      "risk_level": "MEDIUM",
      "cascade_probability": 1.0,
      "top_cascade_paths": [
        {"path": ["payments", "orders", "gateway"], "frequency": 847}
      ],
      "affected_nodes": ["orders", "gateway"]
    }
  }
}
```

---

## Parallelization

Each node's simulation is independent — we can simulate all nodes in parallel.

```go
// Semaphore pattern: bounded concurrency
sem := make(chan struct{}, MaxConcurrentNodes)  // e.g., 20 slots

for each node:
    sem <- struct{}{}    // Acquire a slot (blocks if all slots taken)
    go func() {
        defer func() { <-sem }()  // Release slot when done
        result := simulateNode(...)
    }()
```

**Why not unlimited goroutines?** Each simulation involves thousands of BFS traversals. Too many goroutines would thrash CPU caches and actually be SLOWER. The semaphore limits concurrency to `NumCPU()`.

---

## How It All Connects

```
Phase 1: eBPF probes → raw kernel events
Phase 2: PID resolver → DAG (topology)
Phase 3: Sliding windows → EdgeMetrics (real-time stats)
Phase 4: Monte Carlo → SimulationReport (risk predictions)
                │
                ├── Phase 4b reads: oracle.LatestReport()
                │   → if RiskLevel == CRITICAL → apply load-shedding
                │
                └── Phase 5 reads: oracle.LatestReport()
                    → display in TUI/Grafana dashboard

Every 30 seconds:
  1. Oracle calls dag.Snapshot() (immutable copy with all EdgeMetrics)
  2. Runs 1000 simulations per node in parallel
  3. Produces SimulationReport with risk scores and cascade paths
  4. Logs summary to stderr
```

---

## Key Concepts Glossary

| Concept | Simple Explanation |
|---|---|
| **Monte Carlo simulation** | Run thousands of random experiments to estimate probabilities. Like flipping a coin 10,000 times to verify it's 50/50. |
| **Perturbation** | A random "nudge" — we make a service slightly slower to see what happens. |
| **Normal distribution** | Bell curve. Most values are near the center, few are extreme. Models real-world randomness. |
| **BFS (Breadth-First Search)** | Explore one level at a time. Like ripples spreading from a stone dropped in water. |
| **Time-To-Failure (TTF)** | How long until a service's resource pool is completely depleted after perturbation. |
| **Risk score** | A number from 0 to 1 combining: probability of cascade × impact × urgency. |
| **Semaphore** | A counter that limits concurrent access. Like a parking garage with N spaces — when full, cars wait. |
| **DAGSnapshot** | An immutable (frozen) copy of the live graph. Safe to read without locks. |
| **Positive feedback loop** | A → more B → more A → more B... (death spiral). Retry amplification is the classic example. |
| **Fixed-point iteration** | Run a formula repeatedly until the output stops changing. Used in the retry model. |

---

## Benchmark Results

| Metric | Result | Target |
|---|---|---|
| 10-node graph, 1000 sims | **5.6ms** | < 500ms |
| Speedup vs target | **89×** | — |
| Allocations | 270K allocs | — |

The simulation is fast enough to run every 5 seconds if needed, leaving plenty of CPU headroom for the eBPF probes and metrics engine.
