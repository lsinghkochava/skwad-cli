"""Tests for eval.lib.skwad_runner."""

import os
import tempfile
import unittest
from unittest.mock import MagicMock, patch

from eval.lib.skwad_runner import (
    BINARY_TIMEOUT_BUFFER_SEC,
    DEFAULT_TIMEOUT,
    run_skwad_review,
)


# ---------------------------------------------------------------------------
# TestSkwadRunnerTimeoutArg — regression guard for the "binary killed the run at
# 600s" bug. run_skwad_review MUST pass --timeout to the skwad-cli binary
# (otherwise the binary's internal 10m default fires), and the Popen wrapper
# must wait binary + buffer so the binary self-terminates first.
# ---------------------------------------------------------------------------


class TestSkwadRunnerTimeoutArg(unittest.TestCase):
    def _capture_popen(self, timeout=None):
        """Run run_skwad_review with mocked Popen; return (popen_mock, proc_mock).

        The mocked run produces no comments file, so run_skwad_review raises
        RuntimeError after the subprocess call — by then Popen and communicate
        have already been invoked and their args captured.
        """
        proc = MagicMock()
        proc.communicate.return_value = ("", "")
        proc.returncode = 0

        with tempfile.TemporaryDirectory() as tmpdir:
            with patch("eval.lib.skwad_runner.subprocess.Popen", return_value=proc) as popen:
                kwargs = dict(
                    repo_path=tmpdir,
                    pr_url="https://github.com/Org/Repo/pull/1",
                    pr_number=1,
                    skwad_binary="./skwad",
                    config_path="cfg.json",
                )
                if timeout is not None:
                    kwargs["timeout"] = timeout
                with self.assertRaises(RuntimeError):
                    run_skwad_review(**kwargs)
        return popen, proc

    def test_timeout_flag_present_with_go_duration_value(self):
        popen, _ = self._capture_popen(timeout=1200)
        cmd = popen.call_args.args[0]
        self.assertIn("--timeout", cmd, "run_skwad_review must pass --timeout to the binary")
        self.assertEqual(cmd[cmd.index("--timeout") + 1], "1200s")

    def test_timeout_flag_tracks_arg_value(self):
        popen, _ = self._capture_popen(timeout=300)
        cmd = popen.call_args.args[0]
        self.assertEqual(cmd[cmd.index("--timeout") + 1], "300s")

    def test_default_timeout_passed_as_go_duration(self):
        # The default must still reach the binary as a Go-duration string.
        popen, _ = self._capture_popen()
        cmd = popen.call_args.args[0]
        self.assertEqual(cmd[cmd.index("--timeout") + 1], f"{DEFAULT_TIMEOUT}s")

    def test_wrapper_timeout_is_binary_plus_buffer(self):
        _, proc = self._capture_popen(timeout=1200)
        self.assertEqual(proc.communicate.call_args.kwargs["timeout"], 1200 + BINARY_TIMEOUT_BUFFER_SEC)


if __name__ == "__main__":
    unittest.main()
