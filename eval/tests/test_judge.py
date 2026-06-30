"""Tests for eval.lib.judge."""

import json
import os
import tempfile
import time
import unittest
from unittest.mock import MagicMock, call, patch

from eval.lib.judge import (
    BINARY_TIMEOUT_BUFFER_SEC,
    CRITERIA,
    DIFF_TRUNCATION_CAP,
    ALLOWED_JUDGE_TOOLS,
    ConfabulationDetected,
    StructuralInvalidRun,
    _aggregate_verification_summaries,
    _apply_canary_injections,
    _backfill_tool_calls_observed,
    _check_canary_outcomes,
    _check_confabulation,
    _check_disallowed_tools,
    _median_vote,
    _parse_stderr_metadata,
    _run_and_verify,
    _run_judge_once,
    _sum_verified_in_output,
    _truncate_diff,
    _unswap,
    _validate_response_structure,
    _warn_trace_divergence,
    count_tool_calls,
    score_paired_reviews,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_criteria(score: int, reasoning: str = "", justifications=None) -> dict:
    """Build a criteria dict for one criterion."""
    entry = {"score": score, "reasoning": reasoning}
    if justifications is not None:
        entry["justifications"] = justifications
    return entry


_COUNT_CRITERIA_TEST = {"issue_detection", "coverage", "depth", "novel_substantive_findings"}


def _make_judge_json(a_scores: dict[str, int], b_scores: dict[str, int]) -> dict:
    """Build a parsed judge JSON with per-criterion scores for review_a and review_b.

    Includes the 4-bucket fields (verified/unverified/contradicted/non_falsifiable)
    on count criteria so the structural validator accepts it. All buckets default
    to 0 — tests don't exercise confabulation/structural paths unless they
    explicitly mock _validate_response_structure.
    """
    def _build_review(scores: dict[str, int]) -> dict:
        criteria = {}
        for c in CRITERIA:
            s = scores.get(c, 0)
            entry = {"score": s, "reasoning": f"r_{c}_{s}"}
            if c in _COUNT_CRITERIA_TEST:
                entry["verified_findings"] = 0
                entry["unverified_findings"] = 0
                entry["contradicted_findings"] = 0
                entry["non_falsifiable_findings"] = 0
            if c == "novel_substantive_findings":
                entry["justifications"] = [f"j_{s}"] if s > 0 else []
            criteria[c] = entry
        return {"criteria": criteria, "total": sum(scores.get(c, 0) for c in CRITERIA), "claim_trace": []}
    return {
        "review_a": _build_review(a_scores),
        "review_b": _build_review(b_scores),
    }


def _uniform_judge_json(a_score: int, b_score: int) -> dict:
    """All criteria get a_score / b_score uniformly."""
    scores = {c: a_score for c in CRITERIA}
    b = {c: b_score for c in CRITERIA}
    return _make_judge_json(scores, b)


def _make_resolved(skwad_scores: dict[str, int], ci_scores: dict[str, int]) -> dict:
    """Build a single resolved dict suitable for _median_vote input."""
    def _build(scores):
        criteria = {}
        for c in CRITERIA:
            s = scores.get(c, 0)
            entry = {"score": s, "reasoning": f"r_{s}"}
            if c == "novel_substantive_findings":
                entry["justifications"] = [f"j{i}" for i in range(s)]
            criteria[c] = entry
        return {"criteria": criteria, "total": sum(scores.get(c, 0) for c in CRITERIA)}
    return {
        "skwad": _build(skwad_scores),
        "claude_ci": _build(ci_scores),
    }


def _uniform_resolved(skwad_score: int, ci_score: int) -> dict:
    return _make_resolved({c: skwad_score for c in CRITERIA}, {c: ci_score for c in CRITERIA})


# ---------------------------------------------------------------------------
# TestTruncateDiff
# ---------------------------------------------------------------------------

class TestTruncateDiff(unittest.TestCase):
    def test_short_diff_unchanged(self):
        diff = "+a line\n" * 100
        result = _truncate_diff(diff)
        self.assertIn("=== DIFF ===", result)
        self.assertNotIn("truncated", result)

    def test_big_diff_notice(self):
        big = "x" * (DIFF_TRUNCATION_CAP + 1)
        result = _truncate_diff(big)
        self.assertIn(f"truncated at {DIFF_TRUNCATION_CAP} chars", result)
        self.assertIn("full diff is", result)
        self.assertIn(str(len(big)), result)

    def test_boundary_exactly_cap_no_truncation(self):
        # Uses strict > so len == cap → no truncation notice.
        diff = "x" * DIFF_TRUNCATION_CAP
        result = _truncate_diff(diff)
        self.assertNotIn("truncated", result)
        self.assertIn("=== DIFF ===", result)


# ---------------------------------------------------------------------------
# TestUnswap
# ---------------------------------------------------------------------------

class TestUnswap(unittest.TestCase):
    def _parsed(self):
        return {
            "review_a": {"criteria": {"issue_detection": {"score": 3}}, "total": 5},
            "review_b": {"criteria": {"issue_detection": {"score": 1}}, "total": 8},
        }

    def test_a_is_skwad(self):
        result = _unswap(self._parsed(), "skwad", "claude_ci")
        self.assertEqual(result["skwad"]["total"], 5)
        self.assertEqual(result["claude_ci"]["total"], 8)
        self.assertEqual(result["skwad"]["criteria"]["issue_detection"]["score"], 3)

    def test_a_is_claude_ci(self):
        # Inverted assignment — skwad was review_b.
        result = _unswap(self._parsed(), "claude_ci", "skwad")
        self.assertEqual(result["claude_ci"]["total"], 5)
        self.assertEqual(result["skwad"]["total"], 8)
        self.assertEqual(result["skwad"]["criteria"]["issue_detection"]["score"], 1)


# ---------------------------------------------------------------------------
# TestMedianVote
# ---------------------------------------------------------------------------

class TestMedianVote(unittest.TestCase):
    def _vote(self, skwad_runs, ci_runs):
        """Build 3 resolved runs where all criteria share the same score."""
        resolved = [_uniform_resolved(s, c) for s, c in zip(skwad_runs, ci_runs)]
        return _median_vote(resolved)

    def test_median_of_1_2_3(self):
        result = self._vote([1, 2, 3], [0, 0, 0])
        for c in CRITERIA:
            self.assertEqual(result["skwad"][c]["voted"], 2)

    def test_full_agreement(self):
        result = self._vote([3, 3, 3], [1, 1, 1])
        for c in CRITERIA:
            self.assertEqual(result["skwad"][c]["voted"], 3)

    def test_majority_wins(self):
        result = self._vote([0, 3, 0], [0, 0, 0])
        for c in CRITERIA:
            self.assertEqual(result["skwad"][c]["voted"], 0)

    def test_two_same_one_different(self):
        result = self._vote([1, 2, 2], [0, 0, 0])
        for c in CRITERIA:
            self.assertEqual(result["skwad"][c]["voted"], 2)

    def test_scores_array_preserved(self):
        result = self._vote([1, 2, 3], [0, 0, 0])
        self.assertEqual(result["skwad"]["issue_detection"]["scores"], [1, 2, 3])

    def test_reasoning_runs_preserved(self):
        result = self._vote([2, 2, 2], [1, 1, 1])
        self.assertEqual(len(result["skwad"]["issue_detection"]["reasoning_runs"]), 3)

    def test_novel_justifications_pooled(self):
        # 3 runs each with 2 justifications for novel_substantive_findings (score=2).
        resolved = []
        for _ in range(3):
            r = _uniform_resolved(2, 0)
            r["skwad"]["criteria"]["novel_substantive_findings"]["justifications"] = ["jA", "jB"]
            resolved.append(r)
        result = _median_vote(resolved)
        entry = result["skwad"]["novel_substantive_findings"]
        # voted=2; justifications pooled from 3 runs = 6 total, truncated to voted=2.
        self.assertEqual(entry["voted"], 2)
        self.assertEqual(len(entry["justifications"]), 2)

    def test_novel_justifications_empty_when_voted_zero(self):
        resolved = [_uniform_resolved(0, 0) for _ in range(3)]
        result = _median_vote(resolved)
        entry = result["skwad"]["novel_substantive_findings"]
        self.assertEqual(entry["justifications"], [])

    def test_total_is_sum_of_voted(self):
        result = self._vote([3, 3, 3], [1, 1, 1])
        self.assertEqual(result["skwad"]["total"], 3 * len(CRITERIA))
        self.assertEqual(result["claude_ci"]["total"], 1 * len(CRITERIA))

    def test_median_low_on_disagreement(self):
        # median_low returns the lower observed value on even-count ties.
        two_run_0_3 = [_uniform_resolved(0, 0), _uniform_resolved(3, 0)]
        result = _median_vote(two_run_0_3)
        for c in CRITERIA:
            self.assertEqual(result["skwad"][c]["voted"], 0, f"criterion {c}: expected 0")

        two_run_1_3 = [_uniform_resolved(1, 0), _uniform_resolved(3, 0)]
        result = _median_vote(two_run_1_3)
        for c in CRITERIA:
            self.assertEqual(result["skwad"][c]["voted"], 1, f"criterion {c}: expected 1")

        # 3-run case unchanged: median_low([1,2,3]) == 2.
        three_runs = [_uniform_resolved(s, 0) for s in (1, 2, 3)]
        result = _median_vote(three_runs)
        for c in CRITERIA:
            self.assertEqual(result["skwad"][c]["voted"], 2, f"criterion {c}: expected 2")

    def test_justifications_from_matching_run(self):
        nsf = "novel_substantive_findings"

        def _nsf_run(score, justifications):
            r = _uniform_resolved(0, 0)
            r["skwad"]["criteria"][nsf] = {
                "score": score,
                "reasoning": f"r_{score}",
                "justifications": justifications,
            }
            return r

        runs = [
            _nsf_run(1, ["DISSENTING - shouldn't appear"]),
            _nsf_run(3, ["match-r2-A", "match-r2-B", "match-r2-C", "match-r2-D"]),
            _nsf_run(3, ["match-r3-A", "match-r3-B", "match-r3-C", "match-r3-D"]),
        ]
        result = _median_vote(runs)
        entry = result["skwad"][nsf]

        self.assertEqual(entry["voted"], 3)
        self.assertNotIn("DISSENTING - shouldn't appear", entry["justifications"])
        # Taken from first matching run (run2), truncated to voted=3.
        self.assertEqual(len(entry["justifications"]), 3)
        self.assertTrue(all("match-r2" in j for j in entry["justifications"]))

        # Edge case: all runs score=0 → justifications=[].
        zero_runs = [_nsf_run(0, ["never"]) for _ in range(3)]
        zero_result = _median_vote(zero_runs)
        self.assertEqual(zero_result["skwad"][nsf]["justifications"], [])


# ---------------------------------------------------------------------------
# TestRunJudgeOnce
# ---------------------------------------------------------------------------

class TestRunJudgeOnce(unittest.TestCase):
    def test_nonzero_exit_raises_and_logs_warning(self):
        failed = MagicMock()
        failed.returncode = 1
        failed.stderr = "judge agent crashed"

        with tempfile.TemporaryDirectory() as tmpdir:
            with patch("eval.lib.judge.subprocess.run", return_value=failed):
                with self.assertLogs("eval.lib.judge", level="WARNING") as cm:
                    with self.assertRaises(RuntimeError):
                        _run_judge_once(
                            diff="", review_a="a", review_b="b",
                            skwad_binary="./skwad", repo_path=tmpdir,
                            config_path="cfg.json",
                            judge_output_name="out.json", port=8800,
                            task_start_time=time.time(),
                        )
        self.assertTrue(any("agent crashed" in m for m in cm.output))

    def test_missing_output_file_raises(self):
        # MCP came up (so we pass the MCP-listening guard) but no output file was
        # written → OutputFreshnessError (a RuntimeError subclass).
        ok = MagicMock()
        ok.returncode = 0
        ok.stderr = "MCP server listening on 127.0.0.1:8800\n"

        with tempfile.TemporaryDirectory() as tmpdir:
            with patch("eval.lib.judge.subprocess.run", return_value=ok):
                with self.assertRaises(RuntimeError, msg="should raise when output file missing"):
                    _run_judge_once(
                        diff="", review_a="a", review_b="b",
                        skwad_binary="./skwad", repo_path=tmpdir,
                        config_path="cfg.json",
                        judge_output_name="out.json", port=8800,
                        task_start_time=time.time(),
                    )


# ---------------------------------------------------------------------------
# TestJudgeTimeoutArg — regression guard for the "binary killed judge at 600s"
# bug. The caller MUST pass --timeout to the skwad-cli binary (otherwise the
# binary's internal 10m default fires and kills the judge at 600s, exit 143,
# no judge_output.json). The Python wrapper timeout MUST be binary + buffer so
# the binary self-terminates gracefully before the wrapper force-kills it.
# ---------------------------------------------------------------------------


def _timeout_value(cmd):
    """Return the Go-duration string following --timeout in a skwad-cli argv."""
    return cmd[cmd.index("--timeout") + 1]


class TestJudgeTimeoutArg(unittest.TestCase):
    def _capture_run(self, timeout):
        """Invoke _run_judge_once with mocked subprocess; return the mock."""
        ok = MagicMock()
        ok.returncode = 0  # passes returncode check, then raises (no MCP line → MCPUnavailable)
        ok.stderr = ""
        with tempfile.TemporaryDirectory() as tmpdir:
            with patch("eval.lib.judge.subprocess.run", return_value=ok) as m:
                with self.assertRaises(RuntimeError):
                    _run_judge_once(
                        diff="", review_a="a", review_b="b",
                        skwad_binary="./skwad", repo_path=tmpdir,
                        config_path="cfg.json",
                        judge_output_name="out.json", port=8800,
                        task_start_time=time.time(),
                        timeout=timeout,
                    )
        return m

    def test_timeout_flag_present_with_go_duration_value(self):
        m = self._capture_run(timeout=1200)
        cmd = m.call_args.args[0]
        self.assertIn("--timeout", cmd, "judge must pass --timeout to the skwad-cli binary")
        self.assertEqual(_timeout_value(cmd), "1200s", "--timeout must be a Go-duration string")

    def test_timeout_flag_tracks_arg_value(self):
        # A different timeout must flow through verbatim (no hard-coded constant).
        m = self._capture_run(timeout=300)
        self.assertEqual(_timeout_value(m.call_args.args[0]), "300s")

    def test_wrapper_timeout_is_binary_plus_buffer(self):
        # The subprocess wrapper must outlive the binary by exactly the buffer,
        # so the binary is authoritative and stops first.
        m = self._capture_run(timeout=1200)
        self.assertEqual(m.call_args.kwargs["timeout"], 1200 + BINARY_TIMEOUT_BUFFER_SEC)


# ---------------------------------------------------------------------------
# TestScorePairedReviews
# ---------------------------------------------------------------------------

class TestScorePairedReviews(unittest.TestCase):
    _PR_DATA = {"pr_number": 99, "diff": "+fix\n"}

    @staticmethod
    def _to_verify_response(parsed_or_exc):
        """Wrap a parsed dict into the (parsed, stderr_meta) tuple _run_and_verify returns."""
        if isinstance(parsed_or_exc, Exception):
            raise parsed_or_exc
        return (parsed_or_exc, {})

    def _call(self, side_effect, seed=42):
        # Wrap each side-effect item to match _run_and_verify's return signature.
        def _wrapped_side_effect(*args, **kwargs):
            item = next(side_effect)
            return self._to_verify_response(item)

        with tempfile.TemporaryDirectory() as run_dir:
            with patch("eval.lib.judge._run_and_verify", side_effect=_wrapped_side_effect):
                result = score_paired_reviews(
                    pr_data=self._PR_DATA,
                    skwad_review="skwad review text",
                    claude_ci_review="ci review text",
                    skwad_binary="./skwad",
                    repo_path=run_dir,
                    seed=seed,
                    run_dir=run_dir,
                )
            return result, run_dir

    def _uniform_response(self, a_score, b_score):
        return _uniform_judge_json(a_score, b_score)

    def test_ab_assignments_first_two_fixed(self):
        responses = iter([self._uniform_response(2, 2)] * 3)
        result, _ = self._call(responses)
        self.assertEqual(result["ab_assignments"][0], ["skwad", "claude_ci"])
        self.assertEqual(result["ab_assignments"][1], ["claude_ci", "skwad"])

    def test_run3_deterministic_with_seed(self):
        # Same seed → identical run3 assignment across two independent calls.
        def responses():
            return iter([self._uniform_response(2, 2)] * 3)

        def _wrap(it):
            def _side(*a, **kw):
                return (next(it), {})
            return _side

        with tempfile.TemporaryDirectory() as d1:
            with patch("eval.lib.judge._run_and_verify", side_effect=_wrap(responses())):
                r1 = score_paired_reviews(
                    pr_data=self._PR_DATA, skwad_review="s", claude_ci_review="c",
                    skwad_binary="./skwad", repo_path=d1, seed=42, run_dir=d1,
                )
        with tempfile.TemporaryDirectory() as d2:
            with patch("eval.lib.judge._run_and_verify", side_effect=_wrap(responses())):
                r2 = score_paired_reviews(
                    pr_data=self._PR_DATA, skwad_review="s", claude_ci_review="c",
                    skwad_binary="./skwad", repo_path=d2, seed=42, run_dir=d2,
                )
        self.assertEqual(r1["ab_assignments"][2], r2["ab_assignments"][2])

    def test_unswap_and_median_compose_correctly(self):
        # Position-biased judge: always scores review_a=3, review_b=1.
        # Seed=42 → assignments: [(skwad,ci), (ci,skwad), (skwad,ci)]
        # After unswap: skwad=[3,1,3], ci=[1,3,1] → medians: skwad=3, ci=1.
        responses = [self._uniform_response(3, 1)] * 3
        result, _ = self._call(iter(responses), seed=42)
        skwad_total = result["skwad"]["total"]
        ci_total = result["claude_ci"]["total"]
        # skwad was A in 2/3 runs → median 3 per criterion; ci was A in 1/3 → median 1.
        self.assertEqual(skwad_total, 3 * len(CRITERIA))
        self.assertEqual(ci_total, 1 * len(CRITERIA))

    def test_runs_raw_in_result(self):
        responses = [self._uniform_response(2, 2)] * 3
        result, _ = self._call(iter(responses))
        self.assertEqual(len(result["runs"]), 3)
        for i, run in enumerate(result["runs"], start=1):
            self.assertEqual(run["run"], i)
            self.assertIn("ab_assignment", run)
            self.assertIn("resolved", run)

    @staticmethod
    def _wrap_side_effect(items):
        """Convert an iterable of (dict | Exception) into a side_effect for _run_and_verify."""
        it = iter(items)
        def _side(*a, **kw):
            x = next(it)
            if isinstance(x, Exception):
                raise x
            return (x, {})
        return _side

    def test_output_files_written(self):
        responses = [self._uniform_response(2, 2)] * 3
        with tempfile.TemporaryDirectory() as run_dir:
            with patch("eval.lib.judge._run_and_verify", side_effect=self._wrap_side_effect(responses)):
                score_paired_reviews(
                    pr_data=self._PR_DATA, skwad_review="s", claude_ci_review="c",
                    skwad_binary="./skwad", repo_path=run_dir, seed=42, run_dir=run_dir,
                )
            for i in range(1, 4):
                self.assertTrue(
                    os.path.exists(os.path.join(run_dir, f"judge_pr99_run{i}.json")),
                    f"judge_pr99_run{i}.json not found",
                )
            self.assertTrue(os.path.exists(os.path.join(run_dir, "judge_pr99_voted.json")))

    def test_single_run_failure_continues(self):
        # One run fails → warning logged, remaining runs succeed, result returned.
        good = self._uniform_response(2, 2)
        responses = [RuntimeError("run 1 failed"), good, good]
        with tempfile.TemporaryDirectory() as run_dir:
            with patch("eval.lib.judge._run_and_verify", side_effect=self._wrap_side_effect(responses)):
                with self.assertLogs("eval.lib.judge", level="WARNING"):
                    result = score_paired_reviews(
                        pr_data=self._PR_DATA, skwad_review="s", claude_ci_review="c",
                        skwad_binary="./skwad", repo_path=run_dir, seed=42, run_dir=run_dir,
                    )
        # 2 successful runs → result returned with 2 raw runs.
        self.assertEqual(len(result["runs"]), 2)

    def test_all_runs_fail_raises(self):
        responses = [RuntimeError("fail")] * 3
        with tempfile.TemporaryDirectory() as run_dir:
            with patch("eval.lib.judge._run_and_verify", side_effect=self._wrap_side_effect(responses)):
                with self.assertLogs("eval.lib.judge", level="WARNING"):
                    with self.assertRaises(RuntimeError, msg="All 3 judge runs failed"):
                        score_paired_reviews(
                            pr_data=self._PR_DATA, skwad_review="s", claude_ci_review="c",
                            skwad_binary="./skwad", repo_path=run_dir, seed=42, run_dir=run_dir,
                        )

    def test_n_runs_completed_present(self):
        # All 3 runs succeed → n_runs_completed==3 at top level and per system.
        good = _uniform_judge_json(2, 2)
        responses = [good] * 3
        result, _ = self._call(iter(responses))
        self.assertEqual(result["n_runs_completed"], 3)
        self.assertEqual(result["n_runs_planned"], 3)
        self.assertEqual(result["skwad"]["n_runs_completed"], 3)
        self.assertEqual(result["claude_ci"]["n_runs_completed"], 3)

        # 1 failure → n_runs_completed==2.
        responses_1_fail = [RuntimeError("fail"), good, good]
        with tempfile.TemporaryDirectory() as run_dir:
            with patch("eval.lib.judge._run_and_verify", side_effect=self._wrap_side_effect(responses_1_fail)):
                with self.assertLogs("eval.lib.judge", level="WARNING"):
                    r2 = score_paired_reviews(
                        pr_data=self._PR_DATA, skwad_review="s", claude_ci_review="c",
                        skwad_binary="./skwad", repo_path=run_dir, seed=42, run_dir=run_dir,
                    )
        self.assertEqual(r2["n_runs_completed"], 2)

        # 2 failures → n_runs_completed==1.
        responses_2_fail = [RuntimeError("fail"), RuntimeError("fail"), good]
        with tempfile.TemporaryDirectory() as run_dir:
            with patch("eval.lib.judge._run_and_verify", side_effect=self._wrap_side_effect(responses_2_fail)):
                with self.assertLogs("eval.lib.judge", level="WARNING"):
                    r3 = score_paired_reviews(
                        pr_data=self._PR_DATA, skwad_review="s", claude_ci_review="c",
                        skwad_binary="./skwad", repo_path=run_dir, seed=42, run_dir=run_dir,
                    )
        self.assertEqual(r3["n_runs_completed"], 1)


# ---------------------------------------------------------------------------
# Section A: Verification helpers — _sum_verified_in_output
# ---------------------------------------------------------------------------

class TestSumVerifiedInOutput(unittest.TestCase):
    def test_empty_parsed_returns_zero(self):
        self.assertEqual(_sum_verified_in_output({}), 0)

    def test_counts_only_count_criteria(self):
        # actionability is NOT in _COUNT_CRITERIA — must be excluded.
        parsed = {
            "review_a": {"criteria": {
                "issue_detection": {"verified_findings": 5},
                "actionability": {"verified_findings": 99},
            }},
            "review_b": {"criteria": {
                "coverage": {"verified_findings": 3},
            }},
        }
        self.assertEqual(_sum_verified_in_output(parsed), 8)

    def test_sums_both_review_sides(self):
        parsed = {
            "review_a": {"criteria": {"depth": {"verified_findings": 4}}},
            "review_b": {"criteria": {"novel_substantive_findings": {"verified_findings": 6}}},
        }
        self.assertEqual(_sum_verified_in_output(parsed), 10)

    def test_missing_verified_findings_treated_as_zero(self):
        parsed = {
            "review_a": {"criteria": {"issue_detection": {}}},
            "review_b": {},
        }
        self.assertEqual(_sum_verified_in_output(parsed), 0)


# ---------------------------------------------------------------------------
# Section A: _check_confabulation
# ---------------------------------------------------------------------------

class TestCheckConfabulation(unittest.TestCase):
    def _parsed_with_issue_detection(self, verified: int) -> dict:
        return {
            "review_a": {"criteria": {"issue_detection": {"verified_findings": verified}}},
            "review_b": {"criteria": {}},
        }

    def test_zero_claims_no_raise(self):
        _check_confabulation({}, [])

    def test_zero_claims_many_tools_no_raise(self):
        _check_confabulation({}, ["Read", "Grep", "Glob"])

    def test_5_claims_1_tool_exactly_meets_minimum(self):
        # min_required = max(1, ceil(5/5)) = 1; len(["Read"]) = 1 >= 1 → no raise.
        _check_confabulation(self._parsed_with_issue_detection(5), ["Read"])

    def test_6_claims_1_tool_raises(self):
        # min_required = max(1, ceil(6/5)) = 2; 1 < 2 → raise.
        with self.assertRaises(ConfabulationDetected):
            _check_confabulation(self._parsed_with_issue_detection(6), ["Read"])

    def test_20_claims_3_tools_raises(self):
        # min_required = max(1, ceil(20/5)) = 4; 3 < 4 → raise.
        with self.assertRaises(ConfabulationDetected):
            _check_confabulation(self._parsed_with_issue_detection(20), ["Read", "Grep", "Glob"])

    def test_20_claims_4_tools_no_raise(self):
        # min_required = 4; 4 >= 4 → no raise.
        _check_confabulation(self._parsed_with_issue_detection(20), ["Read", "Grep", "Glob", "Extra"])


# ---------------------------------------------------------------------------
# Section A: _check_disallowed_tools
# ---------------------------------------------------------------------------

class TestCheckDisallowedTools(unittest.TestCase):
    """The judge may now use ANY tool: _check_disallowed_tools is a no-op that
    never raises (PR 883 regression — Write / mcp / Bash tools must be accepted)."""

    def test_all_allowed_no_raise(self):
        self.assertIsNone(_check_disallowed_tools(list(ALLOWED_JUDGE_TOOLS)))

    def test_empty_list_no_raise(self):
        self.assertIsNone(_check_disallowed_tools([]))

    def test_mcp_prefixed_tool_no_longer_raises(self):
        # Previously raised DisallowedToolUsed; mcp tools are now permitted.
        self.assertIsNone(_check_disallowed_tools(["mcp:set-status"]))

    def test_non_allowlisted_native_tool_no_longer_raises(self):
        # Bash / Write are outside the old allowlist but are now accepted.
        self.assertIsNone(_check_disallowed_tools(["Read", "Bash", "Write"]))

    def test_pr883_traceback_tool_set_no_longer_raises(self):
        # The exact tool set from the PR 883 traceback must no longer raise.
        self.assertIsNone(_check_disallowed_tools(
            ["Write", "mcp:set-status", "ToolSearch", "mcp__skwad__set-status"]))


# ---------------------------------------------------------------------------
# Section A: _warn_trace_divergence
# ---------------------------------------------------------------------------

class TestWarnTraceDivergence(unittest.TestCase):
    def test_no_crash_when_observed_zero(self):
        parsed = {"review_a": {"claim_trace": [{"tools_used": ["Read"]}]}, "review_b": {}}
        _warn_trace_divergence(parsed, [])

    def test_warning_emitted_on_large_divergence(self):
        # 0 declared, 10 observed → divergence = 1.0 > 0.20 → warning.
        parsed = {"review_a": {}, "review_b": {}}
        with self.assertLogs("eval.lib.judge", level="WARNING") as cm:
            _warn_trace_divergence(parsed, [f"R{i}" for i in range(10)])
        self.assertTrue(any("divergence" in m.lower() for m in cm.output))

    def test_no_crash_on_empty_parsed(self):
        _warn_trace_divergence({}, [])


# ---------------------------------------------------------------------------
# Section B: _run_and_verify retry logic
# ---------------------------------------------------------------------------

def _structurally_valid_review() -> dict:
    """Build a structurally valid review (all 4 bucket fields on count criteria, empty claim_trace OK)."""
    criteria = {}
    for c in CRITERIA:
        entry = {"score": 0, "reasoning": ""}
        if c in _COUNT_CRITERIA_TEST:
            entry["verified_findings"] = 0
            entry["unverified_findings"] = 0
            entry["contradicted_findings"] = 0
            entry["non_falsifiable_findings"] = 0
        if c == "novel_substantive_findings":
            entry["justifications"] = []
        criteria[c] = entry
    return {"criteria": criteria, "total": 0, "claim_trace": []}


class TestRunAndVerify(unittest.TestCase):
    _PARSED = {
        "review_a": _structurally_valid_review(),
        "review_b": _structurally_valid_review(),
    }
    _META = {"event_log_path": "/fake/events.jsonl", "agent_ids": {"Judge": "agent-id-1"}}

    def _call(self, **kw):
        with tempfile.TemporaryDirectory() as d:
            return _run_and_verify(
                diff="diff", review_a="a", review_b="b",
                skwad_binary="./s", repo_path=d,
                config_path="cfg.json",
                judge_output_name="out.json", base_port=8800,
                task_start_time=time.time(),
                **kw,
            )

    @patch("eval.lib.judge._check_disallowed_tools")
    @patch("eval.lib.judge._check_confabulation")
    @patch("eval.lib.judge.count_tool_calls", return_value=["Read"])
    @patch("eval.lib.judge._run_judge_once")
    def test_first_success_no_retry(self, mock_run, _count, _conf, _dis):
        mock_run.return_value = (self._PARSED, self._META)
        parsed, _ = self._call()
        self.assertEqual(parsed, self._PARSED)
        self.assertEqual(mock_run.call_count, 1)

    @patch("eval.lib.judge._check_disallowed_tools")
    @patch("eval.lib.judge._check_confabulation",
           side_effect=[ConfabulationDetected("x"), None])
    @patch("eval.lib.judge.count_tool_calls", return_value=["Read"])
    @patch("eval.lib.judge._run_judge_once")
    def test_confabulation_retry_succeeds(self, mock_run, _count, _conf, _dis):
        # Retryable failure then success → no raise, two attempts. Counter bookkeeping
        # now lives in the single-threaded merge, not here.
        mock_run.return_value = (self._PARSED, self._META)
        parsed, _ = self._call()
        self.assertEqual(parsed, self._PARSED)
        self.assertEqual(mock_run.call_count, 2)

    @patch("eval.lib.judge._check_disallowed_tools")
    @patch("eval.lib.judge._check_confabulation",
           side_effect=ConfabulationDetected("x"))
    @patch("eval.lib.judge.count_tool_calls", return_value=["Read"])
    @patch("eval.lib.judge._run_judge_once")
    def test_confabulation_both_attempts_raises(self, mock_run, _count, _conf, _dis):
        # Both attempts fail → _run_and_verify RAISES (does not mutate counters).
        mock_run.return_value = (self._PARSED, self._META)
        with self.assertRaises(ConfabulationDetected):
            self._call()
        self.assertEqual(mock_run.call_count, 2)

    # NB: _check_disallowed_tools is intentionally NOT patched here — we let the
    # real (now no-op) implementation run so this exercises production behavior.
    @patch("eval.lib.judge._check_confabulation")
    @patch("eval.lib.judge.count_tool_calls",
           return_value=["Write", "mcp:set-status", "ToolSearch", "mcp__skwad__set-status"])
    @patch("eval.lib.judge._run_judge_once")
    def test_pr883_disallowed_tools_no_longer_rejected(self, mock_run, _count, _conf):
        """PR 883 regression: a judge run whose tool calls include Write /
        mcp__skwad__set-status / ToolSearch completes normally — no raise, no retry."""
        mock_run.return_value = (self._PARSED, self._META)
        parsed, _ = self._call()
        self.assertEqual(parsed, self._PARSED)
        self.assertEqual(mock_run.call_count, 1)  # accepted first try, no retry

    @patch("eval.lib.judge._run_judge_once", side_effect=RuntimeError("judge crashed"))
    def test_non_verification_error_propagates_without_retry(self, mock_run):
        with self.assertRaises(RuntimeError):
            self._call()
        self.assertEqual(mock_run.call_count, 1)


# ---------------------------------------------------------------------------
# Section C: _backfill_tool_calls_observed
# ---------------------------------------------------------------------------

class TestBackfillToolCallsObserved(unittest.TestCase):
    def test_sets_count_in_both_reviews(self):
        parsed = {
            "review_a": {"verification_summary": {"tool_calls_observed": 0}},
            "review_b": {"verification_summary": {"tool_calls_observed": 0}},
        }
        _backfill_tool_calls_observed(parsed, ["R", "G"], ["R"])
        self.assertEqual(parsed["review_a"]["verification_summary"]["tool_calls_observed"], 2)
        self.assertEqual(parsed["review_b"]["verification_summary"]["tool_calls_observed"], 1)

    def test_no_vs_no_crash(self):
        parsed = {"review_a": {"criteria": {}}, "review_b": {}}
        _backfill_tool_calls_observed(parsed, ["R"], ["G"])
        self.assertNotIn("verification_summary", parsed["review_a"])


# ---------------------------------------------------------------------------
# Section C: _aggregate_verification_summaries
# ---------------------------------------------------------------------------

class TestAggregateVerificationSummaries(unittest.TestCase):
    def _vs(self, verified=2, unverified=1, contradicted=1,
            non_falsifiable=0, rate=0.5, tool_calls=3):
        return {
            "claims_verified": verified,
            "claims_unverified": unverified,
            "claims_contradicted": contradicted,
            "claims_non_falsifiable": non_falsifiable,
            "verification_rate": rate,
            "tool_calls_observed": tool_calls,
        }

    def _run(self, skwad_vs, ci_vs):
        return {"skwad": {"verification_summary": skwad_vs},
                "claude_ci": {"verification_summary": ci_vs}}

    def test_single_run_passes_through(self):
        result = _aggregate_verification_summaries([
            self._run(self._vs(verified=4), self._vs(verified=2)),
        ])
        self.assertEqual(result["skwad"]["claims_verified"], 4)
        self.assertEqual(result["claude_ci"]["claims_verified"], 2)

    def test_counts_summed_across_runs(self):
        run = self._run(self._vs(verified=4, tool_calls=3), self._vs())
        result = _aggregate_verification_summaries([run, run])
        self.assertEqual(result["skwad"]["claims_verified"], 8)
        self.assertEqual(result["skwad"]["tool_calls_observed"], 6)

    def test_verification_rate_averaged(self):
        r1 = self._run(self._vs(rate=0.4), self._vs())
        r2 = self._run(self._vs(rate=0.6), self._vs())
        result = _aggregate_verification_summaries([r1, r2])
        self.assertAlmostEqual(result["skwad"]["verification_rate"], 0.5)


# ---------------------------------------------------------------------------
# Section C: _parse_stderr_metadata
# ---------------------------------------------------------------------------

class TestParseStderrMetadata(unittest.TestCase):
    def test_extracts_run_id(self):
        meta = _parse_stderr_metadata("Run ID: abc-123")
        self.assertEqual(meta["run_id"], "abc-123")

    def test_extracts_event_log_path(self):
        meta = _parse_stderr_metadata("Event log: /path/to/events.jsonl")
        self.assertEqual(meta["event_log_path"], "/path/to/events.jsonl")

    def test_extracts_agent_ids(self):
        meta = _parse_stderr_metadata("Agent: Judge agent-id-123")
        self.assertEqual(meta["agent_ids"]["Judge"], "agent-id-123")

    def test_event_log_missing_no_fallback(self):
        # Reviewer Important #3: the platform-dependent fallback was removed.
        # If 'Event log:' line is missing from stderr, event_log_path stays None;
        # _run_judge_once raises later with a clear error. Here we just verify
        # _parse_stderr_metadata no longer fabricates a path.
        meta = _parse_stderr_metadata("Run ID: my-run-id")
        self.assertIsNone(meta["event_log_path"])
        self.assertEqual(meta["run_id"], "my-run-id")

    def test_empty_stderr_returns_default_structure(self):
        meta = _parse_stderr_metadata("")
        self.assertIsNone(meta["run_id"])
        self.assertIsNone(meta["event_log_path"])
        self.assertEqual(meta["agent_ids"], {})


# ---------------------------------------------------------------------------
# Section C: count_tool_calls
# ---------------------------------------------------------------------------

class TestCountToolCalls(unittest.TestCase):
    def _write_events(self, tmpdir, events):
        path = os.path.join(tmpdir, "events.jsonl")
        with open(path, "w") as f:
            for e in events:
                f.write(json.dumps(e) + "\n")
        return path

    def test_returns_tool_names_for_agent(self):
        with tempfile.TemporaryDirectory() as d:
            path = self._write_events(d, [
                {"type": "tool_call", "data": {"agent_id": "a1", "tool_name": "Read"}},
                {"type": "tool_call", "data": {"agent_id": "a1", "tool_name": "Grep"}},
            ])
            self.assertEqual(count_tool_calls(path, "a1"), ["Read", "Grep"])

    def test_filters_by_agent_id(self):
        with tempfile.TemporaryDirectory() as d:
            path = self._write_events(d, [
                {"type": "tool_call", "data": {"agent_id": "a1", "tool_name": "Read"}},
                {"type": "tool_call", "data": {"agent_id": "a2", "tool_name": "Glob"}},
            ])
            self.assertEqual(count_tool_calls(path, "a1"), ["Read"])

    def test_non_tool_call_events_skipped(self):
        with tempfile.TemporaryDirectory() as d:
            path = self._write_events(d, [
                {"type": "run_started", "data": {}},
                {"type": "tool_call", "data": {"agent_id": "a1", "tool_name": "Read"}},
            ])
            self.assertEqual(count_tool_calls(path, "a1"), ["Read"])

    def test_missing_file_returns_empty(self):
        self.assertEqual(count_tool_calls("/nonexistent/events.jsonl", "a1"), [])

    def test_malformed_json_line_skipped(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "events.jsonl")
            with open(path, "w") as f:
                f.write("not json\n")
                f.write(json.dumps({"type": "tool_call",
                                    "data": {"agent_id": "a1", "tool_name": "Read"}}) + "\n")
            self.assertEqual(count_tool_calls(path, "a1"), ["Read"])


# ---------------------------------------------------------------------------
# Section D: _apply_canary_injections
# ---------------------------------------------------------------------------

class TestApplyCanaryInjections(unittest.TestCase):
    def _canary(self, pr=42, inject_into="skwad", claim="injected claim"):
        return {"target_pr": {"pr": pr}, "inject_into": inject_into, "claim_text": claim}

    def test_matching_pr_appends_claim_to_target_system(self):
        reviews = {"skwad": "original skwad", "claude_ci": "original ci"}
        result = _apply_canary_injections(reviews, [self._canary()], {"pr_number": 42})
        self.assertIn("[INJECTED CLAIM] injected claim", result["skwad"])
        self.assertIn("original skwad", result["skwad"])
        self.assertEqual(result["claude_ci"], "original ci")

    def test_non_matching_pr_leaves_reviews_unchanged(self):
        reviews = {"skwad": "review", "claude_ci": "ci"}
        result = _apply_canary_injections(reviews, [self._canary(pr=99)], {"pr_number": 42})
        self.assertEqual(result["skwad"], "review")

    def test_inject_into_claude_ci(self):
        reviews = {"skwad": "s", "claude_ci": "c"}
        result = _apply_canary_injections(
            reviews, [self._canary(inject_into="claude_ci", claim="ci claim")], {"pr_number": 42}
        )
        self.assertIn("[INJECTED CLAIM] ci claim", result["claude_ci"])
        self.assertEqual(result["skwad"], "s")

    def test_empty_claim_text_skipped(self):
        reviews = {"skwad": "s", "claude_ci": "c"}
        result = _apply_canary_injections(reviews, [self._canary(claim="")], {"pr_number": 42})
        self.assertEqual(result["skwad"], "s")

    def test_wildcard_canary_injects_into_any_pr(self):
        # A PR-agnostic canary (target_pr absent / null / "*") injects regardless
        # of pr_number — including a PR != the legacy 1746 hardcoding.
        reviews = {"skwad": "s", "claude_ci": "c"}
        for target_pr in ({}, {"pr": None}, {"pr": "*"}):
            canary = {"target_pr": target_pr, "inject_into": "skwad", "claim_text": "wild"}
            result = _apply_canary_injections(reviews, [canary], {"pr_number": 567})
            self.assertIn("[INJECTED CLAIM] wild", result["skwad"])
        # injection stays scoped to skwad, not claude_ci.
        self.assertEqual(result["claude_ci"], "c")

    def test_pinned_canary_only_injects_for_its_pr(self):
        # Pinned capability preserved: injects for its PR, skips others.
        reviews = {"skwad": "s", "claude_ci": "c"}
        canary = self._canary(pr=1818, claim="pinned")
        hit = _apply_canary_injections(reviews, [canary], {"pr_number": 1818})
        self.assertIn("[INJECTED CLAIM] pinned", hit["skwad"])
        miss = _apply_canary_injections(reviews, [canary], {"pr_number": 567})
        self.assertEqual(miss["skwad"], "s")

    def test_string_pinned_pr_matches_int_pr_number(self):
        # A JSON-authored string `"pr": "567"` must match int pr_number 567 (compared
        # as strings) — a type mismatch would silently never inject.
        reviews = {"skwad": "s", "claude_ci": "c"}
        canary = self._canary(pr="567", claim="coerced")
        hit = _apply_canary_injections(reviews, [canary], {"pr_number": 567})
        self.assertIn("[INJECTED CLAIM] coerced", hit["skwad"])
        # still skips a genuinely non-matching PR.
        miss = _apply_canary_injections(reviews, [canary], {"pr_number": 999})
        self.assertEqual(miss["skwad"], "s")


# ---------------------------------------------------------------------------
# Section D: _check_canary_outcomes
# ---------------------------------------------------------------------------

class TestCheckCanaryOutcomes(unittest.TestCase):
    _CLAIM = "function processBatchScenarios is leaking memory"

    def _canary(self, expected="contradicted", match_token=None):
        canary = {
            "id": "c1",
            "target_pr": {"pr": 1},
            "inject_into": "skwad",
            "claim_text": self._CLAIM,
            "expected_outcome": expected,
            "rationale": "fabricated",
        }
        if match_token is not None:
            canary["match_token"] = match_token
        return canary

    def _parsed_with_trace(self, review_key, outcome, claim_text=None):
        return {
            review_key: {"claim_trace": [{
                "claim_text": self._CLAIM if claim_text is None else claim_text,
                "outcome": outcome,
                "tools_used": ["Read"],
                "evidence": "evidence text",
            }]},
            "review_a" if review_key != "review_a" else "review_b": {},
        }

    def test_matching_trace_passes(self):
        parsed = self._parsed_with_trace("review_a", "contradicted")
        outcomes = _check_canary_outcomes(parsed, [self._canary()], "skwad", "claude_ci", {"pr_number": 1})
        self.assertEqual(len(outcomes), 1)
        self.assertTrue(outcomes[0]["passed"])
        self.assertEqual(outcomes[0]["actual_outcome"], "contradicted")

    def test_non_matching_pr_returns_empty_list(self):
        canary = self._canary()
        canary["target_pr"]["pr"] = 99
        outcomes = _check_canary_outcomes({}, [canary], "skwad", "claude_ci", {"pr_number": 1})
        self.assertEqual(outcomes, [])

    def test_wrong_outcome_fails(self):
        parsed = self._parsed_with_trace("review_a", "verified")
        outcomes = _check_canary_outcomes(parsed, [self._canary()], "skwad", "claude_ci", {"pr_number": 1})
        self.assertFalse(outcomes[0]["passed"])
        self.assertEqual(outcomes[0]["actual_outcome"], "verified")

    def test_claim_not_in_trace_actual_is_none_fails(self):
        parsed = {"review_a": {"claim_trace": []}, "review_b": {}}
        outcomes = _check_canary_outcomes(parsed, [self._canary()], "skwad", "claude_ci", {"pr_number": 1})
        self.assertFalse(outcomes[0]["passed"])
        self.assertIsNone(outcomes[0]["actual_outcome"])

    def test_paraphrased_trace_matches_via_token(self):
        # Robust matcher: the trace entry PARAPHRASES the canary (different
        # surrounding words) but contains the distinctive match_token → matches.
        parsed = self._parsed_with_trace(
            "review_a", "contradicted",
            claim_text="The function processBatchScenarios does not exist in the diff")
        canary = self._canary(match_token="processBatchScenarios")
        outcomes = _check_canary_outcomes(parsed, [canary], "skwad", "claude_ci", {"pr_number": 1})
        self.assertTrue(outcomes[0]["passed"])
        self.assertEqual(outcomes[0]["actual_outcome"], "contradicted")

    def test_token_match_is_case_and_whitespace_insensitive(self):
        parsed = self._parsed_with_trace(
            "review_a", "contradicted",
            claim_text="PROCESSBATCHSCENARIOS   is   fabricated")
        canary = self._canary(match_token="processBatchScenarios")
        outcomes = _check_canary_outcomes(parsed, [canary], "skwad", "claude_ci", {"pr_number": 1})
        self.assertTrue(outcomes[0]["passed"])
        self.assertEqual(outcomes[0]["actual_outcome"], "contradicted")

    def test_unrelated_trace_does_not_spuriously_match(self):
        # Negative: an unrelated claim lacks the token → no match → actual_outcome
        # stays None → canary correctly FAILS (a real judge miss is not masked).
        parsed = self._parsed_with_trace(
            "review_a", "verified",
            claim_text="useMediaSources has a real memory leak")
        canary = self._canary(match_token="processBatchScenarios")
        outcomes = _check_canary_outcomes(parsed, [canary], "skwad", "claude_ci", {"pr_number": 1})
        self.assertIsNone(outcomes[0]["actual_outcome"])
        self.assertFalse(outcomes[0]["passed"])

    def test_wildcard_canary_checked_for_any_pr(self):
        # A PR-agnostic canary is asserted against whatever PR is judged.
        parsed = self._parsed_with_trace("review_a", "contradicted")
        canary = self._canary()
        canary["target_pr"] = {}  # wildcard
        outcomes = _check_canary_outcomes(parsed, [canary], "skwad", "claude_ci", {"pr_number": 567})
        self.assertEqual(len(outcomes), 1)
        self.assertTrue(outcomes[0]["passed"])


# ---------------------------------------------------------------------------
# Section B: _validate_response_structure (Reviewer Critical #2)
# ---------------------------------------------------------------------------

def _all_zero_count_crit() -> dict:
    """A count criterion with all 4 bucket fields set to zero (valid structure)."""
    return {
        "score": 0,
        "reasoning": "r",
        "verified_findings": 0,
        "unverified_findings": 0,
        "contradicted_findings": 0,
        "non_falsifiable_findings": 0,
    }


def _valid_review(*, findings_present: bool = False) -> dict:
    """Build a review_a/review_b dict that passes structural validation.

    If findings_present=True, issue_detection has verified_findings=2 and
    claim_trace is populated (non-empty).
    """
    issue = _all_zero_count_crit()
    claim_trace: list = []
    if findings_present:
        issue["verified_findings"] = 2
        claim_trace = [{"claim_text": "c", "outcome": "verified", "tools_used": ["Read"]}]
    return {
        "criteria": {
            "issue_detection": issue,
            "actionability": {"score": 0, "reasoning": "r"},
            "severity_accuracy": {"score": 0, "reasoning": "r"},
            "coverage": _all_zero_count_crit(),
            "signal_to_noise": {"score": 0, "reasoning": "r"},
            "depth": _all_zero_count_crit(),
            "novel_substantive_findings": _all_zero_count_crit(),
        },
        "claim_trace": claim_trace,
    }


def _valid_parsed(findings_present: bool = False) -> dict:
    return {
        "review_a": _valid_review(findings_present=findings_present),
        "review_b": _valid_review(findings_present=False),
    }


class TestValidateResponseStructure(unittest.TestCase):
    def test_validate_missing_verified_findings_raises(self):
        parsed = _valid_parsed()
        del parsed["review_a"]["criteria"]["issue_detection"]["verified_findings"]
        with self.assertRaises(StructuralInvalidRun) as ctx:
            _validate_response_structure(parsed)
        self.assertIn("missing bucket fields", str(ctx.exception))
        self.assertIn("verified_findings", str(ctx.exception))

    def test_validate_missing_unverified_findings_raises(self):
        parsed = _valid_parsed()
        del parsed["review_a"]["criteria"]["issue_detection"]["unverified_findings"]
        with self.assertRaises(StructuralInvalidRun) as ctx:
            _validate_response_structure(parsed)
        self.assertIn("unverified_findings", str(ctx.exception))

    def test_validate_missing_contradicted_findings_raises(self):
        parsed = _valid_parsed()
        del parsed["review_a"]["criteria"]["coverage"]["contradicted_findings"]
        with self.assertRaises(StructuralInvalidRun) as ctx:
            _validate_response_structure(parsed)
        self.assertIn("contradicted_findings", str(ctx.exception))

    def test_validate_missing_non_falsifiable_findings_raises(self):
        parsed = _valid_parsed()
        del parsed["review_a"]["criteria"]["depth"]["non_falsifiable_findings"]
        with self.assertRaises(StructuralInvalidRun) as ctx:
            _validate_response_structure(parsed)
        self.assertIn("non_falsifiable_findings", str(ctx.exception))

    def test_validate_findings_present_empty_trace_raises(self):
        # verified_findings=5 but claim_trace=[] → raises "claim_trace empty but N finding(s) present".
        parsed = _valid_parsed()
        parsed["review_a"]["criteria"]["issue_detection"]["verified_findings"] = 5
        parsed["review_a"]["claim_trace"] = []
        with self.assertRaises(StructuralInvalidRun) as ctx:
            _validate_response_structure(parsed)
        msg = str(ctx.exception)
        self.assertIn("claim_trace empty", msg)
        self.assertIn("5 finding", msg)

    def test_validate_zero_findings_empty_trace_passes(self):
        # All bucket counts = 0 + empty trace → no raise (legit "no claims" case).
        _validate_response_structure(_valid_parsed(findings_present=False))

    def test_validate_findings_present_populated_trace_passes(self):
        # verified_findings > 0 + claim_trace populated → no raise.
        _validate_response_structure(_valid_parsed(findings_present=True))

    def test_validate_only_count_criteria_checked(self):
        # Non-count criteria (actionability/severity_accuracy/signal_to_noise) do NOT
        # require bucket fields. Removing fields from those must NOT trigger a raise.
        parsed = _valid_parsed()
        # actionability is fine — already lacks bucket fields. Removing the "score"
        # shouldn't trigger structural validation either.
        parsed["review_a"]["criteria"]["actionability"] = {"reasoning": "r"}
        parsed["review_a"]["criteria"]["severity_accuracy"] = {"reasoning": "r"}
        parsed["review_a"]["criteria"]["signal_to_noise"] = {"reasoning": "r"}
        _validate_response_structure(parsed)  # no raise


# ---------------------------------------------------------------------------
# Section C: _validate_response_structure integrated into retry-once
# ---------------------------------------------------------------------------

class TestRunAndVerifyStructural(unittest.TestCase):
    _VALID = _valid_parsed(findings_present=False)
    _INVALID = {"review_a": {"criteria": {"issue_detection": {"score": 1}}}, "review_b": {"criteria": {}}}
    _META = {"event_log_path": "/fake/events.jsonl", "agent_ids": {"Judge": "agent-id-1"}}

    def _call(self, **kw):
        with tempfile.TemporaryDirectory() as d:
            return _run_and_verify(
                diff="d", review_a="a", review_b="b",
                skwad_binary="./s", repo_path=d,
                config_path="cfg.json",
                judge_output_name="out.json", base_port=8800,
                task_start_time=time.time(),
                **kw,
            )

    @patch("eval.lib.judge._check_disallowed_tools")
    @patch("eval.lib.judge._check_confabulation")
    @patch("eval.lib.judge.count_tool_calls", return_value=["Read"])
    @patch("eval.lib.judge._run_judge_once")
    def test_run_and_verify_structural_invalid_then_pass(
            self, mock_run, _count, _conf, _dis):
        # First attempt returns malformed (no bucket fields), second returns valid →
        # retry succeeds, no raise.
        mock_run.side_effect = [
            (self._INVALID, self._META),
            (self._VALID, self._META),
        ]
        self._call()
        self.assertEqual(mock_run.call_count, 2)

    @patch("eval.lib.judge._check_disallowed_tools")
    @patch("eval.lib.judge._check_confabulation")
    @patch("eval.lib.judge.count_tool_calls", return_value=["Read"])
    @patch("eval.lib.judge._run_judge_once")
    def test_run_and_verify_structural_invalid_both_attempts_raises(
            self, mock_run, _count, _conf, _dis):
        # Both attempts malformed → raises StructuralInvalidRun (no counter mutation).
        mock_run.return_value = (self._INVALID, self._META)
        with self.assertRaises(StructuralInvalidRun):
            self._call()
        self.assertEqual(mock_run.call_count, 2)


# ---------------------------------------------------------------------------
# Section D: No platform-dependent event_log fallback
# ---------------------------------------------------------------------------

class TestRunJudgeOnceEventLogMissing(unittest.TestCase):
    # MCP came up (listening line present) and a fresh output file is written, so the
    # run reaches the event-log check — but there is NO "Event log:" line on stderr.
    _STDERR_NO_EVENT_LOG = (
        "MCP server listening on 127.0.0.1:8800\nRun ID: some-run-id\nAgent: Judge agent-1\n"
    )
    _OUTPUT_NAME = "out.json"

    def _make_subprocess_writer(self, stderr: str):
        """Return a side_effect that simulates skwad-cli writing the per-task output."""
        def _run(*args, **kwargs):
            cwd = kwargs.get("cwd")
            if cwd:
                with open(os.path.join(cwd, self._OUTPUT_NAME), "w") as f:
                    f.write('{"review_a": {}, "review_b": {}}')
            ok = MagicMock()
            ok.returncode = 0
            ok.stderr = stderr
            return ok
        return _run

    def test_run_judge_once_raises_when_event_log_missing(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            with patch(
                "eval.lib.judge.subprocess.run",
                side_effect=self._make_subprocess_writer(self._STDERR_NO_EVENT_LOG),
            ):
                with self.assertRaises(RuntimeError) as ctx:
                    _run_judge_once(
                        diff="", review_a="a", review_b="b",
                        skwad_binary="./s", repo_path=tmpdir,
                        config_path="cfg.json",
                        judge_output_name=self._OUTPUT_NAME, port=8800,
                        task_start_time=time.time(),
                    )
        self.assertIn("Event log", str(ctx.exception))

    def test_no_dot_config_skwad_fallback(self):
        """Reviewer Important #3: even with run_id present and a plausible
        ~/.config/skwad path, the harness must NOT fall back. The explicit
        version-mismatch error must fire instead."""
        with tempfile.TemporaryDirectory() as tmpdir:
            with patch(
                "eval.lib.judge.subprocess.run",
                side_effect=self._make_subprocess_writer(self._STDERR_NO_EVENT_LOG),
            ):
                with self.assertRaises(RuntimeError) as ctx:
                    _run_judge_once(
                        diff="", review_a="a", review_b="b",
                        skwad_binary="./s", repo_path=tmpdir,
                        config_path="cfg.json",
                        judge_output_name=self._OUTPUT_NAME, port=8800,
                        task_start_time=time.time(),
                    )
        msg = str(ctx.exception)
        # Explicit version-mismatch hint, not a silent miss.
        self.assertIn("Event log", msg)
        self.assertNotIn(".config/skwad", msg)


if __name__ == "__main__":
    unittest.main()
