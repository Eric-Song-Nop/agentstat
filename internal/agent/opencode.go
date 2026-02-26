package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Eric-Song-Nop/agentstat/internal/model"
	"github.com/Eric-Song-Nop/agentstat/internal/platform"
)

// openCodeInstance represents a discovered OpenCode TUI instance.
type openCodeInstance struct {
	Port int
	PID  int
}

// sessionStatusEntry represents one entry from /session/status response.
type sessionStatusEntry struct {
	Type string `json:"type"` // "busy", "retry", etc.
}

// sessionListEntry represents one session from /session response.
type sessionListEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	Time      struct {
		Updated int64 `json:"updated"`
	} `json:"time"`
}

var httpClient = &http.Client{Timeout: 500 * time.Millisecond}

// DiscoverOpenCode finds all running OpenCode instances.
// Each process = one AgentSession. Status is "busy"/"retry" if any session is active, otherwise "idle".
func DiscoverOpenCode() []model.AgentSession {
	instances := findOpenCodeInstances()
	if len(instances) == 0 {
		return nil
	}
	return ConcurrentProbe(instances, queryOpenCodeInstance)
}

// findOpenCodeInstances uses FindListenTCP to discover all opencode listening ports.
// Deduplicates by PID (a single process may listen on multiple ports).
func findOpenCodeInstances() []openCodeInstance {
	entries := platform.P.FindListenTCP()

	seen := make(map[int]bool)
	var instances []openCodeInstance
	for _, e := range entries {
		if strings.EqualFold(e.Cmd, "opencode") && !seen[e.PID] {
			seen[e.PID] = true
			instances = append(instances, openCodeInstance{Port: e.Port, PID: e.PID})
		}
	}
	return instances
}

// queryOpenCodeInstance queries a single OpenCode process and returns one AgentSession.
// Only populates session metadata (ID, title, directory) when busy.
// When idle, those fields are left empty — we can't reliably determine which session the TUI is viewing.
func queryOpenCodeInstance(inst openCodeInstance) *model.AgentSession {
	base := fmt.Sprintf("http://localhost:%d", inst.Port)

	statusMap := fetchSessionStatus(base)

	// If any session is busy/retry, report that session's metadata.
	for id, entry := range statusMap {
		result := &model.AgentSession{
			Agent:     "opencode",
			Status:    entry.Type,
			SessionID: id,
			PID:       inst.PID,
		}
		// Enrich with title/directory from session list.
		if sessions := fetchSessionList(base); sessions != nil {
			for _, s := range sessions {
				if s.ID == id {
					result.Title = s.Title
					result.Directory = s.Directory
					break
				}
			}
		}
		return result
	}

	// No busy session — report idle with no session metadata.
	return &model.AgentSession{
		Agent:  "opencode",
		Status: model.StatusIdle,
		PID:    inst.PID,
	}
}

// fetchSessionList calls GET /session and returns the session list.
func fetchSessionList(base string) []sessionListEntry {
	resp, err := httpClient.Get(base + "/session")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var sessions []sessionListEntry
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil
	}
	return sessions
}

// fetchSessionStatus calls GET /session/status and returns the status map.
func fetchSessionStatus(base string) map[string]sessionStatusEntry {
	resp, err := httpClient.Get(base + "/session/status")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var statusMap map[string]sessionStatusEntry
	if err := json.NewDecoder(resp.Body).Decode(&statusMap); err != nil {
		return nil
	}
	return statusMap
}
