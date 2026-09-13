# CascadeShield — System Architecture & Implementation Plan

> **eBPF-Powered Predictive Cascading Failure Engine for Microservices**
>
> Autonomous kernel-level topology discovery → Monte Carlo cascade prediction → Graph-aware load-shedding

---

## High-Level Architecture

```
┌─────────────────────────────────────── KERNEL SPACE ───────────────────────────────────────┐
│                                                                                            │
│   ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌───────────────────┐              │
│   │  tcp_connect  │  │  tcp_accept  │  │  tcp_close   │  │  tcp_retransmit   │              │
│   │  kprobe (C)   │  │  kprobe (C)  │  │  kprobe (C)  │  │  tracepoint (C)   │              │
│   └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └────────┬──────────┘              │
│          │                 │                 │                    │                         │
│          └────────────┬────┴────────┬────────┴────────────────────┘                         │
│                       ▼             ▼                                                       │
│              ┌─────────────────────────────┐                                               │
│              │    BPF Ring Buffer           │   ← Zero-copy event stream to userspace      │
│              │    (per-CPU, lock-free)      │                                               │
│              └─────────────┬───────────────┘                                               │
└────────────────────────────┼───────────────────────────────────────────────────────────────┘
                             │
═══════════════════════════════════════════════════════════════════════════════════════════════
                             │
┌────────────────────────────┼──────────────── USER SPACE (Go) ─────────────────────────────┐
│                            ▼                                                               │
│  ┌─────────────────────────────────────────┐                                               │
│  │         PHASE 1: KERNEL EYE             │                                               │
│  │  ┌─────────────┐   ┌────────────────┐   │                                               │
│  │  │ eBPF Loader │   │ Event Decoder  │   │                                               │
│  │  │ (cilium/ebpf)│   │ (ring buffer   │   │                                               │
│  │  │             │   │  consumer)     │   │                                               │
│  │  └─────────────┘   └───────┬────────┘   │                                               │
│  └────────────────────────────┼────────────┘                                               │
│                               ▼                                                            │
│  ┌─────────────────────────────────────────┐                                               │
│  │         PHASE 2: TOPOLOGY ENGINE        │                                               │
│  │  ┌─────────────┐   ┌────────────────┐   │                                               │
│  │  │ Pod/Service │   │ Dependency DAG │   │                                               │
│  │  │ Resolver    │   │ Constructor    │   │                                               │
│  │  │ (K8s API +  │   │ (adjacency     │   │                                               │
│  │  │  /proc)     │   │  list + edge   │   │                                               │
│  │  │             │   │  metadata)     │   │                                               │
│  │  └─────────────┘   └───────┬────────┘   │                                               │
│  └────────────────────────────┼────────────┘                                               │
│                               ▼                                                            │
│  ┌─────────────────────────────────────────┐     ┌─────────────────────────────────────┐   │
│  │         PHASE 3: METRICS ENGINE         │     │     PHASE 5: OBSERVABILITY          │   │
│  │  ┌─────────────┐   ┌────────────────┐   │     │  ┌────────────┐  ┌──────────────┐  │   │
│  │  │ Sliding     │   │ Edge Annotator │   │     │  │ Prometheus │  │ Grafana      │  │   │
│  │  │ Window      │   │ (p50/p95/p99,  │   │────▶│  │ Exporter   │  │ Dashboards   │  │   │
│  │  │ Aggregator  │   │  error rate,   │   │     │  └────────────┘  └──────────────┘  │   │
│  │  │             │   │  req/s)        │   │     │  ┌────────────┐                    │   │
│  │  └─────────────┘   └───────┬────────┘   │     │  │ CLI TUI    │                    │   │
│  └────────────────────────────┼────────────┘     │  │ (bubbletea)│                    │   │
│                               ▼                  │  └────────────┘                    │   │
│  ┌─────────────────────────────────────────┐     └─────────────────────────────────────┘   │
│  │         PHASE 4: THE ORACLE             │                                               │
│  │  ┌─────────────────────────────────┐    │                                               │
│  │  │ Monte Carlo Cascade Simulator   │    │                                               │
│  │  │                                 │    │                                               │
│  │  │  • Thread pool exhaustion model │    │                                               │
│  │  │  • Connection pool saturation   │    │                                               │
│  │  │  • Timeout chain propagation    │    │                                               │
│  │  │  • Retry amplification factor   │    │                                               │
│  │  │  • Risk score per node          │    │                                               │
│  │  │  • Cascade path prediction      │    │                                               │
│  │  │  • Time-to-failure estimation   │    │                                               │
│  │  └──────────────┬──────────────────┘    │                                               │
│  └─────────────────┼──────────────────────┘                                               │
│                    ▼                                                                       │
│  ┌─────────────────────────────────────────┐                                               │
│  │         PHASE 4b: THE SHIELD            │                                               │
│  │  ┌─────────────────────────────────┐    │                                               │
│  │  │ Autonomous Remediation Engine   │    │                                               │
│  │  │                                 │    │                                               │
│  │  │  • Graph-aware load shedding    │    │                                               │
│  │  │  • Optimal edge weight calc     │    │                                               │
│  │  │  • Gradual ramp-down / ramp-up  │    │                                               │
│  │  │  • K8s integration (tc/iptables)│    │                                               │
│  │  └─────────────────────────────────┘    │                                               │
│  └─────────────────────────────────────────┘                                               │
│                                                                                            │
│  ┌─────────────────────────────────────────┐                                               │
│  │         PHASE 6: CHAOS ARENA            │                                               │
│  │  ┌─────────────────────────────────┐    │                                               │
│  │  │ Demo Microservice Mesh          │    │                                               │
│  │  │ (5-service synthetic cluster)   │    │                                               │
│  │  │                                 │    │                                               │
│  │  │  • Chaos injection controller   │    │                                               │
│  │  │  • Controlled latency/error     │    │                                               │
│  │  │  • Before/After cascade proof   │    │                                               │
│  │  └─────────────────────────────────┘    │                                               │
│  └─────────────────────────────────────────┘                                               │
└────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## Repository Structure

```
cascadeshield/
│
├── cmd/
│   └── cascadeshield/
│       └── main.go                    # Entry point — CLI flags, component wiring
│
├── bpf/                               # ── KERNEL SPACE (C) ──
│   ├── headers/
│   │   ├── vmlinux.h                  # Auto-generated kernel type definitions
│   │   └── bpf_helpers.h             # BPF helper function declarations
│   ├── tcp_connect.c                  # kprobe: tcp_v4_connect / tcp_v6_connect
│   ├── tcp_accept.c                   # kretprobe: inet_csk_accept
│   ├── tcp_close.c                    # kprobe: tcp_close (connection duration)
│   ├── tcp_retransmit.c               # tracepoint: tcp:tcp_retransmit_skb
│   └── types.h                        # Shared structs between C ↔ Go
│
├── pkg/                               # ── USER SPACE (Go) ──
│   ├── probe/                         # Phase 1: Kernel Eye
│   │   ├── loader.go                  # Load compiled eBPF objects into kernel
│   │   ├── reader.go                  # Consume events from BPF ring buffer
│   │   └── event.go                   # Event type definitions (Go side)
│   │
│   ├── resolver/                      # Phase 2: Identity Resolution
│   │   ├── proc.go                    # /proc/<pid>/cgroup → container ID
│   │   ├── kubernetes.go              # Container ID → Pod name → Service name
│   │   └── cache.go                   # LRU cache for PID → Service lookups
│   │
│   ├── graph/                         # Phase 2: Topology Engine
│   │   ├── dag.go                     # Directed Acyclic Graph data structure
│   │   ├── edge.go                    # Edge metadata (latency, error rate, req/s)
│   │   ├── node.go                    # Node metadata (service name, pod count, resource limits)
│   │   ├── builder.go                 # Event stream → graph mutations
│   │   └── snapshot.go               # Point-in-time graph serialization
│   │
│   ├── metrics/                       # Phase 3: Metrics Engine
│   │   ├── window.go                  # Sliding window aggregator (tumbling + sliding)
│   │   ├── histogram.go              # HDR histogram for latency percentiles
│   │   ├── annotator.go              # Attach computed metrics to graph edges
│   │   └── prometheus.go             # Prometheus client exposition
│   │
│   ├── simulator/                     # Phase 4: The Oracle
│   │   ├── engine.go                  # Monte Carlo simulation orchestrator
│   │   ├── models/
│   │   │   ├── threadpool.go          # Thread pool exhaustion model
│   │   │   ├── connpool.go            # Connection pool saturation model
│   │   │   ├── timeout.go             # Timeout chain propagation model
│   │   │   └── retry.go              # Retry amplification model
│   │   ├── cascade.go                 # Cascade path computation (BFS/DFS on DAG)
│   │   ├── risk.go                    # Per-node risk score calculation
│   │   └── report.go                 # Simulation result output
│   │
│   ├── shield/                        # Phase 4b: The Shield
│   │   ├── controller.go             # Main remediation control loop
│   │   ├── strategy.go               # Load-shedding strategy computation
│   │   ├── actuator.go               # Apply traffic control (tc/iptables/K8s)
│   │   └── recovery.go              # Auto-recovery when risk subsides
│   │
│   ├── tui/                           # Phase 5: Terminal UI
│   │   ├── app.go                     # Bubbletea main model
│   │   ├── topology.go               # Real-time graph visualization
│   │   ├── simulation.go             # Cascade simulation results view
│   │   └── status.go                 # System health status bar
│   │
│   └── chaos/                         # Phase 6: Chaos Arena
│       ├── injector.go                # Controlled latency/error injection
│       ├── scenario.go               # Pre-built chaos scenarios
│       └── validator.go              # Verify prediction accuracy
│
├── deploy/
│   ├── kubernetes/
│   │   ├── cascadeshield-daemonset.yaml   # DaemonSet (runs on every node)
│   │   ├── rbac.yaml                      # ServiceAccount + ClusterRole
│   │   └── configmap.yaml                 # Runtime configuration
│   ├── grafana/
│   │   └── dashboard.json                 # Pre-built Grafana dashboard
│   └── prometheus/
│       └── rules.yaml                     # Alerting rules
│
├── hack/
│   └── demo-cluster/                  # Demo microservice mesh for testing
│       ├── gateway/                   # API Gateway service (Go)
│       ├── auth/                      # Auth service (Go)
│       ├── orders/                    # Orders service (Go)
│       ├── inventory/                 # Inventory service (Go)
│       ├── payments/                  # Payments service (Go)
│       ├── docker-compose.yaml        # Local dev (Docker Compose)
│       └── kubernetes/               # K8s manifests for demo cluster
│
├── docs/
│   ├── architecture.md
│   ├── ebpf-deep-dive.md
│   └── failure-models.md
│
├── Makefile                           # Build eBPF C → .o, compile Go, generate vmlinux.h
├── go.mod
├── go.sum
└── README.md
```

---

## Phase-by-Phase Architecture

---

## Phase 1: Kernel Eye — eBPF Probe Layer

> **Goal:** Intercept every TCP connection event in the Linux kernel and stream them to userspace with zero overhead.

### What we're hooking and why

| Kernel Hook | Hook Type | What We Capture | Why We Need It |
|---|---|---|---|
| `tcp_v4_connect` | kprobe + kretprobe | Source IP:port, Dest IP:port, PID, timestamp, return code | Detect outgoing connections (Service A → Service B) |
| `inet_csk_accept` | kretprobe | Source IP:port, Dest IP:port, PID, timestamp | Detect incoming connections (Service B accepted from A) |
| `tcp_close` | kprobe | Connection 4-tuple, duration, bytes_sent, bytes_received | Measure connection lifetime and data volume |
| `tcp_retransmit_skb` | tracepoint (`tcp:tcp_retransmit_skb`) | 4-tuple, retransmit count | Detect degradation — retransmits = network issues or slow consumers |

### eBPF Event Structure (shared C ↔ Go)

```c
// bpf/types.h
struct tcp_event {
    __u64 timestamp_ns;     // ktime_get_ns()
    __u32 pid;              // Process ID
    __u32 tid;              // Thread ID
    __u32 saddr;            // Source IPv4
    __u32 daddr;            // Destination IPv4
    __u16 sport;            // Source port
    __u16 dport;            // Destination port
    __u64 duration_ns;      // Connection duration (for close events)
    __u64 bytes_sent;       // TX bytes (for close events)
    __u64 bytes_received;   // RX bytes (for close events)
    __u32 retransmits;      // Retransmit count (for retransmit events)
    __u8  event_type;       // CONNECT=1, ACCEPT=2, CLOSE=3, RETRANSMIT=4
    __u8  ip_version;       // 4 or 6
    __s32 return_code;      // connect() return code
    char  comm[16];         // Process name (task_struct->comm)
};
```

### Data Flow

```
┌─────────────────────────────────────────────────────────┐
│                    KERNEL SPACE                          │
│                                                         │
│  tcp_v4_connect() called by any process                 │
│       │                                                 │
│       ▼                                                 │
│  ┌─────────────────┐     ┌──────────────────────┐       │
│  │ kprobe handler  │────▶│ BPF_MAP_TYPE_HASH    │       │
│  │ (entry: save    │     │ (inflight_connects)  │       │
│  │  args to map)   │     │ key: pid_tgid        │       │
│  └─────────────────┘     │ val: {saddr, daddr,  │       │
│                          │       sport, ts}      │       │
│  tcp_v4_connect() returns│                      │       │
│       │                  └──────────┬───────────┘       │
│       ▼                            │                    │
│  ┌─────────────────┐    lookup     │                    │
│  │ kretprobe       │◀─────────────┘                    │
│  │ (exit: lookup   │                                    │
│  │  saved args,    │     ┌──────────────────────┐       │
│  │  emit event)    │────▶│ BPF_MAP_TYPE_RINGBUF │       │
│  └─────────────────┘     │ (events ring buffer) │       │
│                          │ size: 256KB per CPU  │       │
│                          └──────────┬───────────┘       │
└─────────────────────────────────────┼───────────────────┘
                                      │
                              epoll / ring_buffer__poll()
                                      │
┌─────────────────────────────────────┼───────────────────┐
│                    USER SPACE (Go)  │                    │
│                                     ▼                    │
│                          ┌──────────────────┐            │
│                          │ probe.Reader      │            │
│                          │                  │            │
│                          │ • Poll ring buf  │            │
│                          │ • Decode C struct│            │
│                          │ • Emit Go event  │            │
│                          │   to channel     │            │
│                          └──────────────────┘            │
└──────────────────────────────────────────────────────────┘
```

### Key Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Event transport | BPF Ring Buffer (not perf buffer) | Ring buffer is newer (5.8+), lock-free, supports variable-length records, lower overhead |
| Hook type for connect | kprobe + kretprobe pair | Need entry (to save args) + exit (to capture return code and compute duration) |
| IPv6 support | Phase 1 = IPv4 only, add IPv6 later | Keeps initial eBPF code simpler; IPv4 covers 95%+ of K8s cluster traffic |
| Go eBPF library | `cilium/ebpf` | Most mature pure-Go library, no CGo dependency, used by Cilium itself |
| Kernel version requirement | Linux 5.8+ | Ring buffer support, BTF (BPF Type Format) for CO-RE (Compile Once Run Everywhere) |

### Deliverables
- [ ] `tcp_connect.c` — kprobe/kretprobe for `tcp_v4_connect`
- [ ] `tcp_accept.c` — kretprobe for `inet_csk_accept`
- [ ] `tcp_close.c` — kprobe for `tcp_close`
- [ ] `tcp_retransmit.c` — tracepoint for `tcp:tcp_retransmit_skb`
- [ ] `types.h` — shared event struct
- [ ] `pkg/probe/loader.go` — load eBPF objects into kernel
- [ ] `pkg/probe/reader.go` — consume ring buffer events
- [ ] `Makefile` target: `make bpf` (compile C → .o using clang)
- [ ] Verification: run on host, make 2 processes talk via TCP, see events stream

---

## Phase 2: Topology Engine — Service Resolution & DAG Construction

> **Goal:** Transform raw kernel events (PIDs, IPs, ports) into a meaningful service dependency graph.

### The Resolution Pipeline

```
Raw eBPF Event                    Resolved Event                    Graph Mutation
┌──────────────┐                 ┌──────────────────┐              ┌──────────────┐
│ PID: 48291   │    ┌────────┐   │ Service: "orders"│   ┌──────┐  │ orders ──────│
│ SRC: 10.0.1.5│───▶│Resolver│──▶│ Pod: orders-7f8d │──▶│Builder│─▶│      │      │
│ DST: 10.0.2.3│    └────────┘   │ → Service: "pay" │   └──────┘  │      ▼      │
│ DPORT: 8080  │                 │   Pod: pay-a3c2   │              │  payments   │
└──────────────┘                 └──────────────────┘              └──────────────┘
```

### Resolution Strategy (3-layer fallback)

```
Layer 1: PID → Container ID
    ├── Read /proc/<pid>/cgroup
    ├── Parse container ID from cgroup path
    │   Docker:  /docker/<container_id>
    │   K8s:     /kubepods/pod<uid>/<container_id>
    └── Cache result (PID → ContainerID) with TTL

Layer 2: Container ID → Pod
    ├── Query K8s API: list pods, match container ID in pod.status.containerStatuses
    └── Cache result (ContainerID → Pod) with TTL

Layer 3: Pod → Service
    ├── Pod labels → match K8s Service selectors
    ├── If no Service match, use Deployment/ReplicaSet owner as group name
    └── Fallback: use pod name prefix (e.g., "orders-7f8d" → "orders")
```

### DAG Data Structures

```go
// Node represents a service in the dependency graph
type Node struct {
    ID          string            // Service name (e.g., "orders")
    Pods        []PodInfo         // Active pods backing this service
    Metadata    map[string]string // Labels, namespace, etc.
    ResourceLimits ResourceLimits // CPU/memory limits from K8s
}

// Edge represents a dependency between two services
type Edge struct {
    Source      string    // Caller service
    Target      string    // Callee service
    Metrics     EdgeMetrics
    FirstSeen   time.Time
    LastSeen    time.Time
    Active      bool      // Had traffic in last sliding window
}

// EdgeMetrics — continuously updated from eBPF events
type EdgeMetrics struct {
    RequestRate   float64   // req/s  (sliding window)
    LatencyP50    float64   // ms
    LatencyP95    float64   // ms
    LatencyP99    float64   // ms
    ErrorRate     float64   // fraction (0.0 - 1.0)
    RetransmitRate float64  // TCP retransmits / total packets
    BytesPerSec   float64   // Throughput
}

// DAG — the core dependency graph
type DAG struct {
    Nodes    map[string]*Node
    Edges    map[string]map[string]*Edge  // source → target → edge
    mu       sync.RWMutex                 // Concurrent read/write safety
}
```

### Deliverables
- [ ] `pkg/resolver/proc.go` — PID → container ID via /proc
- [ ] `pkg/resolver/kubernetes.go` — container ID → Pod → Service via K8s API
- [ ] `pkg/resolver/cache.go` — LRU cache with TTL for all resolution layers
- [ ] `pkg/graph/dag.go` — thread-safe DAG with add/remove node/edge
- [ ] `pkg/graph/builder.go` — consumes resolved events, mutates DAG
- [ ] `pkg/graph/snapshot.go` — serialize DAG state for simulation input
- [ ] Verification: deploy 3 services on K8s, see correct topology in logs

---

## Phase 3: Metrics Engine — Edge Annotation & Sliding Windows

> **Goal:** Enrich every edge in the graph with real-time latency percentiles, error rates, and throughput using sliding window aggregation.

### Sliding Window Design

```
                    Time ──────────────────────────────────▶

Window size = 60s, slide interval = 5s

    ┌──────────────────────────────────────────────────────┐
    │              Full 60-second window                    │
    │  ┌────┐┌────┐┌────┐┌────┐┌────┐┌────┐               │
    │  │ B1 ││ B2 ││ B3 ││ B4 ││ B5 ││ B6 │ ... ┌────┐   │
    │  │ 5s ││ 5s ││ 5s ││ 5s ││ 5s ││ 5s │     │B12 │   │
    │  └────┘└────┘└────┘└────┘└────┘└────┘     └────┘   │
    │                                                      │
    │  Every 5s:                                           │
    │    1. Drop oldest bucket (B1)                        │
    │    2. Add new bucket (B13)                           │
    │    3. Recompute aggregates across all 12 buckets     │
    └──────────────────────────────────────────────────────┘
```

### Latency Histogram

Using HDR Histogram for memory-efficient percentile computation:

| Metric | How It's Computed | Source Event |
|---|---|---|
| Latency (p50/p95/p99) | HDR Histogram over `tcp_close.duration_ns` per edge | `CLOSE` events |
| Error Rate | `(retransmit_events + rst_events) / total_connections` per edge | `RETRANSMIT` + `CLOSE` with RST |
| Request Rate | `connection_count / window_duration` per edge | `CONNECT` events |
| Throughput | `sum(bytes_sent + bytes_received) / window_duration` | `CLOSE` events |

### Deliverables
- [ ] `pkg/metrics/window.go` — Generic sliding window with configurable buckets
- [ ] `pkg/metrics/histogram.go` — HDR histogram wrapper for percentile calc
- [ ] `pkg/metrics/annotator.go` — Pipeline: events → window → edge metrics update
- [ ] `pkg/metrics/prometheus.go` — Expose all edge metrics as Prometheus gauges/histograms
- [ ] Verification: generate traffic, confirm p50/p95/p99 accuracy within 5% of actual

---

## Phase 4: The Oracle — Monte Carlo Cascade Simulator

> **Goal:** Given the live annotated DAG, predict cascading failure paths and time-to-failure for every service.

### Failure Models

CascadeShield models 4 real-world failure propagation mechanisms:

#### Model 1: Thread Pool Exhaustion

```
Scenario: Service B slows down → Service A's threads block waiting for B

Parameters:
  - A.thread_pool_size     = 200 threads (from K8s resource limits or config)
  - A→B.latency_current    = 50ms (from eBPF metrics)
  - A→B.request_rate       = 1000 req/s (from eBPF metrics)

Simulation:
  1. Inject: B.latency += delta (e.g., +200ms)
  2. A's threads now blocked for 250ms each on B calls
  3. Active threads for B = request_rate × latency = 1000 × 0.250 = 250
  4. But pool size = 200 → EXHAUSTED
  5. A cannot serve ANY requests (including those not going to B)
  6. Time to exhaustion = pool_size / arrival_rate = 200ms

Output: "If B latency increases by 200ms, A thread pool exhausts in 200ms"
```

#### Model 2: Connection Pool Saturation

```
Same mechanics as thread pool but for database/downstream connection pools.
Tracks connection checkout time + hold duration.
When pool saturates → requests queue → latency spikes → cascades upstream.
```

#### Model 3: Timeout Chain Propagation

```
Scenario: A → B → C → D, each with configured timeouts

If D becomes slow:
  - C times out waiting for D after C→D.timeout (e.g., 3s)
  - C returns error to B after 3s
  - B has been waiting for C for 3s of its B→C.timeout (e.g., 5s)
  - If B retries C, B now waits 3s + 3s = 6s > B→C.timeout → B times out
  - A was waiting for B, A times out too

Key insight: Timeouts DON'T prevent cascades. They just change the cascade speed.
The REAL killer is retry amplification (Model 4).
```

#### Model 4: Retry Amplification

```
Scenario: Service C fails 50% of requests. B retries failed requests 3 times.

Normal load on C:  1000 req/s
With 50% failures: 1000 + (500 × 3 retries) = 2500 req/s
C is now at 2.5x load → more failures → more retries → exponential death spiral

Amplification factor = 1 + (error_rate × max_retries)
If error_rate itself is a function of load... it's a positive feedback loop.
```

### Monte Carlo Simulation Flow

```
┌─────────────────────────────────────────────────────┐
│           SIMULATION ORCHESTRATOR                    │
│                                                     │
│  for each node N in DAG:                            │
│    for i in 1..NUM_SIMULATIONS (e.g., 1000):        │
│      1. Clone current DAG state (snapshot)          │
│      2. Inject perturbation at node N:              │
│         • latency_increase = sample(distribution)   │
│         • error_rate_increase = sample(distribution)│
│      3. Propagate through DAG:                      │
│         for each affected edge, apply:              │
│           • Thread pool model                       │
│           • Connection pool model                   │
│           • Timeout chain model                     │
│           • Retry amplification model               │
│      4. Record: which nodes cascaded, TTF, path     │
│    end                                              │
│    Aggregate: P(cascade), mean TTF, top-3 paths     │
│  end                                                │
│                                                     │
│  Output:                                            │
│  ┌────────────────────────────────────────────┐     │
│  │ Node: "payments"                           │     │
│  │ Cascade Risk Score: 0.87 (CRITICAL)        │     │
│  │ If payments p99 > 500ms:                   │     │
│  │   → orders exhausts in 1.2s (92% prob)     │     │
│  │   → gateway exhausts in 1.8s (87% prob)    │     │
│  │   → Full cluster failure in 3.1s (71% prob)│     │
│  │ Top cascade path:                          │     │
│  │   payments → orders → gateway → [all]      │     │
│  └────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────┘
```

### Risk Score Formula

```
Risk(node) = Σ over downstream dependents D of:
    P(cascade to D | node degrades) × Impact(D) × (1 / TTF(D))

Where:
  P(cascade)  = fraction of Monte Carlo runs where D failed
  Impact(D)   = number of upstream services that depend on D (fan-in)
  TTF(D)      = mean time-to-failure of D across simulations
  1/TTF       = urgency factor (faster cascade = higher risk)
```

### Deliverables
- [ ] `pkg/simulator/models/threadpool.go` — Thread pool exhaustion model
- [ ] `pkg/simulator/models/connpool.go` — Connection pool saturation model
- [ ] `pkg/simulator/models/timeout.go` — Timeout chain propagation model
- [ ] `pkg/simulator/models/retry.go` — Retry amplification model
- [ ] `pkg/simulator/engine.go` — Monte Carlo orchestrator (parallelized with goroutines)
- [ ] `pkg/simulator/cascade.go` — BFS cascade path computation
- [ ] `pkg/simulator/risk.go` — Risk score aggregation
- [ ] `pkg/simulator/report.go` — Structured simulation output
- [ ] Verification: feed a known DAG, verify predictions match manual calculation

---

## Phase 4b: The Shield — Autonomous Remediation

> **Goal:** When cascade risk exceeds threshold, compute and apply optimal load-shedding across graph edges.

### Control Loop

```
Every simulation_interval (e.g., 10s):
    
    1. Run Oracle simulation on current DAG
    2. For each node with Risk > THRESHOLD (e.g., 0.7):
        a. Identify the critical cascade path
        b. Find the "cut edge" — the edge whose shedding
           maximally reduces cascade probability
        c. Compute optimal shed percentage:
           shed% = min(1.0, (Risk - THRESHOLD) / (1.0 - THRESHOLD))
        d. Apply traffic control on that edge
    3. For each node with Risk < RECOVERY_THRESHOLD (e.g., 0.3):
        a. If load-shedding is active, gradually ramp up (10% per interval)
    4. Export all decisions as Prometheus metrics + structured logs
```

### Actuation Methods (pluggable)

| Method | How | Pros | Cons |
|---|---|---|---|
| **eBPF/TC** | Attach eBPF program to `tc` (traffic control) qdisc, probabilistically drop packets | Kernel-level, zero user-space overhead | Aggressive — drops at TCP level |
| **iptables** | Insert iptables rules with `-m statistic --probability` | Well understood, easy to debug | Slightly higher overhead than eBPF/TC |
| **K8s NetworkPolicy** | Modify NetworkPolicy to restrict traffic | K8s-native, audit trail | Slow to apply (API server round-trip) |

> [!IMPORTANT]
> **Phase 4b is the most dangerous component.** Autonomous traffic manipulation in production requires:
> - A dry-run mode (log what WOULD be done, don't do it)
> - A kill switch (instantly remove all shedding rules)
> - Rate limits on how fast shedding can ramp up
> - An audit log of every action taken

### Deliverables
- [ ] `pkg/shield/controller.go` — Main control loop (observe → decide → act)
- [ ] `pkg/shield/strategy.go` — Compute which edges to shed and by how much
- [ ] `pkg/shield/actuator.go` — Pluggable interface for TC/iptables/K8s actuation
- [ ] `pkg/shield/recovery.go` — Gradual ramp-up when risk subsides
- [ ] Dry-run mode as default, explicit `--armed` flag to enable real actuation
- [ ] Verification: inject latency in demo cluster, verify CascadeShield sheds correct edge

---

## Phase 5: Observability — Prometheus, Grafana, CLI TUI

> **Goal:** Make CascadeShield's internals visible and beautiful.

### Prometheus Metrics Exported

```
# Edge-level metrics (labels: source, target)
cascadeshield_edge_latency_p99_ms{source="orders", target="payments"}
cascadeshield_edge_error_rate{source="orders", target="payments"}
cascadeshield_edge_request_rate{source="orders", target="payments"}

# Node-level risk (labels: service)
cascadeshield_node_risk_score{service="payments"}
cascadeshield_node_cascade_probability{service="payments"}
cascadeshield_node_ttf_seconds{service="payments"}

# Shield actions (labels: edge, action)
cascadeshield_shield_active{source="orders", target="payments"}
cascadeshield_shield_shed_percentage{source="orders", target="payments"}

# System health
cascadeshield_ebpf_events_per_second
cascadeshield_graph_node_count
cascadeshield_graph_edge_count
cascadeshield_simulation_duration_ms
```

### CLI TUI (Terminal UI)

```
┌─ CascadeShield v0.1.0 ─────────────────────────────────────────────────────┐
│                                                                            │
│  ┌─ Live Topology ──────────────────────┐  ┌─ Risk Scores ──────────────┐  │
│  │                                      │  │                            │  │
│  │   gateway ──▶ orders ──▶ payments    │  │  payments    ██████░ 0.87  │  │
│  │      │          │           │        │  │  inventory   ███░░░░ 0.42  │  │
│  │      │          ▼           ▼        │  │  orders      ██░░░░░ 0.31  │  │
│  │      │      inventory    stripe      │  │  auth        █░░░░░░ 0.12  │  │
│  │      ▼                              │  │  gateway     █░░░░░░ 0.08  │  │
│  │     auth                            │  │                            │  │
│  │                                      │  └────────────────────────────┘  │
│  └──────────────────────────────────────┘                                  │
│                                                                            │
│  ┌─ Cascade Prediction ────────────────────────────────────────────────┐   │
│  │  ⚠ CRITICAL: payments p99 approaching threshold (487ms / 500ms)     │   │
│  │  Predicted cascade: payments → orders (1.2s) → gateway (1.8s)       │   │
│  │  Shield: ARMED | Shed: orders→payments 23% | Mode: GRADUAL          │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                            │
│  Events: 48,291/s │ Nodes: 5 │ Edges: 7 │ Sim: 142ms │ Uptime: 2h 14m    │
└────────────────────────────────────────────────────────────────────────────┘
```

### Deliverables
- [ ] `pkg/metrics/prometheus.go` — All metrics registered and updated
- [ ] `pkg/tui/app.go` — Bubbletea TUI main application
- [ ] `pkg/tui/topology.go` — ASCII graph renderer
- [ ] `pkg/tui/simulation.go` — Risk scores and cascade predictions
- [ ] `deploy/grafana/dashboard.json` — Pre-built Grafana dashboard
- [ ] Verification: TUI renders correct topology, Grafana shows metrics

---

## Phase 6: Chaos Arena — Demo Cluster & Validation

> **Goal:** Build a synthetic microservice mesh, inject real chaos, and prove CascadeShield predicts and prevents cascading failures.

### Demo Microservice Architecture

```
                    ┌─────────┐
       ─────────────│ Gateway │─────────────
      │             └────┬────┘             │
      ▼                  ▼                  ▼
┌──────────┐      ┌──────────┐      ┌──────────┐
│   Auth   │      │  Orders  │      │ Analytics│
│          │      │          │      │          │
│ • JWT    │      │ • CRUD   │      │ • Read   │
│ • 10ms   │      │ • 30ms   │      │ • 50ms   │
│   resp   │      │   resp   │      │   resp   │
└──────────┘      └────┬─────┘      └──────────┘
                       │
              ┌────────┴────────┐
              ▼                 ▼
        ┌──────────┐     ┌──────────┐
        │Inventory │     │ Payments │
        │          │     │          │
        │ • Stock  │     │ • Stripe │
        │ • 20ms   │     │ • 100ms  │
        │   resp   │     │   resp   │
        └──────────┘     └──────────┘
```

Each service is a simple Go HTTP server with:
- Configurable response latency (via env var)
- Configurable error rate (via env var)
- A `/chaos` endpoint to dynamically inject latency/errors
- A `/health` endpoint for liveness

### Chaos Scenarios to Validate

| # | Scenario | Inject | Expected Prediction | Expected Shield Action |
|---|---|---|---|---|
| 1 | **Payment slowdown** | payments latency += 300ms | orders thread exhaustion in ~1s | Shed orders→payments traffic by 40% |
| 2 | **Inventory crash** | inventory returns 503 at 80% rate | orders retries amplify load 3x on inventory | Shed orders→inventory, circuit break |
| 3 | **Auth cascade** | auth latency += 2s | ALL services blocked (auth is in critical path) | Shed gateway→auth, serve degraded |
| 4 | **Slow burn** | payments latency increases 10ms/min | Predict future exhaustion before threshold hit | Preemptive shed at predicted T-30s |

### Demo Script (for resume/interviews)

```
1. Deploy demo cluster to Minikube/Kind
2. Deploy CascadeShield as DaemonSet
3. Show TUI: clean topology, all risks green
4. Inject chaos: curl payments:8080/chaos?latency=300ms
5. Watch TUI: risk scores change in real-time
6. Watch prediction: "orders will exhaust in 1.2s"
7. Watch shield: auto-sheds 40% of orders→payments traffic
8. Show Grafana: before/after dashboard
9. Remove chaos: services auto-recover
```

### Deliverables
- [ ] 5 demo microservices in `hack/demo-cluster/` (Go, ~100 LOC each)
- [ ] Docker Compose for local dev
- [ ] K8s manifests for Kind/Minikube deployment
- [ ] `pkg/chaos/injector.go` — chaos injection via HTTP endpoints
- [ ] `pkg/chaos/scenario.go` — pre-built scenario definitions
- [ ] `pkg/chaos/validator.go` — compare prediction vs. actual outcome
- [ ] Demo recording script

---

## Technology Stack Summary

| Layer | Technology | Why |
|---|---|---|
| Kernel probes | C (eBPF) | Only language the eBPF verifier accepts |
| eBPF loader | `cilium/ebpf` (Go) | Pure Go, no CGo, battle-tested by Cilium |
| Control plane | Go | Goroutines for concurrent simulation, strong K8s ecosystem |
| K8s integration | `client-go` | Official Kubernetes Go client |
| Metrics | Prometheus + Grafana | Industry standard SRE observability |
| Terminal UI | Bubbletea (Go) | Best TUI framework in Go ecosystem |
| Container runtime | Docker + Kind/Minikube | Local K8s cluster for dev & demo |
| Build system | Makefile + clang | Standard for eBPF C → .o compilation |

---

## Prerequisites & Dev Environment

| Requirement | Minimum | Recommended |
|---|---|---|
| Linux Kernel | 5.8+ | 5.15+ (Ubuntu 22.04) |
| Go | 1.21+ | 1.22+ |
| clang/llvm | 12+ | 15+ |
| Docker | 20.10+ | 24+ |
| Kind or Minikube | Latest | Kind (lighter than Minikube) |
| kubectl | 1.27+ | Latest |
| bpftool | Matching kernel | For vmlinux.h generation |

> [!WARNING]
> **eBPF requires Linux.** If you're on macOS/Windows, you'll need a Linux VM or use the Docker-based dev environment we'll set up. WSL2 on Windows works but has kernel limitations.

---

## Timeline Estimate

| Phase | Duration | Dependency |
|---|---|---|
| Phase 1: Kernel Eye | 2 weeks | None |
| Phase 2: Topology Engine | 1.5 weeks | Phase 1 |
| Phase 3: Metrics Engine | 1 week | Phase 2 |
| Phase 4: The Oracle | 2 weeks | Phase 3 |
| Phase 4b: The Shield | 1.5 weeks | Phase 4 |
| Phase 5: Observability | 1 week | Phase 3 (parallel with 4) |
| Phase 6: Chaos Arena | 1 week | All phases |
| **Total** | **~10 weeks** | |

> [!TIP]
> Phase 5 (Observability) can be built in parallel with Phase 4 since it only depends on the metrics engine. Phase 6 is the integration test of everything.
