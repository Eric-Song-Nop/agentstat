//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// findPIDsByName scans /proc/*/cmdline and returns PIDs whose argv[0] matches re.
func findPIDsByName(re *regexp.Regexp) []int {
	entries, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return nil
	}

	var pids []int
	for _, entry := range entries {
		data, err := os.ReadFile(entry)
		if err != nil {
			continue
		}
		// cmdline is null-delimited; take the first arg (the binary path).
		args := strings.Split(string(data), "\x00")
		if len(args) == 0 {
			continue
		}
		if re.MatchString(args[0]) {
			parts := strings.Split(entry, "/")
			if len(parts) >= 3 {
				pid, err := strconv.Atoi(parts[2])
				if err == nil {
					pids = append(pids, pid)
				}
			}
		}
	}
	return pids
}

// listOpenFiles returns absolute file paths of all open FDs for a process
// by reading /proc/{pid}/fd/* symlinks.
func listOpenFiles(pid int) []string {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil
	}

	var paths []string
	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			continue
		}
		paths = append(paths, link)
	}
	return paths
}

// readProcessCwd returns the current working directory of a process
// by reading /proc/{pid}/cwd symlink.
func readProcessCwd(pid int) string {
	link, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		return "-"
	}
	return link
}

// listenEntry represents a TCP listening socket.
type listenEntry struct {
	Port int
	PID  int
	Cmd  string
}

// findListenTCP parses `ss -tlnp` output and returns all TCP LISTEN sockets.
func findListenTCP() []listenEntry {
	out, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		return nil
	}

	// Example line:
	// LISTEN  0  4096  0.0.0.0:38129  0.0.0.0:*  users:(("opencode",pid=1059916,fd=30))
	re := regexp.MustCompile(`:(\d+)\s+\S+\s+users:\(\("([^"]+)",pid=(\d+),`)

	var entries []listenEntry
	for _, line := range strings.Split(string(out), "\n") {
		matches := re.FindStringSubmatch(line)
		if len(matches) < 4 {
			continue
		}
		port, _ := strconv.Atoi(matches[1])
		pid, _ := strconv.Atoi(matches[3])
		cmd := matches[2]
		if port > 0 && pid > 0 {
			entries = append(entries, listenEntry{Port: port, PID: pid, Cmd: cmd})
		}
	}
	return entries
}
