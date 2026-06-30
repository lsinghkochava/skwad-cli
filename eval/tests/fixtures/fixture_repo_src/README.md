# fixture-repo

A tiny **deterministic** repository used by the eval judge test-suite. Its file
layout, contents, and symbols are fixed so canary claims can be refuted (or
confirmed) via the judge's Read / Grep / Glob tools.

## Modules
- `src/auth.py` — token validation (`validate_token`) and password hashing (`hash_password`).
- `src/utils.py` — config parsing (`parse_config`) and the `MAX_RETRIES` / `DEFAULT_TIMEOUT` constants.
- `src/cache.py` — an LRU cache (`LRUCache`) with `get` / `put` / `evict`.

## Behavior notes
- `MAX_RETRIES` bounds retry attempts conservatively.
- `LRUCache` protects recently-accessed keys: reads touch recency, so eviction
  drops the genuinely least-recently-used entry.
- `validate_token` enforces expiry against `TOKEN_TTL_SECONDS`.

> Ground-truth absences (symbols/files a fabricated claim might cite) are
> documented in the test harness (`eval/tests/fixture_repo.py`), deliberately
> NOT here — naming them in-repo would defeat grep/glob refutation.
