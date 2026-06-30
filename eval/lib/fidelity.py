"""Descriptive fidelity comparison between two eval runs (Phase 6).

NOT a clean causal before/after. The old run (N=7, claude judge, default-branch
checkout) and the new run (gpt-5.1 judge, PR-head checkout) differ on MULTIPLE
variables at once — judge model AND checkout, and usually different PR sets — so
any difference reported here is DESCRIPTIVE only and must NOT be attributed to the
PR-checkout fix alone. The new run is a fresh baseline, not an extension of the old.
"""

import glob
import json
import os

_SYSTEMS = ("skwad", "claude_ci")
_BUCKETS = (
    "claims_verified",
    "claims_unverified",
    "claims_contradicted",
    "claims_non_falsifiable",
)

_CAVEAT = (
    "DESCRIPTIVE, not causal: the old run (N=7, claude judge / default-branch "
    "checkout) and the new run (gpt-5.1 judge / PR-head checkout) differ on judge "
    "model AND checkout (and usually on PR set). Differences are NOT attributable "
    "to the checkout fix alone — the new run is a fresh baseline."
)


def _summarize_run(run_dir: str) -> dict:
    """Aggregate per-system claim-outcome totals + canary catch-rate for one run dir.

    Reads every ``judge_pr*_voted.json`` (recursively) for per-system
    ``verification_summary`` buckets, and ``manifest.json`` for the canary
    catch-rate. Missing/malformed files are skipped — never raises.
    """
    totals = {s: {b: 0 for b in _BUCKETS} for s in _SYSTEMS}
    n_prs = 0
    voted_files = sorted(set(
        glob.glob(os.path.join(run_dir, "**", "judge_pr*_voted.json"), recursive=True)
    ))
    for vf in voted_files:
        try:
            with open(vf) as f:
                voted = json.load(f)
        except (OSError, json.JSONDecodeError):
            continue
        n_prs += 1
        for system in _SYSTEMS:
            vs = (voted.get(system) or {}).get("verification_summary", {}) or {}
            for bucket in _BUCKETS:
                totals[system][bucket] += vs.get(bucket, 0)

    catch_rate = None
    manifest_path = os.path.join(run_dir, "manifest.json")
    if os.path.exists(manifest_path):
        try:
            with open(manifest_path) as f:
                manifest = json.load(f)
            outcomes = manifest.get("canary_outcomes") or []
            if outcomes:
                passed = sum(1 for o in outcomes if o.get("passed"))
                catch_rate = passed / len(outcomes)
        except (OSError, json.JSONDecodeError):
            pass

    return {"outcome_totals": totals, "canary_catch_rate": catch_rate, "n_prs": n_prs}


def compare_claim_outcomes(old_dir: str, new_dir: str) -> dict:
    """Descriptive comparison of two eval runs' claim-verification outcomes.

    Aggregate-level only (per-claim matching across two different judges/checkouts
    is not meaningful). Returns old/new summaries, the per-bucket delta
    (new − old), and an always-present ``caveat`` stating the comparison is
    DESCRIPTIVE, not causal. See module docstring.
    """
    old = _summarize_run(old_dir)
    old["label"] = "old (N=7 claude / default-branch)"
    new = _summarize_run(new_dir)
    new["label"] = "new (gpt-5.1 / PR-head)"
    delta = {
        system: {
            bucket: new["outcome_totals"][system][bucket] - old["outcome_totals"][system][bucket]
            for bucket in _BUCKETS
        }
        for system in _SYSTEMS
    }
    return {"old": old, "new": new, "delta": delta, "caveat": _CAVEAT}
