"""OpenAI client wrapper for the Python judge (Route B).

Thin construction layer only — the agentic verification loop, retry/backoff
(#7/#22) and the per-run wall-clock cap (#26) land in later phases. This module
just centralizes the model id, request timeout, and key resolution so every
caller builds an identically-configured client.
"""

import os

from openai import OpenAI

# Model id is configurable for an easy bump later (Manager/user decision: GPT-5.1).
DEFAULT_MODEL = os.environ.get("OPENAI_JUDGE_MODEL", "gpt-5.1")

# Per-request timeout (seconds). Phase 0 / approved: 120s per OpenAI request; the
# 300s per-run wall-clock cap is enforced by the loop, not here.
REQUEST_TIMEOUT_SEC = 120

# Env var holding the OpenAI API key (OpenAI SDK default name).
API_KEY_ENV = "OPENAI_API_KEY"


class MissingAPIKey(RuntimeError):
    """Raised when OPENAI_API_KEY is not set in the environment."""


def build_client(timeout: float = REQUEST_TIMEOUT_SEC) -> OpenAI:
    """Construct an OpenAI client configured with the per-request timeout.

    Raises MissingAPIKey if OPENAI_API_KEY is absent so callers fail loudly
    rather than emitting an opaque SDK auth error mid-run.
    """
    api_key = os.environ.get(API_KEY_ENV)
    if not api_key:
        raise MissingAPIKey(
            f"{API_KEY_ENV} is not set; the OpenAI judge cannot run. "
            f"Add {API_KEY_ENV}=... to the environment (.env)."
        )
    return OpenAI(api_key=api_key, timeout=timeout)
