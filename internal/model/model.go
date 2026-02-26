package model

// Status constants for agent sessions.
const (
	StatusBusy    = "busy"
	StatusIdle    = "idle"
	StatusRetry   = "retry"
	StatusUnknown = "unknown"
)

// AgentSession represents a single discovered agent session.
type AgentSession struct {
	Agent     string `json:"agent"`      // "opencode" | "codex" | "claude" | "amp" | "gemini"
	Status    string `json:"status"`     // "busy" | "idle" | "retry" | "unknown"
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	PID       int    `json:"pid"`
}

// AllAgents lists the known agent names for validation.
var AllAgents = []string{"opencode", "codex", "claude", "amp", "gemini"}
