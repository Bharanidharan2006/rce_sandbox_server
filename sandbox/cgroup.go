package sandbox

import (
	"fmt"
	"log"
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
			cgroupPath := filepath.Join("/sys/fs/cgroup", parts[2])
			// Verify the path exists and we can write to it
			if info, err := os.Stat(cgroupPath); err == nil && info.IsDir() {
				return cgroupPath
			}
		}
	}
	return "/sys/fs/cgroup"
}

// ensureSubtreeControllers tries to enable the required controllers on the
// parent cgroup. On systemd-managed cgroups the controllers might already be
// enabled, or the write may fail because processes live in the parent node
// (the cgroup v2 "no internal processes" rule). This helper handles both
// situations gracefully.
func ensureSubtreeControllers(parentCgroup string) error {
	needed := []string{"+memory", "+cpu", "+pids"}

	// Check which controllers are already enabled
	existing, _ := os.ReadFile(filepath.Join(parentCgroup, "cgroup.subtree_control"))
	existingStr := strings.TrimSpace(string(existing))

	// Check which controllers are available
	available, _ := os.ReadFile(filepath.Join(parentCgroup, "cgroup.controllers"))
	availableStr := strings.TrimSpace(string(available))

	log.Printf("cgroup parent: %s", parentCgroup)
	log.Printf("cgroup available controllers: %s", availableStr)
	log.Printf("cgroup enabled subtree_control: %s", existingStr)

	var toEnable []string
	for _, ctrl := range needed {
		ctrlName := strings.TrimPrefix(ctrl, "+")
		if !strings.Contains(existingStr, ctrlName) && strings.Contains(availableStr, ctrlName) {
			toEnable = append(toEnable, ctrl)
		}
	}

	if len(toEnable) == 0 {
		// All needed controllers are already enabled (or not available)
		return nil
	}

	// Move all processes out of the parent to satisfy the leaf rule
	supervisorPath := filepath.Join(parentCgroup, "supervisor")
	if err := os.MkdirAll(supervisorPath, 0755); err != nil {
		return fmt.Errorf("failed to create supervisor cgroup: %v", err)
	}

	procsData, err := os.ReadFile(filepath.Join(parentCgroup, "cgroup.procs"))
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(procsData)), "\n")
		for _, pidStr := range lines {
			if pidStr != "" {
				_ = os.WriteFile(filepath.Join(supervisorPath, "cgroup.procs"), []byte(pidStr), 0644)
			}
		}
	}

	// Now try to enable controllers
	enableStr := strings.Join(toEnable, " ")
	if err := os.WriteFile(filepath.Join(parentCgroup, "cgroup.subtree_control"), []byte(enableStr), 0644); err != nil {
		return fmt.Errorf("failed to enable subtree_control (%s): %v", enableStr, err)
	}

	return nil
}

func attachToCGroup(pid int) (string, error) {
	parentCgroup := getDelegatedCgroup()

	// Try to enable controllers on the parent cgroup
	if err := ensureSubtreeControllers(parentCgroup); err != nil {
		log.Printf("warning: subtree_control setup failed: %v", err)
		// Still try to create the cgroup – if controllers were already
		// enabled by systemd or an ancestor, this will work.
	}

	// Create the sandbox leaf node
	cgPath := filepath.Join(parentCgroup, "container_"+strconv.Itoa(pid))
	if err := os.MkdirAll(cgPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create sandbox cgroup %s: %v", cgPath, err)
	}

	// Apply limits – each write is best-effort so one failure doesn't
	// prevent the remaining limits from being applied.
	limits := []struct {
		file  string
		value string
	}{
		{"memory.max", "50000000"},
		{"pids.max", "10"},
		{"memory.swap.max", "0"},
		{"cpu.max", "50000 100000"},
	}

	for _, l := range limits {
		p := filepath.Join(cgPath, l.file)
		if err := os.WriteFile(p, []byte(l.value), 0644); err != nil {
			log.Printf("warning: could not write %s=%s: %v", l.file, l.value, err)
			// Non-fatal: the controller file may not exist if the
			// controller wasn't enabled, but the sandbox can still run.
		}
	}

	// Move the child process into the sandbox cgroup
	pidStr := strconv.Itoa(pid)
	if err := os.WriteFile(filepath.Join(cgPath, "cgroup.procs"), []byte(pidStr), 0644); err != nil {
		// Clean up
		os.Remove(cgPath)
		return "", fmt.Errorf("failed to attach pid %d to cgroup: %v", pid, err)
	}

	return cgPath, nil
}
