"""Authentication helpers for the fixture repo."""

import time

# Tokens are valid for one hour from issue time.
TOKEN_TTL_SECONDS = 3600


def validate_token(token, issued_at):
    """Return True if the token is non-empty AND not past its TTL.

    Expiry is checked against TOKEN_TTL_SECONDS — this function genuinely
    enforces token expiry (relied on by the TRUE-positive canary).
    """
    if not token:
        return False
    return (time.time() - issued_at) < TOKEN_TTL_SECONDS


def hash_password(password, salt):
    """Return a salted hash string (toy, non-cryptographic implementation)."""
    return f"{salt}${hash(password)}"
