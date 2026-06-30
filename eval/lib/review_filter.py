"""Extract and clean Claude CI review comments from PR data."""

import re

CLAUDE_COMMENT_PREFIX = "**Claude finished"

BOILERPLATE_PATTERN = re.compile(
    r"^\*\*Claude finished @\S+'s task(?: in [^*]+)?\*\*"
    r"(?:\s*——\s*\[View job\]\([^)]+\))?"
    r"\s*\n---\n?",
    re.MULTILINE,
)


def is_claude_ci_comment(comment: dict) -> bool:
    user_type = (comment.get("user") or {}).get("type", "")
    body = comment.get("body") or ""
    return user_type == "Bot" and body.startswith(CLAUDE_COMMENT_PREFIX)


def strip_boilerplate(body: str) -> str:
    cleaned = BOILERPLATE_PATTERN.sub("", body, count=1).strip()
    return cleaned


def extract_claude_review(comments: list[dict]) -> str | None:
    claude_comments = [c for c in comments if is_claude_ci_comment(c)]
    if not claude_comments:
        return None

    best = max(claude_comments, key=lambda c: (len(c.get("body", "")), c.get("created_at", "")))

    return strip_boilerplate(best["body"])
