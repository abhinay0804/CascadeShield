# CascadeShield — Phase 0: Environment Setup Documentation

> **Phase:** 0 — Environment Setup
> **Completed:** 2026-09-09
> **Agent:** Claude Opus 4.6 (Thinking)
> **OS:** EndeavourOS (Arch Linux) — kernel 7.1.8-arch1-3

---

## Summary

Phase 0 established the complete Go + eBPF development environment on EndeavourOS and
initialized the CascadeShield project with a working build system. This includes:

- Verified all system dependencies (Go, clang, llvm, bpftool, Docker, kubectl, kind)
- Initialized the Go module (`github.com/abhinay0804/cascadeshield`)
- Generated `vmlinux.h` from the running kernel's BTF data (164,014 lines)
- Created the shared eBPF event struct (`bpf/types.h`)
- Created a stub eBPF kprobe (`bpf/tcp_connect.c`) that compiles to valid BPF bytecode
- Created the Makefile with all build targets
- Created a minimal `main.go` entry point with signal handling

The full `make all` pipeline (clean → bpf compile → go build) works end-to-end.

---

## Files Created/Modified

| File | Purpose |
|---|---|
| `go.mod` | Go module definition — `github.com/abhinay0804/cascadeshield` |
| `bpf/headers/vmlinux.h` | Auto-generated kernel type definitions (164K lines) for CO-RE |
| `bpf/types.h` | Shared `tcp_event` struct — the contract between C and Go |
| `bpf/tcp_connect.c` | Stub kprobe on `tcp_v4_connect` — verifies eBPF toolchain |
| `cmd/cascadeshield/main.go` | Entry point — CLI flags, signal handling, shutdown scaffolding |
| `Makefile` | Build system — `bpf`, `build`, `test`, `clean`, `run`, `vmlinux`, `help` targets |

---

## Architecture Decisions

### 1. vmlinux.h via BTF (CO-RE approach)
**Decision:** Use `bpftool btf dump` to generate `vmlinux.h` from the running kernel's BTF data.
**Rationale:** CO-RE (Compile Once, Run Everywhere) allows our eBPF programs to work across different kernel versions without recompilation. The vmlinux.h header provides all kernel type definitions.
**Gotcha:** `bpftool` prints an info message (`skipping /sys/kernel/btf/vmlinux (will be loaded as base)`) to stderr that can pollute the output if stderr isn't redirected. The Makefile uses `2>/dev/null` to handle this.

### 2. Packed struct for cross-language alignment
**Decision:** `tcp_event` uses `__attribute__((packed))` to eliminate compiler padding.
**Rationale:** The Go side will use `unsafe.Sizeof` / binary decoding to read this struct from the ring buffer. Any padding mismatch between clang (C) and Go's `encoding/binary` would cause data corruption. Packed structs guarantee identical layout.

### 3. Warning suppression for vmlinux.h
**Decision:** Added `-Wno-missing-declarations` to the clang flags.
**Rationale:** The auto-generated vmlinux.h produces ~9 harmless warnings about forward-declared structs inside kernel types. These are not actionable and would create noise on every build.

### 4. Kind over Minikube for local K8s
**Decision:** Installed `kind` v0.33.0 via `go install`.
**Rationale:** Kind (Kubernetes in Docker) is lighter weight, faster to create/destroy clusters, and doesn't require a hypervisor. Perfect for development and CI.
**Note:** Kind binary is at `~/go/bin/kind`. Users need `~/go/bin` in their PATH. If not already there, add `export PATH=$PATH:~/go/bin` to `.bashrc`.

---

## Verified Environment

| Component | Version | Status |
|---|---|---|
| Go | 1.26.5 | ✅ |
| Linux Kernel | 7.1.8-arch1-3 | ✅ |
| BTF | `/sys/kernel/btf/vmlinux` exists | ✅ |
| Linux Headers | 7.1.8.arch1-3 | ✅ |
| Clang | 22.1.8 | ✅ |
| LLVM Strip | Compatible with GNU strip | ✅ |
| bpftool | 7.8.0 (libbpf 1.8) | ✅ |
| Docker | 29.7.2 | ✅ |
| kubectl | Latest (2026 build) | ✅ |
| Kind | 0.33.0 | ✅ (at ~/go/bin/kind) |

---

## Build Verification Results

| Check | Result |
|---|---|
| `make bpf` — eBPF C → .o | ✅ Compiled, 0 errors, 0 warnings |
| `make build` — Go binary | ✅ Built at `bin/cascadeshield` |
| `make test` — Go tests | ✅ Passed (no test files yet) |
| `make all` — Full pipeline | ✅ clean → bpf → build all succeed |
| `./bin/cascadeshield --version` | ✅ Prints version string |
| `go vet ./...` | ✅ No issues |
| `file bpf/tcp_connect.o` | ✅ `ELF 64-bit LSB relocatable, eBPF` |

---

## Data Flow (This Phase)

```
bpf/tcp_connect.c  ──[clang -target bpf]──▶  bpf/tcp_connect.o (ELF eBPF object)

cmd/cascadeshield/main.go  ──[go build]──▶  bin/cascadeshield (Go binary, skeleton)
```

No runtime data flow yet — the binary is a skeleton that waits for Ctrl+C. Phase 1 will connect the eBPF object to the Go binary via `cilium/ebpf`.

---

## Key Interfaces (This Phase)

### bpf/types.h — `struct tcp_event`
The single most important data structure in the entire project. Every eBPF probe writes this struct to the ring buffer, and the Go reader decodes it. Fields:
- `timestamp_ns`, `duration_ns`, `bytes_sent`, `bytes_received` (u64)
- `pid`, `tid`, `saddr`, `daddr`, `retransmits`, `return_code` (u32/s32)
- `sport`, `dport` (u16)
- `event_type`, `ip_version` (u8)
- `comm[16]` (char array — process name)

Event types: `EVENT_CONNECT=1`, `EVENT_ACCEPT=2`, `EVENT_CLOSE=3`, `EVENT_RETRANSMIT=4`

---

## Dependencies

| This Phase Provides | Used By |
|---|---|
| `bpf/types.h` | All eBPF C programs (Phase 1) |
| `bpf/headers/vmlinux.h` | All eBPF C programs (Phase 1) |
| `Makefile` → `make bpf` target | eBPF compilation (Phase 1+) |
| `cmd/cascadeshield/main.go` skeleton | All phases (wiring point) |
| `go.mod` | All Go packages |

---

## Gotchas & Lessons

1. **bpftool stderr contamination:** `bpftool btf dump` prints `skipping /sys/kernel/btf/vmlinux (will be loaded as base)` to stderr. If you redirect with just `>`, this line ends up at the top of vmlinux.h and breaks compilation. Always use `2>/dev/null` when generating vmlinux.h.

2. **Kind PATH:** `go install` puts binaries in `~/go/bin/`, which may not be in the system PATH. The user should add `export PATH=$PATH:$(go env GOPATH)/bin` to their shell config.

3. **Arch Linux packages:** On EndeavourOS/Arch, clang, llvm, bpftool, and kubectl were already available via pacman. The `bpf` package provides `bpftool` on Arch (unlike Ubuntu where it's a separate package).

4. **vmlinux.h warnings:** The auto-generated header produces ~9 `-Wmissing-declarations` warnings from forward-declared kernel structs. These are harmless and suppressed with `-Wno-missing-declarations`.

---

## Notes for Next Phase (Phase 1: Kernel Eye)

### What Phase 1 needs to do:
1. **Replace the stub** `bpf/tcp_connect.c` with the full implementation that:
   - Uses kprobe + kretprobe pair on `tcp_v4_connect`
   - Stores args in a BPF hashmap on entry (keyed by `pid_tgid`)
   - Looks up args on exit, populates full `tcp_event`, submits to ring buffer
   - Extracts source/dest IP and port from the `sock` struct using CO-RE helpers

2. **Create three new eBPF programs:**
   - `tcp_accept.c` — kretprobe on `inet_csk_accept`
   - `tcp_close.c` — kprobe on `tcp_close`
   - `tcp_retransmit.c` — tracepoint on `tcp:tcp_retransmit_skb`

3. **Create Go userspace consumer:**
   - `pkg/probe/event.go` — Go-side mirror of `struct tcp_event`
   - `pkg/probe/loader.go` — Load .o files, attach probes, using `cilium/ebpf`
   - `pkg/probe/reader.go` — Poll ring buffer, decode events, emit to Go channel

4. **Wire into main.go:** Load probes → read events → print to stdout

### Important considerations for Phase 1:
- The `types.h` struct is already defined — don't redefine it, mirror it exactly in Go
- Use `BPF_CORE_READ()` macro for safe kernel struct field access (CO-RE)
- The kprobe/kretprobe pair pattern requires a BPF hashmap to pass data between entry and exit
- Ring buffer size (256KB) may need tuning under high connection rates
- Must handle `SIGINT`/`SIGTERM` to cleanly detach probes (skeleton in main.go already does this)
- Consider using `bpf2go` from `cilium/ebpf` for auto-generating Go bindings — it's the recommended approach
