# 🛡️ CascadeShield

> **Predictive & Autonomous Cascading Failure Remediation Engine for Cloud-Native Microservices**

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev)
[![eBPF CO-RE](https://img.shields.io/badge/eBPF-CO--RE%20Linux%205.8+-orange?style=flat&logo=linux)](https://ebpf.io)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Tests](https://img.shields.io/badge/Tests-Passing%20(-race)-brightgreen)](file:///mnt/shared/Projects/CascadeShield/documentation/docu_phase_6.md)

---

## 🌟 Overview

**CascadeShield** is an autonomous, eBPF-powered resilience engine designed to predict, prevent, and remediate cascading failures in cloud-native microservices *before* they cause total system outages.

Traditional observability tools (Prometheus, Datadog) are **reactive** — they fire alerts after CPU hits 90% or error rates spike. By the time an engineer responds, connection pools across downstream dependencies have already exhausted, goroutines have piled up, and the entire cluster has crashed.

CascadeShield pairs **kernel-level eBPF connection tracing** with a real-time **Monte Carlo simulation engine ("The Oracle")** and an **autonomous load-shedding controller ("The Shield")** to predict failures seconds in advance and execute precise, min-cut traffic shedding.

---

## 🏗️ Architecture

```
 ┌─────────────────┐
 │ eBPF Probes     │  (C kprobes & tracepoints capturing TCP events in Linux Kernel)
 └────────┬────────┘
          │ (Ring Buffer Event Channel)
          ▼
 ┌─────────────────┐
 │ Resolver & DAG  │  (PID/cgroup/K8s service resolution & dynamic Directed Acyclic Graph)
 └────────┬────────┘
          │ (DAGSnapshot)
          ▼
 ┌─────────────────┐
 │ Metrics Engine  │  (Sliding window HDR histograms: P50/P95/P99 latency, retransmits)
 └────────┬────────┘
          │ (Annotated Graph Metrics)
          ▼
 ┌─────────────────┐
 │ The Oracle      │  (1,000 parallel Monte Carlo runs across 4 non-linear failure models)
 └────────┬────────┘
          │ (SimulationReport: Risk Scores & Time-to-Failure)
          ▼
 ┌─────────────────┐
 │ The Shield      │  (Min-cut traffic shedding, dry-run safety, ramp-up/down thermostat)
 └────────┬────────┘
          │ (Telemetry & Actuation)
          ▼
 ┌─────────────────┐
 │ Observability   │  (Prometheus metrics exporter, Grafana dashboard, 3-panel Bubbletea TUI)
 └─────────────────┘
```

---

## ⚡ Key Features

- **Kernel Eye (eBPF)**: High-speed TCP tracing (`connect`, `accept`, `close`, `retransmit`) at near-zero overhead (< 0.5% CPU) using Linux ring buffers.
- **Topology Engine**: Dynamic PID-to-cgroup/Kubernetes service mapping building an active Directed Acyclic Graph (DAG) without sidecar proxies.
- **Metrics Engine**: High-precision sliding window HDR histograms computing P50/P95/P99 latencies, retransmit error rates, and connection rates.
- **The Oracle**: 1,000 parallel Monte Carlo simulations modeling **Thread Exhaustion**, **Retry Amplification**, **GC Pauses**, and **Slow Burn** degradation trajectories.
- **The Shield**: Autonomous remediation controller executing min-cut edge load-shedding with a gradual thermostat ramp-up/ramp-down recovery state machine (`Armed=false` dry-run mode by default).
- **Full Observability**: Prometheus metrics exporter (`:9090`), pre-built Grafana dashboard (`deploy/grafana/dashboard.json`), and interactive 3-panel Terminal UI (`charmbracelet/bubbletea`).
- **Chaos Arena**: Synthetic 6-microservice mesh (`gateway`, `auth`, `orders`, `inventory`, `payments`, `analytics`) with dynamic `/chaos` HTTP injection endpoints.

---

## 🚀 Quick Start

### Prerequisites
- Linux Kernel 5.8+ (eBPF ring buffer support)
- Go 1.21+
- `clang` & `llvm` (for eBPF C compilation)

### 1. Build Agent & Demo Cluster
```bash
# Clone repository
git clone https://github.com/abhinay0804/cascadeshield.git
cd cascadeshield

# Build main binary
go build -o ./bin/cascadeshield ./cmd/cascadeshield

# Start all 6 demo microservices in background
./hack/demo-cluster/start-all.sh
```

### 2. Verify Microservice Health
```bash
./hack/demo-cluster/health-check.sh
```

### 3. Launch CascadeShield Agent & TUI
```bash
sudo ./bin/cascadeshield --tui
```

### 4. Run Interactive Chaos Demo
```bash
./hack/demo-cluster/demo-script.sh
```

---

## 🧪 Chaos Validation Scenarios

| # | Scenario | Injected Chaos | Expected Oracle Prediction | Shield Remediation |
|---|---|---|---|---|
| 1 | **Payment Slowdown** | `payments` latency += 300ms | `orders` thread pool exhaustion in ~1s | Shed `orders→payments` traffic up to 40% |
| 2 | **Inventory Crash** | `inventory` returns 503 at 80% rate | `orders` 3x retry amplification on inventory | Shed `orders→inventory`, activate circuit breaker |
| 3 | **Auth Cascade** | `auth` latency += 2000ms | Critical path block; all gateway calls delayed | Shed `gateway→auth`, serve degraded responses |
| 4 | **Slow Burn** | `payments` latency increases 10ms/step | Preemptive threshold breach prediction | Preemptive shedding at predicted T-30s |

---

## 📁 Repository Structure

```text
├── bpf/                        # C eBPF kernel probes (kprobes, tracepoints, ring buffer)
├── cmd/cascadeshield/          # Main CLI entry point & control loop
├── deploy/                     # Prometheus rules, Grafana dashboard JSON, K8s manifests
├── documentation/              # Technical phase completion docs, ADRs, & guides
│   └── COMPREHENSIVE_WHY_HOW_GUIDE.md
├── hack/demo-cluster/          # 6 Go HTTP demo microservices, scripts, Docker Compose
│   ├── start-all.sh
│   ├── stop-all.sh
│   └── health-check.sh
└── pkg/                        # Core Go packages
    ├── chaos/                  # Chaos injector, scenario definitions, & validator
    ├── graph/                  # DAG Builder, Node/Edge models, DAGSnapshot
    ├── metrics/                # Sliding window HDR histograms & Prometheus exporter
    ├── probe/                  # bpf2go loader, event decoder, ring buffer reader
    ├── resolver/               # PID -> cgroup -> Pod/K8s service resolver
    ├── shield/                 # Autonomous remediation controller & recovery manager
    ├── simulator/              # Monte Carlo Oracle engine & failure models
    └── tui/                    # 3-Panel Bubbletea Terminal UI dashboard
```

---

## ⚙️ Zero Hardcoding Policy

CascadeShield strictly enforces a **Zero Hardcoding Policy**. Every tunable parameter (thresholds, timeouts, rate limits, buffer sizes, intervals) is managed via `Config` structs and `DefaultConfig()` constructors:
- `probe.DefaultConfig()`
- `graph.DefaultConfig()`
- `metrics.DefaultConfig()`
- `simulator.DefaultConfig()`
- `shield.DefaultConfig()`
- `tui.DefaultConfig()`
- `chaos.DefaultConfig()`

---

## 🧪 Testing

Run the complete test suite across all packages with race detector enabled:
```bash
go test -race ./pkg/...
```

---

## 📜 License

Distributed under the MIT License. See [`LICENSE`](LICENSE) for details.
