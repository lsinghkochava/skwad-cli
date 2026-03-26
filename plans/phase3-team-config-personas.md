# Phase 3: Team Configuration, Personas & Workflows

## Goal

Wire persona injection so agents actually get their instructions. Expand the team config schema. Support macOS workspace export import. Add built-in templates and report generation.

## Context

Live testing in Phase 2 revealed that agents spawn without persona instructions — the `Persona` field in `AgentConfig` is never resolved or passed to the command builder. This is the #1 fix. Additionally, the macOS Skwad app exports workspace JSON with persona definitions that our CLI should accept.

## Design Decisions

- **Transient Persona for inline instructions** — when `persona_instructions` is set in config, create a temporary `models.Persona` with a random UUID and register it with the Manager before spawning. Reuses existing command builder plumbing.
- **Separate `LoadOrConvert`** — `LoadTeamConfig` loads team configs, `ConvertMacOSExport` converts macOS exports, `LoadOrConvert` tries team config first then auto-detects macOS format. Clean function contracts.
- **JSON for templates** — no YAML dependency. Templates embedded via `//go:embed`.
- **`${var}` substitution** — shell-conventional, avoids Go template syntax confusion. Simple `strings.ReplaceAll` per `--set key=value`.
- **`skwad report` is a separate command** — different lifecycle from `run`. Operates on collected output after completion.
- **`wait` and `init` deferred to Phase 4** — not on the critical path.

---

## Action Items

### 3.1 — Wire persona injection when spawning

**Goal**: When a team config specifies a persona (by name, UUID, or inline instructions), the spawned agent actually gets those instructions.

**Resolution order** (in `createAgentsFromConfig`):
1. `persona_instructions` (inline text) → create transient `models.Persona` with `uuid.New()`, register with Manager
2. `persona_id` (UUID string) → look up in default personas + persistence store
3. `persona` (name string) → match by name (case-insensitive) against default personas + store
4. None → no persona, agent spawns generic

**Chain**: `AgentConfig.Persona*` → `models.Agent.PersonaID` → `Pool.Spawn` → `command_builder.Build` → `--append-system-prompt "instructions"`

**Files**:
- `internal/cli/helpers.go` — resolve persona, set `Agent.PersonaID`, register transient personas with Manager
- `internal/models/persona.go` — add `FindPersonaByName(name string) *Persona` helper if not present

**Commit**: `feat: wire persona injection when spawning agents`

---

### 3.2 — Expand team config schema

**Goal**: Support inline personas, team-level prompts, and macOS export fields.

**Add to `AgentConfig`**:
```go
PersonaInstructions string `json:"persona_instructions,omitempty"` // inline instructions (highest priority)
PersonaID          string `json:"persona_id,omitempty"`           // UUID reference
Avatar             string `json:"avatar,omitempty"`               // emoji or text
```

**Add to `TeamConfig`**:
```go
Prompt   string    `json:"prompt,omitempty"`   // team-level default prompt
Personas []Persona `json:"personas,omitempty"` // inline persona definitions for self-contained configs
```

Where `Persona` mirrors `models.Persona` fields: `{id, name, instructions}`.

**Validation updates**:
- `persona_id` if provided must be valid UUID
- `persona_instructions` if provided must be non-empty
- Inline `personas[]` names must be unique

**Files**:
- `internal/config/team.go`
- `internal/config/team_test.go`

**Commit**: `feat: expand team config with inline personas and team prompt`

---

### 3.3 — `skwad convert` command

**Goal**: Convert macOS Skwad workspace export → skwad-cli team config.

**Input format** (macOS export):
```json
{
  "agents": [{"name": "...", "agentType": "claude", "personaId": "UUID", ...}],
  "personas": [{"id": "UUID", "name": "...", "instructions": "...", ...}],
  "workspace": {"name": "...", "colorHex": "...", ...},
  "formatVersion": 1,
  "appVersion": "1.8.0"
}
```

**Conversion logic**:
- Map `agents[].agentType` → `agent_type`
- Map `agents[].personaId` → find in `personas[]` → inline as `persona_instructions`
- Map `workspace.name` → team `name`
- Use first agent's `folder` → team `repo`
- Strip: `isCompanion`, `createdBy`, workspace metadata (colors, layout, splitRatio)

**`LoadOrConvert(path)`** — tries `LoadTeamConfig` first. If JSON has `formatVersion` or `appVersion` keys, calls `ConvertMacOSExport` then validates result.

**CLI**: `skwad convert --input export.json --output team.json` (explicit conversion)
**Auto**: `skwad start --config export.json` works transparently via `LoadOrConvert`

**Files**:
- `internal/config/convert.go` (new)
- `internal/config/convert_test.go` (new)
- `internal/cli/convert.go` (new command)
- `internal/config/team.go` — add `LoadOrConvert`

**Commit**: `feat: add macOS workspace export conversion`

---

### 3.4 — Built-in templates with variable substitution

**Goal**: Ship embedded team templates that users can invoke with `--team`.

**Templates** (embedded via `//go:embed`):
- `review-team.json` — 7 agents with full inline persona instructions (Performance Analyst, Consistency Checker, Bug Hunter, Architecture Reviewer, Security Sentinel, Test Analyst, Review Coordinator). Uses `${repo}` and `${prompt}` variables.
- `dev-team.json` — 3 agents (Explorer, Coder, Tester) for development workflows. Uses `${repo}` and `${prompt}`.

**Variable substitution**: `${key}` replaced via `strings.ReplaceAll` for each `--set key=value` flag.

**CLI**:
```
skwad run --team review-team --set repo=/path/to/project --prompt "Review PR #123"
skwad start --team dev-team --set repo=. --watch
```

`--team` is mutually exclusive with `--config`. Team name resolves to embedded template, applies `--set` substitutions, then proceeds as normal config.

**Files**:
- `internal/config/templates/review-team.json` (new, embedded)
- `internal/config/templates/dev-team.json` (new, embedded)
- `internal/config/template.go` (new — `LoadTemplate(name, vars)`, `ListTemplates()`)
- `internal/cli/root.go` — add `--team` and `--set` flags
- `internal/cli/start.go` + `internal/cli/run.go` — resolve `--team` via template loader

**Commit**: `feat: add built-in team templates with variable substitution`

---

### 3.5 — Per-agent prompts in `skwad run`

**Goal**: Each agent can receive a different prompt. Team-level prompt is the fallback.

**Priority**: `AgentConfig.Prompt` > `--prompt`/`--prompt-file` flag > `TeamConfig.Prompt` > no prompt

**Files**:
- `internal/cli/run.go` — update prompt sending loop to check per-agent prompt first

**Commit**: `feat: wire per-agent prompts in skwad run`

---

### 3.6 — `skwad report` command

**Goal**: Format agent output into different report formats, including GitHub PR comments.

**Input**: JSON output from `skwad run --output-format json` via `--input` flag or stdin pipe.

**Formats**:
- `markdown` — formatted markdown with agent sections (default)
- `json` — passthrough/restructure
- `github-pr-comment` — uses `gh pr comment` to post summary. Requires `--pr` flag (PR number or URL). Before posting: queries existing bot comments via `gh api`, minimizes outdated ones.

**CLI**:
```
skwad run --config x --output-format json | skwad report --format github-pr-comment --pr 123
skwad report --input results.json --format markdown
```

**Files**:
- `internal/report/report.go` (new — format logic)
- `internal/report/github.go` (new — `gh` CLI integration for PR comments)
- `internal/cli/report.go` (new command)

**Commit**: `feat: implement skwad report with github pr comment support`

---

### 3.7 — Tests

**Tests to write**:
- Persona injection: config with name → correct `--append-system-prompt` in built command
- Persona injection: inline `persona_instructions` → transient persona created and used
- Config expansion: team-level prompt, inline personas, persona_id validation
- Convert: macOS export → team config (with persona inlining, field mapping, metadata stripping)
- Convert: auto-detection in `LoadOrConvert`
- Templates: load by name, variable substitution, unknown template error
- Per-agent prompts: agent prompt > global prompt > team prompt priority
- Report: markdown format, JSON format, `gh` CLI invocation (mocked)

**Files**:
- `internal/config/convert_test.go`
- `internal/config/template_test.go`
- `internal/cli/helpers_test.go` (persona resolution tests)
- `internal/report/report_test.go`

**Commit**: `test: phase 3 tests`

---

## Dependency Graph

```
3.1 (persona injection) ← no deps, START HERE
  ↓
3.2 (expand config) ← depends on 3.1
  ↓
3.3 (convert) ← depends on 3.2
3.4 (templates) ← depends on 3.2
3.5 (per-agent prompts) ← depends on 3.2
  ↓
3.6 (report) ← independent, can parallel with 3.3-3.5
  ↓
3.7 (tests) ← after all above
```

## Milestone

Persona-aware agents. macOS workspace export import. Built-in review-team template. `skwad run --team review-team` works end-to-end. `skwad report` posts PR comments via `gh`.

## Status

- [x] 3.1 — Wire persona injection when spawning
- [x] 3.2 — Expand team config schema
- [x] 3.3 — `skwad convert` command
- [x] 3.4 — Built-in templates with variable substitution
- [x] 3.5 — Per-agent prompts in `skwad run`
- [x] 3.6 — `skwad report` command
- [x] 3.7 — Tests

## Key Learnings

- **Transient personas should be in-memory only** — persisting session-scoped personas to disk causes accumulation across runs. `Manager.RegisterTransientPersona()` keeps them session-scoped.
- **macOS export format auto-detection via `LoadOrConvert`** — separate functions with clear contracts (`LoadTeamConfig`, `ConvertMacOSExport`, `LoadOrConvert`) are cleaner than auto-detection inside a single function.
- **`${var}` over `{{var}}` for template substitution** — avoids Go template syntax confusion. Simple `strings.ReplaceAll` is sufficient.
- **Embedding real user personas in templates** — the review-team template ships with the actual persona instructions from the user's macOS export, making it immediately useful out of the box.
