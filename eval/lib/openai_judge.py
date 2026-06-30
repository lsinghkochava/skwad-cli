"""Python OpenAI (GPT-5.1) judge — Route B.

This module is the in-process replacement for the skwad-spawned judge. Phase 2
lifts the provider-agnostic core VERBATIM from ``eval/lib/judge.py`` (#11–#21,
#23–#25, #27–#28): structural validation, A/B counterbalancing, paired judging,
median-low voting, verification-summary aggregation, confabulation accounting,
canary injection/checking, and finalize. Only the plumbing — the actual judge
invocation — changes to an OpenAI tool loop (Phase 3/4); these scoring and
validation primitives are unchanged so parity holds 1:1.

The agentic verification loop (#1–#4, #8–#10), PR-checkout/isolation (#30/M2),
and retry/timeout/rate-limit (#7/#22/#26) land in later phases.
"""

import difflib
import json
import logging
import math
import os
import random
import re
import shlex
import statistics
import subprocess
import time
from pathlib import Path
from typing import Literal

# Re-exported so the per-PR checkout/isolation API is reachable from the judge
# module (impl lives in pr_fetcher.py — git plumbing). Phase 3 (#30/M2).
# Relative import so it resolves under both import roots (pytest's eval.lib.* and
# main.py's lib.*).
from .pr_fetcher import (  # noqa: F401
    cleanup_pr_worktrees,
    prepare_pr_checkout,
    resolve_pr_head_sha,
)
from openai import APITimeoutError, RateLimitError

from .openai_client import DEFAULT_MODEL, build_client
from .repo_tools import TOOL_SCHEMAS, RepoTools, dispatch_tool_call

logger = logging.getLogger(__name__)

EVAL_DIR = Path(__file__).resolve().parent.parent
JUDGE_CONFIG = EVAL_DIR / "config" / "judge_team.json"

# Keep the 32k cap initially (#29). A real tool loop can Read past it; revisit
# in a later phase if evidence-binding needs more of the diff in-context.
DIFF_TRUNCATION_CAP = 32_000

CRITERIA = [
    "issue_detection",
    "actionability",
    "severity_accuracy",
    "coverage",
    "signal_to_noise",
    "depth",
    "novel_substantive_findings",
]

# Criteria that carry 4-bucket finding counts.
_COUNT_CRITERIA = {
    "issue_detection",
    "coverage",
    "depth",
    "novel_substantive_findings",
}

System = Literal["skwad", "claude_ci"]
_FIXED_ASSIGNMENTS: list[tuple[System, System]] = [
    ("skwad", "claude_ci"),
    ("claude_ci", "skwad"),
]


class ConfabulationDetected(RuntimeError):
    """Raised when verified_findings > 0 but observed tool calls < min_required."""


class StructuralInvalidRun(RuntimeError):
    """Raised when the judge response violates structural requirements:
    - claim_trace empty/missing while findings present, OR
    - missing 4-bucket fields on any count-based criterion.
    """


class EvidenceBindingError(StructuralInvalidRun):
    """Raised when a verified/contradicted claim's evidence is not bound to an
    in-transcript tool result (fabricated/misquoted/missing). A subclass of
    StructuralInvalidRun (so it stays retryable) but counted separately so the
    fabrication signal isn't conflated with bad-JSON in the manifest."""


class RateLimited(RuntimeError):
    """Raised when an OpenAI call hits a 429/RateLimitError or APITimeoutError.
    Retryable with backoff (#7). Replaces the skwad stderr 429-scraping."""


class JudgeRunTimeout(RuntimeError):
    """Raised when a single judge run exceeds the per-run wall-clock cap (#26) —
    the agentic loop spin guard. Retryable."""


# Maps a final (post-retry) failure exception to the pilot counter it increments
# (#25). Applied SINGLE-THREADED during result merge — the parallel callable
# never mutates shared counters. The skwad-era DisallowedToolUsed is gone under
# Route B: disallowed tools are simply never exposed (#9).
# Exact-type lookup (not isinstance), so EvidenceBindingError routes to its own
# counter even though it subclasses StructuralInvalidRun.
_EXC_COUNTER_KEY = {
    ConfabulationDetected: "confabulation_rejections",
    StructuralInvalidRun: "structural_invalid_rejections",
    EvidenceBindingError: "evidence_binding_rejections",
}


_REQUIRED_BUCKET_FIELDS = (
    "verified_findings",
    "unverified_findings",
    "contradicted_findings",
    "non_falsifiable_findings",
)


# ---------------------------------------------------------------------------
# System / user prompt construction (#27 / #28)
# ---------------------------------------------------------------------------

def load_system_prompt(config_path: str | Path = JUDGE_CONFIG) -> str:
    """Return the judge system prompt VERBATIM from judge_team.json (#27).

    The rubric/workflow/schema lives inline in the team config's
    ``persona_instructions``; it is used unmodified as the OpenAI system prompt
    (no paraphrase). The file is retained, not deleted.
    """
    data = json.loads(Path(config_path).read_text())
    return data["agents"][0]["persona_instructions"]


def build_user_prompt(diff: str, review_a: str, review_b: str) -> str:
    """Build the user message: instruction + diff + Review A + Review B (#28).

    Both reviews are presented together every run (paired judging, #16). Unlike
    the skwad path, the verdict is returned via OpenAI structured output (Phase
    4), so this prompt carries no "write JSON to a file" instruction.
    """
    return (
        "Score both code reviews using the rubric in your instructions.\n\n"
        f"{_truncate_diff(diff)}\n\n"
        "=== REVIEW A ===\n"
        f"{review_a}\n\n"
        "=== REVIEW B ===\n"
        f"{review_b}\n"
    )


# ---------------------------------------------------------------------------
# Structural validation (#11–#14, #11b/M3) + JSON parsing (#13)
# ---------------------------------------------------------------------------

def _validate_response_structure(parsed: dict) -> None:
    """Validate the judge response structure. Raises StructuralInvalidRun on violation.

    Checks:
    1. Every count-based criterion has all 4 bucket fields present.
    2. If sum(verified+contradicted+non_falsifiable) > 0 on any criterion in a
       review, that review's claim_trace MUST be non-empty.
    """
    for review_key in ("review_a", "review_b"):
        review = parsed.get(review_key)
        if not isinstance(review, dict):
            continue
        criteria = review.get("criteria", {})
        # Check 1: all 4 bucket fields present on each count criterion.
        for crit_name in _COUNT_CRITERIA:
            crit = criteria.get(crit_name, {})
            if not isinstance(crit, dict):
                raise StructuralInvalidRun(
                    f"{review_key}.{crit_name}: not a dict"
                )
            missing = [f for f in _REQUIRED_BUCKET_FIELDS if f not in crit]
            if missing:
                raise StructuralInvalidRun(
                    f"{review_key}.{crit_name}: missing bucket fields: {missing}"
                )

        # Check 2: claim_trace populated when findings present.
        total_findings = 0
        for crit_name in _COUNT_CRITERIA:
            crit = criteria.get(crit_name, {})
            total_findings += (
                crit.get("verified_findings", 0)
                + crit.get("contradicted_findings", 0)
                + crit.get("non_falsifiable_findings", 0)
            )
        claim_trace = review.get("claim_trace", [])
        if total_findings > 0 and not claim_trace:
            raise StructuralInvalidRun(
                f"{review_key}: claim_trace empty but {total_findings} finding(s) present"
            )


def _truncate_diff(diff: str) -> str:
    if len(diff) > DIFF_TRUNCATION_CAP:
        return (
            f"=== DIFF (truncated at {DIFF_TRUNCATION_CAP} chars; "
            f"full diff is {len(diff)} chars) ===\n{diff[:DIFF_TRUNCATION_CAP]}"
        )
    return f"=== DIFF ===\n{diff}"


def _parse_json_output(raw: str) -> dict:
    """Fence-tolerant JSON extraction (#13). Phase 4 prefers structured output;
    this remains as a fallback parser for free-text JSON."""
    raw = raw.strip()
    raw = re.sub(r"^```(?:json)?\s*\n?", "", raw)
    raw = re.sub(r"\n?```\s*$", "", raw)
    start = raw.find("{")
    end = raw.rfind("}")
    if start == -1 or end == -1:
        raise ValueError(f"No JSON object in judge output: {raw[:200]}")
    return json.loads(raw[start : end + 1])


# ---------------------------------------------------------------------------
# Confabulation accounting (#3) — pure helpers; loop wiring is Phase 4
# ---------------------------------------------------------------------------

def _sum_verified_in_output(parsed: dict) -> int:
    """Sum verified_findings across all count-based criteria in both reviews."""
    total = 0
    for review_key in ("review_a", "review_b"):
        review = parsed.get(review_key, {})
        criteria = review.get("criteria", {})
        for crit in _COUNT_CRITERIA:
            total += criteria.get(crit, {}).get("verified_findings", 0)
    return total


def _verdict_drove_scoreable_output(verdict: dict) -> bool:
    """Whether a verdict carries ANY scoreable signal across BOTH review sides: a
    non-empty claim_trace, verified/contradicted findings, or a nonzero criterion-score
    sum. Used by the whole-run zero-tool-call confab check — a from-memory fabricated
    verdict can score WITHOUT any verified findings (zero findings + empty claim_trace +
    nonzero criterion scores → grounding_eligible=0 → rate=None → no alarm), so
    'verified > 0' alone let it slip past both the hard drop and the grounding alarm."""
    for review_key in ("review_a", "review_b"):
        review = verdict.get(review_key, {})
        if review.get("claim_trace"):
            return True
        criteria = review.get("criteria", {})
        for crit in _COUNT_CRITERIA:
            c = criteria.get(crit, {})
            if c.get("verified_findings", 0) > 0 or c.get("contradicted_findings", 0) > 0:
                return True
        if sum(criteria.get(c, {}).get("score", 0) for c in CRITERIA) > 0:
            return True
    return False


# Confab floor, RECALIBRATED for GPT-5.1 (was 5, the Claude calibration). GPT-5.1
# BATCHES verification — a live run verified 16 claims in 3 tool calls (~5
# claims/call), which the old ceil(verified/5) floor falsely rejected as
# confabulation. Evidence-binding (_check_evidence_binding) is now the STRONG
# per-claim gate; this tool-call floor is only a COARSE backstop against egregious
# over-claiming (many "verified" with ~0 tool calls). 10 gives ~2x headroom over
# observed batching while still requiring >=1 tool call whenever claims are
# verified. *** VALIDITY-CRITICAL KNOB — flagged for Reviewer/Manager scrutiny. ***
CONFAB_CLAIMS_PER_TOOL_CALL = 10


def _check_confabulation(parsed: dict, tool_calls: list) -> None:
    """Raise ConfabulationDetected if verified claims >> observed tool calls (#3).

    Coarse backstop only (evidence-binding is the per-claim gate). Requires
    >=1 tool call when any claims are verified, plus a loose ratio that trips only
    on egregiously low tool use relative to verified-claim count.
    """
    claims_verified = _sum_verified_in_output(parsed)
    min_required = max(1, math.ceil(claims_verified / CONFAB_CLAIMS_PER_TOOL_CALL))
    if claims_verified > 0 and len(tool_calls) < min_required:
        raise ConfabulationDetected(
            f"verified={claims_verified}, tools_observed={len(tool_calls)}, "
            f"expected>={min_required} (floor=1 per {CONFAB_CLAIMS_PER_TOOL_CALL} claims)"
        )


# ---------------------------------------------------------------------------
# Phase 4 — tool-call counting (#2/M4), evidence-binding (#1), #8 / #10
# ---------------------------------------------------------------------------

def _msg_get(obj, key):
    """Read a field from a transcript element that may be a dict (test fixtures /
    our loop) or an SDK message/tool_call object (real responses)."""
    if isinstance(obj, dict):
        return obj.get(key)
    return getattr(obj, key, None)


def _is_empty_grep(content: str) -> bool:
    """Whether a Grep tool result indicates NO matches (supports absence claims).
    dispatch_tool_call returns "(no matches)" for an empty Grep."""
    return (content or "").strip() in ("", "(no matches)")


def _paths_match(cited: str, read_path: str) -> bool:
    """Whether a cited file path refers to a Read'd path, tolerant of ./ and
    sub-dir prefixes (normpath + component-suffix either direction). The snippet
    substring check remains the real guard, so suffix-matching can't rubber-stamp."""
    c = os.path.normpath((cited or "").strip())
    r = os.path.normpath((read_path or "").strip())
    if c == r:
        return True
    cs, rs = c.split(os.sep), r.split(os.sep)
    n = min(len(cs), len(rs))
    return n > 0 and cs[-n:] == rs[-n:]


def _grep_tied_to_claim(grep_pattern: str, claim_text: str) -> bool:
    """Tie an absence Grep to its claim: the pattern appears in the claim text, OR
    shares a 3+char identifier token with it. Blocks citing an unrelated trivially
    -empty Grep (e.g. Grep("zzz_nonexistent")) as evidence for a real claim."""
    grep_pattern = grep_pattern or ""
    claim_text = claim_text or ""
    if grep_pattern and grep_pattern in claim_text:
        return True
    tokens = re.findall(r"[A-Za-z_][A-Za-z0-9_]{2,}", grep_pattern)
    return any(tok in claim_text for tok in tokens)


def count_emitted_tool_calls(transcript: list[dict]) -> list[str]:
    """Count EMITTED tool-call names across all assistant messages (#2/M4).

    Replaces the skwad event-log `count_tool_calls`: counts every entry in each
    assistant message's `tool_calls[]` (parallel-tool-calling means one message
    can carry several), success OR error — an errored Read is legitimate
    verification, so the tool RESULT never changes the emission count. Returns the
    list of tool names (len == emitted entries); `[]` when none.
    """
    names: list[str] = []
    for msg in transcript:
        if _msg_get(msg, "role") != "assistant":
            continue
        for tc in (_msg_get(msg, "tool_calls") or []):
            fn = _msg_get(tc, "function") or {}
            names.append(_msg_get(fn, "name") or "")
    return names


def _check_evidence_binding(parsed: dict, transcript: list[dict]) -> None:
    """Cross-check every claim's evidence against the tool transcript (#1, the crux).

    Binding is MANDATORY for `verified`/`contradicted` claims (the outcomes that
    drive the score) — keyed on `claim["outcome"]`, NOT on evidence shape, so a
    verified claim with a string rationale can't opt out of verification.
      - Content evidence `{file, line, snippet}`: the cited file must have been
        Read (path-normalized) AND the snippet must substring-match its returned
        text (anti-misquote, R1).
      - Absence evidence `{grep_pattern, grep_scope, result}`: an emitted Grep with
        the EXACT pattern must have returned no matches, and the pattern must be
        tied to the claim text (blocks citing an unrelated empty Grep).
      - String/missing evidence on a verified/contradicted claim → reject.
    `non_falsifiable`/`unverified` claims may carry a string rationale — skipped.

    Raises EvidenceBindingError (a StructuralInvalidRun) on any binding failure.
    """
    # Pair assistant tool_calls with their tool-result content via tool_call_id.
    results_by_id: dict[str, str] = {}
    for msg in transcript:
        if _msg_get(msg, "role") == "tool":
            results_by_id[_msg_get(msg, "tool_call_id")] = _msg_get(msg, "content") or ""

    # Map each Read'd file path to the concatenated returned text, and collect the
    # emitted Grep calls (pattern -> list of returned results) for absence claims.
    read_text_by_file: dict[str, str] = {}
    grep_calls: list[tuple[str, str]] = []
    for msg in transcript:
        if _msg_get(msg, "role") != "assistant":
            continue
        for tc in (_msg_get(msg, "tool_calls") or []):
            fn = _msg_get(tc, "function") or {}
            name = (_msg_get(fn, "name") or "").lower()
            try:
                args = json.loads(_msg_get(fn, "arguments") or "{}")
            except (json.JSONDecodeError, TypeError):
                continue
            content = results_by_id.get(_msg_get(tc, "id"), "")
            if name == "read":
                path = args.get("path")
                if path:
                    read_text_by_file[path] = read_text_by_file.get(path, "") + "\n" + content
            elif name == "grep":
                pattern = args.get("pattern")
                if pattern:
                    grep_calls.append((pattern, content))

    for review_key in ("review_a", "review_b"):
        review = parsed.get(review_key, {})
        for claim in review.get("claim_trace", []):
            # Binding is MANDATORY for exactly the outcomes that drive the score.
            # non_falsifiable/unverified may carry a string rationale — nothing to bind.
            outcome = claim.get("outcome")
            if outcome not in ("verified", "contradicted"):
                continue
            claim_text = str(claim.get("claim_text", ""))
            evidence = claim.get("evidence")
            if not isinstance(evidence, dict):
                raise EvidenceBindingError(
                    f"{outcome} claim must cite binding object evidence, got "
                    f"{type(evidence).__name__} (claim: {claim_text[:60]!r})"
                )
            cited_file = evidence.get("file")
            snippet = evidence.get("snippet")
            grep_pattern = evidence.get("grep_pattern")

            if cited_file and snippet:
                # Content claim: cited file must have been Read (path-normalized) AND
                # the snippet must substring-match its returned text (anti-misquote).
                matched_text = None
                for read_path, text in read_text_by_file.items():
                    if _paths_match(cited_file, read_path):
                        matched_text = text
                        break
                if matched_text is None:
                    raise EvidenceBindingError(
                        f"evidence cites a file never read in the transcript: "
                        f"{cited_file!r} (claim: {claim_text[:60]!r})"
                    )
                if snippet not in matched_text:
                    raise EvidenceBindingError(
                        f"evidence snippet not found in returned text for {cited_file!r}: "
                        f"{snippet!r}"
                    )
            elif grep_pattern:
                # Absence claim: an emitted Grep with the EXACT cited pattern must
                # have returned NO matches, and the pattern must be tied to the claim.
                exact = [(p, c) for (p, c) in grep_calls if p == grep_pattern]
                if not exact:
                    raise EvidenceBindingError(
                        f"absence evidence cites a Grep never run (exact pattern): "
                        f"{grep_pattern!r} (claim: {claim_text[:60]!r})"
                    )
                if not any(_is_empty_grep(c) for (_p, c) in exact):
                    raise EvidenceBindingError(
                        f"absence evidence: Grep {grep_pattern!r} returned matches, "
                        f"not absence — claim contradicted by the transcript"
                    )
                if not _grep_tied_to_claim(grep_pattern, claim_text):
                    raise EvidenceBindingError(
                        f"absence Grep pattern {grep_pattern!r} not tied to the claim: "
                        f"{claim_text[:60]!r}"
                    )
            else:
                raise EvidenceBindingError(
                    f"{outcome} claim evidence object missing binding fields "
                    f"(need file+snippet OR grep_pattern): claim {claim_text[:60]!r}"
                )


def _backfill_tool_calls_observed(parsed: dict, tool_calls_a: list, tool_calls_b: list) -> None:
    """Back-fill tool_calls_observed in verification_summary from observed counts (#10)."""
    for review_key, tool_calls in (("review_a", tool_calls_a), ("review_b", tool_calls_b)):
        review = parsed.get(review_key, {})
        vs = review.get("verification_summary")
        if isinstance(vs, dict):
            vs["tool_calls_observed"] = len(tool_calls)


def _warn_trace_divergence(parsed: dict, tool_calls: list) -> None:
    """Log a warning if declared tool uses in claim_trace diverge >20% from observed (#8)."""
    declared_uses = 0
    for review_key in ("review_a", "review_b"):
        for claim in parsed.get(review_key, {}).get("claim_trace", []):
            declared_uses += len(claim.get("tools_used", []))
    observed = len(tool_calls)
    if observed > 0:
        divergence = abs(declared_uses - observed) / observed
        if divergence > 0.20:
            logger.warning(
                "trace-observation divergence: declared=%d, observed=%d, ratio=%.2f",
                declared_uses, observed, divergence,
            )


# ---------------------------------------------------------------------------
# Unswap (#17) + median vote (#18/#19) + verification aggregation (#20)
# ---------------------------------------------------------------------------

def _unswap(parsed: dict, a_system: System, b_system: System) -> dict:
    """Map review_a/review_b criterion scores back to named systems."""
    return {
        a_system: parsed.get("review_a", {}),
        b_system: parsed.get("review_b", {}),
    }


def _median_vote(runs_resolved: list[dict]) -> dict:
    """Per-criterion median across runs for each system."""
    result: dict = {}
    for system in ("skwad", "claude_ci"):
        sys_runs = [r.get(system, {}).get("criteria", {}) for r in runs_resolved]
        system_result: dict = {}
        for criterion in CRITERIA:
            scores = [r.get(criterion, {}).get("score", 0) for r in sys_runs]
            reasonings = [r.get(criterion, {}).get("reasoning", "") for r in sys_runs]
            voted = statistics.median_low(scores)
            entry: dict = {
                "scores": scores,
                "voted": voted,
                "reasoning_runs": reasonings,
            }
            if criterion in _COUNT_CRITERIA:
                for bucket in ("verified_findings", "unverified_findings",
                               "contradicted_findings", "non_falsifiable_findings"):
                    entry[bucket] = sum(
                        r.get(criterion, {}).get(bucket, 0) for r in sys_runs
                    )
            if criterion == "novel_substantive_findings":
                matching = [r for r in sys_runs if r.get(criterion, {}).get("score", -1) == voted]
                if voted > 0 and matching:
                    entry["justifications"] = matching[0].get(criterion, {}).get("justifications", [])[:voted]
                else:
                    entry["justifications"] = []
            system_result[criterion] = entry
        system_result["total"] = sum(system_result[c]["voted"] for c in CRITERIA)
        system_result["n_runs_completed"] = len(runs_resolved)
        system_result["n_runs_planned"] = 3
        result[system] = system_result
    return result


# Soft-signal binding: grounding_rate FLOOR. A run/review below this is surfaced LOUDLY
# (logger + low_grounding annotation) so organic fabrication stays non-silent now that an
# ungrounded claim no longer drops the run.
_GROUNDING_RATE_FLOOR = 0.5


def _grounding_stats(review: dict) -> dict:
    """Per-review grounding rollup over SCORE-DRIVING (verified/contradicted) claims:
    grounded/ungrounded counts, grounding_eligible (the denominator), and grounding_rate
    (None when no eligible claims — avoids div-by-zero and a misleading 0.0)."""
    grounded = ungrounded = 0
    for claim in review.get("claim_trace", []):
        if claim.get("outcome") in ("verified", "contradicted"):
            if claim.get("grounded") is True:
                grounded += 1
            else:
                ungrounded += 1
    eligible = grounded + ungrounded
    return {
        "grounded": grounded,
        "ungrounded": ungrounded,
        "grounding_eligible": eligible,
        "grounding_rate": (grounded / eligible) if eligible else None,
    }


def _aggregate_verification_summaries(runs_resolved: list[dict]) -> dict:
    """Aggregate verification_summary fields across runs.

    Counts (verified/unverified/contradicted/non_falsifiable) are summed.
    verification_rate is averaged. tool_calls_observed is summed. Soft-signal grounding
    (grounded/ungrounded over verified+contradicted claims) is summed across runs, with
    grounding_rate = grounded/eligible (None when no eligible claims).
    """
    agg: dict = {}
    for system in ("skwad", "claude_ci"):
        totals: dict = {
            "claims_verified": 0,
            "claims_unverified": 0,
            "claims_contradicted": 0,
            "claims_non_falsifiable": 0,
            "verification_rate": 0.0,
            "tool_calls_observed": 0,
            "grounded": 0,
            "ungrounded": 0,
            "grounding_eligible": 0,
        }
        n = 0
        for run in runs_resolved:
            review = run.get(system, {})
            # Grounding is read from the claim_trace (harness-injected) across ALL runs,
            # independent of whether the model emitted a verification_summary.
            gs = _grounding_stats(review)
            totals["grounded"] += gs["grounded"]
            totals["ungrounded"] += gs["ungrounded"]
            totals["grounding_eligible"] += gs["grounding_eligible"]
            vs = review.get("verification_summary", {})
            if not vs:
                continue
            n += 1
            for k in ("claims_verified", "claims_unverified", "claims_contradicted",
                      "claims_non_falsifiable", "tool_calls_observed"):
                totals[k] += vs.get(k, 0)
            totals["verification_rate"] += vs.get("verification_rate", 0.0)
        if n > 0:
            totals["verification_rate"] /= n
        eligible = totals["grounding_eligible"]
        totals["grounding_rate"] = (totals["grounded"] / eligible) if eligible else None
        agg[system] = totals
    return agg


# ---------------------------------------------------------------------------
# A/B counterbalancing (#15) + finalize (#21, #24)
# ---------------------------------------------------------------------------

def derive_ab_assignments(seed: int) -> list[tuple[System, System]]:
    """Deterministic counterbalanced A/B order for a PR's 3 runs.

    Uses a PER-CALL Random INSTANCE (never the global random module) so it is
    safe under concurrency and reproducible from `seed` alone.
    """
    rng = random.Random(seed)
    run3_assignment: tuple[System, System] = rng.choice(_FIXED_ASSIGNMENTS)
    return [*_FIXED_ASSIGNMENTS, run3_assignment]


def finalize_pr_runs(
    pr_number: int,
    run_results: list[dict],
    assignments: list[tuple[System, System]],
    run_dir: str,
    *,
    canary_injections: list[dict] | None = None,
) -> dict:
    """Vote + aggregate completed run results for ONE PR into its scored result.

    Consumes per-run task outputs (in any order). Raises RuntimeError if no run
    for the PR succeeded (#21). Crashed runs are skipped, so a PR finalizes on
    its remaining runs, e.g. 2/3 (#24). Returns the same shape the skwad judge
    always returned, plus run_durations_seconds for every ATTEMPTED run.
    """
    ordered = sorted(run_results, key=lambda r: r.get("run_index", 0))
    ok = [r for r in ordered if r.get("status") == "ok"]
    runs_raw = [r["run_record"] for r in ok]
    runs_resolved = [r["resolved"] for r in ok]
    run_durations = [r.get("duration_seconds", 0.0) for r in ordered]
    canary_outcomes: list[dict] = []
    for r in ok:
        canary_outcomes.extend(r.get("canary_outcomes", []))

    if not runs_resolved:
        raise RuntimeError(f"All {len(ordered)} judge runs failed for PR #{pr_number}")

    voted = _median_vote(runs_resolved)
    verification_summaries = _aggregate_verification_summaries(runs_resolved)
    for system in ("skwad", "claude_ci"):
        voted[system]["verification_summary"] = verification_summaries.get(system, {})

    voted_path = os.path.join(run_dir, f"judge_pr{pr_number}_voted.json")
    with open(voted_path, "w") as f:
        json.dump(voted, f, indent=2)
    logger.info(
        "Voted result saved: %s (skwad=%d, ci=%d)",
        os.path.basename(voted_path), voted["skwad"]["total"], voted["claude_ci"]["total"],
    )

    result = {
        "skwad": voted["skwad"],
        "claude_ci": voted["claude_ci"],
        "runs": runs_raw,
        "ab_assignments": [[a, b] for a, b in assignments],
        "n_runs_completed": len(runs_resolved),
        "n_runs_planned": len(assignments),
        "run_durations_seconds": run_durations,
        # Bundling observability: PR-level rollup over completed runs (per-run detail
        # lives in each run_record). Sum of bundled events; max sub-commands seen.
        "bundled_command_events": sum(r.get("bundled_command_events", 0) for r in runs_raw),
        "max_bundled_subcommands": max(
            (r.get("max_bundled_subcommands", 0) for r in runs_raw), default=0
        ),
    }
    if canary_injections:
        result["canary_outcomes"] = canary_outcomes
    return result


# ---------------------------------------------------------------------------
# Canary support (#32)
# ---------------------------------------------------------------------------

def _canary_targets_pr(canary: dict, pr_number) -> bool:
    """Whether a canary applies to the given PR.

    A canary is PR-agnostic (injects into every PR) when its target PR is
    absent, null, or "*". A specific `pr` pins it to that PR only — compared as
    strings so a JSON-authored `"pr": "1746"` still matches int pr_number 1746
    (a type mismatch would silently never inject and pass canaries_caught vacuously).
    """
    target_pr = canary.get("target_pr") or {}
    target = target_pr.get("pr")
    if target in (None, "*"):
        return True
    return str(target) == str(pr_number)


def _normalize_claim_text(text: str) -> str:
    """Lowercase and collapse all whitespace runs for tolerant matching."""
    return " ".join((text or "").lower().split())


def _canary_trace_match(canary: dict, claim_text: str, entry_text: str) -> bool:
    """Conservatively decide whether a claim_trace entry is the injected canary.

    Prefers an explicit distinctive token (`match_token`, e.g. the fabricated
    function name) that cannot collide with a genuine claim, so a paraphrased
    trace entry still matches. `claim_text` is only the FALLBACK discriminator,
    used (as a normalized first-50-char substring) when the canary has no
    `match_token`. Stays conservative — no fuzzy overlap that could match an
    unrelated claim and mask a genuine judge miss.
    """
    norm_entry = _normalize_claim_text(entry_text)
    if not norm_entry:
        return False
    match_token = _normalize_claim_text(canary.get("match_token", ""))
    if match_token:
        return match_token in norm_entry
    norm_claim = _normalize_claim_text(claim_text)
    if not norm_claim:
        return False
    return norm_claim[:50] in norm_entry


def _apply_canary_injections(
    reviews: dict[System, str],
    canaries: list[dict],
    pr_data: dict,
) -> dict[System, str]:
    """Inject canary claims into the target review text for the matching PR."""
    patched = dict(reviews)
    for canary in canaries:
        if not _canary_targets_pr(canary, pr_data.get("pr_number")):
            continue
        inject_into: str = canary.get("inject_into", "skwad")
        claim_text: str = canary.get("claim_text", "")
        if not claim_text:
            continue
        system_key: System = "skwad" if inject_into == "skwad" else "claude_ci"
        patched[system_key] = patched[system_key] + f"\n\n[INJECTED CLAIM] {claim_text}"
    return patched


# Minimum SequenceMatcher ratio for a non-contained candidate to be accepted as
# the canary's trace entry. Below this floor we record no outcome (canary FAILS)
# rather than bind to an unrelated entry and silently mask a real judge miss.
_CANARY_SIMILARITY_FLOOR = 0.6


def _check_canary_outcomes(
    parsed: dict,
    canaries: list[dict],
    a_system: System,
    b_system: System,
    pr_data: dict,
) -> list[dict]:
    """Assert canary claims in claim_trace and return outcome records."""
    outcomes: list[dict] = []
    for canary in canaries:
        if not _canary_targets_pr(canary, pr_data.get("pr_number")):
            continue
        inject_into: str = canary.get("inject_into", "skwad")
        system_key: System = "skwad" if inject_into == "skwad" else "claude_ci"
        # Map system to review_a/review_b based on assignment.
        if a_system == system_key:
            review_key = "review_a"
        elif b_system == system_key:
            review_key = "review_b"
        else:
            continue

        claim_text = canary.get("claim_text", "")
        expected_outcome = canary.get("expected_outcome", "")
        review = parsed.get(review_key, {})
        claim_trace = review.get("claim_trace", [])

        # Gather ALL trace entries that pass the conservative pre-filter, in
        # trace order. The model paraphrases the injected claim, so several
        # entries can match; binding to the FIRST (old behavior) could land on a
        # divergent-outcome sibling and silently mask a real judge miss as a PASS.
        candidates = [
            entry for entry in claim_trace
            if _canary_trace_match(canary, claim_text, entry.get("claim_text") or "")
        ]

        norm_canary = _normalize_claim_text(claim_text)
        selected = None
        if candidates:
            # (a) Containment anchor — the verbatim canary claim is always present
            # among the candidates, so a candidate that contains it (even embedded
            # in longer commentary) is a definitive match.
            contained = [
                entry for entry in candidates
                if norm_canary and norm_canary in _normalize_claim_text(entry.get("claim_text") or "")
            ]
            if len(contained) == 1:
                selected = contained[0]  # definitive, auto-accept
            else:
                # >1 contained → narrow to them (still anchored, auto-accept);
                # 0 contained → fall back to all candidates and apply the floor.
                pool = contained if contained else candidates
                anchored = bool(contained)
                # (b)/(d) Highest SequenceMatcher ratio; earliest in trace order
                # wins ties. Selection consults ONLY containment, ratio, and trace
                # order — never expected_outcome.
                ratios = [
                    difflib.SequenceMatcher(
                        None, norm_canary, _normalize_claim_text(entry.get("claim_text") or "")
                    ).ratio()
                    for entry in pool
                ]
                best_ratio = max(ratios)
                best_idx = ratios.index(best_ratio)
                tied = [entry for entry, r in zip(pool, ratios) if r == best_ratio]
                if len(tied) > 1 and len({e.get("outcome") for e in tied}) > 1:
                    logger.warning(
                        "Ambiguous canary disambiguation: id=%s has %d trace entries "
                        "tied at ratio=%.3f with divergent outcomes %s; selecting "
                        "earliest in trace order",
                        canary.get("id"), len(tied), best_ratio,
                        sorted({str(e.get("outcome")) for e in tied}),
                    )
                # (c) Threshold floor — a containment anchor auto-accepts (ratio
                # treated as 1.0); otherwise a sub-floor best is not a real match.
                if anchored or best_ratio >= _CANARY_SIMILARITY_FLOOR:
                    selected = pool[best_idx]

        if selected is not None:
            actual_outcome = selected.get("outcome")
            rationale = canary.get("rationale", "")
        else:
            actual_outcome = None
            rationale = "no trace entry matched canary claim above similarity floor"

        # Strengthened detection (soft-signal): a canary is caught if the model's outcome
        # matches expected (existing), OR the harness annotated the matched injected claim
        # as NOT grounded (binding couldn't ground the fabricated claim → caught anyway).
        if actual_outcome == expected_outcome:
            caught_via = "outcome"
        elif selected is not None and selected.get("grounded") is False:
            caught_via = "grounding"
        else:
            caught_via = None
        passed = caught_via is not None
        if not passed:
            logger.error(
                "CANARY FAILED: id=%s expected=%s actual=%s",
                canary.get("id"), expected_outcome, actual_outcome,
            )
        outcomes.append({
            "id": canary.get("id"),
            "expected_outcome": expected_outcome,
            "actual_outcome": actual_outcome,
            "passed": passed,
            "caught_via": caught_via,
            "rationale": rationale,
        })
    return outcomes


# ---------------------------------------------------------------------------
# Phase 4 — system prompt (#27 verbatim + additive addendum) & verdict schema
# ---------------------------------------------------------------------------

# ADDITIVE addendum (user-approved): strengthens ONLY claim_trace.evidence format
# + verification discipline. Touches NO rubric anchor, criterion, or
# score-computation rule — those stay verbatim in load_system_prompt() (#27).
_EVIDENCE_BINDING_ADDENDUM = """

=== EVIDENCE-BINDING (verification discipline) ===
This section adds verification requirements ONLY; every rubric anchor, criterion,
and score-computation rule above is unchanged and authoritative.
- Verify claims using ONLY the Read, Grep, and Glob tools. Interleave tool calls
  with reasoning, calling them as many times as needed, BEFORE the final verdict.
- NEVER score or count any claim as `verified` or `contradicted` unless an
  in-transcript tool result backs it. A claim you did not check with a tool is at
  most `unverified`.
- For every claim_trace entry whose outcome is `verified` or `contradicted`,
  `evidence` MUST be an OBJECT: {"file": "<repo-relative path you Read>",
  "line": <line number>, "snippet": "<exact substring copied verbatim from the
  tool's returned text>"}. The snippet MUST appear verbatim in what a Read of that
  file returned — never paraphrase or invent it.
- For `non_falsifiable` or `unverified` claims, `evidence` is a one-sentence
  string rationale (no file referent required).
- SEMANTIC claims (behavior, control/data flow — e.g. "eviction uses FIFO", "X
  calls Y", "this is dead code") require TRACING the actual control and data flow
  through the code you Read before marking them `verified` or `contradicted`.
  Confirming that a symbol, name, or string merely EXISTS is NOT verification of a
  behavioral claim — follow what the methods actually do to the data. (Example: a
  cache whose get() calls move_to_end() is LRU, not FIFO, even though both have an
  eviction step — read the access path, don't pattern-match the symbol.)
- ABSENCE claims (a symbol/function is never defined or never called) cannot cite a
  file+snippet. Verify them with Grep over the relevant scope and record evidence as
  an object {"grep_pattern": "<regex you searched>", "grep_scope": "<path/glob>",
  "result": "absent"} — backed by a Grep that returned no matches. An absence claim
  whose Grep actually returned matches, or that cites a Grep you never ran, is rejected.
- Fabricated or misquoted evidence is detected and the run is rejected.
"""

_FINAL_VERDICT_NUDGE = (
    "You have finished gathering evidence. Now output ONLY the final JSON verdict "
    "conforming exactly to the required schema. Every verified/contradicted claim's "
    "evidence must be bound to a tool result you actually obtained above."
)

# Loop bounds + caps (#26). Per-request timeout = openai_client.REQUEST_TIMEOUT_SEC
# (120s, set on the SDK client); per-RUN wall-clock cap below guards the agentic
# loop from spinning. Rate-limit backoff (#7) is applied in the retry loop.
MAX_VERIFY_ITERS = 24
MAX_JUDGE_ATTEMPTS = 2
PER_RUN_WALLCLOCK_SEC = 500
RATE_LIMIT_BACKOFF_SEC = 5.0

# Retryable failures (#22): structural (incl. EvidenceBindingError subclass) +
# confab + rate-limit + per-run timeout. A plain RuntimeError/ValueError is NOT
# retryable and propagates. Dead skwad retryables (MCP/freshness/port) dropped.
_RETRYABLE_VERIFY = (StructuralInvalidRun, ConfabulationDetected, RateLimited, JudgeRunTimeout)


def build_system_prompt(config_path: str | Path | None = None) -> str:
    """System prompt = VERBATIM persona (#27) + additive evidence-binding addendum.

    load_system_prompt() remains the unmodified persona; this appends only the
    verification-discipline section, so the full persona is still a substring.
    """
    base = load_system_prompt(config_path) if config_path else load_system_prompt()
    return base + _EVIDENCE_BINDING_ADDENDUM


def _score_only_criterion() -> dict:
    return {
        "type": "object", "additionalProperties": False,
        "properties": {"reasoning": {"type": "string"}, "score": {"type": "integer"}},
        "required": ["reasoning", "score"],
    }


def _count_criterion(*, with_justifications: bool = False) -> dict:
    props = {
        "reasoning": {"type": "string"},
        "score": {"type": "integer"},
        "verified_findings": {"type": "integer"},
        "unverified_findings": {"type": "integer"},
        "contradicted_findings": {"type": "integer"},
        "non_falsifiable_findings": {"type": "integer"},
    }
    req = list(props)
    if with_justifications:
        props["justifications"] = {"type": "array", "items": {"type": "string"}}
        req.append("justifications")
    return {"type": "object", "additionalProperties": False, "properties": props, "required": req}


# Content claim: a concrete file referent quoted from a Read.
_EVIDENCE_OBJECT = {
    "type": "object", "additionalProperties": False,
    "properties": {
        "file": {"type": "string"},
        "line": {"type": "integer"},
        "snippet": {"type": "string"},
    },
    "required": ["file", "line", "snippet"],
}

# Absence claim: a symbol/string proven ABSENT via Grep — no file to Read/quote,
# so evidence records the Grep instead (bound against an emitted no-match Grep).
_ABSENCE_EVIDENCE = {
    "type": "object", "additionalProperties": False,
    "properties": {
        "grep_pattern": {"type": "string"},
        "grep_scope": {"type": "string"},
        "result": {"type": "string", "enum": ["absent"]},
    },
    "required": ["grep_pattern", "grep_scope", "result"],
}

_CLAIM_ITEM = {
    "type": "object", "additionalProperties": False,
    "properties": {
        "claim_text": {"type": "string"},
        "outcome": {"type": "string",
                    "enum": ["verified", "unverified", "contradicted", "non_falsifiable"]},
        "tools_used": {"type": "array", "items": {"type": "string"}},
        # [0] content {file,line,snippet}; [1] absence {grep_pattern,grep_scope,result};
        # [2] string rationale (non_falsifiable/unverified). Order kept stable for the
        # guardrail test's anyOf[0] content assertion.
        "evidence": {"anyOf": [_EVIDENCE_OBJECT, _ABSENCE_EVIDENCE, {"type": "string"}]},
    },
    "required": ["claim_text", "outcome", "tools_used", "evidence"],
}

_VERIFICATION_SUMMARY = {
    "type": "object", "additionalProperties": False,
    "properties": {
        "claims_verified": {"type": "integer"},
        "claims_unverified": {"type": "integer"},
        "claims_contradicted": {"type": "integer"},
        "claims_non_falsifiable": {"type": "integer"},
        "verification_rate": {"type": "number"},
        "tool_calls_observed": {"type": "integer"},
    },
    "required": ["claims_verified", "claims_unverified", "claims_contradicted",
                 "claims_non_falsifiable", "verification_rate", "tool_calls_observed"],
}


def _review_schema() -> dict:
    criteria_props = {
        "issue_detection": _count_criterion(),
        "actionability": _score_only_criterion(),
        "severity_accuracy": _score_only_criterion(),
        "coverage": _count_criterion(),
        "signal_to_noise": _score_only_criterion(),
        "depth": _count_criterion(),
        "novel_substantive_findings": _count_criterion(with_justifications=True),
    }
    return {
        "type": "object", "additionalProperties": False,
        "properties": {
            "review_label": {"type": "string"},
            "criteria": {
                "type": "object", "additionalProperties": False,
                "properties": criteria_props, "required": list(criteria_props),
            },
            "total": {"type": "integer"},
            "verification_summary": _VERIFICATION_SUMMARY,
            "claim_trace": {"type": "array", "items": _CLAIM_ITEM},
        },
        "required": ["review_label", "criteria", "total", "verification_summary", "claim_trace"],
    }


def _build_verdict_schema() -> dict:
    return {
        "name": "review_quality_verdict",
        "strict": True,
        "schema": {
            "type": "object", "additionalProperties": False,
            "properties": {"review_a": _review_schema(), "review_b": _review_schema()},
            "required": ["review_a", "review_b"],
        },
    }


# The strict json_schema passed to response_format. Exposed for the guardrail test
# (claim_trace.evidence object branch requires file+line+snippet).
VERDICT_SCHEMA = _build_verdict_schema()


# ---------------------------------------------------------------------------
# Phase 4 — single agentic verification loop (#1) + verify gate
# ---------------------------------------------------------------------------

def _assistant_to_dict(msg) -> dict:
    """Serialize an SDK assistant message to a plain dict (role/content/tool_calls)
    for both the next API turn and the transcript that count/binding parse. Drops
    reasoning-only fields the API would reject on replay."""
    d: dict = {"role": "assistant"}
    if msg.content is not None:
        d["content"] = msg.content
    if msg.tool_calls:
        d["tool_calls"] = [
            {"id": tc.id, "type": "function",
             "function": {"name": tc.function.name, "arguments": tc.function.arguments}}
            for tc in msg.tool_calls
        ]
    return d


def _judge_create(client, **kwargs):
    """Wrap a chat.completions.create call, mapping OpenAI 429/timeout to the
    retryable RateLimited (#7) — replaces the skwad stderr 429-scraping."""
    try:
        return client.chat.completions.create(**kwargs)
    except (RateLimitError, APITimeoutError) as exc:
        raise RateLimited(f"OpenAI rate-limit/timeout: {exc}") from exc


def _run_openai_judge_once(client, model, system_prompt, user_prompt, sandbox,
                           *, max_iters: int = MAX_VERIFY_ITERS):
    """One judge run: a SINGLE agentic loop (tool_choice=auto, Read/Grep/Glob
    interleaved) until the model stops calling tools, THEN a final structured-output
    call (tools omitted) that forces the verdict (#1). Returns (parsed, transcript).

    Guards against a spinning loop with a per-run wall-clock cap (#26): raises
    JudgeRunTimeout if PER_RUN_WALLCLOCK_SEC is exceeded.
    """
    messages: list[dict] = [
        {"role": "system", "content": system_prompt},
        {"role": "user", "content": user_prompt},
    ]
    deadline = time.monotonic() + PER_RUN_WALLCLOCK_SEC
    for _ in range(max_iters):
        if time.monotonic() > deadline:
            raise JudgeRunTimeout(
                f"per-run wall-clock cap {PER_RUN_WALLCLOCK_SEC}s exceeded in tool loop"
            )
        resp = _judge_create(
            client, model=model, messages=messages, tools=TOOL_SCHEMAS, tool_choice="auto",
        )
        choice = resp.choices[0]
        msg = choice.message
        messages.append(_assistant_to_dict(msg))
        if choice.finish_reason != "tool_calls" or not msg.tool_calls:
            break
        for tc in msg.tool_calls:
            try:
                arguments = json.loads(tc.function.arguments or "{}")
            except (json.JSONDecodeError, TypeError):
                arguments = {}
            result = dispatch_tool_call(sandbox, tc.function.name, arguments)
            messages.append({"role": "tool", "tool_call_id": tc.id, "content": result})

    if time.monotonic() > deadline:
        raise JudgeRunTimeout(
            f"per-run wall-clock cap {PER_RUN_WALLCLOCK_SEC}s exceeded before verdict"
        )
    # Force the structured verdict at the end — tools omitted entirely (so no
    # tool_choice either: OpenAI rejects tool_choice when tools aren't specified).
    messages.append({"role": "user", "content": _FINAL_VERDICT_NUDGE})
    final = _judge_create(
        client, model=model, messages=messages,
        response_format={"type": "json_schema", "json_schema": VERDICT_SCHEMA},
    )
    content = final.choices[0].message.content
    messages.append({"role": "assistant", "content": content})
    return _parse_json_output(content), messages


def _write_transcript_artifact(artifact_path, attempts_log) -> None:
    """Persist per-attempt {parsed, transcript, verify_error} to a stable sidecar
    file. Best-effort — never raises (artifact failure must not sink a run). Written
    BEFORE verification so a verification rejection still leaves the transcript on
    disk for diagnosis (e.g. emitted Grep patterns vs cited grep_pattern)."""
    if not artifact_path:
        return
    try:
        with open(artifact_path, "w") as f:
            json.dump(attempts_log, f, indent=2)
    except OSError as exc:
        logger.warning("could not write transcript artifact %s: %s", artifact_path, exc)


def _run_and_verify_openai(client, model, system_prompt, diff, review_a, review_b, sandbox,
                           *, max_attempts: int = MAX_JUDGE_ATTEMPTS, artifact_path=None):
    """Run the loop, then verify in the #11b/M3 order: structural validation FIRST,
    then evidence-binding (#1), then the confab gate (#3/#4). Retries the retryable
    verification failures (structural/confab/rate-limit/timeout); rate-limit backs
    off. Returns (parsed, transcript, tool_calls).

    If ``artifact_path`` is given, each attempt's transcript is persisted there
    BEFORE verification, so a rejection still leaves the transcript for diagnosis.
    """
    user_prompt = build_user_prompt(diff, review_a, review_b)
    last_exc: Exception | None = None
    attempts_log: list[dict] = []
    for attempt in range(max_attempts):
        entry = {"attempt": attempt, "parsed": None, "transcript": None, "verify_error": None}
        attempts_log.append(entry)
        try:
            parsed, transcript = _run_openai_judge_once(
                client, model, system_prompt, user_prompt, sandbox,
            )
            # Persist the raw transcript BEFORE verification — a binding/confab
            # rejection below must NOT discard what the model actually emitted.
            entry["parsed"] = parsed
            entry["transcript"] = transcript
            _write_transcript_artifact(artifact_path, attempts_log)

            # #11b/M3: structural validation BEFORE confab (a malformed schema would
            # silently default verified=0 and bypass the confab gate).
            _validate_response_structure(parsed)
            tool_calls = count_emitted_tool_calls(transcript)          # #2/M4
            _backfill_tool_calls_observed(parsed, tool_calls, tool_calls)  # #10
            _warn_trace_divergence(parsed, tool_calls)                 # #8
            _check_evidence_binding(parsed, transcript)                # #1
            if tool_calls:
                _check_confabulation(parsed, tool_calls)               # #3
            elif _sum_verified_in_output(parsed) > 0:
                # #4: zero tool calls but verified claims → never silently score.
                raise ConfabulationDetected(
                    "verification gate: zero tool calls but verified claims present"
                )
            return parsed, transcript, tool_calls
        except RateLimited as exc:
            # #7: backoff (scaled by attempt) before retrying a rate-limited run.
            last_exc = exc
            entry["verify_error"] = f"{type(exc).__name__}: {exc}"
            _write_transcript_artifact(artifact_path, attempts_log)
            if attempt + 1 < max_attempts:
                logger.warning("judge rate-limited, backing off then retrying: %s", exc)
                time.sleep(RATE_LIMIT_BACKOFF_SEC * (attempt + 1))
        except _RETRYABLE_VERIFY as exc:
            last_exc = exc
            entry["verify_error"] = f"{type(exc).__name__}: {exc}"
            _write_transcript_artifact(artifact_path, attempts_log)
            logger.warning(
                "judge verify failed (%s), attempt %d/%d: %s",
                type(exc).__name__, attempt + 1, max_attempts, exc,
            )
    assert last_exc is not None
    raise last_exc


# ---------------------------------------------------------------------------
# Phase 4 — orchestration (replaces the skwad-subprocess judge invocation)
# ---------------------------------------------------------------------------

def run_single_judge_task(task: dict) -> dict:
    """Run ONE judge invocation (one PR × one A/B run) via the OpenAI loop. Never
    raises for a judge failure — the failure is captured in status/error so a single
    bad run doesn't sink the PR (#23). Shape mirrors the skwad judge's task record.
    """
    a_system: System = task["a_system"]
    b_system: System = task["b_system"]
    i = task["run_index"]
    pr_number = task["pr_number"]
    out = {
        "pr_number": pr_number,
        "run_index": i,
        "ab_assignment": [a_system, b_system],
        "status": "failed",
        "resolved": None,
        "run_record": None,
        "canary_outcomes": [],
        "counter_increment": None,
        "duration_seconds": 0.0,
        "error": None,
        "out_of_worktree_reads": [],  # G4: surfaced regardless of flag/quarantine
    }
    t_start = time.perf_counter()
    logger.info("Codex judge PR#%s run %d: A=%s B=%s", pr_number, i, a_system, b_system)
    # Stable sidecar so the verdict+trace survive even a verification rejection (which
    # returns status=failed below) — needed to diagnose emitted vs cited commands.
    transcript_path = os.path.join(
        task["run_dir"], task["run_record_name"].replace(".json", "_transcript.json")
    )
    # Codex artifacts (verdict.json, --json stream) MUST live OUTSIDE the worktree (G3).
    out_dir = task.get("out_dir") or os.path.join(task["run_dir"], f"codex_run{i}")
    try:
        verdict, trace_jsonl, parsed_trace = _run_and_verify_codex(
            task["diff"], task["review_a"], task["review_b"],
            task["repo_path"], out_dir,
            model=task.get("model", CODEX_DEFAULT_MODEL),
            config_path=task.get("config_path"),
            artifact_path=transcript_path,
        )
    except Exception as e:
        out["duration_seconds"] = time.perf_counter() - t_start
        out["error"] = f"{type(e).__name__}: {e}"
        out["counter_increment"] = _EXC_COUNTER_KEY.get(type(e))
        logger.warning("Codex judge PR#%s run %d failed: %s", pr_number, i, out["error"])
        return out

    duration = time.perf_counter() - t_start
    oow_reads = parsed_trace.get("out_of_worktree_reads", [])
    out["out_of_worktree_reads"] = oow_reads  # surface for manifest aggregation (Phase C)
    # G4 (3): quarantine the run from scoring if it read outside the worktree AND the
    # policy is `fail` (untrusted PRs). Default `flag` records but still scores
    # (semi-trusted pilot; G1 already hard-rejects cited-outside paths in binding).
    if oow_reads and task.get("on_out_of_worktree_read") == "fail":
        out["duration_seconds"] = duration
        out["error"] = f"OutOfWorktreeRead: quarantined (read outside worktree: {oow_reads})"
        out["counter_increment"] = "out_of_worktree_read_quarantines"
        logger.warning("Codex judge PR#%s run %d QUARANTINED (out-of-worktree reads: %s)",
                       pr_number, i, oow_reads)
        return out

    resolved = _unswap(verdict, a_system, b_system)
    canary_outcomes = []
    if task.get("canary_injections"):
        canary_outcomes = _check_canary_outcomes(
            verdict, task["canary_injections"], a_system, b_system, task["pr_data"]
        )
    bundling = _count_bundled_command_events(parsed_trace)  # observability, not a gate
    # Soft-signal grounding rollup (per system) + LOUD low-grounding alarm. Mirrors the
    # out-of-worktree-read flagging: per-run data on the out dict for manifest aggregation,
    # plus an immediate logger.warning so organic fabrication is never silent.
    grounding = {sys_: _grounding_stats(resolved.get(sys_, {})) for sys_ in ("skwad", "claude_ci")}
    low_grounding = {}
    for sys_, gs in grounding.items():
        rate = gs["grounding_rate"]
        if rate is not None and rate < _GROUNDING_RATE_FLOOR:
            low_grounding[sys_] = gs
            logger.warning(
                "LOW GROUNDING PR#%s run %d %s: grounding_rate=%.2f (%d/%d grounded) < floor %.2f",
                pr_number, i, sys_, rate, gs["grounded"], gs["grounding_eligible"],
                _GROUNDING_RATE_FLOOR,
            )
    run_record = {
        "run": i,
        "ab_assignment": [a_system, b_system],
        "raw_response": verdict,
        "resolved": resolved,
        "command_count": len(parsed_trace.get("commands", [])),
        "out_of_worktree_reads": parsed_trace.get("out_of_worktree_reads", []),  # G4 flag
        "bundled_command_events": bundling["bundled_command_events"],
        "max_bundled_subcommands": bundling["max_bundled_subcommands"],
        "grounding": grounding,
        "duration_seconds": duration,
    }
    run_path = os.path.join(task["run_dir"], task["run_record_name"])
    with open(run_path, "w") as f:
        json.dump(run_record, f, indent=2)

    out.update({
        "status": "ok",
        "resolved": resolved,
        "run_record": run_record,
        "canary_outcomes": canary_outcomes,
        "bundled_command_events": bundling["bundled_command_events"],
        "max_bundled_subcommands": bundling["max_bundled_subcommands"],
        "low_grounding": low_grounding,
        "duration_seconds": duration,
    })
    return out


def prepare_pr_judge_tasks(
    pr_data: dict,
    skwad_review: str,
    claude_ci_review: str,
    repo_path: str,
    seed: int,
    run_dir: str,
    *,
    model: str | None = None,
    config_path: str | None = None,
    canary_injections: list[dict] | None = None,
    on_out_of_worktree_read: str = "flag",
) -> tuple[list[dict], list[tuple[System, System]]]:
    """Build the 3 per-run Codex judge task specs for one PR (no judge invocation yet).

    A/B order + reviews are derived from the PER-PR seed only (#15). Spec building is
    FILESYSTEM-FREE — Codex is invoked per-task in run_single_judge_task against
    ``repo_path`` (the per-PR WORKTREE), writing artifacts to ``out_dir`` OUTSIDE it
    (G3). Returns (tasks, assignments).
    """
    model = model or CODEX_DEFAULT_MODEL
    os.makedirs(run_dir, exist_ok=True)
    pr_number = pr_data["pr_number"]
    diff = pr_data.get("diff", "")
    assignments = derive_ab_assignments(seed)
    reviews: dict[System, str] = {"skwad": skwad_review, "claude_ci": claude_ci_review}
    if canary_injections:
        reviews = _apply_canary_injections(reviews, canary_injections, pr_data)

    tasks: list[dict] = []
    for i, (a_system, b_system) in enumerate(assignments, start=1):
        tasks.append({
            "pr_number": pr_number,
            "run_index": i,
            "a_system": a_system,
            "b_system": b_system,
            "diff": diff,
            "review_a": reviews[a_system],
            "review_b": reviews[b_system],
            "canary_injections": canary_injections or [],
            "pr_data": pr_data,
            "run_dir": run_dir,
            "run_record_name": f"judge_pr{pr_number}_run{i}.json",
            "repo_path": repo_path,
            "out_dir": os.path.join(run_dir, f"codex_run{i}"),
            "model": model,
            "config_path": config_path,
            "on_out_of_worktree_read": on_out_of_worktree_read,
        })
    return tasks, assignments


def score_paired_reviews(
    pr_data: dict,
    skwad_review: str,
    claude_ci_review: str,
    repo_path: str,
    seed: int,
    run_dir: str,
    *,
    model: str | None = None,
    config_path: str | None = None,
    canary_injections: list[dict] | None = None,
    pilot_counters: dict | None = None,
    on_out_of_worktree_read: str = "flag",
) -> dict:
    """Score both reviews for a PR via counterbalanced A/B Codex judge runs (SEQUENTIAL).

    Each run invokes a grounded `codex exec` against ``repo_path`` (the PER-PR worktree
    checkout — NOT the base clone), then votes via finalize_pr_runs. The parallel
    orchestrator (main.py) calls prepare_pr_judge_tasks + run_single_judge_task
    directly; this shares those building blocks.
    """
    model = model or CODEX_DEFAULT_MODEL

    tasks, assignments = prepare_pr_judge_tasks(
        pr_data, skwad_review, claude_ci_review, repo_path, seed, run_dir,
        model=model, config_path=config_path, canary_injections=canary_injections,
        on_out_of_worktree_read=on_out_of_worktree_read,
    )
    run_results = []
    for task in tasks:
        res = run_single_judge_task(task)
        if pilot_counters is not None and res.get("counter_increment"):
            key = res["counter_increment"]
            pilot_counters[key] = pilot_counters.get(key, 0) + 1
        run_results.append(res)

    return finalize_pr_runs(
        pr_data["pr_number"], run_results, assignments, run_dir,
        canary_injections=canary_injections,
    )


# ===========================================================================
# Route C — Codex (`codex exec`) grounded verifier (C1: wrapper + trace parser)
# ===========================================================================
# Replaces the in-process two-phase OpenAI loop, whose separate from-memory verdict
# call fabricated evidence. Codex runs the tool loop AND emits the schema-constrained
# verdict in ONE grounded context. The OpenAI loop above is RETAINED, frozen, as a
# fallback (not deleted). Evidence-binding (re-framed over Codex's shell trace, C3)
# stays as the backstop.

CODEX_DEFAULT_MODEL = "gpt-5.4"

# Tools whose output can be trusted as file content (read/search file inspection)…
_CODEX_SEARCH_TOOLS = {"rg", "grep", "egrep", "fgrep", "ag", "ack"}
_CODEX_READ_TOOLS = {"cat", "bat", "nl", "sed", "head", "tail", "less", "view", "more"}
# …vs commands that SYNTHESIZE content (an echo'd snippet must NOT count as evidence).
_CODEX_NONREAD_TOOLS = {"echo", "printf", "python", "python3", "perl", "ruby", "node",
                        "yes", "seq"}
_CODEX_TRANSFORM_TOOLS = {"tr", "awk"}  # stream editors taint attribution
# Inert navigation/no-ops — NEUTRAL: they neither read a file faithfully nor synthesize
# content, so they must NOT taint a pipe NOR contribute paths. `ls` is EXCLUDED on
# purpose (it emits filenames that could bind as a fabricated snippet → stays tainted).
_CODEX_INERT_NAV_TOOLS = {"cd", "pwd", "true", ":"}

# `codex exec` wraps each command as `/bin/zsh -lc '<inner>'` — single OR double
# quoted (double when the inner command itself contains single quotes, e.g. sed).
_SH_WRAP_RE = re.compile(r"^/bin/\w+sh\s+-l?c\s+(['\"])(.*)\1$", re.S)


class CodexExecError(RuntimeError):
    """Raised when `codex exec` fails (nonzero exit / missing or invalid verdict.json
    / subprocess error). Wired into the retryable set in C4."""


# DEFAULT-DENY env allowlist for the agent shell (Reviewer C4 fix). The parent env
# holds OPENAI_API_KEY / ANTHROPIC_API_KEY / AWS_* / *_TOKEN / *_SECRET; a blanket
# `dict(os.environ)` would let a prompt-injected `echo $AWS_SECRET_ACCESS_KEY`
# exfiltrate them into the trace WITHOUT a file read (so the out-of-worktree-read
# scan can't catch it). Only these provably-needed, non-secret vars pass; everything
# else is absent. Codex auth comes from CODEX_HOME/auth.json — NOT env (Explorer-
# proven) — so it still authenticates. Add a var here only if codex/rg/git need it.
_CODEX_ENV_ALLOWLIST = frozenset({
    "PATH", "LANG", "LANGUAGE", "TERM", "TZ", "USER", "LOGNAME", "SHELL",
})


def _build_codex_env(scratch_home: str) -> dict:
    """Build the MINIMAL child env for `codex exec` (default-deny; see allowlist).

    HOME/TMPDIR → per-run scratch (tilde-read prevent, partial — see run_codex_exec).
    CODEX_HOME → the real ~/.codex (auth + config) so codex authenticates despite the
    HOME override. NO secrets carried over.
    """
    real_home = os.environ.get("HOME") or os.path.expanduser("~")
    real_codex_home = os.environ.get("CODEX_HOME") or os.path.join(real_home, ".codex")
    env = {k: os.environ[k] for k in _CODEX_ENV_ALLOWLIST if k in os.environ}
    env.update({k: v for k, v in os.environ.items() if k.startswith("LC_")})  # locale
    env["HOME"] = scratch_home
    env["TMPDIR"] = scratch_home
    env["CODEX_HOME"] = real_codex_home
    return env


def run_codex_exec(prompt: str, worktree: str, schema_path: str, out_dir: str,
                   *, model: str = CODEX_DEFAULT_MODEL,
                   timeout: float = PER_RUN_WALLCLOCK_SEC) -> tuple[dict, str]:
    """Run a grounded `codex exec` judge. Returns (verdict_dict, trace_jsonl).

    The SINGLE patchable mock seam (mirrors build_client) — tests stub it with a
    canned (verdict, --json) pair so the offline suite makes ZERO live Codex calls.

    Guardrails baked in: G2 — stdin=DEVNULL (codex exec hangs on stdin otherwise);
    G3 — verdict.json + the --json stream live in ``out_dir`` OUTSIDE the worktree
    (else the agent's `rg .` matches its own trace file).
    """
    os.makedirs(out_dir, exist_ok=True)
    verdict_path = os.path.join(out_dir, "verdict.json")
    cmd = [
        "codex", "exec",
        "--skip-git-repo-check",
        "--output-schema", schema_path,
        "--json",
        "-C", worktree,
        "-m", model,
        "-s", "read-only",
        "-o", verdict_path,
        prompt,
    ]
    # G4 PREVENT — PARTIAL defense-in-depth (C4; Explorer-verified live). `-s read-only`
    # blocks writes but NOT reads outside `-C`, and the PR diff is untrusted (prompt-
    # injection: "read ~/.aws/credentials"). Point HOME/TMPDIR at a per-run scratch dir
    # so a TILDE-relative read lands in empty scratch, not real secrets. CODEX_HOME →
    # the real ~/.codex (captured BEFORE the HOME override) keeps Codex's auth working
    # (codex reads ~/.codex/auth.json; CODEX_HOME is respected independent of HOME).
    #
    # ⚠️ This closes ONLY the tilde vector. It does NOT stop ABSOLUTE-path reads
    # (`cat /Users/x/.aws/credentials`) — read-only doesn't confine reads — and it even
    # re-exposes the real ~/.codex/auth.json via CODEX_HOME. The REAL backstops against
    # secret exfil influencing a score are: (a) the out-of-worktree-read detect +
    # `--on-out-of-worktree-read=fail` quarantine, and (b) evidence-binding's
    # worktree-containment (G1) — an out-of-worktree path can't be cited as evidence.
    scratch_home = os.path.join(out_dir, "scratch_home")
    os.makedirs(scratch_home, exist_ok=True)
    env = _build_codex_env(scratch_home)
    try:
        result = subprocess.run(
            cmd, stdin=subprocess.DEVNULL, env=env,
            capture_output=True, text=True, timeout=timeout,
        )
    except subprocess.TimeoutExpired as exc:
        raise CodexExecError(f"codex exec timed out after {timeout}s") from exc
    except OSError as exc:
        raise CodexExecError(f"codex exec could not run: {exc}") from exc

    trace_jsonl = result.stdout or ""  # --json JSONL events stream on stdout
    if result.returncode != 0:
        raise CodexExecError(
            f"codex exec exit={result.returncode}: {(result.stderr or '')[:500]}"
        )
    try:
        with open(verdict_path) as f:
            verdict_dict = json.load(f)
    except (OSError, json.JSONDecodeError) as exc:
        raise CodexExecError(f"codex produced no/invalid verdict.json: {exc}") from exc
    return verdict_dict, trace_jsonl


def _unwrap_codex_cmd(command: str) -> str:
    """Strip the `/bin/zsh -lc '<inner>'` (or "<inner>") wrapper; fall back to raw."""
    m = _SH_WRAP_RE.match((command or "").strip())
    return m.group(2) if m else (command or "")


def _looks_like_path(tok: str) -> bool:
    """Heuristic: a token that names a file (has an extension or a path separator),
    excluding bare dir scopes like '.'."""
    if not tok or tok == ".":
        return False
    if "/" in tok and not tok.endswith("/"):
        return True
    return bool(re.search(r"\.\w+$", tok))


# sed commands whose script faithfully REPRODUCES file content (print/list/line-num).
# Everything else fails closed to "transform" — a mutated stdout must not bind to a file.
_SED_READSAFE_CMDS = set("pPl=")


def _sed_strip_address(script: str) -> str:
    """Strip a leading 1- or 2-address selector (+ optional `!`) from a sed script,
    returning the remainder starting at the COMMAND char. Handles `/regex/`, `\\cregexc`,
    numeric / `$` / `first~step` / `+N` addresses, and `addr1,addr2` ranges."""
    def strip_one(t: str) -> str:
        t = t.lstrip()
        if not t:
            return t
        if t[0] == "/":  # /regex/ — consume to next UNescaped /
            i = 1
            while i < len(t):
                if t[i] == "\\" and i + 1 < len(t):
                    i += 2; continue
                if t[i] == "/":
                    return t[i + 1:]
                i += 1
            return ""  # unterminated → consume all
        if t[0] == "\\" and len(t) > 1:  # \cregexc — custom delimiter
            delim = t[1]; i = 2
            while i < len(t):
                if t[i] == "\\" and i + 1 < len(t):
                    i += 2; continue
                if t[i] == delim:
                    return t[i + 1:]
                i += 1
            return ""
        m = re.match(r"^[\d$][\d~+\-]*", t)  # number / $ / first~step / +N
        return t[m.end():] if m else t

    rest = strip_one(script).lstrip()
    if rest.startswith(","):
        rest = strip_one(rest[1:]).lstrip()
    while rest.startswith("!"):
        rest = rest[1:].lstrip()
    return rest


def _sed_script_is_transform(script: str) -> bool:
    """True if a sed SCRIPT (NOT a file arg) mutates the stream. Read-only print/list/
    line-number scripts (`1,8p`, `/pat/p`, `=`) faithfully reproduce file content;
    everything else (s///, y///, d, c, a, i, r, w, hold-space ops, grouping) fails
    closed to transform so model-controlled mutated stdout can't bind to a real file."""
    for part in re.split(r"[;\n]", script):
        cmd = _sed_strip_address(part.strip())
        if cmd and cmd[0] not in _SED_READSAFE_CMDS:
            return True
    return False


def _sed_is_transform(tokens: list[str]) -> bool:
    """Whether a sed invocation MUTATES content (→ unattributable) vs a faithful read.

    Inspects ONLY the sed SCRIPT position — grammar `sed [flags] {script | -e script |
    -f file} [files...]` — NEVER the trailing FILE args. This fixes the false positive
    on `sed -n '200,340p' src/stores/apps.ts` (old code saw `s/` inside `stores/`) while
    still catching real transforms. `-f scriptfile` (unreadable script) and `-i`/
    `--in-place` (rewrites the file) fail closed to transform.
    """
    scripts: list[str] = []
    i, n = 0, len(tokens)
    saw_script_flag = False
    while i < n:
        t = tokens[i]
        if t in ("-e", "--expression"):
            saw_script_flag = True
            if i + 1 < n:
                scripts.append(tokens[i + 1]); i += 2; continue
            i += 1; continue
        if t.startswith("-e") and len(t) > 2:  # -e's/a/b/' bundled
            saw_script_flag = True; scripts.append(t[2:]); i += 1; continue
        if t in ("-f", "--file") or t.startswith("-f"):  # external script → fail closed
            return True
        if t == "--in-place" or t.startswith("--in-place=") or re.match(r"^-[a-zA-Z]*i", t):
            return True  # in-place editing rewrites the file
        i += 1
    if not saw_script_flag:  # script = FIRST non-flag token; the rest are FILES
        for t in tokens:
            if not t.startswith("-"):
                scripts.append(t); break
    return any(_sed_script_is_transform(s) for s in scripts)


def _split_shell_segments(command: str, statement_only: bool = False) -> tuple[list[str], bool]:
    """Split a compound command into segments on UNQUOTED `|`, `||`, `&&`, `;`, `&`, and
    newline — the boundaries between distinct commands whose taint must be OR'd. Tracks
    single/double/escaped quotes so a `|` INSIDE a quoted regex (`rg 'a|b' f`) is NOT a
    boundary. Returns (segments, balanced); on UNBALANCED quoting it FAILS SAFE — returns
    ([whole_command], False) so the caller over-merges and taints, never dropping a
    taint segment from the OR.

    statement_only=True splits ONLY on STATEMENT separators (`\\n`, `;`, `&&`, `||`, `&`)
    and treats a single `|` PIPE as part of one command (a pipeline is one command). Used
    by the bundling counter; the default (False) keeps the all-operators behavior the
    taint/classifier callers rely on."""
    segs: list[str] = []
    buf: list[str] = []
    i, n = 0, len(command)
    quote: str | None = None
    while i < n:
        ch = command[i]
        if quote:
            buf.append(ch)
            if ch == "\\" and quote == '"' and i + 1 < n:
                buf.append(command[i + 1]); i += 2; continue
            if ch == quote:
                quote = None
            i += 1; continue
        if ch == "\\" and i + 1 < n:
            buf.append(ch); buf.append(command[i + 1]); i += 2; continue
        if ch in ("'", '"'):
            quote = ch; buf.append(ch); i += 1; continue
        if ch in (";", "\n", "|", "&"):
            two = command[i:i + 2]
            if statement_only and ch == "|" and two != "||":
                buf.append(ch); i += 1; continue  # single pipe is NOT a statement boundary
            segs.append("".join(buf)); buf = []
            i += 2 if two in ("&&", "||") else 1
            continue
        buf.append(ch); i += 1
    if quote is not None:  # unbalanced quoting → fail safe: one segment, force taint
        return [command], False
    segs.append("".join(buf))
    return [s for s in segs if s.strip()], True


def _classify_codex_segment(seg: str):
    """Classify one segment → (is_search, is_readlike, is_transform, is_synthesis,
    patterns, files). synthesis = a tool that PRODUCES/DERIVES content rather than
    faithfully reading a file (the _CODEX_NONREAD_TOOLS class, xargs/eval, and any
    UNKNOWN tool — fail closed)."""
    try:
        toks = shlex.split(seg)
    except ValueError:
        toks = seg.split()
    if not toks:
        return False, False, False, False, [], []
    tool = toks[0]
    rest = toks[1:]
    if tool == "git" and rest and rest[0] in ("grep", "show"):
        tool, rest = "git " + rest[0], rest[1:]
    base = "git grep" if tool == "git grep" else os.path.basename(tool)

    if base in _CODEX_SEARCH_TOOLS or base == "git grep":
        nonflag = [t for t in rest if not t.startswith("-")]
        pattern = nonflag[0:1]  # first non-flag token = the search pattern
        files = [t for t in nonflag[1:] if _looks_like_path(t)]
        return True, True, False, False, pattern, files
    if base in _CODEX_READ_TOOLS:
        if base == "sed" and _sed_is_transform(rest):
            return False, False, True, False, [], []
        files = [t for t in rest if not t.startswith("-") and _looks_like_path(t)]
        return False, True, False, False, [], files
    if base in _CODEX_TRANSFORM_TOOLS:
        # awk with an action block, or tr → mutates the stream → taint
        if base == "tr" or any("{" in t for t in rest):
            return False, False, True, False, [], []
        return False, False, False, False, [], []  # awk '/pat/' passthrough: neutral
    if base in _CODEX_INERT_NAV_TOOLS:
        # cd/pwd/true/: — inert. NEUTRAL: don't taint the pipe, don't contribute paths,
        # so `pwd && cat f.go` / `cd dir && sed -n '10,40p' f.go` stay readlike.
        return False, False, False, False, [], []
    if base in ("xargs", "eval"):
        # xargs/eval run an unmodeled SUBcommand → conservatively taint as synthesis.
        # TRADE-OFF: legit `xargs cat`/`xargs head` reads get rejected; deliberate, vs
        # the risk of `xargs python`/`eval` laundering a mutation onto a real file.
        return False, False, False, True, [], []
    # echo/printf/python -c / any UNKNOWN tool → synthesis, NOT a faithful read.
    return False, False, False, True, [], []


def _attribute_output(output: str, read_paths: list[str], is_search: bool) -> list[str]:
    """Which file(s) the command's output is TRUSTED to come from (anti mix-and-match).

    rg/grep: path-prefixed match lines (`path:line:text`) attribute to that path; a
    no-prefix `line:text` (single explicit file arg) attributes to that file. Pure
    read tools (cat/sed/head): the whole output is the file arg(s)."""
    attributed: set[str] = set()
    if is_search:
        saw_no_prefix = False
        for line in output.splitlines():
            m = re.match(r"^([^:]+):(\d+):", line)
            if m and not m.group(1).isdigit():
                attributed.add(m.group(1))
            elif re.match(r"^\d+:", line):
                saw_no_prefix = True
        if saw_no_prefix:
            attributed.update(read_paths)
    else:
        # Pure read: the whole output is attributed to the file arg(s).
        # KNOWN PRE-EXISTING GAP (Q2 follow-up, NOT fixed here): a compound pure-read
        # like `cat a.py ; cat b.py` unions read_paths, so a snippet from a.py could bind
        # to a cite of b.py. Fix = per-segment attribution for pure reads (C3-for-reads),
        # mirroring the multi-file search per-line attribution above.
        attributed.update(read_paths)
    return sorted(attributed)


def _classify_codex_cmd(inner: str, output: str):
    """Classify a full (possibly piped/compound) command → the per-command binding fields.

    SECURITY INVARIANT: a transform OR synthesis tool ANYWHERE in the pipe/compound
    (`|`, `&&`, `||`, `;`, `&`, newline) means the final stdout no longer faithfully
    reflects any single file → the whole command is forced non-readlike with attribution
    dropped. The taint OR is applied AFTER unioning read_paths across all segments, so a
    later taint segment can't be missed. Unbalanced quoting also taints (fail closed)."""
    segments, balanced = _split_shell_segments(inner)
    is_search = is_readlike = transforming = is_synthesis = False
    searched_symbols: list[str] = []
    read_paths: list[str] = []
    for seg in segments:
        s_search, s_read, s_transform, s_synth, patterns, files = _classify_codex_segment(seg.strip())
        is_search = is_search or s_search
        is_readlike = is_readlike or s_read
        transforming = transforming or s_transform
        is_synthesis = is_synthesis or s_synth
        searched_symbols += patterns
        read_paths += files

    # STRUCTURAL TAINT — applied AFTER the read_paths union (NOT inside the loop). A
    # mutated/synthesized stage, or quoting we couldn't safely split, means the output
    # is unattributable → not read-like for binding (fail safe).
    if transforming or is_synthesis or not balanced:
        return {
            "is_search": is_search, "is_readlike": False,
            "attributed_paths": [], "searched_symbols": searched_symbols,
            "read_paths": read_paths,
        }
    attributed = _attribute_output(output, read_paths, is_search) if is_readlike else []
    return {
        "is_search": is_search, "is_readlike": is_readlike,
        "attributed_paths": attributed, "searched_symbols": searched_symbols,
        "read_paths": read_paths,
    }


def _parse_codex_trace(jsonl: str) -> dict:
    """Parse a `codex exec --json` JSONL stream into the binding-ready trace shape.

    Consumes ONLY `item.completed` / `command_execution` events (item.started is a
    useless duplicate; agent_message prose is ignored — the verdict comes from the
    -o file). Records `exit_code` verbatim as `exit`; the binding (C3) branches on
    it, NOT on `status` (rg no-match is status:"failed" but exit 1).
    """
    commands: list[dict] = []
    for line in jsonl.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if event.get("type") != "item.completed":
            continue
        item = event.get("item") or {}
        if item.get("type") != "command_execution":
            continue
        inner = _unwrap_codex_cmd(item.get("command") or "")
        output = item.get("aggregated_output") or ""
        fields = _classify_codex_cmd(inner, output)
        commands.append({
            "cmd": inner,
            "output": output,
            "exit": item.get("exit_code"),
            **fields,
        })
    return {"commands": commands}


def _emit_verdict_schema(out_dir: str) -> str:
    """Write the BARE JSON Schema (`VERDICT_SCHEMA['schema']`, NOT the
    {name,strict,schema} wrapper) for `codex exec --output-schema`. Returns its path."""
    os.makedirs(out_dir, exist_ok=True)
    path = os.path.join(out_dir, "verdict_schema.json")
    with open(path, "w") as f:
        json.dump(VERDICT_SCHEMA["schema"], f, indent=2)
    return path


# CODEX-ONLY. Codex bundles up to ~10 commands into one command_execution event,
# merging their outputs under a single exit — which corrupts absence-empty detection and
# per-file attribution. This instruction asks for one command per tool call. NOT added to
# build_system_prompt/_EVIDENCE_BINDING_ADDENDUM (those are SHARED with the OpenAI judge,
# which uses a one-per-call Read/Grep tool-loop and never bundles — adding it there would
# break OpenAI-prompt parity). The fail-safe binding rejection remains the backstop.
_CODEX_COMMAND_DISCIPLINE = """=== COMMAND DISCIPLINE (one command per tool call) ===
Run EXACTLY ONE shell command per tool call. Do NOT combine multiple commands in
a single call with newlines, `;`, `&&`, or `||`. (A single pipeline with `|` —
e.g. `nl file | sed -n '10,40p'` or `rg foo | head` — counts as ONE command and is fine.)
- Each ABSENCE search MUST be its own command. If you search for several things in
  one call, their outputs merge and an empty result can't be attributed — your
  absence evidence will be REJECTED. Run the exact pattern you record as evidence
  as its own search, so its empty result is recorded individually.
- Each file READ you cite MUST be its own command, so its output is attributed to that file alone."""


def _build_codex_prompt(diff: str, review_a: str, review_b: str,
                        config_path: str | None = None) -> str:
    """Single grounded prompt for `codex exec`: the VERBATIM rubric persona (#27) +
    evidence-binding addendum, the codex-only command-discipline block, then the task
    (diff + Review A + Review B). Codex has no separate system role — rubric + task share
    one prompt; the verdict is emitted schema-constrained to the -o file from WITHIN the
    grounded tool-using turn (no separate from-memory call → no fabrication surface)."""
    return (
        build_system_prompt(config_path) + "\n\n"
        + _CODEX_COMMAND_DISCIPLINE + "\n\n"
        + build_user_prompt(diff, review_a, review_b)
    )


def _run_codex_judge_once(diff: str, review_a: str, review_b: str, worktree: str,
                          out_dir: str, *, model: str = CODEX_DEFAULT_MODEL,
                          config_path: str | None = None) -> tuple[dict, str]:
    """One grounded Codex judge run: emit the bare schema, build the single prompt,
    call run_codex_exec (the mock seam). Returns (verdict_dict, trace_jsonl). The
    Codex analogue of _run_openai_judge_once (which is retained, frozen, as fallback)."""
    schema_path = _emit_verdict_schema(out_dir)
    prompt = _build_codex_prompt(diff, review_a, review_b, config_path)
    return run_codex_exec(prompt, worktree, schema_path, out_dir, model=model)


def _path_in_worktree(cited: str, worktree_real: str) -> bool:
    """G1: does a cited path resolve INSIDE the per-PR worktree? Codex `-s read-only`
    blocks writes but NOT reads outside `-C`, so binding can't trust the sandbox —
    it must reject any cited file resolving outside the worktree itself."""
    if not cited:
        return False
    p = cited if os.path.isabs(cited) else os.path.join(worktree_real, cited)
    real = os.path.realpath(p)
    return real == worktree_real or real.startswith(worktree_real + os.sep)


def _is_empty_output(output) -> bool:
    return not (output or "").strip()


def _count_bundled_command_events(parsed_trace: dict) -> dict:
    """Observability (NOT a gate): how many command_execution events bundled ≥2
    sub-commands via a STATEMENT separator (`\\n`/`;`/`&&`/`||`/`&`). A single `|`
    pipeline counts as ONE command (not bundling). Codex bundling merges outputs under a
    single exit, corrupting absence-empty detection + per-file attribution; this lets us
    SEE the residual bundling rate per run. Returns {bundled_command_events,
    max_bundled_subcommands}."""
    bundled = 0
    max_sub = 0
    for cmd in parsed_trace.get("commands", []):
        segs, _ = _split_shell_segments(cmd.get("cmd") or "", statement_only=True)
        nseg = len(segs)
        if nseg >= 2:
            bundled += 1
        if nseg > max_sub:
            max_sub = nseg
    return {"bundled_command_events": bundled, "max_bundled_subcommands": max_sub}


def count_emitted_codex_commands(parsed_trace: dict) -> list[str]:
    """Codex analogue of count_emitted_tool_calls: the `command_execution` cmds the
    agent ran (len() = command count, feeds the confab gate). Granularity differs
    from Read/Grep/Glob — the 1-per-10 ratio gets a live re-sanity (R2b, non-blocking)."""
    return [c.get("cmd", "") for c in parsed_trace.get("commands", [])]


def _out_of_worktree_reads(parsed_trace: dict, worktree: str) -> list[str]:
    """G4 (hostile PR diff): read_paths in the trace resolving OUTSIDE the worktree —
    a prompt-injected `read ~/.aws/credentials` would surface here. Returned for
    manifest flagging (observability); cited-outside-worktree is hard-rejected in
    binding (G1)."""
    worktree_real = os.path.realpath(worktree)
    out: set[str] = set()
    for cmd in parsed_trace.get("commands", []):
        for rp in cmd.get("read_paths") or []:
            if not _path_in_worktree(rp, worktree_real):
                out.add(rp)
    return sorted(out)


def _is_line_numbering_read(raw_cmd: str) -> bool:
    """True if the command is a known LINE-NUMBERING read (`nl`, `cat -n`/`cat -b`,
    `bat -n`/`bat --number`) — the only readers whose output carries a `<n>\\t` prefix
    we should strip for multi-line matching. COMMAND-BASED (not output-shape) so a raw
    `sed -n`/`cat` of a TSV data file with `42\\tvalue` lines is NEVER de-prefixed (which
    would let a fabricated `value` snippet bind)."""
    segments, _ = _split_shell_segments(raw_cmd or "")
    for seg in segments:
        try:
            toks = shlex.split(seg)
        except ValueError:
            toks = seg.split()
        if not toks:
            continue
        base = os.path.basename(toks[0])
        flags = toks[1:]
        if base == "nl":
            return True
        if base == "cat" and any(
            f in ("--number", "--number-nonblank")
            or (f.startswith("-") and not f.startswith("--") and ("n" in f or "b" in f))
            for f in flags
        ):
            return True
        if base == "bat" and any(f in ("-n", "--number") for f in flags):
            return True
    return False


def _multiline_lstrip_bound(snippet: str, output: str) -> bool:
    """Multi-line ONLY fallback for the model re-indenting a copied block: the snippet's
    lines (each `lstrip`-ed) must match a CONTIGUOUS, IN-ORDER run of `lstrip`-ed output
    lines with EXACT POSITIONAL correspondence — blank lines included, NEVER skipped on
    either side (skipping would open a stitching vector). Leading-strip ONLY: no rstrip,
    no internal-whitespace collapse, no content normalization — the line content after
    the leading indent must be VERBATIM equal. BOUNDARY: a claim specifically ABOUT
    indentation could bind to differently-indented real content — acceptable, the gate
    confirms "did the model see this content", not indentation semantics (the judge's
    reasoning is a separate layer); this is NOT an exemption."""
    snip = [ln.lstrip() for ln in snippet.splitlines()]
    if len(snip) < 2:
        return False
    out = [ln.lstrip() for ln in output.splitlines()]
    k = len(snip)
    return any(out[s:s + k] == snip for s in range(len(out) - k + 1))


def _read_output_binds(snippet: str, output: str, deprefixed: str | None) -> bool:
    """Binding test for the PURE-READ / single-file-search returns: the exact substring
    fast path (raw or Bug-#2 command-gated de-prefixed view) FIRST, then the multi-line
    `lstrip` fallback against both views. Single-line / already-exact cites take the fast
    path (zero regression); only re-indented multi-line cites reach the fallback."""
    if snippet in output or (deprefixed is not None and snippet in deprefixed):
        return True
    if _multiline_lstrip_bound(snippet, output):
        return True
    return deprefixed is not None and _multiline_lstrip_bound(snippet, deprefixed)


def _content_snippet_bound(cited_file: str, snippet: str, cmd: dict) -> bool:
    """Whether a single command's output binds (snippet, cited_file) for a content claim.

    SEARCH commands need PER-LINE attribution (Reviewer C3 fix — closes the multi-file
    co-match crack): when the output is path-prefixed (`path:line:text`, i.e. a dir /
    multi-file search), the snippet must appear on a match line whose path matches the
    cited file — NOT merely somewhere in the whole output while the cited file happens
    to be one of several matched files. Single-file search (`rg foo f.py` → `line:text`,
    no path prefix) and pure reads (`cat f`) attribute the WHOLE output to the one file,
    so snippet-in-output is correct (no co-match risk).
    """
    if not cmd.get("is_readlike"):
        return False
    output = cmd.get("output") or ""
    if not any(_paths_match(cited_file, ap) for ap in cmd.get("attributed_paths") or []):
        return False

    # De-prefixed view for line-numbering reads: `nl`/`cat -n` lines carry a `<n>\t`
    # prefix that breaks a verbatim multi-line `snippet in output`. The gate is now
    # COMMAND-BASED (not output-shape) — built ONLY when the producing command is a known
    # line-numbering read. This closes the TSV false-accept: a raw `sed -n`/`cat` of a
    # data file with `42\tvalue` lines is NOT de-prefixed, so a fabricated `value\nother`
    # snippet can't bind. `nl` preserves original indentation after the tab, so stripping
    # `^\s*\d+\t` recovers exact content — the snippet still matches verbatim. NOTE: `bat`
    # uses `   N │ content` (not `\d+\t`), so the strip is a no-op for it — multi-line
    # `bat` cites remain a known single-line-only limitation.
    deprefixed = None
    if _is_line_numbering_read(cmd.get("cmd") or ""):
        deprefixed = "\n".join(re.sub(r"^\s*\d+\t", "", ln) for ln in output.splitlines())

    if cmd.get("is_search"):
        prefixed = []
        for line in output.splitlines():
            m = re.match(r"^([^:]+):\d+:", line)
            if m and not m.group(1).isdigit():
                prefixed.append((m.group(1), line))
        if prefixed:  # multi-file/dir search → snippet must be on a CITED-file line
            # NB: substring-on-cited-line ONLY — the lstrip multi-line fallback is NOT
            # applied here (lstripping `path:N:text` lines would corrupt C3 attribution).
            return any(_paths_match(cited_file, path) and snippet in line for path, line in prefixed)
        # single-file search (no prefix) → whole output is that file
        return _read_output_binds(snippet, output, deprefixed)
    # pure read (cat/sed/…) → whole output is the cited file
    return _read_output_binds(snippet, output, deprefixed)


def _strip_grep_quotes(s: str) -> str:
    """Strip a single matching pair of surrounding quotes (single or double)."""
    s = (s or "").strip()
    if len(s) >= 2 and s[0] == s[-1] and s[0] in ("'", '"'):
        return s[1:-1]
    return s


def _normalize_grep_pattern(pattern: str) -> str:
    """Lexical normalization for grep-pattern EQUALITY: quote-strip + whitespace-
    collapse ONLY, applied SYMMETRICALLY to both sides. Deliberately does NOT
    semantically unescape (`\\|`→`|`, dropping `\\b`) — that would change the regex's
    meaning and could collapse genuinely-different patterns to "equal", re-opening the
    narrow-strawman launder."""
    return " ".join(_strip_grep_quotes(pattern).split())


# Value-bearing rg/grep flags whose following value must NOT be mistaken for the search
# pattern. Longer alternations first (so `--type-add` wins over `--type`). `=value` and
# space-separated `value` (quoted or bare) forms both consumed. `-e`/`--regexp` is NOT
# here — that value IS the pattern, handled separately.
_RG_VALUE_FLAGS_RE = re.compile(
    r"(?:^|\s)(?:--glob|--iglob|--type-add|--type-not|--type|--file|--pre|--hostname-bin|-g|-f)"
    r"(?:=|\s+)(?:'[^']*'|\"[^\"]*\"|\S+)"
)

# Escaped-AWARE value captures. The double-quote alt does NOT terminate at an escaped
# `\"` (the embedded-quote-truncation bug): `(?:\\.|[^"\\])*` consumes `\"`/`\b` as units.
# Single-quote does no escaping; bare = an unquoted token. Named groups select per-style
# handling in _resolve_pattern_value.
_DQ_VALUE = r"\"(?P<dq>(?:\\.|[^\"\\])*)\""
_SQ_VALUE = r"'(?P<sq>[^']*)'"
_BARE_VALUE = r"(?P<bare>\S+)"
# -e/--regexp value IS the pattern (quoted or bare). First QUOTED token = the pattern
# fallback (quoted only; a bare word → None → boundary-containment floor).
_E_FLAG_PATTERN_RE = re.compile(
    r"(?:^|\s)(?:-e|--regexp)(?:[=\s]+)(?:" + _DQ_VALUE + r"|" + _SQ_VALUE + r"|" + _BARE_VALUE + r")"
)
_FIRST_QUOTED_RE = re.compile(_DQ_VALUE + r"|" + _SQ_VALUE)


def _dq_unescape(value: str) -> str:
    """MINIMAL shell double-quote unescape: `\\"`→`"` and `\\\\`→`\\` ONLY (the literal
    set a shell processes inside double quotes). The literal-targeted `\\([\"\\\\])` form
    matches a backslash ONLY before `"` or `\\`, so regex metachars survive verbatim —
    `\\b` stays `\\b`, `\\|` stays `\\|`. This is NOT the forbidden generic
    `\\(.)` semantic unescape (which would collapse distinct regexes and re-open the launder)."""
    return re.sub(r'\\(["\\])', r"\1", value)


def _resolve_pattern_value(m: "re.Match") -> str:
    """Resolve a value-capture match to the pattern string: DOUBLE-quoted → minimal
    unescape (CONDITION 1/2); SINGLE-quoted and BARE → RAW (shell single-quotes do no
    escaping, so unescaping there would corrupt a legit literal-backslash pattern)."""
    gd = m.groupdict()
    if gd.get("dq") is not None:
        return _dq_unescape(gd["dq"])
    if gd.get("sq") is not None:
        return gd["sq"]
    return gd.get("bare") or ""


def _extract_search_pattern_arg(raw_cmd: str) -> str | None:
    """Best-effort extraction of the PATTERN argument from a RAW (un-shlex'd) search
    command — the `-e PAT`/`--regexp=PAT` value, else the first quoted token. The raw
    string is reliable (unlike the shlex-mangled searched_symbols). Returns None when
    the pattern can't be unambiguously identified (e.g. a bare unquoted word), which
    signals the caller to fall back to boundary-anchored containment."""
    raw = (raw_cmd or "").strip()
    if not raw:
        return None
    # -e/--regexp value IS the pattern (highest priority). Escaped-aware + dq-only unescape.
    m = _E_FLAG_PATTERN_RE.search(raw)
    if m:
        return _resolve_pattern_value(m)
    # Otherwise the first quoted token — but FIRST strip VALUE-BEARING rg/grep flags and
    # their values (`--glob '*.ts'`, `-g '*.go'`, `--type ts`, …) so a glob/type/file
    # value isn't mistaken for the search pattern. (Benign: prevents over-rejecting valid
    # absence claims on flag-prefixed rg.)
    stripped = _RG_VALUE_FLAGS_RE.sub(" ", raw)
    m = _FIRST_QUOTED_RE.search(stripped)
    if m:
        return _resolve_pattern_value(m)
    return None


def _emitted_pattern_matches(raw_cmd: str, recorded_pattern: str) -> bool:
    """Provenance: does this emitted search's pattern argument correspond to the model's
    RECORDED `evidence.grep_pattern`? Pattern-EQUALITY under lexical normalization when
    the argument is extractable; boundary-anchored containment (the recorded pattern
    delimited by quote/whitespace/shell-delimiter in the raw command) as the floor when
    it isn't. Plain substring is intentionally REJECTED — a narrow strawman pattern (run
    genuinely empty) must not launder a BROADER recorded pattern that was never run."""
    recorded = _strip_grep_quotes(recorded_pattern)
    if not recorded.strip():
        return False
    extracted = _extract_search_pattern_arg(raw_cmd)
    if extracted is not None:
        return _normalize_grep_pattern(extracted) == _normalize_grep_pattern(recorded_pattern)
    delim = r"['\"\s|;&()<>]"
    return re.search(rf"(?:^|{delim}){re.escape(recorded)}(?:$|{delim})", raw_cmd or "") is not None


def _mark_ungrounded(claim: dict, reason: str) -> None:
    """Annotate a claim as NOT grounded (soft-signal binding) with a diagnostic reason."""
    claim["grounded"] = False
    claim["grounding_reason"] = reason


def _check_evidence_binding_codex(verdict: dict, parsed_trace: dict, worktree: str) -> None:
    """C3 — output-based evidence-binding over the Codex shell trace, as a SOFT per-claim
    SIGNAL (soft-signal reframe). ANNOTATES each verified/contradicted claim with
    `grounded` (bool) and, when False, `grounding_reason` — and NEVER raises, so judge
    runs always complete. The certified anti-fab checks are unchanged; a failure now
    annotates instead of dropping the run. Grounding is surfaced via grounding_rate + the
    low-grounding alarm; the canary harness reads it too.

    Outcome-driven: only `verified`/`contradicted` claims are annotated (they drive the
    score); `non_falsifiable`/`unverified` are left untouched. Binds on what the agent
    ACTUALLY SAW (command output), with attribution + command-class guards:
      - Content {file,line,snippet}: snippet ∈ some command's output AND cited_file ∈
        that command's attributed_paths AND the command is read-like AND cited_file
        resolves inside the worktree (G1).
      - Absence: some is_search command returned EMPTY output (exit!=2) whose emitted
        pattern equals the recorded grep_pattern tied to the claim.
    Per-claim CONFAB: a score-driving claim in a run with ZERO commands is ungrounded
    ("no tool calls observed for this claim"). (The whole-run zero-commands-with-verified
    case is still a HARD drop in _run_and_verify_codex.)
    """
    commands = parsed_trace.get("commands", [])
    worktree_real = os.path.realpath(worktree)
    for review_key in ("review_a", "review_b"):
        for claim in verdict.get(review_key, {}).get("claim_trace", []):
            outcome = claim.get("outcome")
            if outcome not in ("verified", "contradicted"):
                continue
            claim_text = str(claim.get("claim_text", ""))
            evidence = claim.get("evidence")
            if not commands:  # per-claim confab: nothing was observed to ground against
                _mark_ungrounded(claim, "no tool calls observed for this claim")
                continue
            if not isinstance(evidence, dict):
                _mark_ungrounded(
                    claim,
                    f"{outcome} claim must cite binding object evidence, got "
                    f"{type(evidence).__name__}: {claim_text[:60]!r}",
                )
                continue
            cited_file = evidence.get("file")
            snippet = evidence.get("snippet")
            grep_pattern = evidence.get("grep_pattern")

            if cited_file and snippet:
                if not _path_in_worktree(cited_file, worktree_real):
                    _mark_ungrounded(
                        claim, f"cited file resolves outside the worktree (G1): {cited_file!r}"
                    )
                    continue
                if not any(_content_snippet_bound(cited_file, snippet, cmd) for cmd in commands):
                    _mark_ungrounded(
                        claim,
                        f"snippet not found in read-like output attributed to "
                        f"{cited_file!r}: {snippet!r} (claim: {claim_text[:60]!r})",
                    )
                    continue
            elif grep_pattern:
                # CLAIM TIE on the model's RECORDED grep_pattern — NOT the lossy,
                # shlex-mangled searched_symbols (which drops escaped alternation like
                # `loading\b`, losing the referent and rejecting valid absence claims).
                if not _grep_tied_to_claim(grep_pattern, claim_text):
                    _mark_ungrounded(
                        claim,
                        f"absence claim's grep_pattern not tied to the claim: "
                        f"{grep_pattern!r} (claim: {claim_text[:60]!r})",
                    )
                    continue
                # PROVENANCE (load-bearing anti-fabrication): a REAL emitted empty search
                # must exist whose EMITTED pattern EQUALS the recorded grep_pattern — so a
                # genuinely-empty NARROW strawman search can't vouch for a BROADER recorded
                # pattern that was never itself run empty.
                if not any(
                    cmd.get("is_search")
                    and _is_empty_output(cmd.get("output"))
                    and cmd.get("exit") != 2
                    and _emitted_pattern_matches(cmd.get("cmd") or "", grep_pattern)
                    for cmd in commands
                ):
                    _mark_ungrounded(
                        claim,
                        f"absence claim not backed by an emitted empty search whose pattern "
                        f"equals the recorded grep_pattern {grep_pattern!r}: {claim_text[:60]!r}",
                    )
                    continue
            else:
                _mark_ungrounded(
                    claim,
                    f"{outcome} claim evidence object missing binding fields "
                    f"(need file+snippet OR grep_pattern): {claim_text[:60]!r}",
                )
                continue
            claim["grounded"] = True  # passed all binding checks


# Retryable set for the Codex path: verification failures + CodexExecError (C4 may
# extend). EvidenceBindingError ⊂ StructuralInvalidRun (covered).
_RETRYABLE_CODEX = (StructuralInvalidRun, ConfabulationDetected, CodexExecError)


def _run_and_verify_codex(diff: str, review_a: str, review_b: str, worktree: str, out_dir: str,
                          *, model: str = CODEX_DEFAULT_MODEL, config_path: str | None = None,
                          max_attempts: int = MAX_JUDGE_ATTEMPTS, artifact_path=None):
    """Run a grounded Codex judge + verify. Order: structural validation FIRST
    (#11b), THEN evidence-binding (C2 interim gate; C3 = real output-based binding).
    Persists the per-attempt verdict+trace artifact BEFORE the gate so a gate failure
    still leaves the trace for diagnosis. Returns (verdict, trace_jsonl, parsed_trace).
    """
    last_exc: Exception | None = None
    attempts_log: list[dict] = []
    for attempt in range(max_attempts):
        entry = {"attempt": attempt, "verdict": None, "trace": None, "verify_error": None}
        attempts_log.append(entry)
        try:
            verdict, trace_jsonl = _run_codex_judge_once(
                diff, review_a, review_b, worktree, out_dir,
                model=model, config_path=config_path,
            )
            parsed_trace = _parse_codex_trace(trace_jsonl)
            entry["verdict"] = verdict
            entry["trace"] = trace_jsonl
            _write_transcript_artifact(artifact_path, attempts_log)
            _validate_response_structure(verdict)                          # #11b: structural FIRST (HARD)
            commands = count_emitted_codex_commands(parsed_trace)          # #2/M4 (codex granularity)
            # #10: back-fill the REAL observed command count into both reviews'
            # verification_summary (codex has ONE shared command stream → same list for
            # A and B). Without this, tool_calls_observed stays the model's placeholder 0,
            # zeroing the pilot tool_calls_per_run gate + the reporter table.
            _backfill_tool_calls_observed(verdict, commands, commands)
            # Soft-signal binding: annotates each verified/contradicted claim with
            # `grounded` IN PLACE (never raises). The coarse per-tool-call ratio confab
            # is superseded by this per-claim grounding and no longer drops the run.
            _check_evidence_binding_codex(verdict, parsed_trace, worktree)
            if not commands and _verdict_drove_scoreable_output(verdict):
                # WHOLE-RUN zero-tool-call that drove ANY scoreable output (claim_trace,
                # verified/contradicted findings, OR nonzero criterion scores) = a
                # from-memory fabricated verdict → unscoreable. HARD + retryable. Broader
                # than verified-only: a zero-finding/empty-claim_trace-but-scored verdict
                # would otherwise slip past BOTH this drop and the grounding alarm
                # (no eligible claims → grounding_rate=None → alarm guarded off).
                raise ConfabulationDetected("zero commands run but verdict drove scoreable output")
            # G4: flag (don't fail) any out-of-worktree reads for manifest observability.
            parsed_trace["out_of_worktree_reads"] = _out_of_worktree_reads(parsed_trace, worktree)
            return verdict, trace_jsonl, parsed_trace
        except _RETRYABLE_CODEX as exc:
            last_exc = exc
            entry["verify_error"] = f"{type(exc).__name__}: {exc}"
            _write_transcript_artifact(artifact_path, attempts_log)
            logger.warning(
                "codex judge attempt %d/%d failed (%s): %s",
                attempt + 1, max_attempts, type(exc).__name__, exc,
            )
    assert last_exc is not None
    raise last_exc
