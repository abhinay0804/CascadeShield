# Phase 6 Explanation: Chaos Arena — Demo Mesh & Validation

Welcome to Phase 6! If you've been following along, we've built the kernel probes (Phase 1), topology builder (Phase 2), metrics engine (Phase 3), prediction engine (Phase 4), autonomous traffic shield (Phase 4b), and observability suite (Phase 5).

Now comes the ultimate test: **proving that CascadeShield actually works under real chaos!**

---

## 1. High-Level Summary

In Phase 6, we created:
1. **A realistic microservice mesh**: 5 microservices written in Go (`gateway`, `auth`, `orders`, `inventory`, `payments`) that talk to each other over HTTP.
2. **A Chaos Control Endpoint (`/chaos`)**: Every service has a secret door that lets us inject latency or errors on the fly.
3. **The Chaos Engine (`pkg/chaos/`)**: A Go package that triggers experiments, runs scenarios, and checks if CascadeShield correctly predicted the failure.
4. **Deployment & Demo Scripts**: Docker Compose files, Kubernetes manifests, and an interactive presentation script (`demo-script.sh`).

---

## 2. Microservice Dependency Graph Explained

Think of our microservice mesh like a restaurant kitchen:

```
                    ┌─────────┐
       ─────────────│ Gateway │─────────────  (The Host / Front Desk)
      │             └────┬────┘             │
      ▼                  ▼                  ▼
┌──────────┐      ┌──────────┐      ┌──────────┐
│   Auth   │      │  Orders  │      │Analytics │  (Security Check & Waitstaff)
│  10ms    │      │  30ms    │      │  50ms    │
└──────────┘      └────┬─────┘      └──────────┘
                       │
              ┌────────┴────────┐
              ▼                 ▼
        ┌──────────┐     ┌──────────┐             (Storage Room & Payment Register)
        │Inventory │     │ Payments │
        │  20ms    │     │  100ms   │
        └──────────┘     └──────────┘
```

- **Gateway**: The front desk that receives client HTTP requests.
- **Auth**: Verifies security credentials (10ms). If Auth slows down, *every* customer gets stuck at the entrance!
- **Orders**: The main ordering workflow (30ms). To fulfill an order, it must check stock in **Inventory** and charge money in **Payments**.
- **Inventory**: Checks if items are in stock (20ms).
- **Payments**: Processes credit card charges (100ms).

---

## 3. The 4 Chaos Scenarios

### Scenario 1: Payment Slowdown
- **What happens**: We inject 300ms of extra delay into Payments.
- **Why it cascades**: Orders waits longer for Payments to reply. Goroutines inside Orders pile up waiting for TCP connections to complete. Orders runs out of capacity.
- **What CascadeShield does**: Predicts that Orders will exhaust its thread pool in ~1s, and automatically sheds 40% of traffic from `Orders -> Payments`.

### Scenario 2: Inventory Crash (Retry Amplification)
- **What happens**: Inventory starts failing 80% of its requests with HTTP 503.
- **Why it cascades**: Orders tries to be helpful by retrying failed requests 3 times. But sending 3x more requests to an already-dying Inventory causes a massive load spike!
- **What CascadeShield does**: Detects retry amplification and activates a circuit-breaker shedding rule on `Orders -> Inventory`.

### Scenario 3: Auth Cascade (Critical Path Failure)
- **What happens**: We inject 2000ms delay into Auth.
- **Why it cascades**: Because Auth is in the critical path of every Gateway request, the Gateway quickly runs out of available HTTP worker goroutines.
- **What CascadeShield does**: Identifies Auth as the bottleneck, sheds `Gateway -> Auth` traffic, and lets Gateway serve degraded fallback responses instead of freezing completely.

### Scenario 4: Slow Burn
- **What happens**: We slowly increase Payments latency by 10ms every minute.
- **Why it cascades**: It doesn't crash immediately, but slowly consumes connection pool margins.
- **What CascadeShield does**: Monte Carlo simulation detects the trajectory and triggers preemptive load shedding *before* total failure occurs.

---

## 4. Key Go Code Concepts

### `/chaos` Endpoint Pattern (`hack/demo-cluster/shared/service.go`)
Instead of restarting microservices with new environment variables during a live demo, each service maintains an in-memory `ChaosState`:

```go
type ChaosState struct {
    mu        sync.RWMutex
    LatencyMs int
    ErrorRate float64
}
```

Whenever a request comes in:
```go
extraLatency, isError := chaos.Apply()
if isError {
    http.Error(w, "synthetic error", http.StatusServiceUnavailable)
    return
}
time.Sleep(extraLatency)
```

This allows us to trigger chaos instantly with a simple `curl` command!

---

## 5. How to Run the Demo

```bash
# 1. Start the microservices locally with Docker Compose
docker compose -f hack/demo-cluster/docker-compose.yaml up -d

# 2. Run the interactive demo script
./hack/demo-cluster/demo-script.sh
```

Follow the prompts on screen to watch the baseline traffic, observe chaos injection, and see CascadeShield protect the cluster!
