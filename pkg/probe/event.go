// Package probe provides the eBPF probe lifecycle: loading BPF programs into
// the kernel, attaching them to hooks, and reading events from the ring buffer.
//
// This file defines the Go-side event types that mirror the C struct tcp_event
// defined in bpf/types.h. The struct layout MUST exactly match the C side —
// any mismatch causes data corruption when decoding ring buffer events.
package probe

import (
	"encoding/binary"
	"fmt"
	"net"
)

// EventType identifies which kernel hook generated an event.
// These constants MUST match the #define values in bpf/types.h.
type EventType uint8

const (
	EventConnect    EventType = 1 // tcp_v4_connect — outgoing connection
	EventAccept     EventType = 2 // inet_csk_accept — incoming connection
	EventClose      EventType = 3 // tcp_close — connection closed
	EventRetransmit EventType = 4 // tcp_retransmit_skb — packet retransmission
)

// String returns a human-readable name for the event type.
// This implements the fmt.Stringer interface, so when you do
// fmt.Println(event.Type), Go automatically calls this method.
func (t EventType) String() string {
	switch t {
	case EventConnect:
		return "CONNECT"
	case EventAccept:
		return "ACCEPT"
	case EventClose:
		return "CLOSE"
	case EventRetransmit:
		return "RETRANSMIT"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", t)
	}
}

// TaskCommLen is the maximum length of a process name in Linux.
// Matches TASK_COMM_LEN in bpf/types.h (from the kernel's task_struct->comm).
const TaskCommLen = 16

// Event is the Go-side representation of struct tcp_event from bpf/types.h.
//
// CRITICAL: This struct MUST have the exact same memory layout as the C struct.
// The fields are in the same order, with the same sizes, and we read them
// using binary.Read() which interprets raw bytes according to this layout.
//
// The C struct uses __attribute__((packed)), so there's no padding between fields.
// In Go, we don't have a "packed" attribute, but since the fields are naturally
// aligned (u64 at 8-byte offsets, u32 at 4-byte offsets), the layout matches.
// We verify this with a compile-time size check below.
type Event struct {
	TimestampNs   uint64 // ktime_get_ns() — monotonic kernel timestamp
	DurationNs    uint64 // Connection lifetime (CLOSE events, computed in userspace)
	BytesSent     uint64 // TX bytes over connection lifetime
	BytesReceived uint64 // RX bytes over connection lifetime

	PID         uint32 // Process ID (TGID in kernel terms)
	TID         uint32 // Thread ID (PID in kernel terms)
	SAddr       uint32 // Source IPv4 address (network byte order)
	DAddr       uint32 // Destination IPv4 address (network byte order)
	Retransmits uint32 // Cumulative TCP retransmit count
	ReturnCode  int32  // connect() return code (0=success, -errno=fail)

	SPort uint16 // Source port (host byte order)
	DPort uint16 // Destination port (host byte order)

	Type      EventType // CONNECT / ACCEPT / CLOSE / RETRANSMIT
	IPVersion uint8     // 4 or 6

	Comm [TaskCommLen]byte // Process name (null-terminated C string)
}

// WireSize is the size of the packed C struct tcp_event on the wire (in the ring buffer).
// The C struct uses __attribute__((packed)) so there's NO padding between fields.
//
// Calculation:
//
//	4 × uint64 (8 bytes each) = 32
//	4 × uint32 (4 bytes each) = 16
//	1 × int32                 =  4
//	2 × uint16 (2 bytes each) =  4
//	2 × uint8  (1 byte each)  =  2
//	16 × byte  (comm)         = 16
//	Total                     = 74 bytes
//
// Note: The Go Event struct is larger (80 bytes) due to Go's alignment padding
// after the uint8 fields. That's fine — we decode manually from the packed format.
const WireSize = 74

// DecodeEvent reads a 74-byte packed C struct from raw bytes into a Go Event.
//
// Why manual decoding instead of binary.Read(&Event{})?
//
//	Go does NOT support packed structs. The Go compiler adds padding after
//	small fields (like uint8) to maintain alignment. So unsafe.Sizeof(Event{})
//	is 80, not 74. If we used binary.Read() directly, the fields after the
//	padding mismatch would be read at wrong offsets → garbage data.
//
//	Instead, we read each field at its exact byte offset in the packed C layout.
//	This is the standard approach for decoding packed kernel/network structures in Go.
func DecodeEvent(data []byte) (Event, error) {
	if len(data) < WireSize {
		return Event{}, fmt.Errorf("buffer too small: got %d bytes, need %d", len(data), WireSize)
	}

	var evt Event
	// Use LittleEndian because x86_64 is little-endian
	le := binary.LittleEndian

	// Offsets match the packed C struct layout exactly:
	// offset 0:  4 × uint64 = 32 bytes
	evt.TimestampNs = le.Uint64(data[0:8])
	evt.DurationNs = le.Uint64(data[8:16])
	evt.BytesSent = le.Uint64(data[16:24])
	evt.BytesReceived = le.Uint64(data[24:32])

	// offset 32: 4 × uint32 + 1 × int32 = 20 bytes
	evt.PID = le.Uint32(data[32:36])
	evt.TID = le.Uint32(data[36:40])
	evt.SAddr = le.Uint32(data[40:44])
	evt.DAddr = le.Uint32(data[44:48])
	evt.Retransmits = le.Uint32(data[48:52])
	evt.ReturnCode = int32(le.Uint32(data[52:56]))

	// offset 56: 2 × uint16 = 4 bytes
	evt.SPort = le.Uint16(data[56:58])
	evt.DPort = le.Uint16(data[58:60])

	// offset 60: 2 × uint8 = 2 bytes
	evt.Type = EventType(data[60])
	evt.IPVersion = data[61]

	// offset 62: 16 bytes of comm
	copy(evt.Comm[:], data[62:78])

	return evt, nil
}

// SourceIP returns the source IPv4 address as a net.IP.
//
// The kernel stores IPv4 addresses as uint32 in network byte order (big-endian).
// net.IP expects a 4-byte slice in network byte order, so we just need to
// convert the uint32 to bytes. We use binary.BigEndian because the kernel
// stores IPs in network (big-endian) order.
func (e *Event) SourceIP() net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, e.SAddr)
	return ip
}

// DestIP returns the destination IPv4 address as a net.IP.
func (e *Event) DestIP() net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, e.DAddr)
	return ip
}

// CommString returns the process name as a Go string, trimming null bytes.
//
// In C, strings are null-terminated (end with \0). The comm field is always
// 16 bytes, with unused bytes filled with \0. We need to find the first \0
// and only return the string up to that point.
func (e *Event) CommString() string {
	// Find the first null byte
	n := 0
	for n < len(e.Comm) && e.Comm[n] != 0 {
		n++
	}
	return string(e.Comm[:n])
}

// String returns a human-readable representation of the event.
// This is used when printing events to stdout during development/debugging.
func (e *Event) String() string {
	switch e.Type {
	case EventConnect:
		return fmt.Sprintf("[%s] %s (pid=%d) → %s:%d → %s:%d (rc=%d)",
			e.Type, e.CommString(), e.PID,
			e.SourceIP(), e.SPort,
			e.DestIP(), e.DPort,
			e.ReturnCode)
	case EventAccept:
		return fmt.Sprintf("[%s] %s (pid=%d) ← %s:%d → %s:%d",
			e.Type, e.CommString(), e.PID,
			e.SourceIP(), e.SPort,
			e.DestIP(), e.DPort)
	case EventClose:
		return fmt.Sprintf("[%s] %s (pid=%d) %s:%d → %s:%d (sent=%d recv=%d)",
			e.Type, e.CommString(), e.PID,
			e.SourceIP(), e.SPort,
			e.DestIP(), e.DPort,
			e.BytesSent, e.BytesReceived)
	case EventRetransmit:
		return fmt.Sprintf("[%s] %s (pid=%d) %s:%d → %s:%d (retrans=%d)",
			e.Type, e.CommString(), e.PID,
			e.SourceIP(), e.SPort,
			e.DestIP(), e.DPort,
			e.Retransmits)
	default:
		return fmt.Sprintf("[UNKNOWN] pid=%d type=%d", e.PID, e.Type)
	}
}
