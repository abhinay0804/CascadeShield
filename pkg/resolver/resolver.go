package resolver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"

	"github.com/abhinay0804/cascadeshield/pkg/probe"
)

// Resolver orchestrates Linux /proc inspection, Kubernetes Informers, and LRU caching
// to resolve raw eBPF socket events into high-level service identities.
type Resolver struct {
	config       Config
	procReader   *ProcReader
	kubeResolver *KubeResolver
	logger       *slog.Logger

	// LRU Caches for fast O(1) resolution lookups
	pidCache       *Cache[uint32, ServiceIdentity]
	containerCache *Cache[string, ServiceIdentity]
	ipCache        *Cache[string, ServiceIdentity]

	// Metrics
	resolvedEvents uint64
	totalEvents    uint64
}

// NewResolver initializes an Identity Resolver instance based on the provided configuration.
func NewResolver(cfg Config, logger *slog.Logger) (*Resolver, error) {
	if logger == nil {
		logger = slog.Default()
	}

	procReader := NewProcReader(cfg.ProcRoot)

	pidCache := NewCache[uint32, ServiceIdentity](cfg.PIDCacheCapacity, cfg.PIDCacheTTL)
	containerCache := NewCache[string, ServiceIdentity](cfg.ContainerCacheCapacity, cfg.ContainerCacheTTL)
	ipCache := NewCache[string, ServiceIdentity](cfg.IPCacheCapacity, cfg.IPCacheTTL)

	var kubeResolver *KubeResolver
	if cfg.KubernetesEnabled {
		var err error
		kubeResolver, err = NewKubeResolver(cfg.KubeconfigPath, cfg.ServiceCacheTTL, logger)
		if err != nil {
			logger.Warn("Kubernetes resolver initialization failed, falling back to bare-metal mode", "error", err)
			kubeResolver = nil
		}
	}

	return &Resolver{
		config:         cfg,
		procReader:     procReader,
		kubeResolver:   kubeResolver,
		logger:         logger,
		pidCache:       pidCache,
		containerCache: containerCache,
		ipCache:        ipCache,
	}, nil
}

// Start begins Kubernetes background informers if enabled.
func (r *Resolver) Start(ctx context.Context) error {
	if r.kubeResolver != nil {
		if err := r.kubeResolver.Start(ctx); err != nil {
			r.logger.Warn("failed to start Kubernetes informers, falling back to host resolution", "error", err)
		}
	}
	return nil
}

// Stop cleans up background resources and informers.
func (r *Resolver) Stop() {
	if r.kubeResolver != nil {
		r.kubeResolver.Stop()
	}
}

// Resolve processes a raw kernel eBPF event and enriches it with source and target service identities.
func (r *Resolver) Resolve(evt probe.Event) ResolvedEvent {
	atomic.AddUint64(&r.totalEvents, 1)

	var localIdent, remoteIdent ServiceIdentity

	// 1. Determine local (PID side) and remote (IP side) roles based on event type
	switch evt.Type {
	case probe.EventConnect:
		// Local process called connect() -> Source = Local (PID), Target = Remote (DAddr:DPort)
		localIdent = r.resolvePID(evt.PID)
		remoteIdent = r.resolveIP(intToIP(evt.DAddr), evt.DPort)

	case probe.EventAccept:
		// Local process accepted socket -> Source = Remote (SAddr:SPort), Target = Local (PID)
		localIdent = r.resolvePID(evt.PID)
		remoteIdent = r.resolveIP(intToIP(evt.SAddr), evt.SPort)

		// For ACCEPT, local is target and remote is source
		return ResolvedEvent{
			Raw:           evt,
			SourceService: remoteIdent.Name,
			TargetService: localIdent.Name,
			SourcePod:     remoteIdent.Pod,
			TargetPod:     localIdent.Pod,
			Namespace:     chooseNamespace(remoteIdent.Namespace, localIdent.Namespace),
			Resolved:      localIdent.IsResolved || remoteIdent.IsResolved,
		}

	case probe.EventClose, probe.EventRetransmit:
		// Default: Source = Local (PID), Target = Remote (DAddr:DPort)
		localIdent = r.resolvePID(evt.PID)
		remoteIdent = r.resolveIP(intToIP(evt.DAddr), evt.DPort)
	}

	isResolved := localIdent.IsResolved || remoteIdent.IsResolved
	if isResolved {
		atomic.AddUint64(&r.resolvedEvents, 1)
	}

	return ResolvedEvent{
		Raw:           evt,
		SourceService: localIdent.Name,
		TargetService: remoteIdent.Name,
		SourcePod:     localIdent.Pod,
		TargetPod:     remoteIdent.Pod,
		Namespace:     chooseNamespace(localIdent.Namespace, remoteIdent.Namespace),
		Resolved:      isResolved,
	}
}

// resolvePID maps a process ID to a ServiceIdentity (proc -> cgroup -> containerID -> K8s Pod -> Service).
func (r *Resolver) resolvePID(pid uint32) ServiceIdentity {
	if pid == 0 {
		return ServiceIdentity{Name: "kernel", Namespace: "host"}
	}

	// 1. Check PID Cache
	if ident, ok := r.pidCache.Get(pid); ok {
		return ident
	}

	// 2. Read container ID from /proc/<pid>/cgroup
	containerID, err := r.procReader.ReadContainerID(pid)
	if err == nil && containerID != "" {
		// Check container cache
		if ident, ok := r.containerCache.Get(containerID); ok {
			r.pidCache.Set(pid, ident)
			return ident
		}

		// Resolve container ID via K8s Informers if available
		if r.kubeResolver != nil {
			podName, namespace, err := r.kubeResolver.ResolveContainerID(containerID)
			if err == nil {
				svcName, err := r.kubeResolver.ResolvePodToService(podName, namespace)
				if err != nil || svcName == "" {
					svcName = podName
				}

				ident := ServiceIdentity{
					Name:       svcName,
					Pod:        podName,
					Namespace:  namespace,
					IsResolved: true,
				}
				r.containerCache.Set(containerID, ident)
				r.pidCache.Set(pid, ident)
				return ident
			}
		}
	}

	// 3. Fallback to process command name (/proc/<pid>/comm) for bare metal
	comm, err := r.procReader.ReadComm(pid)
	if err != nil || comm == "" {
		comm = fmt.Sprintf("pid-%d", pid)
	}

	ident := ServiceIdentity{
		Name:       comm,
		Namespace:  "host",
		IsResolved: false,
	}
	r.pidCache.Set(pid, ident)
	return ident
}

// resolveIP maps an IP address to a ServiceIdentity via K8s pod IP cache or IP:port fallback.
func (r *Resolver) resolveIP(ipStr string, port uint16) ServiceIdentity {
	if ipStr == "" || ipStr == "0.0.0.0" {
		return ServiceIdentity{Name: "unknown", Namespace: "host"}
	}

	// 1. Check IP Cache
	if ident, ok := r.ipCache.Get(ipStr); ok {
		return ident
	}

	// 2. Try K8s IP lookup
	if r.kubeResolver != nil {
		podName, namespace, err := r.kubeResolver.ResolveIP(ipStr)
		if err == nil {
			svcName, err := r.kubeResolver.ResolvePodToService(podName, namespace)
			if err != nil || svcName == "" {
				svcName = podName
			}
			ident := ServiceIdentity{
				Name:       svcName,
				Pod:        podName,
				Namespace:  namespace,
				IsResolved: true,
			}
			r.ipCache.Set(ipStr, ident)
			return ident
		}
	}

	// 3. Fallback to IP:Port string representation
	formattedName := fmt.Sprintf("%s:%d", ipStr, port)
	ident := ServiceIdentity{
		Name:       formattedName,
		Namespace:  "external",
		IsResolved: false,
	}
	r.ipCache.Set(ipStr, ident)
	return ident
}

// Helper functions:

func intToIP(ipUint uint32) string {
	ip := make(net.IP, 4)
	ip[0] = byte(ipUint)
	ip[1] = byte(ipUint >> 8)
	ip[2] = byte(ipUint >> 16)
	ip[3] = byte(ipUint >> 24)
	return ip.String()
}

func chooseNamespace(ns1, ns2 string) string {
	if ns1 != "" && ns1 != "host" && ns1 != "external" {
		return ns1
	}
	if ns2 != "" && ns2 != "host" && ns2 != "external" {
		return ns2
	}
	return "host"
}
