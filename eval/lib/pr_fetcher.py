"""Fetch PR data (diff, metadata, comments) via gh CLI and clone repo via SSH."""

import json
import logging
import os
import re
import shutil
import subprocess
from pathlib import Path

logger = logging.getLogger(__name__)

EVAL_DIR = Path(__file__).resolve().parent.parent
REPOS_DIR = EVAL_DIR / "repos"


def _git(args: list[str], cwd: str | None = None, check: bool = True) -> subprocess.CompletedProcess:
    result = subprocess.run(["git", *args], cwd=cwd, capture_output=True, text=True)
    if check and result.returncode != 0:
        raise RuntimeError(f"git {' '.join(args)} failed: {result.stderr.strip()}")
    return result


def run_gh(args: list[str]) -> str:
    result = subprocess.run(
        ["gh"] + args,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(f"gh {' '.join(args)} failed: {result.stderr}")
    return result.stdout.strip()


def ssh_url_to_owner_repo(ssh_url: str) -> str:
    m = re.match(r"git@github\.com:(.+?)(?:\.git)?$", ssh_url)
    if m:
        return m.group(1)
    m = re.match(r"https://github\.com/(.+?)(?:\.git)?$", ssh_url)
    if m:
        return m.group(1)
    raise ValueError(f"Cannot parse owner/repo from: {ssh_url}")


def ssh_url_to_repo_name(ssh_url: str) -> str:
    owner_repo = ssh_url_to_owner_repo(ssh_url)
    return owner_repo.split("/")[-1]


def fetch_pr_metadata(owner_repo: str, pr_number: int) -> dict:
    raw = run_gh([
        "pr", "view", str(pr_number),
        "--repo", owner_repo,
        "--json", "title,body,baseRefName,headRefName,headRefOid,files,url",
    ])
    return json.loads(raw)


def fetch_pr_diff(owner_repo: str, pr_number: int) -> str:
    return run_gh([
        "pr", "diff", str(pr_number),
        "--repo", owner_repo,
    ])


def fetch_pr_comments(owner_repo: str, pr_number: int) -> list[dict]:
    owner, name = owner_repo.split("/")
    raw = run_gh([
        "api", f"repos/{owner}/{name}/issues/{pr_number}/comments",
        "--paginate",
    ])
    return json.loads(raw)


def clone_repo_ssh(ssh_url: str) -> str:
    repo_name = ssh_url_to_repo_name(ssh_url)
    target_dir = str(REPOS_DIR / repo_name)

    if os.path.isdir(os.path.join(target_dir, ".git")):
        logger.info("Repo already cloned at %s, fetching latest...", target_dir)
        # Detached-HEAD-safe: per-PR worktrees check out specific SHAs off this
        # shared object store, so the base clone only needs its objects refreshed.
        # `git pull` would fail on a detached HEAD and could move the shared tree —
        # `git fetch` just updates refs/objects.
        subprocess.run(
            ["git", "fetch", "origin"],
            cwd=target_dir,
            capture_output=True,
            text=True,
        )
        return target_dir

    os.makedirs(REPOS_DIR, exist_ok=True)
    result = subprocess.run(
        ["git", "clone", ssh_url, target_dir],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(f"git clone failed: {result.stderr}")
    return target_dir


def resolve_pr_head_sha(metadata: dict) -> str:
    """Pure: extract the PR head SHA (``headRefOid``) from fetch_pr_metadata JSON.

    The authoritative checked-out SHA is later re-derived from FETCH_HEAD in
    prepare_pr_checkout (avoids a force-push TOCTOU); this is the metadata view.
    """
    return metadata.get("headRefOid", "")


def _default_pr_head_fetcher(pr_data: dict) -> str | None:
    """Fetch the PR head ref into the base clone and return the FETCH_HEAD SHA.

    Derives the SHA from ``git rev-parse FETCH_HEAD`` after
    ``git fetch origin pull/<n>/head`` — NOT a separate gh/API call — so a
    force-push between metadata fetch and now can't desync the checked-out SHA.
    Returns None when the head can't be fetched (deleted fork / missing head).
    """
    base_clone = pr_data["clone_path"]
    pr_number = pr_data["pr_number"]
    fetched = _git(
        ["fetch", "origin", f"pull/{pr_number}/head"], cwd=base_clone, check=False
    )
    if fetched.returncode != 0:
        return None
    return _git(["rev-parse", "FETCH_HEAD"], cwd=base_clone).stdout.strip() or None


def prepare_pr_checkout(pr_data: dict, dest: str, *, fetcher=None) -> str | None:
    """Create an ISOLATED working tree checked out at a PR's head SHA (#30/M2).

    Resolves the head SHA via ``fetcher`` (default: fetch ``pull/<n>/head`` from
    the base clone in ``pr_data["clone_path"]`` and read FETCH_HEAD), then adds a
    detached ``git worktree`` at that SHA in ``dest``. Because every PR gets its
    own working tree off the shared object store, concurrent judges reading
    different PRs never race over one checkout.

    MUST run in the SEQUENTIAL prep phase (worktree creation mutates the base
    clone's worktree list), never the parallel executor.

    ``fetcher`` is injectable: a callable ``(pr_data) -> str | None`` so tests can
    drive the per-PR-SHA / isolation logic offline against a local fixture repo.

    Returns the checked-out SHA, or None when the PR head can't be fetched or the
    checkout fails (deleted fork / missing head) — the caller skips + records the
    PR (mirrors the existing skip pattern), never crashing the run.
    """
    base_clone = pr_data["clone_path"]
    fetcher = fetcher or _default_pr_head_fetcher
    sha = fetcher(pr_data)
    if not sha:
        return None

    # Idempotent: clear any stale worktree at this path before recreating, so
    # re-runs don't fail on an existing path.
    dest = os.path.abspath(dest)
    if os.path.exists(dest):
        _git(["worktree", "remove", "--force", dest], cwd=base_clone, check=False)
        if os.path.exists(dest):
            shutil.rmtree(dest, ignore_errors=True)
    _git(["worktree", "prune"], cwd=base_clone, check=False)
    os.makedirs(os.path.dirname(dest), exist_ok=True)

    # CAVEAT: `git worktree add/remove/prune` is NOT lock-safe across two
    # CONCURRENT eval runs sharing one base clone. Safe within a single run —
    # creation here is sequential (prep phase) — but do not run two eval runs
    # against the same base clone simultaneously.
    added = _git(["worktree", "add", "--detach", dest, sha], cwd=base_clone, check=False)
    if added.returncode != 0:
        logger.warning(
            "prepare_pr_checkout: worktree add failed for PR #%s at %s: %s",
            pr_data.get("pr_number"), sha, added.stderr.strip(),
        )
        return None
    return sha


def cleanup_pr_worktrees(worktree_specs: list[tuple[str, str]]) -> None:
    """End-of-run teardown: remove per-PR worktrees so disk doesn't grow unbounded.

    ``worktree_specs`` is a list of ``(base_clone_path, worktree_path)`` pairs.
    Each worktree is removed via ``git worktree remove --force`` (falling back to
    rmtree), then each base clone's stale worktree metadata is pruned. Best-effort:
    a failure to reclaim one worktree is logged, never raised — cleanup must not
    sink a completed run.

    Same lock-safety caveat as prepare_pr_checkout: not safe to run concurrently
    with another eval run sharing the same base clone.
    """
    bases: set[str] = set()
    for base_clone, worktree_path in worktree_specs:
        bases.add(base_clone)
        try:
            if os.path.exists(worktree_path):
                _git(["worktree", "remove", "--force", worktree_path], cwd=base_clone, check=False)
                if os.path.exists(worktree_path):
                    shutil.rmtree(worktree_path, ignore_errors=True)
        except OSError as e:  # e.g. base clone dir gone — best-effort, never raise
            logger.warning("cleanup_pr_worktrees: failed to remove %s: %s", worktree_path, e)
    for base in bases:
        try:
            _git(["worktree", "prune"], cwd=base, check=False)
        except OSError as e:
            logger.warning("cleanup_pr_worktrees: prune failed in %s: %s", base, e)


def fetch_pr(repo_ssh: str, pr_number: int) -> dict:
    owner_repo = ssh_url_to_owner_repo(repo_ssh)

    metadata = fetch_pr_metadata(owner_repo, pr_number)
    diff = fetch_pr_diff(owner_repo, pr_number)
    comments = fetch_pr_comments(owner_repo, pr_number)

    # headRefOid is GitHub's reported PR head at fetch time. We record it as
    # metadata and seed commit_sha with it; the AUTHORITATIVE checked-out SHA is
    # re-derived from `git rev-parse FETCH_HEAD` in prepare_pr_checkout (step c)
    # to avoid a force-push TOCTOU between this gh call and the fetch.
    head_ref_oid = resolve_pr_head_sha(metadata)
    return {
        "repo": owner_repo,
        "repo_ssh": repo_ssh,
        "pr_number": pr_number,
        "title": metadata.get("title", ""),
        "body": metadata.get("body", ""),
        "url": metadata.get("url", ""),
        "base_branch": metadata.get("baseRefName", "main"),
        "head_branch": metadata.get("headRefName", ""),
        "head_ref_oid": head_ref_oid,
        "commit_sha": head_ref_oid,
        "files_changed": len(metadata.get("files", [])),
        "diff": diff,
        "comments": comments,
        "clone_path": None,
    }
