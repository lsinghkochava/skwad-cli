# Review Evaluation Experiment Framework

Extends `plans/review-eval-harness.md`. Goal: take the existing harness from a 1-PR pilot (TIE result, 5/5 ceiling) to a scientifically rigorous, statistically defensible experiment that can confirm or reject the hypothesis "skwad-cli reviews beat GitHub Claude CI reviews."

## Hypothesis

- **H1**: skwad-cli multi-agent reviews score higher than GitHub Claude CI reviews on real PRs, measured by a structured rubric.
- **H0 (null)**: No meaningful difference between the two systems.
- **Test direction**: two-sided Wilcoxon (honest evaluation; sign of result reveals direction).
- **Primary endpoint** (pre-registered): difference in **total score** across all PRs.
- **Secondary / exploratory**: per-criterion and per-difficulty subgroup tests, FDR-controlled.

User is fine with either outcome — honest evaluation, not hypothesis-fitting.

## Decisions (locked in)

| # | Decision | Choice |
|---|----------|--------|
| 1 | Rubric scale | 0-3 per criterion, 21-point max |
| 2 | Novel Findings | Judge sees both reviews, must justify each novel finding |
| 3 | Difficulty classifier | LLM-assisted, **bidirectional** refinement (±1 bucket) |
| 4 | Sample size | **N = 30 PRs** (user curates at runtime) |
| 5 | Judge model | Same model family across all: Claude Sonnet (cross-model sensitivity DEFERRED — primary threat to validity documented prominently) |
| 6 | Multiple comparisons | BH-FDR (q=0.05) on exploratory tests; primary endpoint pre-registered as total score |
| 7 | Inter-rater reliability | Krippendorff's α (ordinal metric) |
| 8 | Position bias | Counterbalanced A/B order across the 3 runs |
| 9 | Reproducibility | `manifest.json` per run with model IDs, SHAs, seeds, prompt hashes, versions |
| 10 | Win-rate definition | Strict `>` (ties reported separately) |

## Statistical Power (N=30)

| Effect Size (Cliff's δ) | Detectable at N=30, 80% power, α=0.05 two-sided? |
|------------------------|--------------------------------------------------|
| Large (δ ≥ 0.47) | Yes |
| Medium (δ ≈ 0.33) | Marginal (~80% power) |
| Small (δ ≈ 0.15) | No (need N ≈ 80+) |

Verdict language must match:
- If δ ≥ medium and p < 0.05 → "evidence supports H1"
- If p ≥ 0.05 → "no medium-or-larger effect detected at N=30" (NOT "no difference exists")
- Effect-size 95% CI always reported alongside p-value.

## Refined Rubric

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

Anchor discipline:
- **Issue Detection counts only.** "Non-obviousness" lives in Depth, not here.
- **Depth scores reasoning quality only.** Count lives in Issue Detection.
- **Novel Findings requires substantive.** Judge must write 1-line justification per counted novel finding. Trivial nitpick padding does not score.

Inter-criterion correlation matrix computed and reported in research doc (transparency on residual overlap).

## Judge Protocol

Each judge invocation:

- Receives **both reviews** (one as "Review A", one as "Review B") plus the diff.
- Scores both on all 7 criteria independently, JSON output.
- For Novel Findings: explicit comparison required, with per-finding justification.

### Counterbalanced A/B Assignment (per PR, 3 runs)

| Run | A = | B = |
|-----|-----|-----|
| 1 | skwad | claude-ci |
| 2 | claude-ci | skwad |
| 3 | random (seeded) |

Guarantees each system seen in both A and B positions; lets us check whether position predicts score (should not, if blinding works). Position bias surfaced in research doc as a sanity check.

### 3-Run Vote

Per criterion per review: majority vote of 3 runs.
Per criterion per review: inter-run agreement reported.

## Inter-Rater Reliability (revised)

Switched from Fleiss' kappa (nominal) to **Krippendorff's α with ordinal metric** — correctly weights disagreement distance (0/3 disagreement > 2/3 disagreement).

Threshold:
- **Gate**: α ≥ 0.6 (acceptable)
- **Goal**: α ≥ 0.75 (good for research)
- α < 0.6 on any criterion → rubric anchors flagged for revision; criterion excluded from headline analysis with footnote.

Reported with **bootstrap 95% CI** (resample PRs, not runs; `nboot=2000`) so the reader sees stability of the estimate rather than a sharp point. Gate decision applied to the lower CI bound, not the point estimate, to guard against small-N noise.

## Difficulty Stratification

Each PR classified into Easy / Medium / Hard.

### Heuristic Bounds (pre-classifier)

| Bucket | Files | LOC | Path Signals |
|--------|-------|-----|--------------|
| Easy | ≤3 | <100 | none |
| Medium | 4-10 | 100-500 | none |
| Hard | 11+ | OR 500+ | OR any path matches `auth\|security\|migration\|perf\|payment\|crypto` |

### LLM Refinement (bidirectional)

LLM classifier may upgrade OR downgrade by at most one bucket. Disagreement with heuristic logged per PR and surfaced in research doc Sample section so reader can audit.

Output: `{ bucket, reasoning, heuristic_bucket, llm_delta: -1|0|+1 }`

## Statistical Tests

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
- **Skwad position check**: Skwad-in-A score (run 1) vs Skwad-in-B score (run 2), paired per PR, two-sided Wilcoxon. Significant → judge influenced by position when scoring skwad's review.
- **Claude CI position check**: CI-in-B score (run 1) vs CI-in-A score (run 2), paired per PR, two-sided Wilcoxon. Significant → judge influenced by position when scoring Claude CI's review.
- Run 3 (seeded random) is excluded from this check; only the deterministic counterbalanced runs are compared.
- A unidirectional position effect on one system but not the other is a more serious blinding failure than a symmetric one — both tests reported separately, never collapsed.

### Sample Diagnostics
- Score histograms per system
- QQ plot of paired differences (not required for Wilcoxon but informs interpretation)
- Skip-PR characteristics: size, path tags, heuristic-bucket distribution

Adds `scipy` (Wilcoxon, BCa bootstrap), `numpy`, and the `krippendorff` PyPI package (standard implementation, ~3-line integration, supports `nboot=` for bootstrap CI). All three pinned in `eval/requirements.txt`.

## Research Document Structure

Generated to `eval/output/research-report.md` when `--research-mode` flag passed.

1. **Executive Summary** — hypothesis, verdict, headline effect size with CI, primary threat-to-validity statement (same-model judge)
2. **Hypothesis & Methodology** — full rubric, judge design w/ counterbalancing, stat test plan (primary + FDR-corrected exploratory), power analysis at N=30
3. **Sample**
   - PR list, repos, commit SHAs
   - Heuristic vs LLM difficulty (disagreements highlighted)
   - Skipped PRs with characteristics
4. **Per-PR Results** — score table, judge reasoning, collapsible raw reviews, classifier output
5. **Aggregate Analysis**
   - Overall mean ± SD per system
   - Win rate (strict `>` on **per-PR total score**) + tie rate (per-PR total score equal)
   - Per-criterion winner
   - Per-difficulty winner
   - Inter-criterion correlation matrix
6. **Judge Consistency** — Krippendorff's α per criterion with bootstrap 95% CI; gate applied to lower bound; low-α-lower-bound criteria flagged; 3-run agreement distribution
7. **Statistical Significance** — primary Wilcoxon (total), BH-FDR-corrected exploratory tests, Cliff's δ + 95% CI, position-bias check
8. **Threats to Validity** *(leads with same-model judge bias)*
   - **Primary**: Same-model judge bias (literature: ~10-25% self-preference per Zheng et al. 2023). Both systems use Claude Sonnet; judge is Claude Sonnet. Plausible direction of residual bias: self-preference literature suggests stronger pull toward outputs stylistically closer to the judge's natural single-completion output — Claude CI is a single Claude completion, skwad-cli is multi-agent and structurally different. The residual bias plausibly favors Claude CI, meaning any skwad win is likely **under-stated** by this design and any skwad loss potentially over-stated. Magnitude is unbounded without cross-model corroboration; cross-model sensitivity analysis deferred — see Open Items.
   - Sample selection bias (user-curated; full PR list and selection criteria disclosed)
   - Ceiling effects (mitigated by 0-3 rubric vs prior 0-1)
   - Position bias (mitigated by counterbalancing; verified via position-bias check)
   - Runtime variability (LLM non-determinism; mitigated by 3-run vote)
   - Prompt sensitivity (single rubric / persona version; SHA pinned in manifest)
9. **Strengths/Weaknesses Analysis** — patterns from judge reasoning logs
10. **Verdict** — H1 accepted/rejected with effect size, CI, FDR-adjusted exploratory findings, and explicit caveat for same-model bias
11. **Appendices**
    - `manifest.json` (full reproducibility metadata)
    - All raw reviews
    - All judge run JSONs (3 per system per PR)
    - Difficulty classifier outputs
    - Position-bias check raw data

## Reproducibility Manifest (`eval/output/manifest.json`)

Per run, emit:

```json
{
  "run_id": "<uuid>",
  "started_at_utc": "...",
  "completed_at_utc": "...",
  "skwad_cli_git_sha": "...",
  "models": {
    "skwad_review_agents": "claude-sonnet-4-6-...",
    "claude_ci": "<as-recorded-on-pr>",
    "judge": "claude-sonnet-4-6-...",
    "difficulty_classifier": "claude-sonnet-4-6-..."
  },
  "rng_seed": 12345,
  "prompt_hashes": {
    "rubric_json_sha256": "...",
    "judge_team_json_sha256": "...",
    "classifier_team_json_sha256": "...",
    "judge_persona_md_sha256": "...",
    "classifier_persona_md_sha256": "..."
  },
  "python_versions": { "python": "3.12.6", "scipy": "...", "numpy": "...", "krippendorff": "..." },
  "os_info": { "uname": "...", "hostname": "..." },
  "prs": [
    { "repo": "...", "pr": ..., "commit_sha": "...", "difficulty": "..." }
  ],
  "skipped_prs": [ { "repo": "...", "pr": ..., "reason": "..." } ]
}
```

## Runtime Workflow

```bash
python eval/cmd/eval-reviews/main.py \
  --pr-file ./pr-sample.json \
  --output-dir ./eval/output/ \
  --research-mode \
  --seed 12345
```

PR file format:
```json
[
  { "repo": "Kochava/frontend-mos", "pr": 1687 },
  { "repo": "Kochava/watson", "pr": 855 }
]
```

Per-PR sequence:
1. Fetch (gh CLI, clone at PR commit SHA, get diff + comments)
2. Filter Claude CI comment
3. Classify difficulty (heuristic + LLM bidirectional)
4. Run skwad-cli review → `comments.md`
5. Judge both reviews 3x with counterbalanced A/B assignment
6. Save raw + voted JSON per run
7. After all PRs: aggregate, stat tests, Krippendorff's α, position-bias check
8. Emit research doc + manifest

## Implementation Plan & Commit Strategy

Each step = own commit. Granular rollback.

| Step | Files | Commit Message |
|------|-------|----------------|
| 1 | `eval/config/rubric.json`, `eval/config/methodology.md`, `eval/config/judge_persona.md` | `feat(eval): expand rubric to 7 criteria x 0-3 with tightened anchors` |
| 2 | `eval/lib/difficulty_classifier.py` (new), wiring in `main.py` | `feat(eval): add LLM-assisted bidirectional difficulty classifier` |
| 3 | `eval/lib/judge.py`, `eval/config/judge_team.json` — schema bump + paired prompt + counterbalanced A/B | `feat(eval): judge sees both reviews with counterbalanced A/B order` |
| 4 | `eval/lib/stats.py` (new) — Wilcoxon, Cliff's δ + BCa CI, Krippendorff's α, BH-FDR | `feat(eval): add statistical analysis module` |
| 5 | `eval/lib/manifest.py` (new) — manifest writer | `feat(eval): add reproducibility manifest writer` |
| 6 | `eval/lib/reporter.py` — `generate_research_report`, all 11 sections | `feat(eval): research-mode reporter with per-difficulty + stat analysis` |
| 7 | `eval/cmd/eval-reviews/main.py` — `--research-mode`, `--seed` flags | `feat(eval): wire --research-mode and --seed CLI flags` |
| 8 | `eval/requirements.txt` — `scipy`, `numpy`, `krippendorff` | `chore(eval): add scipy + numpy + krippendorff for stat tests` |
| 9 | `eval/README.md` (new) | `docs(eval): add eval harness README` |
| 10 | Full run on user-curated N=30 PR sample | (no commit — produces research doc artifact) |

## Tests

Each new module needs unit tests. Tester writes after each Coder step.

| Module | Test Coverage |
|--------|--------------|
| `difficulty_classifier.py` | Heuristic bounds per bucket; bidirectional LLM delta; fixture diffs Easy/Medium/Hard; disagreement logging |
| `judge.py` (revised) | New 0-3 schema parsing; counterbalanced A/B assignment (runs 1+2 fixed, run 3 seeded); Novel Findings justification required; rejection of trivial-only novel scores |
| `stats.py` | Wilcoxon on known fixtures (paired data, known p-value); Cliff's δ sign + magnitude; Krippendorff's α on agreement fixtures (perfect, perfect-disagreement, random); BH-FDR adjustment on known p-vectors |
| `manifest.py` | All required fields present; SHA-256 deterministic on fixture prompts; JSON schema valid |
| `reporter.py` (research) | All 11 sections render; collapsible blocks valid markdown; per-difficulty bucketing; Threats to Validity always lists same-model first; inter-criterion correlation matrix present |

## Validation Criteria

Before declaring framework ready for full N=30 run:

1. Unit tests across all new modules green
2. End-to-end smoke test on 2 PRs (NOT scored as pilot; just verifies pipeline emits valid research doc)
3. Schema validation: manifest.json conforms to spec; research doc has all 11 sections
4. (No statistical pilot — full run is the real run)

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| **Same-model judge bias (PRIMARY)** | Documented as headline threat-to-validity. Verdict language explicitly caveated. Cross-model sensitivity deferred to follow-up (see Open Items). |
| Sample curation bias | All selection criteria disclosed in research doc; full PR list + characteristics published |
| Ceiling effects | 0-3 anchors per criterion + 7 criteria = 21-point scale; correlation matrix surfaces residual overlap |
| Position bias | Counterbalanced A/B + position-bias Wilcoxon check reported |
| Multiple comparisons | BH-FDR (q=0.05) on exploratory tests; total score pre-registered as primary |
| Stat power at N=30 | Power analysis published; verdict language matched to detectable effect size |
| Judge non-determinism | 3-run vote + Krippendorff's α lower-CI gate (≥0.6, `nboot=2000`) |
| Claude CI missing on some PRs | Skip + report skipped-PR characteristics, not just count |
| PR moves between fetch and re-fetch | Pin commit SHA at fetch; reproducible via manifest |

## Open Items After Framework Lands

- **Cross-model judge sensitivity analysis** (GPT-4 or Gemini as secondary judge) — primary follow-up to harden the same-model caveat. Without this, the headline conclusion has a documented but unbounded confound. **Until cross-model corroboration is completed, headline conclusions remain conditional on the residual same-model bias documented in §8.**
- Bayesian framing as alternative analysis (Beta-Binomial on win rate)
- Inter-PR independence assumption check (if same author or same feature area, paired draws may not be independent)
- Active-learning loop: judge flags low-confidence PRs for human re-review

## Key Learnings (appended after execution)

_To be filled in at end of run._
