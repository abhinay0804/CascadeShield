// Auth — CascadeShield Demo Cluster
//
// The Auth service simulates JWT validation. Every gateway request goes
// through Auth first, making it a critical single point of failure.
//
// Dependency topology:
//
//	Gateway → Auth  (no downstream deps — Auth is a leaf in the other direction)
//
// This service is the target of Scenario 3 (Auth Cascade):
// injecting 2s latency here blocks every single gateway request because
// the gateway waits for auth before returning to the client.
//
// Configuration (environment variables):
//
//	AUTH_PORT          — TCP port to listen on           (default: 8081)
//	AUTH_LATENCY_MS    — Base JWT validation latency ms  (default: 10)
//	AUTH_ERROR_RATE    — Base synthetic error rate 0–1   (default: 0.0)
package main

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	shared "github.com/abhinay0804/cascadeshield/hack/demo-cluster/shared"
)

// Config holds all auth service tunables.
type Config struct {
	Port          string
	BaseLatencyMs int
	BaseErrorRate float64
}

// DefaultConfig returns safe defaults matching the arch spec (10ms latency).
func DefaultConfig() Config {
	return Config{
		Port:          shared.EnvOrDefault("AUTH_PORT", "8081"),
		BaseLatencyMs: shared.EnvIntOrDefault("AUTH_LATENCY_MS", 10),
		BaseErrorRate: shared.EnvFloat64OrDefault("AUTH_ERROR_RATE", 0.0),
	}
}

// Server wires together config, chaos state, and HTTP handlers.
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

// handleValidate simulates JWT token validation.
// Returns a user identity on success, 401 on synthetic auth failure.
//
// The critical insight for the demo: when chaos injects 2000ms latency here,
// the gateway's goroutine waiting on this call blocks for 2s, exhausting
// gateway connection pool capacity and cascading to all callers.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	// 1. Apply base service latency (simulates real JWT decode + HMAC verify).
	time.Sleep(time.Duration(s.cfg.BaseLatencyMs) * time.Millisecond)

	// 2. Apply chaos (e.g., Scenario 3 injects 2000ms here).
	extraLat, isErr := s.chaos.Apply()
	if isErr {
		shared.WriteError(w, http.StatusServiceUnavailable, "auth service unavailable")
		return
	}
	if extraLat > 0 {
		time.Sleep(extraLat)
	}

	// 3. Synthetic "auth failure" for base error rate.
	if s.cfg.BaseErrorRate > 0 && rand.Float64() < s.cfg.BaseErrorRate {
		shared.WriteError(w, http.StatusUnauthorized, "token validation failed")
		return
	}

	// 4. Return a fake user identity.
	userID := fmt.Sprintf("user-%04d", rand.IntN(9999)+1)
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"valid":   true,
		"user_id": userID,
		"scope":   []string{"read:orders", "write:orders"},
		"exp":     time.Now().Add(1 * time.Hour).Unix(),
	})
}

func main() {
	cfg := DefaultConfig()
	srv := NewServer(cfg)

	slog.Info("auth service starting",
		"port", cfg.Port,
		"base_latency_ms", cfg.BaseLatencyMs,
		"base_error_rate", cfg.BaseErrorRate)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", shared.HealthHandler("auth"))
	mux.HandleFunc("GET /chaos", shared.ChaosHandler(srv.chaos))
	mux.HandleFunc("GET /validate", srv.handleValidate)
	mux.HandleFunc("POST /validate", srv.handleValidate)

	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		slog.Error("auth service exited", "err", err)
	}
}
