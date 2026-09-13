// Analytics — CascadeShield Demo Cluster
//
// The Analytics service processes read-only metrics queries from Gateway.
//
// Dependency topology:
//
//	Gateway → Analytics (no further deps)
//
// Configuration (environment variables):
//
//	ANALYTICS_PORT       — TCP port to listen on          (default: 8085)
//	ANALYTICS_LATENCY_MS — Base response latency ms       (default: 50)
//	ANALYTICS_ERROR_RATE — Base synthetic error rate 0–1  (default: 0.0)
package main

import (
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	shared "github.com/abhinay0804/cascadeshield/hack/demo-cluster/shared"
)

type Config struct {
	Port          string
	BaseLatencyMs int
	BaseErrorRate float64
}

func DefaultConfig() Config {
	return Config{
		Port:          shared.EnvOrDefault("ANALYTICS_PORT", "8085"),
		BaseLatencyMs: shared.EnvIntOrDefault("ANALYTICS_LATENCY_MS", 50),
		BaseErrorRate: shared.EnvFloat64OrDefault("ANALYTICS_ERROR_RATE", 0.0),
	}
}

type Server struct {
	cfg   Config
	chaos *shared.ChaosState
}

func NewServer(cfg Config) *Server {
	return &Server{
		cfg:   cfg,
		chaos: &shared.ChaosState{},
	}
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	time.Sleep(time.Duration(s.cfg.BaseLatencyMs) * time.Millisecond)

	if s.cfg.BaseErrorRate > 0 && rand.Float64() < s.cfg.BaseErrorRate {
		shared.WriteError(w, http.StatusServiceUnavailable, "analytics pipeline error")
		return
	}

	extraLat, isErr := s.chaos.Apply()
	if isErr {
		shared.WriteError(w, http.StatusServiceUnavailable, "analytics synthetic error")
		return
	}
	if extraLat > 0 {
		time.Sleep(extraLat)
	}

	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"active_users": rand.IntN(1000) + 200,
		"events_sec":   rand.IntN(5000) + 1000,
		"status":       "healthy",
	})
}

func main() {
	cfg := DefaultConfig()
	srv := NewServer(cfg)

	slog.Info("analytics service starting", "port", cfg.Port, "base_latency_ms", cfg.BaseLatencyMs)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", shared.HealthHandler("analytics"))
	mux.HandleFunc("GET /chaos", shared.ChaosHandler(srv.chaos))
	mux.HandleFunc("GET /metrics", srv.handleMetrics)

	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		slog.Error("analytics service exited", "err", err)
	}
}
