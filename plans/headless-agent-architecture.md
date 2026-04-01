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
3. **Explicitly verify `--include-hook-events` output**: Capture the exact structure of hook events in the stream (field names, status values, event lifecycle). The entire status detection pipeline in Phase 1.4 depends on this format. If hook events aren't present or differ from assumptions, the ActivityController rewrite must adapt.
4. Test multi-turn: can we send a second JSON message on stdin after receiving the first response? Or does `--print` exit after one turn?
5. If multi-turn doesn't work via stdin, test the fallback: `--resume <session-id>` with repeated `--print` invocations.

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

Add a new `BuildArgs()` method that returns `[]string` (args slice) alongside the existing `Build()` method. The existing `Build()` stays untouched so `internal/terminal/pool.go` continues to compile. `Build()` will be removed in Phase 3.1 when `internal/terminal/` is deleted. The new `BuildArgs()` is consumed by `Runner` and `Pool` in Phases 1.2-1.3. The new `claudeCommand()` builds:

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

**Important**: `SendPrompt()` must select on BOTH the `ready` channel AND the runner's `done` channel. If the process exits before becoming ready (crash, permission error, bad args), `ready` will never close. Without this guard, `SendPrompt()` hangs forever. Implementation:
```go
select {
case <-ma.ready: // agent is ready, proceed to send
case <-ma.runner.Wait(): // agent died before becoming ready
    return fmt.Errorf("agent %s exited before becoming ready (exit code %d)", ma.name, ma.runner.ExitCode())
}
```

**Tests**: `internal/process/pool_test.go`

**Commit**: `feat: add process pool for multi-agent lifecycle management`

---

#### 1.4 — Rewrite `ActivityController` for stream-based status

**File**: `internal/agent/activity.go`

The current `ActivityController` (line 19) is built around terminal output events and idle timers. For headless mode:

- **Remove**: `OnTerminalOutput()`, `OnUserInput()` — no terminal
- **Remove**: `activateInputGuard()`, `guardActive`, `inputGuard` timer — the input guard mechanism was designed around human keyboard input timing in PTY mode. In headless mode there's no human typing, so the entire guard mechanism is dead code.
- **Keep**: `OnHookRunning()`, `OnHookIdle()`, `OnHookBlocked()` — now driven by parsed JSON stream events instead of HTTP hook posts
- **Add**: `OnStreamEvent(msg StreamMessage)` — parses hook events from the JSON stream and routes to appropriate status handler
- **Simplify**: `QueueText()` becomes trivial — no input guard, no pending delivery timing. Either call `Runner.SendPrompt()` directly from the caller (Pool), or keep `QueueText()` as a thin passthrough. Prefer removing `QueueText()` entirely and having `Pool.SendPrompt()` be the sole entry point for message delivery.
- **Remove**: idle timer logic — hook events from the JSON stream are the source of truth for Claude agents

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

#### 2.2 — Update `start.go` + replace terminal-emulator TUI with log stream

**File**: `internal/cli/start.go`

> **Note**: Phase 2.5 (log stream watch mode) MUST land before or together with this commit. `start.go` imports `internal/tui/` and `bubbletea` — removing the TUI code path without a replacement won't compile. We merge 2.2 and 2.5 into a single commit to maintain compilation continuity.

Changes:
- Spawn loop (line 69): `d.Pool.Spawn(a)` — same API, new implementation
- **Replace `--watch` TUI mode** (lines 83-96): Remove `tui.New()` call and `bubbletea` import. Replace with log stream watch mode (see Phase 2.5 below). The `--watch` flag stays, but now starts the log stream instead of the terminal-emulator TUI.
- Remove imports: `internal/tui/`, `bubbletea`, `bubbleterm`
- Headless mode (line 99-105): Still blocks on signals, manages processes

**Commit**: `refactor: update start command, replace tui with log stream watch mode`

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
- `internal/terminal/cleaner.go` — no ANSI to strip from JSON. **Exception**: Move `StripANSI()` and `CleanTitle()` to a new `internal/text/` utility package before deleting. Future non-Claude agent integrations (Codex, Gemini) may produce ANSI output that needs stripping.
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

### Phase 4 — TUI Dashboard (deferred — post-pivot polish)

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

### Phase 5 — Enhancements (Inspired by oh-my-codex competitive analysis)

**Context**: Analysis of oh-my-codex (OMX) — a tmux-based Codex CLI wrapper written in TypeScript + Rust — revealed several patterns worth adopting. OMX's core architecture (tmux keystroke injection, Codex dependency) is inferior to our headless approach, but several of their feature-level ideas are excellent. These are post-pivot enhancements that layer on top of the headless foundation.

**Dependency**: Phases 0-3 must be complete (headless foundation working end-to-end). Phase 4 (TUI) is independent and can be done in parallel.

---

#### 5.1 — Explore mode (sandboxed read-only agent)

**Inspiration**: OMX's `omx-explore` crate — sandboxed Codex sessions with allowlisted commands, symlink escape detection, and path validation. Prevents exploration agents from modifying the codebase.

**Goal**: Add an explore mode that restricts an agent's permissions to read-only operations, useful for codebase analysis tasks where you want safety guarantees.

##### 5.1.1 — Model + config changes

**File**: `internal/models/agent.go`

Add field to `Agent` struct (after `IsCompanion bool`):
```go
ExploreMode bool `json:"exploreMode"` // restrict agent to read-only operations
```

**File**: `internal/config/team.go`

Add field to `AgentConfig` struct:
```go
ExploreMode bool `json:"explore_mode,omitempty"` // restrict to read-only mode
```

Wire in `teamConfigToAgents()`: copy `ExploreMode` from config to agent model.

**Commit**: `feat: add explore mode field to agent model and team config`

##### 5.1.2 — CommandBuilder explore mode flags

**File**: `internal/agent/command_builder.go`

In the new `BuildArgs()` method (Phase 1.1), after permission mode logic, add explore mode flag injection:
```go
if a.ExploreMode {
    args = append(args, "--permission-mode", "plan")
    args = append(args, "--allowed-tools", "Read,Glob,Grep,WebSearch,WebFetch,mcp__skwad__*")
} else {
    args = append(args, "--permission-mode", permissionMode)
    args = append(args, "--allowed-tools", "mcp__skwad__*")
}
```

> **Note**: The code snippet above uses the args-slice API from Phase 1.1 (`BuildArgs()`), NOT the old string builder (`sb.WriteString`). The old `Build()` method is kept for backward compat until Phase 3.1 but explore mode only needs to work in the new headless path.

Note: `--permission-mode plan` is Claude's built-in read-only mode — it prevents `Edit`, `Write`, `Bash` (destructive), and `NotebookEdit`. We layer `--allowed-tools` on top for belt-and-suspenders.

**Tests**: `internal/agent/command_builder_test.go`
- Test: explore mode agent → command contains `--permission-mode plan` and restricted `--allowed-tools`
- Test: normal agent → command contains `--permission-mode auto` and standard `--allowed-tools`
- Test: explore mode in team config → propagates to agent model → propagates to command

**Commit**: `feat: add explore mode permission flags to command builder`

##### 5.1.3 — CLI support

**File**: `internal/cli/run.go`

Add `--explore` flag that marks all agents as explore mode (useful for one-shot analysis):
```go
cmd.Flags().BoolVar(&explore, "explore", false, "Run all agents in read-only explore mode")
```

When set, override `ExploreMode = true` on all agents before spawning.

**Commit**: `feat: add --explore flag to run command`

---

#### 5.2 — Output summarization for large results

**Inspiration**: OMX's `omx-sparkshell` — auto-detects when output exceeds a line threshold and summarizes via LLM. Smart context-budget management.

**Goal**: When collecting agent output in CI `run` mode, detect oversized results and truncate/summarize so reports stay readable.

##### 5.2.1 — Truncation engine

**New file**: `internal/report/summarizer.go`

```go
type SummaryConfig struct {
    MaxLines     int  // default 500 — truncate above this
    HeadLines    int  // default 50 — keep first N lines
    TailLines    int  // default 50 — keep last N lines
}

// Truncate checks line count and returns truncated output if over threshold.
// Returns (output, wasTruncated).
func Truncate(output string, cfg SummaryConfig) (string, bool)
```

Logic:
- Count lines in output
- If under `MaxLines`: return as-is, `false`
- If over: return `head(HeadLines) + "\n\n[... N lines truncated ...]\n\n" + tail(TailLines)`, `true`

**Tests**: `internal/report/summarizer_test.go`
- Test: output under threshold → unchanged
- Test: output over threshold → head + marker + tail, correct line counts
- Test: edge case: exactly at threshold → unchanged
- Test: empty output → unchanged

**Commit**: `feat: add output truncation for large agent results`

##### 5.2.2 — Wire into report pipeline

**File**: `internal/report/report.go`

In `FormatMarkdown()` and `FormatJSON()`, before writing agent output:
```go
cfg := SummaryConfig{MaxLines: 500, HeadLines: 50, TailLines: 50}
output, truncated := Truncate(agent.Output, cfg)
if truncated {
    slog.Info("truncated agent output", "agent", agent.Name, "originalLines", lineCount)
}
```

**File**: `internal/config/team.go`

Add optional per-agent output limit:
```go
OutputLimit int `json:"output_limit,omitempty"` // max lines before truncation (default 500)
```

When set, override `SummaryConfig.MaxLines` for that agent.

**File**: `internal/cli/run.go`

Add `--output-limit` flag:
```go
cmd.Flags().IntVar(&outputLimit, "output-limit", 500, "Max output lines per agent before truncation (0=unlimited)")
```

**Tests**: Integration test — run with large output, verify report contains truncation marker.

**Commit**: `feat: wire output truncation into report pipeline and cli`

---

#### 5.3 — Phase-gated CI pipeline with iteration limits

**Inspiration**: OMX's team orchestrator state machine: `plan → prd → exec → verify → fix` with configurable fix-loop limits preventing runaway execution.

**Goal**: Add iteration limits and optional phase sequencing to `skwad run` so CI pipelines don't loop forever.

##### 5.3.1 — Iteration tracking

**New file**: `internal/run/pipeline.go`

```go
type Pipeline struct {
    MaxIterations int           // default 3, 0=unlimited
    Timeout       time.Duration // existing timeout logic, moved here
    Iteration     int           // current iteration count
    Phase         string        // current phase name
    StartedAt     time.Time
    Events        []PipelineEvent
}

type PipelineEvent struct {
    Time      time.Time `json:"time"`
    Type      string    `json:"type"`      // "phase_start", "phase_end", "iteration", "timeout", "complete"
    Phase     string    `json:"phase"`
    Iteration int       `json:"iteration"`
    Detail    string    `json:"detail"`
}

func NewPipeline(maxIterations int, timeout time.Duration) *Pipeline
func (p *Pipeline) NextIteration() (int, error)  // returns iteration number or error if max hit
func (p *Pipeline) SetPhase(name string)
func (p *Pipeline) RecordEvent(eventType, detail string)
func (p *Pipeline) IsExpired() bool
```

`NextIteration()` increments counter and returns `ErrMaxIterationsReached` if over limit.

**Tests**: `internal/run/pipeline_test.go`
- Test: iteration counting up to max
- Test: `ErrMaxIterationsReached` when exceeded
- Test: phase transitions recorded in events
- Test: timeout expiry detection

**Commit**: `feat: add pipeline iteration tracking and phase management`

##### 5.3.2 — Wire into run command

**File**: `internal/cli/run.go`

Replace the current wait loop (polling `Pool.IsRunning()` every 2s) with pipeline-managed execution:

```go
cmd.Flags().IntVar(&maxIterations, "max-iterations", 3, "Max fix→verify cycles before stopping (0=unlimited)")
```

Current flow (simplified):
```
spawn → prompt → wait loop (poll IsRunning every 2s) → report
```

New flow:
```
spawn → pipeline.NextIteration() → prompt → wait loop → check results
  → if agents exited cleanly: pipeline.RecordEvent("complete") → report
  → if agents exited with retryable code AND iterations remaining: pipeline.NextIteration() → re-prompt → wait loop
  → if agents exited with non-retryable code: pipeline.RecordEvent("fatal_exit") → report with exit code 2
  → if max iterations reached: pipeline.RecordEvent("max_iterations") → report with exit code 1
```

The re-prompt on retryable exit is the key difference — it enables fix loops. The agent gets a follow-up prompt like: `"Previous iteration failed (exit code N). Please review and fix the issues."` This is only active when `--max-iterations > 1`.

**Exit code classification** (Claude CLI exit codes):
- Exit 0 → success, no retry
- Exit 1 → tool/task failure → **retryable**
- Exit 2 → permission denied → **not retryable** (user intervention needed)
- Exit 130 → SIGINT (user cancelled) → **not retryable**
- Exit 137 → SIGKILL (OOM or force kill) → **not retryable**
- Other non-zero → **retryable by default**, configurable via `--no-retry-exits` flag

```go
cmd.Flags().IntSliceVar(&noRetryExits, "no-retry-exits", []int{2, 130, 137}, "Exit codes that should NOT trigger a retry iteration")
```

**File**: `internal/report/report.go`

Add pipeline events to report:
```go
type RunReport struct {
    Agents   []AgentResult   `json:"agents"`
    Pipeline []PipelineEvent `json:"pipeline,omitempty"` // NEW — phase/iteration history
}
```

**Tests**: Integration test — verify iteration loop, max enforcement, report includes pipeline events.

**Commit**: `feat: wire pipeline iteration limits into run command`

---

#### 5.4 — Enriched agent system prompt with team protocol

**Inspiration**: OMX's worker-bootstrap system — per-worker AGENTS.md overlays with XML-structured role prompts, team protocol, communication patterns, verification requirements, and anti-patterns.

**Goal**: Replace the minimal `skwadInstructions()` + `personaPrompt()` with a rich, layered system prompt that includes behavioral directives, team coordination protocol, role-specific guidance, and verification gates.

##### 5.4.1 — Universal prompt preamble (all agents)

**File**: `internal/agent/command_builder.go`

Replace the current `skwadInstructions()` function (line ~181) with a structured multi-section prompt builder.

**New file**: `internal/agent/prompt.go`

```go
// BuildSystemPrompt constructs the full system prompt for an agent.
// Layers: preamble → team protocol → role instructions → persona
func BuildSystemPrompt(agent *models.Agent, persona *models.Persona, teammates []models.Agent) string
```

**Layer 1 — Universal preamble** (injected into ALL agents):

```
You are part of a team of agents called a skwad. A skwad is made of high-performing agents
who collaborate to achieve complex goals.

Your skwad agent ID: {agentID}

## Operating Principles
- Execute tasks to completion without asking for permission on obvious next steps.
- If blocked, try an alternative approach before escalating.
- Prefer evidence over assumption — verify before claiming completion.
- Proceed automatically on clear, low-risk, reversible steps.
- Default to compact, information-dense responses.

## Verification Protocol
Before claiming any task is complete, verify:
1. Identify what proves the claim (test output, build success, file evidence).
2. Run the verification.
3. Read and interpret the output.
4. Report with evidence. No evidence = not complete.

## Continuation Check
Before concluding your work, confirm:
- No pending work items remain
- Features working as specified
- Tests passing (if applicable)
- Zero known errors
- Verification evidence collected
If any item fails, continue working rather than reporting incomplete.

## Failure Recovery
After 3 distinct failed approaches on the same blocker, stop adding risk.
Escalate clearly to your teammates or the user with what you tried and what failed.
```

**Tests**: Verify preamble includes all sections, agent ID is interpolated.

**Commit**: `feat: add universal prompt preamble with operating principles`

##### 5.4.2 — Team protocol section

**File**: `internal/agent/prompt.go`

**Layer 2 — Team protocol** (injected when agent is part of a team, i.e., teammates exist):

```
## Your Skwad Team

| Agent | Role | ID |
|-------|------|----|
{for each teammate: | {name} | {persona.Name or "General"} | {id} |}

## Communication Protocol
CRITICAL: Before you start working on anything, your FIRST action must be calling
set-status with what you are about to do. When you finish, call set-status again.
When you change direction, call set-status. Other agents depend on your status to
coordinate — if you do not update it, the team cannot function.

Available MCP tools for coordination:
- set-status: Update your status so teammates know what you're doing
- send-message: Send a message to a specific teammate by name
- check-messages: Check your inbox for messages from teammates
- broadcast-message: Send a message to all teammates
- list-agents: See all agents and their current status

## Coordination Rules
- When you need help with exploration, coding, testing, or review, prefer
  coordinating with your skwad teammates over spinning up local subagents.
  Your teammates are already running and have shared context.
- Only edit files within your assigned scope. If you need to modify a file
  another agent is working on, send them a message and coordinate.
- Commit your changes before reporting task completion.
- When delegating work, provide complete context: goal, relevant files,
  constraints, and expected output format.
```

**Tests**: Verify team roster table is generated correctly, all MCP tools listed, protocol rules present.

**Commit**: `feat: add team protocol section to agent system prompt`

##### 5.4.3 — Role-specific instructions

**File**: `internal/agent/prompt.go`

**Layer 3 — Role-specific behavioral rules** based on persona name or agent name pattern matching. This enriches the basic persona instructions with structured behavioral guidance.

Add a `roleInstructions()` function that maps known role names to behavioral rules:

```go
var rolePrompts = map[string]string{
    "explorer": `## Role: Explorer
<constraints>
  <scope_guard>Read-only. You explore, search, and analyze — you do NOT edit code.</scope_guard>
  <ask_gate>Never return incomplete results. The caller should be able to proceed immediately.</ask_gate>
</constraints>
<execution>
  - Launch 3+ parallel searches on your first action. Use broad-to-narrow strategy.
  - Before reading a file, check its size. For files >200 lines, scan for the relevant section first.
  - Always provide file paths with line numbers, not just summaries.
  - Structure your response: Summary → Key Files table → How It Works → Impact → Open Questions.
</execution>`,

    "coder": `## Role: Coder
<constraints>
  <scope_guard>Prefer the smallest viable diff. Do not broaden scope unless correctness requires it.</scope_guard>
  <ask_gate>If one reasonable interpretation exists, proceed. Ask only when progress is impossible.</ask_gate>
</constraints>
<execution>
  - Implement, then verify: run tests, check diagnostics, confirm build succeeds.
  - No debug leftovers (console.log, print, TODO hacks) in final code.
  - Report: Changes Made (file:line for every change) → Test Results → Issues Found.
</execution>
<anti_patterns>
  - Overengineering instead of a direct fix.
  - Scope creep beyond the assigned task.
  - Claiming completion without running tests.
</anti_patterns>`,

    "tester": `## Role: Tester
<constraints>
  <scope_guard>Write and run tests. Do not modify production code unless a test setup requires a minor testability fix.</scope_guard>
  <ask_gate>If test intent is clear, write the test. Ask only when the expected behavior is ambiguous.</ask_gate>
</constraints>
<execution>
  - Cover: happy path, edge cases, error states.
  - Run ALL tests after writing new ones — ensure no regressions.
  - Report: Tests Written → Test Results (pass/fail counts) → Failing Tests → Coverage Gaps.
</execution>`,

    "reviewer": `## Role: Reviewer
<constraints>
  <scope_guard>Review only. Do not edit production code or tests. Report findings.</scope_guard>
  <ask_gate>Complete the review fully before reporting. Do not ask for clarification on obvious patterns.</ask_gate>
</constraints>
<execution>
  - Two-stage review: spec compliance FIRST, then code quality.
  - Rate issues by severity: CRITICAL / HIGH / MEDIUM / LOW.
  - Never approve code with CRITICAL or HIGH severity issues.
  - Report: Verdict (APPROVE/REQUEST_CHANGES/COMMENT) → Summary → Issues table → What Looks Good.
</execution>`,

    "manager": `## Role: Manager
<constraints>
  <scope_guard>Plan, coordinate, delegate, verify. You NEVER write code or tests yourself.</scope_guard>
  <ask_gate>Surface architectural trade-offs to the user. Do not make these decisions alone.</ask_gate>
</constraints>
<execution>
  - Break tasks into discrete work items with clear ownership.
  - Provide complete context when delegating: goal, files, constraints, expected output.
  - Verify delivered work meets the original requirements before marking complete.
  - Conservative staffing: prefer minimal fanout unless the task is clearly decomposable.
</execution>`,
}
```

Matching logic: case-insensitive **substring/contains** match on `agent.Name` or `persona.Name`. This ensures names like "Lead Coder", "Senior Reviewer", or "my-code-explorer" still match the correct role. First match wins (check `agent.Name` first, then `persona.Name`). If no match, skip this layer (the persona instructions alone are sufficient for custom roles).

**Tests**: `internal/agent/prompt_test.go`
- Test: agent named "Explorer" → gets explorer role instructions
- Test: agent named "My Custom Agent" → gets no role instructions (just persona)
- Test: case-insensitive matching ("CODER" → coder rules)
- Test: persona named "Reviewer" → gets reviewer role instructions even if agent name differs

**Commit**: `feat: add role-specific behavioral instructions to agent prompts`

##### 5.4.4 — Persona layer (existing, moved)

**File**: `internal/agent/prompt.go`

**Layer 4 — Persona** (existing logic, moved from `command_builder.go`):

The current `personaPrompt()` function moves into `BuildSystemPrompt()` as the final layer. This is the user-customizable part — team config `persona_instructions` or stored persona `Instructions`.

```go
// Layer 4: persona instructions (user-customizable)
if persona != nil && persona.Instructions != "" {
    prompt += "\n\n## Persona: " + persona.Name + "\n" + persona.Instructions
}
```

**Commit**: `refactor: move persona prompt into layered system prompt builder`

##### 5.4.5 — Wire into CommandBuilder + team config

**File**: `internal/agent/command_builder.go`

Replace the inline system prompt construction in `claudeCommand()` (lines 89-93):

```go
// OLD:
// systemPrompt := skwadInstructions(a.ID.String())
// if persona != nil { systemPrompt += " " + personaPrompt(persona) }

// NEW:
systemPrompt := BuildSystemPrompt(a, persona, teammates)
```

This requires `Build()` to receive the teammates list. Update the `CommandBuilder` to accept `[]models.Agent` (the full team roster minus the current agent).

**File**: `internal/config/team.go`

Add optional `protocol` field to `TeamConfig` for custom team protocol overrides:
```go
type TeamConfig struct {
    // ... existing fields ...
    Protocol string `json:"protocol,omitempty"` // custom team protocol (appended to default)
}
```

When set, this text is appended to the team protocol section (Layer 2), allowing teams to add custom coordination rules.

**Tests**:
- End-to-end: build system prompt for a 3-agent team → verify all 4 layers present
- Test: custom protocol in team config → appended to team protocol section
- Test: solo agent (no teammates) → team protocol section omitted
- Test: total prompt length is reasonable (under 3000 tokens for a 5-agent team, approximated at 4 chars/token — assert `len(prompt) < 12000` chars)

**Commit**: `feat: wire layered system prompt into command builder and team config`

---

#### 5.5 — Event-sourced run state (for long-running CI)

**Inspiration**: OMX's `omx-runtime-core` Rust engine — event-sourced state machine with replay/recovery from event log. Robust against crashes during long-running operations.

**Goal**: For long-running CI executions (`skwad run`), persist state as an append-only event log so runs can be resumed after crashes. This is what makes hours-long runs viable.

##### 5.5.1 — Event log writer

**New file**: `internal/persistence/eventlog.go`

```go
type EventType string

const (
    EventRunStarted       EventType = "run_started"
    EventAgentSpawned     EventType = "agent_spawned"
    EventAgentRegistered  EventType = "agent_registered"
    EventPromptSent       EventType = "prompt_sent"
    EventResponseReceived EventType = "response_received"
    EventAgentExited      EventType = "agent_exited"
    EventPhaseTransition  EventType = "phase_transition"
    EventIteration        EventType = "iteration"
    EventRunCompleted     EventType = "run_completed"
    EventRunFailed        EventType = "run_failed"
)

type Event struct {
    Time      time.Time       `json:"time"`
    Type      EventType       `json:"type"`
    RunID     string          `json:"run_id"`
    AgentID   string          `json:"agent_id,omitempty"`
    AgentName string          `json:"agent_name,omitempty"`
    Data      json.RawMessage `json:"data,omitempty"` // event-specific payload
}

type EventLog struct {
    mu     sync.Mutex
    file   *os.File
    runID  string
    enc    *json.Encoder
}

// NewEventLog creates or opens an event log at ~/.config/skwad/runs/<runID>/events.jsonl
func NewEventLog(runID string) (*EventLog, error)

// Append writes an event to the log. Fsync strategy: immediate fsync for critical
// events (run_started, run_completed, run_failed, agent_exited), batched fsync
// (every 10 events or 5 seconds) for high-frequency events (prompt_sent, response_received).
// This balances durability with performance for long-running multi-agent runs.
func (l *EventLog) Append(event Event) error

// Close flushes and closes the log file
func (l *EventLog) Close() error
```

**Storage path**: `~/.config/skwad/runs/<run-id>/events.jsonl` — one JSON object per line, append-only.

Run ID format: `<timestamp>-<short-uuid>` e.g., `20260401-053200-a1b2c3d4`

**Tests**: `internal/persistence/eventlog_test.go`
- Test: create log, append events, close, re-read → all events present
- Test: concurrent appends (goroutine safety)
- Test: fsync durability (write, kill, re-read → last event present)
- Test: invalid run ID → error

**Commit**: `feat: add append-only event log for run state persistence`

##### 5.5.2 — Event log reader + state replay

**New file**: `internal/persistence/replay.go`

```go
type RunState struct {
    RunID           string
    StartedAt       time.Time
    Agents          map[string]AgentRunState // agentID → state
    CurrentPhase    string
    CurrentIteration int
    Completed       bool
    Failed          bool
    LastEvent       Event
}

type AgentRunState struct {
    AgentID    string
    AgentName  string
    Spawned    bool
    Registered bool
    Exited     bool
    ExitCode   int
    PromptsSent int
    LastPrompt  string // the last prompt text sent (for re-send on resume)
}

// Replay reads an event log and reconstructs RunState
func Replay(runID string) (*RunState, error)
```

Replay logic:
1. Open `events.jsonl`, decode line by line
2. For each event, update `RunState`:
   - `run_started` → set `StartedAt`
   - `agent_spawned` → add agent to map, `Spawned = true`
   - `agent_registered` → mark `Registered = true`
   - `prompt_sent` → increment `PromptsSent`, save `LastPrompt`
   - `agent_exited` → mark `Exited = true`, save `ExitCode`
   - `phase_transition` → update `CurrentPhase`
   - `iteration` → update `CurrentIteration`
   - `run_completed` → mark `Completed = true`
   - `run_failed` → mark `Failed = true`
3. Return reconstructed state

**Tests**: `internal/persistence/replay_test.go`
- Test: replay empty log → zero state
- Test: replay full lifecycle → correct final state
- Test: replay partial (crash mid-run) → state reflects last known point
- Test: agents that were spawned but not exited → `Exited = false`

**Commit**: `feat: add event log replay for run state reconstruction`

##### 5.5.3 — Wire into run command with --resume

**File**: `internal/cli/run.go`

Add flags:
```go
cmd.Flags().StringVar(&runID, "run-id", "", "Custom run ID (default: auto-generated)")
cmd.Flags().StringVar(&resumeID, "resume", "", "Resume a previous run by ID")
```

**New run flow with event logging**:
```
1. Generate or use provided run-id
2. Create EventLog
3. Append EventRunStarted
4. For each agent spawn → Append EventAgentSpawned
5. For each prompt sent → Append EventPromptSent
6. Wire Pool.OutputSubscriber to → Append EventResponseReceived (summary, not full output)
7. Wire Pool exit callback → Append EventAgentExited
8. On completion → Append EventRunCompleted
9. On failure/timeout → Append EventRunFailed
```

**Resume flow**:
```
1. Replay(resumeID) → get RunState
2. If RunState.Completed or RunState.Failed → error "run already finished"
3. Re-create agents that were spawned but not exited
4. Restore original UUIDs: the event log's `agent_spawned` events contain the UUID-to-name
   mapping. Override the auto-generated UUIDs from `createAgentsFromConfig()` with the
   original UUIDs from the event log. This ensures MCP registrations, message routing,
   and persistence all reference the same agent IDs as the original run.
5. Re-spawn them with --resume --session-id <sessionID> to resume Claude sessions
6. Re-send last prompt to agents that received prompts but didn't exit
7. Continue normal wait loop from current iteration
8. Append events to SAME log file (continuation)
```

**UUID restoration detail**: `AgentRunState` in the replayed state contains `AgentID` (UUID) and `AgentName`. On resume, match replayed agents to team config agents by name, then assign the replayed UUID instead of generating a new one. If a name doesn't match (team config changed between runs), skip that agent and log a warning.

**Session continuity**: Claude's `--resume --session-id` flag allows resuming a previous conversation. Store `session_id` (from stream JSON output) in the event log's `agent_registered` or `response_received` events so resumed agents can continue where they left off.

**CLI UX**:
```bash
# Normal run (auto-generates run ID, prints it)
skwad run -t dev-team.json -p "fix the auth bug"
# Output: Run ID: 20260401-053200-a1b2c3d4

# Resume after crash
skwad run --resume 20260401-053200-a1b2c3d4

# List recent runs
skwad run --list-runs
```

**File**: `internal/cli/run.go` — add `--list-runs` flag:
```go
cmd.Flags().BoolVar(&listRuns, "list-runs", false, "List recent run IDs with status")
```

Scans `~/.config/skwad/runs/` directories, reads last event from each, displays table:
```
Run ID                          Status      Started             Agents  Iterations
20260401-053200-a1b2c3d4        completed   2026-04-01 05:32    3       1
20260401-041500-e5f6g7h8        failed      2026-04-01 04:15    5       3
20260331-220000-i9j0k1l2        interrupted 2026-03-31 22:00    3       2
```

**Tests**:
- Test: normal run creates event log with all lifecycle events
- Test: resume from interrupted state → agents re-spawned, prompts re-sent
- Test: resume completed run → error
- Test: `--list-runs` displays correct status from event logs
- Test: run ID is printed to stdout on start

**Commit**: `feat: wire event-sourced state into run command with resume support`

##### 5.5.4 — Run state cleanup

**File**: `internal/cli/run.go`

Add `--clean-runs` flag to delete old run state:
```go
cmd.Flags().StringVar(&cleanRuns, "clean-runs", "", "Delete run state older than duration (e.g., '7d', '24h', 'all')")
```

Deletes `~/.config/skwad/runs/<run-id>/` directories older than the specified duration.

**Commit**: `feat: add run state cleanup for old event logs`

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
  2.5 watch mode     → log stream for --watch flag (MUST land before 2.2)
  2.2 start.go       → update spawn calls, replace TUI with log stream (merged with 2.5 in one commit)
  2.3 run.go         → update output collection, remove sleep hack
  2.4 hooks.go       → make supplementary for non-Claude only

Phase 3 (Cleanup) — after Phase 2 works end-to-end:
  3.1 Remove internal/terminal/ package
  3.2 Remove internal/tui/ package (terminal emulator)
  3.3 Remove creack/pty + bubbleterm dependencies
  3.4 Remove Claude plugin scripts

Phase 4 (TUI Dashboard) — after cleanup, deferred:
  4.1 Bubble Tea scaffold
  4.2 Status table panel
  4.3 Activity log panel
  4.4 Keyboard shortcuts + status bar

Phase 5 (Enhancements — from oh-my-codex analysis) — after headless pivot is solid:
  5.1 Explore mode:
    5.1.1 Model + config changes
    5.1.2 CommandBuilder explore flags
    5.1.3 CLI --explore flag
  5.2 Output summarization:
    5.2.1 Truncation engine
    5.2.2 Wire into report pipeline + CLI
  5.3 Phase-gated CI:
    5.3.1 Pipeline iteration tracking
    5.3.2 Wire into run command
  5.4 Enriched system prompts (depends on 5.3 for pipeline context):
    5.4.1 Universal prompt preamble
    5.4.2 Team protocol section
    5.4.3 Role-specific instructions
    5.4.4 Persona layer (move existing)
    5.4.5 Wire into CommandBuilder + team config
  5.5 Event-sourced run state (depends on 5.3 for pipeline events):
    5.5.1 Event log writer
    5.5.2 Event log reader + replay
    5.5.3 Wire into run command with --resume
    5.5.4 Run state cleanup

  Parallelization: 5.1, 5.2, 5.4 are independent — can be built in parallel.
  5.3 should land before 5.5 (pipeline events feed into event log).
  5.4.1-5.4.3 can be built in parallel, then 5.4.4-5.4.5 sequentially.
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
| 7 | `feat: add log stream watch mode for agent activity monitoring` | 2.5 |
| 8 | `refactor: update start command, replace tui with log stream watch mode` | 2.2 |
| 9 | `refactor: update run command for stream-json output` | 2.3 |
| 10 | `refactor: make http hook handler supplementary for non-claude agents` | 2.4 |
| 11 | `refactor: remove terminal package (replaced by process)` | 3.1 |
| 12 | `refactor: remove terminal-emulator tui package` | 3.2 |
| 13 | `chore: remove creack/pty and bubbleterm dependencies` | 3.3 |
| 14 | `chore: remove claude plugin scripts (replaced by stream hook events)` | 3.4 |
| 15 | `feat: add tui dashboard scaffold with bubble tea v2` | 4.1 |
| 16 | `feat: add agent status table panel` | 4.2 |
| 17 | `feat: add scrollable activity log panel` | 4.3 |
| 18 | `feat: add keyboard shortcuts and status bar` | 4.4 |
| 19 | `feat: add explore mode field to agent model and team config` | 5.1.1 |
| 20 | `feat: add explore mode permission flags to command builder` | 5.1.2 |
| 21 | `feat: add --explore flag to run command` | 5.1.3 |
| 22 | `feat: add output truncation for large agent results` | 5.2.1 |
| 23 | `feat: wire output truncation into report pipeline and cli` | 5.2.2 |
| 24 | `feat: add pipeline iteration tracking and phase management` | 5.3.1 |
| 25 | `feat: wire pipeline iteration limits into run command` | 5.3.2 |
| 26 | `feat: add universal prompt preamble with operating principles` | 5.4.1 |
| 27 | `feat: add team protocol section to agent system prompt` | 5.4.2 |
| 28 | `feat: add role-specific behavioral instructions to agent prompts` | 5.4.3 |
| 29 | `refactor: move persona prompt into layered system prompt builder` | 5.4.4 |
| 30 | `feat: wire layered system prompt into command builder and team config` | 5.4.5 |
| 31 | `feat: add append-only event log for run state persistence` | 5.5.1 |
| 32 | `feat: add event log replay for run state reconstruction` | 5.5.2 |
| 33 | `feat: wire event-sourced state into run command with resume support` | 5.5.3 |
| 34 | `feat: add run state cleanup for old event logs` | 5.5.4 |

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
| System prompt token budget — rich prompts could waste context | Keep total prompt under 3000 tokens (~5 agents). Monitor with token counter. Team protocol section omitted for solo agents. |
| Event log file corruption on hard crash | fsync after each event write. Replay skips malformed lines (lenient JSON parsing). Last event may be lost on power failure — acceptable. |
| Resume session continuity — Claude `--resume` may not work across process restarts | Store `session_id` from stream JSON. If `--resume` fails, fall back to new session with context summary of previous work. |
| Iteration loops — fix→verify may not converge | Default max 3 iterations. Non-zero exit after max → clear error message in report. User must explicitly increase limit. |
| Role instruction mismatch — custom personas may conflict with role instructions | Role instructions are additive, not overriding. Persona layer (Layer 4) has final say. If persona contradicts role, persona wins. |

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
- [ ] Phase 5.1.1 — Explore mode: model + config changes
- [ ] Phase 5.1.2 — Explore mode: command builder flags
- [ ] Phase 5.1.3 — Explore mode: CLI --explore flag
- [ ] Phase 5.2.1 — Output summarization: truncation engine
- [ ] Phase 5.2.2 — Output summarization: report pipeline + CLI wiring
- [ ] Phase 5.3.1 — Phase-gated CI: pipeline iteration tracking
- [ ] Phase 5.3.2 — Phase-gated CI: wire into run command
- [ ] Phase 5.4.1 — System prompts: universal preamble
- [ ] Phase 5.4.2 — System prompts: team protocol section
- [ ] Phase 5.4.3 — System prompts: role-specific instructions
- [ ] Phase 5.4.4 — System prompts: persona layer (move existing)
- [ ] Phase 5.4.5 — System prompts: wire into command builder + team config
- [ ] Phase 5.5.1 — Event-sourced state: event log writer
- [ ] Phase 5.5.2 — Event-sourced state: replay + state reconstruction
- [ ] Phase 5.5.3 — Event-sourced state: wire into run command with --resume
- [ ] Phase 5.5.4 — Event-sourced state: run state cleanup

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
9. **Competitive analysis (2026-04-01)**: Deep analysis of oh-my-codex (OMX) — a tmux-based TypeScript+Rust Codex wrapper. Confirmed our headless architecture is the right direction (OMX is fundamentally tmux-dependent, can't run headless). Added Phase 5 with five enhancements cherry-picked from OMX's best ideas: explore mode, output summarization, phase-gated CI, enriched team protocol prompts, and event-sourced run state
10. **Full plan review (2026-04-01)**: Reviewer approved with 12 concerns (1 CRITICAL, 3 HIGH, 4 MEDIUM, 4 LOW). All addressed:
    - CRITICAL: `Build()` → `BuildArgs()` alongside existing method (compilation continuity)
    - HIGH: `SendPrompt()` select on `ready` + `done` channels (prevents hang on early exit)
    - HIGH: ActivityController input guard explicitly removed (dead code in headless mode)
    - HIGH: Phase 2.5 (log stream) lands before 2.2 (TUI removal) — compilation continuity
    - MEDIUM: Phase 5.1.2 code snippet updated to use args-slice API
    - MEDIUM: Exit code classification for retry — only retryable codes trigger iteration
    - MEDIUM: Role matching changed to substring/contains (catches "Lead Coder" etc.)
    - MEDIUM: UUID restoration on resume detailed — match by name from event log
    - LOW: Phase 0 explicitly tests `--include-hook-events` format
    - LOW: `StripANSI`/`CleanTitle` moved to `internal/text/` before terminal/ deletion
    - LOW: fsync batching strategy (immediate for critical events, batched for high-frequency)
    - LOW: Token budget test with measurable assertion (4 chars/token approximation)

---

## Key Learnings

(To be filled after implementation)
