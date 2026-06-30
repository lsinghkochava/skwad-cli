"""Builder + known-facts manifest for the deterministic judge fixture repo.

The judge's Read / Grep / Glob tools verify claims against a small repository
whose contents, symbols, and file layout are fixed. This module materializes
that repo into a throwaway directory and exposes the ground-truth facts the
canaries and review fixtures rely on, so tests can assert against KNOWN values
rather than magic strings.

Nothing here touches the skwad-cli project repo: ``build_fixture_repo`` copies
the source tree under ``fixtures/fixture_repo_src/`` into a caller-provided
(usually temp) directory. ``with_git=True`` runs ``git init`` + a single local
commit INSIDE that temp directory — required by the PR-checkout / per-PR
isolation tests (#30, M2) — which is unrelated to committing project changes.
"""

import json
import os
import shutil
import subprocess
from pathlib import Path

FIXTURES_DIR = Path(__file__).resolve().parent / "fixtures"
FIXTURE_REPO_SRC = FIXTURES_DIR / "fixture_repo_src"
CANARIES_FIXTURE_REPO = FIXTURES_DIR / "canaries_fixture_repo.json"
REVIEWS_DIR = FIXTURES_DIR / "reviews"

# --------------------------------------------------------------------------
# Ground truth — what the fixture repo actually contains. Canaries and the
# review fixtures are calibrated to these; the self-tests assert they hold.
# --------------------------------------------------------------------------

# Symbols that DO exist, mapped to the file (relative path) that defines them.
PRESENT_SYMBOLS = {
    "validate_token": "src/auth.py",
    "hash_password": "src/auth.py",
    "MAX_RETRIES": "src/utils.py",
    "parse_config": "src/utils.py",
    "LRUCache": "src/cache.py",
    "TOKEN_TTL_SECONDS": "src/auth.py",
}

# Symbols a fabricated claim might cite that are ABSENT everywhere → grep-refutable.
ABSENT_SYMBOLS = ["processBatchScenarios", "chargeCard", "RedisCache"]

# Files that DO exist (relative paths).
PRESENT_FILES = ["README.md", "src/__init__.py", "src/auth.py", "src/utils.py", "src/cache.py"]

# Files a fabricated claim might cite that are ABSENT → glob-refutable.
ABSENT_FILES = ["src/payment.py", "src/composables/useMediaSources.ts", "src/redis.py"]

# Literal facts a Read of the file reveals (used to calibrate read-refutable canaries).
KNOWN_FACTS = {
    "max_retries": 3,             # src/utils.py — a canary falsely claims 10
    "default_timeout": 30,        # src/utils.py
    "token_ttl_seconds": 3600,    # src/auth.py
    "eviction_policy": "LRU",     # src/cache.py — a canary falsely claims FIFO
}


def build_fixture_repo(dest: str, *, with_git: bool = False) -> str:
    """Copy the fixture source tree into ``dest`` and return ``dest``.

    ``dest`` is created if missing. When ``with_git`` is True, a local git repo
    is initialized with a single commit so the checkout / isolation tests have a
    real SHA to fetch and check out. The commit is entirely local to ``dest``.
    """
    dest_path = Path(dest)
    dest_path.mkdir(parents=True, exist_ok=True)
    for item in FIXTURE_REPO_SRC.iterdir():
        target = dest_path / item.name
        if item.is_dir():
            shutil.copytree(item, target, dirs_exist_ok=True)
        else:
            shutil.copy2(item, target)
    if with_git:
        _git_init_commit(dest_path)
    return str(dest_path)


def _git_init_commit(repo: Path) -> None:
    env = {
        **os.environ,
        "GIT_AUTHOR_NAME": "fixture",
        "GIT_AUTHOR_EMAIL": "fixture@example.com",
        "GIT_COMMITTER_NAME": "fixture",
        "GIT_COMMITTER_EMAIL": "fixture@example.com",
    }

    def _run(*args):
        subprocess.run(["git", *args], cwd=repo, env=env, check=True,
                       capture_output=True, text=True)

    _run("init", "-q")
    _run("add", "-A")
    _run("commit", "-q", "-m", "fixture repo")


def load_fixture_canaries() -> list[dict]:
    """Return the hardened canary set targeting the fixture repo."""
    with open(CANARIES_FIXTURE_REPO) as f:
        return json.load(f)


def load_review(name: str) -> str:
    """Load a review fixture by basename (e.g. 'good_review' or 'fabricated_review')."""
    return (REVIEWS_DIR / f"{name}.md").read_text()


def tool_text(result) -> str:
    """Normalize a Read/Grep/Glob result to text for substring assertions.

    The sandbox's grep/glob return type has churned between ``str`` and
    ``list[str]`` during development; tests assert on SEMANTICS (what was
    found), not the container, so they survive that churn.
    """
    if result is None:
        return ""
    if isinstance(result, str):
        return result
    return "\n".join(result)


def tool_is_empty(result) -> bool:
    """True when a Read/Grep/Glob result found nothing (handles "" / [] / None)."""
    return not result


def head_sha(repo: str) -> str:
    """Return the current HEAD commit SHA of a git repo built with with_git=True."""
    out = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=repo,
        capture_output=True, text=True, check=True,
    )
    return out.stdout.strip()


# A SHA that is valid hex but cannot exist in any fixture repo — models a
# deleted-fork PR head (#30 / M2: skip + record, never crash).
NONEXISTENT_SHA = "0" * 40


def build_multi_sha_repo(dest: str, pr_numbers) -> dict:
    """Build one git repo with a distinct commit per PR, each identifiable by
    content, for the per-PR checkout / isolation tests (#30, M2).

    For each PR number a commit OVERWRITES ``PR_HEAD.txt`` with ``PR-<n>`` so a
    checkout's identity is read straight from that file: a working tree at PR n's
    SHA must contain exactly ``PR-<n>``. A shared-clone race (last-writer-wins on
    one tree) would surface as the WRONG marker for some PR.

    Returns ``{pr_number: {"sha": <40-hex>, "marker": "PR-<n>"}}`` (insertion
    order = commit order). The repo itself is left at the last PR's commit.
    """
    repo = Path(build_fixture_repo(dest, with_git=True))
    env = {
        **os.environ,
        "GIT_AUTHOR_NAME": "fixture", "GIT_AUTHOR_EMAIL": "fixture@example.com",
        "GIT_COMMITTER_NAME": "fixture", "GIT_COMMITTER_EMAIL": "fixture@example.com",
    }

    def _run(*args):
        subprocess.run(["git", *args], cwd=repo, env=env, check=True,
                       capture_output=True, text=True)

    manifest: dict = {}
    for n in pr_numbers:
        marker = f"PR-{n}"
        (repo / "PR_HEAD.txt").write_text(marker + "\n")
        _run("add", "-A")
        _run("commit", "-q", "-m", f"pr {n} head")
        manifest[n] = {"sha": head_sha(str(repo)), "marker": marker}
    return manifest


def checkout_marker(repo: str) -> str:
    """Return the trimmed contents of PR_HEAD.txt in a checked-out tree (the
    identity marker written by build_multi_sha_repo)."""
    return (Path(repo) / "PR_HEAD.txt").read_text().strip()


def add_pull_refs(repo: str, manifest: dict) -> None:
    """Create ``refs/pull/<n>/head`` for each PR in ``manifest`` so the repo can
    serve as a local 'origin' for a fetch-based checkout (mirrors GitHub's
    ``pull/<n>/head`` refs). Lets the #30 fetch+checkout path be tested OFFLINE:
    ``git fetch <repo> pull/<n>/head`` then ``git rev-parse FETCH_HEAD``.
    """
    for n, info in manifest.items():
        subprocess.run(
            ["git", "update-ref", f"refs/pull/{n}/head", info["sha"]],
            cwd=repo, check=True, capture_output=True, text=True,
        )
