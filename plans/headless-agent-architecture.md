# Headless Agent Architecture — PTY to Process Manager Pivot

## Goal

Replace the PTY/terminal-based agent model with headless Claude CLI child processes using bidirectional JSON streaming (`--print --input-format stream-json --output-format stream-json`). skwad-cli becomes a pure process manager + coordination layer. No more terminal emulation, no more ANSI stripping, no more `creack/pty`.

## Design Decisions

- **Long-lived streaming processes**: Agents run as persistent `--print --input-format stream-json --output-format stream-json` child processes. New prompts are sent via stdin JSON, responses are parsed from stdout JSON. This gives us multi-turn conversations without process restarts.
- **Hook events via JSON stream**: Use `--include-hook-events` to capture status changes directly from the stdout JSON stream. Remove `--plugin-dir` for Claude agents entirely — no duplicate status events. Keep the HTTP `/hook` endpoint for future non-Claude agents only.
- **skwad coordination stays**: We do NOT use Claude's `--agents` flag. Our MCP-based coordination (send-message, set-status, check-messages, list-agents) remains the team communication layer.
- **Permission mode is configurable**: Default to `--permission-mode auto`, allow override in team config. No hardcoded `--dangerously-skip-permissions`.
- **Package rename**: `internal/terminal/` → `internal/process/` to reflect the new model.
- **Claude-only first**: This pivot targets Claude Code. Multi-provider (Codex, Gemini, Copilot) headless modes are deferred. Non-Claude spawn paths return an explicit error.
- **Watch mode**: Two tiers — log stream (Phase 2) for immediate visibility, TUI dashboard (Phase 4) for interactive monitoring.

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────┐
│                   skwad-cli                          │
│                                                     │
│  ┌──────────┐  ┌─────────────┐  ┌───────────────┐  │
│  │ CLI      │  │ Daemon      │  │ MCP Server    │  │
│  │ Commands │→ │ (orchestr.) │← │ (JSON-RPC 2.0)│  │
│  └──────────┘  └──────┬──────┘  └───────────────┘  │
│                       │                              │
│              ┌────────▼────────┐                     │
│              │ Process Manager │                     │
│              │ (new Pool)      │                     │
│              └────────┬────────┘                     │
│                       │                              │
│         ┌─────────────┼─────────────┐                │
│         ▼             ▼             ▼                │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐   │
│  │ Agent Proc  │ │ Agent Proc  │ │ Agent Proc  │   │
│  │ stdin(json) │ │ stdin(json) │ │ stdin(json) │   │
│  │ stdout(json)│ │ stdout(json)│ │ stdout(json)│   │
│  └─────────────┘ └─────────────┘ └─────────────┘   │
│                                                     │
│  Each agent: claude -p --input-format stream-json   │
│    --output-format stream-json --include-hook-events│
│    --mcp-config <skwad> --append-system-prompt <...>│
└─────────────────────────────────────────────────────┘
```

---

## What Stays Unchanged

| Package | Why |
|---------|-----|
| `internal/mcp/server.go`, `tools.go`, `types.go` | No PTY dependency — pure HTTP/JSON-RPC. Talks to Coordinator. |
| `internal/agent/manager.go` | Pure CRUD business logic, no PTY coupling. |
| `internal/agent/coordinator.go` | Message queue is PTY-agnostic. Only the `OnDeliverMessage` callback changes (one line). |
| `internal/models/` | Pure data types. |
| `internal/config/` | Config loading only. |
| `internal/persistence/` | JSON file store only. |
| `internal/git/` | Git operations only. |
| `internal/history/` | Session file parsers — still useful for post-hoc analysis. |
| `internal/report/` | Report formatting — input source changes but format stays. |

---

## Phases

### Phase 0 — Stream Format Verification (Prerequisite Gate)

**Goal**: Capture actual `stream-json` output from Claude CLI and document the real message format before writing any parser code. This is a **hard go/no-go gate** — if the format differs from assumptions, the type system must match reality, not speculation.

**Steps**:
1. Run a simple bidirectional streaming test:
   ```bash
   echo '{"type":"user","content":"say hello"}' | claude -p --input-format stream-json --output-format stream-json --include-hook-events 2>/dev/null
   ```
2. Capture and document every message type, field name, and structure.
3. Test multi-turn: can we send a second JSON message on stdin after receiving the first response? Or does `--print` exit after one turn?
4. If multi-turn doesn't work via stdin, test the fallback: `--resume <session-id>` with repeated `--print` invocations.

**Output**: A `docs/stream-json-format.md` reference file with real captured examples.

**Go/no-go decision**:
- If bidirectional multi-turn works → proceed with long-lived process model
- If `--print` exits after one turn → pivot to repeated invocations with `--resume --session-id`
- If `stream-json` format is wildly different from assumptions → adjust type definitions before proceeding

**Commit**: `docs: capture and document claude stream-json protocol format`

---

### Phase 1 — Process Manager Foundation

**Goal**: Replace `internal/terminal/` with `internal/process/` — a new package that spawns Claude CLI as child processes with stdin/stdout JSON pipes instead of PTY sessions.

#### 1.1 — Update `CommandBuilder` for headless mode

**File**: `internal/agent/command_builder.go`

> **Why first**: Runner and Pool consume the command builder output. Changing the return type from `string` to `[]string` after they're built creates a chicken-and-egg problem. Define the interface first.

Change `Build()` (line 19) to return `[]string` (args slice) instead of `string` (shell command). The new `claudeCommand()` builds:

```
claude -p
  --input-format stream-json
  --output-format stream-json
  --include-hook-events
  --mcp-config '{"mcpServers":{"skwad":{"type":"http","url":"..."}}}'
  --allowed-tools 'mcp__skwad__*'
  --append-system-prompt "<skwad instructions + persona>"
  --permission-mode auto
  --model <model if specified>
  --session-id <uuid>
  --add-dir <workDir if specified>
  --name <agent name>
```

No more `--plugin-dir` for Claude agents (hook events come from the JSON stream — no duplicate status events).
No more wrapping in `$SHELL -c "..."` — we call `exec.Command("claude", args...)` directly.

Non-Claude agent commands (`codexCommand`, `geminiCommand`, `copilotCommand`) return an error — explicitly unsupported in headless mode for now.

**Tests**: `internal/agent/command_builder_test.go` — verify args slice output, flag presence, non-Claude error.

**Commit**: `refactor: update command builder to return args slice for headless mode`

---

#### 1.2 — Create `internal/process/` package with `Runner` type

**New file**: `internal/process/runner.go`

The `Runner` replaces `Session`. It:
- Spawns `claude` via `exec.Command` (not PTY) using args from `CommandBuilder`
- Connects stdin pipe (for sending JSON messages) and stdout pipe (for reading JSON stream)
- Captures stderr separately for error reporting (logged as warnings, never mixed with JSON stream)
- Manages process lifecycle (start, stop, kill, wait)
- Exposes callbacks: `OnMessage func(msg StreamMessage)`, `OnExit func(exitCode int)`
- Uses process groups for orphan cleanup: `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`
- Graceful shutdown sequence: SIGTERM → wait 5 seconds → SIGKILL

```go
type Runner struct {
    mu       sync.Mutex
    cmd      *exec.Cmd
    stdin    io.WriteCloser
    stdout   io.ReadCloser
    stderr   io.ReadCloser
    stopped  atomic.Bool
    exitCode atomic.Int32
    done     chan struct{}

    OnMessage func(msg StreamMessage)
    OnExit    func(exitCode int)
}

func NewRunner(args []string, env []string, dir string) (*Runner, error)
func (r *Runner) Start() error
func (r *Runner) SendPrompt(text string) error   // writes JSON to stdin
func (r *Runner) Stop() error                     // SIGTERM → 5s timeout → SIGKILL
func (r *Runner) Kill()                           // immediate SIGKILL (process group)
func (r *Runner) IsRunning() bool
func (r *Runner) ExitCode() int
func (r *Runner) Wait() <-chan struct{}
```

**Platform note**: On macOS, `Pdeathsig` is not available (Linux-only). Orphan cleanup relies on process group kill via `Setpgid` + signal forwarding in `StopAll()`. Document this limitation.

**New file**: `internal/process/stream.go`

JSON stream types matching Claude CLI's `stream-json` output format. **Types MUST match the real format captured in Phase 0** — do not use speculative definitions.

```go
// Populated from Phase 0 findings
type StreamMessage struct {
    Type    string          `json:"type"`
    // ... fields from actual stream-json output
}

type UserMessage struct {
    Type    string `json:"type"`
    Content string `json:"content"`
}
```

The stdout read loop in `Runner.Start()` decodes newline-delimited JSON from stdout and dispatches to `OnMessage`. Unknown message types are logged and skipped (lenient parsing).

**Tests**: `internal/process/runner_test.go` — test spawn, send, receive, stop, kill, graceful shutdown timeout using a mock command (small Go test binary that speaks JSON on stdin/stdout).

**Commit**: `feat: add process runner for headless agent management`

---

#### 1.3 — Create `Pool` in `internal/process/`

**New file**: `internal/process/pool.go`

The new `Pool` replaces `internal/terminal/pool.go`. It:
- Manages a map of `uuid.UUID → *managedAgent` (runner + metadata)
- Spawns agents via `Runner`
- Routes messages to agents via `SendPrompt()`
- Collects output from agents via `OnMessage` callback
- Provides `OutputSubscriber` for CI mode output collection
- Provides `LogSubscriber` for watch mode log streaming
- Handles graceful shutdown (`StopAll` — SIGTERM all, wait, SIGKILL stragglers)
- Detects agent readiness: waits for first `system` or `assistant` message on stdout before marking agent as ready to receive prompts

```go
type managedAgent struct {
    runner  *Runner
    agentID uuid.UUID
    name    string
    ready   chan struct{} // closed when first message received
}

type Pool struct {
    mu          sync.RWMutex
    agents      map[uuid.UUID]*managedAgent
    builder     *agent.CommandBuilder
    manager     *agent.Manager
    coordinator *agent.Coordinator

    OutputSubscriber func(agentID uuid.UUID, agentName string, msg StreamMessage)
    LogSubscriber    func(agentID uuid.UUID, agentName string, text string)
}

func NewPool(mgr *agent.Manager, coord *agent.Coordinator, builder *agent.CommandBuilder) *Pool
func (p *Pool) Spawn(a *models.Agent) error
func (p *Pool) SendPrompt(agentID uuid.UUID, text string) error  // blocks until agent ready
func (p *Pool) Stop(agentID uuid.UUID) error
func (p *Pool) Kill(agentID uuid.UUID)
func (p *Pool) StopAll()
func (p *Pool) IsRunning(agentID uuid.UUID) bool
func (p *Pool) ExitCode(agentID uuid.UUID) int
```

Key difference from old Pool: No `InjectText`, no `QueueText`, no `ActivityController`, no ANSI stripping. Message delivery is `SendPrompt()` which writes JSON to stdin. Readiness detection replaces the old `time.Sleep(2 * time.Second)` hack.

**Tests**: `internal/process/pool_test.go`

**Commit**: `feat: add process pool for multi-agent lifecycle management`

---

#### 1.4 — Rewrite `ActivityController` for stream-based status

**File**: `internal/agent/activity.go`

The current `ActivityController` (line 19) is built around terminal output events and idle timers. For headless mode:

- **Remove**: `OnTerminalOutput()`, `OnUserInput()` — no terminal
- **Keep**: `OnHookRunning()`, `OnHookIdle()`, `OnHookBlocked()` — now driven by parsed JSON stream events instead of HTTP hook posts
- **Add**: `OnStreamEvent(msg StreamMessage)` — parses hook events from the JSON stream and routes to appropriate status handler
- **Keep**: `QueueText()` and pending delivery logic — delivery now calls `Runner.SendPrompt()` instead of `Session.InjectText()`
- **Simplify**: Remove idle timer logic — hook events from the JSON stream are the source of truth for Claude agents

**Tests**: `internal/agent/activity_test.go` — test stream event parsing → status transitions.

**Commit**: `refactor: rewrite activity controller for stream-based status detection`

---

### Phase 2 — Daemon & CLI Rewiring

**Goal**: Wire the new process manager into the daemon and CLI commands. Remove the existing terminal-emulator TUI.

#### 2.1 — Rewire `daemon.go`

**File**: `internal/daemon/daemon.go`

Changes:
- `Daemon` struct (line 28): Replace `Pool *terminal.Pool` with `Pool *process.Pool`
- `Start()` (line 81): Create `process.NewPool()` instead of `terminal.NewPool()`
- Message delivery callback (line 100-102): Change from `d.Pool.InjectText(agentID, text)` to `d.Pool.SendPrompt(agentID, text)`
- `hookBridge` (line 121): Keep for future non-Claude agents. For Claude agents, status comes from the JSON stream via Pool → ActivityController. No duplicate paths.
- Wire `Pool.OutputSubscriber` for stream message routing

**Tests**: `internal/daemon/daemon_test.go`

**Commit**: `refactor: rewire daemon to use process pool`

---

#### 2.2 — Update `start.go` + remove terminal-emulator TUI

**File**: `internal/cli/start.go`

Changes:
- Spawn loop (line 69): `d.Pool.Spawn(a)` — same API, new implementation
- **Remove `--watch` TUI mode** (lines 83-96): The existing TUI (`internal/tui/`) is a terminal emulator wrapping PTY sessions via `bubbleterm` — fundamentally incompatible with headless JSON processes. Remove it now, replace with log stream watch mode (Phase 2.5).
- Headless mode (line 99-105): Still blocks on signals, manages processes

**Commit**: `refactor: update start command for headless agent processes`

---

#### 2.3 — Update `run.go`

**File**: `internal/cli/run.go`

Changes:
- Output collection (line 111-117): Change `OutputSubscriber` to receive `StreamMessage` instead of raw `[]byte`. Parse structured JSON for report data.
- Prompt injection (line 144-169): Change `d.Pool.QueueText()` to `d.Pool.SendPrompt()`. Remove `time.Sleep(2 * time.Second)` hack — use Pool's readiness detection instead.
- Wait loop (line 172-199): Same polling logic, new pool API
- Exit code (line 209): Same API

**Commit**: `refactor: update run command for stream-json output`

---

#### 2.4 — Update MCP hook handler

**File**: `internal/mcp/hooks.go`

Keep the `/hook` HTTP endpoint for future non-Claude agents only. For Claude agents, the JSON stream is the sole status source (no `--plugin-dir`, no plugin scripts posting to `/hook`). No dedup logic needed — there's only one status path per agent type.

**Commit**: `refactor: make http hook handler supplementary for non-claude agents`

---

#### 2.5 — Log stream watch mode

**Goal**: Replace the terminal-emulator TUI with a simple log stream for `skwad start --watch`. This gives immediate visibility into what agents are doing without any TUI framework.

**File**: `internal/cli/watch.go` (new)

Implementation:
- Subscribe to `Pool.LogSubscriber` to receive formatted agent output
- Print each agent's messages with a colored prefix and timestamp:
  ```
  [14:32:01] [Explorer] Reading internal/agent/manager.go...
  [14:32:03] [Coder]    Editing internal/process/runner.go:45
  [14:32:05] [Tester]   Running tests... 12/12 passed
  [14:32:06] [Manager]  Delegating review to Reviewer
  ```
- Color-coded by agent name (consistent per agent, not random)
- Parse `StreamMessage` types to produce human-readable summaries:
  - `assistant` messages → show text content (truncated if long)
  - `tool_use` → show tool name + target file
  - `hook_event` → show status change (Running, Idle, Blocked)
  - Other types → skip or show type name only
- `--watch` flag on `skwad start` enables this mode
- Also available as `skwad watch` command connecting to a running daemon (reads from MCP server or daemon socket)

**No new dependencies** — just formatted stdout using `log/slog` or `fmt` with ANSI color codes.

**Tests**: `internal/cli/watch_test.go` — test message formatting and color assignment.

**Commit**: `feat: add log stream watch mode for agent activity monitoring`

---

### Phase 3 — Cleanup & Removal

**Goal**: Remove dead code and dependencies.

#### 3.1 — Remove `internal/terminal/` package

Delete:
- `internal/terminal/session.go` — replaced by `internal/process/runner.go`
- `internal/terminal/pool.go` — replaced by `internal/process/pool.go`
- `internal/terminal/manager.go` — no longer needed (no terminal interface)
- `internal/terminal/cleaner.go` — no ANSI to strip from JSON
- All corresponding test files

**Commit**: `refactor: remove terminal package (replaced by process)`

---

#### 3.2 — Remove `internal/tui/` package

Delete:
- `internal/tui/` — entire package. The terminal-emulator TUI (`bubbleterm`-based) is incompatible with headless agents. Replaced by log stream watch mode (Phase 2.5).

**Commit**: `refactor: remove terminal-emulator tui package`

---

#### 3.3 — Remove PTY and TUI dependencies

**File**: `go.mod`

Remove:
- `github.com/creack/pty v1.1.24` — no more PTY sessions
- `bubbleterm` — no more terminal emulation widget
- Any other dependencies only used by `internal/terminal/` or `internal/tui/`

Run `go mod tidy`.

**Commit**: `chore: remove creack/pty and bubbleterm dependencies`

---

#### 3.4 — Remove Claude plugin scripts

**Directory**: `plugin/claude/`

Remove Claude plugin scripts (`startup.sh`, `activity.sh`) that POST to `/hook`. With `--include-hook-events` in the JSON stream, these are dead code for Claude agents. No `--plugin-dir` flag is passed to Claude processes anymore.

Keep `plugin/codex/` if it exists — future non-Claude agents may still use HTTP hooks.

**Commit**: `chore: remove claude plugin scripts (replaced by stream hook events)`

---

### Phase 4 — TUI Dashboard

**Goal**: Build an interactive TUI dashboard for `skwad start --watch` that goes beyond the log stream. This is a richer monitoring experience built on parsed JSON data, NOT terminal emulation.

**Tech**: Bubble Tea v2 + Lip Gloss v2 (no `bubbleterm` — we're rendering text, not terminals)

**Layout**:
```
┌─────────────────────────────────────────────────────┐
│  Agent          Status       Last Activity          │
│  ─────          ──────       ─────────────          │
│  ● Explorer     Running      Reading pool.go        │
│  ● Coder        Idle         Waiting for task       │
│  ● Tester       Running      12/12 tests passed     │
│  ● Reviewer     Idle         —                      │
├─────────────────────────────────────────────────────┤
│  [Explorer] Reading internal/agent/manager.go...    │
│  [Explorer] Found 3 PTY-coupled methods             │
│  [Manager]  Delegating implementation to Coder      │
│  [Coder]    Editing internal/process/runner.go:45   │
│  [Coder]    Running `go test ./internal/process/`   │
│  [Tester]   All 12 tests passing                    │
├─────────────────────────────────────────────────────┤
│  MCP: http://127.0.0.1:8766  │  Agents: 4/4 active │
└─────────────────────────────────────────────────────┘
```

**Panels**:
- **Top**: Agent status table — name, status dot (green/orange/red), current activity summary
- **Middle**: Scrollable activity log — same formatted output as log stream watch mode, but in a scrollable buffer
- **Bottom**: Status bar — MCP URL, active agent count

**Keyboard shortcuts**:
- `q` — quit
- `j/k` or `↑/↓` — scroll activity log
- `tab` — filter log to specific agent
- `s` — send message to agent (prompt input)
- `?` — help

**Data source**: Same `Pool.LogSubscriber` + `Pool.OutputSubscriber` as the log stream, but rendered in Bubble Tea panels instead of raw stdout.

**New package**: `internal/tui/` (reused name, completely different implementation — text dashboard, not terminal emulator)

**Files**:
- `internal/tui/app.go` — Bubble Tea model, top-level layout
- `internal/tui/status_table.go` — agent status panel
- `internal/tui/activity_log.go` — scrollable log panel
- `internal/tui/status_bar.go` — bottom bar

**Commit strategy**:
- `feat: add tui dashboard scaffold with bubble tea v2`
- `feat: add agent status table panel`
- `feat: add scrollable activity log panel`
- `feat: add keyboard shortcuts and status bar`

---

## Stream JSON Protocol

The bidirectional streaming protocol is the core of this architecture. Understanding it is critical.

**IMPORTANT**: All type definitions below are speculative. Phase 0 MUST verify the actual format before any code is written.

### Sending prompts (stdin)

```json
{"type": "user", "content": "Implement the auth module"}
```

### Receiving output (stdout)

Each line is a JSON object. Expected message types (to be verified in Phase 0):

```json
{"type": "assistant", "message": {"role": "assistant", "content": [...]}, "session_id": "..."}
{"type": "tool_use", "tool": "Edit", "input": {...}}
{"type": "tool_result", "tool": "Edit", "output": "..."}
{"type": "hook_event", "hook": "activity", "status": "running"}
{"type": "hook_event", "hook": "activity", "status": "idle"}
{"type": "system", "content": "Session started"}
{"type": "result", "result": "...", "session_id": "..."}
```

### Important flags

| Flag | Purpose |
|------|---------|
| `--print` | Non-interactive mode, required for piped I/O |
| `--input-format stream-json` | Accept JSON on stdin |
| `--output-format stream-json` | Emit JSON on stdout |
| `--include-hook-events` | Include hook lifecycle events in stream |
| `--include-partial-messages` | Include partial chunks as they arrive |
| `--replay-user-messages` | Echo user messages back on stdout for ack |

---

## Execution Order

```
Phase 0 (Verification) — hard prerequisite:
  0.1 Capture real stream-json output, document format, go/no-go decision

Phase 1 (Foundation) — sequential, ordered by dependency:
  1.1 CommandBuilder → return []string args (no dependencies, consumed by Runner/Pool)
  1.2 Runner         → new process/runner.go (uses args from CommandBuilder)
  1.3 Pool           → new process/pool.go (depends on Runner)
  1.4 ActivityCtrl   → rewrite for streams (depends on stream types from 1.2)

Phase 2 (Rewiring) — after Phase 1 is solid:
  2.1 Daemon         → rewire to new Pool
  2.2 start.go       → update spawn calls, remove terminal-emulator TUI mode
  2.3 run.go         → update output collection, remove sleep hack
  2.4 hooks.go       → make supplementary for non-Claude only
  2.5 watch mode     → log stream for --watch flag

Phase 3 (Cleanup) — after Phase 2 works end-to-end:
  3.1 Remove internal/terminal/ package
  3.2 Remove internal/tui/ package (terminal emulator)
  3.3 Remove creack/pty + bubbleterm dependencies
  3.4 Remove Claude plugin scripts

Phase 4 (TUI Dashboard) — after cleanup:
  4.1 Bubble Tea scaffold
  4.2 Status table panel
  4.3 Activity log panel
  4.4 Keyboard shortcuts + status bar
```

---

## Commit Strategy

| # | Commit | Phase |
|---|--------|-------|
| 1 | `docs: capture and document claude stream-json protocol format` | 0.1 |
| 2 | `refactor: update command builder to return args slice for headless mode` | 1.1 |
| 3 | `feat: add process runner for headless agent management` | 1.2 |
| 4 | `feat: add process pool for multi-agent lifecycle management` | 1.3 |
| 5 | `refactor: rewrite activity controller for stream-based status detection` | 1.4 |
| 6 | `refactor: rewire daemon to use process pool` | 2.1 |
| 7 | `refactor: update start command, remove terminal-emulator tui mode` | 2.2 |
| 8 | `refactor: update run command for stream-json output` | 2.3 |
| 9 | `refactor: make http hook handler supplementary for non-claude agents` | 2.4 |
| 10 | `feat: add log stream watch mode for agent activity monitoring` | 2.5 |
| 11 | `refactor: remove terminal package (replaced by process)` | 3.1 |
| 12 | `refactor: remove terminal-emulator tui package` | 3.2 |
| 13 | `chore: remove creack/pty and bubbleterm dependencies` | 3.3 |
| 14 | `chore: remove claude plugin scripts (replaced by stream hook events)` | 3.4 |
| 15 | `feat: add tui dashboard scaffold with bubble tea v2` | 4.1 |
| 16 | `feat: add agent status table panel` | 4.2 |
| 17 | `feat: add scrollable activity log panel` | 4.3 |
| 18 | `feat: add keyboard shortcuts and status bar` | 4.4 |

Each commit is buildable and testable. Tests at every step.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| `stream-json` format undocumented — actual output may differ from assumptions | **Phase 0 gate**: capture real output, verify format before writing any parser code. Design stream parser to skip unknown message types gracefully. |
| Long-lived `--print` processes may not support multi-turn via stdin | Verify in Phase 0. Fallback: use `--resume <session-id>` with repeated `--print` invocations per prompt. |
| Orphan Claude processes on crash | Process group management: `Setpgid: true` + kill entire process group on shutdown. On macOS, `Pdeathsig` is unavailable — rely on `StopAll()` signal forwarding. |
| Stderr pollution — Claude CLI may write warnings/errors to stderr | Capture stderr in a separate goroutine, log as warnings via `slog`, never mix with JSON stream parsing. |
| MCP server connection timing — agent may call MCP before server is ready | MCP server starts before agent spawn (already the case). Agent's first MCP call may fail — Claude CLI handles tool call retries internally. |
| Agent readiness — when is a process ready to receive stdin prompts? | Pool tracks readiness via `ready` channel — closed when first message is received on stdout. `SendPrompt()` blocks until ready. Replaces the old `time.Sleep(2s)` hack. |
| Duplicate status events if plugin scripts AND JSON stream both active | Eliminated: no `--plugin-dir` for Claude agents. JSON stream is sole status source. HTTP `/hook` endpoint kept only for future non-Claude agents. |
| Graceful shutdown — agents may be mid-tool-execution | SIGTERM → 5 second grace period → SIGKILL. Allows Claude to finish current tool execution and clean up. |

---

## Status

- [ ] Phase 0.1 — Stream format verification (prerequisite gate)
- [ ] Phase 1.1 — Command Builder update
- [ ] Phase 1.2 — Process Runner
- [ ] Phase 1.3 — Process Pool
- [ ] Phase 1.4 — Activity Controller rewrite
- [ ] Phase 2.1 — Daemon rewire
- [ ] Phase 2.2 — start.go update + remove TUI
- [ ] Phase 2.3 — run.go update
- [ ] Phase 2.4 — Hook handler update
- [ ] Phase 2.5 — Log stream watch mode
- [ ] Phase 3.1 — Remove terminal/ package
- [ ] Phase 3.2 — Remove tui/ package
- [ ] Phase 3.3 — Remove creack/pty + bubbleterm deps
- [ ] Phase 3.4 — Remove Claude plugin scripts
- [ ] Phase 4.1 — TUI dashboard scaffold
- [ ] Phase 4.2 — Status table panel
- [ ] Phase 4.3 — Activity log panel
- [ ] Phase 4.4 — Keyboard shortcuts + status bar

---

## Review Notes

Plan reviewed by Reviewer agent. Key revisions incorporated:
1. Added Phase 0 as hard prerequisite gate for stream-json format verification
2. Reordered Phase 1: CommandBuilder (1.1) before Runner (1.2) to resolve dependency ordering
3. Added explicit TUI removal in Phase 2.2 and Phase 3.2 — existing `internal/tui/` is incompatible with headless model
4. Added process group management detail and graceful shutdown sequence (SIGTERM → 5s → SIGKILL)
5. Added agent readiness detection via `ready` channel (replaces `time.Sleep` hack)
6. Resolved plugin script ambiguity: remove `--plugin-dir` for Claude, no duplicate status events
7. Non-Claude agent spawn paths explicitly return errors (Claude-only for now)
8. Added log stream watch mode (Phase 2.5) and TUI dashboard (Phase 4) as two-tier watch solution

---

## Key Learnings

(To be filled after implementation)
