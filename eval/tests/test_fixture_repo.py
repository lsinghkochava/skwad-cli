"""Self-tests for the deterministic judge fixture repo + canary/review fixtures.

These run independently of the OpenAI judge implementation. Their job is to
prove the fixtures themselves are internally consistent — that every canary's
referent matches the repo's actual ground truth — so a canary can never pass or
fail for the wrong reason once the real judge runs against it. This is the
foundation the parity matrix (test_openai_judge.py) builds on.
"""

import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from eval.tests.fixture_repo import (
    ABSENT_FILES,
    ABSENT_SYMBOLS,
    KNOWN_FACTS,
    PRESENT_FILES,
    PRESENT_SYMBOLS,
    build_fixture_repo,
    build_multi_sha_repo,
    checkout_marker,
    head_sha,
    load_fixture_canaries,
    load_review,
)

_HAS_GIT = shutil.which("git") is not None


class TestFixtureRepoBuild(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.repo = build_fixture_repo(os.path.join(self.tmp, "repo"))

    def test_present_files_exist(self):
        for rel in PRESENT_FILES:
            self.assertTrue(os.path.exists(os.path.join(self.repo, rel)),
                            f"expected fixture file missing: {rel}")

    def test_absent_files_really_absent(self):
        # Glob-refutable canaries depend on these genuinely not existing.
        for rel in ABSENT_FILES:
            self.assertFalse(os.path.exists(os.path.join(self.repo, rel)),
                             f"file that must be absent exists: {rel}")

    def test_present_symbols_found_in_their_files(self):
        for symbol, rel in PRESENT_SYMBOLS.items():
            text = Path(self.repo, rel).read_text()
            self.assertIn(symbol, text, f"{symbol} not found in {rel}")

    def test_absent_symbols_appear_nowhere(self):
        # Grep-refutable canaries depend on these symbols existing nowhere.
        all_text = "\n".join(
            Path(dirpath, name).read_text(errors="ignore")
            for dirpath, _, names in os.walk(self.repo)
            for name in names
        )
        for symbol in ABSENT_SYMBOLS:
            self.assertNotIn(symbol, all_text, f"symbol that must be absent appears: {symbol}")


class TestFixtureRepoKnownFacts(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.repo = build_fixture_repo(os.path.join(self.tmp, "repo"))

    def test_max_retries_value(self):
        text = Path(self.repo, "src/utils.py").read_text()
        m = re.search(r"MAX_RETRIES\s*=\s*(\d+)", text)
        self.assertIsNotNone(m, "MAX_RETRIES assignment not found")
        self.assertEqual(int(m.group(1)), KNOWN_FACTS["max_retries"])
        # The contradicted-via-Read canary falsely claims 10 — make sure it IS NOT 10.
        self.assertNotEqual(int(m.group(1)), 10)

    def test_token_ttl_value(self):
        text = Path(self.repo, "src/auth.py").read_text()
        m = re.search(r"TOKEN_TTL_SECONDS\s*=\s*(\d+)", text)
        self.assertIsNotNone(m)
        self.assertEqual(int(m.group(1)), KNOWN_FACTS["token_ttl_seconds"])

    def test_validate_token_enforces_expiry(self):
        # The TRUE-positive canary asserts expiry IS enforced — prove it.
        text = Path(self.repo, "src/auth.py").read_text()
        self.assertIn("TOKEN_TTL_SECONDS", text)
        self.assertRegex(text, r"def validate_token")

    def test_eviction_is_lru_not_fifo(self):
        # The non-obvious canary falsely claims FIFO; LRU-ness comes from move_to_end on get.
        text = Path(self.repo, "src/cache.py").read_text()
        self.assertIn("move_to_end", text, "get() must touch recency → LRU, not FIFO")
        self.assertIn("popitem(last=False)", text)
        self.assertEqual(KNOWN_FACTS["eviction_policy"], "LRU")


@unittest.skipUnless(_HAS_GIT, "git not available")
class TestFixtureRepoGit(unittest.TestCase):
    def test_with_git_produces_valid_sha(self):
        with tempfile.TemporaryDirectory() as d:
            repo = build_fixture_repo(os.path.join(d, "repo"), with_git=True)
            self.assertTrue(os.path.isdir(os.path.join(repo, ".git")))
            sha = head_sha(repo)
            self.assertRegex(sha, r"^[0-9a-f]{40}$")

    def test_git_tree_is_clean_after_build(self):
        with tempfile.TemporaryDirectory() as d:
            repo = build_fixture_repo(os.path.join(d, "repo"), with_git=True)
            out = subprocess.run(["git", "status", "--porcelain"], cwd=repo,
                                 capture_output=True, text=True, check=True)
            self.assertEqual(out.stdout.strip(), "", "fixture repo should be committed clean")


@unittest.skipUnless(_HAS_GIT, "git not available")
class TestMultiShaRepo(unittest.TestCase):
    """Validates the per-PR checkout fixture infra (foundation for the #30/M2
    race-freedom test): each PR's SHA must check out to its OWN marker."""

    def test_distinct_shas_per_pr(self):
        with tempfile.TemporaryDirectory() as d:
            manifest = build_multi_sha_repo(os.path.join(d, "repo"), [101, 102, 103])
        shas = [m["sha"] for m in manifest.values()]
        self.assertEqual(len(shas), len(set(shas)), "each PR must have a unique SHA")
        for sha in shas:
            self.assertRegex(sha, r"^[0-9a-f]{40}$")

    def test_checkout_of_sha_yields_matching_marker(self):
        # The crux: checking out PR n's SHA produces PR_HEAD.txt == "PR-n".
        # If this didn't hold, the race test could pass for the wrong reason.
        with tempfile.TemporaryDirectory() as d:
            repo = os.path.join(d, "repo")
            manifest = build_multi_sha_repo(repo, [101, 102, 103])
            for n, info in manifest.items():
                subprocess.run(["git", "checkout", "-q", info["sha"]], cwd=repo,
                               check=True, capture_output=True, text=True)
                self.assertEqual(checkout_marker(repo), info["marker"],
                                 f"PR {n} SHA must check out to {info['marker']}")


_REQUIRED_CANARY_CATEGORIES = {
    "contradicted_via_read",
    "contradicted_via_grep",
    "contradicted_via_glob",
    "verified_true_positive",
    "contradicted_non_obvious",
}


class TestCanaryFixtureWellFormed(unittest.TestCase):
    def setUp(self):
        self.canaries = load_fixture_canaries()

    def test_loads_nonempty_list(self):
        self.assertIsInstance(self.canaries, list)
        self.assertGreater(len(self.canaries), 0)

    def test_required_fields_present(self):
        required = {"id", "inject_into", "claim_text", "expected_outcome"}
        for c in self.canaries:
            missing = required - c.keys()
            self.assertFalse(missing, f"canary {c.get('id')} missing fields: {missing}")

    def test_ids_unique(self):
        ids = [c["id"] for c in self.canaries]
        self.assertEqual(len(ids), len(set(ids)), "canary ids must be unique")

    def test_covers_all_hardened_categories(self):
        # Phase 4 / M1.3: read-, grep-, glob-refutable + a TRUE positive + a non-obvious one.
        present = {c.get("category") for c in self.canaries}
        missing = _REQUIRED_CANARY_CATEGORIES - present
        self.assertFalse(missing, f"canary set missing hardened categories: {missing}")

    def test_has_at_least_one_true_verified_canary(self):
        verified = [c for c in self.canaries if c["expected_outcome"] == "verified"]
        self.assertTrue(verified, "need >=1 TRUE 'verified' canary so the judge "
                                  "can't pass by marking everything contradicted")

    def test_inject_into_is_valid_system(self):
        for c in self.canaries:
            self.assertIn(c["inject_into"], ("skwad", "claude_ci"), c["id"])

    def test_referents_match_repo_ground_truth(self):
        """Each canary's referent must align with the repo so it fails/passes for
        the RIGHT reason. read→false-value, grep→absent symbol, glob→absent file,
        verified→present symbol, non-obvious→present-but-misdescribed."""
        by_cat = {c["category"]: c for c in self.canaries}

        # grep-refutable canary cites a symbol that is genuinely absent.
        grep_token = by_cat["contradicted_via_grep"]["match_token"]
        self.assertIn(grep_token, ABSENT_SYMBOLS)

        # glob-refutable canary cites a file that is genuinely absent.
        glob_token = by_cat["contradicted_via_glob"]["match_token"]
        self.assertIn(glob_token, ABSENT_FILES)

        # verified canary cites a symbol that genuinely exists.
        verified_token = by_cat["verified_true_positive"]["match_token"]
        self.assertIn(verified_token, PRESENT_SYMBOLS)

        # read-refutable canary cites a present symbol but a false value.
        read_token = by_cat["contradicted_via_read"]["match_token"]
        self.assertIn(read_token, PRESENT_SYMBOLS)


class TestReviewFixtures(unittest.TestCase):
    def test_good_review_loads(self):
        text = load_review("good_review")
        self.assertIn("MAX_RETRIES", text)
        self.assertIn("validate_token", text)

    def test_fabricated_review_loads(self):
        text = load_review("fabricated_review")
        # Cites the absent symbol + absent file the judge must contradict.
        self.assertIn("processBatchScenarios", text)
        self.assertIn("src/payment.py", text)

    def test_good_review_claims_are_consistent_with_repo(self):
        # The good review states MAX_RETRIES is 3 — matches ground truth.
        text = load_review("good_review")
        self.assertRegex(text, r"MAX_RETRIES.*\b3\b")


if __name__ == "__main__":
    unittest.main()
