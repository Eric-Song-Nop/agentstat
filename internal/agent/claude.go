package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	return ConcurrentProbe(pids, probeClaudePID)
}

// findClaudePIDs returns PIDs of processes whose binary is "claude".
func findClaudePIDs() []int {
	re := regexp.MustCompile(`(^|/)claude$`)
	return platform.P.FindPIDsByName(re)
}

// probeClaudePID examines a single Claude Code process and returns its session info.
func probeClaudePID(pid int) *model.AgentSession {
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

// findClaudeSessionLocks inspects open files of a process for .claude/tasks/{uuid}/.lock
// and returns deduplicated session UUIDs.
func findClaudeSessionLocks(pid int) []string {
	files := platform.P.ListOpenFiles(pid)

	re := regexp.MustCompile(`/\.claude/tasks/([0-9a-f-]{36})/\.lock$`)
	seen := make(map[string]bool)
	var ids []string

	for _, f := range files {
		matches := re.FindStringSubmatch(f)
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
