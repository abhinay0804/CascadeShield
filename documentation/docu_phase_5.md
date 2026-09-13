# Phase 5 Technical Completion Document: Observability Engine

**Phase:** Phase 5 (Observability — Prometheus, Grafana, CLI TUI)  
**Status:** ✅ Completed  
**Completion Date:** 2026-09-13  
**Authors/Agents:** Gemini 3.6 Flash (High) & Claude Opus 4.6 (Thinking)

---

## 1. Executive Summary

Phase 5 delivers full operational observability for CascadeShield:
- **Extended Prometheus Metrics Endpoint**: Exposes edge latency percentiles, request rates, error rates, node risk scores, cascade failure probabilities, estimated time-to-failure (TTF), simulation execution latency, active load-shedding states, and eBPF event throughput.
- **Prometheus Alerting Rules**: Provides production-grade alerting definitions (`deploy/prometheus/rules.yaml`) for critical cascading failure risks (`> 0.85`), active load shedding, and eBPF event drops.
- **Grafana Dashboard**: Delivers a ready-to-import Grafana dashboard definition (`deploy/grafana/dashboard.json`) featuring DAG topology metrics, risk heatmaps, latency percentiles, shield action timelines, and eBPF throughput.
- **Interactive Terminal UI (TUI)**: Implements a 3-panel terminal dashboard using `charmbracelet/bubbletea` and `lipgloss` rendering DAG topology ASCII graphs, service risk progress bars, cascade failure path predictions, and system health status. Includes terminal size detection (< 80×24 fallback).
- **CLI Integration**: Introduces `--tui` and `--tui-refresh` flags to `cmd/cascadeshield/main.go`.

---

## 2. Files Created & Modified

### New Files Created
- `pkg/tui/config.go` — TUI configuration struct (`Config`) and `DefaultConfig()` method.
- `pkg/tui/app.go` — Main Bubbletea application model (`AppModel`) implementing `tea.Model`.
- `pkg/tui/topology.go` — ASCII graph renderer (`TopologyRenderer`) with health color coding.
- `pkg/tui/simulation.go` — Monte Carlo risk bar chart & cascade prediction renderer (`SimulationRenderer`).
- `pkg/tui/status.go` — Single-line system health status bar renderer (`StatusRenderer`).
- `pkg/tui/app_test.go` — Unit tests for TUI rendering, size validation, and model updates.
- `pkg/metrics/prometheus_test.go` — Unit tests for Phase 5 Prometheus scrape output and data race safety.
- `deploy/prometheus/rules.yaml` — Prometheus alerting rules file.
- `deploy/grafana/dashboard.json` — Pre-built Grafana dashboard JSON (schema v38).
- `documentation/docu_phase_5.md` — Technical completion document.
- `documentation/explanation/phase-5.md` — Beginner explanation guide.

### Existing Files Modified
- `pkg/simulator/report.go` — Added `Duration time.Duration` field to `SimulationReport`.
- `pkg/simulator/engine.go` — Set `report.Duration = time.Since(start)` in `RunSimulation()`.
- `pkg/probe/reader.go` — Added atomic `totalEvents` counter and `TotalEvents() uint64` method.
- `pkg/shield/recovery.go` — Added `sync.RWMutex` to `RecoveryManager` and thread-safe `ActiveSheddingStates()`.
- `pkg/shield/controller.go` — Exposed `ActiveSheddingStates()` method on `Controller`.
- `pkg/metrics/prometheus.go` — Extended `PrometheusServer` with Phase 5 metrics and reporter interfaces (`RiskReporter`, `ShieldReporter`, `EventReporter`).
- `cmd/cascadeshield/main.go` — Added `--tui` and `--tui-refresh` flags, wired Prometheus reporters, redirected slog logs in TUI mode.
- `documentation/task_tracker_v1.md` — Updated Phase 5 status to completed.

---

## 3. Architecture & Data Flow

```
                               ┌────────────────────────────────┐
                               │     eBPF Ring Buffer Reader    │
                               └───────────────┬────────────────┘
                                               │ (TotalEvents)
                                               ▼
┌─────────────────────────┐       ┌──────────────────────────┐       ┌────────────────────────┐
│   Monte Carlo Oracle    │       │     Prometheus Server    │       │   Shield Controller    │
│  (SimulationReport)     │       │    (:9090 /metrics)      │       │ (ActiveSheddingStates) │
└───────────┬─────────────┘       └────────────▲─────────────┘       └───────────┬────────────┘
            │                                  │                                 │
            │          ┌───────────────────────┴───────────────────────┐         │
            └─────────►│         Bubbletea CLI TUI App         │◄────────┘
                       │              (--tui)                  │
                       └───────────────────────────────────────┘
```

---

## 4. Key Metrics Exported

| Metric Name | Type | Labels | Description |
|-------------|------|--------|-------------|
| `cascadeshield_edge_request_rate` | Gauge | `source_service`, `target_service` | TCP connection rate (conn/s) |
| `cascadeshield_edge_latency_p99_ms` | Gauge | `source_service`, `target_service` | 99th percentile latency in ms |
| `cascadeshield_edge_error_rate` | Gauge | `source_service`, `target_service` | Fraction of connections with retransmits |
| `cascadeshield_dag_nodes_total` | Gauge | None | Number of service nodes in DAG |
| `cascadeshield_dag_edges_total` | Gauge | None | Number of active edges in DAG |
| `cascadeshield_node_risk_score` | Gauge | `service` | Monte Carlo risk score [0.0, 1.0] |
| `cascadeshield_node_cascade_probability` | Gauge | `service` | Cascade failure probability [0.0, 1.0] |
| `cascadeshield_node_ttf_seconds` | Gauge | `service` | Estimated time-to-failure in seconds |
| `cascadeshield_shield_active` | Gauge | `source`, `target` | 1 if load shedding active, 0 otherwise |
| `cascadeshield_shield_shed_percentage` | Gauge | `source`, `target` | Traffic shedding fraction [0.0, 1.0] |
| `cascadeshield_simulation_duration_ms` | Gauge | None | Simulation execution time in ms |
| `cascadeshield_ebpf_events_per_second` | Gauge | None | eBPF event processing rate |

---

## 5. Testing & Verification

```bash
# Unit tests across all packages with race detector enabled
$ go test -race -count=1 ./pkg/...
ok  	github.com/abhinay0804/cascadeshield/pkg/graph	1.206s
ok  	github.com/abhinay0804/cascadeshield/pkg/metrics	1.668s
ok  	github.com/abhinay0804/cascadeshield/pkg/resolver	1.104s
ok  	github.com/abhinay0804/cascadeshield/pkg/shield	1.230s
ok  	github.com/abhinay0804/cascadeshield/pkg/simulator	1.029s
ok  	github.com/abhinay0804/cascadeshield/pkg/simulator/models	1.010s
ok  	github.com/abhinay0804/cascadeshield/pkg/tui	1.030s

# Binary compilation
$ go build -o ./bin/cascadeshield ./cmd/cascadeshield
```

All 8 packages passed unit testing with **0 data races**.
