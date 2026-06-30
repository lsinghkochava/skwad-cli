"""Tests for eval.lib.pilot — Section F: evaluate_pilot_pass criteria."""

import unittest

from eval.lib.pilot import (
    ACCEPTABLE_PER_TASK_LATENCY_SEC,
    ALPHA_GATE,
    COST_SAFETY_FACTOR,
    PilotPassResult,
    evaluate_pilot_pass,
)

# ---------------------------------------------------------------------------
# Fixture helpers
# ---------------------------------------------------------------------------

_CRITERIA = [
    "issue_detection", "actionability", "severity_accuracy",
    "coverage", "signal_to_noise", "depth", "novel_substantive_findings",
]

_GOOD_ALPHA = {c: 0.8 for c in _CRITERIA}
# CI lower bounds comfortably above the gate. The gate reads THESE, not the
# point α — see _inter_run_alpha_passes.
_GOOD_CI_LOWER = {c: 0.7 for c in _CRITERIA}
_CANARY_PASS = [{"passed": True, "id": "c1"}]


def _alpha(point=None, ci_lower=None, reason=None) -> dict:
    """Build an inter_run_alpha dict mirroring compute_inter_run_alpha output:
    point α per criterion, the ``_ci_lower`` map (success criteria only), and an
    optional ``_degenerate_reason`` map. Degenerate (α=None) criteria carry NO CI
    lower bound, matching the producer. Defaults pass the gate."""
    a = dict(point if point is not None else _GOOD_ALPHA)
    cil = dict(ci_lower if ci_lower is not None else _GOOD_CI_LOWER)
    if reason:
        a["_degenerate_reason"] = dict(reason)
        for crit in reason:  # faithful: degenerate criteria have no CI lower bound
            cil.pop(crit, None)
    a["_ci_lower"] = cil
    return a


def _make_pr(n_runs: int = 3, tools_per_run: int = 3, duration: float = 10.0) -> dict:
    """Build a minimal pr_result dict accepted by pilot criteria functions."""
    run = {
        "raw_response": {
            "review_a": {"verification_summary": {"tool_calls_observed": tools_per_run}},
            "review_b": {"verification_summary": {"tool_calls_observed": tools_per_run}},
        },
    }
    return {
        "runs": [run] * n_runs,
        "n_runs_completed": n_runs,
        "n_runs_planned": 3,
        "run_durations_seconds": [duration] * n_runs,
    }


def _pilot(pr_results=None, pilot_counters=None, canary_outcomes=None,
           inter_run_alpha=None, reporter_succeeded=True,
           total_wallclock_seconds=100.0, expected_tasks=3, max_workers=3):
    """Call evaluate_pilot_pass with sensible defaults.

    Cost defaults (3 tasks ÷ 3 workers, 100s wall-clock) are comfortably within the
    derived budget so unrelated criteria can be exercised in isolation.
    """
    if pr_results is None:
        pr_results = [_make_pr()]
    if pilot_counters is None:
        pilot_counters = {"confabulation_rejections": 0, "disallowed_tool_rejections": 0}
    if canary_outcomes is None:
        canary_outcomes = list(_CANARY_PASS)
    if inter_run_alpha is None:
        inter_run_alpha = _alpha()
    return evaluate_pilot_pass(
        pr_results=pr_results,
        pilot_counters=pilot_counters,
        canary_outcomes=canary_outcomes,
        inter_run_alpha=inter_run_alpha,
        reporter_succeeded=reporter_succeeded,
        total_wallclock_seconds=total_wallclock_seconds,
        expected_tasks=expected_tasks,
        max_workers=max_workers,
    )


# ---------------------------------------------------------------------------
# Criterion 1: tool_calls_per_run
# ---------------------------------------------------------------------------

class TestToolCallsPerRun(unittest.TestCase):
    def test_all_runs_have_tools_passes(self):
        result = _pilot(pr_results=[_make_pr(tools_per_run=3)])
        self.assertTrue(result.criterion_results["tool_calls_per_run"])

    def test_any_run_with_zero_tool_calls_fails(self):
        result = _pilot(pr_results=[_make_pr(tools_per_run=0)])
        self.assertFalse(result.criterion_results["tool_calls_per_run"])

    def test_empty_pr_results_fails(self):
        result = _pilot(pr_results=[])
        self.assertFalse(result.criterion_results["tool_calls_per_run"])


# ---------------------------------------------------------------------------
# Criterion 2: no_confabulation_rejections
# ---------------------------------------------------------------------------

class TestNoConfabulationRejections(unittest.TestCase):
    def test_zero_rejections_passes(self):
        result = _pilot(pilot_counters={"confabulation_rejections": 0,
                                        "disallowed_tool_rejections": 0})
        self.assertTrue(result.criterion_results["no_confabulation_rejections"])

    def test_nonzero_rejections_fails(self):
        result = _pilot(pilot_counters={"confabulation_rejections": 1,
                                        "disallowed_tool_rejections": 0})
        self.assertFalse(result.criterion_results["no_confabulation_rejections"])


# ---------------------------------------------------------------------------
# Criterion 3: canaries_caught
# ---------------------------------------------------------------------------

class TestCanariesCaught(unittest.TestCase):
    def test_all_canaries_passed_passes(self):
        result = _pilot(canary_outcomes=[{"passed": True}, {"passed": True}])
        self.assertTrue(result.criterion_results["canaries_caught"])

    def test_one_failed_canary_fails(self):
        result = _pilot(canary_outcomes=[{"passed": True}, {"passed": False}])
        self.assertFalse(result.criterion_results["canaries_caught"])

    def test_no_canary_outcomes_skipped(self):
        # New contract: a canary-free run records canaries_caught as SKIPPED/NA
        # (None) — NOT False. The old assertFalse passed only because None is
        # falsy; assert the explicit SKIPPED sentinel and its reason string.
        result = _pilot(canary_outcomes=[])
        self.assertIsNone(result.criterion_results["canaries_caught"])
        skipped_reason = next(
            r for r in result.reasons if r.startswith("canaries_caught")
        )
        self.assertEqual(
            skipped_reason,
            "canaries_caught: SKIPPED — no canaries injected (criterion not applicable)",
        )


# ---------------------------------------------------------------------------
# Canary un-bundling: canaries_caught is gated ONLY when canaries were injected.
# On a canary-free run the 6 canary-independent criteria still gate, canaries_caught
# is recorded as SKIPPED/NA (None) and excluded from the pilot_pass AND, so
# pilot_pass is a real bool. Canary-PRESENT path is unchanged (canaries in the AND).
# ---------------------------------------------------------------------------

class TestCanaryFreePilotPass(unittest.TestCase):
    def test_pilot_pass_is_real_bool_when_no_canaries(self):
        # All 6 applicable criteria pass → pilot_pass is True (a bool), not None.
        result = _pilot(canary_outcomes=[])
        self.assertIsInstance(result.passed, bool)
        self.assertTrue(result.passed)

    def test_pilot_pass_false_when_an_applicable_criterion_fails_no_canaries(self):
        # A canary-free run still fails on a real criterion failure; the SKIPPED
        # canaries_caught=None must not rescue or poison the AND.
        result = _pilot(
            canary_outcomes=[],
            pilot_counters={"confabulation_rejections": 1,
                            "disallowed_tool_rejections": 0},
        )
        self.assertIsInstance(result.passed, bool)
        self.assertFalse(result.passed)

    def test_canaries_caught_excluded_from_and_when_skipped(self):
        # passed is the AND over the 6 applicable criteria; the None entry is excluded.
        result = _pilot(canary_outcomes=[])
        applicable = {k: v for k, v in result.criterion_results.items() if v is not None}
        self.assertEqual(len(applicable), 6)
        self.assertEqual(result.passed, all(applicable.values()))

    def test_no_canary_run_has_no_inline_canary_pass_fail_reason(self):
        # The inline "canaries_caught: PASS/FAIL" reason is absent; only the
        # SKIPPED reason is present for that criterion.
        result = _pilot(canary_outcomes=[])
        canary_reasons = [r for r in result.reasons if r.startswith("canaries_caught")]
        self.assertEqual(len(canary_reasons), 1)
        self.assertIn("SKIPPED", canary_reasons[0])


class TestCanaryPresentRegression(unittest.TestCase):
    """Regression guard: the canary-PRESENT path is unchanged — canaries_caught
    stays in the AND and a failing canary drives pilot_pass to False."""

    def test_passing_canary_included_in_and_and_pilot_passes(self):
        result = _pilot(canary_outcomes=[{"passed": True, "id": "c1"}])
        self.assertTrue(result.criterion_results["canaries_caught"])
        self.assertTrue(result.passed)

    def test_failing_canary_drives_pilot_pass_false(self):
        # Every other criterion passes; only the canary fails → pilot_pass False,
        # proving canaries_caught is still part of the AND.
        result = _pilot(canary_outcomes=[{"passed": False, "id": "c1"}])
        self.assertFalse(result.criterion_results["canaries_caught"])
        self.assertFalse(result.passed)

    def test_canary_present_reason_is_pass_fail_not_skipped(self):
        result = _pilot(canary_outcomes=[{"passed": True, "id": "c1"}])
        canary_reason = next(r for r in result.reasons if r.startswith("canaries_caught"))
        self.assertIn("PASS", canary_reason)
        self.assertNotIn("SKIPPED", canary_reason)


# ---------------------------------------------------------------------------
# Criterion 4: cost gate — total scoring wall-clock within the derived budget.
# budget = expected_tasks × ACCEPTABLE_PER_TASK_LATENCY_SEC ÷ max_workers × COST_SAFETY_FACTOR.
# The per-invocation mean is REPORTED but does NOT gate (concurrency-independent signal).
# ---------------------------------------------------------------------------

def _budget(expected_tasks, max_workers):
    return (expected_tasks * ACCEPTABLE_PER_TASK_LATENCY_SEC / max(1, max_workers)) * COST_SAFETY_FACTOR


class TestCostWallclockWithinBudget(unittest.TestCase):
    def test_wallclock_under_budget_passes(self):
        budget = _budget(6, 3)
        result = _pilot(total_wallclock_seconds=budget - 1, expected_tasks=6, max_workers=3)
        self.assertTrue(result.criterion_results["cost_overhead_within_target"])

    def test_wallclock_over_budget_fails(self):
        budget = _budget(6, 3)
        result = _pilot(total_wallclock_seconds=budget + 1, expected_tasks=6, max_workers=3)
        self.assertFalse(result.criterion_results["cost_overhead_within_target"])

    def test_no_tasks_fails(self):
        result = _pilot(total_wallclock_seconds=0.0, expected_tasks=0, max_workers=3)
        self.assertFalse(result.criterion_results["cost_overhead_within_target"])

    def test_per_invocation_mean_is_reported_not_gated(self):
        # Huge per-invocation durations (mean ≫ budget) but the parallel wall-clock is
        # tiny → the gate keys off wall-clock and PASSES. The mean is report-only.
        slow_pr = _make_pr(duration=5000.0)  # per-invocation mean 5000s
        result = _pilot(
            pr_results=[slow_pr],
            total_wallclock_seconds=50.0, expected_tasks=2, max_workers=2,
        )
        self.assertTrue(result.criterion_results["cost_overhead_within_target"])
        cost_reason = next(r for r in result.reasons if r.startswith("cost_overhead_within_target"))
        self.assertIn("per-invocation mean", cost_reason)
        self.assertIn("reported", cost_reason)


# ---------------------------------------------------------------------------
# Criterion 5: inter_run_alpha_passes
# ---------------------------------------------------------------------------

class TestInterRunAlphaPasses(unittest.TestCase):
    def test_all_criteria_ci_lower_above_gate_passes(self):
        result = _pilot(inter_run_alpha=_alpha(ci_lower={c: ALPHA_GATE + 0.1 for c in _CRITERIA}))
        self.assertTrue(result.criterion_results["inter_run_alpha_passes"])

    def test_one_criterion_ci_lower_below_gate_fails(self):
        # Point α stays high; only the CI lower bound dips below the gate → FAIL.
        alpha = _alpha(ci_lower={**_GOOD_CI_LOWER, "issue_detection": ALPHA_GATE - 0.1})
        result = _pilot(inter_run_alpha=alpha)
        self.assertFalse(result.criterion_results["inter_run_alpha_passes"])

    def test_high_point_alpha_low_ci_lower_fails(self):
        # The discriminating case: point α 0.74 would have PASSED the old point-α
        # gate, but a CI lower bound of 0.29 now FAILS.
        alpha = _alpha(
            point={**_GOOD_ALPHA, "coverage": 0.74},
            ci_lower={**_GOOD_CI_LOWER, "coverage": 0.29},
        )
        result = _pilot(inter_run_alpha=alpha)
        self.assertFalse(result.criterion_results["inter_run_alpha_passes"])

    def test_ci_lower_exactly_at_gate_passes(self):
        # gate is `ci_lower < ALPHA_GATE`; exactly at the gate is not below it.
        alpha = _alpha(ci_lower={**_GOOD_CI_LOWER, "coverage": ALPHA_GATE})
        result = _pilot(inter_run_alpha=alpha)
        self.assertTrue(result.criterion_results["inter_run_alpha_passes"])

    def test_fail_message_reports_ci_lower_per_criterion(self):
        alpha = _alpha(
            point={**_GOOD_ALPHA, "coverage": 0.74},
            ci_lower={**_GOOD_CI_LOWER, "coverage": 0.29},
        )
        result = _pilot(inter_run_alpha=alpha)
        reason = next(r for r in result.reasons if r.startswith("inter_run_alpha_passes"))
        self.assertIn("coverage CI-lower=0.29", reason)
        self.assertIn("CI lower bound below gate 0.6", reason)

    def test_point_alpha_present_but_ci_lower_missing_is_ungated_fails(self):
        # Point α present but CI lower bound unavailable → reliability unmeasurable →
        # FAIL (ungated). THIS IS THE CASE THAT PREVIOUSLY SILENTLY PASSED.
        result = _pilot(inter_run_alpha={c: 0.74 for c in _CRITERIA})  # no _ci_lower key
        self.assertFalse(result.criterion_results["inter_run_alpha_passes"])
        self.assertIn("coverage", result.ungated_alpha_criteria)

    def test_perfect_agreement_criterion_passes_and_is_degenerate(self):
        # α=None with reason "perfect_agreement" → PASS, surfaced as degenerate
        # (NOT ungated).
        alpha = _alpha(
            point={**_GOOD_ALPHA, "coverage": None},
            reason={"coverage": "perfect_agreement"},
        )
        result = _pilot(inter_run_alpha=alpha)
        self.assertTrue(result.criterion_results["inter_run_alpha_passes"])
        self.assertIn("coverage", result.degenerate_alpha_criteria)
        self.assertNotIn("coverage", result.ungated_alpha_criteria)

    def test_insufficient_items_criterion_is_ungated_fails(self):
        # α=None with a non-perfect reason (insufficient_items) → FAIL (ungated).
        alpha = _alpha(
            point={**_GOOD_ALPHA, "coverage": None},
            reason={"coverage": "insufficient_items"},
        )
        result = _pilot(inter_run_alpha=alpha)
        self.assertFalse(result.criterion_results["inter_run_alpha_passes"])
        self.assertIn("coverage", result.ungated_alpha_criteria)
        self.assertNotIn("coverage", result.degenerate_alpha_criteria)

    def test_fail_message_distinguishes_below_gate_from_ungated(self):
        # One below-gate criterion + one ungated criterion → message names both,
        # with the distinct phrasings.
        alpha = _alpha(
            point={**_GOOD_ALPHA, "coverage": 0.74, "depth": None},
            ci_lower={**_GOOD_CI_LOWER, "coverage": 0.29},
            reason={"depth": "insufficient_items"},
        )
        result = _pilot(inter_run_alpha=alpha)
        reason = next(r for r in result.reasons if r.startswith("inter_run_alpha_passes"))
        self.assertIn("CI lower bound below gate 0.6: coverage CI-lower=0.29", reason)
        self.assertIn("reliability unmeasurable (ungated): depth", reason)

    def test_empty_alpha_dict_fails(self):
        result = _pilot(inter_run_alpha={})
        self.assertFalse(result.criterion_results["inter_run_alpha_passes"])


# ---------------------------------------------------------------------------
# Criterion 6: no_unexplained_crashes
# ---------------------------------------------------------------------------

class TestNoUnexplainedCrashes(unittest.TestCase):
    def test_all_prs_complete_passes(self):
        result = _pilot(pr_results=[_make_pr(n_runs=3)])
        self.assertTrue(result.criterion_results["no_unexplained_crashes"])

    def test_one_incomplete_pr_fails(self):
        result = _pilot(pr_results=[_make_pr(n_runs=2)])
        self.assertFalse(result.criterion_results["no_unexplained_crashes"])


# ---------------------------------------------------------------------------
# Criterion 7: schema_validates
# ---------------------------------------------------------------------------

class TestSchemaValidates(unittest.TestCase):
    def test_reporter_succeeded_passes(self):
        result = _pilot(reporter_succeeded=True)
        self.assertTrue(result.criterion_results["schema_validates"])

    def test_reporter_failed_fails(self):
        result = _pilot(reporter_succeeded=False)
        self.assertFalse(result.criterion_results["schema_validates"])


# ---------------------------------------------------------------------------
# Overall evaluate_pilot_pass
# ---------------------------------------------------------------------------

class TestEvaluatePilotPass(unittest.TestCase):
    def test_all_criteria_pass_result_is_true(self):
        result = _pilot()
        self.assertTrue(result.passed)
        self.assertTrue(all(result.criterion_results.values()))

    def test_one_criterion_fail_result_is_false(self):
        result = _pilot(pilot_counters={"confabulation_rejections": 1,
                                        "disallowed_tool_rejections": 0})
        self.assertFalse(result.passed)

    def test_reasons_list_has_exactly_seven_entries(self):
        result = _pilot()
        self.assertEqual(len(result.reasons), 7)

    def test_to_dict_has_expected_keys(self):
        d = _pilot().to_dict()
        self.assertIn("passed", d)
        self.assertIn("criterion_results", d)
        self.assertIn("reasons", d)


# ---------------------------------------------------------------------------
# Section E: Null α = perfect agreement (Reviewer Important #5)
# ---------------------------------------------------------------------------

class TestDegenerateAndUngatedAlpha(unittest.TestCase):
    """α=None splits two ways: reason 'perfect_agreement' → PASS, surfaced as
    degenerate; any other reason / an unmeasurable CI lower bound → FAIL, surfaced
    as ungated. Perfect agreement is METHODOLOGICALLY GOOD; unmeasurable is not."""

    def test_all_perfect_agreement_passes(self):
        # Every criterion all-identical → perfect agreement → PASS.
        alpha = _alpha(
            point={c: None for c in _CRITERIA},
            reason={c: "perfect_agreement" for c in _CRITERIA},
        )
        result = _pilot(inter_run_alpha=alpha)
        self.assertTrue(result.criterion_results["inter_run_alpha_passes"])

    def test_ci_lower_below_gate_still_fails_alongside_perfect_companion(self):
        # A perfect-agreement companion must NOT hide a real below-gate failure.
        alpha = _alpha(
            point={**_GOOD_ALPHA, "issue_detection": None},
            ci_lower={**_GOOD_CI_LOWER, "actionability": ALPHA_GATE - 0.1},
            reason={"issue_detection": "perfect_agreement"},
        )
        result = _pilot(inter_run_alpha=alpha)
        self.assertFalse(result.criterion_results["inter_run_alpha_passes"])

    def test_perfect_agreement_criteria_surfaced_as_degenerate(self):
        alpha = _alpha(
            point={**_GOOD_ALPHA, "coverage": None, "depth": None},
            reason={"coverage": "perfect_agreement", "depth": "perfect_agreement"},
        )
        result = _pilot(inter_run_alpha=alpha)
        self.assertIn("coverage", result.degenerate_alpha_criteria)
        self.assertIn("depth", result.degenerate_alpha_criteria)
        self.assertNotIn("issue_detection", result.degenerate_alpha_criteria)
        self.assertEqual(result.ungated_alpha_criteria, [])

    def test_pilot_pass_dict_includes_degenerate_and_ungated_fields(self):
        d = _pilot().to_dict()
        self.assertIn("degenerate_alpha_criteria", d)
        self.assertIn("ungated_alpha_criteria", d)

    def test_lists_empty_when_all_criteria_measurable_and_pass(self):
        result = _pilot(inter_run_alpha=_alpha())
        self.assertEqual(result.degenerate_alpha_criteria, [])
        self.assertEqual(result.ungated_alpha_criteria, [])


if __name__ == "__main__":
    unittest.main()
