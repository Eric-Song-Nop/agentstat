package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
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

// discoverOpenCode finds all running OpenCode instances.
// Each process = one AgentSession. Status is "busy"/"retry" if any session is active, otherwise "idle".
func discoverOpenCode() []AgentSession {
	instances := findOpenCodeInstances()
	if len(instances) == 0 {
		return nil
	}

	var mu sync.Mutex
	var results []AgentSession
	var wg sync.WaitGroup

	for _, inst := range instances {
		wg.Add(1)
		go func(inst openCodeInstance) {
			defer wg.Done()
			session := queryOpenCodeInstance(inst)
			if session != nil {
				mu.Lock()
				results = append(results, *session)
				mu.Unlock()
			}
		}(inst)
	}

	wg.Wait()
	return results
}

// findOpenCodeInstances uses findListenTCP to discover all opencode listening ports.
// Deduplicates by PID (a single process may listen on multiple ports).
func findOpenCodeInstances() []openCodeInstance {
	entries := findListenTCP()

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
func queryOpenCodeInstance(inst openCodeInstance) *AgentSession {
	base := fmt.Sprintf("http://localhost:%d", inst.Port)

	statusMap := fetchSessionStatus(base)

	// If any session is busy/retry, report that session's metadata.
	for id, entry := range statusMap {
		result := &AgentSession{
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
	return &AgentSession{
		Agent:  "opencode",
		Status: "idle",
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
