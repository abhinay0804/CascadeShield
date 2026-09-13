package resolver

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Regex patterns to extract container IDs from various cgroup formats:
// - Docker: /docker/1a2b3c... or /docker-<id>.scope
// - containerd: /system.slice/containerd-1a2b3c...scope or /cri-containerd-<id>.scope
// - CRI-O: /crio-1a2b3c...scope or /crio-<id>
var containerIDRegexes = []*regexp.Regexp{
	// 64-character hexadecimal SHA (Docker / Containerd standard)
	regexp.MustCompile(`(?:docker/|docker-|containerd-|crio-|cri-containerd-)([0-9a-fA-F]{64})`),

	// CRI-O / Containerd alphanumeric hyphenated IDs
	regexp.MustCompile(`(?:crio-|containerd-|cri-containerd-)([0-9a-zA-Z_-]{32,64})`),

	// Docker legacy format / systemd scope suffix (.scope)
	regexp.MustCompile(`([0-9a-fA-F]{64})\.scope`),
}

// ProcReader reads process metadata from the Linux /proc filesystem.
type ProcReader struct {
	procRoot string
}

// NewProcReader constructs a ProcReader pointing to the given proc root directory (default "/proc").
func NewProcReader(procRoot string) *ProcReader {
	if procRoot == "" {
		procRoot = "/proc"
	}
	return &ProcReader{
		procRoot: procRoot,
	}
}

// ReadContainerID attempts to resolve a process ID (PID) to a container runtime ID
// by parsing `/proc/<pid>/cgroup`. Returns empty string "" if the process is host-native.
func (p *ProcReader) ReadContainerID(pid uint32) (string, error) {
	cgroupPath := filepath.Join(p.procRoot, fmt.Sprintf("%d", pid), "cgroup")

	file, err := os.Open(cgroupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // Process exited
		}
		return "", fmt.Errorf("failed to open cgroup for pid %d: %w", pid, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Parse line (format: `hierarchy-id:subsystems:path`)
		// Example: 0::/system.slice/containerd-7f8d9a0b1c.scope
		for _, re := range containerIDRegexes {
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				return matches[1], nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error scanning cgroup for pid %d: %w", pid, err)
	}

	// No container ID found -> host native process
	return "", nil
}

// ReadComm reads the process command name from `/proc/<pid>/comm`.
// Example output: "nginx", "curl", "cascadeshield".
func (p *ProcReader) ReadComm(pid uint32) (string, error) {
	commPath := filepath.Join(p.procRoot, fmt.Sprintf("%d", pid), "comm")

	content, err := os.ReadFile(commPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("pid-%d", pid), nil
		}
		return "", fmt.Errorf("failed to read comm for pid %d: %w", pid, err)
	}

	comm := strings.TrimSpace(string(content))
	if comm == "" {
		return fmt.Sprintf("pid-%d", pid), nil
	}
	return comm, nil
}
