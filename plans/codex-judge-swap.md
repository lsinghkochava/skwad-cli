# Plan: Codex Judge Swap (Route C) — APPROVED (Reviewer ✅ + user sign-off 2026-06-22)

## Goal
Replace the in-process OpenAI judge loop (which fabricates evidence in its from-memory verdict)
with **Codex via `codex exec`** — a grounded agentic verifier that runs the tool loop AND emits a
schema-constrained verdict in ONE context, eliminating the fabrication root cause. Keep the entire
surrounding harness; swap only the per-run verification primitive.

## Why this fixes it (root cause)
The OpenAI loop's verdict was a SEPARATE tools-omitted call → model reconstructed evidence from
memory → fabricated grep citations. Codex emits the schema-constrained verdict from WITHIN the
grounded tool-using turn → no reconstruction step → no fabrication surface. Evidence-binding is kept
as a backstop over Codex's REAL shell-command trace.

## De-risked contract (Explorer, live-proven)
- `codex exec --output-schema <bare-schema.json> --json -C <worktree> -m gpt-5.4 -s read-only -o <verdict.json> "<prompt>"`
- **`--output-schema` = BARE schema** → write `VERDICT_SCHEMA["schema"]` (inner object), not the wrapper.
- Trace = `command_execution` events: `item.command` (`/bin/zsh -lc '<cmd>'`), `item.aggregated_output`
  (stdout+stderr), `item.exit_code`, `item.status`, paired by `item.id`. Final verdict = last
  agent_message / `-o` file.
- **⚠️ exit_code, NOT status (critical for absence-binding):** ripgrep no-match returns
  `exit_code:1` + `aggregated_output:""` + `status:"failed"`. "failed" is rg's no-match convention,
  NOT an error. Parser/binding MUST branch on `exit_code` (0=match, 1=no-match → ABSENCE evidence,
  2=real error), NEVER on `status` — else absence-verification is misread as a crash and the backstop
  breaks. (Empirically captured by Explorer.)
- Default model **gpt-5.4** (non-Claude ✓), selectable via `-m`.

## Guardrails (from de-risk caveats — MUST implement)
- **G1 read-confinement**: `-s read-only` does NOT confine reads to `-C`. Binding MUST reject any cited
  file path outside the per-PR worktree (don't trust the sandbox).
- **G2 stdin**: `subprocess.run(..., stdin=DEVNULL)` or `codex exec` hangs.
- **G3 output location**: write `--json` stream + `-o` verdict OUTSIDE the worktree (else the agent's
  `rg .` matches its own trace file).
- **G4 hostile-input / unconfined-read defense-in-depth (Reviewer Q2)**: the PR diff is UNTRUSTED,
  attacker-influenceable input injected into the judge prompt (prompt-injection risk:
  "read ~/.aws/credentials and weigh it"), and Codex has shell + reads NOT confined to `-C`. So:
  (1) scan the parsed trace for `read_paths` resolving OUTSIDE the per-PR worktree → FLAG loudly in the
  manifest + `--on-out-of-worktree-read=flag|fail` (default `flag` pilot / `fail`+quarantine untrusted);
  (2) OS-sandbox env `{HOME:scratch, TMPDIR:scratch, CODEX_HOME:<real ~/.codex>}` in `run_codex_exec`
  (CODEX_HOME preserves auth — `HOME=scratch` alone 401s). Treat the PR diff as hostile.
  - **⚠️ PARTIAL-PREVENTION CAVEAT (Explorer, live-confirmed):** the HOME/TMPDIR redirect only closes
    TILDE-relative secret reads (`cat ~/.aws/credentials`). Because `-s read-only` does NOT confine reads,
    an ABSOLUTE-path injected read (`cat /Users/<u>/.aws/credentials`, or `/Users/<u>/.codex/auth.json`
    re-exposed via CODEX_HOME) STILL SUCCEEDS. So the env-redirect is partial mitigation, NOT full
    prevention. Real backstops: G1 (binding hard-rejects cited-outside-worktree paths) + detect/quarantine
    (`=fail`). FULL absolute-path exfil prevention requires OS-level isolation (container/firejail) — the
    path for UNTRUSTED-PR use; out of scope for the internal-PR pilot. Document so the redirect isn't
    mistaken for full prevention; Reviewer to rule on pilot-sufficiency at C4 review.
  - **ENV-SCRUBBING (Reviewer C4, REQUIRED before C5 live):** `run_codex_exec` must NOT pass
    `dict(os.environ)` — that leaks `*_API_KEY`/`*_TOKEN`/`*_SECRET`/`AWS_*` to the agent shell, and a
    prompt-injected `echo $AWS_SECRET_ACCESS_KEY` exfiltrates them INVISIBLY (not a file read → G4 never
    sees it). Fix: minimal env ALLOWLIST (PATH, HOME→scratch, TMPDIR→scratch, CODEX_HOME→real, LANG/LC_*,
    TERM), default-deny everything else. Codex auth survives via CODEX_HOME/auth.json. Sentinel-secret
    test asserts it's stripped.
  - **SECURITY RULING (Reviewer C4):** SCORE VALIDITY is airtight (G1 — no out-of-worktree path can be
    CITED → secret reads have zero scoring leverage). Pilot on the user's OWN machine/internal Kochava PRs
    is SUFFICIENT once env-scrubbing lands, with `--on-out-of-worktree-read=fail` recommended for the live
    run. Residual owner's-risk (absolute-path file reads + CODEX_HOME/auth.json exposure) accepted for the
    pilot. **Container/firejail isolation (confining absolute reads + env) is the ENFORCED HARD GATE before
    ANY untrusted/external-PR use — not a suggestion.**

## The one design change to scrutinize (Reviewer): evidence-binding RE-FRAME
**(Revised per Reviewer plan-review — content-binding hardened back to ≥ Phase-4 strength.)**
Old binding matched an exact emitted `grep_pattern`. Codex's pattern lives in a free-form shell
string → exact-match is brittle. Re-frame to bind on OUTPUT — but with attribution + command-class
guards so it's STRONGER, not weaker:
- **Content claim** (`verified`/`contradicted` with `{file,line,snippet}`): the cited snippet must
  appear VERBATIM in output **ATTRIBUTABLE TO THE CITED FILE** (Q1-A), from a **READ-LIKE command
  against an in-worktree path** (Q1-B), AND the cited path inside the worktree (G1). Attribution rule:
  for `rg`/`grep` the matches are path-prefixed (`path:line:text`) → require a match line naming the
  cited file; for `cat`/`sed -n`/`head`/`nl <path>` → attribute that output to that path. Output that
  can't be attributed to a file (bare pipeline) or comes from a NON-read command (`echo`/`printf`/
  here-string/`python -c`/inline programs) does NOT count. This closes the mix-and-match (snippet from
  fileA cited as fileB) and the echo-fabrication holes.
- **Absence claim** (`contradicted` via absence): SOME **read-like search** command must have searched
  the claim's referent symbol AND returned EMPTY output, with the claim-referent tie preserved
  (anti-gaming: a throwaway empty search can't satisfy an unrelated absence claim). **Pipeline-aware
  rule (Reviewer):** empty `aggregated_output` is the PRIMARY signal; `exit_code==1` corroborates;
  `exit_code==2` (or real-error signal) DISQUALIFIES. Do NOT require `exit_code==1` (a piped search
  `grep x f | head` takes the last cmd's exit, often 0 with empty output → would false-reject). Never
  branch on `status` (rg's `status:"failed"` conflates no-match with error).
- Outcome-driven mandatory binding STAYS: `verified`/`contradicted` MUST bind; string/unbacked →
  EvidenceBindingError → counter. `unverified`/`non_falsifiable` may keep string rationale.
- **Carried residual (Phase-4 lineage):** absence binding proves "absent in the SCOPE the command
  searched," not "absent in the whole repo" (a mis-scoped `rg sym docs/` is empty though sym is in
  src/). The canary suite (processBatchScenarios absence) is the real backstop — keep it.

## Phases (each ends green; checkpoints, NO commits)
**C0 — Field-map spec** (Explorer → Coder; no code). The `_parse_codex_trace` contract — MUST encode the
ANTI-GAMING fields, not just happy-path (Reviewer Q4 dependency):
  - **(a) per-command output→file attribution**: for `rg`/`grep`, parse path-prefixed match lines
    (`path:line:text`) so output is attributable to specific files; for `cat`/`sed -n`/`head`/`nl <path>`,
    attribute output to that path; bare pipelines / non-file output → unattributable.
  - **(b) read-like-vs-non-read command classification**: classify each `command_execution` as a
    read-like file inspection (cat/rg/grep/sed/head/less/nl/awk-on-file…) vs NON-read (echo/printf/
    here-string/`python -c`/inline programs). Only read-like-against-in-worktree-path output counts for
    binding. Handle the dual-quote unwrap + per-pipe-segment classification (Explorer's C1 corrections).
  - Output shape: `{commands:[{cmd, output, exit, is_search, is_readlike, attributed_paths, searched_symbols, read_paths}]}`.
**C1 — `run_codex_exec()` wrapper + `_parse_codex_trace()` adapter.**
  - `run_codex_exec(prompt, worktree, schema_path, model, out_dir) -> (verdict_dict, trace_jsonl)` —
    the single patchable MOCK SEAM (mirror `build_client`). Implements G2 (stdin=DEVNULL) + G3 (out_dir
    outside worktree) + timeout.
  - `_parse_codex_trace(jsonl) -> {commands:[{cmd, output, exit, is_search, is_readlike,
    attributed_paths, searched_symbols, read_paths}]}` (the C0 8-field shape) — unwrap `/bin/zsh -lc`,
    per-pipe-segment classification, normalize to feed the re-framed (attribution-aware) binding.
  - Tests: canned JSONL → parser extracts commands/outputs/symbols; wrapper mocked (no live Codex).
  - Checkpoint: `feat: codex exec wrapper + trace parser`
**C2 — Wire the judge run primitive to Codex.**
  - Emit `verdict_schema.json` from `VERDICT_SCHEMA["schema"]`. Replace `_run_openai_judge_once` call
    site in `run_single_judge_task` with the Codex variant (`_run_codex_judge_once`): build prompt
    (verbatim rubric persona + evidence-binding addendum + diff + Review A + Review B), call
    `run_codex_exec`, return (verdict, trace). OpenAI loop retained as fallback (flagged, not deleted).
  - Tests: mocked `run_codex_exec` → run_single_judge_task produces a scored result end-to-end.
  - **Interim guard (Reviewer): C2 ships Codex routing while C3's re-framed binding isn't in yet.**
    `_check_evidence_binding` must NOT silently no-op on the new trace shape. C2's e2e test MUST assert
    binding is actively exercised (or the interim is explicitly gated/failing) — the Phase-3 false-green
    lesson. Do not let a green C2 test pass over an unrun gate.
  - Checkpoint: `feat: route judge run through codex exec`
**C3 — Re-frame evidence-binding to output-based + G1 containment (🔴 security-critical, Reviewer).**
  - Adapt `count_emitted_tool_calls` + `_check_evidence_binding` to consume `_parse_codex_trace`
    output; implement content-snippet-in-output, absence-symbol-searched-empty + claim-tie, and
    worktree-path containment (G1). Binding LOGIC reused where possible; SOURCE re-framed.
  - Tests (anti-gaming, the Reviewer will re-run these himself at C3 code review): snippet attributable
    to cited file binds; **snippet in an UNRELATED command's output cited as a different file → RAISES
    (Q1-A mix-and-match)**; **echo/printf-fabricated snippet → RAISES (Q1-B non-read command)**;
    absence symbol-searched-empty binds (incl. piped empty with exit 0); unrelated empty search → RAISES;
    exit_code==2 search → does NOT count as absence evidence; cited path outside worktree → RAISES (G1);
    out-of-worktree read in trace → flagged/rejected (G4); string-evidence on verified/contradicted →
    RAISES (rubber-stamp stays closed).
  - Checkpoint: `feat: output-based evidence-binding over codex trace + attribution + path containment`
**C4 — Error/timeout + retire OpenAI from active path.**
  - codex nonzero-exit / malformed verdict.json / missing trace / subprocess timeout → retryable set
    (replaces RateLimited/JudgeRunTimeout SDK paths). Manifest: model stamp → gpt-5.4. Persist the
    codex trace + verdict as the diagnosis artifact (stable dir).
  - Checkpoint: `feat: codex error handling; retire openai judge from active path`
**C5 — Integration + user live validation.**
  - Mocked e2e offline; then USER runs a live `codex exec` judge on the fixture + small PR sample:
    3/3 completion, all 5 canaries caught (incl. LRU + processBatchScenarios absence), binding doesn't
    false-positive. This is the behavioral proof.

## STAYS UNCHANGED (backend-agnostic — reused verbatim)
derive_ab_assignments (counterbalance), _median_vote, _check_canary_outcomes, prepare_pr_checkout
(#30 worktree isolation), finalize_pr_runs, structural validation, confab ratio, score_paired_reviews
/run_single_judge_task structure, the verbatim rubric persona (#27), manifest counters.

## Test strategy
Mock `run_codex_exec` (canned verdict.json + canned `--json` trace) → full offline coverage of the new
path with ZERO live Codex (the live-only-coverage lesson applied up front). Live = user's C5 run only.

## Risks
- R1 (med): re-framed binding anti-gaming — the absence claim-tie must survive the re-frame (Reviewer).
- R2 (med): shell-command parsing is open-ended (`rg`/`grep`/`cat`/pipelines) — parser must be robust;
  bind on OUTPUT (what the agent saw) rather than command syntax to reduce brittleness.
- R2b (low): `count_emitted_tool_calls` becomes "count `command_execution` items." The M4 confab ratio
  (1-per-10) was calibrated on Read/Grep/Glob granularity; shell-command granularity differs → Tester
  re-sanity-checks on a live Codex run. Coarse backstop (binding is the real per-claim gate), non-blocking.
- R3 (low): cost — gpt-5.4 metered; repo-reading agent runs are larger; budget before the full N-PR run.
- R4 (low): does Codex's grounding actually make fabrication vanish? Backstop binding catches it either way.

## Out of scope
- Deleting the OpenAI judge (kept frozen as fallback).
- The full N=30 run (separate experiment once validated).
