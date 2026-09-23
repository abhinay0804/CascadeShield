![CI](https://github.com/abhinay0804/CascadeShield/actions/workflows/ci.yml/badge.svg)
<div align="center">

# 🛡️ CascadeShield

### *Autonomous eBPF Resilience Engine for Cloud-Native Microservices*

  <p align="center">
    <b>Predicts, prevents, and remediates cascading failures before cluster-wide outages occur.</b>
    <br />
    <a href="file:///mnt/shared/Projects/CascadeShield/documentation/COMPREHENSIVE_WHY_HOW_GUIDE.md"><strong>Explore the Why & How Guide »</strong></a>
    <br />
    <br />
    <a href="#-quick-start">Quick Start</a>
    ·
    <a href="#-architecture">Architecture</a>
    ·
    <a href="#-chaos-arena--scenario-walkthrough">Chaos Scenarios</a>
    ·
    <a href="#-terminal-ui--observability">Telemetry & TUI</a>
    ·
    <a href="file:///mnt/shared/Projects/CascadeShield/documentation/docu_phase_6.md">Docs</a>
  </p>

  [![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev)
  [![eBPF CO-RE](https://img.shields.io/badge/eBPF-CO--RE%20Linux%205.8+-orange?style=for-the-badge&logo=linux&logoColor=white)](https://ebpf.io)
  [![Architecture](https://img.shields.io/badge/Architecture-eBPF%20%2B%20Monte%20Carlo-purple?style=for-the-badge)](file:///mnt/shared/Projects/CascadeShield/documentation/COMPREHENSIVE_WHY_HOW_GUIDE.md)
  [![Race Detector](https://img.shields.io/badge/Race%20Detector-PASSED%20(0%20races)-brightgreen?style=for-the-badge)](file:///mnt/shared/Projects/CascadeShield/documentation/docu_phase_6.md)
  [![License](https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge)](LICENSE)

</div>

---

## 💡 Why CascadeShield?

Traditional observability tools (Prometheus alerts, Datadog, Grafana dashboards) are **reactive**. They alert SREs *after* CPU hits 95% or error rates spike. By the time a human receives a PagerDuty page, goroutines have piled up, connection pools have exhausted, and downstream cascading failures have already crashed the cluster.

**CascadeShield replaces post-mortem alerting with real-time predictive remediation.** By combining **kernel-level eBPF tracing** with **Monte Carlo probabilistic simulation**, CascadeShield detects non-linear degradation trajectories seconds in advance and executes precise min-cut traffic shedding to keep critical user flows alive.

### ⚔️ Traditional Alerting vs. CascadeShield

| Feature | Traditional Observability (Prometheus/Grafana) | CascadeShield |
|---|---|---|
| **Detection Speed** | Reactive (15s–60s scrape intervals) | **Real-Time Kernel Probing** (< 1s detection) |
| **Overhead** | Sidecar proxies add 2ms–5ms hop latency | **Zero-Proxy eBPF** (minimal kernel overhead via ring buffer zero-copy telemetry, 0ms hop latency) |
| **Failure Awareness** | Historical metric thresholds (CPU > 80%) | **Predictive Monte Carlo Simulation** (TTF prediction) |
| **Remediation** | Manual human intervention / auto-scaling | **Autonomous Min-Cut Traffic Shedding** |
| **Safety Guardrails** | All-or-nothing circuit breaking | **Gradual Thermostat Ramp-Up/Down Hysteresis** |

---

## 🏗️ Architecture & Pipeline Overview

CascadeShield runs as a lightweight Linux daemon with zero sidecar proxy overhead:

```text
 ┌────────────────────────────────────────────────────────────────────────┐
 │ 1. KERNEL EYE (C / eBPF Layer)                                         │
 │    kprobe: tcp_v4_connect  │  kretprobe: tcp_v4_connect               │
 │    tracepoint: tcp_retransmit_skb  │  kprobe: tcp_close                │
 └───────────────────────────────────┬────────────────────────────────────┘
                                     │ BPF Ring Buffer (FIFO, zero-copy)
                                     ▼
 ┌────────────────────────────────────────────────────────────────────────┐
 │ 2. TOPOLOGY ENGINE (Go Resolver & Dynamic DAG)                         │
 │    PID ──► /proc/cgroup ──► K8s Pod Informer ──► Service Node          │
 └───────────────────────────────────┬────────────────────────────────────┘
                                     │ DAGSnapshot (Thread-Safe Immutable Copy)
                                     ▼
 ┌────────────────────────────────────────────────────────────────────────┐
 │ 3. METRICS ENGINE (Sliding Windows & HDR Histograms)                   │
 │    Computes P50 / P95 / P99 Latency, Retransmit Error Rate, & Conn/s    │
 └───────────────────────────────────┬────────────────────────────────────┘
                                     │ Annotated Graph Metrics
                                     ▼
 ┌────────────────────────────────────────────────────────────────────────┐
 │ 4. THE ORACLE (Monte Carlo Simulator)                                  │
 │    1,000 Parallel Runs  │ 4 Non-Linear Failure Models (Thread Pool,  │
 │    Retry Amplification, GC Pauses, Slow Burn Trajectory)              │
 └───────────────────────────────────┬────────────────────────────────────┘
                                     │ SimulationReport (Node Risk Score & TTF)
                                     ▼
 ┌────────────────────────────────────────────────────────────────────────┐
 │ 4b. THE SHIELD (Autonomous Remediation Controller)                     │
 │     Min-Cut Path Shedding │ Thermostat Ramp-Up/Down Recovery          │
 │     Dry-Run Default Mode  │ Hardware Kill Switch                      │
 └───────────────────────────────────┬────────────────────────────────────┘
                                     │ Telemetry & Actuation
                                     ▼
 ┌────────────────────────────────────────────────────────────────────────┐
 │ 5 & 6. OBSERVABILITY & DEMO ARENA                                      │
 │        Prometheus Exporter (:9090) │ Grafana Dashboard JSON             │
 │        3-Panel Bubbletea CLI TUI   │ 6-Microservice Chaos Mesh         │
 └────────────────────────────────────────────────────────────────────────┘
```

---

## 🔥 Key Components & Capabilities

### 1. 👁️ Kernel Eye (`pkg/probe/`)
- Compiled via `bpf2go` into a single binary with **CO-RE (Compile Once – Run Everywhere)** kernel compatibility.
- Uses `BPF_MAP_TYPE_RINGBUF` for lockless, high-throughput kernel-to-user-space event streaming.
- Solves C vs. Go packed struct memory alignment (74 vs 80 bytes padding) via bitwise slice decoding.

### 2. 🗺️ Topology Engine (`pkg/resolver/` & `pkg/graph/`)
- Resolves Linux PIDs to container IDs via `/proc/PID/cgroup` and maps IP/ports to Kubernetes Services using `client-go`.
- Dynamically constructs and updates a Directed Acyclic Graph (DAG) representing live cluster dependencies.

### 3. 📊 Metrics Engine (`pkg/metrics/`)
- Maintains sub-millisecond precision sliding-window HDR histograms.
- Computes real-time P50, P95, and P99 latency percentiles alongside TCP retransmit error rates per edge.

### 4. 🔮 The Oracle (`pkg/simulator/`)
- Executes 1,000 parallel Monte Carlo simulations per cycle.
- Models non-linear failure dynamics:
  - **Goroutine / Thread Pool Exhaustion** (Little's Law saturation)
  - **Naïve Retry Amplification** (3.4x load multiplier on failing services)
  - **GC Stop-The-World Pauses** (Memory pressure cascades > 85%)
  - **Slow-Burn Trajectories** (Preemptive exhaustion prediction)

### 5. 🛡️ The Shield (`pkg/shield/`)
- Autonomous control loop: `Observe → Decide → Act → Recover`.
- Executes min-cut edge selection to shed traffic on degraded target paths while keeping non-degraded services operating.
- **Safety Hysteresis**: Thermostat model limits ramp-up increases to max $15\%$/interval and requires $3$ consecutive low-risk intervals ($30\text{s}$) before releasing shedding.
- **Dry-Run Default**: `Armed=false` by default to ensure safe evaluation before turning on live traffic dropping.

### 6. 🖥️ Observability & TUI (`pkg/tui/` & `pkg/metrics/`)
- Exposes native Prometheus metrics on `:9090/metrics`.
- Ships with production-ready Grafana Dashboard JSON (`deploy/grafana/dashboard.json`) and Alerting Rules (`deploy/prometheus/rules.yaml`).
- Features a **3-Panel Terminal UI** (`charmbracelet/bubbletea`) rendering ASCII DAG graphs, service risk progress bars, and execution health.

### 7. ☸️ Kubernetes Production Deployment Design (DaemonSet Architecture)
> **Note on Deployment Design:** CascadeShield's production deployment model for Kubernetes is designed as a **node-level DaemonSet** (`hack/demo-cluster/kubernetes/cascadeshield-daemonset.yaml`). While local development runs CascadeShield directly as a host binary with `sudo`, production Kubernetes clusters deploy one agent pod per physical/virtual node.
- **`hostPID: true`**: Allows inspecting `/proc/PID/cgroup` across container runtimes on the node to map sockets to K8s Pods.
- **`hostNetwork: true`**: Exposes Prometheus metrics on the node's host network (`:9090`).
- **Privileged eBPF Capabilities**: Utilizes `CAP_BPF`, `CAP_PERFMON`, `CAP_SYS_ADMIN`, and `CAP_NET_ADMIN` to attach eBPF kprobes to kernel TCP sockets.
- **Host Volume Mounts**: Mounts `/sys/kernel/debug` (debugfs) and `/sys/fs/bpf` (BPF filesystem) from the host.

---

## ⚡ Quick Start Guide

### Prerequisites
- **Linux Kernel 5.8+** (for eBPF ring buffer support)
- **Go 1.21+**
- **clang / llvm** (for eBPF C compilation)

### 1. Build Agent & Microservice Mesh
```bash
# Clone the repository
git clone git@github.com:abhinay0804/CascadeShield.git
cd CascadeShield

# Build main CascadeShield agent binary
go build -o ./bin/cascadeshield ./cmd/cascadeshield

# Start all 6 synthetic demo microservices in background
./hack/demo-cluster/start-all.sh
```

### 2. Verify Cluster Health
```bash
./hack/demo-cluster/health-check.sh
```
*Output:*
```text
Checking health across all CascadeShield microservices...

  [ONLINE] Gateway (http://localhost:8080/health) -> {"service":"gateway","status":"ok"}
  [ONLINE] Auth (http://localhost:8081/health) -> {"service":"auth","status":"ok"}
  [ONLINE] Orders (http://localhost:8082/health) -> {"service":"orders","status":"ok"}
  [ONLINE] Inventory (http://localhost:8083/health) -> {"service":"inventory","status":"ok"}
  [ONLINE] Payments (http://localhost:8084/health) -> {"service":"payments","status":"ok"}
  [ONLINE] Analytics (http://localhost:8085/health) -> {"service":"analytics","status":"ok"}

✓ All 6 microservices are HEALTHY and ready!
```

### 3. Launch Agent with Terminal UI
In a separate terminal window:
```bash
sudo ./bin/cascadeshield --tui
```

### 4. Run the Chaos Experiment Suite
In your main terminal window:
```bash
./hack/demo-cluster/demo-script.sh
```

---

## 🧪 Chaos Arena & Scenario Walkthrough

The demo cluster contains 6 synthetic microservices:

```text
                    ┌─────────┐
       ─────────────│ Gateway │─────────────  (:8080)
      │             └────┬────┘             │
      ▼                  ▼                  ▼
┌──────────┐      ┌──────────┐      ┌──────────┐
│   Auth   │      │  Orders  │      │Analytics │  (:8081, :8082, :8085)
│  10ms    │      │  30ms    │      │  50ms    │
└──────────┘      └────┬─────┘      └──────────┘
                       │
              ┌────────┴────────┐
              ▼                 ▼
        ┌──────────┐     ┌──────────┐
        │Inventory │     │ Payments │             (:8083, :8084)
        │  20ms    │     │  100ms   │
        └──────────┘     └──────────┘
```

### Validated Chaos Scenarios

| # | Scenario | Injected Chaos Command | Physical Mechanism | Expected Oracle Prediction | Shield Remediation Action |
|---|---|---|---|---|---|
| **1** | **Payment Slowdown** | `curl "localhost:8084/chaos?latency_ms=300"` | Latency +300ms causes goroutines in `Orders` to hold sockets 10x longer. | `Orders` thread pool exhaustion predicted in ~1.2s | Sheds `Orders → Payments` traffic up to 40% |
| **2** | **Inventory Crash** | `curl "localhost:8083/chaos?error_rate=0.80"` | 80% error rate causes `Orders` retries to multiply load 3.4x on `Inventory`. | Retry amplification load spike detected | Activates circuit breaker shedding on `Orders → Inventory` |
| **3** | **Auth Cascade** | `curl "localhost:8081/chaos?latency_ms=2000"` | 2000ms delay in Auth stalls every incoming Gateway API request. | Critical path block identified | Sheds `Gateway → Auth`, allowing Gateway to serve degraded fallbacks |
| **4** | **Slow Burn** | Stepwise +10ms latency increments | Gradual capacity erosion below standard static alert thresholds. | Preemptive threshold breach prediction | Preemptive shedding triggered at predicted $T-30\text{s}$ |

---

## ⚙️ Configuration & Zero Hardcoding Policy

CascadeShield strictly enforces a **Zero Hardcoding Policy**. Every tunable parameter is configurable via clean Go `Config` structs with safe production defaults:

```go
// Example: The Shield Safety Config (pkg/shield/config.go)
type Config struct {
    ControlInterval         time.Duration // Default: 10s
    ShedThreshold           float64       // Default: 0.70 risk score
    RecoveryThreshold       float64       // Default: 0.30 risk score
    ConsecutiveLowIntervals int           // Default: 3 intervals (30s hysteresis)
    MaxShedPercentage       float64       // Default: 0.80 (20% traffic floor)
    RampUpStep              float64       // Default: 0.15 (max 15% increase/step)
    RampDownStep            float64       // Default: 0.10 (max 10% decrease/step)
    Armed                   bool          // Default: false (DRY-RUN mode)
}
```

---

## 📁 Repository Structure

```text
CascadeShield/
├── bpf/                        # eBPF C kernel probes (kprobes, tracepoints, ring buffer)
├── cmd/cascadeshield/          # Main CLI binary entry point & execution loop
├── deploy/                     # Prometheus rules, Grafana JSON, Kubernetes manifests
│   ├── grafana/dashboard.json
│   ├── prometheus/rules.yaml
│   └── kubernetes/
├── documentation/              # Technical docs, architecture guides, & phase breakdowns
│   ├── COMPREHENSIVE_WHY_HOW_GUIDE.md
│   ├── docu_phase_0.md ... docu_phase_6.md
│   └── explanation/
├── hack/demo-cluster/          # 6 Go HTTP demo microservices & script automation
│   ├── start-all.sh
│   ├── stop-all.sh
│   ├── health-check.sh
│   └── demo-script.sh
└── pkg/                        # Modular Go packages
    ├── chaos/                  # Chaos injector, scenario definitions, & validator
    ├── graph/                  # Dynamic DAG Builder, Node/Edge models, Snapshot
    ├── metrics/                # Sliding window HDR histograms & Prometheus exporter
    ├── probe/                  # bpf2go loader, event decoder, ring buffer reader
    ├── resolver/               # PID -> cgroup -> Pod/K8s service resolver
    ├── shield/                 # Autonomous remediation controller & recovery manager
    ├── simulator/              # Monte Carlo Oracle engine & 4 failure models
    └── tui/                    # 3-Panel Bubbletea Terminal UI dashboard
```

---

## 📊 Verification & Tests

Run the full automated unit test suite across all packages with Go's race detector enabled:

```bash
$ go test -race ./pkg/...
ok  	github.com/abhinay0804/cascadeshield/pkg/chaos	1.037s
ok  	github.com/abhinay0804/cascadeshield/pkg/graph	1.210s
ok  	github.com/abhinay0804/cascadeshield/pkg/metrics	1.677s
ok  	github.com/abhinay0804/cascadeshield/pkg/resolver	1.115s
ok  	github.com/abhinay0804/cascadeshield/pkg/shield	1.232s
ok  	github.com/abhinay0804/cascadeshield/pkg/simulator	1.035s
ok  	github.com/abhinay0804/cascadeshield/pkg/simulator/models	1.012s
ok  	github.com/abhinay0804/cascadeshield/pkg/tui	1.040s
```

---

## 📖 Deep-Dive Documentation

For complete mathematical breakdowns, eBPF C code walkthroughs, and detailed system design explanations, see:
- 📖 [**Complete Architecture & System Design Guide**](file:///mnt/shared/Projects/CascadeShield/documentation/COMPREHENSIVE_WHY_HOW_GUIDE.md)
- 📋 [**Master Task Tracker & Phase Logs**](file:///mnt/shared/Projects/CascadeShield/documentation/task_tracker_v1.md)

---

## 📜 License

Distributed under the **MIT License**. See [`LICENSE`](LICENSE) for full details.
