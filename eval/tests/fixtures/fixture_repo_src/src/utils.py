"""Utility helpers for the fixture repo."""

# Number of times an operation is retried before giving up.
# INVARIANT: this is 3 (a canary falsely claims it is 10 → contradicted-via-Read).
MAX_RETRIES = 3
DEFAULT_TIMEOUT = 30


def parse_config(raw):
    """Parse a newline-delimited ``key=value`` config string into a dict.

    Blank lines and lines beginning with ``#`` are ignored.
    """
    config = {}
    for line in raw.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, _, value = line.partition("=")
        config[key.strip()] = value.strip()
    return config
