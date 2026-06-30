"""Reproducibility manifest writer for the eval experiment framework.

Creates and writes eval/output/manifest.json — everything needed to reproduce a run from a seed.
No PR content is stored; only metadata (repo, PR number, commit SHA, difficulty, skip reason).
"""

from __future__ import annotations

import hashlib
import importlib.metadata
import json
import logging
import platform
import subprocess
import uuid
from datetime import datetime, timezone
from pathlib import Path

logger = logging.getLogger(__name__)

EVAL_DIR = Path(__file__).resolve().parent.parent

METHODOLOGY_VERSION = 2
_DEFAULT_PROMPT_FILES = {
    "rubric_json_sha256": str(EVAL_DIR / "config" / "rubric.json"),
    "judge_team_json_sha256": str(EVAL_DIR / "config" / "judge_team.json"),
    "classifier_team_json_sha256": str(EVAL_DIR / "config" / "classifier_team.json"),
    "judge_persona_md_sha256": str(EVAL_DIR / "config" / "judge_persona.md"),
    "classifier_persona_md_sha256": str(EVAL_DIR / "config" / "classifier_persona.md"),
}


def _now_utc() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _detect_git_sha() -> str:
    try:
        result = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            capture_output=True,
            text=True,
            cwd=str(EVAL_DIR.parent),
        )
        if result.returncode == 0:
            return result.stdout.strip()
    except Exception:
        pass
    return "unknown"


def _collect_python_versions() -> dict:
    versions: dict[str, str] = {"python": platform.python_version()}
    for pkg in ("scipy", "numpy", "krippendorff"):
        try:
            versions[pkg] = importlib.metadata.version(pkg)
        except importlib.metadata.PackageNotFoundError:
            versions[pkg] = "not installed"
    return versions


def _collect_os_info() -> dict:
    return {
        "uname": " ".join(str(x) for x in platform.uname()),
        "hostname": platform.node(),
    }


def hash_prompt_file(path: str) -> str:
    """Compute SHA-256 hex digest of the file at path. Returns 64-char lowercase hex."""
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()


def _compute_prompt_hashes() -> dict:
    hashes: dict[str, str] = {}
    for key, path in _DEFAULT_PROMPT_FILES.items():
        try:
            hashes[key] = hash_prompt_file(path)
        except OSError:
            hashes[key] = f"MISSING:{path}"
            logger.warning("prompt file not found for hash key %r: %s", key, path)
    return hashes


def open_manifest(
    output_path: str,
    *,
    seed: int,
    skwad_cli_git_sha: str | None = None,
) -> dict:
    """Initialize a manifest dict with all static fields populated.

    Caller mutates the returned dict (append to prs[], record models, etc.)
    then calls write_manifest() at end of run to persist.

    Args:
        output_path: Destination path for the manifest JSON file.
        seed: RNG seed used in this run (recorded verbatim for reproducibility).
        skwad_cli_git_sha: Git SHA of skwad-cli. If None, auto-detected via git.

    Returns:
        In-memory manifest dict with all static fields set and prs/skipped_prs empty.
    """
    return {
        "run_id": str(uuid.uuid4()),
        "started_at_utc": _now_utc(),
        "completed_at_utc": None,
        "methodology_version": METHODOLOGY_VERSION,
        "skwad_cli_git_sha": skwad_cli_git_sha if skwad_cli_git_sha is not None else _detect_git_sha(),
        "models": {},
        "rng_seed": seed,
        "prompt_hashes": _compute_prompt_hashes(),
        "python_versions": _collect_python_versions(),
        "os_info": _collect_os_info(),
        "prs": [],
        "skipped_prs": [],
        "pilot_pass": None,
        "canary_outcomes": [],
        "confabulation_rejections": 0,
        "disallowed_tool_rejections": 0,
        "structural_invalid_rejections": 0,
        "evidence_binding_rejections": 0,
        "out_of_worktree_read_quarantines": 0,
        "out_of_worktree_reads": [],
        "inter_run_alpha": {},
        "_output_path": output_path,
    }


def write_manifest(manifest: dict, output_path: str) -> None:
    """Write the manifest to disk at output_path as pretty-printed JSON.

    Sets completed_at_utc to now() if not already set.
    """
    if not manifest.get("completed_at_utc"):
        manifest["completed_at_utc"] = _now_utc()

    serializable = {k: v for k, v in manifest.items() if not k.startswith("_")}
    Path(output_path).parent.mkdir(parents=True, exist_ok=True)
    with open(output_path, "w") as f:
        json.dump(serializable, f, indent=2, sort_keys=False)


def record_pr(
    manifest: dict,
    repo: str,
    pr_number: int,
    commit_sha: str,
    difficulty: str,
) -> None:
    """Append a PR entry to manifest['prs']."""
    manifest["prs"].append({
        "repo": repo,
        "pr": pr_number,
        "commit_sha": commit_sha,
        "difficulty": difficulty,
    })


def record_skipped_pr(
    manifest: dict,
    repo: str,
    pr_number: int,
    reason: str,
) -> None:
    """Append a skipped-PR entry to manifest['skipped_prs']."""
    manifest["skipped_prs"].append({
        "repo": repo,
        "pr": pr_number,
        "reason": reason,
    })


def record_out_of_worktree_reads(
    manifest: dict,
    repo: str,
    pr_number: int,
    reads: list[str],
) -> None:
    """Surface a G4 security signal: a judge run read paths OUTSIDE the per-PR
    worktree (possible prompt-injection via the untrusted PR diff). Loud + top-level
    so it isn't buried in per-run records."""
    if not reads:
        return
    manifest.setdefault("out_of_worktree_reads", []).append({
        "repo": repo,
        "pr": pr_number,
        "reads": sorted(set(reads)),
    })
    logger.warning(
        "G4: PR #%s (%s) judge run read OUTSIDE the worktree: %s", pr_number, repo, sorted(set(reads)),
    )


def record_models(
    manifest: dict,
    *,
    skwad_review_agents: str,
    claude_ci: str,
    judge: str,
    difficulty_classifier: str,
    per_agent: dict | None = None,
) -> None:
    """Set manifest['models'] to the provided model IDs.

    The four top-level values record the team/default model per role (back-compat).
    When per_agent is provided it is stored under manifest['models']['per_agent']
    as {role: {agent_name: resolved_model}} — the authoritative per-agent detail
    now that the binary honors per-agent models.
    """
    manifest["models"] = {
        "skwad_review_agents": skwad_review_agents,
        "claude_ci": claude_ci,
        "judge": judge,
        "difficulty_classifier": difficulty_classifier,
    }
    if per_agent is not None:
        manifest["models"]["per_agent"] = per_agent


def record_prompt_hash(manifest: dict, name: str, path: str) -> None:
    """Add an additional prompt file hash to manifest['prompt_hashes']."""
    manifest["prompt_hashes"][name] = hash_prompt_file(path)
