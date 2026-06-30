"""LLM-assisted bidirectional PR difficulty classifier."""

import json
import logging
import os
import re
import subprocess
from pathlib import Path
from typing import Literal

logger = logging.getLogger(__name__)

EVAL_DIR = Path(__file__).resolve().parent.parent
CLASSIFIER_CONFIG = EVAL_DIR / "config" / "classifier_team.json"

SENSITIVE_PATH_RE = re.compile(r"auth|security|migration|perf|payment|crypto", re.IGNORECASE)

# The skwad-cli binary has its own --timeout (default 10m) that self-terminates the
# run. We make the binary authoritative by passing --timeout explicitly, and give the
# Python subprocess wrapper this many extra seconds so the binary stops gracefully
# BEFORE the wrapper force-kills it.
BINARY_TIMEOUT_BUFFER_SEC = 120

Bucket = Literal["easy", "medium", "hard"]
BUCKET_ORDER: list[Bucket] = ["easy", "medium", "hard"]


def _loc_from_diff(diff: str) -> int:
    count = 0
    for line in diff.splitlines():
        if (line.startswith("+") and not line.startswith("+++")) or \
                (line.startswith("-") and not line.startswith("---")):
            count += 1
    return count


def _file_paths_from_diff(diff: str) -> list[str]:
    paths = []
    for line in diff.splitlines():
        if line.startswith("+++ b/"):
            paths.append(line[6:])
        elif line.startswith("rename to "):
            paths.append(line[len("rename to "):])
    return list(dict.fromkeys(paths))


def _heuristic_bucket(pr_data: dict) -> Bucket:
    """Classify PR difficulty using heuristic rules only."""
    files: int = pr_data.get("files_changed", 0)
    diff: str = pr_data.get("diff", "")

    additions = pr_data.get("additions")
    deletions = pr_data.get("deletions")
    loc = (additions + deletions) if (additions is not None and deletions is not None) else _loc_from_diff(diff)

    file_paths: list[str] = pr_data.get("file_paths") or _file_paths_from_diff(diff)
    sensitive = any(SENSITIVE_PATH_RE.search(p) for p in file_paths)

    if sensitive or files >= 11 or loc >= 500:
        return "hard"
    if files >= 4 and loc >= 100:
        return "medium"
    return "easy"


def _parse_json_output(raw: str) -> dict:
    raw = raw.strip()
    raw = re.sub(r"^```(?:json)?\s*\n?", "", raw)
    raw = re.sub(r"\n?```\s*$", "", raw)
    start = raw.find("{")
    end = raw.rfind("}")
    if start == -1 or end == -1:
        raise ValueError(f"No JSON object in classifier output: {raw[:200]}")
    return json.loads(raw[start : end + 1])


def _llm_refine(
    pr_data: dict,
    heuristic_bucket: Bucket,
    skwad_binary: str,
    repo_path: str,
    timeout: int = 120,
) -> tuple[Bucket, str, int]:
    """Invoke LLM classifier via skwad-cli subprocess. Returns (final_bucket, reasoning, delta)."""
    files: int = pr_data.get("files_changed", 0)
    diff: str = pr_data.get("diff", "")
    file_paths: list[str] = pr_data.get("file_paths") or _file_paths_from_diff(diff)
    loc = _loc_from_diff(diff)

    paths_block = "\n".join(f"  - {p}" for p in file_paths) or "  (none)"
    cap = 32000
    if len(diff) > cap:
        diff_for_prompt = f"=== DIFF (truncated at {cap} chars; full diff is {len(diff)} chars) ===\n{diff[:cap]}\n\n"
    else:
        diff_for_prompt = f"=== DIFF ===\n{diff}\n\n"
    prompt = (
        f"Classify the difficulty of this pull request for code review purposes.\n\n"
        f"Heuristic assessment: {heuristic_bucket.upper()}\n"
        f"Files changed: {files}\n"
        f"Lines changed (additions + deletions): {loc}\n"
        f"File paths:\n{paths_block}\n\n"
        + diff_for_prompt
        + "Write your JSON output to classifier_output.json at the root of the repo."
    )

    output_path = os.path.join(repo_path, "classifier_output.json")
    if os.path.exists(output_path):
        os.remove(output_path)

    result = subprocess.run(
        [
            os.path.abspath(skwad_binary), "run",
            "--config", str(CLASSIFIER_CONFIG),
            "--set", f"repo={os.path.abspath(repo_path)}",
            "--timeout", f"{timeout}s",
            "--prompt", prompt,
        ],
        cwd=repo_path,
        timeout=timeout + BINARY_TIMEOUT_BUFFER_SEC,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        logger.warning(
            "classifier subprocess exit=%d; stderr: %s",
            result.returncode, (result.stderr or "")[:500],
        )
        raise RuntimeError(f"classifier subprocess failed (exit {result.returncode})")

    if not os.path.exists(output_path):
        raise RuntimeError("Classifier produced no output file (classifier_output.json missing)")

    with open(output_path) as f:
        raw = f.read()
    parsed = _parse_json_output(raw)

    llm_bucket_raw: str = parsed.get("bucket", heuristic_bucket).lower()
    reasoning: str = parsed.get("reasoning", "")

    h_idx = BUCKET_ORDER.index(heuristic_bucket)
    try:
        l_idx = BUCKET_ORDER.index(llm_bucket_raw)
    except ValueError:
        logger.warning("LLM returned unknown bucket %r; keeping heuristic %s", llm_bucket_raw, heuristic_bucket)
        return heuristic_bucket, reasoning, 0

    raw_delta = l_idx - h_idx
    delta = max(-1, min(1, raw_delta))
    if raw_delta != delta:
        logger.warning(
            "LLM attempted multi-step jump %s→%s (delta=%d); clamped to delta=%d",
            heuristic_bucket, llm_bucket_raw, raw_delta, delta,
        )

    final_bucket: Bucket = BUCKET_ORDER[h_idx + delta]
    return final_bucket, reasoning, delta


def classify_pr(
    pr_data: dict,
    skwad_binary: str = "./skwad-cli",
    repo_path: str = ".",
    timeout: int = 120,
) -> dict:
    """Classify a PR into easy/medium/hard using heuristics + LLM bidirectional refinement.

    Returns dict with keys: bucket, reasoning, heuristic_bucket, llm_delta.
    """
    heuristic = _heuristic_bucket(pr_data)

    try:
        final_bucket, reasoning, delta = _llm_refine(
            pr_data=pr_data,
            heuristic_bucket=heuristic,
            skwad_binary=skwad_binary,
            repo_path=repo_path,
            timeout=timeout,
        )
    except Exception as e:
        logger.warning("LLM refinement failed (%s); falling back to heuristic bucket=%s", e, heuristic)
        return {
            "bucket": heuristic,
            "reasoning": f"LLM refinement unavailable ({e}); heuristic used.",
            "heuristic_bucket": heuristic,
            "llm_delta": 0,
        }

    if delta != 0:
        logger.warning(
            "Classifier disagreement: heuristic=%s llm_final=%s delta=%+d files=%d",
            heuristic, final_bucket, delta, pr_data.get("files_changed", 0),
        )

    return {
        "bucket": final_bucket,
        "reasoning": reasoning,
        "heuristic_bucket": heuristic,
        "llm_delta": delta,
    }
