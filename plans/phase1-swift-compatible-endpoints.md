# Phase 1: Swift-Compatible Hook Endpoints + `set-status` Tool

## Goal

Align the Go MCP server with the Swift Skwad app's HTTP API so the same plugin scripts work with both servers. Add the missing `set-status` MCP tool.

## Context

The Swift MCP server (Hummingbird, port 8766) has 6 HTTP endpoints and 13 MCP tools. The Go server currently has 2 endpoints (`POST /mcp`, `POST /hook`) and 12 tools. Phase 1 bridges the gap.

### Key Differences (Go vs Swift)

| Concern | Go (current) | Swift (target) |
|---------|-------------|----------------|
| Health check | None | `GET /health` → `"OK"` |
| Debug endpoint | None | `GET /` → JSON agent array |
| Agent registration | `POST /hook` (camelCase `agentId`) | `POST /api/v1/agent/register` (snake_case `agent_id`) |
| Status updates | `POST /hook` (unified) | `POST /api/v1/agent/status` (snake_case `agent_id`) |
| `set-status` tool | Missing | Tool #13 — `{agentId, status, category}` |
| `AgentInfo` struct | `{ID, Name, Folder, Status}` | `{id, name, folder, status, isRegistered}` |
| Plugin scripts | `notify.sh` → `/hook` | `startup.sh` + `activity.sh` → `/api/v1/*` |

### Design Decisions

- **Keep existing `/hook` endpoint** alongside new `/api/v1/*` routes (backward compat)
- **Accept both `agent_id` (snake) and `agentId` (camel)** in new endpoints (tolerant reader)
- **Extract shared `dispatchStatus()` function** — both `/hook` and `/api/v1/agent/status` call into it
- **New request/response structs go in `types.go`**, handler logic stays in `hooks.go`
- **`set-status` uses 3 params** `{agentId, status, category}` — matches live deployed tool schema
- **Plugin scripts use `jq`** for JSON parsing (lighter than `python3`)
- **SSE `GET /mcp` endpoint deferred** — not needed for Phase 1

---

## Action Items

### 1.1 — Expand Agent model + AgentInfo + AgentStatusUpdater

**Goal**: Add `statusText` and `statusCategory` fields to the Agent model, expand `AgentInfo` with `IsRegistered`, and add `SetStatusText` to the `AgentStatusUpdater` interface.

**Files**:
- `internal/models/agent.go` — add `StatusText string` and `StatusCategory string` fields to `Agent`
- `internal/agent/coordinator.go` — expand `AgentInfo` with `IsRegistered bool`; add `SetStatusText(agentID, status, category)` method
- `internal/mcp/hooks.go` — add `SetStatusText(agentID uuid.UUID, status, category string)` to `AgentStatusUpdater` interface

**Commit**: `feat: add statusText and category fields to agent model`

---

### 1.2 — Add `GET /health` endpoint

**Goal**: Return `200 OK` with plain text body `"OK"`. Plugin scripts check this before registering.

**Files**:
- `internal/mcp/server.go` — register `GET /health` route

**Commit**: `feat: add health endpoint`

---

### 1.3 — Add Swift-format request/response types

**Goal**: Define Go structs matching the Swift API payload formats.

**Types to add in `internal/mcp/types.go`**:
```go
// POST /api/v1/agent/register request
RegisterHookRequest {
    AgentID   string                 `json:"agent_id"`
    Agent     string                 `json:"agent"`      // "claude" | "codex"
    Source    string                 `json:"source"`     // "startup" | "resume"
    SessionID string                `json:"session_id"`
    Payload   map[string]interface{} `json:"payload"`    // {transcript_path, cwd, model, session_id}
}

// POST /api/v1/agent/register response
RegisterHookResponse {
    Success            bool        `json:"success"`
    Message            string      `json:"message"`
    UnreadMessageCount int         `json:"unreadMessageCount"`
    SkwadMembers       []AgentInfo `json:"skwadMembers"`
}

// POST /api/v1/agent/status request
StatusHookRequest {
    AgentID string                 `json:"agent_id"`
    Agent   string                 `json:"agent"`       // "claude" | "codex"
    Hook    string                 `json:"hook"`        // "Stop", "PreToolUse", "notify", etc.
    Status  string                 `json:"status"`      // "running", "idle", "input"
    Payload map[string]interface{} `json:"payload"`
}
```

**Files**:
- `internal/mcp/types.go` — add structs above + `ToolSetStatus` constant

**Commit**: `feat: add swift-compatible request/response types`

---

### 1.4 — Add `POST /api/v1/agent/register` endpoint

**Goal**: Handle Swift-format agent registration. Plugin `startup.sh` hits this on SessionStart.

**Behavior**:
- Parse `RegisterHookRequest` (accept both `agent_id` and `agentId`)
- Validate `agent_id` is a valid UUID, return `400` if not
- Find agent by UUID, return `404 "Agent not found"` if missing
- Dispatch by `agent` field (only `"claude"` handled for now; return `400 "Unknown agent type"` otherwise)
- Handle `source` field: `"startup"` = full registration (set session ID, metadata, mark running); `"resume"` = only update session ID (skip if fork)
- Extract metadata from `payload` subobject: `transcript_path`, `cwd`, `model`, `session_id`
- Return `RegisterHookResponse` with `skwadMembers` list and `unreadMessageCount`

**Files**:
- `internal/mcp/server.go` — register route, parse request, call handler
- `internal/mcp/hooks.go` — `handleRegister()` method with source/resume/fork logic

**Commit**: `feat: add swift-compatible register endpoint`

---

### 1.5 — Refactor status dispatch + Add `POST /api/v1/agent/status` endpoint

**Goal**: Extract shared dispatch logic, then add the Swift-format status endpoint.

**Refactor**: Extract `dispatchStatus(agentID uuid.UUID, status string, metadata map[string]string)` from existing `hookHandler.dispatch()`. Both `/hook` and `/api/v1/agent/status` call into it.

**Behavior**:
- Parse `StatusHookRequest` (accept both `agent_id` and `agentId`)
- Validate `agent_id`, return `400` if invalid
- Dispatch by `agent` field:
  - `"claude"`: map `status` field → Running/Idle/Blocked; extract metadata from `payload`; trigger autopilot on `Stop` hook
  - `"codex"`: only process `agent-turn-complete` events; store `thread-id` as session ID; set idle
- Return `200 "OK"`

**Files**:
- `internal/mcp/hooks.go` — extract `dispatchStatus()`, add `handleStatus()`, refactor existing `dispatch()` to use shared function
- `internal/mcp/server.go` — register route

**Commit**: `feat: add swift-compatible status endpoint with shared dispatch`

---

### 1.6 — Add `GET /` debug endpoint

**Goal**: Return JSON array of all agents with state, metadata, and registration status.

**Response format**:
```json
[{
  "agent_id": "uuid",
  "name": "Agent Name",
  "folder": "/path/to/repo",
  "state": "running",
  "status": "Implementing auth module",
  "registered": true,
  "agent_type": "claude",
  "session_id": "session-uuid",
  "metadata": {"cwd": "...", "model": "..."}
}]
```

**Files**:
- `internal/mcp/server.go` — register route, query agent manager, serialize response

**Commit**: `feat: add debug agent list endpoint`

---

### 1.7 — Add `set-status` MCP tool (#13)

**Goal**: Allow agents to set a human-readable status text visible to other agents.

**Tool definition**:
```json
{
  "name": "set-status",
  "description": "MANDATORY: Set your status so other agents know what you are doing. Call before starting any task, after completing it, and when changing direction. Keep it short and specific.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "agentId": {"type": "string", "description": "Your agent ID"},
      "status": {"type": "string", "description": "Short status text. Use empty string to clear."},
      "category": {"type": "string", "description": "Category of action. Predefined: code, test, explore, review, plan, delegate, coordinate. Custom also accepted."}
    },
    "required": ["agentId", "status"]
  }
}
```

**Implementation**: Validate params → find agent by name or ID → call `SetStatusText(agentID, status, category)` → return `"Status updated"`

**Files**:
- `internal/mcp/tools.go` — add tool definition + handler
- `internal/mcp/types.go` — add `ToolSetStatus = "set-status"` constant

**Commit**: `feat: add set-status MCP tool`

---

### 1.8 — Update plugin scripts to Swift format

**Goal**: Rewrite plugin scripts to use `/api/v1/*` endpoints and match Swift's script structure.

**Scripts**:
- `plugin/claude/scripts/startup.sh` — check `/health`, post to `/api/v1/agent/register`, parse response for `skwadMembers`, return `additionalContext`
- `plugin/claude/scripts/activity.sh` — post to `/api/v1/agent/status` (fire-and-forget)
- `plugin/claude/scripts/shutdown.sh` — SessionEnd cleanup
- `plugin/codex/scripts/notify.sh` — post to `/api/v1/agent/status` on `agent-turn-complete`
- All scripts use `jq` for JSON parsing

**Files**:
- `plugin/claude/scripts/startup.sh` (new)
- `plugin/claude/scripts/activity.sh` (new)
- `plugin/claude/scripts/shutdown.sh` (new)
- `plugin/codex/scripts/notify.sh` (rewrite)
- Remove old `plugin/claude/notify.sh` and `plugin/codex/notify.sh`

**Commit**: `feat: update plugin scripts to swift format`

---

### 1.9 — Integration tests for all new endpoints + tool

**Goal**: Full test coverage for Phase 1 additions.

**Tests to add**:
- `TestHealth` — GET /health returns 200 "OK"
- `TestDebugEndpoint` — GET / returns JSON agent array with correct fields
- `TestRegisterEndpoint` — POST /api/v1/agent/register with valid/invalid payloads; startup vs resume source
- `TestStatusEndpoint` — POST /api/v1/agent/status for claude and codex agents; verify status dispatch
- `TestSetStatusTool` — set-status via JSON-RPC; verify statusText and category stored
- `TestRegisterEndpointSnakeAndCamel` — both `agent_id` and `agentId` accepted
- Update `newTestServer` helper to register all new routes

**Files**:
- `internal/mcp/integration_test.go`

**Commit**: `test: swift-compatible endpoints and set-status tool`

---

## Milestone

Go MCP server is API-compatible with Swift. Same plugin scripts work with both servers. `set-status` tool available to agents.

## Status

- [x] 1.1 — Expand Agent model + AgentInfo + AgentStatusUpdater
- [x] 1.2 — Add `GET /health` endpoint
- [x] 1.3 — Add Swift-format request/response types
- [x] 1.4 — Add `POST /api/v1/agent/register` endpoint
- [x] 1.5 — Refactor status dispatch + Add `POST /api/v1/agent/status` endpoint
- [x] 1.6 — Add `GET /` debug endpoint
- [x] 1.7 — Add `set-status` MCP tool (#13)
- [x] 1.8 — Update plugin scripts to Swift format
- [x] 1.9 — Integration tests for all new endpoints + tool

## Key Learnings

- **Tolerant reader pattern pays off**: Accepting both `agent_id` and `agentId` via raw JSON map parsing avoids breaking either convention. Worth the slight complexity over strict struct deserialization.
- **Shared dispatch extraction before adding new callers**: Refactoring `dispatchStatus()` out of the existing `/hook` handler *before* adding `/api/v1/agent/status` prevented logic divergence. Always refactor first, then extend.
- **`AgentInfoResponse` avoids import cycles**: Using a local response struct in the `mcp` package instead of importing `agent.AgentInfo` directly keeps package boundaries clean in Go.
- **Review caught real gaps**: The plan review identified `category` param (3 vs 2 params), `AgentInfo.IsRegistered` field, and resume/fork logic — all would have caused issues if missed. Plan reviews are worth the time on multi-file changes.
- **Agent existence check matters for tool trust**: Tools that silently succeed on invalid input erode agent confidence. Always validate before reporting success.
