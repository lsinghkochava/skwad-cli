"""Tests for eval.lib.manifest."""

import json
import os
import re
import tempfile
import unittest
from unittest.mock import patch

from eval.lib.manifest import (
    hash_prompt_file,
    open_manifest,
    record_models,
    record_pr,
    record_skipped_pr,
    record_prompt_hash,
    write_manifest,
)

_ISO8601_Z_RE = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$")
_HEX40_RE = re.compile(r"^[0-9a-f]{40}$")


class TestOpenManifest(unittest.TestCase):
    def setUp(self):
        self.m = open_manifest("/tmp/manifest.json", seed=99)

    def test_all_top_level_keys_present(self):
        expected = {
            "run_id", "started_at_utc", "completed_at_utc", "skwad_cli_git_sha",
            "models", "rng_seed", "prompt_hashes", "python_versions", "os_info",
            "prs", "skipped_prs",
        }
        self.assertTrue(expected.issubset(self.m.keys()))

    def test_rng_seed_matches_argument(self):
        self.assertEqual(self.m["rng_seed"], 99)

    def test_prs_and_skipped_prs_empty_at_init(self):
        self.assertEqual(self.m["prs"], [])
        self.assertEqual(self.m["skipped_prs"], [])

    def test_g4_out_of_worktree_keys_init(self):
        # C4/G4: a top-level list of out-of-worktree-read incidents + a quarantine
        # counter, both initialized empty/zero.
        self.assertEqual(self.m["out_of_worktree_reads"], [])
        self.assertEqual(self.m["out_of_worktree_read_quarantines"], 0)

    def test_models_is_dict_at_init(self):
        self.assertIsInstance(self.m["models"], dict)

    def test_prompt_hashes_is_dict(self):
        self.assertIsInstance(self.m["prompt_hashes"], dict)

    def test_python_versions_keys(self):
        pv = self.m["python_versions"]
        for key in ("python", "scipy", "numpy", "krippendorff"):
            self.assertIn(key, pv)

    def test_os_info_keys(self):
        self.assertIn("uname", self.m["os_info"])
        self.assertIn("hostname", self.m["os_info"])

    def test_run_id_is_valid_uuid(self):
        import uuid
        # Should not raise.
        uuid.UUID(self.m["run_id"])

    def test_started_at_utc_iso8601_z(self):
        self.assertRegex(self.m["started_at_utc"], _ISO8601_Z_RE)

    def test_git_sha_explicit_value(self):
        m = open_manifest("/tmp/x.json", seed=0, skwad_cli_git_sha="abc123")
        self.assertEqual(m["skwad_cli_git_sha"], "abc123")

    def test_git_sha_auto_detected_format(self):
        # Auto-detected SHA is either 40 hex chars (full SHA) or "unknown".
        sha = self.m["skwad_cli_git_sha"]
        valid = (_HEX40_RE.match(sha) is not None) or sha == "unknown"
        self.assertTrue(valid, f"Unexpected git SHA format: {sha!r}")

    def test_git_sha_unknown_fallback(self):
        failed = unittest.mock.MagicMock()
        failed.returncode = 1
        failed.stdout = ""
        with patch("eval.lib.manifest.subprocess.run", return_value=failed):
            m = open_manifest("/tmp/x.json", seed=0)
        self.assertEqual(m["skwad_cli_git_sha"], "unknown")


class TestRecordOutOfWorktreeReads(unittest.TestCase):
    def _m(self):
        return open_manifest("/tmp/x.json", seed=0)

    def test_records_entry_with_sorted_deduped_reads(self):
        from eval.lib.manifest import record_out_of_worktree_reads
        m = self._m()
        record_out_of_worktree_reads(m, "Kochava/repo", 7, ["/etc/shadow", "/etc/hosts", "/etc/hosts"])
        self.assertEqual(len(m["out_of_worktree_reads"]), 1)
        entry = m["out_of_worktree_reads"][0]
        self.assertEqual(entry["repo"], "Kochava/repo")
        self.assertEqual(entry["pr"], 7)
        self.assertEqual(entry["reads"], ["/etc/hosts", "/etc/shadow"])  # sorted + deduped

    def test_empty_reads_is_noop(self):
        from eval.lib.manifest import record_out_of_worktree_reads
        m = self._m()
        record_out_of_worktree_reads(m, "Kochava/repo", 7, [])
        self.assertEqual(m["out_of_worktree_reads"], [])


class TestWriteManifest(unittest.TestCase):
    def _write_and_reload(self, manifest, path):
        write_manifest(manifest, path)
        with open(path) as f:
            return json.load(f)

    def test_all_public_keys_preserved_on_write(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "manifest.json")
            m = open_manifest(path, seed=7)
            data = self._write_and_reload(m, path)
        for key in ("run_id", "prs", "skipped_prs", "rng_seed", "models"):
            self.assertIn(key, data)

    def test_internal_keys_stripped(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "manifest.json")
            m = open_manifest(path, seed=7)
            data = self._write_and_reload(m, path)
        for key in data:
            self.assertFalse(key.startswith("_"), f"Internal key {key!r} leaked into output")

    def test_completed_at_utc_set_on_write(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "manifest.json")
            m = open_manifest(path, seed=7)
            self.assertIsNone(m["completed_at_utc"])
            data = self._write_and_reload(m, path)
        self.assertIsNotNone(data["completed_at_utc"])
        self.assertRegex(data["completed_at_utc"], _ISO8601_Z_RE)

    def test_json_is_pretty_printed(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "manifest.json")
            m = open_manifest(path, seed=7)
            write_manifest(m, path)
            size = os.path.getsize(path)
        self.assertGreater(size, 200)

    def test_existing_completed_at_utc_not_overwritten(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "manifest.json")
            m = open_manifest(path, seed=7)
            m["completed_at_utc"] = "2025-01-01T00:00:00Z"
            data = self._write_and_reload(m, path)
        self.assertEqual(data["completed_at_utc"], "2025-01-01T00:00:00Z")


class TestHashPromptFile(unittest.TestCase):
    def _write_fixture(self, tmpdir, name, content):
        path = os.path.join(tmpdir, name)
        with open(path, "wb") as f:
            f.write(content)
        return path

    def test_deterministic(self):
        with tempfile.TemporaryDirectory() as d:
            p = self._write_fixture(d, "f.txt", b"hello world")
            h1 = hash_prompt_file(p)
            h2 = hash_prompt_file(p)
        self.assertEqual(h1, h2)

    def test_64_char_lowercase_hex(self):
        with tempfile.TemporaryDirectory() as d:
            p = self._write_fixture(d, "f.txt", b"test content")
            h = hash_prompt_file(p)
        self.assertEqual(len(h), 64)
        self.assertEqual(h, h.lower())
        self.assertTrue(all(c in "0123456789abcdef" for c in h))

    def test_different_content_different_hash(self):
        with tempfile.TemporaryDirectory() as d:
            p1 = self._write_fixture(d, "a.txt", b"content A")
            p2 = self._write_fixture(d, "b.txt", b"content B")
            self.assertNotEqual(hash_prompt_file(p1), hash_prompt_file(p2))

    def test_file_not_found_raises(self):
        with self.assertRaises(Exception):
            hash_prompt_file("/nonexistent/path/file.txt")


class TestRecordPr(unittest.TestCase):
    def _manifest(self):
        return open_manifest("/tmp/x.json", seed=0)

    def test_single_pr_appended_with_correct_fields(self):
        m = self._manifest()
        record_pr(m, "owner/repo", 42, "abc123" * 7, "easy")
        self.assertEqual(len(m["prs"]), 1)
        pr = m["prs"][0]
        self.assertEqual(pr["repo"], "owner/repo")
        self.assertEqual(pr["pr"], 42)
        self.assertEqual(pr["commit_sha"], "abc123" * 7)
        self.assertEqual(pr["difficulty"], "easy")

    def test_multiple_prs_accumulate_in_order(self):
        m = self._manifest()
        record_pr(m, "r", 1, "sha1", "easy")
        record_pr(m, "r", 2, "sha2", "medium")
        record_pr(m, "r", 3, "sha3", "hard")
        self.assertEqual(len(m["prs"]), 3)
        self.assertEqual(m["prs"][0]["pr"], 1)
        self.assertEqual(m["prs"][2]["pr"], 3)

    def test_prs_independent_from_skipped_prs(self):
        m = self._manifest()
        record_pr(m, "r", 1, "sha", "easy")
        record_skipped_pr(m, "r", 99, "no diff")
        self.assertEqual(len(m["prs"]), 1)
        self.assertEqual(len(m["skipped_prs"]), 1)


class TestRecordSkippedPr(unittest.TestCase):
    def _manifest(self):
        return open_manifest("/tmp/x.json", seed=0)

    def test_skipped_pr_appended_with_correct_fields(self):
        m = self._manifest()
        record_skipped_pr(m, "org/repo", 7, "no diff available")
        self.assertEqual(len(m["skipped_prs"]), 1)
        s = m["skipped_prs"][0]
        self.assertEqual(s["repo"], "org/repo")
        self.assertEqual(s["pr"], 7)
        self.assertEqual(s["reason"], "no diff available")

    def test_multiple_skipped_accumulate(self):
        m = self._manifest()
        record_skipped_pr(m, "r", 1, "reason A")
        record_skipped_pr(m, "r", 2, "reason B")
        self.assertEqual(len(m["skipped_prs"]), 2)
        self.assertEqual(m["skipped_prs"][1]["reason"], "reason B")


class TestRecordModels(unittest.TestCase):
    def _manifest(self):
        return open_manifest("/tmp/x.json", seed=0)

    def test_all_four_model_keys_set(self):
        m = self._manifest()
        record_models(
            m,
            skwad_review_agents="claude-opus-4",
            claude_ci="claude-sonnet-4",
            judge="claude-opus-4",
            difficulty_classifier="claude-haiku-4",
        )
        self.assertEqual(m["models"]["skwad_review_agents"], "claude-opus-4")
        self.assertEqual(m["models"]["claude_ci"], "claude-sonnet-4")
        self.assertEqual(m["models"]["judge"], "claude-opus-4")
        self.assertEqual(m["models"]["difficulty_classifier"], "claude-haiku-4")

    def test_calling_twice_overwrites(self):
        m = self._manifest()
        record_models(m, skwad_review_agents="v1", claude_ci="v1", judge="v1", difficulty_classifier="v1")
        record_models(m, skwad_review_agents="v2", claude_ci="v2", judge="v2", difficulty_classifier="v2")
        self.assertEqual(m["models"]["skwad_review_agents"], "v2")
        self.assertEqual(len(m["models"]), 4)

    def test_per_agent_omitted_when_not_provided(self):
        m = self._manifest()
        record_models(m, skwad_review_agents="x", claude_ci="x", judge="x", difficulty_classifier="x")
        self.assertNotIn("per_agent", m["models"])

    def test_per_agent_mapping_recorded_when_provided(self):
        m = self._manifest()
        per_agent = {
            "skwad_review_agents": {"Performance Analyst": "claude-sonnet-4-6",
                                    "Review Coordinator": "claude-haiku-4-5"},
            "judge": {"Judge": "claude-sonnet-4-6"},
            "difficulty_classifier": {"Difficulty Classifier": "claude-haiku-4-5"},
        }
        record_models(
            m,
            skwad_review_agents="claude-sonnet-4-6",
            claude_ci="claude-sonnet-4",
            judge="claude-sonnet-4-6",
            difficulty_classifier="claude-haiku-4-5",
            per_agent=per_agent,
        )
        self.assertEqual(m["models"]["per_agent"], per_agent)
        self.assertEqual(
            m["models"]["per_agent"]["skwad_review_agents"]["Review Coordinator"],
            "claude-haiku-4-5",
        )


class TestPromptHashesSchema(unittest.TestCase):
    def test_five_default_keys_in_prompt_hashes(self):
        m = open_manifest("/tmp/x.json", seed=0)
        expected = {
            "rubric_json_sha256",
            "judge_team_json_sha256",
            "classifier_team_json_sha256",
            "judge_persona_md_sha256",
            "classifier_persona_md_sha256",
        }
        self.assertEqual(set(m["prompt_hashes"].keys()), expected)

    def test_missing_prompt_file_value_starts_with_missing_prefix(self):
        with patch("eval.lib.manifest.hash_prompt_file", side_effect=OSError("not found")):
            m = open_manifest("/tmp/x.json", seed=0)
        for key, val in m["prompt_hashes"].items():
            self.assertTrue(val.startswith("MISSING:"), f"Key {key!r}: expected MISSING: prefix, got {val!r}")

    def test_present_prompt_file_value_is_64_char_hex(self):
        fake_hash = "a" * 64
        with patch("eval.lib.manifest.hash_prompt_file", return_value=fake_hash):
            m = open_manifest("/tmp/x.json", seed=0)
        for key, val in m["prompt_hashes"].items():
            self.assertEqual(val, fake_hash, f"Key {key!r}: expected fake hash, got {val!r}")

    def test_record_prompt_hash_adds_extra_key_with_valid_hash(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "extra.json")
            with open(path, "wb") as f:
                f.write(b'{"extra": true}')
            m = open_manifest("/tmp/x.json", seed=0)
            record_prompt_hash(m, "extra_sha256", path)
        self.assertIn("extra_sha256", m["prompt_hashes"])
        val = m["prompt_hashes"]["extra_sha256"]
        self.assertEqual(len(val), 64)
        self.assertEqual(val, val.lower())



# ---------------------------------------------------------------------------
# Section G: v2 fields in open_manifest
# ---------------------------------------------------------------------------

class TestManifestV2Fields(unittest.TestCase):
    def setUp(self):
        self.m = open_manifest("/tmp/v2_test.json", seed=42)

    def test_methodology_version_is_2(self):
        self.assertEqual(self.m["methodology_version"], 2)

    def test_pilot_pass_is_none_at_init(self):
        self.assertIsNone(self.m["pilot_pass"])

    def test_canary_outcomes_is_empty_list(self):
        self.assertEqual(self.m["canary_outcomes"], [])

    def test_confabulation_rejections_is_zero(self):
        self.assertEqual(self.m["confabulation_rejections"], 0)

    def test_disallowed_tool_rejections_is_zero(self):
        self.assertEqual(self.m["disallowed_tool_rejections"], 0)

    def test_inter_run_alpha_is_empty_dict(self):
        self.assertEqual(self.m["inter_run_alpha"], {})

    def test_structural_invalid_rejections_is_zero(self):
        # Reviewer Critical #2: pilot manifest must surface structural-invalid counter.
        self.assertEqual(self.m["structural_invalid_rejections"], 0)

    def test_v2_fields_preserved_on_write(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "m.json")
            m = open_manifest(path, seed=0)
            m["pilot_pass"] = True
            m["canary_outcomes"] = [{"id": "c1", "passed": True}]
            m["confabulation_rejections"] = 2
            m["inter_run_alpha"] = {"issue_detection": 0.75}
            write_manifest(m, path)
            with open(path) as f:
                data = json.load(f)
        self.assertTrue(data["pilot_pass"])
        self.assertEqual(data["confabulation_rejections"], 2)
        self.assertEqual(data["methodology_version"], 2)
        self.assertEqual(data["inter_run_alpha"]["issue_detection"], 0.75)


if __name__ == "__main__":
    unittest.main()
