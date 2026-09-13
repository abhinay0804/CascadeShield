package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	"github.com/abhinay0804/cascadeshield/pkg/shield"
	"github.com/abhinay0804/cascadeshield/pkg/simulator"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// EventCounter represents any component capable of reporting total eBPF events read.
type EventCounter interface {
	TotalEvents() uint64
}

// AppModel is the top-level Bubbletea model for the CascadeShield CLI TUI.
type AppModel struct {
	cfg        Config
	dag        *graph.DAG
	oracle     *simulator.Engine
	shield     *shield.Controller
	eventCount EventCounter

	width      int
	height     int
	startTime  time.Time
	lastEvents uint64
	lastTime   time.Time
	eventRate  float64

	topoRenderer   *TopologyRenderer
	simRenderer    *SimulationRenderer
	statusRenderer *StatusRenderer

	headerStyle lipgloss.Style
	boxStyle    lipgloss.Style
	warnStyle   lipgloss.Style
}

type tickMsg time.Time

// NewAppModel creates a new Bubbletea TUI application model.
func NewAppModel(
	cfg Config,
	dag *graph.DAG,
	oracle *simulator.Engine,
	shieldCtrl *shield.Controller,
	counter EventCounter,
) *AppModel {
	return &AppModel{
		cfg:            cfg,
		dag:            dag,
		oracle:         oracle,
		shield:         shieldCtrl,
		eventCount:     counter,
		startTime:      time.Now(),
		lastTime:       time.Now(),
		topoRenderer:   NewTopologyRenderer(cfg),
		simRenderer:    NewSimulationRenderer(cfg),
		statusRenderer: NewStatusRenderer(cfg),
		headerStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("57")).
			Padding(0, 1),
		boxStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")),
		warnStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")),
	}
}

func (m *AppModel) Init() tea.Cmd {
	return m.tickCmd()
}

func (m *AppModel) tickCmd() tea.Cmd {
	return tea.Tick(m.cfg.RefreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		now := time.Time(msg)
		if m.eventCount != nil {
			currEvents := m.eventCount.TotalEvents()
			dt := now.Sub(m.lastTime).Seconds()
			if dt > 0 {
				m.eventRate = float64(currEvents-m.lastEvents) / dt
				if m.eventRate < 0 {
					m.eventRate = 0
				}
			}
			m.lastEvents = currEvents
			m.lastTime = now
		}
		return m, m.tickCmd()
	}

	return m, nil
}

func (m *AppModel) View() string {
	// Review Fix 4: Check minimum terminal window dimensions (80x24)
	if m.width > 0 && m.height > 0 {
		if m.width < m.cfg.MinWidth || m.height < m.cfg.MinHeight {
			return fmt.Sprintf("\n  %s\n  Current dimensions: %dx%d (minimum required: %dx%d)\n  Please resize your terminal window or press 'q' to quit.\n",
				m.warnStyle.Render("⚠️  Terminal window too small!"),
				m.width, m.height,
				m.cfg.MinWidth, m.cfg.MinHeight,
			)
		}
	}

	var sb strings.Builder

	// 1. Header Banner
	header := m.headerStyle.Render("🛡️  CascadeShield v0.1.0 — Autonomous Cascading Failure Prevention System")
	sb.WriteString(header + "\n\n")

	// Read state snapshots
	var snap *graph.DAGSnapshot
	if m.dag != nil {
		snap = m.dag.Snapshot()
	}

	var report *simulator.SimulationReport
	if m.oracle != nil {
		report = m.oracle.LatestReport()
	}

	var shieldStates map[string]float64
	var activeShieldCount int
	if m.shield != nil {
		shieldStates = m.shield.ActiveSheddingStates()
		activeShieldCount = m.shield.ActiveSheddingCount()
	}

	// 2. Render Topology & Risk Panels side-by-side or stacked
	topoView := m.topoRenderer.Render(snap)
	riskView := m.simRenderer.RenderRiskScores(report)

	leftBox := m.boxStyle.Width(38).Render(topoView)
	rightBox := m.boxStyle.Width(38).Render(riskView)

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftBox, " ", rightBox) + "\n")

	// 3. Render Cascade Prediction & Shield Status Panel
	predView := m.simRenderer.RenderCascadePrediction(report, shieldStates, false)
	bottomBox := m.boxStyle.Width(78).Render(predView)
	sb.WriteString(bottomBox + "\n")

	// 4. Status Bar
	var totalEvts uint64
	if m.eventCount != nil {
		totalEvts = m.eventCount.TotalEvents()
	}

	nodeCount := 0
	edgeCount := 0
	if snap != nil {
		nodeCount = len(snap.Nodes)
		for _, targets := range snap.Edges {
			edgeCount += len(targets)
		}
	}

	simDuration := time.Duration(0)
	if report != nil {
		simDuration = report.Duration
	}

	statusBar := m.statusRenderer.Render(
		m.eventRate,
		totalEvts,
		nodeCount,
		edgeCount,
		simDuration,
		false, // default armed = false in dry-run view unless wired
		activeShieldCount,
		time.Since(m.startTime),
	)
	sb.WriteString(statusBar + "\n")

	return sb.String()
}
