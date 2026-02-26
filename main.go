package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// AgentSession represents a single discovered agent session.
type AgentSession struct {
	Agent     string `json:"agent"`      // "opencode" | "codex"
	Status    string `json:"status"`     // "busy" | "idle" | "retry" | "unknown"
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	PID       int    `json:"pid"`
}

func main() {
	jsonFlag := flag.Bool("json", false, "output in JSON format")
	flag.Parse()

	var sessions []AgentSession

	// Discover OpenCode instances
	ocSessions := discoverOpenCode()
	sessions = append(sessions, ocSessions...)

	// Discover Codex instances
	cxSessions := discoverCodex()
	sessions = append(sessions, cxSessions...)

	if len(sessions) == 0 {
		if *jsonFlag {
			fmt.Println("[]")
		} else {
			fmt.Println("No agent sessions found.")
		}
		os.Exit(0)
	}

	if *jsonFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(sessions)
		return
	}

	// Aligned table output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tSTATUS\tSESSION\tTITLE\tDIRECTORY\tPID")
	for _, s := range sessions {
		title := truncate(s.Title, 28)
		sessionID := truncate(s.SessionID, 38)
		dir := shortenHome(s.Directory)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n",
			s.Agent, s.Status, sessionID, title, dir, s.PID)
	}
	w.Flush()
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// shortenHome replaces the user's home directory prefix with "~".
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
