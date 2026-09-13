# ==============================================================================
# CascadeShield — Makefile
#
# Build system for the CascadeShield eBPF + Go project.
#
# Targets:
#   make all       — Full build: clean → bpf → generate → build
#   make bpf       — Compile eBPF C programs to .o objects
#   make generate  — Run go generate (bpf2go code generation)
#   make build     — Compile Go binary
#   make test      — Run Go unit tests
#   make run       — Build and run (requires root for eBPF)
#   make clean     — Remove all build artifacts
#   make vmlinux   — Regenerate vmlinux.h from running kernel's BTF
#   make lint      — Run Go linter (if installed)
#   make help      — Show this help
# ==============================================================================

# --- Configuration -----------------------------------------------------------

# Go build settings
GO         := go
BINARY     := cascadeshield
CMD_DIR    := ./cmd/cascadeshield
BIN_DIR    := ./bin
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS    := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"

# eBPF build settings
CLANG      := clang
LLVM_STRIP := llvm-strip
BPF_DIR    := ./bpf
BPF_HEADERS:= $(BPF_DIR)/headers
BPF_CFLAGS := -O2 -g -target bpf -D__TARGET_ARCH_x86 -I$(BPF_HEADERS) -Wno-missing-declarations

# Find all eBPF C source files (excluding headers)
BPF_SRC    := $(wildcard $(BPF_DIR)/*.c)
BPF_OBJ    := $(BPF_SRC:.c=.o)

# --- Targets -----------------------------------------------------------------

.PHONY: all bpf generate build test run clean vmlinux lint help

## all: Full build pipeline — clean, compile eBPF, generate Go bindings, build binary
all: clean bpf build

## help: Show available targets with descriptions
help:
	@echo "CascadeShield Build System"
	@echo "=========================="
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /' | column -t -s ':'
	@echo ""
	@echo "Environment:"
	@echo "  Go:      $(shell $(GO) version 2>/dev/null || echo 'not found')"
	@echo "  Clang:   $(shell $(CLANG) --version 2>/dev/null | head -1 || echo 'not found')"
	@echo "  Kernel:  $(shell uname -r)"
	@echo "  Arch:    $(shell uname -m)"

## bpf: Compile eBPF C programs to BPF bytecode (.o files)
bpf: $(BPF_OBJ)
	@echo "✅ eBPF objects compiled successfully"

# Pattern rule: compile any .c in bpf/ to .o
# The -g flag includes DWARF debug info, which llvm-strip then removes.
# This two-step process keeps the .o small while allowing debugging during development.
$(BPF_DIR)/%.o: $(BPF_DIR)/%.c $(BPF_DIR)/types.h $(BPF_HEADERS)/vmlinux.h
	@echo "  CC  $<"
	$(CLANG) $(BPF_CFLAGS) -c $< -o $@
	$(LLVM_STRIP) -g $@

## generate: Run go generate for bpf2go code generation (Phase 1+)
generate:
	@echo "Running go generate..."
	$(GO) generate ./...
	@echo "✅ Go generate completed"

## build: Compile the Go binary
build:
	@echo "Building $(BINARY)..."
	@mkdir -p $(BIN_DIR)
	$(GO) build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) $(CMD_DIR)
	@echo "✅ Binary built: $(BIN_DIR)/$(BINARY)"

## test: Run all Go unit tests with race detector
test:
	@echo "Running tests..."
	$(GO) test -race -v ./...
	@echo "✅ Tests passed"

## run: Build and run the binary (eBPF requires root — use sudo)
run: build
	@echo "Running $(BINARY) (may require root for eBPF)..."
	$(BIN_DIR)/$(BINARY)

## clean: Remove all build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR)
	@rm -f $(BPF_DIR)/*.o
	@echo "✅ Clean complete"

## vmlinux: Regenerate vmlinux.h from the running kernel's BTF data
vmlinux:
	@echo "Generating vmlinux.h from /sys/kernel/btf/vmlinux..."
	bpftool btf dump file /sys/kernel/btf/vmlinux format c 2>/dev/null > $(BPF_HEADERS)/vmlinux.h
	@echo "✅ vmlinux.h generated ($(shell wc -l < $(BPF_HEADERS)/vmlinux.h) lines)"

## lint: Run Go linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	golangci-lint run ./...
	@echo "✅ Lint passed"
