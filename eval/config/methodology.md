# Review Evaluation Methodology

## Purpose

Compare the quality of code reviews produced by two systems:
1. **Claude CI** — single-agent review triggered by GitHub Actions
2. **skwad-cli** — multi-agent review team (5 specialized agents: Bug Hunter, Security Sentinel, Performance Analyst, Architecture Reviewer, Review Coordinator)

---

## 1. Hypothesis

- **H1**: skwad-cli multi-agent reviews score higher than GitHub Claude CI reviews on real PRs, measured by a structured rubric.
- **H0 (null)**: No meaningful difference between the two systems.
- **Test direction**: Two-sided Wilcoxon (honest evaluation; sign of result reveals direction).
- **Primary endpoint** (pre-registered): Difference in **total score** across all PRs. Secondary / exploratory: per-criterion and per-difficulty subgroup tests, FDR-controlled.

User is fine with either outcome — honest evaluation, not hypothesis-fitting.

---

## 2. Refined Rubric

Seven criteria, 0-3 each, 21-point max. Anchors tightened to reduce inter-criterion overlap.

| Criterion | 0 | 1 | 2 | 3 |
|-----------|---|---|---|---|
| **Issue Detection** (count only) | No real issues / all FPs | 1 genuine issue | 2-3 genuine issues | 4+ genuine issues |
| **Actionability** | Vague only | Some specifics | Most have fixes | Every finding has code-level fix or clear direction |
| **Severity Accuracy** | Wrong throughout | Mostly right, 1 misorder | Correct prioritization | Correct + explicit labels matched to impact |
| **Coverage** (breadth) | Misses obvious | Catches main, misses some | Catches most relevant | Spans security, perf, correctness, patterns |
| **Signal-to-Noise** | Mostly noise | More signal than noise | High signal, minor noise | Every comment adds value, zero noise |
| **Depth** (reasoning quality only) | Surface (style, naming) | Some logic reasoning | Cross-file or architectural reasoning | Deep arch + invariants + cross-cutting concerns |
| **Novel & Substantive Findings** | 0 substantive novel | 1 substantive novel | 2-3 substantive novel | 4+ substantive novel |

### Anchor Discipline

- **Issue Detection counts only.** "Non-obviousness" lives in Depth, not here.
- **Depth scores reasoning quality only.** Count lives in Issue Detection.
- **Novel Findings requires substantive.** Judge must write a 1-line justification per counted novel finding. Trivial nitpick padding does not score.

Inter-criterion correlation matrix computed and reported in research doc (transparency on residual overlap).

---

## 3. Judge Protocol

### Setup

Each judge invocation receives **both reviews** (labeled "Review A" and "Review B") plus the diff. System identities ("skwad" / "claude-ci") are never revealed in the prompt — the judge evaluates purely on content.

The judge scores both reviews on all 7 criteria independently, producing a JSON output per review. For Novel & Substantive Findings: explicit comparison required, with a per-finding justification for each counted novel finding.

### Counterbalanced A/B Assignment (per PR, 3 runs)

| Run | A = | B = |
|-----|-----|-----|
| 1 | skwad | claude-ci |
| 2 | claude-ci | skwad |
| 3 | random (seeded) |

Guarantees each system is seen in both A and B positions across the fixed runs. Lets us check whether position predicts score (should not, if blinding works). Position bias surfaced in research doc as a sanity check.

### 3-Run Vote

- Per criterion per review: majority vote of 3 runs.
- Per criterion per review: inter-run agreement reported.
- No-majority cases flagged for human review.

---

## 4. Inter-Rater Reliability

**Metric**: Krippendorff's α with ordinal metric — correctly weights disagreement distance (0/3 disagreement > 2/3 disagreement).

**Threshold**:
- **Gate**: α lower-CI-bound ≥ 0.6 (acceptable)
- **Goal**: α ≥ 0.75 (good for research)
- α lower-CI-bound < 0.6 on any criterion → rubric anchors flagged for revision; criterion flagged (⚠️) as a low-reliability caveat and counted against the `pilot_pass` harness-validity check. NOT excluded from the headline statistics — the Wilcoxon / Cliff's δ / verdict run on the full 7-criterion total; the low-reliability criteria are reported as a footnote.

**Bootstrap CI**: Resample PRs (not runs), `nboot=2000`. Gate decision applied to the lower CI bound, not the point estimate, to guard against small-N noise.

---

## 5. Statistical Tests

Paired design (each PR scored under both systems).

### Primary (pre-registered)
- **Wilcoxon signed-rank** on total scores (two-sided, α=0.05)
- **Cliff's δ** + 95% bootstrap CI (BCa method)

### Exploratory (FDR-controlled, q=0.05)
- Per-criterion Wilcoxon (7 tests)
- Per-difficulty Wilcoxon (Easy / Medium / Hard, 3 tests)
- Benjamini-Hochberg correction applied across all 10 exploratory tests
- Adjusted p-values reported; raw p-values shown alongside for transparency

### Position-Bias Check
Per-system within-PR comparison across the two fixed A/B orderings (runs 1 and 2):
- **skwad-cli position check**: skwad-cli-in-A score (run 1) vs skwad-cli-in-B score (run 2), paired per PR, two-sided Wilcoxon.
- **Claude CI position check**: CI-in-B score (run 1) vs CI-in-A score (run 2), paired per PR, two-sided Wilcoxon.
- Run 3 (seeded random) excluded from this check.
- A unidirectional effect on one system but not the other is a more serious blinding failure — both tests reported separately, never collapsed.

---

## 6. Statistical Power (N=30)

| Effect Size (Cliff's δ) | Detectable at N=30, 80% power, α=0.05 two-sided? |
|------------------------|--------------------------------------------------|
| Large (δ ≥ 0.47) | Yes |
| Medium (δ ≈ 0.33) | Marginal (~80% power) |
| Small (δ ≈ 0.15) | No (need N ≈ 80+) |

### Verdict Language Guardrails

- If δ ≥ medium and p < 0.05 → **"evidence supports H1"**
- If p ≥ 0.05 → **"no medium-or-larger effect detected at N=30"** (NOT "no difference exists")
- Effect-size 95% CI always reported alongside p-value.

---

## 7. Reproducibility

Each run emits `eval/output/manifest.json` containing: run UUID, UTC timestamps, skwad-cli git SHA, model IDs for all LLM calls, RNG seed, SHA-256 hashes of `rubric.json` and `judge_persona.md`, Python package versions, OS info, full PR list with commit SHAs, and skipped PRs with reasons.

---

## 7a. Claim Verification Protocol (v2)

### 4-Bucket Schema

Each claim in a review is classified into one of four buckets:

| Bucket | Definition |
|--------|------------|
| `verified` | The judge confirmed the claim is correct via diff text, Read, Grep, or Glob |
| `unverified` | The judge could not confirm or deny the claim (code not accessible) |
| `contradicted` | The judge confirmed the claim is WRONG via diff text or tool use |
| `non_falsifiable` | Claim has no concrete code referent AND severity ≤ MEDIUM |

### Verification Scope

- **Locatable claims**: reference a specific function, file, line, or pattern → judge MUST verify via Read/Grep/Glob.
- **Analytic claims** (no concrete referent, severity ≤ MEDIUM): `non_falsifiable` — not penalized, not counted as verified.
- **REMOVED code claims** (deleted in diff): verify against diff text only; do NOT mark unverifiable because Read cannot find deleted code.

### Persona Verification Workflow

1. Enumerate all claims in the review.
2. For each claim, classify into verified/unverified/contradicted/non_falsifiable.
3. Record in `claim_trace` with `{claim_text, outcome, tools_used, evidence}`.
4. Score only after all claims processed.

### Cross-Check Gates (Confabulation + Disallowed Tools)

After each judge run, the harness (`eval/lib/judge.py`) reads the event log and:

1. **Confabulation guard**: If `claims_verified > 0` but `tool_calls_observed < ceil(claims_verified / 5)`, the run is rejected and retried once. Prevents fabrication of verifications without opening files.
2. **Disallowed-tool guard**: If the judge called any tool outside `{Read, Grep, Glob}` (e.g., Bash, Write, mcp:*), the run is rejected and retried once. Prevents state mutation and network access.
3. **Trace-observation divergence warning**: If declared tool use in claim_trace diverges >20% from observed tool calls in event log, a warning is logged (not blocking).

### Score Computation Rules

- `verified` + `non_falsifiable` → count toward Issue Detection, Coverage, Depth, Novel Findings thresholds.
- `unverified` → NEUTRAL — excluded from positive AND negative scoring.
- `contradicted` → false positive — does NOT count toward finding-count criteria; DECREMENTS Signal-to-Noise score.

### Pilot Pass Criteria

A pilot run is marked `pilot_pass=true` if:
- All canary injections (if any) return the expected outcome.
- Confabulation rejection rate < 20% across all judge runs.
- Disallowed-tool rejection rate < 10% across all judge runs.
- Average verification rate ≥ 30% (i.e., judge is actually opening files).

### Version Compatibility

v1 records (no `methodology_version` field) and v2 records (`methodology_version: 2`) are **NOT comparable**. `stats.py` raises `MethodologyMismatchError` if records with mixed versions are aggregated.

---

## 8. Threats to Validity

> **Primary threat: same-model judge bias.** All scoring uses Claude Sonnet. Both review systems also use Claude Sonnet. Literature (Zheng et al. 2023) documents ~10-25% self-preference in LLM judges. Plausible direction of residual bias: self-preference literature suggests stronger pull toward outputs stylistically closer to the judge's natural single-completion output — Claude CI is a single Claude completion, skwad-cli is multi-agent and structurally different. The residual bias plausibly **favors Claude CI**, meaning any skwad win is likely **under-stated** by this design and any skwad loss potentially over-stated. Magnitude is unbounded without cross-model corroboration; cross-model sensitivity analysis deferred — see Open Items.

Additional threats:
- **Sample selection bias**: User-curated PR list. Full PR list and selection criteria disclosed in research doc.
- **Ceiling effects**: Mitigated by 0-3 rubric × 7 criteria = 21-point scale (vs prior 0-1 × 5 = 5-point).
- **Position bias**: Mitigated by counterbalanced A/B; verified via position-bias Wilcoxon check.
- **Runtime variability**: LLM non-determinism mitigated by 3-run majority vote.
- **Prompt sensitivity**: Single rubric / persona version; SHA pinned in manifest.
- **Claim-verification compliance** — PARTIALLY MITIGATED dependent on judge tool-use compliance, monitored via tool-call cross-check (confabulation guard) and canary fabrication detection. Remaining exposure: judge may classify claims as `non_falsifiable` when they are verifiable. Rate visible in per-PR verification_summary.

> **Note**: v1 and v2 data are NOT comparable. `stats.py` raises `MethodologyMismatchError` if records with mixed `methodology_version` values are aggregated. Separate result sets by version before running any analysis.

---

## 9. References

- **MT-Bench (Zheng et al., 2023)**: "Judging LLM-as-a-Judge with MT-Bench and Chatbot Arena" — https://arxiv.org/abs/2306.05685
- **HuggingFace LLM-as-Judge Cookbook**: https://huggingface.co/learn/cookbook/en/llm_judge
- **Evidently AI Guide**: "LLM as a Judge" — https://www.evidentlyai.com/llm-guide/llm-as-a-judge
- **Google Engineering Practices**: Code Review Standard — https://github.com/google/eng-practices/blob/master/review/reviewer/standard.md

---

## Calibration / Validation Log

*To be populated after calibration run.*
