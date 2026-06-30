"""Pilot pass-criteria evaluator for the v2 methodology.

Evaluates the pass criteria over a completed pilot run. Six criteria are
canary-independent and are ALWAYS evaluated. The seventh (canaries_caught) is
gated only when canaries were injected; on canary-free runs it is recorded as
SKIPPED/NA (None) and excluded from the pilot_pass AND, so pilot_pass is a real
bool rather than None even without --inject-canary.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field

logger = logging.getLogger(__name__)


# Acceptable wall-clock per judge task = the OBSERVED MEAN per-invocation latency
# from the pilot run (~605.6s mean; one A/B run incl. tool verification + at most one
# retry), used with COST_SAFETY_FACTOR below. Measured, NOT a curve-fit target.
ACCEPTABLE_PER_TASK_LATENCY_SEC = 605
# Headroom over the ideal fully-packed wall-clock for scheduling/queueing overhead
# and tail latency under concurrency.
COST_SAFETY_FACTOR = 1.5

# Krippendorff alpha gate.
ALPHA_GATE = 0.6


@dataclass
class PilotPassResult:
    passed: bool = False
    # bool per criterion; None means SKIPPED/NA (e.g. canaries_caught on a
    # canary-free run) — excluded from the pilot_pass AND.
    criterion_results: dict[str, bool | None] = field(default_factory=dict)
    reasons: list[str] = field(default_factory=list)
    degenerate_alpha_criteria: list[str] = field(default_factory=list)
    ungated_alpha_criteria: list[str] = field(default_factory=list)

    def to_dict(self) -> dict:
        return {
            "passed": self.passed,
            "criterion_results": self.criterion_results,
            "reasons": self.reasons,
            "degenerate_alpha_criteria": self.degenerate_alpha_criteria,
            "ungated_alpha_criteria": self.ungated_alpha_criteria,
        }


def _has_tool_calls_per_run(pr_results: list[dict]) -> tuple[bool, str]:
    """Criterion 1: every successful run has tool_calls_observed > 0."""
    runs_with_zero = 0
    total_runs = 0
    for pr in pr_results:
        for run in pr.get("runs", []):
            raw = run.get("raw_response", {})
            total_runs += 1
            for review_key in ("review_a", "review_b"):
                vs = raw.get(review_key, {}).get("verification_summary", {})
                if vs.get("tool_calls_observed", 0) == 0:
                    runs_with_zero += 1
                    break
    if total_runs == 0:
        return False, "no successful runs to evaluate"
    if runs_with_zero > 0:
        return False, f"{runs_with_zero}/{total_runs} run(s) had zero tool calls"
    return True, f"all {total_runs} run(s) had tool calls > 0"


def _no_confabulation_rejections(pilot_counters: dict) -> tuple[bool, str]:
    """Criterion 2: zero runs rejected by confabulation cross-check."""
    n = pilot_counters.get("confabulation_rejections", 0)
    if n > 0:
        return False, f"{n} run(s) rejected by confabulation cross-check"
    return True, "zero confabulation rejections"


def _canaries_caught(canary_outcomes: list[dict]) -> tuple[bool, str]:
    """Criterion 3: every injected canary was flagged as expected (contradicted)."""
    if not canary_outcomes:
        return False, "no canary outcomes recorded (expected at least 1 for pilot)"
    failures = [c for c in canary_outcomes if not c.get("passed")]
    if failures:
        return False, f"{len(failures)}/{len(canary_outcomes)} canary(ies) failed: {[c.get('id') for c in failures]}"
    return True, f"all {len(canary_outcomes)} canary(ies) caught as expected"


def _cost_wallclock_within_budget(
    total_wallclock_seconds: float,
    expected_tasks: int,
    max_workers: int,
    pr_results: list[dict],
) -> tuple[bool, str]:
    """Criterion 4: parallel SCORING-phase wall-clock within the derived budget.

    Budget = expected_tasks × ACCEPTABLE_PER_TASK_LATENCY_SEC ÷ max_workers ×
    COST_SAFETY_FACTOR — the ideal fully-packed wall-clock for the task count at the
    chosen concurrency, plus headroom. This is concurrency-aware, unlike the old
    per-invocation mean (which is now REPORTED-only below as the stable,
    concurrency-independent signal — it does NOT gate).
    """
    if expected_tasks <= 0:
        return False, "no judge tasks to evaluate"
    workers = max(1, max_workers)
    budget = (expected_tasks * ACCEPTABLE_PER_TASK_LATENCY_SEC / workers) * COST_SAFETY_FACTOR

    # Reported-only: per-invocation mean (concurrency-independent).
    all_durations: list[float] = []
    for pr in pr_results:
        all_durations.extend(pr.get("run_durations_seconds", []))
    mean_note = ""
    if all_durations:
        mean_note = f"; per-invocation mean {sum(all_durations) / len(all_durations):.1f}s (reported)"

    if total_wallclock_seconds > budget:
        return False, (
            f"scoring wall-clock {total_wallclock_seconds:.1f}s > budget {budget:.1f}s "
            f"({expected_tasks} tasks ÷ {workers} workers){mean_note}"
        )
    return True, (
        f"scoring wall-clock {total_wallclock_seconds:.1f}s within budget {budget:.1f}s "
        f"({expected_tasks} tasks ÷ {workers} workers){mean_note}"
    )


def _categorize_alpha_criteria(inter_run_alpha: dict) -> tuple[list[str], list[str], list[str]]:
    """Split criteria for the alpha gate into (perfect, ungated, below).

    - perfect: α=None because all scores identical ("perfect_agreement") → PASS.
    - ungated: reliability UNMEASURABLE → FAIL. Covers α=None for any non-perfect
      reason (insufficient_items / computation_error) AND α present but its CI
      lower bound is missing/None (the authoritative value can't be computed).
    - below: CI lower bound present but < ALPHA_GATE → FAIL (formatted "crit CI-lower=x").

    Single source of truth for both the gate verdict and the surfaced lists.
    """
    reasons = inter_run_alpha.get("_degenerate_reason", {})
    ci_lowers = inter_run_alpha.get("_ci_lower", {})
    perfect: list[str] = []
    ungated: list[str] = []
    below: list[str] = []
    for criterion, alpha in inter_run_alpha.items():
        if criterion.startswith("_"):
            continue
        if alpha is None:
            if reasons.get(criterion) == "perfect_agreement":
                perfect.append(criterion)
            else:
                ungated.append(criterion)
            continue
        ci_lower = ci_lowers.get(criterion)
        if ci_lower is None:
            ungated.append(criterion)
        elif ci_lower < ALPHA_GATE:
            below.append(f"{criterion} CI-lower={ci_lower:.2f}")
    return perfect, ungated, below


def _inter_run_alpha_passes(inter_run_alpha: dict) -> tuple[bool, str]:
    """Criterion 5: every criterion's Krippendorff alpha CI lower bound >= ALPHA_GATE.

    The bootstrap CI lower bound — not the point estimate — is authoritative. Three
    outcomes per criterion (see _categorize_alpha_criteria):
    - all-identical scores (α=None, perfect agreement) → PASS (maximal reliability).
    - reliability unmeasurable (insufficient data / CI uncomputable) → FAIL (ungated).
    - CI lower bound < ALPHA_GATE → FAIL (below gate).
    """
    if not inter_run_alpha:
        return False, "inter_run_alpha empty (cannot evaluate)"
    perfect, ungated, below = _categorize_alpha_criteria(inter_run_alpha)
    if below or ungated:
        parts: list[str] = []
        if below:
            parts.append(f"CI lower bound below gate {ALPHA_GATE}: {', '.join(below)}")
        if ungated:
            parts.append(f"reliability unmeasurable (ungated): {', '.join(ungated)}")
        return False, "; ".join(parts)
    if perfect:
        return True, (
            f"perfect agreement on: {', '.join(perfect)}; "
            f"all measurable criteria CI lower bound >= {ALPHA_GATE}"
        )
    return True, f"all criteria CI lower bound >= α gate ({ALPHA_GATE})"


def _no_unexplained_crashes(pr_results: list[dict]) -> tuple[bool, str]:
    """Criterion 6: every PR completed all 3 planned runs (no subprocess crashes)."""
    incomplete = [
        pr for pr in pr_results
        if pr.get("n_runs_completed", 0) < pr.get("n_runs_planned", 3)
    ]
    if incomplete:
        return False, (
            f"{len(incomplete)} PR(s) had incomplete runs: "
            + ", ".join(
                f"PR#{pr.get('pr_data', {}).get('pr_number', '?')} "
                f"({pr.get('n_runs_completed')}/{pr.get('n_runs_planned')})"
                for pr in incomplete
            )
        )
    return True, f"all {len(pr_results)} PR(s) completed all 3 runs"


def _schema_validates(reporter_succeeded: bool) -> tuple[bool, str]:
    """Criterion 7: reporter rendered without errors."""
    if not reporter_succeeded:
        return False, "reporter raised an error"
    return True, "reporter rendered successfully"


def evaluate_pilot_pass(
    pr_results: list[dict],
    pilot_counters: dict,
    canary_outcomes: list[dict],
    inter_run_alpha: dict,
    reporter_succeeded: bool,
    total_wallclock_seconds: float = 0.0,
    expected_tasks: int = 0,
    max_workers: int = 1,
) -> PilotPassResult:
    """Evaluate the pilot pass criteria and return a structured result.

    Six criteria are canary-independent and always evaluated. The canary
    criterion (canaries_caught) is gated ONLY when canaries were injected; on
    canary-free runs it is recorded as SKIPPED/NA (None) and excluded from the
    pilot_pass AND, so `passed` is a real bool either way.

    Args:
        pr_results: List of per-PR result dicts from evaluate_pr()
        pilot_counters: {confabulation_rejections, disallowed_tool_rejections}
        canary_outcomes: List of canary outcome dicts (empty when no canaries)
        inter_run_alpha: {criterion: alpha_value | None}
        reporter_succeeded: True if the research/per-PR reporter ran without error
        total_wallclock_seconds: wall-clock of the parallel scoring phase only
        expected_tasks: number of (PR × run) judge tasks scheduled
        max_workers: judge worker-pool concurrency used
    """
    canaries_present = bool(canary_outcomes)

    # Criterion 3 (canaries_caught) is the only canary-dependent check. Include
    # it inline (preserving position) only when canaries were injected.
    checks = [
        ("tool_calls_per_run", _has_tool_calls_per_run(pr_results)),
        ("no_confabulation_rejections", _no_confabulation_rejections(pilot_counters)),
    ]
    if canaries_present:
        checks.append(("canaries_caught", _canaries_caught(canary_outcomes)))
    checks += [
        ("cost_overhead_within_target",
         _cost_wallclock_within_budget(total_wallclock_seconds, expected_tasks, max_workers, pr_results)),
        ("inter_run_alpha_passes", _inter_run_alpha_passes(inter_run_alpha)),
        ("no_unexplained_crashes", _no_unexplained_crashes(pr_results)),
        ("schema_validates", _schema_validates(reporter_succeeded)),
    ]

    result = PilotPassResult()
    for name, (passed, reason) in checks:
        result.criterion_results[name] = passed
        result.reasons.append(f"{name}: {'PASS' if passed else 'FAIL'} — {reason}")

    if not canaries_present:
        # Record explicitly as SKIPPED/NA so the manifest is self-describing
        # about what was gated; None is excluded from the pilot_pass AND below.
        result.criterion_results["canaries_caught"] = None
        result.reasons.append(
            "canaries_caught: SKIPPED — no canaries injected (criterion not applicable)"
        )

    # Surface α criteria for transparency in the manifest: degenerate = all-identical
    # (perfect agreement, PASS); ungated = reliability unmeasurable (FAIL).
    perfect, ungated, _below = _categorize_alpha_criteria(inter_run_alpha)
    result.degenerate_alpha_criteria = perfect
    result.ungated_alpha_criteria = ungated

    result.passed = all(v for v in result.criterion_results.values() if v is not None)
    return result
