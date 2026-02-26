# agentstat

Detect and report the status of running AI coding agents on your machine.

## Supported Agents

| Agent | Detection Method |
|-------|-----------------|
| [OpenCode](https://github.com/opencode-ai/opencode) | HTTP API via listening port (Linux: `ss -tlnp`, macOS: `lsof`) |
| [Codex](https://github.com/openai/codex) | Open file scan → rollout JSONL + SQLite DB (Linux: `/proc`, macOS: `lsof`) |
| [Claude Code](https://github.com/anthropics/claude-code) | Open file scan → session lock + JSONL (Linux: `/proc`, macOS: `lsof`) |

## Installation

```bash
go install github.com/Eric-Song-Nop/agentstat@latest
```

Or build from source:

```bash
git clone <repo-url>
cd agents_status_collector
go build -o agentstat .
```

## Usage

```
agentstat [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--json` | Output in JSON format |
| `--agents` | Comma-separated list of agents to discover (`opencode`, `codex`, `claude`); default: all |

### Examples

```bash
# Detect all agents (default)
agentstat

# Only detect Claude Code sessions
agentstat --agents claude

# Detect OpenCode and Codex, skip Claude
agentstat --agents opencode,codex

# JSON output for a specific agent
agentstat --agents claude --json
```

## Output

### Table (default)

```
AGENT    STATUS  SESSION                                 TITLE                         DIRECTORY                PID
claude   busy    fb28fab7-c8f6-4ac2-8ed6-1139a69cb4fc   agents_status_collector        ~/Documents/Sources/…    12345
codex    idle    019c9aa5-8f55-7833-b235-d00a5faa09d0   refactor auth module           ~/projects/myapp         23456
opencode idle                                                                                                    34567
```

### JSON (`--json`)

```json
[
  {
    "agent": "claude",
    "status": "busy",
    "session_id": "fb28fab7-c8f6-4ac2-8ed6-1139a69cb4fc",
    "title": "agents_status_collector",
    "directory": "/home/user/Documents/Sources/project",
    "pid": 12345
  }
]
```

## Detection Principles

### OpenCode

OpenCode runs a built-in HTTP server. `agentstat` finds listening ports owned by an `opencode` process (Linux: `ss -tlnp`, macOS: `lsof -iTCP`), then queries `/session/status` and `/session` endpoints to determine busy/idle state and session metadata.

### Codex

Codex writes rollout JSONL files during active sessions. `agentstat` scans open file descriptors (Linux: `/proc/{pid}/fd`, macOS: `lsof -p`) for rollout files, reads the last JSONL entry to determine status (`task_complete` → idle, otherwise busy), and enriches metadata (title, cwd) from the Codex SQLite database (`~/.codex/state_5.sqlite`).

### Claude Code

Claude Code holds a `.lock` file under `~/.claude/tasks/{session-id}/` while running. `agentstat` scans open file descriptors (Linux: `/proc/{pid}/fd`, macOS: `lsof -p`) for these lock files, resolves the corresponding session JSONL under `~/.claude/projects/`, and reads the trailing entries to determine status (`turn_duration` → idle, `assistant`/`user` → busy).

## Platform

Linux and macOS. Platform-specific operations (`/proc` on Linux, `lsof`/`ps` on macOS) are abstracted behind a unified interface using Go build tags. No external dependencies beyond standard system tools.

## License

MIT
