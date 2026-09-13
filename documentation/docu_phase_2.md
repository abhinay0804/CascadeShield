# Phase 2 Technical Completion Document: Topology Engine

**Phase:** Phase 2 (Topology Engine — Service Resolution & DAG Construction)  
**Status:** Completed  
**Implementing Agent:** Gemini 3.6 Flash (High)  
**Date:** September 9, 2026  

---

## 1. Executive Summary

Phase 2 builds the **Topology Engine** for CascadeShield. It transforms raw, low-level Linux kernel socket events (PIDs, IPv4 addresses, host ports) captured by Phase 1 eBPF probes into a **live, thread-safe Service Dependency DAG (Directed Acyclic Graph)**.

The engine provides dual-mode resolution:
1. **Bare-Metal Mode**: Resolves process IDs (PIDs) via `/proc/<pid>/cgroup` and `/proc/<pid>/comm` to host process names (e.g., `nginx`, `curl`, `chrome`) and IP:port strings.
2. **Kubernetes Mode**: Uses `client-go` **SharedInformers** to watch K8s Pods and Services in background watch streams, building O(1) in-memory lookups mapping container IDs and pod IPs to logical Kubernetes Service names.

---

## 2. Files Created & Modified

### `pkg/resolver/`
- **`config.go`**: Defines `Config` struct and `DefaultConfig()` enforcing zero hardcoding for cache sizes, TTLs, and `/proc` paths.
- **`types.go`**: Defines `ResolvedEvent` and `ServiceIdentity` cross-package type contracts.
- **`cache.go`**: Generic thread-safe LRU cache (`Cache[K comparable, V any]`) using Go 1.18+ generics, doubly-linked lists (`container/list`), TTL expiration, and `sync.Mutex`.
- **`proc.go`**: `ProcReader` parsing cgroup v1, cgroup v2 (Docker, containerd, CRI-O formats), and process `comm` files.
- **`kubernetes.go`**: `KubeResolver` integrating `k8s.io/client-go` SharedInformers for Pod/Service lookups with fallback chains (Service selector -> OwnerReference -> Pod prefix).
- **`resolver.go`**: Main orchestrator combining `ProcReader`, `KubeResolver`, and LRU caches into a fast `Resolve(evt probe.Event) ResolvedEvent` method.
- **`cache_test.go`**: Unit tests for LRU eviction, TTL expiration, and race safety.
- **`proc_test.go`**: Table-driven tests verifying cgroup v1/v2 regex parsing for Docker, containerd, and CRI-O.

### `pkg/graph/`
- **`config.go`**: Graph configuration struct with stale edge timeouts, prune intervals, and node capacity limits.
- **`node.go`**: Defines `Node`, `PodInfo`, and `ResourceLimits` structs representing microservices and K8s replicas.
- **`edge.go`**: Defines `Edge` and `EdgeMetrics` structs representing directed TCP traffic links with cumulative connection/byte/retransmit counters.
- **`dag.go`**: Thread-safe `RWMutex`-protected DAG supporting `EnsureNode`, `RecordConnect`, `RecordClose`, `RecordRetransmit`, and `PruneStaleEdges`.
- **`builder.go`**: `Builder` worker goroutine consuming `ResolvedEvent` channels to continuously mutate the DAG and log topology summaries.
- **`snapshot.go`**: `DAGSnapshot` creating point-in-time deep copies for Phase 4 simulation and Phase 6 API rendering without lock contention.
- **`dag_test.go`**, **`builder_test.go`**, **`snapshot_test.go`**: Unit tests covering DAG mutations, builder worker loops, deep-copy isolation, and zero-race safety.

### `cmd/cascadeshield/`
- **`main.go`**: Wired end-to-end pipeline: `eBPF Probes -> Ring Buffer Reader -> Identity Resolver -> Graph Builder -> Live DAG`.

### `hack/test-topology/`
- **`manifests.yaml`**: Kubernetes manifests for 3 inter-communicating services (`frontend` -> `orders` -> `payments`) for live K8s DAG verification.

---

## 3. Architecture & Design Decisions

### 3.1 Two-Sided Resolution Strategy
- **`CONNECT` events**: Local side = Source (PID), Remote side = Target (Destination IPv4:Port).
- **`ACCEPT` events**: Local side = Target (PID), Remote side = Source (Source IPv4:Port).
- **`CLOSE` & `RETRANSMIT` events**: Mutate existing edges based on matching endpoints.

### 3.2 Kubernetes SharedInformer Pattern
Instead of executing direct API server polling (which causes API server throttling under high event rates), `KubeResolver` uses watch-based `SharedInformerFactory`. Local cache lookups run in O(1) time.

### 3.3 Thread-Safe Centralized DAG Mutations
To eliminate data races between the `Builder` goroutine writing traffic metrics and external readers (simulators/APIs), all edge mutations (`RecordConnect`, `RecordClose`, `RecordRetransmit`) occur inside `DAG.mu.Lock()`. Readers obtain immutable `DAGSnapshot` deep copies or read under `DAG.mu.RLock()`.

---

## 4. Verification & Test Results

```bash
# Unit & Race Tests
go test -v -race ./pkg/resolver/... ./pkg/graph/...
```
**Result:** PASS (0 data races detected, 100% test pass rate across all resolver and graph tests).

```bash
# Compilation & Build
go build -o ./bin/cascadeshield ./cmd/cascadeshield
```
**Result:** Executable `./bin/cascadeshield` built successfully (45MB binary).

---

## 5. Next Steps for Phase 3
- Pass `DAG` reference to Phase 3 (Metrics Pipeline).
- Implement sliding-window metrics calculators (request rate, latency percentiles P50/P95/P99, error rate, retransmit rate) on each `Edge`.
