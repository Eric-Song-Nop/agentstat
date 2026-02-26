package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Eric-Song-Nop/agentstat/internal/model"
	"github.com/Eric-Song-Nop/agentstat/internal/platform"
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

// DiscoverClaude finds all running Claude Code processes and determines their status.
func DiscoverClaude() []model.AgentSession {
	pids := findClaudePIDs()
	if len(pids) == 0 {
		return nil
	}

	pidMap := buildPIDSessionMap(pids)

	return ConcurrentProbe(pids, func(pid int) *model.AgentSession {
		return probeClaudePID(pid, pidMap)
	})
}

// findClaudePIDs returns PIDs of processes whose binary is "claude".
func findClaudePIDs() []int {
	re := regexp.MustCompile(`(^|/)claude$`)
	return platform.P.FindPIDsByName(re)
}

// debugFileInfo pairs a debug log path with its modification time for sorting.
type debugFileInfo struct {
	path    string
	modTime time.Time
}

// buildPIDSessionMap scans ~/.claude/debug/*.txt to build a PID → SessionID mapping.
//
// Each debug log is named {sessionId}.txt. Inside the file, lines contain temporary
// file references like ".tmp.{PID}." which reveal which PID owns that session.
// Files are scanned in mtime-descending order (newest first) so active sessions
// are found quickly. Scanning stops as soon as all target PIDs are mapped.
func buildPIDSessionMap(pids []int) map[int]string {
	pidMap := make(map[int]string, len(pids))
	if len(pids) == 0 {
		return pidMap
	}

	// Build target PID set for O(1) lookup.
	target := make(map[int]bool, len(pids))
	for _, pid := range pids {
		target[pid] = true
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return pidMap
	}

	debugDir := filepath.Join(home, ".claude", "debug")
	entries, err := os.ReadDir(debugDir)
	if err != nil {
		return pidMap
	}

	// Collect .txt files with their modification times.
	var files []debugFileInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, debugFileInfo{
			path:    filepath.Join(debugDir, e.Name()),
			modTime: info.ModTime(),
		})
	}

	// Sort by mtime descending — newest files first.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	re := regexp.MustCompile(`\.tmp\.(\d+)\.`)
	remaining := len(target)

	for _, df := range files {
		if remaining == 0 {
			break
		}

		pid := extractPIDFromDebugLog(df.path, re, target, pidMap)
		if pid != 0 {
			// Derive sessionID from filename: {sessionId}.txt → {sessionId}
			base := filepath.Base(df.path)
			sessionID := strings.TrimSuffix(base, ".txt")
			pidMap[pid] = sessionID
			remaining--
		}
	}

	return pidMap
}

// extractPIDFromDebugLog reads a debug log line-by-line looking for a .tmp.{PID}. pattern.
// Returns the matched PID if it's in the target set and not yet mapped, otherwise 0.
func extractPIDFromDebugLog(path string, re *regexp.Regexp, target map[int]bool, mapped map[int]string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		matches := re.FindStringSubmatch(scanner.Text())
		if len(matches) < 2 {
			continue
		}
		pid, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		if target[pid] && mapped[pid] == "" {
			return pid
		}
	}
	return 0
}

// probeClaudePID examines a single Claude Code process and returns its session info.
func probeClaudePID(pid int, pidMap map[int]string) *model.AgentSession {
	sessionID, ok := pidMap[pid]
	if !ok || sessionID == "" {
		return nil
	}

	info := resolveClaudeSession(sessionID)
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
		dir = platform.P.ReadProcessCwd(pid)
	}

	return &model.AgentSession{
		Agent:     "claude",
		Status:    status,
		SessionID: info.SessionID,
		Title:     title,
		Directory: dir,
		PID:       pid,
	}
}

// resolveClaudeSession finds the JSONL file for a session ID under ~/.claude/projects/.
func resolveClaudeSession(sessionID string) *claudeSessionInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	projectsDir := filepath.Join(home, ".claude", "projects")
	pattern := filepath.Join(projectsDir, "*", sessionID+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return nil
	}

	// If multiple project dirs contain the same sessionID, pick the most recent.
	var best *claudeSessionInfo
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil {
			continue
		}
		if best == nil || fi.ModTime().After(best.ModTime) {
			best = &claudeSessionInfo{
				SessionID: sessionID,
				JSONLPath: m,
				ModTime:   fi.ModTime(),
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
		return model.StatusUnknown, "", ""
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
		return model.StatusUnknown, "", ""
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
				return model.StatusIdle, slug, cwd
			}
			// Other system subtypes — keep searching.
		case "assistant":
			return model.StatusBusy, slug, cwd
		case "user":
			return model.StatusBusy, slug, cwd
		}
	}

	return model.StatusUnknown, slug, cwd
}
