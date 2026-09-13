// SPDX-License-Identifier: GPL-2.0 OR BSD-3-Clause
//
// CascadeShield — eBPF TCP Connection Probes
//
// This file contains ALL eBPF probes for CascadeShield, combined into a single
// compilation unit so they can share a ring buffer map. The probes hook into
// 4 kernel functions to capture the complete TCP connection lifecycle:
//
//   1. tcp_v4_connect    (kprobe + kretprobe)  → outgoing connections
//   2. inet_csk_accept   (kretprobe)           → incoming connections
//   3. tcp_close          (kprobe)              → connection close (duration, bytes)
//   4. tcp_retransmit_skb (tracepoint)          → packet retransmissions
//
// Architecture note: The architecture plan specified separate .c files per hook,
// but combining them here allows all probes to share a single ring buffer map,
// which is more efficient and simpler for the Go userspace consumer (one reader).
//
// Build: compiled via bpf2go at `go generate` time (see pkg/probe/loader.go)

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>    // bpf_ntohs() for network byte order conversion

#include "../types.h"

// =============================================================================
// BPF Maps — shared data structures between kernel and userspace
// =============================================================================

// Ring buffer for streaming events to userspace.
// All probes write to this single ring buffer, and the Go reader polls it.
//
// Why ring buffer (not perf buffer)?
//   - Lock-free, multi-producer safe (important when multiple CPUs fire probes)
//   - Supports variable-length records
//   - Lower overhead than perf buffer (no per-CPU copying)
//   - Available since Linux 5.8 (our kernel is 7.1.8)
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256 KB — configurable via Go loader
} events SEC(".maps");

// Hashmap to pass data between kprobe entry and kretprobe exit for tcp_v4_connect.
//
// Why do we need this?
//   tcp_v4_connect is hooked with BOTH a kprobe (entry) and kretprobe (exit).
//   On entry, the `sock` struct has source info but the connection hasn't been
//   established yet. On exit, the connection is complete and we can read the
//   destination. But the kretprobe doesn't receive the original function arguments!
//
//   Solution: On entry, save the sock pointer in this hashmap keyed by pid_tgid.
//   On exit, look it up, read the connection info, then delete the entry.
//
// Key:   u64 pid_tgid (unique per thread — prevents races between threads)
// Value: struct sock* (the socket pointer to read connection info from)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240); // Max 10K concurrent in-flight connects
    __type(key, __u64);
    __type(value, struct sock *);
} inflight_connects SEC(".maps");

// =============================================================================
// Helper: populate common fields in a tcp_event
// =============================================================================

// Fills in the fields that every event type needs: timestamp, PID, TID, comm.
// Declared as `static __always_inline` so the compiler inlines it into each
// probe — eBPF doesn't support function calls across programs.
static __always_inline void fill_event_common(struct tcp_event *evt, __u8 type)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();

    evt->timestamp_ns = bpf_ktime_get_ns();
    evt->pid = pid_tgid >> 32;      // Upper 32 bits = TGID (process ID)
    evt->tid = (__u32)pid_tgid;     // Lower 32 bits = PID (thread ID)
    evt->event_type = type;
    evt->ip_version = IP_V4;        // Phase 1 = IPv4 only

    bpf_get_current_comm(&evt->comm, sizeof(evt->comm));
}

// Extracts IPv4 addresses and ports from a sock struct using CO-RE.
//
// CO-RE (Compile Once, Run Everywhere) note:
//   BPF_CORE_READ() is a macro that generates BPF instructions to safely
//   read kernel struct fields. If the struct layout changes between kernel
//   versions, the BPF loader automatically adjusts the field offsets at
//   load time using BTF relocation. This is why we use vmlinux.h + CO-RE
//   instead of hardcoding struct offsets.
static __always_inline void fill_event_addrs(struct tcp_event *evt, struct sock *sk)
{
    // sk->__sk_common is the shared part of all socket types.
    // It contains the IP addresses and ports.
    evt->saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);  // Source IPv4
    evt->daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);      // Dest IPv4
    evt->sport = BPF_CORE_READ(sk, __sk_common.skc_num);        // Source port (host order)

    // Destination port is stored in network byte order in the kernel.
    // bpf_ntohs() converts it to host byte order so Go can read it directly.
    evt->dport = bpf_ntohs(BPF_CORE_READ(sk, __sk_common.skc_dport));
}

// =============================================================================
// PROBE 1: tcp_v4_connect — Outgoing TCP connections
// =============================================================================
//
// When any process on the system calls connect() to a TCP endpoint, the kernel
// internally calls tcp_v4_connect(). We hook both the ENTRY and EXIT:
//
//   Entry (kprobe):  Save the sock pointer for later
//   Exit (kretprobe): Read connection info from sock, emit CONNECT event
//
// This two-phase approach is necessary because:
//   - On entry: we have the sock pointer (function argument)
//   - On exit: we know the return code (success/failure) but don't have args
//   - We need BOTH to emit a complete event

SEC("kprobe/tcp_v4_connect")
int BPF_KPROBE(tcp_v4_connect_entry, struct sock *sk)
{
    // Save the sock pointer, keyed by the calling thread's pid_tgid.
    // This lets the kretprobe find it when the function returns.
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    bpf_map_update_elem(&inflight_connects, &pid_tgid, &sk, BPF_ANY);
    return 0;
}

SEC("kretprobe/tcp_v4_connect")
int BPF_KRETPROBE(tcp_v4_connect_exit, int ret)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();

    // Look up the sock pointer we saved on entry
    struct sock **skp = bpf_map_lookup_elem(&inflight_connects, &pid_tgid);
    if (!skp) {
        return 0; // Entry wasn't captured (race or map full) — skip
    }
    struct sock *sk = *skp;

    // Always clean up the hashmap entry, even if we can't emit an event
    bpf_map_delete_elem(&inflight_connects, &pid_tgid);

    // Reserve space in the ring buffer
    struct tcp_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt) {
        return 0; // Ring buffer full — drop (acceptable, counted in userspace)
    }

    // Populate the event
    fill_event_common(evt, EVENT_CONNECT);
    fill_event_addrs(evt, sk);
    evt->return_code = ret; // 0 = success, -errno = failure

    // Zero out fields not relevant for CONNECT events
    evt->duration_ns = 0;
    evt->bytes_sent = 0;
    evt->bytes_received = 0;
    evt->retransmits = 0;

    bpf_ringbuf_submit(evt, 0);
    return 0;
}

// =============================================================================
// PROBE 2: inet_csk_accept — Incoming TCP connections
// =============================================================================
//
// When a server process calls accept() and a new connection arrives, the kernel
// calls inet_csk_accept(). We hook the RETURN to capture the accepted socket.
//
// Unlike tcp_v4_connect, we only need a kretprobe here because:
//   - The returned sock pointer already has all connection info populated
//   - We don't need to correlate entry/exit

SEC("kretprobe/inet_csk_accept")
int BPF_KRETPROBE(inet_csk_accept_exit, struct sock *sk)
{
    // If accept() failed, sk is NULL — nothing to report
    if (!sk) {
        return 0;
    }

    // Filter: only capture IPv4 TCP connections (AF_INET = 2)
    __u16 family = BPF_CORE_READ(sk, __sk_common.skc_family);
    if (family != 2) { // AF_INET
        return 0;
    }

    struct tcp_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt) {
        return 0;
    }

    fill_event_common(evt, EVENT_ACCEPT);
    fill_event_addrs(evt, sk);

    // Not applicable for ACCEPT events
    evt->return_code = 0;
    evt->duration_ns = 0;
    evt->bytes_sent = 0;
    evt->bytes_received = 0;
    evt->retransmits = 0;

    bpf_ringbuf_submit(evt, 0);
    return 0;
}

// =============================================================================
// PROBE 3: tcp_close — Connection closed
// =============================================================================
//
// When a TCP connection is closed (by either side), the kernel calls tcp_close().
// This is where we capture connection lifetime metrics:
//   - bytes_sent / bytes_received (throughput)
//   - We DON'T get exact duration from this probe alone (would need to correlate
//     with the original connect/accept event in userspace), so duration_ns is
//     set to 0 here. The Go userspace layer can compute duration by matching
//     CONNECT/ACCEPT events with their corresponding CLOSE events.
//
// tcp_sock is a "subclass" of sock — it extends sock with TCP-specific fields
// like byte counters. We use BPF_CORE_READ to safely cast and read from it.

SEC("kprobe/tcp_close")
int BPF_KPROBE(tcp_close_entry, struct sock *sk)
{
    // Filter: only IPv4
    __u16 family = BPF_CORE_READ(sk, __sk_common.skc_family);
    if (family != 2) {
        return 0;
    }

    struct tcp_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt) {
        return 0;
    }

    fill_event_common(evt, EVENT_CLOSE);
    fill_event_addrs(evt, sk);

    // Cast sock to tcp_sock to access TCP-specific byte counters.
    //
    // In the kernel's type hierarchy:
    //   struct sock → struct inet_connection_sock → struct tcp_sock
    //
    // tcp_sock has fields like bytes_sent, bytes_received, bytes_acked, etc.
    // We read bytes_acked (data acknowledged by peer = successfully sent)
    // and bytes_received (data received from peer).
    struct tcp_sock *tp = (struct tcp_sock *)sk;
    evt->bytes_sent = BPF_CORE_READ(tp, bytes_acked);
    evt->bytes_received = BPF_CORE_READ(tp, bytes_received);

    evt->return_code = 0;
    evt->duration_ns = 0;  // Computed in userspace by correlating with CONNECT/ACCEPT
    evt->retransmits = 0;

    bpf_ringbuf_submit(evt, 0);
    return 0;
}

// =============================================================================
// PROBE 4: tcp_retransmit_skb — TCP segment retransmission
// =============================================================================
//
// When a TCP segment needs to be retransmitted (due to packet loss, congestion,
// or slow consumer), the kernel fires the tcp:tcp_retransmit_skb tracepoint.
//
// Retransmissions are a KEY signal for CascadeShield:
//   - They indicate network degradation or an overwhelmed service
//   - High retransmit rates correlate with cascading failures
//   - They're an early warning signal (appear before full timeout)
//
// Tracepoints vs kprobes:
//   - Tracepoints are STABLE kernel interfaces (won't break across versions)
//   - Kprobes hook internal functions that can change without notice
//   - We use a tracepoint here because tcp:tcp_retransmit_skb is a stable API
//
// The tracepoint provides structured args via a context struct. We use
// BPF_CORE_READ to access the sock from the skb (socket buffer).

SEC("tracepoint/tcp/tcp_retransmit_skb")
int handle_tcp_retransmit(struct trace_event_raw_tcp_retransmit_skb *ctx)
{
    // The tracepoint struct for tcp_retransmit_skb already contains extracted
    // fields (sport, dport, saddr, daddr, family) — no need to read from sock.
    // This is one advantage of tracepoints over kprobes: stable, pre-parsed args.

    // Filter: only IPv4 (family == 2 == AF_INET)
    __u16 family = BPF_CORE_READ(ctx, family);
    if (family != 2) {
        return 0;
    }

    struct tcp_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt) {
        return 0;
    }

    fill_event_common(evt, EVENT_RETRANSMIT);

    // Read addresses directly from the tracepoint context.
    // saddr/daddr are __u8[4] arrays in the tracepoint struct,
    // but our event uses __u32. We need to copy and reinterpret.
    __u8 saddr[4], daddr[4];
    BPF_CORE_READ_INTO(&saddr, ctx, saddr);
    BPF_CORE_READ_INTO(&daddr, ctx, daddr);
    __builtin_memcpy(&evt->saddr, &saddr, 4);
    __builtin_memcpy(&evt->daddr, &daddr, 4);

    evt->sport = BPF_CORE_READ(ctx, sport);
    evt->dport = BPF_CORE_READ(ctx, dport);

    // Read the total retransmit count from tcp_sock via the skaddr pointer
    struct sock *sk = (struct sock *)BPF_CORE_READ(ctx, skaddr);
    if (sk) {
        struct tcp_sock *tp = (struct tcp_sock *)sk;
        evt->retransmits = BPF_CORE_READ(tp, total_retrans);
    } else {
        evt->retransmits = 0;
    }

    evt->return_code = 0;
    evt->duration_ns = 0;
    evt->bytes_sent = 0;
    evt->bytes_received = 0;

    bpf_ringbuf_submit(evt, 0);
    return 0;
}

// Every eBPF program MUST declare a license. GPL is required for certain
// BPF helpers (bpf_probe_read_kernel, bpf_get_current_comm, etc.).
char LICENSE[] SEC("license") = "GPL";
