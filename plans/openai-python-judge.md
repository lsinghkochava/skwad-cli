# Plan: Python-based OpenAI Judge (Route B) — APPROVED (Reviewer ✅ + user sign-off 2026-06-22)

## Goal

Replace the eval judge with an in-process **Python OpenAI (GPT-5.1) judge** that bypasses skwad
entirely, while **retaining 100% of the checks the current judge performs**. The new judge MUST
verify every claim against the real repo via a forced tool-use loop before producing any verdict —
it must NOT degrade into "read the two reviews and rank them."

## Locked decisions (from user)

1. **PR checkout: FIX** — check out the PR head SHA before verification (today it verifies against
   default-branch HEAD; `commit_sha` is `""` everywhere). This is a validity fix.
2. **Model: GPT-5.1** (frontier) — strongest tool-use + structured-output discipline.
3. **Strategy: OpenAI-only** — wholesale swap; the Claude judge path is retired. New run is a
   fresh, more-valid baseline (NOT comparable to the old N=7 — by design).

## Non-negotiable constraint

The verification semantics survive 1:1. Only the *plumbing* changes from
"spawned-agent + skwad event log" to "in-process OpenAI tool loop + tool-call transcript."
The **canary injections** (fabricated claims the judge must catch) are the proof the loop is honest
and MUST pass under Route B.

---

## Acceptance criteria — the parity checklist (from Explorer inventory)

Every item below must hold in the new judge. Tester writes a test per row that proves it still fires.

### Verification & anti-confabulation spine (the rewrite surface)
- [ ] **#1 Forced verification loop** — Read/Grep/Glob exposed as OpenAI tools scoped to the clone;
      **single agentic loop** (`tool_choice="auto"`, interleaved) with structured-output verdict forced
      only at the end + evidence-binding (see RESOLVED design decisions #1/#2). Mirrors the old
      self-driven agent; system prompt forbids scoring any claim not backed by an in-transcript tool result.
- [ ] **#2 Tool-call counting** — count EMITTED `tool_calls[]` entries (success OR error) across all
      assistant messages from the OpenAI transcript (replaces event-log `tool_call` parsing).
- [ ] **#3 Confabulation rule** — if `claims_verified > 0` and
      `len(tool_calls) < max(1, ceil(claims_verified/5))` → reject run (`ConfabulationDetected`).
      Logic + `_sum_verified_in_output` carry over verbatim; only the `tool_calls` input changes.
- [ ] **#4 "Verification actually ran" gate** — replaces the MCP-availability gate: fail/retry if
      the tool loop made zero tool calls when claims exist (never silently score).
- [ ] **#7 Rate-limit handling** — OpenAI SDK `RateLimitError`/429 → existing backoff-and-retry.
- [ ] **#8 Trace-divergence warning** — warn if declared `tools_used` vs observed diverge >20%.
- [ ] **#9 Disallowed-tool** — enforce by only exposing Read/Grep/Glob as tools.
- [ ] **#10 `tool_calls_observed` back-fill** into each review's verification_summary.
- [ ] **#31 Tool sandboxing** — Read/Grep/Glob implementations hard-scoped to `clone_path`
      (prevent path traversal / reads outside the repo).

### Dropped (skwad-specific, concern disappears under Route B) — confirm each is truly moot
- [ ] **#5 stderr metadata contract** (Run ID/Event log/Agent) — no subprocess; drop.
- [ ] **#6 output-freshness guard** — response in-memory; staleness impossible; drop.

### Carry over UNCHANGED (pure Python — lift & shift, regression-test only)
- [ ] **#11–#14** Structural validation (all 4 bucket fields per count-criterion; findings⇒claim_trace).
- [ ] **#11b ORDERING (M3)** — structural validation MUST run BEFORE the confab check. A malformed
      schema silently defaults `verified_findings` to 0 and bypasses confabulation. Explicit test:
      malformed-schema input → `StructuralInvalidRun` before the confab path runs.
- [ ] **#15 A/B counterbalancing** (run1 A=skwad, run2 A=ci, run3 seeded `random.Random(seed)`).
- [ ] **#16 Paired judging** (judge sees BOTH reviews as A/B every run).
- [ ] **#17 Unswap** A/B → named systems.
- [ ] **#18 Median vote** (`statistics.median_low`), sum buckets, total = sum of 7.
- [ ] **#19 Novel-findings justifications** kept to voted-count.
- [ ] **#20 Verification-summary aggregation** (sum counts, avg rate, sum tool_calls).
- [ ] **#21 Finalize** (raise if all runs failed; persist voted.json).
- [ ] **#23 Per-task failure isolation** (run_single_judge_task never raises).
- [ ] **#24 Orchestrator crash isolation** (PR finalizes on remaining runs, e.g. 2/3).
- [ ] **#25 Pilot rejection counters** (exception type → counter).
- [ ] **#27 System prompt = `persona_instructions` VERBATIM** from judge_team.json (no paraphrase).
      **DECISION (user-approved, 2026-06-22): verbatim + ADDITIVE evidence-binding addendum.** The
      persona's `evidence` field is free-text, but the R1 misquote check needs structured
      `{file,line,snippet}`. Resolution: keep rubric/anchors/criteria/score-rules byte-for-byte
      verbatim; ADD a Phase-4 evidence-binding section to the prompt + enforce the evidence object in
      the json_schema. Guardrail test asserts `load_system_prompt()` still contains the full verbatim
      persona (additive, not rewrite) and that no anchor/criterion/score-rule text changed.
- [ ] **#28 User prompt** = instruction + diff + Review A + Review B.
- [ ] **#32 Canary injections** — fabricated claims caught with expected outcomes.

### Adapted (logic keeps; mechanism changes)
- [ ] **#13 JSON parsing** — use OpenAI structured-output / JSON mode (stronger than fence-stripping).
- [ ] **#22 Retry loop** — keep max-2-attempts structure; retryable set = RateLimited + structural +
      confab; drop MCP/freshness/port-offset.
- [ ] **#26 Timeout** — OpenAI request timeout + overall per-run wall-clock cap (was subprocess timeout).
- [ ] **#29 Diff truncation** — keep 32k cap initially (revisit: real tool loop can Read past it).
- [ ] **#30 PR checkout** — **FIX**: fetch + checkout PR head SHA into the clone before verification.

---

## Design decisions — RESOLVED via Reviewer plan review (was NEEDS REVISION)

1. **Forced verification = SINGLE AGENTIC LOOP, not two-phase. (M1 — the crux.)**
   `tool_choice="required"` forces the *act* of a tool call, not the *use* of its result, and a
   two-phase split divorces verification from scoring (the old judge did both in ONE context with
   file contents in-context at scoring time). Therefore:
   - **Single loop**: model interleaves Read/Grep/Glob with reasoning (`tool_choice="auto"`),
     structured-output verdict forced only at the END. This IS what the old skwad agent did.
   - **Evidence-binding** (stronger than old, per user's ask): every `claim_trace.evidence` must
     cite file+line+quoted snippet; harness cross-checks the cited file was actually returned by a
     tool call in the transcript. Fabricated evidence becomes detectable.
   - **System prompt forbids scoring any claim not backed by an in-transcript tool result.**
2. **Canary design hardened (M1.3).** Canaries must reference concrete code referents refutable
   ONLY via tools: a mix of contradicted-via-Read (file content), contradicted-via-Grep (symbol
   absence), contradicted-via-Glob (file absence), PLUS ≥1 TRUE "verified" canary (so the judge
   isn't just marking everything contradicted), PLUS a non-obvious fabrication not guessable from
   surface pattern. Pattern-matching "this smells fake" must NOT pass.
3. **PR checkout = per-PR isolation, sequential phase. (M2.)** Shared-clone + checkout-SHA is a
   race (parallel judges fight over one working tree → nondeterministic wrong-code). Use one clone
   per PR at its SHA, or `git worktree add` per PR off a shared object store; checkout happens in the
   SEQUENTIAL prep phase, never the parallel executor. Derive SHA from `git rev-parse FETCH_HEAD`
   after `git fetch origin pull/<n>/head` (avoids force-push TOCTOU); add `headRefOid` to
   `fetch_pr_metadata --json`; thread `commit_sha` to manifest; deleted-fork head → skip + record
   (mirror existing skip pattern), don't crash. Fix `clone_repo_ssh` reuse (`git pull` fails on
   detached HEAD → fetch+checkout instead). Tool sandbox (#31) scopes to the per-PR path.
4. **Tool-call counting = EMITTED, individual entries. (M4.)** Count emitted `tool_calls[]` entries
   (success OR error — an errored Read is legitimate verification) across ALL assistant messages
   (parallel tool-calling means one message can carry multiple). Matches old `count_tool_calls`.
   Note: the 1-per-5 ratio was calibrated on Claude granularity; Tester sanity-checks observed
   counts on a real run (not a blocker).
5. **Cost/latency at N=30** — Phase 0 must produce a real estimate
   (2 reviews × 3 runs × 30 PRs × multi-turn agentic loop = hundreds of conversations), not just
   "reachable." The per-run wall-clock cap (#26) matters MORE with an agentic loop that can spin.

---

## Phased implementation + fine-grained commit strategy (RESEQUENCED per Reviewer)

Each phase ends green (tests pass) and is independently committable/revertible.
**Key resequence (M2 + Consider):** the PR-checkout fix moves BEFORE the verification loop — the
canaries (which reference real PR code) can't be validated until the judge reads the correct checkout.

**Phase 0 — Pre-flight (Coder/Tester spike, no production code)**
- Verify GPT-5.1 reachable; confirm tool-use + structured-output API shape; confirm `OPENAI_API_KEY`.
- **Produce a real cost/concurrency estimate** (hundreds of multi-turn conversations at N=30) + a
  proposed per-run wall-clock cap. Commit: none (spike report only).

**Phase 1 — OpenAI client + sandboxed repo tools (judge unused)**
- Add `openai` dep; client wrapper; Read/Grep/Glob implementations **hard-sandboxed to a path (#31)**.
- Tests: sandbox rejects path traversal / reads outside the repo; each tool correct on a fixture repo.
- Commit: `feat: add openai client + sandboxed repo tools for python judge`

**Phase 2 — Port the pure-Python core (no behavior change)**
- Lift #11–#21, #23–#25, #27–#28 into the new judge module; prove parity on voting/structural logic.
- Keep `judge_team.json` as the VERBATIM `persona_instructions` prompt source (#27) — do NOT delete.
- Commit: `refactor: extract provider-agnostic judge scoring/validation core`

**Phase 3 — PR checkout fix + per-PR isolation (#30, M2) — split into 3 commits**
- (a) Add `headRefOid` to `fetch_pr_metadata --json`; thread `commit_sha` through to manifest.
  Commit: `feat: capture PR head SHA in metadata + manifest`
- (b) Per-PR clone/worktree isolation in the SEQUENTIAL prep phase; fix `clone_repo_ssh` reuse
  (detached-HEAD safe: fetch+checkout, not `git pull`); deleted-fork head → skip + record.
  Commit: `fix: per-PR repo isolation to prevent shared-clone checkout race`
- (c) Checkout PR head SHA (from `FETCH_HEAD`); scope the tool sandbox to the per-PR path.
  Commit: `fix: verify against PR head SHA, sandbox tools to per-PR checkout`
- Tests: each PR's judge sees its own SHA under concurrency; PR-only files visible; race-free.

**Phase 4 — Single agentic verification loop + confab re-anchor (#1–#4, #8–#10, M1/M3/M4)**
- Single loop (`tool_choice="auto"`, interleaved), structured-output verdict forced at end.
- Evidence-binding: claim_trace must cite file+line+snippet, cross-checked vs the tool transcript
  (file-level: cited file was actually read; AND snippet-level: substring-match the quoted snippet
  against the tool's returned text — closes the misquote residual).
- Count EMITTED `tool_calls[]` entries across all messages (M4); wire #3 confab gate.
- **Structural validation runs BEFORE confab (#11b/M3).** #4 ("verification actually ran") is a
  HARD fail/retry (inherits #5's no-silent-scoring job), never a soft skip.
- Tests: hardened canaries (read/grep/glob-refutable + a true-positive + a non-obvious one) all
  caught; zero-tool-call-with-claims → rejected; confab ratio enforced; malformed schema rejected
  before confab; fabricated-evidence (cited file never read) → flagged.
- Commit: `feat: single-loop openai verification with evidence-binding + confab gate`

**Phase 5 — Retry/timeout/rate-limit + retire skwad JUDGE path (#7, #22, #26)**
- SDK `RateLimitError`/429 backoff; request timeout + overall wall-clock cap; drop
  stderr/MCP/freshness/port logic FROM THE JUDGE ONLY.
- **Scope guard:** do NOT touch the difficulty classifier (`classifier_team.json`) or the
  skwad-review-under-test (`run_skwad_review`) — those remain skwad subprocesses with ports.
- Stamp manifest judge model = `gpt-5.1` (not the old `claude-sonnet-4-6`).
- Commit: `feat: openai-native retry/timeout; retire skwad-spawned judge path`

**Phase 6 — Integration + small re-run validation**
- End-to-end on the existing PR sample (small n); confirm all parity behaviors fire; quantify the
  fidelity gain from the checkout fix (claim outcomes before/after).
- Commit: `test: end-to-end python openai judge integration`

---

## Test strategy (every level)
- **Unit**: each parity-checklist row (tool sandbox, structural validation, median vote, confab rule,
  canary detection, retry classification).
- **Integration**: full judge on a fixture PR with a known-good and a fabricated review.
- **Acceptance**: small re-run on real PRs; canaries pass; no run scores with zero tool calls when
  claims exist.

## Risks
- **R1 (high): judge games the loop** — emits a verdict without genuine verification. Mitigated by
  the single-loop in-context design + evidence-binding + hardened canaries + the #3/#4 gates. Residual
  (narrow): a model that legitimately reads a file could still *misquote* the snippet and pass the
  file-level cross-check → Phase 4 adds a substring-match of the quoted snippet against the tool's
  returned text to close it.
- **R2 (med): PR-checkout edge cases** — force-pushed/closed PRs, missing SHA.
- **R3 (med): cost/rate-limits at N=30** — size concurrency in Phase 0/6.
- **R4 (low): structured-output schema drift** vs the old fence-stripping parser.

## Live validation findings (2026-06-22) — Phase 4 follow-up fixes

First live GPT-5.1 batch on the fixture repo surfaced the R1/M4 cross-model risks empirically
(2 of 3 runs failed verification gates). User-approved fixes (still inside the Phase 4 Reviewer gate):

- **#1 Confab-floor recalibration (validity-sensitive).** GPT-5.1 BATCHES verification (e.g. 16 claims
  verified in 3 tool calls), so the Claude-calibrated `ceil(verified/5)` floor falsely rejected an
  honest run. DECISION: evidence-binding is now the strong per-claim gate; the tool-call floor is
  demoted to a COARSE backstop catching only egregious near-zero-tool runs. **LOCKED (user-approved):
  `CONFAB_CLAIMS_PER_TOOL_CALL = 10`** (was 5) → `min_required = max(1, ceil(verified/10))`. Provisional
  / extrapolated from 2 live data points; named constant, one-line-tunable; user's live re-validation
  is the calibration check. Behavior: 16/3✅, 5/1✅, 11/1❌, 30/2❌, 3/0❌(≥1 floor), 0/0✅. Deliberate
  deviation from the old Claude calibration, recorded here so it isn't silent.
- **#2 Absence-via-Grep evidence (additive schema variant).** A claim contradicted via Grep
  (symbol absent) has no file to Read/quote, so the `{file,line,snippet}` requirement wrongly raised
  "file never read" and sank 2 runs. Fix: additive evidence variant recording grep pattern+scope+empty
  result, validated against an emitted Grep tool call. Existing content-claim object kept.
- **#4 Judge-quality prompt tuning.** Judge missed a subtle LRU-vs-FIFO canary (marked a false claim
  verified). Addendum strengthened to require control/data-flow tracing before marking SEMANTIC claims
  verified. Addendum-only; rubric/anchors/score-rules stay verbatim.

Validation split: Coder implements offline-only; Tester covers offline-reproducible pieces (#2 fully,
#1 regression+toothiness); USER owns live re-validation (#1 threshold realism, #4 canary catch).

### CRITICAL fix (Reviewer-caught, 2026-06-22) — evidence-binding rubber-stamp opt-out CLOSED
The first Phase-4 review found a model-controlled opt-out: `_check_evidence_binding` keyed on evidence
SHAPE, so a `verified` claim with STRING evidence skipped binding entirely → rubber-stamp path. FIXED:
binding now keys on `claim["outcome"]` — verified/contradicted MUST cite a binding object (content or
absence) → `EvidenceBindingError(StructuralInvalidRun)`, routed to a distinct `evidence_binding_rejections`
counter (retryable). Plus: grep-absence requires exact-pattern + claim-tie (kills unrelated-empty-grep
gaming); content-binding path-normalized (normpath + basename-suffix) with snippet substring still the
real guard. Reviewer re-reviewed + independently re-ran → **code APPROVED (506 pass / 0 fail)**.

### Phase-4 status: CODE APPROVED. Closure pending USER live re-validation (3/3 runs + LRU canary caught).

### Known residual (PARKED by user) + Phase-5 follow-ups
- **Count/trace decoupling (residual):** score uses integer `verified_findings` buckets; structural only
  requires the trace be NON-EMPTY, so verified counts could exceed bound-trace entries (phantom findings
  inflate score). Binding protects every TRACED claim, not the bucket counts. UNPARK the consistency
  check (bound-verified-trace-count ≥ sum(verified_findings)) IF the user's live run shows the symptom.
- **Phase 5:** wire `evidence_binding_rejections` into the manifest (init + persist) when the judge is
  wired into main.py; `grep_scope` is currently decorative (future hardening, backstopped by addendum+canaries).

## Out of scope
- Any Go changes (Route B is Python-only).
- Re-running the full N=30 (separate experiment once the judge is validated).

---

## COMPLETION STATUS (2026-06-22)
- **Phases 1–5: CODE COMPLETE + independently Reviewer-approved** (Reviewer re-ran the suite each gate).
- **Phase 6 prep COMPLETE** (turnkey live commands, fidelity helper, dead-module banner) — Reviewer-approved.
- **Offline suite GREEN: ~523–525 tests, 0 failing, 2 skipped** (the 2 = live double-gated, user-owned).
- **Remaining = USER-gated behavioral validation only:** Phase 4 live re-validation (3/3 + LRU canary) + Phase 6 e2e. Blocked solely on the user authorizing live token spend in the agents' session (harness correctly refuses peer-relayed re-auth).
- **Parked for user:** count/trace consistency check (unpark if live shows score inflation); full `lib/judge.py` deletion; `grep_scope` enforcement (future hardening).

## KEY LEARNINGS (ways of working + design patterns)
1. **Layered gates pay for themselves.** Plan review → per-phase code review → independent suite re-runs caught issues in increasing-cheapness order: a scope/architecture fork (Route A vs B) at plan time, a clone race + missing teardown at Phase 3, and a CRITICAL rubber-stamp hole at Phase 4 — each before it could compound.
2. **Verify, don't trust reports.** The Reviewer re-ran the suite itself rather than trusting agent-reported greens, catching a stale "0 failing" snapshot and a moving-tree edit mid-review. Treat agent "done" as a claim to verify.
3. **Freeze the changeset before test/review.** A file changing under the Reviewer caused a stale review. Adopt: land fix → FREEZE → test on frozen tree → review on frozen tree.
4. **Pre-lock contracts before writing.** Signature churn (Phases 1/3) was eliminated once Coder+Tester locked symbol/exception contracts up front and the Tester pre-wrote gated tests that activate on landing (no red window).
5. **Adversarial LIVE testing on a fixture is non-negotiable for cross-model work.** One bounded live batch exposed confab-floor miscalibration, a grep-absence false-fail, and a missed subtle canary — all of which would have silently corrupted an N=30 run. Catch on a fixture for a few dollars, not at scale.
6. **A gate must not trust the prompt.** The rubber-stamp hole existed because binding keyed on evidence SHAPE while the prompt merely *asked* for objects. Enforce guarantees in code keyed on the scoring outcome, not on model compliance.
7. **Surface deviations + record decisions, never drift silently.** The verbatim-persona additive addendum and the /10 recalibration were both user-approved and written into this plan with rationale — so a future reader sees intentional, bounded choices.
8. **Respect harness/session boundaries.** A user-set token-spend revocation can only be lifted by the user in-session; a manager's relayed authorization does not carry. Design completion plans around what is and isn't delegable across sessions.
