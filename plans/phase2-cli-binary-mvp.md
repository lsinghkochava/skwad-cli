# Phase 2: CLI Binary (MVP)

## Goal

Build the `skwad-cli` command-line tool as a separate binary in the same Go repo. Strictly CLI-only — no GUI, no Fyne. Must run on headless servers and CI runners.

## Architecture

```
cmd/skwad-cli/main.go          <- CLI entry point (Cobra)
internal/cli/                   <- Cobra command definitions
  root.go, start.go, status.go, list.go,
  send.go, broadcast.go, watch.go, stop.go, run.go
internal/daemon/daemon.go       <- Shared lifecycle: Store + Manager + Coordinator + MCP + Pool
internal/config/team.go         <- Team config schema + loader + validation
```

The CLI binary imports all `internal/` packages EXCEPT `internal/ui/`. No build tags needed — just no `ui` import.

## Design Decisions

- **`internal/daemon/`** encapsulates the full lifecycle (Store → Manager → Coordinator → MCP Server → Pool → hookBridge). Both the GUI (`cmd/skwad/`) and CLI (`cmd/skwad-cli/`) instantiate a `Daemon`. Avoids duplicating ~200 lines of initialization code.
- **Cobra commands live in `internal/cli/`**, not `cmd/skwad-cli/cmd/`. `main.go` just calls `cli.Execute()`. Commands are independently testable.
- **Client commands use REST endpoints** — `GET /` for status/list, new `POST /api/v1/agent/send` and `POST /api/v1/agent/broadcast` for messaging. NOT MCP JSON-RPC (that's for AI agents, not CLI tools).
- **Port discovery** — client commands find the daemon via `SKWAD_MCP_PORT` env var (consistent with plugin scripts), `--port` flag override, or fallback to default 8766.
- **`skwad start` is foreground-only** — blocks on SIGINT/SIGTERM. Use tmux/screen/nohup for background. Client commands (`status`, `send`, etc.) run from a second terminal.
- **`skwad watch` is in-process only** — runs as `skwad start --watch` mode. Remote streaming deferred to Phase 3 (requires SSE endpoint).
- **PID file with flock** — `skwad start` writes `~/.config/skwad/skwad.pid` with advisory lock. `skwad stop` reads PID, verifies process is alive, sends SIGTERM. Stale PID files detected and cleaned up.
- **`skwad run` completion** — an agent is "completed" when its PTY session exits. If timeout reached, all remaining agents get SIGTERM. If one agent errors, others continue until timeout. Exit codes: 0=all exited cleanly, 1=timeout/error, 2=any agent exited non-zero.

---

## Action Items

### 2.1 — Create `internal/daemon/` package

**Goal**: Extract the shared initialization sequence from `cmd/skwad/main.go` into a reusable `Daemon` struct.

**`internal/daemon/daemon.go`**:
```go
type Config struct {
    MCPPort     int
    DataDir     string  // ~/.config/skwad/
    PluginDir   string
}

type Daemon struct {
    Store       *persistence.Store
    Manager     *agent.Manager
    Coordinator *agent.Coordinator
    MCPServer   *mcp.Server
    Pool        *terminal.Pool
}

func New(cfg Config) (*Daemon, error)   // initialize all components, wire hookBridge
func (d *Daemon) Start() error          // start MCP server + pool
func (d *Daemon) Stop() error           // graceful shutdown: stop pool, stop MCP, save state
```

- The `hookBridge` (currently in `cmd/skwad/main.go`) moves inside the Daemon as a private struct
- Update `cmd/skwad/main.go` to use `daemon.New()` + `daemon.Start()` instead of inline initialization
- MCP UI callbacks (`OnDisplayMarkdown`, `OnCreateAgent`, etc.) remain nil in CLI mode — already nil-safe

**Files**:
- `internal/daemon/daemon.go` (new)
- `cmd/skwad/main.go` (refactor to use Daemon)

**Tests**: `go build ./...` compiles, `go test ./...` passes, GUI binary still works.

**Commit**: `feat: extract daemon lifecycle into internal/daemon package`

---

### 2.2 — Add Cobra + scaffold CLI binary

**Goal**: Create the CLI binary entry point with all subcommand stubs.

**Steps**:
- `go get github.com/spf13/cobra`
- Create `cmd/skwad-cli/main.go` — calls `cli.Execute()`
- Create `internal/cli/root.go` — root command with `--port`, `--config`, `--verbose` persistent flags
- Create stub files: `start.go`, `status.go`, `list.go`, `send.go`, `broadcast.go`, `stop.go`, `run.go`
- Verify `go build ./cmd/skwad-cli/` compiles and does NOT pull in Fyne (check binary size / `go list -deps`)

**Files**:
- `cmd/skwad-cli/main.go` (new)
- `internal/cli/root.go` (new)
- `internal/cli/start.go` through `run.go` (new, stubs)

**Commit**: `feat: scaffold skwad-cli binary with cobra`

---

### 2.3 — Team config schema + loader

**Goal**: Define the team config JSON format and a loader with basic validation.

**`internal/config/team.go`**:
```go
type TeamConfig struct {
    Name   string        `json:"name"`
    Repo   string        `json:"repo"`
    Agents []AgentConfig `json:"agents"`
}

type AgentConfig struct {
    Name         string   `json:"name"`
    AgentType    string   `json:"agent_type"`
    Persona      string   `json:"persona,omitempty"`
    Command      string   `json:"command,omitempty"`
    AllowedTools []string `json:"allowed_tools,omitempty"`
    Prompt       string   `json:"prompt,omitempty"`
}

func LoadTeamConfig(path string) (*TeamConfig, error)
func (tc *TeamConfig) Validate() error
```

**Validation** (Phase 2 — basic):
- `name` required
- `repo` required, must be a valid directory
- At least one agent
- Each agent: `name` required, `agent_type` required and must be known type (claude, codex, gemini, copilot, opencode, custom)
- No duplicate agent names

**Tests**: Valid config loads, invalid configs error with clear messages.

**Commit**: `feat: add team config schema and loader`

---

### 2.4 — `skwad start` command

**Goal**: Foreground daemon that spawns agents from config and runs MCP server.

**Behavior**:
1. Load team config from `--config` flag (required)
2. Validate repo path exists
3. Initialize `Daemon` with config
4. Create agents from team config (map `AgentConfig` → `models.Agent`, add to Manager)
5. Spawn all agents via Pool
6. Start MCP server
7. Write PID file with flock (`~/.config/skwad/skwad.pid`)
8. Print startup banner: port, agent count, agent names
9. Block on SIGINT/SIGTERM
10. On signal: graceful shutdown via `Daemon.Stop()`, remove PID file

**Flags**: `--config` (required), `--port` (default 8766), `--data-dir` (default `~/.config/skwad/`)

**Files**:
- `internal/cli/start.go`
- `internal/daemon/pidfile.go` (new — PID file + flock helpers)

**Commit**: `feat: implement skwad start command`

---

### 2.5 — Add REST endpoints for send/broadcast

**Goal**: Add thin REST wrappers so CLI client commands can send messages without MCP JSON-RPC.

**Endpoints**:
- `POST /api/v1/agent/send` — `{from, to, content}` → coordinator.SendMessage
- `POST /api/v1/agent/broadcast` — `{from, content}` → coordinator.BroadcastMessage

**Response**: `200 {success: true, message: "..."}` or `400/404` with error

**Files**:
- `internal/mcp/server.go` — add routes + handlers
- `internal/mcp/types.go` — add request/response structs

**Tests**: Integration tests for both endpoints.

**Commit**: `feat: add REST endpoints for send and broadcast`

---

### 2.6 — `skwad status` + `skwad list` commands

**Goal**: Query running daemon and display agent information.

**`skwad status`**:
- HTTP GET to `http://127.0.0.1:{port}/`
- Parse JSON response
- Display as formatted table: Name | Type | State | Status | Folder
- Colorize state: green=running, yellow=idle, red=error, orange=blocked

**`skwad list`**:
- Same data source, simpler output: Name | ID | Type

**Port discovery**: `--port` flag > `SKWAD_MCP_PORT` env var > default 8766

**Files**:
- `internal/cli/status.go`
- `internal/cli/list.go`
- `internal/cli/client.go` (new — shared HTTP client helper for port discovery + requests)

**Commit**: `feat: implement skwad status and list commands`

---

### 2.7 — `skwad send` + `skwad broadcast` commands

**Goal**: Send messages between agents via the daemon.

**`skwad send`**: `skwad send --from <name|id> --to <name|id> "message"`
- POST to `/api/v1/agent/send`

**`skwad broadcast`**: `skwad broadcast --from <name|id> "message"`
- POST to `/api/v1/agent/broadcast`

**Files**:
- `internal/cli/send.go`
- `internal/cli/broadcast.go`

**Commit**: `feat: implement skwad send and broadcast commands`

---

### 2.8 — Output streaming + `skwad start --watch`

**Goal**: Add output subscriber pattern to Pool, then wire it into `start --watch` mode.

**Pool changes** (`internal/terminal/pool.go`):
- Add `OutputSubscriber func(agentID uuid.UUID, agentName string, data []byte)` callback field
- In the PTY read loop, call subscriber (non-blocking) alongside existing output handling
- Subscriber must not slow down the PTY read

**`start --watch` mode**:
- `--watch` flag on `skwad start`
- When enabled, print agent output to stdout as it arrives
- Per-agent ANSI color prefix (cycle through 6-8 colors)
- Per-agent line buffer — only emit complete lines
- Format: `[AgentName] output line here`

**Files**:
- `internal/terminal/pool.go` — add subscriber callback
- `internal/cli/start.go` — add `--watch` flag, wire subscriber to stdout
- `internal/cli/watch.go` — `skwad watch` prints "not supported standalone, use `skwad start --watch`" for now

**Commit**: `feat: implement output streaming and start --watch mode`

---

### 2.9 — `skwad stop` command

**Goal**: Gracefully shut down a running daemon.

**Behavior**:
1. Read PID from `~/.config/skwad/skwad.pid`
2. If PID file missing → error "no daemon running"
3. Verify PID is alive (signal 0)
4. If PID is dead → clean up stale PID file, error "no daemon running (cleaned stale PID file)"
5. Send SIGTERM
6. Wait up to 5s, polling process status
7. If still alive → SIGKILL
8. Print "Daemon stopped"

**Files**:
- `internal/cli/stop.go`

**Commit**: `feat: implement skwad stop with graceful shutdown`

---

### 2.10 — `skwad run` — one-shot mode

**Goal**: CI/scripting mode. Spawn → prompt → wait → report → exit.

**Flags**: `--config` (required), `--prompt` or `--prompt-file`, `--timeout` (default 10m), `--output-format` (markdown|json, default markdown), `--port` (default 8766)

**Behavior**:
1. Load and validate team config
2. Initialize Daemon, create agents, spawn via Pool
3. Start MCP server
4. Wait for agents to register (up to 30s)
5. Send initial prompt to each agent via Pool's `QueueText()`
   - Per-agent prompt from config, or `--prompt`/`--prompt-file` for all
6. Wait loop: check every 2s if all agent PTY sessions have exited
   - If all exited → proceed to report
   - If `--timeout` reached → SIGTERM remaining agents, wait 5s, SIGKILL
7. Collect output from Pool output buffers
8. Format report:
   - `markdown`: agent name + output as fenced code blocks
   - `json`: `{agents: [{name, type, exit_code, output}]}`
9. Print report to stdout
10. Graceful shutdown
11. Exit code: 0=all exited 0, 1=timeout/infra error, 2=any agent exited non-zero

**Files**:
- `internal/cli/run.go`

**Commit**: `feat: implement skwad run one-shot command`

---

### 2.11 — Integration tests

**Goal**: End-to-end tests for the CLI commands.

**Tests**:
- Team config: load valid, reject invalid (missing name, unknown type, duplicate agents)
- Daemon lifecycle: New → Start → Stop (verify MCP server responds, then stops cleanly)
- PID file: write + flock, stale detection, cleanup
- REST send/broadcast endpoints
- `run` one-shot: spawn echo agent, verify output collected, verify exit code
- Output subscriber: verify callback receives data from spawned process

**Files**:
- `internal/config/team_test.go` (new)
- `internal/daemon/daemon_test.go` (new)
- `internal/cli/run_test.go` (new — or `integration_test.go`)
- `internal/mcp/integration_test.go` (add send/broadcast REST tests)

**Commit**: `test: phase 2 integration tests`

---

## Milestone

Fully functional CLI. `skwad start --config team.json` spawns agents. `skwad status` shows states. `skwad send` routes messages. `skwad start --watch` streams output. `skwad stop` tears down. `skwad run` works in CI. Works over SSH on headless servers.

## Status

- [ ] 2.1 — Create `internal/daemon/` package
- [ ] 2.2 — Add Cobra + scaffold CLI binary
- [ ] 2.3 — Team config schema + loader
- [ ] 2.4 — `skwad start` command
- [ ] 2.5 — Add REST endpoints for send/broadcast
- [ ] 2.6 — `skwad status` + `skwad list` commands
- [ ] 2.7 — `skwad send` + `skwad broadcast` commands
- [ ] 2.8 — Output streaming + `skwad start --watch`
- [ ] 2.9 — `skwad stop` command
- [ ] 2.10 — `skwad run` — one-shot mode
- [ ] 2.11 — Integration tests
