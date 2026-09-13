package probe

// Config holds all configurable parameters for the eBPF probe subsystem.
//
// ZERO HARDCODING POLICY: Every tunable value lives here with a sensible default.
// Overrides come from CLI flags → environment variables → config file.
//
// Why a separate Config struct?
//   In Go, it's idiomatic to pass configuration as a struct rather than
//   individual parameters. This makes it easy to add new options without
//   changing function signatures, and lets callers see all options at a glance.
type Config struct {
	// RingBufferSize is the size of the BPF ring buffer in bytes.
	// Larger = fewer dropped events under high load, but more memory.
	//
	// Must be a power of 2 and a multiple of the page size (4096).
	// 256KB handles ~50K events/s comfortably. Increase for high-traffic nodes.
	//
	// Default: 256 * 1024 (256 KB)
	RingBufferSize int

	// EventChannelSize is the buffer size of the Go channel that receives
	// decoded events from the ring buffer reader.
	//
	// A larger channel buffer absorbs short bursts without backpressure.
	// If the channel fills up, the reader blocks (ring buffer keeps buffering).
	//
	// Default: 4096
	EventChannelSize int
}

// DefaultConfig returns a Config with sensible defaults.
//
// This is the single source of truth for default values. Any code that
// creates a Config without specifying all fields gets these defaults.
func DefaultConfig() Config {
	return Config{
		RingBufferSize:   256 * 1024, // 256 KB — good for moderate traffic
		EventChannelSize: 4096,       // Buffer 4K events before backpressure
	}
}
