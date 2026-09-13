package resolver

import (
	"time"
)

// Config holds configuration parameters for the Identity Resolver.
// ZERO HARDCODING POLICY: All resolution tunables, cache sizes, TTLs, and fallback
// options must be explicitly configured here with sane defaults provided by DefaultConfig().
type Config struct {
	// ProcRoot is the path to the /proc filesystem (default: "/proc").
	// Customizable for containerized execution or testing with mock filesystems.
	ProcRoot string

	// KubeconfigPath is the explicit path to a kubeconfig file.
	// If empty (""), in-cluster configuration or standard ~/.kube/config auto-detection is used.
	KubeconfigPath string

	// KubernetesEnabled toggles K8s pod/service identity resolution.
	// If false, the resolver falls back to host process names and IP:port strings.
	KubernetesEnabled bool

	// FallbackToComm determines whether non-K8s processes use their binary comm name (e.g. "nginx", "curl").
	FallbackToComm bool

	// PIDCacheTTL is the maximum time a PID -> ContainerID mapping remains valid.
	PIDCacheTTL time.Duration

	// PIDCacheCapacity is the maximum number of items in the PID LRU cache.
	PIDCacheCapacity int

	// ContainerCacheTTL is the maximum time a ContainerID -> Pod mapping remains valid.
	ContainerCacheTTL time.Duration

	// ContainerCacheCapacity is the maximum number of items in the Container LRU cache.
	ContainerCacheCapacity int

	// ServiceCacheTTL is the maximum time a Pod -> Service mapping remains valid.
	ServiceCacheTTL time.Duration

	// ServiceCacheCapacity is the maximum number of items in the Service LRU cache.
	ServiceCacheCapacity int

	// IPCacheTTL is the maximum time an IP -> Pod/Service mapping remains valid.
	IPCacheTTL time.Duration

	// IPCacheCapacity is the maximum number of items in the IP LRU cache.
	IPCacheCapacity int

	// LogStatsInterval is the interval at which cache hit/miss statistics are logged.
	LogStatsInterval time.Duration
}

// DefaultConfig returns a Config struct initialized with production-tested default values.
func DefaultConfig() Config {
	return Config{
		ProcRoot:               "/proc",
		KubeconfigPath:         "",
		KubernetesEnabled:      true,
		FallbackToComm:         true,
		PIDCacheTTL:            60 * time.Second,
		PIDCacheCapacity:       4096,
		ContainerCacheTTL:      30 * time.Second,
		ContainerCacheCapacity: 2048,
		ServiceCacheTTL:        120 * time.Second,
		ServiceCacheCapacity:   1024,
		IPCacheTTL:             60 * time.Second,
		IPCacheCapacity:        2048,
		LogStatsInterval:       30 * time.Second,
	}
}
