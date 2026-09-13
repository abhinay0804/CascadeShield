package chaos

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/shield"
	"github.com/abhinay0804/cascadeshield/pkg/simulator"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.HTTPTimeout <= 0 {
		t.Errorf("expected positive HTTPTimeout, got %v", cfg.HTTPTimeout)
	}
	if cfg.MinimumAccuracyRatio <= 0 || cfg.MinimumAccuracyRatio > 1.0 {
		t.Errorf("invalid MinimumAccuracyRatio %f", cfg.MinimumAccuracyRatio)
	}
}

func TestInjectorAndScenarios(t *testing.T) {
	var lastLatency int
	var lastErrorRate float64
	var lastReset bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("reset") == "true" {
			lastReset = true
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
			return
		}
		if lat := q.Get("latency_ms"); lat != "" {
			var l int
			for _, c := range lat {
				if c >= '0' && c <= '9' {
					l = l*10 + int(c-'0')
				}
			}
			lastLatency = l
		}
		if rate := q.Get("error_rate"); rate != "" {
			if rate == "0.80" {
				lastErrorRate = 0.80
			}
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer ts.Close()

	cfg := DefaultConfig()
	cfg.HTTPTimeout = 2 * time.Second
	injector := NewInjector(cfg)

	injector.RegisterTarget("payments", ts.URL)
	injector.RegisterTarget("inventory", ts.URL)
	injector.RegisterTarget("auth", ts.URL)

	// Scenario 1: Payment Slowdown
	s1 := NewPaymentSlowdownScenario()
	if s1.Name() != "Payment Slowdown" {
		t.Errorf("unexpected scenario name %s", s1.Name())
	}
	if err := s1.Run(injector); err != nil {
		t.Fatalf("scenario 1 run failed: %v", err)
	}
	if lastLatency != 300 {
		t.Errorf("expected latency 300ms, got %d", lastLatency)
	}

	// Scenario 2: Inventory Crash
	s2 := NewInventoryCrashScenario()
	if err := s2.Run(injector); err != nil {
		t.Fatalf("scenario 2 run failed: %v", err)
	}
	if lastErrorRate != 0.80 {
		t.Errorf("expected error rate 0.80, got %f", lastErrorRate)
	}

	// Reset All
	errs := injector.ResetAll()
	if len(errs) > 0 {
		t.Errorf("expected no reset errors, got %v", errs)
	}
	if !lastReset {
		t.Errorf("expected reset to have been called")
	}
}

func TestValidator(t *testing.T) {
	cfg := DefaultConfig()
	val := NewValidator(cfg)

	report := &simulator.SimulationReport{
		Duration: 1500 * time.Millisecond,
		NodeResults: map[string]*simulator.NodeSimResult{
			"orders": {
				NodeID:    "orders",
				RiskScore: 0.85,
			},
			"payments": {
				NodeID:    "payments",
				RiskScore: 0.90,
			},
		},
	}

	s1 := NewPaymentSlowdownScenario()
	res := val.ValidateReport(s1, report, "payments")
	if !res.Passed {
		t.Errorf("expected report validation pass, got failed: %s", res.Details)
	}
	if res.AccuracyScore != 1.0 {
		t.Errorf("expected accuracy score 1.0, got %f", res.AccuracyScore)
	}

	shedding := []shield.EdgeState{
		{
			Source:      "orders",
			Target:      "payments",
			CurrentShed: 0.40,
			Active:      true,
		},
	}
	sRes := val.ValidateShieldAction(s1, shedding, "payments")
	if !sRes.Passed {
		t.Errorf("expected shield action validation pass, got failed: %s", sRes.Details)
	}
}
