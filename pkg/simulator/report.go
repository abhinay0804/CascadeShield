package simulator

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/simulator/models"
)

// SimulationReport is the top-level output of a full Oracle simulation cycle.
// It contains per-node risk assessments for every service in the DAG.
//
// Phase 4b (The Shield) reads this to decide load-shedding actions.
// Phase 5 (Observability) reads this to render the TUI/Grafana dashboard.
type SimulationReport struct {
	// Timestamp is when this simulation cycle completed.
	Timestamp time.Time `json:"timestamp"`

	// SnapshotTime is when the DAG snapshot was taken (before simulation started).
	SnapshotTime time.Time `json:"snapshot_time"`

	// NodeCount is the number of nodes in the graph at simulation time.
	NodeCount int `json:"node_count"`

	// EdgeCount is the number of edges in the graph at simulation time.
	EdgeCount int `json:"edge_count"`

	// NumSimulations is the Monte Carlo sample count per node.
	NumSimulations int `json:"num_simulations"`

	// Duration is the total execution time of the simulation cycle.
	Duration time.Duration `json:"duration_ns"`

	// NodeResults maps each node ID to its risk assessment.
	NodeResults map[string]*NodeSimResult `json:"node_results"`
}

// NodeSimResult holds the simulation output for a single origin node.
type NodeSimResult struct {
	// NodeID is the service node this result is about.
	NodeID string `json:"node_id"`

	// RiskScore is the normalized cascade risk score in [0.0, 1.0].
	RiskScore float64 `json:"risk_score"`

	// RiskLevel is the human-readable classification (LOW/MEDIUM/HIGH/CRITICAL).
	RiskLevel RiskLevel `json:"risk_level"`

	// MeanTTF is the average time-to-failure across all simulation runs
	// where a cascade occurred. Zero if no cascades were observed.
	MeanTTF time.Duration `json:"mean_ttf_ns"`

	// CascadeProbability is the fraction of simulation runs where at least
	// one other node was exhausted due to this node's degradation.
	CascadeProbability float64 `json:"cascade_probability"`

	// TopCascadePaths are the most frequently observed cascade paths,
	// sorted by frequency (most common first).
	TopCascadePaths []CascadePath `json:"top_cascade_paths"`

	// AffectedNodes lists all unique nodes that were exhausted in at least
	// one simulation run.
	AffectedNodes []string `json:"affected_nodes"`
}

// CascadePath represents a specific cascade failure path and how often
// it was observed across Monte Carlo runs.
type CascadePath struct {
	// Path is the ordered list of nodes in the cascade chain.
	Path []string `json:"path"`

	// Frequency is the number of simulation runs that produced this exact path.
	Frequency int `json:"frequency"`

	// MeanTTF is the average time-to-failure for the last node in this path.
	MeanTTF time.Duration `json:"mean_ttf_ns"`
}

// BuildNodeResult aggregates raw CascadeRun data into a structured NodeSimResult.
func BuildNodeResult(originID string, runs []CascadeRun, cfg Config, riskScore float64) *NodeSimResult {
	result := &NodeSimResult{
		NodeID:    originID,
		RiskScore: riskScore,
		RiskLevel: ClassifyRisk(riskScore, cfg),
	}

	if len(runs) == 0 {
		return result
	}

	// Count cascades (runs where at least one non-origin node was exhausted)
	cascadeCount := 0
	var totalTTF time.Duration
	var ttfCount int
	affectedSet := make(map[string]bool)
	pathCounts := make(map[string]pathAggregator)

	for _, run := range runs {
		hadCascade := false
		for nodeID, state := range run.NodeStates {
			if nodeID == originID {
				continue
			}
			if state == models.StateExhausted {
				hadCascade = true
				affectedSet[nodeID] = true
			}
		}
		if hadCascade {
			cascadeCount++
		}

		// Aggregate cascade paths
		if len(run.Path) > 1 {
			key := pathKey(run.Path)
			agg := pathCounts[key]
			agg.path = run.Path
			agg.count++
			// Track TTF for last node in path
			lastNode := run.Path[len(run.Path)-1]
			if ttf, ok := run.TimeToFailure[lastNode]; ok && ttf > 0 {
				agg.totalTTF += ttf
				agg.ttfCount++
				totalTTF += ttf
				ttfCount++
			}
			pathCounts[key] = agg
		}
	}

	result.CascadeProbability = float64(cascadeCount) / float64(len(runs))

	if ttfCount > 0 {
		result.MeanTTF = totalTTF / time.Duration(ttfCount)
	}

	// Collect affected nodes
	for nodeID := range affectedSet {
		result.AffectedNodes = append(result.AffectedNodes, nodeID)
	}
	sort.Strings(result.AffectedNodes)

	// Build top cascade paths (sorted by frequency, keep top N)
	var paths []CascadePath
	for _, agg := range pathCounts {
		cp := CascadePath{
			Path:      agg.path,
			Frequency: agg.count,
		}
		if agg.ttfCount > 0 {
			cp.MeanTTF = agg.totalTTF / time.Duration(agg.ttfCount)
		}
		paths = append(paths, cp)
	}
	sort.Slice(paths, func(i, j int) bool {
		return paths[i].Frequency > paths[j].Frequency
	})
	if len(paths) > cfg.TopCascadePathsCount {
		paths = paths[:cfg.TopCascadePathsCount]
	}
	result.TopCascadePaths = paths

	return result
}

// pathAggregator accumulates path statistics during report building.
type pathAggregator struct {
	path     []string
	count    int
	totalTTF time.Duration
	ttfCount int
}

// pathKey creates a hashable string key from a cascade path.
func pathKey(path []string) string {
	if len(path) == 0 {
		return ""
	}
	key := path[0]
	for _, p := range path[1:] {
		key += "→" + p
	}
	return key
}

// ToJSON serializes the report to indented JSON for API/TUI consumption.
func (r *SimulationReport) ToJSON() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SimulationReport: %w", err)
	}
	return data, nil
}

// Summary returns a concise human-readable summary of the simulation results.
func (r *SimulationReport) Summary() string {
	if r == nil || len(r.NodeResults) == 0 {
		return "No simulation results"
	}

	// Count by risk level
	counts := map[RiskLevel]int{
		RiskLow: 0, RiskMedium: 0, RiskHigh: 0, RiskCritical: 0,
	}
	var highestRisk float64
	var highestNode string
	for _, nr := range r.NodeResults {
		counts[nr.RiskLevel]++
		if nr.RiskScore > highestRisk {
			highestRisk = nr.RiskScore
			highestNode = nr.NodeID
		}
	}

	return fmt.Sprintf(
		"Simulation complete: %d nodes, %d edges | "+
			"Risk: %d CRITICAL, %d HIGH, %d MEDIUM, %d LOW | "+
			"Highest: %s (%.3f)",
		r.NodeCount, r.EdgeCount,
		counts[RiskCritical], counts[RiskHigh], counts[RiskMedium], counts[RiskLow],
		highestNode, highestRisk,
	)
}
