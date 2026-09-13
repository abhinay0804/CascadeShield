# CascadeShield — Project Brief

> **Read this first.** This document gives any agent the full context needed to work on this project.
> Last updated: 2026-09-09

---

## 1. WHAT Are We Building?

**CascadeShield** is an **eBPF-powered predictive cascading failure engine for microservices.**

In plain English: a system that hooks into the Linux kernel to watch how microservices talk to each other, builds a live dependency graph, then uses Monte Carlo simulations to **predict** cascading failures **before they happen** — and autonomously applies load-shedding to prevent them.

### The Three Layers

```
┌────────────────────────────────────────────────────────────┐
│  LAYER 1: KERNEL EYE                                       │
│  eBPF probes in the Linux kernel intercept every TCP        │
│  connect/accept/close/retransmit — zero config, zero code   │
│  changes to any application. Streams events to userspace.   │
├────────────────────────────────────────────────────────────┤
│  LAYER 2: THE ORACLE                                       │
│  Builds a real-time service dependency DAG from kernel       │
│  events. Annotates edges with latency percentiles, error     │
│  rates, throughput. Runs Monte Carlo simulations to predict  │
│  cascading failures: "If payments degrades by 200ms, orders  │
│  thread pool exhausts in 1.2s, gateway fails in 1.8s."      │
├────────────────────────────────────────────────────────────┤
│  LAYER 3: THE SHIELD                                       │
│  When cascade risk exceeds threshold, computes optimal       │
│  load-shedding across graph edges and autonomously applies   │
│  traffic control to isolate the blast radius.                │
└────────────────────────────────────────────────────────────┘
```

### One-Liner for Interviews

> *"I built a system that hooks into the Linux kernel via eBPF to automatically discover microservice dependencies in real-time, runs Monte Carlo simulations to predict cascading failures before they happen, and autonomously applies graph-aware load-shedding to prevent cluster-wide outages."*

---

## 2. WHY Are We Building This?

### The Problem

Cascading failures are the #1 cause of large-scale distributed system outages:
- **Meta (2021):** 6-hour global outage from a cascading BGP failure
- **AWS us-east-1:** Multiple cascading outages from downstream service degradation
- **Every microservice architecture:** A 200ms latency spike in one service can starve thread pools across 50 upstream services in seconds

### The Gap — What Exists vs. What Doesn't

| Exists (Corporate, $B+ tools) | Does NOT Exist |
|---|---|
| eBPF topology discovery (Pixie, Cilium Hubble) | **Predictive** cascade failure simulation |
| Service mesh visualization (Istio/Kiali) | **Autonomous** graph-aware load-shedding |
| Chaos engineering (Gremlin, LitmusChaos) | Monte Carlo failure modeling for microservices |
| Per-service circuit breakers (Hystrix) | **Coordinated** cross-graph remediation |

**Every existing tool answers:** "What IS happening right now?"
**CascadeShield answers:** "What WILL happen in 3 seconds if we don't act?"

### Why It Can't Be Vibe-Coded

1. **eBPF verifier** — statically analyzes kernel bytecode, rejects anything unsafe. AI tools hallucinate eBPF constantly.
2. **Failure models** — thread pool exhaustion, retry amplification, timeout propagation require genuine distributed systems understanding.
3. **Graph optimization** — computing optimal load-shedding across a dynamic DAG is combinatorics + control theory.

### Personal Motivation

The developer (Abhinay) is a B.Tech CS student targeting **SRE / Cloud / Platform Engineering** roles. This project fills every resume gap in one shot: Go, eBPF, Linux internals, Kubernetes, distributed systems, observability, and reliability engineering.

---

## 3. WHO Is Building This?

**Nama Abhinay**
- B.Tech CS @ VIT Chennai (2023–2027), CGPA: 8.69
- Strong in: Python, FastAPI, Docker, PostgreSQL, Redis, Celery, security (CEH certified)
- Learning through this project: **Go, eBPF, Linux kernel, Kubernetes, distributed systems, observability**
- This is his **first Go project** — agents should explain Go idioms when introducing new patterns
- OS: **EndeavourOS (Arch Linux)** — native Linux, modern kernel (6.x), pacman package manager

---

## 4. HOW Is It Built? (Architecture Summary)

### Tech Stack

| Layer | Technology | Why |
|---|---|---|
| Kernel probes | C (eBPF bytecode) | Only language the eBPF verifier accepts |
| eBPF loader | `cilium/ebpf` (Go) | Pure Go, no CGo, used by Cilium itself |
| Control plane | Go | Goroutines, strong K8s ecosystem, industry SRE standard |
| K8s integration | `client-go` | Official Kubernetes Go client |
| Metrics | Prometheus + Grafana | Industry standard SRE observability |
| Terminal UI | Bubbletea (Go) | Best TUI framework in Go |
| Local K8s | Kind | Lightweight local Kubernetes clusters |
| Build | Makefile + clang | Standard for eBPF C compilation |

### Data Flow (End-to-End)

```
Linux Kernel (tcp_v4_connect, inet_csk_accept, tcp_close, tcp_retransmit_skb)
    │
    ▼ [BPF Ring Buffer — zero-copy, per-CPU, lock-free]
    │
    ▼ pkg/probe — Go eBPF loader + ring buffer consumer
    │
    ▼ pkg/resolver — PID → /proc/cgroup → Container ID → K8s Pod → Service name
    │
    ▼ pkg/graph — Build/update directed dependency graph (DAG) with thread-safe mutations
    │
    ▼ pkg/metrics — Sliding window aggregation → HDR histograms → edge annotations (p50/p95/p99, error rate, req/s)
    │
    ▼ pkg/simulator — Monte Carlo cascade simulation (4 failure models × 1000 runs per node)
    │
    ▼ pkg/shield — Risk threshold check → compute optimal load-shedding → apply via TC/iptables (or dry-run log)
    │
    ▼ pkg/tui + pkg/metrics/prometheus — Terminal UI + Grafana dashboards
```

### 6 Implementation Phases

| Phase | Name | What | Agent | Status |
|---|---|---|---|---|
| 0 | Environment Setup | Install Go, clang, Kind, generate vmlinux.h, create Makefile | Claude Opus 4.6 | 🔴 Not Started |
| 1 | Kernel Eye | 4 eBPF C probes + Go loader + ring buffer consumer | Claude Opus 4.6 | 🔴 Not Started |
| 2 | Topology Engine | PID→Service resolver + DAG construction | Gemini 3.6 Pro | 🔴 Not Started |
| 3 | Metrics Engine | Sliding windows + HDR histograms + Prometheus exporter | Claude Sonnet 4.6 | 🔴 Not Started |
| 4 | The Oracle | Monte Carlo simulator + 4 failure models + risk scoring | Claude Opus 4.6 | 🔴 Not Started |
| 4b | The Shield | Autonomous load-shedding controller + dry-run safety | Claude Opus 4.6 | 🔴 Not Started |
| 5 | Observability | Prometheus metrics + Grafana dashboard + Bubbletea CLI TUI | Gemini 3.6 Pro | 🔴 Not Started |
| 6 | Chaos Arena | 5 demo microservices + chaos injection + validation scenarios | Claude Sonnet 4.6 | 🔴 Not Started |

### 4 Failure Models in The Oracle

1. **Thread Pool Exhaustion** — upstream latency increase → threads block → pool exhausts → all traffic fails
2. **Connection Pool Saturation** — similar to thread pool but for DB/downstream connections
3. **Timeout Chain Propagation** — nested timeouts across multi-hop paths cause sequential failures
4. **Retry Amplification** — retries on a failing service multiply load exponentially → death spiral

---

## 5. WHERE Is Everything?

### Project Structure

```
/mnt/shared/Projects/CascadeShield/          ← PROJECT ROOT (set this as workspace)
├── documentation/                            ← ALL docs live here
│   ├── PROJECT_BRIEF.md                      ← THIS FILE — read first
│   ├── arch_v1.md                            ← Full system architecture with diagrams
│   ├── task_tracker_v1.md                    ← Phase-by-phase TODO list with agent assignments
│   ├── docu_phase_0.md                       ← (created after Phase 0 completion)
│   ├── docu_phase_1.md                       ← (created after Phase 1 completion)
│   └── ...                                   ← One doc per completed phase
├── bpf/                                      ← eBPF C source code (kernel space)
│   ├── headers/vmlinux.h                     ← Auto-generated kernel types
│   ├── types.h                               ← Shared structs (C ↔ Go)
│   ├── tcp_connect.c
│   ├── tcp_accept.c
│   ├── tcp_close.c
│   └── tcp_retransmit.c
├── cmd/cascadeshield/main.go                 ← Entry point
├── pkg/                                      ← Go packages (user space)
│   ├── probe/                                ← eBPF loader + event reader
│   ├── resolver/                             ← PID → Service resolution
│   ├── graph/                                ← DAG data structure + builder
│   ├── metrics/                              ← Sliding windows + Prometheus
│   ├── simulator/models/                     ← 4 failure models + Monte Carlo engine
│   ├── shield/                               ← Autonomous remediation controller
│   ├── tui/                                  ← Bubbletea terminal UI
│   └── chaos/                                ← Chaos injection + validation
├── deploy/{kubernetes,grafana,prometheus}/    ← Deployment manifests
├── hack/demo-cluster/                        ← 5 demo microservices for testing
│   ├── {gateway,auth,orders,inventory,payments}/
│   ├── docker-compose.yaml
│   └── kubernetes/
├── Makefile
├── go.mod
└── README.md
```

### Antigravity Brain (Conversation Artifacts)

```
/home/abhi/.gemini/antigravity/brain/5f00291f-d36b-4d14-870c-81a7a38a42bb/
├── implementation_plan.md     ← Original architecture (same as documentation/arch_v1.md)
└── task.md                    ← Original task tracker (same as documentation/task_tracker_v1.md)
```

**Conversation ID:** `5f00291f-d36b-4d14-870c-81a7a38a42bb`
**Title:** CascadeShield Architecture & Planning

---

## 6. RULES For Every Agent

> [!IMPORTANT]
> These rules are non-negotiable. Every agent working on this project MUST follow them.

### Before Starting Any Phase

```
1. Read this file (PROJECT_BRIEF.md) for full context
2. Read arch_v1.md for detailed architecture
3. Read task_tracker_v1.md for your specific tasks
4. Read ALL existing docu_phase_*.md files from prior phases
5. Read existing source code in relevant pkg/ directories
6. Don't redefine types/interfaces that already exist
7. Follow existing code style and naming conventions
```

### After Completing Any Phase

```
1. Create: documentation/docu_phase_X.md containing:
   ├── Summary: What was built and why
   ├── Files Created/Modified: Full list with paths and purpose
   ├── Architecture Decisions: Why specific approaches were chosen
   ├── Data Flow: How data moves through the components
   ├── Key Interfaces: Public APIs/interfaces exposed
   ├── Dependencies: What this phase depends on / what depends on it
   ├── Testing: How to verify this phase works
   ├── Gotchas & Lessons: Non-obvious issues encountered
   └── Notes for Next Phase: What the next agent needs to know

2. Update task_tracker_v1.md:
   - Mark completed tasks as [x]
   - Add any new tasks discovered
   - Note deviations from original plan

3. Verify all tests/builds pass before marking phase complete
```

### Code Standards

```
- Language: Go (1.22+) for userspace, C for eBPF
- Error handling: Always handle errors explicitly, no silent swallows
- Concurrency: Use channels and context.Context for cancellation
- Logging: Use structured logging (slog or zerolog)
- Comments: Every exported function gets a doc comment
- Tests: Write tests for non-trivial logic, especially failure models
- Git: Commit after each logical unit of work with descriptive messages
```

### Developer Context

```
- Abhinay is learning Go for the first time through this project
- When introducing new Go patterns (goroutines, channels, interfaces, etc.),
  add inline comments explaining the pattern
- Prefer clarity over cleverness
- EndeavourOS (Arch) — use pacman for system packages
```

---

## 7. CURRENT STATE

**Status:** Architecture complete. Implementation not started.

**Next Step:** Phase 0 — Environment Setup (install Go, clang, Kind, create Makefile, generate vmlinux.h)

**Assigned Agent:** Claude Opus 4.6 (Thinking)

**To start, tell the agent:**
> *"Read `/mnt/shared/Projects/CascadeShield/documentation/PROJECT_BRIEF.md` and start Phase 0: Environment Setup. Follow the task tracker at `/mnt/shared/Projects/CascadeShield/documentation/task_tracker_v1.md`."*
