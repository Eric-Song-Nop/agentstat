package agent

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Eric-Song-Nop/agentstat/internal/model"
	"github.com/Eric-Song-Nop/agentstat/internal/platform"
)

// ampThreadFile holds a parsed Amp thread JSON file with its mtime.
type ampThreadFile struct {
	Path    string
	ModTime time.Time
	Data    ampThread
}

// ampThread is the top-level structure of ~/.local/share/amp/threads/*.json.
type ampThread struct {
	Env      ampEnv       `json:"env"`
	Messages []ampMessage `json:"messages"`
}

// ampEnv contains the initial environment for an Amp session.
type ampEnv struct {
	Initial ampInitial `json:"initial"`
}

// ampInitial holds workspace trees and platform info.
type ampInitial struct {
	Trees []ampTree `json:"trees"`
}

// ampTree represents a single workspace tree entry.
type ampTree struct {
	DisplayName string `json:"displayName"`
	URI         string `json:"uri"`
}

// ampMessage represents a single message in the Amp thread.
type ampMessage struct {
	Role  string   `json:"role"`
	State ampState `json:"state"`
}

// ampState holds the assistant message's execution state.
type ampState struct {
	Type       string `json:"type"`
	StopReason string `json:"stopReason"`
}

// DiscoverAmp finds all running Amp Code processes and determines their status.
func DiscoverAmp() []model.AgentSession {
	pids := findAmpPIDs()
	if len(pids) == 0 {
		return nil
	}

	threads := loadAmpThreads()
	if len(threads) == 0 {
		// Processes exist but no thread files — report unknown status.
		return ConcurrentProbe(pids, func(pid int) *model.AgentSession {
			cwd := platform.P.ReadProcessCwd(pid)
			return &model.AgentSession{
				Agent:     "amp",
				Status:    model.StatusUnknown,
				Directory: cwd,
				PID:       pid,
			}
		})
	}

	return ConcurrentProbe(pids, func(pid int) *model.AgentSession {
		return probeAmpPID(pid, threads)
	})
}

// findAmpPIDs returns PIDs of Amp Code processes.
// Amp runs as `node --no-warnings ~/.local/share/pnpm/amp`, so argv[0] is "node".
// We match any argument ending with /amp or equal to "amp".
func findAmpPIDs() []int {
	re := regexp.MustCompile(`(^|/)amp$`)
	return platform.P.FindPIDsByArgs(re)
}

// loadAmpThreads scans ~/.local/share/amp/threads/*.json and parses each file.
func loadAmpThreads() []ampThreadFile {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	threadsDir := filepath.Join(home, ".local", "share", "amp", "threads")
	entries, err := os.ReadDir(threadsDir)
	if err != nil {
		return nil
	}

	var threads []ampThreadFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		path := filepath.Join(threadsDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var thread ampThread
		if json.Unmarshal(data, &thread) != nil {
			continue
		}

		threads = append(threads, ampThreadFile{
			Path:    path,
			ModTime: info.ModTime(),
			Data:    thread,
		})
	}

	return threads
}

// probeAmpPID examines a single Amp process and returns its session info.
func probeAmpPID(pid int, threads []ampThreadFile) *model.AgentSession {
	cwd := platform.P.ReadProcessCwd(pid)
	if cwd == "" || cwd == "-" {
		return nil
	}

	thread := matchThreadByCwd(cwd, threads)
	if thread == nil {
		return &model.AgentSession{
			Agent:     "amp",
			Status:    model.StatusUnknown,
			Directory: cwd,
			PID:       pid,
		}
	}

	status := ampStatusFromThread(&thread.Data)

	// Use the thread filename (without extension) as session ID.
	sessionID := strings.TrimSuffix(filepath.Base(thread.Path), ".json")

	// Use the first tree's display name as title, if available.
	title := "-"
	if len(thread.Data.Env.Initial.Trees) > 0 {
		title = thread.Data.Env.Initial.Trees[0].DisplayName
	}

	return &model.AgentSession{
		Agent:     "amp",
		Status:    status,
		SessionID: sessionID,
		Title:     title,
		Directory: cwd,
		PID:       pid,
	}
}

// matchThreadByCwd finds the thread whose workspace tree URI matches the given CWD.
// When multiple threads match, the one with the most recent mtime wins.
func matchThreadByCwd(cwd string, threads []ampThreadFile) *ampThreadFile {
	// Sort by mtime descending so we naturally pick the newest match.
	sorted := make([]ampThreadFile, len(threads))
	copy(sorted, threads)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ModTime.After(sorted[j].ModTime)
	})

	for i := range sorted {
		for _, tree := range sorted[i].Data.Env.Initial.Trees {
			treePath := uriToPath(tree.URI)
			if treePath == "" {
				continue
			}
			// CWD may be inside the workspace tree directory.
			if cwd == treePath || strings.HasPrefix(cwd, treePath+"/") {
				return &sorted[i]
			}
		}
	}
	return nil
}

// ampStatusFromThread reads the last assistant message's state to determine status.
func ampStatusFromThread(thread *ampThread) string {
	// Walk messages in reverse to find the last assistant message.
	for i := len(thread.Messages) - 1; i >= 0; i-- {
		msg := thread.Messages[i]
		if msg.Role != "assistant" {
			continue
		}
		return ampStatus(msg.State)
	}
	// No assistant message found — session just started or empty.
	return model.StatusIdle
}

// ampStatus maps an Amp message state to a model status.
//
// | state.type   | state.stopReason | → Status |
// |-------------|-----------------|----------|
// | "streaming" | —               | BUSY     |
// | "complete"  | "tool_use"      | BUSY     |
// | "complete"  | "end_turn"      | IDLE     |
// | "cancelled" | —               | IDLE     |
// | "error"     | —               | IDLE     |
// | ""          | —               | IDLE     |
func ampStatus(state ampState) string {
	switch state.Type {
	case "streaming":
		return model.StatusBusy
	case "complete":
		if state.StopReason == "tool_use" {
			return model.StatusBusy
		}
		return model.StatusIdle
	case "cancelled", "error":
		return model.StatusIdle
	default:
		return model.StatusIdle
	}
}

// uriToPath converts a file:// URI to a local filesystem path.
// Returns empty string for non-file URIs or parse errors.
func uriToPath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return ""
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return parsed.Path
}
