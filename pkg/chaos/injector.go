package chaos

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Target maps service names (e.g. "payments") to their base endpoint (e.g. "http://localhost:8084").
type Target struct {
	ServiceName string
	BaseURL     string
}

// Injector manages dynamic chaos injection into target microservices via HTTP endpoints.
type Injector struct {
	cfg       Config
	client    *http.Client
	targets   map[string]string // serviceName -> baseURL
	targetsMu sync.RWMutex
}

// NewInjector constructs a new Injector with the given Config.
func NewInjector(cfg Config) *Injector {
	return &Injector{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
		targets: make(map[string]string),
	}
}

// RegisterTarget adds or updates a service target.
func (i *Injector) RegisterTarget(serviceName, baseURL string) {
	i.targetsMu.Lock()
	defer i.targetsMu.Unlock()
	i.targets[serviceName] = strings.TrimRight(baseURL, "/")
}

// Inject applies extra latency and/or error rate to the named service.
func (i *Injector) Inject(serviceName string, latencyMs int, errorRate float64) error {
	i.targetsMu.RLock()
	baseURL, exists := i.targets[serviceName]
	i.targetsMu.RUnlock()

	if !exists {
		return fmt.Errorf("target service %q not registered in chaos injector", serviceName)
	}

	endpoint := fmt.Sprintf("%s/chaos?latency_ms=%d&error_rate=%.2f", baseURL, latencyMs, errorRate)
	resp, err := i.client.Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to call chaos endpoint for %s: %w", serviceName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chaos injection returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// Reset clears all injected chaos for a single service.
func (i *Injector) Reset(serviceName string) error {
	i.targetsMu.RLock()
	baseURL, exists := i.targets[serviceName]
	i.targetsMu.RUnlock()

	if !exists {
		return fmt.Errorf("target service %q not registered", serviceName)
	}

	endpoint := fmt.Sprintf("%s/chaos?reset=true", baseURL)
	resp, err := i.client.Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to reset chaos for %s: %w", serviceName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chaos reset returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ResetAll resets chaos on all registered target services.
func (i *Injector) ResetAll() map[string]error {
	i.targetsMu.RLock()
	services := make([]string, 0, len(i.targets))
	for svc := range i.targets {
		services = append(services, svc)
	}
	i.targetsMu.RUnlock()

	errs := make(map[string]error)
	for _, svc := range services {
		if err := i.Reset(svc); err != nil {
			errs[svc] = err
		}
	}
	return errs
}

// InjectRaw sends query parameters directly to a raw endpoint (useful for tests or custom scenarios).
func (i *Injector) InjectRaw(rawURL string, params url.Values) (map[string]any, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	u.RawQuery = params.Encode()

	resp, err := i.client.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var res map[string]any
	if err := json.Unmarshal(body, &res); err != nil {
		res = map[string]any{"raw": string(body), "status_code": resp.StatusCode}
	}
	return res, nil
}
