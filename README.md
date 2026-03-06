# Skwad Linux

A native Linux desktop application for running multiple AI coding agents simultaneously — each in its own embedded terminal, coordinated via a built-in MCP (Model Context Protocol) server.

This is a Go port of the [Skwad macOS app](https://skwad.ai), built for X11 and Wayland desktops.

## What It Does

- Runs multiple AI coding agents in parallel (Claude Code, Codex, Gemini CLI, GitHub Copilot, OpenCode, or custom shell commands), each in a persistent embedded terminal
- Organizes agents into named, color-coded **workspaces** with 1, 2, 3, and 4-pane split layouts
- Built-in **MCP HTTP server** (JSON-RPC 2.0) so agents can register, message each other, query worktrees, and spawn new agents programmatically
- **Git integration**: diff viewer, per-file staging, commit dialog, worktree creation, live git stats in the sidebar
- **Markdown** and **Mermaid** diagram preview panels rendered on demand via MCP tool calls
- **Fuzzy file finder**, agent personas, conversation history browser, and an autopilot service that uses an LLM to handle agent prompts automatically

## Getting Started

### Requirements

**Linux (for full functionality):**
```
sudo apt install libvte-2.91-dev libgtk-3-dev pkg-config
```

**macOS (for development/testing — VTE not available):**
```
brew install go
```

### Build

```bash
git clone https://github.com/Jared-Boschmann/skwad-linux
cd skwad-linux
make build
```

Or with `go` directly:
```bash
go build -o skwad ./cmd/skwad
./skwad
```

### Run

```bash
./skwad
```

Configuration is stored in `~/.config/skwad/`. On first launch use **Ctrl+N** (or **Cmd+N** on macOS) to create your first agent.

## UI Overview

### Workspace Bar (far left)
Circular colored badges — one per workspace. Click to switch workspaces. Right-click to rename or delete. The **+** button creates a new workspace. The **gear icon** opens Settings.

### Sidebar
Lists agents in the active workspace. Click an agent to assign it to the focused pane. Right-click for the full context menu (restart, duplicate, fork session, history, move to workspace, etc.). **+ New Agent** opens the agent creation window.

### Terminal Area
Shows agent output in one or more panes. Use the **layout buttons** in the top toolbar to switch between single, vertical split, horizontal split, 3-pane, and 4-pane grid layouts.

**Assigning agents to split panes:**
1. Switch to a split layout using the toolbar icons
2. Click a pane's header to focus it — the header turns **blue**
3. Click an agent in the sidebar — it is assigned to the blue (focused) pane

## Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| Ctrl+N | New agent |
| Ctrl+, | Settings |
| Ctrl+G | Toggle git panel |
| Ctrl+\ | Toggle sidebar |
| Ctrl+P | Fuzzy file finder |
| Ctrl+] / Ctrl+[ | Next / previous agent |
| Ctrl+1–9 | Select agent by index |
| Ctrl+Shift+] / [ | Next / previous workspace |
| Ctrl+Shift+O | Open agent folder in editor |

> On macOS, replace Ctrl with Cmd.

## Architecture

```
cmd/skwad/           Entry point
internal/
  models/            Pure data types (Agent, Workspace, Settings, Persona)
  agent/             Business logic: Manager, Coordinator, ActivityController
  mcp/               MCP HTTP server (JSON-RPC 2.0) + hook event handler
  terminal/          PTY session management (creack/pty) + Pool orchestrator
  git/               Git CLI wrapper: status, diff, stage, commit, worktrees
  persistence/       JSON file storage (~/.config/skwad/)
  search/            Fuzzy file path scorer
  ui/                Fyne v2 GUI components
  autopilot/         LLM-based autopilot (OpenAI / Anthropic / Gemini)
  notifications/     Desktop notifications via notify-send
  voice/             Push-to-talk voice input (stub)
plugin/
  claude/notify.sh   Hook script for Claude Code lifecycle events
  codex/notify.sh    Hook script for Codex lifecycle events
```

## Tech Stack

| Concern | Choice |
|---|---|
| Language | Go 1.22+ |
| GUI | [Fyne v2](https://fyne.io/) — OpenGL, X11 + Wayland |
| Terminal widget | VTE (libvte-2.91) via CGo on Linux |
| PTY sessions | [creack/pty](https://github.com/creack/pty) |
| MCP server | `net/http` stdlib, JSON-RPC 2.0 |
| File watching | [fsnotify](https://github.com/fsnotify/fsnotify) |
| Markdown | [goldmark](https://github.com/yuin/goldmark) |
| Persistence | JSON in `~/.config/skwad/` |

## MCP Server

Skwad exposes an MCP server at `http://127.0.0.1:8766/mcp` (port configurable in Settings). AI agents that support MCP can use the following tools:

| Tool | Description |
|---|---|
| `register-agent` | Register with the Skwad session |
| `list-agents` | List all registered agents |
| `send-message` | Send a message to another agent |
| `check-messages` | Read messages in your inbox |
| `broadcast-message` | Send to all registered agents |
| `list-repos` | List recently used git repos |
| `list-worktrees` | List worktrees for a repo |
| `create-worktree` | Create a new git worktree |
| `display-markdown` | Open a markdown file in the preview panel |
| `view-mermaid` | Render a Mermaid diagram |
| `create-agent` | Spawn a new agent |
| `close-agent` | Close an agent |

Hook scripts in `plugin/claude/` and `plugin/codex/` post lifecycle events to `/hook` so Skwad can track agent status (running / idle / blocked).

## Development Status

| Area | Status |
|---|---|
| Data layer (models, persistence, manager) | ✅ Complete |
| Git + file services | ✅ Complete |
| Terminal + MCP server (headless) | ✅ Complete |
| UI shell — workspaces, sidebar, split panes | ✅ Complete |
| UI visual design — dark theme, circular badges, SVG toolbar icons | ✅ Complete |
| Pane focus + agent assignment UX | ✅ Complete |
| Session history browser (Claude + Codex) | ✅ Complete |
| Fork / resume agent sessions | ✅ Complete |
| Autopilot service (OpenAI / Anthropic / Gemini) | ✅ Complete |
| Settings window (all tabs) | ✅ Complete |
| Agent personas + bench | ✅ Complete |
| File finder, git panel, markdown/mermaid panels | ✅ Complete |
| VTE native terminal embedding (Linux) | 🔄 In progress |
| Voice STT backend | Planned |
| Agent drag-to-reorder | Planned |
| Split ratio persistence on drag | Planned |

## Testing

```bash
make test
# or
go test ./...
```

All packages through Phase 3 have unit tests. The `ui` package is manually verified.

## License

MIT

## Contributing

Issues and pull requests welcome. See [`DEVPLAN.md`](DEVPLAN.md) for the build plan and coding rules before contributing.
