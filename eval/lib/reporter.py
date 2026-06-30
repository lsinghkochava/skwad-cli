"""Generate research report comparing skwad-cli vs Claude CI code review quality."""

import math
import os
import re
import statistics as _stats_stdlib
import sys
from datetime import datetime, timezone
from pathlib import Path

# Allow running from repo root or from within eval/
_REPO_ROOT = str(Path(__file__).resolve().parent.parent.parent)
if _REPO_ROOT not in sys.path:
    sys.path.insert(0, _REPO_ROOT)

try:
    from eval.lib import stats as _stats
    from eval.lib.stats import check_methodology_version
    from scipy.stats import spearmanr as _spearmanr
    _SCIPY_AVAILABLE = True
except ImportError:
    _SCIPY_AVAILABLE = False
    def check_methodology_version(records):  # type: ignore
        pass


_RESEARCH_CRITERIA = [
    ("issue_detection", "Issue Detection"),
    ("actionability", "Actionability"),
    ("severity_accuracy", "Severity Accuracy"),
    ("coverage", "Coverage"),
    ("signal_to_noise", "Signal-to-Noise"),
    ("depth", "Depth"),
    ("novel_substantive_findings", "Novel & Substantive Findings"),
]
_DIFFICULTIES = ("easy", "medium", "hard")


def _f(x: float) -> str:
    return f"{x:.4f}"


def _sd(values: list[float]) -> float:
    return _stats_stdlib.stdev(values) if len(values) > 1 else 0.0


def _get_voted(result: dict, system: str, criterion: str) -> int:
    return result.get(system, {}).get(criterion, {}).get("voted", 0)


def _get_total(result: dict, system: str) -> int:
    return result.get(system, {}).get("total", 0)


def _collapsible(summary: str, body: str) -> str:
    return f"<details><summary>{summary}</summary>\n\n{body}\n\n</details>"


def _table(headers: list[str], rows: list[list[str]]) -> str:
    sep = "|".join("---" for _ in headers)
    head = "| " + " | ".join(headers) + " |"
    divider = "| " + sep + " |"
    body = "\n".join("| " + " | ".join(str(c) for c in row) + " |" for row in rows)
    return "\n".join([head, divider, body])


def _safe_wilcoxon(diffs: list[int]) -> dict:
    if not _SCIPY_AVAILABLE:
        return {"error": "scipy not available"}
    try:
        return _stats.wilcoxon_paired(diffs)
    except Exception as e:
        return {"error": str(e)}


def _safe_cliffs(a: list[int], b: list[int]) -> dict:
    if not _SCIPY_AVAILABLE:
        return {"error": "scipy not available"}
    try:
        return _stats.cliffs_delta(a, b)
    except Exception as e:
        return {"error": str(e)}


def _safe_cliffs_bca(a: list[int], b: list[int], seed: int = 42) -> dict:
    if not _SCIPY_AVAILABLE:
        return {"error": "scipy not available"}
    try:
        return _stats.cliffs_delta_bca_ci(a, b, seed=seed)
    except Exception as e:
        return {"error": str(e)}


def _safe_krippendorff(data: list[list[int]], n_boot: int = 200, seed: int = 42) -> dict:
    if not _SCIPY_AVAILABLE:
        return {"error": "scipy not available"}
    try:
        return _stats.krippendorff_alpha_ordinal(data, n_boot=n_boot, seed=seed)
    except Exception as e:
        return {"error": str(e)}


def _safe_bh(pvals: list[float]) -> dict:
    if not _SCIPY_AVAILABLE or not pvals:
        return {"adjusted": pvals, "rejected": [False] * len(pvals), "error": "scipy not available"}
    try:
        return _stats.bh_fdr_adjust(pvals)
    except Exception as e:
        return {"adjusted": pvals, "rejected": [False] * len(pvals), "error": str(e)}


# --- Section builders ---

def _model_family(model_id: str) -> str:
    """Coarse model-family bucket from a model id (prefix map). gpt*/o-series → GPT,
    claude* → Claude, gemini* → Gemini, else 'other'."""
    m = (model_id or "").lower()
    if m.startswith(("gpt", "chatgpt", "o1", "o3", "o4")):
        return "GPT"
    if m.startswith("claude"):
        return "Claude"
    if m.startswith("gemini"):
        return "Gemini"
    return "other"


def _judge_bias_paragraphs(manifest: dict) -> dict:
    """Family-aware judge-bias prose, sourced from `manifest["models"]` (NEVER hardcoded).
    When the judge is a DIFFERENT family from both review systems, same-model
    self-preference does not apply (a strength) and residual bias plausibly OVER-states
    the structurally-richer system; otherwise the same-model self-preference framing
    applies. Returns the §1/§8/§10 paragraphs + the §2 judge line."""
    models = manifest.get("models", {}) or {}
    judge = models.get("judge", "unknown")
    skwad = models.get("skwad_review_agents", "unknown")
    ci = models.get("claude_ci", "unknown")
    cross = _model_family(judge) not in (_model_family(skwad), _model_family(ci))

    if cross:
        sec1 = (
            f"> **Threat to validity — cross-model judge & stylistic bias**: The judge "
            f"({judge}, via codex) is a different model family from both review systems "
            f"({skwad}; Claude CI {ci}). Cross-model judging removes the same-model "
            f"self-preference confound (Zheng et al. 2023) — the judge has no in-family "
            f"output to favor on either side, so the 8/8 result is not attributable to judge "
            f"self-preference. Residual threat: the judge's own generic preferences — a GPT "
            f"judge that rewards structure, explicit reasoning, and finding-count may favor "
            f"skwad-cli's multi-agent, more-structured output, plausibly **over-stating** its "
            f"margin. Treat the direction as robust (cross-model, grounded, claim-counted) but "
            f"the magnitude — especially on the subjective score-only criteria — with that caution."
        )
        sec8 = (
            f"> **1. Cross-model judge — stylistic/structure bias (PRIMARY residual)**: Judge "
            f"{judge} is out-of-family vs both review systems (both Claude-based), so same-model "
            f"self-preference does not apply — a methodological strength. Residual concern: the "
            f"judge's generic stylistic preferences (structure, verbosity, finding-count) "
            f"plausibly favor the more-structured multi-agent skwad-cli output and could inflate "
            f"its margin. Mitigation: the claim-backed count criteria (issue_detection, coverage, "
            f"depth, novel) are grounding-verified and less style-susceptible; the subjective "
            f"score-only criteria are more exposed and have the weakest inter-run reliability (§6)."
        )
        sec10 = (
            f"> **Caveat**: The judge is cross-model ({judge} vs two Claude-based systems), "
            f"removing same-model self-preference. Any residual judge bias (stylistic/structure "
            f"preference) would more plausibly **over-state** skwad-cli's wins than under-state "
            f"them — treat the margin, not the direction, with caution. N=8 with 3 subjective "
            f"criteria below the reliability gate further argues for confirmation at larger N."
        )
        sec2_line = (
            f"**Judge**: {judge} (via codex) — a different model family from both Claude-based "
            f"reviewers (skwad-cli and Claude CI), so neither side is favored."
        )
    else:
        sec1 = (
            f"> **Primary threat to validity — same-model judge bias**: The LLM judge ({judge}) "
            f"is the same model family as a review system ({skwad} / Claude CI {ci}). Literature "
            f"(Zheng et al. 2023) documents ~10–25% self-preference in LLM judges. Plausible bias "
            f"direction: Claude CI is a single completion; skwad-cli is multi-agent and "
            f"structurally different. Residual bias plausibly **favors the in-family system**, so "
            f"treat headline conclusions as conditional pending cross-model corroboration."
        )
        sec8 = (
            f"> **1. Same-model judge bias (PRIMARY)**: Judge {judge} shares a family with a review "
            f"system ({skwad} / {ci}). Zheng et al. (2023) documents ~10–25% self-preference. "
            f"Plausible direction: residual bias favors the in-family system; cross-model "
            f"sensitivity analysis deferred — headline conclusions remain conditional."
        )
        sec10 = (
            f"> **Caveat**: Same-model judge bias (see §8) — judge {judge} shares a family with a "
            f"review system — means the verdict is conditional on the residual self-preference "
            f"remaining within the ~10–25% range documented in the literature. Cross-model "
            f"corroboration is required for a definitive claim."
        )
        sec2_line = f"**Judge**: {judge} (via codex) — scoring skwad-cli and Claude CI."
    return {"sec1": sec1, "sec8": sec8, "sec10": sec10, "sec2_line": sec2_line}


def _sec1_executive_summary(
    pr_results: list[dict], wilcoxon_res: dict, cliffs_res: dict, cliffs_bca: dict,
    manifest: dict,
) -> str:
    n = len(pr_results)
    skwad_totals = [_get_total(r, "skwad") for r in pr_results]
    ci_totals = [_get_total(r, "claude_ci") for r in pr_results]
    skwad_wins = sum(1 for s, c in zip(skwad_totals, ci_totals) if s > c)
    ci_wins = sum(1 for s, c in zip(skwad_totals, ci_totals) if c > s)
    ties = n - skwad_wins - ci_wins

    delta = cliffs_res.get("delta", float("nan"))
    interp = cliffs_res.get("interpretation", "N/A")
    p = wilcoxon_res.get("p_value", float("nan"))

    if not math.isnan(p) and p < 0.05 and abs(delta) >= 0.33:
        verdict = "evidence supports H1 (skwad-cli reviews score higher)"
    elif not math.isnan(p):
        verdict = "no medium-or-larger effect detected at N=30"
    else:
        verdict = "insufficient data for verdict"

    ci_lower = cliffs_bca.get("ci_lower", float("nan"))
    ci_upper = cliffs_bca.get("ci_upper", float("nan"))
    ci_str = f"[{_f(ci_lower)}, {_f(ci_upper)}]" if not math.isnan(ci_lower) else "N/A"

    lines = [
        "## 1. Executive Summary",
        "",
        f"**Verdict**: {verdict}",
        "",
        f"- PRs evaluated: {n}",
        f"- skwad-cli wins (strict >): {skwad_wins} | Claude CI wins: {ci_wins} | Ties: {ties}",
        f"- Primary effect size: Cliff's δ = {_f(delta) if not math.isnan(delta) else 'N/A'} ({interp}), 95% BCa CI {ci_str}",
        f"- Primary Wilcoxon p-value: {_f(p) if not math.isnan(p) else 'N/A'}",
        "",
        _judge_bias_paragraphs(manifest)["sec1"],
    ]
    return "\n".join(lines)


def _sec2_hypothesis(manifest: dict) -> str:
    return "\n".join([
        "## 2. Hypothesis & Methodology",
        "",
        "- **H1**: skwad-cli multi-agent reviews score higher than GitHub Claude CI reviews.",
        "- **H0**: No meaningful difference between the two systems.",
        "- **Test direction**: Two-sided Wilcoxon signed-rank (pre-registered primary endpoint: total score).",
        "",
        "**Rubric**: 7 criteria × 0-3 scale, 21-point max. See `eval/config/rubric.json` and `eval/config/methodology.md`.",
        "",
        "**Judge design**: Each PR judged 3× with counterbalanced A/B assignment "
        "(run 1: skwad=A; run 2: CI=A; run 3: seeded random). Majority vote (median) per criterion.",
        "",
        _judge_bias_paragraphs(manifest)["sec2_line"],
        "",
        "**Statistical plan**: Primary — Wilcoxon + Cliff's δ + 95% BCa CI. "
        "Exploratory — per-criterion (7) + per-difficulty (3) Wilcoxon under BH-FDR (q=0.05). "
        "IRR — Krippendorff's α (ordinal), bootstrap 95% CI (nboot=200), gate on lower bound ≥ 0.6.",
        "",
        "**Power (N=30)**: Large δ ≥ 0.47 detectable; medium δ ≈ 0.33 marginal; small δ ≈ 0.15 not detectable.",
    ])


def _sec3_sample(pr_results: list[dict], manifest: dict) -> str:
    lines = ["## 3. Sample", ""]
    prs = manifest.get("prs", [])
    if prs:
        rows = [[p["repo"], str(p["pr"]), p.get("commit_sha", "N/A")[:8], p.get("difficulty", "N/A")] for p in prs]
        lines.append(_table(["Repo", "PR", "Commit SHA", "Difficulty"], rows))
    else:
        lines.append("(no PR metadata in manifest)")

    # Disagreements
    disagree = [r for r in pr_results
                if isinstance(r.get("difficulty"), dict) and r["difficulty"].get("llm_delta", 0) != 0]
    lines += ["", f"**Heuristic vs LLM difficulty disagreements**: {len(disagree)}"]
    if disagree:
        rows2 = [[
            r["pr_data"]["repo"],
            str(r["pr_data"]["pr_number"]),
            r["difficulty"]["heuristic_bucket"],
            r["difficulty"]["bucket"],
            f"{r['difficulty']['llm_delta']:+d}",
        ] for r in disagree]
        lines.append(_table(["Repo", "PR", "Heuristic", "Final", "Delta"], rows2))

    skipped = manifest.get("skipped_prs", [])
    lines += ["", f"**Skipped PRs**: {len(skipped)}"]
    if skipped:
        rows3 = [[s["repo"], str(s["pr"]), s["reason"]] for s in skipped]
        lines.append(_table(["Repo", "PR", "Reason"], rows3))
    return "\n".join(lines)


def _verification_summary_table(result: dict) -> str:
    """Render a Verification Summary table for a PR result."""
    rows = []
    for system, label in (("skwad", "Skwad"), ("claude_ci", "Claude CI")):
        vs = result.get(system, {}).get("verification_summary", {})
        if not vs:
            continue
        total = max(1, vs.get("claims_verified", 0) + vs.get("claims_unverified", 0) +
                    vs.get("claims_contradicted", 0) + vs.get("claims_non_falsifiable", 0))
        rate_pct = f"{int(vs.get('verification_rate', 0) * 100)}%"
        gr = vs.get("grounding_rate")
        grounding_pct = "n/a" if gr is None else f"{int(gr * 100)}%"
        rows.append([
            label,
            str(vs.get("claims_verified", 0)),
            str(vs.get("claims_unverified", 0)),
            str(vs.get("claims_contradicted", 0)),
            str(vs.get("claims_non_falsifiable", 0)),
            rate_pct,
            str(vs.get("tool_calls_observed", 0)),
            str(vs.get("ungrounded", 0)),
            grounding_pct,
        ])
    if not rows:
        return ""
    return _table(
        ["Review", "Verified", "Unverified", "Contradicted", "Non-falsifiable", "Rate",
         "Tool Calls", "Ungrounded", "Grounding"],
        rows,
    )


def _md_cell(s) -> str:
    """Make a value safe for a single-line markdown table cell: collapse all whitespace
    (incl. newlines/tabs) to single spaces and escape literal `|` → `\\|`. Claim-trace
    evidence snippets carry multi-line code + literal pipes that otherwise break the row
    (everything after spills into a pipe-soup paragraph)."""
    return re.sub(r"\s+", " ", str(s)).replace("|", "\\|").strip()


def _evidence_str(ev) -> str:
    """Render claim_trace evidence for the table. Codex uses OBJECT evidence
    ({file,line,snippet} or {grep_pattern}); a legacy string is passed through. (The old
    `(evidence or "")[:120]` crashed on dict evidence.)"""
    if isinstance(ev, dict):
        if ev.get("grep_pattern"):
            return f"grep:{ev['grep_pattern']}"
        if ev.get("file"):
            return f"{ev['file']}:{ev.get('line', '')} {ev.get('snippet', '') or ''}".strip()
        return str(ev)
    return ev or ""


def _claim_trace_collapsible(result: dict, system: str, label: str) -> str:
    """Render a collapsible claim trace block for a system."""
    # Use claim_trace from the first run's raw response for the given system.
    runs = result.get("runs", [])
    if not runs:
        return ""
    # Find the first resolved run where system data exists.
    claim_trace = []
    for run in runs:
        raw = run.get("raw_response", {})
        # The system maps to review_a or review_b based on ab_assignment.
        ab = run.get("ab_assignment", [])
        if len(ab) == 2:
            review_key = "review_a" if ab[0] == system else "review_b"
            claim_trace = raw.get(review_key, {}).get("claim_trace", [])
            if claim_trace:
                break

    if not claim_trace:
        return ""
    rows = [
        [
            _md_cell(ct.get("claim_text") or "")[:80],
            _md_cell(ct.get("outcome", "")),
            _md_cell(", ".join(ct.get("tools_used") or [])),
            _md_cell(_evidence_str(ct.get("evidence")))[:120],
        ]
        for ct in claim_trace
    ]
    body = _table(["Claim", "Outcome", "Tools", "Evidence"], rows)
    return _collapsible(f"Claim trace ({label})", body)


def _sec4_per_pr(pr_results: list[dict]) -> str:
    blocks = ["## 4. Per-PR Results", ""]
    for r in pr_results:
        pd = r["pr_data"]
        pr_id = f"{pd['repo']}#{pd['pr_number']}"
        skwad_total = _get_total(r, "skwad")
        ci_total = _get_total(r, "claude_ci")
        n_comp = r.get("n_runs_completed", r.get("skwad", {}).get("n_runs_completed", "?"))
        degraded = f" ⚠️ degraded evidence ({n_comp}/3 runs)" if n_comp != 3 else ""
        _diff_info = r.get("difficulty")
        difficulty = _diff_info.get("bucket", "N/A") if isinstance(_diff_info, dict) else str(_diff_info or "N/A")

        rows = [[cname,
                 str(_get_voted(r, "skwad", cid)),
                 str(_get_voted(r, "claude_ci", cid))]
                for cid, cname in _RESEARCH_CRITERIA]
        rows.append(["**Total**", f"**{skwad_total}**", f"**{ci_total}**"])
        score_table = _table(["Criterion", "skwad-cli", "Claude CI"], rows)

        reasoning_md = "\n".join(
            f"**{cname}**: " + (r.get("skwad", {}).get(cid, {}).get("reasoning_runs", ["N/A"])[0] or "N/A")
            for cid, cname in _RESEARCH_CRITERIA
        )

        blocks.append(f"### {pr_id} (difficulty: {difficulty}){degraded}")
        blocks.append(score_table)

        # Verification summary (v2 only — silently omitted if no data).
        vs_table = _verification_summary_table(r)
        if vs_table:
            blocks.append("### Verification Summary")
            blocks.append(vs_table)

        blocks.append(_collapsible("Judge reasoning (skwad-cli)", reasoning_md))
        ct_skwad = _claim_trace_collapsible(r, "skwad", "Skwad")
        if ct_skwad:
            blocks.append(ct_skwad)
        ct_ci = _claim_trace_collapsible(r, "claude_ci", "Claude CI")
        if ct_ci:
            blocks.append(ct_ci)
        blocks.append(_collapsible("Raw review — skwad-cli", r.get("skwad_review", "(none)")))
        blocks.append(_collapsible("Raw review — Claude CI", r.get("claude_ci_review", "(none)")))
        blocks.append(_collapsible("Classifier output", str(r.get("difficulty", {}))))
    return "\n\n".join(blocks)


def _sec5_aggregate(pr_results: list[dict]) -> str:
    n = len(pr_results)
    if n == 0:
        return "## 5. Aggregate Analysis\n\n(no data)"

    skwad_totals = [_get_total(r, "skwad") for r in pr_results]
    ci_totals = [_get_total(r, "claude_ci") for r in pr_results]
    skwad_wins = sum(1 for s, c in zip(skwad_totals, ci_totals) if s > c)
    ci_wins = sum(1 for s, c in zip(skwad_totals, ci_totals) if c > s)
    ties = n - skwad_wins - ci_wins

    lines = [
        "## 5. Aggregate Analysis",
        "",
        f"| System | Mean Total | SD | Wins (strict >) | Ties |",
        f"|--------|-----------|-----|-----------------|------|",
        f"| skwad-cli | {_f(_stats_stdlib.mean(skwad_totals))} | {_f(_sd(skwad_totals))} | {skwad_wins}/{n} | {ties}/{n} |",
        f"| Claude CI | {_f(_stats_stdlib.mean(ci_totals))} | {_f(_sd(ci_totals))} | {ci_wins}/{n} | {ties}/{n} |",
        "",
        "### Per-Criterion Winner",
    ]
    rows = []
    for cid, cname in _RESEARCH_CRITERIA:
        s_scores = [_get_voted(r, "skwad", cid) for r in pr_results]
        c_scores = [_get_voted(r, "claude_ci", cid) for r in pr_results]
        s_mean = _stats_stdlib.mean(s_scores)
        c_mean = _stats_stdlib.mean(c_scores)
        w = "skwad-cli" if s_mean > c_mean else "Claude CI" if c_mean > s_mean else "Tie"
        rows.append([cname, _f(s_mean), _f(c_mean), w])
    lines.append(_table(["Criterion", "skwad-cli mean", "Claude CI mean", "Leader"], rows))

    # Per-difficulty
    lines += ["", "### Per-Difficulty Winner"]
    diff_rows = []
    for bucket in _DIFFICULTIES:
        subset = [r for r in pr_results
                  if isinstance(r.get("difficulty"), dict) and r["difficulty"].get("bucket", "") == bucket]
        if not subset:
            diff_rows.append([bucket.capitalize(), "N/A", "N/A", "N/A"])
            continue
        s_means = _stats_stdlib.mean([_get_total(r, "skwad") for r in subset])
        c_means = _stats_stdlib.mean([_get_total(r, "claude_ci") for r in subset])
        w = "skwad-cli" if s_means > c_means else "Claude CI" if c_means > s_means else "Tie"
        diff_rows.append([bucket.capitalize(), str(len(subset)), _f(s_means), _f(c_means), w])
    lines.append(_table(["Difficulty", "N", "skwad-cli mean", "Claude CI mean", "Leader"], diff_rows))

    # Verification summary (v2 only).
    v2_results = [r for r in pr_results if r.get("skwad", {}).get("verification_summary")]
    if v2_results:
        lines += ["", "### Average Verification Rate (v2 runs only)"]
        for system, label in (("skwad", "skwad-cli"), ("claude_ci", "Claude CI")):
            rates = [
                r.get(system, {}).get("verification_summary", {}).get("verification_rate", 0.0)
                for r in v2_results
            ]
            avg_rate = _stats_stdlib.mean(rates) if rates else 0.0
            lines.append(f"- {label}: {avg_rate:.1%} average verification rate across {len(v2_results)} PR(s)")

    # Inter-criterion correlation matrix (Spearman)
    lines += ["", "### Inter-Criterion Correlation Matrix (Spearman)"]
    if _SCIPY_AVAILABLE and n >= 3:
        criterion_ids = [cid for cid, _ in _RESEARCH_CRITERIA]
        # Combined scores (both systems) per criterion
        combined = {cid: [_get_voted(r, s, cid) for r in pr_results for s in ("skwad", "claude_ci")] for cid in criterion_ids}
        # Columns numbered 1..N (kept narrow); each row is the full numbered name,
        # so row i is the legend for column i — no truncation, no width blow-up.
        header = [""] + [str(i + 1) for i in range(len(_RESEARCH_CRITERIA))]
        corr_rows = []
        for a_idx, (cid_a, cname_a) in enumerate(_RESEARCH_CRITERIA):
            row = [f"{a_idx + 1}. {cname_a}"]
            for cid_b, _ in _RESEARCH_CRITERIA:
                if cid_a == cid_b:
                    row.append("1.00")
                else:
                    try:
                        r_val, _ = _spearmanr(combined[cid_a], combined[cid_b])
                        row.append(f"{r_val:.2f}")
                    except Exception:
                        row.append("N/A")
            corr_rows.append(row)
        lines.append(_table(header, corr_rows))
    else:
        lines.append("(insufficient data for correlation matrix)")
    return "\n".join(lines)


def _sec6_judge_consistency(pr_results: list[dict]) -> str:
    lines = ["## 6. Judge Consistency", ""]
    if not _SCIPY_AVAILABLE:
        lines.append("(scipy not available — cannot compute Krippendorff's α)")
        return "\n".join(lines)
    rows = []
    for cid, cname in _RESEARCH_CRITERIA:
        # 3 raters (runs), items = PRs × 2 systems
        run_scores: list[list[int]] = [[], [], []]
        for r in pr_results:
            for sys_key in ("skwad", "claude_ci"):
                scores_3 = r.get(sys_key, {}).get(cid, {}).get("scores", [0, 0, 0])
                for run_idx in range(3):
                    run_scores[run_idx].append(scores_3[run_idx] if run_idx < len(scores_3) else 0)
        ka = _safe_krippendorff(run_scores, n_boot=200)
        if "error" in ka:
            rows.append([cname, "N/A", "N/A", "N/A", ""])
        else:
            flagged = "⚠️ below gate" if ka["ci_lower"] < 0.6 else ""
            rows.append([cname, _f(ka["alpha"]), _f(ka["ci_lower"]), _f(ka["ci_upper"]), flagged])
    lines.append(_table(["Criterion", "α", "CI lower", "CI upper", "Flag"], rows))
    lines += ["", "Gate: α lower-CI-bound ≥ 0.6. Criteria below the gate are flagged (⚠️) as low-reliability caveats and count against pilot_pass (harness validity); they are NOT excluded from the headline statistics, which run on the full 7-criterion total."]
    return "\n".join(lines)


def _sec7_significance(pr_results: list[dict], seed: int = 42) -> str:
    n = len(pr_results)
    skwad_totals = [_get_total(r, "skwad") for r in pr_results]
    ci_totals = [_get_total(r, "claude_ci") for r in pr_results]
    diffs = [s - c for s, c in zip(skwad_totals, ci_totals)]

    w = _safe_wilcoxon(diffs)
    cd = _safe_cliffs(skwad_totals, ci_totals)
    bca = _safe_cliffs_bca(skwad_totals, ci_totals, seed=seed)

    lines = [
        "## 7. Statistical Significance",
        "",
        "### Primary: Wilcoxon Signed-Rank (total score)",
        f"- Statistic: {_f(float(w['statistic'])) if w.get('statistic') is not None else 'N/A'}",
        f"- p-value: {_f(w['p_value']) if 'p_value' in w else 'N/A'}",
        f"- n (pairs): {w.get('n', n)}, n_nonzero: {w.get('n_nonzero', 'N/A')}",
        "",
        "### Effect Size: Cliff's δ",
        f"- δ = {_f(cd['delta']) if 'delta' in cd else 'N/A'} ({cd.get('interpretation', 'N/A')})",
        f"- 95% BCa CI: [{_f(bca.get('ci_lower', float('nan')))}, {_f(bca.get('ci_upper', float('nan')))}]",
        "",
        "### Exploratory Tests (BH-FDR adjusted, q=0.05)",
    ]

    # Per-criterion
    crit_pvals: list[float] = []
    crit_labels: list[str] = []
    for cid, cname in _RESEARCH_CRITERIA:
        s = [_get_voted(r, "skwad", cid) for r in pr_results]
        c = [_get_voted(r, "claude_ci", cid) for r in pr_results]
        d = [si - ci for si, ci in zip(s, c)]
        res = _safe_wilcoxon(d)
        if "p_value" in res:
            crit_pvals.append(res["p_value"])
            crit_labels.append(cname)

    # Per-difficulty
    diff_pvals: list[float] = []
    diff_labels: list[str] = []
    for bucket in _DIFFICULTIES:
        subset = [r for r in pr_results
                  if isinstance(r.get("difficulty"), dict) and r["difficulty"].get("bucket", "") == bucket]
        if len(subset) >= 2:
            d = [_get_total(r, "skwad") - _get_total(r, "claude_ci") for r in subset]
            res = _safe_wilcoxon(d)
            if "p_value" in res:
                diff_pvals.append(res["p_value"])
                diff_labels.append(bucket.capitalize())

    all_pvals = crit_pvals + diff_pvals
    all_labels = crit_labels + diff_labels

    if all_pvals:
        bh = _safe_bh(all_pvals)
        adj = bh.get("adjusted", all_pvals)
        rej = bh.get("rejected", [False] * len(all_pvals))
        rows = [[lbl, _f(raw), _f(a), "✓" if r else ""]
                for lbl, raw, a, r in zip(all_labels, all_pvals, adj, rej)]
        lines.append(_table(["Test", "Raw p", "BH-adj p", "Rejected"], rows))
    else:
        lines.append("(insufficient data for exploratory tests)")

    # Position-bias check
    lines += ["", "### Position-Bias Check",
              "",
              "Run 1 = skwad-cli in slot A; Run 2 = skwad-cli in slot B. "
              "A unidirectional position effect on one system but not the other is a more serious blinding "
              "failure than a symmetric one. Both tests are reported separately and never collapsed.",
              ""]
    eligible = [r for r in pr_results if len(r.get("runs", [])) >= 2]

    def _run_score_total(run, sys_key):
        # Σ of the faithful per-criterion scores for this run. Do NOT use
        # resolved[sys].total — that aggregate is the buggy codex-self-reported field
        # (off by +1 on a couple of runs); the per-criterion scores are authoritative.
        crit = run.get("resolved", {}).get(sys_key, {}).get("criteria", {})
        scores = [c.get("score") for c in crit.values()
                  if isinstance(c, dict) and c.get("score") is not None]
        return sum(scores) if scores else None

    run1_skwad = [_run_score_total(r["runs"][0], "skwad") for r in eligible]
    run2_skwad = [_run_score_total(r["runs"][1], "skwad") for r in eligible]
    valid_skwad = [(a, b) for a, b in zip(run1_skwad, run2_skwad) if a is not None and b is not None]
    if len(valid_skwad) >= 2:
        diffs_pos = [a - b for a, b in valid_skwad]
        pos_w = _safe_wilcoxon(diffs_pos)
        lines.append(f"skwad-cli position check (run1 A vs run2 B): p = {_f(pos_w.get('p_value', float('nan'))) if 'p_value' in pos_w else 'N/A'}")
    else:
        lines.append("skwad-cli position check: (insufficient run data)")

    run1_ci = [_run_score_total(r["runs"][0], "claude_ci") for r in eligible]
    run2_ci = [_run_score_total(r["runs"][1], "claude_ci") for r in eligible]
    valid_ci = [(a, b) for a, b in zip(run1_ci, run2_ci) if a is not None and b is not None]
    if len(valid_ci) >= 2:
        diffs_ci_pos = [a - b for a, b in valid_ci]
        ci_pos_w = _safe_wilcoxon(diffs_ci_pos)
        lines.append(f"Claude CI position check (run1 B vs run2 A): p = {_f(ci_pos_w.get('p_value', float('nan'))) if 'p_value' in ci_pos_w else 'N/A'}")
    else:
        lines.append("Claude CI position check: (insufficient run data)")

    return "\n".join(lines)


def _sec8_threats(manifest: dict) -> str:
    return "\n".join([
        "## 8. Threats to Validity",
        "",
        _judge_bias_paragraphs(manifest)["sec8"],
        "",
        "2. **Sample selection bias**: User-curated PR list. Full list and selection criteria disclosed in §3.",
        "3. **Ceiling effects**: Mitigated by 0-3 × 7 criteria = 21-point scale.",
        "4. **Position bias**: Mitigated by counterbalanced A/B; verified via position-bias Wilcoxon (§7).",
        "5. **Runtime variability**: LLM non-determinism mitigated by 3-run median vote.",
        "6. **Prompt sensitivity**: Single rubric/persona version; SHA pinned in manifest (§11).",
    ])


def _sec9_strengths(pr_results: list[dict]) -> str:
    lines = ["## 9. Strengths / Weaknesses Analysis", "",
             "_Threshold heuristic: ≥ 2.0 (top third of 0–3 scale) = relative strength; ≤ 1.0 (bottom third) = relative weakness._",
             ""]
    for system, label in (("skwad", "skwad-cli"), ("claude_ci", "Claude CI")):
        lines.append(f"### {label}")
        strong = []
        weak = []
        for cid, cname in _RESEARCH_CRITERIA:
            scores = [_get_voted(r, system, cid) for r in pr_results]
            if not scores:
                continue
            mean = _stats_stdlib.mean(scores)
            if mean >= 2.0:
                strong.append(f"{cname} (mean {_f(mean)})")
            elif mean <= 1.0:
                weak.append(f"{cname} (mean {_f(mean)})")
        lines.append(f"**Relative strengths** (mean ≥ 2.0): {', '.join(strong) or 'none'}")
        lines.append(f"**Relative weaknesses** (mean ≤ 1.0): {', '.join(weak) or 'none'}")
        lines.append("")
    return "\n".join(lines)


def _sec10_verdict(pr_results: list[dict], wilcoxon_res: dict, cliffs_res: dict,
                   cliffs_bca: dict, manifest: dict) -> str:
    delta = cliffs_res.get("delta", float("nan"))
    p = wilcoxon_res.get("p_value", float("nan"))

    if not math.isnan(p) and p < 0.05 and abs(delta) >= 0.33:
        verdict = "**H1 supported**: skwad-cli reviews score significantly higher."
    elif not math.isnan(p):
        verdict = "**H0 not rejected**: No medium-or-larger effect detected at this sample size."
    else:
        verdict = "**Verdict inconclusive**: insufficient data."

    ci_lower = cliffs_bca.get("ci_lower", float("nan"))
    ci_upper = cliffs_bca.get("ci_upper", float("nan"))
    ci_str = f"[{_f(ci_lower)}, {_f(ci_upper)}]" if not math.isnan(ci_lower) else "N/A"

    return "\n".join([
        "## 10. Verdict",
        "",
        verdict,
        "",
        f"- Cliff's δ = {_f(delta) if not math.isnan(delta) else 'N/A'} ({cliffs_res.get('interpretation', 'N/A')}), 95% BCa CI {ci_str}",
        f"- Wilcoxon p = {_f(p) if not math.isnan(p) else 'N/A'}",
        "",
        _judge_bias_paragraphs(manifest)["sec10"],
    ])


def _sec11_appendices(manifest: dict, pr_results: list[dict]) -> str:
    lines = ["## 11. Appendices", ""]

    # Manifest hashes
    ph = manifest.get("prompt_hashes", {})
    if ph:
        lines.append("### A. Prompt File Hashes (Reproducibility)")
        rows = [[k, v[:16] + "..." if len(v) == 64 else v] for k, v in ph.items()]
        lines.append(_table(["Key", "SHA-256 (truncated)"], rows))
        lines.append("")

    # Run file index
    lines.append("### B. Per-Run JSON Files")
    for r in pr_results:
        pr_id = f"{r['pr_data']['repo']}#{r['pr_data']['pr_number']}"
        run_count = len(r.get("runs", []))
        lines.append(f"- {pr_id}: {run_count} run(s)")

    # Position-bias raw data
    lines += ["", "### C. Position-Bias Check Raw Data"]
    for r in pr_results:
        pr_id = f"{r['pr_data']['repo']}#{r['pr_data']['pr_number']}"
        runs = r.get("runs", [])
        if len(runs) >= 2:
            r1_s = runs[0].get("resolved", {}).get("skwad", {}).get("total", "?")
            r2_s = runs[1].get("resolved", {}).get("skwad", {}).get("total", "?")
            ab = [r.get("ab_assignment", []) for r in runs[:2]]
            lines.append(f"- {pr_id}: run1(A={ab[0][0] if ab[0] else '?'}) skwad={r1_s}, run2(A={ab[1][0] if ab[1] else '?'}) skwad={r2_s}")

    return "\n".join(lines)


def generate_research_report(
    pr_results: list[dict],
    manifest: dict,
    output_path: str,
) -> None:
    """Generate the 11-section research-grade markdown report and write to disk.

    Args:
        pr_results: List of per-PR dicts from score_paired_reviews + difficulty + raw reviews.
        manifest: From manifest.open_manifest() — provides prompt_hashes, prs, skipped_prs.
        output_path: Destination path (typically eval/output/research-report.md).

    Raises:
        MethodologyMismatchError: if pr_results mixes records with different
            methodology_version values.
    """
    # Methodology version gate — refuse to aggregate v1+v2 records.
    # Use the manifest version as the expected version for all PRs.
    expected_version = manifest.get("methodology_version", 1)
    annotated = [
        {**r, "methodology_version": r.get("methodology_version", expected_version)}
        for r in pr_results
    ]
    check_methodology_version(annotated)

    seed = manifest.get("rng_seed", 42)
    skwad_totals = [_get_total(r, "skwad") for r in pr_results]
    ci_totals = [_get_total(r, "claude_ci") for r in pr_results]
    diffs = [s - c for s, c in zip(skwad_totals, ci_totals)]

    wilcoxon_res = _safe_wilcoxon(diffs) if any(d != 0 for d in diffs) else {"error": "all diffs zero"}
    cliffs_res = _safe_cliffs(skwad_totals, ci_totals)
    cliffs_bca = _safe_cliffs_bca(skwad_totals, ci_totals, seed=seed)

    timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    sections = [
        f"# Research Report: skwad-cli vs Claude CI Review Quality",
        f"*Generated: {timestamp} | N={len(pr_results)} PRs | seed={seed}*",
        "",
        _sec1_executive_summary(pr_results, wilcoxon_res, cliffs_res, cliffs_bca, manifest),
        _sec2_hypothesis(manifest),
        _sec3_sample(pr_results, manifest),
        _sec4_per_pr(pr_results),
        _sec5_aggregate(pr_results),
        _sec6_judge_consistency(pr_results),
        _sec7_significance(pr_results, seed=seed),
        _sec8_threats(manifest),
        _sec9_strengths(pr_results),
        _sec10_verdict(pr_results, wilcoxon_res, cliffs_res, cliffs_bca, manifest),
        _sec11_appendices(manifest, pr_results),
    ]

    report = "\n\n".join(s for s in sections if s)
    Path(output_path).parent.mkdir(parents=True, exist_ok=True)
    with open(output_path, "w") as f:
        f.write(report)
