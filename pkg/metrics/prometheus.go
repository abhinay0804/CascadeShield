package metrics

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	"github.com/abhinay0804/cascadeshield/pkg/simulator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// RiskReporter provides access to the latest Oracle simulation report.
type RiskReporter interface {
	LatestReport() *simulator.SimulationReport
}

// ShieldReporter provides access to active load-shedding states.
type ShieldReporter interface {
	ActiveSheddingStates() map[string]float64
}

// EventReporter provides access to cumulative eBPF event counts.
type EventReporter interface {
	TotalEvents() uint64
}

// PrometheusServer manages a Prometheus HTTP metrics exposition endpoint.
//
// For beginners: Prometheus is a popular metrics collection system. It "scrapes"
// (polls) your /metrics endpoint at a configurable interval and stores the values.
// Grafana dashboards then query Prometheus to display graphs.
//
// Our /metrics endpoint is a plain HTTP server that, on every GET request,
// reads the current live state from the DAG, Oracle simulator, and Shield controller
// and writes all edge metrics as Prometheus-format text.
type PrometheusServer struct {
	cfg       Config
	dag       *graph.DAG
	annotator *Annotator
	logger    *slog.Logger
	server    *http.Server

	riskReporter   RiskReporter
	shieldReporter ShieldReporter
	eventReporter  EventReporter

	// Prometheus gauge vectors — edge level
	reqRateGauge   *prometheus.GaugeVec
	p50Gauge       *prometheus.GaugeVec
	p95Gauge       *prometheus.GaugeVec
	p99Gauge       *prometheus.GaugeVec
	errorRateGauge *prometheus.GaugeVec
	bpsGauge       *prometheus.GaugeVec

	// Graph system gauges
	dagNodesGauge prometheus.Gauge
	dagEdgesGauge prometheus.Gauge

	// Phase 5 node risk gauges
	nodeRiskGauge        *prometheus.GaugeVec
	nodeCascadeProbGauge *prometheus.GaugeVec
	nodeTTFGauge         *prometheus.GaugeVec

	// Phase 5 shield gauges
	shieldActiveGauge *prometheus.GaugeVec
	shieldShedGauge   *prometheus.GaugeVec

	// Phase 5 system metrics
	simDurationGauge prometheus.Gauge
	ebpfEventsGauge  prometheus.Gauge

	// Scrape rate calculation state
	scrapeMu       sync.Mutex
	lastScrapeTime time.Time
	lastEventCount uint64

	// Custom registry (not the global default) so tests don't clash
	registry *prometheus.Registry
}

// NewPrometheusServer creates a PrometheusServer and registers all metrics.
func NewPrometheusServer(cfg Config, dag *graph.DAG, annotator *Annotator, logger *slog.Logger) *PrometheusServer {
	if logger == nil {
		logger = slog.Default()
	}

	// Use a dedicated registry to avoid conflicts with the default global one.
	reg := prometheus.NewRegistry()

	edgeLabels := []string{"source_service", "target_service"}
	shieldLabels := []string{"source", "target"}
	serviceLabels := []string{"service"}

	// Edge-level gauge vectors
	reqRateGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "edge_request_rate",
		Help:      "TCP connections per second on this service-to-service edge (sliding window)",
	}, edgeLabels)

	p50Gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "edge_latency_p50_ms",
		Help:      "Median (50th percentile) TCP connection latency in milliseconds",
	}, edgeLabels)

	p95Gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "edge_latency_p95_ms",
		Help:      "95th percentile TCP connection latency in milliseconds",
	}, edgeLabels)

	p99Gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "edge_latency_p99_ms",
		Help:      "99th percentile TCP connection latency in milliseconds",
	}, edgeLabels)

	errorRateGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "edge_error_rate",
		Help:      "Fraction of connections with TCP retransmits (0.0–1.0)",
	}, edgeLabels)

	bpsGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "edge_bytes_per_sec",
		Help:      "Combined throughput (bytes_sent + bytes_received) per second on this edge",
	}, edgeLabels)

	dagNodesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "dag_nodes_total",
		Help:      "Total number of service nodes currently in the live dependency DAG",
	})

	dagEdgesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "dag_edges_total",
		Help:      "Total number of active edges in the live dependency DAG",
	})

	// Phase 5 Node Risk Gauges
	nodeRiskGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "node_risk_score",
		Help:      "Monte Carlo simulation risk score for service [0.0, 1.0]",
	}, serviceLabels)

	nodeCascadeProbGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "node_cascade_probability",
		Help:      "Probability of service causing cascading failure [0.0, 1.0]",
	}, serviceLabels)

	nodeTTFGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "node_ttf_seconds",
		Help:      "Estimated time-to-failure in seconds under cascade conditions",
	}, serviceLabels)

	// Phase 5 Shield Action Gauges
	shieldActiveGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "shield_active",
		Help:      "1 if active load shedding is applied on this edge, 0 otherwise",
	}, shieldLabels)

	shieldShedGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "shield_shed_percentage",
		Help:      "Traffic shedding percentage currently applied on edge [0.0, 1.0]",
	}, shieldLabels)

	// Phase 5 System Health Gauges
	simDurationGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "simulation_duration_ms",
		Help:      "Execution duration of the latest Monte Carlo simulation cycle in milliseconds",
	})

	ebpfEventsGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cascadeshield",
		Name:      "ebpf_events_per_second",
		Help:      "Throughput of eBPF events processed per second",
	})

	// Register all metrics with our custom registry
	reg.MustRegister(
		reqRateGauge, p50Gauge, p95Gauge, p99Gauge,
		errorRateGauge, bpsGauge,
		dagNodesGauge, dagEdgesGauge,
		nodeRiskGauge, nodeCascadeProbGauge, nodeTTFGauge,
		shieldActiveGauge, shieldShedGauge,
		simDurationGauge, ebpfEventsGauge,
	)

	return &PrometheusServer{
		cfg:                  cfg,
		dag:                  dag,
		annotator:            annotator,
		logger:               logger,
		registry:             reg,
		reqRateGauge:         reqRateGauge,
		p50Gauge:             p50Gauge,
		p95Gauge:             p95Gauge,
		p99Gauge:             p99Gauge,
		errorRateGauge:       errorRateGauge,
		bpsGauge:             bpsGauge,
		dagNodesGauge:        dagNodesGauge,
		dagEdgesGauge:        dagEdgesGauge,
		nodeRiskGauge:        nodeRiskGauge,
		nodeCascadeProbGauge: nodeCascadeProbGauge,
		nodeTTFGauge:         nodeTTFGauge,
		shieldActiveGauge:    shieldActiveGauge,
		shieldShedGauge:      shieldShedGauge,
		simDurationGauge:     simDurationGauge,
		ebpfEventsGauge:      ebpfEventsGauge,
		lastScrapeTime:       time.Now(),
	}
}

// SetRiskReporter sets the reporter for Monte Carlo simulation risk data.
func (ps *PrometheusServer) SetRiskReporter(rr RiskReporter) {
	ps.riskReporter = rr
}

// SetShieldReporter sets the reporter for Shield traffic control state data.
func (ps *PrometheusServer) SetShieldReporter(sr ShieldReporter) {
	ps.shieldReporter = sr
}

// SetEventReporter sets the reporter for eBPF event throughput metrics.
func (ps *PrometheusServer) SetEventReporter(er EventReporter) {
	ps.eventReporter = er
}

// updateGauges refreshes all gauge values from the live system state.
// This is called on each scrape request from Prometheus.
func (ps *PrometheusServer) updateGauges() {
	edges := ps.dag.GetAllEdges()

	// Reset vectors to remove stale label combinations
	ps.reqRateGauge.Reset()
	ps.p50Gauge.Reset()
	ps.p95Gauge.Reset()
	ps.p99Gauge.Reset()
	ps.errorRateGauge.Reset()
	ps.bpsGauge.Reset()

	for _, e := range edges {
		labels := prometheus.Labels{
			"source_service": e.Source,
			"target_service": e.Target,
		}
		ps.reqRateGauge.With(labels).Set(e.Metrics.RequestRate)
		ps.p50Gauge.With(labels).Set(e.Metrics.LatencyP50)
		ps.p95Gauge.With(labels).Set(e.Metrics.LatencyP95)
		ps.p99Gauge.With(labels).Set(e.Metrics.LatencyP99)
		ps.errorRateGauge.With(labels).Set(e.Metrics.ErrorRate)
		ps.bpsGauge.With(labels).Set(e.Metrics.BytesPerSec)
	}

	ps.dagNodesGauge.Set(float64(ps.dag.NodeCount()))
	ps.dagEdgesGauge.Set(float64(ps.dag.EdgeCount()))

	// Update Phase 5 Oracle simulation risk metrics
	if ps.riskReporter != nil {
		report := ps.riskReporter.LatestReport()
		if report != nil {
			ps.simDurationGauge.Set(float64(report.Duration.Milliseconds()))
			ps.nodeRiskGauge.Reset()
			ps.nodeCascadeProbGauge.Reset()
			ps.nodeTTFGauge.Reset()

			for svc, res := range report.NodeResults {
				ps.nodeRiskGauge.WithLabelValues(svc).Set(res.RiskScore)
				ps.nodeCascadeProbGauge.WithLabelValues(svc).Set(res.CascadeProbability)
				ps.nodeTTFGauge.WithLabelValues(svc).Set(res.MeanTTF.Seconds())
			}
		}
	}

	// Update Phase 5 Shield load-shedding metrics
	if ps.shieldReporter != nil {
		states := ps.shieldReporter.ActiveSheddingStates()
		ps.shieldActiveGauge.Reset()
		ps.shieldShedGauge.Reset()

		for key, shedPct := range states {
			parts := strings.Split(key, "→")
			if len(parts) == 2 {
				src, target := parts[0], parts[1]
				ps.shieldActiveGauge.WithLabelValues(src, target).Set(1.0)
				ps.shieldShedGauge.WithLabelValues(src, target).Set(shedPct)
			}
		}
	}

	// Update Phase 5 eBPF throughput metrics
	if ps.eventReporter != nil {
		ps.scrapeMu.Lock()
		now := time.Now()
		total := ps.eventReporter.TotalEvents()
		dt := now.Sub(ps.lastScrapeTime).Seconds()
		if dt > 0 {
			rate := float64(total-ps.lastEventCount) / dt
			if rate < 0 {
				rate = 0
			}
			ps.ebpfEventsGauge.Set(rate)
		}
		ps.lastScrapeTime = now
		ps.lastEventCount = total
		ps.scrapeMu.Unlock()
	}
}

// Start starts the Prometheus HTTP server in the background.
// It returns immediately — the server runs until ctx is cancelled.
//
// Go Pattern: goroutine + context for lifecycle management.
func (ps *PrometheusServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Wrap the standard prometheus handler so we update gauges on each scrape.
	// promhttp.HandlerFor uses our custom registry (not the global default).
	stdHandler := promhttp.HandlerFor(ps.registry, promhttp.HandlerOpts{
		ErrorLog: nil, // use default stderr
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		ps.updateGauges()
		stdHandler.ServeHTTP(w, r)
	})

	// Health check endpoint — useful for Kubernetes liveness probes
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	ps.server = &http.Server{
		Addr:    ps.cfg.MetricsAddr,
		Handler: mux,
	}

	// Start the HTTP server in a goroutine so Start() returns immediately
	go func() {
		ps.logger.Info("Prometheus metrics server starting", "addr", ps.cfg.MetricsAddr)
		if err := ps.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ps.logger.Error("Prometheus metrics server error", "error", err)
		}
	}()

	// Start a goroutine that shuts down the server when ctx is cancelled
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ps.server.Shutdown(shutdownCtx); err != nil {
			ps.logger.Error("Prometheus server shutdown error", "error", err)
		} else {
			ps.logger.Info("Prometheus metrics server stopped")
		}
	}()

	return nil
}
