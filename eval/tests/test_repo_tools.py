"""Tests for the sandboxed Read/Grep/Glob tools (eval.lib.repo_tools, #31/#9).

The sandbox is the security spine of Route B: the judge can only touch the
per-PR checkout. These tests prove every escape vector is refused, the happy
paths work on the deterministic fixture repo, and the OpenAI dispatcher rejects
disallowed tools + surfaces tool errors as text (never crashes the loop).
"""

import os
import shutil
import tempfile
import unittest

from eval.lib.repo_tools import (
    ALLOWED_TOOLS,
    TOOL_SCHEMAS,
    PathEscapeError,
    RepoTools,
    dispatch_tool_call,
)
from eval.tests.fixture_repo import build_fixture_repo, tool_is_empty, tool_text

_HAS_SYMLINK = hasattr(os, "symlink")


def _joined(rows) -> str:
    """grep/glob may return str or list[str]; normalize for substring assertions."""
    return tool_text(rows)


# ---------------------------------------------------------------------------
# Sandbox escape rejection (#31)
# ---------------------------------------------------------------------------

class TestSandboxEscapes(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.repo = build_fixture_repo(os.path.join(self.tmp, "repo"))
        self.sandbox = RepoTools(self.repo)
        # A secret file OUTSIDE the sandbox root, for symlink-escape tests.
        self.outside = os.path.join(self.tmp, "outside")
        os.makedirs(self.outside, exist_ok=True)
        with open(os.path.join(self.outside, "secret.txt"), "w") as f:
            f.write("TOP SECRET")

    def test_dotdot_traversal_rejected(self):
        with self.assertRaises(PathEscapeError):
            self.sandbox.read("../../../../etc/passwd")

    def test_absolute_path_outside_rejected(self):
        with self.assertRaises(PathEscapeError):
            self.sandbox.read(os.path.join(self.outside, "secret.txt"))

    def test_embedded_dotdot_escape_rejected(self):
        # internal/../../passwd climbs out of the root even with a leading subdir.
        os.makedirs(os.path.join(self.repo, "internal"), exist_ok=True)
        with self.assertRaises(PathEscapeError):
            self.sandbox.read("internal/../../secret.txt")

    @unittest.skipUnless(_HAS_SYMLINK, "platform lacks symlink support")
    def test_symlink_escape_rejected(self):
        # A symlink inside the repo pointing OUT must not be followed past root.
        link = os.path.join(self.repo, "escape")
        os.symlink(self.outside, link)
        with self.assertRaises(PathEscapeError):
            self.sandbox.read("escape/secret.txt")

    @unittest.skipUnless(_HAS_SYMLINK, "platform lacks symlink support")
    def test_grep_does_not_follow_symlink_out_of_tree(self):
        os.symlink(self.outside, os.path.join(self.repo, "escape"))
        # The out-of-tree secret must never surface in grep results.
        self.assertNotIn("TOP SECRET", _joined(self.sandbox.grep("TOP SECRET")))

    def test_empty_path_rejected(self):
        with self.assertRaises(PathEscapeError):
            self.sandbox.read("")

    def test_root_must_be_directory(self):
        with self.assertRaises(NotADirectoryError):
            RepoTools(os.path.join(self.repo, "src/utils.py"))


# ---------------------------------------------------------------------------
# Happy-path Read/Grep/Glob on the fixture repo
# ---------------------------------------------------------------------------

class TestSandboxHappyPath(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.repo = build_fixture_repo(os.path.join(self.tmp, "repo"))
        self.sandbox = RepoTools(self.repo)

    def test_read_returns_file_contents(self):
        text = self.sandbox.read("src/utils.py")
        self.assertIn("MAX_RETRIES = 3", text)

    def test_read_offset_and_limit(self):
        # Lines 0..2 only — should not contain the later parse_config def.
        head = self.sandbox.read("src/utils.py", offset=0, limit=3)
        self.assertNotIn("def parse_config", head)

    def test_read_missing_file_raises(self):
        with self.assertRaises(FileNotFoundError):
            self.sandbox.read("src/payment.py")  # absent file, but inside root

    def test_grep_finds_present_symbol(self):
        out = self.sandbox.grep("MAX_RETRIES")
        self.assertRegex(_joined(out), r"src/utils\.py:\d+:")  # relpath:lineno:line shape

    def test_grep_absent_symbol_is_empty(self):
        self.assertTrue(tool_is_empty(self.sandbox.grep("processBatchScenarios")))

    def test_grep_glob_filter(self):
        # Restrict to *.py — README mention (none) excluded; code hits included.
        out = self.sandbox.grep("def ", glob="*.py")
        self.assertIn("src/auth.py", _joined(out))

    def test_grep_invalid_regex_raises_valueerror(self):
        with self.assertRaises(ValueError):
            self.sandbox.grep("(unclosed")

    def test_glob_finds_present_files(self):
        out = _joined(self.sandbox.glob("src/*.py"))
        self.assertIn("src/auth.py", out)
        self.assertIn("src/cache.py", out)

    def test_glob_absent_file_is_empty(self):
        self.assertTrue(tool_is_empty(self.sandbox.glob("src/payment.py")))

    def test_glob_recursive_doublestar(self):
        self.assertIn("src/utils.py", _joined(self.sandbox.glob("**/*.py")))

    def test_glob_skips_git_dir(self):
        # Build with git, then ensure .git internals never leak into glob output.
        repo = build_fixture_repo(os.path.join(self.tmp, "gitrepo"), with_git=True)
        lines = [ln for ln in _joined(RepoTools(repo).glob("**/*")).splitlines()]
        self.assertFalse([p for p in lines if p.startswith(".git/")],
                         f".git internals leaked into glob output: {lines}")


# ---------------------------------------------------------------------------
# OpenAI tool dispatch (#9 disallowed-tool enforcement + error surfacing)
# ---------------------------------------------------------------------------

class TestDispatchToolCall(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.repo = build_fixture_repo(os.path.join(self.tmp, "repo"))
        self.sandbox = RepoTools(self.repo)

    def test_read_dispatch_returns_contents(self):
        out = dispatch_tool_call(self.sandbox, "Read", {"path": "src/utils.py"})
        self.assertIn("MAX_RETRIES = 3", out)

    def test_grep_dispatch_returns_rows(self):
        out = dispatch_tool_call(self.sandbox, "Grep", {"pattern": "LRUCache"})
        self.assertIn("src/cache.py", out)

    def test_glob_dispatch_returns_paths(self):
        out = dispatch_tool_call(self.sandbox, "Glob", {"pattern": "src/*.py"})
        self.assertIn("src/auth.py", out)

    def test_disallowed_tool_rejected_as_error_string(self):
        # #9: Bash is not offered → dispatch returns an ERROR string, no raise.
        out = dispatch_tool_call(self.sandbox, "Bash", {"command": "rm -rf /"})
        self.assertTrue(out.startswith("ERROR:"))
        self.assertIn("Bash", out)

    def test_path_escape_surfaced_as_error_not_raised(self):
        # An errored Read is legitimate verification (M4) → returned as text.
        out = dispatch_tool_call(self.sandbox, "Read", {"path": "../../etc/passwd"})
        self.assertTrue(out.startswith("ERROR:"))
        self.assertIn("PathEscapeError", out)

    def test_missing_required_arg_surfaced_as_error(self):
        out = dispatch_tool_call(self.sandbox, "Read", {})  # no 'path'
        self.assertTrue(out.startswith("ERROR:"))


# ---------------------------------------------------------------------------
# Tool schemas exposed to the model
# ---------------------------------------------------------------------------

class TestToolSchemas(unittest.TestCase):
    def test_exactly_read_grep_glob_exposed(self):
        names = {s["function"]["name"] for s in TOOL_SCHEMAS}
        self.assertEqual(names, {"Read", "Grep", "Glob"})

    def test_schema_names_match_allowed_tools(self):
        names = {s["function"]["name"] for s in TOOL_SCHEMAS}
        self.assertEqual(names, ALLOWED_TOOLS)

    def test_each_schema_is_a_function_with_params(self):
        for s in TOOL_SCHEMAS:
            self.assertEqual(s["type"], "function")
            self.assertIn("parameters", s["function"])
            self.assertIn("required", s["function"]["parameters"])


if __name__ == "__main__":
    unittest.main()
