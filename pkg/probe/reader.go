package probe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/cilium/ebpf/ringbuf"
)

// Reader consumes events from the BPF ring buffer and sends decoded
// Event structs to a Go channel for downstream processing.
//
// The reader runs in its own goroutine and respects context cancellation
// for graceful shutdown. When the context is cancelled, it closes the
// ring buffer reader (which unblocks any pending Read() call) and returns.
type Reader struct {
	probe       *Probe
	logger      *slog.Logger
	totalEvents atomic.Uint64
}

// NewReader creates a new event reader bound to the given probe.
func NewReader(probe *Probe, logger *slog.Logger) *Reader {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reader{
		probe:  probe,
		logger: logger,
	}
}

// Run starts reading events from the ring buffer and sending them to the
// provided channel. It blocks until the context is cancelled.
//
// This function is designed to be called in a goroutine:
//
//	events := make(chan probe.Event, cfg.EventChannelSize)
//	go reader.Run(ctx, events)
//
// How ring buffer reading works:
//   1. rd.Read() blocks until an event is available in the ring buffer
//   2. When an event arrives, we get raw bytes (matching struct tcp_event)
//   3. We decode the bytes into a Go Event struct using binary.Read()
//   4. We send the decoded event to the channel
//   5. If the context is cancelled, we close the reader and return
//
// Events are NOT dropped by the reader — if the channel is full, the reader
// blocks (backpressure). The ring buffer continues to buffer kernel events
// during this time. If the ring buffer ALSO fills up, the kernel drops events
// (counted in eBPF metrics, not here).
func (r *Reader) Run(ctx context.Context, events chan<- Event) {
	rd, err := r.probe.NewReader()
	if err != nil {
		r.logger.Error("failed to create ring buffer reader", "error", err)
		return
	}

	// When the context is cancelled, close the ring buffer reader.
	// This unblocks any pending rd.Read() call with an error, causing
	// the loop below to exit cleanly.
	//
	// We use a separate goroutine for this because rd.Read() is blocking
	// and we can't check ctx.Done() while blocked in Read().
	go func() {
		<-ctx.Done()
		rd.Close()
	}()

	r.logger.Info("ring buffer reader started, consuming events...")

	var eventCount uint64
	var errCount uint64

	for {
		// Read the next event from the ring buffer.
		// This blocks until an event is available or the reader is closed.
		record, err := rd.Read()
		if err != nil {
			// When we close the reader (on shutdown), Read() returns this error.
			// This is expected and not an actual error.
			if errors.Is(err, ringbuf.ErrClosed) {
				r.logger.Info("ring buffer reader closed",
					"events_read", eventCount,
					"decode_errors", errCount,
				)
				return
			}
			r.logger.Warn("error reading from ring buffer", "error", err)
			errCount++
			continue
		}

		// Decode the raw bytes into a Go Event struct.
		//
		// We use our manual DecodeEvent() function instead of binary.Read()
		// because Go doesn't support packed structs. The C struct is 74 bytes
		// (packed), but Go's Event struct is 80 bytes (with alignment padding).
		// DecodeEvent reads each field at its exact byte offset in the packed layout.
		evt, err := DecodeEvent(record.RawSample)
		if err != nil {
			r.logger.Warn("failed to decode event",
				"error", err,
				"raw_size", len(record.RawSample),
				"expected_size", WireSize,
			)
			errCount++
			continue
		}

		eventCount++
		r.totalEvents.Add(1)

		// Send the decoded event to the channel.
		// If the channel is full, this blocks (backpressure).
		select {
		case events <- evt:
			// Event sent successfully
		case <-ctx.Done():
			// Context cancelled while trying to send — shut down
			return
		}
	}
}

// TotalEvents returns the cumulative number of events read from the ring buffer.
func (r *Reader) TotalEvents() uint64 {
	return r.totalEvents.Load()
}

// FormatEvent returns a human-readable string for an event.
// This is a convenience function used by the main event printer.
func FormatEvent(evt Event) string {
	return fmt.Sprintf("%-12s %-16s pid=%-6d %15s:%-5d → %15s:%-5d",
		evt.Type,
		evt.CommString(),
		evt.PID,
		evt.SourceIP(), evt.SPort,
		evt.DestIP(), evt.DPort,
	)
}
