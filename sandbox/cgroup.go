package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func getDelegatedCgroup() string {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "/sys/fs/cgroup"
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 {
			return filepath.Join("/sys/fs/cgroup", parts[2])
		}
	}
	return "/sys/fs/cgroup"
}

func attachToCGroup(pid int) (string, error) {
	parentCgroup := getDelegatedCgroup()

	// 1. Create the supervisor leaf node
	supervisorPath := filepath.Join(parentCgroup, "supervisor")
	if err := os.MkdirAll(supervisorPath, 0755); err != nil {
		return "", err
	}

	// 2. Read ALL processes currently in the parent and move them ALL
	procsData, err := os.ReadFile(filepath.Join(parentCgroup, "cgroup.procs"))
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(procsData)), "\n")
		for _, pidStr := range lines {
			if pidStr != "" {
				// Sweep every process out of the way to satisfy the Leaf Rule
				_ = os.WriteFile(filepath.Join(supervisorPath, "cgroup.procs"), []byte(pidStr), 0644)
			}
		}
	}

	// 3. The parent is now guaranteed empty. Activate the controllers.
	err = os.WriteFile(filepath.Join(parentCgroup, "cgroup.subtree_control"), []byte("+memory +cpu +pids"), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to enable subtree_control: %v", err)
	}

	// 4. Create the sandbox leaf node (The kernel will now auto-generate memory.max)
	cgPath := filepath.Join(parentCgroup, "container_"+strconv.Itoa(pid))
	if err := os.MkdirAll(cgPath, 0755); err != nil {
		return "", err
	}

	// 5. Apply the strict limits
	if err := os.WriteFile(filepath.Join(cgPath, "memory.max"), []byte("50000000"), 0644); err != nil {
		return "", fmt.Errorf("failed to write memory limit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cgPath, "pids.max"), []byte("10"), 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(cgPath, "memory.swap.max"), []byte("0"), 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(cgPath, "cpu.max"), []byte("50000 100000"), 0644); err != nil {
		return "", err
	}

	// 6. Throw the Python process into the sandbox trap
	pidStr := strconv.Itoa(pid)
	if err := os.WriteFile(filepath.Join(cgPath, "cgroup.procs"), []byte(pidStr), 0644); err != nil {
		return "", err
	}

	return cgPath, nil
}
