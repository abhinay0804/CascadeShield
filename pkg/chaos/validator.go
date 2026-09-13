package chaos

import (
	"fmt"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/shield"
	"github.com/abhinay0804/cascadeshield/pkg/simulator"
)

// ValidationResult holds the result of comparing an Oracle SimulationReport / Shield action against real chaos outcome.
type ValidationResult struct {
	ScenarioName      string             `json:"scenario_name"`
	Passed            bool               `json:"passed"`
	PredictedNodeRisk map[string]float64 `json:"predicted_node_risk"`
	PredictedTTD      time.Duration      `json:"predicted_ttd"`
	ActiveShedCount   int                `json:"active_shed_count"`
	AccuracyScore     float64            `json:"accuracy_score"`
	Details           string             `json:"details"`
}

// Validator validates simulator predictions against expectations.
type Validator struct {
	cfg Config
}

// NewValidator constructs a new Validator with the given Config.
func NewValidator(cfg Config) *Validator {
	return &Validator{cfg: cfg}
}

// ValidateReport checks if a SimulationReport correctly identified risk in high-risk nodes.
func (v *Validator) ValidateReport(scenario Scenario, report *simulator.SimulationReport, expectedHighRiskNode string) ValidationResult {
	res := ValidationResult{
		ScenarioName:      scenario.Name(),
		PredictedNodeRisk: make(map[string]float64),
		PredictedTTD:      report.Duration,
	}

	if report == nil {
		res.Passed = false
		res.Details = "Nil simulation report provided"
		return res
	}

	for node, nr := range report.NodeResults {
		if nr != nil {
			res.PredictedNodeRisk[node] = nr.RiskScore
		}
	}

	nr, exists := report.NodeResults[expectedHighRiskNode]
	if !exists || nr == nil {
		res.Passed = false
		res.Details = fmt.Sprintf("Expected node %q not found in simulation report", expectedHighRiskNode)
		return res
	}

	highRisk := nr.RiskScore
	// Calculate accuracy score based on whether high risk score was assigned (> 0.5)
	if highRisk >= 0.5 {
		res.AccuracyScore = 1.0
		res.Passed = true
		res.Details = fmt.Sprintf("Correctly predicted high risk (%.2f >= 0.50) on node %q", highRisk, expectedHighRiskNode)
	} else {
		res.AccuracyScore = highRisk / 0.5
		res.Passed = res.AccuracyScore >= v.cfg.MinimumAccuracyRatio
		res.Details = fmt.Sprintf("Risk score for %q was %.2f, expected >= 0.50", expectedHighRiskNode, highRisk)
	}

	return res
}

// ValidateShieldAction verifies that shedding actions were applied to the degraded target path.
func (v *Validator) ValidateShieldAction(scenario Scenario, sheddingStates []shield.EdgeState, expectedTargetNode string) ValidationResult {
	res := ValidationResult{
		ScenarioName:      scenario.Name(),
		ActiveShedCount:   len(sheddingStates),
		PredictedNodeRisk: make(map[string]float64),
	}

	matched := false
	for _, state := range sheddingStates {
		if state.Target == expectedTargetNode && state.CurrentShed > 0 {
			matched = true
			break
		}
	}

	if matched {
		res.Passed = true
		res.AccuracyScore = 1.0
		res.Details = fmt.Sprintf("Shield actively shedding traffic targeting node %q (%d active rules)", expectedTargetNode, len(sheddingStates))
	} else {
		res.Passed = false
		res.AccuracyScore = 0.0
		res.Details = fmt.Sprintf("No active shedding rules found for target node %q", expectedTargetNode)
	}

	return res
}
