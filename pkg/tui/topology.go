package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	"github.com/charmbracelet/lipgloss"
)

// TopologyRenderer renders an ASCII representation of the service dependency DAG.
type TopologyRenderer struct {
	cfg Config

	greenStyle  lipgloss.Style
	yellowStyle lipgloss.Style
	redStyle    lipgloss.Style
	labelStyle  lipgloss.Style
	arrowStyle  lipgloss.Style
}

// NewTopologyRenderer creates a new topology ASCII renderer.
func NewTopologyRenderer(cfg Config) *TopologyRenderer {
	return &TopologyRenderer{
		cfg:         cfg,
		greenStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("42")),  // Vibrant Green
		yellowStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("214")), // Warm Yellow/Orange
		redStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("196")), // Red
		labelStyle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")),
		arrowStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
	}
}

// Render returns a formatted ASCII graph string representing the DAG snapshot.
func (r *TopologyRenderer) Render(snap *graph.DAGSnapshot) string {
	if snap == nil || len(snap.Nodes) == 0 {
		return "  (No active service nodes detected in DAG)"
	}

	var sb strings.Builder

	// Sort node IDs for stable output rendering
	nodeIDs := make([]string, 0, len(snap.Nodes))
	for id := range snap.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	sb.WriteString("  Live Dependency Topology:\n\n")

	hasEdges := false
	for _, source := range nodeIDs {
		targetsMap, exists := snap.Edges[source]
		if !exists || len(targetsMap) == 0 {
			continue
		}
		hasEdges = true

		targetIDs := make([]string, 0, len(targetsMap))
		for t := range targetsMap {
			targetIDs = append(targetIDs, t)
		}
		sort.Strings(targetIDs)

		for _, target := range targetIDs {
			edge := targetsMap[target]
			healthColor := r.getHealthStyle(edge.Metrics)

			sourceStr := r.labelStyle.Render(fmt.Sprintf("%-12s", source))
			arrowStr := r.arrowStyle.Render("──▶")
			targetStr := r.labelStyle.Render(fmt.Sprintf("%-12s", target))

			metricsStr := healthColor.Render(fmt.Sprintf(
				"[P99: %-5.1fms | Err: %-4.1f%% | %-.1freq/s]",
				edge.Metrics.LatencyP99,
				edge.Metrics.ErrorRate*100,
				edge.Metrics.RequestRate,
			))

			sb.WriteString(fmt.Sprintf("  %s %s %s %s\n", sourceStr, arrowStr, targetStr, metricsStr))
		}
	}

	if !hasEdges {
		sb.WriteString("  Isolated Nodes (no active service-to-service edges):\n")
		for _, id := range nodeIDs {
			sb.WriteString(fmt.Sprintf("  • %s\n", r.labelStyle.Render(id)))
		}
	}

	return sb.String()
}

// getHealthStyle determines edge health color based on latency and error rates.
func (r *TopologyRenderer) getHealthStyle(m graph.EdgeMetrics) lipgloss.Style {
	if m.ErrorRate >= 0.10 || m.LatencyP99 >= 500 {
		return r.redStyle
	}
	if m.ErrorRate >= 0.02 || m.LatencyP99 >= 200 {
		return r.yellowStyle
	}
	return r.greenStyle
}
