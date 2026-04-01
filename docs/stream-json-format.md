# Claude CLI Stream-JSON Protocol Format

Reference documentation for the Claude CLI `--output-format stream-json` protocol, verified by real testing.

## Required Flags

| Flag | Required | Notes |
|------|----------|-------|
| `-p` / `--print` | Yes | Non-interactive mode |
| `--output-format stream-json` | Yes | JSON stream on stdout |
| `--verbose` | Yes | Required when using stream-json output |
| `--input-format stream-json` | Optional | Structured JSON input on stdin |
| `--permission-mode auto` | Recommended | Avoids permission prompts |

`--include-hook-events` has no visible effect — hook events don't appear in the stream.

## Input Format (stdin)

```json
{"type":"user","message":{"role":"user","content":"your prompt here"}}
```

The nested `message` object is required. A flat `{"type":"user","content":"..."}` format does NOT work — it throws `TypeError: undefined is not an object (evaluating '_.message.role')`.

## Output Format (stdout)

Newline-delimited JSON. Each line is a complete JSON object. Five message types are emitted:

### 1. `system` (subtype: `init`) — Session initialization

First message emitted. Contains session metadata.

```json
{
  "type": "system",
  "subtype": "init",
  "cwd": "/path/to/working/dir",
  "session_id": "abc123",
  "tools": ["Read", "Write", "Bash", "..."],
  "mcp_servers": [],
  "model": "claude-sonnet-4-20250514",
  "permissionMode": "auto",
  "claude_code_version": "1.0.0",
  "uuid": "uuid-v4",
  "fast_mode_state": "..."
}
```

### 2. `assistant` (tool_use content) — Agent calling a tool

```json
{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "content": [
      {
        "type": "tool_use",
        "id": "toolu_abc123",
        "name": "Read",
        "input": {"file_path": "/some/file.go"}
      }
    ]
  },
  "parent_tool_use_id": null,
  "session_id": "abc123",
  "uuid": "uuid-v4"
}
```

### 3. `user` (tool_result content) — Tool result echoed back

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {
        "tool_use_id": "toolu_abc123",
        "type": "tool_result",
        "content": "file contents here..."
      }
    ]
  },
  "session_id": "abc123",
  "uuid": "uuid-v4",
  "timestamp": "2026-04-01T00:00:00Z",
  "tool_use_result": {}
}
```

### 4. `assistant` (text content) — Agent text response

```json
{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "content": [
      {
        "type": "text",
        "text": "Here is my response..."
      }
    ]
  },
  "session_id": "abc123",
  "uuid": "uuid-v4"
}
```

### 5. `result` (subtype: `success`) — Turn complete

Final message after all turns complete.

```json
{
  "type": "result",
  "subtype": "success",
  "is_error": false,
  "duration_ms": 15000,
  "duration_api_ms": 12000,
  "num_turns": 3,
  "result": "final text output",
  "stop_reason": "end_turn",
  "session_id": "abc123",
  "total_cost_usd": 0.05,
  "usage": {},
  "modelUsage": {},
  "permission_denials": [],
  "uuid": "uuid-v4"
}
```

## Multi-turn Support

Multiple JSON messages on stdin are processed sequentially in the same process:

- Each message has context from all previous messages in the session
- Process exits after stdin EOF with a single `result` message covering all turns
- `num_turns` in `result` reflects total turns (user messages + tool calls)

## Status Detection Strategy

Since hook events don't appear in the stream, derive agent status from message types:

| Event | Status |
|-------|--------|
| Receive `system` init | Agent started (Ready) |
| Receive `assistant` message | Agent actively working (Running) |
| Receive `result` message | Turn complete (Idle), ready for next prompt |
| Process exits | Agent stopped |

## Key Discoveries

1. `--verbose` is **required** — without it, `stream-json` output fails with error
2. Input format requires nested `message` object — not flat `content` field
3. `--include-hook-events` has no visible effect — no separate hook event messages appear in stream
4. Tool use/results are NOT separate stream events — they're nested inside `assistant`/`user` message content arrays
5. `session_id` is available in `system.init` and all subsequent messages
