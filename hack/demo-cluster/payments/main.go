// Payments — CascadeShield Demo Cluster
//
// The Payments service processes credit card transactions.
// It is a leaf node in the dependency graph with higher base latency (100ms).
//
// Dependency topology:
//
//	Orders → Payments (no further deps)
//
// This service is targeted by:
//   - Scenario 1 (Payment Slowdown): latency +300ms causes thread pool exhaustion in Orders
//   - Scenario 4 (Slow Burn): latency increases gradually 10ms/min to test preemptive shedding
//
// Configuration (environment variables):
//
//	PAYMENTS_PORT       — TCP port to listen on          (default: 8084)
//	PAYMENTS_LATENCY_MS — Base transaction latency ms    (default: 100)
//	PAYMENTS_ERROR_RATE — Base synthetic error rate 0–1  (default: 0.0)
package main

import (
	"fmt"
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
		Port:          shared.EnvOrDefault("PAYMENTS_PORT", "8084"),
		BaseLatencyMs: shared.EnvIntOrDefault("PAYMENTS_LATENCY_MS", 100),
		BaseErrorRate: shared.EnvFloat64OrDefault("PAYMENTS_ERROR_RATE", 0.0),
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

func (s *Server) handleCharge(w http.ResponseWriter, r *http.Request) {
	time.Sleep(time.Duration(s.cfg.BaseLatencyMs) * time.Millisecond)

	if s.cfg.BaseErrorRate > 0 && rand.Float64() < s.cfg.BaseErrorRate {
		shared.WriteError(w, http.StatusServiceUnavailable, "payments gateway unavailable")
		return
	}

	extraLat, isErr := s.chaos.Apply()
	if isErr {
		shared.WriteError(w, http.StatusServiceUnavailable, "payments: synthetic failure")
		return
	}
	if extraLat > 0 {
		time.Sleep(extraLat)
	}

	txID := fmt.Sprintf("tx-%08d", rand.IntN(99999999)+1)
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"charged":        true,
		"tx_id":          txID,
		"amount_cents":   rand.IntN(10000) + 500,
		"currency":       "USD",
		"payment_method": "visa_ending_4242",
	})
}

func main() {
	cfg := DefaultConfig()
	srv := NewServer(cfg)

	slog.Info("payments service starting",
		"port", cfg.Port,
		"base_latency_ms", cfg.BaseLatencyMs)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", shared.HealthHandler("payments"))
	mux.HandleFunc("GET /chaos", shared.ChaosHandler(srv.chaos))
	mux.HandleFunc("POST /charge", srv.handleCharge)
	mux.HandleFunc("GET /charge", srv.handleCharge)

	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		slog.Error("payments service exited", "err", err)
	}
}
