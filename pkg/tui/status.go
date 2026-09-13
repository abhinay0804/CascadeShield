package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// StatusRenderer formats the bottom system health status bar.
type StatusRenderer struct {
	cfg Config

	barStyle   lipgloss.Style
	labelStyle lipgloss.Style
	valueStyle lipgloss.Style
	armedStyle lipgloss.Style
	dryStyle   lipgloss.Style
}

// NewStatusRenderer creates a status bar renderer.
func NewStatusRenderer(cfg Config) *StatusRenderer {
	return &StatusRenderer{
		cfg:        cfg,
		barStyle:   lipgloss.NewStyle().Background(lipgloss.Color("235")).Foreground(lipgloss.Color("255")).Padding(0, 1),
		labelStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		valueStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")),
		armedStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")),
		dryStyle:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),
	}
}

// Render returns the single-line status bar.
func (r *StatusRenderer) Render(
	eventRate float64,
	totalEvents uint64,
	nodeCount int,
	edgeCount int,
	simDuration time.Duration,
	armed bool,
	activeShieldCount int,
	uptime time.Duration,
) string {
	shieldStr := r.dryStyle.Render("DRY-RUN")
	if armed {
		shieldStr = r.armedStyle.Render(fmt.Sprintf("⚠️ ARMED (%d active)", activeShieldCount))
	} else if activeShieldCount > 0 {
		shieldStr = r.dryStyle.Render(fmt.Sprintf("DRY-RUN (%d simulated)", activeShieldCount))
	}

	uptimeStr := formatDuration(uptime)

	statusLine := fmt.Sprintf(
		"Events: %s/s (total: %s) │ Nodes: %s │ Edges: %s │ Sim: %s │ Shield: %s │ Uptime: %s",
		r.valueStyle.Render(formatFloat(eventRate)),
		r.valueStyle.Render(formatUint(totalEvents)),
		r.valueStyle.Render(fmt.Sprintf("%d", nodeCount)),
		r.valueStyle.Render(fmt.Sprintf("%d", edgeCount)),
		r.valueStyle.Render(fmt.Sprintf("%dms", simDuration.Milliseconds())),
		shieldStr,
		r.valueStyle.Render(uptimeStr),
	)

	return r.barStyle.Render(statusLine)
}

func formatFloat(f float64) string {
	if f >= 1000000 {
		return fmt.Sprintf("%.1fM", f/1000000)
	}
	if f >= 1000 {
		return fmt.Sprintf("%.1fk", f/1000)
	}
	return fmt.Sprintf("%.0f", f)
}

func formatUint(n uint64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
