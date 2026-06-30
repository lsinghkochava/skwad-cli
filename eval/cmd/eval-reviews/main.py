#!/usr/bin/env python3
"""Review evaluation harness CLI.

Compare skwad-cli multi-agent reviews vs Claude CI reviews on real GitHub PRs.
"""

import argparse
import json
import logging
import os
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed

sys.stdout.reconfigure(line_buffering=True)
sys.stderr.reconfigure(line_buffering=True)

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", ".."))

from lib.difficulty_classifier import classify_pr, _parse_json_output
from lib.pr_fetcher import (
    fetch_pr,
    clone_repo_ssh,
    prepare_pr_checkout,
    cleanup_pr_worktrees,
    ssh_url_to_repo_name,
    ssh_url_to_owner_repo,
    REPOS_DIR,
)
from lib.review_filter import extract_claude_review
from lib.skwad_runner import run_skwad_review
# JUDGE path is now the in-process OpenAI judge (Route B). The difficulty
# classifier (classify_pr) and the skwad-review-under-test (run_skwad_review)
# remain skwad subprocesses — NOT swapped.
from lib.openai_judge import (
    score_paired_reviews,
    prepare_pr_judge_tasks,
    run_single_judge_task,
    finalize_pr_runs,
    CODEX_DEFAULT_MODEL as JUDGE_MODEL,
)
from lib import manifest as _manifest
from lib.reporter import generate_research_report
from lib.stats import compute_inter_run_alpha, check_methodology_version
from lib.pilot import evaluate_pilot_pass


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Evaluate code review quality: skwad vs Claude CI"
    )
    parser.add_argument(
        "--pr-file",
        help='JSON file: [{"repo_ssh": "git@github.com:Org/Repo.git", "prs": [1687]}]',
    )
    parser.add_argument(
        "--repo-ssh",
        help="Single repo SSH URL (e.g. git@github.com:Kochava/frontend-mos.git)",
    )
    parser.add_argument("--pr", type=int, help="Single PR number (use with --repo-ssh)")
    parser.add_argument(
        "--judge-model",
        default="claude-sonnet-4-20250514",
        help="Model for judge (metadata only, judge uses skwad-cli)",
    )
    parser.add_argument(
        "--skwad-binary",
        default="./skwad-cli",
        help="Path to skwad-cli binary",
    )
    parser.add_argument(
        "--skwad-config",
        default="./test_configs/skwad_review_team.json",
        help="Path to skwad review team config",
    )
    parser.add_argument(
        "--judge-config",
        default=None,
        help="Path to judge team config (default: eval/config/judge_team.json)",
    )
    parser.add_argument(
        "--output-dir",
        default="eval/output",
        help="Output directory for reports",
    )
    parser.add_argument(
        "--timeout",
        type=int,
        default=1200,
        help="Timeout in seconds for skwad run and judge subprocess",
    )
    parser.add_argument(
        "--research-mode",
        action="store_true",
        default=False,
        help="Generate full 11-section research report (eval/output/research-report.md)",
    )
    parser.add_argument(
        "--seed",
        type=int,
        default=12345,
        help="RNG seed for reproducible counterbalanced judge run-3 and manifest",
    )
    parser.add_argument(
        "--inject-canary",
        dest="inject_canary",
        default=None,
        help="Path to canaries.json fixture file to inject fabricated claims for pilot validation",
    )
    parser.add_argument(
        "--max-workers",
        type=int,
        default=4,
        help="Max concurrent judge subprocesses across all PR×run tasks (default 4)",
    )
    parser.add_argument(
        "--on-out-of-worktree-read",
        choices=["flag", "fail"],
        default="flag",
        help="G4 policy when a judge run reads OUTSIDE the per-PR worktree: 'flag' "
             "(record + warn, still score — default for semi-trusted pilot) or 'fail' "
             "(quarantine the run from scoring — for untrusted PRs).",
    )
    parser.add_argument(
        "--resume", "--judge-only",
        dest="resume",
        action="store_true",
        default=False,
        help="Re-run ONLY the codex judge against already-cached reviews (the cached "
             "comments_pr{N}.md in each per-PR worktree). Skips clone/checkout/review "
             "generation and leaves worktrees intact on exit (iterate fix→resume).",
    )
    parser.add_argument(
        "--allow-drift",
        dest="allow_drift",
        action="store_true",
        default=False,
        help="On --resume, judge a PR even if its cached worktree HEAD no longer "
             "matches the live PR head SHA (default: skip drifted PRs — judging a diff "
             "that doesn't match the checked-out code yields invalid scores).",
    )
    parser.add_argument(
        "--report-from",
        dest="report_from",
        default=None,
        help="Re-render the research report (research-report.md) over an ALREADY-FINISHED "
             "output dir, with NO re-judge. Reconstructs pr_results from the recorded "
             "manifest + judge_pr*_voted/run JSON (CI review re-fetched cheaply), then "
             "exits. Stats are recomputed at render time, so they reproduce exactly.",
    )
    return parser.parse_args()


def load_repo_pr_list(args: argparse.Namespace) -> list[dict]:
    if args.pr_file:
        with open(args.pr_file) as f:
            return json.load(f)
    elif args.repo_ssh and args.pr:
        return [{"repo_ssh": args.repo_ssh, "prs": [args.pr]}]
    else:
        print("Error: provide --pr-file or both --repo-ssh and --pr")
        sys.exit(1)


def _read_model_from_config(path: str) -> str:
    """Extract model field from team config JSON. Returns 'unknown' if absent."""
    try:
        with open(path) as f:
            d = json.load(f)
        if d.get("model"):
            return d["model"]
        agents = d.get("agents", [])
        if agents and agents[0].get("model"):
            return agents[0]["model"]
    except Exception:
        pass
    return "unknown"


def _read_agent_models_from_config(path: str) -> dict[str, str]:
    """Map each agent name to its resolved model from a team config JSON.

    Mirrors the binary's precedence: per-agent `agents[].model` > top-level
    `model` (team default). When an agent declares neither, record 'default'
    rather than guessing (Python sees no global ClaudeOptions equivalent).
    Returns {} if the config can't be read.
    """
    result: dict[str, str] = {}
    try:
        with open(path) as f:
            d = json.load(f)
    except Exception:
        return result
    top_level = d.get("model") or None
    for agent in d.get("agents", []):
        name = agent.get("name") or "unknown"
        result[name] = agent.get("model") or top_level or "default"
    return result


logger = logging.getLogger(__name__)


def load_canaries(path: str | None) -> list[dict]:
    """Load canary fixtures from a JSON file, or return empty list if path is None."""
    if not path:
        return []
    with open(path) as f:
        return json.load(f)


def prepare_pr(
    pr_data: dict,
    clone_path: str,
    skwad_binary: str,
    skwad_config: str,
    judge_config: str | None,
    run_dir: str,
    seed: int,
    timeout: int,
    run_manifest: dict,
    canary_injections: list[dict] | None = None,
    *,
    judge_model: str | None = None,
    on_out_of_worktree_read: str = "flag",
) -> dict | None:
    """Sequential per-PR preparation: extract CI review, classify, generate the
    skwad review, and build the judge task specs. Does NO judge scoring.

    Returns a PR context (with `tasks`/`assignments` for the parallel scoring
    phase) or None if the PR is skipped (recorded in the manifest).
    """
    repo = pr_data["repo"]
    pr_number = pr_data["pr_number"]
    commit_sha = pr_data.get("commit_sha", "")

    print(f"\n  --- PR #{pr_number} ---")

    print("    Extracting Claude CI review...")
    claude_ci_review = extract_claude_review(pr_data["comments"])
    if claude_ci_review is None:
        print("    WARNING: No Claude CI review found, skipping")
        _manifest.record_skipped_pr(run_manifest, repo, pr_number, "Claude CI comment not found")
        return None

    print("    Classifying PR difficulty...")
    difficulty = classify_pr(pr_data, skwad_binary=skwad_binary, repo_path=clone_path)
    print(
        f"    Difficulty: {difficulty['bucket']}"
        f" (heuristic={difficulty['heuristic_bucket']}, delta={difficulty['llm_delta']:+d})"
    )
    if difficulty["llm_delta"] != 0:
        logger.warning(
            "PR #%d difficulty disagreement: heuristic=%s final=%s delta=%+d",
            pr_number, difficulty["heuristic_bucket"], difficulty["bucket"], difficulty["llm_delta"],
        )

    print(f"    Running skwad review (timeout: {timeout}s)...")
    skwad_review = run_skwad_review(
        repo_path=clone_path,
        pr_url=pr_data["url"],
        pr_number=pr_number,
        skwad_binary=skwad_binary,
        config_path=skwad_config,
        timeout=timeout,
    )
    if not skwad_review or not skwad_review.strip():
        print("    WARNING: skwad review empty, skipping")
        _manifest.record_skipped_pr(run_manifest, repo, pr_number, "skwad review empty")
        return None

    # JUDGE = grounded `codex exec` (Route C). No client/system-prompt to build —
    # Codex is invoked per run against the per-PR worktree (clone_path).
    judge_model = judge_model or JUDGE_MODEL
    tasks, assignments = prepare_pr_judge_tasks(
        pr_data, skwad_review, claude_ci_review, clone_path, seed, run_dir,
        model=judge_model, config_path=judge_config,
        canary_injections=canary_injections or [],
        on_out_of_worktree_read=on_out_of_worktree_read,
    )

    return {
        "pr_data": pr_data,
        "repo": repo,
        "pr_number": pr_number,
        "commit_sha": commit_sha,
        "difficulty": difficulty,
        "skwad_review": skwad_review,
        "claude_ci_review": claude_ci_review,
        "run_dir": run_dir,
        "tasks": tasks,
        "assignments": assignments,
    }


def _git_head_sha(worktree_path: str) -> str | None:
    """Authoritative checked-out SHA of a worktree (`git -C <wt> rev-parse HEAD`).
    Returns None if git can't report it (used by the resume drift guard)."""
    try:
        r = subprocess.run(
            ["git", "-C", worktree_path, "rev-parse", "HEAD"],
            capture_output=True, text=True, timeout=30,
        )
        if r.returncode == 0:
            return r.stdout.strip()
        logger.warning("resume: git rev-parse HEAD failed in %s: %s",
                       worktree_path, (r.stderr or "").strip()[:200])
    except Exception as e:
        logger.warning("resume: git rev-parse HEAD error in %s: %s", worktree_path, e)
    return None


def _git_local_diff(worktree_path: str, base: str) -> str | None:
    """Worktree-derived PR diff: `git -C <wt> diff origin/<base>...HEAD`. The three-dot
    form is merge-base→HEAD = GitHub PR-diff semantics (robust to the base advancing).
    Returns the diff text, or None if git errors (e.g. origin/<base> unreachable)."""
    try:
        r = subprocess.run(
            ["git", "-C", worktree_path, "diff", f"origin/{base}...HEAD"],
            capture_output=True, text=True, timeout=60,
        )
        if r.returncode == 0:
            return r.stdout
        logger.warning("resume: local diff derivation failed in %s (origin/%s...HEAD): %s",
                       worktree_path, base, (r.stderr or "").strip()[:200])
    except Exception as e:
        logger.warning("resume: local diff derivation error in %s: %s", worktree_path, e)
    return None


def _resume_difficulty(worktree_path: str, pr_data: dict, skwad_binary: str) -> dict:
    """Difficulty for a resumed PR: prefer the cached classifier_output.json (no LLM
    call); fall back to re-running the cheap classifier; default to 'medium' if both
    fail. Tags `source` so the report can distinguish loaded vs recomputed."""
    cls_path = os.path.join(worktree_path, "classifier_output.json")
    if os.path.exists(cls_path):
        try:
            with open(cls_path) as f:
                parsed = _parse_json_output(f.read())
            bucket = (parsed.get("bucket") or "medium").lower()
            # heuristic_bucket/llm_delta aren't persisted in the cache file; report the
            # loaded bucket as both, delta 0, and mark source="loaded" (not misleading 0s).
            return {"bucket": bucket, "reasoning": parsed.get("reasoning", ""),
                    "heuristic_bucket": bucket, "llm_delta": 0, "source": "loaded"}
        except Exception as e:
            logger.warning("resume: cached classifier_output.json unparseable (%s); re-classifying", e)
    try:
        return {**classify_pr(pr_data, skwad_binary=skwad_binary, repo_path=worktree_path),
                "source": "reclassified"}
    except Exception as e:
        logger.warning("resume: classify_pr fallback failed (%s); defaulting to 'medium'", e)
        return {"bucket": "medium", "reasoning": "", "heuristic_bucket": "medium",
                "llm_delta": 0, "source": "default"}


def prepare_pr_resume(
    repo_ssh: str,
    repo_name: str,
    pr_number: int,
    *,
    output_dir: str,
    judge_config: str | None,
    seed: int,
    timeout: int,
    run_manifest: dict,
    canary_injections: list[dict] | None,
    judge_model: str | None,
    on_out_of_worktree_read: str,
    skwad_binary: str,
    allow_drift: bool,
) -> dict | None:
    """Resume-mode per-PR preparation: re-run ONLY the codex judge against the cached
    skwad review in the existing per-PR worktree. NO clone, NO checkout, NO review
    regeneration, and the worktree is NOT registered for teardown. Returns a PR context
    (same shape as prepare_pr) or None if the PR is skipped (recorded in the manifest)."""
    repo = ssh_url_to_owner_repo(repo_ssh)
    worktree_path = os.path.join(str(REPOS_DIR), "worktrees", f"{repo_name}-pr{pr_number}")
    print(f"\n  --- PR #{pr_number} (resume) ---")

    # (a) Worktree must already exist — resume never creates it.
    if not os.path.isdir(worktree_path):
        print(f"    SKIP: worktree absent on resume: {worktree_path}")
        _manifest.record_skipped_pr(run_manifest, repo, pr_number, "worktree absent on resume")
        return None

    # (b) Cached skwad review (comments_pr{N}.md) must be present and non-empty.
    comments_path = os.path.join(worktree_path, f"comments_pr{pr_number}.md")
    if not os.path.exists(comments_path):
        print(f"    SKIP: cached skwad review absent: {comments_path}")
        _manifest.record_skipped_pr(
            run_manifest, repo, pr_number,
            f"cached skwad review (comments_pr{pr_number}.md) absent on resume")
        return None
    with open(comments_path) as f:
        skwad_review = f.read()
    if not skwad_review.strip():
        print("    SKIP: cached skwad review empty")
        _manifest.record_skipped_pr(run_manifest, repo, pr_number, "cached skwad review empty on resume")
        return None

    # (d) Re-fetch live PR data (diff + comments) for the judge + drift guard.
    try:
        pr_data = fetch_pr(repo_ssh, pr_number)
    except Exception as e:
        logger.exception("resume: fetch_pr failed for PR #%d: %s", pr_number, e)
        _manifest.record_skipped_pr(run_manifest, repo, pr_number, f"fetch_pr failed on resume: {e}")
        return None
    repo = pr_data["repo"]
    pr_data["clone_path"] = worktree_path

    claude_ci_review = extract_claude_review(pr_data["comments"])
    if claude_ci_review is None:
        print("    SKIP: no Claude CI review found")
        _manifest.record_skipped_pr(run_manifest, repo, pr_number, "Claude CI comment not found")
        return None

    # DRIFT-PROOFING: derive the diff from the CACHED worktree so a force-pushed PR
    # (worktree HEAD != live head, e.g. #1818) still scores against the code that was
    # actually reviewed. With a derived diff, worktree HEAD == head_ref_oid by
    # construction → no drift. (CI comments / claude_ci review persist across force-push;
    # only the diff needs pinning.)
    worktree_head = _git_head_sha(worktree_path)
    if worktree_head:
        base = pr_data.get("base_branch", "main")
        local_diff = _git_local_diff(worktree_path, base)
        if local_diff and local_diff.strip():
            pr_data["diff"] = local_diff
            pr_data["commit_sha"] = worktree_head
            pr_data["head_ref_oid"] = worktree_head  # diff matches reviewed code now
            logger.info("resume PR #%d: using worktree-derived diff (origin/%s...HEAD @ %s)",
                        pr_number, base, worktree_head[:12])

    # (e) DRIFT GUARD → FALLBACK (condition 6). After derivation, worktree_head ==
    # head_ref_oid for derivable PRs, so this won't trip. It still SKIPS conservatively
    # when derivation was IMPOSSIBLE — worktree HEAD unknown, origin/<base> unreachable
    # (git diff errored), or an EMPTY local diff → head_ref_oid stays the LIVE value, so
    # the heads mismatch. --allow-drift overrides (judge the live diff anyway).
    live_head = pr_data.get("head_ref_oid")
    if not allow_drift:
        if worktree_head is None:
            msg = "could not read worktree HEAD for drift check"
            print(f"    SKIP: {msg} (use --allow-drift to override)")
            _manifest.record_skipped_pr(run_manifest, repo, pr_number, msg)
            return None
        if not live_head:
            msg = "could not determine live PR head for drift check (use --allow-drift to override)"
            print(f"    SKIP: {msg}")
            _manifest.record_skipped_pr(run_manifest, repo, pr_number, msg)
            return None
        if worktree_head != live_head:
            msg = (f"worktree HEAD {worktree_head} != live {live_head} and local diff "
                   f"derivation unavailable — possible drift")
            logger.warning("PR #%d %s — SKIPPING (use --allow-drift to override)", pr_number, msg)
            print(f"    SKIP: {msg} (use --allow-drift to override)")
            _manifest.record_skipped_pr(run_manifest, repo, pr_number, msg)
            return None
    elif worktree_head and live_head and worktree_head != live_head:
        logger.warning("PR #%d drift worktree=%s live=%s — proceeding (--allow-drift, live diff)",
                       pr_number, worktree_head, live_head)
        print("    WARNING: drift — proceeding on live diff (--allow-drift)")
    commit_sha = worktree_head or live_head or pr_data.get("commit_sha", "")
    pr_data["commit_sha"] = commit_sha

    # (c) Difficulty: cached file → cheap reclassify → default.
    difficulty = _resume_difficulty(worktree_path, pr_data, skwad_binary)
    print(f"    Difficulty: {difficulty['bucket']} (source={difficulty.get('source')})")

    # (f) Same judge-task builder + context shape as the fresh path → converge.
    safe_repo = repo.replace("/", "-")
    run_dir = os.path.join(output_dir, f"{safe_repo}-{pr_number}")
    judge_model = judge_model or JUDGE_MODEL
    tasks, assignments = prepare_pr_judge_tasks(
        pr_data, skwad_review, claude_ci_review, worktree_path, seed, run_dir,
        model=judge_model, config_path=judge_config,
        canary_injections=canary_injections or [],
        on_out_of_worktree_read=on_out_of_worktree_read,
    )
    return {
        "pr_data": pr_data,
        "repo": repo,
        "pr_number": pr_number,
        "commit_sha": commit_sha,
        "difficulty": difficulty,
        "skwad_review": skwad_review,
        "claude_ci_review": claude_ci_review,
        "run_dir": run_dir,
        "tasks": tasks,
        "assignments": assignments,
        "resumed": True,
    }


def assemble_pr_result(context: dict, scored: dict, run_manifest: dict) -> dict:
    """Single-threaded: record canary outcomes + the PR in the manifest and build
    the pr_results entry from a PR context and its finalized `scored` dict."""
    repo = context["repo"]
    pr_number = context["pr_number"]

    for outcome in scored.get("canary_outcomes", []):
        run_manifest.setdefault("canary_outcomes", []).append(outcome)
        if not outcome.get("passed"):
            print(f"    CANARY FAILED: id={outcome.get('id')} "
                  f"expected={outcome.get('expected_outcome')} "
                  f"actual={outcome.get('actual_outcome')}")

    _manifest.record_pr(run_manifest, repo, pr_number, context["commit_sha"],
                        context["difficulty"]["bucket"])

    return {
        "pr_data": context["pr_data"],
        "repo": repo,
        "pr": pr_number,
        "commit_sha": context["commit_sha"],
        "difficulty": context["difficulty"],
        "resumed": context.get("resumed", False),
        "skwad_review": context["skwad_review"],
        "claude_ci_review": context["claude_ci_review"],
        "skwad": scored["skwad"],
        "claude_ci": scored["claude_ci"],
        "runs": scored["runs"],
        "ab_assignments": scored["ab_assignments"],
        "n_runs_completed": scored["n_runs_completed"],
        "n_runs_planned": scored["n_runs_planned"],
        "run_durations_seconds": scored.get("run_durations_seconds", []),
    }


def evaluate_pr(
    pr_data: dict,
    clone_path: str,
    skwad_binary: str,
    skwad_config: str,
    judge_config: str | None,
    run_dir: str,
    seed: int,
    timeout: int,
    run_manifest: dict,
    canary_injections: list[dict] | None = None,
    pilot_counters: dict | None = None,
) -> dict | None:
    """Sequential single-PR evaluation (prepare → score → assemble). Retained for
    direct/single-PR use; the parallel orchestrator in main() uses prepare_pr +
    the judge worker pool + finalize_pr_runs instead."""
    context = prepare_pr(
        pr_data, clone_path, skwad_binary, skwad_config, judge_config, run_dir,
        seed, timeout, run_manifest, canary_injections=canary_injections,
        judge_model=JUDGE_MODEL,  # legacy single-PR path → default on_out_of_worktree_read="flag"
    )
    if context is None:
        return None

    print("    Scoring paired reviews (3 runs)...")
    scored = score_paired_reviews(
        pr_data=pr_data,
        skwad_review=context["skwad_review"],
        claude_ci_review=context["claude_ci_review"],
        repo_path=clone_path,
        seed=seed,
        run_dir=run_dir,
        config_path=judge_config,
        canary_injections=canary_injections or [],
        pilot_counters=pilot_counters,
    )
    return assemble_pr_result(context, scored, run_manifest)


def render_report_from_dir(report_dir: str) -> None:
    """Re-render the research report over an ALREADY-FINISHED output dir — NO re-judge.
    Reconstructs pr_results from the recorded manifest + per-PR judge_pr*_voted/run JSON
    (the skwad review from its cached worktree; the CI review re-fetched cheaply), then
    calls the canonical generate_research_report. generate_research_report recomputes all
    stats from the recorded voted totals, so the render reproduces them exactly."""
    manifest_path = os.path.join(report_dir, "manifest.json")
    with open(manifest_path) as f:
        manifest = json.load(f)

    pr_results: list[dict] = []
    none_ci: list[int] = []
    for entry in manifest.get("prs", []):
        repo = entry["repo"]                      # e.g. "Kochava/frontend-mos"
        pr = entry["pr"]
        safe_repo = repo.replace("/", "-")
        pr_dir = os.path.join(report_dir, f"{safe_repo}-{pr}")
        voted_path = os.path.join(pr_dir, f"judge_pr{pr}_voted.json")
        if not os.path.exists(voted_path):
            logger.warning("report-from: %s missing — skipping PR %s", voted_path, pr)
            continue
        with open(voted_path) as f:
            voted = json.load(f)
        runs = []
        for ri in (1, 2, 3):
            run_path = os.path.join(pr_dir, f"judge_pr{pr}_run{ri}.json")
            if os.path.exists(run_path):
                with open(run_path) as f:
                    runs.append(json.load(f))

        # Back-fill tool_calls_observed from the recorded per-run `command_count` (REAL
        # data). The voted verification_summary predates the live back-fill, so its
        # tool_calls_observed is the placeholder 0. The live path sets each run's value to
        # that run's command_count then SUMS across runs, so the aggregate = Σ command_count
        # (codex has one shared stream → skwad and claude_ci sides get the same total).
        # Don't fabricate: only count runs that actually recorded command_count.
        cmd_counts = [r["command_count"] for r in runs if isinstance(r.get("command_count"), int)]
        if cmd_counts:
            cmd_total = sum(cmd_counts)
            for sys_ in ("skwad", "claude_ci"):
                vs = voted.get(sys_, {}).get("verification_summary")
                if isinstance(vs, dict):
                    vs["tool_calls_observed"] = cmd_total

        # skwad review: the cached comments file in the per-PR worktree.
        repo_name = repo.split("/")[-1]
        review_path = os.path.join(
            str(REPOS_DIR), "worktrees", f"{repo_name}-pr{pr}", f"comments_pr{pr}.md"
        )
        skwad_review = "(none)"
        if os.path.exists(review_path):
            with open(review_path) as f:
                skwad_review = f.read()

        # CI review: cheap comments re-fetch (NOT a judge run); any failure → "(none)".
        ci_review = "(none)"
        try:
            ci = extract_claude_review(fetch_pr(f"git@github.com:{repo}.git", pr)["comments"])
            ci_review = ci if ci else "(none)"
        except Exception as e:
            logger.warning("report-from: CI review fetch failed for PR %s: %s", pr, e)
        if ci_review == "(none)":
            none_ci.append(pr)

        pr_results.append({
            "pr_data": {"repo": repo, "pr_number": int(pr)},  # reporter reads repo + pr_number
            "repo": repo,
            "pr": int(pr),
            "commit_sha": entry.get("commit_sha", ""),
            "difficulty": {"bucket": entry.get("difficulty", "unknown")},  # wrap → sec4/5 render
            "skwad": voted["skwad"],
            "claude_ci": voted["claude_ci"],
            "runs": runs,
            "ab_assignments": [r.get("ab_assignment") for r in runs],
            "n_runs_completed": len(runs),
            "n_runs_planned": 3,
            "skwad_review": skwad_review,
            "claude_ci_review": ci_review,
            "run_durations_seconds": [r.get("duration_seconds", 0) for r in runs],
        })

    pr_results.sort(key=lambda r: (r["repo"], r["pr"]))  # deterministic order
    out_path = os.path.join(report_dir, "research-report.md")
    generate_research_report(pr_results, manifest, out_path)
    print(f"Report rendered: {out_path}  ({len(pr_results)} PR(s))")
    if none_ci:
        print(f"  CI review fell back to (none) for PR(s): {sorted(none_ci)}")


def main():
    args = parse_args()
    # --report-from: re-render the report over a finished dir, NO judge/checkout. Diverge
    # BEFORE any other setup (mirrors the resume early-branch pattern).
    if args.report_from:
        render_report_from_dir(args.report_from)
        return
    repo_entries = load_repo_pr_list(args)

    total_prs = sum(len(e["prs"]) for e in repo_entries)
    print(f"Evaluating {total_prs} PR(s) across {len(repo_entries)} repo(s)")
    print(f"Skwad binary: {args.skwad_binary}")
    print(f"Skwad config: {args.skwad_config}")
    print(f"Judge model (metadata): {args.judge_model}")
    print(f"Output dir: {args.output_dir}")
    print(f"Research mode: {args.research_mode}")
    print(f"Seed: {args.seed}")

    os.makedirs(args.output_dir, exist_ok=True)
    manifest_path = os.path.join(args.output_dir, "manifest.json")
    run_manifest = _manifest.open_manifest(manifest_path, seed=args.seed)

    canaries = load_canaries(args.inject_canary)
    if canaries:
        print(f"Canary injection: {len(canaries)} canary(ies) loaded from {args.inject_canary}")

    # Model auto-detection from team config JSONs; fallback to "unknown" with warning.
    _eval_config_dir = os.path.join(os.path.dirname(__file__), "..", "..", "config")
    judge_config_path = args.judge_config or os.path.join(_eval_config_dir, "judge_team.json")
    classifier_config_path = os.path.join(_eval_config_dir, "classifier_team.json")

    skwad_model = _read_model_from_config(args.skwad_config)
    judge_model_detected = _read_model_from_config(judge_config_path)
    classifier_model = _read_model_from_config(classifier_config_path)

    for label, val, path in [
        ("skwad review agents", skwad_model, args.skwad_config),
        ("judge", judge_model_detected, judge_config_path),
        ("difficulty classifier", classifier_model, classifier_config_path),
    ]:
        if val == "unknown":
            logger.warning("Could not detect %s model from %s", label, path)

    # Per-agent model detail (now that the binary honors agents[].model). The
    # JUDGE is the in-process OpenAI judge (Route B), NOT the skwad persona config.
    per_agent_models = {
        "skwad_review_agents": _read_agent_models_from_config(args.skwad_config),
        "judge": {"Judge": JUDGE_MODEL},
        "difficulty_classifier": _read_agent_models_from_config(classifier_config_path),
    }

    _manifest.record_models(
        run_manifest,
        skwad_review_agents=skwad_model,
        claude_ci=args.judge_model,
        judge=JUDGE_MODEL,
        difficulty_classifier=classifier_model,
        per_agent=per_agent_models,
    )

    pr_results: list[dict] = []
    pilot_counters: dict = {
        "confabulation_rejections": 0,
        "disallowed_tool_rejections": 0,
        "structural_invalid_rejections": 0,
        "evidence_binding_rejections": 0,
        "out_of_worktree_read_quarantines": 0,
    }

    # ----- Phase A (SEQUENTIAL): clone per repo, per-PR checkout + prep -----
    # The base clone is fetched once per repo (shared object store). Each PR then
    # gets its OWN isolated worktree checked out at the PR head SHA (#30/M2),
    # created HERE in the sequential phase so concurrent judges in the parallel
    # scoring phase can't race over one working tree. The OpenAI judge has no
    # MCP server, so there are no ports to deconflict — API rate limits (handled
    # via RateLimited backoff) replace port-collision avoidance.
    contexts: list[dict] = []
    flat_tasks: list[dict] = []
    # (base_clone, worktree_path) pairs to reclaim at end of run (disk teardown).
    worktree_specs: list[tuple[str, str]] = []
    for repo_entry in repo_entries:
        repo_ssh = repo_entry["repo_ssh"]
        prs = repo_entry["prs"]
        repo_name = ssh_url_to_repo_name(repo_ssh)

        # EARLY BRANCH (--resume): diverge BEFORE any clone/checkout/worktree
        # registration. The destructive ops (clone_repo_ssh, prepare_pr_checkout,
        # worktree_specs.append, end-of-run cleanup) are STRUCTURALLY unreachable on
        # resume — we re-judge cached reviews in the existing worktrees and `continue`.
        if args.resume:
            print(f"\n{'='*60}")
            print(f"Resume (judge-only): {repo_ssh}")
            print(f"{'='*60}")
            for pr_number in prs:
                try:
                    context = prepare_pr_resume(
                        repo_ssh, repo_name, pr_number,
                        output_dir=args.output_dir,
                        judge_config=args.judge_config,
                        seed=args.seed,
                        timeout=args.timeout,
                        run_manifest=run_manifest,
                        canary_injections=canaries,
                        judge_model=JUDGE_MODEL,
                        on_out_of_worktree_read=args.on_out_of_worktree_read,
                        skwad_binary=args.skwad_binary,
                        allow_drift=args.allow_drift,
                    )
                except Exception as e:
                    logger.exception("ERROR resuming PR #%d: %s", pr_number, e)
                    print(f"    ERROR resuming PR #{pr_number}: {e}")
                    # Account for the PR in the manifest — an unexpected exception must
                    # not make a requested PR silently vanish from the report.
                    _manifest.record_skipped_pr(
                        run_manifest, ssh_url_to_owner_repo(repo_ssh), pr_number,
                        f"resume prep failed: {type(e).__name__}: {e}",
                    )
                    continue
                if context is None:
                    continue
                contexts.append(context)
                flat_tasks.extend(context["tasks"])
            continue

        print(f"\n{'='*60}")
        print(f"Cloning {repo_ssh}...")
        print(f"{'='*60}")

        try:
            clone_path = clone_repo_ssh(repo_ssh)
            print(f"  Cloned to: {clone_path}")
        except RuntimeError as e:
            print(f"  ERROR cloning {repo_ssh}: {e}")
            continue

        for pr_number in prs:
            try:
                pr_data = fetch_pr(repo_ssh, pr_number)
                pr_data["clone_path"] = clone_path  # base clone (object store) for the fetcher
                repo = pr_data["repo"]
                safe_repo = repo.replace("/", "-")
                run_dir = os.path.join(args.output_dir, f"{safe_repo}-{pr_number}")

                # Per-PR isolation + PR-head checkout (#30/M2). A deleted-fork /
                # missing head → None → skip + record (mirror the existing skip
                # pattern), never crash the run.
                worktree_path = os.path.join(
                    str(REPOS_DIR), "worktrees", f"{repo_name}-pr{pr_number}"
                )
                head_sha = prepare_pr_checkout(pr_data, worktree_path)
                if head_sha is None:
                    print(f"    SKIP PR #{pr_number}: PR head unavailable (deleted fork?)")
                    _manifest.record_skipped_pr(
                        run_manifest, repo, pr_number,
                        "PR head checkout failed (deleted fork or missing head)",
                    )
                    continue

                worktree_specs.append((clone_path, worktree_path))
                pr_data["clone_path"] = worktree_path
                pr_data["commit_sha"] = head_sha
                if pr_data.get("head_ref_oid") and pr_data["head_ref_oid"] != head_sha:
                    logger.warning(
                        "PR #%d head moved between metadata and fetch: "
                        "headRefOid=%s FETCH_HEAD=%s (using FETCH_HEAD)",
                        pr_number, pr_data["head_ref_oid"], head_sha,
                    )

                context = prepare_pr(
                    pr_data=pr_data,
                    clone_path=worktree_path,
                    skwad_binary=args.skwad_binary,
                    skwad_config=args.skwad_config,
                    judge_config=args.judge_config,
                    run_dir=run_dir,
                    seed=args.seed,
                    timeout=args.timeout,
                    run_manifest=run_manifest,
                    canary_injections=canaries,
                    judge_model=JUDGE_MODEL,
                    on_out_of_worktree_read=args.on_out_of_worktree_read,
                )
                if context is None:
                    continue
                contexts.append(context)
                flat_tasks.extend(context["tasks"])
            except Exception as e:
                logger.exception("ERROR preparing PR #%d: %s", pr_number, e)
                print(f"    ERROR preparing PR #{pr_number}: {e}")

    # ----- Phase B (PARALLEL): score all PR×run judge tasks in a worker pool -----
    # Wall-clock is measured around THIS phase only (excludes sequential clone/pull
    # + prep) — it is the concurrency-sensitive signal the cost gate evaluates.
    expected_tasks = len(flat_tasks)
    results_by_pr: dict[int, list[dict]] = {}
    failed_tasks: list[dict] = []
    scoring_wallclock = 0.0
    if flat_tasks:
        print(f"\nScoring {expected_tasks} judge task(s) with max_workers={args.max_workers}...")
        t_scoring_start = time.perf_counter()
        with ThreadPoolExecutor(max_workers=args.max_workers) as pool:
            future_to_task = {
                pool.submit(run_single_judge_task, task): task for task in flat_tasks
            }
            for future in as_completed(future_to_task):
                task = future_to_task[future]
                try:
                    res = future.result()
                except Exception as e:
                    # Defensive: run_single_judge_task should not raise, but never
                    # let one task sink the whole run.
                    logger.exception("Judge task PR#%s run %s crashed: %s",
                                     task.get("pr_number"), task.get("run_index"), e)
                    res = {
                        "pr_number": task["pr_number"],
                        "run_index": task["run_index"],
                        "status": "failed",
                        "error": f"{type(e).__name__}: {e}",
                        "counter_increment": None,
                        "duration_seconds": 0.0,
                        "resolved": None, "run_record": None, "canary_outcomes": [],
                    }
                results_by_pr.setdefault(res["pr_number"], []).append(res)
                if res.get("status") != "ok":
                    failed_tasks.append({
                        "pr_number": res["pr_number"],
                        "run_index": res.get("run_index"),
                        "error": res.get("error"),
                    })
        scoring_wallclock = time.perf_counter() - t_scoring_start

    run_manifest["total_wallclock_seconds"] = scoring_wallclock
    run_manifest["expected_tasks"] = expected_tasks
    run_manifest["max_workers"] = args.max_workers
    if failed_tasks:
        run_manifest["failed_tasks"] = failed_tasks
        print(f"  {len(failed_tasks)} judge task(s) failed (recorded, run continues)")

    # ----- Phase C (SEQUENTIAL): merge counters, vote/aggregate per PR -----
    # Bundling observability rollup (across all PR×run results): sum of bundled
    # command_execution events, max sub-commands seen in any one event.
    bundled_command_events = 0
    max_bundled_subcommands = 0
    # Soft-signal: low-grounding alarm rollup (mirror the out-of-worktree-read flagging) —
    # one entry per (PR, run, system) whose grounding_rate fell below the floor.
    low_grounding_runs: list[dict] = []
    for context in contexts:
        pr_number = context["pr_number"]
        run_results = results_by_pr.get(pr_number, [])
        # Apply pilot counters single-threaded (parallel callable never mutated them).
        for res in run_results:
            key = res.get("counter_increment")
            if key:
                pilot_counters[key] = pilot_counters.get(key, 0) + 1
            # G4: surface out-of-worktree reads loudly into the manifest (top-level).
            oow = res.get("out_of_worktree_reads") or []
            if oow:
                _manifest.record_out_of_worktree_reads(run_manifest, context["repo"], pr_number, oow)
            bundled_command_events += res.get("bundled_command_events", 0)
            max_bundled_subcommands = max(max_bundled_subcommands,
                                          res.get("max_bundled_subcommands", 0))
            for system, gs in (res.get("low_grounding") or {}).items():
                low_grounding_runs.append({
                    "repo": context["repo"],
                    "pr": pr_number,
                    "run_index": res.get("run_index"),
                    "system": system,
                    "grounding_rate": gs.get("grounding_rate"),
                    "grounded": gs.get("grounded"),
                    "grounding_eligible": gs.get("grounding_eligible"),
                })
        try:
            scored = finalize_pr_runs(
                pr_number, run_results, context["assignments"], context["run_dir"],
                canary_injections=canaries,
            )
        except Exception as e:
            logger.exception("ERROR finalizing PR #%d: %s", pr_number, e)
            print(f"    ERROR finalizing PR #{pr_number}: {e}")
            continue
        pr_results.append(assemble_pr_result(context, scored, run_manifest))

    # Methodology version gate — refuse to aggregate v1+v2 records.
    # Each pr_results entry inherits the run manifest's methodology_version.
    expected_version = run_manifest.get("methodology_version", 2)
    versioned_results = [
        {**r, "methodology_version": r.get("methodology_version", expected_version)}
        for r in pr_results
    ]
    check_methodology_version(versioned_results)

    # Persist rejection counters into manifest.
    run_manifest["confabulation_rejections"] = pilot_counters["confabulation_rejections"]
    run_manifest["disallowed_tool_rejections"] = pilot_counters["disallowed_tool_rejections"]
    run_manifest["structural_invalid_rejections"] = pilot_counters.get(
        "structural_invalid_rejections", 0
    )
    run_manifest["evidence_binding_rejections"] = pilot_counters.get(
        "evidence_binding_rejections", 0
    )
    run_manifest["out_of_worktree_read_quarantines"] = pilot_counters.get(
        "out_of_worktree_read_quarantines", 0
    )

    # Bundling observability (codex command-discipline non-compliance): top-level so we
    # can SEE residual bundling across runs. Non-gating — bundling costs retries, not binds.
    run_manifest["bundled_command_events"] = bundled_command_events
    run_manifest["max_bundled_subcommands"] = max_bundled_subcommands

    # Soft-signal: low-grounding alarm at manifest level (completes Condition 1). With the
    # binding gate softened, a low grounding_rate is now the primary organic-fabrication
    # signal — surface it loudly here, not just in per-run logs.
    run_manifest["low_grounding_runs"] = low_grounding_runs
    if low_grounding_runs:
        print(f"  ⚠ {len(low_grounding_runs)} review-run(s) below grounding floor "
              f"(recorded in manifest.low_grounding_runs)")

    # Compute inter-run Krippendorff alpha if we have data.
    if pr_results:
        try:
            inter_run_alpha = compute_inter_run_alpha(pr_results)
        except Exception as e:
            logger.warning("inter_run_alpha computation failed: %s", e)
            inter_run_alpha = {"_warnings": [f"computation failed: {e}"]}
        run_manifest["inter_run_alpha"] = inter_run_alpha
    else:
        run_manifest["inter_run_alpha"] = {}

    reporter_succeeded = True
    if args.research_mode and pr_results:
        research_path = os.path.join(args.output_dir, "research-report.md")
        try:
            generate_research_report(pr_results, run_manifest, research_path)
            print(f"\nResearch report saved: {research_path}")
        except Exception as e:
            reporter_succeeded = False
            logger.error("Reporter failed: %s", e)
            print(f"\nERROR: reporter failed: {e}")
    elif args.research_mode and not pr_results:
        print("\nResearch mode: no PRs evaluated, skipping research report.")

    # Pilot pass evaluation: always run. The six canary-independent criteria are
    # evaluated on every run; the canary criterion is gated inside
    # evaluate_pilot_pass only when canaries were injected (canary_outcomes
    # non-empty). pilot_pass is therefore a real bool even on canary-free runs.
    pilot_result = evaluate_pilot_pass(
        pr_results=pr_results,
        pilot_counters=pilot_counters,
        canary_outcomes=run_manifest.get("canary_outcomes", []),
        inter_run_alpha=run_manifest.get("inter_run_alpha", {}),
        reporter_succeeded=reporter_succeeded,
        total_wallclock_seconds=run_manifest.get("total_wallclock_seconds", 0.0),
        expected_tasks=run_manifest.get("expected_tasks", 0),
        max_workers=run_manifest.get("max_workers", args.max_workers),
    )
    run_manifest["pilot_pass"] = pilot_result.passed
    run_manifest["pilot_pass_details"] = pilot_result.to_dict()
    print(f"\nPilot pass: {'PASS' if pilot_result.passed else 'FAIL'}")
    for reason in pilot_result.reasons:
        print(f"  {reason}")

    _manifest.write_manifest(run_manifest, manifest_path)
    print(f"\nManifest saved: {manifest_path}")
    print(f"\nResults: {len(pr_results)}/{total_prs} PRs evaluated")
    skipped = len(run_manifest.get("skipped_prs", []))
    if skipped:
        print(f"Skipped: {skipped} PR(s)")

    # End-of-run teardown: reclaim the per-PR worktrees (a 30-PR run would
    # otherwise leave 30 full checkouts under eval/repos/worktrees/). Best-effort.
    # GUARD: never tear down on --resume — resume reuses the cached worktrees and must
    # leave them (and comments_pr{N}.md) intact so we can iterate fix→resume→fix. Resume
    # also never appends to worktree_specs, so this is belt-and-suspenders.
    if worktree_specs and not args.resume:
        cleanup_pr_worktrees(worktree_specs)
        print(f"Cleaned up {len(worktree_specs)} per-PR worktree(s)")


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(name)s: %(message)s")
    main()
