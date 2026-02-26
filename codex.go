package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// rolloutPayload represents the relevant fields from a rollout JSONL line.
type rolloutPayload struct {
	Payload struct {
		Type string `json:"type"`
	} `json:"payload"`
}

// codexThreadInfo holds metadata fetched from the Codex SQLite database.
type codexThreadInfo struct {
	Title       string
	RolloutPath string
	CWD         string
}

// discoverCodex finds all running Codex processes and determines their status.
func discoverCodex() []AgentSession {
	pids := findCodexPIDs()
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
			session := probeCodexPID(pid)
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

// findCodexPIDs scans /proc/*/cmdline to find PIDs where cmdline ends with "codex/codex".
func findCodexPIDs() []int {
	entries, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return nil
	}

	re := regexp.MustCompile(`codex/codex$`)
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

// probeCodexPID examines a single Codex process and returns its session info.
// Strategy: find open rollout file via /proc/fd, then enrich with DB metadata.
func probeCodexPID(pid int) *AgentSession {
	rolloutPath, threadID := findRolloutFile(pid)
	if rolloutPath == "" {
		return nil
	}

	status := readRolloutStatus(rolloutPath)
	cwd := readProcCwd(pid)
	title := "-"

	// Enrich from DB — title and cwd (DB cwd is the original launch dir).
	if info := lookupCodexThread(threadID); info != nil {
		title = info.Title
		if info.CWD != "" {
			cwd = info.CWD
		}
	}

	return &AgentSession{
		Agent:     "codex",
		Status:    status,
		SessionID: threadID,
		Title:     title,
		Directory: cwd,
		PID:       pid,
	}
}

// findRolloutFile scans /proc/{pid}/fd/* for a symlink pointing to a rollout JSONL file.
// Returns the file path and extracted thread ID (UUID).
func findRolloutFile(pid int) (path string, threadID string) {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return "", ""
	}

	// Filename: rollout-2026-02-26T23-51-07-019c9aa5-8f55-7833-b235-d00a5faa09d0.jsonl
	// Extract the trailing UUID (8-4-4-4-12 hex).
	re := regexp.MustCompile(`rollout.*?([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$`)

	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			continue
		}
		matches := re.FindStringSubmatch(link)
		if len(matches) >= 2 {
			return link, matches[1]
		}
	}
	return "", ""
}

// readRolloutStatus reads the last line of a rollout JSONL file and extracts the status.
func readRolloutStatus(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "unknown"
	}
	defer f.Close()

	var lastLine string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			lastLine = line
		}
	}

	if lastLine == "" {
		return "unknown"
	}

	var payload rolloutPayload
	if err := json.Unmarshal([]byte(lastLine), &payload); err != nil {
		return "unknown"
	}

	if payload.Payload.Type == "task_complete" {
		return "idle"
	}
	return "busy"
}

// readProcCwd reads the current working directory of a process from /proc.
func readProcCwd(pid int) string {
	link, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		return "-"
	}
	return link
}

// lookupCodexThread queries the Codex SQLite database for thread metadata.
// The threads table stores title, rollout_path, and cwd per thread.
func lookupCodexThread(threadID string) *codexThreadInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	db, err := sql.Open("sqlite", dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return nil
	}
	defer db.Close()

	var info codexThreadInfo
	err = db.QueryRow(
		"SELECT title, rollout_path, cwd FROM threads WHERE id = ?",
		threadID,
	).Scan(&info.Title, &info.RolloutPath, &info.CWD)
	if err != nil {
		return nil
	}
	return &info
}
