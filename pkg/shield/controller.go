package shield

import (
	"context"
	"log/slog"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/simulator"
)

// Controller is the main Shield control loop.
//
// It runs every ControlInterval (default 10s) and:
//   1. OBSERVE: Read the latest SimulationReport from The Oracle
//   2. DECIDE:  Compute shedding strategy (which edges, how much)
//   3. ACT:     Apply via the Actuator (dry-run by default)
//   4. RECOVER: Gradually ramp down shedding when risk subsides
//
// ⚠️  DRY-RUN BY DEFAULT. Real actuation requires Armed=true.
//
// For beginners: this is a classic "control loop" — the same pattern used
// in thermostats, cruise control, and Kubernetes controllers. It continuously
// observes the system state, compares it to desired state, and takes small
// corrective actions.
type Controller struct {
	cfg      Config
	oracle   *simulator.Engine
	actuator Actuator
	recovery *RecoveryManager
	logger   *slog.Logger
}

// NewController creates a new Shield controller.
//
// Parameters:
//   - cfg: Shield config (thresholds, rate limits, armed flag)
//   - oracle: The Oracle engine to read SimulationReport from
//   - actuator: The traffic control implementation (LogActuator for dry-run)
//   - logger: Structured logger
func NewController(cfg Config, oracle *simulator.Engine, actuator Actuator, logger *slog.Logger) *Controller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{
		cfg:      cfg,
		oracle:   oracle,
		actuator: actuator,
		recovery: NewRecoveryManager(cfg),
		logger:   logger,
	}
}

// Run starts the Shield control loop. It blocks until ctx is cancelled.
func (c *Controller) Run(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.ControlInterval)
	defer ticker.Stop()

	mode := "DRY-RUN"
	if c.cfg.Armed {
		mode = "⚠️  ARMED (live traffic manipulation enabled)"
	}

	c.logger.Info("Shield controller started",
		"mode", mode,
		"actuator", c.actuator.Name(),
		"control_interval", c.cfg.ControlInterval,
		"shed_threshold", c.cfg.ShedThreshold,
		"recovery_threshold", c.cfg.RecoveryThreshold,
		"max_shed_pct", c.cfg.MaxShedPercentage,
		"ramp_up_step", c.cfg.RampUpStep,
		"ramp_down_step", c.cfg.RampDownStep,
	)

	for {
		select {
		case <-ctx.Done():
			c.shutdown()
			return
		case <-ticker.C:
			c.tick()
		}
	}
}

// tick executes one iteration of the control loop.
func (c *Controller) tick() {
	// 1. OBSERVE: Get latest Oracle report
	report := c.oracle.LatestReport()
	if report == nil {
		return // Oracle hasn't run yet
	}

	// 2. DECIDE: Compute shedding strategy
	decisions := ComputeSheddingStrategy(report, c.cfg)

	// 3. RECOVER + RATE-LIMIT: Process decisions through recovery manager
	toApply, toRemove := c.recovery.ProcessDecisions(decisions)

	// 4. ACT: Apply through actuator
	for _, state := range toApply {
		if c.cfg.AuditLogEnabled {
			c.logger.Info("Shield action",
				"action", "SHED",
				"source", state.Source,
				"target", state.Target,
				"shed_pct", state.CurrentShed,
				"target_pct", state.TargetShed,
				"armed", c.cfg.Armed,
			)
		}

		if c.cfg.Armed {
			if err := c.actuator.Apply(state.Source, state.Target, state.CurrentShed); err != nil {
				c.logger.Error("Shield actuator Apply failed",
					"source", state.Source,
					"target", state.Target,
					"error", err,
				)
			}
		} else {
			// Dry-run: log but still record in the LogActuator for audit
			_ = c.actuator.Apply(state.Source, state.Target, state.CurrentShed)
		}
	}

	for _, state := range toRemove {
		if c.cfg.AuditLogEnabled {
			c.logger.Info("Shield action",
				"action", "REMOVE",
				"source", state.Source,
				"target", state.Target,
				"armed", c.cfg.Armed,
			)
		}

		if c.cfg.Armed {
			if err := c.actuator.Remove(state.Source, state.Target); err != nil {
				c.logger.Error("Shield actuator Remove failed",
					"source", state.Source,
					"target", state.Target,
					"error", err,
				)
			}
		} else {
			_ = c.actuator.Remove(state.Source, state.Target)
		}
	}

	if len(toApply) > 0 || len(toRemove) > 0 {
		c.logger.Info("Shield tick complete",
			"edges_shedding", c.recovery.ActiveEdgeCount(),
			"decisions", len(decisions),
			"applied", len(toApply),
			"removed", len(toRemove),
		)
	}
}

// shutdown is called on context cancellation. Removes all shedding rules.
func (c *Controller) shutdown() {
	c.logger.Info("Shield controller shutting down — removing all shedding rules")
	if c.cfg.Armed {
		if err := c.actuator.RemoveAll(); err != nil {
			c.logger.Error("Shield RemoveAll failed during shutdown", "error", err)
		}
	} else {
		_ = c.actuator.RemoveAll()
	}
	c.recovery.Reset()
	c.logger.Info("Shield controller stopped",
		"active_rules", c.actuator.ActiveRules(),
	)
}

// KillSwitch immediately removes all active shedding rules.
// Can be triggered via the --kill-switch CLI flag.
func (c *Controller) KillSwitch() {
	c.logger.Warn("⚠️  KILL SWITCH ACTIVATED — removing ALL shedding rules immediately")
	if c.cfg.Armed {
		if err := c.actuator.RemoveAll(); err != nil {
			c.logger.Error("Kill switch RemoveAll failed", "error", err)
		}
	} else {
		_ = c.actuator.RemoveAll()
	}
	c.recovery.Reset()
	c.logger.Info("Kill switch complete — all shedding removed")
}

// ActiveSheddingCount returns how many edges are currently being shed.
func (c *Controller) ActiveSheddingCount() int {
	return c.recovery.ActiveEdgeCount()
}

// ActiveSheddingStates returns a map of active shedding edges ("source→target" -> shedPercent).
func (c *Controller) ActiveSheddingStates() map[string]float64 {
	return c.recovery.ActiveSheddingStates()
}
