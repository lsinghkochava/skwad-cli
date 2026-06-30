"""Tests for eval.lib.fidelity.compare_claim_outcomes (Phase 6 fidelity helper).

Artifact-differ ONLY — reads persisted judge artifacts (voted.json + manifest.json)
from an old run dir and a new run dir and reports descriptive deltas. It does NOT
re-run anything. The caveat must always frame the comparison as DESCRIPTIVE, not
causal (old vs new differ on model + checkout + PR set, not the checkout fix alone).

Gated on the module existing → skips cleanly until the Coder lands it, then activates.
"""

import json
import os
import tempfile
import unittest

try:
    import eval.lib.fidelity as fid
    _FID = fid
except Exception:
    _FID = None

_BUCKETS = ("claims_verified", "claims_unverified", "claims_contradicted", "claims_non_falsifiable")


def _vs(verified=0, unverified=0, contradicted=0, non_falsifiable=0):
    return {"claims_verified": verified, "claims_unverified": unverified,
            "claims_contradicted": contradicted, "claims_non_falsifiable": non_falsifiable}


def _write_voted(base, sub, vs_skwad, vs_ci):
    d = os.path.join(base, sub)
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, "judge_pr1_voted.json"), "w") as f:
        json.dump({"skwad": {"verification_summary": vs_skwad},
                   "claude_ci": {"verification_summary": vs_ci}}, f)


def _write_manifest(base, canary_outcomes):
    with open(os.path.join(base, "manifest.json"), "w") as f:
        json.dump({"canary_outcomes": canary_outcomes}, f)


@unittest.skipUnless(_FID is not None, "blocked: eval.lib.fidelity not implemented yet")
class TestCompareClaimOutcomes(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        import shutil
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.old = os.path.join(self.tmp, "old")
        self.new = os.path.join(self.tmp, "new")

    def test_outcome_totals_summed_across_prs(self):
        # Two PRs in old, each skwad verified=3 → summed to 6 (recursive walk).
        _write_voted(self.old, "Kochava-repo-1", _vs(verified=3), _vs(verified=1))
        _write_voted(self.old, "Kochava-repo-2", _vs(verified=3), _vs(verified=1))
        _write_voted(self.new, "Kochava-repo-1", _vs(verified=5), _vs(verified=2))
        result = _FID.compare_claim_outcomes(self.old, self.new)
        self.assertEqual(result["old"]["outcome_totals"]["skwad"]["claims_verified"], 6)
        self.assertEqual(result["old"]["outcome_totals"]["claude_ci"]["claims_verified"], 2)
        self.assertEqual(result["old"]["n_prs"], 2)
        self.assertEqual(result["new"]["n_prs"], 1)

    def test_delta_is_new_minus_old(self):
        _write_voted(self.old, "pr1", _vs(verified=3, contradicted=1), _vs())
        _write_voted(self.new, "pr1", _vs(verified=5, contradicted=4), _vs())
        result = _FID.compare_claim_outcomes(self.old, self.new)
        self.assertEqual(result["delta"]["skwad"]["claims_verified"], 2)   # 5-3
        self.assertEqual(result["delta"]["skwad"]["claims_contradicted"], 3)  # 4-1

    def test_canary_catch_rate_from_manifest(self):
        _write_voted(self.new, "pr1", _vs(), _vs())
        _write_manifest(self.new, [{"passed": True}, {"passed": False}])
        result = _FID.compare_claim_outcomes(self.old, self.new)
        self.assertEqual(result["new"]["canary_catch_rate"], 0.5)

    def test_missing_manifest_catch_rate_none(self):
        _write_voted(self.new, "pr1", _vs(), _vs())  # no manifest written
        result = _FID.compare_claim_outcomes(self.old, self.new)
        self.assertIsNone(result["new"]["canary_catch_rate"])

    def test_empty_canary_outcomes_catch_rate_none(self):
        _write_voted(self.new, "pr1", _vs(), _vs())
        _write_manifest(self.new, [])
        result = _FID.compare_claim_outcomes(self.old, self.new)
        self.assertIsNone(result["new"]["canary_catch_rate"])

    def test_caveat_always_present_and_descriptive_not_causal(self):
        _write_voted(self.old, "pr1", _vs(verified=1), _vs())
        _write_voted(self.new, "pr1", _vs(verified=1), _vs())
        caveat = _FID.compare_claim_outcomes(self.old, self.new)["caveat"]
        self.assertTrue(caveat)
        low = caveat.lower()
        self.assertTrue(
            ("causal" in low) or ("not attributable" in low) or ("descriptive" in low),
            f"caveat must frame the comparison as non-causal; got: {caveat!r}")

    def test_missing_dirs_return_zeros_and_none_without_raising(self):
        # Pure/offline: nonexistent dirs → empty totals + None catch-rate, never raises.
        result = _FID.compare_claim_outcomes(
            os.path.join(self.tmp, "nope_old"), os.path.join(self.tmp, "nope_new"))
        self.assertEqual(result["old"]["n_prs"], 0)
        self.assertIsNone(result["old"]["canary_catch_rate"])
        for bucket in _BUCKETS:
            self.assertEqual(result["old"]["outcome_totals"]["skwad"][bucket], 0)
        self.assertIn("caveat", result)

    def test_recursive_walk_finds_deeply_nested_voted(self):
        _write_voted(self.old, os.path.join("a", "b", "Kochava-repo-9"), _vs(verified=7), _vs())
        result = _FID.compare_claim_outcomes(self.old, self.new)
        self.assertEqual(result["old"]["outcome_totals"]["skwad"]["claims_verified"], 7)
        self.assertEqual(result["old"]["n_prs"], 1)


if __name__ == "__main__":
    unittest.main()
