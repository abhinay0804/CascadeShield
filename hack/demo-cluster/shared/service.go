// Package shared provides common HTTP handler utilities shared across all
// CascadeShield demo microservices (gateway, auth, orders, inventory, payments).
//
// Every demo service includes:
//   - /health endpoint — liveness probe for K8s/Docker health checks
//   - /chaos endpoint  — dynamic latency + error-rate injection at runtime
//   - Config struct    — zero-hardcoding policy: every tunable comes from env vars
package shared

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// ChaosState holds the currently injected chaos parameters.
// Protected by a RWMutex because the /chaos endpoint writes while
// service handlers read on every request.
type ChaosState struct {
	mu        sync.RWMutex
	LatencyMs int     // Extra milliseconds to sleep on every request
	ErrorRate float64 // Fraction of requests that return 503 (0.0–1.0)
}

// Apply sleeps for the injected latency and decides whether to return an error.
// Returns true if this request should be a synthetic failure.
func (c *ChaosState) Apply() (extraLatency time.Duration, isError bool) {
	c.mu.RLock()
	lat := c.LatencyMs
	rate := c.ErrorRate
	c.mu.RUnlock()

	if lat > 0 {
		extraLatency = time.Duration(lat) * time.Millisecond
	}
	if rate > 0 {
		isError = rand.Float64() < rate
	}
	return
}

// Set atomically updates the chaos state.
func (c *ChaosState) Set(latencyMs int, errorRate float64) {
	c.mu.Lock()
	c.LatencyMs = latencyMs
	c.ErrorRate = errorRate
	c.mu.Unlock()
}

// Reset zeroes out all injected chaos.
func (c *ChaosState) Reset() {
	c.Set(0, 0.0)
}

// ChaosHandler returns an http.HandlerFunc for the /chaos endpoint.
//
// Query params:
//
//	?latency_ms=300        — add 300ms to every request
//	?error_rate=0.8        — fail 80% of requests with 503
//	?reset=true            — clear all chaos
//
// Example:
//
//	curl "localhost:8084/chaos?latency_ms=400&error_rate=0.2"
//	curl "localhost:8084/chaos?reset=true"
func ChaosHandler(state *ChaosState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		if q.Get("reset") == "true" {
			state.Reset()
			slog.Info("chaos reset")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
			return
		}

		latMs := state.LatencyMs
		errRate := state.ErrorRate

		if v := q.Get("latency_ms"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				http.Error(w, "latency_ms must be a non-negative integer", http.StatusBadRequest)
				return
			}
			latMs = n
		}
		if v := q.Get("error_rate"); v != "" {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f < 0 || f > 1 {
				http.Error(w, "error_rate must be between 0.0 and 1.0", http.StatusBadRequest)
				return
			}
			errRate = f
		}

		state.Set(latMs, errRate)
		slog.Info("chaos applied", "latency_ms", latMs, "error_rate", errRate)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"latency_ms": latMs,
			"error_rate": errRate,
		})
	}
}

// HealthHandler returns an http.HandlerFunc for the /health endpoint.
// Returns 200 {"status":"ok","service":name} so K8s readiness probes pass.
func HealthHandler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": serviceName,
		})
	}
}

// EnvOrDefault reads an environment variable, returning defaultVal if unset.
func EnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// EnvIntOrDefault reads an integer env var, returning defaultVal if unset or invalid.
func EnvIntOrDefault(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return defaultVal
}

// EnvFloat64OrDefault reads a float64 env var, returning defaultVal if unset or invalid.
func EnvFloat64OrDefault(key string, defaultVal float64) float64 {
	if v := os.Getenv(key); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
	}
	return defaultVal
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to write JSON response", "err", err)
	}
}

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, statusCode int, msg string) {
	WriteJSON(w, statusCode, map[string]string{"error": msg})
}

// CallService performs an HTTP GET to the given URL with a timeout.
// Returns the response body bytes or an error.
func CallService(url string, timeout time.Duration) ([]byte, int, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, 0, fmt.Errorf("call to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf, resp.StatusCode, nil
}
