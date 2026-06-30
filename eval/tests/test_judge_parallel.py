"""Judge parallelization test matrix (hazards 8a–8f + retry/cost/429).

The judge now runs one task per (PR, A/B run). These tests exercise the pure
building blocks — prepare_pr_judge_tasks / run_single_judge_task / finalize_pr_runs
/ derive_ab_assignments — and the retryable failure modes in _run_and_verify /
_run_judge_once. The subprocess is ALWAYS mocked; no real judge is ever spawned.
"""

import json
import os
import tempfile
import time
import unittest
from unittest.mock import MagicMock, patch

from eval.lib.judge import (
    CRITERIA,
    JUDGE_BASE_PORT,
    RATE_LIMIT_BACKOFF_SEC,
    RETRY_PORT_OFFSET,
    MCPUnavailable,
    OutputFreshnessError,
    RateLimited,
    derive_ab_assignments,
    finalize_pr_runs,
    prepare_pr_judge_tasks,
    run_single_judge_task,
    score_paired_reviews,
    _run_and_verify,
    _run_judge_once,
)

_COUNT = {"issue_detection", "coverage", "depth", "novel_substantive_findings"}


# ---------------------------------------------------------------------------
# Fixture builders
# ---------------------------------------------------------------------------

def _review(score: int) -> dict:
    criteria = {}
    for c in CRITERIA:
        entry = {"score": score, "reasoning": ""}
        if c in _COUNT:
            entry.update(verified_findings=0, unverified_findings=0,
                         contradicted_findings=0, non_falsifiable_findings=0)
        if c == "novel_substantive_findings":
            entry["justifications"] = []
        criteria[c] = entry
    return {"criteria": criteria, "total": score * len(CRITERIA), "claim_trace": []}


def _valid_parsed() -> dict:
    return {"review_a": _review(0), "review_b": _review(0)}


def _resolved(skwad_score: int, ci_score: int) -> dict:
    """An _unswap-shaped resolved dict consumed by finalize/_median_vote."""
    return {"skwad": _review(skwad_score), "claude_ci": _review(ci_score)}


def _ok_result(run_index: int, resolved: dict, canary=None, duration: float = 1.0) -> dict:
    return {
        "pr_number": 7, "run_index": run_index,
        "ab_assignment": ["skwad", "claude_ci"],
        "status": "ok", "resolved": resolved,
        "run_record": {
            "run": run_index, "ab_assignment": ["skwad", "claude_ci"],
            "raw_response": {}, "resolved": resolved, "stderr_meta": {},
            "duration_seconds": duration,
        },
        "canary_outcomes": canary or [],
        "counter_increment": None, "duration_seconds": duration, "error": None,
    }


def _failed_result(run_index: int, error="MCPUnavailable: x", counter=None, duration=0.5) -> dict:
    return {
        "pr_number": 7, "run_index": run_index,
        "ab_assignment": ["skwad", "claude_ci"],
        "status": "failed", "resolved": None, "run_record": None,
        "canary_outcomes": [], "counter_increment": counter,
        "duration_seconds": duration, "error": error,
    }


_ASSIGN = derive_ab_assignments(42)
_META_OK = {"event_log_path": "/fake/events.jsonl", "agent_ids": {"Judge": "agent-id-1"}}


# ---------------------------------------------------------------------------
# 8a — Order independence / determinism of the merge
# ---------------------------------------------------------------------------

class TestMergeOrderIndependence(unittest.TestCase):
    def _runs(self):
        # Distinct per-run scores so the median is well-defined: skwad medians to 2,
        # ci medians to 2; durations distinct so ordering is observable.
        return [
            _ok_result(1, _resolved(1, 3), duration=1.0),
            _ok_result(2, _resolved(2, 2), duration=2.0),
            _ok_result(3, _resolved(3, 1), duration=3.0),
        ]

    def test_voted_result_identical_regardless_of_completion_order(self):
        in_order = self._runs()
        shuffled = [in_order[2], in_order[0], in_order[1]]  # as_completed arrives out of order
        with tempfile.TemporaryDirectory() as d1, tempfile.TemporaryDirectory() as d2:
            r_in = finalize_pr_runs(7, in_order, _ASSIGN, d1)
            r_sh = finalize_pr_runs(7, shuffled, _ASSIGN, d2)
        self.assertEqual(json.dumps(r_in, sort_keys=True), json.dumps(r_sh, sort_keys=True))

    def test_run_durations_follow_run_index_not_arrival(self):
        shuffled = [self._runs()[2], self._runs()[0], self._runs()[1]]
        with tempfile.TemporaryDirectory() as d:
            result = finalize_pr_runs(7, shuffled, _ASSIGN, d)
        # Sorted by run_index → [run1, run2, run3] durations.
        self.assertEqual(result["run_durations_seconds"], [1.0, 2.0, 3.0])
        self.assertEqual([r["run"] for r in result["runs"]], [1, 2, 3])


# ---------------------------------------------------------------------------
# 8b — Counter summation + canary/run-record concatenation
# ---------------------------------------------------------------------------

class TestMergeAggregation(unittest.TestCase):
    def test_finalize_concatenates_canary_outcomes_without_dup_or_drop(self):
        runs = [
            _ok_result(1, _resolved(2, 2), canary=[{"id": "c1", "passed": True}]),
            _ok_result(2, _resolved(2, 2), canary=[{"id": "c2", "passed": False}]),
            _ok_result(3, _resolved(2, 2), canary=[]),
        ]
        with tempfile.TemporaryDirectory() as d:
            result = finalize_pr_runs(7, runs, _ASSIGN, d, canary_injections=[{"id": "c1"}])
        ids = [c["id"] for c in result["canary_outcomes"]]
        self.assertEqual(sorted(ids), ["c1", "c2"])

    def test_finalize_durations_count_all_attempted_runs(self):
        runs = [_ok_result(1, _resolved(2, 2), duration=1.0),
                _failed_result(2, duration=0.5),
                _ok_result(3, _resolved(2, 2), duration=2.0)]
        with tempfile.TemporaryDirectory() as d:
            result = finalize_pr_runs(7, runs, _ASSIGN, d)
        self.assertEqual(result["run_durations_seconds"], [1.0, 0.5, 2.0])  # incl. the failed run
        self.assertEqual(result["n_runs_completed"], 2)
        self.assertEqual(result["n_runs_planned"], 3)

    def test_score_paired_reviews_sums_counter_increments_single_threaded(self):
        # Two failed tasks bump distinct counters; one ok task lets finalize succeed.
        results = [
            _failed_result(1, counter="confabulation_rejections"),
            _failed_result(2, counter="structural_invalid_rejections"),
            _ok_result(3, _resolved(2, 2)),
        ]
        counters = {"confabulation_rejections": 0, "structural_invalid_rejections": 0}
        with tempfile.TemporaryDirectory() as d:
            with patch("eval.lib.judge.run_single_judge_task", side_effect=results):
                score_paired_reviews(
                    pr_data={"pr_number": 7, "diff": "+x\n"},
                    skwad_review="s", claude_ci_review="c",
                    skwad_binary="./s", repo_path=d, seed=42, run_dir=d,
                    pilot_counters=counters,
                )
        self.assertEqual(counters["confabulation_rejections"], 1)
        self.assertEqual(counters["structural_invalid_rejections"], 1)


# ---------------------------------------------------------------------------
# 8c — Failure isolation
# ---------------------------------------------------------------------------

class TestFailureIsolation(unittest.TestCase):
    def _one_task(self, run_dir):
        tasks, _ = prepare_pr_judge_tasks(
            {"pr_number": 7, "diff": "+x\n"}, "s", "c", "./s", run_dir, 42, run_dir,
        )
        return tasks[0]

    def test_run_single_judge_task_captures_exception_never_raises(self):
        with tempfile.TemporaryDirectory() as d:
            task = self._one_task(d)
            with patch("eval.lib.judge._run_and_verify", side_effect=MCPUnavailable("no mcp")):
                out = run_single_judge_task(task)  # must NOT raise
        self.assertEqual(out["status"], "failed")
        self.assertTrue(out["error"].startswith("MCPUnavailable:"))
        self.assertIsNone(out["resolved"])
        # MCPUnavailable is infra, not a methodology rejection → no counter bump.
        self.assertIsNone(out["counter_increment"])

    def test_one_failed_task_does_not_sink_the_others(self):
        runs = [_ok_result(1, _resolved(2, 2)), _failed_result(2), _ok_result(3, _resolved(2, 2))]
        with tempfile.TemporaryDirectory() as d:
            result = finalize_pr_runs(7, runs, _ASSIGN, d)
        self.assertEqual(len(result["runs"]), 2)
        self.assertEqual(result["n_runs_completed"], 2)

    def test_all_tasks_failed_raises(self):
        runs = [_failed_result(1), _failed_result(2), _failed_result(3)]
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaises(RuntimeError):
                finalize_pr_runs(7, runs, _ASSIGN, d)


# ---------------------------------------------------------------------------
# 8d — Index separation: A/B + seed from per-PR run_index; port from task index
# ---------------------------------------------------------------------------

class TestIndexSeparation(unittest.TestCase):
    def test_ports_offset_from_base_per_run(self):
        with tempfile.TemporaryDirectory() as d:
            tasks, _ = prepare_pr_judge_tasks(
                {"pr_number": 7, "diff": ""}, "s", "c", "./s", d, 42, d, base_port=8800,
            )
        self.assertEqual([t["port"] for t in tasks], [8800, 8801, 8802])
        self.assertEqual([t["run_index"] for t in tasks], [1, 2, 3])

    def test_same_run_index_diff_base_port_same_ab_diff_ports(self):
        # Two PRs (different base_port = the global task offset) with the SAME seed:
        # identical A/B order per run_index, but disjoint ports.
        with tempfile.TemporaryDirectory() as d:
            t1, _ = prepare_pr_judge_tasks({"pr_number": 1, "diff": ""}, "s", "c", "./s", d, 42, d, base_port=8800)
            t2, _ = prepare_pr_judge_tasks({"pr_number": 2, "diff": ""}, "s", "c", "./s", d, 42, d, base_port=9000)
        for a, b in zip(t1, t2):
            self.assertEqual((a["a_system"], a["b_system"]), (b["a_system"], b["b_system"]))
        self.assertTrue(set(t["port"] for t in t1).isdisjoint(t["port"] for t in t2))

    def test_derive_ab_assignments_deterministic_and_counterbalanced(self):
        self.assertEqual(derive_ab_assignments(42), derive_ab_assignments(42))
        a = derive_ab_assignments(42)
        self.assertEqual(a[0], ("skwad", "claude_ci"))
        self.assertEqual(a[1], ("claude_ci", "skwad"))  # first two are fixed/counterbalanced

    def test_default_base_port_is_8800(self):
        self.assertEqual(JUDGE_BASE_PORT, 8800)


# ---------------------------------------------------------------------------
# 8e — Silent-MCP detection (HARD fail → retry, never soft-skip-and-pass)
# ---------------------------------------------------------------------------

class TestSilentMCPDetection(unittest.TestCase):
    @patch("eval.lib.judge._validate_response_structure")
    @patch("eval.lib.judge._run_judge_once")
    def test_empty_event_log_is_hard_fail_and_retried(self, mock_once, _val):
        # Primary structured check: no event_log_path/agent_ids → MCPUnavailable, retried.
        mock_once.return_value = (_valid_parsed(), {"event_log_path": None, "agent_ids": {}})
        with self.assertRaises(MCPUnavailable):
            _run_and_verify(diff="d", review_a="a", review_b="b", skwad_binary="./s",
                            repo_path="/tmp", config_path="c", judge_output_name="o.json",
                            base_port=8800, task_start_time=time.time())
        self.assertEqual(mock_once.call_count, 2)  # retried, NOT soft-skipped to a pass

    @patch("eval.lib.judge._validate_response_structure")
    @patch("eval.lib.judge._run_judge_once")
    def test_empty_agent_ids_alone_is_hard_fail(self, mock_once, _val):
        mock_once.return_value = (_valid_parsed(), {"event_log_path": "/e.jsonl", "agent_ids": {}})
        with self.assertRaises(MCPUnavailable):
            _run_and_verify(diff="d", review_a="a", review_b="b", skwad_binary="./s",
                            repo_path="/tmp", config_path="c", judge_output_name="o.json",
                            base_port=8800, task_start_time=time.time())

    def test_stderr_missing_listening_line_is_mcp_unavailable(self):
        # Secondary stderr check: the server logs "MCP server listening on
        # 127.0.0.1:<port>" only on a successful bind (internal/mcp/server.go:102;
        # Addr built :87, net.Listen :91). Absent → MCPUnavailable.
        ok = MagicMock()
        ok.returncode = 0
        ok.stderr = "Run ID: r\nAgent: Judge id\nEvent log: /e.jsonl\n"  # no listening line
        with tempfile.TemporaryDirectory() as d:
            with patch("eval.lib.judge.subprocess.run", return_value=ok):
                with self.assertRaises(MCPUnavailable):
                    _run_judge_once(diff="", review_a="a", review_b="b", skwad_binary="./s",
                                    repo_path=d, config_path="c", judge_output_name="o.json",
                                    port=8800, task_start_time=time.time())

    def test_listening_line_for_wrong_port_is_mcp_unavailable(self):
        # A collision: the line is present but for a DIFFERENT port than ours.
        ok = MagicMock()
        ok.returncode = 0
        ok.stderr = "MCP server listening on 127.0.0.1:9999\n"
        with tempfile.TemporaryDirectory() as d:
            with patch("eval.lib.judge.subprocess.run", return_value=ok):
                with self.assertRaises(MCPUnavailable):
                    _run_judge_once(diff="", review_a="a", review_b="b", skwad_binary="./s",
                                    repo_path=d, config_path="c", judge_output_name="o.json",
                                    port=8800, task_start_time=time.time())


# ---------------------------------------------------------------------------
# 8f — Output-file freshness guard
# ---------------------------------------------------------------------------

class TestOutputFreshnessGuard(unittest.TestCase):
    def _subprocess(self, *, write: bool, stale: bool, port: int = 8800, output_name="o.json"):
        def _run(*args, **kwargs):
            cwd = kwargs.get("cwd")
            if write and cwd:
                p = os.path.join(cwd, output_name)
                with open(p, "w") as f:
                    f.write('{"review_a": {"criteria": {}}, "review_b": {"criteria": {}}}')
                if stale:
                    old = time.time() - 10_000
                    os.utime(p, (old, old))
            ok = MagicMock()
            ok.returncode = 0
            ok.stderr = (
                f"MCP server listening on 127.0.0.1:{port}\n"
                "Event log: /e.jsonl\nAgent: Judge id\n"
            )
            return ok
        return _run

    def test_missing_output_file_fails(self):
        with tempfile.TemporaryDirectory() as d:
            with patch("eval.lib.judge.subprocess.run", side_effect=self._subprocess(write=False, stale=False)):
                with self.assertRaises(OutputFreshnessError):
                    _run_judge_once(diff="", review_a="a", review_b="b", skwad_binary="./s",
                                    repo_path=d, config_path="c", judge_output_name="o.json",
                                    port=8800, task_start_time=time.time())

    def test_stale_output_file_fails(self):
        with tempfile.TemporaryDirectory() as d:
            with patch("eval.lib.judge.subprocess.run", side_effect=self._subprocess(write=True, stale=True)):
                with self.assertRaises(OutputFreshnessError):
                    _run_judge_once(diff="", review_a="a", review_b="b", skwad_binary="./s",
                                    repo_path=d, config_path="c", judge_output_name="o.json",
                                    port=8800, task_start_time=time.time())

    def test_fresh_output_file_passes(self):
        with tempfile.TemporaryDirectory() as d:
            task_start = time.time()
            with patch("eval.lib.judge.subprocess.run", side_effect=self._subprocess(write=True, stale=False)):
                parsed, meta = _run_judge_once(
                    diff="", review_a="a", review_b="b", skwad_binary="./s",
                    repo_path=d, config_path="c", judge_output_name="o.json",
                    port=8800, task_start_time=task_start,
                )
        self.assertIn("review_a", parsed)
        self.assertEqual(meta["event_log_path"], "/e.jsonl")


# ---------------------------------------------------------------------------
# Retry mechanics: offset port + 429 backoff
# ---------------------------------------------------------------------------

class TestRetryMechanics(unittest.TestCase):
    @patch("eval.lib.judge._check_disallowed_tools")
    @patch("eval.lib.judge._check_confabulation")
    @patch("eval.lib.judge.count_tool_calls", return_value=["Read"])
    @patch("eval.lib.judge._validate_response_structure")
    @patch("eval.lib.judge._run_judge_once")
    def test_retry_uses_offset_port(self, mock_once, _val, _count, _conf, _dis):
        ports = []

        def _side(**kw):
            ports.append(kw["port"])
            if len(ports) == 1:
                raise OutputFreshnessError("stale leftover")
            return (_valid_parsed(), _META_OK)

        mock_once.side_effect = _side
        _run_and_verify(diff="d", review_a="a", review_b="b", skwad_binary="./s",
                        repo_path="/tmp", config_path="c", judge_output_name="o.json",
                        base_port=8800, task_start_time=time.time())
        self.assertEqual(ports, [8800, 8800 + RETRY_PORT_OFFSET])

    @patch("eval.lib.judge.time.sleep")
    @patch("eval.lib.judge._check_disallowed_tools")
    @patch("eval.lib.judge._check_confabulation")
    @patch("eval.lib.judge.count_tool_calls", return_value=["Read"])
    @patch("eval.lib.judge._validate_response_structure")
    @patch("eval.lib.judge._run_judge_once")
    def test_rate_limited_retries_with_backoff(self, mock_once, _val, _count, _conf, _dis, mock_sleep):
        def _side(**kw):
            if mock_once.call_count == 1:
                raise RateLimited("429 overloaded")
            return (_valid_parsed(), _META_OK)

        mock_once.side_effect = _side
        parsed, _ = _run_and_verify(diff="d", review_a="a", review_b="b", skwad_binary="./s",
                                    repo_path="/tmp", config_path="c", judge_output_name="o.json",
                                    base_port=8800, task_start_time=time.time())
        self.assertEqual(mock_once.call_count, 2)
        mock_sleep.assert_called_once_with(RATE_LIMIT_BACKOFF_SEC * 1)


if __name__ == "__main__":
    unittest.main()
