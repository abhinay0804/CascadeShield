package shield

import (
	"sync"
)

// EdgeState tracks the current shedding state and recovery progress for one edge.
type EdgeState struct {
	Source          string
	Target          string
	CurrentShed     float64 // Current shed percentage [0.0, 1.0]
	TargetShed      float64 // Target shed percentage from strategy
	LowRiskCount    int     // Consecutive intervals below RecoveryThreshold
	Active          bool    // Whether shedding is currently active on this edge
}

// RecoveryManager tracks per-edge shedding state and manages gradual ramp-up/ramp-down.
//
// The key safety property: shedding never changes instantly.
//   - Ramp UP: increase shed % by at most RampUpStep per interval
//   - Ramp DOWN: decrease shed % by at most RampDownStep per interval
//   - Full removal: only after ConsecutiveLowIntervals below RecoveryThreshold
//
// For beginners: this is like a thermostat. You don't flip the furnace on/off
// every second — you gradually adjust. Sudden changes in traffic shedding would
// cause traffic storms and oscillation.
type RecoveryManager struct {
	cfg    Config
	states map[string]*EdgeState // key: "source→target"
	mu     sync.RWMutex
}

// NewRecoveryManager creates a new recovery manager.
func NewRecoveryManager(cfg Config) *RecoveryManager {
	return &RecoveryManager{
		cfg:    cfg,
		states: make(map[string]*EdgeState),
	}
}

// edgeKey creates a map key from source and target.
func edgeKey(source, target string) string {
	return source + "→" + target
}

// ProcessDecisions takes the strategy's shedding decisions and computes the
// actual shed percentages to apply, respecting ramp-up and ramp-down constraints.
//
// Returns: list of (source, target, actualShedPercent) to apply via the Actuator,
// and a list of edges that should be fully removed.
func (rm *RecoveryManager) ProcessDecisions(decisions []SheddingDecision) (toApply []EdgeState, toRemove []EdgeState) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	// Track which edges have decisions this round
	decisionMap := make(map[string]SheddingDecision)
	for _, d := range decisions {
		decisionMap[edgeKey(d.Source, d.Target)] = d
	}

	// Process edges that have NEW or UPDATED shedding decisions
	for _, d := range decisions {
		key := edgeKey(d.Source, d.Target)
		state, exists := rm.states[key]
		if !exists {
			state = &EdgeState{
				Source: d.Source,
				Target: d.Target,
			}
			rm.states[key] = state
		}

		state.TargetShed = d.ShedPercent
		state.LowRiskCount = 0 // Reset recovery counter — risk is still high
		state.Active = true

		// Ramp UP: don't jump to target, increase gradually
		if state.CurrentShed < state.TargetShed {
			newShed := state.CurrentShed + rm.cfg.RampUpStep
			if newShed > state.TargetShed {
				newShed = state.TargetShed
			}
			if newShed > rm.cfg.MaxShedPercentage {
				newShed = rm.cfg.MaxShedPercentage
			}
			state.CurrentShed = newShed
		} else if state.CurrentShed > state.TargetShed {
			// Target decreased (risk dropping) — ramp down
			newShed := state.CurrentShed - rm.cfg.RampDownStep
			if newShed < state.TargetShed {
				newShed = state.TargetShed
			}
			state.CurrentShed = newShed
		}

		toApply = append(toApply, *state)
	}

	// Process edges that are currently shedding but have NO decision this round
	// (risk has dropped below threshold → start recovery)
	for key, state := range rm.states {
		if !state.Active {
			continue
		}
		if _, hasDecision := decisionMap[key]; hasDecision {
			continue // Already processed above
		}

		// No decision = risk is below threshold → start ramping down
		state.LowRiskCount++

		if state.LowRiskCount >= rm.cfg.ConsecutiveLowIntervals {
			// Fully recovered — remove shedding
			state.Active = false
			state.CurrentShed = 0
			state.TargetShed = 0
			toRemove = append(toRemove, *state)
			delete(rm.states, key)
		} else {
			// Gradually ramp down
			newShed := state.CurrentShed - rm.cfg.RampDownStep
			if newShed < 0 {
				newShed = 0
			}
			state.CurrentShed = newShed
			if newShed > 0 {
				toApply = append(toApply, *state)
			} else {
				toRemove = append(toRemove, *state)
				state.Active = false
				delete(rm.states, key)
			}
		}
	}

	return toApply, toRemove
}

// ActiveEdgeCount returns the number of edges currently being shed.
func (rm *RecoveryManager) ActiveEdgeCount() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	count := 0
	for _, state := range rm.states {
		if state.Active {
			count++
		}
	}
	return count
}

// ActiveSheddingStates returns a map of active shedding edges ("source→target" -> shedPercent).
func (rm *RecoveryManager) ActiveSheddingStates() map[string]float64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make(map[string]float64)
	for key, state := range rm.states {
		if state.Active && state.CurrentShed > 0 {
			result[key] = state.CurrentShed
		}
	}
	return result
}

// Reset clears all recovery state (kill switch).
func (rm *RecoveryManager) Reset() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.states = make(map[string]*EdgeState)
}
