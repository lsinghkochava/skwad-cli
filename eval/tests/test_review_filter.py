"""Tests for eval.lib.review_filter — Claude CI comment identification + cleanup.

Regression coverage for the PR 883 failure: the filter previously matched a
hardcoded ``github-actions[bot]`` login and required a duration in the
boilerplate line. Real Claude CI comments now post as ``claude[bot]`` and the
"View job" footer can omit the duration, so neither was recognized. The filter
now keys off ``user.type == "Bot"`` AND ``body.startswith("**Claude finished")``
with no hardcoded login, and the boilerplate stripper treats the duration as
optional.
"""

import unittest

from eval.lib.review_filter import (
    extract_claude_review,
    is_claude_ci_comment,
    strip_boilerplate,
)


# ---------------------------------------------------------------------------
# Fixture helpers — mirror the GitHub issues-comments API dict shape that
# pr_fetcher.fetch_pr_comments returns (gh api .../issues/<n>/comments).
# ---------------------------------------------------------------------------

def _comment(login: str, user_type: str, body: str, created_at: str = "2026-06-17T00:00:00Z") -> dict:
    return {
        "user": {"login": login, "type": user_type},
        "body": body,
        "created_at": created_at,
    }


# The PR 883 form: no duration, "—— [View job]" footer, then the review.
_PR883_BODY = (
    "**Claude finished @lsingh's task** —— "
    "[View job](https://github.com/acme/web/actions/runs/123)\n\n"
    "---\n"
    "## Review\n"
    "Found one blocking bug in the auth handler."
)

# The older form: duration present.
_DURATION_BODY = (
    "**Claude finished @lsingh's task in 2m** —— "
    "[View job](https://github.com/acme/web/actions/runs/456)\n\n"
    "---\n"
    "## Review\n"
    "Looks good, two nits."
)

_CLAUDE_REVIEW_TEXT = "## Review\nFound one blocking bug in the auth handler."


# ---------------------------------------------------------------------------
# is_claude_ci_comment
# ---------------------------------------------------------------------------

class TestIsClaudeCIComment(unittest.TestCase):
    def test_recognizes_claude_bot_with_finished_body(self):
        """PR 883 regression: claude[bot] (Bot) + '**Claude finished' body qualifies."""
        comment = _comment("claude[bot]", "Bot", _PR883_BODY)
        self.assertTrue(is_claude_ci_comment(comment))

    def test_recognizes_legacy_github_actions_bot(self):
        """Backward compat: github-actions[bot] (Bot) + finished body still qualifies."""
        comment = _comment("github-actions[bot]", "Bot", _DURATION_BODY)
        self.assertTrue(is_claude_ci_comment(comment))

    def test_rejects_human_user_with_lookalike_body(self):
        """A human whose comment coincidentally starts with the prefix is not Claude."""
        comment = _comment("lsingh", "User", _PR883_BODY)
        self.assertFalse(is_claude_ci_comment(comment))

    def test_rejects_other_bot_without_finished_prefix(self):
        """A different bot (codecov) without the '**Claude finished' prefix is ignored."""
        comment = _comment("codecov[bot]", "Bot", "## Coverage report\n92% (+0.3%)")
        self.assertFalse(is_claude_ci_comment(comment))

    def test_rejects_bot_with_prefix_in_the_middle(self):
        """The prefix must START the body, not merely appear somewhere in it."""
        body = "Heads up:\n\n**Claude finished @lsingh's task** —— done"
        comment = _comment("claude[bot]", "Bot", body)
        self.assertFalse(is_claude_ci_comment(comment))

    def test_missing_user_object_does_not_raise(self):
        """Defensive: a comment lacking a user object is simply not a match."""
        self.assertFalse(is_claude_ci_comment({"body": _PR883_BODY}))

    def test_missing_body_does_not_raise(self):
        """Defensive: a comment lacking a body is simply not a match."""
        self.assertFalse(is_claude_ci_comment({"user": {"login": "claude[bot]", "type": "Bot"}}))

    def test_null_user_does_not_raise(self):
        """Regression: GitHub returns ``user: null`` for deleted-account comments.

        The ABSENT-key case ({"body": ...}) was already covered, but the real
        crash came from an explicit ``None`` value, which ``dict.get`` returns
        as-is — ``None.get(...)`` then raised AttributeError. The null-guard must
        treat it as a non-match, not a crash.
        """
        self.assertFalse(is_claude_ci_comment({"user": None, "body": _PR883_BODY}))

    def test_null_body_does_not_raise(self):
        """Defensive symmetry: an explicit ``body: null`` is a non-match, not a crash."""
        self.assertFalse(is_claude_ci_comment({"user": {"type": "Bot"}, "body": None}))


# ---------------------------------------------------------------------------
# strip_boilerplate
# ---------------------------------------------------------------------------

class TestStripBoilerplate(unittest.TestCase):
    def test_strips_pr883_form_without_duration(self):
        """PR 883 regression: the duration-less '**Claude finished ... **' header is removed."""
        cleaned = strip_boilerplate(_PR883_BODY)
        self.assertEqual(cleaned, _CLAUDE_REVIEW_TEXT)
        self.assertFalse(cleaned.startswith("**Claude finished"))
        self.assertNotIn("View job", cleaned)

    def test_strips_form_with_duration(self):
        """The older '... task in 2m **' header is also removed."""
        cleaned = strip_boilerplate(_DURATION_BODY)
        self.assertEqual(cleaned, "## Review\nLooks good, two nits.")
        self.assertFalse(cleaned.startswith("**Claude finished"))

    def test_preserves_body_with_no_boilerplate(self):
        """A body without the boilerplate header is returned stripped but otherwise intact."""
        body = "## Review\nNo boilerplate here."
        self.assertEqual(strip_boilerplate(body), body)

    def test_only_strips_leading_occurrence(self):
        """Only the leading boilerplate header is removed; later prose is preserved."""
        cleaned = strip_boilerplate(_PR883_BODY)
        self.assertIn("blocking bug", cleaned)


# ---------------------------------------------------------------------------
# extract_claude_review
# ---------------------------------------------------------------------------

class TestExtractClaudeReview(unittest.TestCase):
    def test_extracts_and_cleans_pr883_comment(self):
        """End-to-end PR 883 case: claude[bot] comment is found and de-boilerplated."""
        comments = [
            _comment("codecov[bot]", "Bot", "## Coverage\n92%"),
            _comment("lsingh", "User", "LGTM"),
            _comment("claude[bot]", "Bot", _PR883_BODY),
        ]
        self.assertEqual(extract_claude_review(comments), _CLAUDE_REVIEW_TEXT)

    def test_returns_none_when_no_claude_comment(self):
        """The old failure path: nothing qualifies → None (not a crash, not an empty string)."""
        comments = [
            _comment("codecov[bot]", "Bot", "## Coverage\n92%"),
            _comment("lsingh", "User", _PR883_BODY),  # human lookalike, must not match
        ]
        self.assertIsNone(extract_claude_review(comments))

    def test_returns_none_for_empty_list(self):
        self.assertIsNone(extract_claude_review([]))

    def test_picks_longest_then_newest_among_multiple(self):
        """When several Claude comments exist, the longest (then newest) body wins."""
        short = _comment("claude[bot]", "Bot", _DURATION_BODY, created_at="2026-06-17T01:00:00Z")
        long_body = (
            "**Claude finished @lsingh's task** —— "
            "[View job](https://github.com/acme/web/actions/runs/789)\n\n"
            "---\n"
            "## Review\n" + ("detailed finding. " * 50)
        )
        longest = _comment("claude[bot]", "Bot", long_body, created_at="2026-06-17T00:30:00Z")
        comments = [short, longest]
        result = extract_claude_review(comments)
        self.assertIn("detailed finding.", result)
        self.assertNotIn("two nits", result)


if __name__ == "__main__":
    unittest.main()
