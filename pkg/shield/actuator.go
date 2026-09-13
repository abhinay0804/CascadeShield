package shield

import (
	"fmt"
	"time"
)

// Actuator is the pluggable interface for applying traffic control on edges.
//
// Implementations determine HOW traffic is actually shed — the Controller
// determines WHAT to shed and by how much.
//
// For beginners: this is the "Strategy Pattern" in Go. The Controller calls
// methods on this interface without knowing whether the implementation uses
// eBPF/TC, iptables, or just logs. You swap implementations by passing a
// different Actuator to the Controller.
type Actuator interface {
	// Apply sets the shed percentage for a specific service-to-service edge.
	// shedPercent is a float64 in [0.0, 1.0] where 0.0 = no shedding,
	// 1.0 = drop all traffic on this edge.
	//
	// Apply is idempotent: calling it twice with the same values is safe.
	Apply(source, target string, shedPercent float64) error

	// Remove removes all shedding rules for a specific edge.
	// If no rules exist for this edge, Remove is a no-op.
	Remove(source, target string) error

	// RemoveAll removes ALL active shedding rules across all edges.
	// This is the "kill switch" — called on shutdown or when --kill-switch is used.
	RemoveAll() error

	// ActiveRules returns the number of currently active shedding rules.
	ActiveRules() int

	// Name returns a human-readable name for this actuator (for logging).
	Name() string
}

// =============================================================================
// LogActuator — Dry-Run (Default)
// =============================================================================

// LogActuator is the dry-run actuator. It logs what WOULD be done without
// actually manipulating traffic. This is the DEFAULT actuator.
//
// When running CascadeShield without --shield-armed, this is what's used.
// It's also used in tests.
type LogActuator struct {
	// actions records all actions for testing/auditing purposes.
	actions []AuditEntry
}

// AuditEntry records a single shield action for the audit log.
type AuditEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	Action      string    `json:"action"`      // "APPLY", "REMOVE", "REMOVE_ALL"
	Source      string    `json:"source"`       // Source service
	Target      string    `json:"target"`       // Target service
	ShedPercent float64   `json:"shed_percent"` // 0.0–1.0
	DryRun      bool      `json:"dry_run"`      // true if LogActuator (no real action)
}

// String returns a human-readable representation of the audit entry.
func (e AuditEntry) String() string {
	prefix := "🛡️  SHIELD"
	if e.DryRun {
		prefix = "🛡️  SHIELD [DRY-RUN]"
	}
	switch e.Action {
	case "APPLY":
		return fmt.Sprintf("%s SHED %s → %s by %.0f%%", prefix, e.Source, e.Target, e.ShedPercent*100)
	case "REMOVE":
		return fmt.Sprintf("%s REMOVE shedding on %s → %s", prefix, e.Source, e.Target)
	case "REMOVE_ALL":
		return fmt.Sprintf("%s KILL SWITCH — remove ALL shedding rules", prefix)
	default:
		return fmt.Sprintf("%s %s %s → %s", prefix, e.Action, e.Source, e.Target)
	}
}

// NewLogActuator creates a dry-run actuator that only logs actions.
func NewLogActuator() *LogActuator {
	return &LogActuator{
		actions: make([]AuditEntry, 0),
	}
}

func (a *LogActuator) Apply(source, target string, shedPercent float64) error {
	a.actions = append(a.actions, AuditEntry{
		Timestamp:   time.Now(),
		Action:      "APPLY",
		Source:      source,
		Target:      target,
		ShedPercent: shedPercent,
		DryRun:      true,
	})
	return nil
}

func (a *LogActuator) Remove(source, target string) error {
	a.actions = append(a.actions, AuditEntry{
		Timestamp: time.Now(),
		Action:    "REMOVE",
		Source:    source,
		Target:    target,
		DryRun:    true,
	})
	return nil
}

func (a *LogActuator) RemoveAll() error {
	a.actions = append(a.actions, AuditEntry{
		Timestamp: time.Now(),
		Action:    "REMOVE_ALL",
		DryRun:    true,
	})
	return nil
}

func (a *LogActuator) ActiveRules() int {
	// Count unique active edges (last action was APPLY, not REMOVE)
	active := make(map[string]bool)
	for _, entry := range a.actions {
		key := entry.Source + "→" + entry.Target
		switch entry.Action {
		case "APPLY":
			active[key] = true
		case "REMOVE":
			delete(active, key)
		case "REMOVE_ALL":
			active = make(map[string]bool)
		}
	}
	return len(active)
}

func (a *LogActuator) Name() string { return "log-only (dry-run)" }

// AuditLog returns all recorded actions (for testing).
func (a *LogActuator) AuditLog() []AuditEntry {
	out := make([]AuditEntry, len(a.actions))
	copy(out, a.actions)
	return out
}
