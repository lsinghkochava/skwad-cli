"""LLM-as-judge scoring engine using skwad-cli — paired review, counterbalanced A/B.

============================================================================
RETIRED (Phase 5) — replaced by ``eval/lib/openai_judge.py`` (in-process OpenAI
GPT-5.1 judge, Route B). This skwad-subprocess judge is NO LONGER imported by
``main.py`` and is NOT part of the live eval path. Do NOT re-import it.

Kept only so its tests (``eval/tests/test_judge.py``) still document the original
Claude-judge behavior. RECOMMENDED FOLLOW-UP (pending user approval): delete this
module + ``test_judge.py`` once the OpenAI judge is fully validated live.
============================================================================
"""

import json
import logging
import math
import os
import random
import re
import statistics
import subprocess
import time
from pathlib import Path
from typing import Literal

logger = logging.getLogger(__name__)

EVAL_DIR = Path(__file__).resolve().parent.parent
JUDGE_CONFIG = EVAL_DIR / "config" / "judge_team.json"

DIFF_TRUNCATION_CAP = 32_000

# The skwad-cli binary has its own --timeout (default 10m) that self-terminates the
# run. We make the binary authoritative by passing --timeout explicitly, and give the
# Python subprocess wrapper this many extra seconds so the binary stops gracefully
# BEFORE the wrapper force-kills it.
BINARY_TIMEOUT_BUFFER_SEC = 120

CRITERIA = [
    "issue_detection",
    "actionability",
    "severity_accuracy",
    "coverage",
    "signal_to_noise",
    "depth",
    "novel_substantive_findings",
]

# Criteria that carry 4-bucket finding counts.
_COUNT_CRITERIA = {
    "issue_detection",
    "coverage",
    "depth",
    "novel_substantive_findings",
}

# Tools the judge is allowed to use (native Claude tools only, no mcp: prefix).
ALLOWED_JUDGE_TOOLS = {"Read", "Grep", "Glob"}

System = Literal["skwad", "claude_ci"]
_FIXED_ASSIGNMENTS: list[tuple[System, System]] = [
    ("skwad", "claude_ci"),
    ("claude_ci", "skwad"),
]


class ConfabulationDetected(RuntimeError):
    """Raised when verified_findings > 0 but observed tool calls < min_required."""


class DisallowedToolUsed(RuntimeError):
    """Raised when the judge invoked a tool outside ALLOWED_JUDGE_TOOLS."""


class StructuralInvalidRun(RuntimeError):
    """Raised when the judge response violates structural requirements:
    - claim_trace empty/missing while findings present, OR
    - missing 4-bucket fields on any count-based criterion.
    """


class MCPUnavailable(RuntimeError):
    """Raised when the judge subprocess did not bring up its MCP server / event log.

    Detected two ways (Reviewer): structurally (empty event_log_path/agent_ids —
    primary, cannot drift) and via the stderr 'MCP server listening' line for the
    expected port (secondary). Retryable — a port collision or silent downgrade.
    """


class OutputFreshnessError(RuntimeError):
    """Raised when the expected per-task output file is missing or stale (an older
    run's file on disk). Retryable."""


class RateLimited(RuntimeError):
    """Raised when the judge subprocess hit an Anthropic 429. Retryable with backoff."""


# Base MCP port for judge subprocesses. Each global task gets base + task_index so
# concurrent judges never collide on the default (8777). On retry the port is
# offset by RETRY_PORT_OFFSET * attempt so a retry can't inherit an occupied port.
JUDGE_BASE_PORT = 8800
RETRY_PORT_OFFSET = 1000

# Backoff (seconds) before retrying a rate-limited (429) judge task; scaled by attempt.
RATE_LIMIT_BACKOFF_SEC = 5.0

# Exceptions that warrant a retry of the judge invocation.
_RETRYABLE_EXC = (
    ConfabulationDetected,
    DisallowedToolUsed,
    StructuralInvalidRun,
    MCPUnavailable,
    OutputFreshnessError,
    RateLimited,
)

# Maps a final (post-retry) failure exception to the pilot counter it increments.
# Applied SINGLE-THREADED during result merge — the parallel callable never mutates
# shared counters.
_EXC_COUNTER_KEY = {
    ConfabulationDetected: "confabulation_rejections",
    DisallowedToolUsed: "disallowed_tool_rejections",
    StructuralInvalidRun: "structural_invalid_rejections",
}


_REQUIRED_BUCKET_FIELDS = (
    "verified_findings",
    "unverified_findings",
    "contradicted_findings",
    "non_falsifiable_findings",
)


def _validate_response_structure(parsed: dict) -> None:
    """Validate the judge response structure. Raises StructuralInvalidRun on violation.

    Checks:
    1. Every count-based criterion has all 4 bucket fields present.
    2. If sum(verified+contradicted+non_falsifiable) > 0 on any criterion in a
       review, that review's claim_trace MUST be non-empty.
    """
    for review_key in ("review_a", "review_b"):
        review = parsed.get(review_key)
        if not isinstance(review, dict):
            continue
        criteria = review.get("criteria", {})
        # Check 1: all 4 bucket fields present on each count criterion.
        for crit_name in _COUNT_CRITERIA:
            crit = criteria.get(crit_name, {})
            if not isinstance(crit, dict):
                raise StructuralInvalidRun(
                    f"{review_key}.{crit_name}: not a dict"
                )
            missing = [f for f in _REQUIRED_BUCKET_FIELDS if f not in crit]
            if missing:
                raise StructuralInvalidRun(
                    f"{review_key}.{crit_name}: missing bucket fields: {missing}"
                )

        # Check 2: claim_trace populated when findings present.
        total_findings = 0
        for crit_name in _COUNT_CRITERIA:
            crit = criteria.get(crit_name, {})
            total_findings += (
                crit.get("verified_findings", 0)
                + crit.get("contradicted_findings", 0)
                + crit.get("non_falsifiable_findings", 0)
            )
        claim_trace = review.get("claim_trace", [])
        if total_findings > 0 and not claim_trace:
            raise StructuralInvalidRun(
                f"{review_key}: claim_trace empty but {total_findings} finding(s) present"
            )


def _truncate_diff(diff: str) -> str:
    if len(diff) > DIFF_TRUNCATION_CAP:
        return (
            f"=== DIFF (truncated at {DIFF_TRUNCATION_CAP} chars; "
            f"full diff is {len(diff)} chars) ===\n{diff[:DIFF_TRUNCATION_CAP]}"
        )
    return f"=== DIFF ===\n{diff}"


def _parse_json_output(raw: str) -> dict:
    raw = raw.strip()
    raw = re.sub(r"^```(?:json)?\s*\n?", "", raw)
    raw = re.sub(r"\n?```\s*$", "", raw)
    start = raw.find("{")
    end = raw.rfind("}")
    if start == -1 or end == -1:
        raise ValueError(f"No JSON object in judge output: {raw[:200]}")
    return json.loads(raw[start : end + 1])


def _parse_stderr_metadata(stderr: str) -> dict:
    """Extract run_id, event_log_path, and agent_ids from skwad-cli stderr output.

    The `Event log:` line is required (printed by skwad-cli per run.go). Missing
    line means a skwad-cli version mismatch — we no longer derive a fallback path
    because the path was platform-dependent (~/.config/skwad on linux, but
    ~/Library/Application Support/skwad on darwin) and silently picking the wrong
    one would skip tool-call verification without anyone noticing.
    """
    meta: dict = {"run_id": None, "event_log_path": None, "agent_ids": {}}
    for line in stderr.splitlines():
        line = line.strip()
        if line.startswith("Run ID: "):
            meta["run_id"] = line[len("Run ID: "):]
        elif line.startswith("Event log: "):
            meta["event_log_path"] = line[len("Event log: "):]
        elif line.startswith("Agent: "):
            parts = line[len("Agent: "):].rsplit(" ", 1)
            if len(parts) == 2:
                name, agent_id = parts
                meta["agent_ids"][name] = agent_id
    return meta


def count_tool_calls(event_log_path: str, judge_agent_id: str) -> list[str]:
    """Return list of tool_names from EventToolCall events for the given agent.

    Silently skips malformed lines and missing fields.
    """
    tool_calls: list[str] = []
    try:
        with open(event_log_path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    logger.debug("count_tool_calls: skipping malformed line")
                    continue
                if event.get("type") != "tool_call":
                    continue
                data = event.get("data") or {}
                if not isinstance(data, dict):
                    continue
                if data.get("agent_id") != judge_agent_id:
                    continue
                tool_name = data.get("tool_name")
                if tool_name:
                    tool_calls.append(tool_name)
    except OSError:
        logger.warning("count_tool_calls: event log not found at %s", event_log_path)
    return tool_calls


def _sum_verified_in_output(parsed: dict) -> int:
    """Sum verified_findings across all count-based criteria in both reviews."""
    total = 0
    for review_key in ("review_a", "review_b"):
        review = parsed.get(review_key, {})
        criteria = review.get("criteria", {})
        for crit in _COUNT_CRITERIA:
            total += criteria.get(crit, {}).get("verified_findings", 0)
    return total


def _backfill_tool_calls_observed(parsed: dict, tool_calls_a: list[str], tool_calls_b: list[str]) -> None:
    """Back-fill tool_calls_observed in verification_summary from event log counts."""
    for review_key, tool_calls in (("review_a", tool_calls_a), ("review_b", tool_calls_b)):
        review = parsed.get(review_key, {})
        vs = review.get("verification_summary")
        if isinstance(vs, dict):
            vs["tool_calls_observed"] = len(tool_calls)


def _check_confabulation(parsed: dict, tool_calls: list[str]) -> None:
    """Raise ConfabulationDetected if verified claims >> observed tool calls."""
    claims_verified = _sum_verified_in_output(parsed)
    min_required = max(1, math.ceil(claims_verified / 5))
    if claims_verified > 0 and len(tool_calls) < min_required:
        raise ConfabulationDetected(
            f"verified={claims_verified}, tools_observed={len(tool_calls)}, "
            f"expected>={min_required}"
        )


def _check_disallowed_tools(tool_calls: list[str]) -> None:
    """No-op: the judge is permitted to use any tool.

    Tool-call telemetry is still collected (see count_tool_calls); we simply
    no longer reject a run based on which tools were invoked.
    """
    return


def _warn_trace_divergence(parsed: dict, tool_calls: list[str]) -> None:
    """Log a warning if declared tool uses in claim_trace diverge >20% from observed."""
    declared_uses = 0
    for review_key in ("review_a", "review_b"):
        for claim in parsed.get(review_key, {}).get("claim_trace", []):
            declared_uses += len(claim.get("tools_used", []))
    observed = len(tool_calls)
    if observed > 0:
        divergence = abs(declared_uses - observed) / observed
        if divergence > 0.20:
            logger.warning(
                "trace-observation divergence: declared=%d, observed=%d, ratio=%.2f",
                declared_uses, observed, divergence,
            )


def _stderr_has_rate_limit(stderr: str) -> bool:
    """Whether the subprocess stderr indicates an Anthropic 429 / rate limit."""
    s = (stderr or "").lower()
    return "429" in s or "rate limit" in s or "rate_limit" in s or "overloaded" in s


def _run_judge_once(
    diff: str,
    review_a: str,
    review_b: str,
    skwad_binary: str,
    repo_path: str,
    config_path: str,
    judge_output_name: str,
    port: int,
    task_start_time: float,
    timeout: int = 1200,
) -> tuple[dict, dict]:
    """Run the judge once. Returns (parsed_output, stderr_meta).

    Writes to a UNIQUE per-task output file (judge_output_name) so concurrent
    judges never clobber each other, on a UNIQUE --port so their MCP servers
    never collide. Raises retryable exceptions (RateLimited / MCPUnavailable /
    OutputFreshnessError) on the corresponding failure modes.
    """
    prompt = (
        "Score both code reviews using the rubric in your instructions. "
        f"Write your JSON output to {judge_output_name} at the root of the repo. "
        "Output ONLY the JSON object, nothing else.\n\n"
        f"{_truncate_diff(diff)}\n\n"
        "=== REVIEW A ===\n"
        f"{review_a}\n\n"
        "=== REVIEW B ===\n"
        f"{review_b}\n"
    )

    output_file = os.path.join(repo_path, judge_output_name)
    if os.path.exists(output_file):
        os.remove(output_file)

    result = subprocess.run(
        [
            os.path.abspath(skwad_binary), "run",
            "--config", config_path,
            "--port", str(port),
            "--set", f"repo={os.path.abspath(repo_path)}",
            "--timeout", f"{timeout}s",
            "--prompt", prompt,
        ],
        cwd=repo_path,
        timeout=timeout + BINARY_TIMEOUT_BUFFER_SEC,
        capture_output=True,
        text=True,
    )
    stderr = result.stderr or ""
    stderr_meta = _parse_stderr_metadata(stderr)

    if result.returncode != 0:
        logger.warning(
            "judge subprocess exit=%d; stderr: %s", result.returncode, stderr[:500],
        )
        if _stderr_has_rate_limit(stderr):
            raise RateLimited(f"judge subprocess hit rate limit (exit {result.returncode})")
        raise RuntimeError(f"judge subprocess failed (exit {result.returncode})")

    if _stderr_has_rate_limit(stderr):
        raise RateLimited("judge subprocess reported a rate limit")

    # Secondary MCP detection: the server logs this line ONLY on a successful
    # bind to our exact port (server.go:102, Addr=127.0.0.1:<port>).
    if f"MCP server listening on 127.0.0.1:{port}" not in stderr:
        raise MCPUnavailable(
            f"no 'MCP server listening on 127.0.0.1:{port}' on stderr — "
            "port collision or silent MCP downgrade"
        )

    # Freshness guard: the expected output must exist AND be written by THIS task
    # (mtime >= when the task started), not a stale file from an earlier run.
    if not os.path.exists(output_file):
        raise OutputFreshnessError(f"judge produced no output file ({judge_output_name} missing)")
    if os.path.getmtime(output_file) < task_start_time:
        raise OutputFreshnessError(
            f"{judge_output_name} is stale (mtime < task start) — likely a leftover file"
        )

    if not stderr_meta.get("event_log_path"):
        raise RuntimeError(
            "expected 'Event log:' on stderr but not found — skwad-cli version mismatch? "
            "v2 judge harness requires skwad-cli with event-log discovery (commit fa7abc3 or later)"
        )

    with open(output_file) as f:
        raw = f.read()
    return _parse_json_output(raw), stderr_meta


def _unswap(parsed: dict, a_system: System, b_system: System) -> dict:
    """Map review_a/review_b criterion scores back to named systems."""
    return {
        a_system: parsed.get("review_a", {}),
        b_system: parsed.get("review_b", {}),
    }


def _median_vote(runs_resolved: list[dict]) -> dict:
    """Per-criterion median across runs for each system."""
    result: dict = {}
    for system in ("skwad", "claude_ci"):
        sys_runs = [r.get(system, {}).get("criteria", {}) for r in runs_resolved]
        system_result: dict = {}
        for criterion in CRITERIA:
            scores = [r.get(criterion, {}).get("score", 0) for r in sys_runs]
            reasonings = [r.get(criterion, {}).get("reasoning", "") for r in sys_runs]
            voted = statistics.median_low(scores)
            entry: dict = {
                "scores": scores,
                "voted": voted,
                "reasoning_runs": reasonings,
            }
            if criterion in _COUNT_CRITERIA:
                for bucket in ("verified_findings", "unverified_findings",
                               "contradicted_findings", "non_falsifiable_findings"):
                    entry[bucket] = sum(
                        r.get(criterion, {}).get(bucket, 0) for r in sys_runs
                    )
            if criterion == "novel_substantive_findings":
                matching = [r for r in sys_runs if r.get(criterion, {}).get("score", -1) == voted]
                if voted > 0 and matching:
                    entry["justifications"] = matching[0].get(criterion, {}).get("justifications", [])[:voted]
                else:
                    entry["justifications"] = []
            system_result[criterion] = entry
        system_result["total"] = sum(system_result[c]["voted"] for c in CRITERIA)
        system_result["n_runs_completed"] = len(runs_resolved)
        system_result["n_runs_planned"] = 3
        result[system] = system_result
    return result


def _aggregate_verification_summaries(runs_resolved: list[dict]) -> dict:
    """Aggregate verification_summary fields across runs.

    Counts (verified/unverified/contradicted/non_falsifiable) are summed.
    verification_rate is averaged. tool_calls_observed is summed.
    """
    agg: dict = {}
    for system in ("skwad", "claude_ci"):
        totals: dict = {
            "claims_verified": 0,
            "claims_unverified": 0,
            "claims_contradicted": 0,
            "claims_non_falsifiable": 0,
            "verification_rate": 0.0,
            "tool_calls_observed": 0,
        }
        n = 0
        for run in runs_resolved:
            vs = run.get(system, {}).get("verification_summary", {})
            if not vs:
                continue
            n += 1
            for k in ("claims_verified", "claims_unverified", "claims_contradicted",
                      "claims_non_falsifiable", "tool_calls_observed"):
                totals[k] += vs.get(k, 0)
            totals["verification_rate"] += vs.get("verification_rate", 0.0)
        if n > 0:
            totals["verification_rate"] /= n
        agg[system] = totals
    return agg


def derive_ab_assignments(seed: int) -> list[tuple[System, System]]:
    """Deterministic counterbalanced A/B order for a PR's 3 runs.

    Uses a PER-CALL Random INSTANCE (never the global random module) so it is
    safe under concurrency and reproducible from `seed` alone.
    """
    rng = random.Random(seed)
    run3_assignment: tuple[System, System] = rng.choice(_FIXED_ASSIGNMENTS)
    return [*_FIXED_ASSIGNMENTS, run3_assignment]


def prepare_pr_judge_tasks(
    pr_data: dict,
    skwad_review: str,
    claude_ci_review: str,
    skwad_binary: str,
    repo_path: str,
    seed: int,
    run_dir: str,
    *,
    config_path: str | None = None,
    timeout: int = 1200,
    canary_injections: list[dict] | None = None,
    base_port: int = JUDGE_BASE_PORT,
) -> tuple[list[dict], list[tuple[System, System]]]:
    """Build the 3 per-run judge task specs for one PR (no judge invocation yet).

    Returns (tasks, assignments). Each task is a self-contained, hashable-free dict
    consumed by run_single_judge_task(). `base_port` is the port for run 1; run i
    uses base_port + (i-1). The caller (parallel orchestrator) passes a globally
    unique base_port per PR so concurrent judges across PRs never collide; A/B
    order and reviews are derived from the PER-PR `run_index`/`seed` ONLY (never a
    global task index — that would break the determinism invariant).
    """
    if config_path is None:
        config_path = str(JUDGE_CONFIG)
    os.makedirs(run_dir, exist_ok=True)

    pr_number = pr_data["pr_number"]
    diff = pr_data.get("diff", "")
    assignments = derive_ab_assignments(seed)
    reviews: dict[System, str] = {"skwad": skwad_review, "claude_ci": claude_ci_review}
    if canary_injections:
        reviews = _apply_canary_injections(reviews, canary_injections, pr_data)

    tasks: list[dict] = []
    for i, (a_system, b_system) in enumerate(assignments, start=1):
        tasks.append({
            "pr_number": pr_number,
            "run_index": i,
            "a_system": a_system,
            "b_system": b_system,
            "diff": diff,
            "review_a": reviews[a_system],
            "review_b": reviews[b_system],
            "skwad_binary": skwad_binary,
            "repo_path": repo_path,
            "config_path": config_path,
            "run_dir": run_dir,
            "judge_output_name": f"judge_output_pr{pr_number}_run{i}.json",
            "run_record_name": f"judge_pr{pr_number}_run{i}.json",
            "port": base_port + (i - 1),
            "timeout": timeout,
            "canary_injections": canary_injections or [],
            "pr_data": pr_data,
        })
    return tasks, assignments


def run_single_judge_task(task: dict) -> dict:
    """Run ONE judge invocation (one PR × one A/B run). Pure: mutates NO shared state.

    Returns a self-contained result record:
        {
          "pr_number": int, "run_index": int, "ab_assignment": [a, b],
          "status": "ok" | "failed",
          "resolved": dict | None,       # named-system criteria (_unswap output)
          "run_record": dict | None,     # {run, ab_assignment, raw_response, resolved,
                                         #  stderr_meta, duration_seconds}
          "canary_outcomes": list[dict], # [] when no canaries / on failure
          "counter_increment": str | None,  # pilot counter to bump, applied single-threaded
          "duration_seconds": float,
          "error": str | None,
        }
    Never raises for a judge failure — the failure is captured in status/error so a
    single bad task can be recorded without sinking the whole run.
    """
    a_system: System = task["a_system"]
    b_system: System = task["b_system"]
    i = task["run_index"]
    pr_number = task["pr_number"]
    out = {
        "pr_number": pr_number,
        "run_index": i,
        "ab_assignment": [a_system, b_system],
        "status": "failed",
        "resolved": None,
        "run_record": None,
        "canary_outcomes": [],
        "counter_increment": None,
        "duration_seconds": 0.0,
        "error": None,
    }

    t_start = time.perf_counter()
    task_start_wall = time.time()
    logger.info("Judge PR#%s run %d: A=%s B=%s (port %d)", pr_number, i, a_system, b_system, task["port"])
    try:
        parsed, stderr_meta = _run_and_verify(
            diff=task["diff"],
            review_a=task["review_a"],
            review_b=task["review_b"],
            skwad_binary=task["skwad_binary"],
            repo_path=task["repo_path"],
            config_path=task["config_path"],
            judge_output_name=task["judge_output_name"],
            base_port=task["port"],
            task_start_time=task_start_wall,
            timeout=task["timeout"],
        )
    except Exception as e:
        out["duration_seconds"] = time.perf_counter() - t_start
        out["error"] = f"{type(e).__name__}: {e}"
        out["counter_increment"] = _EXC_COUNTER_KEY.get(type(e))
        logger.warning("Judge PR#%s run %d failed: %s", pr_number, i, out["error"])
        return out

    duration = time.perf_counter() - t_start
    resolved = _unswap(parsed, a_system, b_system)
    canary_outcomes = []
    if task["canary_injections"]:
        canary_outcomes = _check_canary_outcomes(
            parsed, task["canary_injections"], a_system, b_system, task["pr_data"]
        )
    run_record = {
        "run": i,
        "ab_assignment": [a_system, b_system],
        "raw_response": parsed,
        "resolved": resolved,
        "stderr_meta": stderr_meta,
        "duration_seconds": duration,
    }
    # Persist the per-run record to its unique path (safe: unique filename per task).
    run_path = os.path.join(task["run_dir"], task["run_record_name"])
    with open(run_path, "w") as f:
        json.dump(run_record, f, indent=2)

    out.update({
        "status": "ok",
        "resolved": resolved,
        "run_record": run_record,
        "canary_outcomes": canary_outcomes,
        "duration_seconds": duration,
    })
    logger.info(
        "Judge PR#%s run %d done: skwad_total=%s ci_total=%s (%.1fs)", pr_number, i,
        resolved.get("skwad", {}).get("total", "?"),
        resolved.get("claude_ci", {}).get("total", "?"),
        duration,
    )
    return out


def finalize_pr_runs(
    pr_number: int,
    run_results: list[dict],
    assignments: list[tuple[System, System]],
    run_dir: str,
    *,
    canary_injections: list[dict] | None = None,
) -> dict:
    """Vote + aggregate completed run results for ONE PR into its scored result.

    Consumes run_single_judge_task() outputs (in any order). Raises RuntimeError if
    no run for the PR succeeded. Returns the same shape score_paired_reviews always
    returned, plus run_durations_seconds for every ATTEMPTED run.
    """
    ordered = sorted(run_results, key=lambda r: r.get("run_index", 0))
    ok = [r for r in ordered if r.get("status") == "ok"]
    runs_raw = [r["run_record"] for r in ok]
    runs_resolved = [r["resolved"] for r in ok]
    run_durations = [r.get("duration_seconds", 0.0) for r in ordered]
    canary_outcomes: list[dict] = []
    for r in ok:
        canary_outcomes.extend(r.get("canary_outcomes", []))

    if not runs_resolved:
        raise RuntimeError(f"All {len(ordered)} judge runs failed for PR #{pr_number}")

    voted = _median_vote(runs_resolved)
    verification_summaries = _aggregate_verification_summaries(runs_resolved)
    for system in ("skwad", "claude_ci"):
        voted[system]["verification_summary"] = verification_summaries.get(system, {})

    voted_path = os.path.join(run_dir, f"judge_pr{pr_number}_voted.json")
    with open(voted_path, "w") as f:
        json.dump(voted, f, indent=2)
    logger.info(
        "Voted result saved: %s (skwad=%d, ci=%d)",
        os.path.basename(voted_path), voted["skwad"]["total"], voted["claude_ci"]["total"],
    )

    result = {
        "skwad": voted["skwad"],
        "claude_ci": voted["claude_ci"],
        "runs": runs_raw,
        "ab_assignments": [[a, b] for a, b in assignments],
        "n_runs_completed": len(runs_resolved),
        "n_runs_planned": len(assignments),
        "run_durations_seconds": run_durations,
    }
    if canary_injections:
        result["canary_outcomes"] = canary_outcomes
    return result


def score_paired_reviews(
    pr_data: dict,
    skwad_review: str,
    claude_ci_review: str,
    skwad_binary: str,
    repo_path: str,
    seed: int,
    run_dir: str,
    config_path: str | None = None,
    timeout: int = 1200,
    canary_injections: list[dict] | None = None,
    pilot_counters: dict | None = None,
) -> dict:
    """Score both reviews for a PR using counterbalanced A/B judge runs (SEQUENTIAL).

    Thin wrapper over prepare_pr_judge_tasks → run_single_judge_task → finalize_pr_runs,
    sharing the exact building blocks the parallel orchestrator uses. Applies
    counter_increment to pilot_counters single-threaded (one PR, in this thread).

    Returns dict with keys: skwad, claude_ci (voted scores), runs (raw), ab_assignments,
    n_runs_completed/planned, run_durations_seconds, canary_outcomes (if injected).
    """
    tasks, assignments = prepare_pr_judge_tasks(
        pr_data, skwad_review, claude_ci_review, skwad_binary, repo_path, seed, run_dir,
        config_path=config_path, timeout=timeout, canary_injections=canary_injections,
    )
    run_results = []
    for task in tasks:
        res = run_single_judge_task(task)
        if pilot_counters is not None and res.get("counter_increment"):
            key = res["counter_increment"]
            pilot_counters[key] = pilot_counters.get(key, 0) + 1
        run_results.append(res)

    return finalize_pr_runs(
        pr_data["pr_number"], run_results, assignments, run_dir,
        canary_injections=canary_injections,
    )


def _run_and_verify(
    diff: str,
    review_a: str,
    review_b: str,
    skwad_binary: str,
    repo_path: str,
    config_path: str,
    judge_output_name: str,
    base_port: int,
    task_start_time: float,
    timeout: int = 1200,
    max_attempts: int = 2,
) -> tuple[dict, dict]:
    """Run judge with confabulation + disallowed-tool checks. Retries once on failure.

    Thread-safe / pure w.r.t. shared state: raises the final exception instead of
    mutating any shared counters — the caller maps it to a counter during the
    single-threaded merge. Each retry uses a port offset by RETRY_PORT_OFFSET so a
    retry never inherits an actively-occupied port; rate-limited retries back off.
    """

    def _attempt(port: int) -> tuple[dict, dict]:
        parsed, stderr_meta = _run_judge_once(
            diff=diff, review_a=review_a, review_b=review_b,
            skwad_binary=skwad_binary, repo_path=repo_path,
            config_path=config_path, judge_output_name=judge_output_name,
            port=port, task_start_time=task_start_time, timeout=timeout,
        )
        # Structural validation FIRST — wrong bucket-field schema would silently
        # default to 0 and bypass the confabulation check below.
        _validate_response_structure(parsed)

        # Primary MCP detection (cannot drift): empty event_log_path/agent_ids means
        # the run did not use the event log → a no-MCP run would skip the confab
        # check and pass as valid. Treat as a HARD FAIL → retry, not a soft skip.
        event_log_path = stderr_meta.get("event_log_path")
        agent_ids = stderr_meta.get("agent_ids", {})
        judge_agent_id = next(iter(agent_ids.values())) if agent_ids else None
        if not event_log_path or not judge_agent_id:
            raise MCPUnavailable(
                "judge run produced no event_log_path/agent_ids — MCP unavailable; "
                "tool-call verification would be skipped"
            )

        tool_calls = count_tool_calls(event_log_path, judge_agent_id)
        _backfill_tool_calls_observed(parsed, tool_calls, tool_calls)
        _warn_trace_divergence(parsed, tool_calls)

        if tool_calls:
            _check_confabulation(parsed, tool_calls)
            _check_disallowed_tools(tool_calls)

        return parsed, stderr_meta

    last_exc: Exception | None = None
    for attempt in range(max_attempts):
        port = base_port + RETRY_PORT_OFFSET * attempt
        try:
            return _attempt(port)
        except RateLimited as exc:
            last_exc = exc
            if attempt + 1 < max_attempts:
                logger.warning("Judge rate-limited, backing off then retrying: %s", exc)
                time.sleep(RATE_LIMIT_BACKOFF_SEC * (attempt + 1))
        except _RETRYABLE_EXC as exc:
            last_exc = exc
            if attempt + 1 < max_attempts:
                logger.warning(
                    "Judge verification failed (%s), retrying once: %s",
                    type(exc).__name__, exc,
                )
    assert last_exc is not None
    raise last_exc


# ---------------------------------------------------------------------------
# Canary support
# ---------------------------------------------------------------------------

def _canary_targets_pr(canary: dict, pr_number) -> bool:
    """Whether a canary applies to the given PR.

    A canary is PR-agnostic (injects into every PR) when its target PR is
    absent, null, or "*". A specific `pr` pins it to that PR only — compared as
    strings so a JSON-authored `"pr": "1746"` still matches int pr_number 1746
    (a type mismatch would silently never inject and pass canaries_caught vacuously).
    """
    target_pr = canary.get("target_pr") or {}
    target = target_pr.get("pr")
    if target in (None, "*"):
        return True
    return str(target) == str(pr_number)


def _normalize_claim_text(text: str) -> str:
    """Lowercase and collapse all whitespace runs for tolerant matching."""
    return " ".join((text or "").lower().split())


def _canary_trace_match(canary: dict, claim_text: str, entry_text: str) -> bool:
    """Conservatively decide whether a claim_trace entry is the injected canary.

    Prefers an explicit distinctive token (`match_token`, e.g. the fabricated
    function name) that cannot collide with a genuine claim, so a paraphrased
    trace entry still matches. `claim_text` is only the FALLBACK discriminator,
    used (as a normalized first-50-char substring) when the canary has no
    `match_token`. Stays conservative — no fuzzy overlap that could match an
    unrelated claim and mask a genuine judge miss.
    """
    norm_entry = _normalize_claim_text(entry_text)
    if not norm_entry:
        return False
    match_token = _normalize_claim_text(canary.get("match_token", ""))
    if match_token:
        return match_token in norm_entry
    norm_claim = _normalize_claim_text(claim_text)
    if not norm_claim:
        return False
    return norm_claim[:50] in norm_entry


def _apply_canary_injections(
    reviews: dict[System, str],
    canaries: list[dict],
    pr_data: dict,
) -> dict[System, str]:
    """Inject canary claims into the target review text for the matching PR."""
    patched = dict(reviews)
    for canary in canaries:
        if not _canary_targets_pr(canary, pr_data.get("pr_number")):
            continue
        inject_into: str = canary.get("inject_into", "skwad")
        claim_text: str = canary.get("claim_text", "")
        if not claim_text:
            continue
        system_key: System = "skwad" if inject_into == "skwad" else "claude_ci"
        patched[system_key] = patched[system_key] + f"\n\n[INJECTED CLAIM] {claim_text}"
    return patched


def _check_canary_outcomes(
    parsed: dict,
    canaries: list[dict],
    a_system: System,
    b_system: System,
    pr_data: dict,
) -> list[dict]:
    """Assert canary claims in claim_trace and return outcome records."""
    outcomes: list[dict] = []
    for canary in canaries:
        if not _canary_targets_pr(canary, pr_data.get("pr_number")):
            continue
        inject_into: str = canary.get("inject_into", "skwad")
        system_key: System = "skwad" if inject_into == "skwad" else "claude_ci"
        # Map system to review_a/review_b based on assignment.
        if a_system == system_key:
            review_key = "review_a"
        elif b_system == system_key:
            review_key = "review_b"
        else:
            continue

        claim_text = canary.get("claim_text", "")
        expected_outcome = canary.get("expected_outcome", "")
        review = parsed.get(review_key, {})
        claim_trace = review.get("claim_trace", [])

        actual_outcome = None
        for trace_entry in claim_trace:
            if _canary_trace_match(canary, claim_text, trace_entry.get("claim_text") or ""):
                actual_outcome = trace_entry.get("outcome")
                break

        passed = actual_outcome == expected_outcome
        if not passed:
            logger.error(
                "CANARY FAILED: id=%s expected=%s actual=%s",
                canary.get("id"), expected_outcome, actual_outcome,
            )
        outcomes.append({
            "id": canary.get("id"),
            "expected_outcome": expected_outcome,
            "actual_outcome": actual_outcome,
            "passed": passed,
            "rationale": canary.get("rationale", ""),
        })
    return outcomes
