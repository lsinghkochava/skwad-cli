# Token Optimization (pre-eval)

**Goal:** Reduce skwad-cli LLM token usage at runtime and config level **before** running the evaluation harness. The optimized team is what the eval will baseline against ("optimize first, then baseline").

**Status:** Approved (user + Reviewer plan-review). Ready to implement.

## Decisions (locked)

- **Autopilot** (`internal/autopilot/`) — out of scope. It's dead code (never wired into runtime), spends zero tokens today.
- **Runtime prompt layers L1 (preamble) / L3 (coordination)** — out of scope. Conservative call: preserve coordination & verification discipline.
- **Baseline sequencing** — no "before" eval run. The eval has not been run yet; the optimized team *becomes* the baseline. Verification of fidelity is done via criteria-preservation tests + Reviewer diff sign-off, not eval-score comparison.
- **Persona compression** — compress **all** personas (judge + 8 review). Classifier left (already tiny).
- **Bootstrap trim** — trivialize for **both** `run` and `start`.

## Verified facts (Explorer)

The eval harness (`eval/cmd/eval-reviews/main.py`) loads three configs:

| Config | Agents | Cost | Multiplier |
|--------|--------|------|------------|
| `test_configs/skwad_review_team.json` | 8 | ~12,159 tok/run | once/run |
| `eval/config/judge_team.json` | 1 (judge) | ~2,454 tok (persona 7,907c / ~1,977 tok) | **×3 per PR** |
| `eval/config/classifier_team.json` | 1 | ~756 tok | ×1 per PR |

- **Persona (L5) is the fattest layer per agent.** Fixed overhead L1+L2 ≈ 886 tok/agent (review team).
- **L2 (team-protocol) already skipped** for solo configs (`len(teammates) > 0` gate, `prompt.go:19`).
- **L3 (coordination) already skipped for all 3 eval configs** — none set a `coordination` key (`prompt.go:25`). → solo-config coordination trim is a **no-op, dropped**.
- **Bootstrap readiness:** agents are pre-registered at spawn (`daemon.go:483`), independent of bootstrap. But `run` gates work-dispatch on a readiness signal that fires on **turn completion** (`OnTurnComplete` → `MarkAgentReady`, `daemon.go:345-364,469-471`). A prompt is required to trigger a turn (`claude -p` emits nothing until input). → **Replacing** bootstrap text with a trivial prompt is safe; **skipping** it triggers the 120s readiness timeout (`run.go:357-361`).

## Phases

### Phase 1 — Persona compression (HIGH value)
- **1a.** Compress the judge persona in `eval/config/judge_team.json` (~7,907c). Highest unit value (×3/PR).
- **1b.** Compress the 8 review-team personas in `test_configs/skwad_review_team.json` (avg ~2,432c → target ~1,200–1,400c).
- **1c.** Classifier persona — leave untouched.

**Hard constraint — compression is wording-only.** Reviewer's bright-line for what counts as a **criteria change** (bounces the diff):
- Scoring scale, ranges, weights, or thresholds altered
- Pass/fail or tie-breaking rules changed
- Number or identity of evaluation dimensions/criteria changed (set must be 1:1)
- Removal/alteration of anchor examples that calibrate a score (load-bearing)
- Ordering changed where order encodes priority/precedence

Allowed: pure rephrasing, dedup of repeated instructions, removal of non-anchoring filler.
**Extra caution** on the 6 review personas with no L4 role layer (their L5 persona is their only role definition — preserve responsibilities, not just checklist items).

### Phase 2 — Bootstrap turn trim (low risk, confirmed safe)
- Replace the roster-table instruction in `defaultBootstrapPrompt` (`start.go:22`) with a minimal prompt that still produces a turn (e.g. "Reply: ready") — applies to both `run` and `start`.
- Eliminates ~8 wasted assistant turns/run (review team) + 1 (classifier) + 3 (judge runs), each with a tool round-trip and unused roster-table output.

### Dropped
- ~~Phase 3 — solo-config L2/L3 coordination trim~~ — already optimal, no-op.

## Test strategy
- **Token-count golden test** on rendered `BuildSystemPrompt` per agent (+ raw persona char counts) — records before/after, proves reduction. (Guards "tokens went down".)
- **Criteria-preservation tripwire** — extract the enumerated criteria/dimensions per persona, snapshot pre-compression as golden, assert the set is unchanged post-compression. (Guards "intent preserved".) Must be robust enough to actually catch a dropped/merged criterion, not theater.
- **Reviewer diff sign-off** is the PRIMARY fidelity gate — item-by-item before/after diff of all 9 personas (judge rubric especially) at code review.
- Existing suite stays green: `make test` / `make test-race`.

## Commit strategy (fine-grained)
1. `test:` add token-count golden + criteria-preservation tests, capture pre-compression golden snapshots (Tester — must land before persona edits).
2. `feat:` trivialize bootstrap prompt (`start.go:22`) — independent, smallest, lowest risk.
3. `chore:` compress judge persona (`eval/config/judge_team.json`).
4. `chore:` compress review-team personas (`test_configs/skwad_review_team.json`).

Per-file/per-concern commits → any single compression is trivially revertable.

## Out of scope / parked
- Autopilot wiring + its lack of prompt caching (dormant).
- L1 preamble / managed-L3 runtime trims (conservative).
- **Latent (non-token) findings to file separately, NOT fix here:**
  - L4 role prompts only match substring "reviewer" — 6 of 8 review agents get no role layer (`prompt.go:184`).
  - Daemon defaults `d.CoordinationMode` to "managed" (`daemon.go:188`) but per-agent `a.CoordinationMode` stays "" — eval team runs managed behavior while agents aren't told the coordination protocol in-prompt.

---

## Revisions (in-flight, 2026-06-16)

### R1 — Compression reality
The ~45% persona-compression target was NOT reachable wording-only — personas are mostly load-bearing content (expertise mandates, verbatim severity scales, judge output schema + anchors). Realized: review-team −9%, judge −2%. The durable runtime win is the **bootstrap-turn elimination**, not persona text.

### R2 — Roster cut (bigger lever than text)
User cut the review team **8 → 6 agents**: removed **Dependency Reviewer** (needs external CVE/registry data a diff-only agent can't reach; security slice already covered by Security Sentinel) and **Consistency Checker** (capped at LOW/NIT, can't block; overlaps Architecture Reviewer). Kept: Performance Analyst, Bug Hunter, Architecture Reviewer, Security Sentinel, Test Analyst, **Review Coordinator** (orchestrator — not a reviewer, always kept). Cutting an agent removes its full ~6K-tok system prompt + every review turn.

### R3 — Baseline confusion (resolved)
The repo had **pre-existing uncommitted user edits** to the persona files (added Dependency Reviewer, reworked severity scales to 5-level, added a top-level `model` field, tag changes) before this work. The Reviewer's first BLOCK diffed vs `git HEAD` and mis-attributed those to the Coder. Settled on evidence: the Tester's pre-Coder criteria golden already contained them, and the tripwire (pre-Coder vs post-Coder) passed → Coder's compression was genuinely wording-only. Tripwire hardened to provably FAIL on tier-add/drop and persona add/remove/rename (`TestDiffCriteria_DetectsSetChanges`); fixed an extractor bug blind to new tier names.

### R4 — Per-agent model support (NEW code feature) + eval-validity bug
**Discovery:** the config `"model"` field is **NOT wired into the Go binary** (`TeamConfig`/`AgentConfig` have no `Model` field — dropped at unmarshal). Real model came from a single global `AppSettings.AgentTypeOptions.ClaudeOptions`, identical for all agents. The config `"model"` is read only by the Python eval harness for the reproducibility **manifest** → the manifest may record a model the runs never used.
**Change (Explorer-spec'd, ~4 edits):** add `Model` to `AgentConfig`+`TeamConfig` (team.go), `Agent` (agent.go), carry in `helpers.go createAgentsFromConfig`, prefer per-agent `--model` in `command_builder.go` (precedence: per-agent > team > global ClaudeOptions > CLI default). TDD: Tester behavior tests → Coder impl → Reviewer review.
**Model assignments:** review team — Coordinator `claude-haiku-4-5`, 5 specialists `claude-sonnet-4-6`; judge `claude-sonnet-4-6`; classifier `claude-haiku-4-5`.
**Risk noted:** Coordinator on Haiku synthesizes the final review the judge scores — possible quality confound, but reversible and exactly what the eval can validate.

### R5 — Decisions
- **Commits HELD** — all work stays in the working tree, scoped to our files only; commit later on user approval.
- Fixed pre-existing typo `"Secutiry Sentinel"` → `"Security Sentinel"`.

### Follow-ups
- ~~Python eval manifest records only top-level model~~ → **DONE.** `_read_agent_models_from_config` (main.py:120) + `record_models(per_agent=...)` (manifest.py:172-195) now record per-agent models; precedence mirrors the Go binary; back-compat preserved; 84 Python tests green.
- Confirm what model the eval runs ACTUALLY used to date (global ClaudeOptions value) vs the old `claude-sonnet-4-20250514` manifest claim. (Open — informational.)

## Status: COMPLETE & VERIFIED (uncommitted, per user hold)
All changes in the working tree, full Go suite (16 pkgs) + Python (84 tests) green, Reviewer APPROVE on: per-agent model code, persona fidelity (all 7, byte-level), manifest mirror.

### Deferred future tickets (non-blocking)
- L4 role prompts match substring "reviewer" only — most review agents get no role layer (`prompt.go:184`).
- Daemon `d.CoordinationMode="managed"` default vs per-agent `a.CoordinationMode=""` in-prompt mismatch (`daemon.go:188`).
- `parseFlag` doesn't handle `--model=value` form; ClaudeOptions only yields `--model` (`command_builder.go`).
- Manifest sentinel mismatch: top-level `"unknown"` vs per-agent `"default"` for a config declaring no models (main.py) — add a clarifying comment.

## Key learnings (ways of working)
- **Establish a clean, committed baseline BEFORE editing — or verification becomes unfalsifiable.** Starting work on a tree with pre-existing uncommitted edits caused the Reviewer to mis-attribute the user's changes to the Coder (diff vs `git HEAD` conflated both). The fix was a clean reset; the lesson is to commit/snapshot the true starting point first.
- **A guard test only counts once it's PROVEN to fail on the right things.** The criteria tripwire passing meant nothing until negative self-tests showed it fails on tier add/drop + persona add/remove/rename — and that exercise surfaced a real extractor blind-spot (new tier names). "Green" ≠ "guarding."
- **Separate the measurement instrument from the subject under test.** Judge persona (the ruler) vs review personas (the measured) needed different rigor; with no prior eval, "optimize-then-baseline" is valid — the shipped artifact becomes the baseline.
- **Compression on an eval artifact is wording-only with criteria preserved verbatim; the human item-by-item diff is the primary fidelity gate, the tripwire is the automated tripwire.** Token-count tests prove "smaller," not "faithful."
- **The biggest token lever was structural (cut whole agents, kill the unused bootstrap turn), not textual.** Question whether a component is needed before compressing its text.
- **Surface latent findings, don't fix them inline.** The dead config-`model` field (eval-validity bug), L4 substring bug, and parseFlag gaps were logged/scoped separately rather than scope-creeping the token work.
