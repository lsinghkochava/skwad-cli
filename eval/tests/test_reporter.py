"""Tests for eval.lib.reporter.generate_research_report."""

import os
import tempfile
import unittest

from eval.lib.reporter import (
    _claim_trace_collapsible,
    _verification_summary_table,
    generate_research_report,
)
from eval.lib.stats import MethodologyMismatchError

_CRITERIA_IDS = [
    "issue_detection", "actionability", "severity_accuracy",
    "coverage", "signal_to_noise", "depth", "novel_substantive_findings",
]


def _sys_scores(total: int, *, issue_detection_voted: int = 1, n_runs: int = 3) -> dict:
    data = {}
    for cid in _CRITERIA_IDS:
        voted = issue_detection_voted if cid == "issue_detection" else 1
        data[cid] = {"scores": [voted] * 3, "voted": voted, "reasoning_runs": ["reason"] * 3}
    data["total"] = total
    data["n_runs_completed"] = n_runs
    data["n_runs_planned"] = 3
    return data


def _make_pr(
    pr_number: int = 1,
    skwad_total: int = 12,
    ci_total: int = 9,
    difficulty: str = "easy",
    n_runs: int = 3,
) -> dict:
    return {
        "pr_data": {"repo": "Kochava/repo", "pr_number": pr_number, "title": "Test PR", "files_changed": 3},
        "difficulty": {"bucket": difficulty, "heuristic_bucket": difficulty, "llm_delta": 0, "reasoning": "test"},
        "skwad_review": "skwad review text",
        "claude_ci_review": "ci review text",
        "skwad": _sys_scores(skwad_total, issue_detection_voted=2, n_runs=n_runs),
        "claude_ci": _sys_scores(ci_total, issue_detection_voted=1, n_runs=n_runs),
        "runs": [
            {"ab_assignment": ["skwad", "claude_ci"],
             "resolved": {"skwad": {"total": skwad_total}, "claude_ci": {"total": ci_total}}},
            {"ab_assignment": ["claude_ci", "skwad"],
             "resolved": {"skwad": {"total": skwad_total}, "claude_ci": {"total": ci_total}}},
        ],
        "ab_assignments": [["skwad", "claude_ci"], ["claude_ci", "skwad"]],
        "n_runs_completed": n_runs,
    }


def _make_manifest(seed: int = 42) -> dict:
    return {
        "run_id": "test-id",
        "rng_seed": seed,
        "prompt_hashes": {
            "rubric_json_sha256": "a" * 64,
            "judge_team_json_sha256": "b" * 64,
        },
        "prs": [{"repo": "Kochava/repo", "pr": 1, "commit_sha": "abc123", "difficulty": "easy"}],
        "skipped_prs": [],
    }


# 3 PRs with distinct totals — ensures non-zero diffs and variance for stats
_THREE_PRS = [
    _make_pr(pr_number=1, skwad_total=12, ci_total=9),
    _make_pr(pr_number=2, skwad_total=11, ci_total=10),
    _make_pr(pr_number=3, skwad_total=13, ci_total=8),
]


class TestReporterStructure(unittest.TestCase):
    def setUp(self):
        self._tmpdir = tempfile.TemporaryDirectory()
        self._path = os.path.join(self._tmpdir.name, "report.md")
        generate_research_report(_THREE_PRS, _make_manifest(), self._path)
        with open(self._path) as f:
            self._report = f.read()

    def tearDown(self):
        self._tmpdir.cleanup()

    def test_file_is_written(self):
        self.assertTrue(os.path.exists(self._path))

    def test_report_is_nonempty(self):
        self.assertGreater(len(self._report), 500)

    def test_title_present(self):
        self.assertIn("# Research Report", self._report)

    def test_all_11_section_headers_present(self):
        for i in range(1, 12):
            self.assertIn(f"## {i}.", self._report, f"Missing section header ## {i}.")

    def test_sections_appear_in_order(self):
        positions = [self._report.index(f"## {i}.") for i in range(1, 12)]
        self.assertEqual(positions, sorted(positions))


class TestExecutiveSummary(unittest.TestCase):
    def setUp(self):
        self._tmpdir = tempfile.TemporaryDirectory()
        path = os.path.join(self._tmpdir.name, "r.md")
        generate_research_report(_THREE_PRS, _make_manifest(), path)
        with open(path) as f:
            self._report = f.read()
        s1_start = self._report.index("## 1.")
        s2_start = self._report.index("## 2.")
        self._sec1 = self._report[s1_start:s2_start]

    def tearDown(self):
        self._tmpdir.cleanup()

    def test_verdict_present(self):
        self.assertIn("**Verdict**", self._sec1)

    def test_zheng_bias_caveat_in_summary(self):
        self.assertIn("Zheng et al.", self._sec1)

    def test_favors_claude_ci_in_summary(self):
        self.assertIn("favors Claude CI", self._sec1)


class TestThreatsToValidity(unittest.TestCase):
    def setUp(self):
        self._tmpdir = tempfile.TemporaryDirectory()
        path = os.path.join(self._tmpdir.name, "r.md")
        generate_research_report(_THREE_PRS, _make_manifest(), path)
        with open(path) as f:
            self._report = f.read()
        s8_start = self._report.index("## 8.")
        s9_start = self._report.index("## 9.")
        self._sec8 = self._report[s8_start:s9_start]

    def tearDown(self):
        self._tmpdir.cleanup()

    def test_zheng_2023_cited(self):
        self.assertIn("Zheng et al. (2023)", self._sec8)

    def test_same_model_judge_mentioned(self):
        self.assertIn("same-model", self._sec8.lower())

    def test_favors_claude_ci_direction(self):
        self.assertIn("favors Claude CI", self._sec8)


class TestDegradedEvidence(unittest.TestCase):
    def _run(self, prs):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "r.md")
            generate_research_report(prs, _make_manifest(), path)
            with open(path) as f:
                return f.read()

    def test_degraded_marker_for_incomplete_runs(self):
        pr = _make_pr(pr_number=1, n_runs=2)
        report = self._run([pr])
        self.assertIn("⚠️ degraded evidence", report)
        self.assertIn("(2/3 runs)", report)

    def test_no_degraded_marker_for_complete_runs(self):
        report = self._run([_make_pr(pr_number=1, n_runs=3)])
        self.assertNotIn("⚠️ degraded evidence", report)


class TestCollapsibleBlocks(unittest.TestCase):
    def setUp(self):
        self._tmpdir = tempfile.TemporaryDirectory()
        path = os.path.join(self._tmpdir.name, "r.md")
        generate_research_report(_THREE_PRS, _make_manifest(), path)
        with open(path) as f:
            self._report = f.read()

    def tearDown(self):
        self._tmpdir.cleanup()

    def test_details_tags_present(self):
        self.assertIn("<details>", self._report)

    def test_four_collapsibles_per_pr(self):
        # _sec4_per_pr emits exactly 4 _collapsible() calls per PR
        count = self._report.count("<details>")
        self.assertEqual(count, 4 * len(_THREE_PRS))


class TestStatsIntegration(unittest.TestCase):
    def setUp(self):
        self._tmpdir = tempfile.TemporaryDirectory()
        path = os.path.join(self._tmpdir.name, "r.md")
        generate_research_report(_THREE_PRS, _make_manifest(), path)
        with open(path) as f:
            self._report = f.read()
        s7_start = self._report.index("## 7.")
        s8_start = self._report.index("## 8.")
        self._sec7 = self._report[s7_start:s8_start]

    def tearDown(self):
        self._tmpdir.cleanup()

    def test_n_nonzero_in_section7(self):
        self.assertIn("n_nonzero", self._sec7)

    def test_bh_adj_p_label_present(self):
        self.assertIn("BH-adj p", self._sec7)

    def test_cliffs_delta_label_in_section7(self):
        self.assertIn("δ", self._sec7)

    def test_bca_ci_label_in_section7(self):
        self.assertIn("BCa CI", self._sec7)


class TestInterCriterionCorrelation(unittest.TestCase):
    def setUp(self):
        self._tmpdir = tempfile.TemporaryDirectory()
        path = os.path.join(self._tmpdir.name, "r.md")
        generate_research_report(_THREE_PRS, _make_manifest(), path)
        with open(path) as f:
            self._report = f.read()
        s5_start = self._report.index("## 5.")
        s6_start = self._report.index("## 6.")
        self._sec5 = self._report[s5_start:s6_start]

    def tearDown(self):
        self._tmpdir.cleanup()

    def test_correlation_matrix_header(self):
        self.assertIn("Inter-Criterion Correlation Matrix", self._sec5)

    def test_diagonal_shows_100(self):
        # 7 criteria → 7 diagonal entries each showing "1.00"
        self.assertGreaterEqual(self._sec5.count("1.00"), 7)


class TestPerDifficulty(unittest.TestCase):
    def _run(self, prs):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "r.md")
            generate_research_report(prs, _make_manifest(), path)
            with open(path) as f:
                report = f.read()
        s5_start = report.index("## 5.")
        s6_start = report.index("## 6.")
        return report[s5_start:s6_start]

    def test_only_easy_prs_medium_and_hard_show_na(self):
        prs = [_make_pr(pr_number=i + 1, difficulty="easy") for i in range(3)]
        sec5 = self._run(prs)
        self.assertIn("N/A", sec5)

    def test_difficulty_none_excluded_from_per_difficulty_table(self):
        pr = _make_pr(pr_number=1)
        pr["difficulty"] = None
        sec5 = self._run([pr])
        # All buckets have no PRs → all show N/A
        self.assertIn("N/A", sec5)


class TestEmptyEdgeCase(unittest.TestCase):
    def test_empty_pr_results_writes_file(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "r.md")
            generate_research_report([], _make_manifest(), path)
            self.assertTrue(os.path.exists(path))

    def test_empty_pr_results_has_all_sections(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "r.md")
            generate_research_report([], _make_manifest(), path)
            with open(path) as f:
                report = f.read()
        for i in range(1, 12):
            self.assertIn(f"## {i}.", report, f"Missing section ## {i}. for empty input")

    def test_missing_manifest_fields_no_crash(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "r.md")
            generate_research_report(_THREE_PRS, {}, path)
            self.assertTrue(os.path.exists(path))


class TestPositionBias(unittest.TestCase):
    def _sec7(self, prs):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "r.md")
            generate_research_report(prs, _make_manifest(), path)
            with open(path) as f:
                report = f.read()
        s7_start = report.index("## 7.")
        s8_start = report.index("## 8.")
        return report[s7_start:s8_start]

    def test_position_bias_both_systems_present(self):
        # _THREE_PRS each have 2+ runs — both checks should appear in §7
        sec7 = self._sec7(_THREE_PRS)
        self.assertIn("skwad-cli position check", sec7)
        self.assertIn("Claude CI position check", sec7)
        # Both lines include "p = " (value or N/A)
        self.assertIn("p = ", sec7)

    def test_position_bias_asymmetric_framing(self):
        sec7 = self._sec7(_THREE_PRS)
        self.assertIn("reported separately and never collapsed", sec7)

    def test_position_bias_handles_short_runs_array(self):
        # PRs with runs length < 2 — must not crash and must degrade gracefully
        pr = _make_pr(pr_number=1)
        pr["runs"] = []  # no runs
        sec7 = self._sec7([pr])
        # Both checks report insufficient data rather than crashing
        self.assertIn("insufficient run data", sec7)

    def test_claude_ci_review_no_fallback(self):
        # Coder dropped the claude_review fallback — only claude_ci_review is used
        pr = _make_pr(pr_number=1)
        del pr["claude_ci_review"]
        pr["claude_review"] = "old review text that must not appear"
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "r.md")
            generate_research_report([pr], _make_manifest(), path)
            with open(path) as f:
                report = f.read()
        self.assertNotIn("old review text that must not appear", report)
        self.assertIn("(none)", report)



# ---------------------------------------------------------------------------
# Section H: _verification_summary_table
# ---------------------------------------------------------------------------

def _make_vs(verified=4, unverified=1, contradicted=1, non_falsifiable=0,
             rate=0.8, tool_calls=5):
    return {
        "claims_verified": verified,
        "claims_unverified": unverified,
        "claims_contradicted": contradicted,
        "claims_non_falsifiable": non_falsifiable,
        "verification_rate": rate,
        "tool_calls_observed": tool_calls,
    }


class TestVerificationSummaryTable(unittest.TestCase):
    def test_empty_result_returns_empty_string(self):
        self.assertEqual(_verification_summary_table({"skwad": {}, "claude_ci": {}}), "")

    def test_with_vs_returns_nonempty_table_with_headers(self):
        result_dict = {"skwad": {"verification_summary": _make_vs()}, "claude_ci": {}}
        table = _verification_summary_table(result_dict)
        self.assertNotEqual(table, "")
        self.assertIn("Verified", table)

    def test_table_includes_both_systems_when_both_have_vs(self):
        result_dict = {
            "skwad": {"verification_summary": _make_vs(verified=2)},
            "claude_ci": {"verification_summary": _make_vs(verified=3)},
        }
        table = _verification_summary_table(result_dict)
        self.assertIn("Skwad", table)
        self.assertIn("Claude CI", table)


# ---------------------------------------------------------------------------
# Section H: _claim_trace_collapsible
# ---------------------------------------------------------------------------

class TestClaimTraceCollapsible(unittest.TestCase):
    def test_no_runs_returns_empty_string(self):
        self.assertEqual(_claim_trace_collapsible({"runs": []}, "skwad", "Skwad"), "")

    def test_runs_without_claim_trace_returns_empty_string(self):
        run = {
            "raw_response": {"review_a": {}, "review_b": {}},
            "ab_assignment": ["skwad", "claude_ci"],
        }
        result = _claim_trace_collapsible({"runs": [run]}, "skwad", "Skwad")
        self.assertEqual(result, "")

    def test_run_with_claim_trace_returns_collapsible_block(self):
        run = {
            "raw_response": {
                "review_a": {"claim_trace": [{
                    "claim_text": "A claim about X",
                    "outcome": "verified",
                    "tools_used": ["Read"],
                    "evidence": "evidence text",
                }]},
            },
            "ab_assignment": ["skwad", "claude_ci"],
        }
        result = _claim_trace_collapsible({"runs": [run]}, "skwad", "Skwad")
        self.assertIn("<details>", result)
        self.assertIn("Claim trace", result)


# ---------------------------------------------------------------------------
# Section A: generate_research_report invokes check_methodology_version
# (Reviewer Critical #1)
# ---------------------------------------------------------------------------

class TestReporterMethodologyGate(unittest.TestCase):
    def test_generate_research_report_raises_on_mixed_methodology(self):
        # Manifest defaults to version 1; one PR declares version 2 → mismatch.
        pr_v1 = _make_pr(pr_number=1)  # inherits manifest version (1)
        pr_v2 = {**_make_pr(pr_number=2), "methodology_version": 2}
        manifest = _make_manifest()  # no methodology_version → defaults to 1
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "r.md")
            with self.assertRaises(MethodologyMismatchError):
                generate_research_report([pr_v1, pr_v2], manifest, path)

    def test_generate_research_report_passes_uniform_v2(self):
        manifest = {**_make_manifest(), "methodology_version": 2}
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "r.md")
            generate_research_report(_THREE_PRS, manifest, path)
            self.assertTrue(os.path.exists(path))


if __name__ == "__main__":
    unittest.main()
