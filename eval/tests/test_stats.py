"""Tests for eval.lib.stats."""

import unittest

import numpy as np

from eval.lib.stats import (
    MethodologyMismatchError,
    bh_fdr_adjust,
    check_methodology_version,
    cliffs_delta,
    cliffs_delta_bca_ci,
    compute_inter_run_alpha,
    krippendorff_alpha_ordinal,
    wilcoxon_paired,
)


# ---------------------------------------------------------------------------
# TestWilcoxonPaired
# ---------------------------------------------------------------------------

class TestWilcoxonPaired(unittest.TestCase):
    def test_all_positive_significant(self):
        result = wilcoxon_paired([3, 4, 2, 5, 6, 4])
        self.assertLess(result["p_value"], 0.05)
        self.assertEqual(result["n"], 6)
        self.assertIn("statistic", result)

    def test_all_zero_raises_value_error(self):
        # Code explicitly raises — Wilcoxon is undefined with no non-zero differences.
        with self.assertRaises(ValueError):
            wilcoxon_paired([0, 0, 0, 0])

    def test_symmetric_not_significant(self):
        result = wilcoxon_paired([-2, 2, -1, 1, -3, 3])
        self.assertGreaterEqual(result["p_value"], 0.1)

    def test_empty_raises_value_error(self):
        with self.assertRaises(ValueError):
            wilcoxon_paired([])


# ---------------------------------------------------------------------------
# TestCliffsDelta
# ---------------------------------------------------------------------------

class TestCliffsDelta(unittest.TestCase):
    def test_perfect_positive(self):
        result = cliffs_delta([3, 3, 4], [1, 2, 2])
        self.assertAlmostEqual(result["delta"], 1.0, places=6)
        self.assertEqual(result["interpretation"], "large")

    def test_perfect_negative(self):
        result = cliffs_delta([1, 2, 2], [3, 3, 4])
        self.assertAlmostEqual(result["delta"], -1.0, places=6)
        self.assertEqual(result["interpretation"], "large")

    def test_equal_arrays_negligible(self):
        result = cliffs_delta([1, 2, 3], [1, 2, 3])
        self.assertAlmostEqual(result["delta"], 0.0, places=6)
        self.assertEqual(result["interpretation"], "negligible")

    def test_small_interpretation(self):
        # a=[2,1], b=[1,2,2] → delta≈-0.167, |δ|=0.167 → 0.147 ≤ |δ| < 0.33 → small.
        result = cliffs_delta([2, 1], [1, 2, 2])
        self.assertAlmostEqual(result["delta"], -1 / 6, places=6)
        self.assertEqual(result["interpretation"], "small")

    def test_medium_interpretation(self):
        # a=[3,3,1], b=[2,2,1] → delta≈0.444, 0.33 ≤ |δ| < 0.474 → medium.
        result = cliffs_delta([3, 3, 1], [2, 2, 1])
        self.assertAlmostEqual(result["delta"], 4 / 9, places=6)
        self.assertEqual(result["interpretation"], "medium")

    def test_large_interpretation(self):
        result = cliffs_delta([5, 5, 5], [1, 1, 1])
        self.assertAlmostEqual(result["delta"], 1.0, places=6)
        self.assertEqual(result["interpretation"], "large")

    def test_empty_input_raises(self):
        with self.assertRaises(ValueError):
            cliffs_delta([], [1, 2])
        with self.assertRaises(ValueError):
            cliffs_delta([1, 2], [])


# ---------------------------------------------------------------------------
# TestCliffsDeltaBcaCI
# ---------------------------------------------------------------------------

class TestCliffsDeltaBcaCI(unittest.TestCase):
    def test_seeded_reproducibility(self):
        a = [3, 2, 4, 1, 5]
        b = [1, 2, 2, 3, 4]
        r1 = cliffs_delta_bca_ci(a, b, n_boot=200, seed=42)
        r2 = cliffs_delta_bca_ci(a, b, n_boot=200, seed=42)
        self.assertAlmostEqual(r1["ci_lower"], r2["ci_lower"], places=10)
        self.assertAlmostEqual(r1["ci_upper"], r2["ci_upper"], places=10)

    def test_strong_dominance_delta_and_ci_structure(self):
        # delta > 0 (positive direction); CI is a valid interval containing the point estimate.
        # Pairs include one b>a to avoid degenerate (zero-variance) bootstrap.
        a = [4, 5, 3, 4, 5]
        b = [1, 3, 2, 5, 2]
        result = cliffs_delta_bca_ci(a, b, n_boot=200, seed=42)
        self.assertGreater(result["delta"], 0.0)
        self.assertLessEqual(result["ci_lower"], result["delta"])
        self.assertGreaterEqual(result["ci_upper"], result["delta"])
        self.assertGreaterEqual(result["ci_lower"], -1.0)
        self.assertLessEqual(result["ci_upper"], 1.0)

    def test_no_dominance_ci_straddles_zero(self):
        # delta=0; pairs include both a>b and a<b directions so bootstrap has variance.
        a = [1, 3, 5]
        b = [2, 3, 4]
        result = cliffs_delta_bca_ci(a, b, n_boot=200, seed=42)
        self.assertAlmostEqual(result["delta"], 0.0, places=6)
        self.assertLessEqual(result["ci_lower"], 0.0)
        self.assertGreaterEqual(result["ci_upper"], 0.0)

    def test_length_mismatch_raises(self):
        with self.assertRaises(ValueError):
            cliffs_delta_bca_ci([1, 2, 3], [1, 2])

    def test_empty_raises(self):
        with self.assertRaises(ValueError):
            cliffs_delta_bca_ci([], [])

    def test_n_boot_in_result(self):
        result = cliffs_delta_bca_ci([1, 2], [1, 2], n_boot=50, seed=0)
        self.assertEqual(result["n_boot"], 50)


# ---------------------------------------------------------------------------
# TestKrippendorffAlphaOrdinal
# ---------------------------------------------------------------------------

class TestKrippendorffAlphaOrdinal(unittest.TestCase):
    def test_perfect_agreement(self):
        data = [[1, 2, 3, 4, 5]] * 3
        result = krippendorff_alpha_ordinal(data, n_boot=100, seed=42)
        self.assertAlmostEqual(result["alpha"], 1.0, places=6)
        self.assertGreater(result["ci_lower"], 0.9)
        self.assertEqual(result["n_raters"], 3)
        self.assertEqual(result["n_items"], 5)

    def test_disagreement_alpha_nonpositive(self):
        # Raters give systematically different orderings → alpha ≤ 0.
        data = [
            [1, 2, 3, 4, 5],
            [5, 4, 3, 2, 1],
            [3, 5, 1, 4, 2],
        ]
        result = krippendorff_alpha_ordinal(data, n_boot=100, seed=42)
        self.assertLessEqual(result["alpha"], 0.0)

    def test_missing_values_no_crash(self):
        data = [
            [1.0, 2.0, float("nan"), 4.0, 5.0],
            [1.0, 2.0, 3.0, 4.0, 5.0],
            [1.0, 2.0, 3.0, 4.0, 5.0],
        ]
        result = krippendorff_alpha_ordinal(data, n_boot=100, seed=42)
        self.assertIn("alpha", result)
        self.assertGreater(result["alpha"], 0.5)

    def test_seeded_reproducibility(self):
        data = [[1, 2, 3], [3, 2, 1], [2, 1, 3]]
        r1 = krippendorff_alpha_ordinal(data, n_boot=200, seed=7)
        r2 = krippendorff_alpha_ordinal(data, n_boot=200, seed=7)
        self.assertAlmostEqual(r1["ci_lower"], r2["ci_lower"], places=10)
        self.assertAlmostEqual(r1["ci_upper"], r2["ci_upper"], places=10)

    def test_empty_raises(self):
        with self.assertRaises(ValueError):
            krippendorff_alpha_ordinal([])

    def test_single_rater_raises(self):
        # krippendorff library requires ≥2 raters — propagates as ValueError.
        with self.assertRaises(ValueError):
            krippendorff_alpha_ordinal([[1, 2, 3, 4, 5]])


# ---------------------------------------------------------------------------
# TestBhFdrAdjust
# ---------------------------------------------------------------------------

class TestBhFdrAdjust(unittest.TestCase):
    def test_standard_example(self):
        result = bh_fdr_adjust([0.001, 0.02, 0.03, 0.04, 0.5])
        self.assertEqual(result["rejected"], [True, True, True, True, False])
        np.testing.assert_allclose(result["adjusted"][0], 0.005, atol=1e-6)
        np.testing.assert_allclose(result["adjusted"][4], 0.5, atol=1e-6)
        self.assertEqual(result["q"], 0.05)
        self.assertEqual(result["raw"], [0.001, 0.02, 0.03, 0.04, 0.5])

    def test_all_large_none_rejected(self):
        result = bh_fdr_adjust([0.5, 0.6, 0.7])
        self.assertEqual(result["rejected"], [False, False, False])

    def test_all_tiny_all_rejected(self):
        result = bh_fdr_adjust([0.001, 0.001, 0.001])
        self.assertTrue(all(result["rejected"]))

    def test_custom_q_more_rejections(self):
        p_values = [0.001, 0.02, 0.03, 0.04, 0.5]
        r_strict = bh_fdr_adjust(p_values, q=0.05)
        r_lenient = bh_fdr_adjust(p_values, q=0.1)
        # q=0.1 must reject at least as many as q=0.05.
        strict_count = sum(r_strict["rejected"])
        lenient_count = sum(r_lenient["rejected"])
        self.assertGreaterEqual(lenient_count, strict_count)
        self.assertEqual(r_lenient["q"], 0.1)

    def test_empty_raises(self):
        with self.assertRaises(ValueError):
            bh_fdr_adjust([])


class TestWilcoxonNNonzero(unittest.TestCase):
    def test_all_nonzero(self):
        result = wilcoxon_paired([3, 4, 2, 5, 6, 4])
        self.assertEqual(result["n"], 6)
        self.assertEqual(result["n_nonzero"], 6)

    def test_some_zeros_excluded_from_n_nonzero(self):
        result = wilcoxon_paired([3, 0, 4, 2, 0, 5])
        self.assertEqual(result["n"], 6)
        self.assertEqual(result["n_nonzero"], 4)

    def test_both_n_and_n_nonzero_keys_present(self):
        result = wilcoxon_paired([1, 2, 3])
        self.assertIn("n", result)
        self.assertIn("n_nonzero", result)



# ---------------------------------------------------------------------------
# Section E: compute_inter_run_alpha
# ---------------------------------------------------------------------------

_STATS_CRITERIA = [
    "issue_detection", "actionability", "severity_accuracy",
    "coverage", "signal_to_noise", "depth", "novel_substantive_findings",
]


def _make_pr_result_for_alpha(scores_3: list) -> dict:
    """Build a pr_result where both systems share the same 3-run scores for every criterion."""
    pr: dict = {"skwad": {}, "claude_ci": {}}
    for sys in ("skwad", "claude_ci"):
        for crit in _STATS_CRITERIA:
            pr[sys][crit] = {"scores": list(scores_3)}
    return pr


class TestComputeInterRunAlpha(unittest.TestCase):
    def test_returns_all_seven_criteria(self):
        prs = [
            _make_pr_result_for_alpha([1, 1, 1]),
            _make_pr_result_for_alpha([3, 3, 3]),
            _make_pr_result_for_alpha([2, 2, 2]),
        ]
        result = compute_inter_run_alpha(prs)
        for c in _STATS_CRITERIA:
            self.assertIn(c, result)

    def test_empty_pr_results_all_criteria_none(self):
        result = compute_inter_run_alpha([])
        for c in _STATS_CRITERIA:
            self.assertIsNone(result[c])

    def test_warnings_key_present_when_criteria_fail(self):
        result = compute_inter_run_alpha([])
        self.assertIn("_warnings", result)

    def test_all_identical_scores_across_items_gives_none(self):
        # All items have scores [2,2,2] → flat set has 1 unique value → degenerate → None.
        prs = [_make_pr_result_for_alpha([2, 2, 2]) for _ in range(3)]
        result = compute_inter_run_alpha(prs)
        for c in _STATS_CRITERIA:
            self.assertIsNone(result[c])

    def test_perfect_agreement_gives_alpha_near_1(self):
        # Different item scores but all runs agree → inter-run alpha = 1.0.
        prs = [
            _make_pr_result_for_alpha([1, 1, 1]),
            _make_pr_result_for_alpha([3, 3, 3]),
            _make_pr_result_for_alpha([2, 2, 2]),
        ]
        result = compute_inter_run_alpha(prs)
        for c in _STATS_CRITERIA:
            if result[c] is not None:
                self.assertGreater(result[c], 0.9)

    # --- Producer guarantees the alpha gate depends on (_ci_lower is authoritative) ---

    def test_ci_lower_map_present_and_reproducible(self):
        # A non-degenerate input yields a _ci_lower map, and it is IDENTICAL across
        # two calls — locking the seed=0 reproducibility the gate relies on.
        prs = [
            _make_pr_result_for_alpha([1, 2, 3]),
            _make_pr_result_for_alpha([3, 1, 2]),
            _make_pr_result_for_alpha([2, 3, 1]),
        ]
        r1 = compute_inter_run_alpha(prs)
        r2 = compute_inter_run_alpha(prs)
        self.assertIn("_ci_lower", r1)
        self.assertIn("issue_detection", r1["_ci_lower"])
        self.assertEqual(r1["_ci_lower"], r2["_ci_lower"])  # reproducible → seed fixed

    def test_all_identical_labeled_perfect_agreement(self):
        prs = [_make_pr_result_for_alpha([2, 2, 2]) for _ in range(3)]
        result = compute_inter_run_alpha(prs)
        self.assertIsNone(result["issue_detection"])
        self.assertEqual(result["_degenerate_reason"]["issue_detection"], "perfect_agreement")
        # Degenerate criteria carry NO CI lower bound (the gate must treat them by reason).
        self.assertNotIn("issue_detection", result.get("_ci_lower", {}))

    def test_insufficient_items_labeled_distinctly(self):
        # Only one (PR × system) item has scores → <2 items → insufficient_items.
        one_item = [{
            "skwad": {c: {"scores": [1, 2, 3]} for c in _STATS_CRITERIA},
            "claude_ci": {},
        }]
        result = compute_inter_run_alpha(one_item)
        self.assertIsNone(result["issue_detection"])
        self.assertEqual(result["_degenerate_reason"]["issue_detection"], "insufficient_items")

    def test_degenerate_reasons_are_distinct_across_paths(self):
        # The producer must label the two degenerate paths DIFFERENTLY so the gate can
        # pass perfect-agreement while failing unmeasurable.
        perfect = compute_inter_run_alpha([_make_pr_result_for_alpha([2, 2, 2]) for _ in range(3)])
        insufficient = compute_inter_run_alpha([])
        self.assertEqual(perfect["_degenerate_reason"]["coverage"], "perfect_agreement")
        self.assertEqual(insufficient["_degenerate_reason"]["coverage"], "insufficient_items")
        self.assertNotEqual(
            perfect["_degenerate_reason"]["coverage"],
            insufficient["_degenerate_reason"]["coverage"],
        )


# ---------------------------------------------------------------------------
# Section E: check_methodology_version
# ---------------------------------------------------------------------------

class TestCheckMethodologyVersion(unittest.TestCase):
    def test_single_v2_version_no_raise(self):
        records = [{"methodology_version": 2}, {"methodology_version": 2}]
        check_methodology_version(records)

    def test_empty_list_no_raise(self):
        check_methodology_version([])

    def test_mixed_v1_v2_raises(self):
        records = [{"methodology_version": 1}, {"methodology_version": 2}]
        with self.assertRaises(MethodologyMismatchError):
            check_methodology_version(records)

    def test_records_without_version_field_treated_as_v1(self):
        # Missing field defaults to version 1. Mixing with explicit v2 must raise.
        records = [{}, {"methodology_version": 2}]
        with self.assertRaises(MethodologyMismatchError):
            check_methodology_version(records)


# ---------------------------------------------------------------------------
# Section A: stats functions invoke check_methodology_version via records= kwarg
# (Reviewer Critical #1)
# ---------------------------------------------------------------------------

import unittest.mock as _mock

_MIXED_RECORDS = [{"methodology_version": 1}, {"methodology_version": 2}]


class TestStatsRecordsKwargInvokesCheck(unittest.TestCase):
    """Each stats function must invoke check_methodology_version when records= is set."""

    def test_wilcoxon_paired_records_kwarg_invokes_check(self):
        with self.assertRaises(MethodologyMismatchError):
            wilcoxon_paired([1, 2, 3], records=_MIXED_RECORDS)

    def test_cliffs_delta_records_kwarg_invokes_check(self):
        with self.assertRaises(MethodologyMismatchError):
            cliffs_delta([1, 2], [1, 2], records=_MIXED_RECORDS)

    def test_cliffs_delta_bca_ci_records_kwarg_invokes_check(self):
        with self.assertRaises(MethodologyMismatchError):
            cliffs_delta_bca_ci([1, 2], [1, 2], n_boot=10, seed=42, records=_MIXED_RECORDS)

    def test_krippendorff_alpha_records_kwarg_invokes_check(self):
        with self.assertRaises(MethodologyMismatchError):
            krippendorff_alpha_ordinal(
                [[1, 2, 3], [1, 2, 3]], n_boot=10, seed=42, records=_MIXED_RECORDS
            )

    def test_stats_records_none_skips_check(self):
        # Default (records omitted) → check_methodology_version is NOT called.
        # Confirms legacy compat: callers that don't opt into the gate aren't broken.
        with _mock.patch("eval.lib.stats.check_methodology_version") as mock_check:
            wilcoxon_paired([1, 2, 3])
            cliffs_delta([1, 2], [1, 2])
            cliffs_delta_bca_ci([1, 2], [1, 2], n_boot=10, seed=42)
            krippendorff_alpha_ordinal([[1, 2, 3], [1, 2, 3]], n_boot=10, seed=42)
        mock_check.assert_not_called()


if __name__ == "__main__":
    unittest.main()
