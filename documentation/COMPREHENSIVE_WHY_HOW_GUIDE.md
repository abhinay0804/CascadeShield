# CascadeShield: Complete Architecture, System Design & Implementation Guide
## The Ultimate "Why & How" Engineering Reference Document

> **System Name:** CascadeShield  
> **Repository:** `github.com/abhinay0804/CascadeShield`  
> **Author & Design:** Nama Abhinay  
> **Scope:** Comprehensive end-to-end breakdown of the physics, mathematics, architecture, code design, eBPF probes, Monte Carlo simulation, autonomous remediation, and operational telemetry of CascadeShield.

---

## 1. Executive Summary & Core Vision

**CascadeShield** is an autonomous, eBPF-powered resilience engine designed to predict, prevent, and remediate cascading failures in cloud-native microservice meshes *before* they cause total system outages.

Traditional observability tools (Prometheus alerts, Datadog, Grafana) are reactive: they fire alerts **after** CPU hits 90% or error rates spike. By the time a human SRE receives a PagerDuty alert, connection pools across downstream dependencies have already exhausted, goroutines have piled up in memory, and the entire cluster has crashed in a chain reaction.

CascadeShield solves this problem by pairing **kernel-level eBPF connection tracing** with a real-time **Monte Carlo simulation engine ("The Oracle")** and an **autonomous traffic-shedding remediation controller ("The Shield")**:
1. **Kernel Eye (eBPF)**: Captures every TCP connection, accept, close, and retransmit at zero overhead directly in the Linux kernel.
2. **Topology Engine**: Maps raw IP/port socket events to process PIDs, container cgroups, and Kubernetes microservices to build a dynamic Directed Acyclic Graph (DAG) of service dependencies.
3. **Metrics Engine**: Computes high-precision sliding-window P50/P95/P99 latency percentiles, retransmit error rates, and connection throughput per dependency edge.
4. **The Oracle**: Runs 1,000 parallel Monte Carlo simulations using 4 non-linear failure models to predict upcoming service exhaustion and time-to-failure (TTF) seconds in advance.
5. **The Shield**: Executes min-cut load-shedding algorithms with a gradual ramp-up/ramp-down recovery state machine to shed traffic on degraded paths while keeping the rest of the cluster operational.

---

## 2. THE WHY: Physics of Cascading Failures in Distributed Systems

To understand why CascadeShield was built, we must understand the fundamental physical and mathematical failure modes of microservices.

### 2.1 The Goroutine / Thread Pool Exhaustion Spiral
Modern cloud services rely on concurrency pools (worker threads or Go HTTP worker goroutines). When a downstream dependency (e.g. `Payments`) experiences a 300ms latency increase:
$$\text{Latency}_{\text{new}} = \text{Latency}_{\text{base}} + 300\,\text{ms}$$

Every upstream request calling `Payments` (e.g. `Orders`) must hold its open TCP connection and goroutine 10x longer while waiting for a response:
$$\text{Concurrency} = \text{Arrival Rate} \times \text{Latency} \quad (\text{Little's Law})$$

As arrival rate remains constant while latency increases 10x, the required worker pool size exceeds available capacity. Goroutines pile up in memory, memory consumption surges, context switching overhead spikes, and `Orders` exhausts its connection pool, cascading to `Gateway` and freezing the entire user-facing API.

```
┌─────────┐                ┌─────────┐                ┌──────────┐
│ Gateway │ ──(Blocks)──►  │ Orders  │ ──(Exhausts)─► │ Payments │ (Slow +300ms)
└─────────┘                └─────────┘                └──────────┘
  Goroutines                 Goroutines                 Queue Length
  Exhausted                  Exhausted                  Spikes 10x
```

### 2.2 Naïve Retry Amplification
When a service starts failing (e.g. `Inventory` returning 503 errors at an 80% rate), upstream services (`Orders`) often attempt retries ($N_{\text{retries}} = 3$) without exponential backoff.
- Baseline request rate: $R = 1,000\,\text{req/s}$
- If $80\%$ fail, $800\,\text{req/s}$ trigger 3 retries:
$$\text{Amplified Load} = R + (0.80 \times 3 \times R) = 1,000 + 2,400 = 3,400\,\text{req/s}$$

Instead of helping, retries **multiply load by 3.4x on an already dying service**, turning a minor transient glitch into an unrecoverable crash.

### 2.3 The Auth / Critical-Path Bottleneck
Some microservices exist on every critical execution path (e.g. `Auth` verifying JWTs on all requests). If `Auth` experiences a 2-second delay:
$$\text{Impact Factor} = 100\% \text{ of Gateway Traffic Blocked}$$

Even though `Orders`, `Inventory`, and `Payments` are 100% healthy, no customer can place an order because every request stalls at the entrance.

### 2.4 Why Traditional Monitoring Fails
1. **Metrics Lag**: Prometheus scrapes metrics every 15–30 seconds. A thread pool exhaustion cascade occurs in < 1.5 seconds.
2. **Threshold Blindness**: Setting a static alert on CPU > 80% misses thread lockup failures where CPU is near 0% because threads are all blocked on socket I/O reads.
3. **No Predictive Modeling**: Traditional tools tell you *what broke in the past*, not *what will break in 2 seconds*.

---

## 3. THE HOW: End-to-End System Architecture

CascadeShield is structured into 6 pipeline layers:

```
┌────────────────────────────────────────────────────────────────────────┐
│ 1. KERNEL EYE (eBPF)                                                   │
│    probes.c: kprobes on tcp_v4_connect, kretprobe, tracepoint retransmit│
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ eBPF Ring Buffer
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 2. TOPOLOGY ENGINE (Resolver & DAG Builder)                            │
│    PID -> cgroup -> Pod/Container -> K8s Service -> Dynamic DAG        │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ DAGSnapshot
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 3. METRICS ENGINE (Sliding Windows & HDR Histograms)                   │
│    Calculates P50/P95/P99 latency, error rates, throughput per edge   │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Annotated Graph Metrics
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 4. THE ORACLE (Monte Carlo Simulator)                                  │
│    1,000 parallel runs with Thread, Retry, GC, Slow-Burn failure models │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ SimulationReport (Node Risk & TTF)
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 4b. THE SHIELD (Autonomous Remediation Controller)                     │
│     Decide -> Act -> Recover state machine with Ramp-up/down safety    │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Telemetry & Actuation
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 5 & 6. OBSERVABILITY & CHAOS ARENA                                      │
│        Prometheus, Grafana, Bubbletea TUI, Demo Mesh & Validator       │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 4. Layer-by-Layer Technical Deep-Dive

### 4.1 Phase 1: Kernel Eye (eBPF Probes)

#### Why eBPF?
User-space proxies (like Envoy or Istio sidecars) introduce 2–5ms latency per hop and consume substantial CPU/memory per pod. eBPF runs bytecode directly inside the Linux kernel with minimal kernel overhead via ring buffer zero-copy telemetry, capturing socket events before they reach user space.

#### Code Architecture (`bpf/probes.c`)
Four eBPF hooks are compiled into a single ELF object using `bpf2go`:
1. `kprobe/tcp_v4_connect`: Fires when an outbound TCP connection begins. Stores socket reference `struct sock *sk` in an eBPF hash map keyed by `pid_tgid`.
2. `kretprobe/tcp_v4_connect`: Fires when connect returns. Retrieves `sk`, extracts source/destination IP and port, and emits a `CONN_START` event to a shared eBPF ring buffer.
3. `tracepoint/tcp/tcp_retransmit_skb`: Fires on TCP packet retransmission. Emits a `RETRANSMIT` event to measure network/service degradation.
4. `kprobe/tcp_close`: Fires when socket closes. Emits a `CONN_CLOSE` event.

#### eBPF Ring Buffer vs Perf Buffer
CascadeShield uses `BPF_MAP_TYPE_RINGBUF` (introduced in Linux 5.8) rather than old perf buffers. Ring buffers use a single shared memory-mapped circular buffer across all CPUs, eliminating per-CPU memory overhead and guarantee FIFO ordering.

#### The C vs Go Packed Struct Issue
In C (`bpf/probes.c`), the event struct is defined as `__attribute__((packed))`:
```c
struct tcp_event {
    u64 timestamp_ns; // 8 bytes
    u32 pid;          // 4 bytes
    u32 saddr;        // 4 bytes
    u32 daddr;        // 4 bytes
    u16 sport;        // 2 bytes
    u16 dport;        // 2 bytes
    u8  event_type;   // 1 byte
    // Total packed size: 25 bytes (aligned to 74 bytes with metadata)
};
```
Because Go struct layout adds padding bytes for memory alignment (80 bytes in Go vs 74 bytes in C), standard `binary.Read()` fails. CascadeShield solves this using **manual binary decoding** in `pkg/probe/event.go` using bitwise shifts from slice offsets.

---

### 4.2 Phase 2: Topology Engine (PID Resolver & DAG)

#### PID to Microservice Resolution
Raw socket events give us `(PID, Source IP:Port, Dest IP:Port)`. How do we convert `PID 14201` into `"orders-service"`?
1. `/proc/PID/cgroup` Inspection: Reads the control group path to find the container ID (Docker/cgroupv2).
2. Kubernetes Client (`client-go`): Queries local K8s pod informer to match container ID or IP address to Pod metadata and Service labels.
3. Fallback Heuristics: Uses process command line (`/proc/PID/cmdline`) for non-K8s binary deployments.

#### Dynamic Directed Acyclic Graph (DAG) Builder (`pkg/graph/builder.go`)
- Service nodes (`Node`) are added dynamically as connection events arrive.
- Directed edges (`Edge`) represent active communication paths: `Source Service -> Target Service`.
- `DAGSnapshot`: Thread-safe, immutable snapshot of the service graph taken at a specific timestamp. This snapshot is handed to The Oracle for Monte Carlo simulation.

---

### 4.3 Phase 3: Metrics Engine (Sliding Windows & Histograms)

#### High-Precision Windowing (`pkg/metrics/`)
Raw event counts are insufficient to compute P99 latency or error rates. CascadeShield uses a **sliding time window** (e.g. 60 seconds split into 1-second buckets):
- **Request Rate ($R$)**: Total connection events per second.
- **Latency Percentiles ($P_{50}, P_{95}, P_{99}$)**: Computed using an HDR-style logarithmic histogram bucket array to preserve sub-millisecond accuracy without storing raw samples in memory.
- **Error Rate ($E$)**: Ratio of retransmit events to total connection events:
$$E = \frac{\text{Retransmit Events}}{\text{Total Connection Events}}$$

---

### 4.4 Phase 4: The Oracle (Monte Carlo Cascade Simulator)

#### How The Oracle Works (`pkg/simulator/engine.go`)
The Oracle predicts future failures by running **Monte Carlo simulations** directly on the live DAG snapshot. It runs $N = 1,000$ iterations per cycle.

In each iteration:
1. It selects an origin node and injects synthetic stress (latency increase or error spike).
2. It propagates traffic through downstream edges based on 4 non-linear failure models.
3. It tracks which downstream nodes exceed their thread/connection capacity and record Time-to-Failure (TTF).

```
                      ┌────────────────────────────────────────┐
                      │    Monte Carlo Loop (N=1000 Runs)     │
                      └───────────────────┬────────────────────┘
                                          │
       ┌──────────────────────┬───────────┴──────────┬──────────────────────┐
       ▼                      ▼                      ▼                      ▼
┌──────────────┐       ┌──────────────┐       ┌──────────────┐       ┌──────────────┐
│ Model 1:     │       │ Model 2:     │       │ Model 3:     │       │ Model 4:     │
│ Thread Pool  │       │ Retry Amp    │       │ GC Pause     │       │ Slow Burn    │
│ Exhaustion   │       │ Multiplier   │       │ Non-linear   │       │ Trajectory   │
└──────┬───────┘       └──────┬───────┘       └──────┬───────┘       └──────┬───────┘
       │                      │                      │                      │
       └──────────────────────┴───────────┬──────────┴──────────────────────┘
                                          │
                                          ▼
                      ┌────────────────────────────────────────┐
                      │  Calculate Node Risk Score [0.0 - 1.0] │
                      │  & Estimated Time-to-Failure (TTF)     │
                      └────────────────────────────────────────┘
```

#### The 4 Failure Models (`pkg/simulator/models/`)
1. **Thread Pool Exhaustion Model**: Models goroutine/thread pool saturation based on Little's Law. As latency grows, available concurrency drops to 0.
2. **Retry Amplification Model**: Models non-linear traffic amplification when error rates trigger upstream retries ($R_{\text{effective}} = R \times (1 + E \times N_{\text{retries}})$).
3. **GC Pause Model**: Models Stop-The-World (STW) garbage collection pauses when memory pressure exceeds 85% utilization.
4. **Slow Burn Model**: Fits a linear regression line over latency trends to predict when a service will hit its failure threshold $T_{\text{exhaustion}}$ in the future.

#### Risk Score Calculation
For each node $i$:
$$\text{RiskScore}_i = w_1 \cdot P(\text{Exhaustion}) + w_2 \cdot \left(1 - \frac{\text{TTF}}{\text{TTF}_{\text{max}}}\right) + w_3 \cdot \text{CascadeProbability}$$
Where weights $w_1 + w_2 + w_3 = 1.0$.

---

### 4.5 Phase 4b: The Shield (Autonomous Remediation Controller)

#### Control Loop Architecture (`pkg/shield/controller.go`)
The Shield executes a tight control loop every 10 seconds:
1. **OBSERVE**: Reads latest `SimulationReport` from The Oracle.
2. **DECIDE**: Identifies nodes where $\text{RiskScore} \ge \text{ShedThreshold}$ ($0.70$). Applies min-cut edge selection to find optimal upstream edges to drop traffic.
3. **ACT**: Applies shedding percentages via the pluggable `Actuator` interface.
4. **RECOVER**: Uses a gradual thermostat ramp-down state machine when risk subsides.

#### Gradual Ramp-Up / Ramp-Down Hysteresis (`pkg/shield/recovery.go`)
To prevent traffic oscillation (flipping shedding on/off rapidly, causing traffic storms):
- **Ramp-Up**: Shed percentage increases by at most `RampUpStep` ($15\%$) per control interval.
- **Ramp-Down**: Shed percentage decreases by at most `RampDownStep` ($10\%$) per interval.
- **Recovery Hysteresis**: Shedding is only fully removed after risk remains below `RecoveryThreshold` ($0.30$) for `ConsecutiveLowIntervals` ($3$ intervals / 30 seconds).

#### Safety Controls & Dry-Run Mode
- **DRY-RUN DEFAULT**: `Armed = false` by default. Decisions are logged with `[DRY-RUN]` prefix without dropping packets unless `--shield-armed` is explicitly passed.
- **Traffic Floor**: `MaxShedPercentage = 0.80` ensures at least $20\%$ of traffic always flows (prevents complete blackholes).
- **Kill Switch**: `--kill-switch` CLI flag or `Controller.KillSwitch()` immediately zeroes out all active shedding rules.

---

### 4.6 Phase 5: Observability Engine

#### Prometheus Metrics Exporter (`pkg/metrics/prometheus.go`)
Exposes operational telemetry on port `:9090`:
- `cascadeshield_node_risk_score{service="..."}`: Real-time risk score [0.0, 1.0].
- `cascadeshield_node_cascade_probability{service="..."}`: Failure probability.
- `cascadeshield_shield_shed_percentage{source="...",target="..."}`: Active traffic shed fraction.
- `cascadeshield_ebpf_events_per_second`: eBPF kernel event throughput.

#### Grafana Dashboard (`deploy/grafana/dashboard.json`)
Pre-built 5-panel dashboard showing topology node count, risk heatmap, edge latency percentiles, active shedding timeline, and eBPF event throughput.

#### Interactive CLI Terminal UI (`pkg/tui/`)
Built with `charmbracelet/bubbletea` and `lipgloss`:
- **Panel 1**: ASCII DAG Topology Renderer with health color-coding (Green $\to$ Yellow $\to$ Red).
- **Panel 2**: Monte Carlo Risk Score bar chart & cascade path prediction list.
- **Panel 3**: Single-line real-time system status bar (events/s, nodes, edges, sim latency, dry-run mode).

---

### 4.7 Phase 6: Chaos Arena (Synthetic Mesh & Validation)

#### Demo Microservice Topology (`hack/demo-cluster/`)
Comprises 6 Go HTTP microservices (`gateway`, `auth`, `orders`, `inventory`, `payments`, `analytics`):
- Each service has a `/health` endpoint for liveness probes.
- Each service features a dynamic `/chaos?latency_ms=N&error_rate=0.N` endpoint for on-the-fly failure injection without container restarts.

#### Chaos Engine & Scenarios (`pkg/chaos/`)
- `Injector`: Client calling microservice `/chaos` endpoints.
- `Scenario`: Formally defines the 4 arch-spec chaos scenarios (`PaymentSlowdown`, `InventoryCrash`, `AuthCascade`, `SlowBurn`).
- `Validator`: Compares `SimulationReport` and `EdgeState` outputs against expected chaos targets to verify system accuracy.

---

### 4.8 Kubernetes Production Deployment Design (DaemonSet Architecture)

> **Deployment Design Specification:** CascadeShield's Kubernetes production architecture is designed as a **node-level DaemonSet**. While local development runs CascadeShield directly as a host daemon binary, production Kubernetes clusters deploy one agent pod per physical/virtual node.

#### DaemonSet Pod Specifications (`hack/demo-cluster/kubernetes/cascadeshield-daemonset.yaml`)
To perform kernel-level eBPF connection probing and process-to-pod resolution across container boundaries, the production deployment design requires specific host privileges:

1. **`hostPID: true`**: Grants access to the host's `/proc` filesystem, allowing CascadeShield to inspect `/proc/PID/cgroup` for all container processes running on the host node.
2. **`hostNetwork: true`**: Binds the Prometheus metrics exporter directly to host network interfaces (`:9090`).
3. **Privileged Security Context & Capabilities**:
   - `securityContext.privileged: true` (or capabilities `CAP_BPF`, `CAP_PERFMON`, `CAP_SYS_ADMIN`, `CAP_NET_ADMIN`).
   - Required for loading C eBPF bytecode into kernel memory and attaching kprobes/tracepoints.
4. **Required Volume Mounts**:
   - `/sys/kernel/debug` mounted from host (`hostPath`) for debugfs access.
   - `/sys/fs/bpf` mounted from host (`hostPath`) for BPF filesystem map pinning across restarts.


---

## 5. Zero Hardcoding Policy Enforcement

CascadeShield enforces a **Strict Zero Hardcoding Policy**. Every tunable parameter is declared in a dedicated package `Config` struct with a `DefaultConfig()` constructor:

| Package | Config Struct Path | Key Configurable Parameters |
|---|---|---|
| `probe` | `pkg/probe/config.go` | RingBufferSize, PerfBufferPageCount |
| `graph` | `pkg/graph/config.go` | MaxNodes, MaxEdges, InactiveEdgeTimeout |
| `metrics` | `pkg/metrics/config.go` | WindowDuration, BucketCount, HistogramMaxVal |
| `simulator` | `pkg/simulator/config.go` | NumSimulations, TopCascadePathsCount, WorkerPoolSize |
| `shield` | `pkg/shield/config.go` | ShedThreshold, RecoveryThreshold, MaxShedPercentage, RampUpStep, RampDownStep, Armed |
| `tui` | `pkg/tui/config.go` | RefreshInterval, FallbackWidth, FallbackHeight |
| `chaos` | `pkg/chaos/config.go` | HTTPTimeout, ScenarioDuration, MinimumAccuracyRatio |

---

## 6. Key Data Structs Quick Reference

### 6.1 eBPF Event (`pkg/probe/event.go`)
```go
type Event struct {
    TimestampNs uint64
    PID         uint32
    SrcIP       net.IP
    DstIP       net.IP
    SrcPort     uint16
    DstPort     uint16
    EventType   EventType // ConnStart, ConnClose, Retransmit
}
```

### 6.2 Simulation Report (`pkg/simulator/report.go`)
```go
type SimulationReport struct {
    Timestamp      time.Time
    NodeCount      int
    EdgeCount      int
    NumSimulations int
    Duration       time.Duration
    NodeResults    map[string]*NodeSimResult
}
```

### 6.3 Edge Shedding State (`pkg/shield/recovery.go`)
```go
type EdgeState struct {
    Source       string
    Target       string
    CurrentShed  float64 // [0.0, 1.0]
    TargetShed   float64 // [0.0, 1.0]
    LowRiskCount int
    Active       bool
}
```

---

## 7. Verification & Quality Assurance Summary

CascadeShield's entire codebase is verified against strict production standards:
- **Unit Testing**: 100% of packages (`pkg/chaos`, `pkg/graph`, `pkg/metrics`, `pkg/resolver`, `pkg/shield`, `pkg/simulator`, `pkg/simulator/models`, `pkg/tui`) pass Go's data race detector (`go test -race ./pkg/...`).
- **Binary Build**: Main binary (`./bin/cascadeshield`) and all 6 demo microservices (`./bin/demo/*`) compile cleanly with 0 warnings.
- **End-to-End Validation**: Validated via live execution using `./hack/demo-cluster/demo-script.sh`.

---

## 8. Master Command Reference

```bash
# Build main CascadeShield agent binary
go build -o ./bin/cascadeshield ./cmd/cascadeshield

# Run complete unit test suite with race detector
go test -race ./pkg/...

# Build & launch all demo microservices in background
./hack/demo-cluster/start-all.sh

# Check microservice cluster health
./hack/demo-cluster/health-check.sh

# Run CascadeShield with eBPF probes and Terminal UI
sudo ./bin/cascadeshield --tui

# Run interactive Chaos Demo presentation script
./hack/demo-cluster/demo-script.sh

# Stop all demo microservices
./hack/demo-cluster/stop-all.sh
```
