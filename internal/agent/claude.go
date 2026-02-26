package agent

import (
	"bufio"
	"encoding/json"
	"io"
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
//
// Deterministic rule (based on Claude Code JSONL protocol):
//   - Each turn ends with a system/turn_duration entry
//   - assistant entries only appear within a turn
//   - Therefore: last turn_duration after last assistant → idle; otherwise → busy
//
// Performance: for files > 128KB, only the trailing 128KB is scanned.
func readClaudeStatus(jsonlPath string) (status, slug, cwd string) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return model.StatusUnknown, "", ""
	}
	defer f.Close()

	// Performance optimization: seek to tail for large files.
	const tailSize = 128 * 1024
	fi, err := f.Stat()
	if err != nil {
		return model.StatusUnknown, "", ""
	}
	if fi.Size() > tailSize {
		if _, err := f.Seek(fi.Size()-tailSize, io.SeekStart); err != nil {
			return model.StatusUnknown, "", ""
		}
		// Discard the first (potentially truncated) line after seeking.
		r := bufio.NewReader(f)
		if _, err := r.ReadBytes('\n'); err != nil {
			return model.StatusUnknown, "", ""
		}
		// Continue scanning from the buffered reader via a new scanner.
		return scanClaudeJSONL(r)
	}

	return scanClaudeJSONL(f)
}

// scanClaudeJSONL performs a forward scan over a reader, tracking the last line
// positions of turn_duration and assistant entries to determine session status.
func scanClaudeJSONL(r io.Reader) (status, slug, cwd string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	var lastTurnDuration, lastAssistant int = -1, -1
	lineNum := 0

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			lineNum++
			continue
		}

		var entry claudeJSONLEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			lineNum++
			continue
		}

		// Continuously update slug and cwd to their latest values.
		if entry.Slug != "" {
			slug = entry.Slug
		}
		if entry.CWD != "" {
			cwd = entry.CWD
		}

		switch entry.Type {
		case "system":
			if entry.Subtype == "turn_duration" {
				lastTurnDuration = lineNum
			}
		case "assistant":
			lastAssistant = lineNum
		}

		lineNum++
	}

	// Deterministic status: compare last positions of the two markers.
	switch {
	case lastTurnDuration > lastAssistant:
		status = model.StatusIdle // Last turn has ended.
	case lastAssistant > lastTurnDuration:
		status = model.StatusBusy // Currently within a turn.
	default:
		// Both -1 (no turn records) → new session waiting for input.
		status = model.StatusIdle
	}

	return status, slug, cwd
}
