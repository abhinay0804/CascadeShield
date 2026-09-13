// CascadeShield — Entry Point
//
// This is the main entry point for the CascadeShield daemon.
//
// Phase 0: Skeleton with version/signal handling ✓
// Phase 1: eBPF probe loading + ring buffer reading ✓
// Phase 2: Topology Engine — Service Resolution & DAG Construction ✓
// Phase 3: Metrics Engine — Sliding windows + HDR histograms + Prometheus ✓
// Phase 4: The Oracle — Monte Carlo cascade simulator ✓
// Phase 4b: The Shield — Autonomous load-shedding remediation ✓
//
// Future phases will add:
//   - Phase 5: TUI mode (--tui flag)
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/abhinay0804/cascadeshield/pkg/graph"
	"github.com/abhinay0804/cascadeshield/pkg/metrics"
	"github.com/abhinay0804/cascadeshield/pkg/probe"
	"github.com/abhinay0804/cascadeshield/pkg/resolver"
	"github.com/abhinay0804/cascadeshield/pkg/shield"
	"github.com/abhinay0804/cascadeshield/pkg/simulator"
	"github.com/abhinay0804/cascadeshield/pkg/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// Build-time variables — injected by the Makefile via -ldflags
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	// -------------------------------------------------------------------------
	// CLI Flags
	// -------------------------------------------------------------------------
	showVersion := flag.Bool("version", false, "Print version and exit")

	// Phase 1 probe flags (zero hardcoding — all tunables exposed)
	ringBufSize := flag.Int("ring-buffer-size", 256*1024,
		"BPF ring buffer size in bytes (must be power of 2, default: 256KB)")
	eventChanSize := flag.Int("event-channel-size", 4096,
		"Go event channel buffer size (default: 4096)")

	// Phase 2 resolver flags
	procRoot := flag.String("proc-root", "/proc",
		"Root path for /proc filesystem")
	kubeconfig := flag.String("kubeconfig", "",
		"Path to kubeconfig (empty = in-cluster or auto-detect)")
	k8sEnabled := flag.Bool("k8s-enabled", true,
		"Enable Kubernetes service resolution")

	// Phase 2 graph flags
	staleTimeout := flag.Duration("stale-edge-timeout", 5*time.Minute,
		"Remove edges with no traffic after this duration")
	pruneInterval := flag.Duration("prune-interval", 30*time.Second,
		"How often to prune stale edges")
	topologyLogInterval := flag.Duration("topology-log-interval", 30*time.Second,
		"How often to log DAG topology summary")

	// Phase 3 metrics flags
	metricsAddr := flag.String("metrics-addr", ":9090",
		"Prometheus metrics HTTP exposition address (default: :9090)")
	windowDuration := flag.Duration("window-duration", 60*time.Second,
		"Sliding window duration for metrics aggregation (default: 60s)")
	windowBuckets := flag.Int("window-buckets", 12,
		"Number of tumbling buckets in the sliding window (default: 12 × 5s each)")

	// Phase 4 simulator flags
	simInterval := flag.Duration("sim-interval", 30*time.Second,
		"How often The Oracle runs Monte Carlo simulations (default: 30s)")
	numSims := flag.Int("num-simulations", 1000,
		"Monte Carlo iterations per origin node (default: 1000)")

	// Phase 4b shield flags
	shieldArmed := flag.Bool("shield-armed", false,
		"⚠️  Enable REAL traffic manipulation (default: dry-run only)")
	shieldInterval := flag.Duration("shield-interval", 10*time.Second,
		"Shield control loop interval (default: 10s)")
	killSwitch := flag.Bool("kill-switch", false,
		"Immediately remove ALL active shedding rules and exit")
	// Phase 5 TUI flags
	tuiEnabled := flag.Bool("tui", false,
		"Run interactive Terminal UI (TUI) mode")
	tuiRefresh := flag.Duration("tui-refresh", 500*time.Millisecond,
		"Terminal UI refresh interval (default: 500ms)")

	flag.Parse()

	if *showVersion {
		fmt.Printf("cascadeshield %s (commit: %s)\n", version, commit)
		os.Exit(0)
	}

	// -------------------------------------------------------------------------
	// Logger Setup
	// -------------------------------------------------------------------------
	var logWriter io.Writer = os.Stderr
	if *tuiEnabled {
		logFile, err := os.OpenFile("/tmp/cascadeshield.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			logWriter = logFile
			defer logFile.Close()
		} else {
			logWriter = io.Discard
		}
	}

	logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// -------------------------------------------------------------------------
	// Signal Handling
	// -------------------------------------------------------------------------
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("CascadeShield starting",
		"version", version,
		"commit", commit,
		"tui_enabled", *tuiEnabled,
	)

	// -------------------------------------------------------------------------
	// Phase 1: Load eBPF Probes
	// -------------------------------------------------------------------------
	probeCfg := probe.Config{
		RingBufferSize:   *ringBufSize,
		EventChannelSize: *eventChanSize,
	}

	p, err := probe.New(probeCfg, logger)
	if err != nil {
		logger.Error("failed to load eBPF probes", "error", err)
		if !*tuiEnabled {
			fmt.Fprintf(os.Stderr, "\n💡 Hint: eBPF requires root privileges. Try:\n")
			fmt.Fprintf(os.Stderr, "   sudo %s\n\n", os.Args[0])
		}
		os.Exit(1)
	}
	defer p.Close()

	// -------------------------------------------------------------------------
	// Phase 2: Initialize Identity Resolver
	// -------------------------------------------------------------------------
	resolverCfg := resolver.DefaultConfig()
	resolverCfg.ProcRoot = *procRoot
	resolverCfg.KubeconfigPath = *kubeconfig
	resolverCfg.KubernetesEnabled = *k8sEnabled

	res, err := resolver.NewResolver(resolverCfg, logger)
	if err != nil {
		logger.Error("failed to initialize identity resolver", "error", err)
		os.Exit(1)
	}
	if err := res.Start(ctx); err != nil {
		logger.Warn("resolver start warning", "error", err)
	}
	defer res.Stop()

	// -------------------------------------------------------------------------
	// Phase 2: Initialize Live Service Dependency DAG & Builder Worker
	// -------------------------------------------------------------------------
	graphCfg := graph.DefaultConfig()
	graphCfg.StaleEdgeTimeout = *staleTimeout
	graphCfg.PruneInterval = *pruneInterval
	graphCfg.TopologyLogInterval = *topologyLogInterval

	dag := graph.NewDAG(graphCfg, logger)
	builder := graph.NewBuilder(dag, graphCfg, logger)

	// Channel for the DAG builder (topo events)
	builderEvents := make(chan resolver.ResolvedEvent, *eventChanSize)
	go builder.Run(ctx, builderEvents)

	// -------------------------------------------------------------------------
	// Phase 3: Initialize Metrics Annotator & Prometheus Server
	// -------------------------------------------------------------------------
	metricsCfg := metrics.DefaultConfig()
	metricsCfg.MetricsAddr = *metricsAddr
	metricsCfg.WindowDuration = *windowDuration
	metricsCfg.BucketCount = *windowBuckets
	metricsCfg.AnnotateInterval = *windowDuration / time.Duration(*windowBuckets)

	metricsEvents := make(chan resolver.ResolvedEvent, *eventChanSize)

	annotator := metrics.NewAnnotator(metricsCfg, dag, logger)
	go annotator.Run(ctx, metricsEvents)

	// Start Prometheus HTTP server
	promServer := metrics.NewPrometheusServer(metricsCfg, dag, annotator, logger)
	if err := promServer.Start(ctx); err != nil {
		logger.Error("failed to start Prometheus server", "error", err)
		os.Exit(1)
	}

	// -------------------------------------------------------------------------
	// Phase 4: The Oracle — Monte Carlo Cascade Simulator
	// -------------------------------------------------------------------------
	simCfg := simulator.DefaultConfig()
	simCfg.SimulationInterval = *simInterval
	simCfg.NumSimulations = *numSims

	oracle := simulator.NewEngine(simCfg, dag, logger)
	go oracle.Run(ctx)

	// -------------------------------------------------------------------------
	// Phase 4b: The Shield — Autonomous Remediation Controller
	// -------------------------------------------------------------------------
	shieldCfg := shield.DefaultConfig()
	shieldCfg.Armed = *shieldArmed
	shieldCfg.ControlInterval = *shieldInterval

	actuator := shield.NewLogActuator() // Dry-run by default
	shieldController := shield.NewController(shieldCfg, oracle, actuator, logger)

	// Kill switch: remove all rules and exit immediately
	if *killSwitch {
		shieldController.KillSwitch()
		logger.Info("kill switch executed — exiting")
		os.Exit(0)
	}

	go shieldController.Run(ctx)

	// -------------------------------------------------------------------------
	// Phase 1: Start Ring Buffer Reader
	// -------------------------------------------------------------------------
	events := make(chan probe.Event, probeCfg.EventChannelSize)
	reader := probe.NewReader(p, logger)
	go reader.Run(ctx, events)

	// Phase 5: Wire reporters to Prometheus server
	promServer.SetRiskReporter(oracle)
	promServer.SetShieldReporter(shieldController)
	promServer.SetEventReporter(reader)

	logger.Info("all systems active",
		"ring_buffer_size", probeCfg.RingBufferSize,
		"k8s_enabled", *k8sEnabled,
		"metrics_addr", *metricsAddr,
		"sim_interval", *simInterval,
		"num_simulations", *numSims,
		"shield_armed", *shieldArmed,
		"shield_interval", *shieldInterval,
	)

	// -------------------------------------------------------------------------
	// Main Event Processing Pipeline
	// -------------------------------------------------------------------------
	if *tuiEnabled {
		// Run event processing pipeline worker in background goroutine
		go func() {
			for {
				select {
				case evt := <-events:
					resolvedEvt := res.Resolve(evt)
					select {
					case builderEvents <- resolvedEvt:
					default:
						logger.Warn("builder events channel full, dropping event")
					}
					select {
					case metricsEvents <- resolvedEvt:
					default:
						logger.Warn("metrics events channel full, dropping event")
					}
				case <-ctx.Done():
					return
				}
			}
		}()

		tuiCfg := tui.DefaultConfig()
		tuiCfg.RefreshInterval = *tuiRefresh

		appModel := tui.NewAppModel(tuiCfg, dag, oracle, shieldController, reader)
		p := tea.NewProgram(appModel, tea.WithAltScreen())

		if _, err := p.Run(); err != nil {
			logger.Error("TUI execution failed", "error", err)
		}
		stop() // Trigger context cancellation on TUI exit
	} else {
		shieldMode := "DRY-RUN"
		if *shieldArmed {
			shieldMode = "⚠️  ARMED"
		}

		fmt.Println()
		fmt.Println("🛡️  CascadeShield — Full Stack (Phases 0–5)")
		fmt.Printf("    Shield: %s | Oracle: every %s | Metrics: %s\n", shieldMode, *simInterval, *metricsAddr)
		fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
		fmt.Printf("%-10s %-16s %-22s ──► %-22s %-6s\n",
			"TYPE", "PROCESS", "SOURCE SERVICE", "TARGET SERVICE", "RESOLVED")
		fmt.Println("───────────────────────────────────────────────────────────────────────────────")

		for {
			select {
			case evt := <-events:
				resolvedEvt := res.Resolve(evt)

				select {
				case builderEvents <- resolvedEvt:
				default:
					logger.Warn("builder events channel full, dropping event")
				}

				select {
				case metricsEvents <- resolvedEvt:
				default:
					logger.Warn("metrics events channel full, dropping event")
				}

				resolvedStr := "no"
				if resolvedEvt.Resolved {
					resolvedStr = "YES (K8s)"
				}
				fmt.Printf("%-10s %-16s %-22s ──► %-22s %-6s\n",
					evt.Type.String(),
					evt.CommString(),
					truncateStr(resolvedEvt.SourceService, 22),
					truncateStr(resolvedEvt.TargetService, 22),
					resolvedStr,
				)

			case <-ctx.Done():
				fmt.Println()
				if report := oracle.LatestReport(); report != nil {
					logger.Info("Oracle final assessment", "summary", report.Summary())
				}
				logger.Info("Shield final state",
					"active_shedding_edges", shieldController.ActiveSheddingCount(),
				)
				logger.Info("shutting down gracefully...",
					"total_dag_nodes", dag.NodeCount(),
					"total_dag_edges", dag.EdgeCount(),
					"total_metrics_edges", annotator.TrackedEdgeCount(),
				)
				return
			}
		}
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
