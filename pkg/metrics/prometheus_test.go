package metrics

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	"github.com/abhinay0804/cascadeshield/pkg/simulator"
)

type mockRiskReporter struct {
	report *simulator.SimulationReport
}

func (m *mockRiskReporter) LatestReport() *simulator.SimulationReport {
	return m.report
}

type mockShieldReporter struct {
	states map[string]float64
}

func (m *mockShieldReporter) ActiveSheddingStates() map[string]float64 {
	return m.states
}

type mockEventReporter struct {
	count uint64
}

func (m *mockEventReporter) TotalEvents() uint64 {
	return m.count
}

func TestPrometheusServer_Phase5Metrics(t *testing.T) {
	logger := slog.Default()
	graphCfg := graph.DefaultConfig()
	dag := graph.NewDAG(graphCfg, logger)

	// Populate dummy nodes and edge
	dag.RecordConnect("orders", "payments", "default")

	metricsCfg := DefaultConfig()
	metricsCfg.MetricsAddr = "127.0.0.1:19090"
	annotator := NewAnnotator(metricsCfg, dag, logger)

	server := NewPrometheusServer(metricsCfg, dag, annotator, logger)

	// Set Phase 5 mock reporters
	report := &simulator.SimulationReport{
		Timestamp: time.Now(),
		Duration:  150 * time.Millisecond,
		NodeCount: 2,
		EdgeCount: 1,
		NodeResults: map[string]*simulator.NodeSimResult{
			"payments": {
				NodeID:             "payments",
				RiskScore:          0.88,
				CascadeProbability: 0.75,
				MeanTTF:            2 * time.Second,
			},
		},
	}
	server.SetRiskReporter(&mockRiskReporter{report: report})
	server.SetShieldReporter(&mockShieldReporter{
		states: map[string]float64{
			"orders→payments": 0.25,
		},
	})
	server.SetEventReporter(&mockEventReporter{count: 5000})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("failed to start prometheus server: %v", err)
	}

	// Wait briefly for server to bind
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://127.0.0.1:19090/metrics")
	if err != nil {
		t.Fatalf("failed to fetch /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	bodyStr := string(body)

	// Verify key metrics exist in Prometheus scrape output
	expectedMetrics := []string{
		`cascadeshield_dag_nodes_total 2`,
		`cascadeshield_dag_edges_total 1`,
		`cascadeshield_node_risk_score{service="payments"} 0.88`,
		`cascadeshield_node_cascade_probability{service="payments"} 0.75`,
		`cascadeshield_node_ttf_seconds{service="payments"} 2`,
		`cascadeshield_shield_active{source="orders",target="payments"} 1`,
		`cascadeshield_shield_shed_percentage{source="orders",target="payments"} 0.25`,
		`cascadeshield_simulation_duration_ms 150`,
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(bodyStr, metric) {
			t.Errorf("missing expected metric line: %q in scrape output", metric)
		}
	}
}
