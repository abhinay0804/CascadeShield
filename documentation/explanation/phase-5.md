# Phase 5 Explained: Observability — Prometheus, Grafana & Terminal UI

Welcome to **Phase 5** of CascadeShield! If you've been following along, Phase 1 captured kernel events, Phase 2 built the live service dependency graph, Phase 3 measured traffic statistical percentiles, Phase 4 predicted cascading failures, and Phase 4b built autonomous traffic-shedding defenses.

In Phase 5, we give CascadeShield its **cockpit instrument panel**.

---

## 1. High-Level Overview: Why Observability Matters

Imagine driving a high-performance sports car with no speedometer, no engine temperature gauge, and no fuel indicator. Even if the engine works perfectly, you have no idea how fast you're moving or when something is about to overheat.

In microservice resilience:
- **Prometheus** acts as the automated data recorder continuously taking snapshots.
- **Grafana** is the wall-mounted flight dashboard showing trends, graphs, and heatmaps.
- **Terminal UI (TUI)** is the real-time interactive terminal screen you see directly in your SSH session.

---

## 2. Core Concepts & Analogies

### A. Prometheus Metrics: Scrape / Pull Model
Instead of having CascadeShield send thousands of HTTP requests out to external log servers, Prometheus uses a **pull model**:
1. CascadeShield exposes a plain web server on `:9090/metrics`.
2. Every 15–30 seconds, Prometheus sends an HTTP GET request to `:9090/metrics`.
3. CascadeShield formats all current DAG metrics, risk scores, and active shedding rules as plain key-value text.
4. Prometheus stores the data points in a time-series database.

### B. Bubbletea & Elm Architecture (Model-Update-View)
To build the interactive terminal UI (`--tui`), we used `charmbracelet/bubbletea`, which follows the **Elm Architecture**:
1. **Model (`AppModel`)**: Holds all current application state in memory (node counts, risk scores, active shedding edges, terminal window size).
2. **Update (`Update()`)**: Receives events/messages (keypresses, window resizes, timer ticks) and returns an updated model plus optional commands.
3. **View (`View()`)**: A pure function that takes the current state and returns a formatted ASCII text string to draw on the screen.

```
       ┌────────────────────────┐
       │     User Keypress /    │
       │    Periodic Ticks      │
       └───────────┬────────────┘
                   │
                   ▼
       ┌────────────────────────┐
       │      Update(msg)       │ ──► Modifies AppModel State
       └───────────┬────────────┘
                   │
                   ▼
       ┌────────────────────────┐
       │        View()          │ ──► Draws ASCII Layout to Screen
       └────────────────────────┘
```

---

## 3. What Was Built in Phase 5

1. **Extended Prometheus Exporter (`pkg/metrics/prometheus.go`)**:
   - `node_risk_score`: Monte Carlo risk score per service (`0.0` = healthy, `1.0` = imminent cascade).
   - `node_cascade_probability`: Likelihood of triggering downstream failure chains.
   - `node_ttf_seconds`: Time-to-failure estimate in seconds.
   - `shield_active`: `1` if traffic shedding is active on edge, `0` otherwise.
   - `shield_shed_percentage`: Fraction of traffic dropped (`0.0–1.0`).
   - `simulation_duration_ms`: Duration of Monte Carlo simulation runs.
   - `ebpf_events_per_second`: Event reader throughput.

2. **Alerting Rules (`deploy/prometheus/rules.yaml`)**:
   - Fires critical alerts when risk exceeds `0.85` for more than 30 seconds.
   - Fires warning alerts whenever Shield activates shedding rules.

3. **Grafana Dashboard (`deploy/grafana/dashboard.json`)**:
   - Ready-to-import JSON dashboard with 8 panels including DAG node graph, risk bar charts, edge latency percentiles (P99), load-shedding timelines, and eBPF throughput.

4. **Terminal UI (`pkg/tui/`)**:
   - **Topology Panel**: Renders service connections as ASCII arrows (`gateway ──▶ orders ──▶ payments`) with health color coding (Green/Yellow/Red).
   - **Risk Panel**: Renders service risk scores as progress bars (`██████░ 0.87`).
   - **Cascade Prediction Panel**: Displays predicted failure chains, time-to-failure timelines, and Shield interventions.
   - **Status Bar**: Live single-line status showing event rate, node count, edge count, simulation latency, shield mode (ARMED vs DRY-RUN), and uptime.

---

## 4. How to Run Phase 5

### Running Headless Daemon Mode (Default)
```bash
$ ./bin/cascadeshield
```

### Running Interactive Terminal UI Mode
```bash
$ ./bin/cascadeshield --tui
```

### Querying Prometheus Metrics Endpoint
```bash
$ curl http://localhost:9090/metrics
```
