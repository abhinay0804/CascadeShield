// Inventory — CascadeShield Demo Cluster
//
// The Inventory service checks product stock levels.
// It is a leaf node in the dependency graph (no downstream deps).
//
// Dependency topology:
//
//	Orders → Inventory  (no further deps)
//
// This service is the primary target for Scenario 2 (Inventory Crash):
// inject 80% error rate → Orders retries amplify load 3x on Inventory.
// CascadeShield should detect the amplified error rate and shed
// Orders → Inventory traffic.
//
// Configuration (environment variables):
//
//	INVENTORY_PORT       — TCP port to listen on          (default: 8083)
//	INVENTORY_LATENCY_MS — Base stock-check latency ms    (default: 20)
//	INVENTORY_ERROR_RATE — Base synthetic error rate 0–1  (default: 0.0)
//	STOCK_COUNT          — Items in fake inventory        (default: 500)
package main

import (
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	shared "github.com/abhinay0804/cascadeshield/hack/demo-cluster/shared"
)

// Config holds all inventory service tunables.
type Config struct {
	Port          string
	BaseLatencyMs int
	BaseErrorRate float64
	StockCount    int
}

// DefaultConfig returns safe defaults matching the arch spec (20ms latency).
func DefaultConfig() Config {
	return Config{
		Port:          shared.EnvOrDefault("INVENTORY_PORT", "8083"),
		BaseLatencyMs: shared.EnvIntOrDefault("INVENTORY_LATENCY_MS", 20),
		BaseErrorRate: shared.EnvFloat64OrDefault("INVENTORY_ERROR_RATE", 0.0),
		StockCount:    shared.EnvIntOrDefault("STOCK_COUNT", 500),
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

// handleStock returns current inventory levels.
//
// When chaos injects 80% error rate (Scenario 2), Orders' retry logic fires
// up to MaxRetries times per request — amplifying load on Inventory by 3x+.
// This is the exact retry amplification pattern CascadeShield must detect.
func (s *Server) handleStock(w http.ResponseWriter, r *http.Request) {
	// 1. Base latency (simulates DB read).
	time.Sleep(time.Duration(s.cfg.BaseLatencyMs) * time.Millisecond)

	// 2. Base error rate (static, loaded from env).
	if s.cfg.BaseErrorRate > 0 && rand.Float64() < s.cfg.BaseErrorRate {
		shared.WriteError(w, http.StatusServiceUnavailable, "inventory: storage backend degraded")
		return
	}

	// 3. Dynamic chaos injection (set via /chaos endpoint).
	extraLat, isErr := s.chaos.Apply()
	if isErr {
		shared.WriteError(w, http.StatusServiceUnavailable, "inventory: synthetic failure")
		return
	}
	if extraLat > 0 {
		time.Sleep(extraLat)
	}

	// 4. Return a plausible stock response.
	available := s.cfg.StockCount - rand.IntN(50)
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"available": available > 0,
		"count":     available,
		"sku":       "SKU-DEMO-001",
		"warehouse": "us-east-1",
	})
}

func main() {
	cfg := DefaultConfig()
	srv := NewServer(cfg)

	slog.Info("inventory service starting",
		"port", cfg.Port,
		"base_latency_ms", cfg.BaseLatencyMs,
		"stock_count", cfg.StockCount)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", shared.HealthHandler("inventory"))
	mux.HandleFunc("GET /chaos", shared.ChaosHandler(srv.chaos))
	mux.HandleFunc("GET /stock", srv.handleStock)

	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		slog.Error("inventory service exited", "err", err)
	}
}
