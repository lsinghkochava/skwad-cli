# skwad-cli

Multi-agent CLI orchestrator for AI coding agents.

Runs multiple AI agents (Claude Code, Codex, Gemini CLI, GitHub Copilot, OpenCode, or custom commands) in parallel, coordinates them via a built-in MCP server, and works headless on servers and in CI pipelines.

## Installation

```bash
go install github.com/lsinghkochava/skwad-cli/cmd/skwad-cli@latest
```

Or build from source:

```bash
git clone https://github.com/lsinghkochava/skwad-cli
cd skwad-cli
make build
```

## Quick Start

```bash
# Start a review team on your project
skwad-cli start --team review-team --set repo=. --watch

# Or use a custom config
skwad-cli start --config team.json

# From another terminal
skwad-cli status
skwad-cli send --from "Coordinator" --to "Coder" "Implement the auth module"
skwad-cli stop
```

## Architecture

- **Headless processes** — agents run as stdin/stdout JSON streaming processes (no PTY, no tmux). Each agent gets a managed subprocess with structured message framing.
- **MCP coordination** — built-in JSON-RPC 2.0 HTTP server enables agent-to-agent messaging, status broadcasting, and dynamic agent spawning.
- **TUI dashboard** — optional Bubble Tea v2 terminal UI (`--watch`) provides real-time monitoring with agent status table, scrollable activity log, and tool call visibility.
- **Event-sourced run state** — append-only event log tracks run lifecycle (spawns, exits, prompts, phases, iterations) with state replay and crash detection via `--list-runs`.
- **Agent worktree isolation** — each writing agent works in its own git worktree on an isolated branch. Changes are consolidated via `skwad merge` into a single reviewable branch. Main is never touched directly.

## Commands

| Command | Description |
|---------|-------------|
| `start` | Start daemon with agents from config or template |
| `stop` | Stop running daemon |
| `status` | Show agent states as formatted table |
| `list` | List agents with IDs |
| `send` | Send message between agents |
| `broadcast` | Broadcast message to all agents |
| `run` | One-shot mode for CI (start, prompt, wait, report, exit) |
| `report` | Format output as markdown, JSON, or GitHub PR comment |
| `convert` | Convert macOS Skwad workspace export to CLI config |
| `merge` | Consolidate agent worktree branches into a single branch |
| `clean` | Remove agent worktrees and optionally their branches |

## Watch Mode

```bash
skwad-cli start --config team.json --watch
```

Launches a 3-panel TUI dashboard:

- **Top** — agent status table showing name, status, and current activity
- **Middle** — scrollable activity log with timestamped agent output and tool calls (bordered, with scroll indicator)
- **Bottom** — status bar with agent count and MCP server URL

Key bindings: `j/k` scroll, `PgUp/PgDn` page, `Tab` cycle agent filter, `?` help, `q` quit.

## Worktree Isolation

Agents that write code work in isolated git worktrees:

```bash
# Agents auto-create worktrees on spawn (branch: skwad/<session>/<agent>)
skwad-cli start --config team.json

# After agents finish, consolidate all branches
skwad-cli merge

# Or auto-merge in CI mode
skwad-cli run --config team.json --prompt "Fix the auth bug" --auto-merge

# Clean up worktrees
skwad-cli clean --branches
```

Isolation is controlled via team config: `isolate_agents: true` (default) with per-agent `isolate: false` override for read-only agents.

## Explore Mode

Run agents in read-only mode for safe codebase analysis:

```bash
skwad-cli run --config team.json --explore --prompt "Analyze the payment service"
skwad-cli start --config team.json --explore --watch
```

Explore mode sets `--permission-mode plan` and restricts tools to read-only (Read, Glob, Grep, Agent). Can also be set per-agent via `explore_mode: true` in team config.

## Run History

Each `skwad run` session is tracked with an event log at `~/.config/skwad/runs/<runID>/`:

```bash
# List past runs with status
skwad-cli run --list-runs

# Clean up old run state
skwad-cli run --clean-runs 7d    # older than 7 days
skwad-cli run --clean-runs all   # all runs
```

Events tracked: run start/complete/fail, agent spawn/exit, prompts sent, phase transitions, iterations.

## Team Configuration

```json
{
  "name": "My Team",
  "repo": "/path/to/repo",
  "prompt": "Review the latest changes",
  "isolate_agents": true,
  "agents": [
    {
      "name": "Reviewer",
      "agent_type": "claude",
      "persona_instructions": "You are a code reviewer focused on correctness.",
      "avatar": "🔍",
      "isolate": false
    },
    {
      "name": "Coder",
      "agent_type": "claude",
      "persona": "Senior Dev"
    },
    {
      "name": "Tester",
      "agent_type": "claude",
      "prompt": "Write tests for the auth module",
      "explore_mode": true
    }
  ],
  "personas": [
    {
      "name": "Senior Dev",
      "instructions": "Write clean, tested code. Follow existing patterns."
    }
  ]
}
```

**Agent fields:** `name` (required), `agent_type` (required: claude, codex, gemini, copilot, opencode, custom), `persona` (name match), `persona_instructions` (inline), `persona_id` (UUID), `avatar`, `command` (custom shell), `allowed_tools`, `prompt` (per-agent), `explore_mode` (read-only), `isolate` (worktree isolation override).

**Team fields:** `isolate_agents` (default: true — agents work in isolated worktrees).

**Persona resolution priority:** `persona_instructions` > `persona_id` > `persona` name > team-level `personas[]` matching agent name.

## Built-in Templates

| Template | Agents | Description |
|----------|--------|-------------|
| `review-team` | 7 | Specialized code review: Performance, Consistency, Bug Hunter, Architecture, Security, Test Analyst, Coordinator |
| `dev-team` | 3 | Explorer, Coder, Tester |

```bash
skwad-cli run --team review-team --set repo=/path --prompt "Review PR #42"
```

Variable substitution: `${repo}`, `${prompt}`, custom via `--set key=value`.

## macOS Export Conversion

```bash
# Convert explicitly
skwad-cli convert --input workspace-export.json --output team.json

# Or use directly (auto-detected)
skwad-cli start --config workspace-export.json
```

Companions are excluded. Persona instructions are inlined from the export's persona list.

## Proposed Use Cases

| Use Case | Command | Description |
|----------|---------|-------------|
| CI PR Review | `skwad-cli run --team review-team` | Multi-perspective review on every PR |
| Migration at Scale | `skwad-cli run` in a loop | Batch codebase changes across repos |
| Remote Teams | `skwad-cli start` over SSH | Full agent team on any machine |
| Nightly Audits | `skwad-cli run` via cron | Wake up to actionable findings |
| Interactive Dev | `skwad-cli start --watch` | Real-time collaboration with agents |

### Automated PR Review Teams in CI

```yaml
# .github/workflows/pr-review.yml
on: pull_request

jobs:
  skwad-review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: AI Code Review Team
        run: |
          skwad-cli run \
            --team review-team \
            --set repo=. \
            --set prompt="Review this PR for architecture, security, and test coverage" \
            --timeout 10m \
            --output-format json | \
          skwad-cli report --format github-pr-comment --pr ${{ github.event.pull_request.number }}
```

Exit codes: `0` = all agents exited cleanly, `1` = timeout or infra error, `2` = any agent exited non-zero.

### Remote Machine Execution

```bash
ssh dev-server
skwad-cli start --config team.json --watch
# From another terminal
skwad-cli send --to Explorer "Map out the payment service dependencies"
skwad-cli status
```

### Migration at Scale

```bash
for repo in $(cat repos.txt); do
  skwad-cli run \
    --config .skwad/migration-team.json \
    --set repo=$repo \
    --set prompt="Migrate from OpenCensus to OpenTelemetry" \
    --timeout 15m
done
```

### Nightly Codebase Audits

```bash
# crontab: run every night at 2am
0 2 * * * skwad-cli run \
  --config .skwad/audit-team.json \
  --set repo=/opt/main-app \
  --set prompt="Full security and quality audit of recent changes" \
  --timeout 30m \
  --output-format json | \
  skwad-cli report --format markdown > /reports/nightly-$(date +\%F).md
```

### Interactive Team Sessions

```bash
# Start a team and interact via CLI
skwad-cli start --config team.json --watch

# In another terminal — send tasks
skwad-cli send --to Coder "Implement the auth middleware"
skwad-cli send --to Tester "Write tests for the auth module"
skwad-cli broadcast "Status update please"
skwad-cli status
```

## MCP Server

Built-in MCP server on port 8766 (configurable via `--port`). Agents coordinate via JSON-RPC 2.0 at `/mcp`. Compatible with the Swift Skwad app's plugin scripts.

**Tools:** `register-agent`, `list-agents`, `send-message`, `check-messages`, `broadcast-message`, `set-status`, `list-repos`, `list-worktrees`, `create-worktree`, `create-agent`, `close-agent`, `display-markdown`, `view-mermaid`, `merge-branches`.

**REST endpoints:** `GET /health`, `GET /` (agent list), `POST /api/v1/agent/register`, `POST /api/v1/agent/status`, `POST /api/v1/agent/send`, `POST /api/v1/agent/broadcast`.

Agents coordinate through messages, status updates, and tool calls. Run lifecycle events are captured in the event log for state tracking and analysis.

## Development

```bash
make build      # Build binary
make test       # Run tests
make test-race  # Run tests with race detector (recommended)
make lint       # Run linters
make help       # Show all targets
```

The race detector (`make test-race`) is recommended for development — the agent coordinator and process pool use concurrent goroutines extensively.

## Features

| Feature | Status | Description |
|---------|--------|-------------|
| Explore mode | ✅ | Read-only agents with `--explore` flag and `--permission-mode plan` |
| Output summarization | ✅ | Head/tail truncation for large agent outputs in reports |
| CI pipeline iteration | ✅ | `--max-iterations` with retryable exit code classification |
| Enriched system prompts | ✅ | 5-layer prompt: preamble, team protocol, role instructions, persona, worktree context |
| Event-sourced run state | ✅ | Append-only event log with `--list-runs` and `--clean-runs` |
| Worktree isolation | ✅ | Per-agent git worktrees with `skwad merge` consolidation |
| Run resume | Planned | `--resume` flag for crash recovery of long-running CI runs |
| Autonomous coordination | Planned | Task management with managed/autonomous modes (Phase 6) |

## License

MIT
