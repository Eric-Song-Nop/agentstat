# agentstat

Detect and report the status of running AI coding agents on your machine.

## Supported Agents

| Agent | Detection Method |
|-------|-----------------|
| [OpenCode](https://github.com/opencode-ai/opencode) | HTTP API via listening port (`ss -tlnp`) |
| [Codex](https://github.com/openai/codex) | `/proc` fd scan → rollout JSONL + SQLite DB |
| [Claude Code](https://github.com/anthropics/claude-code) | `/proc` fd scan → session lock + JSONL |

## Installation

```bash
go install agentstat@latest
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

OpenCode runs a built-in HTTP server. `agentstat` uses `ss -tlnp` to find listening ports owned by an `opencode` process, then queries `/session/status` and `/session` endpoints to determine busy/idle state and session metadata.

### Codex

Codex writes rollout JSONL files during active sessions. `agentstat` scans `/proc/{pid}/fd` for symlinks pointing to rollout files, reads the last JSONL entry to determine status (`task_complete` → idle, otherwise busy), and enriches metadata (title, cwd) from the Codex SQLite database (`~/.codex/state_5.sqlite`).

### Claude Code

Claude Code holds a `.lock` file under `~/.claude/tasks/{session-id}/` while running. `agentstat` scans `/proc/{pid}/fd` for these lock symlinks, resolves the corresponding session JSONL under `~/.claude/projects/`, and reads the trailing entries to determine status (`turn_duration` → idle, `assistant`/`user` → busy).

## Platform

Linux only — relies on `/proc` filesystem and `ss` command.

## License

MIT
