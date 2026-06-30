"""Integration tests for eval/cmd/eval-reviews/main.py.

The directory name contains a hyphen so normal import is impossible;
importlib.util.spec_from_file_location is used to load it as '_eval_main'.
"""

import importlib.util
import json
import os
import re
import sys
import tempfile
import unittest
from contextlib import ExitStack
from unittest.mock import patch

# ---------------------------------------------------------------------------
# Load main.py once at module level — exec_module runs top-level setup
# (sys.path mutation, lib.* imports, logger declaration)
# ---------------------------------------------------------------------------
_MAIN_PATH = os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "cmd", "eval-reviews", "main.py")
)
_spec = importlib.util.spec_from_file_location("_eval_main", _MAIN_PATH)
_main = importlib.util.module_from_spec(_spec)
sys.modules["_eval_main"] = _main
_spec.loader.exec_module(_main)


# ---------------------------------------------------------------------------
# C2 (Codex swap): the judge is now an in-process `codex exec` subprocess — main()
# / evaluate_pr build NO OpenAI client, so the old build_client/build_system_prompt
# module-stubs are gone. The judge invocation is mocked at score_paired_reviews /
# prepare_pr / run_single_judge_task in the tests below; nothing reaches real codex.
# ---------------------------------------------------------------------------


# ---------------------------------------------------------------------------
# Shared fixture helpers
# ---------------------------------------------------------------------------

def _make_difficulty() -> dict:
    return {"bucket": "easy", "heuristic_bucket": "easy", "llm_delta": 0, "reasoning": "test"}


def _make_scored() -> dict:
    criteria = [
        "issue_detection", "actionability", "severity_accuracy",
        "coverage", "signal_to_noise", "depth", "novel_substantive_findings",
    ]
    base_sys = {
        **{c: {"scores": [1, 1, 1], "voted": 1, "reasoning_runs": ["r"] * 3} for c in criteria},
        "n_runs_completed": 3,
        "n_runs_planned": 3,
    }
    return {
        "skwad": dict(base_sys, total=10),
        "claude_ci": dict(base_sys, total=8),
        "runs": [
            {"ab_assignment": ["skwad", "claude_ci"],
             "resolved": {"skwad": {"total": 10}, "claude_ci": {"total": 8}}},
        ],
        "ab_assignments": [["skwad", "claude_ci"]],
        "n_runs_completed": 3,
        "n_runs_planned": 3,
    }


def _make_pr_data() -> dict:
    return {
        "repo": "Kochava/repo",
        "pr_number": 1,
        "commit_sha": "abc123",
        "url": "https://github.com/Kochava/repo/pull/1",
        "comments": [],
        "diff": "+def foo(): pass",
        "files_changed": 2,
        "title": "Test PR",
    }


def _make_run_manifest() -> dict:
    return {
        "run_id": "test-id",
        "rng_seed": 42,
        "prs": [],
        "skipped_prs": [],
        "models": {},
        "prompt_hashes": {},
    }


def _build_pr_file(tmpdir: str) -> str:
    path = os.path.join(tmpdir, "prs.json")
    entries = [{"repo_ssh": "git@github.com:Kochava/repo.git", "prs": [1]}]
    with open(path, "w") as f:
        json.dump(entries, f)
    return path


def _run_main(tmpdir: str, extra_argv=None, evaluate_return=None):
    """Helper: run main() with all external calls patched. Returns the generate_research_report mock.

    main() now scores via the parallel pipeline (prepare_pr → run_single_judge_task
    worker pool → finalize_pr_runs → assemble_pr_result), so we patch that path:
      - evaluate_return is None  → simulate a SKIPPED PR (prepare_pr returns None);
        pr_results stays empty (matches the old evaluate_pr-returns-None behavior).
      - evaluate_return is a dict → simulate one fully-scored PR; assemble_pr_result
        yields it so pr_results == [evaluate_return].
    """
    pr_file = _build_pr_file(tmpdir)
    argv = ["prog", "--pr-file", pr_file, "--output-dir", tmpdir] + (extra_argv or [])
    with ExitStack() as stack:
        stack.enter_context(patch("sys.argv", argv))
        stack.enter_context(patch("_eval_main.clone_repo_ssh", return_value="/tmp/repo"))
        stack.enter_context(patch("_eval_main.fetch_pr", return_value=_make_pr_data()))
        # Per-PR checkout/isolation (Phase 3) runs in the sequential prep loop; mock
        # it so the orchestration tests don't touch real git.
        stack.enter_context(patch("_eval_main.prepare_pr_checkout", return_value="deadbeef"))
        if evaluate_return is None:
            stack.enter_context(patch("_eval_main.prepare_pr", return_value=None))
        else:
            context = {
                "pr_number": 1,
                "run_dir": os.path.join(tmpdir, "Kochava-repo-1"),
                "tasks": [{"pr_number": 1, "run_index": 1}],
                "assignments": [("skwad", "claude_ci")],
            }
            ok_res = {
                "pr_number": 1, "run_index": 1, "status": "ok",
                "counter_increment": None, "duration_seconds": 1.0,
                "resolved": {}, "run_record": {}, "canary_outcomes": [],
            }
            stack.enter_context(patch("_eval_main.prepare_pr", return_value=context))
            stack.enter_context(patch("_eval_main.run_single_judge_task", return_value=ok_res))
            stack.enter_context(patch("_eval_main.finalize_pr_runs", return_value=_make_scored()))
            stack.enter_context(patch("_eval_main.assemble_pr_result", return_value=evaluate_return))
        mock_report = stack.enter_context(patch("_eval_main.generate_research_report"))
        _main.main()
    return mock_report


# ---------------------------------------------------------------------------
# TestArgparse
# ---------------------------------------------------------------------------

class TestArgparse(unittest.TestCase):
    def _parse(self, extra=None):
        with patch("sys.argv", ["prog", "--pr-file", "/tmp/x.json"] + (extra or [])):
            return _main.parse_args()

    def test_research_mode_default_false(self):
        self.assertFalse(self._parse().research_mode)

    def test_research_mode_flag_sets_true(self):
        self.assertTrue(self._parse(["--research-mode"]).research_mode)

    def test_seed_default_12345(self):
        self.assertEqual(self._parse().seed, 12345)

    def test_seed_accepts_int(self):
        self.assertEqual(self._parse(["--seed", "99"]).seed, 99)


# ---------------------------------------------------------------------------
# TestReadModelFromConfig
# ---------------------------------------------------------------------------

class TestReadModelFromConfig(unittest.TestCase):
    def test_agents_model_returned(self):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "c.json")
            with open(p, "w") as f:
                json.dump({"agents": [{"model": "claude-sonnet-4-20250514"}]}, f)
            self.assertEqual(_main._read_model_from_config(p), "claude-sonnet-4-20250514")

    def test_top_level_model_returned(self):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "c.json")
            with open(p, "w") as f:
                json.dump({"model": "claude-opus-4"}, f)
            self.assertEqual(_main._read_model_from_config(p), "claude-opus-4")

    def test_no_model_field_returns_unknown(self):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "c.json")
            with open(p, "w") as f:
                json.dump({"agents": [{"name": "coder"}]}, f)
            self.assertEqual(_main._read_model_from_config(p), "unknown")

    def test_file_not_found_returns_unknown(self):
        self.assertEqual(_main._read_model_from_config("/nonexistent/path.json"), "unknown")


# ---------------------------------------------------------------------------
# TestReadAgentModelsFromConfig
# ---------------------------------------------------------------------------

class TestReadAgentModelsFromConfig(unittest.TestCase):
    def _write(self, d, data):
        p = os.path.join(d, "c.json")
        with open(p, "w") as f:
            json.dump(data, f)
        return p

    def test_per_agent_models_mapped_by_name(self):
        with tempfile.TemporaryDirectory() as d:
            p = self._write(d, {"model": "claude-sonnet-4-6", "agents": [
                {"name": "Performance Analyst", "model": "claude-sonnet-4-6"},
                {"name": "Review Coordinator", "model": "claude-haiku-4-5"},
            ]})
            self.assertEqual(
                _main._read_agent_models_from_config(p),
                {"Performance Analyst": "claude-sonnet-4-6",
                 "Review Coordinator": "claude-haiku-4-5"},
            )

    def test_falls_back_to_top_level_when_agent_has_no_model(self):
        with tempfile.TemporaryDirectory() as d:
            p = self._write(d, {"model": "claude-sonnet-4-6", "agents": [
                {"name": "Bug Hunter"},  # no per-agent model -> top-level default
                {"name": "Review Coordinator", "model": "claude-haiku-4-5"},
            ]})
            self.assertEqual(
                _main._read_agent_models_from_config(p),
                {"Bug Hunter": "claude-sonnet-4-6",
                 "Review Coordinator": "claude-haiku-4-5"},
            )

    def test_default_when_no_agent_and_no_top_level_model(self):
        with tempfile.TemporaryDirectory() as d:
            p = self._write(d, {"agents": [{"name": "Judge"}]})
            self.assertEqual(_main._read_agent_models_from_config(p), {"Judge": "default"})

    def test_file_not_found_returns_empty(self):
        self.assertEqual(_main._read_agent_models_from_config("/nonexistent/path.json"), {})


# ---------------------------------------------------------------------------
# TestEvaluatePrOrchestration
# ---------------------------------------------------------------------------

class TestEvaluatePrOrchestration(unittest.TestCase):
    def _evaluate(self, seed=42, run_manifest=None, **kw):
        return _main.evaluate_pr(
            pr_data=_make_pr_data(),
            clone_path="/tmp/repo",
            skwad_binary="./skwad",
            skwad_config="./config.json",
            judge_config=None,
            run_dir="/tmp/runs",
            seed=seed,
            timeout=60,
            run_manifest=run_manifest if run_manifest is not None else _make_run_manifest(),
            **kw,
        )

    @patch("_eval_main.score_paired_reviews")
    @patch("_eval_main.run_skwad_review", return_value="skwad review")
    @patch("_eval_main.extract_claude_review", return_value="ci review")
    @patch("_eval_main.classify_pr")
    def test_happy_path_all_required_keys(self, mock_classify, _ec, _rs, mock_score):
        mock_classify.return_value = _make_difficulty()
        mock_score.return_value = _make_scored()
        result = self._evaluate()
        self.assertIsNotNone(result)
        for key in ("pr_data", "repo", "pr", "commit_sha", "difficulty",
                    "skwad_review", "claude_ci_review", "skwad", "claude_ci",
                    "runs", "ab_assignments", "n_runs_completed", "n_runs_planned"):
            self.assertIn(key, result, f"Missing key: {key!r}")

    @patch("_eval_main.score_paired_reviews")
    @patch("_eval_main.run_skwad_review", return_value="skwad review")
    @patch("_eval_main.extract_claude_review", return_value="ci review")
    @patch("_eval_main.classify_pr")
    def test_seed_propagated_to_score_paired_reviews(self, mock_classify, _ec, _rs, mock_score):
        mock_classify.return_value = _make_difficulty()
        mock_score.return_value = _make_scored()
        self._evaluate(seed=77)
        self.assertEqual(mock_score.call_args.kwargs["seed"], 77)

    @patch("_eval_main.run_skwad_review", return_value="skwad review")
    @patch("_eval_main.extract_claude_review", return_value=None)
    @patch("_eval_main.classify_pr")
    def test_skip_when_claude_ci_review_missing(self, mock_classify, _ec, _rs):
        mock_classify.return_value = _make_difficulty()
        manifest = _make_run_manifest()
        result = self._evaluate(run_manifest=manifest)
        self.assertIsNone(result)
        self.assertEqual(len(manifest["skipped_prs"]), 1)
        self.assertIn("Claude CI", manifest["skipped_prs"][0]["reason"])

    @patch("_eval_main.run_skwad_review", return_value="")
    @patch("_eval_main.extract_claude_review", return_value="ci review")
    @patch("_eval_main.classify_pr")
    def test_skip_when_skwad_review_empty(self, mock_classify, _ec, _rs):
        mock_classify.return_value = _make_difficulty()
        manifest = _make_run_manifest()
        result = self._evaluate(run_manifest=manifest)
        self.assertIsNone(result)
        self.assertEqual(len(manifest["skipped_prs"]), 1)

    @patch("_eval_main.score_paired_reviews")
    @patch("_eval_main.run_skwad_review", return_value="skwad review")
    @patch("_eval_main.extract_claude_review", return_value="ci review")
    @patch("_eval_main.classify_pr")
    def test_successful_run_appends_to_manifest_prs(self, mock_classify, _ec, _rs, mock_score):
        mock_classify.return_value = _make_difficulty()
        mock_score.return_value = _make_scored()
        manifest = _make_run_manifest()
        result = self._evaluate(run_manifest=manifest)
        self.assertIsNotNone(result)
        self.assertEqual(len(manifest["prs"]), 1)
        self.assertEqual(manifest["prs"][0]["pr"], 1)

    @patch("_eval_main.score_paired_reviews")
    @patch("_eval_main.run_skwad_review", return_value="skwad review")
    @patch("_eval_main.extract_claude_review", return_value="ci review")
    @patch("_eval_main.classify_pr")
    def test_judge_called_with_worktree_and_no_skwad_binary(self, mock_classify, _ec, mock_rs, mock_score):
        # Phase 5 wire-in: the OpenAI judge gets the per-PR repo path (clone_path,
        # the worktree) and NO skwad_binary (that arg is gone from the judge).
        mock_classify.return_value = _make_difficulty()
        mock_score.return_value = _make_scored()
        self._evaluate()
        kw = mock_score.call_args.kwargs
        self.assertEqual(kw.get("repo_path"), "/tmp/repo")
        self.assertNotIn("skwad_binary", kw)
        # SCOPE GUARD: the skwad-subprocess path (classifier + skwad review) still ran.
        mock_classify.assert_called_once()
        mock_rs.assert_called_once()


# ---------------------------------------------------------------------------
# TestMainOrchestration
# ---------------------------------------------------------------------------

class TestMainOrchestration(unittest.TestCase):
    def test_manifest_written_to_output_dir(self):
        with tempfile.TemporaryDirectory() as d:
            _run_main(d)
            self.assertTrue(os.path.exists(os.path.join(d, "manifest.json")))

    def test_manifest_completed_at_utc_set(self):
        with tempfile.TemporaryDirectory() as d:
            _run_main(d)
            with open(os.path.join(d, "manifest.json")) as f:
                data = json.load(f)
            self.assertIsNotNone(data["completed_at_utc"])

    def test_research_mode_calls_generate_research_report(self):
        pr_result = {
            "pr_data": _make_pr_data(), "repo": "Kochava/repo", "pr": 1,
            "commit_sha": "abc", "difficulty": _make_difficulty(),
            "skwad_review": "s", "claude_ci_review": "c",
            "skwad": {}, "claude_ci": {}, "runs": [],
            "ab_assignments": [], "n_runs_completed": 0, "n_runs_planned": 3,
        }
        with tempfile.TemporaryDirectory() as d:
            mock_report = _run_main(d, extra_argv=["--research-mode"], evaluate_return=pr_result)
        mock_report.assert_called_once()

    def test_no_research_mode_skips_generate_research_report(self):
        with tempfile.TemporaryDirectory() as d:
            mock_report = _run_main(d)  # research_mode defaults to False
        mock_report.assert_not_called()


# ---------------------------------------------------------------------------
# TestLegacyRemoved
# ---------------------------------------------------------------------------

class TestLegacyRemoved(unittest.TestCase):
    def test_generate_pr_report_not_in_reporter(self):
        from eval.lib import reporter
        self.assertFalse(hasattr(reporter, "generate_pr_report"))

    def test_generate_aggregate_report_not_in_reporter(self):
        from eval.lib import reporter
        self.assertFalse(hasattr(reporter, "generate_aggregate_report"))

    def test_save_functions_not_in_reporter(self):
        from eval.lib import reporter
        self.assertFalse(hasattr(reporter, "save_pr_report"))
        self.assertFalse(hasattr(reporter, "save_aggregate_report"))

    def test_no_non_test_python_files_reference_generate_pr_report(self):
        eval_dir = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
        pattern = re.compile(r"generate_pr_report|generate_aggregate_report")
        culprits = []
        for root, dirs, files in os.walk(eval_dir):
            dirs[:] = [d for d in dirs if d != "__pycache__"]
            for fname in files:
                if not fname.endswith(".py") or fname.startswith("test_"):
                    continue
                fpath = os.path.join(root, fname)
                with open(fpath) as fh:
                    if pattern.search(fh.read()):
                        culprits.append(fpath)
        self.assertEqual(culprits, [], f"Legacy references found in: {culprits}")


# ---------------------------------------------------------------------------
# TestEvalConfigDir  (tests 1 — pending Coder's path fix + module-level move)
# ---------------------------------------------------------------------------

_PROJECT_ROOT = os.path.abspath(os.path.join(os.path.dirname(_MAIN_PATH), "..", "..", ".."))
_EXPECTED_EVAL_CONFIG = os.path.normpath(os.path.join(_PROJECT_ROOT, "eval", "config"))


class TestEvalConfigDir(unittest.TestCase):
    # Compute expected path: dirname(main.py)/../../config = eval/config
    # (Coder's fix dropped the extra "eval" segment)
    _DIR = os.path.normpath(os.path.join(os.path.dirname(_MAIN_PATH), "..", "..", "config"))

    def test_eval_config_resolves_to_existing_directory(self):
        self.assertTrue(os.path.exists(self._DIR), f"eval/config dir not found at: {self._DIR!r}")

    def test_eval_config_ends_with_eval_config_not_eval_eval_config(self):
        self.assertTrue(
            self._DIR.endswith(os.path.join("eval", "config")),
            f"Expected path ending with eval/config, got: {self._DIR!r}",
        )
        self.assertNotIn(os.path.join("eval", "eval"), self._DIR)

    def test_judge_and_classifier_configs_reachable_at_eval_config(self):
        for fname in ("judge_team.json", "classifier_team.json"):
            self.assertTrue(os.path.exists(os.path.join(self._DIR, fname)),
                            f"{fname} not found under {self._DIR!r}")


# ---------------------------------------------------------------------------
# TestArgparse — round-2 addition: --judge-runs removed
# ---------------------------------------------------------------------------

class TestJudgeRunsFlagRemoved(unittest.TestCase):
    def test_judge_runs_flag_raises_system_exit(self):
        # --judge-runs was a dead flag; Coder removed it. argparse exits on unknown flags.
        with patch("sys.argv", ["prog", "--pr-file", "/tmp/x.json", "--judge-runs", "5"]):
            with self.assertRaises(SystemExit) as ctx:
                _main.parse_args()
        self.assertNotEqual(ctx.exception.code, 0)


# ---------------------------------------------------------------------------
# TestTeamConfigs — locks config model fields (pending Coder's config update)
# ---------------------------------------------------------------------------

_TEAM_CONFIG_PATHS = [
    os.path.join(_PROJECT_ROOT, "eval", "config", "judge_team.json"),
    os.path.join(_PROJECT_ROOT, "eval", "config", "classifier_team.json"),
    os.path.join(_PROJECT_ROOT, "test_configs", "skwad_review_team.json"),
]


class TestTeamConfigs(unittest.TestCase):
    def test_all_team_configs_have_model_field(self):
        for path in _TEAM_CONFIG_PATHS:
            with self.subTest(path=path):
                self.assertTrue(os.path.exists(path), f"Config file missing: {path}")
                with open(path) as f:
                    d = json.load(f)
                # _read_model_from_config checks top-level first, then agents[0]
                model = d.get("model") or (d.get("agents", [{}])[0].get("model") if d.get("agents") else None)
                self.assertIsNotNone(model, f"No model field found in {path}")
                self.assertRegex(model, r"^claude-", f"Model in {path} should start with 'claude-'")


# ---------------------------------------------------------------------------
# TestEvaluatePrOrchestration — round-2 addition: order fix
# (pending Coder's swap of classify_pr / extract_claude_review)
# ---------------------------------------------------------------------------

class TestEvaluatePrOrderFix(unittest.TestCase):
    @patch("_eval_main.run_skwad_review", return_value="skwad review")
    @patch("_eval_main.extract_claude_review", return_value=None)
    @patch("_eval_main.classify_pr")
    def test_classify_pr_not_called_when_ci_review_absent(self, mock_classify, _ec, _rs):
        """After order fix: CI extraction runs first; classify_pr is skipped on CI miss."""
        manifest = _make_run_manifest()
        result = _main.evaluate_pr(
            pr_data=_make_pr_data(), clone_path="/tmp/r",
            skwad_binary="./s", skwad_config="./c", judge_config=None,
            run_dir="/tmp/r", seed=42, timeout=60, run_manifest=manifest,
        )
        self.assertIsNone(result)
        self.assertEqual(mock_classify.call_count, 0, "classify_pr must NOT be called when CI review is absent")
        self.assertEqual(len(manifest["skipped_prs"]), 1)
        self.assertIn("Claude CI", manifest["skipped_prs"][0]["reason"])


# ---------------------------------------------------------------------------
# TestReadModelFromConfig — round-2: real model field, no warning
# ---------------------------------------------------------------------------

class TestReadModelFromConfigRealField(unittest.TestCase):
    def test_real_model_field_no_warning_emitted(self):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "cfg.json")
            with open(p, "w") as f:
                json.dump({"model": "claude-sonnet-4-20250514", "agents": []}, f)
            # Should not log any warnings
            import logging
            with self.assertNoLogs(level=logging.WARNING):
                result = _main._read_model_from_config(p)
        self.assertEqual(result, "claude-sonnet-4-20250514")


# ---------------------------------------------------------------------------
# TestMainOrchestration — round-2: real model IDs in manifest
# (pending Coder's path fix + config model fields)
# ---------------------------------------------------------------------------

class TestMainOrchestrationModelIds(unittest.TestCase):
    def test_judge_model_stamped_codex_default_not_from_config(self):
        # C2 (Codex swap): the JUDGE is now the in-process `codex exec` judge, so its
        # model is stamped from openai_judge.CODEX_DEFAULT_MODEL ("gpt-5.4") — NOT
        # auto-detected from judge_team.json (which still holds the old persona model).
        from eval.lib.openai_judge import CODEX_DEFAULT_MODEL
        with tempfile.TemporaryDirectory() as d:
            _run_main(d)
            with open(os.path.join(d, "manifest.json")) as f:
                data = json.load(f)
        self.assertEqual(data["models"]["judge"], "gpt-5.4")
        self.assertEqual(data["models"]["judge"], CODEX_DEFAULT_MODEL)
        # claude_ci (the system under test's CI reviewer) is untouched by the swap.
        self.assertNotEqual(data["models"].get("claude_ci"), "gpt-5.4")


# ---------------------------------------------------------------------------
# Phase 5 wire-in SCOPE GUARD — only the JUDGE swaps to the in-process OpenAI
# judge; the difficulty classifier and the skwad-review-under-test stay skwad
# subprocesses. Asserted by source module so it survives the dual-import path.
# ---------------------------------------------------------------------------

class TestJudgeWireInScopeGuard(unittest.TestCase):
    def test_judge_swapped_to_openai_judge(self):
        self.assertIn("openai_judge", _main.score_paired_reviews.__module__)
        self.assertIn("openai_judge", _main.prepare_pr_judge_tasks.__module__)

    def test_classifier_and_skwad_review_NOT_swapped(self):
        # The two skwad subprocess entrypoints must remain on their own modules,
        # never routed through openai_judge.
        self.assertIn("difficulty_classifier", _main.classify_pr.__module__)
        self.assertIn("skwad_runner", _main.run_skwad_review.__module__)
        self.assertNotIn("openai_judge", _main.classify_pr.__module__)
        self.assertNotIn("openai_judge", _main.run_skwad_review.__module__)



# ---------------------------------------------------------------------------
# Section I: load_canaries
# ---------------------------------------------------------------------------

class TestLoadCanaries(unittest.TestCase):
    def test_none_path_returns_empty_list(self):
        self.assertEqual(_main.load_canaries(None), [])

    def test_valid_json_file_returns_list(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "canaries.json")
            with open(path, "w") as f:
                json.dump([{"id": "c1", "target_pr": {"pr": 1}}], f)
            result = _main.load_canaries(path)
        self.assertEqual(result, [{"id": "c1", "target_pr": {"pr": 1}}])

    def test_empty_json_array_returns_empty_list(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "canaries.json")
            with open(path, "w") as f:
                json.dump([], f)
            result = _main.load_canaries(path)
        self.assertEqual(result, [])


# ---------------------------------------------------------------------------
# Section I: --inject-canary flag + pilot_pass in manifest
# ---------------------------------------------------------------------------

class TestInjectCanaryFlag(unittest.TestCase):
    def test_inject_canary_default_is_none(self):
        with patch("sys.argv", ["prog", "--pr-file", "/tmp/x.json"]):
            args = _main.parse_args()
        self.assertIsNone(args.inject_canary)

    def test_inject_canary_accepts_path(self):
        with patch("sys.argv", ["prog", "--pr-file", "/tmp/x.json",
                                "--inject-canary", "/tmp/canaries.json"]):
            args = _main.parse_args()
        self.assertEqual(args.inject_canary, "/tmp/canaries.json")

    def test_main_with_canary_writes_pilot_pass_to_manifest(self):
        with tempfile.TemporaryDirectory() as d:
            canary_path = os.path.join(d, "canaries.json")
            with open(canary_path, "w") as f:
                json.dump([{
                    "id": "c1",
                    "target_pr": {"pr": 1},
                    "inject_into": "skwad",
                    "claim_text": "fake claim",
                    "expected_outcome": "contradicted",
                }], f)

            _run_main(d, extra_argv=["--inject-canary", canary_path])

            with open(os.path.join(d, "manifest.json")) as f:
                data = json.load(f)

        # pilot_pass is initialized to None in open_manifest; the canary block
        # runs even with 0 successful PRs and sets it to a bool.
        self.assertIn("pilot_pass", data)
        self.assertIsNotNone(data["pilot_pass"])

    def test_main_without_canary_writes_bool_pilot_pass_to_manifest(self):
        # Un-bundled contract: with NO --inject-canary, evaluate_pilot_pass still
        # runs over the 6 canary-independent criteria and writes a real bool
        # pilot_pass (not None). Mirrors the canary case for the no-canary path.
        with tempfile.TemporaryDirectory() as d:
            _run_main(d)  # no --inject-canary

            with open(os.path.join(d, "manifest.json")) as f:
                data = json.load(f)

        self.assertIn("pilot_pass", data)
        self.assertIsInstance(data["pilot_pass"], bool)
        # canaries_caught is recorded SKIPPED/NA (None) and excluded from the AND.
        details = data["pilot_pass_details"]
        self.assertIsNone(details["criterion_results"]["canaries_caught"])


# ---------------------------------------------------------------------------
# Section A: main aggregation invokes check_methodology_version
# (Reviewer Critical #1)
# ---------------------------------------------------------------------------

class TestMainMethodologyGate(unittest.TestCase):
    def test_main_aggregation_block_invokes_version_check(self):
        # The post-PR-loop block must call check_methodology_version exactly once.
        with tempfile.TemporaryDirectory() as d:
            with patch("_eval_main.check_methodology_version") as mock_check:
                _run_main(d)
            mock_check.assert_called_once()

    def test_main_version_check_blocks_reporter(self):
        # If check_methodology_version raises, generate_research_report must NOT be called.
        from eval.lib.stats import MethodologyMismatchError
        with tempfile.TemporaryDirectory() as d:
            pr_file = _build_pr_file(d)
            argv = ["prog", "--pr-file", pr_file, "--output-dir", d, "--research-mode"]
            with ExitStack() as stack:
                stack.enter_context(patch("sys.argv", argv))
                stack.enter_context(patch("_eval_main.clone_repo_ssh", return_value="/tmp/repo"))
                stack.enter_context(patch("_eval_main.fetch_pr", return_value=_make_pr_data()))
                stack.enter_context(patch("_eval_main.evaluate_pr", return_value=None))
                stack.enter_context(patch(
                    "_eval_main.check_methodology_version",
                    side_effect=MethodologyMismatchError("test mismatch"),
                ))
                mock_report = stack.enter_context(patch("_eval_main.generate_research_report"))
                with self.assertRaises(MethodologyMismatchError):
                    _main.main()
            mock_report.assert_not_called()


# ---------------------------------------------------------------------------
# Section C: pilot_counters init includes structural_invalid_rejections
# (Reviewer Critical #2 / Important #4)
# ---------------------------------------------------------------------------

class TestPilotCountersInit(unittest.TestCase):
    def test_pilot_counters_init_includes_structural_invalid(self):
        """main.py must initialize pilot_counters with structural_invalid_rejections=0
        (alongside the other rejection counters) and persist them to the manifest.

        Verified via the observable manifest, since the parallel orchestrator applies
        counters single-threaded in main() rather than passing them to evaluate_pr.
        """
        with tempfile.TemporaryDirectory() as d:
            _run_main(d)  # skipped PR → counters initialized to 0 and persisted
            with open(os.path.join(d, "manifest.json")) as f:
                manifest = json.load(f)

        self.assertEqual(manifest["structural_invalid_rejections"], 0)
        self.assertEqual(manifest["confabulation_rejections"], 0)
        self.assertEqual(manifest["disallowed_tool_rejections"], 0)
        # Phase 5: the fabrication signal is counted separately from bad-JSON.
        self.assertEqual(manifest["evidence_binding_rejections"], 0)
        # C4/G4: out-of-worktree-read quarantine counter + incident list initialized.
        self.assertEqual(manifest["out_of_worktree_read_quarantines"], 0)
        self.assertEqual(manifest["out_of_worktree_reads"], [])


# ---------------------------------------------------------------------------
# main() parallel scoring pool — defensive isolation of a hard crash
# ---------------------------------------------------------------------------

class TestMainParallelPoolFailureIsolation(unittest.TestCase):
    def test_task_crash_in_pool_is_recorded_and_run_continues(self):
        """A judge task that HARD-CRASHES inside the worker pool (future.result()
        raises, vs. the normal status='failed' return) must be wrapped into a
        failed record, land in run_manifest['failed_tasks'], and NOT sink the run —
        the surviving task still finalizes into a pr_results entry. Also confirms the
        parallel-cost manifest fields are written.
        """
        # One PR with two run-tasks; --max-workers 1 makes pool order deterministic:
        # the first task crashes, the second returns ok.
        ctx_tasks = [
            {"pr_number": 1, "run_index": 1, "port": 8800},
            {"pr_number": 1, "run_index": 2, "port": 8801},
        ]
        ok_res = {
            "pr_number": 1, "run_index": 2, "status": "ok",
            "counter_increment": None, "duration_seconds": 1.0,
            "resolved": {}, "run_record": {}, "canary_outcomes": [],
        }
        calls = {"n": 0}

        def _side(task):
            calls["n"] += 1
            if calls["n"] == 1:
                raise RuntimeError("pool task hard-crashed")
            return ok_res

        with tempfile.TemporaryDirectory() as d:
            context = {
                "pr_number": 1,
                "run_dir": os.path.join(d, "Kochava-repo-1"),
                "tasks": ctx_tasks,
                "assignments": [("skwad", "claude_ci"), ("claude_ci", "skwad")],
            }
            pr_file = _build_pr_file(d)
            argv = ["prog", "--pr-file", pr_file, "--output-dir", d, "--max-workers", "1"]
            with ExitStack() as stack:
                stack.enter_context(patch("sys.argv", argv))
                stack.enter_context(patch("_eval_main.clone_repo_ssh", return_value="/tmp/repo"))
                stack.enter_context(patch("_eval_main.fetch_pr", return_value=_make_pr_data()))
                stack.enter_context(patch("_eval_main.prepare_pr_checkout", return_value="deadbeef"))
                stack.enter_context(patch("_eval_main.prepare_pr", return_value=context))
                stack.enter_context(patch("_eval_main.run_single_judge_task", side_effect=_side))
                stack.enter_context(patch("_eval_main.finalize_pr_runs", return_value=_make_scored()))
                mock_assemble = stack.enter_context(patch(
                    "_eval_main.assemble_pr_result",
                    return_value={"pr": 1, "skwad": {}, "claude_ci": {}},
                ))
                stack.enter_context(patch("_eval_main.generate_research_report"))
                _main.main()
            with open(os.path.join(d, "manifest.json")) as f:
                manifest = json.load(f)

        # The crash is recorded, exactly once, and the run continued.
        self.assertEqual(len(manifest["failed_tasks"]), 1)
        self.assertEqual(manifest["failed_tasks"][0]["pr_number"], 1)
        # Surviving task still finalized into a PR result (run did not abort).
        mock_assemble.assert_called_once()
        # Parallel-cost manifest fields are written.
        self.assertEqual(manifest["expected_tasks"], 2)
        self.assertEqual(manifest["max_workers"], 1)
        self.assertIn("total_wallclock_seconds", manifest)

    def test_phase_c_merges_counters_and_groups_results_by_pr(self):
        """Reviewer #6 (c)+(d): across 2 PRs, Phase C merges pilot_counters from each
        task's counter_increment AND groups run-results by pr_number so finalize_pr_runs
        receives only its own PR's runs. Mixes ok + failed(counter) + a hard crash.
        """
        # PR1: run1 ok, run2 failed-with-counter. PR2: run1 ok, run2 hard-crash.
        def _ok(pr, ri):
            return {"pr_number": pr, "run_index": ri, "status": "ok",
                    "counter_increment": None, "duration_seconds": 1.0,
                    "resolved": {}, "run_record": {}, "canary_outcomes": []}

        def _dispatch(task):
            pr, ri = task["pr_number"], task["run_index"]
            if pr == 1 and ri == 2:  # failed with a methodology-rejection counter
                return {"pr_number": 1, "run_index": 2, "status": "failed",
                        "counter_increment": "confabulation_rejections",
                        "duration_seconds": 0.5, "resolved": None,
                        "run_record": None, "canary_outcomes": [], "error": "ConfabulationDetected: x"}
            if pr == 2 and ri == 2:  # hard crash inside the pool
                raise RuntimeError("pr2 run2 hard-crashed")
            return _ok(pr, ri)

        finalize_calls = []

        def _cap_finalize(pr_number, run_results, assignments, run_dir, *, canary_injections=None):
            finalize_calls.append((pr_number, [r["pr_number"] for r in run_results]))
            return _make_scored()

        with tempfile.TemporaryDirectory() as d:
            ctx1 = {"pr_number": 1, "run_dir": os.path.join(d, "pr1"),
                    "tasks": [{"pr_number": 1, "run_index": 1, "port": 8800},
                              {"pr_number": 1, "run_index": 2, "port": 8801}],
                    "assignments": [("skwad", "claude_ci"), ("claude_ci", "skwad")]}
            ctx2 = {"pr_number": 2, "run_dir": os.path.join(d, "pr2"),
                    "tasks": [{"pr_number": 2, "run_index": 1, "port": 8810},
                              {"pr_number": 2, "run_index": 2, "port": 8811}],
                    "assignments": [("skwad", "claude_ci"), ("claude_ci", "skwad")]}
            pr_file = os.path.join(d, "prs.json")
            with open(pr_file, "w") as f:
                json.dump([{"repo_ssh": "git@github.com:Kochava/repo.git", "prs": [1, 2]}], f)
            argv = ["prog", "--pr-file", pr_file, "--output-dir", d, "--max-workers", "2"]
            with ExitStack() as stack:
                stack.enter_context(patch("sys.argv", argv))
                stack.enter_context(patch("_eval_main.clone_repo_ssh", return_value="/tmp/repo"))
                stack.enter_context(patch("_eval_main.fetch_pr", return_value=_make_pr_data()))
                stack.enter_context(patch("_eval_main.prepare_pr_checkout", return_value="deadbeef"))
                stack.enter_context(patch("_eval_main.prepare_pr", side_effect=[ctx1, ctx2]))
                stack.enter_context(patch("_eval_main.run_single_judge_task", side_effect=_dispatch))
                stack.enter_context(patch("_eval_main.finalize_pr_runs", side_effect=_cap_finalize))
                stack.enter_context(patch("_eval_main.assemble_pr_result",
                                          return_value={"pr": 1, "skwad": {}, "claude_ci": {}}))
                stack.enter_context(patch("_eval_main.generate_research_report"))
                _main.main()
            with open(os.path.join(d, "manifest.json")) as f:
                manifest = json.load(f)

        # (c) counter merged single-threaded from the one failed-with-counter task.
        self.assertEqual(manifest["confabulation_rejections"], 1)
        # both the failed return and the hard crash are recorded.
        self.assertEqual(len(manifest["failed_tasks"]), 2)
        self.assertEqual(manifest["expected_tasks"], 4)
        # (d) results grouped by pr_number — each finalize sees ONLY its PR's runs.
        self.assertEqual(len(finalize_calls), 2)
        for pr_number, run_prs in finalize_calls:
            self.assertTrue(all(p == pr_number for p in run_prs),
                            f"PR#{pr_number} finalize received cross-PR runs: {run_prs}")
        self.assertEqual({c[0] for c in finalize_calls}, {1, 2})

    def test_g4_out_of_worktree_reads_aggregated_to_manifest(self):
        # C4/G4 WIRING: a per-run result that surfaced out_of_worktree_reads at top
        # level must be aggregated into manifest["out_of_worktree_reads"] (main.py
        # Phase-C). Integration glue the unit tests don't cover together.
        oow_res = {
            "pr_number": 1, "run_index": 1, "status": "ok",
            "counter_increment": None, "duration_seconds": 1.0,
            "resolved": {}, "run_record": {}, "canary_outcomes": [],
            "out_of_worktree_reads": ["/etc/hosts"],
        }
        with tempfile.TemporaryDirectory() as d:
            context = {
                "pr_number": 1, "repo": "Kochava/repo",
                "run_dir": os.path.join(d, "Kochava-repo-1"),
                "tasks": [{"pr_number": 1, "run_index": 1}],
                "assignments": [("skwad", "claude_ci")],
            }
            pr_file = _build_pr_file(d)
            argv = ["prog", "--pr-file", pr_file, "--output-dir", d, "--max-workers", "1"]
            with ExitStack() as stack:
                stack.enter_context(patch("sys.argv", argv))
                stack.enter_context(patch("_eval_main.clone_repo_ssh", return_value="/tmp/repo"))
                stack.enter_context(patch("_eval_main.fetch_pr", return_value=_make_pr_data()))
                stack.enter_context(patch("_eval_main.prepare_pr_checkout", return_value="deadbeef"))
                stack.enter_context(patch("_eval_main.prepare_pr", return_value=context))
                stack.enter_context(patch("_eval_main.run_single_judge_task", return_value=oow_res))
                stack.enter_context(patch("_eval_main.finalize_pr_runs", return_value=_make_scored()))
                stack.enter_context(patch("_eval_main.assemble_pr_result",
                                          return_value={"pr": 1, "skwad": {}, "claude_ci": {}}))
                stack.enter_context(patch("_eval_main.generate_research_report"))
                _main.main()
            with open(os.path.join(d, "manifest.json")) as f:
                manifest = json.load(f)
        self.assertTrue(manifest["out_of_worktree_reads"],
                        "G4 out-of-worktree reads must aggregate into the manifest")
        entry = manifest["out_of_worktree_reads"][0]
        self.assertEqual(entry["pr"], 1)
        self.assertEqual(entry["reads"], ["/etc/hosts"])


class TestDeletedForkCheckoutSkip(unittest.TestCase):
    """Phase 3 / M2 companion: prepare_pr_checkout → None (deleted fork / missing
    head) must make the orchestrator skip+record the PR (main.py:426-433) and never
    reach prepare_pr / scoring — the run completes, it does not crash."""

    def test_none_checkout_skips_and_records_pr(self):
        with tempfile.TemporaryDirectory() as d:
            pr_file = _build_pr_file(d)
            argv = ["prog", "--pr-file", pr_file, "--output-dir", d]
            with ExitStack() as stack:
                stack.enter_context(patch("sys.argv", argv))
                stack.enter_context(patch("_eval_main.clone_repo_ssh", return_value="/tmp/repo"))
                stack.enter_context(patch("_eval_main.fetch_pr", return_value=_make_pr_data()))
                # Deleted-fork head → None → the skip+record branch.
                stack.enter_context(patch("_eval_main.prepare_pr_checkout", return_value=None))
                mock_prep = stack.enter_context(patch("_eval_main.prepare_pr"))
                mock_skip = stack.enter_context(patch("_eval_main._manifest.record_skipped_pr"))
                stack.enter_context(patch("_eval_main.generate_research_report"))
                _main.main()  # must NOT raise
            # Recorded as skipped, for the right PR, with a head/checkout reason...
            self.assertEqual(mock_skip.call_count, 1)
            args = mock_skip.call_args.args
            self.assertEqual(args[2], 1, "skipped record must be for PR #1")
            self.assertIn("head", args[3].lower())
            # ...and never prepared/scored → the PR is absent from flat_tasks.
            mock_prep.assert_not_called()


if __name__ == "__main__":
    unittest.main()
