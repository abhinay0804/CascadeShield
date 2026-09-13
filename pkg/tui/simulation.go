package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/abhinay0804/cascadeshield/pkg/simulator"
	"github.com/charmbracelet/lipgloss"
)

// SimulationRenderer formats Monte Carlo risk reports and cascade predictions.
type SimulationRenderer struct {
	cfg Config

	lowStyle      lipgloss.Style
	medStyle      lipgloss.Style
	highStyle     lipgloss.Style
	critStyle     lipgloss.Style
	boldStyle     lipgloss.Style
	dimStyle      lipgloss.Style
	criticalAlert lipgloss.Style
	shieldArmed   lipgloss.Style
	shieldDryRun  lipgloss.Style
}

// NewSimulationRenderer creates a renderer for simulation & risk panels.
func NewSimulationRenderer(cfg Config) *SimulationRenderer {
	return &SimulationRenderer{
		cfg:           cfg,
		lowStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color("42")),  // Green
		medStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color("214")), // Yellow
		highStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("208")), // Orange
		critStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("196")), // Red
		boldStyle:     lipgloss.NewStyle().Bold(true),
		dimStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		criticalAlert: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Background(lipgloss.Color("52")),
		shieldArmed:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")),
		shieldDryRun:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),
	}
}

// RenderRiskScores formats per-service risk scores as visual progress bars.
func (r *SimulationRenderer) RenderRiskScores(report *simulator.SimulationReport) string {
	if report == nil || len(report.NodeResults) == 0 {
		return "  (Waiting for initial Oracle Monte Carlo simulation...)"
	}

	var sb strings.Builder
	sb.WriteString("  Service Risk Scores:\n\n")

	// Sort nodes by risk score descending
	type nodeRisk struct {
		id string
		res *simulator.NodeSimResult
	}
	list := make([]nodeRisk, 0, len(report.NodeResults))
	for id, res := range report.NodeResults {
		list = append(list, nodeRisk{id: id, res: res})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].res.RiskScore > list[j].res.RiskScore
	})

	const barWidth = 12

	for _, item := range list {
		score := item.res.RiskScore
		filled := int(score * float64(barWidth))
		if filled > barWidth {
			filled = barWidth
		}
		empty := barWidth - filled

		barStr := strings.Repeat("█", filled) + strings.Repeat("░", empty)
		style := r.getScoreStyle(score)

		formattedBar := style.Render(fmt.Sprintf("%s %.2f (%s)", barStr, score, item.res.RiskLevel))
		serviceName := r.boldStyle.Render(fmt.Sprintf("%-14s", truncateStr(item.id, 14)))

		sb.WriteString(fmt.Sprintf("  %s %s\n", serviceName, formattedBar))
	}

	return sb.String()
}

// RenderCascadePrediction renders cascade path predictions and active Shield interventions.
func (r *SimulationRenderer) RenderCascadePrediction(report *simulator.SimulationReport, shieldStates map[string]float64, armed bool) string {
	var sb strings.Builder

	// 1. Critical warning banner
	var highestNode string
	var highestScore float64
	if report != nil {
		for id, res := range report.NodeResults {
			if res.RiskScore > highestScore {
				highestScore = res.RiskScore
				highestNode = id
			}
		}
	}

	if highestScore >= 0.85 {
		alertMsg := fmt.Sprintf(" ⚠️ CRITICAL: Service %s cascade risk score at %.2f! ", highestNode, highestScore)
		sb.WriteString(fmt.Sprintf("  %s\n\n", r.criticalAlert.Render(alertMsg)))
	} else if highestScore >= 0.60 {
		warnMsg := fmt.Sprintf(" ⚠️ WARNING: Elevated cascade risk detected on service %s (%.2f) ", highestNode, highestScore)
		sb.WriteString(fmt.Sprintf("  %s\n\n", r.medStyle.Render(warnMsg)))
	} else {
		sb.WriteString("  " + r.lowStyle.Render("✓ System Stable — Low Cascade Risk") + "\n\n")
	}

	// 2. Cascade Failure Path Predictions
	sb.WriteString("  Predicted Cascade Paths:\n")
	if report == nil || len(report.NodeResults) == 0 {
		sb.WriteString("  " + r.dimStyle.Render("No cascade path data available yet") + "\n")
	} else {
		renderedPaths := 0
		for _, id := range sortedNodeIDs(report.NodeResults) {
			res := report.NodeResults[id]
			for _, cp := range res.TopCascadePaths {
				if renderedPaths >= r.cfg.MaxTopCascadePaths {
					break
				}
				pathStr := strings.Join(cp.Path, " ──▶ ")
				ttfStr := fmt.Sprintf("%.1fs", cp.MeanTTF.Seconds())
				sb.WriteString(fmt.Sprintf("  • Path: %s (Est. TTF: %s)\n",
					r.highStyle.Render(pathStr),
					r.boldStyle.Render(ttfStr),
				))
				renderedPaths++
			}
			if renderedPaths >= r.cfg.MaxTopCascadePaths {
				break
			}
		}
		if renderedPaths == 0 {
			sb.WriteString("  " + r.lowStyle.Render("No cascading propagation observed in Monte Carlo runs.") + "\n")
		}
	}

	sb.WriteString("\n  Shield Intervention State:\n")
	modeStr := r.shieldDryRun.Render("DRY-RUN (Logging Only)")
	if armed {
		modeStr = r.shieldArmed.Render("⚠️ ARMED (Traffic Shedding Active)")
	}
	sb.WriteString(fmt.Sprintf("  Mode: %s\n", modeStr))

	if len(shieldStates) == 0 {
		sb.WriteString("  Active Shedding Rules: None (0 edges shed)\n")
	} else {
		sb.WriteString("  Active Shedding Rules:\n")
		for edge, shedPct := range shieldStates {
			sb.WriteString(fmt.Sprintf("  • %s ──▶ Shed %s\n",
				r.boldStyle.Render(edge),
				r.critStyle.Render(fmt.Sprintf("%.0f%%", shedPct*100)),
			))
		}
	}

	return sb.String()
}

func (r *SimulationRenderer) getScoreStyle(score float64) lipgloss.Style {
	if score >= 0.85 {
		return r.critStyle
	}
	if score >= 0.70 {
		return r.highStyle
	}
	if score >= 0.40 {
		return r.medStyle
	}
	return r.lowStyle
}

func sortedNodeIDs(m map[string]*simulator.NodeSimResult) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncateStr(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
