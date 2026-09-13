// Gateway — CascadeShield Demo Cluster
//
// The API gateway is the single entry point for all external traffic.
// It fans out to Auth, Orders, and Analytics services.
//
// Dependency topology:
//
//	Gateway → Auth       (every /api/order request validates the JWT)
//	Gateway → Orders     (/api/order retrieves order data)
//	Gateway → Analytics  (/api/analytics serves read-only metrics)
//
// When Auth is slow, EVERY request blocks — making it the highest-risk
// node in the topology for Scenario 3 (Auth Cascade).
//
// Configuration (environment variables):
//
//	GATEWAY_PORT          — TCP port to listen on           (default: 8080)
//	GATEWAY_LATENCY_MS    — Base processing latency in ms   (default: 5)
//	GATEWAY_ERROR_RATE    — Base synthetic error rate 0–1   (default: 0.0)
//	AUTH_URL              — Auth service base URL           (default: http://localhost:8081)
//	ORDERS_URL            — Orders service base URL         (default: http://localhost:8082)
//	ANALYTICS_URL         — Analytics service base URL      (default: http://localhost:8085)
//	CALL_TIMEOUT_MS       — Upstream call timeout in ms     (default: 5000)
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	shared "github.com/abhinay0804/cascadeshield/hack/demo-cluster/shared"
)

// Config holds all gateway tunables. Loaded once from env vars at startup.
type Config struct {
	Port          string
	BaseLatencyMs int
	BaseErrorRate float64
	AuthURL       string
	OrdersURL     string
	AnalyticsURL  string
	CallTimeoutMs int
}

// DefaultConfig returns safe defaults matching the arch spec.
func DefaultConfig() Config {
	return Config{
		Port:          shared.EnvOrDefault("GATEWAY_PORT", "8080"),
		BaseLatencyMs: shared.EnvIntOrDefault("GATEWAY_LATENCY_MS", 5),
		BaseErrorRate: shared.EnvFloat64OrDefault("GATEWAY_ERROR_RATE", 0.0),
		AuthURL:       shared.EnvOrDefault("AUTH_URL", "http://localhost:8081"),
		OrdersURL:     shared.EnvOrDefault("ORDERS_URL", "http://localhost:8082"),
		AnalyticsURL:  shared.EnvOrDefault("ANALYTICS_URL", "http://localhost:8085"),
		CallTimeoutMs: shared.EnvIntOrDefault("CALL_TIMEOUT_MS", 5000),
	}
}

// Server wires together the config, chaos state, and HTTP handlers.
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

// handleOrder fans out to Auth + Orders concurrently, then returns an
// aggregated response. If Auth times out, the entire request stalls —
// demonstrating how a slow Auth cascades to the Gateway.
func (s *Server) handleOrder(w http.ResponseWriter, r *http.Request) {
	// Apply base latency first.
	time.Sleep(time.Duration(s.cfg.BaseLatencyMs) * time.Millisecond)

	// Apply chaos.
	extraLat, isErr := s.chaos.Apply()
	if isErr {
		shared.WriteError(w, http.StatusServiceUnavailable, "gateway synthetic error")
		return
	}
	if extraLat > 0 {
		time.Sleep(extraLat)
	}

	timeout := time.Duration(s.cfg.CallTimeoutMs) * time.Millisecond

	type result struct {
		name string
		body []byte
		code int
		err  error
	}

	// Fan out to Auth and Orders concurrently.
	results := make(chan result, 2)
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		body, code, err := shared.CallService(s.cfg.AuthURL+"/validate", timeout)
		results <- result{name: "auth", body: body, code: code, err: err}
	}()
	go func() {
		defer wg.Done()
		body, code, err := shared.CallService(s.cfg.OrdersURL+"/orders", timeout)
		results <- result{name: "orders", body: body, code: code, err: err}
	}()

	wg.Wait()
	close(results)

	response := map[string]any{}
	overallStatus := http.StatusOK

	for res := range results {
		if res.err != nil {
			slog.Warn("upstream call failed", "service", res.name, "err", res.err)
			response[res.name] = map[string]string{"error": res.err.Error()}
			overallStatus = http.StatusBadGateway
			continue
		}
		if res.code >= 500 {
			overallStatus = http.StatusBadGateway
		}
		var parsed any
		if err := json.Unmarshal(res.body, &parsed); err != nil {
			response[res.name] = string(res.body)
		} else {
			response[res.name] = parsed
		}
	}

	response["gateway"] = "ok"
	shared.WriteJSON(w, overallStatus, response)
}

// handleAnalytics calls the Analytics service for read-only data.
func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	time.Sleep(time.Duration(s.cfg.BaseLatencyMs) * time.Millisecond)

	extraLat, isErr := s.chaos.Apply()
	if isErr {
		shared.WriteError(w, http.StatusServiceUnavailable, "gateway synthetic error")
		return
	}
	if extraLat > 0 {
		time.Sleep(extraLat)
	}

	timeout := time.Duration(s.cfg.CallTimeoutMs) * time.Millisecond
	body, code, err := shared.CallService(s.cfg.AnalyticsURL+"/metrics", timeout)
	if err != nil {
		shared.WriteError(w, http.StatusBadGateway, fmt.Sprintf("analytics unreachable: %v", err))
		return
	}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		shared.WriteJSON(w, code, string(body))
		return
	}
	shared.WriteJSON(w, code, parsed)
}

func main() {
	cfg := DefaultConfig()
	srv := NewServer(cfg)

	slog.Info("gateway starting", "port", cfg.Port,
		"auth_url", cfg.AuthURL,
		"orders_url", cfg.OrdersURL,
		"analytics_url", cfg.AnalyticsURL)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", shared.HealthHandler("gateway"))
	mux.HandleFunc("GET /chaos", shared.ChaosHandler(srv.chaos))
	mux.HandleFunc("GET /api/order", srv.handleOrder)
	mux.HandleFunc("GET /api/analytics", srv.handleAnalytics)

	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		slog.Error("gateway exited", "err", err)
	}
}
