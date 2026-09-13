package chaos

import (
	"fmt"
	"time"
)

// Scenario represents a runnable chaos experiment.
type Scenario interface {
	Name() string
	Description() string
	Run(injector *Injector) error
	Cleanup(injector *Injector) error
}

// BaseScenario contains common fields for scenario metadata.
type BaseScenario struct {
	ScenarioName string
	Desc         string
}

func (b *BaseScenario) Name() string {
	return b.ScenarioName
}

func (b *BaseScenario) Description() string {
	return b.Desc
}

// PaymentSlowdownScenario implements Scenario 1: Payment Slowdown.
// Inject 300ms latency into payments service.
type PaymentSlowdownScenario struct {
	BaseScenario
	TargetService string
	LatencyMs     int
}

// NewPaymentSlowdownScenario constructs Scenario 1.
func NewPaymentSlowdownScenario() *PaymentSlowdownScenario {
	return &PaymentSlowdownScenario{
		BaseScenario: BaseScenario{
			ScenarioName: "Payment Slowdown",
			Desc:         "Inject 300ms latency into Payments; expect Orders thread pool exhaustion within ~1s.",
		},
		TargetService: "payments",
		LatencyMs:     300,
	}
}

func (s *PaymentSlowdownScenario) Run(injector *Injector) error {
	return injector.Inject(s.TargetService, s.LatencyMs, 0.0)
}

func (s *PaymentSlowdownScenario) Cleanup(injector *Injector) error {
	return injector.Reset(s.TargetService)
}

// InventoryCrashScenario implements Scenario 2: Inventory Crash.
// Inject 80% error rate into inventory service to trigger 3x retry amplification.
type InventoryCrashScenario struct {
	BaseScenario
	TargetService string
	ErrorRate     float64
}

// NewInventoryCrashScenario constructs Scenario 2.
func NewInventoryCrashScenario() *InventoryCrashScenario {
	return &InventoryCrashScenario{
		BaseScenario: BaseScenario{
			ScenarioName: "Inventory Crash",
			Desc:         "Inject 80% 503 error rate into Inventory; expect Orders retry amplification 3x.",
		},
		TargetService: "inventory",
		ErrorRate:     0.80,
	}
}

func (s *InventoryCrashScenario) Run(injector *Injector) error {
	return injector.Inject(s.TargetService, 0, s.ErrorRate)
}

func (s *InventoryCrashScenario) Cleanup(injector *Injector) error {
	return injector.Reset(s.TargetService)
}

// AuthCascadeScenario implements Scenario 3: Auth Cascade.
// Inject 2000ms latency into Auth service (critical path).
type AuthCascadeScenario struct {
	BaseScenario
	TargetService string
	LatencyMs     int
}

// NewAuthCascadeScenario constructs Scenario 3.
func NewAuthCascadeScenario() *AuthCascadeScenario {
	return &AuthCascadeScenario{
		BaseScenario: BaseScenario{
			ScenarioName: "Auth Cascade",
			Desc:         "Inject 2000ms latency into Auth; expect all upstream API Gateway calls to block.",
		},
		TargetService: "auth",
		LatencyMs:     2000,
	}
}

func (s *AuthCascadeScenario) Run(injector *Injector) error {
	return injector.Inject(s.TargetService, s.LatencyMs, 0.0)
}

func (s *AuthCascadeScenario) Cleanup(injector *Injector) error {
	return injector.Reset(s.TargetService)
}

// SlowBurnScenario implements Scenario 4: Slow Burn.
// Stepwise increase in latency to test preemptive threshold prediction.
type SlowBurnScenario struct {
	BaseScenario
	TargetService string
	StepMs        int
	Steps         int
	Interval      time.Duration
}

// NewSlowBurnScenario constructs Scenario 4.
func NewSlowBurnScenario() *SlowBurnScenario {
	return &SlowBurnScenario{
		BaseScenario: BaseScenario{
			ScenarioName: "Slow Burn",
			Desc:         "Stepwise increase of Payments latency by 10ms until preemptive prediction threshold is hit.",
		},
		TargetService: "payments",
		StepMs:        10,
		Steps:         10,
		Interval:      100 * time.Millisecond,
	}
}

func (s *SlowBurnScenario) Run(injector *Injector) error {
	for i := 1; i <= s.Steps; i++ {
		lat := i * s.StepMs
		if err := injector.Inject(s.TargetService, lat, 0.0); err != nil {
			return fmt.Errorf("step %d failed: %w", i, err)
		}
		time.Sleep(s.Interval)
	}
	return nil
}

func (s *SlowBurnScenario) Cleanup(injector *Injector) error {
	return injector.Reset(s.TargetService)
}
