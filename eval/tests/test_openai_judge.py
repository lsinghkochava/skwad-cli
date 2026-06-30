"""Parity matrix for the Python OpenAI (GPT-5.1) judge — Route B.

Plan: plans/openai-python-judge.md. This file holds ONE test (or cohesive group)
per retained parity-checklist row (#1–#32). The contract: every check the skwad
judge performed must still FIRE under the new in-process OpenAI tool loop.

Status mechanics
----------------
The judge module (`eval.lib.openai_judge`) is built phase-by-phase by the Coder.
Each test is gated with ``@needs(...)`` on the specific symbols it exercises:

  * Module absent            → every test skips with "blocked: ... not implemented".
  * Module present, symbol   → that test auto-activates and runs for real.
    absent

So today this file is GREEN-by-skip and documents the target behavior; as each
phase commits, the relevant rows light up with zero edits here. The live Codex
judge test requires RUN_LIVE_CODEX_JUDGE=1 (codex auth via ~/.codex/auth.json);
the frozen OpenAI-fallback client smoke separately requires OPENAI_API_KEY.

Expected module interface (coordinated with Coder — adjust names here if they
diverge; the skip message surfaces the mismatch):
  - Pure core (Phase 2, carried over from judge.py VERBATIM names):
      CRITERIA, _COUNT_CRITERIA, StructuralInvalidRun, ConfabulationDetected,
      _validate_response_structure, _sum_verified_in_output, _check_confabulation,
      _median_vote, _unswap, derive_ab_assignments, _truncate_diff,
      DIFF_TRUNCATION_CAP, _apply_canary_injections, _check_canary_outcomes,
      _aggregate_verification_summaries, finalize_pr_runs
  - Sandboxed tools (Phase 1): class RepoTools(root) with .read/.grep/.glob,
      rejecting reads outside `root`.
  - Verification loop (Phase 4): count_emitted_tool_calls(transcript) -> list,
      _check_evidence_binding(parsed, transcript) raising on fabricated evidence.
  - Checkout (Phase 3): fetch_pr_metadata exposing headRefOid; per-PR isolation.
"""

import os
import shutil
import tempfile
import unittest
from pathlib import Path

from eval.tests.fixture_repo import (
    PRESENT_SYMBOLS,
    add_pull_refs,
    build_fixture_repo,
    build_multi_sha_repo,
    checkout_marker,
    head_sha,
    load_fixture_canaries,
    load_review,
    tool_is_empty,
)

try:
    import eval.lib.openai_judge as oj  # noqa: F401
    _OJ = oj
    _IMPORT_ERR = None
except Exception as e:  # ImportError today; broad so a half-built module still skips cleanly
    _OJ = None
    _IMPORT_ERR = e

# repo_tools (Phase 1) landed in its OWN module; probe it independently so the
# sandbox parity row activates without waiting on openai_judge.
try:
    import eval.lib.repo_tools as rt  # noqa: F401
    _RT = rt
except Exception:
    _RT = None

try:
    import eval.lib.openai_client as oc  # noqa: F401
    _OC = oc
except Exception:
    _OC = None

try:
    import eval.lib.pr_fetcher as pf  # noqa: F401
    _PF = pf
except Exception:
    _PF = None

_HAS_GIT = shutil.which("git") is not None
_HAS_KEY = bool(os.environ.get("OPENAI_API_KEY"))  # OpenAI FALLBACK client smoke only
# Codex judge live gate: a single explicit opt-in flag. The post-swap judge is Codex
# (auth via ~/.codex/auth.json, NOT OPENAI_API_KEY — which is env-scrubbed anyway), so
# the OLD OPENAI_API_KEY requirement is dropped. Flag-only keeps a clean skip on plain
# pytest/CI (no accidental live spend); an unauthed run fails loudly with CodexExecError
# (clearer than a silent skip on a flag the user explicitly set).
_LIVE_CODEX = os.environ.get("RUN_LIVE_CODEX_JUDGE") == "1"


def needs(*symbols):
    """Skip a test unless openai_judge exists AND exposes every named symbol."""
    if _OJ is None:
        return unittest.skip(f"blocked: eval.lib.openai_judge not implemented yet "
                             f"(Coder Phase 2+); import error: {_IMPORT_ERR}")
    missing = [s for s in symbols if not hasattr(_OJ, s)]
    if missing:
        return unittest.skip(f"blocked: openai_judge missing {missing} (phase not landed)")
    return lambda fn: fn


def needs_rt():
    """Skip unless the Phase-1 repo_tools module is importable."""
    if _RT is None:
        return unittest.skip("blocked: eval.lib.repo_tools not importable")
    return lambda fn: fn


# ===========================================================================
# Shared builders (mirror test_judge.py conventions so behavior matches parity)
# ===========================================================================

_COUNT_CRITERIA_T = {"issue_detection", "coverage", "depth", "novel_substantive_findings"}


def _criteria_list():
    return getattr(_OJ, "CRITERIA", [
        "issue_detection", "actionability", "severity_accuracy", "coverage",
        "signal_to_noise", "depth", "novel_substantive_findings",
    ])


def _review(score, *, verified=0, claim_trace=None):
    criteria = {}
    for c in _criteria_list():
        entry = {"score": score, "reasoning": f"r_{c}"}
        if c in _COUNT_CRITERIA_T:
            entry.update(verified_findings=0, unverified_findings=0,
                         contradicted_findings=0, non_falsifiable_findings=0)
        if c == "issue_detection":
            entry["verified_findings"] = verified
        if c == "novel_substantive_findings":
            entry["justifications"] = []
        criteria[c] = entry
    return {"criteria": criteria, "total": score * len(_criteria_list()),
            "claim_trace": claim_trace if claim_trace is not None else []}


def _parsed(a_score=0, b_score=0, **kw):
    return {"review_a": _review(a_score, **kw), "review_b": _review(b_score)}


def _resolved(skwad_score, ci_score):
    return {"skwad": _review(skwad_score), "claude_ci": _review(ci_score)}


# ===========================================================================
# #31 / Phase 1 — Tool sandboxing (Read/Grep/Glob scoped to clone_path)
# ===========================================================================
# ACTIVE (Phase 1 landed). This is the parity ANCHOR — it proves the matrix row
# is satisfied against the real eval.lib.repo_tools.RepoTools. Exhaustive escape
# / dispatch / schema coverage lives in test_repo_tools.py.

class TestToolSandbox(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.repo = build_fixture_repo(os.path.join(self.tmp, "repo"))

    @needs_rt()
    def test_read_inside_repo_succeeds(self):
        self.assertIn("MAX_RETRIES", _RT.RepoTools(self.repo).read("src/utils.py"))

    @needs_rt()
    def test_read_path_traversal_rejected(self):
        # #31: ../.. escape must be refused, not silently served.
        with self.assertRaises(_RT.PathEscapeError):
            _RT.RepoTools(self.repo).read("../../../../etc/passwd")

    @needs_rt()
    def test_grep_present_vs_absent_symbol(self):
        tools = _RT.RepoTools(self.repo)
        self.assertTrue(tools.grep("MAX_RETRIES"))
        self.assertTrue(tool_is_empty(tools.grep("processBatchScenarios")))

    @needs_rt()
    def test_glob_present_vs_absent_file(self):
        tools = _RT.RepoTools(self.repo)
        self.assertTrue(tools.glob("src/*.py"))
        self.assertTrue(tool_is_empty(tools.glob("src/payment.py")))


# ===========================================================================
# #1 / #4 / Phase 4 — Forced verification loop + "verification actually ran" gate
# ===========================================================================

class TestVerificationLoop(unittest.TestCase):
    @needs("_check_confabulation", "ConfabulationDetected")
    def test_zero_tool_calls_with_claims_is_rejected(self):
        # #3/#4: claims present but no tool calls → confabulation / no-silent-score.
        parsed = _parsed(verified=3)
        with self.assertRaises(_OJ.ConfabulationDetected):
            _OJ._check_confabulation(parsed, [])

    @needs("_check_confabulation")
    def test_zero_claims_zero_tools_ok(self):
        _OJ._check_confabulation(_parsed(verified=0), [])  # no raise


# ===========================================================================
# #2 / M4 — Tool-call counting (EMITTED tool_calls[] entries, success OR error)
# ===========================================================================

class TestToolCallCounting(unittest.TestCase):
    def _transcript(self):
        # OpenAI-style assistant messages; one message may carry parallel tool_calls.
        return [
            {"role": "assistant", "tool_calls": [
                {"id": "1", "function": {"name": "read", "arguments": "{}"}},
                {"id": "2", "function": {"name": "grep", "arguments": "{}"}},
            ]},
            {"role": "tool", "tool_call_id": "1", "content": "..."},
            {"role": "assistant", "tool_calls": [
                {"id": "3", "function": {"name": "glob", "arguments": "{}"}},
            ]},
            {"role": "assistant", "content": "final verdict"},  # no tool_calls
        ]

    @needs("count_emitted_tool_calls")
    def test_counts_all_emitted_entries_across_messages(self):
        # M4: 3 emitted tool_calls across 2 assistant messages.
        calls = _OJ.count_emitted_tool_calls(self._transcript())
        self.assertEqual(len(calls), 3)

    @needs("count_emitted_tool_calls")
    def test_errored_tool_call_still_counted(self):
        # An errored Read is legitimate verification → still counts.
        transcript = [
            {"role": "assistant", "tool_calls": [
                {"id": "1", "function": {"name": "read", "arguments": "{}"}}]},
            {"role": "tool", "tool_call_id": "1", "content": "Error: file not found"},
        ]
        self.assertEqual(len(_OJ.count_emitted_tool_calls(transcript)), 1)

    @needs("count_emitted_tool_calls")
    def test_no_tool_calls_returns_empty(self):
        self.assertEqual(
            list(_OJ.count_emitted_tool_calls([{"role": "assistant", "content": "x"}])), [])


# ===========================================================================
# #3 — Confabulation rule: RECALIBRATED for GPT-5.1 batching (openai_judge only).
# Floor = max(1, ceil(verified / CONFAB_CLAIMS_PER_TOOL_CALL)) with the constant
# = 10 (was /5 for the Claude path; judge.py keeps /5). GPT-5.1 batches ~5
# verified/tool-call (live: 16 verified in 3 calls). Evidence-binding is now the
# strong per-claim gate; this floor is a coarse backstop for egregious over-claiming.
# ===========================================================================

class TestConfabulationRule(unittest.TestCase):
    @needs("_check_confabulation", "ConfabulationDetected")
    def test_5_claims_1_tool_meets_minimum(self):
        _OJ._check_confabulation(_parsed(verified=5), ["read"])  # ceil(5/10)=1, ok

    @needs("_check_confabulation", "ConfabulationDetected")
    def test_live_regression_16_verified_3_tools_passes(self):
        # The exact case that wrongly tripped ConfabulationDetected live under /5
        # (ceil(16/5)=4 > 3). Under /10: ceil(16/10)=2 <= 3 → must PASS now.
        _OJ._check_confabulation(_parsed(verified=16), ["read", "grep", "glob"])

    @needs("_check_confabulation", "ConfabulationDetected")
    def test_11_claims_1_tool_raises(self):
        # ceil(11/10)=2 > 1 → raise (just over one tool-call's batch budget).
        with self.assertRaises(_OJ.ConfabulationDetected):
            _OJ._check_confabulation(_parsed(verified=11), ["read"])

    @needs("_check_confabulation", "ConfabulationDetected")
    def test_30_claims_2_tools_raises(self):
        # ceil(30/10)=3 > 2 → raise.
        with self.assertRaises(_OJ.ConfabulationDetected):
            _OJ._check_confabulation(_parsed(verified=30), ["read", "grep"])

    @needs("_check_confabulation", "ConfabulationDetected")
    def test_egregious_overclaim_still_rejected(self):
        # The floor must keep its teeth: 100 verified on 1 tool call → ceil=10 > 1.
        with self.assertRaises(_OJ.ConfabulationDetected):
            _OJ._check_confabulation(_parsed(verified=100), ["read"])

    @needs("_check_confabulation", "ConfabulationDetected", "CONFAB_CLAIMS_PER_TOOL_CALL")
    def test_floor_constant_is_ten(self):
        # Encode the validity-critical constant so a silent change is caught.
        self.assertEqual(_OJ.CONFAB_CLAIMS_PER_TOOL_CALL, 10)

    @needs("_sum_verified_in_output")
    def test_sum_verified_counts_only_count_criteria(self):
        self.assertEqual(_OJ._sum_verified_in_output(_parsed(verified=4)), 4)


# ===========================================================================
# #11–#14 / #11b (M3) — Structural validation, and it runs BEFORE confab
# ===========================================================================

class TestStructuralValidation(unittest.TestCase):
    @needs("_validate_response_structure", "StructuralInvalidRun")
    def test_missing_bucket_fields_raises(self):
        parsed = _parsed()
        del parsed["review_a"]["criteria"]["issue_detection"]["verified_findings"]
        with self.assertRaises(_OJ.StructuralInvalidRun):
            _OJ._validate_response_structure(parsed)

    @needs("_validate_response_structure", "StructuralInvalidRun")
    def test_findings_present_empty_trace_raises(self):
        parsed = _parsed(verified=5)
        parsed["review_a"]["claim_trace"] = []
        with self.assertRaises(_OJ.StructuralInvalidRun):
            _OJ._validate_response_structure(parsed)

    @needs("_validate_response_structure")
    def test_zero_findings_empty_trace_passes(self):
        _OJ._validate_response_structure(_parsed(verified=0))  # no raise

    @needs("_validate_response_structure", "_check_confabulation", "StructuralInvalidRun")
    def test_malformed_schema_rejected_before_confab_path(self):
        """#11b/M3 anti-bypass: a malformed schema (missing buckets) must raise
        StructuralInvalidRun and NEVER reach the confab check (which would see a
        defaulted-0 verified count and pass vacuously). Asserted on the public
        runner once it exists; here we prove the ordering primitive: structural
        validation rejects the malformed input outright."""
        parsed = _parsed(verified=8)
        del parsed["review_a"]["criteria"]["coverage"]["contradicted_findings"]
        with self.assertRaises(_OJ.StructuralInvalidRun):
            _OJ._validate_response_structure(parsed)


# ===========================================================================
# Phase 4 — Evidence-binding (cited file actually read; snippet substring-matches)
# ===========================================================================

class TestEvidenceBinding(unittest.TestCase):
    def _transcript_having_read(self, path, returned_text):
        return [
            {"role": "assistant", "tool_calls": [
                {"id": "1", "function": {"name": "read",
                                         "arguments": f'{{"path": "{path}"}}'}}]},
            {"role": "tool", "tool_call_id": "1", "content": returned_text},
        ]

    @needs("_check_evidence_binding")
    def test_claim_citing_unread_file_flagged(self):
        # A claim cites a file that was NEVER read in the transcript → flagged.
        parsed = _parsed(verified=1, claim_trace=[{
            "claim_text": "c", "outcome": "verified",
            "evidence": {"file": "src/never_read.py", "line": 1, "snippet": "x"},
            "tools_used": ["read"],
        }])
        transcript = self._transcript_having_read("src/auth.py", "TOKEN_TTL_SECONDS = 3600")
        with self.assertRaises((_OJ.StructuralInvalidRun, ValueError, RuntimeError)):
            _OJ._check_evidence_binding(parsed, transcript)

    @needs("_check_evidence_binding")
    def test_claim_misquoting_snippet_flagged(self):
        # R1 residual: file WAS read but the quoted snippet does not substring-match
        # the tool's returned text → flagged (closes the misquote gap).
        parsed = _parsed(verified=1, claim_trace=[{
            "claim_text": "c", "outcome": "verified",
            "evidence": {"file": "src/auth.py", "line": 1, "snippet": "TTL = 99999"},
            "tools_used": ["read"],
        }])
        transcript = self._transcript_having_read("src/auth.py", "TOKEN_TTL_SECONDS = 3600")
        with self.assertRaises((_OJ.StructuralInvalidRun, ValueError, RuntimeError)):
            _OJ._check_evidence_binding(parsed, transcript)

    @needs("_check_evidence_binding")
    def test_well_bound_evidence_passes(self):
        parsed = _parsed(verified=1, claim_trace=[{
            "claim_text": "c", "outcome": "verified",
            "evidence": {"file": "src/auth.py", "line": 5, "snippet": "TOKEN_TTL_SECONDS = 3600"},
            "tools_used": ["read"],
        }])
        transcript = self._transcript_having_read("src/auth.py", "TOKEN_TTL_SECONDS = 3600")
        _OJ._check_evidence_binding(parsed, transcript)  # no raise

    # ---- Absence-via-Grep (fix #2: the regression that sank 2 live runs) ----
    # An absent-symbol claim contradicted via Grep has no file to Read/quote; its
    # evidence is {grep_pattern, grep_scope, result:"absent"} and must bind to an
    # EMITTED Grep that returned no matches — NOT raise "file never read".

    def _transcript_having_grep(self, pattern, result):
        return [
            {"role": "assistant", "tool_calls": [
                {"id": "1", "function": {"name": "Grep",
                                         "arguments": f'{{"pattern": "{pattern}"}}'}}]},
            {"role": "tool", "tool_call_id": "1", "content": result},
        ]

    def _absence_claim(self, pattern):
        return _parsed(verified=0, claim_trace=[{
            "claim_text": f"{pattern} does not exist in the repo",
            "outcome": "contradicted", "tools_used": ["Grep"],
            "evidence": {"grep_pattern": pattern, "grep_scope": "**/*.py", "result": "absent"},
        }])

    @needs("_check_evidence_binding")
    def test_absence_via_grep_passes_when_backed_by_empty_grep(self):
        # The fix: emitted Grep for the pattern returned "(no matches)" → absence
        # evidence is bound → PASSES (previously raised 'file never read').
        parsed = self._absence_claim("clampPositive")
        transcript = self._transcript_having_grep("clampPositive", "(no matches)")
        _OJ._check_evidence_binding(parsed, transcript)  # no raise

    @needs("_check_evidence_binding")
    def test_absence_claim_without_any_grep_raises(self):
        # Absence evidence but no matching Grep was ever emitted → unbacked → raise.
        parsed = self._absence_claim("clampPositive")
        transcript = self._transcript_having_read("src/auth.py", "TOKEN_TTL_SECONDS = 3600")
        with self.assertRaises((_OJ.StructuralInvalidRun, ValueError, RuntimeError)):
            _OJ._check_evidence_binding(parsed, transcript)

    @needs("_check_evidence_binding")
    def test_absence_claim_contradicted_by_grep_matches_raises(self):
        # The Grep actually FOUND the symbol → the "absent" claim is contradicted by
        # the transcript → raise (can't assert absence when the tool found it).
        parsed = self._absence_claim("clampPositive")
        transcript = self._transcript_having_grep("clampPositive", "src/util.py:5:def clampPositive():")
        with self.assertRaises((_OJ.StructuralInvalidRun, ValueError, RuntimeError)):
            _OJ._check_evidence_binding(parsed, transcript)

    # ---- 🔴 CRITICAL (Reviewer): string evidence can't opt out of binding ----
    # outcome in {verified,contradicted} → binding object MANDATORY; string → raise.
    # outcome in {non_falsifiable,unverified} → string rationale allowed (skip).

    def _claim(self, outcome, evidence):
        return _parsed(verified=(1 if outcome == "verified" else 0), claim_trace=[
            {"claim_text": "some claim", "outcome": outcome, "evidence": evidence}])

    @needs("_check_evidence_binding", "EvidenceBindingError")
    def test_verified_with_string_evidence_raises(self):
        # THE rubber-stamp hole the Reviewer caught: a 'verified' claim must not be
        # accepted on a free-text string — it must carry bound object evidence.
        with self.assertRaises(_OJ.EvidenceBindingError):
            _OJ._check_evidence_binding(self._claim("verified", "trust me, it's fine"), [])

    @needs("_check_evidence_binding", "EvidenceBindingError")
    def test_contradicted_with_string_evidence_raises(self):
        with self.assertRaises(_OJ.EvidenceBindingError):
            _OJ._check_evidence_binding(self._claim("contradicted", "it's just wrong"), [])

    @needs("_check_evidence_binding")
    def test_non_falsifiable_string_rationale_allowed(self):
        _OJ._check_evidence_binding(self._claim("non_falsifiable", "subjective style nit"), [])

    @needs("_check_evidence_binding")
    def test_unverified_string_rationale_allowed(self):
        _OJ._check_evidence_binding(self._claim("unverified", "couldn't confirm"), [])

    # ---- path normalization (Reviewer Important #2): ./ and sub-dir cites bind ----

    @needs("_check_evidence_binding")
    def test_dotslash_cite_binds_read(self):
        parsed = _parsed(verified=1, claim_trace=[{
            "claim_text": "c", "outcome": "verified",
            "evidence": {"file": "./src/utils.py", "line": 3, "snippet": "MAX_RETRIES = 3"}}])
        transcript = self._transcript_having_read("src/utils.py", "MAX_RETRIES = 3")
        _OJ._check_evidence_binding(parsed, transcript)  # no raise

    @needs("_check_evidence_binding")
    def test_basename_suffix_cite_binds_read(self):
        parsed = _parsed(verified=1, claim_trace=[{
            "claim_text": "c", "outcome": "verified",
            "evidence": {"file": "utils.py", "line": 3, "snippet": "MAX_RETRIES = 3"}}])
        transcript = self._transcript_having_read("src/utils.py", "MAX_RETRIES = 3")
        _OJ._check_evidence_binding(parsed, transcript)  # no raise

    # ---- grep-absence tightening (Reviewer Important #1) ----

    @needs("_check_evidence_binding")
    def test_grep_absence_unrelated_pattern_is_gaming_and_raises(self):
        # Gaming hole: an absence claim about 'foo' can't be rubber-stamped by
        # citing an unrelated trivially-empty Grep('zzzzz'). Pattern must tie to
        # the claim referent.
        parsed = _parsed(verified=0, claim_trace=[{
            "claim_text": "function foo is never defined", "outcome": "contradicted",
            "tools_used": ["Grep"],
            "evidence": {"grep_pattern": "zzzzz", "grep_scope": "**/*.py", "result": "absent"}}])
        transcript = self._transcript_having_grep("zzzzz", "(no matches)")
        with self.assertRaises((_OJ.StructuralInvalidRun, ValueError, RuntimeError)):
            _OJ._check_evidence_binding(parsed, transcript)

    @needs("_check_evidence_binding")
    def test_grep_absence_cited_pattern_must_exactly_match_emitted(self):
        # Cited grep_pattern must == an EMITTED Grep's pattern (substring fuzz dropped).
        parsed = self._absence_claim("clampPositive")  # claim_text contains clampPositive
        transcript = self._transcript_having_grep("clampPositiveButDifferent", "(no matches)")
        with self.assertRaises((_OJ.StructuralInvalidRun, ValueError, RuntimeError)):
            _OJ._check_evidence_binding(parsed, transcript)

    @needs("_check_evidence_binding")
    def test_different_dir_same_basename_cite_raises(self):
        # Reviewer Important #2: a cite of test/auth.py against Read(src/auth.py) is a
        # DIFFERENT path (suffix compares full min-component overlap) → not bound → raise.
        parsed = _parsed(verified=1, claim_trace=[{
            "claim_text": "c", "outcome": "verified",
            "evidence": {"file": "test/auth.py", "line": 1, "snippet": "TOKEN_TTL_SECONDS = 3600"}}])
        transcript = self._transcript_having_read("src/auth.py", "TOKEN_TTL_SECONDS = 3600")
        with self.assertRaises((_OJ.StructuralInvalidRun, ValueError, RuntimeError)):
            _OJ._check_evidence_binding(parsed, transcript)


# ===========================================================================
# EvidenceBindingError type + distinct counter routing (Reviewer Critical)
# ===========================================================================

class TestEvidenceBindingErrorType(unittest.TestCase):
    @needs("EvidenceBindingError", "StructuralInvalidRun")
    def test_is_structural_subclass(self):
        # Subclass → stays retryable AND caught by existing StructuralInvalidRun
        # handlers, but is a distinct type for separate counting.
        self.assertTrue(issubclass(_OJ.EvidenceBindingError, _OJ.StructuralInvalidRun))

    @needs("EvidenceBindingError", "_EXC_COUNTER_KEY")
    def test_distinct_counter_key(self):
        key = _OJ._EXC_COUNTER_KEY.get(_OJ.EvidenceBindingError)
        self.assertEqual(key, "evidence_binding_rejections")
        # Must NOT collapse into the generic structural counter (fabrication signal
        # kept separate from bad-JSON in the manifest).
        self.assertNotEqual(key, _OJ._EXC_COUNTER_KEY.get(_OJ.StructuralInvalidRun))


# ===========================================================================
# #5 (Reviewer) — _run_and_verify_openai: verify ORDER + #4 zero-tool gate
# ===========================================================================
# Stub client (Coder guaranteed the injectable first-arg seam): create() returns
# canned objects with .choices[0].message.content/.tool_calls + .finish_reason.

from types import SimpleNamespace


class _FakeToolCall:
    def __init__(self, id, name, arguments):
        self.id = id
        self.function = SimpleNamespace(name=name, arguments=arguments)


def _resp(content=None, tool_calls=None, finish_reason="stop"):
    msg = SimpleNamespace(content=content, tool_calls=tool_calls)
    return SimpleNamespace(choices=[SimpleNamespace(message=msg, finish_reason=finish_reason)])


class _FakeClient:
    """Drives the loop: queued tool-turns are emitted in order (each a single
    tool_call), then 'stop'; the response_format (final verdict) call returns the
    canned verdict JSON. Exhausted queue → 'stop', so retries don't hang."""
    def __init__(self, tool_turns, final_json):
        self._turns = list(tool_turns)
        self._final = final_json
        self.chat = SimpleNamespace(completions=SimpleNamespace(create=self._create))

    def _create(self, **kw):
        if "response_format" in kw:
            return _resp(content=self._final, finish_reason="stop")
        if self._turns:
            name, args = self._turns.pop(0)
            return _resp(tool_calls=[_FakeToolCall("tc1", name, args)], finish_reason="tool_calls")
        return _resp(content="done", finish_reason="stop")


@unittest.skipUnless(_HAS_GIT, "git not available")
class TestRunAndVerifyOpenAIOrder(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.sandbox = _RT.RepoTools(build_fixture_repo(os.path.join(self.tmp, "repo"))) if _RT else None

    def _verdict_json(self, claim_trace, verified=0):
        import json
        return json.dumps(_parsed(verified=verified, claim_trace=claim_trace))

    @needs("_run_and_verify_openai", "build_system_prompt")
    def test_happy_path_well_bound_run_passes(self):
        # Full pipeline: a Read in the loop, then a verified claim whose evidence
        # binds to that Read → structural+binding+confab all pass → returns.
        verdict = self._verdict_json(verified=1, claim_trace=[{
            "claim_text": "MAX_RETRIES is 3", "outcome": "verified",
            "evidence": {"file": "src/utils.py", "line": 3, "snippet": "MAX_RETRIES = 3"}}])
        client = _FakeClient([("Read", '{"path": "src/utils.py"}')], verdict)
        parsed, transcript, tool_calls = _OJ._run_and_verify_openai(
            client, "m", _OJ.build_system_prompt(), "diff", "rev_a", "rev_b", self.sandbox)
        self.assertIn("review_a", parsed)
        self.assertEqual(len(tool_calls), 1)

    @needs("_run_and_verify_openai", "build_system_prompt", "EvidenceBindingError")
    def test_structural_runs_before_binding(self):
        # A verdict that is BOTH structurally invalid (missing a bucket field) AND has
        # a verified+string binding violation must raise the STRUCTURAL error first —
        # NOT EvidenceBindingError. Locks structural → binding order.
        import json
        bad = _parsed(verified=1, claim_trace=[{
            "claim_text": "c", "outcome": "verified", "evidence": "a string"}])
        del bad["review_a"]["criteria"]["coverage"]["contradicted_findings"]  # structural break
        client = _FakeClient([], json.dumps(bad))
        with self.assertRaises(_OJ.StructuralInvalidRun) as ctx:
            _OJ._run_and_verify_openai(
                client, "m", _OJ.build_system_prompt(), "d", "a", "b", self.sandbox)
        self.assertNotIsInstance(ctx.exception, _OJ.EvidenceBindingError,
                                 "structural validation must fire before evidence-binding")

    @needs("_run_and_verify_openai", "build_system_prompt")
    def test_zero_tool_with_verified_is_rejected(self):
        # #4: a verified claim with NO tool calls in the transcript must be REJECTED
        # (never silently scored) — binding catches the unbacked verified claim.
        verdict = self._verdict_json(verified=1, claim_trace=[{
            "claim_text": "c", "outcome": "verified",
            "evidence": {"file": "src/utils.py", "line": 3, "snippet": "MAX_RETRIES = 3"}}])
        client = _FakeClient([], verdict)  # NO tool turns → zero tool calls
        with self.assertRaises((_OJ.StructuralInvalidRun, _OJ.ConfabulationDetected)):
            _OJ._run_and_verify_openai(
                client, "m", _OJ.build_system_prompt(), "d", "a", "b", self.sandbox)


# ===========================================================================
# Phase 5 (#7/#22/#26) — rate-limit backoff, retry classification, wall-clock cap
# ===========================================================================
# Contracts (Coder): RateLimited(RuntimeError) on openai.RateLimitError/APITimeoutError;
# JudgeRunTimeout(RuntimeError) when monotonic() exceeds PER_RUN_WALLCLOCK_SEC(300);
# _RETRYABLE_VERIFY = (StructuralInvalidRun, ConfabulationDetected, RateLimited,
# JudgeRunTimeout); RATE_LIMIT_BACKOFF_SEC=5.0 applied as sleep*(attempt+1).

import unittest.mock as _mock


class TestPhase5RetryTimeoutRateLimit(unittest.TestCase):
    @needs("RateLimited", "JudgeRunTimeout", "_RETRYABLE_VERIFY")
    def test_retryable_set_membership(self):
        # #22: RateLimited + JudgeRunTimeout join the retryable set; EvidenceBindingError
        # rides in as a StructuralInvalidRun subclass.
        rset = _OJ._RETRYABLE_VERIFY
        for exc in (_OJ.StructuralInvalidRun, _OJ.ConfabulationDetected,
                    _OJ.RateLimited, _OJ.JudgeRunTimeout):
            self.assertIn(exc, rset)
        self.assertTrue(issubclass(_OJ.EvidenceBindingError, _OJ.StructuralInvalidRun))

    @needs("RateLimited", "RATE_LIMIT_BACKOFF_SEC")
    def test_constants(self):
        self.assertEqual(_OJ.RATE_LIMIT_BACKOFF_SEC, 5.0)
        self.assertEqual(_OJ.PER_RUN_WALLCLOCK_SEC, 500)  # Phase 5: bumped 300→500

    @needs("RateLimited")
    def test_rate_limited_backoff_then_retry_succeeds(self):
        # #7: first attempt RateLimited → sleep(backoff*1) → retry → succeeds. A clean
        # zero-findings verdict so structural/binding/confab all pass on attempt 2.
        good = (_parsed(verified=0), [])
        with _mock.patch.object(_OJ, "_run_openai_judge_once",
                                side_effect=[_OJ.RateLimited("429"), good]) as once, \
             _mock.patch.object(_OJ.time, "sleep") as sleep:
            parsed, _, _ = _OJ._run_and_verify_openai(
                object(), "m", "sys", "d", "a", "b", None)
        self.assertEqual(once.call_count, 2)
        sleep.assert_called_once_with(_OJ.RATE_LIMIT_BACKOFF_SEC * 1)
        self.assertIn("review_a", parsed)

    @needs("RateLimited")
    def test_rate_limited_both_attempts_raises(self):
        with _mock.patch.object(_OJ, "_run_openai_judge_once",
                                side_effect=_OJ.RateLimited("429")), \
             _mock.patch.object(_OJ.time, "sleep"):
            with self.assertRaises(_OJ.RateLimited):
                _OJ._run_and_verify_openai(object(), "m", "sys", "d", "a", "b", None)

    @needs("RateLimited")
    def test_non_retryable_error_propagates_without_retry(self):
        # #22: a plain ValueError is NOT in the retryable set → propagates, 1 attempt.
        with _mock.patch.object(_OJ, "_run_openai_judge_once",
                                side_effect=ValueError("boom")) as once:
            with self.assertRaises(ValueError):
                _OJ._run_and_verify_openai(object(), "m", "sys", "d", "a", "b", None)
        self.assertEqual(once.call_count, 1)

    @needs("JudgeRunTimeout")
    def test_wall_clock_cap_raises_timeout(self):
        # #26: monotonic crosses the deadline at the top of iter 2 → JudgeRunTimeout
        # (no real sleep). iter1 does one Read against a real sandbox, iter2 trips.
        repo = build_fixture_repo(os.path.join(tempfile.mkdtemp(), "repo"))
        sandbox = _RT.RepoTools(repo)
        client = _FakeClient([("Read", '{"path": "src/utils.py"}')], "{}")
        # monotonic: base(deadline) , iter1 check (ok) , iter2 check (over)
        with _mock.patch.object(_OJ.time, "monotonic", side_effect=[1000.0, 1000.0, 2000.0]):
            with self.assertRaises(_OJ.JudgeRunTimeout):
                _OJ._run_openai_judge_once(client, "m", "sys", "user", sandbox)


# ===========================================================================
# score_paired_reviews wrapper — END-TO-END offline (CLOSES THE COVERAGE GAP)
# ===========================================================================
# main.py drives prepare_pr_judge_tasks + run_single_judge_task directly, so this
# convenience wrapper was ONLY exercised by the live-gated test → its body (incl.
# the finalize_pr_runs call) ran ZERO offline coverage and a NameError shipped.
# This drives the whole wrapper with a mocked client so latent issues are caught
# offline, no network.

@unittest.skipUnless(_HAS_GIT, "git not available")
class TestScorePairedReviewsOffline(unittest.TestCase):
    """C2: score_paired_reviews routes through Codex. Mock the run_codex_exec seam
    (canned verdict + trace) so the wrapper body runs end-to-end through
    finalize_pr_runs offline — the coverage gap that let the NameError ship."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)

    def _mock_codex(self, verdict_dict):
        from eval.tests.fixture_codex import full_stream
        return _mock.patch.object(_OJ, "run_codex_exec",
                                  return_value=(verdict_dict, full_stream("cat_read")))

    @needs("score_paired_reviews", "run_codex_exec")
    def test_wrapper_runs_through_finalize_offline(self):
        repo = build_fixture_repo(os.path.join(self.tmp, "repo"))
        # Zero-findings verdict → structural ok + interim binding passes (nothing to
        # bind) → all 3 runs reach finalize_pr_runs (the wrapper body fully runs).
        with self._mock_codex(_parsed(verified=0)):
            result = _OJ.score_paired_reviews(
                pr_data={"pr_number": 7, "diff": "+x\n", "repo": "fixture/repo"},
                skwad_review="skwad review", claude_ci_review="ci review",
                repo_path=repo, seed=42, run_dir=os.path.join(self.tmp, "runs"))
        self.assertIn("skwad", result)
        self.assertIn("claude_ci", result)
        self.assertEqual(result["n_runs_planned"], 3)
        self.assertGreaterEqual(result["n_runs_completed"], 1)

    @needs("score_paired_reviews", "run_codex_exec")
    def test_wrapper_threads_canaries_and_counters_offline(self):
        repo = build_fixture_repo(os.path.join(self.tmp, "repo"))
        counters = {}
        with self._mock_codex(_parsed(verified=0)):
            result = _OJ.score_paired_reviews(
                pr_data={"pr_number": 7, "diff": "+x\n", "repo": "fixture/repo"},
                skwad_review="s", claude_ci_review="c",
                repo_path=repo, seed=42, run_dir=os.path.join(self.tmp, "runs"),
                canary_injections=[], pilot_counters=counters)
        self.assertIn("skwad", result)  # canary/counter kwargs don't break the path


# ===========================================================================
# Transcript artifact preservation (diagnosis enabler for binding rejections)
# ===========================================================================

@unittest.skipUnless(_HAS_GIT, "git not available")
class TestTranscriptArtifact(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)

    @needs("_write_transcript_artifact")
    def test_writes_json_log(self):
        import json
        p = os.path.join(self.tmp, "artifact.json")
        _OJ._write_transcript_artifact(p, [{"attempt": 0, "transcript": [{"role": "x"}]}])
        self.assertTrue(os.path.exists(p))
        self.assertEqual(json.load(open(p))[0]["attempt"], 0)

    @needs("_write_transcript_artifact")
    def test_none_path_is_noop(self):
        _OJ._write_transcript_artifact(None, [{"attempt": 0}])  # no raise, nothing written

    @needs("_write_transcript_artifact")
    def test_bad_path_is_best_effort_no_raise(self):
        # Artifact failure must NEVER sink a run.
        _OJ._write_transcript_artifact("/no/such/dir/artifact.json", [{"attempt": 0}])

    @needs("_run_and_verify_openai", "EvidenceBindingError")
    def test_rejected_run_still_leaves_transcript_on_disk(self):
        # THE diagnostic property: a verdict that FAILS verification (verified+string
        # → EvidenceBindingError) must still leave its transcript persisted, so we can
        # diagnose what the model actually emitted (e.g. the processBatchScenarios case).
        import json
        sandbox = _RT.RepoTools(build_fixture_repo(os.path.join(self.tmp, "repo")))
        bad = _parsed(verified=1, claim_trace=[
            {"claim_text": "c", "outcome": "verified", "evidence": "a free-text string"}])
        client = _FakeClient([], json.dumps(bad))
        p = os.path.join(self.tmp, "artifact.json")
        with self.assertRaises(_OJ.EvidenceBindingError):
            _OJ._run_and_verify_openai(
                client, "m", "sys", "d", "a", "b", sandbox, artifact_path=p)
        self.assertTrue(os.path.exists(p), "transcript artifact must survive a rejection")
        log = json.load(open(p))
        self.assertTrue(log and log[-1]["transcript"] is not None,
                        "persisted artifact must contain the raw transcript for diagnosis")


# ===========================================================================
# prepare_pr_judge_tasks / build_user_prompt / run_single_judge_task — DIRECT
# offline coverage (audit: these were only transitively exercised — the same
# class of gap that let the score_paired_reviews NameError ship).
# ===========================================================================

class TestPrepareTasksAndPrompt(unittest.TestCase):
    @needs("prepare_pr_judge_tasks")
    def test_three_tasks_codex_shape(self):
        # C2: Codex task spec — repo_path(=worktree) + out_dir + model + config_path;
        # NO client/system_prompt/sandbox/port (those were the OpenAI-loop plumbing).
        import tempfile as _tf
        with _tf.TemporaryDirectory() as d:
            tasks, assignments = _OJ.prepare_pr_judge_tasks(
                {"pr_number": 7, "diff": "+x\n"}, "skwad rev", "ci rev",
                repo_path="/per-pr/worktree", seed=42, run_dir=d,
                model="m", config_path="cfg.json")
        self.assertEqual(len(tasks), 3)
        self.assertEqual(len(assignments), 3)
        for t in tasks:
            self.assertEqual(t["repo_path"], "/per-pr/worktree")
            self.assertEqual(t["model"], "m")
            self.assertEqual(t["config_path"], "cfg.json")
            self.assertIn("out_dir", t)
            self.assertIn("run_record_name", t)
            for gone in ("client", "system_prompt", "sandbox", "port", "base_port"):
                self.assertNotIn(gone, t)

    @needs("prepare_pr_judge_tasks")
    def test_canary_injected_into_task_reviews(self):
        import tempfile as _tf
        canary = {"target_pr": {"pr": "*"}, "inject_into": "skwad",
                  "claim_text": "INJECTED-CANARY-XYZ", "match_token": "XYZ",
                  "expected_outcome": "contradicted"}
        with _tf.TemporaryDirectory() as d:
            tasks, _ = _OJ.prepare_pr_judge_tasks(
                {"pr_number": 7, "diff": ""}, "base skwad", "base ci",
                repo_path="/wt", seed=42, run_dir=d, canary_injections=[canary])
        joined = " ".join(t["review_a"] + t["review_b"] for t in tasks)
        self.assertIn("INJECTED-CANARY-XYZ", joined)

    @needs("build_user_prompt")
    def test_build_user_prompt_carries_diff_and_both_reviews(self):
        # #28: user prompt = instruction + diff + Review A + Review B (still used by
        # the frozen OpenAI fallback + the Codex prompt builder).
        out = _OJ.build_user_prompt("DIFF-MARKER-123", "REVIEW-A-MARKER", "REVIEW-B-MARKER")
        self.assertIn("DIFF-MARKER-123", out)
        self.assertIn("REVIEW-A-MARKER", out)
        self.assertIn("REVIEW-B-MARKER", out)


def _codex_task(run_dir, **over):
    """A C2 Codex task spec (no client/sandbox/system_prompt)."""
    t = {
        "pr_number": 7, "run_index": 1, "a_system": "skwad", "b_system": "claude_ci",
        "diff": "", "review_a": "a", "review_b": "b", "canary_injections": [],
        "pr_data": {"pr_number": 7}, "run_dir": run_dir,
        "run_record_name": "judge_pr7_run1.json", "repo_path": "/x",
        "out_dir": os.path.join(run_dir, "codex_run1"), "model": "m", "config_path": None,
    }
    t.update(over)
    return t


def _codex(verdict_dict):
    """Mock the run_codex_exec seam → (verdict, trace) so _run_and_verify_codex runs
    its REAL structural + interim-binding gate over the canned verdict."""
    from eval.tests.fixture_codex import full_stream
    return _mock.patch.object(_OJ, "run_codex_exec",
                              return_value=(verdict_dict, full_stream("cat_read")))


class TestCodexRunSingleJudgeTask(unittest.TestCase):
    @needs("run_single_judge_task", "_run_and_verify_codex")
    def test_failure_is_captured_never_raises(self):
        # #23: a judge error (CodexExecError) → captured in status/error, never raised.
        import tempfile as _tf
        with _tf.TemporaryDirectory() as d:
            with _mock.patch.object(_OJ, "_run_and_verify_codex",
                                    side_effect=_OJ.CodexExecError("boom")):
                out = _OJ.run_single_judge_task(_codex_task(d))  # must NOT raise
        self.assertEqual(out["status"], "failed")
        self.assertTrue(out["error"].startswith("CodexExecError"))
        self.assertIsNone(out["resolved"])

    @needs("run_single_judge_task", "EvidenceBindingError")
    def test_binding_rejection_routes_to_evidence_binding_counter(self):
        import tempfile as _tf
        with _tf.TemporaryDirectory() as d:
            with _mock.patch.object(_OJ, "_run_and_verify_codex",
                                    side_effect=_OJ.EvidenceBindingError("C3: binding not yet re-framed")):
                out = _OJ.run_single_judge_task(_codex_task(d))
        self.assertEqual(out["counter_increment"], "evidence_binding_rejections")


@unittest.skipUnless(_HAS_GIT, "git not available")
class TestCodexJudgeRoutingE2E(unittest.TestCase):
    """C3: run_single_judge_task routes through the REAL codex binding (gate is
    reached, not the C2 stub). End-to-end via the mocked run_codex_exec seam."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)

    @needs("run_single_judge_task", "run_codex_exec")
    def test_unbound_verified_claim_annotated_ungrounded_run_completes(self):
        # SOFT-SIGNAL (condition c — load-bearing): a verified claim with string
        # (unbindable) evidence is NO LONGER a hard drop. The real binding annotates it
        # grounded=False and the run COMPLETES (status ok, verdict produced) — the gate
        # no longer drops the run. The ungrounded claim surfaces LOUDLY in the low-
        # grounding alarm (skwad below floor), so fabrication isn't silent.
        verdict = _parsed(verified=1, claim_trace=[
            {"claim_text": "c", "outcome": "verified", "evidence": "trust me"}])
        with _codex(verdict) as m:
            out = _OJ.run_single_judge_task(_codex_task(self.tmp))
        self.assertEqual(out["status"], "ok")          # run completes, NOT dropped
        self.assertIsNotNone(out["resolved"])
        self.assertIn("skwad", out["low_grounding"])    # ungrounded verified claim flagged
        self.assertTrue(m.called, "run_codex_exec must be invoked (routing)")

    @needs("run_single_judge_task", "run_codex_exec")
    def test_no_findings_verdict_scores_ok(self):
        with _codex(_parsed(verified=0)):
            out = _OJ.run_single_judge_task(_codex_task(self.tmp))
        self.assertEqual(out["status"], "ok")
        self.assertIsNotNone(out["resolved"])

    @needs("run_single_judge_task", "run_codex_exec")
    def test_structural_validation_runs_before_binding(self):
        malformed = {"review_a": {"criteria": {"issue_detection": {"score": 1}}},
                     "review_b": {"criteria": {}}}
        with _codex(malformed):
            out = _OJ.run_single_judge_task(_codex_task(self.tmp))
        self.assertEqual(out["status"], "failed")
        self.assertIn("StructuralInvalidRun", out["error"])
        self.assertNotIn("EvidenceBindingError", out["error"])  # structural fires first


# ---------------------------------------------------------------------------
# C3 ANTI-GAMING SUITE (security-critical; the Reviewer re-runs these).
# Direct _check_evidence_binding_codex(verdict, parsed_trace, worktree) — binds on
# what the agent ACTUALLY SAW (command output + attribution + worktree containment).
# ---------------------------------------------------------------------------

def _cmd(**f):
    base = {"cmd": "", "output": "", "exit": 0, "is_search": False, "is_readlike": False,
            "attributed_paths": [], "searched_symbols": [], "read_paths": []}
    base.update(f)
    return base


def _trace(*cmds):
    return {"commands": list(cmds)}


def _verdict(outcome, evidence, claim_text="c"):
    return {"review_a": {"claim_trace": [
        {"claim_text": claim_text, "outcome": outcome, "evidence": evidence}]},
        "review_b": {}}


# Soft-signal reframe: `_check_evidence_binding_codex` ANNOTATES claims in place
# (grounded True/False + grounding_reason) instead of raising. Robust assertions
# (grounded bool + reason truthy) — not anchored on exact reason strings.
def _claim0(verdict, review="review_a"):
    return verdict[review]["claim_trace"][0]


def assert_ungrounded(tc, verdict, review="review_a"):
    c = _claim0(verdict, review)
    tc.assertIs(c.get("grounded"), False,
                f"expected grounded=False, got {c.get('grounded')!r} (reason={c.get('grounding_reason')!r})")
    tc.assertTrue(c.get("grounding_reason"), "ungrounded claim must carry a non-empty grounding_reason")


def assert_grounded(tc, verdict, review="review_a"):
    tc.assertIs(_claim0(verdict, review).get("grounded"), True,
                f"expected grounded=True, got {_claim0(verdict, review).get('grounded')!r}")


@unittest.skipUnless(_HAS_GIT, "git not available")
class TestCodexBindingAntiGaming(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.wt = build_fixture_repo(os.path.join(self.tmp, "repo"))

    def _bind(self, verdict, trace):
        return _OJ._check_evidence_binding_codex(verdict, trace, self.wt)

    # ---- content claims (soft-signal: ANNOTATE grounded, never raise) ----
    @needs("_check_evidence_binding_codex")
    def test_content_happy_grounded(self):
        v = _verdict("verified", {"file": "src/cache.py", "line": 1, "snippet": "move_to_end"})
        t = _trace(_cmd(is_readlike=True, output="    self._store.move_to_end(key)",
                        attributed_paths=["src/cache.py"]))
        self._bind(v, t)
        assert_grounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_content_mix_and_match_ungrounded(self):
        # snippet IS in a read-like output, but attributed to a DIFFERENT file than cited.
        v = _verdict("verified", {"file": "src/cache.py", "line": 1, "snippet": "move_to_end"})
        t = _trace(_cmd(is_readlike=True, output="    self._store.move_to_end(key)",
                        attributed_paths=["src/auth.py"]))
        self._bind(v, t)  # no raise — soft signal
        assert_ungrounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_content_echo_fabrication_ungrounded(self):
        # The snippet appears in a NON-read-like (echo/python-c) command output.
        v = _verdict("verified", {"file": "src/cache.py", "line": 1, "snippet": "move_to_end"})
        t = _trace(_cmd(is_readlike=False, output="move_to_end", attributed_paths=[]))
        self._bind(v, t)
        assert_ungrounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_content_snippet_not_in_output_ungrounded(self):
        v = _verdict("verified", {"file": "src/cache.py", "line": 1, "snippet": "move_to_end"})
        t = _trace(_cmd(is_readlike=True, output="something unrelated",
                        attributed_paths=["src/cache.py"]))
        self._bind(v, t)
        assert_ungrounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_content_cited_outside_worktree_ungrounded_g1(self):
        for bad in ("/etc/passwd", "../../../../etc/passwd"):
            v = _verdict("verified", {"file": bad, "line": 1, "snippet": "x"})
            t = _trace(_cmd(is_readlike=True, output="x", attributed_paths=[bad]))
            self._bind(v, t)
            assert_ungrounded(self, v)  # G1 worktree-containment still flagged

    # ---- multi-file search per-line attribution (Reviewer RFC crack) ----
    _MULTIFILE = "src/fileA.py:10:def foo(): return BUG\nsrc/fileB.py:20:foo_helper()"

    @needs("_check_evidence_binding_codex")
    def test_multifile_search_misattribution_ungrounded(self):
        # The crack: snippet is fileA's matched LINE, but the claim cites fileB. Both
        # files are in attributed_paths (co-match). Per-line attribution must flag it
        # UNGROUNDED — it's fileA's content, not fileB's.
        v = _verdict("verified",
                     {"file": "src/fileB.py", "line": 20, "snippet": "def foo(): return BUG"})
        t = _trace(_cmd(is_search=True, is_readlike=True, output=self._MULTIFILE,
                        attributed_paths=["src/fileA.py", "src/fileB.py"],
                        searched_symbols=["foo"]))
        self._bind(v, t)
        assert_ungrounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_multifile_search_correct_attribution_grounded(self):
        # Same multi-file output; the claim cites fileA + the snippet ON fileA's
        # prefixed line → per-line match → grounded (no false-reject).
        v = _verdict("verified",
                     {"file": "src/fileA.py", "line": 10, "snippet": "def foo(): return BUG"})
        t = _trace(_cmd(is_search=True, is_readlike=True, output=self._MULTIFILE,
                        attributed_paths=["src/fileA.py", "src/fileB.py"],
                        searched_symbols=["foo"]))
        self._bind(v, t)
        assert_grounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_single_file_search_no_prefix_snippet_anywhere_grounded(self):
        # Regression guard: a single-file search (`rg foo f.py` → "2:text", NO path
        # prefix) is whole-output snippet-anywhere — the per-line tightening is
        # multi-file-output ONLY and must not break this.
        v = _verdict("verified",
                     {"file": "src/cache.py", "line": 2, "snippet": "def foo(): return BUG"})
        t = _trace(_cmd(is_search=True, is_readlike=True,
                        output="2:def foo(): return BUG\n3:    pass",
                        attributed_paths=["src/cache.py"], searched_symbols=["foo"]))
        self._bind(v, t)
        assert_grounded(self, v)

    # ---- absence claims ----
    @needs("_check_evidence_binding_codex")
    def test_absence_tied_empty_search_grounded(self):
        # Provenance reads the RAW cmd: `rg foo .` → bare `foo` boundary-matches the
        # recorded grep_pattern "foo".
        v = _verdict("contradicted", {"grep_pattern": "foo", "grep_scope": ".", "result": "absent"},
                     claim_text="function foo is never called")
        t = _trace(_cmd(cmd="rg foo .", is_search=True, output="", exit=1, searched_symbols=["foo"]))
        self._bind(v, t)
        assert_grounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_absence_piped_empty_exit0_grounded(self):
        v = _verdict("contradicted", {"grep_pattern": "foo", "result": "absent"},
                     claim_text="foo is never called")
        t = _trace(_cmd(cmd="rg foo . | head", is_search=True, output="", exit=0,
                        searched_symbols=["foo"]))
        self._bind(v, t)
        assert_grounded(self, v)  # piped search → last cmd exit 0

    @needs("_check_evidence_binding_codex")
    def test_absence_unrelated_empty_search_ungrounded(self):
        # An empty search for a DIFFERENT symbol can't back a foo-absence claim.
        v = _verdict("contradicted", {"grep_pattern": "foo", "result": "absent"},
                     claim_text="foo is never called")
        t = _trace(_cmd(cmd="rg zzz_unrelated .", is_search=True, output="", exit=1,
                        searched_symbols=["zzz_unrelated"]))
        self._bind(v, t)
        assert_ungrounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_absence_exit2_ungrounded(self):
        v = _verdict("contradicted", {"grep_pattern": "foo", "result": "absent"},
                     claim_text="foo is never called")
        t = _trace(_cmd(cmd="rg foo .", is_search=True, output="", exit=2, searched_symbols=["foo"]))
        self._bind(v, t)
        assert_ungrounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_absence_nonempty_search_ungrounded(self):
        # A search that FOUND matches cannot back an absence claim.
        v = _verdict("contradicted", {"grep_pattern": "foo", "result": "absent"},
                     claim_text="foo is never called")
        t = _trace(_cmd(cmd="rg foo .", is_search=True, output="src/x.py:3:foo()", exit=0,
                        searched_symbols=["foo"]))
        self._bind(v, t)
        assert_ungrounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_absence_no_search_emitted_ungrounded(self):
        # The processBatchScenarios fabrication: cite an absence grep that was NEVER run.
        v = _verdict("contradicted", {"grep_pattern": "foo", "result": "absent"},
                     claim_text="foo is never called")
        self._bind(v, _trace())  # no commands at all
        assert_ungrounded(self, v)

    # ---- evidence shape / outcome gating ----
    @needs("_check_evidence_binding_codex")
    def test_string_evidence_on_verified_ungrounded(self):
        v = _verdict("verified", "trust me")
        self._bind(v, _trace(_cmd(cmd="cat x", is_readlike=True, output="x")))
        assert_ungrounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_missing_binding_fields_ungrounded(self):
        v = _verdict("contradicted", {"note": "no file/snippet or grep_pattern"})
        self._bind(v, _trace(_cmd(cmd="cat x", is_readlike=True, output="x")))
        assert_ungrounded(self, v)

    @needs("_check_evidence_binding_codex")
    def test_non_falsifiable_and_unverified_skip(self):
        self._bind(_verdict("non_falsifiable", "subjective style nit"), _trace())  # no raise
        self._bind(_verdict("unverified", "couldn't confirm"), _trace())  # no raise


# ===========================================================================
# Bug #2 — line-number-tolerant snippet binding in _content_snippet_bound.
#
# A multi-line snippet cited against `nl`/`cat -n` numbered read output failed to
# bind: the cited body has no `   N\t` prefixes, so `snippet in output` (which
# spans line boundaries) was False even for a faithful quote. The fix de-prefixes
# read output BUT ONLY when it is UNIFORMLY numbered (every non-empty line matches
# `^\s*\d+\t`), stripping `^\s*\d+\t` per line. The anti-fabrication guards are
# load-bearing: de-prefixing must NOT leak into raw reads (a `sed -n` dump that
# merely contains a `42\tvalue` data line) nor into the multi-file `path:N:text`
# search branch — either leak would let a fabricated quote bind. Contiguous
# substring; NO whitespace normalization.
#
# Gated on a runtime probe so the group is GREEN-by-skip until the Coder lands the
# de-prefix in `_content_snippet_bound`, then auto-activates with zero edits here.
# ===========================================================================

def _read_cmd(cited_file, output, *, is_search=False, attributed_paths=None, cmd=None):
    # `cmd` defaults to a numbering read (`nl -ba <file>`) so the Bug#2 de-prefix —
    # which under the command-based gate keys off the READ TOOL, not the output shape —
    # still fires for the numbered-output cases. Override with a raw read (`sed`/`cat`)
    # or a search command where de-prefix must NOT apply.
    return {"cmd": cmd if cmd is not None else f"nl -ba {cited_file}",
            "is_readlike": True, "is_search": is_search, "output": output,
            "attributed_paths": attributed_paths if attributed_paths is not None else [cited_file]}


def _deprefix_supported():
    """True once _content_snippet_bound de-prefixes uniformly-numbered read output.
    Probe: a bare 2-line snippet binds against `nl`-style output ONLY via de-prefix
    (the snippet spans a line boundary, so plain `snippet in output` is False)."""
    if _OJ is None or not hasattr(_OJ, "_content_snippet_bound"):
        return False
    try:
        return bool(_OJ._content_snippet_bound(
            "f.py", "alpha\nbeta", _read_cmd("f.py", "     1\talpha\n     2\tbeta\n")))
    except Exception:
        return False


@unittest.skipUnless(_deprefix_supported(),
                     "blocked: _content_snippet_bound de-prefix (Bug #2) not landed yet")
class TestContentSnippetLineNumberTolerance(unittest.TestCase):
    # nl -ba style: right-aligned number + TAB, then the (indented) file content.
    _NL_GET = ("    10\t    def get(self, key):\n"
               "    11\t        self._store.move_to_end(key)\n"
               "    12\t        return self._store[key]\n")
    _GET_BODY = ("    def get(self, key):\n"
                 "        self._store.move_to_end(key)\n"
                 "        return self._store[key]")

    def _bind(self, cited, snippet, output, **kw):
        return _OJ._content_snippet_bound(cited, snippet, _read_cmd(cited, output, **kw))

    def test_multiline_nl_numbered_read_binds(self):
        # THE bug: a faithful multi-line get() body quoted against nl output binds.
        # (Plain substring fails — the snippet has no line-number prefixes.)
        self.assertNotIn(self._GET_BODY, self._NL_GET)  # proves it needs de-prefix
        self.assertTrue(self._bind("src/cache.py", self._GET_BODY, self._NL_GET))

    def test_single_line_cite_against_numbered_output_still_binds(self):
        # Regression: the single-line path that already worked must keep working.
        self.assertTrue(self._bind("src/cache.py", "def get(self, key):", self._NL_GET))

    def test_fabricated_multiline_not_in_numbered_output_rejected(self):
        # The guard isn't weakened: a body that never appears (even de-prefixed) fails.
        fabricated = "    def evict(self):\n        raise NotImplementedError"
        self.assertFalse(self._bind("src/cache.py", fabricated, self._NL_GET))

    def test_cat_n_numbered_read_binds(self):
        # `cat -n` numbering (6-wide field + TAB) is handled the same as `nl`.
        out = "     1\timport os\n     2\tCACHE = {}\n"
        self.assertTrue(self._bind("src/cache.py", "import os\nCACHE = {}", out,
                                   cmd="cat -n src/cache.py"))

    def test_gate_proof_raw_read_with_digit_tab_line_not_deprefixed(self):
        # CRITICAL anti-fabrication: a `sed -n` RAW dump (NOT a numbering command) that
        # happens to contain a genuine `42\tvalue` data line must NOT be de-prefixed.
        # The fabricated snippet binds ONLY if `42\t`/`99\t` were stripped — and it is
        # NOT a raw substring — so the gate must keep it REJECTED.
        raw = "plain text line\n42\tvalue\n99\tbeta\n"
        fabricated = "value\nbeta"
        self.assertNotIn(fabricated, raw)  # not a raw match → only de-prefix could bind
        self.assertFalse(self._bind("src/data.py", fabricated, raw,
                                    cmd="sed -n '1,5p' src/data.py"))

    def test_multifile_search_stitched_snippet_still_rejected(self):
        # The de-prefix must NOT leak into the multi-file `path:N:text` search branch:
        # a snippet stitched from a line in fileA + a line in fileB spans two files and
        # is on no single cited-file match line → REJECTED (per-line attribution governs).
        out = ("src/fileA.py:10:def foo(): return BUG\n"
               "src/fileB.py:20:    foo_helper()\n")
        stitched = "def foo(): return BUG\n    foo_helper()"
        self.assertFalse(self._bind(
            "src/fileA.py", stitched, out, cmd="rg -n foo src/",
            is_search=True, attributed_paths=["src/fileA.py", "src/fileB.py"]))

    def test_nl_prefix_stripped_but_content_digit_tab_preserved(self):
        # First TAB anchors: a content line whose own text starts with `42\t` (after
        # nl's `   1\t` prefix) must have ONLY nl's prefix stripped — the content's
        # `42\t` survives — so a multi-line cite carrying that `42\tvalue` still binds.
        out = "     1\t42\tvalue\n     2\t    return timeout\n"
        snippet = "42\tvalue\n    return timeout"
        self.assertNotIn(snippet, out)  # genuinely needs de-prefix (and only the prefix)
        self.assertTrue(self._bind("src/cfg.py", snippet, out))


# ===========================================================================
# Leading-whitespace-tolerant MULTI-LINE content matching (1818 fix). When the
# model re-indents a faithful multi-line quote, the exact-substring path misses it.
# NEW fallback (snippet has >=2 lines): leading-strip-ONLY, CONTIGUOUS, EXACT
# positional blank correspondence, single-file, on BOTH the raw and de-prefixed
# views — but NEVER on the multi-file `path:N:text` search branch. The stitching /
# blank-skip / content-alteration / cross-file guards are the load-bearing anti-
# fabrication tests: lstrip tolerance must NOT open a path to bind altered content.
# Gated on a probe → GREEN-by-skip until the fallback lands, then auto-activates.
# ===========================================================================

def _leading_ws_multiline_landed():
    """True once the multi-line leading-strip fallback exists. Probe: a re-indented
    2-line cite (5 spaces vs the file's 1) that is NOT an exact substring binds ONLY
    via the lstrip fallback."""
    if _OJ is None or not hasattr(_OJ, "_content_snippet_bound"):
        return False
    cmd = {"cmd": "cat f.py", "is_readlike": True, "is_search": False,
           "output": " alpha\n beta\n", "attributed_paths": ["f.py"]}
    try:
        return _OJ._content_snippet_bound("f.py", "     alpha\n     beta", cmd) is True
    except Exception:
        return False


@unittest.skipUnless(_leading_ws_multiline_landed(),
                     "blocked: leading-whitespace-tolerant multi-line matching not landed yet")
class TestContentMultilineLeadingWhitespace(unittest.TestCase):
    def _csb(self, cited, snippet, output, cmd, *, is_search=False, attributed_paths=None):
        d = {"cmd": cmd, "is_readlike": True, "is_search": is_search, "output": output,
             "attributed_paths": attributed_paths if attributed_paths is not None else [cited]}
        return _OJ._content_snippet_bound(cited, snippet, d)

    def test_reindented_multiline_cite_binds(self):
        # #1 THE FIX (1818-shape): the model re-indented the quote (5 spaces) vs the
        # file's 1-space indent; content is byte-identical after leading-strip → BINDS.
        out = " def handler(self):\n     self.run()\n"          # file: 1-space outer indent
        snippet = "     def handler(self):\n         self.run()"  # re-indented quote
        self.assertNotIn(snippet, out)  # exact substring misses → needs the fallback
        self.assertTrue(self._csb("h.py", snippet, out, "cat h.py"))

    def test_stitched_noncontiguous_snippet_rejected(self):
        # #2 CRITICAL: realA+realC skipping the intervening realB is NOT contiguous → REJECT.
        out = "    realA\n    realB\n    realC\n"
        self.assertFalse(self._csb("f.py", "realA\nrealC", out, "cat f.py"))

    def test_blank_skip_rejected(self):
        # #3: a blank line between realA and realC in the file must be positionally
        # present in the snippet — omitting it (realA\nrealC) → REJECT.
        out = "realA\n\nrealC\n"
        self.assertFalse(self._csb("f.py", "realA\nrealC", out, "cat f.py"))

    def test_blank_present_positional_binds(self):
        # #3 converse: blank present in the same position → BINDS (positional correspondence).
        out = "realA\n\nrealC\n"
        self.assertTrue(self._csb("f.py", "realA\n\nrealC", out, "cat f.py"))

    def test_internal_whitespace_alteration_rejected(self):
        # #4a: leading-strip is leading-ONLY — an INTERNAL whitespace change (foo  bar
        # vs foo bar) is a content alteration → REJECT.
        out = "  alpha\n  return foo bar\n"
        snippet = "    alpha\n    return foo  bar"  # double space inside line 2
        self.assertFalse(self._csb("f.py", snippet, out, "cat f.py"))

    def test_trailing_whitespace_alteration_rejected(self):
        # #4b: a trailing-whitespace change is NOT stripped → content mismatch → REJECT.
        out = "alpha\nfoo bar\n"
        snippet = "alpha\nfoo bar "  # trailing space on line 2
        self.assertFalse(self._csb("f.py", snippet, out, "cat f.py"))

    def test_cross_file_search_branch_not_leading_strip_matched(self):
        # #5: the fallback must NOT apply to the multi-file `path:N:text` search branch.
        # A snippet stitched across two files' match lines → REJECT (per-line attribution).
        out = ("src/a.py:10:    realA\n"
               "src/b.py:20:    realB\n")
        self.assertFalse(self._csb(
            "src/a.py", "realA\nrealB", out, "rg -n real src/",
            is_search=True, attributed_paths=["src/a.py", "src/b.py"]))

    def test_single_line_substring_bind_unchanged(self):
        # #6: the single-line exact-substring fast path is untouched (fallback is
        # multi-line only).
        self.assertTrue(self._csb("f.py", "return foo", "    return foo\n", "cat f.py"))

    def test_reindented_multiline_against_nl_deprefixed_binds(self):
        # #7: the fallback applies to the de-prefixed view too — a re-indented multi-line
        # cite vs `nl`-numbered output binds after both de-prefix AND leading-strip.
        out = "    10\t  def handler(self):\n    11\t      self.run()\n"
        snippet = "        def handler(self):\n            self.run()"  # different indent
        self.assertNotIn(snippet, out)
        self.assertTrue(self._csb("h.py", snippet, out, "nl -ba h.py"))


# ===========================================================================
# Absence-claim binding — pattern-EQUALITY provenance (anti-launder).
#
# An absence claim ("X is nowhere in the repo") binds only if BOTH: (1) the cited
# `grep_pattern` is tied to the claim text, AND (2) the trace contains an emitted
# EMPTY search (is_search, empty output, exit==1) whose pattern argument is
# LEXICALLY-normalized-EQUAL to the cited `grep_pattern` (quote-strip + whitespace-
# collapse only; plain substring forbidden). The fix it guards:
#   * Over-rejection (case 1): a real escaped-alternation pattern
#     `isValidating|:loading=|loading\b` — which the trace parser pipe-mangles in
#     searched_symbols — must still BIND when correctly extracted from the command.
#   * LAUNDER (case 2, the load-bearing anti-fabrication guard): a NARROW strawman
#     `rg "loadingSpinnerInternalXYZ"` was run empty, but the cited pattern is a
#     BROADER on-topic `"loading"` that is only a SUBSTRING of the raw command and
#     was never itself run empty → must be REJECTED. A substring provenance check
#     would wrongly bind it; pattern-equality must not.
#
# Tests drive the REAL `_parse_codex_trace` from realistic `rg …` command strings,
# so whether the binding reads searched_symbols or re-extracts from cmd, both are
# authentic. Gated on a runtime probe → GREEN-by-skip until the Coder lands the
# pattern-equality branch, then auto-activates.
# ===========================================================================

def _codex_cmd_event(inner_cmd, output, exit_code):
    """One `codex exec --json` command_execution event (zsh single-quote wrapper,
    mirroring eval/tests/fixture_codex.py) for an arbitrary inner command."""
    import json as _json
    return _json.dumps({
        "type": "item.completed",
        "item": {"id": "x", "type": "command_execution",
                 "command": "/bin/zsh -lc '" + inner_cmd + "'",
                 "aggregated_output": output, "exit_code": exit_code,
                 "status": "completed" if exit_code == 0 else "failed"}})


def _trace_from(*events):
    return _OJ._parse_codex_trace("\n".join(events) + "\n") if _OJ else {"commands": []}


def _absence_evidence(grep_pattern, scope="**/*.ts"):
    return {"grep_pattern": grep_pattern, "grep_scope": scope, "result": "absent"}


def _absence_verdict(grep_pattern, claim_text, scope="**/*.ts"):
    return _verdict("contradicted", _absence_evidence(grep_pattern, scope), claim_text)


class TestAbsenceClaimPatternEquality(unittest.TestCase):
    """Soft-signal reframe: the absence pattern-equality check now ANNOTATES grounded
    instead of raising. A genuinely-backed absence claim → grounded=True; a launder /
    unbacked / off-topic / matched / semantically-different one → grounded=False."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)

    def _bind(self, verdict, trace):
        _OJ._check_evidence_binding_codex(verdict, trace, self.tmp)
        return verdict

    def test_real_escaped_alternation_grounded(self):
        # CASE 1 — the over-rejection fix: a genuine escaped-alternation pattern the
        # trace was actually run empty for is GROUNDED (correctly extracted).
        pattern = r"isValidating|:loading=|loading\b"
        claim = "PR adds no loading state — isValidating is never set"
        trace = _trace_from(_codex_cmd_event('rg -n "' + pattern + '" .', "", 1))
        assert_grounded(self, self._bind(_absence_verdict(pattern, claim), trace))

    def test_launder_broader_substring_pattern_ungrounded(self):
        # CASE 2 (CRITICAL anti-fabrication): only a NARROW strawman was run empty; the
        # cited pattern is a BROADER on-topic substring never itself searched empty →
        # UNGROUNDED (pattern-equality, not substring).
        claim = "there is no loading indicator in the component"
        trace = _trace_from(_codex_cmd_event('rg "loadingSpinnerInternalXYZ" .', "", 1))
        assert_ungrounded(self, self._bind(_absence_verdict("loading", claim), trace))

    def test_no_corresponding_empty_search_ungrounded(self):
        # CASE 3: cited pattern ties to the claim, but NO emitted empty search EQUALS it.
        claim = "no loading state is present"
        trace = _trace_from(_codex_cmd_event('rg "spinner" .', "", 1))  # empty, but ≠ "loading"
        assert_ungrounded(self, self._bind(_absence_verdict("loading", claim), trace))

    def test_search_returned_matches_ungrounded(self):
        # CASE 4: an emitted search whose pattern EQUALS the cited grep_pattern but
        # returned MATCHES (non-empty, exit 0) → the symbol exists → UNGROUNDED.
        claim = "no loading state is present"
        trace = _trace_from(_codex_cmd_event(
            "rg loading .", "./src/widget.ts:12:const loading = ref(false)\n", 0))
        assert_ungrounded(self, self._bind(_absence_verdict("loading", claim), trace))

    def test_pattern_not_tied_to_claim_ungrounded(self):
        # CASE 5: cited pattern is OFF-TOPIC for the claim — claim-tie fails → UNGROUNDED.
        claim = "no loading state is present"
        trace = _trace_from(_codex_cmd_event('rg "zzz_unrelated_symbol" .', "", 1))
        assert_ungrounded(self, self._bind(_absence_verdict("zzz_unrelated_symbol", claim), trace))

    def test_lexical_normalization_cosmetic_diff_grounded(self):
        # CASE 6a: cited grep_pattern differs only by quoting/whitespace → normalized
        # equal → GROUNDED.
        claim = "no loading state is present"
        trace = _trace_from(_codex_cmd_event("rg loading .", "", 1))  # emitted bare token
        assert_grounded(self, self._bind(_absence_verdict('"loading"  ', claim), trace))

    def test_semantic_diff_not_unescaped_into_match_ungrounded(self):
        # CASE 6b: emitted `loading\b` vs cited `loading` are SEMANTICALLY different;
        # normalization keeps them distinct → UNGROUNDED (no unescape laundering).
        claim = "no loading state is present"
        trace = _trace_from(_codex_cmd_event(r'rg "loading\b" .', "", 1))
        assert_ungrounded(self, self._bind(_absence_verdict("loading", claim), trace))


# ===========================================================================
# Embedded-quote extraction (`_extract_search_pattern_arg`). Escaped-aware double-quote
# capture `"((?:\\.|[^"\\])*)"` + MINIMAL shell-unescape (`\"`→`"`, `\\`→`\` ONLY) on
# DOUBLE-QUOTED captures; single-quoted + bare stay raw; `_emitted_pattern_matches`
# stays EQUALITY. Load-bearing anti-fab guards: regex metachars (`\b`) must survive
# (shell-unescape, NOT semantic-unescape), the launder stays REJECTED, single-quoted
# values are NOT corrupted, and genuinely-distinct patterns do NOT collapse to equal.
# ===========================================================================

def _abs_bind(recorded, inner_cmd, claim):
    """Run the soft-signal binding and return the verdict (claim annotated in place)."""
    verdict = _absence_verdict(recorded, claim)
    trace = _trace_from(_codex_cmd_event(inner_cmd, "", 1))
    with tempfile.TemporaryDirectory() as d:
        _OJ._check_evidence_binding_codex(verdict, trace, d)
    return verdict


class TestEmbeddedQuotePatternExtraction(unittest.TestCase):
    _EX = staticmethod(lambda raw: _OJ._extract_search_pattern_arg(raw))

    def test_embedded_escaped_quote_fully_extracted(self):
        # #1 THE FIX (unit): the escaped `\"` inside the double-quoted arg no longer
        # truncates the capture — the FULL pattern is recovered, `\"` → literal `"`.
        got = self._EX(r'rg -n "isValidating|:loading=|loading=\"|loading\b" scope')
        self.assertEqual(got, 'isValidating|:loading=|loading="|loading\\b')

    def test_embedded_escaped_quote_absence_grounded_end_to_end(self):
        # #1 THE FIX (end-to-end): emitted empty search w/ escaped embedded quote +
        # recorded literal-quote pattern → equality holds → GROUNDED.
        assert_grounded(self, _abs_bind(
            'isValidating|:loading=|loading="|loading\\b',
            r'rg -n "isValidating|:loading=|loading=\"|loading\b" f.vue',
            "no loading state: isValidating is never set"))

    def test_regex_metachar_backslash_b_preserved(self):
        # #2 CRITICAL: shell-unescape is `\"`/`\\` ONLY — it must NOT touch `\b`. The
        # extracted pattern still contains the literal `\b` (NOT collapsed to `b`).
        got = self._EX(r'rg "foo\b" f')
        self.assertEqual(got, r'foo\b')
        self.assertIn('\\b', got)            # backslash-b survives
        self.assertNotEqual(got, 'foob')     # NOT semantic-unescaped
        assert_grounded(self, _abs_bind(r'foo\b', r'rg "foo\b" f', "the symbol foo\\b is absent"))

    def test_launder_narrow_strawman_still_ungrounded(self):
        # #3: a genuinely-empty NARROW strawman search can't vouch for a BROADER recorded
        # pattern — extraction recovers full patterns, but equality still UNGROUNDS it.
        assert_ungrounded(self, _abs_bind("isValidating|loading", 'rg "zzz" f',
                                          "no isValidating or loading anywhere"))

    def test_single_quoted_value_not_shell_unescaped(self):
        # #4: shell-unescape applies to DOUBLE-quoted captures ONLY. A single-quoted
        # literal stays RAW — `'a\\b'` is NOT collapsed to `a\b` (which would be a visible
        # corruption). Single quotes are a literal context in the shell.
        got = self._EX(r"rg 'a\\b' f")
        self.assertEqual(got, r'a\\b')       # two backslashes preserved
        self.assertNotEqual(got, r'a\b')     # NOT collapsed (corruption would show here)

    def test_distinct_patterns_do_not_collapse_to_equal(self):
        # #5: `foo\b` and `foob` are genuinely different regexes — they must NOT be
        # treated as equal (the metachar difference is preserved through extraction+norm).
        assert_ungrounded(self, _abs_bind(r'foo\b', 'rg "foob" f', "foo\\b is absent"))
        assert_ungrounded(self, _abs_bind('foob', r'rg "foo\b" f', "foob is absent"))

    def test_dash_e_path_also_escaped_quote_aware(self):
        # #6: the `-e PAT` capture got the same escaped-aware fix.
        got = self._EX(r'rg -e "pat\"x" f')
        self.assertEqual(got, 'pat"x')
        assert_grounded(self, _abs_bind('pat"x', r'rg -e "pat\"x" f', "the token pat\"x is absent"))


# ===========================================================================
# Fix B — classifier robustness (synthesis-taint, sed script-vs-file, quote-aware
# splitting, newline-join union). The load-bearing anti-fabrication guard is the
# STRUCTURAL taint: any synthesis segment (echo/printf/python/perl/…) anywhere in a
# pipe/compound makes the WHOLE command non-attributable, so an echo'd or python-
# printed snippet downstream of a real `cat` can never launder as read evidence.
#
# `transforming` is not an output field — it manifests as is_readlike=False +
# attributed_paths=[]; tests assert that observable effect. Gated on a runtime probe
# (structural synthesis-taint) so the suite is GREEN-by-skip until the Coder lands
# Fix B, then auto-activates.
# ===========================================================================

def _classify(inner, output="file contents"):
    return _OJ._classify_codex_cmd(inner, output)


def _structural_taint_landed():
    """True once a synthesis segment taints the whole pipe. Probe: `cat realfile |
    python -c …` must become non-read-like (today it stays readlike → fabrication hole)."""
    if _OJ is None or not hasattr(_OJ, "_classify_codex_cmd"):
        return False
    try:
        r = _OJ._classify_codex_cmd("cat realfile.py | python3 -c 'print(1)'", "1\n")
        return r.get("is_readlike") is False and r.get("attributed_paths") == []
    except Exception:
        return False


@unittest.skipUnless(_structural_taint_landed(),
                     "blocked: Fix B classifier (structural synthesis-taint) not landed yet")
class TestClassifierSynthesisTaint(unittest.TestCase):
    # ---- A: read-then-synthesis taints the WHOLE pipe (override survives read-union) ----
    def test_cat_then_python_compound_tainted(self):
        # `&&` compound: a real cat read followed by python synthesis → non-attributable.
        r = _classify('cat realfile.py && python -c "print(open(\'x\').read())"', "synth\n")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])

    def test_nl_then_perl_semicolon_tainted(self):
        r = _classify("nl realfile.py ; perl -e 'print \"X\"'", "X")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])

    def test_cat_piped_into_printf_tainted(self):
        r = _classify("cat realfile.py | printf 'X'", "X")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])

    # ---- B: the taint covers the whole synthesis class (printf AND echo), not just python ----
    def test_cat_piped_into_printf_fabrication_tainted(self):
        r = _classify("cat realfile.py | printf 'FABRICATED'", "FABRICATED")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])

    def test_cat_piped_into_echo_fabrication_tainted(self):
        r = _classify("cat realfile.py | echo FABRICATED", "FABRICATED")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])


@unittest.skipUnless(_structural_taint_landed(),
                     "blocked: Fix B classifier (sed script-position) not landed yet")
class TestClassifierSedScriptVsFile(unittest.TestCase):
    """C: sed transform-detection must look ONLY at the SCRIPT position — never at a
    trailing file arg (a path like `…/stores/apps.ts` containing `s/` must not read
    as a substitution). Both halves locked in one place."""

    def test_sed_substitution_is_transform(self):
        # Regression guard: a genuine s/// substitution MUST stay transforming (tainted).
        r = _classify("sed 's/x/y/g' realfile.py", "mutated")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])

    def test_sed_delete_command_is_transform(self):
        # `/foo/d` in script position is a delete → transform (and `/foo/d` must NOT be
        # mis-read as a file path either).
        r = _classify("sed '/foo/d' realfile.py", "mutated")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])

    def test_sed_print_range_of_path_with_s_slash_is_read(self):
        # THE #1 target: a read of a file whose PATH contains `s/` (stores/) was being
        # false-flagged as a substitution. Print-range is a read → readlike + attributed.
        r = _classify("sed -n '200,340p' src/stores/apps.ts", "code")
        self.assertTrue(r["is_readlike"])
        self.assertIn("src/stores/apps.ts", r["attributed_paths"])

    def test_sed_print_range_go_path_with_s_slash_is_read(self):
        r = _classify("sed -n '1,80p' cmd/watson/structs/structs.go", "code")
        self.assertTrue(r["is_readlike"])
        self.assertIn("cmd/watson/structs/structs.go", r["attributed_paths"])


@unittest.skipUnless(_structural_taint_landed(),
                     "blocked: Fix B classifier (newline-join / splitter) not landed yet")
class TestClassifierSplittingAndUnion(unittest.TestCase):
    def test_newline_joined_reads_union_both_files(self):
        # D: two newline-separated read pipelines → ONE OR-combined record whose
        # read_paths unions BOTH files (neither read is dropped).
        inner = "nl -ba A.py | sed -n '1,5p'\nnl -ba B.py | sed -n '1,5p'"
        r = _classify(inner, "code")
        self.assertTrue(r["is_readlike"])
        self.assertIn("A.py", r["read_paths"])
        self.assertIn("B.py", r["read_paths"])

    def test_unbalanced_quote_fails_safe_not_silently_dropped(self):
        # E: an unbalanced-quote command must NOT silently mis-split and drop the
        # synthesis segment (which would leave cat's read attributed → fabrication).
        # Fail-safe = over-merge/taint: the python synthesis is still seen → tainted.
        r = _classify("cat realfile.py | python -c 'print(", "x")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])


@unittest.skipUnless(_structural_taint_landed(),
                     "blocked: Fix B classifier not landed yet")
class TestClassifierMatrix(unittest.TestCase):
    """Explorer's classifier matrix (verified against the live classifier on real
    pilot transcripts). Search rows use output="" so attributed_paths is
    deterministically [] (path-attribution derives from path-prefixed output lines).
    Per the scope note, rg-alternation rows assert is_search/is_readlike/attribution
    only — searched_symbols pattern-capture is deferred to a later commit."""

    def _c(self, inner, output=""):
        return _classify(inner, output)

    # ---- #1 / #1b: sed print-range of a path containing `s/` → READ (the #1 target) ----
    def test_row1_sed_print_range_stores_path_is_read(self):
        r = self._c("sed -n '200,340p' packages/advertiser/src/views/AppsAssets/stores/apps.ts")
        self.assertTrue(r["is_readlike"])
        self.assertFalse(r["is_search"])
        self.assertEqual(r["attributed_paths"],
                         ["packages/advertiser/src/views/AppsAssets/stores/apps.ts"])
        self.assertEqual(r["read_paths"],
                         ["packages/advertiser/src/views/AppsAssets/stores/apps.ts"])

    def test_row1b_sed_print_range_go_path_is_read(self):
        r = self._c("sed -n '1,80p' cmd/watson/structs/structs.go")
        self.assertTrue(r["is_readlike"])
        self.assertFalse(r["is_search"])
        self.assertEqual(r["attributed_paths"], ["cmd/watson/structs/structs.go"])

    # ---- #2: rg with escaped alternation → SEARCH (pattern-capture deferred) ----
    def test_row2_rg_alternation_is_search(self):
        r = self._c('rg -n "AdEngagementTime|AdEngagementType|EventID" cmd -S')
        self.assertTrue(r["is_search"])
        self.assertTrue(r["is_readlike"])  # search tools are read-like
        self.assertEqual(r["attributed_paths"], [])  # output="" → no path-prefixed lines

    # ---- #3: newline-joined read pipelines → union of BOTH files ----
    def test_row3_newline_joined_reads_union_both_files(self):
        inner = ("nl -ba cmd/watson/facebook/reconcile.go | sed -n '180,220p;1061,1110p'\n"
                 "nl -ba cmd/watson/structs/structs.go | sed -n '340,362p'")
        r = self._c(inner)
        self.assertTrue(r["is_readlike"])
        self.assertIn("cmd/watson/facebook/reconcile.go", r["read_paths"])
        self.assertIn("cmd/watson/structs/structs.go", r["read_paths"])

    # ---- #3b: single nl|sed read (green guard — passed before Fix B, must stay) ----
    def test_row3b_single_nl_sed_read_is_attributed(self):
        r = self._c("nl -ba cmd/watson/facebook/reconcile.go | sed -n '248,266p'")
        self.assertTrue(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], ["cmd/watson/facebook/reconcile.go"])

    # ---- #4: `pwd && rg --files` → the && must NOT hide the rg search ----
    def test_row4_pwd_and_rg_files_is_search(self):
        # Load-bearing + uncontested: is_search stays True despite the `&&` compound,
        # and attributed_paths is [] (output=""). NOTE: is_readlike is intentionally
        # NOT asserted — Explorer's table expected True, but the landed code taints it
        # to False because `pwd` is a non-pure-read/non-pure-search segment (spec rule
        # 3). Flagged to the team for reconciliation; asserting it either way here would
        # bless a contested value.
        r = self._c("pwd && rg --files packages/advertiser/src/views/AppsAssets/stores")
        self.assertTrue(r["is_search"])
        self.assertEqual(r["attributed_paths"], [])

    # ---- #5: `rg --files DIR | rg PATTERN` → still a search, && /pipe doesn't hide it ----
    def test_row5_piped_rg_is_search_readlike(self):
        r = self._c('rg --files cmd/watson | rg "reconcile_handler|fb_event_enrichment_url_test"')
        self.assertTrue(r["is_search"])
        self.assertTrue(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])

    # ---- POS / POS2 / POS3: genuine transforms stay tainted (green guards) ----
    def test_rowPOS_sed_substitution_tainted(self):
        r = self._c("sed 's/x/y/' f.txt")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])

    def test_rowPOS2_sed_delete_tainted(self):
        r = self._c("sed '3d' f.go")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])

    def test_rowPOS3_awk_action_block_tainted(self):
        r = self._c("awk '{print $1}' f.go")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])

    # ---- transforming via the segment 5-tuple (3rd element), per Explorer's note ----
    def test_segment_transforming_flag_for_pos_and_neg(self):
        seg = _OJ._classify_codex_segment
        self.assertTrue(seg("sed 's/x/y/' f.txt")[2])      # POS substitution
        self.assertTrue(seg("sed '3d' f.go")[2])           # POS2 delete
        self.assertTrue(seg("awk '{print $1}' f.go")[2])   # POS3 awk block
        self.assertFalse(seg("cat realfile.py")[2])        # pure read → not transforming


# Case A (absence, real pilot data). Previously deferred: the alternation pattern's
# embedded quote defeated extraction. The embedded-quote extraction fix (escaped-aware
# `\"` capture + minimal shell-unescape) now recovers the full pattern, so this binds —
# un-gated to a permanent regression test. KEY: codex emits the embedded quote ESCAPED
# (`loading=\"`), which the recorded grep_pattern carries as a literal `"`; the command
# below uses that real escaped shell form (NOT a bare quote, which would close the arg).
_CASE_A_PATTERN = 'isValidating|:loading=|loading="|loading\\b'
_CASE_A_CMD_ARG = _CASE_A_PATTERN.replace('"', '\\"')  # shell-escape the embedded quote
_CASE_A_FILES = ("packages/advertiser/src/views/AppsAssets/AppsV2/AddAppModalV2/Step2AppDetails/"
                 "Step2AppDetails.vue packages/advertiser/src/views/AppsAssets/AppsV2/components/"
                 "SettingsComponents/AppConfigurations/GeneralSectionV2/GeneralSectionV2.vue")
_CASE_A_CLAIM = "There is no loading indicator or loading state wired for the async validation UI."


class TestBindingIntegrationAbsence(unittest.TestCase):
    def test_real_pilot_absence_binds_end_to_end(self):
        # Real pilot: an emitted empty alternation search (embedded quote escaped as `\"`)
        # over two .vue files backs the absence claim end-to-end (claim-tie + escaped-aware
        # pattern-equality) → BINDS.
        verdict = {"review_a": {"claim_trace": [{
            "claim_text": _CASE_A_CLAIM, "outcome": "verified",
            "evidence": {"grep_pattern": _CASE_A_PATTERN, "grep_scope": _CASE_A_FILES,
                         "result": "absent"}}]}, "review_b": {}}
        trace = _trace_from(_codex_cmd_event(f'rg -n "{_CASE_A_CMD_ARG}" {_CASE_A_FILES}', "", 1))
        with tempfile.TemporaryDirectory() as d:
            _OJ._check_evidence_binding_codex(verdict, trace, d)
        assert_grounded(self, verdict)  # soft-signal: real backing → grounded


@unittest.skipUnless(_structural_taint_landed(),
                     "blocked: Fix B classifier (#1 sed-read of /stores/ path) not landed yet")
class TestBindingIntegrationContent(unittest.TestCase):
    """Case B (#1-only single-dependency, real frontend-mos-1816 pilot data). A content
    claim cites a snippet from a file whose path contains `/stores/`. PRE-#1-fix the
    `s/` in `stores/` mis-flagged the `sed -n` read as a substitution → tainted → the
    snippet couldn't bind; POST-#1 the read is attributed and the snippet binds. RAW
    sed output (no nl prefix), single-line snippet → depends ONLY on #1, no de-prefix /
    multiline whitespace risk."""

    _SNIPPET = "    const hasValidAppleAppID = Number.isFinite(appleAppID) && appleAppID > 0;"
    _OUTPUT = ("        const appleAppID = Number(appData.app_store_id);\n"
               "    const hasValidAppleAppID = Number.isFinite(appleAppID) && appleAppID > 0;\n"
               "        const shouldAutoEnable = hasValidAppleAppID && appData.skan_tracking_enabled;\n")
    _FILE = "packages/advertiser/src/views/AppsAssets/stores/apps.ts"

    def test_real_pilot_content_snippet_binds_via_sed_read_of_stores_path(self):
        verdict = {"review_a": {"claim_trace": [{
            "claim_text": "`Number(appData.app_store_id)` can produce `NaN`, and the backend "
                          "will receive `apple_app_id: NaN` in the POST body.",
            "outcome": "contradicted",
            "evidence": {"file": self._FILE, "line": 237, "snippet": self._SNIPPET}}]},
            "review_b": {}}
        trace = _trace_from(_codex_cmd_event(f"sed -n '200,340p' {self._FILE}", self._OUTPUT, 0))
        with tempfile.TemporaryDirectory() as d:
            _OJ._check_evidence_binding_codex(verdict, trace, d)
        assert_grounded(self, verdict)  # read attributed → snippet grounded

    def test_snippet_ungrounded_when_read_attributed_to_different_file(self):
        # Teeth: the SAME snippet+claim against a sed read of a DIFFERENT file → the
        # cited file isn't in attributed_paths → UNGROUNDED (real attribution, not a
        # trivially-true substring match).
        verdict = {"review_a": {"claim_trace": [{
            "claim_text": "x", "outcome": "contradicted",
            "evidence": {"file": self._FILE, "line": 237, "snippet": self._SNIPPET}}]},
            "review_b": {}}
        trace = _trace_from(_codex_cmd_event("sed -n '1,50p' some/other/file.ts", self._OUTPUT, 0))
        with tempfile.TemporaryDirectory() as d:
            _OJ._check_evidence_binding_codex(verdict, trace, d)
        assert_ungrounded(self, verdict)


# ===========================================================================
# A+B review refinements — Q1 (nav-neutral) + Fix A (extraction value-flags) +
# Bug#2 (command-based de-prefix gate). The nav-EXEMPTION boundary and the Bug#2
# command-gate are the load-bearing anti-fabrication guards: they must rescue the
# benign-prefix reads WITHOUT re-opening any synthesis/TSV fabrication path.
# ===========================================================================

class TestClassifierNavNeutral(unittest.TestCase):
    """Q1: inert nav prefixes {cd, pwd, true, :} are NEUTRAL — they don't taint a real
    read in the same compound (rescuing the ~20 pilot reads shaped `pwd && cat …`).
    The exemption is TIGHT: ls/echo/printf/python/perl/unknown still taint."""

    def test_pwd_then_cat_stays_readlike(self):
        r = _classify("pwd && cat file.go", "package main\n")
        self.assertTrue(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], ["file.go"])

    def test_cd_then_sed_print_stays_readlike(self):
        r = _classify("cd dir && sed -n '10,40p' f.go", "func main() {}\n")
        self.assertTrue(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], ["f.go"])

    def test_echo_after_read_NOT_exempt_taints(self):
        # ANTI-FAB: echo is synthesis — `cat realfile && echo FABRICATED` must taint so
        # the echo'd text can never bind as read evidence.
        r = _classify("cat realfile.go && echo FABRICATED", "FABRICATED")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])

    def test_ls_NOT_exempt_taints(self):
        # ANTI-FAB: ls is EXCLUDED from the nav exemption (it lists, doesn't faithfully
        # read file content) → `ls && cat foo.go` taints.
        r = _classify("ls && cat foo.go", "foo.go\nbar.go\n")
        self.assertFalse(r["is_readlike"])
        self.assertEqual(r["attributed_paths"], [])


class TestFixASearchPatternExtraction(unittest.TestCase):
    """Fix A: `_extract_search_pattern_arg` skips value-carrying flags (`--glob`/`-g`/
    `--type`/`-f`/…) so the FIRST quoted token isn't mistaken for the pattern when it's
    actually a flag's value."""

    def test_glob_value_skipped_real_pattern_extracted(self):
        self.assertEqual(
            _OJ._extract_search_pattern_arg("rg --glob '*.ts' 'myPattern' ."), "myPattern")

    def test_absence_binds_when_pattern_follows_glob_flag(self):
        # End-to-end: an empty `rg --glob '*.ts' "myPattern"` backs an absence claim for
        # myPattern — extraction skips the glob value `*.ts` and matches `myPattern`.
        claim = "the symbol myPattern does not exist in any .ts file"
        verdict = _absence_verdict("myPattern", claim)
        trace = _trace_from(_codex_cmd_event("rg --glob '*.ts' \"myPattern\" .", "", 1))
        with tempfile.TemporaryDirectory() as d:
            _OJ._check_evidence_binding_codex(verdict, trace, d)  # no raise

    def test_alternation_pattern_still_extracts_and_binds(self):
        # Regression: the escaped-alternation pattern (no flag confusion) still extracts.
        pattern = r"isValidating|:loading=|loading\b"
        claim = "PR adds no loading state — isValidating is never set"
        trace = _trace_from(_codex_cmd_event('rg -n "' + pattern + '" src/', "", 1))
        with tempfile.TemporaryDirectory() as d:
            _OJ._check_evidence_binding_codex(_absence_verdict(pattern, claim), trace, d)  # no raise


def _bug2_command_gated():
    """True once Bug#2 de-prefix keys off the READ TOOL (nl/cat -n/bat --number), not
    the output shape. Probe: a `sed -n` read of TSV-shaped output (`42\\tvalue…`, which
    LOOKS uniformly numbered) must NOT de-prefix → the fabricated multi-line `value\\nbeta`
    is REJECTED. Under the old output-shape gate it (wrongly) binds → probe False → skip."""
    if _OJ is None or not hasattr(_OJ, "_content_snippet_bound"):
        return False
    cmd = {"cmd": "sed -n '1,5p' data.tsv", "is_readlike": True, "is_search": False,
           "output": "42\tvalue\n99\tbeta\n", "attributed_paths": ["data.tsv"]}
    try:
        return _OJ._content_snippet_bound("data.tsv", "value\nbeta", cmd) is False
    except Exception:
        return False


@unittest.skipUnless(_bug2_command_gated(),
                     "blocked: Bug#2 command-based de-prefix gate not landed yet")
class TestBug2CommandBasedDeprefixGate(unittest.TestCase):
    def _csb(self, cited, snippet, output, cmd, **kw):
        d = {"cmd": cmd, "is_readlike": True, "is_search": False, "output": output,
             "attributed_paths": [cited]}
        d.update(kw)
        return _OJ._content_snippet_bound(cited, snippet, d)

    def test_sed_tsv_numbered_looking_output_NOT_deprefixed(self):
        # THE critical TSV anti-fab guard: a `sed -n` dump of a .tsv whose data lines are
        # `<digits>\t<value>` LOOKS uniformly numbered, but sed is NOT a numbering tool →
        # no de-prefix → the fabricated `value\nbeta` (only matchable de-prefixed) REJECTED.
        out = "42\tvalue\n99\tbeta\n"
        self.assertNotIn("value\nbeta", out)
        self.assertFalse(self._csb("data.tsv", "value\nbeta", out, "sed -n '1,5p' data.tsv"))

    def test_plain_cat_without_n_flag_NOT_deprefixed(self):
        # Command-based distinction: `cat f` (no -n) is NOT a numbering read, so numbered-
        # LOOKING output is not de-prefixed → a cross-line fabrication can't bind.
        out = "1\talpha\n2\tbeta\n"
        self.assertFalse(self._csb("f.txt", "alpha\nbeta", out, "cat f.txt"))

    def test_nl_multiline_still_binds_under_command_gate(self):
        # The numbering path still works: `nl -ba` read → de-prefix → multi-line binds.
        out = "    10\t    def get(self):\n    11\t        return self._v\n"
        snippet = "    def get(self):\n        return self._v"
        self.assertNotIn(snippet, out)
        self.assertTrue(self._csb("c.py", snippet, out, "nl -ba c.py"))

    def test_cat_n_multiline_still_binds_under_command_gate(self):
        out = "     1\timport os\n     2\tCACHE = {}\n"
        self.assertTrue(self._csb("c.py", "import os\nCACHE = {}", out, "cat -n c.py"))


@unittest.skipUnless(_HAS_GIT, "git not available")
class TestCodexCommandCountAndG4(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.wt = build_fixture_repo(os.path.join(self.tmp, "repo"))

    @needs("count_emitted_codex_commands")
    def test_command_count_feeds_confab(self):
        t = _trace(_cmd(cmd="rg foo ."), _cmd(cmd="cat src/cache.py"))
        cmds = _OJ.count_emitted_codex_commands(t)
        self.assertEqual(len(cmds), 2)
        self.assertEqual(cmds, ["rg foo .", "cat src/cache.py"])

    @needs("_out_of_worktree_reads")
    def test_g4_flags_reads_outside_worktree(self):
        t = _trace(_cmd(read_paths=["src/cache.py", "/etc/shadow"]),
                   _cmd(read_paths=["../../../../etc/passwd"]))
        flagged = _OJ._out_of_worktree_reads(t, self.wt)
        self.assertEqual(set(flagged), {"/etc/shadow", "../../../../etc/passwd"})
        self.assertNotIn("src/cache.py", flagged)  # inside the worktree → not flagged


def _jsonl_cmd(inner, output="data", exit_code=0):
    """One command_execution JSONL line (for driving _run_and_verify_codex via a
    mocked run_codex_exec → a real _parse_codex_trace pass)."""
    import json
    q = '"' if "'" in inner else "'"
    return json.dumps({"type": "item.completed", "item": {
        "type": "command_execution", "id": "c",
        "command": f"/bin/zsh -lc {q}{inner}{q}",
        "aggregated_output": output, "exit_code": exit_code, "status": "completed"}})


@unittest.skipUnless(_HAS_GIT, "git not available")
class TestG4OutOfWorktreeQuarantine(unittest.TestCase):
    """C4 G4(3): a run that read OUTSIDE the worktree is recorded under `flag`
    (default, still scored) and QUARANTINED under `fail` (untrusted PRs)."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.wt = build_fixture_repo(os.path.join(self.tmp, "repo"))

    def _run(self, mode):
        # Trace reads an absolute path OUTSIDE the worktree → flagged by G4. Clean
        # zero-findings verdict so binding passes and the run reaches the G4 decision.
        trace = _jsonl_cmd("cat /etc/hosts", output="127.0.0.1 localhost")
        task = _codex_task(self.tmp, repo_path=self.wt, on_out_of_worktree_read=mode)
        with _mock.patch.object(_OJ, "run_codex_exec", return_value=(_parsed(verified=0), trace)):
            return _OJ.run_single_judge_task(task)

    @needs("run_single_judge_task", "run_codex_exec")
    def test_flag_mode_records_but_still_scores(self):
        out = self._run("flag")
        self.assertEqual(out["status"], "ok")  # default: recorded but still scored
        self.assertIn("/etc/hosts", out["run_record"]["out_of_worktree_reads"])

    @needs("run_single_judge_task", "run_codex_exec")
    def test_fail_mode_quarantines_the_run(self):
        out = self._run("fail")
        self.assertEqual(out["status"], "failed")
        self.assertIn("OutOfWorktreeRead", out["error"])
        self.assertEqual(out["counter_increment"], "out_of_worktree_read_quarantines")


# ===========================================================================
# #15–#18 — A/B counterbalancing, paired judging, unswap, median vote
# ===========================================================================

class TestPureScoringParity(unittest.TestCase):
    @needs("derive_ab_assignments")
    def test_ab_counterbalanced_and_deterministic(self):
        # #15: run1 = (skwad,ci), run2 = (ci,skwad); run3 seeded-deterministic.
        a = _OJ.derive_ab_assignments(42)
        self.assertEqual(a[0], ("skwad", "claude_ci"))
        self.assertEqual(a[1], ("claude_ci", "skwad"))
        self.assertEqual(_OJ.derive_ab_assignments(42), a)

    @needs("_unswap")
    def test_unswap_maps_ab_to_named_systems(self):
        parsed = {"review_a": {"total": 5}, "review_b": {"total": 8}}
        out = _OJ._unswap(parsed, "skwad", "claude_ci")
        self.assertEqual(out["skwad"]["total"], 5)
        self.assertEqual(out["claude_ci"]["total"], 8)

    @needs("_median_vote")
    def test_median_low_voting(self):
        # #18: median_low([1,2,3]) == 2 per criterion.
        runs = [_resolved(1, 0), _resolved(2, 0), _resolved(3, 0)]
        voted = _OJ._median_vote(runs)
        for c in _criteria_list():
            self.assertEqual(voted["skwad"][c]["voted"], 2)

    @needs("_median_vote")
    def test_median_low_breaks_even_ties_low(self):
        runs = [_resolved(0, 0), _resolved(3, 0)]
        voted = _OJ._median_vote(runs)
        for c in _criteria_list():
            self.assertEqual(voted["skwad"][c]["voted"], 0)


# ===========================================================================
# #21 / #23–#25 — Finalize, per-task + orchestrator crash isolation
# ===========================================================================

class TestFinalizeAndIsolation(unittest.TestCase):
    @needs("finalize_pr_runs", "derive_ab_assignments")
    def test_finalize_on_remaining_runs_when_one_crashes(self):
        # #24: a crashed run → PR still finalizes on the remaining (e.g. 2/3).
        assign = _OJ.derive_ab_assignments(42)
        ok = lambda i, r: {  # noqa: E731
            "pr_number": 7, "run_index": i, "ab_assignment": ["skwad", "claude_ci"],
            "status": "ok", "resolved": r,
            "run_record": {"run": i, "ab_assignment": ["skwad", "claude_ci"],
                           "raw_response": {}, "resolved": r, "stderr_meta": {},
                           "duration_seconds": 1.0},
            "canary_outcomes": [], "counter_increment": None,
            "duration_seconds": 1.0, "error": None,
        }
        failed = {"pr_number": 7, "run_index": 2, "ab_assignment": ["skwad", "claude_ci"],
                  "status": "failed", "resolved": None, "run_record": None,
                  "canary_outcomes": [], "counter_increment": None,
                  "duration_seconds": 0.5, "error": "boom"}
        runs = [ok(1, _resolved(2, 2)), failed, ok(3, _resolved(2, 2))]
        with tempfile.TemporaryDirectory() as d:
            result = _OJ.finalize_pr_runs(7, runs, assign, d)
        self.assertEqual(result["n_runs_completed"], 2)
        self.assertEqual(result["n_runs_planned"], 3)

    @needs("finalize_pr_runs", "derive_ab_assignments")
    def test_all_runs_failed_raises(self):
        # #21: raise if all runs failed.
        assign = _OJ.derive_ab_assignments(42)
        failed = [{"pr_number": 7, "run_index": i, "status": "failed", "resolved": None,
                   "run_record": None, "canary_outcomes": [], "counter_increment": None,
                   "duration_seconds": 0.1, "error": "x", "ab_assignment": ["skwad", "claude_ci"]}
                  for i in (1, 2, 3)]
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaises(RuntimeError):
                _OJ.finalize_pr_runs(7, failed, assign, d)


# ===========================================================================
# #29 — Diff truncation (32k cap retained initially)
# ===========================================================================

class TestDiffTruncation(unittest.TestCase):
    @needs("_truncate_diff", "DIFF_TRUNCATION_CAP")
    def test_big_diff_truncated(self):
        big = "x" * (_OJ.DIFF_TRUNCATION_CAP + 1)
        out = _OJ._truncate_diff(big)
        self.assertIn("truncated", out)

    @needs("_truncate_diff", "DIFF_TRUNCATION_CAP")
    def test_short_diff_untouched(self):
        out = _OJ._truncate_diff("+a\n")
        self.assertNotIn("truncated", out)


# ===========================================================================
# #32 — Canary detection end-to-end on the parsed verdict + claim_trace
# ===========================================================================

class TestCanaryOutcomes(unittest.TestCase):
    @needs("_check_canary_outcomes")
    def test_contradicted_canary_passes_when_judge_contradicts(self):
        canaries = load_fixture_canaries()
        read_canary = next(c for c in canaries if c["category"] == "contradicted_via_read")
        # Judge correctly marked the injected claim contradicted in skwad's trace (review_a).
        parsed = {"review_a": {"claim_trace": [{
            "claim_text": read_canary["claim_text"], "outcome": "contradicted",
            "tools_used": ["read"], "evidence": "MAX_RETRIES = 3"}]},
            "review_b": {}}
        outcomes = _OJ._check_canary_outcomes(
            parsed, [read_canary], "skwad", "claude_ci", {"pr_number": 1})
        self.assertEqual(len(outcomes), 1)
        self.assertTrue(outcomes[0]["passed"])

    @needs("_check_canary_outcomes")
    def test_verified_true_positive_canary_passes_when_judge_verifies(self):
        canaries = load_fixture_canaries()
        tp = next(c for c in canaries if c["category"] == "verified_true_positive")
        # TRUE canary injected into claude_ci (review_a here) and judge verified it.
        parsed = {"review_a": {"claim_trace": [{
            "claim_text": tp["claim_text"], "outcome": "verified",
            "tools_used": ["read"], "evidence": "TOKEN_TTL_SECONDS"}]},
            "review_b": {}}
        outcomes = _OJ._check_canary_outcomes(
            parsed, [tp], "claude_ci", "skwad", {"pr_number": 1})
        self.assertTrue(outcomes[0]["passed"])

    @needs("_check_canary_outcomes")
    def test_judge_miss_fails_canary(self):
        # If the judge marks a fabricated claim "verified", the canary FAILS (no masking).
        canaries = load_fixture_canaries()
        grep_canary = next(c for c in canaries if c["category"] == "contradicted_via_grep")
        parsed = {"review_a": {"claim_trace": [{
            "claim_text": grep_canary["claim_text"], "outcome": "verified",
            "tools_used": [], "evidence": ""}]},
            "review_b": {}}
        outcomes = _OJ._check_canary_outcomes(
            parsed, [grep_canary], "skwad", "claude_ci", {"pr_number": 1})
        self.assertFalse(outcomes[0]["passed"])


# ===========================================================================
# #32 (hardening) — Containment-anchored canary disambiguation.
#
# The model paraphrases the injected claim, so a canary's match_token can hit
# SEVERAL claim_trace entries. The OLD matcher bound to the FIRST token match,
# which (a) could land on a divergent-outcome sibling and FAIL a canary the judge
# actually caught (LRU bug), and (b) far worse — could land on an unrelated
# sibling whose outcome happened to equal expected and silently mask a real judge
# miss as a PASS. The new matcher selects by: containment anchor → max
# SequenceMatcher ratio (≥ _CANARY_SIMILARITY_FLOOR) → earliest trace order on
# ties, NEVER consulting expected_outcome. These guards are non-negotiable.
# ===========================================================================

def _disambig_canary(claim_text, expected_outcome, *, match_token="LRUCache.evict",
                     cid="canary_disambig"):
    return {"id": cid, "target_pr": {"pr": "*"}, "inject_into": "skwad",
            "claim_text": claim_text, "match_token": match_token,
            "expected_outcome": expected_outcome, "rationale": "disambig regression"}


def _entry(claim_text, outcome):
    return {"claim_text": claim_text, "outcome": outcome,
            "tools_used": ["read"], "evidence": "x"}


def _run_canary(canary, trace_entries):
    # inject_into="skwad" + a_system="skwad" → trace lives in review_a.
    parsed = {"review_a": {"claim_trace": trace_entries}, "review_b": {}}
    return _OJ._check_canary_outcomes(
        parsed, [canary], "skwad", "claude_ci", {"pr_number": 1})[0]


class TestCanaryDisambiguation(unittest.TestCase):
    # Distinctive, verbatim-injected canary claim shared by the multi-entry cases.
    _LRU_CLAIM = ("LRUCache.evict in src/cache.py uses FIFO ordering and ignores "
                  "access recency, evicting frequently-read keys prematurely.")

    @needs("_check_canary_outcomes", "_CANARY_SIMILARITY_FLOOR")
    def test_similarity_floor_constant_is_point_six(self):
        # Validity-critical constant: a silent change to the floor reshapes which
        # near-misses bind. Encode it so the change is caught (margins elsewhere
        # keep the behavior tests off the precise float).
        self.assertEqual(_OJ._CANARY_SIMILARITY_FLOOR, 0.6)

    @needs("_check_canary_outcomes")
    def test_lru_shape_picks_injected_contradicted_not_first_verified_sibling(self):
        # GUARD 1 (the actual bug): a verified "popitem" sibling appears FIRST, the
        # injected verbatim contradicted claim SECOND — both carry the match_token.
        # Old first-match recorded `verified` (canary wrongly FAILED). Containment
        # must select the verbatim entry → `contradicted` → canary PASSES.
        canary = _disambig_canary(self._LRU_CLAIM, "contradicted")
        out = _run_canary(canary, [
            _entry("LRUCache.evict drops the front entry via popitem, not access-ordered.",
                   "verified"),                       # token-sharing sibling, FIRST
            _entry(self._LRU_CLAIM, "contradicted"),  # the injected canary, verbatim
        ])
        self.assertEqual(out["actual_outcome"], "contradicted")
        self.assertTrue(out["passed"])

    @needs("_check_canary_outcomes")
    def test_no_injected_entry_does_not_false_pass_on_token_sibling(self):
        # GUARD 2 (non-negotiable): NONE of the token-sharing entries is the injected
        # claim (judge missed it), and one sibling's outcome == expected. A first-match
        # (or expected-biased) matcher would rubber-stamp that sibling → false PASS,
        # masking a real miss. Below the floor → no outcome bound → canary FAILS.
        canary = _disambig_canary(self._LRU_CLAIM, "contradicted")
        out = _run_canary(canary, [
            _entry("LRUCache.evict is missing a docstring.", "contradicted"),  # == expected, BAIT
            _entry("LRUCache.evict could be renamed for clarity.", "unverified"),
        ])
        self.assertIsNone(out["actual_outcome"])
        self.assertFalse(out["passed"])

    @needs("_check_canary_outcomes")
    def test_embedded_echo_wins_over_shorter_divergent_sibling(self):
        # GUARD 3: the verbatim claim is EMBEDDED in a longer entry (commentary around
        # it), alongside a SHORTER near-twin sibling with a divergent outcome and a
        # HIGHER raw ratio. Containment must anchor to the embedded echo regardless of
        # ratio → records its `contradicted` outcome, not the length-fooled sibling.
        canary = _disambig_canary(self._LRU_CLAIM, "contradicted")
        embedded = ("After reading src/cache.py I confirm: " + self._LRU_CLAIM +
                    " This is refuted by move_to_end in get(); extensive notes follow "
                    "to dilute the overall similarity ratio well below the near-twin.")
        near_twin = ("LRUCache.evict in src/cache.py uses LIFO ordering and ignores "
                     "access recency, evicting frequently-read keys prematurely.")  # FIFO→LIFO
        out = _run_canary(canary, [
            _entry(near_twin, "verified"),    # high ratio, NO verbatim containment
            _entry(embedded, "contradicted"),  # buried verbatim claim → containment anchor
        ])
        self.assertEqual(out["actual_outcome"], "contradicted")
        self.assertTrue(out["passed"])

    @needs("_check_canary_outcomes")
    def test_outcome_blind_tie_records_earliest_even_when_wrong_for_pass(self):
        # GUARD 4: an EXACT similarity tie (two identical paraphrases, neither a
        # verbatim containment) where the EARLIEST in trace order has the outcome that
        # does NOT match expected. Selection must consult only trace order — recording
        # the wrong-for-pass `verified` outcome, proving zero bias toward expected.
        canary = _disambig_canary(self._LRU_CLAIM, "contradicted")
        # ordering→order: ≥floor similar, but NOT a verbatim substring of the claim.
        paraphrase = ("LRUCache.evict in src/cache.py uses FIFO order and ignores "
                      "access recency, evicting frequently-read keys prematurely.")
        out = _run_canary(canary, [
            _entry(paraphrase, "verified"),      # EARLIEST → must win the tie
            _entry(paraphrase, "contradicted"),  # would PASS — must NOT be preferred
        ])
        self.assertEqual(out["actual_outcome"], "verified")
        self.assertNotEqual(out["actual_outcome"], canary["expected_outcome"])
        self.assertFalse(out["passed"])


class TestCanarySingleOutcomePreserved(unittest.TestCase):
    """GUARD 5: the original single-outcome absence canaries (one matching trace
    entry, outcome == expected) still bind and PASS unchanged under the new matcher
    (the len(contained)==1 auto-accept path)."""

    @needs("_check_canary_outcomes")
    def test_grep_absent_processbatchscenarios_still_passes(self):
        canaries = load_fixture_canaries()
        grep_canary = next(c for c in canaries if c["category"] == "contradicted_via_grep")
        out = _run_canary(grep_canary, [
            _entry(grep_canary["claim_text"], "contradicted")])  # judge caught it
        self.assertEqual(out["actual_outcome"], "contradicted")
        self.assertTrue(out["passed"])

    @needs("_check_canary_outcomes")
    def test_glob_absent_payment_py_still_passes(self):
        canaries = load_fixture_canaries()
        glob_canary = next(c for c in canaries if c["category"] == "contradicted_via_glob")
        out = _run_canary(glob_canary, [
            _entry(glob_canary["claim_text"], "contradicted")])
        self.assertEqual(out["actual_outcome"], "contradicted")
        self.assertTrue(out["passed"])


# ===========================================================================
# Phase 3(a) — fetch_pr_metadata now requests headRefOid (#30 SHA capture)
# ===========================================================================

class TestFetchPRMetadataArgs(unittest.TestCase):
    @unittest.skipIf(_PF is None, "blocked: eval.lib.pr_fetcher not importable")
    def test_headrefoid_in_requested_json_fields(self):
        import unittest.mock as mock
        with mock.patch.object(_PF, "run_gh", return_value="{}") as m:
            _PF.fetch_pr_metadata("owner/repo", 5)
        argv = " ".join(m.call_args.args[0])
        self.assertIn("headRefOid", argv, "PR head SHA must be captured from gh")


class TestCloneRepoReuseDetachedSafe(unittest.TestCase):
    """#30: reusing an existing clone must `git fetch` (detached-HEAD-safe), not
    `git pull` (which fails on a detached HEAD and could move the shared tree)."""

    @unittest.skipIf(_PF is None, "blocked: eval.lib.pr_fetcher not importable")
    def test_existing_clone_fetches_not_pulls(self):
        import unittest.mock as mock
        from pathlib import Path
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / "myrepo" / ".git").mkdir(parents=True)
            with mock.patch.object(_PF, "REPOS_DIR", Path(d)), \
                 mock.patch.object(_PF.subprocess, "run") as m:
                _PF.clone_repo_ssh("git@github.com:owner/myrepo.git")
            cmds = [" ".join(c.args[0]) for c in m.call_args_list]
            self.assertTrue(any("git fetch" in c for c in cmds), f"expected git fetch, got {cmds}")
            self.assertFalse(any("git pull" in c for c in cmds),
                             "must NOT git pull on reuse (detached-HEAD-unsafe)")


# ===========================================================================
# #30 / M2 / Phase 3 — PR checkout fix + per-PR isolation (no cross-PR contam)
# ===========================================================================
# FINAL contract (signature B, Manager-endorsed): prepare_pr_checkout(pr_data, dest,
#   *, fetcher=None) -> str | None. Reads pr_data["clone_path"] + ["pr_number"]; the
#   default fetcher runs `git fetch origin pull/<n>/head` + `rev-parse FETCH_HEAD`;
#   adds a detached `git worktree` at dest off the shared object store. Returns the
#   40-char SHA, or None on a deleted/missing head (NO PRCheckoutError — that type
#   was dropped; None matches the existing prepare_pr skip pattern).
#
# NOTE ON STRENGTH: isolation here is by CONSTRUCTION (each PR gets its own
# worktree). The headline assertion is therefore DETERMINISTIC, not probabilistic
# — N worktrees coexisting at N distinct SHAs simultaneously is something a single
# shared working tree (the old race) physically cannot produce. The concurrency
# test below is supplementary read-stress, not the primary proof.

@unittest.skipUnless(_HAS_GIT, "git not available")
class TestPerPRCheckoutIsolation(unittest.TestCase):
    """Contract: prepare_pr_checkout(pr_data, dest, *, fetcher=None) -> str | None.
    `pr_data` carries clone_path (base, shared object store) + pr_number; `fetcher`
    is an injectable (pr_data)->sha|None; returns the checked-out SHA, or None on a
    deleted/missing head (caller skips+records — never crashes)."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)

    def _base_repo(self, pr_numbers):
        """A git repo serving as the base clone, holding one commit per PR (each
        SHA reachable in its object store). Returns (manifest, base_path)."""
        base = os.path.join(self.tmp, "base")
        manifest = build_multi_sha_repo(base, pr_numbers)
        return manifest, base

    @staticmethod
    def _sha_fetcher(manifest):
        return lambda pr_data: manifest[pr_data["pr_number"]]["sha"]

    @needs("prepare_pr_checkout")
    def test_worktrees_coexist_at_their_own_shas(self):
        # DETERMINISTIC core proof: prepare 3 PRs, then assert all three working
        # trees exist AT THE SAME TIME, each at its own SHA + marker. A single
        # shared working tree (the old race) could never hold 3 SHAs at once.
        manifest, base = self._base_repo([1, 2, 3])
        fetch = self._sha_fetcher(manifest)
        dests, shas = {}, {}
        for n in (1, 2, 3):
            dests[n] = os.path.join(self.tmp, f"wt_{n}")
            shas[n] = _OJ.prepare_pr_checkout(
                {"clone_path": base, "pr_number": n}, dests[n], fetcher=fetch)
        for n in (1, 2, 3):
            self.assertEqual(shas[n], manifest[n]["sha"])
            self.assertEqual(head_sha(dests[n]), manifest[n]["sha"])
            self.assertEqual(checkout_marker(dests[n]), manifest[n]["marker"])
        self.assertEqual(len(set(shas.values())), 3)

    @needs("prepare_pr_checkout")
    def test_returns_authoritative_sha(self):
        # #30: returns the authoritative checked-out SHA (40-hex), not "".
        manifest, base = self._base_repo([1])
        sha = _OJ.prepare_pr_checkout(
            {"clone_path": base, "pr_number": 1}, os.path.join(self.tmp, "wt_1"),
            fetcher=self._sha_fetcher(manifest))
        self.assertRegex(sha, r"^[0-9a-f]{40}$")
        self.assertEqual(sha, manifest[1]["sha"])

    @needs("prepare_pr_checkout")
    def test_default_fetcher_resolves_real_pull_ref(self):
        # Exercises the PRODUCTION default fetcher: git fetch origin pull/<n>/head
        # + rev-parse FETCH_HEAD (no injected fetcher), against a local origin.
        import subprocess
        remote = os.path.join(self.tmp, "remote")
        manifest = build_multi_sha_repo(remote, [1, 2])
        add_pull_refs(remote, manifest)
        base = os.path.join(self.tmp, "base")
        subprocess.run(["git", "clone", "-q", remote, base],
                       check=True, capture_output=True, text=True)
        dest = os.path.join(self.tmp, "wt_2")
        sha = _OJ.prepare_pr_checkout({"clone_path": base, "pr_number": 2}, dest)
        self.assertEqual(sha, manifest[2]["sha"])
        self.assertEqual(checkout_marker(dest), manifest[2]["marker"])

    @needs("prepare_pr_checkout")
    def test_deleted_fork_head_returns_none(self):
        # M2: an unfetchable head → None (caller skips+records), NOT a crash.
        _, base = self._base_repo([1])
        res = _OJ.prepare_pr_checkout(
            {"clone_path": base, "pr_number": 999}, os.path.join(self.tmp, "wt_x"),
            fetcher=lambda pr_data: None)
        self.assertIsNone(res)

    @needs("prepare_pr_checkout")
    def test_concurrent_readers_never_cross_contaminate(self):
        # Supplementary read-stress: parallel judges reading their OWN isolated
        # worktree must always see their own marker (never a sibling's).
        import concurrent.futures
        manifest, base = self._base_repo([1, 2])
        fetch = self._sha_fetcher(manifest)
        dests = {}
        for n in (1, 2):
            dests[n] = os.path.join(self.tmp, f"wt_{n}")
            _OJ.prepare_pr_checkout({"clone_path": base, "pr_number": n}, dests[n], fetcher=fetch)

        def _read(n):
            return all(checkout_marker(dests[n]) == manifest[n]["marker"] for _ in range(50))

        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as ex:
            results = list(ex.map(_read, [1, 2] * 20))
        self.assertTrue(all(results), "a reader saw another PR's checkout — contamination")


# ===========================================================================
# #27/#28 + Phase 4 — system prompt verbatim + ADDITIVE evidence-binding addendum
# ===========================================================================
# User-approved deviation: evidence-binding needs claim_trace.evidence as a
# {file,line,snippet} OBJECT (the verbatim persona has it as a string). The fix is
# ADDITIVE — the full persona stays verbatim and an evidence-binding addendum is
# appended. These guards prove the deviation is additive + bounded, not silent
# drift: the persona text (incl. every rubric anchor/criterion) survives untouched.

def _raw_persona():
    import json
    return json.loads(Path(_OJ.JUDGE_CONFIG).read_text())["agents"][0]["persona_instructions"]


class TestSystemPromptVerbatimAndAddendum(unittest.TestCase):
    @needs("load_system_prompt", "JUDGE_CONFIG")
    def test_load_system_prompt_is_verbatim_persona(self):
        # #27: load_system_prompt() is the persona_instructions VERBATIM — the
        # untouched source the addendum is layered on top of (never a rewrite).
        self.assertEqual(_OJ.load_system_prompt().strip(), _raw_persona().strip())

    @needs("load_system_prompt", "JUDGE_CONFIG", "CRITERIA")
    def test_no_rubric_criterion_text_mutated(self):
        # Small explicit guard: every scored criterion name still appears in the
        # verbatim persona — the addendum changed nothing in the rubric/score rules.
        prompt = _OJ.load_system_prompt()
        for crit in _OJ.CRITERIA:
            self.assertIn(crit, prompt, f"rubric criterion '{crit}' missing/mutated")

    @needs("build_system_prompt", "load_system_prompt", "JUDGE_CONFIG")
    def test_build_system_prompt_is_additive(self):
        # Phase 4: build_system_prompt() = verbatim persona + evidence-binding
        # addendum. Prove ADDITIVE: the full persona survives as a substring, AND
        # the appended delta is about {file,line,snippet} evidence binding.
        built = _OJ.build_system_prompt()
        persona = _OJ.load_system_prompt()
        self.assertIn(persona, built, "persona must survive verbatim inside build_system_prompt")
        self.assertGreater(len(built), len(persona), "addendum must add content")
        delta = built.replace(persona, "").lower()
        self.assertTrue(
            ("snippet" in delta) or ("file" in delta and "line" in delta),
            "appended addendum should describe {file,line,snippet} evidence binding")

    @needs("VERDICT_SCHEMA")
    def test_verdict_schema_requires_evidence_object(self):
        # Enforcement half of the deviation: claim_trace[].evidence anyOf has a
        # content branch {file,line,snippet} (falsifiable content claims).
        blob = str(_OJ.VERDICT_SCHEMA)
        for field in ("file", "line", "snippet"):
            self.assertIn(field, blob, f"VERDICT_SCHEMA evidence object missing '{field}'")

    @needs("VERDICT_SCHEMA")
    def test_verdict_schema_has_absence_evidence_branch(self):
        # Fix #2: the evidence anyOf gains an absence branch
        # {grep_pattern, grep_scope, result:"absent"} for grep-refuted claims.
        blob = str(_OJ.VERDICT_SCHEMA)
        self.assertIn("grep_pattern", blob)
        self.assertIn("absent", blob)

    @needs("build_system_prompt", "load_system_prompt", "JUDGE_CONFIG")
    def test_addendum_has_semantic_flow_tracing_instruction(self):
        # Fix #4: the addendum must instruct tracing control/data FLOW before
        # marking semantic claims verified (so the LRU-vs-FIFO class of canary is
        # actually checked, not surface-matched). Anchored on a robust keyword.
        delta = _OJ.build_system_prompt().replace(_OJ.load_system_prompt(), "").lower()
        self.assertTrue(
            ("flow" in delta) or ("trace" in delta),
            "addendum should instruct control/data-flow tracing for semantic claims")

    @needs("build_system_prompt", "load_system_prompt", "JUDGE_CONFIG")
    def test_addendum_requires_emitted_grep_for_absence_claims(self):
        # Absence-addendum HARDENING is ON HOLD (Manager) pending an Explorer
        # loop-vs-protocol audit: a loop bug under-emitting/under-capturing tool
        # calls could MIMIC the fabrication symptom, which a prompt fix wouldn't
        # touch. The addendum is reverted to baseline → the hardening marker is
        # absent. CONTENT-GATED: auto-skips while held; auto-activates + asserts the
        # moment the hardening is greenlit + re-landed. (Instruction presence only;
        # model OBEDIENCE is live-only.)
        prompt = _OJ.build_system_prompt()
        if "ACTUALLY EMITTING a Grep" not in prompt:
            self.skipTest("absence-addendum hardening on hold (loop-vs-protocol audit)")
        low = prompt.lower()
        self.assertIn("absence", low)
        self.assertTrue(
            ("assert absence from inference" in low) or ("no matches" in low),
            "addendum must forbid asserting absence from inference (require a real Grep)")


# ===========================================================================
# Codex command-discipline prompt — codex-ONLY, with the OpenAI-parity invariant
# as the load-bearing guard. `_CODEX_COMMAND_DISCIPLINE` is appended ONLY in
# `_build_codex_prompt`; it must NEVER leak into `build_system_prompt` /
# `build_user_prompt` (the shared OpenAI-judge prompt stays byte-identical to before).
# Constant-relationship assertions are robust to exact wording; gated to auto-activate.
# ===========================================================================

def _command_discipline_landed():
    return _OJ is not None and hasattr(_OJ, "_CODEX_COMMAND_DISCIPLINE")


@unittest.skipUnless(_command_discipline_landed(),
                     "blocked: _CODEX_COMMAND_DISCIPLINE not landed yet")
class TestCodexCommandDiscipline(unittest.TestCase):
    def _codex_prompt(self):
        return _OJ._build_codex_prompt("DIFF", "REVIEW-A", "REVIEW-B")

    def test_codex_prompt_contains_discipline_block(self):
        # #1: the discipline constant is appended into the codex prompt verbatim.
        self.assertIn(_OJ._CODEX_COMMAND_DISCIPLINE, self._codex_prompt())

    def test_codex_prompt_contains_key_discipline_phrases(self):
        # Intent anchors (Coder-confirmed verbatim phrases). Case-insensitive so trivial
        # casing/spacing variants don't false-fail; the concepts (one-command-per-call +
        # absence-evidence rejection) must be present in the codex prompt.
        low = self._codex_prompt().lower()
        self.assertIn("command discipline (one command per tool call)", low)
        self.assertIn("exactly one shell command per tool call", low)
        self.assertIn("absence evidence will be rejected", low)

    def test_openai_system_prompt_excludes_discipline(self):
        # #2 LOAD-BEARING parity invariant: the discipline must NOT appear in the shared
        # OpenAI-judge system prompt — it stays codex-scoped (byte-identical to before).
        self.assertNotIn(_OJ._CODEX_COMMAND_DISCIPLINE, _OJ.build_system_prompt())

    def test_openai_user_prompt_excludes_discipline(self):
        self.assertNotIn(_OJ._CODEX_COMMAND_DISCIPLINE,
                         _OJ.build_user_prompt("DIFF", "REVIEW-A", "REVIEW-B"))


@unittest.skipUnless(_OJ is not None and hasattr(_OJ, "_build_codex_prompt") and
                     hasattr(_OJ, "build_system_prompt"),
                     "blocked: codex prompt builders not present")
class TestCodexPromptStructureInvariant(unittest.TestCase):
    def test_codex_prompt_still_contains_verbatim_system_and_user_prompts(self):
        # #3: regardless of any appended discipline block, the persona+addendum system
        # prompt AND the user prompt (diff + both reviews) survive verbatim inside the
        # codex prompt — the discipline is purely additive, never a rewrite.
        codex = _OJ._build_codex_prompt("DIFF-MARKER", "REVIEW-A-MARKER", "REVIEW-B-MARKER")
        self.assertIn(_OJ.build_system_prompt(), codex)
        self.assertIn("DIFF-MARKER", codex)
        self.assertIn("REVIEW-A-MARKER", codex)
        self.assertIn("REVIEW-B-MARKER", codex)


# ===========================================================================
# Manifest: bundled_command_events — observability for codex bundling ≥2 statements
# into one tool call. "Bundled" = a command_execution whose inner cmd splits into ≥2
# STATEMENT segments (`\n`/`;`/`&&`/`||`/single `&`); a single `|` PIPE is ONE command
# and must NOT count. Traces built through the real `_parse_codex_trace`.
# ===========================================================================

@unittest.skipUnless(_OJ is not None and hasattr(_OJ, "_count_bundled_command_events"),
                     "blocked: _count_bundled_command_events not landed yet")
class TestBundledCommandEventsManifest(unittest.TestCase):
    def test_andand_bundle_counted(self):
        trace = _trace_from(_codex_cmd_event("cat a.go && cat b.go", "x", 0))
        r = _OJ._count_bundled_command_events(trace)
        self.assertEqual(r["bundled_command_events"], 1)
        self.assertEqual(r["max_bundled_subcommands"], 2)

    def test_semicolon_bundle_counted(self):
        trace = _trace_from(_codex_cmd_event("cd d ; cat b.go", "x", 0))
        self.assertEqual(_OJ._count_bundled_command_events(trace)["bundled_command_events"], 1)

    def test_single_pipe_is_one_command_not_bundled(self):
        # LOAD-BEARING: a pipeline is ONE command — `statement_only` must not split on `|`.
        trace = _trace_from(_codex_cmd_event("rg -n foo . | head -5", "x", 0))
        r = _OJ._count_bundled_command_events(trace)
        self.assertEqual(r["bundled_command_events"], 0)
        self.assertEqual(r["max_bundled_subcommands"], 1)

    def test_clean_single_command_zero(self):
        trace = _trace_from(_codex_cmd_event("cat c.go", "x", 0))
        r = _OJ._count_bundled_command_events(trace)
        self.assertEqual(r["bundled_command_events"], 0)
        self.assertEqual(r["max_bundled_subcommands"], 1)

    def test_quoted_separator_not_counted_as_bundle(self):
        # Anti-false-positive: a `&&` / `;` INSIDE a quoted search pattern is part of one
        # command, not a statement separator → must NOT inflate the bundle count.
        for inner in ('rg "foo && bar" .', "rg 'a ; b' ."):
            r = _OJ._count_bundled_command_events(_trace_from(_codex_cmd_event(inner, "x", 0)))
            self.assertEqual(r["bundled_command_events"], 0, inner)
            self.assertEqual(r["max_bundled_subcommands"], 1, inner)

    def test_empty_trace_zeroes(self):
        r = _OJ._count_bundled_command_events({"commands": []})
        self.assertEqual(r["bundled_command_events"], 0)
        self.assertEqual(r["max_bundled_subcommands"], 0)

    def test_mixed_trace_count_and_max(self):
        # 2 bundled events (2 and 3 statements) + 1 single + 1 pipeline → count=2, max=3.
        trace = _trace_from(
            _codex_cmd_event("cat a.go && cat b.go", "x", 0),          # 2
            _codex_cmd_event("cd d && nl x.go && cat y.go", "x", 0),   # 3
            _codex_cmd_event("cat c.go", "x", 0),                       # 1
            _codex_cmd_event("rg -n foo . | head", "x", 0),            # pipeline → 1
        )
        r = _OJ._count_bundled_command_events(trace)
        self.assertEqual(r["bundled_command_events"], 2)
        self.assertEqual(r["max_bundled_subcommands"], 3)


# ===========================================================================
# Phase 3 (Reviewer Important) — end-of-run worktree teardown
# ===========================================================================
# cleanup_pr_worktrees(worktree_specs: list[(base_clone_path, worktree_path)]) -> None
# Best-effort: removes each worktree (git worktree remove --force, rmtree fallback)
# and prunes the base clone's stale metadata. NEVER raises — a bad spec is logged,
# not propagated, so it can't sink an otherwise-complete run.

@unittest.skipUnless(_HAS_GIT, "git not available")
class TestWorktreeTeardown(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)

    @staticmethod
    def _worktree_count(base):
        import subprocess
        out = subprocess.run(["git", "worktree", "list", "--porcelain"], cwd=base,
                             capture_output=True, text=True, check=True)
        return sum(1 for ln in out.stdout.splitlines() if ln.startswith("worktree "))

    @needs("cleanup_pr_worktrees", "prepare_pr_checkout")
    def test_worktrees_removed_and_metadata_pruned(self):
        manifest = build_multi_sha_repo(os.path.join(self.tmp, "base"), [1, 2, 3])
        base = os.path.join(self.tmp, "base")
        fetch = lambda pr_data: manifest[pr_data["pr_number"]]["sha"]  # noqa: E731
        specs = []
        for n in (1, 2, 3):
            dest = os.path.join(self.tmp, f"wt_{n}")
            _OJ.prepare_pr_checkout({"clone_path": base, "pr_number": n}, dest, fetcher=fetch)
            specs.append((base, dest))
        # Sanity: 3 PR worktrees + the base itself are registered before teardown.
        self.assertEqual(self._worktree_count(base), 4)

        _OJ.cleanup_pr_worktrees(specs)

        for _, dest in specs:
            self.assertFalse(os.path.exists(dest), f"worktree dir not removed: {dest}")
        # Only the base working tree remains; stale metadata pruned.
        self.assertEqual(self._worktree_count(base), 1)

    @needs("cleanup_pr_worktrees")
    def test_best_effort_missing_paths_do_not_raise(self):
        # A completed run must never be sunk by a teardown error on a bad spec.
        try:
            _OJ.cleanup_pr_worktrees([("/nonexistent/base", "/nope/worktree")])
        except Exception as e:  # noqa: BLE001
            self.fail(f"cleanup_pr_worktrees must be best-effort, but raised: {e!r}")

    @needs("cleanup_pr_worktrees")
    def test_empty_specs_no_op(self):
        _OJ.cleanup_pr_worktrees([])  # must not raise


# ===========================================================================
# Phase 1 — OpenAI client wrapper (offline contract + opt-in live smoke)
# ===========================================================================

class TestOpenAIClient(unittest.TestCase):
    @unittest.skipIf(_OC is None, "blocked: eval.lib.openai_client not importable")
    def test_default_model_is_gpt_5_1(self):
        self.assertEqual(_OC.DEFAULT_MODEL, "gpt-5.1")

    @unittest.skipIf(_OC is None, "blocked: eval.lib.openai_client not importable")
    def test_missing_key_raises_loudly(self):
        # Fail-loud contract: no key → MissingAPIKey, not an opaque SDK auth error.
        import unittest.mock as mock
        with mock.patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(_OC.MissingAPIKey):
                _OC.build_client()

    @unittest.skipUnless(_OC is not None and _HAS_KEY,
                         "set OPENAI_API_KEY to run the live client smoke test")
    def test_build_client_live_smoke(self):
        # User is provisioning the key later; this stays dormant until then.
        client = _OC.build_client()
        self.assertIsNotNone(client)


# ===========================================================================
# Phase 6 — LIVE end-to-end (spends OpenAI tokens; opt-in only)
# ===========================================================================

@unittest.skipUnless(_LIVE_CODEX, "set RUN_LIVE_CODEX_JUDGE=1 (and `codex login`) to run the live Codex judge")
@unittest.skipUnless(_HAS_GIT, "git not available")
class TestLiveEndToEnd(unittest.TestCase):
    """JUDGE-LEVEL live acceptance (real Codex judge, gpt-5.4, over the fixture).
    Gated on RUN_LIVE_CODEX_JUDGE=1 (codex auth via ~/.codex/auth.json; default
    on_out_of_worktree_read='flag' is correct here — trusted fixture repo). USER-run:
        RUN_LIVE_CODEX_JUDGE=1 python -m pytest \\
            eval/tests/test_openai_judge.py::TestLiveEndToEnd -v"""

    @needs("score_paired_reviews")
    def test_fabricated_review_canaries_all_caught_live(self):
        canaries = load_fixture_canaries()
        # STABLE run_dir (git-ignored eval/output/) so the per-run transcript
        # sidecars (judge_pr*_run*_transcript.json) SURVIVE for diagnosis — they
        # capture the Greps the model emitted vs cited on the absence canaries.
        run_dir = str(Path(__file__).resolve().parent.parent / "output" / "phase6-live-judge")
        os.makedirs(run_dir, exist_ok=True)
        with tempfile.TemporaryDirectory() as d:
            repo = build_fixture_repo(os.path.join(d, "repo"))
            result = _OJ.score_paired_reviews(
                pr_data={"pr_number": 1, "diff": "+MAX_RETRIES = 3\n", "repo": "fixture/repo"},
                skwad_review=load_review("fabricated_review"),
                claude_ci_review=load_review("good_review"),
                repo_path=repo, seed=42, run_dir=run_dir,
                canary_injections=canaries)
        # /10 recalibration: all 3 counterbalanced runs should now complete.
        self.assertEqual(result["n_runs_completed"], 3,
                         f"expected 3/3 runs; got {result['n_runs_completed']}")
        # Acceptance: every hardened canary caught (incl. the LRU non-obvious one).
        outcomes = result.get("canary_outcomes", [])
        self.assertTrue(outcomes, "no canary outcomes recorded")
        failed = [o for o in outcomes if not o.get("passed")]
        self.assertFalse(failed, f"canaries missed: {[o.get('id') for o in failed]}")


if __name__ == "__main__":
    unittest.main()
