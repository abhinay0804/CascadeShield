package shield

import (
	"github.com/abhinay0804/cascadeshield/pkg/simulator"
)

// SheddingDecision represents the Controller's decision for a single edge.
type SheddingDecision struct {
	Source      string  // Source service name
	Target      string  // Target service name
	ShedPercent float64 // Target shed percentage [0.0, 1.0]
	Reason      string  // Human-readable reason
}

// ComputeSheddingStrategy analyzes a SimulationReport and returns the set of
// shedding decisions needed to mitigate high-risk nodes.
//
// The strategy algorithm:
//   1. For each node with RiskScore >= ShedThreshold:
//      a. Look at its top cascade path
//      b. The "cut edge" is the FIRST edge in the cascade path (origin → next hop)
//         — shedding there prevents the cascade from starting
//      c. Compute shed% proportional to risk overshoot:
//         shed% = min(MaxShedPercentage, (risk - threshold) / (1.0 - threshold))
//   2. Return the list of SheddingDecisions
//
// For beginners: this is like closing a valve partially. If a pipe is about
// to burst (high risk), you reduce flow (shed traffic) at the first junction
// (cut edge) to relieve pressure downstream.
func ComputeSheddingStrategy(report *simulator.SimulationReport, cfg Config) []SheddingDecision {
	if report == nil {
		return nil
	}

	var decisions []SheddingDecision

	for _, result := range report.NodeResults {
		if result.RiskScore < cfg.ShedThreshold {
			continue // Risk too low to warrant shedding
		}

		// Find the cut edge from the top cascade path.
		// The cascade path is [origin, next_hop, ...].
		// The cut edge is origin → next_hop (the first edge in the cascade chain).
		if len(result.TopCascadePaths) == 0 || len(result.TopCascadePaths[0].Path) < 2 {
			continue // No actionable cascade path
		}

		path := result.TopCascadePaths[0].Path

		// The cut edge: the edge from the second node to the origin.
		// Remember: cascade propagates UPSTREAM. path[0] is the origin (degraded),
		// path[1] is the first caller affected. The edge to shed is path[1] → path[0]
		// (caller → callee), because that's where the caller's thread pool exhausts.
		cutSource := path[1] // The caller that's being overwhelmed
		cutTarget := path[0] // The callee that's slow (the origin of degradation)

		// Compute shed percentage proportional to risk overshoot.
		// shed% = (risk - threshold) / (1 - threshold)
		// This is 0% at the threshold and 100% when risk = 1.0.
		overshoot := (result.RiskScore - cfg.ShedThreshold) / (1.0 - cfg.ShedThreshold)
		shedPercent := overshoot
		if shedPercent > cfg.MaxShedPercentage {
			shedPercent = cfg.MaxShedPercentage
		}
		if shedPercent < 0.05 {
			shedPercent = 0.05 // Minimum meaningful shed
		}

		decisions = append(decisions, SheddingDecision{
			Source:      cutSource,
			Target:      cutTarget,
			ShedPercent: shedPercent,
			Reason: formatReason(result.NodeID, result.RiskScore, result.RiskLevel,
				cutSource, cutTarget, shedPercent),
		})
	}

	return decisions
}

func formatReason(nodeID string, score float64, level simulator.RiskLevel, src, tgt string, shed float64) string {
	return nodeID + " risk=" + formatFloat(score) + " (" + string(level) +
		") → shed " + src + "→" + tgt + " by " + formatPercent(shed)
}

func formatFloat(f float64) string {
	s := ""
	whole := int(f * 1000)
	s += string(rune('0'+whole/1000)) + "."
	s += string(rune('0'+(whole%1000)/100))
	s += string(rune('0'+(whole%100)/10))
	s += string(rune('0'+whole%10))
	return s
}

func formatPercent(f float64) string {
	pct := int(f * 100)
	if pct >= 100 {
		return "100%"
	}
	s := ""
	if pct >= 10 {
		s += string(rune('0' + pct/10))
	}
	s += string(rune('0'+pct%10)) + "%"
	return s
}
