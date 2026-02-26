//go:build darwin

package main

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// findPIDsByName runs `ps ax -o pid,command` and returns PIDs whose command matches re.
func findPIDsByName(re *regexp.Regexp) []int {
	out, err := exec.Command("ps", "ax", "-o", "pid,command").Output()
	if err != nil {
		return nil
	}

	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Each line: "  PID COMMAND ARGS..."
		// Split into at most 2 fields: PID and the rest (command + args).
		fields := strings.SplitN(line, " ", 2)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		cmd := strings.TrimSpace(fields[1])
		// Match only the first argument (binary path), equivalent to argv[0].
		argv0 := strings.SplitN(cmd, " ", 2)[0]
		if re.MatchString(argv0) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// listOpenFiles runs `lsof -p PID -Fn` and returns absolute file paths
// of all open file descriptors for the given process.
func listOpenFiles(pid int) []string {
	out, err := exec.Command("lsof", "-p", strconv.Itoa(pid), "-Fn").Output()
	if err != nil {
		return nil
	}

	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		// lsof -Fn outputs lines prefixed with field character.
		// "n" lines contain the file name; keep only absolute paths.
		if strings.HasPrefix(line, "n/") {
			paths = append(paths, line[1:])
		}
	}
	return paths
}

// readProcessCwd runs `lsof -a -p PID -d cwd -Fn` and returns the
// current working directory of the given process.
func readProcessCwd(pid int) string {
	out, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return "-"
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n/") {
			return line[1:]
		}
	}
	return "-"
}

// listenEntry represents a TCP listening socket.
type listenEntry struct {
	Port int
	PID  int
	Cmd  string
}

// findListenTCP runs `lsof -iTCP -sTCP:LISTEN -nP -Fpcn` and returns
// all TCP LISTEN sockets with their PID, port, and command name.
func findListenTCP() []listenEntry {
	out, err := exec.Command("lsof", "-iTCP", "-sTCP:LISTEN", "-nP", "-Fpcn").Output()
	if err != nil {
		return nil
	}

	// lsof -Fpcn outputs grouped records:
	//   p<PID>        — new process group
	//   c<command>    — command name
	//   n<name>       — network name (e.g. "*:8080" or "127.0.0.1:3000")
	// A single PID may have multiple "n" lines (multiple listening ports).
	var entries []listenEntry
	var curPID int
	var curCmd string

	for _, line := range strings.Split(string(out), "\n") {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, err := strconv.Atoi(line[1:])
			if err == nil {
				curPID = pid
			}
		case 'c':
			curCmd = line[1:]
		case 'n':
			// Extract port from "host:port" format.
			name := line[1:]
			idx := strings.LastIndex(name, ":")
			if idx < 0 {
				continue
			}
			port, err := strconv.Atoi(name[idx+1:])
			if err != nil || port <= 0 {
				continue
			}
			if curPID > 0 {
				entries = append(entries, listenEntry{Port: port, PID: curPID, Cmd: curCmd})
			}
		}
	}
	return entries
}
