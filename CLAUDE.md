# skwad-cli

## Purpose

Headless multi-agent CLI orchestrator written in Go. Spawns AI coding agents (Claude Code, Codex, Gemini CLI, GitHub Copilot, OpenCode, or custom) in parallel PTY sessions, coordinates them via a built-in MCP (JSON-RPC 2.0) HTTP server, and manages their lifecycle from the command line. Designed for both interactive use and CI pipelines.

## What This App Does

- Runs multiple AI coding agents in parallel, each in its own PTY session
- Organizes agents into named workspaces loaded from team config JSON files
- Provides a built-in MCP HTTP server so agents can register, message each other, query worktrees, and spawn new agents
- Supports CI one-shot mode (`skwad run`) — spawn, prompt, wait, collect output, report, exit
- Posts markdown reports and GitHub PR comments (`skwad report`)
- Converts macOS Skwad workspace exports to CLI team configs (`skwad convert`)
- Hook-based status detection — plugin scripts post agent state changes to the MCP server

## CLI Commands

| Command | Description |
|---------|-------------|
| `start` | Start daemon: load config, spawn agents, block on signals |
| `stop` | Send SIGTERM to running daemon via PID file |
| `status` | Query daemon, display color-coded agent state table |
| `list` | Query daemon, display agent names + IDs + types |
| `send` | Send message between agents via MCP |
| `broadcast` | Broadcast message to all agents |
| `run` | CI one-shot: spawn, prompt, wait, collect, report, exit |
| `report` | Format JSON report as markdown or GitHub PR comment |
| `convert` | Convert macOS workspace export to CLI team config |

## Package Layout

| Package | Purpose |
|---------|---------|
| `cmd/skwad-cli/` | Entry point — calls `cli.Execute()` |
| `internal/cli/` | Cobra command tree, global flags, structured logging |
| `internal/daemon/` | Lifecycle orchestrator — wires Store + Manager + Coordinator + MCP + Pool |
| `internal/mcp/` | JSON-RPC 2.0 MCP HTTP server, tool handlers, session manager, hook handler |
| `internal/terminal/` | PTY session management, terminal pool, activity controller, text cleaner |
| `internal/agent/` | Agent manager (CRUD, lifecycle), coordinator (message queue), command builder |
| `internal/models/` | Pure data types — Agent, Workspace, Persona, BenchAgent, AppSettings |
| `internal/config/` | Team config loader, built-in templates (go:embed), macOS converter |
| `internal/persistence/` | JSON file store (~/.config/skwad/) |
| `internal/git/` | Git CLI wrapper, repository ops, worktree management, fsnotify watcher |
| `internal/history/` | Session file parsers (Claude, Codex, Gemini, Copilot) |
| `internal/autopilot/` | LLM classifier for agent output → action routing |
| `internal/report/` | Markdown/JSON report formatter, GitHub PR comment poster |
| `internal/search/` | Fuzzy file search scorer |
| `internal/notifications/` | Desktop notification service (notify-send) |
| `plugin/` | Hook scripts for Claude and Codex agent integration |
| `examples/` | Example team config files |

## Technology

| Concern | Choice |
|---------|--------|
| Language | Go 1.23+ |
| CLI framework | spf13/cobra |
| PTY | creack/pty |
| File watching | fsnotify |
| UUIDs | google/uuid |
| MCP server | net/http stdlib, JSON-RPC 2.0 |
| Logging | log/slog (stdlib) |
| Persistence | JSON files in ~/.config/skwad/ |

## Coding Rules

1. **Thread safety**: `agent.Manager` is mutex-protected. `agent.Coordinator` is the goroutine-safe message queue — never access it directly without going through its public API.
2. **Agent IDs are stable** across restarts — never regenerate an ID on restart.
3. **MCP messages are in-memory only** — do not persist them.
4. **Default persona UUIDs are fixed** — hardcoded in `internal/models/persona.go`.
5. **Hook-based status detection** — plugin scripts post to MCP server; `hookBridge` in daemon routes events to Pool and Manager.
6. **Structured logging** — `log/slog` throughout; `--verbose` = Debug, default = Info, `--quiet` = Error.
7. **Signal handling** — double SIGINT/SIGTERM: first graceful (StopAll), second force kill.
8. **No global mutable state** except persistence store and service singletons.
9. **Write tests for all business logic** in `internal/` packages.

## Development

```bash
make build       # Build binary
make test        # Run tests
make test-race   # Run tests with race detector
make lint        # go vet + golangci-lint
make fmt         # Format code
make help        # List all targets
```
