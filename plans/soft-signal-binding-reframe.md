# Soft-Signal Evidence Binding Reframe

## Problem
Evidence-binding is a HARD GATE: any `EvidenceBindingError` in `_check_evidence_binding_codex`
drops the judge run; if all runs for a PR drop, `finalize_pr_runs` raises "All N judge runs
failed" → the PR is excluded from results (zero score). Real PRs keep tripping NEW binding
edges (command bundling, quote/alternation parsing, claim-tie symbol↔prose mismatch, multi-line
indentation drift, timeouts). Eight incremental parser fixes have not stopped the tail — the
gate itself is the root cause.

## Goal
Convert binding from a hard gate to a SOFT, per-claim `grounded` signal so judge runs ALWAYS
complete and produce verdicts. Preserve (and strengthen) fabrication detection via the canary
harness. Also: include force-pushed PRs via local diff derivation; bump the codex timeout.

## User-approved decisions
- **Softening scope:** soften the 6 binding checks **+ confabulation** (verified-claim-with-~0-
  tool-calls is itself ungrounded) into one harness-injected per-claim `grounded` field. KEEP
  `StructuralInvalidRun` (malformed verdict = unscoreable) and `CodexExecError`/timeout (infra)
  as HARD drops.
- **Penalty model:** OBSERVABILITY-FIRST. Annotate `grounded` + report a per-review
  `grounding_rate`; do NOT alter the A/B score in v1. A score penalty for ungrounded claims is
  a tracked fast-follow (needs new score-recompute logic; build with data in hand).

## Plan-review revisions (incorporated — Reviewer conditions)
1. **Low-grounding ALARM (must):** define a per-run/review grounding_rate FLOOR; below it, surface
   the run LOUDLY in the manifest (mirror out-of-worktree-read/quarantine flagging), not just a
   report column. Keeps organic fabrication non-silent now that the hard consequence is gone.
2. **Confab SPLIT (must — nuances the user's "soften confab" decision):** keep WHOLE-RUN
   zero-tool-call confab (`_run_and_verify_codex:2168-2170`, zero commands but verified claims =
   from-memory fabricated verdict) HARD + RETRYABLE (it's unscoreable, like StructuralInvalidRun).
   Soften ONLY per-claim binding + per-claim confab. Excluding an all-runs-zero-work PR is correct,
   not the exclusion bug. FLAG to user on return.
3. **Phase 1+2 land together (must):** never soften the gate without grounding surfacing live —
   that's the silent-fabrication window. No run/pilot on Phase-1-only.
4. **No schema re-validation (must — verify):** `grounded` is injected into in-memory claim_trace
   after `_validate_response_structure`; `run_record["raw_response"]=verdict` (1209) persists the
   mutated verdict. Coder MUST grep-confirm nothing re-validates raw_response/resolved against the
   `additionalProperties:False` VERDICT_SCHEMA downstream. STOP if found.
5. **Migrate zeroed counters:** audit all readers of `evidence_binding_rejections`/
   `confabulation_rejections` (`_EXC_COUNTER_KEY`:112) — none may KeyError or treat 0 as "healthy".
   Health signal moves to grounding_rate.
6. **Drift empty-diff policy:** on empty/underivable local diff → SKIP with reason (conservative);
   `--allow-drift` remains the live-diff override. Confirm `origin/<base>` is fetched in the worktree.
7. **Green at EACH phase boundary;** test-flip asserts BOTH `grounded is False` AND run completes
   (status=ok), not just the flag.

## Key facts (from Explorer architecture map)
- `_check_evidence_binding_codex` (openai_judge.py:2058-2133) is the ONLY binding run-dropper;
  6 raise points (2085 non-dict evidence, 2095 out-of-worktree G1, 2100 content snippet,
  2109 absence claim-tie, 2125 absence provenance, 2130 missing fields).
- `run_single_judge_task` never raises (wraps in try/except → status=failed). `finalize_pr_runs`
  (584) raises "All N failed" only if ALL runs drop → PR excluded.
- Scores are the MODEL's verdict totals; harness only median-votes. So observability-first =
  no score logic change.
- `grounded` is HARNESS-INJECTED on claim_trace entries AFTER schema validation (not a model
  output field → not gameable, not re-validated against strict VERDICT_SCHEMA).
- Canary (`_check_canary_outcomes`, 690) reads the model's self-reported outcome (not the gate),
  but currently runs only after binding succeeds → lost on abort. Soft-signal → always evaluated.
- Timeout `PER_RUN_WALLCLOCK_SEC=300` (openai_judge.py:841). One-command-per-call raised turn
  count; soft-signal removes binding-retries (net per-PR time likely drops).
- Drift: `fetch_pr_diff` (pr_fetcher.py:59) = live `gh pr diff`. Local derivation
  `git -C <wt> diff origin/<base>...HEAD` (three-dot) verified working on PR#1818; base ref
  reachable; CI comments persist across force-push (only diff needs pinning).

## Phased plan (fine-grained, each independently revertable; ALL held uncommitted)

### Phase 1 — Soft-signal binding core
- `_check_evidence_binding_codex`: replace each `raise EvidenceBindingError(msg)` with
  `claim["grounded"]=False; claim["grounding_reason"]=msg; continue`. Set `grounded=True` on
  claims that pass all checks. NEVER raise for binding reasons.
- Fold CONFABULATION (verified/contradicted claim with ~0 tool calls) into the same annotation:
  `grounded=False, grounding_reason="no tool calls observed"` instead of raising
  `ConfabulationDetected`.
- `_run_and_verify_codex`: binding/confab pass becomes annotation-only, not a drop point. Remove
  EvidenceBindingError/ConfabulationDetected from `_RETRYABLE_CODEX` (fewer retries → faster).
- KEEP `StructuralInvalidRun` + `CodexExecError`/timeout hard.
- Tests: flip every `assertRaises(EvidenceBindingError|ConfabulationDetected)` →
  `assert claim["grounded"] is False` (+ reason). LARGEST test surface.

### Phase 2 — Grounding-rate surfacing (observability)
- `_aggregate_verification_summaries` (512): compute per-review `grounding_rate` =
  grounded(verified+contradicted) / (verified+contradicted); add `ungrounded` count.
- `reporter.py:_verification_summary_table` (210): add `ungrounded` + `grounding_rate` columns.
- Fleet rollup (369 / generate_research_report 623): per-system grounding-rate.
- Manifest: replace the now-zero `evidence_binding_rejections` counter with grounding metrics.
- Score logic UNCHANGED (observability-first).

### Phase 3 — Canary strengthening
- `_check_canary_outcomes` (690): a canary is caught if injected-claim
  `outcome == expected_outcome` (existing) OR the injected claim is annotated `grounded=False`.
  Record `caught_via: "outcome"|"grounding"`. Always evaluated (runs always complete now).
- Tests: caught-via-grounding cases; confirm no canary lost on (formerly-aborting) runs.

### Phase 4 — Drift-proof resume (include 1818)
- `prepare_pr_resume` (main.py:375-383): after `fetch_pr`, when worktree HEAD known, OVERRIDE
  `pr_data["diff"]` with `git -C <wt> diff origin/<base>...HEAD`; set commit_sha/head_ref_oid to
  the worktree HEAD. Diff matches reviewed code by construction → no drift.
- Drift guard (391-411): demote to FALLBACK (skip only when derivation impossible — worktree
  missing, base unreachable, empty diff); `--allow-drift` stays an override.
- Tests: 1818-shape (wt HEAD != live HEAD) → derives local diff, proceeds, scores.

### Phase 5 — Timeout
- `PER_RUN_WALLCLOCK_SEC` 300 → 500.

## Out of scope / tracked fast-follows
- SCORE PENALTY for ungrounded claims (option b): reclassify ungrounded verified/contradicted →
  unverified + re-derive criterion scores. Build after seeing real grounding_rate data.
- Per-segment attribution for pure-read cross-file co-attribution (Q2) — separate follow-up.

## Anti-fabrication invariants to preserve
- `grounded` stays harness-injected (not model schema).
- Canary detection must be ≥ as strong as today (outcome OR grounding).
- StructuralInvalidRun/infra stay hard (no verdict ≠ ungrounded claim).
- Observability-first must not SILENTLY hide fabrication — grounding_rate must be reported.

## Test strategy
Flip binding/confab raise-tests → annotation-tests; add grounding_rate aggregation + report
tests; canary caught-via-grounding; drift local-diff derivation. Full offline suite green.
NO live tests.
