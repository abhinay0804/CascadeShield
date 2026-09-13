// Package shield implements Phase 4b: The Shield — autonomous load-shedding
// to prevent cascading failures predicted by The Oracle.
//
// ⚠️  SAFETY-CRITICAL CODE: This package can manipulate live traffic.
//
// The Shield operates in a tight control loop:
//   1. OBSERVE:  Read the latest SimulationReport from The Oracle
//   2. DECIDE:   Identify nodes above risk threshold, compute cut edges + shed %
//   3. ACT:      Apply load-shedding via the pluggable Actuator interface
//   4. RECOVER:  Gradually ramp traffic back up when risk subsides
//
// DEFAULT MODE IS DRY-RUN. Real actuation requires explicit --shield-armed flag.
//
// This file defines all configurable parameters.
package shield

import (
	"time"
)

// Config holds all tunables for The Shield remediation controller.
//
// ZERO HARDCODING POLICY: Every threshold, rate limit, and safety constraint
// is configurable here with safe defaults.
type Config struct {
	// --- Control Loop ---

	// ControlInterval is how often The Shield evaluates the latest Oracle report
	// and makes shedding decisions.
	// Default: 10 seconds
	ControlInterval time.Duration

	// --- Risk Thresholds ---

	// ShedThreshold is the risk score above which The Shield will start shedding.
	// Only nodes with RiskScore >= ShedThreshold trigger shedding actions.
	// Default: 0.7
	ShedThreshold float64

	// RecoveryThreshold is the risk score below which shedding starts ramping down.
	// Once a node's risk drops below this AND stays below for ConsecutiveLowIntervals,
	// shedding is fully removed.
	// Default: 0.3
	RecoveryThreshold float64

	// ConsecutiveLowIntervals is how many consecutive control intervals a node's
	// risk must remain below RecoveryThreshold before shedding is fully removed.
	// This prevents oscillation (removing shedding too early causes risk to spike again).
	// Default: 3
	ConsecutiveLowIntervals int

	// --- Shedding Rate Limits ---

	// MaxShedPercentage is the absolute maximum fraction of traffic that can be shed
	// on any single edge. 1.0 = drop all traffic. A value like 0.8 ensures at least
	// 20% of traffic always flows (minimum traffic floor).
	// Default: 0.8 (keep at least 20% of traffic)
	MaxShedPercentage float64

	// RampUpStep is the maximum increase in shed percentage per control interval.
	// This prevents shock-shedding (going from 0% to 80% instantly).
	// Default: 0.15 (increase by at most 15% per interval)
	RampUpStep float64

	// RampDownStep is how much to DECREASE shed percentage per interval during recovery.
	// Gradual ramp-down prevents traffic flood when shedding is removed.
	// Default: 0.10 (decrease by 10% per interval)
	RampDownStep float64

	// --- Safety ---

	// Armed determines whether The Shield actually applies traffic manipulation.
	// When false (DEFAULT), all actions are logged but not executed (dry-run mode).
	// Must be explicitly set to true via --shield-armed CLI flag.
	//
	// ⚠️  SETTING THIS TO TRUE ENABLES REAL TRAFFIC DROPPING
	Armed bool

	// AuditLogEnabled enables detailed logging of every shield decision and action.
	// Default: true
	AuditLogEnabled bool
}

// DefaultConfig returns a Config pre-filled with production-safe defaults.
// DRY-RUN MODE IS DEFAULT. Armed must be explicitly enabled.
func DefaultConfig() Config {
	return Config{
		ControlInterval: 10 * time.Second,

		ShedThreshold:           0.7,
		RecoveryThreshold:       0.3,
		ConsecutiveLowIntervals: 3,

		MaxShedPercentage: 0.8,
		RampUpStep:        0.15,
		RampDownStep:      0.10,

		Armed:           false, // DRY-RUN by default — NON-NEGOTIABLE
		AuditLogEnabled: true,
	}
}
