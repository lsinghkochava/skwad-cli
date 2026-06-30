"""Statistical analysis module for the eval experiment framework.

Pure-compute; no I/O, no logging. All functions return plain dicts for trivial JSON serialization.
"""

from __future__ import annotations

import math
from typing import Sequence


class MethodologyMismatchError(ValueError):
    """Raised when records with different methodology_version values are aggregated together."""


def check_methodology_version(records: list[dict]) -> None:
    """Raise MethodologyMismatchError if records mix different methodology versions.

    v1 records have no 'methodology_version' field (treated as version 1).
    v2 records have 'methodology_version': 2.
    Records with different versions must NOT be aggregated.
    """
    versions = {r.get("methodology_version", 1) for r in records}
    if len(versions) > 1:
        raise MethodologyMismatchError(
            f"Cannot aggregate records with mixed methodology versions: {sorted(versions)}. "
            "v1 and v2 data are not comparable. Separate your result sets by methodology_version."
        )

import krippendorff
import numpy as np
from scipy.stats import bootstrap, false_discovery_control, wilcoxon


def wilcoxon_paired(paired_diffs: list[int | float], *, records: list[dict] | None = None) -> dict:
    """Two-sided Wilcoxon signed-rank test on paired score differences.

    Args:
        paired_diffs: List of per-PR score differences (e.g. skwad_total - ci_total).
                      Must have at least 1 non-zero entry.
        records: Optional list of source records to gate against mixed methodology
                 versions. If provided, raises MethodologyMismatchError on mix.

    Returns:
        {"statistic": float, "p_value": float, "n": int, "n_nonzero": int}
    """
    if records is not None:
        check_methodology_version(records)
    if not paired_diffs:
        raise ValueError("paired_diffs must not be empty")
    if all(d == 0 for d in paired_diffs):
        raise ValueError("paired_diffs are all zero — Wilcoxon is undefined (no non-zero differences)")

    n_nonzero = sum(1 for d in paired_diffs if d != 0)
    result = wilcoxon(paired_diffs, zero_method="wilcox", alternative="two-sided")
    return {
        "statistic": float(result.statistic),
        "p_value": float(result.pvalue),
        "n": len(paired_diffs),
        "n_nonzero": n_nonzero,
    }


def _cliffs(a: Sequence[int | float], b: Sequence[int | float]) -> float:
    greater = sum(1 for x in a for y in b if x > y)
    less = sum(1 for x in a for y in b if x < y)
    return (greater - less) / (len(a) * len(b))


def cliffs_delta(
    a: list[int | float],
    b: list[int | float],
    *,
    records: list[dict] | None = None,
) -> dict:
    """Cliff's delta effect size for ordinal data.

    Positive delta = a stochastically greater than b.
    Thresholds: |δ|<0.147 negligible; <0.33 small; <0.474 medium; ≥0.474 large.

    Args:
        a: Scores for system A (e.g. skwad).
        b: Scores for system B (e.g. claude_ci). Must be same length as a.
        records: Optional list of source records to gate against mixed methodology
                 versions.

    Returns:
        {"delta": float, "interpretation": "negligible|small|medium|large"}
    """
    if records is not None:
        check_methodology_version(records)
    if not a or not b:
        raise ValueError("a and b must both be non-empty")

    delta = _cliffs(a, b)
    abs_d = abs(delta)
    if abs_d < 0.147:
        interpretation = "negligible"
    elif abs_d < 0.33:
        interpretation = "small"
    elif abs_d < 0.474:
        interpretation = "medium"
    else:
        interpretation = "large"

    return {"delta": float(delta), "interpretation": interpretation}


def cliffs_delta_bca_ci(
    a: list[int | float],
    b: list[int | float],
    *,
    n_boot: int = 2000,
    seed: int | None = None,
    alpha: float = 0.05,
    records: list[dict] | None = None,
) -> dict:
    """Bootstrap BCa confidence interval on Cliff's delta.

    Args:
        a: Scores for system A.
        b: Scores for system B. Must be same length as a.
        n_boot: Number of bootstrap resamples.
        seed: RNG seed for reproducibility.
        alpha: CI alpha level (default 0.05 → 95% CI).

    Returns:
        {"delta": float, "ci_lower": float, "ci_upper": float, "n_boot": int}
    """
    if records is not None:
        check_methodology_version(records)
    if not a or not b:
        raise ValueError("a and b must both be non-empty")
    if len(a) != len(b):
        raise ValueError(f"a and b must be the same length; got {len(a)} vs {len(b)}")

    a_arr = np.array(a, dtype=float)
    b_arr = np.array(b, dtype=float)

    delta = _cliffs(a, b)

    def statistic(x: np.ndarray, y: np.ndarray) -> float:
        return _cliffs(x.tolist(), y.tolist())

    res = bootstrap(
        (a_arr, b_arr),
        statistic,
        n_resamples=n_boot,
        paired=True,
        method="BCa",
        confidence_level=1.0 - alpha,
        random_state=seed,
    )
    ci = res.confidence_interval
    return {
        "delta": float(delta),
        "ci_lower": float(ci.low),
        "ci_upper": float(ci.high),
        "n_boot": n_boot,
    }


def krippendorff_alpha_ordinal(
    reliability_data: list[list[int]],
    *,
    n_boot: int = 2000,
    seed: int | None = None,
    alpha: float = 0.05,
    records: list[dict] | None = None,
) -> dict:
    """Krippendorff's α (ordinal) with bootstrap 95% CI.

    Args:
        reliability_data: Outer list = raters, inner list = scores per item.
                          Use np.nan for missing values. All rater lists must be same length.
        n_boot: Number of bootstrap resamples (resample items, not raters).
        seed: RNG seed for reproducibility.
        alpha: CI alpha level (default 0.05 → 95% CI).

    Returns:
        {"alpha": float, "ci_lower": float, "ci_upper": float,
         "n_boot": int, "n_raters": int, "n_items": int}
    """
    if records is not None:
        check_methodology_version(records)
    if not reliability_data:
        raise ValueError("reliability_data must not be empty")
    n_raters = len(reliability_data)
    n_items = len(reliability_data[0])
    if n_items == 0:
        raise ValueError("reliability_data raters have no items")

    data_arr = np.array(reliability_data, dtype=float)
    point_alpha = float(krippendorff.alpha(data_arr, level_of_measurement="ordinal"))

    rng = np.random.default_rng(seed)
    boot_alphas: list[float] = []
    for _ in range(n_boot):
        item_indices = rng.integers(0, n_items, size=n_items)
        sample = data_arr[:, item_indices]
        try:
            a = float(krippendorff.alpha(sample, level_of_measurement="ordinal"))
            boot_alphas.append(a)
        except Exception:
            pass

    if not boot_alphas:
        raise RuntimeError("All bootstrap iterations failed for Krippendorff's α")

    low = float(np.percentile(boot_alphas, 100 * (alpha / 2)))
    high = float(np.percentile(boot_alphas, 100 * (1 - alpha / 2)))

    return {
        "alpha": point_alpha,
        "ci_lower": low,
        "ci_upper": high,
        "n_boot": n_boot,
        "n_raters": n_raters,
        "n_items": n_items,
    }


def compute_inter_run_alpha(pr_results: list[dict]) -> dict:
    """Compute Krippendorff's α (ordinal) per criterion across the 3 judge runs.

    For each criterion, gather the 3 run-scores per (PR × system) as items rated
    by 3 raters (one per run). Returns {criterion_name: alpha_or_None}.

    Sets a criterion's alpha to None if computation is degenerate (e.g. <2 items
    or all-identical scores). When present, the dict carries: "_warnings" (criteria
    whose alpha could not be computed), "_ci_lower" (non-degenerate criterion →
    bootstrap 95% CI lower bound, the authoritative value the pilot alpha gate
    compares), and "_degenerate_reason" (None-alpha criterion → why: one of
    "perfect_agreement" | "insufficient_items" | "computation_error"). The gate
    treats only "perfect_agreement" as a pass; the others are unmeasurable → fail.
    """
    CRITERIA = [
        "issue_detection",
        "actionability",
        "severity_accuracy",
        "coverage",
        "signal_to_noise",
        "depth",
        "novel_substantive_findings",
    ]

    result: dict = {}
    ci_lower: dict = {}
    degenerate_reason: dict = {}
    warnings: list[str] = []

    for criterion in CRITERIA:
        # 3 raters (runs), items = PR × system entries.
        run_scores: list[list[float]] = [[], [], []]
        for r in pr_results:
            for sys_key in ("skwad", "claude_ci"):
                scores_3 = r.get(sys_key, {}).get(criterion, {}).get("scores", [])
                if not isinstance(scores_3, list) or len(scores_3) < 3:
                    continue
                for run_idx in range(3):
                    run_scores[run_idx].append(float(scores_3[run_idx]))

        n_items = len(run_scores[0]) if run_scores[0] else 0
        if n_items < 2:
            result[criterion] = None
            degenerate_reason[criterion] = "insufficient_items"
            warnings.append(f"{criterion}: <2 items ({n_items}), alpha undefined")
            continue

        # Check for all-identical scores (degenerate — perfect agreement).
        flat = [s for r in run_scores for s in r]
        if len(set(flat)) <= 1:
            result[criterion] = None
            degenerate_reason[criterion] = "perfect_agreement"
            warnings.append(f"{criterion}: all scores identical, alpha undefined")
            continue

        try:
            # Reuse the bootstrap estimator so the gate can read the CI lower
            # bound; seed fixed for reproducible gating across re-runs.
            stats = krippendorff_alpha_ordinal(run_scores, seed=0)
            result[criterion] = stats["alpha"]
            ci_lower[criterion] = stats["ci_lower"]
        except Exception as e:
            result[criterion] = None
            degenerate_reason[criterion] = "computation_error"
            warnings.append(f"{criterion}: {type(e).__name__}: {e}")

    if ci_lower:
        result["_ci_lower"] = ci_lower
    if degenerate_reason:
        result["_degenerate_reason"] = degenerate_reason
    if warnings:
        result["_warnings"] = warnings
    return result


def bh_fdr_adjust(
    p_values: list[float],
    *,
    q: float = 0.05,
) -> dict:
    """Benjamini-Hochberg FDR adjustment.

    Args:
        p_values: Raw p-values (parallel to tests being adjusted).
        q: FDR threshold (default 0.05).

    Returns:
        {"raw": list[float], "adjusted": list[float], "rejected": list[bool], "q": float}
    """
    if not p_values:
        raise ValueError("p_values must not be empty")

    adjusted_arr = false_discovery_control(p_values, method="bh")
    adjusted = [float(v) for v in adjusted_arr]
    rejected = [v <= q for v in adjusted]

    return {
        "raw": list(p_values),
        "adjusted": adjusted,
        "rejected": rejected,
        "q": q,
    }
