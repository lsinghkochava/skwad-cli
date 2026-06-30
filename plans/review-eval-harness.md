# Review Evaluation Harness

## Goal

CLI executable (`eval-reviews`) that compares skwad-cli multi-agent reviews vs existing Claude CI reviews on real PRs. Automated end-to-end: fetch PR → get Claude CI review → run skwad review → score both → generate comparison report.

## Key Facts (from investigation)

- **Claude CI comments**: author `github-actions`, start with `**Claude finished @{user}'s task in...`
- **Target comment**: latest *detailed* comment on PR (not last inline comment)
- **Skwad run command**: `./skwad-cli run --config ./test_configs/skwad_review_team.json --prompt "..."`
- **Skwad output**: prompt must ask agents to write findings to `comments.md` at repo root (CLI output truncated)
- **Review team**: 8 agents — Bug Hunter, Security Sentinel, Performance Analyst, Architecture Reviewer, Consistency Checker, Dependency Reviewer, Test Analyst, Review Coordinator
- **Judge**: single model, user (Lovepreet) is final approver on each comparison

## Architecture

```
eval/
  cmd/eval-reviews/main.py    # CLI entry point
  lib/
    pr_fetcher.py              # gh CLI wrapper — fetch diff, comments, PR metadata
    review_filter.py           # Extract Claude CI review comments from PR
    skwad_runner.py            # Run skwad-cli review on cloned repo + diff
    judge.py                   # LLM-as-judge scoring engine
    reporter.py                # Generate per-PR and aggregate reports
  config/
    judge_persona.md           # Full judge agent specification
    methodology.md             # Scoring methodology documentation
    rubric.json                # Machine-readable rubric definition
  output/                      # Generated reports (gitignored)
    per-pr/                    # Individual PR comparison reports
    aggregate-report.md        # Final deep research report
  requirements.txt
  README.md
```

## Phases

### Phase 1 — Methodology & Judge Agent Documentation

**Files:** `eval/config/methodology.md`, `eval/config/judge_persona.md`, `eval/config/rubric.json`

**Scoring Rubric (Additive, 0-5 integer, with reasoning):**

| Criterion | Points | Anchors |
|-----------|--------|---------|
| Issue Detection | 0-1 | 0: No real issues found or all false positives. 1: Identifies genuine, meaningful code issues |
| Actionability | 0-1 | 0: Vague feedback ("this could be better"). 1: Specific fix with code suggestion or clear direction |
| Severity Accuracy | 0-1 | 0: Treats nits as critical or misses critical issues. 1: Correctly prioritizes by actual impact |
| Coverage | 0-1 | 0: Misses obvious concerns in scope. 1: Addresses relevant security, performance, correctness, pattern issues |
| Signal-to-Noise | 0-1 | 0: Mostly noise, false positives, or irrelevant comments. 1: Every comment adds value, no noise |

**Judge requirements:**
- Chain-of-thought reasoning for EACH criterion before assigning score
- Structured JSON output: `{ criterion: { reasoning: "...", score: 0|1 }, total: N }`
- Blind evaluation — judge sees "Review A" and "Review B", not source identity
- Target: 80%+ agreement on repeated runs (run judge 3x per review, majority vote)

**Commit:** `feat(eval): add scoring methodology and judge agent documentation`

### Phase 2 — PR Fetcher & Review Filter

**Files:** `eval/cmd/eval-reviews/main.py`, `eval/lib/pr_fetcher.py`, `eval/lib/review_filter.py`

**PR Fetcher:**
- Input: JSON file with repo-to-PR mapping: `[{ "repo": "Kochava/frontend-mos", "pr": 1687 }]`
- Uses `gh pr view` to get: title, description, diff, files changed
- Uses `gh api repos/{owner}/{repo}/issues/{number}/comments` to get PR comments
- Clones repo at PR base branch (full clone, not shallow)
- Outputs structured PR data object

**Review Filter:**
- Filter by author: `github-actions`
- Filter by content: starts with `**Claude finished`
- Select: latest detailed comment (longest body among matching comments, or last by date)
- Strip boilerplate header (`**Claude finished @user's task in Xm Ys** —— [View job](...)\n\n---\n`)
- Output: clean review text for judge consumption

**Commit:** `feat(eval): add PR fetcher and Claude review filter`

### Phase 3 — Skwad Runner

**Files:** `eval/lib/skwad_runner.py`

**Skwad Runner:**
- Takes cloned repo path + PR URL
- Runs: `./skwad-cli run --config ./test_configs/skwad_review_team.json --prompt "Please review {pr_url} and put your comments in a comments.md file at root of repo"`
- Waits for completion (skwad run blocks until done)
- Reads `comments.md` from cloned repo root
- Structures output as clean review text
- Handles timeout (configurable, default 10min), error cases

**Commit:** `feat(eval): add skwad review runner`

### Phase 4 — LLM-as-Judge Scoring Engine

**Files:** `eval/lib/judge.py`

**Judge Engine:**
- Loads rubric from `rubric.json`
- Loads judge persona/prompt from `judge_persona.md`
- Scores one review at a time (direct scoring, NOT pairwise — avoids position bias)
- Runs judge 3x per review with temperature > 0 for consistency check
- Takes majority vote per criterion
- Computes confidence: if 3/3 agree = high, 2/3 = medium, no majority = low (flag for human review)
- Outputs structured JSON per review

**Judge prompt structure:**
```
You are a code review quality evaluator. You assess the quality of code reviews
based on a structured rubric. You are impartial and evaluate based on evidence only.

You will be given:
1. A code diff (the PR changes being reviewed)
2. A code review of that diff

Score this review on each criterion below. For each criterion:
1. Write 2-3 sentences of reasoning
2. Assign a score of 0 or 1

Criteria:
- Issue Detection (0-1): Does the review identify genuine, meaningful code issues?
  0 = No real issues found, or all findings are false positives
  1 = Identifies at least one genuine, meaningful code issue

- Actionability (0-1): Are the review's suggestions actionable?
  0 = Vague feedback ("this could be better", "consider improving")
  1 = Specific fix suggestions with code or clear direction

- Severity Accuracy (0-1): Does the review correctly prioritize findings?
  0 = Treats nits as critical, or misses critical issues
  1 = Severity ratings match actual impact

- Coverage (0-1): Does the review address relevant concerns for this diff?
  0 = Misses obvious security, performance, correctness, or pattern issues
  1 = Addresses the concerns relevant to the changes

- Signal-to-Noise (0-1): Is every comment valuable?
  0 = Mostly noise, false positives, or irrelevant observations
  1 = Every comment adds value, minimal noise

Output strict JSON:
{
  "issue_detection": { "reasoning": "...", "score": 0 or 1 },
  "actionability": { "reasoning": "...", "score": 0 or 1 },
  "severity_accuracy": { "reasoning": "...", "score": 0 or 1 },
  "coverage": { "reasoning": "...", "score": 0 or 1 },
  "signal_to_noise": { "reasoning": "...", "score": 0 or 1 },
  "total": <sum of scores>
}
```

**Commit:** `feat(eval): add LLM-as-judge scoring engine`

### Phase 5 — Reporter

**Files:** `eval/lib/reporter.py`

**Per-PR Report (markdown):**
```markdown
# PR Review Comparison: {repo}#{pr_number}
## PR: {title}
## Files Changed: {count}

### Scores
| Criterion        | Claude CI | Skwad | Winner |
|-----------------|-----------|-------|--------|
| Issue Detection  | 1         | 1     | Tie    |
| Actionability    | 0         | 1     | Skwad  |
| Severity         | 1         | 1     | Tie    |
| Coverage         | 1         | 1     | Tie    |
| Signal-to-Noise  | 0         | 1     | Skwad  |
| **Total**        | **3**     | **5** | **Skwad** |

### Judge Reasoning

#### Claude CI Review
[Per-criterion reasoning]

#### Skwad Review
[Per-criterion reasoning]

### Confidence
| Criterion | Claude CI (3 runs) | Skwad (3 runs) |
|-----------|-------------------|----------------|
| Issue Detection | 1,1,1 (high) | 1,1,0 (medium) |
| ... | ... | ... |

### Raw Reviews
<details><summary>Claude CI Review</summary>
{claude_review_text}
</details>

<details><summary>Skwad Review</summary>
{skwad_review_text}
</details>
```

**Aggregate Report (Deep Research — `eval/output/aggregate-report.md`):**
1. Executive summary — winner, confidence level, key differentiators
2. Methodology documentation (full rubric, anchors, statistical approach)
3. Judge agent specification (model, full prompt, temperature, version)
4. Per-PR detailed results with reasoning
5. Aggregate statistics: mean scores, win rate, per-criterion breakdown
6. Confidence analysis: agreement rates across 3 judge runs, flagged low-confidence scores
7. Strengths/weaknesses analysis for each reviewer
8. Statistical significance assessment
9. All supporting data used during evaluation
10. Final verdict with evidence

**Commit:** `feat(eval): add per-PR and aggregate report generator`

### Phase 6 — CLI Integration

**Files:** `eval/cmd/eval-reviews/main.py`

**CLI interface:**
```bash
# Run evaluation from PR list file
python eval/cmd/eval-reviews/main.py \
  --pr-file eval/pr-list.json \
  --judge-model "claude-sonnet-4-20250514" \
  --skwad-binary ./skwad-cli \
  --skwad-config ./test_configs/skwad_review_team.json \
  --output-dir eval/output/

# Single PR
python eval/cmd/eval-reviews/main.py \
  --repo "Kochava/frontend-mos" \
  --pr 1687 \
  --judge-model "claude-sonnet-4-20250514" \
  --output-dir eval/output/
```

**PR list format (JSON):**
```json
[
  { "repo": "Kochava/frontend-mos", "pr": 1687 },
  { "repo": "Kochava/watson", "pr": 855 }
]
```

**Commit:** `feat(eval): wire up CLI entry point with argument parsing`

### Phase 7 — Calibration & Validation

Before running on full PR list:
1. Run on 1 PR (Kochava/frontend-mos#1687) as pilot
2. Run judge 3x — check agreement >= 80% across runs
3. If agreement < 80%, tune rubric anchors and judge prompt
4. Present results to Lovepreet for manual approval
5. Document calibration results in methodology.md

**Commit:** `docs(eval): add calibration results to methodology`

## Commit Strategy

| Phase | Commit |
|-------|--------|
| 1 | `feat(eval): add scoring methodology and judge agent documentation` |
| 2 | `feat(eval): add PR fetcher and Claude review filter` |
| 3 | `feat(eval): add skwad review runner` |
| 4 | `feat(eval): add LLM-as-judge scoring engine` |
| 5 | `feat(eval): add per-PR and aggregate report generator` |
| 6 | `feat(eval): wire up CLI entry point with argument parsing` |
| 7 | `docs(eval): add calibration results to methodology` |

## Dependencies

- Python 3.11+
- `gh` CLI (authenticated, with access to Kochava repos)
- `anthropic` SDK (for judge API calls)
- `skwad-cli` binary (built, in PATH or specified via --skwad-binary)
- `git` (for cloning repos)

## Resolved Questions

1. **Skwad run interface**: `./skwad-cli run --config {config} --prompt "..."` — outputs to `comments.md` in repo root
2. **Claude bot identification**: author `github-actions`, content starts with `**Claude finished`
3. **Judge model**: single model, Lovepreet is final approver
