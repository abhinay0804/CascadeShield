#ifndef __CASCADESHIELD_TYPES_H
#define __CASCADESHIELD_TYPES_H

// =============================================================================
// CascadeShield — Shared Event Structure (C ↔ Go)
//
// This header defines the tcp_event struct that is shared between the eBPF C
// programs running in kernel space and the Go userspace consumer. Both sides
// MUST agree on the exact layout, field sizes, and alignment of this struct.
//
// Any change here MUST be mirrored in: pkg/probe/event.go
// =============================================================================

// Event type constants — identifies which kernel hook generated this event
#define EVENT_CONNECT    1  // tcp_v4_connect — outgoing connection initiated
#define EVENT_ACCEPT     2  // inet_csk_accept — incoming connection accepted
#define EVENT_CLOSE      3  // tcp_close — connection closed (has duration/bytes)
#define EVENT_RETRANSMIT 4  // tcp_retransmit_skb — TCP segment retransmitted

// IP version constants
#define IP_V4 4
#define IP_V6 6

// Maximum length of process name (matches Linux task_struct->comm)
#define TASK_COMM_LEN 16

// tcp_event is the primary data structure emitted by all eBPF probes.
// It is written to a BPF ring buffer in kernel space and consumed
// by the Go userspace reader.
//
// Layout notes:
//   - Fields are ordered to minimize padding (largest fields first)
//   - __u64 fields are 8-byte aligned
//   - __u32 fields are 4-byte aligned
//   - __u16/__u8 fields are packed at the end
//   - comm[] is fixed-size to avoid variable-length records in ring buffer
struct tcp_event {
    __u64 timestamp_ns;     // ktime_get_ns() — monotonic kernel timestamp
    __u64 duration_ns;      // Connection lifetime in nanoseconds (CLOSE events only)
    __u64 bytes_sent;       // Total TX bytes over connection (CLOSE events only)
    __u64 bytes_received;   // Total RX bytes over connection (CLOSE events only)

    __u32 pid;              // Process ID (tgid) of the task that triggered the event
    __u32 tid;              // Thread ID (pid in kernel terms) for thread-level tracking
    __u32 saddr;            // Source IPv4 address (network byte order)
    __u32 daddr;            // Destination IPv4 address (network byte order)
    __u32 retransmits;      // Cumulative retransmit count (RETRANSMIT events only)
    __s32 return_code;      // connect() return code: 0=success, -errno=failure

    __u16 sport;            // Source port (host byte order)
    __u16 dport;            // Destination port (host byte order)

    __u8  event_type;       // One of EVENT_CONNECT/ACCEPT/CLOSE/RETRANSMIT
    __u8  ip_version;       // IP_V4 or IP_V6

    char  comm[TASK_COMM_LEN]; // Process name from task_struct->comm (null-terminated)
} __attribute__((packed));
// Note: __attribute__((packed)) ensures no padding between fields.
// This is critical for correct C ↔ Go struct alignment.

#endif // __CASCADESHIELD_TYPES_H
