# Code Review — fixture-repo

Every claim below is **verifiable** against the fixture repo via Read / Grep / Glob.

## Findings

1. **`src/utils.py` — retry budget is conservative.** `MAX_RETRIES` is set to `3`,
   so a failing operation is retried at most three times. This is a reasonable
   default and bounds load on downstream services.

2. **`src/auth.py` — token expiry is enforced.** `validate_token` rejects empty
   tokens and compares elapsed time against `TOKEN_TTL_SECONDS` (3600s), so stale
   tokens are correctly refused.

3. **`src/cache.py` — eviction is genuinely LRU.** `LRUCache.get` calls
   `move_to_end`, and `evict` drops the front (oldest) entry via
   `popitem(last=False)`. Recently-read keys are therefore protected from eviction.

## Suggestions
- `hash_password` in `src/auth.py` uses the builtin `hash()`, which is not
  cryptographically secure — consider `hashlib` with a per-user salt.
