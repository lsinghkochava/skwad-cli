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

## CI Usage

```yaml
# GitHub Actions example
- name: AI Code Review
  run: |
    skwad-cli run \
      --team review-team \
      --set repo=. \
      --prompt "Review this PR" \
      --timeout 10m \
      --output-format json | \
    skwad-cli report --format github-pr-comment --pr ${{ github.event.pull_request.number }}
```

Exit codes: `0` = all agents exited cleanly, `1` = timeout or infra error, `2` = any agent exited non-zero.

## MCP Server

Built-in MCP server on port 8766 (configurable via `--port`). Agents coordinate via JSON-RPC 2.0 at `/mcp`. Compatible with the Swift Skwad app's plugin scripts.

**Tools:** `register-agent`, `list-agents`, `send-message`, `check-messages`, `broadcast-message`, `set-status`, `list-repos`, `list-worktrees`, `create-worktree`, `create-agent`, `close-agent`, `display-markdown`, `view-mermaid`.

**REST endpoints:** `GET /health`, `GET /` (agent list), `POST /api/v1/agent/register`, `POST /api/v1/agent/status`, `POST /api/v1/agent/send`, `POST /api/v1/agent/broadcast`.

## Development

```bash
make build      # Build binary
make test       # Run tests
make test-race  # Run tests with race detector
make lint       # Run linters
make help       # Show all targets
```

## License

MIT
