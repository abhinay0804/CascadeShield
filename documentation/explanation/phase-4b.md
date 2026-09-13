# Phase 4b: The Shield — A Beginner's Guide
## Autonomous Load-Shedding for Cascade Prevention (For Abhinay)

---

## Summary of Previous Phase

Phase 4 (The Oracle) runs Monte Carlo simulations to PREDICT cascading failures:
- "If payments gets slow, orders will crash in 0.2 seconds, and then gateway will crash."
- Risk score: 0.87 (CRITICAL)

**But predicting a fire isn't the same as putting it out.** That's Phase 4b's job.

---

## What Was Done

We built **The Shield** — a controller that automatically reduces traffic to prevent predicted cascading failures.

In plain English: **if The Oracle predicts a cascade, The Shield turns down the traffic tap before the pipe bursts.**

---

## Why It Was Done

### The Problem: Human Response Time Is Too Slow

Real cascading failures happen in **seconds**. The Oracle can predict them 30 seconds in advance. But by the time a human:
1. Reads the alert (30s)
2. Opens the dashboard (10s)
3. Understands the situation (60s)
4. Decides what to do (30s)
5. Executes the fix (30s)

...the entire cluster has been down for 2+ minutes. Revenue lost. Users angry.

### The Solution: Automated Traffic Reduction

The Shield reads The Oracle's predictions and automatically reduces traffic on the most critical edge — the one connection that, if shed, prevents the entire cascade.

**Analogy:** A dam with flood gates. When sensors detect rising water (Oracle risk score), the gates partially open (load shedding) to relieve pressure. The dam operator (you) can override at any time.

---

## The Safety Architecture

This is the most dangerous component in CascadeShield. It can DROP REAL TRAFFIC. So every safety feature was designed to prevent mistakes.

### 1. Dry-Run by Default 🔒

```bash
# Normal mode — logs what WOULD happen, changes NOTHING
sudo ./bin/cascadeshield

# Armed mode — ACTUALLY drops traffic (requires explicit flag)
sudo ./bin/cascadeshield --shield-armed
```

Without `--shield-armed`, every Shield action is logged but NOT executed:
```
🛡️  SHIELD [DRY-RUN] SHED orders → payments by 35%
```

### 2. Kill Switch 🚨

```bash
# Instantly remove ALL shedding rules, no questions asked
sudo ./bin/cascadeshield --kill-switch
```

This is the "oh no" button. It calls `RemoveAll()` on the actuator and exits immediately.

### 3. Rate Limits 🐢

The Shield never makes sudden changes:
- **Ramp UP:** Maximum 15% increase per 10-second interval
  - To go from 0% → 80% takes at least 60 seconds (6 intervals)
- **Ramp DOWN:** Maximum 10% decrease per interval
  - To go from 80% → 0% takes at least 80 seconds (8 intervals)

### 4. Minimum Traffic Floor 🌊

`MaxShedPercentage = 0.8` means **at least 20% of traffic always flows.** Even in the worst case, some users can still reach the service.

### 5. Recovery Guard ⏳

Shedding is only FULLY removed after risk stays LOW for 3 consecutive intervals (30 seconds). This prevents oscillation:
```
Without guard:
  Risk HIGH → add shedding → risk drops → remove shedding → load returns → risk HIGH → ...
  (infinite oscillation!)

With guard:
  Risk HIGH → add shedding → risk drops → wait 3 intervals → risk still LOW → remove shedding
  (stable!)
```

---

## How It Works — Step by Step

### Step 1: OBSERVE — Read The Oracle

```go
report := oracle.LatestReport()
// report.NodeResults["payments"].RiskScore = 0.85 (CRITICAL)
```

### Step 2: DECIDE — Compute Strategy

```
payments risk = 0.85, threshold = 0.7

Cascade path: [payments, orders, gateway]
Direction:     ← upstream ← (downstream failure propagates to callers)

Cut edge: orders → payments
  (orders is the caller being overwhelmed by slow payments)

Shed percentage = (0.85 - 0.7) / (1.0 - 0.7) = 50%
```

**Why orders→payments?** Because shedding there reduces the number of requests orders sends to payments. Fewer requests = lower load on payments = payments recovers = cascade prevented.

### Step 3: ACT — Apply via Actuator

The Shield tells the Actuator: "shed 50% of orders→payments traffic."

In dry-run mode, this just logs. In armed mode, the Actuator would apply an iptables rule or eBPF/TC filter to probabilistically drop packets.

### Step 4: RECOVER — Ramp Down When Safe

Once risk drops below 0.3 for 3 consecutive intervals:
```
Interval 1: shed 50% → risk drops to 0.25 → low_count=1
Interval 2: shed 40% → risk stays 0.20  → low_count=2  
Interval 3: shed 30% → risk stays 0.15  → low_count=3 ≥ threshold
→ FULL RECOVERY: remove shedding entirely
```

---

## The Control Loop Pattern

```
Every 10 seconds:
┌─────────────────────────────────────────┐
│ 1. READ oracle.LatestReport()           │  ← OBSERVE
│ 2. ComputeSheddingStrategy(report)      │  ← DECIDE
│ 3. RecoveryManager.ProcessDecisions()   │  ← RATE-LIMIT
│ 4. Actuator.Apply() or .Remove()        │  ← ACT
└─────────────────────────────────────────┘
```

This is the exact same pattern used by:
- Kubernetes controllers (reconciliation loop)
- Thermostats (read temperature → adjust heating)
- Cruise control (read speed → adjust throttle)

---

## Key Design Patterns Used

### Strategy Pattern (Actuator Interface)

```go
type Actuator interface {
    Apply(source, target string, shedPercent float64) error
    Remove(source, target string) error
    RemoveAll() error
}
```

The Controller doesn't know HOW traffic is shed. It just calls `Apply()`. You can swap:
- `LogActuator` (dry-run, logs only)
- Future: `TCActuator` (eBPF/TC kernel-level packet drop)
- Future: `IptablesActuator` (user-space packet filter)
- Future: `K8sActuator` (Kubernetes NetworkPolicy)

### State Machine (Recovery Manager)

```
NO_SHEDDING → RAMPING_UP → STEADY → RAMPING_DOWN → RECOVERY_GUARD → NO_SHEDDING
```

Each edge has its own state machine. The RecoveryManager tracks all of them independently.

---

## Configuration Reference

All tunables are in `Config` struct with `DefaultConfig()`:

| Parameter | Default | What it controls |
|---|---|---|
| ControlInterval | 10s | How often the loop runs |
| ShedThreshold | 0.7 | Risk score that triggers shedding |
| RecoveryThreshold | 0.3 | Risk score that starts recovery |
| ConsecutiveLowIntervals | 3 | Low-risk intervals before full removal |
| MaxShedPercentage | 0.8 | Maximum fraction of traffic to drop |
| RampUpStep | 0.15 | Max increase per interval |
| RampDownStep | 0.10 | Max decrease per interval |
| Armed | false | Whether to actually drop traffic |

---

## How It All Connects

```
Phase 1:  eBPF events
Phase 2:  DAG topology
Phase 3:  EdgeMetrics
Phase 4:  Oracle SimulationReport → RiskScore per node
Phase 4b: Shield reads report → computes shedding → applies via Actuator
              │
              └── Actuator (pluggable):
                    ├── LogActuator (dry-run, default)
                    ├── TCActuator (eBPF/TC, future)
                    └── IptablesActuator (iptables, future)
```

---

## Testing Summary

10 tests, all pass:
- Strategy correctly identifies cut edge and computes proportional shed %
- Recovery ramp-up respects 15%/interval limit
- Recovery guard prevents premature removal
- MaxShedPercentage caps at 80%
- Kill switch calls RemoveAll()
- Dry-run mode logs but doesn't execute
