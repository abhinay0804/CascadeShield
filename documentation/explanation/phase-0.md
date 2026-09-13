# Phase 0: Environment Setup — Beginner Explanation

> 🎯 **Who is this for?** You — someone who knows Python and basic CS but has never touched Go, eBPF, or low-level Linux tooling. This document explains everything we did in Phase 0 and WHY, from the ground up.

---

## Summary of Previous Phase

There is no previous phase — this is where it all begins! Before Phase 0, we had:
- An architecture plan describing what CascadeShield should be
- An empty directory structure with no actual code
- A computer (EndeavourOS / Arch Linux) that may or may not have the right tools installed

After Phase 0, we have a **working build pipeline** — the foundation that every future phase builds on.

---

## What Was Done (Plain English)

Imagine you're about to build a car. Before you start welding metal and wiring engines, you need to:
1. **Check your garage** — do you have the right tools? Wrenches, a welder, a lift?
2. **Buy missing tools** — go to the store, get what's missing
3. **Set up your workbench** — organize everything so when you start building, you can grab what you need
4. **Test with a simple task** — tighten a bolt, make a simple weld, just to prove the tools work

That's exactly what Phase 0 does for CascadeShield:

1. **Checked what's installed** — Go compiler, clang, kernel version, Docker, etc.
2. **Installed missing tools** — `kind` (for running Kubernetes locally)
3. **Set up the project** — created the Go module, generated kernel headers, wrote a build system
4. **Tested with a stub** — compiled a tiny eBPF program and a skeleton Go binary to prove everything works end-to-end

---

## Why It Was Done

### Why can't we just start coding?

CascadeShield is unusual because it lives in **two worlds simultaneously**:

```
┌─────────────────────────────────────┐
│         KERNEL SPACE (C)            │  ← Runs INSIDE the Linux kernel
│   eBPF programs that watch TCP      │  ← Compiled with clang to BPF bytecode
│   connections at the OS level       │  ← Needs: clang, llvm, bpftool, kernel headers
└──────────────────┬──────────────────┘
                   │ data flows up
┌──────────────────▼──────────────────┐
│         USER SPACE (Go)             │  ← Runs as a normal program
│   Reads events, builds graphs,      │  ← Compiled with the Go compiler
│   runs simulations, applies fixes   │  ← Needs: Go, cilium/ebpf library
└─────────────────────────────────────┘
```

Each world has its own programming language, compiler, and toolchain. If ANY of these tools are missing or the wrong version, nothing works. Worse — the errors you'd get would be cryptic and confusing.

Phase 0 ensures we never hit those problems.

### Why each tool matters

| Tool | What it does | Analogy |
|---|---|---|
| **Go** | Compiles our main program (userspace) | The engine builder |
| **clang** | Compiles C code into eBPF bytecode for the kernel | The welder — joins our code to the kernel |
| **llvm-strip** | Removes debug info from eBPF objects to keep them small | Filing off rough edges after welding |
| **bpftool** | Generates `vmlinux.h` — a massive file containing every data structure the kernel uses | A complete blueprint of the engine you're working on |
| **Docker** | Runs containers (lightweight VMs) | The testing garage |
| **Kind** | Creates a Kubernetes cluster inside Docker containers | A miniature highway system in your garage to test the car |
| **kubectl** | Talks to Kubernetes clusters | The remote control for the highway system |
| **Linux kernel headers** | Tell the compiler what the kernel's internal structures look like | The engine's technical manual |

---

## How It Was Done

### Step-by-step conceptual walkthrough

#### 1. Environment Audit — "What do we already have?"

Before installing anything, we checked every tool:

```bash
go version          # → go1.26.5 ✅ (need 1.22+)
uname -r            # → 7.1.8    ✅ (need 5.8+)
clang --version     # → 22.1.8   ✅ (need 15+)
bpftool version     # → 7.8.0    ✅
docker version      # → 29.7.2   ✅
kubectl version     # → installed ✅
kind version        # → ❌ not found!
```

**Surprise:** Almost everything was already installed on this EndeavourOS machine! Only `kind` was missing.

#### 2. Install Kind — "The missing tool"

```bash
go install sigs.k8s.io/kind@latest
```

This is a Go command that downloads the `kind` source code from the internet, compiles it, and puts the binary in `~/go/bin/`. We also had to add that directory to the system's PATH (more on this below).

#### 3. Initialize the Go Module — "Giving our project an identity"

```bash
go mod init github.com/abhinay0804/cascadeshield
```

**What is a Go module?**

In Python, you have `requirements.txt` or `pyproject.toml` to track your project's dependencies. In Go, the equivalent is `go.mod`. When you run `go mod init`, it creates this file with:
- Your project's **module path** (like a unique name): `github.com/abhinay0804/cascadeshield`
- The Go version you're using

Every Go file in your project can now import other packages using this module path as a prefix. For example, `import "github.com/abhinay0804/cascadeshield/pkg/probe"` would import the probe package.

**The resulting `go.mod` file:**
```
module github.com/abhinay0804/cascadeshield

go 1.26.5
```

That's it! Simple, but essential.

#### 4. Generate `vmlinux.h` — "A blueprint of the kernel"

```bash
bpftool btf dump file /sys/kernel/btf/vmlinux format c 2>/dev/null > bpf/headers/vmlinux.h
```

This is one of the most important steps. Let me break it down:

**What is `vmlinux.h`?**

The Linux kernel is a massive C program with thousands of data structures (structs) — things like `struct sock` (a network socket), `struct task_struct` (a running process), `struct tcp_sock` (a TCP connection). Our eBPF programs need to read fields from these structs to extract information like IP addresses and ports.

Traditionally, you'd `#include <linux/tcp.h>` and dozens of other kernel headers. The problem? **Those headers change between kernel versions.** A struct field might be at offset 48 on kernel 5.15 but offset 56 on kernel 7.1.

**BTF (BPF Type Format)** solves this. Modern kernels embed a machine-readable description of ALL their types at `/sys/kernel/btf/vmlinux`. The `bpftool` command converts this into a single C header file — `vmlinux.h` — that has **every single kernel type definition** for YOUR specific kernel.

Result: **164,014 lines** of auto-generated C type definitions. Our eBPF programs include this one file instead of dozens of individual kernel headers.

**CO-RE (Compile Once, Run Everywhere):** By using vmlinux.h + BPF CO-RE helpers, our eBPF programs can be compiled once and run on different kernel versions. The BPF loader automatically adjusts struct field offsets at load time. Think of it like Java's "write once, run anywhere" but for kernel programs.

**The `2>/dev/null` gotcha:**

`bpftool` prints an informational message to stderr: `skipping /sys/kernel/btf/vmlinux (will be loaded as base)`. If we used just `>` without `2>/dev/null`, that message would end up as the first line of vmlinux.h, breaking the C compiler. This is a classic Unix trap — always be careful about stderr vs stdout when redirecting output to files.

#### 5. Create `bpf/types.h` — "The contract between two worlds"

This is the single most important data structure in the entire project:

```c
struct tcp_event {
    __u64 timestamp_ns;     // When did this happen?
    __u64 duration_ns;      // How long was the connection? (close events only)
    __u64 bytes_sent;       // How much data was sent?
    __u64 bytes_received;   // How much data was received?
    __u32 pid;              // Which process did this?
    __u32 tid;              // Which thread specifically?
    __u32 saddr;            // Source IP address
    __u32 daddr;            // Destination IP address
    __u32 retransmits;      // How many packets had to be re-sent?
    __s32 return_code;      // Did the connection succeed or fail?
    __u16 sport;            // Source port
    __u16 dport;            // Destination port
    __u8  event_type;       // CONNECT, ACCEPT, CLOSE, or RETRANSMIT
    __u8  ip_version;       // IPv4 or IPv6
    char  comm[16];         // Process name (e.g., "nginx", "orders-svc")
} __attribute__((packed));
```

**Why is this the "contract"?**

This struct is written in the eBPF C code (kernel side) and READ by the Go code (user side). Both sides MUST agree on the exact layout — same field order, same sizes, same alignment. If there's even a 1-byte mismatch, the Go side would read garbage data.

**What does `__attribute__((packed))` mean?**

Without this, the C compiler might insert invisible padding bytes between fields for alignment optimization. For example, after a `__u16` field (2 bytes), the compiler might add 2 padding bytes to align the next `__u32` field to a 4-byte boundary. With `packed`, we tell the compiler: "No padding. Pack everything tightly." This makes it much easier to mirror the struct exactly in Go.

**What are those `__u64`, `__u32`, etc. types?**

These are Linux kernel type aliases:
- `__u64` = unsigned 64-bit integer (like Python's `int`, but exactly 8 bytes)
- `__u32` = unsigned 32-bit integer (4 bytes)
- `__u16` = unsigned 16-bit integer (2 bytes)
- `__u8` = unsigned 8-bit integer (1 byte)
- `__s32` = signed 32-bit integer (can be negative — needed for error codes)

The "u" means unsigned, "s" means signed, the number is the bit width.

#### 6. Create Stub `bpf/tcp_connect.c` — "A baby eBPF program"

This isn't the real program — it's a minimal stub that proves the entire compilation pipeline works. Let me walk through the key parts:

```c
#include "vmlinux.h"              // All kernel types
#include <bpf/bpf_helpers.h>      // BPF helper functions
#include <bpf/bpf_core_read.h>    // Safe kernel struct access
#include <bpf/bpf_tracing.h>      // kprobe argument macros
#include "../types.h"             // Our tcp_event struct
```

**The ring buffer map:**
```c
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);  // 256 KB
} events SEC(".maps");
```

**What is a BPF map?** It's a data structure shared between the kernel-side eBPF program and the user-space Go program. Think of it as a **mailbox** — the eBPF program drops messages (events) in, and the Go program picks them up.

A ring buffer is a specific type of map that works like a **circular conveyor belt** — new events go on one end, the consumer reads from the other end. If the consumer is slow, old events get overwritten. It's lock-free and very fast.

**The probe itself:**
```c
SEC("kprobe/tcp_v4_connect")
int BPF_KPROBE(tcp_v4_connect_stub, struct sock *sk)
{
    struct tcp_event *evt;
    evt = bpf_ringbuf_reserve(&events, sizeof(struct tcp_event), 0);
    if (!evt) return 0;

    evt->timestamp_ns = bpf_ktime_get_ns();
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    evt->pid = pid_tgid >> 32;
    evt->tid = (__u32)pid_tgid;
    evt->event_type = EVENT_CONNECT;
    bpf_get_current_comm(&evt->comm, sizeof(evt->comm));
    bpf_ringbuf_submit(evt, 0);

    return 0;
}
```

**What is a kprobe?** A kprobe (kernel probe) is a hook that lets you run your code every time a specific kernel function is called. `tcp_v4_connect` is the kernel function called whenever ANY process on the system makes an outgoing TCP connection. By attaching a kprobe to it, we get notified of EVERY outgoing connection — without changing any application code.

**The `SEC("kprobe/tcp_v4_connect")` line** tells the eBPF loader which kernel function to attach to. `SEC` stands for "section" — it places the function in a specific ELF section that the loader reads.

**The `>> 32` trick:** `bpf_get_current_pid_tgid()` returns a 64-bit number where the upper 32 bits are the process ID (TGID) and the lower 32 bits are the thread ID (PID). Shifting right by 32 extracts the upper half. This is a common pattern in eBPF code because the kernel packs two IDs into one value for efficiency.

#### 7. Create the Makefile — "The project's instruction manual"

A Makefile tells the `make` command how to build things. Instead of remembering long commands, you just type:

```bash
make bpf     # Compile C → eBPF bytecode
make build   # Compile Go → binary
make all     # Do both (clean first)
make clean   # Delete all compiled files
make run     # Build and run
```

**Key clang flags for eBPF:**
```
-O2                        # Optimize (required — eBPF verifier rejects unoptimized code)
-g                         # Include debug info (llvm-strip removes it after)
-target bpf                # Compile for BPF virtual machine, not x86
-D__TARGET_ARCH_x86        # Tell headers we're on x86_64
-I./bpf/headers            # Look for vmlinux.h in this directory
-Wno-missing-declarations  # Suppress harmless vmlinux.h warnings
```

The `-target bpf` flag is crucial — it tells clang to generate BPF bytecode instead of normal x86 machine code. BPF is a special instruction set that the kernel's eBPF virtual machine can execute safely.

#### 8. Create `cmd/cascadeshield/main.go` — "The skeleton"

This is our Go program's entry point. Key patterns explained:

**Signal handling with context:**
```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
```

In Go, `context.Context` is the standard way to propagate cancellation. When you press Ctrl+C (SIGINT) or the process is killed (SIGTERM), the context is cancelled. Every goroutine (lightweight thread) in the program can check `ctx.Done()` to know it should shut down. This is like Python's signal handlers, but more structured.

**`defer` keyword:**
```go
defer stop()  // This runs when the function exits, no matter what
```

`defer` schedules a function call to run when the enclosing function returns. It's like Python's `finally` block but cleaner. You put cleanup code right next to the setup code — no need for try/finally blocks.

**`<-ctx.Done()` — blocking on a channel:**
```go
<-ctx.Done()
```

This reads from the context's "done" channel. Channels are Go's way of communicating between goroutines (like Python's `queue.Queue` but built into the language). Reading from a channel blocks until something is sent. The context sends a signal on this channel when it's cancelled, so this line effectively means "wait here until Ctrl+C".

---

## Key Concepts Explained

### What is eBPF?

Imagine the Linux kernel as a **high-security government building**. Normally, you can't go inside — it's dangerous and you might break something. But what if the building had **special visitor booths** where you could sit, watch what's happening through a one-way mirror, and take notes?

That's eBPF. It lets you run small, sandboxed programs inside the kernel that can observe (and sometimes modify) kernel behavior. The "sandbox" part is enforced by the **eBPF verifier** — a static analyzer that checks your program before loading it to guarantee:
- It will terminate (no infinite loops)
- It won't access memory it shouldn't
- It won't crash the kernel

If your program doesn't pass the verifier, it's rejected. No exceptions.

### What is a Build Pipeline?

```
Source Code  →  Compiler  →  Binary/Object  →  Run
     ↑              ↑            ↑              ↑
  .c / .go       clang/go    .o / executable   kernel/user
```

We have TWO build pipelines:
1. **C → eBPF:** `bpf/*.c` → clang → `bpf/*.o` (runs in kernel)
2. **Go → Binary:** `cmd/cascadeshield/main.go` → go build → `bin/cascadeshield` (runs in userspace)

The Makefile orchestrates both.

### What is PATH?

PATH is an environment variable that tells your shell where to look for commands. When you type `kind`, the shell searches each directory in PATH looking for a file called `kind`. If `~/go/bin` isn't in PATH, the shell can't find `kind` even though it exists.

```
PATH=/usr/local/bin:/usr/bin    ← Shell looks here for commands
                                  ~/go/bin is NOT here → "kind: not found"

PATH=/usr/local/bin:/usr/bin:~/go/bin    ← Now shell can find kind!
```

---

## How It All Connects

```
Phase 0 (THIS PHASE)                    Phase 1 (NEXT)
═══════════════════                      ═══════════════

bpf/types.h ─────────────────────────→  Used by tcp_connect.c, tcp_accept.c,
(event struct)                           tcp_close.c, tcp_retransmit.c

bpf/headers/vmlinux.h ───────────────→  #included by every eBPF C program

Makefile (make bpf) ─────────────────→  Compiles all 4 real eBPF programs

cmd/cascadeshield/main.go ───────────→  Extended to load probes, read events

go.mod ──────────────────────────────→  Dependencies added (cilium/ebpf)

Verified toolchain (clang, bpftool) ─→  Used every time we compile eBPF
```

**Before Phase 0:** Empty directories, no tools verified, no build system.
**After Phase 0:** A fully verified environment where `make all` compiles both C and Go code, producing a running (but skeletal) binary.

**Phase 1 will:** Replace the stub eBPF program with real kernel probes, create the Go-side event consumer, and wire them together so we can see live TCP connection events streaming from the kernel. That's when it gets exciting! 🔥
