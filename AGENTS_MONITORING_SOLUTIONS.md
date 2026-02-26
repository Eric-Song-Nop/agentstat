# AI Coding Agents - External Status Monitoring Solutions

> How to detect whether each agent is **running**, **idle** (waiting for user input), or **busy** (working on a task) — from the outside.

---

## Invasiveness Legend

Each detection method is tagged with an invasiveness label:

| Tag | Meaning | Description |
|-----|---------|-------------|
| 🟢 **PASSIVE** | Non-invasive | Only observes external signals (process list, CPU, file mtime, public API). Agent runs normally, zero risk of interference. |
| 🟡 **READ-INTERNAL** | Reads internal data | Reads agent's internal files/databases (SQLite, JSON state). Works without changing agent, but depends on undocumented internals — **may break on agent updates**. |
| 🔴 **LAUNCH-FLAG** | Requires modified startup | Agent must be started with specific flags (e.g. `--json`, `--acp --port N`). **Cannot monitor an already-running instance** unless it was started this way. |
| ⚪ **CLOUD-API** | Requires API key | Needs authentication credentials to call cloud REST API. Not invasive to the agent itself, but requires account setup. |

---

## Quick Reference Table

| Agent | Type | Detection Method | Idle/Busy Signal | Invasiveness | Reliability |
|-------|------|-----------------|-----------------|-------------|-------------|
| **OpenCode** | CLI + HTTP Server | HTTP API `/session/status` | Direct: `idle` / `busy` / `retry` | 🟢 PASSIVE | ★★★★★ |
| **Devin** | Cloud SaaS | REST API `GET /v1/sessions/{id}` | Direct: `working` / `blocked` / `finished` | ⚪ CLOUD-API | ★★★★★ |
| **Warp AI (Oz)** | Terminal + Cloud | REST API `GET /agent/runs/{id}` | Direct: `QUEUED` / `INPROGRESS` / `SUCCEEDED` / `FAILED` | ⚪ CLOUD-API | ★★★★★ |
| **OpenHands** | Docker + HTTP | REST API `localhost:3000` + SSE | Direct: `WORKING` / `READY` / `WAITING_FOR_SANDBOX` | 🟢 PASSIVE | ★★★★★ |
| **Goose** | CLI + HTTP | `goosed` HTTP API + SSE stream | SSE events: `ToolCall` / `AgentMessageChunk` | 🟢 PASSIVE | ★★★★☆ |
| **Codex** | CLI | Process + `/proc/fd` → rollout JSONL | Last line: `task_complete` → idle | 🟡 READ-INTERNAL | ★★★★☆ |
| **Plandex** | CLI + Server | HTTP `localhost:8099` + PostgreSQL | API: plan status | 🟢 PASSIVE | ★★★★☆ |
| **Cline CLI** | CLI | `--json` output + file watch | `ask` msg = idle, `say` msg = busy | 🔴 LAUNCH-FLAG / 🟡 READ-INTERNAL | ★★★☆☆ |
| **Claude Code** | CLI | Process + file activity monitoring | CPU + `~/.claude/todos/` mtime | 🟢 PASSIVE | ★★★☆☆ |
| **Copilot CLI** | CLI | ACP TCP mode (`--acp --port N`) | JSON-RPC over TCP | 🔴 LAUNCH-FLAG | ★★★☆☆ |
| **Amp Code** | CLI | Process + file changes directory | `~/.amp/file-changes/` transaction mtime | 🟢 PASSIVE | ★★★☆☆ |
| **Gemini CLI** | CLI | Process + checkpoint files | CPU + `~/.gemini/tmp/` mtime | 🟢 PASSIVE | ★★★☆☆ |
| **Aider** | CLI | Process + file watch | CPU + `.aider.chat.history.md` mtime | 🟢 PASSIVE | ★★★☆☆ |
| **Amazon Q CLI** | CLI | Process + log files + Unix socket | `$XDG_RUNTIME_DIR/qlog/*.log` activity | 🟢 PASSIVE | ★★★☆☆ |
| **SWE-agent** | CLI (batch) | Process existence + trajectory files | Process alive = busy, exit = done | 🟢 PASSIVE | ★★★☆☆ |
| **Cursor** | IDE (Electron) | Process + SQLite `state.vscdb` | `composer.composerData` polling | 🟡 READ-INTERNAL | ★★☆☆☆ |
| **Windsurf** | IDE (Electron) | Process + Cascade directory | `~/.codeium/windsurf/cascade/` mtime | 🟢 PASSIVE / 🟡 READ-INTERNAL | ★★☆☆☆ |
| **Cline (VSCode)** | VSCode Extension | File watch `tasks/` directory | `ui_messages.json`: last msg type | 🟡 READ-INTERNAL | ★★☆☆☆ |
| **Roo Code** | VSCode Extension | SQLite `state.vscdb` + file watch | `ui_messages.json`: last msg type | 🟡 READ-INTERNAL | ★★☆☆☆ |
| **Copilot (VSCode)** | VSCode Extension | Process CPU monitoring | CPU + network to copilot endpoints | 🟢 PASSIVE | ★★☆☆☆ |
| **Zed AI** | IDE (native) | Process + SQLite | `db.sqlite` mtime + CPU | 🟡 READ-INTERNAL | ★★☆☆☆ |
| **JetBrains/Junie** | IDE Plugin | IDE process + log files | Log file activity | 🟢 PASSIVE | ★★☆☆☆ |
| **Augment/Auggie** | VSCode Ext + CLI | `--print` mode / ACP protocol | Process exit = done | 🔴 LAUNCH-FLAG | ★★☆☆☆ |
| **Trae** | IDE (Electron) | Process monitoring | Network activity (noisy telemetry) | 🟢 PASSIVE | ★☆☆☆☆ |
| **Tabnine** | IDE Plugin | Process `TabNine` | CPU activity (request-response) | 🟢 PASSIVE | ★☆☆☆☆ |
| **Sourcery** | IDE Plugin (LSP) | Process `sourcery` | CPU activity | 🟢 PASSIVE | ★☆☆☆☆ |
| **Google Jules** | Cloud SaaS | REST API (alpha) | API: Session activity status | ⚪ CLOUD-API | ★★★★☆ (cloud-only) |
| **v0 (Vercel)** | Cloud SaaS | Platform API | API query | ⚪ CLOUD-API | ★★★☆☆ (cloud-only) |
| **Replit Agent** | Cloud SaaS | Code Execution API | Progress tab (no local) | ⚪ CLOUD-API | ★★☆☆☆ (cloud-only) |
| **Bolt.new** | Cloud SaaS | None | Not monitorable locally | N/A | ☆☆☆☆☆ |

---

## Detailed Solutions Per Agent

---

### 1. OpenCode

| Item | Detail |
|------|--------|
| **Process name** | `opencode` |
| **Config path** | `~/.config/opencode/opencode.json` |
| **Data path** | `~/.local/share/opencode/` |
| **Database** | `~/.local/share/opencode/opencode.db` (SQLite, 437MB) |
| **Log path** | `~/.local/share/opencode/log/` |
| **HTTP API** | **Each TUI instance starts its own server on a random port** |
| **mDNS** | `opencode-{port}._http._tcp.local` (if enabled) |

> **IMPORTANT**: Each `opencode` TUI instance runs its own independent HTTP server on a
> **randomly assigned port**. There is no single "main" port. Port `4096` is only the default
> for `opencode serve` — a TUI started normally will NOT use it.
> See: https://opencode.ai/docs/server/#connect-to-an-existing-server

**Step 1 — Discover all instances:** 🟢 PASSIVE
```bash
# Find all opencode listening ports from the system socket table
ss -tlnp | grep opencode
# Output example:
# LISTEN 0.0.0.0:38129  users:(("opencode",pid=1059916,fd=30))
# LISTEN 0.0.0.0:34897  users:(("opencode",pid=401514,fd=30))
```

**Step 2 — Query each instance:** 🟢 PASSIVE
```bash
# Health check (verify it's alive)
curl http://localhost:{port}/global/health
# → {"healthy":true,"version":"1.2.10"}

# Session status (idle/busy/retry for ALL sessions on this server)
curl http://localhost:{port}/session/status
# → {} when idle, {"ses_xxx":{"type":"busy"}} when working

# List sessions (NOTE: no trailing slash!)
curl http://localhost:{port}/session
```

> OpenCode's HTTP server is **always on by default** — no startup flag needed. This is the ideal case.

**Alternative — SSE real-time stream:** 🟢 PASSIVE
```bash
curl http://localhost:{port}/global/event
```
Event type `session.status` pushes status changes in real time.

**Alternative — mDNS discovery:** 🟢 PASSIVE
```bash
# If mDNS is enabled in config ("mdns": true)
avahi-browse -t _http._tcp | grep opencode
```

**Alternative — SQLite query:** 🟡 READ-INTERNAL
```sql
SELECT id, title, directory FROM session ORDER BY updated DESC;
```
> Reads internal database. Schema may change between versions. All instances share the same DB file.

---

### 2. Claude Code

| Item | Detail |
|------|--------|
| **Process name** | `claude` |
| **Config path** | `~/.claude/settings.json` |
| **State path** | `~/.claude/` (~1.7GB total) |
| **Task locks** | `~/.claude/tasks/{uuid}/.lock` (99 lock files) |
| **Todos** | `~/.claude/todos/{uuid}-agent-{uuid}.json` |
| **Transcripts** | `~/.claude/transcripts/ses_{id}.jsonl` |
| **History** | `~/.claude/history.jsonl` |
| **HTTP API** | None |
| **Database** | None |

**Status detection (combined approach):**

1. 🟢 PASSIVE — **Find running instances:**
   ```bash
   ps aux | grep '[c]laude'
   # Get PID, working directory from /proc/{pid}/cwd
   ```

2. 🟢 PASSIVE — **Determine busy/idle — CPU method:**
   ```bash
   # Read /proc/{pid}/stat, compute CPU delta over 1-2 seconds
   # High CPU = busy (calling LLM or executing tools)
   # Near-zero CPU = idle (waiting for user input)
   ```

3. 🟢 PASSIVE — **Determine busy/idle — file mtime method:**
   ```bash
   # Watch ~/.claude/todos/ for recent file modification TIMES (not content)
   # Watch ~/.claude/tasks/{id}/.lock for lock activity
   # Recent write (< 5s) = busy
   ```
   > Only checks `stat()` mtime, does not read file content. Safe.

4. 🟡 READ-INTERNAL — **Determine busy/idle — transcript method:**
   ```bash
   # Find most recent transcript: ~/.claude/transcripts/ses_*.jsonl
   # Check last line — if role is "assistant" and recent = busy
   # If role is "user" prompt and old = idle
   ```
   > Reads internal JSONL transcript format. May break if format changes.

---

### 3. Codex (OpenAI)

| Item | Detail |
|------|--------|
| **Process name** | `codex` (Rust binary), `node codex-acp` |
| **Config path** | `~/.codex/config.toml` |
| **State path** | `~/.codex/` |
| **Database** | `~/.codex/state_5.sqlite` (320KB) — `threads` table only (metadata); `jobs` table is **empty and useless** |
| **Sessions** | `~/.codex/sessions/2026/` (by year) |
| **History** | `~/.codex/history.jsonl` (306 lines) |
| **Log path** | `~/.codex/log/codex-tui.log` (28MB) |
| **Rollout files** | `~/.codex/sessions/2026/MM/DD/rollout-{date}T{time}-{thread_uuid}.jsonl` — JSONL event stream per thread |
| **HTTP API** | None |

**Status detection (recommended approach — `/proc/fd` + rollout JSONL):**

1. 🟢 PASSIVE — **Process detection:**
   ```bash
   pgrep -f 'codex/codex$'
   ```
   > Matches only the main Codex Rust binary, not `codex-acp` or other subprocesses.

2. 🟡 READ-INTERNAL — **PID → rollout file via `/proc/{pid}/fd`:**
   ```bash
   # Codex holds its active rollout JSONL file open via a file descriptor.
   # Find it by scanning /proc/{pid}/fd/* symlinks:
   readlink /proc/{pid}/fd/* 2>/dev/null | grep rollout
   # → /home/user/.codex/sessions/2026/02/26/rollout-2026-02-26T23-51-07-019c9aa5-8f55-7833-b235-d00a5faa09d0.jsonl
   ```
   > The rollout filename contains the **thread ID** (trailing UUID). The `threads.rollout_path` column in SQLite also stores this path.

3. 🟡 READ-INTERNAL — **Status from last line of rollout JSONL:**
   ```bash
   tail -1 /path/to/rollout-xxx.jsonl | jq -r '.payload.type'
   # "task_complete" → idle (waiting for user input)
   # "task_started" / "ToolCall" / anything else → busy (working)
   ```
   > Each line is a JSON object with `{"payload":{"type":"..."}}`. The last line's `payload.type` directly indicates the agent's current state.

4. 🟡 READ-INTERNAL — **Thread title/metadata from SQLite:**
   ```sql
   -- Get thread title (for display only — NOT for status detection)
   SELECT title FROM threads WHERE id = '{thread_id}';
   ```
   > The `threads` table stores metadata (title, timestamps). The `jobs` table exists but is **always empty** — do not use it for status detection.

5. 🟢 PASSIVE — **CWD from `/proc`:**
   ```bash
   readlink /proc/{pid}/cwd
   ```

**Why not use the `jobs` table?**
> Despite having a `jobs` table in `state_5.sqlite`, it is always empty in practice. Codex tracks execution state in the rollout JSONL files, not in SQLite. The only useful SQLite data is the `threads` table for metadata (title, creation time).

---

### 4. Amp Code (Sourcegraph)

| Item | Detail |
|------|--------|
| **Process name** | `amp` |
| **Config path** | `~/.config/amp/settings.json` |
| **State path** | `~/.amp/` |
| **File changes** | `~/.amp/file-changes/T-{ULID}/` (transaction dirs) |
| **HTTP API** | None |
| **Database** | None |

**Status detection:**

1. 🟢 PASSIVE — **Process detection + CPU:**
   ```bash
   pgrep -f amp
   ```

2. 🟢 PASSIVE — **File change transaction mtime:**
   ```bash
   # Check most recent transaction directory in ~/.amp/file-changes/
   # Recent modification = busy
   # ULID in directory name encodes timestamp — only stat(), no content read
   ```

---

### 5. Aider

| Item | Detail |
|------|--------|
| **Process name** | `aider` (Python) |
| **Config path** | `~/.aider.conf.yml`, `$CWD/.aider.conf.yml` |
| **Chat history** | `$CWD/.aider.chat.history.md` |
| **Input history** | `$CWD/.aider.input.history` |
| **LLM log** | Via `AIDER_LLM_HISTORY_FILE` env var |
| **Tags cache** | `$CWD/.aider.tags.cache.v3/` |
| **HTTP API** | None (browser mode on port 8501 is Streamlit UI only) |
| **Database** | None |

**Status detection:**

1. 🟢 PASSIVE — **Process detection + CPU:**
   ```bash
   pgrep -f aider
   # CPU near zero = idle (waiting for input)
   ```

2. 🟢 PASSIVE — **Chat history file mtime:**
   ```bash
   # Monitor .aider.chat.history.md mtime
   # Growing = busy
   ```

3. 🔴 LAUNCH-FLAG — **Notification hook:**
   Aider supports `--notifications` flag — sends OS notification when waiting for input.
   > **Must start aider with `--notifications`.** Cannot enable on already-running instance.

4. 🔴 LAUNCH-FLAG — **LLM history log:**
   ```bash
   # Set AIDER_LLM_HISTORY_FILE=/path/to/llm.log before starting aider
   # Then watch that file for activity
   ```
   > **Requires env var set before launch.** Not available by default.

---

### 6. Cline

| Item | Detail |
|------|--------|
| **Process name (CLI)** | `node` (cline) |
| **Process name (VSCode)** | Inside VSCode Extension Host |
| **CLI state path** | `~/.cline/data/` |
| **CLI log** | `~/.cline/log/cline.log` |
| **VSCode state path** | `~/.config/Code/User/globalStorage/saoudrizwan.claude-dev/` |
| **Tasks** | `tasks/{task-id}/ui_messages.json`, `api_conversation_history.json` |
| **HTTP API** | gRPC (Cline Core), not HTTP |

**Status detection (CLI mode):**

1. 🔴 LAUNCH-FLAG — **`--json` streaming output:**
   ```bash
   cline --json "task description"
   # Parse structured JSON stream for status
   ```
   > **Must start cline with `--json` flag.** Cannot attach to a normally-started instance.

2. 🟡 READ-INTERNAL — **File monitoring:**
   ```bash
   # Watch tasks/{task-id}/ui_messages.json
   # Last message type "ask" = idle (waiting for user)
   # Last message type "say" = busy (agent outputting)
   ```
   > Reads internal JSON task files. Format may change between versions.

**Status detection (VSCode extension):**

1. 🟡 READ-INTERNAL — **SQLite query on `state.vscdb`:**
   ```sql
   SELECT value FROM ItemTable
   WHERE key LIKE '%saoudrizwan.claude-dev%';
   ```
   > Reads VSCode's internal SQLite database. Concurrent read while VSCode writes — use `PRAGMA journal_mode=WAL` for safety.

---

### 7. Roo Code

| Item | Detail |
|------|--------|
| **Process name** | Inside VSCode Extension Host |
| **VSCode state path** | `~/.config/Code/User/globalStorage/rooveterinaryinc.roo-cline/` |
| **Tasks** | `tasks/{task-id}/ui_messages.json` |
| **Database** | `~/.config/Code/User/globalStorage/state.vscdb` |
| **HTTP API** | None |

**Status detection:** 🟡 READ-INTERNAL

Same as Cline VSCode extension — monitor `ui_messages.json` for last message type, or query `state.vscdb`.
> All methods read internal files/database. No passive-only option available.

---

### 8. Cursor

| Item | Detail |
|------|--------|
| **Process name** | `cursor` (Electron) |
| **Config path** | `~/.config/Cursor/User/settings.json` |
| **State DB** | `~/.config/Cursor/User/globalStorage/state.vscdb` |
| **Workspace DB** | `~/.config/Cursor/User/workspaceStorage/{hash}/state.vscdb` |
| **Logs** | `~/.config/Cursor/logs/` |
| **HTTP API** | None (internal gRPC/ConnectRPC to `agent.api5.cursor.sh`) |

**Status detection:**

1. 🟢 PASSIVE — **Process + CPU monitoring:**
   ```bash
   pgrep -f cursor
   # Monitor CPU usage — agent mode uses significant CPU when working
   ```

2. 🟡 READ-INTERNAL — **SQLite polling:**
   ```sql
   -- Query composer/agent session data
   SELECT value FROM ItemTable WHERE key = 'composer.composerData';
   -- Parse JSON to find active sessions
   ```
   > Reads Cursor's internal SQLite. `composerData` is an undocumented internal key. **High risk of breaking on updates.**

3. 🟢 PASSIVE — **Network monitoring:**
   ```bash
   # Active connections to agent.api5.cursor.sh = busy
   ss -tp | grep cursor
   ```

---

### 9. Windsurf / Codeium

| Item | Detail |
|------|--------|
| **Process name** | `windsurf` (Electron) + `language_server_linux_x64` |
| **Config path** | `~/.config/Windsurf/User/settings.json` |
| **Cascade data** | `~/.codeium/windsurf/cascade/` |
| **State DB** | `~/.config/Windsurf/User/globalStorage/state.vscdb` |
| **Language server port** | `localhost:42100` (default) |
| **HTTP API** | Language server port (protobuf, not REST) |

**Status detection:**

1. 🟢 PASSIVE — **Process monitoring + CPU:**
   ```bash
   pgrep -f windsurf
   pgrep -f language_server_linux_x64
   ```

2. 🟢 PASSIVE — **Cascade directory mtime watch:**
   ```bash
   # Monitor ~/.codeium/windsurf/cascade/ for file modification times
   ```

3. 🟢 PASSIVE — **Language server port check:**
   ```bash
   ss -tlnp | grep 42100
   # Port open = running, but doesn't tell idle vs busy
   ```

4. 🟡 READ-INTERNAL — **SQLite state query:**
   > Same pattern as Cursor — query `state.vscdb` for Cascade session data.

---

### 10. GitHub Copilot

| Item | Detail |
|------|--------|
| **Process name (VSCode)** | `node` (copilot extension in Extension Host) |
| **Process name (CLI)** | `copilot` |
| **VSCode extension** | `~/.vscode/extensions/github.copilot-*/` |
| **CLI config** | `~/.copilot/config.json` |
| **CLI sessions** | `~/.copilot/session-state/` |
| **CLI MCP config** | `~/.copilot/mcp-config.json` |
| **CLI logs** | `~/.copilot/logs/` |
| **HTTP API** | CLI: `--acp --port N` exposes TCP JSON-RPC |

**Status detection (CLI):**

1. 🔴 LAUNCH-FLAG — **ACP TCP mode (best but invasive):**
   ```bash
   # Must start copilot this way:
   copilot --acp --port 8080
   # Then query via JSON-RPC over TCP
   ```
   > **Cannot monitor a normally-started `copilot` CLI.** Must launch with `--acp --port`.

2. 🟢 PASSIVE — **Process + CPU (fallback):**
   ```bash
   pgrep -f copilot
   ```

3. 🟢 PASSIVE — **Log file mtime:**
   ```bash
   # Watch ~/.copilot/logs/ for activity
   ```

**Status detection (VSCode):** 🟢 PASSIVE

CPU + network monitoring to GitHub Copilot endpoints.

---

### 11. JetBrains AI / Junie

| Item | Detail |
|------|--------|
| **Process name** | `idea`, `pycharm`, `webstorm`, etc. (plugin runs inside IDE) |
| **Config path** | `~/.config/JetBrains/{Product}{Version}/` |
| **ACP config** | `~/.jetbrains/acp.json` |
| **Cache** | `~/.cache/JetBrains/{Product}{Version}/aia/codex/` |
| **Log path** | `~/.cache/JetBrains/{Product}{Version}/log/` |
| **HTTP API** | ACP over stdio (agent as subprocess) |

**Status detection:**

1. 🟢 PASSIVE — **Log file mtime monitoring:**
   ```bash
   # Watch log directory for AI Assistant request activity
   # ~/.cache/JetBrains/IntelliJIdea2024.3/log/
   ```

2. 🟡 READ-INTERNAL — **Log content parsing:**
   ```bash
   # Parse AI Assistant request logs for active/completed patterns
   ```
   > Log format is undocumented internal detail.

3. 🟢 PASSIVE — **Junie CLI (standalone):**
   ```bash
   pgrep -f junie
   # Has /cost and /history commands for session info
   ```

---

### 12. Zed AI

| Item | Detail |
|------|--------|
| **Process name** | `zed` |
| **Config path** | `~/.config/zed/settings.json` |
| **Data path** | `~/.local/share/zed/` |
| **Database** | `~/.local/share/zed/db/0-stable/db.sqlite` |
| **Logs** | `~/.local/share/zed/logs/` |
| **HTTP API** | ACP for external agents |

**Status detection:**

1. 🟢 PASSIVE — **Process + CPU:**
   ```bash
   pgrep -f zed
   # Zed reduces GPU usage when idle — measurable signal
   ```

2. 🟢 PASSIVE — **Log file mtime:**
   ```bash
   # Watch ~/.local/share/zed/logs/ for activity
   ```

3. 🟡 READ-INTERNAL — **SQLite query:**
   ```bash
   # Query db.sqlite for workspace/buffer state
   ```
   > Reads Zed's internal database. Schema versioned (`0-stable`) but undocumented.

---

### 13. Goose (Block/Square)

| Item | Detail |
|------|--------|
| **Process name** | `goose` (CLI), `goosed` (backend daemon) |
| **Config path** | `~/.config/goose/config.yaml` |
| **Database** | `~/.config/goose/sessions.db` (SQLite) |
| **Log path** | `~/.config/goose/logs/goose.log` |
| **HTTP API** | `goosed` exposes HTTP on **random port** |

**Status detection (best approach):**

1. 🟢 PASSIVE — **Find goosed port** (from process args or log):
   ```bash
   ss -tlnp | grep goosed
   ```

2. 🟢 PASSIVE — **Health check:**
   ```
   GET http://localhost:{port}/health
   ```
   > `goosed` HTTP server starts automatically — no special flag needed. Port is random but discoverable.

3. 🟢 PASSIVE — **SSE event stream:**
   ```
   GET http://localhost:{port}/acp/session/{id}/stream
   ```
   Events: `AgentMessageChunk` (outputting), `ToolCall` with status `pending`/`completed`.

4. 🟡 READ-INTERNAL — **SQLite session query:**
   ```sql
   SELECT * FROM sessions ORDER BY updated_at DESC LIMIT 1;
   ```
   > Reads internal SQLite. Table schema may change.

---

### 14. Gemini CLI

| Item | Detail |
|------|--------|
| **Process name** | `gemini` (Node.js) |
| **Config path** | `~/.gemini/settings.json` |
| **Context file** | `~/.gemini/GEMINI.md` |
| **Temp/checkpoints** | `~/.gemini/tmp/` |
| **HTTP API** | None |
| **Database** | None |

**Status detection:**

1. 🟢 PASSIVE — **Process + CPU:**
   ```bash
   pgrep -f gemini
   # CPU monitoring — high = calling Gemini API
   ```

2. 🟢 PASSIVE — **Checkpoint file mtime:**
   ```bash
   # Monitor ~/.gemini/tmp/ for new checkpoint file mtimes
   # Recent checkpoint = busy
   ```

---

### 15. Amazon Q Developer CLI

| Item | Detail |
|------|--------|
| **Process name** | `q`, `figterm`, `fig_desktop` |
| **Config path** | `~/.aws/amazonq/` |
| **History** | `~/.aws/amazonq/history/` |
| **Log path** | `$XDG_RUNTIME_DIR/qlog/chat.log`, `qchat.log` |
| **Unix socket** | `/run/user/{uid}/cwrun/desktop.sock` |
| **HTTP API** | None local |

**Status detection:**

1. 🟢 PASSIVE — **Process detection:**
   ```bash
   pgrep -f "q chat"
   pgrep figterm
   ```

2. 🟢 PASSIVE — **Log file mtime:**
   ```bash
   # Watch $XDG_RUNTIME_DIR/qlog/qchat.log mtime for activity
   ```

3. 🟡 READ-INTERNAL — **Log content parsing:**
   ```bash
   # Parse qchat.log for state patterns like "Interrupted", MCP errors
   ```

4. 🟢 PASSIVE — **Unix socket existence check:**
   ```bash
   # Check /run/user/{uid}/cwrun/desktop.sock connection state
   ```

> **Note:** Being replaced by **Kiro CLI** (`~/.kiro/`).

---

### 16. OpenHands (formerly OpenDevin)

| Item | Detail |
|------|--------|
| **Process** | Docker container (`openhands`) |
| **State path** | `~/.openhands-state/` (host), `~/.openhands/` (persistence) |
| **HTTP API** | `http://localhost:3000/api/` (FastAPI) |
| **WebSocket** | Socket.IO for real-time events |

**Status detection (best approach):** 🟢 PASSIVE
```
POST http://localhost:3000/api/v1/app-conversations/stream-start
```
Conversation states: `WORKING` | `READY` | `WAITING_FOR_SANDBOX` | `PREPARING_REPOSITORY`.

> HTTP API starts automatically with the Docker container. No special flags needed.

---

### 17. SWE-agent

| Item | Detail |
|------|--------|
| **Process name** | `sweagent` (Python) |
| **Config path** | `config/default.yaml` |
| **Trajectories** | `trajectories/{user}/{experiment}/` |
| **HTTP API** | None (batch tool, not a service) |

**Status detection:**

1. 🟢 PASSIVE — **Process existence = busy, exit = done.**
2. 🟢 PASSIVE — **Trajectory file mtime:** `{instance_id}.traj`, `.debug.log`, `.info.log` — watch mtimes.
3. 🟡 READ-INTERNAL — **Batch status file:** `run_batch_exit_statuses.yaml` — parse YAML for completion status.

---

### 18. Devin (Cloud)

| Item | Detail |
|------|--------|
| **Local process** | None (fully cloud) |
| **REST API** | `https://api.devin.ai/v1/` |
| **Auth** | Bearer token `apk_user_*` or `apk_*` |

**Status detection:** ⚪ CLOUD-API
```bash
curl -H "Authorization: Bearer $DEVIN_API_KEY" \
  https://api.devin.ai/v1/sessions/{session_id}
```
Returns `status_enum`: `working` | `blocked` | `finished` | `expired` | `suspend_requested` | `resumed`.

> Requires paid account + API key. Not invasive to the agent itself.

---

### 19. Warp AI / Oz (Cloud)

| Item | Detail |
|------|--------|
| **Local process** | `warp-terminal` (Linux), `oz` (CLI) |
| **REST API** | `https://app.warp.dev/api/v1/agent/` |
| **Auth** | API key prefix `wk-`, env `WARP_API_KEY` |
| **SDK** | Python: `oz_agent_sdk`, TypeScript: `oz-sdk-typescript` |

**Status detection:** ⚪ CLOUD-API
```bash
# Via API
curl -H "Authorization: Bearer $WARP_API_KEY" \
  https://app.warp.dev/api/v1/agent/runs/{runId}

# Via CLI
oz run list
oz run get {runId}
```
Status: `QUEUED` | `INPROGRESS` | `SUCCEEDED` | `FAILED`.

> Requires Warp account + API key.

---

### 20. Google Jules (Cloud)

| Item | Detail |
|------|--------|
| **Local CLI** | `jules` (via `npm install -g @google/jules`) |
| **REST API** | Jules API (alpha), auth via `X-Goog-Api-Key` |

**Status detection:** ⚪ CLOUD-API
```bash
jules remote list    # List active sessions
jules remote new     # Create session
# Or via REST API to query session activity status
```
> Requires Google API key.

---

### 21. Trae (ByteDance)

| Item | Detail |
|------|--------|
| **Process name** | `trae` (Electron) |
| **Config path** | `~/.config/Trae/` (estimated, VS Code fork) |
| **HTTP API** | None (internal WebSocket + telemetry) |

**Status detection:** 🟢 PASSIVE (but low quality)

Difficult. Heavy telemetry noise (~30s intervals even when idle). Only approach:
- Process + CPU monitoring
- Network traffic analysis (distinguish AI requests from telemetry)

> All methods are passive but unreliable due to constant telemetry background noise.

---

### 22. Augment / Auggie

| Item | Detail |
|------|--------|
| **Process name (CLI)** | `node` (auggie), requires Node.js 22+ |
| **Process name (VSCode)** | Inside Extension Host |
| **Project config** | `.augment/commands/`, `.augment/agents/`, `.augmentignore` |
| **Auth** | `AUGMENT_SESSION_AUTH` env var |

**Status detection:**

1. 🔴 LAUNCH-FLAG — **`--print` mode:**
   Process exit = task complete.
   > **Must start auggie with `--print`.** This makes it a one-shot command rather than interactive — changes the agent's behavior entirely.

2. 🔴 LAUNCH-FLAG — **ACP protocol:**
   Auggie supports ACP, but must be started as an ACP agent to use it.

3. 🟢 PASSIVE — **Process + CPU (fallback):**
   ```bash
   pgrep -f auggie
   ```

---

### 23. Plandex (Discontinued)

| Item | Detail |
|------|--------|
| **Process name** | `plandex` (Go binary) |
| **Project state** | `.plandex/` in working directory |
| **Server** | Port `8099` (default) |
| **Database** | PostgreSQL |
| **HTTP API** | `GET /health` (returns 200) |

> **Note:** Shut down October 2025. Listed for reference only.

---

### 24. Tabnine

| Item | Detail |
|------|--------|
| **Process name** | `TabNine` (closed-source binary) |
| **Config path** | `~/.config/TabNine/tabnine_config.json` |
| **HTTP API** | stdin/stdout communication only |
| **Log** | Requires `--log-file-path` flag |

**Status detection:**

1. 🟢 PASSIVE — **CPU monitoring only.** Request-response model, no persistent "busy" state.

2. 🔴 LAUNCH-FLAG — **Log file monitoring:**
   > TabNine must be started with `--log-file-path` to produce logs. IDE plugins don't expose this flag easily.

---

### 25. Sourcery

| Item | Detail |
|------|--------|
| **Process name** | `sourcery` (LSP server) |
| **Config** | `.sourcery.yaml` (project root) |
| **HTTP API** | LSP over stdin/stdout |

**Status detection:** 🟢 PASSIVE

CPU monitoring only. LSP request-response model. No way to query status.

---

## Detection Strategy Summary

### Tier 1: Direct API (Best) — 🟢 PASSIVE

Agents that expose explicit status APIs **without any startup changes**:

| Agent | Method | Notes |
|-------|--------|-------|
| OpenCode | `GET /session/status` or SSE `/api/global/event` | HTTP server always on by default |
| OpenHands | `POST /api/.../stream-start` → conversation state | FastAPI always on in Docker |
| Goose | `GET /health` + SSE stream events | `goosed` HTTP always on (random port) |
| Devin | `GET /v1/sessions/{id}` → `status_enum` | ⚪ Cloud API, needs API key |
| Warp/Oz | `GET /agent/runs/{id}` → status enum | ⚪ Cloud API, needs API key |

### Tier 2: Database Query (Good) — 🟡 READ-INTERNAL

Agents with queryable local databases. **No startup changes needed, but reads undocumented internal formats:**

| Agent | Database | Query Target | Breakage Risk |
|-------|----------|-------------|---------------|
| OpenCode | `~/.local/share/opencode/opencode.db` | `session` table | Low (also has API) |
| Codex | `/proc/{pid}/fd` → rollout JSONL | `payload.type` in last line | **Medium** (file format stable, fd approach reliable) |
| Goose | `~/.config/goose/sessions.db` | sessions | Low (also has API) |
| Cursor | `~/.config/Cursor/.../state.vscdb` | `composer.composerData` key | **High** (undocumented key) |
| Windsurf | `~/.config/Windsurf/.../state.vscdb` | extension state keys | **High** (undocumented) |
| Zed | `~/.local/share/zed/db/0-stable/db.sqlite` | workspace state | Medium |
| Cline/Roo (VSCode) | `~/.config/Code/.../state.vscdb` | extension state keys | **High** (undocumented) |

### Tier 3: File System Monitoring (Acceptable) — 🟢 PASSIVE

Watch file modification times to infer activity. **Fully passive — only `stat()` calls, no content reads:**

| Agent | Files to Watch |
|-------|---------------|
| Claude Code | `~/.claude/todos/`, `~/.claude/tasks/`, `~/.claude/transcripts/` |
| Amp Code | `~/.amp/file-changes/T-*/` |
| Aider | `$CWD/.aider.chat.history.md` |
| Gemini CLI | `~/.gemini/tmp/` |
| Amazon Q | `$XDG_RUNTIME_DIR/qlog/*.log` |
| SWE-agent | `trajectories/**/*.traj` |

### Tier 4: Process + CPU Heuristic (Fallback) — 🟢 PASSIVE

Universal fallback for any agent — works but least precise:

```bash
# 1. Find process
PID=$(pgrep -f "agent_name")

# 2. Sample CPU over 2 seconds
CPU1=$(cat /proc/$PID/stat | awk '{print $14+$15}')
sleep 2
CPU2=$(cat /proc/$PID/stat | awk '{print $14+$15}')
DELTA=$((CPU2 - CPU1))

# 3. Interpret
# DELTA > threshold → busy
# DELTA ≈ 0 → idle (waiting for input)
```

Applicable to: Claude Code, Codex, Aider, Gemini CLI, Amazon Q, Cursor, Windsurf, Zed, Trae, Tabnine, Sourcery, any other local process.

---

## ⚠️ Invasiveness Summary — Methods Requiring Startup Changes

**The following methods ONLY work if the agent is started with specific flags or environment variables. They CANNOT monitor an already-running instance started normally.**

| Agent | Required Flag/Env | What It Enables | Impact |
|-------|-------------------|----------------|--------|
| **Copilot CLI** | `--acp --port N` | TCP JSON-RPC server for status queries | **Changes agent mode entirely** — runs as ACP agent instead of interactive CLI |
| **Cline CLI** | `--json` | Structured JSON output stream | **Changes output format** — not suitable for normal interactive use |
| **Auggie CLI** | `--print` | One-shot execution, exit = done | **Changes behavior** — becomes non-interactive, exits after task |
| **Aider** | `--notifications` | OS notification on idle | Low impact — just adds notifications, behavior unchanged |
| **Aider** | `AIDER_LLM_HISTORY_FILE=path` | LLM conversation log file | Low impact — just enables extra logging |
| **Tabnine** | `--log-file-path` | Log file output | Low impact — just enables logging, but hard to set via IDE plugins |

### Recommendation

For building the status collector, **prioritize methods that require zero changes to how agents are launched:**

1. ✅ **Always use Tier 1 (API) when available** — OpenCode, Goose, OpenHands already expose APIs by default
2. ✅ **Use Tier 3 (file mtime) as primary method** for agents without APIs — works on Claude Code, Amp, Aider, Gemini CLI, Amazon Q
3. ✅ **Use Tier 4 (CPU heuristic) as universal fallback** — works on every local agent
4. ⚠️ **Use Tier 2 (DB read) sparingly** — useful but fragile, needs version-specific adapters
5. ❌ **Avoid Tier LAUNCH-FLAG methods** unless the user specifically opts in via a wrapper script

---

## Cloud-Only Agents (No Local Monitoring)

These agents run entirely in the cloud. Monitor via their APIs only:

| Agent | API Available | Notes |
|-------|--------------|-------|
| Devin | Yes (REST) | Best cloud API |
| Warp/Oz | Yes (REST + SDK) | Python & TypeScript SDKs |
| Google Jules | Yes (REST, alpha) | + CLI tool `jules` |
| v0 (Vercel) | Yes (Platform API) | Limited |
| Replit Agent | Partial | Progress tab, no direct API |
| Bolt.new | No | Cannot monitor |
