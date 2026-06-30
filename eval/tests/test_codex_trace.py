"""C1 coverage for the Codex-judge swap: _parse_codex_trace + run_codex_exec seam.

Drives the parser with the VERBATIM Explorer captures in eval/tests/fixture_codex.py
(codex-cli 0.141.0) — the single source of truth shared with the Coder's impl, so
zero format drift. The parser branches on exit_code, NOT status (rg no-match is
status:"failed" but exit 1 + empty output — the absence signal).
"""

import json
import os
import shutil
import subprocess
import tempfile
import unittest
import unittest.mock as mock

try:
    import eval.lib.openai_judge as oj
    _OJ = oj
except Exception:  # pragma: no cover
    _OJ = None

from eval.tests.fixture_codex import CODEX_SAMPLES, full_stream


@unittest.skipUnless(_OJ is not None and hasattr(_OJ, "_parse_codex_trace"),
                     "blocked: _parse_codex_trace not present")
class TestParseCodexTrace(unittest.TestCase):
    def _one(self, sample_name):
        cmds = _OJ._parse_codex_trace(CODEX_SAMPLES[sample_name])["commands"]
        self.assertEqual(len(cmds), 1, f"{sample_name} should yield 1 command")
        return cmds[0]

    def test_all_eight_fields_present(self):
        c = self._one("cat_read")
        for f in ("cmd", "output", "exit", "is_search", "is_readlike",
                  "attributed_paths", "searched_symbols", "read_paths"):
            self.assertIn(f, c)

    def test_rg_dir_presence_attributes_path_prefixed_match(self):
        c = self._one("rg_dir_presence")
        self.assertTrue(c["is_search"])
        self.assertEqual(c["searched_symbols"], ["move_to_end"])
        self.assertEqual(c["attributed_paths"], ["./cache.py"])

    def test_rg_file_presence_no_prefix_attributes_to_file_arg(self):
        c = self._one("rg_file_presence")
        self.assertEqual(c["read_paths"], ["cache.py"])
        self.assertEqual(c["attributed_paths"], ["cache.py"])

    def test_rg_absence_empty_output_exit1_branches_on_exit_not_status(self):
        # THE absence raw signal. status is "failed" (rg no-match convention) but
        # exit is 1 — the parser must surface exit, not be misled by status.
        self.assertIn('"status": "failed"', CODEX_SAMPLES["rg_absence"])
        c = self._one("rg_absence")
        self.assertEqual(c["output"], "")
        self.assertEqual(c["exit"], 1)
        self.assertEqual(c["attributed_paths"], [])

    def test_cat_read_attributes_to_file(self):
        c = self._one("cat_read")
        self.assertTrue(c["is_readlike"])
        self.assertFalse(c["is_search"])
        self.assertEqual(c["attributed_paths"], ["cache.py"])

    def test_sed_read_dual_quote_unwrap(self):
        c = self._one("sed_read")
        self.assertTrue(c["is_readlike"])
        self.assertEqual(c["attributed_paths"], ["cache.py"])

    def test_echo_fabrication_not_readlike(self):
        # 🔴 An echo'd snippet must NOT count as read evidence (fabrication block).
        c = self._one("echo_fabrication")
        self.assertFalse(c["is_readlike"])
        self.assertEqual(c["attributed_paths"], [])

    def test_python_c_synthesis_classified_by_command_not_output(self):
        # 🔴 python -c that PRINTS file-looking text is still synthesis, not a read.
        c = self._one("python_c_synthesis")
        self.assertFalse(c["is_readlike"])
        self.assertEqual(c["attributed_paths"], [])

    def test_transform_pipeline_unattributable(self):
        # 🔴 cat | sed s/// mutates the stream → output no longer reflects a file.
        c = self._one("transform_pipeline")
        self.assertFalse(c["is_readlike"])
        self.assertEqual(c["attributed_paths"], [])

    def test_search_pipeline_attributes_both_matched_paths(self):
        c = self._one("search_pipeline")
        self.assertTrue(c["is_search"])
        self.assertEqual(set(c["attributed_paths"]), {"./events.jsonl", "./cache.py"})

    def test_turn_completed_yields_no_commands(self):
        self.assertEqual(_OJ._parse_codex_trace(CODEX_SAMPLES["turn_completed"])["commands"], [])

    def test_full_stream_parses_nine_command_events(self):
        # 10 samples joined; turn_completed is not a command_execution → 9 commands.
        self.assertEqual(len(_OJ._parse_codex_trace(full_stream())["commands"]), 9)

    def test_malformed_lines_are_skipped(self):
        jsonl = "not json\n" + CODEX_SAMPLES["cat_read"] + "\n{bad}\n"
        self.assertEqual(len(_OJ._parse_codex_trace(jsonl)["commands"]), 1)


@unittest.skipUnless(_OJ is not None and hasattr(_OJ, "run_codex_exec"),
                     "blocked: run_codex_exec not present")
class TestRunCodexExec(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.worktree = os.path.join(self.tmp, "wt")
        self.out_dir = os.path.join(self.tmp, "out")
        os.makedirs(self.worktree, exist_ok=True)

    def _subproc(self, *, returncode=0, stdout="", write_verdict=None):
        def _run(cmd, **kw):
            self._cmd, self._kw = cmd, kw
            if write_verdict is not None:
                os.makedirs(self.out_dir, exist_ok=True)
                with open(os.path.join(self.out_dir, "verdict.json"), "w") as f:
                    json.dump(write_verdict, f)
            return mock.Mock(returncode=returncode, stdout=stdout, stderr="boom")
        return _run

    def test_happy_returns_verdict_and_trace_with_guardrails(self):
        trace = full_stream("cat_read")
        with mock.patch.object(_OJ.subprocess, "run",
                               side_effect=self._subproc(returncode=0, stdout=trace,
                                                         write_verdict={"review_a": {}, "review_b": {}})):
            verdict, got_trace = _OJ.run_codex_exec("score it", self.worktree,
                                                    "/schema.json", self.out_dir)
        self.assertEqual(verdict, {"review_a": {}, "review_b": {}})
        self.assertEqual(got_trace, trace)
        # G2 stdin closed; G3 verdict.json under out_dir OUTSIDE worktree; read-only + -C.
        self.assertEqual(self._kw.get("stdin"), subprocess.DEVNULL)
        self.assertIn("--json", self._cmd)
        self.assertIn("read-only", self._cmd)
        self.assertIn(self.worktree, self._cmd)
        # The judge worktree is a detached checkout (no .git of its own) → codex exec
        # would abort without --skip-git-repo-check. Regression guard: never dropped.
        self.assertIn("--skip-git-repo-check", self._cmd)
        verdict_arg = self._cmd[self._cmd.index("-o") + 1]
        self.assertTrue(verdict_arg.startswith(self.out_dir))
        self.assertNotIn(self.worktree, verdict_arg)

    def test_skip_git_repo_check_flag_immediately_follows_exec(self):
        # POSITION lock: --skip-git-repo-check must sit RIGHT AFTER the `exec`
        # subcommand. codex parses it as an exec-scoped flag; a later position
        # (after the prompt positional) would be swallowed as prompt text and the
        # git-repo check would re-fire, aborting on the detached judge worktree.
        with mock.patch.object(_OJ.subprocess, "run",
                               side_effect=self._subproc(returncode=0, stdout="",
                                                         write_verdict={"review_a": {}, "review_b": {}})):
            _OJ.run_codex_exec("score it", self.worktree, "/schema.json", self.out_dir)
        self.assertEqual(self._cmd[:3], ["codex", "exec", "--skip-git-repo-check"])

    def test_nonzero_exit_raises(self):
        with mock.patch.object(_OJ.subprocess, "run",
                               side_effect=self._subproc(returncode=2, stdout="")):
            with self.assertRaises(_OJ.CodexExecError):
                _OJ.run_codex_exec("p", self.worktree, "/s.json", self.out_dir)

    def test_timeout_raises(self):
        with mock.patch.object(_OJ.subprocess, "run",
                               side_effect=subprocess.TimeoutExpired("codex", 1)):
            with self.assertRaises(_OJ.CodexExecError):
                _OJ.run_codex_exec("p", self.worktree, "/s.json", self.out_dir)

    def test_missing_verdict_json_raises(self):
        with mock.patch.object(_OJ.subprocess, "run",
                               side_effect=self._subproc(returncode=0, stdout="", write_verdict=None)):
            with self.assertRaises(_OJ.CodexExecError):
                _OJ.run_codex_exec("p", self.worktree, "/s.json", self.out_dir)

    def test_g4_env_scratches_home_tmpdir_preserves_codex_home(self):
        # G4 prevent-half (PARTIAL — tilde-only): HOME/TMPDIR → a per-run scratch under
        # out_dir so a `~`-relative read can't reach the real home. ABSOLUTE-path reads
        # bypass this; the real protection is detect+quarantine (TestG4OutOfWorktree*)
        # + G1 binding-containment. CODEX_HOME is preserved (real ~/.codex — auth must
        # survive the HOME override).
        with mock.patch.object(_OJ.subprocess, "run",
                               side_effect=self._subproc(returncode=0, stdout="",
                                                         write_verdict={"review_a": {}, "review_b": {}})):
            _OJ.run_codex_exec("p", self.worktree, "/s.json", self.out_dir)
        env = self._kw["env"]
        self.assertTrue(env["HOME"].startswith(self.out_dir), env["HOME"])
        self.assertTrue(env["TMPDIR"].startswith(self.out_dir), env["TMPDIR"])
        self.assertTrue(env["CODEX_HOME"].endswith(".codex"))
        self.assertFalse(env["CODEX_HOME"].startswith(self.out_dir),
                         "CODEX_HOME must stay the real ~/.codex, NOT the scratch home")

    def test_env_allowlist_strips_secrets_keeps_required(self):
        # SECURITY (Reviewer): the child env must NOT carry parent secrets — a
        # prompt-injected `echo $AWS_SECRET_ACCESS_KEY` would otherwise exfil them into
        # the trace (which _out_of_worktree_reads can't catch — it's not a file read).
        # Default-deny allowlist: secrets + any non-allowlisted var are ABSENT; PATH /
        # HOME(scratch) / TMPDIR(scratch) / CODEX_HOME(real) / LANG / LC_* survive.
        canaries = {
            "AWS_SECRET_ACCESS_KEY": "CANARY", "OPENAI_API_KEY": "sk-CANARY",
            "FOO_TOKEN": "CANARY-TOK", "BAR_SECRET": "CANARY-SEC",
            "AWS_REGION": "us-east-1", "TOTALLY_RANDOM_VAR": "CANARY-RAND",
            "LANG": "en_US.UTF-8", "LC_ALL": "C", "PATH": os.environ.get("PATH", "/usr/bin"),
        }
        with mock.patch.dict(os.environ, canaries, clear=False):
            with mock.patch.object(_OJ.subprocess, "run",
                                   side_effect=self._subproc(returncode=0, stdout="",
                                                             write_verdict={"review_a": {}, "review_b": {}})):
                _OJ.run_codex_exec("p", self.worktree, "/s.json", self.out_dir)
        env = self._kw["env"]
        # Secrets + non-allowlisted vars stripped (default-deny, not just denylist).
        for leaked in ("AWS_SECRET_ACCESS_KEY", "OPENAI_API_KEY", "FOO_TOKEN",
                       "BAR_SECRET", "AWS_REGION", "TOTALLY_RANDOM_VAR"):
            self.assertNotIn(leaked, env, f"{leaked} must not reach the agent shell")
        # Defense in depth: the canary VALUE must not smuggle in under any key.
        self.assertFalse(any("CANARY" in str(v) for v in env.values()),
                         "a canary value leaked into the child env under some key")
        # Required vars survive so codex still runs + auths.
        self.assertIn("PATH", env)
        self.assertTrue(env["HOME"].startswith(self.out_dir))
        self.assertTrue(env["TMPDIR"].startswith(self.out_dir))
        self.assertTrue(env["CODEX_HOME"].endswith(".codex"))
        self.assertEqual(env.get("LANG"), "en_US.UTF-8")
        self.assertEqual(env.get("LC_ALL"), "C")  # LC_* preserved

    @unittest.skipUnless(_OJ is not None and hasattr(_OJ, "_build_codex_env"),
                         "blocked: _build_codex_env not present")
    def test_build_codex_env_helper_strips_secrets_directly(self):
        # Direct unit on the security boundary (no subprocess) — complements the
        # run_codex_exec wiring test above: the helper itself must default-deny.
        scratch = os.path.join(self.out_dir, "scratch_home")
        canaries = {
            "AWS_SECRET_ACCESS_KEY": "CANARY", "OPENAI_API_KEY": "sk-CANARY",
            "FOO_TOKEN": "CANARY-TOK", "BAR_SECRET": "CANARY-SEC",
            "TOTALLY_RANDOM_VAR": "CANARY-RAND",
            "PATH": os.environ.get("PATH", "/usr/bin"), "LANG": "en_US.UTF-8",
        }
        with mock.patch.dict(os.environ, canaries, clear=False):
            env = _OJ._build_codex_env(scratch)
        for leaked in ("AWS_SECRET_ACCESS_KEY", "OPENAI_API_KEY", "FOO_TOKEN",
                       "BAR_SECRET", "TOTALLY_RANDOM_VAR"):
            self.assertNotIn(leaked, env)
        self.assertFalse(any("CANARY" in str(v) for v in env.values()))
        self.assertIn("PATH", env)
        self.assertEqual(env["HOME"], scratch)
        self.assertEqual(env["TMPDIR"], scratch)
        self.assertTrue(env["CODEX_HOME"].endswith(".codex"))
        self.assertEqual(env.get("LANG"), "en_US.UTF-8")


if __name__ == "__main__":
    unittest.main()
