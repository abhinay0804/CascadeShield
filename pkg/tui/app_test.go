package tui

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	tea "github.com/charmbracelet/bubbletea"
)

type dummyCounter struct{}

func (d *dummyCounter) TotalEvents() uint64 {
	return 12345
}

func TestTUI_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MinWidth != 80 || cfg.MinHeight != 24 {
		t.Fatalf("expected min dimensions 80x24, got %dx%d", cfg.MinWidth, cfg.MinHeight)
	}
	if cfg.RefreshInterval <= 0 {
		t.Fatalf("invalid refresh interval: %v", cfg.RefreshInterval)
	}
}

func TestTUI_AppModelView(t *testing.T) {
	logger := slog.Default()
	graphCfg := graph.DefaultConfig()
	dag := graph.NewDAG(graphCfg, logger)

	dag.RecordConnect("gateway", "orders", "default")
	dag.RecordConnect("orders", "payments", "default")

	cfg := DefaultConfig()
	counter := &dummyCounter{}

	app := NewAppModel(cfg, dag, nil, nil, counter)

	// Test window size resize (valid size)
	updatedModel, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app = updatedModel.(*AppModel)

	viewStr := app.View()

	if !strings.Contains(viewStr, "CascadeShield v0.1.0") {
		t.Errorf("missing header in TUI view")
	}

	if !strings.Contains(viewStr, "gateway") || !strings.Contains(viewStr, "orders") {
		t.Errorf("missing graph nodes in TUI view")
	}

	if !strings.Contains(viewStr, "Events:") {
		t.Errorf("missing status bar in TUI view")
	}
}

func TestTUI_SmallTerminalWarning(t *testing.T) {
	cfg := DefaultConfig()
	app := NewAppModel(cfg, nil, nil, nil, nil)

	// Window too small
	updatedModel, _ := app.Update(tea.WindowSizeMsg{Width: 60, Height: 15})
	app = updatedModel.(*AppModel)

	viewStr := app.View()

	if !strings.Contains(viewStr, "Terminal window too small") {
		t.Errorf("expected small terminal warning message, got: %s", viewStr)
	}
}

func TestTUI_RenderTopology(t *testing.T) {
	cfg := DefaultConfig()
	renderer := NewTopologyRenderer(cfg)

	logger := slog.Default()
	dag := graph.NewDAG(graph.DefaultConfig(), logger)
	dag.RecordConnect("auth", "db", "default")

	snap := dag.Snapshot()
	out := renderer.Render(snap)

	if !strings.Contains(out, "auth") || !strings.Contains(out, "db") {
		t.Errorf("topology renderer missing nodes, got: %s", out)
	}
}

func TestTUI_RenderStatus(t *testing.T) {
	cfg := DefaultConfig()
	renderer := NewStatusRenderer(cfg)

	out := renderer.Render(
		1250.5,
		50000,
		5,
		7,
		142*time.Millisecond,
		false,
		0,
		2*time.Hour+14*time.Minute,
	)

	if !strings.Contains(out, "1.3k/s") && !strings.Contains(out, "1250") {
		t.Errorf("status bar missing rate: %s", out)
	}
	if !strings.Contains(out, "Nodes: 5") || !strings.Contains(out, "142ms") {
		t.Errorf("status bar missing key metrics: %s", out)
	}
}
