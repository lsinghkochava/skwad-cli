# Eval Harness: skwad-cli vs Claude CI Review Quality

## Overview

This harness runs a scientifically rigorous LLM-as-judge evaluation comparing skwad-cli multi-agent code reviews against GitHub Claude CI reviews on real pull requests. It fetches real PR diffs and comments, classifies each PR by difficulty, runs both review systems, scores them with a counterbalanced blind judge, and emits a research-grade markdown report with full statistical analysis.

## Hypothesis

**H1** (alternative): skwad-cli reviews score higher than Claude CI reviews on the 7-criterion 0–21 rubric (two-sided Wilcoxon signed-rank, primary endpoint = per-PR total score).

**H0** (null): No difference in median total score between systems.

Pre-registered primary endpoint: total score difference per PR pair. Same-model judge bias (same Claude model family scoring both systems) is the primary threat to validity — plausible direction favors Claude CI.

## Architecture

```
eval/
├── cmd/eval-reviews/
│   └── main.py                 # CLI entrypoint: fetch → classify → review → score → report
├── config/
│   ├── rubric.json             # 7-criterion × 0-3 rubric (SHA-256 pinned in manifest)
│   ├── judge_team.json         # Judge agent skwad team config
│   ├── judge_persona.md        # Judge persona reference (inlined in judge_team.json)
│   ├── classifier_team.json    # Difficulty classifier agent skwad team config
│   ├── classifier_persona.md   # Classifier persona reference (inlined in classifier_team.json)
│   └── methodology.md          # Full methodology: rubric anchors, stats plan, threats
├── lib/
│   ├── pr_fetcher.py           # Fetch PR metadata, diff, comments via gh CLI; clone repo
│   ├── review_filter.py        # Extract Claude CI review from PR comments
│   ├── difficulty_classifier.py # Heuristic + LLM difficulty bucketing (easy/medium/hard)
│   ├── skwad_runner.py         # Run skwad-cli review on cloned repo, collect output
│   ├── judge.py                # Counterbalanced A/B judge: 3 runs, median_low vote
│   ├── stats.py                # Wilcoxon, Cliff's delta + BCa CI, Krippendorff's alpha, BH-FDR
│   ├── manifest.py             # Reproducibility manifest writer (eval/output/manifest.json)
│   └── reporter.py             # generate_research_report: 11-section research markdown doc
├── tests/
│   ├── test_difficulty_classifier.py
│   ├── test_judge.py
│   ├── test_stats.py
│   ├── test_manifest.py
│   ├── test_reporter.py
│   └── test_main.py
└── output/                     # Generated at runtime (git-ignored)
    ├── manifest.json
    ├── research-report.md
    └── <repo>-<pr>/            # Per-PR judge run JSONs
```

The skwad review team config lives at `test_configs/skwad_review_team.json` (repo root).

## Prerequisites

- **Python 3.11+** (tested on 3.12)
- **`gh` CLI** authenticated to the Kochava org (`gh auth login`)
- **`skwad-cli` binary** built from repo root (`make build`) and present at `./skwad-cli` or in PATH
- **`ANTHROPIC_API_KEY`** set in shell env or `.env` file at repo root
- **Python deps**: `pip install -r eval/requirements.txt`

```bash
pip install -r eval/requirements.txt
```

Dependencies: `scipy>=1.11.0`, `numpy>=1.24.0`, `krippendorff>=0.6.0`.

## Quickstart

```bash
# 1. Build the binary
make build

# 2. Install Python deps
pip install -r eval/requirements.txt

# 3. Curate a PR list
cat > pr-sample.json <<'EOF'
[
  { "repo_ssh": "git@github.com:Kochava/frontend-mos.git", "prs": [1687, 1690] },
  { "repo_ssh": "git@github.com:Kochava/watson.git",       "prs": [855] }
]
EOF

# 4. Run the experiment
python eval/cmd/eval-reviews/main.py \
  --pr-file ./pr-sample.json \
  --output-dir ./eval/output/ \
  --research-mode \
  --seed 12345
```

> **Sample size**: N >= 30 PRs minimum; reliably detects large effects (Cliff's delta >= 0.47), marginal (~80% power) for medium effects (delta >= 0.33). See `plans/review-eval-experiment-framework.md` §Statistical Power for the full table.

## CLI Reference

```
python eval/cmd/eval-reviews/main.py --help
```

| Flag | Default | Description |
|------|---------|-------------|
| `--pr-file` | — | JSON file listing repos and PR numbers to evaluate |
| `--repo-ssh` | — | Single repo SSH URL (use with `--pr`) |
| `--pr` | — | Single PR number (use with `--repo-ssh`) |
| `--output-dir` | `eval/output` | Directory for manifest, report, and per-PR JSONs |
| `--research-mode` | off | Generate full 11-section research report |
| `--seed` | `12345` | RNG seed for reproducible run-3 assignment and bootstrap CIs |
| `--judge-model` | `claude-sonnet-4-20250514` | Judge model ID (metadata only; judge uses skwad-cli) |
| `--skwad-binary` | `./skwad-cli` | Path to skwad-cli binary |
| `--skwad-config` | `./test_configs/skwad_review_team.json` | Skwad review team config |
| `--judge-config` | `eval/config/judge_team.json` | Judge team config |
| `--timeout` | `600` | Timeout in seconds for skwad and judge subprocesses |

The `--pr-file` JSON format is:

```json
[
  { "repo_ssh": "git@github.com:Org/Repo.git", "prs": [1234, 1235] }
]
```

## Configuration

| File | Purpose |
|------|---------|
| `eval/config/rubric.json` | 7-criterion × 0-3 rubric. Editable, but changing it invalidates prior runs — the manifest captures a SHA-256 of this file. |
| `eval/config/judge_team.json` | Judge agent skwad team config. Contains the full rubric persona inline. |
| `eval/config/classifier_team.json` | Difficulty classifier agent skwad team config. |
| `test_configs/skwad_review_team.json` | 5-agent review team: Bug Hunter, Security Sentinel, Performance Analyst, Architecture Reviewer, Review Coordinator. |
| `eval/config/methodology.md` | Full methodology document: rubric anchors, A/B judge protocol, stats plan, threats to validity. |
| `eval/config/judge_persona.md` | Judge persona reference doc (the content is inlined into `judge_team.json`). |
| `eval/config/classifier_persona.md` | Difficulty classifier persona reference doc. |

## Output Files

| Path | Description |
|------|-------------|
| `eval/output/manifest.json` | Reproducibility manifest: `run_id`, timestamps, model IDs, git SHA, RNG seed, prompt SHA-256 hashes, scipy/numpy/krippendorff versions, OS info, PR list with commit SHAs, skipped PRs with reasons. |
| `eval/output/research-report.md` | 11-section research doc (see §"Statistical Methodology"). Only written with `--research-mode`. |
| `eval/output/<repo>-<pr>/judge_pr<N>_run<R>.json` | Per-run audit JSON for each judge run (3 per PR). |
| `eval/output/<repo>-<pr>/judge_pr<N>_voted.json` | Median-low voted aggregate scores per PR. |

## Rubric Summary

7 criteria, 0–3 scale each. Total max = 21.

| Criterion | Scale | Description |
|-----------|-------|-------------|
| Issue Detection | 0–3 | Count of genuine issues found (non-obviousness not required) |
| Actionability | 0–3 | How actionable are the suggestions? |
| Severity Accuracy | 0–3 | Does the review correctly prioritize by actual impact? |
| Coverage | 0–3 | Breadth across relevant concern areas of the diff |
| Signal-to-Noise | 0–3 | Is every comment valuable, or does noise dilute useful findings? |
| Depth | 0–3 | Reasoning quality only — not issue count (that lives in Issue Detection) |
| Novel & Substantive Findings | 0–3 | Issues found by this review absent from the other; requires per-finding justification |

**Anchor discipline rules** (prevent double-counting):
1. Issue Detection counts only. Non-obviousness lives in Depth, not here.
2. Depth scores reasoning quality only. Count lives in Issue Detection.
3. Novel Findings requires substantive issues only. Trivial nitpick padding does not score.

## Statistical Methodology

- **Primary test**: Two-sided Wilcoxon signed-rank on per-PR total score differences (skwad total − CI total). Pre-registered.
- **Effect size**: Cliff's delta + 95% BCa bootstrap CI (2000 resamples, paired).
- **Exploratory**: Per-criterion + per-difficulty Wilcoxon under Benjamini-Hochberg FDR control (q=0.05).
- **Inter-rater reliability**: Krippendorff's alpha (ordinal metric, bootstrap 95% CI, n_boot=2000). Gate: lower CI bound >= 0.6 required to include a criterion in headline analysis.
- **Position bias mitigation**: Counterbalanced A/B design — run 1 = skwad-as-A/CI-as-B, run 2 = CI-as-A/skwad-as-B, run 3 = seeded-random. Both systems tested separately; results never collapsed.
- **Vote aggregation**: `statistics.median_low` across 3 runs per criterion per system (always returns an observed score, never a phantom midpoint).
- **Degraded evidence**: PRs with fewer than 3 successful judge runs are retained but flagged in the research report with a ⚠️ degraded-evidence marker (via `n_runs_completed` in `pr_result`).

## Threats to Validity

1. **Same-model judge bias (PRIMARY)**: The judge uses the same Claude model family as both review systems. Zheng et al. (2023) documents approximately 10–25% self-preference. Plausible direction favors Claude CI (single-completion output closer to the judge's natural style), meaning skwad-cli wins are likely under-stated. Cross-model sensitivity analysis deferred.
2. **Sample selection bias**: User-curated PR list. Full list and selection criteria are disclosed in the manifest.
3. **Ceiling effects**: Mitigated by 0–3 × 7 criteria = 21-point scale.
4. **Runtime variability**: LLM non-determinism mitigated by 3-run median vote.
5. **Prompt sensitivity**: Single rubric and persona version; SHA-256 pinned in manifest.

## Reproducibility

Every run emits `eval/output/manifest.json` containing:

- `run_id` (UUID), `started_at_utc`, `completed_at_utc`
- Model IDs for all agents (review, judge, classifier)
- `skwad_cli_git_sha` (auto-detected from `git rev-parse HEAD`)
- `rng_seed` (from `--seed` flag)
- SHA-256 hashes of all prompt/config files (`rubric_json_sha256`, `judge_team_json_sha256`, `classifier_team_json_sha256`, `judge_persona_md_sha256`, `classifier_persona_md_sha256`)
- scipy, numpy, and krippendorff package versions
- OS info (uname, hostname)
- PR list with commit SHAs and difficulty buckets
- Skipped PRs with skip reasons

Same seed + same inputs = deterministic run-3 A/B assignment and deterministic bootstrap CI results.

## Testing

```bash
pip install -r eval/requirements.txt
python3 -m unittest discover eval/tests -v
```

180 tests, ~1.5s. Test coverage:

| File | What it tests |
|------|---------------|
| `test_difficulty_classifier.py` | Heuristic bucketing, LLM refinement, monotonicity clamp |
| `test_judge.py` | A/B assignment, median_low voting, justification selection |
| `test_stats.py` | Wilcoxon, Cliff's delta + BCa, Krippendorff alpha, BH-FDR |
| `test_manifest.py` | open_manifest, write_manifest, record_pr, record_skipped_pr, hash_prompt_file |
| `test_reporter.py` | All 11 sections, position-bias check, stats integration, edge cases |
| `test_main.py` | CLI flag parsing, evaluate_pr orchestration, manifest wiring |

## Limitations and Follow-ups

- **Same-model judge bias** is the headline limitation. A follow-up cross-model sensitivity analysis using GPT-4 or Gemini as judge would sharpen conclusions.
- **Statistical power**: At N=30, large effects (Cliff's delta >= 0.47) are reliably detectable at 80% power; medium effects (delta >= 0.33) are marginal. Smaller effects require larger samples.
- **Directory naming**: `eval-reviews/` uses a hyphen, which prevents direct Python import. Tests use `importlib` to work around this.
- **Model IDs in manifest**: Auto-detected from `"model"` field in team config JSONs. If a config lacks this field, manifest records `"unknown"` with a warning.

## Plan and References

- `plans/review-eval-experiment-framework.md` — full implementation plan with power analysis, commit strategy, and phase breakdown.
- `plans/review-eval-harness.md` — original harness plan (pre-methodology revision).
- `eval/config/methodology.md` — rubric anchors, A/B judge protocol, stat plan, verdict language guardrails, calibration log.
