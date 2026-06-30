# Research Report: skwad-cli vs Claude CI Review Quality

*Generated: 2026-06-29 05:50 UTC | N=8 PRs | seed=12345*

## 1. Executive Summary

**Verdict**: evidence supports H1 (skwad-cli reviews score higher)

- PRs evaluated: 8
- skwad-cli wins (strict >): 8 | Claude CI wins: 0 | Ties: 0
- Primary effect size: Cliff's δ = 0.7812 (large), 95% BCa CI [0.5000, 1.0000]
- Primary Wilcoxon p-value: 0.0078

> **Threat to validity — cross-model judge & stylistic bias**: The judge (gpt-5.4, via codex) is a different model family from both review systems (claude-sonnet-4-6; Claude CI claude-sonnet-4-5-20250929). Cross-model judging removes the same-model self-preference confound (Zheng et al. 2023) — the judge has no in-family output to favor on either side, so the 8/8 result is not attributable to judge self-preference. Residual threat: the judge's own generic preferences — a GPT judge that rewards structure, explicit reasoning, and finding-count may favor skwad-cli's multi-agent, more-structured output, plausibly **over-stating** its margin. Treat the direction as robust (cross-model, grounded, claim-counted) but the magnitude — especially on the subjective score-only criteria — with that caution.

## 2. Hypothesis & Methodology

- **H1**: skwad-cli multi-agent reviews score higher than GitHub Claude CI reviews.
- **H0**: No meaningful difference between the two systems.
- **Test direction**: Two-sided Wilcoxon signed-rank (pre-registered primary endpoint: total score).

**Rubric**: 7 criteria × 0-3 scale, 21-point max. See `eval/config/rubric.json` and `eval/config/methodology.md`.

**Judge design**: Each PR judged 3× with counterbalanced A/B assignment (run 1: skwad=A; run 2: CI=A; run 3: seeded random). Majority vote (median) per criterion.

**Judge**: gpt-5.4 (via codex) — a different model family from both Claude-based reviewers (skwad-cli and Claude CI), so neither side is favored.

**Statistical plan**: Primary — Wilcoxon + Cliff's δ + 95% BCa CI. Exploratory — per-criterion (7) + per-difficulty (3) Wilcoxon under BH-FDR (q=0.05). IRR — Krippendorff's α (ordinal), bootstrap 95% CI (nboot=200), gate on lower bound ≥ 0.6.

**Power (N=30)**: Large δ ≥ 0.47 detectable; medium δ ≈ 0.33 marginal; small δ ≈ 0.15 not detectable.

## 3. Sample

| Repo | PR | Commit SHA | Difficulty |
| ---|---|---|--- |
| Kochava/frontend-mos | 1816 | 8ccdf860 | hard |
| Kochava/frontend-mos | 1818 | 4f5bc456 | hard |
| Kochava/watson | 893 | 67cd3640 | easy |
| Kochava/watson | 890 | 9fe96f58 | easy |
| Kochava/watson | 879 | 2730d1c2 | easy |
| Kochava/dash-api | 565 | b5c5eddf | hard |
| Kochava/dash-api | 558 | 43e786ac | medium |
| Kochava/dash-api | 562 | 844cb370 | hard |

**Heuristic vs LLM difficulty disagreements**: 0

**Skipped PRs**: 1
| Repo | PR | Reason |
| ---|---|--- |
| Kochava/frontend-mos | 1823 | worktree absent on resume |

## 4. Per-PR Results



### Kochava/dash-api#558 (difficulty: medium)

| Criterion | skwad-cli | Claude CI |
| ---|---|--- |
| Issue Detection | 3 | 0 |
| Actionability | 3 | 1 |
| Severity Accuracy | 2 | 0 |
| Coverage | 3 | 0 |
| Signal-to-Noise | 2 | 1 |
| Depth | 2 | 1 |
| Novel & Substantive Findings | 3 | 0 |
| **Total** | **18** | **3** |

### Verification Summary

| Review | Verified | Unverified | Contradicted | Non-falsifiable | Rate | Tool Calls | Ungrounded | Grounding |
| ---|---|---|---|---|---|---|---|--- |
| Skwad | 36 | 7 | 0 | 4 | 76% | 75 | 2 | 94% |
| Claude CI | 22 | 7 | 6 | 4 | 56% | 75 | 1 | 96% |

<details><summary>Judge reasoning (skwad-cli)</summary>

**Issue Detection**: Review A identifies seven genuine issues in the changed code and tests: missing-key postmeta writes, the empty install fixture, missing PUT failure-path coverage, PUT's non-atomic update behavior, the unbounded GetPosts call, client-visible internal terminology, and by-value copying of PartnerData.
**Actionability**: Nearly every finding includes a concrete code-level fix or a specific test case to add, with example code for the main defects.
**Severity Accuracy**: The per-finding LOW/MEDIUM/NIT labels are mostly reasonable, but the review overstates some medium test/documentation gaps as merge-blocking 'critical' problems.
**Coverage**: The review spans correctness/data consistency, test adequacy, performance, and client-facing error exposure, covering most relevant concern areas in this PR.
**Signal-to-Noise**: Most comments are valuable, but the review repeats the missing PUT failure-path test in two sections and includes a few pre-change assertions that cannot be verified from PR head.
**Depth**: The reasoning goes beyond surface comments: it traces the multi-step POST/PUT flows, connects helper extraction to test blind spots, and cross-checks helper behavior against GetPosts and HandleError implementations.
**Novel & Substantive Findings**: Review A contributes multiple substantive findings absent from Review B, especially around missing-key postmeta writes, test blind spots, PUT non-atomicity, and smaller perf/security issues.

</details>

<details><summary>Claim trace (Skwad)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| syncInstallPostmeta writes a formatted map lookup result without checking whethe | verified | Read | internal/handlers/partners/edit-postback.go:424 for _, value := range contextMeta.Template.ContextMeta { if value.Input. |
| installContextMeta() defines the install test fixture with no Template.ContextMe | verified | Read | internal/handlers/partners/edit-postback_test.go:1356 func installContextMeta() models.PartnerData { return models.Partn |
| The new POST install-guard tests call handlePost with installContextMeta(). | verified | Read | internal/handlers/partners/edit-postback_test.go:1461 handler.handlePost(w, req.Context(), req, "123", "321", "test-api- |
| The new PUT install-guard tests call handlePut with installContextMeta(). | verified | Read | internal/handlers/partners/edit-postback_test.go:1527 handler.handlePut(w, req.Context(), req, installContextMeta(), 456 |
| TestEditPostbackHandler_handlePut_InstallPipelineGuard covers only the missing-p | verified | Read | internal/handlers/partners/edit-postback_test.go:1476 { name: "no published pipeline -> 400 and trigger never upserted", |
| handlePut performs UpsertTrigger before syncInstallPostmeta and returns 500 on p | verified | Read | internal/handlers/partners/edit-postback.go:597 err := handler.DBGlobal().UpsertTrigger(ctx, editPostbackUpsert, filterN |
| The PUT comment says the guard avoids leaving the trigger updated while postmeta | verified | Read | internal/handlers/partners/edit-postback.go:586 // Validate the install pipeline before any GlobalDB writes so a missing |
| resolveInstallPipeline fetches posts with an empty limit and then returns only p | verified | Read | internal/handlers/partners/edit-postback.go:403 posts, err := dao.GetPosts(appID, "", []string{"ko_cpi_campaign"}, []str |
| handleDelete already uses GetPosts(appID, "1", ...) for the same pipeline lookup | verified | Read | internal/handlers/partners/edit-postback.go:731 if responseData.Trigger.Type == "install" { var posts []models.Post post |
| syncInstallPostmeta accepts models.PartnerData by value. | verified | Read | internal/handlers/partners/edit-postback.go:419 func syncInstallPostmeta(dao database.AppDAO, appID string, pipelineID i |
| models.PartnerData contains nested structs and slices, so passing it by value co | verified | Read | internal/utils/models/partners.go:128 type PartnerData struct { Trigger Trigger `json:"trigger"` Template Template `json |
| resolveInstallPipeline uses the client-visible error text "SAN-interlinks not sa | verified | Read | internal/handlers/partners/edit-postback.go:408 if len(posts) == 0 \|\| posts[0].ID < 1 { response.HandleError(ctx, w, h |
| HandleError serializes its errorMessage into the HTTP response body. | verified | Read | internal/utils/response/response.go:40 response := models.BaseResponse{ HTTPCode: httpCode, Success: false, Errors: []st |
| dbApp.GetPosts interpolates appID directly into the schema name using fmt.Sprint | verified | Read | internal/database/dbApp.go:46 fromSQL := fmt.Sprintf(`FROM app_%s.posts`, appID) |
| UpsertPostmeta first reads existing postmeta and then chooses update or insert. | verified | Read | internal/database/dbApp.go:158 func (db *ClusterAppDB) UpsertPostmeta(appID string, postID int, metaKey, metaValue strin |
| The review's statement that the <nil> write behavior was introduced by this PR c | unverified | Read | The checked-out repository only shows PR HEAD, so whether this behavior existed before the refactor cannot be establishe |
| The review's statement that the <nil> write behavior was pre-existing also canno | unverified | Read | Confirming pre-existing behavior requires the pre-change implementation, which is not present in the checked-out PR-head |
| The extracted helpers are consistent with other helpers in the partners package. | non_falsifiable | Read | This is a qualitative style judgment rather than a concrete, code-referent defect that can be proven true or false from  |
| POST install-pipeline coverage is thorough. | non_falsifiable | Read | Whether a test set is 'thorough' is subjective and does not have a concrete pass/fail code referent in the repository al |

</details>

<details><summary>Claim trace (Claude CI)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| The POST handler validates the install pipeline before creating a trigger. | verified | Read | internal/handlers/partners/edit-postback.go:446 // For install postbacks, validate the app cluster's install pipeline BE |
| The POST handler rolls back an orphan trigger with DeletePostback when syncInsta | verified | Read | internal/handlers/partners/edit-postback.go:466 if err := syncInstallPostmeta(handler.Base.AppDAO, appID, installPipelin |
| The PUT handler also validates the install pipeline before the GlobalDB write. | verified | Read | internal/handlers/partners/edit-postback.go:586 // Validate the install pipeline before any GlobalDB writes so a missing |
| The solution maintains transaction-like behavior across the two database writes. | contradicted | Read | internal/handlers/partners/edit-postback.go:597 err := handler.DBGlobal().UpsertTrigger(ctx, editPostbackUpsert, filterN |
| resolveInstallPipeline handles empty GetPosts results and zero-ID rows. | verified | Read | internal/handlers/partners/edit-postback.go:408 if len(posts) == 0 \|\| posts[0].ID < 1 { response.HandleError(ctx, w, h |
| The added tests include a zero-ID pipeline-row scenario. | verified | Read | internal/handlers/partners/edit-postback_test.go:1404 name: "GetPosts returns zero-id row -> 400 and trigger never creat |
| The added tests include a postmeta-failure rollback scenario. | verified | Read | internal/handlers/partners/edit-postback_test.go:1412 name: "postmeta upsert fails -> orphan trigger rolled back", setup |
| The new PUT install-pipeline tests provide excellent coverage for the fix. | contradicted | Read | internal/handlers/partners/edit-postback_test.go:1476 { name: "no published pipeline -> 400 and trigger never upserted", |
| The tests explicitly verify that CreateTrigger and UpsertTrigger are not called  | unverified | Read, Grep | The test file contains comments stating that intent, but it has no AssertNotCalled calls, and whether an unexpected test |
| The error message "SAN-interlinks not saved, invalid install pipeline for app" i | non_falsifiable | Read | Whether this message is 'clear' is a subjective assessment, not an objectively falsifiable code claim. |
| This change adds one extra GetPosts call per install postback operation. | unverified | Read | Determining whether the PR added an extra GetPosts call requires the pre-change implementation, which is not available i |
| No new security vulnerabilities are introduced. | unverified | Read | A broad absence-of-vulnerabilities claim cannot be conclusively proven from the checked-out repository state alone. |
| The helper functions follow Go best practices. | non_falsifiable | Read | This is a qualitative coding-style judgment rather than a concrete falsifiable defect claim. |
| Rollback-failure logging does not prevent the handler from returning the origina | verified | Read | internal/handlers/partners/edit-postback.go:469 if _, delErr := handler.DBGlobal().DeletePostback(appID, int(newTriggerI |

</details>

<details><summary>Raw review — skwad-cli</summary>

# PR #558 Review: fix/san-postback-orphan-trigger

**Overall Assessment:** The core fix is correct and addresses a real data consistency bug. However, the refactoring introduces **4 critical test and behavior gaps** that must be resolved before merge. Two of these (nil-to-postmeta and fixture masking) are actual data bugs; two are test coverage gaps that hide those bugs.

---

## Summary

This PR prevents orphan triggers when install postback saves fail by:
1. Validating the `ko_cpi_campaign` pipeline **before** any GlobalDB writes
2. Rolling back the trigger if postmeta sync fails in POST (non-idempotent CreateTrigger)
3. Refactoring postmeta logic into reusable helpers

The bug fix is solid. The implementation has clarity issues and a rollback asymmetry that exposes the system to inconsistent state in PUT.

---

## Critical Issues

### MEDIUM: `syncInstallPostmeta` writes literal `"<nil>"` for absent context keys

**File**: `edit-postback.go:429`  
**Severity**: MEDIUM (data corruption via omitted context fields)

```go
contextValue := fmt.Sprintf("%v", triggerContext[value.Input.ID])
```

When `triggerContext[value.Input.ID]` is absent, Go returns `nil` (interface{}). `fmt.Sprintf("%v", nil)` produces the literal string `"<nil>"`. This string is then written to postmeta via `UpsertPostmeta(appID, pipelineID, value.Input.ID, "<nil>")`. 

A client that omits a context key from the request body will have `"<nil>"` persisted for that key in every ContextMeta entry. This corrupts the postmeta row with a fake value instead of being absent. The bug is in a newly extracted function, making it introduced by this PR.

**Fix**: Check for nil/absent keys before writing:

```go
raw, ok := triggerContext[value.Input.ID]
if !ok || raw == nil {
    continue  // Skip writing postmeta for absent keys
}
contextValue := fmt.Sprintf("%v", raw)
```

---

### MEDIUM: `installContextMeta()` fixture masks the context loop entirely

**File**: `edit-postback_test.go:1356`, `edit-postback.go:424–433`  
**Severity**: MEDIUM (test blind spot prevents bug detection)

The shared `installContextMeta()` test fixture has an empty `Template.ContextMeta` slice. All existing tests using it skip the `for _, value := range contextMeta.Template.ContextMeta` loop entirely. Only the `UpsertPostmeta("cpi_partner_456", "true")` call is ever executed. This means **the `"<nil>"` bug above (Bug 1) is invisible to the test suite** — no test exercises the context loop with an actual context key.

**Fix**: Add a test fixture variant with at least one `ContextMeta` entry and use it in at least one test:

```go
func installContextMetaWithContext() models.PartnerData {
    data := installContextMeta()
    data.Template.ContextMeta = []models.ContextMeta{
        {
            Input: models.Input{ID: "test_key"},
            Label: models.Label{Name: "Test Field"},
        },
    }
    return data
}
```

Then update at least one POST and one PUT test to use this variant so the context loop is exercised.

---

### MEDIUM: PUT syncInstallPostmeta failure path has zero test coverage

**File**: `edit-postback_test.go:1470`, `TestEditPostbackHandler_handlePut_InstallPipelineGuard`  
**Severity**: MEDIUM (core new code path untested)

`TestEditPostbackHandler_handlePut_InstallPipelineGuard` covers:
- No pipeline → 400
- Happy path → 200

It does **not** cover: "UpsertTrigger succeeds but syncInstallPostmeta fails → 500 returned with no rollback."

This is a new failure path introduced by this PR, and it's invisible to tests. The POST test suite covers the analogous case (postmeta fails after CreateTrigger), but PUT does not.

**Fix**: Add this test case to `TestEditPostbackHandler_handlePut_InstallPipelineGuard`:

```go
{
    name: "postmeta upsert fails after trigger upserted -> 500, no rollback",
    setupMocks: func(mockDB *testdata.MockGlobalDBInterface, mockAppDAO *testdata.MockAppDAO) {
        mockAppDAO.On("GetPosts", "123", "", []string{"ko_cpi_campaign"}, []string{"publish"}).
            Return([]models.Post{{ID: 7}}, nil)
        mockDB.On("UpsertTrigger", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
        mockAppDAO.On("UpsertPostmeta", "123", 7, "cpi_partner_456", "true").
            Return(errors.New("app cluster unreachable"))
        // DeletePostback must NOT be called — PUT does not roll back.
    },
    expectedStatus: http.StatusInternalServerError,
},
```

---

### MEDIUM: Missing rollback in `handlePut` (asymmetric with `handlePost`)

**File**: `edit-postback.go:603–608`  
**Severity**: MEDIUM (data consistency gap + contradictory documentation)

The PR explicitly rolls back created triggers in POST when `syncInstallPostmeta` fails (lines 469–473). However, PUT does not roll back. The asymmetry has valid reasoning (UpsertTrigger is idempotent, CreateTrigger is not), **but it is undocumented**, and the existing comment contradicts this logic.

**The problem:**
- If `syncInstallPostmeta` fails after `UpsertTrigger` succeeds in PUT, the trigger row is updated but postmeta is not
- The caller receives HTTP 500, but the trigger is already committed to GlobalDB
- On retry, the user re-submits, creating a second inconsistency window
- The comment at line 586 says "so a missing pipeline returns 400 cleanly instead of leaving the trigger updated while postmeta failed" — implying the intent is to prevent trigger-updated-but-postmeta-failed entirely, when PUT *can* land in exactly that state

A maintainer reading the POST rollback logic will assume the PUT rollback is missing accidentally.

**Fix (choose one):**

**Option A** (recommended): Add a compensating action to PUT. This is complex because we'd need to retrieve the prior state to roll back to, or use a savepoint. Document this complexity:

```go
// After UpsertTrigger, if syncInstallPostmeta fails, we have no easy rollback
// since UpsertTrigger is idempotent — a retry will re-attempt the upsert.
// This is acceptable IF clients retry; unsuitable if clients expect atomic PUT.
// Consider wrapping in a transaction or documenting the non-atomicity contract.
```

**Option B** (pragmatic): Clarify that PUT is intentionally non-atomic. Add this comment around line 603:

```go
// No compensating rollback: UpsertTrigger is idempotent, so a client retry will
// re-upsert the same trigger row and re-attempt postmeta write cleanly. This differs
// from handlePost (where CreateTrigger is non-idempotent and requires rollback on
// postmeta failure). PUT callers must retry on 500; they will converge after postmeta
// succeeds. Trigger-updated-but-postmeta-failed state is transient and repair-friendly.
```

Then update line 586's comment to reflect this:

```go
// Validate install pipeline before UpsertTrigger (which is non-atomic with postmeta).
// On postmeta failure, callers should retry; UpsertTrigger is idempotent.
```

---

## Secondary Issues

### MEDIUM: PUT pipeline-guard test suite is incomplete

**File**: `edit-postback_test.go:1470` (`TestEditPostbackHandler_handlePut_InstallPipelineGuard`)  
**Severity**: MEDIUM (asymmetric rollback is untested + risky gap)

The POST test suite covers:
- No pipeline → 400 before CreateTrigger
- Zero-ID row → 400 before CreateTrigger
- postmeta fails → orphan rolled back
- Happy path

The PUT test suite covers only:
- No pipeline → 400 before UpsertTrigger
- Happy path

**Missing test**: "UpsertTrigger succeeds, syncInstallPostmeta fails → 500, no DeletePostback called"

This is the riskiest gap because the asymmetric rollback is the most subtle part of this PR. Without this test, the non-rollback behavior is not explicitly specified, and accidental rollback-adding would not be caught.

**Fix**: Add this test case to `TestEditPostbackHandler_handlePut_InstallPipelineGuard`:

```go
{
    name: "postmeta upsert fails after trigger upserted -> 500, no rollback",
    setupMocks: func(mockDB *testdata.MockGlobalDBInterface, mockAppDAO *testdata.MockAppDAO) {
        mockAppDAO.On("GetPosts", "123", "", []string{"ko_cpi_campaign"}, []string{"publish"}).
            Return([]models.Post{{ID: 7}}, nil)
        mockDB.On("UpsertTrigger", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
        mockAppDAO.On("UpsertPostmeta", "123", 7, "cpi_partner_456", "true").
            Return(errors.New("app cluster unreachable"))
        // DeletePostback must NOT be called — PUT does not roll back.
    },
    expectedStatus: http.StatusInternalServerError,
},
```

This documents the intended behavior and would catch an accidental rollback being added.

---

### LOW: Missing `LIMIT 1` in `resolveInstallPipeline`

**File**: `edit-postback.go:403`  
**Severity**: LOW (performance, pre-existing but extracted)

```go
posts, err := dao.GetPosts(appID, "", []string{"ko_cpi_campaign"}, []string{"publish"})
```

The second argument `""` means `GetPosts` (in `dbApp.go:37-67`) generates a query with **no LIMIT clause**. All published posts are fetched over the wire, but only `posts[0].ID` is read. The old inline code had this same issue, but extraction into a named helper is the right place to fix it. Compare to `handleDelete` (line 732):

```go
posts, err = dbAppDao.GetPosts(appID, "1", []string{"ko_cpi_campaign"}, []string{"publish"})
```

**Fix**: Pass `"1"` as the limit to reduce DB load:

```go
posts, err := dao.GetPosts(appID, "1", []string{"ko_cpi_campaign"}, []string{"publish"})
```

Impact is linear with the number of published posts per app.

---

### NIT: `PartnerData` copied by value into `syncInstallPostmeta`

**File**: `edit-postback.go:419`  
**Severity**: NIT (minor performance, introduced by extraction)

```go
func syncInstallPostmeta(dao database.AppDAO, appID string, pipelineID int, contextMeta models.PartnerData, ...) error {
```

`models.PartnerData` contains `[]Event`, `[]ContextMeta`, nested structs — a non-trivial value copy on every call. The old inline code had `contextMeta` already in scope, so no copy. The new helper introduces a copy on both POST and PUT paths. Since both are hot paths for install postbacks, pass by pointer:

```go
func syncInstallPostmeta(dao database.AppDAO, appID string, pipelineID int, contextMeta *models.PartnerData, ...) error {
```

Then update callsites to pass `&contextMeta`. Negligible absolute impact but consistent with Go idioms for large structs.

---

### LOW: Internal system terminology exposed in error response

**File**: `edit-postback.go:409`  
**Severity**: LOW (information disclosure, auth-gated)

```go
response.HandleError(ctx, w, http.StatusBadRequest, response.DebugLvl, "SAN-interlinks not saved, invalid install pipeline for app")
```

This error message is written directly to the HTTP response. Terms like "SAN-interlinks" and "install pipeline" are internal product terminology useful for targeted probing. The endpoint is auth-gated, but the message itself reveals architecture.

**Fix**: Sanitize to a client-facing message:

```go
response.HandleError(ctx, w, http.StatusBadRequest, response.DebugLvl, "Install postback configuration is incomplete")
```

---

## Non-Issues / Pre-existing

### ✅ Code organization
- Extraction of `resolveInstallPipeline` and `syncInstallPostmeta` is consistent with other helpers in the `partners` package
- Pipeline-first ordering change is correct
- `handleDelete` intentionally does NOT use `resolveInstallPipeline` — it treats missing pipelines as skippable, not an error

### ✅ Test coverage for POST
- Thorough and explicit about mock expectations
- Covers happy path, no-pipeline, zero-ID, and rollback scenarios

### ⚠️  Pre-existing gaps (flagged for awareness, not blocking this PR)
- `dbApp.go` interpolates `appID` directly into SQL schema names without integer validation (`fmt.Sprintf("... app_%s.posts", appID)`). This is a SQL identifier injection path that predates this PR but worth a separate security fix.
- `fmt.Sprintf("%v", triggerContext[key])` produces literal `"<nil>"` string when key is absent, allowing clients to write `"<nil>"` to postmeta. Same behavior as before; flag for future cleanup.
- N+1 `UpsertPostmeta` pattern (each call does SELECT + UPDATE/INSERT) — identical to old code, not introduced by this PR.

---

## Recommendation

**DO NOT MERGE** until these critical bugs are fixed:

### 🛑 **MUST FIX** (blocking):
1. **`"<nil>"` bug** — Add nil-check before writing context keys to postmeta (line 429)
2. **Test fixture gap** — Add `installContextMetaWithContext()` variant with ContextMeta entries and use in at least one POST and one PUT test
3. **PUT failure path untested** — Add test case for "UpsertTrigger succeeds, syncInstallPostmeta fails → 500" scenario
4. **Rollback asymmetry undocumented** — Clarify PUT's intentional non-atomicity with explicit comments explaining idempotency reasoning

### 🔧 **SHOULD FIX** (before merge, non-blocking if documented):
5. Add `LIMIT 1` to `resolveInstallPipeline` GetPosts call (line 403)
6. Sanitize error message at line 409 to avoid leaking internal terminology
7. Pass `PartnerData` by pointer in `syncInstallPostmeta` signature

The core bug fix is sound, but test coverage gaps mask two actual data bugs. Once the critical items are fixed, the PR will be merge-ready.



</details>

<details><summary>Raw review — Claude CI</summary>

### PR Review: Orphan Triggers Fix

I have completed a comprehensive review of this PR that fixes the orphan trigger issue for install postbacks. Here's my detailed analysis:

#### Todo List:
- [x] Read repository standards and guidelines
- [x] Analyze the main handler changes in `edit-postback.go`
- [x] Review the test additions in `edit-postback_test.go`
- [x] Check for potential bugs and edge cases
- [x] Evaluate error handling and rollback logic
- [x] Assess performance implications
- [x] Review security considerations
- [x] Evaluate test coverage adequacy
- [x] Provide comprehensive feedback and recommendations

---

### Review Summary ✅

**Overall Assessment:** This is a well-implemented fix that addresses the root cause of the orphan trigger issue. The solution follows defensive programming principles with proper validation and rollback mechanisms.

### Code Quality & Architecture ✅

**Excellent refactoring approach:**
- **Early validation**: The new `resolveInstallPipeline` function validates the install pipeline BEFORE creating triggers, preventing the original race condition
- **Clean separation**: Pipeline resolution and postmeta sync are properly abstracted into focused functions
- **Rollback compensation**: POST handler correctly rolls back orphaned triggers using `DeletePostback` when postmeta fails
- **Consistent application**: Both POST and PUT handlers use the same validation approach

**Function design follows Go best practices:**
- Clear single responsibilities for `resolveInstallPipeline` and `syncInstallPostmeta`
- Proper error handling with early returns
- Good function signatures with clear return values

### Bug Prevention ✅

**Original Issue Resolution:**
- ✅ Eliminates orphan triggers by validating pipeline first
- ✅ Prevents silent failures where UI shows success but postmeta doesn't persist  
- ✅ Maintains transaction-like behavior across two databases

**Edge Cases Handled:**
- ✅ Empty `GetPosts` results (lines 408-411 in edit-postback.go)
- ✅ Zero ID pipeline rows  
- ✅ Database connection failures
- ✅ Rollback failure logging without throwing additional errors

### Security Considerations ✅

**No new security vulnerabilities introduced:**
- Same authentication and authorization checks maintained
- API key validation preserved
- Input validation unchanged
- Audit logging remains comprehensive

### Performance Analysis ✅

**Performance impact is minimal and justified:**
- **Additional DB call**: One extra `GetPosts` call per install postback operation
- **Network overhead**: Negligible for the reliability gained
- **User experience**: Eliminates confusing retry behavior, improving perceived performance
- **Database efficiency**: Prevents accumulation of orphan records

### Test Coverage Assessment 🔍

**Excellent test coverage for the fix:**
- ✅ **New focused test cases** in `TestEditPostbackHandler_handlePost_InstallPipelineGuard` and `TestEditPostbackHandler_handlePut_InstallPipelineGuard`
- ✅ **Edge case coverage**: Missing pipeline, zero ID, postmeta failures, happy path
- ✅ **Rollback verification**: Tests confirm orphan trigger deletion occurs
- ✅ **Mock expectations**: Proper verification that CreateTrigger/UpsertTrigger are NOT called when pipeline validation fails

**Test quality follows best practices:**
- Clear test names describing scenarios
- Proper mock setup and expectations
- Verification of both positive and negative paths

### Documentation & Error Messages ✅

**Good error messaging:**
- Clear error message: "SAN-interlinks not saved, invalid install pipeline for app" 
- Helpful logging for rollback failures
- Detailed function documentation

### Specific Code Quality Notes

**`edit-postback.go:402-413` - `resolveInstallPipeline`:**
✅ **Excellent defensive pattern**
- Returns both success flag and ID
- Writes HTTP error response directly (prevents caller errors)
- Clear validation logic

**`edit-postback.go:419-434` - `syncInstallPostmeta`:**
✅ **Well-structured sync function**
- Returns error for caller's rollback decision
- Handles template context iteration safely
- Good separation of concerns

**`edit-postback.go:466-474` - Rollback logic in POST:**
✅ **Proper compensation pattern**
- Attempts rollback without failing the response
- Logs rollback failures appropriately
- Follows error handling best practices

### Minor Suggestions

**1. Error handling consistency** (edit-postback.go:469):
Consider using `zap.Error` for the rollback error to maintain logging consistency:
```go
clog.Error(ctx, "Failed to roll back orphan trigger after postmeta error", 
    zap.Int64("trigger_id", newTriggerID), 
    zap.Error(delErr), 
    zap.Error(err)) // Include the original postmeta error too
```

**2. Function visibility** (edit-postback.go:399, 419):
Consider whether `resolveInstallPipeline` and `syncInstallPostmeta` should be exported if other handlers might need similar functionality.

### Compliance with Standards ✅

**Adheres to Kochava Go Style Guide:**
- ✅ Proper error handling with contexts
- ✅ Database interface usage
- ✅ Function naming conventions
- ✅ Variable declaration practices
- ✅ HTTP handler patterns

### Recommendation: **APPROVE** ✅

This PR successfully fixes a critical data consistency issue while maintaining code quality and following established patterns. The comprehensive test coverage and defensive programming approach give confidence in the solution's reliability.

The fix is minimal, targeted, and backward-compatible while eliminating a customer-facing issue that required manual retries.

---

[View job](https://github.com/Kochava/dash-api/actions/runs/26238044547)

</details>

<details><summary>Classifier output</summary>

{'bucket': 'medium'}

</details>

### Kochava/dash-api#562 (difficulty: hard)

| Criterion | skwad-cli | Claude CI |
| ---|---|--- |
| Issue Detection | 3 | 2 |
| Actionability | 3 | 2 |
| Severity Accuracy | 1 | 0 |
| Coverage | 3 | 1 |
| Signal-to-Noise | 2 | 1 |
| Depth | 3 | 1 |
| Novel & Substantive Findings | 3 | 1 |
| **Total** | **18** | **8** |

### Verification Summary

| Review | Verified | Unverified | Contradicted | Non-falsifiable | Rate | Tool Calls | Ungrounded | Grounding |
| ---|---|---|---|---|---|---|---|--- |
| Skwad | 34 | 5 | 5 | 3 | 72% | 87 | 1 | 97% |
| Claude CI | 15 | 7 | 7 | 1 | 45% | 87 | 2 | 90% |

<details><summary>Judge reasoning (skwad-cli)</summary>

**Issue Detection**: Review A identifies many genuine issues that are borne out by the code: non-atomic multi-key writes, per-key upsert fanout, incomplete reset deletion, a mismatched test expectation, per-request regex compilation, cache-miss stampedes, an unnecessary Watson call on reset, cold-cache GET latency, returning cached maps by reference, a brittle fixed-size assertion, and duplicated fallback values in the test mock. A few architecture/convention points are more speculative, and one positive claim is contradicted.
**Actionability**: Most findings include concrete remediation steps, and the major ones come with code-level direction rather than vague advice.
**Severity Accuracy**: The review correctly elevates the main correctness and test issues, but it overstates some medium concerns as must-fix/blocking and includes at least one contradicted performance statement, so prioritization is not consistently calibrated.
**Coverage**: The verified findings cover correctness, performance, API/cache behavior, test robustness, and maintainability. It reaches most of the meaningful concern areas exposed by this change.
**Signal-to-Noise**: Signal is high overall, but not perfect: the review contains one clear false-positive/contradicted statement and a few speculative architecture comments.
**Depth**: The strongest findings trace behavior across handler logic, DAO implementation, delete semantics, caching, and tests. The review reasons about control flow and state transitions rather than staying at a stylistic level.
**Novel & Substantive Findings**: Review A contributes multiple substantive issues that Review B does not surface, especially around write atomicity, DB fanout, reset semantics, test correctness, cache stampedes, and unnecessary runtime dependency on Watson during reset.

</details>

<details><summary>Claim trace (Skwad)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| Partial upsert failure can leave reconciliation settings partially written becau | verified | Read | internal/handlers/partners/configure-reconciliation.go:305 for key, value := range payload { |
| Each upsert fans out into an existence read plus an update/insert, creating a 2N | verified | Read | internal/database/dbApp.go:160 postmetas, err := db.GetPostmetas(appID, []int{postID}, []string{metaKey}) |
| deleteReconciliationSettings can leave orphaned reconciliation postmeta behind b | verified | Read | internal/database/dbApp.go:172 query := fmt.Sprintf("DELETE FROM app_%s.postmeta WHERE post_id = ? AND meta_key IN (?)", |
| The SparsePOST test uses the wrong LIKE pattern in its specific GetPostmetas moc | verified | Read | internal/handlers/partners/configure-reconciliation_test.go:268 []string{"cpi_2_"}, mock.AnythingOfType("[]map[string]st |
| regexp.MustCompile is executed on every request in the reconciliation handlers. | verified | Read | internal/handlers/partners/configure-reconciliation.go:168 if !regexp.MustCompile(`^[0-9]+$`).MatchString(appID) { |
| The Watson client has no singleflight protection on cache misses. | verified | Grep | grep:singleflight |
| Placing the client under internal/clients/watson breaks an existing project-wide | unverified | Read, Glob | The repository shows both flat service packages and the new internal/clients/watson path, but whether this is a broken c |
| BaseHandler now mixes two incompatible optionality patterns for service clients  | unverified | Read | The struct does use a pointer for AIMWebhookClient and an interface for WatsonClient, but the review's claim that this i |
| deleteReconciliationSettings unnecessarily calls into Watson via effectiveDefaul | verified | Read | internal/handlers/partners/configure-reconciliation.go:323 defaults := h.effectiveDefaults(ctx) |
| GET requests can incur a synchronous Watson call with the configured 2s timeout  | verified | Read | internal/config/config.go:64 WatsonTimeout time.Duration `env:"WATSON_TIMEOUT,required=false,default=2s"` |
| On cache hit, GetReconciliationDefaults returns the cached map by reference with | verified | Read | internal/clients/watson/client.go:69 return m |
| The effectiveDefaults test's fixed len(got)==20 assertion is brittle if Watson a | verified | Read | internal/handlers/partners/configure-reconciliation_test.go:399 assert.Equal(t, 20, len(got), "expected 20 keys total (4 |
| Sharing the global cache instance means expired Watson entries never evict becau | unverified | Read | The code shows a shared cache created with cache.New(0, 0), but the review's eviction behavior depends on library semant |
| The fallback values are duplicated manually in fallback.go and the mock Watson c | verified | Read | internal/utils/helpers/testdata/mock_watson_client.go:25 m.On("GetReconciliationDefaults", mock.Anything).Return(map[str |
| No N+1 query patterns were detected in the changed code. | contradicted | Read | internal/database/dbApp.go:160 postmetas, err := db.GetPostmetas(appID, []int{postID}, []string{metaKey}) |

</details>

<details><summary>Claim trace (Claude CI)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| The PR introduces a HIGH severity bug because effectiveDefaults returns empty st | contradicted | Read | internal/handlers/partners/configure-reconciliation.go:306 if value == "" { |
| Ignoring the error from resp.Body.Close is a concrete defect that masks connecti | unverified | Read | The code does ignore Close errors, but the repository evidence here does not establish a concrete failure mode or user-v |
| Empty values are not validated before persistence in updateReconciliationSetting | contradicted | Read | internal/handlers/partners/configure-reconciliation.go:307 continue |
| Watson configuration lacks required validation and should be rejected at startup | unverified | Read, Grep | Watson settings are defined and passed through, but the review's claim that startup validation is required is a recommen |
| Keeping fallbackDefaults synchronized with Watson's EngineDefaults is a real mai | verified | Read | internal/clients/watson/fallback.go:8 // Keep values in sync with watson/cmd/watson/ruleset/defaults.go EngineDefaults. |
| Current coverage is 84.84% with 15 missing lines. | unverified | Read | That percentage cannot be confirmed from the repository contents without running coverage tooling, which this evaluation |
| Invalid numeric reconciliation values are not validated before persistence and c | unverified | Read | The handler does not validate numeric payload contents, but the downstream breakage asserted by the review is not tracea |

</details>

<details><summary>Raw review — skwad-cli</summary>

# PR #562 Code Review: feat(reconciliation): source defaults from watson, write only payload keys

**PR Author:** Mohamed Benali  
**Status:** ⛔ NEEDS FIXES BEFORE MERGE  
**Overall Assessment:** Well-designed fix with solid fundamentals, but critical performance issue (N+1 DB) and architectural inconsistencies must be addressed.

---

## Summary

This PR fixes a critical bug where toggling Override ON in Partner Configuration without changing any fields would silently shrink impression-device attribution from 14 days (watson engine default) to 1 day (stale dash-api default). The fix has two layers:

1. **Read truth from watson at runtime** — New HTTP client fetches `/api/v1/reconciliation/defaults` with 5-min cache
2. **Stop writing defaults on Save** — POST now persists only keys present in payload (sparse write), not all 20 defaults

---

## Issues Summary

**CRITICAL (Data Corruption Risk) — Must Fix Before Merge:**
0. ⛔ CRITICAL — Partial upsert failure leaves DB inconsistent (no transaction wrapping)
1. ⛔ HIGH — Partial upsert also: N+1 DB pattern (2N DB calls for N keys)
2. ⛔ HIGH — `deleteReconciliationSettings` silently skips orphaned postmeta keys (LIKE mismatch)
3. ⛔ HIGH — SparsePOST test mock LIKE pattern incorrect (`cpi_2_` not `cpi_2_%`)

**MEDIUM (Architecture & Design) — Must Fix Before Merge:**
4. MEDIUM — `regexp.MustCompile` called on every request
5. MEDIUM — No singleflight on watson cache miss
6. MEDIUM — Package placement breaks existing conventions (`internal/clients/watson` vs `internal/aimwebhook`)
7. MEDIUM — BaseHandler has two different optionality patterns

**Worth Addressing Soon:**
7. MEDIUM — Synchronous 2s HTTP call degrades GET latency on cold cache
8. NIT — Cached map returned without cloning

---

## Strengths

### Security ✅ CLEAR
- ✅ HTTP client has proper timeout (configurable, default 2s) and context propagation
- ✅ Response validation checks for non-empty map before accepting
- ✅ Fallback mechanism is safe — returns in-binary defaults on any HTTP error
- ✅ No credentials in URLs; feature header only set when non-empty (routing label, not a secret)
- ✅ Response cloning avoids mutation issues; handler returns new map each time
- ✅ appID validated as digits-only via regex (lines 85, 168, 230)
- ✅ networkID validated as integer + DB lookup before use
- ✅ Sparse POST doesn't introduce new attack surface (pre-existing payload-key-write capability)
- ✅ No SSRF, credential leakage, or injection risks identified
- ✅ New outbound HTTP connection only to internal watson service (not user-controlled)

**Security Sentinel verdict:** Clear to merge from security standpoint. Attack surface is narrow and well-guarded.

### Performance
- ✅ Cache TTL (5 min) is appropriate — limits watson hits while staying fresh
- ✅ Sparse POST reduces DB writes significantly vs pre-fix behavior (1-2 keys vs all 20)
- ✅ No N+1 query patterns detected
- ✅ GET handler calls watson client once per request, but cache mitigates for 5 min windows

### Correctness
- ✅ Sparse POST logic is sound — skipping empty values prevents watson parser corruption
- ✅ `effectiveDefaults()` correctly merges watson defaults + 16 UI blanks
- ✅ `mergeRulesWithDefaults()` correctly overlays postmeta overrides
- ✅ `deleteReconciliationSettings()` correctly uses `effectiveDefaults()` to determine all keys
- ✅ Empty-value skip is well-justified in comments (prevents strconv.ParseFloat("") → 0 coercion)

### Architecture
- ✅ Watson client package mirrors `aimwebhook` pattern — consistent with existing clients
- ✅ Dependency injection via `BaseHandler` is clean
- ✅ `nilClient` pattern for optional watson is elegant and allows local dev
- ✅ Interface is minimal (one method) and testable
- ✅ Mock watson client pre-stubbed for most tests — good DX pattern

### Test Coverage
- ✅ Sparse POST tests cover: single key, two keys, empty payload, empty values, mixed
- ✅ GET tests verify watson defaults (336/168/30/30) vs old hardcoded values (24/30)
- ✅ Postmeta override tests verify explicit values win over defaults
- ✅ Watson client tests cover: HTTP success, error, timeout, empty body, malformed JSON, nil cache, new keys
- ✅ Test structure uses testify mocks effectively with Maybe() for optional calls

---

## Observations & Considerations

### Must Verify Before Merge

1. **Fallback values must match watson engine defaults** (`internal/clients/watson/fallback.go`)
   - The fallback map (device_id_lookback_window=30, engaged_click_lookback_device=30, etc.) must stay in sync with watson's actual EngineDefaults
   - Comment says "Keep values in sync with watson/cmd/watson/ruleset/defaults.go EngineDefaults"
   - Consider: Can we add a note in release checklist to verify this on watson release changes?

2. **Coordination with watson PR #883**
   - This PR depends on watson#883 which adds `/api/v1/reconciliation/defaults` endpoint
   - Both PRs must merge together for feature to work
   - Fallback behavior means dash-api can deploy first safely (nilClient returns fallback until watson endpoint is live)

3. **Feature header validation**
   - `X-Watson-Feature` header is sent as-is without validation (`featureHeader` parameter)
   - Current usage (e.g., "watson-mbenali") seems safe for internal feature branches
   - Not a security issue, but worth noting if header becomes user-configurable

### Performance Considerations

1. **GET latency on cache miss**
   - First GET request after cache expiry will add up to 2 seconds (watson timeout)
   - Mitigated by: 5-min cache TTL, fallback on error, typical cache hits
   - Acceptable for Partner Configuration UI which isn't performance-critical

2. **Watson availability requirement**
   - GET requests now depend on watson availability (with fallback safety)
   - With 5-min cache, brief watson outages won't affect UI
   - Consider: monitoring watson client errors for alerting

### Edge Cases Handled Well

1. ✅ Empty payload (POST with `{}`) writes nothing — correct behavior
2. ✅ Empty values in payload are skipped, not persisted — prevents watson parser bugs
3. ✅ Nil cache supported (client works without caching) — line 288 in tests
4. ✅ New keys from watson are passed through — "future_new_key" test passes them along
5. ✅ Override logic: postmeta values override watson defaults correctly

### Code Quality Notes

- ✅ Error wrapping is good: `fmt.Errorf("upsert %s: %w", metaKey, err)`
- ✅ Logging is appropriate: info on startup, warn on fallback, info on reads
- ✅ Comments are thorough and explain the WHY (empty value skip, sparse POST rationale)
- ✅ No global mutable state; clean dependency injection

---

## Critical Issues Found (Must Fix)

### 0. **CRITICAL: Partial Upsert Failure Leaves DB Inconsistent** ⛔ BLOCKS MERGE

**Location:** `internal/handlers/partners/configure-reconciliation.go:305-312`

The `updateReconciliationSettings` function loops over payload keys calling `UpsertPostmeta` one-by-one with **NO WRAPPING TRANSACTION**:

```go
for key, value := range payload {
    if value == "" {
        continue
    }
    if err := dbAppDAO.UpsertPostmeta(appID, pipelineID, metaKey, value); err != nil {
        return nil, fmt.Errorf("upsert %s: %w", metaKey, err)
    }
}
```

**The Problem:** If the Nth upsert fails, the first N-1 are already committed to the database with no rollback. The handler returns a 500 error to the client, but postmeta is now **partially updated** — some keys reflect the new values, others don't.

**Concrete Scenario:** 
- Payload: `{impression_lookback_device: "240", engaged_view_lookback_device: "100", engaged_click_lookback_device: "50"}`
- Upsert A (impression_lookback_device) succeeds and commits
- Upsert B (engaged_view_lookback_device) hits a transient DB error → handler returns 500
- Client sees error but `impression_lookback_device` is now updated while the others are stale
- On retry, idempotency is lost — third attempt may partially update other keys, creating inconsistent state

**Impact:** Data corruption in postmeta. Users see partial saves. Watson reads inconsistent values on reconciliation.

**Fix:** Wrap the entire upsert loop in a DB transaction:

```go
// Start transaction
tx := dbAppDAO.BeginTx(ctx)
for key, value := range payload {
    if value == "" {
        continue
    }
    metaKey := fmt.Sprintf("cpi_%s_%s", networkID, key)
    if err := tx.UpsertPostmeta(appID, pipelineID, metaKey, value); err != nil {
        tx.Rollback()
        return nil, fmt.Errorf("upsert %s: %w", metaKey, err)
    }
}
// Commit atomically
if err := tx.Commit(); err != nil {
    return nil, fmt.Errorf("commit: %w", err)
}
```

This is **separate from and in addition to** the N+1 perf fix. Even if upserts are batched into one call, that call must be atomic.

---

### 1. **HIGH: N+1 DB Pattern in POST Handler** ⛔ BLOCKS MERGE

**Location:** `internal/handlers/partners/configure-reconciliation.go:305-312`

Each `UpsertPostmeta` call does **two** DB round trips:
1. `GetPostmetas` (SELECT to check existence)
2. Either `UpdatePostmetas` or `InsertPostmeta`

**Impact:** A payload with N keys = **2N sequential DB calls per save**. A full 16-key settings form = **32 DB round trips**. Serious performance regression.

**Fix:** Combine with transaction fix — batch all upserts into one transactional `INSERT ... ON DUPLICATE KEY UPDATE` call.

---

### 2. **HIGH: `deleteReconciliationSettings` Silently Skips Orphaned Keys** ⛔ BLOCKS MERGE

**Location:** `internal/handlers/partners/configure-reconciliation.go:322-334`

```go
func (h *ConfigureReconciliation) deleteReconciliationSettings(...) {
    defaults := h.effectiveDefaults(ctx)  // Gets 4 watson keys + 16 blank keys
    metaKeys := make([]string, 0, len(defaults))
    for key := range defaults {
        metaKeys = append(metaKeys, fmt.Sprintf("cpi_%s_%s", networkID, key))
    }
    // Delete only those 20 keys
    return dbAppDAO.DeletePostmeta(appID, pipelineID, metaKeys)
}
```

**The Problem:** Only deletes 20 hardcoded key names. But `handlePUT` detects postmeta existence with a LIKE scan (`cpi_<nid>_%`). Any postmeta key that doesn't fit the 20 — written by old code, manual migration, legacy values — is silently left behind.

**Concrete Scenario:**
- App has old postmeta `cpi_5_legacy_custom_key` from legacy code
- User clicks Reset
- Handler scans LIKE `cpi_5_%`, finds it, proceeds
- `deleteReconciliationSettings` only deletes 20 known keys, skips the legacy one
- Returns 200 OK, but orphaned key persists
- Watson reads the legacy key on next reconciliation

**Fix:** Delete all keys matching the LIKE pattern, not just the 20 known ones:

```go
func (h *ConfigureReconciliation) deleteReconciliationSettings(...) {
    metaKeys := []string{fmt.Sprintf("cpi_%s_%", networkID) + "%"}
    return dbAppDAO.DeletePostmeta(appID, pipelineID, metaKeys)
}
```

Or use the postmetas already fetched in `handlePUT` to determine which keys to delete.

---

### 3. **HIGH: SparsePOST Test Mock LIKE Pattern Incorrect** ⛔ BLOCKS MERGE

**Location:** `internal/handlers/partners/configure-reconciliation_test.go:267-268`

```go
mockAppDAO.On("GetPostmetas", "12345", []int{99},
    []string{"cpi_2_"},   // ← WRONG: missing % suffix
    mock.AnythingOfType("[]map[string]string")).
    Return([]models.Postmeta{}, nil).Maybe()
```

**The Problem:** Production code builds `[]string{fmt.Sprintf("cpi_%s_", networkID) + "%"}` = `[]string{"cpi_2_%"}`. Testify does deep equality matching, so this mock never fires. The broad catch-all `.Maybe()` mock catches it instead. The test passes but **doesn't actually verify the LIKE pattern**.

**Impact:** Removing the `%` from production code won't be caught by tests. The assertion is silent about the LIKE syntax.

**Fix:** Change the expected slice and remove the broad fallback:

```go
mockAppDAO.On("GetPostmetas", "12345", []int{99},
    []string{"cpi_2_%"},  // ← CORRECT
    mock.AnythingOfType("[]map[string]string")).
    Return([]models.Postmeta{}, nil)
```

Or keep `.Maybe()` only for truly unexpected calls, not as the primary mock.

---

### 2. **MEDIUM: regexp.MustCompile Called on Every Request**

**Location:** `internal/handlers/partners/configure-reconciliation.go:85, 168, 231`

All three handlers (GET, POST, PUT) call `regexp.MustCompile('^[0-9]+$').MatchString(appID)` inline:
```go
if !regexp.MustCompile(`^[0-9]+$`).MatchString(appID) {
```

**The Problem:** The regex is recompiled on every HTTP request. Go's regex compiler allocates and invokes the NFA builder each time — unnecessary GC pressure.

**Fix:** Hoist to a package-level variable:
```go
var appIDRegex = regexp.MustCompile(`^[0-9]+$`)

// Then in handlers:
if !appIDRegex.MatchString(appID) {
```

---

### 3. **MEDIUM: No Singleflight on Watson Cache Miss**

**Location:** `internal/clients/watson/client.go:65-84`

The cache check and HTTP fetch have no singleflight guard:
```go
if c.cache != nil {
    if v, ok := c.cache.Get(cacheKey); ok {
        if m, ok := v.(map[string]string); ok && len(m) > 0 {
            return m
        }
    }
}

defaults, err := c.fetch(ctx)  // <-- Every concurrent goroutine refetches here
```

**The Problem:** When the 5-min TTL expires under load, every concurrent goroutine that sees a miss fires its own request to watson before any of them cache the result. One cache expiry event can translate into dozens of simultaneous watson calls.

**Fix:** Use `golang.org/x/sync/singleflight`:
```go
import "golang.org/x/sync/singleflight"

type clientImpl struct {
    // ...
    sfGroup singleflight.Group
}

func (c *clientImpl) GetReconciliationDefaults(ctx context.Context) map[string]string {
    if c.cache != nil {
        if v, ok := c.cache.Get(cacheKey); ok {
            if m, ok := v.(map[string]string); ok && len(m) > 0 {
                return m
            }
        }
    }

    v, _, _ := c.sfGroup.Do(cacheKey, func() (interface{}, error) {
        return c.fetch(ctx)
    })
    defaults, _ := v.(map[string]string)
    if len(defaults) == 0 {
        return cloneMap(fallbackDefaults)
    }
    if c.cache != nil {
        c.cache.Set(cacheKey, defaults, cacheTTL)
    }
    return defaults
}
```

---

### 4. **NIT: Hardcoded Key Count Test is Brittle** 

**Location:** `internal/handlers/partners/configure-reconciliation_test.go:837`

```go
assert.Equal(t, 20, len(got), "expected 20 keys total (4 engine + 16 blank)")
```

If watson returns a 5th engine key (that isn't already in the 16 blanks), the map has 21 entries and this test fails for the wrong reason. `TestClient_NewKeyFromServer_PassedThrough` explicitly tests that new keys from watson propagate, so this invariant will eventually break.

**Fix:** Assert by presence, not by count:
```go
assert.Equal(t, "336", got["impression_lookback_device"])
assert.Equal(t, "168", got["engaged_view_lookback_device"])
assert.Equal(t, "30", got["engaged_click_lookback_device"])
assert.Equal(t, "30", got["device_id_lookback_window"])
for _, key := range []string{...all 16 blank keys...} {
    assert.Equal(t, "", got[key])
}
```

Or use `assert.GreaterOrEqual(t, len(got), 20)`.

---

## Architectural Issues (Must Address)

### 5. **MEDIUM: Package Placement Breaks Existing Convention**

**Location:** `internal/clients/watson/` (NEW)

**The Problem:** The project has no `clients/` namespace. Every other service client sits flat under `internal/`:
- `aimwebhook/` (AIM webhook client)
- `httpclient/` (Generic HTTP client)
- `pubsub/` (Google Pub/Sub)
- `database/` (DB connections)

Placing watson at `internal/clients/watson/` implicitly starts a new `clients/` sub-namespace with only one occupant and no documented rationale.

**Concrete Risk:** The next engineer adding a new service client will see `internal/aimwebhook` and `internal/httpclient`, follow that pattern, and create `internal/newservice`. You'll end up with two conventions in one codebase: `internal/clients/watson` and `internal/newservice`.

**Fix:** Either:
1. Move watson to `internal/watson/` (matches existing flat layout), OR
2. Document explicitly in `CLAUDE.md` that all future service clients go under `internal/clients/` and file a follow-up to migrate existing clients.

Don't leave the intent ambiguous.

---

### 6. **MEDIUM: BaseHandler Optionality Pattern Inconsistency**

**Location:** `internal/handlers/base_handler.go:42-47`

Two different patterns for optional service clients:
```go
AIMWebhookClient  *aimwebhook.Client  // concrete ptr, can be nil
WatsonClient      watson.Client       // interface, always non-nil (NilObject)
```

**The Problem:** 
- `AIMWebhookClient` can be nil; callers must nil-check before use (legacy pattern, error-prone)
- `WatsonClient` is always non-nil via the NilObject pattern (safer, modern pattern)

Having both patterns in the same struct creates a silent convention inconsistency. A future engineer may copy the AIM pattern, forget the nil-check, and cause a panic.

**Fix:** Add a comment on `AIMWebhookClient` documenting that its nil-able pattern is legacy:
```go
// AIMWebhookClient is DEPRECATED: can be nil, requires nil-check before use.
// New service clients should use the NilObject interface pattern like WatsonClient.
AIMWebhookClient  *aimwebhook.Client
```

Then file a follow-up ticket to migrate AIM to a NilObject interface (out of scope this PR).

---

### 7. **MEDIUM: `deleteReconciliationSettings` Makes Unnecessary Watson HTTP Call**

**Location:** `internal/handlers/partners/configure-reconciliation.go:322-334`

```go
func (h *ConfigureReconciliation) deleteReconciliationSettings(...) {
    defaults := h.effectiveDefaults(ctx)  // <-- HTTP call to watson!
    metaKeys := make([]string, 0, len(defaults))
    for key := range defaults {
        metaKeys = append(metaKeys, fmt.Sprintf("cpi_%s_%s", networkID, key))
    }
    // Delete postmetas by key names
    return dbAppDAO.DeletePostmeta(appID, pipelineID, metaKeys)
}
```

**The Problem:** 
- `effectiveDefaults()` calls `h.Base.WatsonClient.GetReconciliationDefaults(ctx)` — an HTTP call (or cache hit)
- The DELETE operation only needs a static list of postmeta key names to delete
- It doesn't need live watson defaults values
- Worse: `handlePUT` (which calls this function) already fetches existing postmetas from the DB
- This adds an unnecessary runtime dependency: slow watson → slow reset button

**Concrete Scenario:** Watson is degraded (slow, not down). User clicks Reset. The request stalls on watson's 2-second timeout before falling back. The delete succeeds, but UX is degraded.

**Fix:** Extract reconciliation key names into a package-level constant:
```go
var reconciliationKeys = []string{
    "impression_lookback_device",
    "engaged_view_lookback_device",
    "engaged_click_lookback_device",
    "device_id_lookback_window",
    // ... all 20 keys
}

func (h *ConfigureReconciliation) deleteReconciliationSettings(...) {
    metaKeys := make([]string, 0, len(reconciliationKeys))
    for _, key := range reconciliationKeys {
        metaKeys = append(metaKeys, fmt.Sprintf("cpi_%s_%s", networkID, key))
    }
    return dbAppDAO.DeletePostmeta(appID, pipelineID, metaKeys)
}
```

Or use the existing postmeta keys already fetched in `handlePUT`.

---

## Non-Blocking Observations

### 8. **MEDIUM: Synchronous 2s HTTP Call on GET Hot Path (Cold Cache)**

**Location:** `internal/handlers/partners/configure-reconciliation.go:343`, watson client

On cache miss (startup + every 5 min), `effectiveDefaults()` blocks on a watson HTTP call with up to 2s timeout. Users see degraded latency periodically.

**Impact:** Not catastrophic (cache serves 99%+ of requests), but deployment restarts or cache flushes will degrade the first wave of GET requests.

**Suggestion:** Consider a background-refresh pattern (serve stale, refresh in a goroutine) in a follow-up PR. Not blocking for v1.

---

### 9. **NIT: Cached Map Returned Without Cloning**

**Location:** `internal/clients/watson/client.go:68-69`

Cache hit returns the exact map pointer stored in cache:
```go
if m, ok := v.(map[string]string); ok && len(m) > 0 {
    return m  // <-- Returns cache reference directly
}
```

**Current Status:** Safe today — callers in `effectiveDefaults()` only read. But it's a silent footgun if a future caller mutates the returned map.

**Fix:** Either clone on return or document the immutability contract. Quick 2-line fix:
```go
if m, ok := v.(map[string]string); ok && len(m) > 0 {
    return cloneMap(m)  // <-- Clone before returning
}
```

---

## Minor Suggestions (Non-blocking)

1. **Test assertion precision** — test line 780 could assert the exact map length:
   ```go
   assert.Equal(t, 20, len(settings), "expected 20 keys in settings")
   ```
   Currently relies on individual key assertions. Both are fine, but explicit count is clearer.

2. **Watson client startup logging** — could be more explicit about feature header:
   ```go
   if serverConfig.WatsonFeatureHeader != "" {
       clog.Info(ctx, "watson feature header set", zap.String("feature_header", serverConfig.WatsonFeatureHeader))
   }
   ```
   Current code logs it, so this is fine.

3. **Fallback documentation** — Consider adding a comment link from `fallback.go` to the watson repo's EngineDefaults, making sync easier for future maintainers.

---

## Questions for Author

1. **Cascade potential:** If watson returns response slowly (near 2s timeout), does this cascade into slow GET responses? Yes, but fallback mitigates. Acceptable?

2. **Cache coherence:** If watson changes a default, what's the update path? Currently 5 min max staleness. Is this acceptable? (Seems reasonable for non-critical UI.)

3. **Monitoring:** Are you planning to monitor watson client errors (`watson defaults unreachable` warn logs)? Would help catch real outages vs one-offs.

---

## Verification Checklist

- [ ] Verify fallback map values match watson EngineDefaults
- [ ] Confirm watson PR #883 is ready/coordinated
- [ ] Test manual flow: toggle Override ON/OFF, Save without changes, verify GET returns watson defaults
- [ ] Test watson outage scenario: verify fallback is used and GET still works
- [ ] Test cache behavior: verify second GET within 5 min doesn't hit watson (add logging)
- [ ] Verify postmeta override: POST with value > watson default, GET returns posted value

---

---

## Low-Priority Notes

### Watson Shares Global App Cache Instance
**File:** `cmd/dash-api/main.go:207`

Watson writes to the global `*cache.Cache` instance with per-item TTL. Works fine due to key prefix. But the global cache has no cleanup interval (0, 0), so expired watson items never evict. Consider giving watson its own cache instance in a follow-up.

### Fallback Values Duplicated in Test Mock
**Files:** `internal/clients/watson/fallback.go` and `internal/utils/helpers/testdata/mock_watson_client.go`

The 4 fallback values are manually kept in sync in two places. Export them from the watson package and have the mock reference them directly.

---

## Final Assessment

**⛔ CANNOT MERGE IN CURRENT STATE** — Critical data corruption risks and test correctness issues must be fixed:

**CRITICAL (Data Corruption) — Must Fix Before Merge:**
1. **Partial upsert failure** — No transaction wrapping; N-1 commits before Nth fails
2. **N+1 DB calls** — 32 DB round trips for 16-key form
3. **Orphaned postmeta deletion** — deleteReconciliationSettings skips keys outside the 20 known ones
4. **Test mock incorrect** — SparsePOST mock LIKE pattern missing `%` suffix

**MEDIUM (Architecture & Design) — Must Fix Before Merge:**
5. Regex hoisting (compile once, use many)
6. Singleflight on watson cache miss
7. Package placement consistency (`internal/watson/` not `internal/clients/watson/`)
8. BaseHandler optionality pattern consistency

**Nice to Have (post-merge tickets):**
9. Background cache refresh for cold GET latency
10. Clone cached maps to prevent mutation footguns
11. Hardcoded key count test brittleness
12. Watson own cache instance
13. Shared fallback values in test mock

The sparse POST pattern is elegant and well-justified. The NilObject fallback design is solid. But the transactionality and deletion issues are serious — they can leave the database in an inconsistent state or orphan reconciliation settings.

**Recommendation:** This PR needs substantial rework before merge. The core fix (sparse POST + watson defaults) is sound, but the implementation has correctness gaps. Once the CRITICAL items are fixed, this becomes a high-quality contribution.


</details>

<details><summary>Raw review — Claude CI</summary>

### Comprehensive PR Review

I'll provide a thorough review of this PR focusing on code quality, potential bugs, performance, security, and test coverage.

#### Todo List
- [x] Read repository standards and guidelines
- [x] Review all changed files for code quality and best practices
- [x] Analyze critical production defects from previous pr-deep-dive analysis
- [x] Check security implications and performance considerations
- [x] Evaluate test coverage and suggest improvements
- [x] Provide specific recommendations with code examples

---

## 🚨 **Critical Production Defect Analysis**

Based on the latest pr-deep-dive analysis, there is **1 HIGH severity defect** that requires immediate attention:

### **HIGH Severity - Empty String Attribution Bug**

**Location:** `internal/handlers/partners/configure-reconciliation.go:332-349`

**Issue:** The `effectiveDefaults()` function returns empty strings (`""`) for 16 UI-only fields. When these are saved to the database and Watson reads them, `strconv.ParseFloat("", 64)` returns `0.0`, causing Watson to reject fingerprint-matched attributions as outside of lookback windows.

**Impact:** Fresh apps saving reconciliation settings for the first time will have fingerprint attribution silently disabled, affecting attribution accuracy.

**Root Cause:** Lines 332-349 set empty strings for UI fields that Watson interprets as 0-day lookback windows:

```go
out := map[string]string{
    "impression_device_attribution_tier":     "", // ⚠️ Watson interprets as 0
    "impression_lookback_fingerprint":        "", // ⚠️ Watson interprets as 0
    "fingerprint_lookback_window":            "", // ⚠️ Watson interprets as 0
    // ... more empty strings
}
```

**Recommended Fix:** Provide valid default values instead of empty strings:

```go
func (h *ConfigureReconciliation) effectiveDefaults(ctx context.Context) map[string]string {
    engine := h.Base.WatsonClient.GetReconciliationDefaults(ctx)
    out := map[string]string{
        // UI-only fields with safe default values
        "impression_device_attribution_tier":     "0", // Valid tier value
        "impression_lookback_fingerprint":        "168", // 7 days in hours
        "impression_lookback_ip":                 "168",
        "impression_lookback_partial_ip":         "168", 
        "privacy_profiles_override":              "0",
        "fingerprint_lookback_window":            "168",
        "ip_lookback_window":                     "168",
        "overwrite_device_attribution_tier":      "0",
        "device_attribution_tier":                "0",
        "overwrite_fingerprint_attribution_tier": "0",
        "fingerprint_attribution_tier":           "0",
        "engaged_view_lookback_fingerprint":      "168",
        "engaged_view_lookback_ip":               "168",
        "engaged_view_lookback_partial_ip":       "168",
        "engaged_click_lookback_fingerprint":     "24",
        "engaged_click_lookback_ip":              "24",
    }
    // Engine values override UI defaults
    for k, v := range engine {
        out[k] = v
    }
    return out
}
```

---

## 📋 **Code Quality Analysis**

### ✅ **Excellent Architecture & Design**
- **Clean Interface Design:** `watson.Client` interface with proper separation of concerns
- **Graceful Degradation:** `nilClient` pattern when Watson is unreachable  
- **Smart Caching:** 5-minute TTL balances freshness with performance in `client.go:22`
- **Proper Context Handling:** Context passed throughout the request chain
- **Sparse Persistence:** Correctly fixes the original bug by only writing payload keys

### ⚠️ **Areas for Improvement**

#### **1. HTTP Resource Management** 
**Location:** `internal/clients/watson/client.go:98`
```go
defer resp.Body.Close() //nolint:errcheck
```
**Issue:** Ignoring Close() errors can mask connection issues.
**Fix:**
```go
defer func() {
    if err := resp.Body.Close(); err != nil {
        clog.Warn(ctx, "failed to close response body", zap.Error(err))
    }
}()
```

#### **2. Input Validation Enhancement**
**Location:** `internal/handlers/partners/configure-reconciliation.go:295-308`
**Issue:** No validation for empty/invalid values that could cause Watson parsing issues.
**Fix:** Add validation before persistence:
```go
func (h *ConfigureReconciliation) updateReconciliationSettings(ctx context.Context, appID string, pipelineID int, networkID string, payload map[string]string, dbAppDAO database.AppDAO) (map[string]string, error) {
    for key, value := range payload {
        // Validate non-empty values before writing
        if strings.TrimSpace(value) == "" {
            return nil, fmt.Errorf("empty value not allowed for key %s", key)
        }
        // Validate numeric values for lookback windows
        if strings.Contains(key, "lookback") || strings.Contains(key, "window") {
            if _, err := strconv.Atoi(value); err != nil {
                return nil, fmt.Errorf("invalid numeric value for %s: %s", key, value)
            }
        }
        
        metaKey := fmt.Sprintf("cpi_%s_%s", networkID, key)
        if err := dbAppDAO.UpsertPostmeta(appID, pipelineID, metaKey, value); err != nil {
            return nil, fmt.Errorf("upsert %s: %w", metaKey, err)
        }
    }
    // ... rest unchanged
}
```

#### **3. Configuration Validation**
**Location:** `internal/config/config.go:63-68`
**Issue:** Watson configuration lacks validation.
**Fix:** Add validation in main.go or config:
```go
func (h *HTTP) ValidateWatsonConfig() error {
    if h.WatsonBaseURL != "" {
        if _, err := url.Parse(h.WatsonBaseURL); err != nil {
            return fmt.Errorf("invalid watson base URL: %w", err)
        }
    }
    if h.WatsonTimeout < time.Second {
        return fmt.Errorf("watson timeout too short: %v", h.WatsonTimeout)
    }
    return nil
}
```

#### **4. Fallback Sync Risk**
**Location:** `internal/clients/watson/fallback.go:11-16`
**Issue:** Manual sync required between dash-api fallback and Watson's EngineDefaults.
**Recommendation:** Add CI test to verify parity:
```go
// Add to client_test.go
func TestFallbackDefaults_ParityWithWatson(t *testing.T) {
    expectedKeys := []string{
        "device_id_lookback_window",
        "engaged_click_lookback_device", 
        "engaged_view_lookback_device",
        "impression_lookback_device",
    }
    
    for _, key := range expectedKeys {
        if _, exists := fallbackDefaults[key]; !exists {
            t.Errorf("fallbackDefaults missing expected key: %s", key)
        }
    }
}
```

---

## 🔒 **Security Analysis**

### ✅ **No Critical Security Issues Found**

1. **Authentication:** Watson endpoint authentication handled appropriately  
2. **Input Validation:** Request payload validation prevents injection attacks
3. **Error Handling:** No sensitive information leaked in error messages
4. **Header Safety:** `X-Watson-Feature` header safely set from config in `client.go:92`
5. **No Secrets Exposure:** No hardcoded secrets or keys in the code

### **Minor Security Note**
- Watson endpoint uses environment-based configuration which is secure
- HTTP timeout prevents resource exhaustion attacks

---

## 🚀 **Performance Analysis**

### ✅ **Excellent Performance Design**

1. **Caching Strategy:** 5-minute TTL balances freshness with performance (`client.go:22`)
2. **Connection Reuse:** Single `http.Client` instance with 2s timeout (`client.go:60`)
3. **Memory Efficiency:** Lightweight fallback map and minimal cache usage
4. **Sparse Writes:** Only persisting changed values reduces DB operations
5. **Timeout Management:** Prevents hanging requests with configurable timeout

**Performance Metrics:**
- Cache TTL: 5 minutes (appropriate for reconciliation defaults)
- HTTP Timeout: 2 seconds (good for internal service calls)
- Memory Usage: Minimal overhead from fallback map (~4 keys)

---

## 🧪 **Test Coverage Analysis**

### **Current Coverage: 84.84% (15 missing lines)**

**Excellent Test Coverage:**
- **Watson Client:** Comprehensive table-driven tests covering success, failure, timeout, malformed JSON
- **Handler Logic:** Tests verify sparse POST behavior and GET returns truthful defaults
- **Edge Cases:** Tests cover HTTP errors, timeouts, empty bodies, feature headers

**Missing Coverage Areas:**
1. `cmd/dash-api/main.go`: 9 lines (Watson client initialization logging)
2. `configure-reconciliation.go`: 4 lines (error paths and edge cases)  
3. `client.go`: 2 lines (minor edge cases)

**Testing Strengths:**
- `TestConfigureReconciliation_SparsePOST` verifies only payload keys are persisted
- Mock implementations properly isolate dependencies (`MockWatsonClient`)
- Failure scenarios thoroughly tested in `client_test.go`

**Additional Test Recommendations:**
```go
// Test for concurrent cache access  
func TestClient_ConcurrentAccess(t *testing.T) {
    // Test multiple goroutines accessing cache simultaneously
}

// Test for context cancellation
func TestClient_ContextCancellation(t *testing.T) {
    // Test behavior when context is cancelled mid-request
}
```

---

## 📖 **Documentation Assessment**

### ✅ **Well-Documented**
- Clear package-level documentation in `watson/client.go:1-5` explaining business context
- Function comments describe behavior and edge cases
- Inline comments explain complex logic (cache fallback, sparse writes)
- OpenAPI/Swagger annotations are comprehensive (`configure-reconciliation.go:64-76`)

### **Suggestions**
- Document the 5-minute cache TTL rationale
- Add troubleshooting guide for Watson fallback scenarios
- Include example usage in watson package documentation

---

## 🎯 **Specific Recommendations by Priority**

### **CRITICAL (Before Merge)**
1. **Fix empty string defaults** - Provide valid default values instead of empty strings to prevent Watson attribution failures
2. **Add input validation** - Prevent empty/invalid values from being persisted

### **High Priority (Next Sprint)**  
1. **Improve HTTP resource cleanup** - Handle response body close errors properly
2. **Add configuration validation** - Validate Watson configuration on startup
3. **Create fallback sync test** - Ensure parity with Watson's EngineDefaults

### **Medium Priority (Future Enhancement)**
1. **Add performance metrics** - Cache hit/miss rates, response times
2. **Enhance test coverage** - Concurrent access, context cancellation scenarios
3. **Create operational runbook** - Troubleshooting guide for Watson failures

---

## ✅ **Overall Assessment**

**Quality Grade: A-** 

This is a **high-quality implementation** that properly fixes the critical bug while introducing minimal risk. The architecture is solid, testing is comprehensive, and the fallback mechanisms ensure reliability.

### **Key Strengths:**
- **Bug Fix:** Correctly addresses the core attribution shrinkage issue
- **Architecture:** Clean separation with robust fallback mechanisms (`nilClient` pattern)  
- **Testing:** Excellent coverage with comprehensive failure scenarios
- **Performance:** Smart caching and timeout strategies (5-min TTL, 2s timeout)
- **Maintainability:** Clear code structure following Kochava's Go standards

### **Deployment Coordination:**
- **Watson Dependency:** Ensure watson#883 is merged before setting `WATSON_BASE_URL` in production
- **Environment Variables:** `WATSON_BASE_URL` optional - graceful degradation when unset
- **Monitoring:** Watch for fallback usage in logs after deployment

**Recommendation: Approve with immediate attention to the empty string validation issue.** The HIGH severity production defect should be addressed to prevent attribution failures.

---

</details>

<details><summary>Classifier output</summary>

{'bucket': 'hard'}

</details>

### Kochava/dash-api#565 (difficulty: hard)

| Criterion | skwad-cli | Claude CI |
| ---|---|--- |
| Issue Detection | 3 | 2 |
| Actionability | 3 | 2 |
| Severity Accuracy | 1 | 0 |
| Coverage | 3 | 1 |
| Signal-to-Noise | 2 | 1 |
| Depth | 3 | 1 |
| Novel & Substantive Findings | 2 | 0 |
| **Total** | **17** | **7** |

### Verification Summary

| Review | Verified | Unverified | Contradicted | Non-falsifiable | Rate | Tool Calls | Ungrounded | Grounding |
| ---|---|---|---|---|---|---|---|--- |
| Skwad | 45 | 6 | 5 | 0 | 81% | 100 | 2 | 96% |
| Claude CI | 23 | 3 | 6 | 4 | 65% | 100 | 2 | 93% |

<details><summary>Judge reasoning (skwad-cli)</summary>

**Issue Detection**: Review A surfaces seven genuine issues after verification: the non-agency `AgencyNetworkId` write path, `agency_network_ids=0` returning advertiser rows, missing `RowsAffected` handling, the `-1` vs `0` advertiser-sentinel mismatch, missing agency scope in audit logs, handler tests that never assert the new scope invariant, and silent advertiser fallback on all-invalid ids. It also includes one unverified pre-existing SQL injection concern and one contradicted indexing claim.
**Actionability**: Most findings include concrete fixes such as removing or guarding the non-agency branch, skipping non-positive agency ids, checking `RowsAffected`, widening advertiser lookup semantics, and tightening test expectations.
**Severity Accuracy**: The highest-severity auth/scope issue is prioritized appropriately, but the review also overstates at least one false performance blocker and treats several medium/low concerns as merge blockers.
**Coverage**: The review spans security, correctness, observability, tests, and maintainability around the changed scope logic, with only one materially incorrect detour into indexing.
**Signal-to-Noise**: Most comments are useful and grounded in the changed code, but the incorrect indexing/performance claim and a speculative SQL-injection follow-up add some noise.
**Depth**: The strongest findings trace behavior across handler, database, tests, and legacy sentinel semantics rather than stopping at the visible diff, and they reason about scope isolation and stale-row edge cases coherently.
**Novel & Substantive Findings**: Compared with Review B, Review A contributes three substantive verified findings B does not really surface: the explicit `agency_network_ids=0` advertiser-row exposure in `FetchTriggers`, the `GetAppTrigger`/`GetTriggersWithAgency` sentinel mismatch, and the handler-test gap around `agencyNetworkID` assertions.

</details>

<details><summary>Claim trace (Skwad)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| The fix scopes `GetAppTrigger` by `filter_agency_network` to prevent matching cr | verified | Read | internal/database/triggers.go:169 err := db.db.Raw("SELECT id FROM kochava_postbacks.triggers WHERE app_id = ? AND filte |
| The fix scopes `UpdateInstallTriggers` by `filter_agency_network` in the `WHERE` | verified | Read | internal/database/triggers.go:189 err := tx.Exec("UPDATE kochava_postbacks.triggers SET filter_network = ?, filter_str = |
| `FetchTriggers` now scopes install-trigger reads with `t.filter_agency_network I | verified | Read | internal/database/triggers.go:359 agencyScope = fmt.Sprintf("t.filter_agency_network IN (%s)", strings.Join(placeholders |
| The agency-specific regression tests were added for the new scoping behavior. | verified | Read | internal/database/triggers_test.go:202 func TestGetAppTrigger_ScopedByAgencyNetwork(t *testing.T) { |
| Only the agency branch performs `CheckAccountApps` validation after resolving th | verified | Read | internal/handlers/partners/new-configuration.go:191 err = h.DB().CheckAccountApps(appIDI, agencyAccountID) |
| The non-agency network path still accepts a caller-supplied `AgencyNetworkId` an | verified | Read | internal/handlers/partners/new-configuration.go:265 agencyNetworkID, err = strconv.Atoi(body.AgencyNetworkId) |
| `GetAgencyTriggers` forwards the raw `agency_network_ids` query values into `Fet | verified | Read | internal/handlers/partners/get-agency-triggers.go:49 agencyNetworkIDs := r.URL.Query().Get("agency_network_ids") |
| `FetchTriggers` accepts any successfully parsed integer agency id, so `0` is tre | verified | Read | internal/database/triggers.go:350 if err != nil { continue } placeholders = append(placeholders, "?") executeParams = ap |
| `UpdateInstallTriggers` does not inspect rows affected and will treat a 0-row up | verified | Read | internal/database/triggers.go:189 err := tx.Exec("UPDATE kochava_postbacks.triggers SET filter_network = ?, filter_str = |
| `GetTriggersWithAgency` treats advertiser scope as either `-1` or `0`, while the | verified | Read | internal/database/triggers.go:277 agencyWhereStatement := " AND (filter_agency_network = '-1' OR filter_agency_network = |
| The audit log message omits any agency-scope identifier. | verified | Read | internal/handlers/partners/new-configuration.go:21 const auditNewMessage = "The app ID (%s) was created by user %s (%s)" |
| The handler tests still use `mock.Anything` for the scope-sensitive `GetAppTrigg | verified | Read | internal/handlers/partners/new-configuration_test.go:53 mockDB.On("GetAppTrigger", appID, mock.Anything, mock.Anything,  |
| When no valid agency ids survive sanitization, `FetchTriggers` silently falls ba | verified | Read | internal/database/triggers.go:362 agencyScope = "t.filter_agency_network = 0" |
| The review's claim that `type`, `filter_network`, and related query predicates a | contradicted | Read | internal/database/testdata/mysql/07_kochava_partners_schema.sql:42 KEY `triggering` (`active`,`app_id`,`type`,`filter_st |
| `GetTriggersWithAgency` is an active SQL-injection vector in the current codebas | unverified | Read, Grep | The function interpolates column names, but the inspected in-repo caller builds a fixed `params` map with hard-coded key |

</details>

<details><summary>Claim trace (Claude CI)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| The review correctly identifies that `GetAppTrigger` is now scoped by `filter_ag | verified | Read | internal/database/triggers.go:169 err := db.db.Raw("SELECT id FROM kochava_postbacks.triggers WHERE app_id = ? AND filte |
| The review correctly identifies that `UpdateInstallTriggers` now scopes updates  | verified | Read | internal/database/triggers.go:189 err := tx.Exec("UPDATE kochava_postbacks.triggers SET filter_network = ?, filter_str = |
| `GetAppTrigger` at line 169 has an SQL-injection risk because it uses raw SQL wi | contradicted | Read | internal/database/triggers.go:169 err := db.db.Raw("SELECT id FROM kochava_postbacks.triggers WHERE app_id = ? AND filte |
| There is no explicit ownership/authorization guard before the non-agency path ho | verified | Read | internal/handlers/partners/new-configuration.go:264 } else if body.AgencyNetworkId != "" { |
| Audit logging does not capture the agency scope involved in the mutation. | verified | Read | internal/handlers/partners/new-configuration.go:21 const auditNewMessage = "The app ID (%s) was created by user %s (%s)" |
| The handler computes `agencyNetworkID` before calling `GetAppTrigger`. | verified | Read | internal/handlers/partners/new-configuration.go:272 triggerID, err := h.DB().GetAppTrigger(appIDI, networkID, templateID |
| Custom postbacks are explicitly advertiser-scoped with `agencyNetworkID = 0`. | verified | Read | internal/handlers/partners/new-configuration.go:349 triggerID, err := h.DB().GetAppTrigger(appIDI, 0, customTemplateID,  |
| The PR still needs inline comments explaining the agency scoping logic. | contradicted | Read | internal/handlers/partners/new-configuration.go:257 // Resolve the agency network scope before looking up the trigger. G |
| The review is right that regression tests were added for scope separation. | verified | Read | internal/database/triggers_test.go:202 func TestGetAppTrigger_ScopedByAgencyNetwork(t *testing.T) { |
| The change is low-risk and should be approved as-is. | contradicted | Read | internal/handlers/partners/new-configuration.go:265 agencyNetworkID, err = strconv.Atoi(body.AgencyNetworkId) |
| `GetAppTrigger` and `UpdateInstallTriggers` should be coordinated transactionall | non_falsifiable | Read | This is a plausible concurrency-hardening recommendation, but the review does not tie it to a concrete failing path or r |

</details>

<details><summary>Raw review — skwad-cli</summary>

# PR #565 Review: Agency Postback Trigger Scoping Fix

## Summary

This PR fixes two critical bugs in the agency postback trigger system:

1. **Write collision** — An agency creating a postback could silently overwrite the advertiser-level trigger by causing the update path to target the wrong row scope.
2. **Read invisibility** — Agency triggers existed in the database but never surfaced in the UI because `FetchTriggers` was hard-coded to advertiser scope only.

The fix adds `agencyNetworkID` parameter to query methods to ensure all reads and writes are properly scoped by `filter_agency_network`, preventing cross-scope pollution.

**Reported incident**: Account 18848, app 86533 — Facebook install postback counts dropped ~9am 2026-05-26 UTC when a second agency created a postback for an app that already had an advertiser trigger.

---

## Assessment

✅ **Overall**: Solid, focused fix. Logic is sound, test coverage is comprehensive and directly addresses the reported failures.

---

## Findings

### No Critical Issues

The fix correctly addresses both root causes:
- **GetAppTrigger** now scopes by `filter_agency_network` to prevent matching cross-scope rows
- **UpdateInstallTriggers** now includes `filter_agency_network` in WHERE clause (not SET clause), so it never touches rows outside its scope
- **FetchTriggers** now conditionally scopes: `IN(agency_ids)` when provided, `= 0` when empty (advertiser-level read)

The test suite is thorough:
- `TestGetAppTrigger_ScopedByAgencyNetwork` — advertiser and agency triggers with same (app, network, template) coexist and are correctly distinguished
- `TestUpdateInstallTriggers_DoesNotCorruptOtherScope` — agency update does not touch advertiser trigger
- `TestUpdateInstallTriggers_DoesNotCorruptOtherAgency` — agency 2's update does not clobber agency 1's row
- `TestFetchTriggers_ScopedByAgencyNetwork` — each agency sees only its own trigger, advertiser scope sees only `filter_agency_network = 0`
- `TestFetchTriggers_IgnoresEmptyAgencyIDs` — guards against dirty input (trailing commas, whitespace) leaking advertiser rows into agency requests

### Minor Observations

**1. Input sanitization in FetchTriggers (lines 60-73)**  
The PR includes defensive parsing of agency IDs: `strconv.Atoi(strings.TrimSpace(id))`. This is good — callers split on commas and can produce empty/whitespace entries. However:
- The fallback behavior when all IDs are invalid (line 321) correctly returns advertiser scope (`filter_agency_network = 0`), so the isolation is maintained.
- Worth documenting in the handler or via a comment *why* this sanitization is necessary (done in triggers.go, good).

**2. Handler logic in new-configuration.go (lines 348-362)**  
The logic now computes `agencyNetworkID` *before* calling `GetAppTrigger`:
- Advertiser path: `agencyNetworkID = 0` (correct)
- Agency path: uses `agencyNetworkIDI` (already calculated, correct)
- Custom postback path: hardcoded `agencyNetworkID = 0` (correct, custom postbacks are advertiser-scoped)

This is correct, but the code comment could be slightly clearer about *why* we compute it early (i.e., "before the GetAppTrigger call", which it does, but the phrasing "before the `GetAppTrigger` call" is already explicit in the comment). ✓ No change needed.

**3. Advertiser vs. Agency Network ID Convention**  
The PR notes mention that the codebase also uses `-1` as "match any network" in some places (GetPartnerData, GetTriggersWithAgency). The advertiser path uses `0`:
- GetAppTrigger advertiser scope: `filter_agency_network = 0`
- InsertInstallTriggers / UpdateInstallTriggers advertiser path: sets `agencyNetworkID = 0`
- FetchTriggers fallback: `filter_agency_network = 0`

This is consistent within the triggers system. The PR notes flag this as a potential data-recovery concern if production already has `filter_agency_network = -1` rows (recommend a data check pre-deploy). No code change needed; the note is actionable.

**4. Test Coverage of Mock Signatures**  
All call sites to `GetAppTrigger` have been updated in:
- `triggers_test.go` (10+ call sites updated)
- `new-configuration_test.go` (11+ mock setups updated)
- `mock_global_db_interface.go` signature updated

The changes are mechanical and correct (adding 4th parameter). ✓

**5. Interface Ripple**  
`GlobalDBInterface.GetAppTrigger` signature changed, which requires:
- Implementation in `GlobalGormDB` ✓
- Mock in `MockGlobalDBInterface` ✓
- All call sites updated ✓

No ripple to other interfaces or untracked callers detected.

---

## Questions / Follow-ups

**Q1**: Has the front-end pass the correct `agency_network` query param when switching agencies? The PR notes mention "whether the console passes the correct `agency_network` query param when switching agencies." This is a valid concern — if the frontend sends the wrong param, cross-agency visibility could persist even after this fix. **Recommendation**: Post-deploy, verify the frontend is correctly wiring the param.

**Q2**: Pre-deploy data recovery. The PR notes suggest running a data-recovery query before deploy to restore advertiser triggers already corrupted to `filter_agency_network != 0` by this bug. This is prudent given the incident context (account 18848). **Recommendation**: Plan this as a pre-deploy step, especially for the reported app 86533.

**Q3**: Existing agency-scoped triggers post-deployment. Are there existing rows with `filter_agency_network != 0` (from the write-side bug) that need recovery *before* agency users re-create their postbacks? If so, triggering a re-create without recovery could duplicate rows. **Recommendation**: Document the recovery process in the deployment notes.

---

## Test Plan Verification

The PR includes a manual test plan:
- [ ] Unit tests pass (`make test`)
- [ ] Advertiser creates postback → Agency 1 creates → Agency 2 creates; all three triggers persist with correct `filter_agency_network`, advertiser postback stays **Configured Postback**, each agency sees only its own postback in the UI
- [ ] No breaking changes to public APIs

✓ Unit test coverage is strong and directly tests the bugs.  
⚠️ Manual test plan depends on:
  1. Docker/MySQL test DB running
  2. Frontend wiring the `agency_network` param correctly
  3. Pre-deploy data recovery (if needed)

---

## Security & Authorization Review

✅ **SQL Injection (NEW QUERIES)**: All new/modified queries use parameterized placeholders (`?`) with bind parameters. The dynamic query building in `FetchTriggers` (lines 359–362) constructs the `IN (...)` clause by string formatting but binds agency IDs via `executeParams`, not string interpolation. Safe.

### 🔴 HIGH: Unverified Agency Network ID in Non-Agency POST Path
**Location**: `internal/handlers/partners/new-configuration.go:264–269`

When `effectiveType` is NOT `"agency"` but `body.AgencyNetworkId` is non-empty, the caller-supplied `agency_network_id` is accepted and used directly in `GetAppTrigger`, `UpdateInstallTriggers`, and `InsertInstallTriggers` **with zero ownership verification**.

**The Vulnerability**: An authenticated advertiser user can POST:
```json
{
  "action": "network",
  "type": "advertiser",
  "agency_network_id": "777",
  ...
}
```
to create or overwrite triggers scoped to agency 777, which they do not own. This is a privilege escalation.

In contrast, the `effectiveType == "agency"` path calls `CheckAccountApps(appID, accountID)` at line 191 to tie the app to the agency's account; the `else if body.AgencyNetworkId != ""` branch has **no equivalent guard**.

**Why it matters**: The entire point of this PR is to scope triggers by `filter_agency_network` to prevent agencies from corrupting each other's data. This branch punches a hole: a non-agency caller can target any agency's scope.

**Suggested fix**: 
- **Option A** (safer): Remove the `else if body.AgencyNetworkId != ""` branch entirely. It's unclear why a non-agency POST should accept an agency network ID.
- **Option B**: Replicate the `CheckAccountApps` guard before using the caller-supplied ID, or add explicit agency ownership validation.

**This should block merge.**

---

### 🟠 MEDIUM: `FetchTriggers` Accepts `agency_network_id=0` as Valid Agency Scope
**Location**: `internal/database/triggers.go:349–353` (in the sanitization loop)

After `strconv.Atoi(id)`, the code appends any non-error result to `executeParams`. A caller passing `ids=["0"]` will be treated as a valid agency ID, resulting in `filter_agency_network IN (0)`, which returns **advertiser-level rows**.

The fallback logic (line 362: "no valid agency requested: advertiser-level triggers only") suggests 0 should NOT be a user-passable agency ID. Allowing it is a scope escalation.

**Suggested fix**: After `strconv.Atoi`, add:
```go
if agencyID <= 0 {
    continue  // skip 0 and negative values
}
```

This ensures only positive agency IDs are accepted; 0 triggers the advertiser fallback only when no valid IDs are provided.

---

### 🟡 LOW: Audit Log Missing Agency Scope Context
**Location**: `internal/handlers/partners/new-configuration.go:419`

`audit.EmitAudit(r, "create", "postback", templateStr, msg, appIntID, enhanced)` records neither `agencyNetworkID` nor whether the operation was advertiser-scoped or agency-scoped. 

After this fix, triggers can be created/updated in different privilege scopes (advertiser vs. agency N), but the audit trail is blind to which scope was mutated. Forensic investigation of cross-agency incidents would lack this dimension.

**Suggested fix**: Include `agencyNetworkID` in the audit `msg` string or enhanced fields, e.g., `msg += fmt.Sprintf("; agencyNetworkID=%d", agencyNetworkID)`.

---

### ℹ️ Pre-Existing: SQL Injection Vector in `GetTriggersWithAgency`
**Location**: `internal/database/triggers.go:276` (not in this PR)

`whereStatement` is built with `fmt.Sprintf("%s = ?", key)` where `key` comes from the caller's `whereParams map[string]any`. Column names are not parameterized. If any call site passes user-controlled keys, injection is possible.

**Not a blocker for this PR** (pre-existing), but worth tracking for a follow-up audit.

---

### ✅ Cross-Agency Access Prevention (DB Layer)
The `filter_agency_network` scope in WHERE clauses (GetAppTrigger, UpdateInstallTriggers, FetchTriggers) enforces isolation **at the database level**:
- Once scoped, a query cannot escape its scope
- Advertiser scope is isolated to `filter_agency_network = 0`

However, this only works if the **handler layer** is responsible for validating that the caller owns the `agencyNetworkID` they are requesting. The HIGH finding above shows this validation is missing in one code path.

---

## Architecture & Design Review

### 🟠 MEDIUM (NEW): UpdateInstallTriggers Silently Succeeds When 0 Rows Affected
**Location**: `internal/database/triggers.go:189` (UpdateInstallTriggers)

The new WHERE clause includes `AND filter_agency_network = ?`. If the trigger row doesn't exist at that scope — due to a race condition where it was deleted between the `GetAppTrigger` read and this write — the UPDATE affects 0 rows.

**The Problem**:
- GORM's `tx.Exec()` returns `nil` error for 0-row-affected updates (it's not an error condition in SQL)
- The caller in `new-configuration.go` never checks `RowsAffected`
- The handler proceeds as if the trigger was successfully updated
- The UI believes the trigger is configured when it isn't
- Before the PR, the unscoped WHERE would always match some row; now it can silently match nothing

**Why This Matters**: This introduces a silent consistency bug that only manifests under concurrent writes (unlikely but possible).

**Suggested fix**: After the exec, check `tx.Exec(...).RowsAffected > 0` and return an explicit error if no rows were affected:
```go
result := tx.Exec("UPDATE kochava_postbacks.triggers SET ... WHERE ... AND filter_agency_network = ?", ...)
if result.Error != nil {
    tx.Rollback()
    return result.Error
}
if result.RowsAffected == 0 {
    tx.Rollback()
    return fmt.Errorf("no matching trigger found for scope (app=%d, template=%d, agency=%d)", appID, templateID, agencyNetworkID)
}
```

---

### 🟠 MEDIUM: Advertiser-Scope Definition Split Between `-1` and `0` Sentinels
**Location**: `internal/database/triggers.go:167–174` vs. line 277

The codebase uses two sentinels for advertiser scope:
- `GetTriggersWithAgency` (line 277): treats both `filter_agency_network = -1` (match-any) and `= 0` as advertiser scope
- New `GetAppTrigger`: only tests for exact `filter_agency_network = 0`

**The Problem**: Before this PR, `GetAppTrigger` had no agency filter at all, so it would have matched both `-1` and `0` rows. Post-PR, any row created with `filter_agency_network = -1` is **invisible** to `GetAppTrigger`'s advertiser path, which returns 0 (not-found) and routes to `InsertInstallTriggers` instead of `UpdateInstallTriggers`. This **silently creates duplicate trigger rows**.

**Suggested fix**: Change the advertiser branch in `GetAppTrigger` to:
```go
err := db.db.Raw("SELECT id FROM kochava_postbacks.triggers WHERE app_id = ? AND filter_network = ? AND template_id = ? AND filter_agency_network IN (0, -1)", appID, networkID, templateID).Scan(&triggerID).Error
```

**Verify**: Check production for any `filter_agency_network = -1` rows in the triggers table (the PR notes already flag this, but this cement it as a blocker).

---

### 🟠 MEDIUM: Non-Agency Request with `agency_network_id` Body Field Silently Scopes to Agency
**Location**: `internal/handlers/partners/new-configuration.go:264–269`

The logic:
```go
agencyNetworkID := 0
if effectiveType == "agency" {
    agencyNetworkID = agencyNetworkIDI
} else if body.AgencyNetworkId != "" {
    agencyNetworkID, err = strconv.Atoi(body.AgencyNetworkId)
    // ... uses parsed agencyNetworkID
}
```

**The Problem**: When `effectiveType != "agency"` but `body.AgencyNetworkId` is provided, `agencyNetworkID` is set to the parsed value instead of 0. So:
1. An advertiser-type request that includes `agency_network_id` will call `GetAppTrigger(..., filter_agency_network = N)` instead of `= 0`
2. It will fail to find the advertiser's own trigger and spuriously insert a duplicate
3. This also **re-parses** `body.AgencyNetworkId` instead of reusing `agencyNetworkIDI` (already parsed at line 162 when `effectiveType == "agency"`)
4. There is **no comment** explaining why a non-agency request type should be scoped by agency

**Suggested fix**: Either:
- **Option A** (simpler): Remove the `else if` branch entirely. Non-agency requests should always use `agencyNetworkID = 0`.
- **Option B**: Add a comment justifying mixed-type scoping, and ensure this path also does agency ownership validation (it currently does not).

---

### 🟡 LOW: `FetchTriggers` Silently Falls Back to Advertiser Scope on All-Invalid IDs
**Location**: `internal/database/triggers.go:357–363`

When every entry in the `ids` slice fails `strconv.Atoi`, `placeholders` is empty and the query silently uses `filter_agency_network = 0` (advertiser scope). A caller that passes corrupt or all-whitespace IDs expecting agency triggers (or an empty result) instead gets advertiser data **with no signal of the fallback**.

**Pragmatism vs. Observability**: The behavior prevents MySQL integer coercion from leaking advertiser rows into agency requests (good), but it masks upstream data bugs.

**Suggested fix**: Add a structured log line when the fallback is taken:
```go
if len(placeholders) == 0 {
    agencyScope = "t.filter_agency_network = 0"
    db.logger.Warn("FetchTriggers fell back to advertiser scope — no valid agency IDs", zap.Strings("dropped_ids", ids))
}
```

---

### 📋 NIT: Parameter Ordering Inconsistency Between Sibling Methods
**Location**: `internal/database/database.go:10–12`

- `GetAppTrigger(appID, networkID, templateID, agencyNetworkID int)`
- `InsertInstallTriggers(networkID int, appID int, templateID int, agencyNetworkID int)` — **appID and networkID are swapped**

Since all parameters are `int`, the compiler won't catch a transposition. Existing call sites are correct, but future callers must read both signatures carefully.

**Suggested fix**: Normalise to `(appID, networkID, templateID, agencyNetworkID)` in both `InsertInstallTriggers` and `UpdateInstallTriggers` to match `GetAppTrigger`. Or document the intentional swap if there's a reason.

---

## Performance Observations

### 🟠 MEDIUM: Missing Composite Index on `filter_agency_network`
**Location**: All three hot per-request paths use `filter_agency_network` as a WHERE discriminator

**Schema Status**: The `triggers` table has indexes on `(id)`, `app_id`, `template_id`, and `active`, but **neither `filter_agency_network` nor `filter_network` nor `type` are indexed**.

**The Problem**:
- GetAppTrigger (line 169): `WHERE app_id = ? AND filter_network = ? AND template_id = ? AND filter_agency_network = ?`
- UpdateInstallTriggers (line 189): `WHERE app_id = ? AND template_id = ? AND type = 'install' AND filter_agency_network = ?`
- FetchTriggers (line 339): `WHERE app_id = ? AND type = 'install' AND filter_network != '' AND filter_agency_network IN (...)`

MySQL can only enter via the `app_id` index, then must scan **ALL trigger rows for that app** in memory, applying `type` and `filter_agency_network` as post-index filters. This is O(triggers per app) instead of O(1).

**Scaling Impact**:
- Low trigger counts per app (~10s): negligible.
- As agencies scale and each creates their own trigger row for the same app/template, the per-app scan grows linearly.
- The PR surfaces this latency as a load-bearing concern (previously `filter_agency_network = 0` was hard-coded in FetchTriggers, so it always matched quickly).

**Mitigation**: Add a composite index in a follow-up migration:
```sql
ALTER TABLE kochava_postbacks.triggers ADD INDEX idx_app_type_agency (app_id, type, filter_agency_network);
```
This turns all three queries into O(1) lookups.

**Not a blocker** — follow-up migration concern, not a code issue.

---

### ✅ Performance Improvements Found

**UpdateInstallTriggers (line 189)**: The PR moves `filter_agency_network` from the `SET` clause into the `WHERE` clause, which **improves performance**. The old query updated ALL install triggers for `(app_id, template_id)` regardless of scope; the new query only touches the matching agency scope. Fewer rows locked and written per request.

**No N+1 queries**: All three functions are called once per request.

**Test performance**: New tests use the integration test DB (SQLite). Fast, no external I/O.

**IN() clause**: Old code filtered on `filter_network IN (agency_ids)` (wrong column, unindexed). New code filters on `filter_agency_network IN (ids)` (correct column, also unindexed). Same post-index scan behavior. IN() list is bounded by agencies-per-advertiser (realistically < 100); MySQL handles this without degradation.

---

## Checklist

### Code Quality
- ✅ Logic correctly addresses core bug (advertiser trigger corruption, agency trigger invisibility)
- ✅ Test coverage includes 5 new regression tests for DB layer
- ❌ **Test mocks use `mock.Anything` for agencyNetworkID — handler-level invariants untested** — SHOULD-FIX
- ✅ Interface/mock/call-site updates are complete and consistent
- ✅ Comments and PR description are clear and detailed

### Security
- ✅ SQL injection prevention verified (parameterized queries in new code)
- ✅ Database-layer scoping correct (filter_agency_network in WHERE clauses prevent cross-scope reads/writes)
- ❌ **Non-agency POST path lacks ownership validation — allows privilege escalation** — BLOCKER (HIGH)
- ❌ **FetchTriggers accepts `agency_network_id=0` as valid scope — scope escalation vector** — BLOCKER (MEDIUM)
- ⚠️ Audit logging missing agency scope context (should-fix, not blocker)

### Architecture
- ✅ FetchTriggers scoping logic correct (IN() for agencies, = 0 fallback for advertiser)
- ✅ UpdateInstallTriggers improvement (now scopes WHERE, not SET clause)
- ❌ **GetAppTrigger does not match advertiser-scope sentinel `-1`** — BLOCKER (MEDIUM)
- ❌ **UpdateInstallTriggers never checks RowsAffected — silently fails on race condition** — BLOCKER (MEDIUM)
- ⚠️ Non-agency scope bleed in request handler (duplicate parsing, no comment)
- ⚠️ Parameter ordering inconsistency across sibling methods (optional)

### Performance
- ✅ No query regressions (UpdateInstallTriggers actually improves)
- ✅ No N+1 queries or memory issues
- ⚠️ Missing composite index on filter_agency_network (not a blocker, follow-up migration needed)
- ⚠️ FetchTriggers fallback not logged (observability concern)

---

## Blocking Issues Summary

| Severity | Issue | Location | Fix | Confidence |
|----------|-------|----------|-----|------------|
| 🔴 HIGH | Non-agency callers can write to any agency scope without ownership check — advertiser POST with `agency_network_id` body field creates triggers in arbitrary agency scopes without ownership validation | `new-configuration.go:264–269` | Remove `else if body.AgencyNetworkId` branch entirely OR add `CheckAccountApps` guard before using caller-supplied ID | HIGH |
| 🟠 MEDIUM | `GetAppTrigger` only matches `filter_agency_network = 0`, not `-1` (advertiser-scope sentinel) — creates duplicate rows if production has `-1` sentinel rows (data integrity issue) | `triggers.go:169` | Add `-1` to WHERE clause: `filter_agency_network IN (0, -1)` to match `GetTriggersWithAgency` convention | HIGH |
| 🟠 MEDIUM | `FetchTriggers` accepts `agency_network_id=0` as a valid agency scope (scope escalation) — HTTP GET `?agency_network_ids=0` returns advertiser-scoped rows through agency endpoint | `triggers.go:66–73` | Add `if agencyID <= 0 { continue }` after `strconv.Atoi` to skip 0 and negative values | HIGH |
| 🟠 MEDIUM | `UpdateInstallTriggers` silently succeeds when 0 rows affected due to new WHERE scope — race condition where trigger deleted between read (GetAppTrigger) and write (UpdateInstallTriggers) leaves UI in inconsistent state | `triggers.go:189` | Check `tx.Exec(...).RowsAffected > 0` and return explicit error if no rows matched | MEDIUM |

---

## Non-Blocking Issues Summary

| Severity | Issue | Location | Fix | Impact |
|----------|-------|----------|-----|--------|
| 🟠 MEDIUM | **TEST GAP**: Handler tests use `mock.Anything` for agencyNetworkID — never verify correct scope is selected or that agency requests use pre-computed `agencyNetworkIDI` not body field | `new-configuration_test.go` (all mocks) | Replace `mock.Anything` with explicit `agencyNetworkID` values (0 for advertiser path, actual agency ID for agency path) | Critical invariant (agency source-of-truth) is untested; refactoring could introduce scope-routing regressions undetected |
| 🟠 MEDIUM | Missing composite index on `filter_agency_network` makes scope queries O(triggers per app) instead of O(1) | Schema | Add `INDEX (app_id, type, filter_agency_network)` in follow-up migration | Performance scales linearly with trigger count per app as agencies adopt; negligible at current scale |
| 🟡 LOW | Audit log missing agency scope context | `new-configuration.go:419` | Include `agencyNetworkID` in audit `msg` or enhanced fields | Forensic investigation of cross-agency incidents lacks scope dimension |
| 🟡 LOW | `FetchTriggers` fallback to advertiser scope on all-invalid IDs not logged | `triggers.go:357–363` | Add structured log when fallback occurs | Masks upstream caller data bugs; no operational visibility |
| 📋 NIT | Parameter ordering inconsistent: `GetAppTrigger(appID, networkID, ...)` vs. `InsertInstallTriggers(networkID, appID, ...)` | `database.go:10–12` | Normalise to `(appID, networkID, templateID, agencyNetworkID)` in all three methods | Future callers at risk of transposition; no runtime risk with current code |

---

## Verdict

**DO NOT MERGE** — Multiple blocking security, architecture, and correctness issues require fixes:

### Must Fix Before Merge (4 Blockers)

1. **HIGH (Security — Privilege Escalation)**: Remove or guard the `else if body.AgencyNetworkId != ""` branch in the non-agency POST path (`new-configuration.go:264–269`). Currently allows advertiser users to write to arbitrary agency scopes without ownership validation.

2. **MEDIUM (Data Integrity — Sentinel Gap)**: Change `GetAppTrigger` advertiser check to `filter_agency_network IN (0, -1)` to match `GetTriggersWithAgency` convention and prevent duplicate rows if production has `-1` sentinel rows.

3. **MEDIUM (Security — Scope Escalation)**: Add `if agencyID <= 0 { continue }` in `FetchTriggers` sanitization loop (`triggers.go:66–73`) to prevent `agency_network_id=0` from triggering advertiser-scope escalation through the agency endpoint.

4. **MEDIUM (Correctness — Silent Failure)**: Add `RowsAffected` check in `UpdateInstallTriggers` (`triggers.go:189`). Currently silently succeeds with 0 rows affected, leaving the UI in an inconsistent state if the trigger was deleted between read and write (race condition).

### Should Fix Before Merge

5. **MEDIUM (Test Coverage Gap)**: Replace `mock.Anything` with explicit `agencyNetworkID` values in all handler test mocks (`new-configuration_test.go`). Currently untested whether agency requests use pre-computed `agencyNetworkIDI` or body field — critical invariant for privilege isolation.

6. **LOW (Security)**: Include `agencyNetworkID` in audit log context so forensic investigation can distinguish advertiser-scoped vs. agency-scoped mutations.

7. **LOW (Observability)**: Add structured log when `FetchTriggers` falls back to advertiser scope due to all-invalid agency IDs, to help diagnose upstream caller bugs.

### Optional Before Merge

8. **NIT (Maintainability)**: Normalise parameter ordering in `InsertInstallTriggers` and `UpdateInstallTriggers` to match `GetAppTrigger`'s `(appID, networkID, templateID, agencyNetworkID)`.

### Pre-Deploy Actions (After Fixes)

9. Verify no `filter_agency_network = -1` rows exist in production triggers table.
10. Run data-recovery query to restore any advertiser triggers already corrupted to `filter_agency_network != 0` by the original bug.
11. Confirm front-end correctly passes `agency_network` query param when switching agencies.


</details>

<details><summary>Raw review — Claude CI</summary>

### Comprehensive Code Review

I've completed a thorough analysis of this critical bug fix. This PR addresses a serious data corruption issue where agency postback creation was overwriting advertiser-level install triggers.

## Summary

✅ **Root Cause Identified**: The issue was correctly identified - missing `filter_agency_network` scoping in `GetAppTrigger` and `UpdateInstallTriggers` functions.

✅ **Fix Strategy**: The solution properly scopes database operations by agency network ID to prevent cross-contamination between advertiser and agency triggers.

## Detailed Analysis

### 🔍 Database Layer Changes (`internal/database/triggers.go`)

**Strengths:**
- **Line 169**: `GetAppTrigger` now properly includes `filter_agency_network` in the WHERE clause, preventing agency lookups from matching advertiser triggers
- **Line 189**: `UpdateInstallTriggers` moves `filter_agency_network` from SET to WHERE clause, ensuring updates only affect the intended scope
- **Scoping Logic**: Proper separation between advertiser (agency_network=0) and agency (agency_network>0) triggers

**Concerns:**
- **SQL Injection Risk**: Line 169 uses raw SQL with user input. Consider using parameterized queries consistently
- **Transaction Usage**: `UpdateInstallTriggers` uses transactions but `GetAppTrigger` doesn't - consider consistency for data integrity
- **Error Handling**: Missing context propagation in some database operations

### 🛡️ Security Implications

**Moderate Risk Areas:**
- **Input Validation**: `agencyNetworkID` parameter needs validation to prevent negative values or SQL injection
- **Authorization**: No explicit checks that the requesting user/agency has permission to access the specified agency network
- **Data Exposure**: The fix prevents data corruption but doesn't add audit logging for agency network access

### 🚀 Handler Changes (`internal/handlers/partners/new-configuration.go`)

**Strengths:**
- **Lines 257-271**: Excellent scoping logic that determines `agencyNetworkID` before calling `GetAppTrigger`
- **Line 349**: Custom postbacks correctly use `agencyNetworkID = 0` (advertiser-scoped)
- **Type Safety**: Proper conversion and error handling for agency network IDs

**Performance Considerations:**
- **Database Calls**: Multiple database round-trips (GetNetworkInfo, GetTemplateID, GetAppTrigger) could be optimized
- **Memory Usage**: Large request bodies in agency configurations could benefit from streaming

### ✅ Test Coverage

**Excellent Test Strategy:**
- **Line 202-226**: `TestGetAppTrigger_ScopedByAgencyNetwork` provides comprehensive regression testing
- **Line 232-261**: `TestUpdateInstallTriggers_DoesNotCorruptOtherScope` validates the core fix
- **Edge Cases**: Tests cover advertiser scope, agency scope, and non-existent scope scenarios

**Missing Test Coverage:**
- Negative agency network IDs
- Concurrent access scenarios
- Custom postback agency scoping edge cases

## 🔧 Recommendations

### Critical (Must Fix)
1. **Input Validation**: Add validation for `agencyNetworkID` parameter:
   ```go
   if agencyNetworkID < 0 {
       return -1, fmt.Errorf("invalid agency network ID: %d", agencyNetworkID)
   }
   ```

2. **Audit Logging**: Add audit entries when agency triggers are created/updated to track cross-agency access

### High Priority
3. **Error Context**: Propagate context through database operations for better tracing:
   ```go
   func (db *GlobalGormDB) GetAppTrigger(ctx context.Context, appID, networkID, templateID, agencyNetworkID int) (int, error)
   ```

4. **Transaction Consistency**: Consider wrapping `GetAppTrigger` + `UpdateInstallTriggers` calls in a transaction to prevent race conditions

### Medium Priority
5. **Query Optimization**: Consider a single query to fetch template and trigger information together
6. **Documentation**: Add inline comments explaining the agency scoping logic
7. **Metrics**: Add monitoring for agency vs advertiser trigger operations

## ✅ Standards Compliance

**Go Style Guide Adherence:**
- ✅ Proper error handling patterns
- ✅ Consistent naming conventions
- ✅ Interface usage for database abstraction
- ⚠️ Some functions could benefit from context parameters (per Kochava standards)

## 🎯 Final Assessment

**Overall: APPROVED with minor recommendations**

This is a well-executed fix for a critical data corruption bug. The root cause analysis was thorough, the solution is targeted and effective, and the test coverage provides strong regression protection.

**Key Strengths:**
- Accurate problem diagnosis
- Minimal, focused changes
- Comprehensive test coverage
- Proper scoping implementation

**Risk Level: Low** - The changes are isolated and well-tested. The fix addresses the specific corruption issue without introducing new risks.

**Merge Recommendation: ✅ APPROVE** - This fix should be merged promptly to prevent further advertiser trigger corruption.

---

</details>

<details><summary>Classifier output</summary>

{'bucket': 'hard'}

</details>

### Kochava/frontend-mos#1816 (difficulty: hard)

| Criterion | skwad-cli | Claude CI |
| ---|---|--- |
| Issue Detection | 3 | 3 |
| Actionability | 2 | 2 |
| Severity Accuracy | 1 | 1 |
| Coverage | 2 | 2 |
| Signal-to-Noise | 2 | 1 |
| Depth | 2 | 2 |
| Novel & Substantive Findings | 2 | 2 |
| **Total** | **14** | **13** |

### Verification Summary

| Review | Verified | Unverified | Contradicted | Non-falsifiable | Rate | Tool Calls | Ungrounded | Grounding |
| ---|---|---|---|---|---|---|---|--- |
| Skwad | 26 | 2 | 7 | 0 | 72% | 131 | 0 | 100% |
| Claude CI | 20 | 4 | 6 | 2 | 59% | 131 | 1 | 96% |

<details><summary>Judge reasoning (skwad-cli)</summary>

**Issue Detection**: Review A identifies five genuine issues supported by the code: the over-broad non-S2S eligibility gate, empty validation status being treated as eligible, skipped backend e2e coverage, suppressed PATCH failure handling, and the dead `void` suppression statements. Two additional findings are contradicted by the repo.
**Actionability**: Most findings include concrete next steps or replacement code, though some recommendations are conditional rather than directly prescriptive.
**Severity Accuracy**: Priorities are mostly sensible, but the review overstates at least one correctness concern by labeling it critical and mixes minor hygiene issues into the findings list.
**Coverage**: The review spans correctness, test completeness, observability, and maintainability, but it misses other important correctness paths such as the broad catch around the existing-model check and stale validation state.
**Signal-to-Noise**: There is substantial useful signal, and the false positives are limited to lower-priority points, but they still reduce trust.
**Depth**: The stronger findings trace behavior across save flow, validation state, and test coverage rather than stopping at style comments, but the review does not reach deep architectural or invariant-level analysis consistently.
**Novel & Substantive Findings**: Compared with Review B, Review A contributes two substantive issues that B does not raise as problems: the over-broad non-S2S gate and allowing empty validation status to pass eligibility.

</details>

<details><summary>Claim trace (Skwad)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| The BR-001 gate is broader than a strict SDK allowlist because it auto-enables f | verified | Read | packages/advertiser/src/views/AppsAssets/stores/apps.ts:240 appData.integration_type !== "s2s" && |
| Empty `app_store_validation_status` is treated as eligible for auto-enable. | verified | Read | packages/advertiser/src/views/AppsAssets/stores/apps.ts:235 const validStatuses = ["Valid", "API_Unavailable", ""]; |
| The backend-asserting SKAN e2e tests are still stubbed as skipped tests. | verified | Read | playwright/e2e/tests/skan-auto-enable.spec.ts:78 test.skip("Happy path: Create iOS SDK app w/ id 284882215 → SKAN auto-e |
| `handleAutoEnableFailure` suppresses PATCH failures with `console.warn` instead  | verified | Read | packages/advertiser/src/views/AppsAssets/stores/apps.ts:221 console.warn("Failed to set skan_auto_enable_failed flag", p |
| There is no explicit unit test for a non-numeric `app_store_id`. | contradicted | Read | packages/advertiser/src/views/AppsAssets/stores/__tests__/apps.spec.ts:391 it("does NOT call SkadApi.post when app_store |
| `lookupAppStoreId` is missing a TypeScript type hint on the `appStoreId` paramet | contradicted | Read | packages/advertiser/src/shared/stores/skad.ts:204 const lookupAppStoreId = async (appStoreId: string, location: string): |
| `IntegrationSection.vue` contains unused `void` suppression statements. | verified | Read | packages/advertiser/src/views/AppsAssets/AppsV2/components/SettingsComponents/AppConfigurations/IntegrationSection/Integ |

</details>

<details><summary>Claim trace (Claude CI)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| `Number(appData.app_store_id)` can yield `NaN` that is then sent in the SKAN POS | contradicted | Read | packages/advertiser/src/views/AppsAssets/stores/apps.ts:237 const hasValidAppleAppID = Number.isFinite(appleAppID) && ap |
| The existing-model check catches non-404 failures and still proceeds to POST a n | verified | Read | packages/advertiser/src/views/AppsAssets/stores/apps.ts:255 } catch { // 404 = no model = expected for new apps; proceed |
| The validation deduplication in `Step2AppDetails.vue` prevents the second valida | contradicted | Read | packages/advertiser/src/views/AppsAssets/AppsV2/AddAppModalV2/Step2AppDetails/Step2AppDetails.vue:120 if (id === lastVal |
| The blur validation logic leaves stale validation state in place when the ID is  | verified | Read | packages/advertiser/src/views/AppsAssets/AppsV2/AddAppModalV2/Step2AppDetails/Step2AppDetails.vue:119 if (!/^[0-9]{1,15} |
| The async App Store lookup has no loading indicator or validation-in-progress st | verified | Grep | grep:isValidating\|:loading=\|loading= |
| If setting `skan_auto_enable_failed` fails, the code only logs a warning and doe | verified | Read | packages/advertiser/src/views/AppsAssets/stores/apps.ts:221 console.warn("Failed to set skan_auto_enable_failed flag", p |
| The numeric ID regex is a real issue because it allows leading zeros that Apple  | unverified | Read | The regex does allow leading zeros, but the review's claim that this is a real bug depends on external App Store ID rule |
| `lastValidatedAppStoreId` will remain stale across app-to-app navigation in sett | unverified | Read, Grep | The component defines `lastValidatedAppStoreId` and has no reset/watch in this file, but whether the component instance  |

</details>

<details><summary>Raw review — skwad-cli</summary>

# PR #1816 Review: SKAN Auto-Enable Tracking (PRD-049)

**PR Author**: Mohamed Benali  
**State**: OPEN  
**Commits**: 13 (6 cherry-picked from Cesar with original authorship + 7 additional from Mohamed)  
**Files Changed**: 33  
**Test Status**: 4604 unit tests passed, 72 skipped (pre-existing), lint clean

---

## Summary

This PR implements automatic SKAN install tracking for iOS SDK apps (PRD-049, Tasks T302-T308), closing a critical gap in mobile app attribution. The feature adds:

1. **Opt-out checkbox & colored status panel** (T302) — Blue "enabled" state with check icon, orange "disabled" state with warning icon in Add App modal Step 2 and existing app Settings
2. **Auto-enable logic with validation** (T304/T305) — POST `/model` with `install_only` on save if iOS SDK app with valid App Store ID; PATCH `skan_auto_enable_failed:true` if POST fails (fail-open, app save still succeeds)
3. **iTunes App Store ID lookup** (T303) — Apple HTTP request at blur on 3 entry points (Add App, App Settings General tab, SKAdNetwork tab) with 24h caching (QA) / 5min invalid cache
4. **SKAdNetwork toggle gating** (T306) — Disabled + tooltip when `validation_status=Invalid`
5. **Axios-retry on app updates** (T307) — 3 exponential-backoff retries for PUT/PATCH on 5xx errors
6. **Test coverage** (T308) — 8 unit tests covering BR-001/FR-021 happy/fail/guard paths, 3 UI e2e cases, 3 backend-asserting `.skip` placeholders

The work builds cleanly on Cesar's PRD-049 groundwork (cherry-picked 6 commits with original authorship preserved) and adds critical fixes (endpoint double-prefix bug, Vuetify color token registration, numeric Apple App ID gate).

---

## Strengths

### Code Quality & Completeness

✅ **Defensive predicate logic** — The BR-001 auto-enable gate in `apps.ts:229-243` checks multiple conditions in the right order:
- Platform (iOS only)
- Integration type (non-S2S)
- Opt-out flag (false)
- Validation status (in allowlist)
- Numeric Apple App ID (hard gate)

This prevents spurious requests to skad-api when preconditions aren't met.

✅ **Fail-open resilience** — App save succeeds even if SKAN auto-enable POST fails (line 313-314: `await autoEnableSkan()` runs after app save completes). FR-021 failure path correctly PATCH-flags the app and notifies the user without blocking the operation.

✅ **Defensive model check** — Line 248-254: Before POSTing a new model, check if a non-`install_only` model already exists and skip if found. Prevents duplicate model rows.

✅ **Numeric Apple App ID gate** — Line 236-237: Validates `Number.isFinite(appleAppID) && appleAppID > 0` instead of trusting user input. Skad-api rejects `apple_app_id=0`, so this prevents wasted 500 errors.

✅ **Comprehensive test coverage** — 5 unit tests in `apps.spec.ts` exercise:
- Happy path: POST with correct payload
- Opt-out=true skips POST
- Validation status Invalid skips POST
- Empty/non-numeric app_store_id skips POST
- POST failure triggers FR-021 (PATCH + toast)

Additionally, 8 component tests cover the UI panel rendering, opt-out flip, validation status emission from blur handler, and iOS-only visibility.

✅ **Mockup compliance** — Step2AppDetails and IntegrationSection panels match the PRD mockup:
- Blue background + check icon (enabled)
- Orange background + warning icon (disabled)
- White-card bordered checkbox row inside the colored panel
- Color tokens correctly wired to registered Vuetify theme aliases (`alerts-blue`, `alerts-orange`)

✅ **Axios-retry for reliability** — Apps service now retries PUT/PATCH 3 times on 5xx or network errors with exponential backoff, improving robustness for flaky connections.

### Architecture & Pattern

✅ **Clean store isolation** — SKAN auto-enable logic lives in the apps store (`apps.ts`) where it owns the app data; skad store remains a read-only query service (`lookupAppStoreId`).

✅ **Composable reuse** — IntegrationSection's `useIntegrationSection` composable exposes computed `isIos`, `skanOptOut` (rw), and `appStoreValidationStatus` (ro) derived from the apps store, avoiding duplication between Add App and Settings.

✅ **Event location tracking** — Lookup calls pass a `location` param ("create" vs "settings" vs "skan_tab") for audit-log disambiguation. This enables post-hoc analysis of which entry point users take.

### Bug Fixes

✅ **Endpoint double-prefix fix** — Commit `1b26edc37`: endpoints.skad.appstore_lookup was `/v1/appstore/lookup` but VITE_SKAD_NETWORK_API already terminates in `/v1`, causing 404s. Now correctly `/appstore/lookup`. Critical for feature to function.

✅ **Vuetify theme token registration** — Commit `cd1255406`: Panel backgrounds weren't rendering because `alerts_text-blue`, `alerts_text-orange` etc. are not registered in vuetify.ts. The code now uses the correct registered aliases `alerts-blue` and `alerts-orange` with explicit RGB variable binding and opacity. (Note: Dashboard.vue:177 and KPICheckboxes.vue:147 still use the old broken aliases but are out of scope here.)

### Process & Authorship

✅ **Preserved original authorship** — Cherry-picks from Cesar (#1808) preserve his commits with `(cherry picked from commit ...)` footers, maintaining git history integrity.

✅ **Clear commit messages** — Each commit in the 13-commit stack has a specific purpose:
- Feature commits: add panel UI, auto-enable, color fixes
- Chore commits: translations, package updates, dependencies
- Test commits: 3 layers of coverage (store, component, e2e)
- Bug fix: endpoint double-prefix and numeric ID gate

✅ **Comprehensive PR description** — Summarizes the 8 tasks, references backend versions, calls out intentional deviations from eng-plan, and provides a detailed test plan.

---

## Issues & Concerns

### Critical Issues

⚠️ **BR-001 predicate `integration_type !== "s2s"` is intentionally broader than spec**  
Commit message notes this uses non-S2S instead of strict `∈ {sdk_native, sdk_wrapped}`. The effect is auto-enable triggers on any SDK platform, not just SDK-based integrations.
- **Risk**: Server-side integration types might include platforms that shouldn't auto-enable (e.g., S2S-adjacent), leading to unexpected SKAN rows in production.
- **Mitigation in PR**: Documented in code comment (line 230). If product testing in QA reveals over-triggering, the predicate can be tightened to exact enum match.
- **Recommendation**: Confirm with product/backend team that non-S2S is the intended scope. If not, change line 240 to `["sdk_native", "sdk_wrapped"].includes(appData.integration_type)`.

⚠️ **Empty `validation_status` is treated as eligible**  
Line 235: `validStatuses = ["Valid", "API_Unavailable", ""]` includes empty string, meaning if a user saves an app before the blur handler fires (no validation roundtrip), SKAN still auto-enables.
- **Risk**: User pastes a random App Store ID, clicks Save immediately → SKAN auto-enable POST fires with an invalid apple_app_id → skad-api rejects it → FR-021 warning appears post-save. UX could be confusing.
- **Mitigation in PR**: Documented as intentional fail-open trade-off. Conservative approach would be `validStatuses = ["Valid"]` (require successful lookup before auto-enable).
- **Recommendation**: Test the UX flow on QA. If users find it confusing to save then see a warning, consider stricter `validStatuses = ["Valid"]`.

### High Priority (Should Fix)

🔴 **E2E tests are stubbed `.skip()` — no backend coverage**  
File `playwright/e2e/tests/skan-auto-enable.spec.ts` has 3 tests marked `.skip`:
- Happy path (POST /model succeeds, SKAN row created)
- Opt-out (POST skipped, no SKAN row)
- Invalid ID (POST skipped due to BR-001 fail)

**Why it matters**: The 5 unit tests exercise the logic in isolation, but e2e tests should verify the full HTTP flow: app creation → iTunes lookup → skad-api POST → BQ audit event. Without these, a regression in the HTTP layer (e.g., wrong Content-Type header, malformed JSON) could slip through.

**Before merge**: 
- [ ] Un-skip the three e2e tests and wire them to a QA test account with data cleanup hooks (as noted in the test comments). OR
- [ ] Document in the PR that the test shells are intentionally skipped and will be un-skipped in a follow-up PR once QA environment wiring is complete.

Current state: Incomplete per the eng-plan T308 description ("3 backend-asserting e2e cases"). The placeholders are valuable but don't prove backend integration works.

### Medium Priority (Nice to Fix)

🟡 **`handleAutoEnableFailure` logs to console.warn but doesn't propagate the patch error**  
Line 221: `console.warn("Failed to set skan_auto_enable_failed flag", patchErr);` catches and suppresses patch errors. If the PATCH itself fails (e.g., 401 auth), the user gets a generic warning toast but the actual error is buried in the console.

**Recommendation**: Either:
1. Log at error level: `console.error(...)` so it's easier to find in logs, or
2. Include a hint in the toast text: `"Failed to auto-enable SKAN, but app was saved. Please contact support if this persists."`

🟡 **No explicit test for non-numeric app_store_id in the empty-ID unit test**  
Line 391-400 tests non-numeric IDs like `com.facebook.Facebook`, but the regex guard `^[0-9]{1,15}$` in `Step2AppDetails.onBundleIdBlur` (line 119) prevents the lookup from even firing. Consider adding a comment or a separate test explicitly verifying the regex blocks non-numeric input.

**Current state**: Logic is correct, but intent could be clearer.

### Low Priority (Style & Clarity)

🟢 **Missing TypeScript type hint on `appStoreId` parameter**  
`lookupAppStoreId(appStoreId: string, location: string)` (skad.ts:204) and the consuming code assume `appStoreId` is numeric. Consider:
```typescript
// Option 1: Rename for clarity
lookupAppStoreId(numericAppStoreId: string, location: string)

// Option 2: Add JSDoc
/** @param appStoreId — numeric Apple App ID as a string (e.g., "284882215") */
lookupAppStoreId(appStoreId: string, location: string)
```

🟢 **Unused `void` statements in IntegrationSection.vue**  
Lines 79-80:
```typescript
void selectIntegrationType;
void appStoreValidationStatus;
```
These are never called. Likely added to suppress unused variable warnings during refactoring. Can be safely removed if the variables are truly unused, or added to the template if they should be reactive.

🟢 **SKAN API service not in scope, but exists as `skad.ts`**  
The PR uses `SkadApi` (imported from `../../services/skad`) but that file is not shown in the diff. Assuming it's a standard axios wrapper that accepts the same headers/params pattern as `AppsApi`. If it's new, it should be included in the PR diff for completeness.

---

## Verification Checklist

Before merge, ensure:

- [x] Commit messages follow Conventional Commits (feat/fix/test/chore, lowercase)
- [x] Tests pass: `pnpm --filter @mos/advertiser test` → 4604 passed, 0 failed
- [x] Linting passes: `pnpm run lint:ci` → clean (per PR description)
- [x] Type checking passes: `pnpm --filter @mos/app type-check` (dependent on these changes)
- [x] No console errors in dev build
- [ ] **E2E tests un-skipped and passing on QA** (blocking or to be addressed in follow-up PR?)
- [x] Mockups match (panel colors, icons, checkbox placement)
- [x] Translation keys added (`apps.skan_tracking.*`)
- [x] Backend dependencies live in QA (skad-api 1.11.0-rc03, sprinkler-api v1.23.0-rc00)

---

## Summary & Recommendation

**This PR is production-ready pending resolution of the BR-001 scope concern and E2E test wiring.**

### What's Strong
- Clean, defensive auto-enable logic with clear fail-open semantics
- Excellent test coverage at the unit and component layer
- Visual mockup compliance with correct color token registration
- Bug fixes (endpoint prefix, numeric ID gate) are critical and correct
- Axios-retry improves reliability of app updates

### What Needs Attention
1. **Confirm BR-001 scope**: Is `integration_type !== "s2s"` the intended gate, or should it be strict `∈ {sdk_native, sdk_wrapped}`?
2. **E2E test wiring**: The three backend-asserting tests are stubbed `.skip()`. Decide:
   - Un-skip and wire to QA before merge, OR
   - Document that they'll be un-skipped in a follow-up PR (less ideal, but acceptable if QA isn't ready yet)
3. **Empty `validation_status` UX**: Test on QA to confirm users don't find it confusing to see a warning toast after saving with a blank ID.

### Code Quality Grade
**B+** — Well-structured, tested, and documented. Minor clarity improvements and one spec scope concern prevent a straight A.

### Recommendation
**Approve with conditions**: Fix the scope concern (BR-001 predicate) and confirm E2E test plan before merging to main. The logic is sound and the test coverage is solid at the unit level.


</details>

<details><summary>Raw review — Claude CI</summary>

### PR Review: SKAN Auto-Enable Tracking

Comprehensive review of PRD-049 Phase 4 implementation (T302-T308). The implementation is solid overall with good test coverage and follows the engineering plan. I've identified several issues to address before merging.

---

### ✅ What's Working Well

- **Clear separation of concerns**: Auto-enable logic cleanly separated into `autoEnableSkan` and `handleAutoEnableFailure` functions
- **Comprehensive test coverage**: 5 unit tests covering BR-001 predicate paths + 3 UI e2e tests
- **Defensive programming**: Existing model check prevents accidental overwrites
- **Fail-open approach**: Treats empty validation status as eligible (documented trade-off)
- **Proper retry strategy**: axios-retry with exponential backoff on PUT/PATCH for 5xx/network errors
- **UI/UX matches mockup**: Blue/orange panels, checkbox styling, icons all match design
- **Vue reactivity**: Computed properties properly implemented with get/set pattern

---

### 🔴 Critical Issues

#### 1. Type Coercion Could Produce NaN
**File**: `packages/advertiser/src/views/AppsAssets/stores/apps.ts:258`

```typescript
apple_app_id: Number(appData.app_store_id),
```

**Issue**: If `app_store_id` is undefined, empty string, or non-numeric, `Number()` returns `NaN`. The backend will receive `apple_app_id: NaN` in the POST body, which could cause 400 errors or worse, be stored.

**Fix**: Add validation before the POST:
```typescript
const appleAppId = Number(appData.app_store_id);
if (!appleAppId || isNaN(appleAppId)) {
  console.warn("Invalid app_store_id for SKAN auto-enable");
  return;
}
```

#### 2. Silent 5xx on Existing Model Check
**File**: `packages/advertiser/src/views/AppsAssets/stores/apps.ts:242-251`

```typescript
try {
  const existing = await SkadApi.get(endpoints.skad.get(appId), {
    headers: { "Authentication-Key": organizationStore.accountApiKey },
  });
  if (existing.data?.data?.model_id && existing.data.data.model_id !== "install_only") {
    return;
  }
} catch {
  // 404 = no model = expected for new apps; proceed
}
```

**Issue**: If the GET fails with 500 or network error (not 404), the catch block assumes "no model exists" and proceeds to POST. This could create a duplicate model or overwrite data if the backend is temporarily unavailable but the model actually exists.

**Fix**: Check the error status:
```typescript
} catch (err: any) {
  // Only proceed on 404 (no model). Other errors = bail out.
  if (err?.response?.status !== 404) {
    console.warn("Failed to check existing SKAN model", err);
    await handleAutoEnableFailure(appId);
    return;
  }
}
```

#### 3. Race Condition in Validation Deduplication
**File**: `packages/advertiser/src/views/AppsAssets/AppsV2/AddAppModalV2/Step2AppDetails/Step2AppDetails.vue:115-128`

```typescript
const onBundleIdBlur = async () => {
  const id = props.bundleId;
  if (props.platform.toLowerCase() !== "ios") return;
  if (!/^[0-9]{1,15}$/.test(id)) return;
  if (id === lastValidatedId.value) return;
  lastValidatedId.value = id;  // ← Set BEFORE async call
  try {
    const data = await skadStore.lookupAppStoreId(id, "create");
    emit("update:appStoreValidationStatus", data.status);
  } catch {
    emit("update:appStoreValidationStatus", "API_Unavailable");
  }
};
```

**Issue**: If the user types "12345", tabs away (triggers validation), then quickly changes to "67890" and tabs away again before the first request completes, the second validation won't run because `lastValidatedId` is set synchronously. The app will have `bundleId="67890"` but `validation_status` from "12345".

**Fix**: Set `lastValidatedId` after the async call completes:
```typescript
const onBundleIdBlur = async () => {
  const id = props.bundleId;
  if (props.platform.toLowerCase() !== "ios") return;
  if (!/^[0-9]{1,15}$/.test(id)) return;
  if (id === lastValidatedId.value) return;
  
  try {
    const data = await skadStore.lookupAppStoreId(id, "create");
    lastValidatedId.value = id;  // ← Move here
    emit("update:appStoreValidationStatus", data.status);
  } catch {
    lastValidatedId.value = id;  // ← And here
    emit("update:appStoreValidationStatus", "API_Unavailable");
  }
};
```

Same issue in `GeneralSectionV2.vue:99-110`.

---

### 🟡 Medium Priority Issues

#### 4. Missing Validation Status Reset
**Files**: `Step2AppDetails.vue:115-128`, `GeneralSectionV2.vue:99-110`

**Issue**: When the user changes the bundle/store ID to an invalid format (e.g., "abc123") or the same ID as `lastValidatedId`, the validation doesn't run but the old `validation_status` persists. This means an app could show "Valid" status for an ID that's no longer valid.

**Fix**: Clear the validation status when the ID changes but doesn't trigger validation:
```typescript
const onBundleIdBlur = async () => {
  const id = props.bundleId;
  if (props.platform.toLowerCase() !== "ios") return;
  
  if (!/^[0-9]{1,15}$/.test(id)) {
    emit("update:appStoreValidationStatus", "");  // ← Clear status
    return;
  }
  
  if (id === lastValidatedId.value) return;
  // ... rest of validation
};
```

#### 5. No Visual Feedback During Validation
**Files**: `Step2AppDetails.vue`, `GeneralSectionV2.vue`

**Issue**: The iTunes lookup is async and can take 1-2 seconds, but there's no loading indicator. Users might think nothing happened or try to save before validation completes.

**Suggestion**: Add a loading state:
```vue
<input-text
  v-model="localBundleId"
  :label="$t('apps.add_app_modal.app_bundle_id_label')"
  :loading="isValidating"
  @blur="onBundleIdBlur"
/>
```

#### 6. Observability Gap in Failure Path
**File**: `packages/advertiser/src/views/AppsAssets/stores/apps.ts:220-222`

```typescript
} catch (patchErr) {
  console.warn("Failed to set skan_auto_enable_failed flag", patchErr);
}
```

**Issue**: If the PATCH to set `skan_auto_enable_failed` fails, the user still sees the warning notification but the backend doesn't record the failure. This creates a mismatch between what the user knows and what support/ops can see.

**Suggestion**: Either:
- Retry the PATCH, or
- Include the PATCH failure in the user notification: "SKAN setup failed. Please configure manually. (Note: failure may not be recorded in system logs)"

---

### 🟢 Minor Issues / Suggestions

#### 7. Regex Allows Leading Zeros
**Files**: `Step2AppDetails.vue:119`, `GeneralSectionV2.vue:102`

```typescript
if (!/^[0-9]{1,15}$/.test(id)) return;
```

**Issue**: Apple App Store IDs never have leading zeros, but this regex allows them (e.g., "00000123"). This could cause unnecessary API calls for typos.

**Suggestion**: Change to `/^[1-9][0-9]{0,14}$/` to require the first digit to be 1-9.

#### 8. Stale lastValidatedId Across Apps
**File**: `GeneralSectionV2.vue:84`

**Issue**: The `lastValidatedAppStoreId` ref is never reset when navigating between different apps in settings. If App A has ID "12345" (validated), then you navigate to App B and type "12345", the validation won't run because it's cached.

**Fix**: Reset when the app changes (watch `currentAppForm.value.app_data.id`).

#### 9. Comment Could Be Clearer
**File**: `packages/advertiser/src/views/AppsAssets/stores/apps.ts:231-232`

```typescript
// Also allow empty status (lookup may not have run yet — fail-open)
```

**Suggestion**: Clarify the race condition scenario:
```typescript
// Also allow empty status to handle race: if user pastes ID and clicks Save
// before blur handler fires, validation_status will be empty. Fail-open here
// means the app still auto-enables (conservative-vs-UX trade-off per Cesar).
```

---

### 📋 Test Coverage Analysis

**Unit Tests** (`stores/__tests__/apps.spec.ts`):
- ✅ BR-001 happy path (eligible iOS SDK app)
- ✅ BR-001 opt-out=true blocks auto-enable
- ✅ BR-001 validation_status=Invalid blocks auto-enable
- ✅ Defensive guard: existing non-install_only model skips POST
- ✅ FR-021: POST failure triggers PATCH + warning notification

**E2E Tests** (`playwright/e2e/tests/skan-auto-enable.spec.ts`):
- ✅ Blue panel visible for iOS in Step 2
- ✅ Checkbox flip to orange/disabled state
- ✅ Panel hidden for non-iOS platforms
- ⏸️ 3 backend-asserting tests stubbed (requires QA env wiring)

**Missing Coverage**:
- No test for the `Number(appData.app_store_id)` NaN scenario
- No test for 5xx error on existing model check
- No test for validation race condition

---

### 🎯 Recommendations

**Before Merge**:
1. Fix Issue #1 (NaN type coercion) - **critical**
2. Fix Issue #2 (silent 5xx on model check) - **critical**
3. Fix Issue #3 (validation race condition) - **critical**
4. Fix Issue #4 (missing validation reset) - **medium**

**Follow-up PRs**:
5. Add loading state for validation (Issue #5) - **UX improvement**
6. Improve PATCH failure observability (Issue #6) - **ops/support**
7. Wire up the 3 backend-asserting e2e tests once QA env cleanup is ready

---

### 💡 Architecture Notes

- **Deviation from eng-plan acknowledged**: `integration_type !== "s2s"` is broader than strict SDK check. This is fine but should be documented in PRD if not already.
- **Fail-open on empty status**: Reasonable trade-off, but consider adding telemetry to track how often this path is hit in production.
- **Axios-retry on PUT/PATCH only**: Good choice. GET retries could mask data issues; POST retries risk duplicates (though skad POST is protected by the defensive check).

---

### ✅ Review Checklist Complete
- [x] Review auto-enable business logic (stores/apps.ts)
- [x] Review failure path handling (FR-021)
- [x] Review iTunes validation integration
- [x] Review UI components and reactivity
- [x] Review axios-retry implementation
- [x] Review test coverage
- [x] Review type safety and error handling
- [x] Check for security issues
- [x] Compile final feedback

---

**Overall Assessment**: Strong implementation with good defensive patterns and test coverage. The 3 critical issues need fixing before merge, but they're all straightforward. The medium/minor issues are quality-of-life improvements that can be addressed now or in follow-ups.

</details>

<details><summary>Classifier output</summary>

{'bucket': 'hard'}

</details>

### Kochava/frontend-mos#1818 (difficulty: hard)

| Criterion | skwad-cli | Claude CI |
| ---|---|--- |
| Issue Detection | 3 | 2 |
| Actionability | 2 | 3 |
| Severity Accuracy | 1 | 3 |
| Coverage | 3 | 1 |
| Signal-to-Noise | 1 | 2 |
| Depth | 3 | 2 |
| Novel & Substantive Findings | 3 | 0 |
| **Total** | **16** | **13** |

### Verification Summary

| Review | Verified | Unverified | Contradicted | Non-falsifiable | Rate | Tool Calls | Ungrounded | Grounding |
| ---|---|---|---|---|---|---|---|--- |
| Skwad | 52 | 4 | 4 | 0 | 86% | 120 | 8 | 85% |
| Claude CI | 17 | 2 | 2 | 0 | 81% | 120 | 0 | 100% |

<details><summary>Judge reasoning (skwad-cli)</summary>

**Issue Detection**: Verified 17 genuine issues/findings spanning correctness, performance, API design, observability, and missing tests. One architecture-boundary claim was not provable from repo evidence.
**Actionability**: Nearly every finding includes a concrete remediation, usually at code level or with a specific test to add.
**Severity Accuracy**: Several core risks are prioritized reasonably, but the review overstates comment drift and some architecture concerns as block-merge MEDIUM issues.
**Coverage**: The review covers correctness, performance, resilience/observability, API-surface design, and test coverage, which is broad for this patch.
**Signal-to-Noise**: There is substantial signal, but the review is noisy: it includes at least one incorrect factual claim and some weakly-supported blocker framing.
**Depth**: The stronger findings trace behavior across the GraphQL payload builder, pagination loop, notifications store side effects, and test suite behavior.
**Novel & Substantive Findings**: Compared with Review B, Review A contributes multiple substantive findings that B never mentions, including a real guard bug, a misleading spec contract, missing finite-value validation, and the leaked page-size test seam.

</details>

<details><summary>Claim trace (Skwad)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| The GraphQL payload always requests `byOrganicPublisher`, so later pages re-fetc | verified | Read, Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/api/attr-api/sources/sources-gql/_internal/download- |
| Pages are fetched strictly sequentially because each loop iteration awaits `fetc | verified | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/sources-download.ts:57 const page = |
| The pagination API exposes no `AbortSignal`, so callers cannot cancel a long-run | verified | Grep | grep:AbortSignal |
| The store exposes no `onProgress` callback or similar progress hook for multi-pa | verified | Grep | grep:onProgress |
| The organic overflow guard compares `availableResultsCount` against the caller-p | verified | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/sources-download.ts:67 if (page.byO |
| The spec header is inaccurate because it says publishers/campaigns keep single-f | verified | Read, Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/__tests__/sources-download.spec.ts: |
| There is no test that exercises the organic overflow throw path. | verified | Grep | grep:exceed the page cap |
| There is no test that exercises the max-iterations throw path. | verified | Grep | grep:max pagination iterations |
| The new pagination tests only cover short final pages; they do not cover the `go | verified | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/__tests__/sources-download.spec.ts: |
| `MmmTopFilter.spec.ts` does not cover the new notification path. | verified | Grep | grep:addNotification |
| There is no `ModalHeader` spec covering the new download error handling. | verified | Grep | grep:ModalHeader |
| `availableResultsCount` is used without any finite-value validation before compu | verified | Grep, Read | grep:Number\.isFinite |
| Passing `debug: error` into notifications logs the raw error object to the conso | verified | Read, Read | packages/core/src/stores/notifications.ts:12 console.info("ℹ️ Notification debug:", notification.debug); |
| The public `downloadData` action now exposes a `pageSize` parameter. | verified | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/sources-download.ts:43 pageSize: nu |
| Current non-test callers do not pass the third `pageSize` argument; only tests u | verified | Grep | grep:downloadData\([^\)]*,[^\)]*,[^\)]*\) |
| The initial `skip`/`take` emitted by download options are immediately overridden | verified | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/sources-download.ts:60 take: pageSi |
| The GQL builder still hardcodes `5000` as a fallback page size. | verified | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/api/attr-api/sources/sources-gql/_internal/download- |
| The review's architecture-strength statement claiming `useNotificationsStore` an | contradicted | Read | packages/advertiser/src/views/Analytics/MmmInsights/MmmTabComponent/Sources/MediaPartnerDetailsModal/ModalHeader/ModalHe |
| Importing `DOWNLOAD_PAGE_SIZE` from an `_internal` path violates a documented mo | unverified | Read | The `_internal` path is visible in code, but no repository-local rule or convention was found proving such imports are f |

</details>

<details><summary>Claim trace (Claude CI)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| The GraphQL query requests `byOrganicPublisher` on every pagination page, while  | verified | Read, Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/api/attr-api/sources/sources-gql/_internal/download- |
| The notification shown on download failure uses a single generic `failed_downloa | verified | Read | packages/advertiser/src/views/Analytics/MmmInsights/MmmTopFilter/MmmTopFilter.vue:78 text: t("mmm_insights.errors.failed |
| The tests verify skip offsets of `0, 2, 4`. | verified | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/__tests__/sources-download.spec.ts: |
| The tests verify the previously dropped networks are present in the merged expor | verified | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/__tests__/sources-download.spec.ts: |
| The tests verify there is no duplication across page boundaries. | verified | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/__tests__/sources-download.spec.ts: |
| The tests verify organic data is only included once. | verified | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/__tests__/sources-download.spec.ts: |
| The tests cover exact-multiple pagination boundaries. | contradicted | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/__tests__/sources-download.spec.ts: |
| The test file has strong inline documentation explaining the two concerns being  | contradicted | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/__tests__/sources-download.spec.ts: |
| The `+2` buffer in `maxIterations` is clearly documented. | verified | Read | packages/advertiser/src/views/Analytics/stores/analytics2/minsights/sources/download/sources-download.ts:73 // +2 = slac |
| Download failures are propagated to the UI through notifications. | verified | Read | packages/advertiser/src/views/Analytics/MmmInsights/MmmTopFilter/MmmTopFilter.vue:76 notificationsStore.addNotification( |

</details>

<details><summary>Raw review — skwad-cli</summary>

# PR #1818 Review: Enable Media Partners Paginated Download

**Title:** fix: enable mediapartners paginated download  
**Author:** lsinghkochava  
**Status:** OPEN  
**Repo:** Kochava/frontend-mos

---

## Summary

This PR fixes a silent data loss bug in the Media Partners CSV download feature. When datasets exceeded the backend's 5000-row limit, rows were silently dropped (affecting TikTok, Liftoff, Rokt, Snapchat, Tatari Brand when sorted by NETWORK_NAME ASC). The fix implements pagination with accumulation to fetch complete datasets while preserving backward compatibility.

---

## What Changed

### Type Definitions
- **ApiOtions.ts**: Added `skip?` and `take?` fields to `DownloadMediaPartnersGqlPayloadOption`
- **SourcesApiResponseInterface.ts**: Added `availableResultsCount: number` to attribution and byOrganicPublisher in all three response types (DownloadMediaPartnersResponse, DownloadPublishersResponse, DownloadCampaignsResponse)

### API & GraphQL
- **download-gql.ts**: Modified query construction to use `getSortSkipTakeString()` for pagination; added `availableResultsCount` to GraphQL response fields

### Store Logic
- **sources-download.ts**: Implemented new pagination loop that:
  - Accumulates paid (attribution) results across pages
  - Consumes organic (byOrganicPublisher) results once on page 1 only
  - Validates organic doesn't exceed page size (fail-loud guard)
  - Breaks on short page or when accumulated results reach available count
  - Safety cap: maxIterations = ceil(availableResultsCount / pageSize) + 2

### UI Error Handling
- **ModalHeader.vue** & **MmmTopFilter.vue**: Added error notifications via `useNotificationsStore` and `useI18n` to surface download failures instead of silent swallowing
- **download-options.ts**: Added `DOWNLOAD_PAGE_SIZE = 5000` constant and initial skip/take to options

### Tests
- **sources-download.spec.ts**: Comprehensive pagination tests covering:
  - Multi-page accumulation with previously-dropped networks
  - Page boundary deduplication and drop detection
  - 103 new test lines validating pagination correctness

### Internationalization
- Added `mmm_insights.errors.failed_download` translation key to all locale files (en.json, de.json, es.json, fr.json, ja.json, ko.json, ru.json, zh-CN.json)

---

## Specialist Reviews Integrated

### Performance Analyst Findings

**Loop Termination: SAFE** ✅
- Three independent exit conditions prevent infinite loops (gotShortPage, gotAllRows, maxIterations guard)
- The `+2` slack in maxIterations is justified and bounded to at most 2 extra short-page requests

**Finding 1 — MEDIUM: `byOrganicPublisher` fetched on every page, consumed only on page 1**
- GQL query includes organic sub-query on every page request, but `sources-download.ts` only reads it when `skip === 0`
- Impact: For 50K rows (10 pages), that's 9 wasted organic queries fetching duplicate data
- Recommended fix: Pass skip to buildDownloadPayload and conditionally omit byOrganicPublisher when skip > 0, OR extract organic into separate one-shot query

**Finding 2 — MEDIUM: Strictly sequential page fetching (O(N) round trips)**
- Pages fetched one at a time inside loop; after page 1, all future offsets are known
- Could fan out pages 2-N in parallel via Promise.all after page 1 returns
- Cost: 50K rows = 10 sequential requests at ~300ms RTT = ~3s waiting; 200K rows = ~12s
- Not a blocker for typical exports but worth noting

**Finding 3 — LOW: No abort signal for mid-export cancellation**
- If user navigates away during 20-page export, all remaining 18 requests still fire silently
- Recommended fix: Accept optional AbortSignal parameter and check signal.aborted at loop top

**Finding 4 — LOW: No progress feedback during multi-page fetch**
- downloadData is fire-and-forget async; no intermediate state for UI to show progress
- Degrades perceived performance and may cause duplicate clicks for large exports
- Recommended fix: Add onProgress callback or update Pinia reactive state for progress bar

**Finding 5 — NIT: push(...spread) safe at 5000 items/page** ✅
- Spread-into-push hits call stack limits at >100K elements, but 5000 is completely safe

---

### Bug Hunter Findings

**Overall verdict:** 2 real bugs found (both MEDIUM confidence); 4 missing test cases. Logic is sound, but implementation details and test coverage have gaps.

**Bug 1 — MEDIUM: Organic overflow guard uses wrong pageSize parameter**
- **File**: `sources-download.ts:67`
- **Current code**: `if (page.byOrganicPublisher.availableResultsCount > pageSize) { throw ... }`
- **Problem**: Uses `pageSize` (the paid/attribution page size parameter) instead of organic row cap
- **Scenario**: Calling `downloadData(type, ids, N)` with N < organic rows < 5000 triggers false throw even though no overflow occurred
- **Production impact**: In production defaults align (both 5000), but custom pageSize values break organic check
- **Correct fix**: Replace with `if (organicResults.length < page.byOrganicPublisher.availableResultsCount) { throw ... }` to directly detect truncation

**Bug 2 — MEDIUM: Comment contradicts code behavior**
- **File**: `sources-download.spec.ts:15-17` (file header)
- **Current comment**: "publishers/campaigns keep single-fetch behavior" + references "type-gate" preventing regressing to type-agnostic loop
- **Reality**: No type-gate exists; code paginates all three types identically
- **Evidence**: Line 145 in same file explicitly tests publishers pagination across 3 pages
- **Impact**: Future maintainers will be misled about intended behavior
- **Fix**: Update comment to accurately describe that all three types paginate

**Missing Test Coverage — 4 cases**

3. **MEDIUM**: Organic overflow fail-loud throw (`sources-download.ts:68-71`) never triggered in tests
   - No test case for `availableResultsCount > pageSize` scenario
   - Recommend: Add test with organic availableResultsCount intentionally exceeding pageSize

4. **MEDIUM**: maxIterations fail-loud throw (`sources-download.ts:83-85`) never triggered
   - No test case for exceeding iteration limit
   - Current tests don't hit this guard; if stale availableResultsCount occurs, this code path unverified
   - Recommend: Add test with availableResultsCount mismatched (claims more rows than actually fetchable)

5. **MEDIUM**: `gotAllRows` exit condition (`line 79`) never the terminator in tests
   - Both pagination tests exit via `gotShortPage` only (page 3 is short)
   - `gotAllRows` path (`paidResults.length >= availableResultsCount`) untested
   - Recommend: Add test where final page is full-sized but accumulated count reaches available total

6. **LOW**: Error notification blocks in MmmTopFilter.vue and ModalHeader.vue have no test coverage
   - New `notificationsStore.addNotification` calls not exercised
   - Both components lack test suites for error cases
   - Recommend: Mock error scenarios and verify notifications are dispatched

**Non-bug observation:** `byOrganicPublisher` alias executes on every page fetch but results consumed only on page 1 (already flagged by Performance Analyst as wasteful but not a correctness issue).

---

### Security Sentinel Findings

**Overall verdict:** No CRITICAL or HIGH findings. Authorization model unchanged; no data exfiltration. Main concern is resilience against unexpected server responses.

**Finding 1 — MEDIUM: Pagination safety cap disabled by non-finite `availableResultsCount`**
- **File**: `sources-download.ts:76`
- **Vulnerability type**: Untrusted input driving loop termination / client-side resource exhaustion
- **Attack scenario**: If server returns `undefined` for `availableResultsCount`, then:
  - `Math.ceil(undefined / 5000) + 2` → `NaN` → `maxIterations = NaN`
  - `paidResults.length >= undefined` → `false` (gotAllRows never fires)
  - `iterations >= NaN` → `false` (safety cap never fires)
  - Loop continues indefinitely if backend returns exactly pageSize rows (due to bug or MITM in CI/dev)
- **Impact**: Frontend resource exhaustion (memory/network DoS). No data exfiltration, but user could exhaust browser memory during legitimate download if backend regresses
- **Recommended fix**: Validate `availableResultsCount` is a finite non-negative number; add hard unconditional iteration cap (e.g., 200) independent of this field

**Finding 2 — LOW: Raw Error object written to console in production**
- **Files**: `ModalHeader.vue:76`, `MmmTopFilter.vue:83`
- **Vulnerability type**: Information disclosure
- **Issue**: Catch blocks pass `debug: error` (raw JavaScript Error) to notification store, which logs it to console in production. Errors include server-returned values (e.g., row counts, pagination details)
- **Impact**: Any authenticated user opening DevTools can read internal pagination logic and backend error details
- **Recommended fix**: Log only `error.message` in production, or omit debug entirely (include debug only in DEV mode)

**Finding 3 — NIT: `getSortSkipTakeString` interpolates strings without allowlisting**
- **File**: `common-gql.ts:40-51` (pre-existing pattern, extended by PR)
- **Vulnerability type**: Potential GraphQL injection
- **Note**: Pre-existing issue. In download path, `primaryField`/`direction` are hardcoded (`"NETWORK_NAME"`, `"ASC"`), not user input. But pattern extends to any future user-controlled sort data
- **Recommended fix**: Add allowlist for field and direction values

**Finding 4 — NIT: Missing null guard on `page.attribution.results`**
- **File**: `sources-download.ts:71` — `paidResults.push(...page.attribution.results)`
- **Issue**: If API returns `null`/`undefined` results, this throws `TypeError: undefined is not iterable` uncaught by pagination logic
- **Recommended fix**: `paidResults.push(...(page.attribution.results ?? []))`

---

### Architecture Reviewer Findings

**Overall verdict: Approve with reservations. Two findings should block merge.**

**Finding 1 — MEDIUM (BLOCKS): Direct `_internal` import across module boundary**
- **File**: `sources-download.ts:6` imports from `../_internal/download-options`
- **Issue**: Project convention is that `_internal/` is implementation detail, not for external import
- **Concrete risk**: If download-options.ts is restructured, import silently breaks; sets precedent for other _internal boundary violations
- **Must fix before merge**: Define DOWNLOAD_PAGE_SIZE in co-located file (e.g. sources-download-constants.ts) or export through public facade

**Finding 2 — MEDIUM (BLOCKS): Test-seam parameter in public store action signature**
- **File**: `sources-download.ts:37` — `pageSize: number = DOWNLOAD_PAGE_SIZE` parameter
- **Issue**: pageSize only exists to inject small page size in unit tests; all production callers use default
- **Concrete risk**: Future developer passes pageSize thinking it optimizes download, introduces mismatch with backend's 5000 hard cap
- **Must fix before merge**: Move seam inside store (e.g. module-level `_pageSizeOverride` with `__setPageSizeForTesting()` helper), or mock the constant in tests rather than threading it through signature

**Finding 3 — LOW: Dead initial values in download-options.ts**
- **File**: `sources-api-option-helper/_internal/download-options.ts:36-37` sets skip/take
- **Issue**: Pagination loop always overrides with `{ ...options, skip, take: pageSize }`. Readers assume these values are used
- **Recommended fix**: Remove skip/take from getSourceDownloadApiOptions; loop owns pagination state

**Finding 4 — LOW: Hardcoded magic number 5000 in GQL layer**
- **File**: `download-gql.ts:57` — `option.take ?? 5000`
- **Issue**: Dead fallback that re-introduces backend cap as magic literal; constant not imported
- **Recommended fix**: Reference DOWNLOAD_PAGE_SIZE or remove fallback

**Finding 5 — NIT: PR documentation contradicts code**
- PR summary says "publishers/campaigns keep single-fetch behavior" but code has no type gate; while(true) loop runs for all three
- Tests at line 145 explicitly test 3 fetches for publishers
- **Recommended fix**: Update PR description and test comment to accurately say all three types paginate

**Finding 6 — NIT: Duplicated error-handling blocks**
- ModalHeader.vue:70-75 and MmmTopFilter.vue:73-78 have identical catch bodies
- Not blocking now, but if a third call site appears should move to composable (useDownloadErrorHandler)

**Architecture Strengths** ✅
- Pagination loop exit conditions (gotShortPage || gotAllRows) are correct and defensive
- Organic-only-on-first-page design clearly documented and tested
- PaidResultRow union kept private to file (correct scope)
- getSortSkipTakeString usage consistent with existing GQL stores
- Test suite thorough: verifies call counts, skip sequence, no boundary duplication, previously-dropped networks
- useNotificationsStore and useI18n both from @mos/core — no package boundary violations

---

## Analysis

### ✅ Strengths

1. **Fixes Real Data Loss**: Directly addresses the reported bug where datasets >5000 rows were silently truncated
2. **Fail-Loud Strategy**: Rather than silently truncating, the implementation throws on overflow, forcing surface-level visibility
3. **Comprehensive Tests**: 103 new test lines verify pagination correctness, including:
   - No boundary duplication or drops
   - Previously-missing networks are recovered
   - Organic is consumed once (not multiplied across pages)
4. **Good Documentation**: Extensive inline comments explain the pagination loop, the +2 slack, and the fail-loud guard
5. **Backward Compatible**: Response shape changes (availableResultsCount) are additive; don't break existing consumers
6. **Error UX**: User-facing error notifications instead of silent failures

### ⚠️ Concerns

#### 1. **Pagination Asymmetry** (Medium)
- **Paid (attribution)**: Paginated and accumulated across multiple requests
- **Organic (byOrganicPublisher)**: Consumed once (page 1 only), not re-fetched
- **Issue**: If organic exceeds pageSize, it fails loud. But this asymmetry is intentional (documented), though non-obvious to future maintainers
- **Recommendation**: Keep the fail-loud guard but add a comment explaining why organic is special (single-fetch is safe because organic grain is unique)

#### 2. **maxIterations Slack** (Low)
- `maxIterations = ceil(availableResultsCount / pageSize) + 2`
- **Justification (from comments)**: "+2 = slack so a final partial page or a slightly stale/under-reported availableResultsCount doesn't false-trip"
- **Risk**: If availableResultsCount is stale (reported higher than actual), the +2 slack hides legitimate infinite loops for very large datasets
- **Assessment**: Low risk in practice (backend likely updates count accurately); the fail-loud approach is better than silent truncation
- **Recommendation**: No change needed; +2 is reasonable defensively

#### 3. **GraphQL Query Construction** (Low)
- `getSortSkipTakeString()` is called per page, building the sort + skip/take string
- **Risk**: If the method doesn't preserve sort order correctly across pages, boundary rows could duplicate/drop
- **Mitigation**: Tests verify this doesn't happen; the backend's appendTiebreaker (mentioned in test comments) makes the sort order a total order
- **Recommendation**: Verify in code review that `getSortSkipTakeString()` preserves (primaryField, DATE) sort order

#### 4. **Memory Accumulation** (Low-Medium)
- All paid results accumulated in `paidResults` array before transformation
- **Risk**: For datasets of millions of rows, memory consumption could spike
- **Assessment**: Acceptable trade-off (CSV export is inherently memory-intensive); no streaming pipe available in this architecture
- **Recommendation**: Add a comment noting memory accumulation strategy; consider streaming for very large datasets in future

#### 5. **Type Union Correctness** (Low)
- New `PaidResultRow` union: `AttributionDataResult | PublisherTimeseriesDataResult | CampaignTimeseriesDataResult`
- **Risk**: Tight coupling across three download types; if one type's structure changes, all are affected
- **Assessment**: Tests cover mediaPartners and publishers; campaigns not tested
- **Recommendation**: Verify campaigns pagination works (test file only tests mediaPartners and publishers); may need additional test

#### 6. **Error Message Specificity** (Low)
- Two fail-loud errors are generic: "organic rows exceed page cap" and "exceeded max pagination iterations"
- **Risk**: Users won't know how to resolve; backend team won't know what triggered it
- **Assessment**: Better than silent truncation; more context could help
- **Recommendation**: Optional: add availableResultsCount value to error messages for debugging

### 🔒 Security

- **Input Validation**: skip/take are integers in range [0, 5000]. GraphQL schema likely validates; no injection risk visible
- **Authorization**: Data scope (account/app filtering) happens before pagination; no new auth gaps introduced
- **Information Disclosure**: Error messages are generic ("failed to download data"); no backend details leaked
- **DoS Risk**: maxIterations cap + fail-loud prevents infinite loops; backoff/retry logic not added (which is correct for this context)

### 🏗️ Architecture

- **Store Pattern**: Pagination loop is contained in sources-download.ts; doesn't leak into API layer
- **Package Boundaries**: New imports (useNotificationsStore, useI18n from @mos/core) are appropriate; no cross-product dependencies
- **Type Safety**: Response shape changes are properly typed; no `any` assignments visible
- **Responsivity**: No await inside computed/watch; pagination happens inside explicit function call

### 📊 Performance

- **Network**: Changes from 1 request to N requests (N = ceil(rows / 5000)). For typical datasets <50K rows, N ≤ 10 (acceptable)
- **Backend Load**: Multiple requests to same endpoint; no rate limiting visible in code (relies on upstream)
- **UI Responsiveness**: No progress indicator added (download is already async/backgrounded); no regression

### ✅ Testing

**Coverage:**
- ✅ Multi-page accumulation verified
- ✅ Page boundary deduplication checked
- ✅ Organic consumption (page 1 only) verified
- ✅ Off-by-one arithmetic validated (skip sequences [0, 2, 4] for pageSize=2)

**Gaps:**
- ❌ Campaigns pagination not explicitly tested (only mediaPartners and publishers)
- ❌ Edge case: availableResultsCount lower than actual results (underreporting) not tested
- ❌ Edge case: empty results set not tested (though loop breaks on short page, so likely safe)

---

## Recommendations

### Before Merge
1. **Campaigns Test**: Add explicit test for campaigns pagination to verify type union works correctly
2. **Code Review**: Verify `getSortSkipTakeString()` correctly preserves sort order across pages

### Optional / Nice-to-Have
1. **Error Context**: Add availableResultsCount to error messages for better debugging
2. **Organic Comment**: Clarify in code why organic is single-fetch (is it always <5000 rows in practice?)
3. **Memory Note**: Document memory accumulation strategy; future refactor to streaming

---

## Verdict

**REQUEST CHANGES** — **5 blocking issues** must be fixed before merge:
- 2 bugs (organic overflow guard, misleading comment)
- 1 security issue (availableResultsCount validation)
- 2 architectural issues (_internal import, test-seam parameter)

### MEDIUM-severity Issues (must fix)

**Bugs** (from Bug Hunter):
1. **Organic overflow guard uses wrong pageSize parameter**
   - `sources-download.ts:67` compares `availableResultsCount > pageSize` where pageSize is paid/attribution parameter, not organic cap
   - Calling `downloadData(type, ids, N)` with N < organic rows < 5000 triggers false throw
   - Fix: Use `organicResults.length < page.byOrganicPublisher.availableResultsCount` to directly detect truncation

2. **Comment contradicts code behavior**
   - `sources-download.spec.ts:15-17` says "publishers/campaigns keep single-fetch behavior" + references non-existent "type-gate"
   - Reality: All three types paginate identically; line 145 tests publishers pagination
   - Fix: Update comment to accurately say all three types paginate

**Security** (from Security Sentinel):
3. **Pagination safety cap disabled by non-finite `availableResultsCount`** 
   - If server returns `undefined` for `availableResultsCount`, `maxIterations` becomes `NaN` and safety cap never fires
   - Could cause infinite loop accumulating unlimited rows if backend returns exactly pageSize rows continuously
   - Fix: Validate `availableResultsCount` is finite non-negative number; add hard unconditional cap (e.g., 200 iterations)

**Architecture** (from Architecture Reviewer):
4. **Remove `_internal` boundary violation**
   - Move DOWNLOAD_PAGE_SIZE to co-located file or export through public facade
   - Status: Must fix

5. **Remove test-seam parameter from public API**
   - pageSize parameter only exists for tests; leaks internal detail into Pinia action signature
   - Move to module-level _pageSizeOverride with __setPageSizeForTesting() helper
   - Status: Must fix

### Non-Blocking Issues (recommended before merge)

From **Security Sentinel** (LOW):
- Raw Error object written to console in production (reveals pagination logic + backend details to any user with DevTools)
- Missing null guard on `page.attribution.results` (throws TypeError if API returns null)
- Pre-existing GraphQL injection risk in `getSortSkipTakeString` (affected by new call site)

From **Performance Analyst** (MEDIUM):
- byOrganicPublisher fetched on every page, consumed only once (9 wasted queries for 50K dataset)
- Sequential O(N×RTT) fetching; parallelizable after page 1

From **Architecture Reviewer** (LOW/NIT):
- Dead skip/take values in download-options.ts
- Magic number 5000 fallback in GQL layer
- PR documentation drift (publishers/campaigns actually paginate, not single-fetch)
- Duplicated error-handling blocks across two components

### Summary

The PR fixes real data loss (silent truncation of datasets >5000 rows) with correct pagination logic. However, **five critical issues block merge**:

**Bugs** (from Bug Hunter):
1. **MEDIUM**: Organic overflow guard uses wrong pageSize parameter (compares against paid pageSize instead of organic cap)
2. **MEDIUM**: Misleading comment contradicts code (says single-fetch for publishers/campaigns; all three types paginate)

**Security** (from Security Sentinel):
3. **MEDIUM**: Untrusted `availableResultsCount` could disable pagination safety cap, allowing infinite loops

**Architecture** (from Architecture Reviewer):
4. **MEDIUM**: `_internal/` import violates module boundaries
5. **MEDIUM**: Test-seam parameter in public API violates contract

Additionally, **4 test gaps** should be addressed (organic/maxIterations fail-loud throws never triggered; gotAllRows terminator untested; error notification coverage missing).

After these fixes, the PR is ready to ship. Performance inefficiencies (organic wastage, sequential fetching) and security hardening (error logging, null guards, GraphQL injection) are acceptable follow-ups.

---

## Specialist Consensus

**Performance Analyst**: No CRITICAL or HIGH issues. Loop termination is sound. Two MEDIUM findings (organic wastage, sequential fetching) are real but not blocking given the alternative is silent data loss.

**Architecture Reviewer**: Two blocking architectural issues must be fixed before merge:
1. Remove `_internal` boundary violation (DOWNLOAD_PAGE_SIZE import)
2. Remove test-seam pageSize parameter from public API

**Security Sentinel**: One MEDIUM blocking issue (availableResultsCount validation) + three LOW issues (error logging, null guards, GraphQL injection pattern)

**Bug Hunter**: Two bugs found (organic overflow guard parameter, misleading comment) + four test gaps (organic throw, maxIterations throw, gotAllRows terminator, error notification coverage); core pagination logic sound

---

## Files Changed Summary

| File | Change | Risk |
|------|--------|------|
| ApiOtions.ts | Added skip/take fields | Low (additive) |
| SourcesApiResponseInterface.ts | Added availableResultsCount | Low (additive) |
| download-gql.ts | Modified query construction | Medium (sort order preservation) |
| sources-download.ts | Pagination loop logic | Medium (boundary conditions) |
| sources-download.spec.ts | Added 103 lines of tests | None (test-only) |
| ModalHeader.vue | Added error notification | Low (additive) |
| MmmTopFilter.vue | Added error notification | Low (additive) |
| download-options.ts | Added constant + initial skip/take | Low (additive) |
| Locale files (7 files) | Added translation keys | Low (additive) |



</details>

<details><summary>Raw review — Claude CI</summary>

### Reviewing PR: fix: enable mediapartners paginated download

#### Todo List
- [x] Read resolved issues file
- [x] Read main implementation file (sources-download.ts)
- [x] Read test file
- [x] Review API changes
- [x] Analyze pagination logic for correctness
- [x] Check for edge cases and robustness issues
- [x] Verify test coverage
- [x] Provide comprehensive review feedback

---

### Review Summary

**Overall Assessment**: Strong implementation. The pagination logic is correct, well-tested, and handles edge cases appropriately. All previously flagged issues have been addressed by the PR owner.

### ✅ Strengths

1. **Excellent Test Coverage**: The test suite comprehensively validates:
   - Pagination arithmetic (skip offsets: 0, 2, 4)
   - Boundary conditions (short pages, exact multiples)
   - The actual bug fix (TikTok, Liftoff, Rokt, Snapchat, Tatari Brand now included)
   - No duplication across page boundaries
   - Organic data consumed only once (not multiplied across pages)

2. **Robust Error Handling**: 
   - Fail-loud approach prevents silent data truncation
   - Organic overflow guard prevents incomplete exports
   - MaxIterations guard prevents infinite loops
   - Proper error propagation to UI with user notifications

3. **Clean Pagination Logic**: 
   - Correct skip arithmetic (`skip += pageSize`)
   - Dual exit conditions (`gotShortPage || gotAllRows`)
   - Proper accumulation of results across pages

### ⚠️ Issues Found

#### **Medium Priority**: Backend Inefficiency

**File**: `packages/advertiser/src/views/Analytics/stores/analytics2/minsights/api/attr-api/sources/sources-gql/_internal/download-gql.ts`

**Issue**: The GraphQL query (lines 82-99) requests `byOrganicPublisher` data on **every** pagination page, but the frontend only consumes it from page 1 (sources-download.ts:66). This aligns with the deep dive finding: "the backend executes a full organic publisher SQL query on every pagination page, but the frontend discards organic results after page 1."

**Impact**: For an N-page download, the backend executes N-1 unnecessary organic queries. For large exports (10+ pages), this is significant wasted DB load.

**Recommendation**: Conditionally include the `byOrganicPublisher` alias only when `skip === 0`:

```typescript
const buildDownloadPayload = (
  appIds: string[],
  option: DownloadMediaPartnersGqlPayloadOption,
  extraMainFields: string[] = [],
): GraphQLPayload => {
  // ... existing code ...
  
  const organicAlias = option.skip === 0 || option.skip === undefined
    ? `byOrganicPublisher: attribution(
         options: {
           ${commonGqlStore.getAppIds(appIds)}
           ${commonGqlStore.getStartEnd(option)}
           periodicity: ${option.periodicity}
           ${commonGqlStore.getNetworkIds([option.organicNetworkId])}
           ${commonGqlStore.getRegionIds(option.regionIds)}
           ${commonGqlStore.getPublisherNames(option.publisherNames)}
           ${commonGqlStore.getCampaignNames(option.campaignNames)}
         }
       ) {
         results {
           date
           publisherName
           ${metricStrings}
         }
         availableResultsCount
       }`
    : '';

  const PAYLOAD = gql`
    query {
      attribution(...) { ... }
      ${organicAlias}
    }
  `;
  // ...
}
```

**Note**: This requires updating the response type to make `byOrganicPublisher` optional when `skip > 0`, and the consumer to handle the undefined case gracefully (defaulting to empty results).

[Fix this →](https://claude.ai/code?q=Optimize%20GraphQL%20query%20in%20download-gql.ts%20to%20only%20request%20byOrganicPublisher%20when%20skip%3D%3D0.%20Update%20response%20type%20to%20make%20it%20optional%20and%20handle%20undefined%20in%20sources-download.ts%20line%2066&repo=Kochava/frontend-mos)

---

#### **Low Priority**: Generic Error Message

**Files**: 
- `packages/advertiser/src/views/Analytics/MmmInsights/MmmTopFilter/MmmTopFilter.vue:77`
- `packages/advertiser/src/views/Analytics/MmmInsights/MmmTabComponent/Sources/MediaPartnerDetailsModal/ModalHeader/ModalHeader.vue:75`

**Issue**: The error notification uses a generic translation key `"mmm_insights.errors.failed_download"` which resolves to "Failed to download data". This doesn't distinguish between different failure modes:
- Organic overflow (>5000 organic publishers)
- Max iterations exceeded (corrupted backend count)
- Network/API errors

**Impact**: Users see the same error message regardless of the actual problem, making it harder to diagnose issues or take corrective action.

**Recommendation**: Either:
1. Parse the error message and show type-specific translations, or
2. Include the actual error message in the notification (already done via `debug: error`, but users may not see debug info)

**Example**:
```typescript
notificationsStore.addNotification({
  type: "error",
  text: error instanceof Error && error.message.includes("organic publisher rows")
    ? t("mmm_insights.errors.organic_overflow")
    : t("mmm_insights.errors.failed_download"),
  debug: error,
});
```

---

### 📝 Minor Observations (Non-Blocking)

1. **Type Safety**: The `networkIds?: string[]` parameter is only meaningful for certain download types but isn't enforced at the type level. This is acceptable given the current architecture but could be refined with discriminated unions if this API grows.

2. **Test Organization**: The test file has excellent inline documentation (lines 4-18) explaining the two concerns being tested. This is a good pattern.

3. **Magic Number Documentation**: The `+2` buffer in maxIterations calculation (line 75) now has a clear comment explaining the rationale. Good improvement.

---

### 🎯 Verdict

**Approve with optional follow-up**: The core functionality is correct, well-tested, and ready to ship. The backend inefficiency issue is worth addressing but doesn't block this PR since:
- It doesn't introduce a bug (functionality is correct)
- It's an optimization opportunity, not a correctness issue
- The deep dive analysis flagged it as [MEDIUM], not [HIGH]

Consider creating a follow-up issue to optimize the GraphQL query for better backend performance.

---

</details>

<details><summary>Classifier output</summary>

{'bucket': 'hard'}

</details>

### Kochava/watson#879 (difficulty: easy)

| Criterion | skwad-cli | Claude CI |
| ---|---|--- |
| Issue Detection | 3 | 1 |
| Actionability | 3 | 2 |
| Severity Accuracy | 1 | 1 |
| Coverage | 3 | 1 |
| Signal-to-Noise | 1 | 2 |
| Depth | 2 | 1 |
| Novel & Substantive Findings | 2 | 0 |
| **Total** | **15** | **8** |

### Verification Summary

| Review | Verified | Unverified | Contradicted | Non-falsifiable | Rate | Tool Calls | Ungrounded | Grounding |
| ---|---|---|---|---|---|---|---|--- |
| Skwad | 42 | 10 | 4 | 3 | 71% | 75 | 4 | 91% |
| Claude CI | 19 | 7 | 0 | 5 | 61% | 75 | 3 | 84% |

<details><summary>Judge reasoning (skwad-cli)</summary>

**Issue Detection**: A identifies five genuine issues that are supported by the code: whitespace-only tokens still pass the guard, the new GPIR test omits any install_time assertion, an older test only checks install_id despite exercising the guarded path, the GPIR fixture carries an AttributionMethod value that the builder never reads, and there is a hardcoded Facebook token in the file. One additional scenario description is wrong, and some impact/performance claims are not evidenced.
**Actionability**: Most concrete findings come with direct fixes: use TrimSpace, add a missing assertion, strengthen an existing test, or document/remove misleading fixture data.
**Severity Accuracy**: Prioritization is mixed: the whitespace edge case is overstated as a blocking security issue, and some minor test gaps are labeled medium, although the hardcoded token is appropriately severe.
**Coverage**: The review ranges across correctness, test coverage, security, maintainability, and some architectural concerns. It catches more of the relevant surface area than B, though several peripheral claims are not evidence-backed.
**Signal-to-Noise**: There is meaningful signal, but it is diluted by speculative business-impact/performance estimates, an out-of-scope pre-existing secret, and one concrete misstatement about an existing test fixture.
**Depth**: The stronger parts reason through control flow in buildFBEventEnrichmentParams, compare test fixtures to runtime behavior, and connect the guard to struct semantics and adjacent code paths. That is solid cross-file reasoning, but not deep architectural analysis throughout.
**Novel & Substantive Findings**: A contributes two substantive findings that B does not surface: the new GPIR test misses an install_time assertion, and a pre-existing test only asserts install_id while exercising the same guarded path.

</details>

<details><summary>Claim trace (Skwad)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| The PR adds `&& details.AuditingToken != ""` before building `attribution_source | verified | Read | cmd/watson/facebook/reconcile.go:1069 if details.AdID != "" && details.AuditingToken != "" { |
| In the FBAttributionDetails path, the builder appends `is_fb`, `event_id`, and ` | verified | Read | cmd/watson/facebook/reconcile.go:1052 params += "&is_fb=true" params += "&event_id=" + url.QueryEscape(request.RequestID |
| Facebook devices without attribution details still follow the `else if request.I | verified | Read | cmd/watson/facebook/reconcile.go:1088 } else if request.IsFB { // Device is FB-attributed but no details available (rest |
| The PR adds a regression test named `Test_buildFBEventEnrichmentParams_GPIR_Empt | verified | Read | cmd/watson/facebook/fb_event_enrichment_url_test.go:146 func Test_buildFBEventEnrichmentParams_GPIR_EmptyAuditingToken_N |
| The new GPIR test checks that `is_fb`, `event_id`, and `install_id` remain prese | verified | Read | cmd/watson/facebook/fb_event_enrichment_url_test.go:174 assert.Contains(t, params, "&is_fb=true", "is_fb should still be |
| The existing full-details test still covers the non-empty-token path by setting  | verified | Read | cmd/watson/facebook/fb_event_enrichment_url_test.go:67 AdID: "ad-789", ClickTime: 1709913600, AuditingToken: "token-xyz" |
| Whitespace-only auditing tokens are not guarded; the current code checks only `! | verified | Read | cmd/watson/facebook/reconcile.go:1069 if details.AdID != "" && details.AuditingToken != "" { attrSource := []map[string] |
| The new GPIR regression test never asserts `install_time=1709800000`. | verified | Grep | grep:install_time=1709800000 |
| `Test_buildFBEventEnrichmentParams_InstallID_UsesDeviceID` only asserts install_ | verified | Read | cmd/watson/facebook/fb_event_enrichment_url_test.go:49 FBAttributionDetails: &structs.FBAttributionDetails{ AdID: "123", |
| `Test_buildFBEventEnrichmentParams_InstallID_UsesDeviceID` has explicit `Auditin | contradicted | Read | cmd/watson/facebook/fb_event_enrichment_url_test.go:49 FBAttributionDetails: &structs.FBAttributionDetails{ AdID: "123", |
| `buildFBEventEnrichmentParams` never references `AttributionMethod`. | verified | Grep | grep:AttributionMethod |
| There is a hardcoded Facebook token at `reconcile.go:44`. | verified | Read | cmd/watson/facebook/reconcile.go:44 token = "248602981818776\|bImoP43x8UgCw1ky-J_-BP83ByA" |
| `FBAttributionDetails.AuditingToken` lacks any field-level documentation about G | verified | Grep | grep:AuditingToken.*//\|//.*AuditingToken\|GPIR |
| `facebookdeeplink` and `facebook-api` do not contain analogous `attribution_sour | verified | Grep | grep:source_auditing_token\|attribution_sources\|auditing_token |
| Skipping the block saves roughly 300-400 bytes of heap and 120-150 bytes of payl | unverified | Read | The repository shows the guarded code path, but it contains no measurements or calculations that substantiate the byte e |
| Meta treats `source_auditing_token=""` as a failed validation and this fix resto | unverified | Read | That behavior is described only in comments; the checked-in code and tests do not provide evidence of Meta's external va |
| No blocking issues were found overall. | non_falsifiable | Read | This is an overall judgment rather than a concrete, code-bound claim with a specific referent. |

</details>

<details><summary>Claim trace (Claude CI)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| The fix is the surgical addition of `&& details.AuditingToken != ""` in `reconci | verified | Read | cmd/watson/facebook/reconcile.go:1069 if details.AdID != "" && details.AuditingToken != "" { |
| The test file includes a dedicated regression test for the GPIR empty-token scen | verified | Read | cmd/watson/facebook/fb_event_enrichment_url_test.go:146 func Test_buildFBEventEnrichmentParams_GPIR_EmptyAuditingToken_N |
| That GPIR test verifies both positive conditions (`is_fb`, `event_id`, `install_ | verified | Read | cmd/watson/facebook/fb_event_enrichment_url_test.go:174 assert.Contains(t, params, "&is_fb=true", "is_fb should still be |
| The non-empty-token path is still covered by an existing test that sets `Auditin | verified | Read | cmd/watson/facebook/fb_event_enrichment_url_test.go:67 AdID: "ad-789", ClickTime: 1709913600, AuditingToken: "token-xyz" |
| Whitespace-only auditing tokens are an uncovered edge case because the code does | verified | Read | cmd/watson/facebook/reconcile.go:1069 if details.AdID != "" && details.AuditingToken != "" { attrSource := []map[string] |
| `attrSource` should be moved to the top of the function block per `go-code-style | unverified | Grep | The repository search did not surface a `go-code-style-guide.md` file or another checked-in standard that would verify t |
| The performance impact is minimal because the code change is only one extra stri | verified | Read | cmd/watson/facebook/reconcile.go:1069 if details.AdID != "" && details.AuditingToken != "" { |
| Test coverage is excellent/comprehensive. | unverified | Read, Grep | The repository confirms the presence of a targeted regression test, but adequacy claims at the level of 'excellent/compr |
| No security vulnerabilities are introduced by this change. | non_falsifiable | Read | This is a broad security judgment rather than a concrete, code-bound claim tied to a specific referent. |
| SRN-attributed devices are unaffected. | verified | Read | cmd/watson/facebook/fb_event_enrichment_url_test.go:67 AdID: "ad-789", ClickTime: 1709913600, AuditingToken: "token-xyz" |
| The fix is ready for production deployment. | non_falsifiable | Read | Production-readiness is an overall judgment call, not a specific falsifiable code claim. |

</details>

<details><summary>Raw review — skwad-cli</summary>

# PR #879 Code Review: Fix attribution_sources for GPIR devices

**Title:** fix: skip attribution_sources when auditing_token is empty (GPIR devices)

**Status:** ✅ APPROVED — No blocking issues found

---

## Summary

This PR fixes a regression introduced in PR #839 where GPIR-attributed devices (those using the facebook-install-referrer module) were incorrectly sending empty `source_auditing_token` values to Meta's Graph API, causing Meta to reject the attribution claim. The fix adds a simple guard to skip `attribution_sources` entirely when `AuditingToken` is empty, restoring pre-#839 behavior for GPIR devices while preserving correct behavior for SRN-attributed devices.

**Impact:** Restores claim win rate for GPIR-attributed devices (estimated 20-30% of FB attribution volume per PR description)

---

## Changes Analysis

### File: `cmd/watson/facebook/reconcile.go` (lines 1060–1069)

**Change:** Added `&& details.AuditingToken != ""` condition to the `attribution_sources` serialization block.

```go
// Before:
if details.AdID != "" {
    attrSource := [...]
    // ... serialize and append to params
}

// After:
if details.AdID != "" && details.AuditingToken != "" {
    attrSource := [...]
    // ... serialize and append to params
}
```

**Analysis:**

✅ **Correct Logic**
- GPIR devices have `AdID` (from utm_content) but no `AuditingToken` (Meta's SAN API is never called)
- The guard correctly prevents sending empty tokens
- The graceful degradation is sound: GPIR devices fall back to `is_fb=true` with `event_id` and `install_id` but no `attribution_sources`, matching pre-#839 behavior

✅ **No Unintended Side Effects**
- SRN-attributed devices are unaffected (they have both `AdID` and valid `AuditingToken`)
- The condition is narrow and doesn't affect other code paths
- Facebook devices without attribution details still follow the `else if request.IsFB` path (lines 1088–1092)

✅ **Well-Documented**
- Excellent inline comment explaining the GPIR limitation, Meta's validation behavior, and the rationale
- References Asana ticket for full context
- Clear enough for future maintainers

---

### File: `cmd/watson/facebook/fb_event_enrichment_url_test.go` (lines 146–184)

**Change:** Added regression test `Test_buildFBEventEnrichmentParams_GPIR_EmptyAuditingToken_NoAttributionSources`.

**Analysis:**

✅ **Comprehensive Test Coverage**
- Test setup mirrors real GPIR scenario: `AdID` present, `AuditingToken` empty, `AttributionMethod: "GPIR"`
- All four positive assertions verify graceful degradation:
  - `is_fb=true` is still sent
  - `event_id` is still sent  
  - `install_id` is still sent
- Two negative assertions verify the fix:
  - `attribution_sources` must NOT appear
  - `source_auditing_token` must NOT appear (catches both empty and malformed cases)

✅ **Test Pattern Consistency**
- Follows the same assertion pattern as existing tests (`Test_buildFBEventEnrichmentParams_NoAdID_NoAttributionSources`, etc.)
- Excellent inline documentation explaining the GPIR limitation and the expected behavior
- Test name clearly indicates the scenario being tested

✅ **Edge Cases Covered**
- Verifies the fix doesn't accidentally suppress other fields
- Verifies negative case with two assertions (belt and suspenders approach is good here)

---

## Verification Checklist

| Dimension | Finding | Severity |
|-----------|---------|----------|
| **Correctness** | Fix correctly addresses root cause (empty token → validation failure). Logic is sound. | ✅ PASS |
| **Test Coverage** | New regression test is comprehensive and follows project patterns. | ✅ PASS |
| **Side Effects** | No unintended impact on SRN or other FB attribution flows. | ✅ PASS |
| **Documentation** | Excellent inline comments and test documentation. | ✅ PASS |
| **Security** | No security implications. Empty string check is appropriate. | ✅ PASS |
| **Performance** | Negligible: one additional string comparison in hot path. Slightly improves payload size to Meta. | ✅ PASS |
| **Architecture** | Fix is in the right place (buildFBEventEnrichmentParams). No deeper refactoring needed. | ✅ PASS |

---

---

## Architecture Review

### Findings (Architecture Reviewer)

**✅ Verdict: Architecturally sound. Fix is at the correct layer.**

The fix belongs in `buildFBEventEnrichmentParams()`, which owns the Meta API contract. GPIR module doesn't need to know about Meta's token validation rules. Empty string for `AuditingToken` is a valid, meaningful state—not missing data, just no token ever issued.

**[LOW] `reconcile.go:1069` — Token guard vs. AttributionMethod guard**
- Could have used `details.AttributionMethod != "GPIR"` but token-based approach is better
- Doesn't couple guard to a specific method string (no constant/enum exists for "GPIR")
- Handles any future non-SAN attribution method with no token
- Design choice is sound

**[LOW] `structs/structs.go` — Empty-string-as-sentinel is undocumented at struct level**
- `AuditingToken = ""` now has semantic meaning: "no token from Meta for this device class"
- Inline comments in `reconcile.go` are excellent, but struct field has no doc comment
- Low risk today (only one consumer), but future callers (cache writer, XNA ping builder) could miss the invariant
- **Suggested improvement (non-blocking):** Add field comment to struct documenting GPIR behavior

**[NIT] Existing test overlap**
- `Test_buildFBEventEnrichmentParams_InstallID_UsesDeviceID` omits `AuditingToken`, now exercises the guard
- Doesn't assert on `attribution_sources`, so doesn't confirm behavior either way
- New dedicated GPIR test covers it properly—no action needed

**Architecture checks:**
- ✅ Right layer: API-contract enforcement in URL-builder, not GPIR module
- ✅ Waterfall alignment: Graceful degradation matches existing cache-miss pattern
- ✅ No similar issues elsewhere: Checked `facebookdeeplink/reconcile.go` and `facebook-api/` — no analogous risk
- ✅ No architectural debt: PR makes empty-token case explicit and documented
- ✅ Condition placement: Co-located with `attribution_sources` construction, minimal blast radius

---

## Performance Review

### Findings (Performance Analyst)

**NIT — `reconcile.go:1069` — Additional string comparison**
- Cost: One `!= ""` check per request when `FBAttributionDetails` is non-nil
- Go's `!= ""` on strings is O(1) and nanoseconds-level; no measurable impact
- **Verdict:** Non-issue

**LOW (positive) — `reconcile.go:1070–1079` — Allocation savings for GPIR devices**
- Skipped when `AuditingToken == ""`: slice allocation, map allocation, `json.Marshal()`, `url.QueryEscape()`, string concatenation
- Per-request savings: ~300–400 bytes of short-lived heap
- GC pressure reduction is real but modest for GPIR volume (Android-only subset)
- **Verdict:** Worth having, doesn't move the needle alone

**LOW (pre-existing) — String concatenation pattern**
- `buildFBEventEnrichmentParams` uses repeated `params += "..."` throughout
- 6–8 allocations per call could be halved with `strings.Builder`
- **Not introduced by this PR** — separate ticket if needed

**Payload Impact:** ~120–150 bytes removed per GPIR request to Meta. TCP/HTTP framing overhead dwarfs this.

**Caching:** No implications — Facebook API responses not cached.

**Scale:** O(1) addition and O(1) skip. Holds at any volume.

**Verdict:** ✅ **No performance concerns. Approve from performance perspective.**

---

## Security Review

### Findings (Security Sentinel)

**Verdict: Approve with minor fix recommendation.**

**[LOW] `reconcile.go:1069` — Whitespace-only tokens not guarded**
- Current check `details.AuditingToken != ""` doesn't catch whitespace-only tokens (e.g., `"   "`)
- Whitespace token would bypass the guard and get sent to Meta, triggering the same validation failure
- **Recommendation: Use `strings.TrimSpace(details.AuditingToken) != ""`** — simple 1-line fix
- **Severity:** LOW but easy to fix before merge

**[HIGH] (pre-existing, not introduced by PR): Hardcoded FB access token**
- `reconcile.go:44` has hardcoded Facebook access token constant
- Not introduced by this PR, should be tracked as separate remediation ticket
- **Out of scope for this PR**

**[NIT] No test coverage for whitespace-only edge case**
- Existing GPIR test doesn't cover `AuditingToken = "   "`
- Should add test case: `Test_buildFBEventEnrichmentParams_GPIR_WhitespaceAuditingToken_NoAttributionSources`

**Core change is sound:**
- ✅ Guard correctly prevents sending empty tokens to Meta for GPIR devices
- ✅ Trust boundary appropriate (data flows from Control, a trusted upstream)
- ✅ No injection risks — all values JSON-marshaled and URL-escaped before use
- ✅ No data leakage in graceful degradation path

---

## Questions for Author (Non-Blocking)

1. **Verification:** Has this been tested against real GPIR devices in a test environment or QA? The fix looks correct, but end-to-end validation would be valuable given the impact on claim win rate.

2. **Rollout:** Meta's validation behavior should be monitored after deployment to confirm the fix actually resolves the XNA drop observed starting April 20.

3. **Future Prevention:** Consider adding explicit documentation in the `FBAttributionDetails` struct (if it exists) that `AuditingToken` is optional and empty for GPIR devices, to prevent similar issues in future code.

---

---

## Summary of Specialist Reviews

| Specialist | Verdict | Key Finding |
|-----------|---------|------------|
| **Performance Analyst** | ✅ Approve | Net positive: O(1) overhead for non-GPIR, 300–400B allocation savings per GPIR request |
| **Architecture Reviewer** | ✅ Approve | Fix at correct layer; token-based guard better than method-string coupling |
| **Security Sentinel** | ⚠️ Approve with fix | **MUST fix whitespace-only tokens before merge** — use `strings.TrimSpace()` |
| **Bug Hunter** | ⚠️ Approve with fixes | Production code correct; test coverage gaps: 2 MEDIUM findings |

---

## 🔴 BLOCKING ISSUE

**Security Sentinel flagged:** The guard `details.AuditingToken != ""` does NOT catch whitespace-only tokens (e.g., `"   "`). These would bypass the guard and trigger the same validation failure the PR was designed to prevent.

**Required Fix:** Change line 1069 from:
```go
if details.AdID != "" && details.AuditingToken != "" {
```
to:
```go
if details.AdID != "" && strings.TrimSpace(details.AuditingToken) != "" {
```

This is a **1-line fix**. After this change, the PR is ready to merge.

---

## Test Coverage Review (Bug Hunter)

**Production code verdict:** ✅ Correct — The `AdID != "" && AuditingToken != ""` guard precisely implements the stated intent with no regression risk.

**Test findings:**

**[MEDIUM] `fb_event_enrichment_url_test.go:146` — Incomplete GPIR test assertions**
- Test sets `InstallTime: 1709800000` but never asserts it's present in output
- Only verifies absences (`attribution_sources` NOT sent), not presences
- Risk: Future change breaking `install_time` generation wouldn't be caught
- **Suggested fix:** Add `assert.Contains(t, params, "install_time=1709800000")`

**[MEDIUM] `fb_event_enrichment_url_test.go:42` — Silent spectator test**
- `Test_buildFBEventEnrichmentParams_InstallID_UsesDeviceID` has `AdID: "123"` + `AuditingToken: ""` (the exact GPIR scenario)
- Makes zero assertions about `attribution_sources` 
- This test masked the pre-existing bug — it passed before AND after the fix with no signal
- **Suggested fix:** Add `assert.NotContains(t, params, "attribution_sources")` or document why `AuditingToken` is empty

**[LOW] `fb_event_enrichment_url_test.go:168` — Misleading fixture field**
- Test sets `AttributionMethod: "GPIR"` but code never reads this field
- Guard is purely `AuditingToken == ""`, not method-based
- Risk: Future maintainers might search for "GPIR" logic and be confused
- **Suggested fix:** Add comment: `// documentary only — guard is based on AuditingToken, not AttributionMethod`

---

## Additional Recommendations (Non-Blocking)

1. **Add test case for whitespace-only token:** `Test_buildFBEventEnrichmentParams_GPIR_WhitespaceAuditingToken_NoAttributionSources`

2. **Architecture:** Add field doc comment to `FBAttributionDetails.AuditingToken` struct explaining GPIR behavior (suggested by Architecture Reviewer)

3. **Pre-existing issue (separate ticket):** Hardcoded FB access token at `reconcile.go:44` should be externalized (flagged by Security Sentinel, not introduced by this PR)

---

## Recommendation

**⚠️ CONDITIONAL APPROVE** — Approve after fixing the blockers identified by Security & QA teams.

This is a focused, correct fix for a real regression affecting 20–30% of FB attribution volume. Production code is sound. Test coverage has gaps that must be addressed.

### Blockers (Fix Before Merge)

1. **[Security Sentinel]** Line 1069: Change `details.AuditingToken != ""` to `strings.TrimSpace(details.AuditingToken) != ""`
   - Prevents whitespace-only tokens from bypassing guard
   - 1-line fix

2. **[Bug Hunter]** Line 146 test: Add assertion for positive case
   - Add `assert.Contains(t, params, "install_time=1709800000")`
   - Ensures future changes don't break `install_time` for GPIR devices

3. **[Bug Hunter]** Line 42 test: Document or guard existing coverage gap
   - Add `assert.NotContains(t, params, "attribution_sources")` 
   - OR add comment explaining `AuditingToken: ""` is intentional

### Nice-to-Haves (After Merge)

- [ ] Add dedicated test for whitespace-only `AuditingToken` edge case
- [ ] Add struct doc comment for `AuditingToken` field (architecture concern)
- [ ] Track pre-existing hardcoded FB token issue (`reconcile.go:44`) in separate ticket
- [ ] Add comment to line 168 explaining why `AttributionMethod` field is documentary-only

### Post-Merge Steps

1. Deploy to QA/Production
2. Monitor Meta XNA claim volume for GPIR devices to confirm recovery
3. Verify no regression in SRN attribution
4. Follow up on pre-existing hardcoded token ticket

---

**Reviewed by:** Review Coordinator + Full Specialist Panel  
- Performance Analyst: ✅ Approve (no perf concerns)
- Architecture Reviewer: ✅ Approve (correct layer, sound design)
- Security Sentinel: ⚠️ Approve with 1-line fix (whitespace guard)
- Bug Hunter: ⚠️ Approve with test fixes (coverage gaps)

**Review Date:** 2026-06-22  
**Effort:** Low — focused single-condition fix; test improvements are straightforward


</details>

<details><summary>Raw review — Claude CI</summary>

### PR Review: Fix for GPIR Empty Auditing Token

#### Todo List
- [x] Read and analyze the changed files
- [x] Review against Kochava's coding standards
- [x] Analyze the fix logic and potential issues
- [x] Check test coverage adequacy
- [x] Provide comprehensive feedback

## Summary

This PR addresses a critical issue where GPIR-attributed devices were sending empty `auditing_token` values to Meta's FB API, causing reduced claim win rates. The fix correctly implements the business logic to only send `attribution_sources` when a valid auditing token exists.

## Code Quality Review

### ✅ **Positive Aspects**

1. **Precise Fix**: The change is surgical and focused - adding `&& details.AuditingToken != ""` to line 1069 in `reconcile.go:1069` 
2. **Excellent Documentation**: The comment block (lines 1060-1068) provides comprehensive context about why this change is needed
3. **Comprehensive Test Coverage**: The test file includes a specific regression test for the GPIR empty token scenario with excellent documentation
4. **Business Logic Correctness**: The fallback to graceful degradation (is_fb=true without attribution_sources) matches pre-PR-#839 behavior

### ⚠️ **Areas for Improvement**

#### 1. **Code Standards Compliance**
- **Follow Kochava's Go Standards**: The code generally adheres to Kochava's Go style guide
- **Variable Declaration**: Consider moving the `attrSource` variable declaration to the top of the function block per Kochava standards (line 7 of go-code-style-guide.md)

#### 2. **Potential Logic Issues**
- **Edge Case**: What happens if `details.AdID` is non-empty but `details.AuditingToken` is whitespace-only? Consider using `strings.TrimSpace()` for robust validation:
  ```go
  if details.AdID != "" && strings.TrimSpace(details.AuditingToken) != "" {
  ```

#### 3. **Security & Performance**
- **No Security Issues**: The change doesn't introduce security vulnerabilities
- **Performance**: Minimal impact - just an additional string comparison

## Test Coverage Analysis

### ✅ **Excellent Test Coverage**

The test file includes a comprehensive GPIR-specific test case:
- **Test Name**: `Test_buildFBEventEnrichmentParams_GPIR_EmptyAuditingToken_NoAttributionSources` 
- **Business Context**: Properly documents why GPIR devices have empty auditing tokens
- **Assertions**: Verifies both positive (is_fb=true) and negative (no attribution_sources) conditions
- **Edge Case Coverage**: Tests the specific GPIR attribution scenario

### 💡 **Suggested Test Improvements**

1. **Add Whitespace Test**: Test with whitespace-only auditing token
2. **Integration Test**: Consider an integration test to verify the full request flow
3. **Performance Test**: Measure performance impact of the additional condition

## Security & Business Impact

### ✅ **Security**
- No security vulnerabilities introduced
- Proper handling of empty/null values
- No exposure of sensitive data

### ✅ **Business Impact**  
- **Positive**: Fixes the install XNA drop issue reported by Meta
- **Low Risk**: Falls back to graceful degradation for GPIR devices
- **SRN Compatibility**: No impact on SRN-attributed devices

## Recommendations

### **High Priority**
1. **Consider Trim Validation**: Add `strings.TrimSpace()` for robust empty string detection
2. **Documentation**: The inline comments are excellent - no changes needed

### **Medium Priority** 
1. **Variable Declaration**: Move `attrSource` to function top per Kochava Go standards
2. **Add Whitespace Test**: Test case for whitespace-only auditing tokens

### **Low Priority**
1. **Metrics**: Consider adding metrics to track GPIR vs SRN attribution sources sent

## Overall Assessment

**Recommendation: ✅ APPROVE**

This is a well-executed fix that:
- Addresses a specific, documented business problem
- Includes excellent test coverage
- Follows good practices for backward compatibility  
- Has minimal risk and clear rollback strategy

The fix is ready for production deployment. The suggested improvements are minor optimizations that could be addressed in follow-up work.

</details>

<details><summary>Classifier output</summary>

{'bucket': 'easy'}

</details>

### Kochava/watson#890 (difficulty: easy)

| Criterion | skwad-cli | Claude CI |
| ---|---|--- |
| Issue Detection | 0 | 0 |
| Actionability | 0 | 0 |
| Severity Accuracy | 0 | 0 |
| Coverage | 0 | 0 |
| Signal-to-Noise | 1 | 0 |
| Depth | 1 | 1 |
| Novel & Substantive Findings | 0 | 0 |
| **Total** | **2** | **1** |

### Verification Summary

| Review | Verified | Unverified | Contradicted | Non-falsifiable | Rate | Tool Calls | Ungrounded | Grounding |
| ---|---|---|---|---|---|---|---|--- |
| Skwad | 8 | 21 | 2 | 2 | 24% | 76 | 2 | 80% |
| Claude CI | 17 | 9 | 7 | 9 | 40% | 76 | 2 | 91% |

<details><summary>Judge reasoning (skwad-cli)</summary>

**Issue Detection**: The review identified no genuine defects and missed the main correctness risk in this PR: the new field is added to `WatsonRequest`, but the proto/reclog conversion path in `cmd/watson/protoutil` still has no `TraceID` handling, so the review's approval missed a relevant behavioral gap.
**Actionability**: There are no actionable code-change requests because the review reports no issues.
**Severity Accuracy**: The review treats the change as fully safe and recommends merge, but that prioritization misses a real correctness concern in the logging/proto path.
**Coverage**: Coverage is narrow and misses the most relevant concern for this feature: whether `trace_id` actually propagates through downstream serialization/logging paths.
**Signal-to-Noise**: A few concrete observations about the new field and tag are correct, but much of the review is approval framing or repo-external commentary, and the overall conclusion includes a contradicted assurance about end-to-end trace propagation.
**Depth**: The review shows some behavioral reasoning about zero values and `omitempty`, but it does not trace any downstream code path and misses the cross-file propagation problem.
**Novel & Substantive Findings**: The review found no substantive issues, so it contributes no novel findings absent from the other review.

</details>

<details><summary>Claim trace (Skwad)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| `WatsonRequest` now contains a `TraceID` field with the JSON tag `json:"trace_id | verified | Read | cmd/watson/structs/structs.go:245 TraceID string `json:"trace_id,omitempty"` |
| The field captures UUIDv7 trace IDs minted by Control for log correlation and tr | unverified | Read | This motivation and provenance are not established anywhere in the checked-out repo content that was read. |
| The change is backward compatible because an absent `trace_id` deserializes to t | verified | Read | cmd/watson/structs/structs.go:245 TraceID string `json:"trace_id,omitempty"` |
| `go build ./cmd/...` and `go test -tags=unit ./cmd/watson/structs/...` pass for  | unverified | Read, Grep | Build and test execution results are not available from repo reads/greps alone, and no in-repo artifact proves those com |
| Using `omitempty` preserves exact-match validation in handler tests and reclog c | unverified | Read, Grep | The repo reads performed do not prove that handler tests or reclog consumers rely on exact serialized request matches fo |
| This PR is a pure struct addition with no handler, waterfall, or module logic ch | unverified | Read | The checked-out file confirms the struct addition, but repo reads at PR HEAD do not by themselves establish the complete |
| The change has no unintended side effects in other packages. | unverified | Read, Grep | Absence of side effects is broader than what the inspected repo snippets can conclusively prove without exhaustive behav |
| The change enables Trace ID tracing across the attribution pipeline without brea | contradicted | Read, Grep | grep:TraceID\|trace_id\|TraceId |
| Follow-up commit `9fe96f58` correctly keeps empty trace IDs out of serialized re | unverified | Read, Grep | That commit is not available as inspectable code in this checkout, so its contents and effect cannot be verified from th |

</details>

<details><summary>Claim trace (Claude CI)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| `TraceID` appears in `WatsonRequest` at `cmd/watson/structs/structs.go:245`. | verified | Read | cmd/watson/structs/structs.go:245 TraceID string `json:"trace_id,omitempty"` |
| The field name uses Go-style `TraceID` casing rather than `TraceId`. | verified | Read | cmd/watson/structs/structs.go:245 TraceID string `json:"trace_id,omitempty"` |
| The field uses the JSON tag `json:"trace_id,omitempty"`. | verified | Read | cmd/watson/structs/structs.go:245 TraceID string `json:"trace_id,omitempty"` |
| Using `string` is appropriate for UUIDv7 trace identifiers. | non_falsifiable | Read | This is a subjective suitability judgment about type choice rather than a directly checkable fact with a concrete pass/f |
| The change is backward compatible because `omitempty` makes the field optional f | verified | Read | cmd/watson/structs/structs.go:245 TraceID string `json:"trace_id,omitempty"` |
| The zero-value empty string provides safe fallback behavior. | verified | Read | cmd/watson/structs/structs.go:245 TraceID string `json:"trace_id,omitempty"` |
| `WatsonResponse` embeds `WatsonRequest` at line 26. | contradicted | Read | cmd/watson/structs/structs.go:26 Request WatsonRequest `json:"request"` |
| Trace IDs automatically flow to reconciliation logs. | contradicted | Read, Grep | grep:TraceID\|trace_id\|TraceId |
| There are no null-pointer risks because the field is a value-type string. | verified | Read | cmd/watson/structs/structs.go:245 TraceID string `json:"trace_id,omitempty"` |
| No validation is needed because UUIDs are self-validating. | unverified | Read | The repo shows only a plain string field and does not contain evidence establishing whether external producers guarantee |
| Existing response tests cover the `trace_id` flow. | contradicted | Grep | grep:TraceID\|trace_id\|TraceId |
| No additional tests are required for this field addition. | unverified | Read, Grep | Repo inspection alone does not prove that existing test coverage is sufficient, and no dedicated `trace_id` assertions w |
| The field is only transmitted when present because of `omitempty`. | verified | Read | cmd/watson/structs/structs.go:245 TraceID string `json:"trace_id,omitempty"` |
| The PR description confirms `go build ./cmd/...` passes. | unverified | Read, Grep | The repo checkout does not contain verifiable command results for that build claim. |

</details>

<details><summary>Raw review — skwad-cli</summary>

# PR #890 Review: feat: accept trace_id on reconcile requests

## Summary

**Overall Assessment**: ✅ APPROVED — Clean, minimal, backward-compatible feature addition.

PR #890 adds a `TraceID` field (`json:"trace_id,omitempty"`) to the `WatsonRequest` struct alongside the existing `ControlTransactionID`. The field captures UUIDv7 trace IDs minted by Control (Kochava/control#489) to enable trace-based log correlation in reconciliation logs and traffic replay tooling. The change is backward compatible (absent field → empty string) and passes all existing tests.

---

## Findings

### ✅ No Issues Found

**Architecture**: Field addition is minimal and focused. Placement next to `ControlTransactionID` is logical.

**Backward Compatibility**: The `omitempty` tag ensures callers that don't send `trace_id` won't trigger unnecessary JSON serialization, preserving exact-match validation in handler tests and reclog consumers.

**Testing**: PR author reports `go build ./cmd/...` and `go test -tags=unit ./cmd/watson/structs/...` both pass. No test fixtures broken.

**Related PRs Tracked**: Control proto field (control-proto#19), Control sender logic (control#489 draft), BigQuery persistence (portal#259, analytics-query#647) are all properly sequenced.

---

## Minor Observations

1. **Field placement (line 245)**: Positioned immediately after `ControlTransactionID` — excellent for discoverability and semantic grouping.

2. **`omitempty` behavior**: Critical for backward compatibility. Callers that don't include `trace_id` in their payloads will see the field as an empty string when deserialized, but it won't be re-serialized into responses or logs unless explicitly set. This is the correct approach for optional fields that are part of a larger data object.

3. **Scope**: Pure struct addition — no handler changes, no waterfall logic, no module logic modified. Reduces risk of cascading bugs.

4. **Documentation**: The PR description clearly explains the motivation (Control tracing, replay tooling) and the linked PRs form a coherent narrative across the org.

---

## Testing Verification

- ✅ `go build ./cmd/...` passes
- ✅ `go test -tags=unit ./cmd/watson/structs/...` passes
- ✅ Field is properly JSON-tagged
- ✅ Backward compatible (omitempty tag present)
- ✅ No unintended side effects in other packages

---

## Recommendation

**Merge.** This is a well-executed struct extension that enablesTrace ID tracing across the attribution pipeline without breaking existing clients or test fixtures. The follow-up commit (9fe96f58, "fix: omit empty trace_id from serialized requests") correctly ensures that empty trace IDs don't pollute test fixtures or reclog data.


</details>

<details><summary>Raw review — Claude CI</summary>

### Comprehensive PR Review: feat: accept trace_id on reconcile requests

#### Review Checklist
- [x] Read Kochava coding standards
- [x] Analyze the changed file and implementation
- [x] Review code quality and best practices
- [x] Check for potential bugs or issues
- [x] Evaluate performance considerations
- [x] Assess security implications
- [x] Review test coverage needs
- [x] Check documentation requirements
- [x] Provide comprehensive feedback

---

## ✅ **Overall Assessment: APPROVED**

This PR implements a clean, well-executed addition of trace_id functionality to Watson's request structure. The implementation follows best practices and integrates seamlessly with the existing codebase.

### 📋 **Code Quality & Best Practices**

**Excellent Implementation**: 
- **Field placement**: `TraceID` is logically positioned next to `ControlTransactionID` at `cmd/watson/structs/structs.go:245`, maintaining code organization
- **Naming convention**: Uses proper Go naming (`TraceID` not `TraceId`) per Kochava standards
- **JSON tag consistency**: `json:"trace_id,omitempty"` follows existing patterns and API expectations
- **Type safety**: `string` type is appropriate for UUIDv7 trace identifiers

**Standards Compliance**:
- ✅ Follows Kochava's Go Style Guide naming conventions
- ✅ Uses standard library types (no unnecessary dependencies)
- ✅ Maintains consistent field patterns across the struct

### 🔍 **Implementation Analysis**

**Current Implementation**:
```go
ControlTransactionID string `json:"control_transaction_id"`
TraceID              string `json:"trace_id,omitempty"`
```

**Strengths**:
1. **Backward Compatible**: `omitempty` tag ensures missing fields don't break existing clients
2. **Strategic Positioning**: Placed logically next to related transaction tracking fields  
3. **Zero-value Safety**: Empty string default provides safe fallback behavior
4. **Automatic Flow**: Since `WatsonResponse` embeds `WatsonRequest` (line 26), trace IDs automatically flow to reconciliation logs

### 🐛 **Bug & Issue Analysis**

**No Issues Identified**: 
- ✅ No null pointer risks (string is a value type)
- ✅ No concurrency concerns (readonly field)
- ✅ No validation needed (UUIDs are self-validating)
- ✅ No breaking changes to existing API contracts

**Robustness**: The `omitempty` tag prevents empty trace_id fields from cluttering JSON output, maintaining clean serialization.

### ⚡ **Performance Considerations**

**Minimal Impact**:
- ✅ **Memory**: Single string field adds ~24 bytes per request (negligible)
- ✅ **CPU**: No computational overhead during serialization/deserialization
- ✅ **Network**: Only transmitted when present due to `omitempty`
- ✅ **Scalability**: No performance bottlenecks introduced

**Optimized Design**: The field-level approach avoids method call overhead and maintains Watson's high-performance request processing.

### 🔒 **Security Assessment**

**Secure Implementation**:
- ✅ **No sensitive data**: UUIDv7 trace IDs are safe for logging and correlation
- ✅ **No injection risks**: String field with no dynamic evaluation
- ✅ **Privacy compliant**: Trace IDs don't expose PII or business logic
- ✅ **Audit trail safe**: Enhances observability without security implications

### 🧪 **Test Coverage Analysis**

**Adequate Coverage**:
- ✅ **Build verification**: PR description confirms `go build ./cmd/...` passes
- ✅ **Struct testing**: Existing `go test -tags=unit ./cmd/watson/structs/...` validates serialization
- ✅ **Integration flow**: Since `WatsonResponse` embeds the request, existing response tests cover the flow

**No Additional Tests Required**: This is a straightforward field addition that leverages existing JSON marshaling infrastructure.

### 📚 **Documentation Review**

**Self-Documenting**: 
- ✅ Field name clearly indicates purpose (`TraceID`)
- ✅ JSON tag matches API expectations (`trace_id`)  
- ✅ PR description provides comprehensive context
- ✅ Relates to broader multi-repo trace correlation system

**No Documentation Updates Needed**: Simple field addition doesn't require additional documentation.

### 🏗️ **Architecture Integration**

**Seamless Integration**:
- ✅ **Multi-repo coordination**: Aligns with control-proto#19 and control#489
- ✅ **Observability enhancement**: Enables trace correlation across services
- ✅ **Future-ready**: Supports upcoming traffic-replay tooling requirements
- ✅ **Zero disruption**: No changes to attribution logic or business rules

### 🚨 **Critical Considerations**

**Deployment Dependencies** (from deep dive analysis):
The broader trace_id feature depends on coordinated deployment across multiple repositories. While this Watson change is safe, ensure:

1. **control-proto v0.5.0** is published before control deployment
2. **Local replace directives** are removed from portal before deployment
3. **Analytics converter** updates include trace_id mapping

### 🎯 **Recommendation**

**STRONG APPROVAL** - This implementation represents excellent software engineering:

✅ **Technical Excellence**: Clean, safe, and well-integrated  
✅ **Standards Compliance**: Follows all Kochava coding standards  
✅ **Backward Compatibility**: Zero breaking changes  
✅ **Performance Safe**: No negative impact  
✅ **Security Sound**: No vulnerabilities introduced  
✅ **Documentation Complete**: Adequately documented for scope  

**Ready for immediate merge.** The implementation is production-ready and will enhance Watson's observability capabilities while maintaining system stability.

---

</details>

<details><summary>Classifier output</summary>

{'bucket': 'easy'}

</details>

### Kochava/watson#893 (difficulty: easy)

| Criterion | skwad-cli | Claude CI |
| ---|---|--- |
| Issue Detection | 3 | 2 |
| Actionability | 3 | 2 |
| Severity Accuracy | 1 | 1 |
| Coverage | 3 | 1 |
| Signal-to-Noise | 2 | 1 |
| Depth | 2 | 1 |
| Novel & Substantive Findings | 3 | 0 |
| **Total** | **17** | **8** |

### Verification Summary

| Review | Verified | Unverified | Contradicted | Non-falsifiable | Rate | Tool Calls | Ungrounded | Grounding |
| ---|---|---|---|---|---|---|---|--- |
| Skwad | 35 | 3 | 3 | 2 | 81% | 104 | 3 | 92% |
| Claude CI | 23 | 3 | 3 | 3 | 71% | 104 | 2 | 92% |

<details><summary>Judge reasoning (skwad-cli)</summary>

**Issue Detection**: Review A identifies one clear behavioral bug in the restricted cache hydration path, plus several real secondary issues around missing tests, zero-value emission, token logging, and type consistency. A few extra findings are speculative, but there are still 4+ genuine issues.
**Actionability**: Most findings include concrete code-level fixes or precise follow-up directions, especially the cache hydration fix, test additions, and redaction recommendation.
**Severity Accuracy**: The main restricted-path bug is serious, but several labels are overstated: the review escalates test coverage and speculative compliance concerns to blocker/high levels without code evidence of production failure.
**Coverage**: The review spans correctness, test coverage, performance, privacy/logging, and maintainability. It covers most relevant concern areas for this PR.
**Signal-to-Noise**: The review has strong signal, led by the restricted-path omission, but it also contains speculative root-cause and compliance commentary that is not verified from the repo.
**Depth**: The strongest findings trace behavior across handler pre-enrichment, downstream URL construction, and tests. That cross-file reasoning is solid, though some later sections drift into unsupported speculation.
**Novel & Substantive Findings**: Review A contributes three substantive findings absent from Review B: the restricted cache hydration omission, the missing restricted-path 7-field test coverage, and the stale JSON round-trip contract test.

</details>

<details><summary>Claim trace (Skwad)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| The restricted cache hydration path in `cmd/watson/handlers/reconcile_handler.go | verified | Read | cmd/watson/handlers/reconcile_handler.go:204 watsonRequest.FBAttributionDetails = &structs.FBAttributionDetails{ AdID: c |
| Because `buildFBEventEnrichmentParams` unconditionally serializes the engagement | verified | Read | cmd/watson/facebook/reconcile.go:1076 if details.AdID != "" { attrSource := []map[string]interface{}{ { "source_ad_id":  |
| The root cause is a schema gap in `restrictedxna.ReducedRestrictedClaim`, which  | unverified | Read, Grep | The repo imports `github.com/Kochava/watson-util/restrictedxna`, but the external module source for `ReducedRestrictedCl |
| The new seven-field URL test only exercises direct `FBAttributionDetails` popula | verified | Read | cmd/watson/facebook/fb_event_enrichment_url_test.go:128 request := &structs.WatsonRequest{ IsFB: true, RequestID: "req-7 |
| There is no handler-layer test that exercises `AdEngagementTime` on the restrict | verified | Grep | grep:AdEngagementTime |
| `TestFBAttributionDetails_JSONRoundTrip` was not updated to assert the three new | verified | Read | cmd/watson/handlers/fb_attribution_enrichment_test.go:243 FBAttributionDetails: &structs.FBAttributionDetails{ AdID: "ad |
| Using `[]map[string]interface{}` for `attrSource` introduces per-call map alloca | verified | Read | cmd/watson/facebook/reconcile.go:1077 attrSource := []map[string]interface{}{ { "source_ad_id": details.AdID, "source_ad |
| The function uses repeated `params +=` concatenation instead of a builder, which | verified | Read | cmd/watson/facebook/reconcile.go:1063 params := "" if request.FBAttributionDetails != nil { details := request.FBAttribu |
| The new engagement fields may need ATT/GDPR gating or explicit compliance sign-o | non_falsifiable | Read | This is a policy/compliance judgment rather than a code-falsifiable fact in the checked-out repo. |
| The new engagement fields are emitted unconditionally, unlike `InstallTime`, whi | verified | Read | cmd/watson/facebook/reconcile.go:1071 if details.InstallTime > 0 { params += fmt.Sprintf("&install_timestamp=%d", detail |
| `ClickTime` is typed as `int` while `AdEngagementTime` is typed as `int64`. | verified | Read | cmd/watson/structs/structs.go:347 AdID string `json:"ad_id"` ClickTime int `json:"click_time"` AuditingToken string `jso |
| Sensitive tokens are exposed in logs because the full Facebook request URL is lo | verified | Read | cmd/watson/facebook/reconcile.go:254 requestEndpoint := fmt.Sprintf( "%s/%d/activities?access_token=%s&event=%s&attribut |

</details>

<details><summary>Claim trace (Claude CI)</summary>

| Claim | Outcome | Tools | Evidence |
| ---|---|---|--- |
| The PR implements all 7 `attribution_sources` sub-fields in `buildFBEventEnrichm | verified | Read | cmd/watson/facebook/reconcile.go:1077 attrSource := []map[string]interface{}{ { "source_ad_id": details.AdID, "source_ad |
| The struct change is additive: the existing JSON-tagged fields remain and the th | verified | Read | cmd/watson/structs/structs.go:347 AdID string `json:"ad_id"` ClickTime int `json:"click_time"` AuditingToken string `jso |
| Test coverage includes the seven-subfield case, empty-AdID handling, restricted  | verified | Read | cmd/watson/facebook/fb_event_enrichment_url_test.go:15 func Test_buildFBEventEnrichmentParams_EventID_UsesRequestID(t *t |
| The implementation has graceful JSON-marshal fallback because it only appends `a | verified | Read | cmd/watson/facebook/reconcile.go:1088 if attrSourceJSON, err := json.Marshal(attrSource); err == nil { params += "&attri |
| There is a real validation gap: if `AdID` is present, zero or empty values for t | verified | Read | cmd/watson/facebook/reconcile.go:1076 if details.AdID != "" { attrSource := []map[string]interface{}{ { "source_ad_id":  |
| `ClickTime` and `AdEngagementTime` use inconsistent integer types. | verified | Read | cmd/watson/structs/structs.go:347 AdID string `json:"ad_id"` ClickTime int `json:"click_time"` AuditingToken string `jso |
| The function lacks logging for the built `attribution_sources` content. | unverified | Read | The code shown does not log the built payload inside `buildFBEventEnrichmentParams`, but whether that is a meaningful de |
| URL encoding is applied correctly using `url.QueryEscape` for request IDs, insta | verified | Read | cmd/watson/facebook/reconcile.go:1068 params += "&event_id=" + url.QueryEscape(request.RequestID) params += "&install_id |
| There is no sensitive data exposure in logs. | contradicted | Read | cmd/watson/facebook/reconcile.go:254 requestEndpoint := fmt.Sprintf( "%s/%d/activities?access_token=%s&event=%s&attribut |
| A partial-fields test is missing for the case where `AdID` is present but the ot | verified | Grep | grep:PartialFields |
| The implementation is solid and ready for merge. | non_falsifiable | Read | This is an overall judgment call rather than a concrete code claim that can be falsified directly from the checked-out f |

</details>

<details><summary>Raw review — skwad-cli</summary>

# PR #893 Review: emit all 7 attribution_sources sub-fields on FB claim request

## Summary

This PR extends the Facebook attribution module by adding 3 new fields to `FBAttributionDetails` struct and updating `buildFBEventEnrichmentParams` to emit all 7 attribution_sources sub-fields per Meta Ads spec (5/4/2026). 

**⚠️ Status: BLOCKER FOUND** — The PR is **incomplete**. The cache population path in `reconcile_handler.go` does not populate the 3 new fields, causing zero-initialized values to be sent to Facebook, which corrupts attribution accuracy for restricted-path devices. This must be fixed before merge.

**Scope**: 54 additions, 10 deletions across 3 files (structs, reconcile logic, tests) — **but missing critical cache path update**.

---

## Findings by Severity

### 🚫 CRITICAL: Cache Path Missing 3 New Fields

**File**: `cmd/watson/handlers/reconcile_handler.go:204-212`

**Issue**: The restricted FB device cache path does not populate the 3 new fields (`AdEngagementTime`, `AdEngagementType`, `EventID`) when creating the `FBAttributionDetails` object from the cached claim. These fields are zero-initialized (int64=0, string=""), and then sent to Facebook's attribution API as blank values.

**Impact**: 
- **Attribution Accuracy**: Restricted-path devices (cache hits on encrypted Redis) will have malformed `attribution_sources` with missing engagement data, corrupting Facebook's ability to reconcile attribution.
- **Data Integrity**: The 3 new fields are spec-required (Meta Ads 5/4/2026) but will be systematically blank for the restricted cache path, creating a mismatch between non-restricted (Control) and restricted (cache) paths.
- **Production Bug**: This will cause attribution mismatches and potentially prevent Facebook from accepting the claim.

**Current Code** (lines 204-212):
```go
watsonRequest.FBAttributionDetails = &structs.FBAttributionDetails{
    AdID:                  cachedClaim.AdID,
    ClickTime:             cachedClaim.ClickTime,
    AuditingToken:         cachedClaim.AuditingToken,
    AttributionMethod:     cachedClaim.AttributionMethod,
    InstallTime:           cachedClaim.InstallTime,
    GoogleInstallReferrer: cachedClaim.GoogleInstallReferrer,
    MetaInstallReferrer:   cachedClaim.MetaInstallReferrer,
    // Missing: AdEngagementTime, AdEngagementType, EventID
}
```

**Suggested Fix**: Add the 3 new fields to the cache population:
```go
watsonRequest.FBAttributionDetails = &structs.FBAttributionDetails{
    AdID:                  cachedClaim.AdID,
    ClickTime:             cachedClaim.ClickTime,
    AuditingToken:         cachedClaim.AuditingToken,
    AttributionMethod:     cachedClaim.AttributionMethod,
    InstallTime:           cachedClaim.InstallTime,
    GoogleInstallReferrer: cachedClaim.GoogleInstallReferrer,
    MetaInstallReferrer:   cachedClaim.MetaInstallReferrer,
    AdEngagementTime:      cachedClaim.AdEngagementTime,    // ADD
    AdEngagementType:      cachedClaim.AdEngagementType,    // ADD
    EventID:               cachedClaim.EventID,              // ADD
}
```

**Note**: This assumes `restrictedxna.ReducedRestrictedClaim` (the return type of `GetFBAttributionForEventTime`) has these 3 fields available. If not, the cache schema must be updated to include them.

**Blocker Status**: Yes, this must be fixed before merge. Do not deploy without ensuring the cache path populates all 7 fields.

**Root Cause Analysis**: The `ReducedRestrictedClaim` type (from `watson-util` vendor package) does not have fields for `AdEngagementTime`, `AdEngagementType`, or `EventID`. These cannot be copied even if the handler code tried to populate them. The issue is a **data-model gap in the cache layer**, not just a missing assignment.

**Confirmed Root Cause** (via `go doc` inspection): The `restrictedxna.ReducedRestrictedClaim` struct (vendored `restrictedxna v1.2.0`) only has 4 fields:
```
type ReducedRestrictedClaim struct {
    AttributionMethod     string
    InstallTime           int64
    GoogleInstallReferrer string
    MetaInstallReferrer   string
    // MISSING: AdEngagementTime, AdEngagementType, EventID
}
```

**Two Paths Forward**:
1. **Recommended**: Update `watson-util/restrictedxna` to version that includes the 3 new fields in `ReducedRestrictedClaim` and update the cache-population logic in `GetReducedRestrictedClaim` to populate them from device details.
2. **Alternative (document as limitation)**: If the `watson-util` update is blocked, explicitly scope this PR's description and add a comment at line 204 stating that the 7-field emission only applies to **non-restricted modules** (facebook/instagram/facebook-deeplink), and restricted (facebook-api proxy) devices will send only 4 fields.

**Current Risk**: The PR claims complete spec compliance ("emit all 7 sub-fields") but only achieves it for 1/2 of the device pathways. Restricted-device (cache-based) attribution requests will silently send malformed `attribution_sources` with zero/empty engagement values. No error signal, no test coverage — complete silent failure for an entire traffic class.

**Visibility Gap**: If Meta's API validates, weights, or downgrades claims with missing/zero engagement timestamps, restricted traffic will suffer regressed attribution until Control's companion PR lands. The cost is invisible because the restriction path has no active test.

---

### MEDIUM: GC Pressure from `map[string]interface{}` Allocation

**File**: `cmd/watson/facebook/reconcile.go:1077-1090`

**Issue**: The `attrSource` map is allocated per FB event request with 7 entries and 2 int values that get interface-boxed. At high FB event volume, this creates ongoing GC pressure and extra allocations.

**Impact**: Performance degradation under load; unnecessary garbage collection overhead on attribution request path.

**Suggested Fix**: Replace `map[string]interface{}` with a typed private struct:

```go
type attributionSource struct {
    SourceAdID              string `json:"source_ad_id"`
    SourceAdEngagementTime  int64  `json:"source_ad_engagement_time"`
    SourceAdEngagementType  string `json:"source_ad_engagement_type"`
    SourceEventTime         int    `json:"source_event_time"`
    SourceEventID           string `json:"source_event_id"`
    SourceAuditingToken     string `json:"source_auditing_token"`
    SourceAttributionMethod string `json:"source_attribution_method"`
}
```

Then marshal the typed struct instead of the map. This eliminates map allocation, int boxing, and speeds up `json.Marshal` via known-type encoding.

**Context**: Function is gated to event/session traffic only (`TrafficType == "event" || "session"`), so this is not on the critical path for all requests. Safe to merge with this logged as a follow-up optimization task.

---

### LOW: String Concatenation Pattern

**File**: `cmd/watson/facebook/reconcile.go:1063-1108`

**Issue**: 8+ `params +=` string concatenations throughout the function. String concatenation in a loop creates new string allocations on each iteration.

**Impact**: Unnecessary allocations; less efficient than streaming to a builder.

**Suggested Fix**: Use `strings.Builder` with `Grow(512)` to pre-allocate buffer capacity:

```go
var sb strings.Builder
sb.Grow(512)
sb.WriteString("&is_fb=true")
sb.WriteString("&event_id=")
sb.WriteString(url.QueryEscape(request.RequestID))
// ... etc
params := sb.String()
```

---

### MEDIUM: New Engagement Fields Lack ATT/GDPR Compliance Gate

**File**: `cmd/watson/facebook/reconcile.go:1076-1090`

**Issue**: The two new behavioral engagement fields (`AdEngagementType`, `AdEngagementTime`) are emitted unconditionally to Facebook with no ATT (App Tracking Transparency) or GDPR consent gate. Existing fields in the same payload are gated (see line 197 for ATT check), but the new engagement data is not.

**Impact**: 
- **Compliance Risk**: If these fields contain or enable tracking of user engagement behavior, they may require ATT authorization or GDPR consent to send to a third party (Facebook).
- **Regional Compliance**: May violate GDPR/ATT requirements in restricted markets.

**Suggested Fix**: Confirm with the compliance/legal team:
1. Are `AdEngagementType` and `AdEngagementTime` exempt from ATT/GDPR gating (e.g., because they are previously-collected Meta data being reflected back)?
2. If not exempt, gate their emission to match the ATT check pattern used elsewhere in the module.

**Note**: This is a sign-off question, not necessarily a code change. Verify compliance status before merging.

---

### HIGH: Restricted Path Not Covered by New Test

**File**: `cmd/watson/facebook/fb_event_enrichment_url_test.go:125-160` (new test `Test_buildFBEventEnrichmentParams_AllSevenSubFields`)

**Issue**: The new 7-field test only exercises the non-restricted path (directly-populated `FBAttributionDetails`). There is no test for the restricted path (where `FBAttributionDetails` is built from a Redis-cached `ReducedRestrictedClaim` that lacks the 3 new fields). The test suite passes 100% while the restricted code path silently fails to emit all 7 fields.

**Impact**: 
- **False Positive CI**: Tests pass, but restricted devices hitting the claim endpoint get malformed `attribution_sources`.
- **Spec Violation**: The PR claims complete 7-field emission but only achieves it for non-restricted modules.
- **Production Outage Risk**: Restricted-device attribution requests (entire facebook-api proxy path) will silently send incomplete data to Facebook.

**Suggested Fix**: Add a test case that constructs `FBAttributionDetails` with only the 4 pre-existing fields (simulating cache hydration) and documents whether the 3 new fields are:
- Zero-initialized in output (documenting the known limitation), OR
- Fixed to be present (once the cache schema is updated per Finding 1)

Example:
```go
func Test_buildFBEventEnrichmentParams_RestrictedCachePath_OnlyFourFields(t *testing.T) {
    // Simulate a restricted device with cache hit that lacks the 3 new fields
    request := &structs.WatsonRequest{
        FBAttributionDetails: &structs.FBAttributionDetails{
            AdID:              "ad-restricted",
            ClickTime:         1709913600,
            AuditingToken:     "token-restricted",
            AttributionMethod: "SRN",
            // NOTE: AdEngagementTime, AdEngagementType, EventID are zero/empty
        },
    }
    
    params := buildFBEventEnrichmentParams(request)
    decoded, _ := url.QueryUnescape(params)
    
    // TODO: Assert that either all 7 fields are present (after cache update)
    // OR that the 3 new fields are empty/zero (documenting the known limitation)
}
```

---

### MEDIUM: JSON Round-Trip Test Missing 3 New Fields

**File**: `cmd/watson/handlers/fb_attribution_enrichment_test.go:240-269` (test `TestFBAttributionDetails_JSONRoundTrip`)

**Issue**: This round-trip test verifies struct fields survive marshal/unmarshal, but it was not updated to test the 3 new fields (`AdEngagementTime`, `AdEngagementType`, `EventID`). If someone later changes a JSON tag (e.g., `json:"event_id"` → `json:"event_id,omitempty"`), the contract assertion won't catch it.

**Impact**: The PR asserts "existing JSON tags untouched per cache contract" but provides no test coverage for the new tags.

**Suggested Fix**: Extend the round-trip test to set and assert the 3 new fields:
```go
func TestFBAttributionDetails_JSONRoundTrip(t *testing.T) {
    original := &structs.FBAttributionDetails{
        AdID:              "ad-123",
        ClickTime:         1709913600,
        AuditingToken:     "token",
        AttributionMethod: "SRN",
        AdEngagementTime:  1709913500,  // ADD
        AdEngagementType:  "click",      // ADD
        EventID:           "evt-456",    // ADD
    }
    
    data, _ := json.Marshal(original)
    var restored *structs.FBAttributionDetails
    json.Unmarshal(data, &restored)
    
    assert.Equal(t, original, restored)
}
```

---

### MEDIUM: `EventID` Field Name Collides with `event_id` URL Param

**File**: `cmd/watson/structs/structs.go:356` and `cmd/watson/facebook/reconcile.go:1068, 1083`

**Issue**: `FBAttributionDetails.EventID` (json tag `"event_id"`) holds the *previous attribution's* FB event/click ID. But in `buildFBEventEnrichmentParams`, the query-level `event_id` URL param (line 1068) is `request.RequestID` (the current Watson request ID). Same English name, same JSON key name, two completely different values.

**Current Code**:
```go
// Line 1068: event_id is the current Watson request
params += "&event_id=" + url.QueryEscape(request.RequestID)

// Lines 1083-1085: EventID from FBAttributionDetails is previous event
"source_event_id": details.EventID,  // Previous FB event
```

**Impact**: A developer debugging or extending this function will see `event_id` in the URL and `source_event_id` in the payload and reasonably wonder if they're the same thing or which one is "correct". The existing regression test comment (reconcile_test.go) shows this area already bit QA once with: `// event_id must be the Watson request_id, NOT kochava_device_id`.

**Suggested Fix**: Rename the struct field for clarity:
- Option 1 (Recommended): `SourceEventID` with json tag `"source_event_id"` — makes the "source" prefix explicit at the struct level too
- Option 2: `PreviousFBEventID` with json tag `"previous_fb_event_id"` — clarifies this is historical data
- Option 3: Add a code comment at the struct: `EventID string // Previous FB event ID from attribution_sources (NOT current Watson request ID)`

**Note**: Changing the JSON tag breaks the cache contract, so if going with Option 1 or 2, must also update the cache schema. If going with Option 3, the JSON tag stays `"event_id"` but the naming ambiguity persists.

---

### LOW: Unconditional Emission of Zero-Value Sub-Fields

**File**: `cmd/watson/facebook/reconcile.go:1076-1090`

**Issue**: `InstallTime` has an explicit guard before emission (`if details.InstallTime > 0`), but the 3 new fields are added to `attribution_sources` unconditionally. Until Control's companion PR lands, `AdEngagementTime=0` and `AdEngagementType=""` and `EventID=""` will be sent to Meta for every request.

**Impact**: The `InstallTime` guard pattern suggests zero/empty means "not available" and shouldn't be emitted. Breaking that pattern for the new fields creates ambiguity: does Meta treat `"source_ad_engagement_time":0` as "not set" or as "epoch engagement"? If Meta rejects or downgrades claims with zero timestamps, attribution could regress before Control ships.

**Suggested Fix**: Either (a) guard the new fields similarly before adding to the map, or (b) confirm with Meta's API that zero/empty sub-fields are benign and add a comment saying so:

Option (a):
```go
attrSource := map[string]interface{}{
    "source_ad_id": details.AdID,
    "source_event_time": details.ClickTime,
    "source_auditing_token": details.AuditingToken,
    "source_attribution_method": details.AttributionMethod,
}
if details.AdEngagementTime > 0 {
    attrSource["source_ad_engagement_time"] = details.AdEngagementTime
}
if details.AdEngagementType != "" {
    attrSource["source_ad_engagement_type"] = details.AdEngagementType
}
// etc.
```

Option (b) — comment:
```go
// Meta accepts zero/empty sub-fields until Control populates them
// (companion PR feat/fb-attribution-postback-payload)
"source_ad_engagement_time": details.AdEngagementTime,
```

---

### LOW: Type Inconsistency in Timestamps

**File**: `cmd/watson/structs/structs.go:348` and `cmd/watson/facebook/reconcile.go:1082`

**Issue**: `ClickTime` is typed `int` while the new `AdEngagementTime` is `int64`. Both are timestamps fed into the same `attribution_sources` JSON object as `source_event_time` and `source_ad_engagement_time`. The inconsistency is a latent correctness hazard for any proto/tooling that serializes `ClickTime` as `int32`, which would silently truncate post-2038 timestamps.

**Impact**: Low immediate risk (Go's `int` is 64-bit on modern platforms), but deepens a pre-existing type inconsistency in the same JSON payload. If tooling treats one as `int32`, post-2038 timestamps will silently corrupt.

**Suggested Fix**: Widen `ClickTime` from `int` to `int64` for consistency:
```go
type FBAttributionDetails struct {
    ClickTime  int64  `json:"click_time"`  // changed from int
    AdEngagementTime int64 `json:"ad_engagement_time"`
    // ... both now int64 for consistency
}
```

This is a safe change because JSON unmarshaling accepts both `int` and `int64` fields, and Go's larger type is backward-compatible.

---

### LOW: No Validation on `AdEngagementType` String

**File**: `cmd/watson/facebook/reconcile.go:1081`

**Issue**: `AdEngagementType` is a string field with no enumeration or length validation. Typos, oversized values, or unexpected values are silently forwarded to Facebook's API.

**Impact**: 
- **Silent Failures**: Facebook may reject or mishandle malformed engagement types, but Watson won't catch the error.
- **Data Quality**: No visibility into whether the field is populated correctly.

**Suggested Fix**: Consider one of these approaches:
1. **Enum Validation**: Define a private enum for valid engagement types (`click`, `impression`, etc.) and validate before emission.
2. **Length Cap**: At minimum, enforce a reasonable max length (e.g., 50 chars) to prevent accidental oversized values.
3. **Whitelist**: If there are known valid values per spec, validate against a whitelist.

**Severity**: Low if the source (Control) is trusted to provide valid data. Upgrade to Medium if Control data is not fully trusted.

---

### NIT: Struct Field Ordering Could Be More Logical

**File**: `cmd/watson/structs/structs.go:347-357`

**Issue**: The new fields are appended at the end of `FBAttributionDetails`, breaking the logical grouping. A more reader-friendly order would group claim-identity fields together:

```go
type FBAttributionDetails struct {
    // Claim identity
    AdID              string `json:"ad_id"`
    ClickTime         int    `json:"click_time"`
    EventID           string `json:"event_id"`
    AuditingToken     string `json:"auditing_token"`
    AttributionMethod string `json:"attribution_method"`
    AdEngagementTime  int64  `json:"ad_engagement_time"`
    AdEngagementType  string `json:"ad_engagement_type"`
    
    // Install/referrer context
    InstallTime           int64  `json:"install_time"`
    GoogleInstallReferrer string `json:"google_install_referrer"`
    MetaInstallReferrer   string `json:"meta_install_referrer"`
}
```

**Impact**: Cosmetic; zero cache/contract impact because JSON tags are untouched.

**Suggested Fix**: Reorder fields for logical grouping. Optional and can be deferred if it would conflict with ongoing Control PR work.

---

### NIT: Sensitive Tokens Logged in Full Request URL

**File**: `cmd/watson/facebook/reconcile.go:316`

**Issue**: The full request URL (including `source_auditing_token` and potentially `access_token` values) is logged, which may expose secrets in logs.

**Impact**: Minimal if logs are properly secured, but best practice is to redact sensitive values before logging.

**Suggested Fix**: Redact or truncate sensitive fields in request URL logs (e.g., show only first/last 4 chars of tokens).

---

### NIT: `[]byte → string` Copy

**File**: `cmd/watson/facebook/reconcile.go:1089`

**Issue**: `string(attrSourceJSON)` creates a copy of the `[]byte` before passing to `url.QueryEscape`.

**Impact**: Minimal; unavoidable with stdlib `url.QueryEscape` API signature.

**Suggested Fix**: None required; this is the correct pattern given stdlib constraints.

---

## Code Quality & Testing

### ✅ Test Coverage: Excellent

- New test `Test_buildFBEventEnrichmentParams_AllSevenSubFields` (lines 125-160) validates all 7 required sub-fields are present in the URL-decoded `attribution_sources`.
- Test constructs a full `FBAttributionDetails` with all fields populated and asserts exact JSON key-value pairs in the output.
- Existing test `Test_buildFBEventEnrichmentParams_NoAdID_NoAttributionSources` (lines 162+) verifies graceful degradation when AdID is empty.
- Previous tests (EventID, InstallID, FullDetails, RestrictedCacheMiss, NotFB) remain unaffected.

### ✅ Struct Design: Sound

- `FBAttributionDetails` field additions are additive only — existing JSON tags untouched.
- Field naming is consistent: `AdEngagementTime`, `AdEngagementType`, `EventID` (not `SourceAdEngagementTime`, etc. — source prefix is only in the JSON output).
- Field types match spec: `int64` for `ad_engagement_time` (timestamp), `string` for type and event ID.

### ✅ Backward Compatibility: Maintained

- The 30-day restrictedxna cache contract is honored — existing JSON tags on the struct are unchanged.
- graceful degradation logic preserved: if AdID is empty, `attribution_sources` is not added to the request (lines 1076, 1099-1103).
- Existing callers of `buildFBEventEnrichmentParams` will continue to work without modification.

### ✅ Spec Alignment

PR correctly implements the Meta Ads spec (5/4/2026) requirements:
- Emits all 7 sub-fields: `source_ad_id`, `source_ad_engagement_time`, `source_ad_engagement_type`, `source_event_time`, `source_event_id`, `source_auditing_token`, `source_attribution_method`.
- `AttributionMethod` field was already on the struct but unemitted — now emitted (line 1085).

---

## Design & Coordination

### ✅ Companion PR Pattern

PR description mentions Control will populate the 3 new fields from device details (companion control PR). This is a standard handoff pattern:
- Watson receives structured data from Control
- Watson emits it to Facebook per spec
- No tight coupling; just structured data flow

### ✅ Environment Variable Support

Separate commit (e47cff7) adds `FACEBOOK_ENDPOINT` env var override for QA mock-service testing (mirrors Snapchat pattern). This enables end-to-end validation of all 7 sub-fields without hitting production Facebook Graph API.

---

## Security & Data Privacy

- New fields (`ad_engagement_time`, `ad_engagement_type`, `event_id`) are device/engagement metadata sent to Facebook.
- No obvious injection risks: fields are JSON-marshaled then URL-encoded (standard safety).
- All data originates from Control's device details (described as "cleartext access"), not user input.
- No new PII exposure introduced; same data classification as existing fields.
- Graceful degradation on missing fields (empty check on AdID; if missing, attribution_sources not sent).

---

## Recommendations

⛔ **DO NOT MERGE** until the CRITICAL blocker is addressed.

### Required Fixes (Blocking)

1. **CRITICAL — Cache Path Update**: Update `reconcile_handler.go:204-212` to populate the 3 new fields from the cached claim:
   ```go
   AdEngagementTime: cachedClaim.AdEngagementTime,
   AdEngagementType: cachedClaim.AdEngagementType,
   EventID:          cachedClaim.EventID,
   ```
   - Verify that `restrictedxna.ReducedRestrictedClaim` has these fields available in the cache schema
   - If not, coordinate with the cache/utilities team to add them
   - Add a test case to verify all 7 fields are present in the cache path (currently only the non-restricted Control path is tested)

2. **MEDIUM — Compliance Gate**: Confirm with legal/compliance that `AdEngagementType` and `AdEngagementTime` are exempt from ATT/GDPR gating, or add consent gates to match existing patterns.

### Strongly Recommended (Non-blocking but high value)

3. **MEDIUM — GC Optimization**: Log a task to optimize `buildFBEventEnrichmentParams` to use typed struct + `strings.Builder`:
   - Replace `map[string]interface{}` with a typed private struct
   - Replace `params +=` concatenations with `strings.Builder`
   - Can be done post-merge as a follow-up perf task

4. **LOW — Validation**: Add enum or whitelist validation for `AdEngagementType` to catch malformed values early.

### Follow-up (Optional)

5. **Companion PR**: Ensure the Control PR (feat/fb-attribution-postback-payload) that populates the 3 new fields on the non-restricted path lands before this is deployed to production.

6. **NIT — Logging**: Redact sensitive tokens from request URL logs to prevent accidental secret exposure.

---

## Complete Findings Summary

| Severity | # | File | Issue | Status |
|----------|---|------|-------|--------|
| **CRITICAL** | 1 | `reconcile_handler.go:204-212` | Restricted path never populates 3 new fields → silently sends zero/empty values to Meta | **BLOCKER** |
| **HIGH** | 1 | `fb_event_enrichment_url_test.go:125-160` | New 7-field test doesn't cover restricted path → false green CI | **BLOCKER** |
| **MEDIUM** | 4 | `reconcile_handler.go:204-212` | Root cause: `ReducedRestrictedClaim` lacks 3 new fields in vendor package | **BLOCKER** |
| **MEDIUM** | 1 | `reconcile.go:1077-1090` | GC pressure from map allocation → ongoing allocations at high volume | Non-blocking (follow-up task) |
| **MEDIUM** | 2 | `fb_attribution_enrichment_test.go:240` | JSON round-trip test missing 3 new fields → contract untested | Non-blocking (add test) |
| **MEDIUM** | 1 | `structs.go:356` / `reconcile.go:1068, 1083` | `EventID` name collides with `event_id` URL param → ambiguity for maintainers | Non-blocking (rename for clarity) |
| **MEDIUM** | 1 | `reconcile.go:1076-1090` | Unconditional zero-value emission breaks `InstallTime` guard pattern | Non-blocking (add guard or comment) |
| **LOW** | 1 | `reconcile.go:1063-1108` | String concatenation pattern → 8+ allocations per request | Non-blocking (use strings.Builder) |
| **LOW** | 1 | `reconcile.go:1081` | No validation on `AdEngagementType` string → silently forwards malformed values | Non-blocking (add enum validation) |
| **LOW** | 1 | `structs.go:348` / `reconcile.go:1082` | Type inconsistency: `ClickTime int` vs `AdEngagementTime int64` → latent post-2038 hazard | Non-blocking (widen to int64) |
| **NIT** | 1 | `reconcile.go:316` | Sensitive tokens in logged request URL | Non-blocking (redact in logs) |
| **NIT** | 1 | `reconcile.go:1089` | `[]byte→string` copy → unavoidable with stdlib | Non-blocking (documented acceptable) |
| **NIT** | 1 | `structs.go:347-357` | Field ordering breaks logical grouping | Non-blocking (cosmetic) |

## Verdict

⛔ **DO NOT MERGE** — Three independent reviewers (Security Sentinel, Bug Hunter, Architecture Reviewer) all identified the same critical blocker: the restricted-path cache hydration does not populate the 3 new fields, causing silent attribution data loss for an entire device class.

---

## Required Pre-Merge Actions

### 1. **Fix the Restricted Path** (CRITICAL)

The PR must address one of these before merge:

**Option A (Recommended)**: Update the vendor package
- Bump `watson-util/restrictedxna` to a version that includes `AdEngagementTime`, `AdEngagementType`, `EventID` in `ReducedRestrictedClaim`
- Update `GetReducedRestrictedClaim` cache-storage logic to populate these fields
- Update `reconcile_handler.go:204-212` to copy them
- Add a test case verifying all 7 fields are present for restricted path

**Option B (Less Preferred)**: Document as a known limitation
- Update PR description to explicitly state: "7-field emission applies only to non-restricted modules (facebook/instagram/facebook-deeplink). Restricted (facebook-api proxy) devices emit 4 fields until Control's companion PR lands."
- Add code comment at line 204 explaining the limitation
- Add test case documenting the 4-field output for restricted path
- Follow up with urgent task to implement Option A before deploying to production

### 2. **Add Test Coverage for Restricted Path** (HIGH)

Add `Test_buildFBEventEnrichmentParams_RestrictedCachePath_PartialFields` to verify the actual behavior:
```go
func Test_buildFBEventEnrichmentParams_RestrictedCachePath_PartialFields(t *testing.T) {
    request := &structs.WatsonRequest{
        FBAttributionDetails: &structs.FBAttributionDetails{
            // Simulating cache hydration (4 fields only)
            AdID:              "ad-cache",
            ClickTime:         1709913600,
            AuditingToken:     "token-cache",
            AttributionMethod: "SRN",
            // AdEngagementTime, AdEngagementType, EventID are zero/empty
        },
    }
    
    params := buildFBEventEnrichmentParams(request)
    decoded, _ := url.QueryUnescape(params)
    
    // TODO: Assert behavior after fix is determined
}
```

### 3. **Fix JSON Round-Trip Test** (MEDIUM)

Extend `TestFBAttributionDetails_JSONRoundTrip` to assert the 3 new fields.

---

## Strongly Recommended Pre-Merge Actions

### 4. **Clarify `EventID` Naming** (MEDIUM)

Choose one:
- Rename to `SourceEventID` with json tag `"source_event_id"` (requires cache schema update)
- Rename to `PreviousFBEventID` with json tag `"previous_fb_event_id"` (requires cache schema update)
- Keep as `EventID` but add clarifying comment in struct

### 5. **Guard Zero-Value Emissions** (MEDIUM)

Either add guards around the new fields in the map, or add a comment confirming Meta's API accepts zero/empty values.

---

## Post-Merge Follow-Up Tasks

These can be logged as follow-up work but should be done before production deployment:

1. Optimize `buildFBEventEnrichmentParams` to use typed struct + `strings.Builder` (MEDIUM perf issue)
2. Add enum validation for `AdEngagementType` (LOW)
3. Widen `ClickTime` from `int` to `int64` (LOW, prevents post-2038 truncation)
4. Redact sensitive tokens from request URL logs (NIT, security best practice)
5. Reorder `FBAttributionDetails` fields for readability (NIT, cosmetic)

---

## Verified Checklist

- [x] `go build` clean (per PR description)
- [x] Unit tests pass (per PR description)
- [ ] New test validates all 7 sub-fields (✗ restricted path not tested)
- [ ] Backward compatibility maintained (✓ on non-restricted path only)
- [ ] Spec requirements satisfied (✗ for restricted devices)
- [ ] No CRITICAL or HIGH issues (✗ 2 blockers identified)
- [ ] Graceful degradation logic intact (✓)
- [ ] All code paths tested (✗ restricted path missing)


</details>

<details><summary>Raw review — Claude CI</summary>

### Pull Request Review

Reviewing PR #893: feat: emit all 7 attribution_sources sub-fields on FB claim request

**Todo List:**
- [x] Read and analyze changed files
- [x] Review Kochava coding standards
- [x] Check code quality and best practices
- [x] Identify potential bugs or issues
- [x] Assess performance considerations
- [x] Review security implications
- [x] Evaluate test coverage
- [x] Check for needed documentation updates
- [x] Provide comprehensive feedback

---

### 📝 Code Review Summary

This PR properly implements the Meta Ads spec updates to emit all 7 sub-fields in the `attribution_sources` JSON array. The implementation is **well-structured and follows good practices**.

### ✅ **Positive Highlights**

**Excellent Backward Compatibility**
- Three new fields added with descriptive JSON tags preserving the 30-day cache contract
- Existing fields untouched - maintains compatibility with Control's device details

**Strong Test Coverage**
- Comprehensive test `Test_buildFBEventEnrichmentParams_AllSevenSubFields` validates all 7 required keys
- Good edge case coverage: empty AdID, cache miss scenarios
- Excellent regression test preventing event_id/install_id confusion

**Clean Implementation**
- Simple, focused changes to `buildFBEventEnrichmentParams:1043-1094`
- Proper error handling with graceful JSON marshal fallback
- Clear separation of concerns

### 🔍 **Areas for Improvement**

#### **Critical: Missing Field Validation**
```go
// cmd/watson/facebook/reconcile.go:1062-1071
attrSource := []map[string]interface{}{
    {
        "source_ad_id":              details.AdID,
        "source_ad_engagement_time": details.AdEngagementTime, // Could be 0
        "source_ad_engagement_type": details.AdEngagementType, // Could be ""  
        "source_event_time":         details.ClickTime,        // Could be 0
        "source_event_id":           details.EventID,          // Could be ""
        "source_auditing_token":     details.AuditingToken,   // Could be ""
        "source_attribution_method": details.AttributionMethod, // Could be ""
    },
}
```

**Recommendation:** Add validation to ensure required fields are populated:
```go
if details.AdID != "" && details.ClickTime > 0 && details.AuditingToken != "" {
    // Only include attribution_sources when core fields are present
}
```

#### **Minor: Type Consistency** 
```go
// cmd/watson/structs/structs.go:297-303
ClickTime:             int    `json:"click_time"`           // int
AdEngagementTime:      int64  `json:"ad_engagement_time"`   // int64
```
Consider standardizing time field types (both int64 for timestamp consistency).

#### **Enhancement Opportunity: Logging**
The function lacks logging for debugging attribution_sources content. Consider adding:
```go
log.Logs = append(log.Logs, fmt.Sprintf("attribution_sources built: %s", string(attrSourceJSON)))
```

### 🚀 **Performance & Security**

**Performance**: ✅ Minimal impact
- Single JSON marshal per request when conditions met
- No additional network calls or database queries

**Security**: ✅ Proper handling
- URL encoding applied correctly via `url.QueryEscape`
- No sensitive data exposure in logs
- Maintains existing privacy compliance patterns

### 🧪 **Test Coverage Assessment**

**Excellent coverage** with room for one addition:
- ✅ All 7 sub-fields validation
- ✅ Empty AdID handling  
- ✅ Event ID vs Device ID separation
- ❓ **Missing**: Test for partial field population (e.g., AdID present but ClickTime=0)

**Suggested addition:**
```go
func Test_buildFBEventEnrichmentParams_PartialFields(t *testing.T) {
    // Test scenario where AdID exists but other required fields are zero/empty
}
```

### 📋 **Documentation & Standards Compliance**

**Follows Kochava standards**:
- ✅ Proper Go naming conventions
- ✅ Clear JSON tags following existing patterns  
- ✅ Good test organization with descriptive names
- ✅ Appropriate error handling patterns

**No documentation updates needed** - this is an internal API enhancement that doesn't change external interfaces.

### 🎯 **Final Recommendation**

**APPROVE with minor suggestions**. This PR successfully implements the Meta spec requirements with good engineering practices. The suggested field validation enhancement would make it more robust, but the current implementation is solid and ready for merge.

The coordination with the companion Control PR mentioned in the description should ensure end-to-end functionality.

---

</details>

<details><summary>Classifier output</summary>

{'bucket': 'easy'}

</details>

## 5. Aggregate Analysis

| System | Mean Total | SD | Wins (strict >) | Ties |
|--------|-----------|-----|-----------------|------|
| skwad-cli | 14.6250 | 5.2898 | 8/8 | 0/8 |
| Claude CI | 7.6250 | 4.2067 | 0/8 | 0/8 |

### Per-Criterion Winner
| Criterion | skwad-cli mean | Claude CI mean | Leader |
| ---|---|---|--- |
| Issue Detection | 2.6250 | 1.5000 | skwad-cli |
| Actionability | 2.3750 | 1.7500 | skwad-cli |
| Severity Accuracy | 1.0000 | 0.7500 | skwad-cli |
| Coverage | 2.5000 | 0.8750 | skwad-cli |
| Signal-to-Noise | 1.6250 | 1.1250 | skwad-cli |
| Depth | 2.2500 | 1.2500 | skwad-cli |
| Novel & Substantive Findings | 2.2500 | 0.3750 | skwad-cli |

### Per-Difficulty Winner
| Difficulty | N | skwad-cli mean | Claude CI mean | Leader |
| ---|---|---|---|--- |
| Easy | 3 | 11.3333 | 5.6667 | skwad-cli |
| Medium | 1 | 18.0000 | 3.0000 | skwad-cli |
| Hard | 4 | 16.2500 | 10.2500 | skwad-cli |

### Average Verification Rate (v2 runs only)
- skwad-cli: 70.9% average verification rate across 8 PR(s)
- Claude CI: 60.2% average verification rate across 8 PR(s)

### Inter-Criterion Correlation Matrix (Spearman)
|  | 1 | 2 | 3 | 4 | 5 | 6 | 7 |
| ---|---|---|---|---|---|---|--- |
| 1. Issue Detection | 1.00 | 0.73 | 0.62 | 0.95 | 0.47 | 0.85 | 0.89 |
| 2. Actionability | 0.73 | 1.00 | 0.75 | 0.81 | 0.70 | 0.70 | 0.61 |
| 3. Severity Accuracy | 0.62 | 0.75 | 1.00 | 0.63 | 0.69 | 0.64 | 0.46 |
| 4. Coverage | 0.95 | 0.81 | 0.63 | 1.00 | 0.52 | 0.86 | 0.91 |
| 5. Signal-to-Noise | 0.47 | 0.70 | 0.69 | 0.52 | 1.00 | 0.51 | 0.41 |
| 6. Depth | 0.85 | 0.70 | 0.64 | 0.86 | 0.51 | 1.00 | 0.81 |
| 7. Novel & Substantive Findings | 0.89 | 0.61 | 0.46 | 0.91 | 0.41 | 0.81 | 1.00 |

## 6. Judge Consistency

| Criterion | α | CI lower | CI upper | Flag |
| ---|---|---|---|--- |
| Issue Detection | 0.9184 | 0.7255 | 0.9946 |  |
| Actionability | 0.6777 | 0.3115 | 0.8895 | ⚠️ below gate |
| Severity Accuracy | 0.4963 | 0.0505 | 0.8108 | ⚠️ below gate |
| Coverage | 0.9667 | 0.9119 | 1.0000 |  |
| Signal-to-Noise | 0.5675 | 0.1564 | 0.8437 | ⚠️ below gate |
| Depth | 0.8600 | 0.6396 | 0.9682 |  |
| Novel & Substantive Findings | 0.9297 | 0.8286 | 0.9887 |  |

Gate: α lower-CI-bound ≥ 0.6. Criteria below the gate are flagged (⚠️) as low-reliability caveats and count against pilot_pass (harness validity); they are NOT excluded from the headline statistics, which run on the full 7-criterion total.

## 7. Statistical Significance

### Primary: Wilcoxon Signed-Rank (total score)
- Statistic: 0.0000
- p-value: 0.0078
- n (pairs): 8, n_nonzero: 8

### Effect Size: Cliff's δ
- δ = 0.7812 (large)
- 95% BCa CI: [0.5000, 1.0000]

### Exploratory Tests (BH-FDR adjusted, q=0.05)
| Test | Raw p | BH-adj p | Rejected |
| ---|---|---|--- |
| Issue Detection | 0.0312 | 0.0703 |  |
| Actionability | 0.1875 | 0.2812 |  |
| Severity Accuracy | 0.7500 | 0.7500 |  |
| Coverage | 0.0312 | 0.0703 |  |
| Signal-to-Noise | 0.2891 | 0.3252 |  |
| Depth | 0.0312 | 0.0703 |  |
| Novel & Substantive Findings | 0.0312 | 0.0703 |  |
| Easy | 0.2500 | 0.3214 |  |
| Hard | 0.1250 | 0.2250 |  |

### Position-Bias Check

Run 1 = skwad-cli in slot A; Run 2 = skwad-cli in slot B. A unidirectional position effect on one system but not the other is a more serious blinding failure than a symmetric one. Both tests are reported separately and never collapsed.

skwad-cli position check (run1 A vs run2 B): p = 0.5312
Claude CI position check (run1 B vs run2 A): p = 0.0938

## 8. Threats to Validity

> **1. Cross-model judge — stylistic/structure bias (PRIMARY residual)**: Judge gpt-5.4 is out-of-family vs both review systems (both Claude-based), so same-model self-preference does not apply — a methodological strength. Residual concern: the judge's generic stylistic preferences (structure, verbosity, finding-count) plausibly favor the more-structured multi-agent skwad-cli output and could inflate its margin. Mitigation: the claim-backed count criteria (issue_detection, coverage, depth, novel) are grounding-verified and less style-susceptible; the subjective score-only criteria are more exposed and have the weakest inter-run reliability (§6).

2. **Sample selection bias**: User-curated PR list. Full list and selection criteria disclosed in §3.
3. **Ceiling effects**: Mitigated by 0-3 × 7 criteria = 21-point scale.
4. **Position bias**: Mitigated by counterbalanced A/B; verified via position-bias Wilcoxon (§7).
5. **Runtime variability**: LLM non-determinism mitigated by 3-run median vote.
6. **Prompt sensitivity**: Single rubric/persona version; SHA pinned in manifest (§11).

## 9. Strengths / Weaknesses Analysis

_Threshold heuristic: ≥ 2.0 (top third of 0–3 scale) = relative strength; ≤ 1.0 (bottom third) = relative weakness._

### skwad-cli
**Relative strengths** (mean ≥ 2.0): Issue Detection (mean 2.6250), Actionability (mean 2.3750), Coverage (mean 2.5000), Depth (mean 2.2500), Novel & Substantive Findings (mean 2.2500)
**Relative weaknesses** (mean ≤ 1.0): Severity Accuracy (mean 1.0000)

### Claude CI
**Relative strengths** (mean ≥ 2.0): none
**Relative weaknesses** (mean ≤ 1.0): Severity Accuracy (mean 0.7500), Coverage (mean 0.8750), Novel & Substantive Findings (mean 0.3750)


## 10. Verdict

**H1 supported**: skwad-cli reviews score significantly higher.

- Cliff's δ = 0.7812 (large), 95% BCa CI [0.5000, 1.0000]
- Wilcoxon p = 0.0078

> **Caveat**: The judge is cross-model (gpt-5.4 vs two Claude-based systems), removing same-model self-preference. Any residual judge bias (stylistic/structure preference) would more plausibly **over-state** skwad-cli's wins than under-state them — treat the margin, not the direction, with caution. N=8 with 3 subjective criteria below the reliability gate further argues for confirmation at larger N.

## 11. Appendices

### A. Prompt File Hashes (Reproducibility)
| Key | SHA-256 (truncated) |
| ---|--- |
| rubric_json_sha256 | 1276824250a7c570... |
| judge_team_json_sha256 | 7ef2f9227e4ca0a8... |
| classifier_team_json_sha256 | a24291da8322c817... |
| judge_persona_md_sha256 | c9f2935e0b6cf539... |
| classifier_persona_md_sha256 | ad909606905d9a12... |

### B. Per-Run JSON Files
- Kochava/dash-api#558: 3 run(s)
- Kochava/dash-api#562: 3 run(s)
- Kochava/dash-api#565: 3 run(s)
- Kochava/frontend-mos#1816: 3 run(s)
- Kochava/frontend-mos#1818: 3 run(s)
- Kochava/watson#879: 3 run(s)
- Kochava/watson#890: 3 run(s)
- Kochava/watson#893: 3 run(s)

### C. Position-Bias Check Raw Data
- Kochava/dash-api#558: run1(A=skwad) skwad=18, run2(A=claude_ci) skwad=17
- Kochava/dash-api#562: run1(A=skwad) skwad=18, run2(A=claude_ci) skwad=18
- Kochava/dash-api#565: run1(A=skwad) skwad=17, run2(A=claude_ci) skwad=18
- Kochava/frontend-mos#1816: run1(A=skwad) skwad=14, run2(A=claude_ci) skwad=13
- Kochava/frontend-mos#1818: run1(A=skwad) skwad=17, run2(A=claude_ci) skwad=16
- Kochava/watson#879: run1(A=skwad) skwad=14, run2(A=claude_ci) skwad=15
- Kochava/watson#890: run1(A=skwad) skwad=2, run2(A=claude_ci) skwad=4
- Kochava/watson#893: run1(A=skwad) skwad=16, run2(A=claude_ci) skwad=17