// Orders — CascadeShield Demo Cluster
//
// The Orders service is the "critical fan-out" node in the topology.
// It calls BOTH Inventory and Payments for every order request, making
// it highly sensitive to downstream degradation.
//
// Dependency topology:
//
//	Gateway → Orders → Inventory  (check stock before charging)
//	Gateway → Orders → Payments   (charge after stock confirmed)
//
// This service is involved in two chaos scenarios:
//   - Scenario 1 (Payment Slowdown): payments latency spikes → orders
//     goroutines pile up waiting for payments → thread pool exhaustion
//   - Scenario 2 (Inventory Crash): inventory returns 503 → orders retries
//     amplify load 3x on an already-degraded inventory
//
// Retry logic is intentionally naïve (up to MaxRetries with no backoff)
// to demonstrate how well-intentioned retries amplify load on a failing
// upstream — the exact behaviour CascadeShield detects and cuts off.
//
// Configuration (environment variables):
//
//	ORDERS_PORT          — TCP port to listen on           (default: 8082)
//	ORDERS_LATENCY_MS    — Base processing latency ms      (default: 30)
//	ORDERS_ERROR_RATE    — Base synthetic error rate 0–1   (default: 0.0)
//	INVENTORY_URL        — Inventory service base URL      (default: http://localhost:8083)
//	PAYMENTS_URL         — Payments service base URL       (default: http://localhost:8084)
//	CALL_TIMEOUT_MS      — Upstream call timeout ms        (default: 3000)
//	MAX_RETRIES          — Max retries on upstream failure (default: 3)
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	shared "github.com/abhinay0804/cascadeshield/hack/demo-cluster/shared"
)

// Config holds all orders service tunables.
type Config struct {
	Port          string
	BaseLatencyMs int
	BaseErrorRate float64
	InventoryURL  string
	PaymentsURL   string
	CallTimeoutMs int
	MaxRetries    int
}

// DefaultConfig returns safe defaults matching the arch spec (30ms latency).
func DefaultConfig() Config {
	return Config{
		Port:          shared.EnvOrDefault("ORDERS_PORT", "8082"),
		BaseLatencyMs: shared.EnvIntOrDefault("ORDERS_LATENCY_MS", 30),
		BaseErrorRate: shared.EnvFloat64OrDefault("ORDERS_ERROR_RATE", 0.0),
		InventoryURL:  shared.EnvOrDefault("INVENTORY_URL", "http://localhost:8083"),
		PaymentsURL:   shared.EnvOrDefault("PAYMENTS_URL", "http://localhost:8084"),
		CallTimeoutMs: shared.EnvIntOrDefault("CALL_TIMEOUT_MS", 3000),
		MaxRetries:    shared.EnvIntOrDefault("MAX_RETRIES", 3),
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

// callWithRetry calls a URL up to maxRetries times.
// Retries on 5xx or network error — demonstrating retry amplification
// when inventory is failing (Scenario 2).
func (s *Server) callWithRetry(url string, attempt int) ([]byte, int, error) {
	timeout := time.Duration(s.cfg.CallTimeoutMs) * time.Millisecond
	for i := range s.cfg.MaxRetries {
		body, code, err := shared.CallService(url, timeout)
		if err == nil && code < 500 {
			return body, code, nil
		}
		slog.Warn("upstream call failed, retrying",
			"url", url, "attempt", i+1, "max", s.cfg.MaxRetries,
			"code", code, "err", err)
		time.Sleep(10 * time.Millisecond) // minimal back-off (intentionally small for demo)
		_ = attempt
	}
	return nil, 503, fmt.Errorf("all %d retries exhausted for %s", s.cfg.MaxRetries, url)
}

// handleOrders is the primary business endpoint.
// It fans out to Inventory and Payments concurrently, then aggregates.
func (s *Server) handleOrders(w http.ResponseWriter, r *http.Request) {
	// 1. Base latency.
	time.Sleep(time.Duration(s.cfg.BaseLatencyMs) * time.Millisecond)

	// 2. Chaos.
	extraLat, isErr := s.chaos.Apply()
	if isErr {
		shared.WriteError(w, http.StatusServiceUnavailable, "orders synthetic error")
		return
	}
	if extraLat > 0 {
		time.Sleep(extraLat)
	}

	// 3. Base error rate.
	if s.cfg.BaseErrorRate > 0 && rand.Float64() < s.cfg.BaseErrorRate {
		shared.WriteError(w, http.StatusServiceUnavailable, "orders service degraded")
		return
	}

	type result struct {
		name string
		body []byte
		code int
		err  error
	}

	// 4. Fan out to Inventory + Payments concurrently (both calls must succeed for an order).
	results := make(chan result, 2)
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		body, code, err := s.callWithRetry(s.cfg.InventoryURL+"/stock", 0)
		results <- result{name: "inventory", body: body, code: code, err: err}
	}()
	go func() {
		defer wg.Done()
		body, code, err := s.callWithRetry(s.cfg.PaymentsURL+"/charge", 0)
		results <- result{name: "payments", body: body, code: code, err: err}
	}()

	wg.Wait()
	close(results)

	response := map[string]any{
		"order_id": fmt.Sprintf("ord-%08d", rand.IntN(99999999)+1),
		"status":   "created",
	}
	overallStatus := http.StatusCreated

	for res := range results {
		if res.err != nil {
			slog.Warn("downstream dependency failed", "service", res.name, "err", res.err)
			response["status"] = "failed"
			response[res.name] = map[string]string{"error": res.err.Error()}
			overallStatus = http.StatusServiceUnavailable
			continue
		}
		if res.code >= 500 {
			overallStatus = http.StatusServiceUnavailable
		}
		var parsed any
		if err := json.Unmarshal(res.body, &parsed); err != nil {
			response[res.name] = string(res.body)
		} else {
			response[res.name] = parsed
		}
	}

	shared.WriteJSON(w, overallStatus, response)
}

func main() {
	cfg := DefaultConfig()
	srv := NewServer(cfg)

	slog.Info("orders service starting",
		"port", cfg.Port,
		"base_latency_ms", cfg.BaseLatencyMs,
		"inventory_url", cfg.InventoryURL,
		"payments_url", cfg.PaymentsURL,
		"max_retries", cfg.MaxRetries)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", shared.HealthHandler("orders"))
	mux.HandleFunc("GET /chaos", shared.ChaosHandler(srv.chaos))
	mux.HandleFunc("GET /orders", srv.handleOrders)
	mux.HandleFunc("POST /orders", srv.handleOrders)

	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		slog.Error("orders service exited", "err", err)
	}
}
