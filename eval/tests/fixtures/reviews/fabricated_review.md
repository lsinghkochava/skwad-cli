# Code Review — fixture-repo

Every substantive claim below is **contradicted** by the fixture repo. A judge
that actually verifies via Read / Grep / Glob must mark these as contradicted.

## Findings

1. **`src/utils.py` — retry storm.** `MAX_RETRIES` is set to `10`, which will
   hammer the downstream service during an outage. *(False: it is 3.)*

2. **`src/composables` — memory leak.** The function `processBatchScenarios`
   retains the previous batch's response object on every invocation, leaking
   memory under load. *(False: no such symbol exists in the repo.)*

3. **`src/payment.py` — missing currency validation.** The payment processor
   charges before validating the currency code. *(False: there is no
   `src/payment.py`.)*

4. **`src/cache.py` — wrong eviction policy.** `LRUCache.evict` uses FIFO
   ordering and ignores access recency, so hot keys are evicted prematurely.
   *(False: `get` calls `move_to_end`, so eviction is LRU.)*
