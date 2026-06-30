"""Run skwad-cli review on a cloned repo and collect output."""

import os
import subprocess
import time


DEFAULT_TIMEOUT = 1200

# The skwad-cli binary has its own --timeout (default 10m) that self-terminates the
# run. We make the binary authoritative by passing --timeout explicitly, and give the
# Python subprocess wrapper this many extra seconds so the binary stops gracefully
# BEFORE the wrapper force-kills it.
BINARY_TIMEOUT_BUFFER_SEC = 120


def run_skwad_review(
    repo_path: str,
    pr_url: str,
    pr_number: int,
    skwad_binary: str,
    config_path: str,
    timeout: int = DEFAULT_TIMEOUT,
) -> str:
    skwad_binary = os.path.abspath(skwad_binary)
    config_path = os.path.abspath(config_path)
    repo_path = os.path.abspath(repo_path)

    comments_filename = f"comments_pr{pr_number}.md"

    prompt = (
        f"Please review {pr_url} and put your comments "
        f"in a {comments_filename} file at root of repo"
    )

    cmd = [
        skwad_binary, "run",
        "--config", config_path,
        "--set", f"repo={repo_path}",
        "--timeout", f"{timeout}s",
        "--prompt", prompt,
    ]

    print(f"  [skwad] cmd: {' '.join(cmd[:6])} --prompt '...'")
    print(f"  [skwad] cwd: {repo_path}")
    print(f"  [skwad] output file: {comments_filename}")
    print(f"  [skwad] started at {time.strftime('%H:%M:%S')}")

    proc = subprocess.Popen(
        cmd,
        cwd=repo_path,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )

    try:
        stdout, stderr = proc.communicate(timeout=timeout + BINARY_TIMEOUT_BUFFER_SEC)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.communicate()
        raise RuntimeError(f"skwad run timed out after {timeout}s for {pr_url}")

    print(f"  [skwad] finished at {time.strftime('%H:%M:%S')}, exit code: {proc.returncode}")

    if stdout.strip():
        print(f"  [skwad] stdout (last 20 lines):")
        for line in stdout.strip().splitlines()[-20:]:
            print(f"    {line}")

    if stderr.strip():
        print(f"  [skwad] stderr (last 10 lines):")
        for line in stderr.strip().splitlines()[-10:]:
            print(f"    {line}")

    comments_path = os.path.join(repo_path, comments_filename)
    if not os.path.exists(comments_path):
        print(f"  [skwad] files at repo root:")
        for f in sorted(os.listdir(repo_path)):
            print(f"    {f}")
        raise RuntimeError(
            f"skwad run completed but {comments_filename} not found at {comments_path}"
        )

    with open(comments_path) as f:
        review = f.read().strip()

    print(f"  [skwad] {comments_filename}: {len(review)} chars")
    return review
