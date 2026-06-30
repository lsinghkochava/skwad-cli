"""Tests for eval.lib.difficulty_classifier."""

import json
import os
import tempfile
import unittest
from unittest.mock import MagicMock, patch

from eval.lib.difficulty_classifier import (
    BINARY_TIMEOUT_BUFFER_SEC,
    BUCKET_ORDER,
    _file_paths_from_diff,
    _heuristic_bucket,
    _llm_refine,
    _loc_from_diff,
    classify_pr,
)


class TestLocFromDiff(unittest.TestCase):
    def test_mixed_additions_and_deletions(self):
        self.assertEqual(_loc_from_diff("+a\n+b\n-c\n"), 3)

    def test_empty_diff(self):
        self.assertEqual(_loc_from_diff(""), 0)

    def test_ignores_header_lines(self):
        diff = "+++ b/foo.go\n--- a/foo.go\n+added line\n-removed line\n"
        self.assertEqual(_loc_from_diff(diff), 2)


class TestFilePathsFromDiff(unittest.TestCase):
    def test_extracts_multiple_paths(self):
        diff = "+++ b/foo.go\n+++ b/bar.go\n"
        self.assertEqual(_file_paths_from_diff(diff), ["foo.go", "bar.go"])

    def test_empty_diff(self):
        self.assertEqual(_file_paths_from_diff(""), [])

    def test_ignores_non_header_plus_lines(self):
        # Only lines starting with "+++ b/" are file headers; content lines are ignored.
        diff = "+++ b/real.go\n+some content\n+ +++ not a header\n"
        self.assertEqual(_file_paths_from_diff(diff), ["real.go"])

    def test_rename_only_diff(self):
        diff = "rename from old/foo.go\nrename to new/foo.go\n"
        self.assertEqual(_file_paths_from_diff(diff), ["new/foo.go"])

    def test_rename_and_content_change_deduplicated(self):
        # Git emits both "rename to" and "+++ b/<path>" for rename+edit — deduplicate.
        diff = "rename from old/foo.go\nrename to new/foo.go\n+++ b/new/foo.go\n+added line\n"
        self.assertEqual(_file_paths_from_diff(diff), ["new/foo.go"])


class TestHeuristicBucket(unittest.TestCase):

    # --- Easy ---

    def test_easy_minimal(self):
        self.assertEqual(
            _heuristic_bucket({"files_changed": 1, "additions": 5, "deletions": 5}),
            "easy",
        )

    def test_easy_files3_loc99(self):
        self.assertEqual(
            _heuristic_bucket({"files_changed": 3, "additions": 50, "deletions": 49}),
            "easy",
        )

    # --- Medium ---

    def test_medium_files4_loc100(self):
        self.assertEqual(
            _heuristic_bucket({"files_changed": 4, "additions": 60, "deletions": 40}),
            "medium",
        )

    def test_medium_files10_loc300(self):
        self.assertEqual(
            _heuristic_bucket({"files_changed": 10, "additions": 200, "deletions": 100}),
            "medium",
        )

    # --- Hard ---

    def test_hard_files11_small_loc(self):
        self.assertEqual(
            _heuristic_bucket({"files_changed": 11, "additions": 5, "deletions": 5}),
            "hard",
        )

    def test_hard_loc_at_threshold(self):
        # loc == 500 triggers hard (>= 500 boundary).
        self.assertEqual(
            _heuristic_bucket({"files_changed": 1, "additions": 300, "deletions": 200}),
            "hard",
        )

    def test_hard_sensitive_payment(self):
        self.assertEqual(
            _heuristic_bucket({
                "files_changed": 1, "additions": 3, "deletions": 2,
                "file_paths": ["payment/charge.py"],
            }),
            "hard",
        )

    def test_hard_sensitive_migration(self):
        self.assertEqual(
            _heuristic_bucket({
                "files_changed": 1, "additions": 5, "deletions": 0,
                "file_paths": ["migrations/001_init.sql"],
            }),
            "hard",
        )

    def test_hard_sensitive_crypto(self):
        self.assertEqual(
            _heuristic_bucket({
                "files_changed": 1, "additions": 5, "deletions": 0,
                "file_paths": ["crypto/aes.go"],
            }),
            "hard",
        )

    def test_hard_sensitive_perf(self):
        self.assertEqual(
            _heuristic_bucket({
                "files_changed": 1, "additions": 5, "deletions": 0,
                "file_paths": ["services/perf-monitor.go"],
            }),
            "hard",
        )

    def test_hard_sensitive_auth_case_insensitive(self):
        # IGNORECASE regex matches "AUTHorize" — accepted false positive for auth-adjacent paths.
        self.assertEqual(
            _heuristic_bucket({
                "files_changed": 1, "additions": 5, "deletions": 0,
                "file_paths": ["docs/AUTHorize.md"],
            }),
            "hard",
        )

    def test_no_sensitive_override_login(self):
        self.assertEqual(
            _heuristic_bucket({
                "files_changed": 1, "additions": 5, "deletions": 0,
                "file_paths": ["app/login.tsx"],
            }),
            "easy",
        )

    # --- Boundary: files=3, LOC=100 ---

    def test_boundary_files3_loc100_is_easy(self):
        # files=3 fails files>=4, so medium threshold never triggers — result is easy.
        self.assertEqual(
            _heuristic_bucket({"files_changed": 3, "additions": 60, "deletions": 40}),
            "easy",
        )


class TestLlmRefine(unittest.TestCase):
    """Tests for _llm_refine with mocked subprocess and real temp-file I/O."""

    def _call(self, pr_data, heuristic, llm_json):
        with tempfile.TemporaryDirectory() as tmpdir:
            def fake_run(*args, **kwargs):
                with open(os.path.join(tmpdir, "classifier_output.json"), "w") as f:
                    json.dump(llm_json, f)
                return MagicMock(returncode=0)

            with patch("eval.lib.difficulty_classifier.subprocess.run", side_effect=fake_run):
                return _llm_refine(pr_data, heuristic, "./skwad", tmpdir)

    def test_llm_upgrade_clamped_to_medium(self):
        # Heuristic=easy, LLM=hard → raw_delta=+2 → clamped +1 → final=medium.
        bucket, _, delta = self._call({"files_changed": 2, "diff": "+a\n"}, "easy", {"bucket": "hard"})
        self.assertEqual(bucket, "medium")
        self.assertEqual(delta, 1)

    def test_llm_downgrade_clamped_to_medium(self):
        # Heuristic=hard, LLM=easy → raw_delta=-2 → clamped -1 → final=medium.
        bucket, _, delta = self._call({"files_changed": 15, "diff": ""}, "hard", {"bucket": "easy"})
        self.assertEqual(bucket, "medium")
        self.assertEqual(delta, -1)

    def test_llm_agrees_delta_zero(self):
        bucket, _, delta = self._call({"files_changed": 5, "diff": "+x\n"}, "medium", {"bucket": "medium"})
        self.assertEqual(bucket, "medium")
        self.assertEqual(delta, 0)

    def test_llm_invalid_bucket_falls_back_to_heuristic(self):
        bucket, _, delta = self._call({"files_changed": 2, "diff": ""}, "easy", {"bucket": "unknown_value"})
        self.assertEqual(bucket, "easy")
        self.assertEqual(delta, 0)

    def test_llm_invalid_json_raises(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            def fake_run_garbage(*args, **kwargs):
                with open(os.path.join(tmpdir, "classifier_output.json"), "w") as f:
                    f.write("not json at all")
                return MagicMock(returncode=0)

            with patch("eval.lib.difficulty_classifier.subprocess.run", side_effect=fake_run_garbage):
                with self.assertRaises(Exception):
                    _llm_refine({"files_changed": 1, "diff": ""}, "easy", "./skwad", tmpdir)

    def test_nonzero_exit_raises_and_falls_back(self):
        # Non-zero returncode → _llm_refine logs WARNING with stderr and raises RuntimeError.
        # classify_pr catches RuntimeError and falls back to heuristic with llm_delta=0.
        pr_data = {"files_changed": 2, "additions": 5, "deletions": 5, "diff": "+x\n"}

        failed_result = MagicMock()
        failed_result.returncode = 1
        failed_result.stderr = "skwad-cli: agent crashed"

        with tempfile.TemporaryDirectory() as tmpdir:
            with patch("eval.lib.difficulty_classifier.subprocess.run", return_value=failed_result):
                with self.assertLogs("eval.lib.difficulty_classifier", level="WARNING") as cm:
                    with self.assertRaises(RuntimeError):
                        _llm_refine(pr_data, "easy", "./skwad", tmpdir)

        self.assertTrue(any("agent crashed" in m for m in cm.output))

        # Integration: classify_pr swallows the RuntimeError → heuristic fallback.
        with patch("eval.lib.difficulty_classifier.subprocess.run", return_value=failed_result):
            with self.assertLogs("eval.lib.difficulty_classifier", level="WARNING"):
                result = classify_pr(pr_data)
        self.assertEqual(result["llm_delta"], 0)
        self.assertEqual(result["bucket"], result["heuristic_bucket"])

    def test_diff_truncation_notice_in_prompt(self):
        big_diff = "+x\n" * 11000  # ~33k chars — over the 32000 cap
        pr_data = {"files_changed": 1, "diff": big_diff}
        captured = {}

        with tempfile.TemporaryDirectory() as tmpdir:
            def capture_run(cmd, **kwargs):
                captured["prompt"] = cmd[cmd.index("--prompt") + 1]
                with open(os.path.join(tmpdir, "classifier_output.json"), "w") as f:
                    json.dump({"bucket": "easy"}, f)
                return MagicMock(returncode=0)

            with patch("eval.lib.difficulty_classifier.subprocess.run", side_effect=capture_run):
                _llm_refine(pr_data, "easy", "./skwad", tmpdir)

        prompt = captured["prompt"]
        self.assertIn("truncated at 32000 chars", prompt)
        self.assertIn("full diff is", prompt)
        self.assertIn(str(len(big_diff)), prompt)

        # Negative: short diff must NOT include a truncation notice.
        short_diff = "+x\n" * 100
        pr_data_short = {"files_changed": 1, "diff": short_diff}

        with tempfile.TemporaryDirectory() as tmpdir2:
            def capture_run_short(cmd, **kwargs):
                captured["prompt_short"] = cmd[cmd.index("--prompt") + 1]
                with open(os.path.join(tmpdir2, "classifier_output.json"), "w") as f:
                    json.dump({"bucket": "easy"}, f)
                return MagicMock(returncode=0)

            with patch("eval.lib.difficulty_classifier.subprocess.run", side_effect=capture_run_short):
                _llm_refine(pr_data_short, "easy", "./skwad", tmpdir2)

        self.assertNotIn("truncated at", captured["prompt_short"])
        self.assertIn("=== DIFF ===", captured["prompt_short"])


class TestClassifyPr(unittest.TestCase):
    """Tests for classify_pr — the public entry point."""

    _SIMPLE_PR = {"files_changed": 2, "additions": 5, "deletions": 5, "diff": "+x\n"}

    def test_disagreement_logs_warning(self):
        # delta != 0 → classify_pr emits a WARNING mentioning the disagreement.
        with patch("eval.lib.difficulty_classifier._llm_refine", return_value=("medium", "reason", 1)):
            with self.assertLogs("eval.lib.difficulty_classifier", level="WARNING") as cm:
                result = classify_pr(self._SIMPLE_PR)
        self.assertEqual(result["bucket"], "medium")
        self.assertTrue(any("disagreement" in m or "heuristic" in m for m in cm.output))

    def test_no_warning_when_llm_agrees(self):
        # delta == 0 → no WARNING emitted by classify_pr.
        with patch("eval.lib.difficulty_classifier._llm_refine", return_value=("easy", "agree", 0)):
            with self.assertNoLogs("eval.lib.difficulty_classifier", level="WARNING"):
                result = classify_pr(self._SIMPLE_PR)
        self.assertEqual(result["llm_delta"], 0)

    def test_llm_failure_falls_back_to_heuristic(self):
        with patch("eval.lib.difficulty_classifier._llm_refine", side_effect=RuntimeError("subprocess failed")):
            with self.assertLogs("eval.lib.difficulty_classifier", level="WARNING"):
                result = classify_pr(self._SIMPLE_PR)
        self.assertEqual(result["bucket"], result["heuristic_bucket"])
        self.assertEqual(result["llm_delta"], 0)

    def test_llm_failure_no_exception_bubbles(self):
        with patch("eval.lib.difficulty_classifier._llm_refine", side_effect=Exception("unexpected")):
            with self.assertLogs("eval.lib.difficulty_classifier", level="WARNING"):
                result = classify_pr(self._SIMPLE_PR)
        self.assertIn("bucket", result)
        self.assertIn("llm_delta", result)


# ---------------------------------------------------------------------------
# TestClassifierTimeoutArg — regression guard: the classifier caller MUST pass
# --timeout to the skwad-cli binary, and the Python wrapper timeout MUST be
# binary + buffer. Without --timeout the binary's 10m default would fire.
# ---------------------------------------------------------------------------


class TestClassifierTimeoutArg(unittest.TestCase):
    def _capture_run(self, timeout):
        with tempfile.TemporaryDirectory() as tmpdir:
            def fake_run(*args, **kwargs):
                with open(os.path.join(tmpdir, "classifier_output.json"), "w") as f:
                    json.dump({"bucket": "medium"}, f)
                return MagicMock(returncode=0)

            with patch("eval.lib.difficulty_classifier.subprocess.run", side_effect=fake_run) as m:
                _llm_refine({"files_changed": 2, "diff": "+a\n"}, "medium", "./skwad", tmpdir, timeout=timeout)
        return m

    def test_timeout_flag_present_with_go_duration_value(self):
        m = self._capture_run(timeout=120)
        cmd = m.call_args.args[0]
        self.assertIn("--timeout", cmd, "classifier must pass --timeout to the skwad-cli binary")
        self.assertEqual(cmd[cmd.index("--timeout") + 1], "120s")

    def test_timeout_flag_tracks_arg_value(self):
        m = self._capture_run(timeout=300)
        cmd = m.call_args.args[0]
        self.assertEqual(cmd[cmd.index("--timeout") + 1], "300s")

    def test_wrapper_timeout_is_binary_plus_buffer(self):
        m = self._capture_run(timeout=120)
        self.assertEqual(m.call_args.kwargs["timeout"], 120 + BINARY_TIMEOUT_BUFFER_SEC)


if __name__ == "__main__":
    unittest.main()
