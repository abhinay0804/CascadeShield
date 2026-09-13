# Phase 4b Technical Completion Document: The Shield — Autonomous Remediation

**Phase:** Phase 4b (The Shield — Autonomous Load-Shedding)
**Status:** Completed
**Implementing Agent:** Claude Opus 4.6 (Thinking)
**Date:** September 13, 2026

---

## 1. Executive Summary

Phase 4b implements **The Shield** — an autonomous load-shedding controller that reads cascade risk predictions from The Oracle (Phase 4) and applies proportional traffic shedding on the most impactful edges to prevent predicted cascading failures.

**⚠️ DRY-RUN BY DEFAULT.** Real traffic manipulation requires explicit `--shield-armed` CLI flag. A kill switch (`--kill-switch`) instantly removes all shedding rules.

---

## 2. Files Created & Modified

### `pkg/shield/` (all new)

| File | Purpose |
|---|---|
| `config.go` | Config struct + DefaultConfig() — all safety tunables |
| `actuator.go` | Pluggable `Actuator` interface + `LogActuator` (dry-run default) + `AuditEntry` |
| `strategy.go` | Cut-edge identification + shed percentage computation |
| `recovery.go` | `RecoveryManager` — gradual ramp-up/ramp-down with consecutive-low guard |
| `controller.go` | Main OODA control loop (Observe → Decide → Act → Recover) |
| `shield_test.go` | 10 unit tests: strategy, recovery, controller, kill switch, actuator |

### `cmd/cascadeshield/main.go` (modified)

- Added `--shield-armed`, `--shield-interval`, `--kill-switch` CLI flags
- Initialized Shield controller with LogActuator (dry-run)
- Kill switch path: `--kill-switch` → `RemoveAll()` → exit(0)
- Logs shield state on shutdown

---

## 3. Control Loop (OODA)

```
Every ControlInterval (10s):

    1. OBSERVE:  Read oracle.LatestReport()
    2. DECIDE:   ComputeSheddingStrategy() →
                   - For each node with RiskScore ≥ 0.7:
                     - Find cut edge (first hop in top cascade path)
                     - shed% = (risk - 0.7) / (1 - 0.7) → proportional to overshoot
    3. ACT:      RecoveryManager.ProcessDecisions() →
                   - Ramp UP:   increase by at most 15%/interval (no shock-shedding)
                   - Ramp DOWN: decrease by at most 10%/interval (no traffic flood)
                   - Full removal: only after 3 consecutive intervals below threshold
                 → Actuator.Apply() or Actuator.Remove()
    4. RECOVER:  If risk < 0.3 for 3 consecutive intervals → remove shedding entirely
```

---

## 4. Safety Architecture

| Safety Feature | Implementation |
|---|---|
| **Dry-run default** | `Armed=false` by default. All actions logged but NOT executed. |
| **Kill switch** | `--kill-switch` → `RemoveAll()` → instant removal of all rules |
| **Max shed cap** | `MaxShedPercentage=0.8` — at least 20% of traffic always flows |
| **Ramp-up rate limit** | `RampUpStep=0.15` — max 15% increase per interval |
| **Ramp-down rate limit** | `RampDownStep=0.10` — max 10% decrease per interval |
| **Recovery guard** | `ConsecutiveLowIntervals=3` — risk must stay LOW for 30 seconds before shedding removed |
| **Audit log** | Every action logged with timestamp, reason, and metrics |
| **Pluggable actuator** | Strategy pattern — swap LogActuator for TC/eBPF/iptables without changing controller |

---

## 5. Strategy: Cut Edge Selection

```
Cascade path (from Phase 4): [payments, orders, gateway]
Propagation direction:         upstream (payments → orders → gateway)

Cut edge = the FIRST edge in the cascade chain:
  orders → payments  (the caller → the slow callee)

Why? Shedding here reduces the load on payments, preventing thread pool exhaustion
in orders, which prevents the cascade from reaching gateway.

Shed percentage:
  shed% = (risk - ShedThreshold) / (1.0 - ShedThreshold)
  At risk=0.85, threshold=0.7:  shed% = (0.85-0.7)/(1-0.7) = 50%
  At risk=0.71, threshold=0.7:  shed% = (0.71-0.7)/(1-0.7) ≈ 3% (minimum 5%)
```

---

## 6. Recovery Manager

```
State transitions:

  NO SHEDDING
      │
      ▼  RiskScore ≥ ShedThreshold
  RAMPING UP (CurrentShed increases by RampUpStep each interval)
      │
      ▼  CurrentShed reaches TargetShed
  STEADY STATE (CurrentShed == TargetShed)
      │
      ▼  RiskScore drops below ShedThreshold
  RAMPING DOWN (CurrentShed decreases by RampDownStep each interval)
      │
      ▼  Risk < RecoveryThreshold for ConsecutiveLowIntervals
  FULL RECOVERY (shedding removed)
```

Key: **the recovery guard prevents oscillation.** Without it, removing shedding causes risk to spike again (because load returns), which triggers shedding again, which lowers risk, etc. The 3-interval guard ensures the system is truly stable.

---

## 7. Configuration (All Tunables)

| Flag / Config | Default | Description |
|---|---|---|
| `--shield-armed` | false | Enable real traffic manipulation |
| `--shield-interval` | 10s | Control loop interval |
| `--kill-switch` | false | Remove all rules and exit |
| `ShedThreshold` | 0.7 | Risk score above which shedding starts |
| `RecoveryThreshold` | 0.3 | Risk score below which recovery begins |
| `ConsecutiveLowIntervals` | 3 | Low-risk intervals before full removal |
| `MaxShedPercentage` | 0.8 | Maximum shed fraction (20% traffic floor) |
| `RampUpStep` | 0.15 | Max shed % increase per interval |
| `RampDownStep` | 0.10 | Max shed % decrease per interval |
| `AuditLogEnabled` | true | Log every shield decision |

---

## 8. Testing Results

```bash
go test -v -race -timeout 60s ./pkg/shield/...
```

**Result:** ALL PASS — 10 tests, 0 failures, 0 data races

| Test | Result | Notes |
|---|---|---|
| `TestStrategy_NoShedBelowThreshold` | ✅ | Risk 0.5 < threshold 0.7 → no decisions |
| `TestStrategy_ShedAboveThreshold` | ✅ | Risk 0.85 → shed orders→payments by 50% |
| `TestStrategy_NilReport` | ✅ | nil report → no decisions |
| `TestRecovery_RampUp` | ✅ | 15% → 30% → 45% → 50% (4 intervals to reach target) |
| `TestRecovery_RampDown` | ✅ | 50% → 40% → ... (10% per interval) |
| `TestRecovery_FullRecoveryAfterConsecutiveLow` | ✅ | Removed after 2 consecutive low intervals |
| `TestRecovery_MaxShedCap` | ✅ | 100% target capped at 80% |
| `TestController_DryRunDefault` | ✅ | Actions logged as DRY-RUN |
| `TestController_KillSwitch` | ✅ | REMOVE_ALL recorded in audit log |
| `TestLogActuator_ActiveRules` | ✅ | Correct tracking of active rules |

---

## 9. Notes for Phase 5 (Observability)

Phase 5 should expose these Shield metrics via Prometheus:

```
cascadeshield_shield_active{source="orders", target="payments"}       1
cascadeshield_shield_shed_percentage{source="orders", target="payments"} 0.35
cascadeshield_shield_decisions_total                                   42
cascadeshield_shield_armed                                             0
```

The Shield's audit log entries (`AuditEntry`) are JSON-serializable and can be pushed to the TUI's event panel.

---

## 10. Notes for Phase 6 (Chaos Arena)

To test The Shield end-to-end:
1. Deploy 5 demo microservices in a chain
2. Inject latency at one leaf service
3. Verify The Oracle predicts the cascade
4. Verify The Shield sheds the correct cut edge
5. Verify the cascade is prevented
