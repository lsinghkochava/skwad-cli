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
- **Structured run logging** — every session produces a JSONL file capturing tool calls, messages, status changes, spawns, exits, and prompts for post-session analysis.

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

## Watch Mode

```bash
skwad-cli start --config team.json --watch
```

Launches a 3-panel TUI dashboard:

- **Top** — agent status table showing name, status, and current activity
- **Middle** — scrollable activity log with timestamped agent output and tool calls (bordered, with scroll indicator)
- **Bottom** — status bar with agent count and MCP server URL

Key bindings: `j/k` scroll, `PgUp/PgDn` page, `Tab` cycle agent filter, `?` help, `q` quit.

## Run Logging

Each session creates a `runlogs/<timestamp>.jsonl` file with structured events:

- **Tool calls** — tool name, arguments, result, and calling agent
- **Messages** — inter-agent messages and broadcasts with sender/recipient
- **Status changes** — agent status text and category updates
- **Lifecycle** — agent spawn (with args), exit (with code), and prompt delivery
- **Hook events** — status transitions from plugin hook scripts

Useful for debugging agent behavior, auditing coordination patterns, and post-session analysis.

## Team Configuration

```json
{
  "name": "My Team",
  "repo": "/path/to/repo",
  "prompt": "Review the latest changes",
  "agents": [
    {
      "name": "Reviewer",
      "agent_type": "claude",
      "persona_instructions": "You are a code reviewer focused on correctness.",
      "avatar": "🔍"
    },
    {
      "name": "Tester",
      "agent_type": "claude",
      "persona": "Bug Hunter",
      "prompt": "Write tests for the auth module"
    }
  ],
  "personas": [
    {
      "name": "Bug Hunter",
      "instructions": "Find correctness bugs. Report file, line, and how to trigger."
    }
  ]
}
```

**Agent fields:** `name` (required), `agent_type` (required: claude, codex, gemini, copilot, opencode, custom), `persona` (name match), `persona_instructions` (inline), `persona_id` (UUID), `avatar`, `command` (custom shell), `allowed_tools`, `prompt` (per-agent).

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

**Tools:** `register-agent`, `list-agents`, `send-message`, `check-messages`, `broadcast-message`, `set-status`, `list-repos`, `list-worktrees`, `create-worktree`, `create-agent`, `close-agent`, `display-markdown`, `view-mermaid`.

**REST endpoints:** `GET /health`, `GET /` (agent list), `POST /api/v1/agent/register`, `POST /api/v1/agent/status`, `POST /api/v1/agent/send`, `POST /api/v1/agent/broadcast`.

Agents coordinate through messages, status updates, and tool calls. All MCP activity is captured in the JSONL run log for debugging and analysis.

## Development

```bash
make build      # Build binary
make test       # Run tests
make test-race  # Run tests with race detector (recommended)
make lint       # Run linters
make help       # Show all targets
```

The race detector (`make test-race`) is recommended for development — the agent coordinator and process pool use concurrent goroutines extensively.

## Roadmap

The following enhancements are planned:

| Feature | Description |
|---------|-------------|
| Explore mode | Sandboxed read-only agents with `--permission-mode plan` |
| Output summarization | Head/tail truncation for large agent outputs |
| CI pipeline iteration | Phase-gated pipelines with `--max-iterations` and retry logic |
| Enriched system prompts | Layered team protocol with role-specific behavioral rules |
| Event-sourced run state | Append-only event log with `--resume` for long-running CI |
| Run log rotation | Automatic cleanup of old JSONL log files |

## License

MIT
