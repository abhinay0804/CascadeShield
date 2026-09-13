package simulator

import (
	"math"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	"github.com/abhinay0804/cascadeshield/pkg/simulator/models"
)

// RiskLevel classifies a node's cascade risk into human-readable categories.
type RiskLevel string

const (
	RiskLow      RiskLevel = "LOW"      // < 0.3 — normal operation
	RiskMedium   RiskLevel = "MEDIUM"   // 0.3–0.6 — elevated concern
	RiskHigh     RiskLevel = "HIGH"     // 0.6–0.85 — significant danger
	RiskCritical RiskLevel = "CRITICAL" // ≥ 0.85 — imminent cascade failure
)

// ClassifyRisk returns the RiskLevel for a given score using config thresholds.
func ClassifyRisk(score float64, cfg Config) RiskLevel {
	switch {
	case score >= cfg.RiskThresholdHigh:
		return RiskCritical
	case score >= cfg.RiskThresholdMedium:
		return RiskHigh
	case score >= cfg.RiskThresholdLow:
		return RiskMedium
	default:
		return RiskLow
	}
}

// ComputeRiskScore computes the risk score for a single origin node based on
// its Monte Carlo simulation results.
//
// Formula (from architecture plan):
//
//	Risk(node) = Σ over affected nodes D:
//	    P(cascade to D) × Impact(D) × Urgency(D)
//
//	P(cascade to D) = (runs where D was Exhausted) / total_runs
//	Impact(D)       = InDegree(D) / maxInDegree  (normalized fan-in — more callers = more impact)
//	Urgency(D)      = 1.0 / mean_TTF(D).Seconds()  (faster cascade = higher urgency, capped)
//
// Final score is clamped to [0.0, 1.0].
func ComputeRiskScore(originID string, runs []CascadeRun, snap *graph.DAGSnapshot) float64 {
	if len(runs) == 0 {
		return 0
	}

	totalRuns := float64(len(runs))

	// Find max in-degree for normalization
	maxInDegree := 1
	for _, node := range snap.Nodes {
		if node.InDegree > maxInDegree {
			maxInDegree = node.InDegree
		}
	}

	// For each node in the graph (that isn't the origin), compute its contribution to risk
	nodeExhaustedCount := make(map[string]int)
	nodeTTFSum := make(map[string]time.Duration)
	nodeTTFCount := make(map[string]int)

	for _, run := range runs {
		for nodeID, state := range run.NodeStates {
			if nodeID == originID {
				continue // Don't count the origin itself
			}
			if state == models.StateExhausted {
				nodeExhaustedCount[nodeID]++
				if ttf, ok := run.TimeToFailure[nodeID]; ok && ttf > 0 {
					nodeTTFSum[nodeID] += ttf
					nodeTTFCount[nodeID]++
				}
			}
		}
	}

	if len(nodeExhaustedCount) == 0 {
		return 0 // No cascades predicted
	}

	var riskScore float64

	for nodeID, exhaustedCount := range nodeExhaustedCount {
		// P(cascade to D)
		pCascade := float64(exhaustedCount) / totalRuns

		// Impact(D) = normalized fan-in
		impact := 1.0
		if node, ok := snap.Nodes[nodeID]; ok {
			impact = float64(node.InDegree+1) / float64(maxInDegree+1)
		}

		// Urgency(D) = 1 / mean_TTF (faster cascade = more urgent)
		urgency := 1.0
		if count := nodeTTFCount[nodeID]; count > 0 {
			meanTTF := nodeTTFSum[nodeID] / time.Duration(count)
			if meanTTF > 0 {
				// Cap urgency to avoid explosion for very fast cascades
				urgency = math.Min(10.0, 1.0/meanTTF.Seconds())
			}
		}

		riskScore += pCascade * impact * urgency
	}

	// Normalize: divide by the number of nodes that could potentially cascade,
	// then clamp to [0, 1]. The max possible raw score is
	// numNodes × 1.0 (pCascade) × 1.0 (impact) × 10.0 (urgency cap).
	maxPossible := float64(len(snap.Nodes)) * 10.0
	if maxPossible > 0 {
		riskScore = riskScore / maxPossible
	}

	return math.Min(1.0, math.Max(0.0, riskScore))
}
