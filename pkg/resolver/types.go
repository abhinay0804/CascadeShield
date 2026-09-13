package resolver

import (
	"github.com/abhinay0804/cascadeshield/pkg/probe"
)

// ResolvedEvent represents a kernel eBPF event enriched with high-level service identity.
// This is the output of the Identity Resolver and the input to the Graph Builder.
type ResolvedEvent struct {
	// Raw is the original, unmodified eBPF event captured by Phase 1 kernel probes.
	Raw probe.Event

	// SourceService is the resolved name of the initiating service (e.g., "orders" or "curl" or "10.0.1.5:45212").
	SourceService string

	// TargetService is the resolved name of the receiving service (e.g., "payments" or "93.184.216.34:443").
	TargetService string

	// SourcePod is the Kubernetes pod name of the source, if running inside K8s (otherwise empty "").
	SourcePod string

	// TargetPod is the Kubernetes pod name of the target, if running inside K8s (otherwise empty "").
	TargetPod string

	// Namespace is the K8s namespace (or "host" for non-containerized process traffic).
	Namespace string

	// Resolved is true if at least one endpoint was successfully mapped to a K8s Pod/Service.
	Resolved bool
}

// ServiceIdentity encapsulates the resolved identity details of a single network endpoint.
type ServiceIdentity struct {
	// Name is the logical service identity (e.g., "payment-service", "nginx", "192.168.1.100:8080").
	Name string

	// Pod is the Kubernetes pod name backing this endpoint, if resolved.
	Pod string

	// Namespace is the Kubernetes namespace (or "host" / "external").
	Namespace string

	// IsResolved indicates whether Kubernetes metadata was attached to this identity.
	IsResolved bool
}
