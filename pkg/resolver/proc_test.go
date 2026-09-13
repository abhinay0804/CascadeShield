package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcReader_ReadContainerID(t *testing.T) {
	tests := []struct {
		name          string
		cgroupContent string
		expectedID    string
	}{
		{
			name:          "cgroup v2 containerd",
			cgroupContent: "0::/system.slice/containerd-1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b.scope\n",
			expectedID:    "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b",
		},
		{
			name:          "cgroup v1 docker",
			cgroupContent: "12:pids:/docker/1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b\n11:memory:/docker/1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b\n",
			expectedID:    "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b",
		},
		{
			name:          "cgroup v2 crio",
			cgroupContent: "0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod12345678_9abc_def0_1234_56789abcdef0.slice/crio-1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b.scope\n",
			expectedID:    "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b",
		},
		{
			name:          "host native process",
			cgroupContent: "0::/user.slice/user-1000.slice/session-1.scope\n",
			expectedID:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			pidDir := filepath.Join(tmpDir, "1234")
			if err := os.MkdirAll(pidDir, 0755); err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}

			cgroupFile := filepath.Join(pidDir, "cgroup")
			if err := os.WriteFile(cgroupFile, []byte(tt.cgroupContent), 0644); err != nil {
				t.Fatalf("failed to write mock cgroup file: %v", err)
			}

			reader := NewProcReader(tmpDir)
			containerID, err := reader.ReadContainerID(1234)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if containerID != tt.expectedID {
				t.Errorf("expected container ID %q, got %q", tt.expectedID, containerID)
			}
		})
	}
}

func TestProcReader_ReadComm(t *testing.T) {
	tmpDir := t.TempDir()
	pidDir := filepath.Join(tmpDir, "5678")
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	commFile := filepath.Join(pidDir, "comm")
	if err := os.WriteFile(commFile, []byte("nginx\n"), 0644); err != nil {
		t.Fatalf("failed to write mock comm file: %v", err)
	}

	reader := NewProcReader(tmpDir)
	comm, err := reader.ReadComm(5678)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comm != "nginx" {
		t.Errorf("expected comm 'nginx', got %q", comm)
	}
}
