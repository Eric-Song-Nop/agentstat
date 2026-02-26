package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// claudeSessionInfo holds metadata extracted from a Claude Code session JSONL.
type claudeSessionInfo struct {
	SessionID string
	Slug      string
	CWD       string
	JSONLPath string
	ModTime   time.Time
}

// claudeJSONLEntry represents the relevant fields from a Claude Code JSONL line.
type claudeJSONLEntry struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Slug    string `json:"slug"`
	CWD     string `json:"cwd"`
}

// discoverClaude finds all running Claude Code processes and determines their status.
func discoverClaude() []AgentSession {
	pids := findClaudePIDs()
	if len(pids) == 0 {
		return nil
	}

	var mu sync.Mutex
	var results []AgentSession
	var wg sync.WaitGroup

	for _, pid := range pids {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			session := probeClaudePID(pid)
			if session != nil {
				mu.Lock()
				results = append(results, *session)
				mu.Unlock()
			}
		}(pid)
	}

	wg.Wait()
	return results
}

// findClaudePIDs scans /proc/*/cmdline to find PIDs where the binary is "claude".
func findClaudePIDs() []int {
	entries, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return nil
	}

	re := regexp.MustCompile(`(^|/)claude$`)
	var pids []int

	for _, entry := range entries {
		data, err := os.ReadFile(entry)
		if err != nil {
			continue
		}
		// cmdline is null-delimited; take the first arg (the binary path)
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

// probeClaudePID examines a single Claude Code process and returns its session info.
func probeClaudePID(pid int) *AgentSession {
	sessionIDs := findClaudeSessionLocks(pid)
	if len(sessionIDs) == 0 {
		return nil
	}

	info := resolveClaudeSession(sessionIDs)
	if info == nil {
		return nil
	}

	status, slug, cwd := readClaudeStatus(info.JSONLPath)

	title := slug
	if title == "" {
		title = "-"
	}

	dir := cwd
	if dir == "" {
		dir = readProcCwd(pid)
	}

	return &AgentSession{
		Agent:     "claude",
		Status:    status,
		SessionID: info.SessionID,
		Title:     title,
		Directory: dir,
		PID:       pid,
	}
}

// findClaudeSessionLocks scans /proc/{pid}/fd/* for symlinks to .claude/tasks/{uuid}/.lock
// and returns deduplicated session UUIDs.
func findClaudeSessionLocks(pid int) []string {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil
	}

	re := regexp.MustCompile(`/\.claude/tasks/([0-9a-f-]{36})/\.lock$`)
	seen := make(map[string]bool)
	var ids []string

	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			continue
		}
		matches := re.FindStringSubmatch(link)
		if len(matches) >= 2 && !seen[matches[1]] {
			seen[matches[1]] = true
			ids = append(ids, matches[1])
		}
	}
	return ids
}

// resolveClaudeSession finds the JSONL file for each session ID under ~/.claude/projects/
// and returns the one with the most recent modification time.
func resolveClaudeSession(sessionIDs []string) *claudeSessionInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	projectsDir := filepath.Join(home, ".claude", "projects")
	var best *claudeSessionInfo

	for _, sid := range sessionIDs {
		pattern := filepath.Join(projectsDir, "*", sid+".jsonl")
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			continue
		}

		for _, m := range matches {
			fi, err := os.Stat(m)
			if err != nil {
				continue
			}
			if best == nil || fi.ModTime().After(best.ModTime) {
				best = &claudeSessionInfo{
					SessionID: sid,
					JSONLPath: m,
					ModTime:   fi.ModTime(),
				}
			}
		}
	}
	return best
}

// readClaudeStatus reads a Claude Code session JSONL and extracts the current status,
// slug (title), and working directory.
func readClaudeStatus(jsonlPath string) (status, slug, cwd string) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return "unknown", "", ""
	}
	defer f.Close()

	// Collect the last N lines for status determination and metadata extraction.
	// We need a small window because the very last line might be a non-status type
	// (e.g. "progress"), so we search backwards for the first meaningful status line.
	const windowSize = 10
	ring := make([]string, 0, windowSize)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(ring) >= windowSize {
			ring = ring[1:]
		}
		ring = append(ring, line)
	}

	if len(ring) == 0 {
		return "unknown", "", ""
	}

	// Extract slug and cwd from the last line that contains them (they appear on most entries).
	for i := len(ring) - 1; i >= 0; i-- {
		var entry claudeJSONLEntry
		if json.Unmarshal([]byte(ring[i]), &entry) != nil {
			continue
		}
		if slug == "" && entry.Slug != "" {
			slug = entry.Slug
		}
		if cwd == "" && entry.CWD != "" {
			cwd = entry.CWD
		}
		if slug != "" && cwd != "" {
			break
		}
	}

	// Determine status by scanning backwards for the first meaningful status line.
	for i := len(ring) - 1; i >= 0; i-- {
		var entry claudeJSONLEntry
		if json.Unmarshal([]byte(ring[i]), &entry) != nil {
			continue
		}
		switch entry.Type {
		case "system":
			if entry.Subtype == "turn_duration" {
				return "idle", slug, cwd
			}
			// Other system subtypes — keep searching.
		case "assistant":
			return "busy", slug, cwd
		case "user":
			return "busy", slug, cwd
		}
	}

	return "unknown", slug, cwd
}
