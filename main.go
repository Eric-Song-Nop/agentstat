package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/Eric-Song-Nop/agentstat/internal/agent"
	"github.com/Eric-Song-Nop/agentstat/internal/model"
)

// parseAgents parses a comma-separated agent list and validates names.
// Returns nil if input is empty (meaning "all agents").
func parseAgents(raw string) map[string]bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	known := make(map[string]bool, len(model.AllAgents))
	for _, a := range model.AllAgents {
		known[a] = true
	}

	selected := make(map[string]bool)
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(strings.ToLower(name))
		if name == "" {
			continue
		}
		if !known[name] {
			fmt.Fprintf(os.Stderr, "warning: unknown agent %q (known: %s)\n", name, strings.Join(model.AllAgents, ", "))
			continue
		}
		selected[name] = true
	}
	return selected
}

// agentEnabled reports whether the named agent should be discovered.
// A nil selected map means all agents are enabled.
func agentEnabled(selected map[string]bool, name string) bool {
	if selected == nil {
		return true
	}
	return selected[name]
}

func main() {
	jsonFlag := flag.Bool("json", false, "output in JSON format")
	agentsFlag := flag.String("agents", "", "comma-separated list of agents to discover (opencode,codex,claude,amp); default: all")
	flag.Parse()

	agents := parseAgents(*agentsFlag)

	var sessions []model.AgentSession

	if agentEnabled(agents, "opencode") {
		sessions = append(sessions, agent.DiscoverOpenCode()...)
	}
	if agentEnabled(agents, "codex") {
		sessions = append(sessions, agent.DiscoverCodex()...)
	}
	if agentEnabled(agents, "claude") {
		sessions = append(sessions, agent.DiscoverClaude()...)
	}
	if agentEnabled(agents, "amp") {
		sessions = append(sessions, agent.DiscoverAmp()...)
	}

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
