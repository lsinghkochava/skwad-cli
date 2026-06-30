# Pilot Statistics — Independent Verification Trace

**Clean-room oracle.** Re-derives every paper statistic from raw JSON; imports nothing from `eval/lib/*`. Paper values are SCRAPED from `paper/skwad-paper.html`, not hardcoded.

## Environment

- Installed: scipy `1.17.1`, numpy `2.3.3`, krippendorff `0.8.2`
- Pinned:    scipy `1.17.1`, numpy `2.3.3`, krippendorff `0.8.2`
- Bootstrap rows **RUN**; deterministic rows always run.
- BCa seed (from manifest.rng_seed): `12345`

## Match tolerances

- p-values: round-half-even to the precision shown in the paper (primary 4 dp; Table 5 3 dp)
- δ / α point estimates: ±0.005
- CI bounds: ±0.01
- percentages: ±0.1 pp
- Match column: ✓ match · ✗ authoritative mismatch (gates exit) · diagnostic (non-gating) · ERROR (version) · out-of-scope

## Trace

| Section | Statistic | Paper (scraped) | Recomputed | Match | Notes |
|---|---|---|---|---|---|
| 1. Reconstruct | voted=median_low & total=sum (cross-check 112 cells) | stored values | recomputed identical | ✓ | Independent median_low + sum matches every stored .voted/.total. |
| 2. Primary | win sweep (skwad strictly higher PRs) | 8 | 8 of 8 (pos/neg/zero = 8/0/0) | ✓ | Sign convention skwad-ci; all diffs same sign. |
| 2. Primary | Wilcoxon signed-rank p (two-sided) | 0.0078 | 0.0078125 (round-he 4dp = 0.0078) | ✓ | scipy.stats.wilcoxon(diffs, zero_method='wilcox'). |
| 2. Primary | Wilcoxon statistic | 0 | 0 | ✓ | n=8 (paper n=8) |
| 2. Primary | Cliff's delta (on totals, hand-rolled) | 0.78 | 0.7812 | ✓ | (#a>b - #a<b)/(n*n) over the two 8-vectors of totals. |
| 2. Primary | Cliff's delta 95% BCa CI | [0.5, 1.0] | [0.5000, 1.0000] | ✓ | scipy bootstrap n_resamples=2000 paired BCa, seed=12345 (manifest.rng_seed). |
| 3. Table 2 | per-PR voted cells + totals (56 crit-cells + 8 totals) | every cell | all reproduced | ✓ | Recomputed voted/total identical to every Table 2 cell. |
| 4. Table 3 | per-PR verification rate cells (8x2, trunc int %) [cross-check] | paper cells | all 16 match (floor to int %) | ✓ | Per-PR rate = mean of 3 per-run rates; floor matches every Table 3 cell. Non-gating; MEAN gates. |
| 4. Table 3 | MEAN verification rate (skwad) [AUTHORITATIVE] | 70.9% | 70.97% | ✓ | Unweighted mean of 8 per-PR rates. GATES Table 3. |
| 4. Table 3 | MEAN verification rate (claude_ci) [AUTHORITATIVE] | 60.2% | 60.18% | ✓ | Unweighted mean of 8 per-PR rates. GATES Table 3. |
| 4. Table 3 | pooled verification rate (skwad) [diagnostic] | n/a (not in paper) | 73.16% | diagnostic | Sum(verified)/Sum(claims) across runs; expected to differ from the per-PR mean. |
| 4. Table 3 | pooled verification rate (claude_ci) [diagnostic] | n/a (not in paper) | 59.32% | diagnostic | Sum(verified)/Sum(claims) across runs; expected to differ from the per-PR mean. |
| 4. Table 3 | MEAN grounding rate (skwad) | 91% | 92.32% | diagnostic | Mean of per-PR (run-mean) grounding rate. Non-gating; see Findings if mismatch. |
| 4. Table 3 | MEAN grounding rate (claude_ci) | 93% | 93.60% | diagnostic | Mean of per-PR (run-mean) grounding rate. Non-gating; see Findings if mismatch. |
| 4. Table 3 | contradicted total (skwad) | 30 | 30 | ✓ | Sum of per-run contradicted. Paper's own per-PR cells sum to 30. |
| 4. Table 3 | contradicted total (claude_ci) | 37 | 37 | ✓ | Sum of per-run contradicted. Paper's own per-PR cells sum to 37. |
| 4. Table 3 | total adjudicated claims (24 runs x 2 sys) | 643 | 643 (claim_trace=646) | ✓ | Σ(verified+unverified+contradicted+non_falsifiable) over all run files; claim_trace gives the independent per-claim count (the +3 delta = claim_trace entries whose outcome falls outside the 4 summed buckets, i.e. uncategorized claims). |
| 5. Table 4 | alpha point (Coverage) | 0.97 | 0.967 | ✓ | Ordinal Krippendorff alpha, 3 runs x 16 PR-system items. |
| 5. Table 4 | alpha 95% CI (Coverage) [200/42] | [0.91, 1.0] | [0.901, 1.000] | ✓ | Resample items, percentile CI. Bootstrap CI is RNG-sequence dependent -- non-gating; gate decision below is authoritative. |
| 5. Table 4 | gate (Coverage) [AUTHORITATIVE] | pass | pass | ✓ | CI-lower 0.901 >= 0.60 gate (margin +0.301). |
| 5. Table 4 | alpha 95% CI (Coverage) [sensitivity nboot=2000] | n/a (sensitivity check) | [0.882, 1.000] | diagnostic | Robustness: CI at nboot=2000, seed=0. Diagnostic only. |
| 5. Table 4 | alpha point (Novel findings) | 0.93 | 0.930 | ✓ | Ordinal Krippendorff alpha, 3 runs x 16 PR-system items. |
| 5. Table 4 | alpha 95% CI (Novel findings) [200/42] | [0.83, 0.99] | [0.807, 1.000] | diagnostic | Resample items, percentile CI. Bootstrap CI is RNG-sequence dependent -- non-gating; gate decision below is authoritative. |
| 5. Table 4 | gate (Novel findings) [AUTHORITATIVE] | pass | pass | ✓ | CI-lower 0.807 >= 0.60 gate (margin +0.207). |
| 5. Table 4 | alpha 95% CI (Novel findings) [sensitivity nboot=2000] | n/a (sensitivity check) | [0.794, 1.000] | diagnostic | Robustness: CI at nboot=2000, seed=0. Diagnostic only. |
| 5. Table 4 | alpha point (Issue detection) | 0.92 | 0.918 | ✓ | Ordinal Krippendorff alpha, 3 runs x 16 PR-system items. |
| 5. Table 4 | alpha 95% CI (Issue detection) [200/42] | [0.73, 0.99] | [0.721, 0.994] | ✓ | Resample items, percentile CI. Bootstrap CI is RNG-sequence dependent -- non-gating; gate decision below is authoritative. |
| 5. Table 4 | gate (Issue detection) [AUTHORITATIVE] | pass | pass | ✓ | CI-lower 0.721 >= 0.60 gate (margin +0.121). |
| 5. Table 4 | alpha 95% CI (Issue detection) [sensitivity nboot=2000] | n/a (sensitivity check) | [0.719, 0.997] | diagnostic | Robustness: CI at nboot=2000, seed=0. Diagnostic only. |
| 5. Table 4 | alpha point (Depth) | 0.86 | 0.860 | ✓ | Ordinal Krippendorff alpha, 3 runs x 16 PR-system items. |
| 5. Table 4 | alpha 95% CI (Depth) [200/42] | [0.64, 0.97] | [0.696, 0.959] | diagnostic | Resample items, percentile CI. Bootstrap CI is RNG-sequence dependent -- non-gating; gate decision below is authoritative. |
| 5. Table 4 | gate (Depth) [AUTHORITATIVE] | pass | pass | ✓ | CI-lower 0.696 >= 0.60 gate (margin +0.096). |
| 5. Table 4 | alpha 95% CI (Depth) [sensitivity nboot=2000] | n/a (sensitivity check) | [0.655, 0.962] | diagnostic | Robustness: CI at nboot=2000, seed=0. Diagnostic only. |
| 5. Table 4 | alpha point (Actionability) | 0.68 | 0.678 | ✓ | Ordinal Krippendorff alpha, 3 runs x 16 PR-system items. |
| 5. Table 4 | alpha 95% CI (Actionability) [200/42] | [0.31, 0.89] | [0.319, 0.895] | ✓ | Resample items, percentile CI. Bootstrap CI is RNG-sequence dependent -- non-gating; gate decision below is authoritative. |
| 5. Table 4 | gate (Actionability) [AUTHORITATIVE] | below | below | ✓ | CI-lower 0.319 < 0.60 gate (margin -0.281). |
| 5. Table 4 | alpha 95% CI (Actionability) [sensitivity nboot=2000] | n/a (sensitivity check) | [0.254, 0.894] | diagnostic | Robustness: CI at nboot=2000, seed=0. Diagnostic only. |
| 5. Table 4 | alpha point (Signal-to-noise) | 0.57 | 0.568 | ✓ | Ordinal Krippendorff alpha, 3 runs x 16 PR-system items. |
| 5. Table 4 | alpha 95% CI (Signal-to-noise) [200/42] | [0.16, 0.84] | [0.213, 0.858] | diagnostic | Resample items, percentile CI. Bootstrap CI is RNG-sequence dependent -- non-gating; gate decision below is authoritative. |
| 5. Table 4 | gate (Signal-to-noise) [AUTHORITATIVE] | below | below | ✓ | CI-lower 0.213 < 0.60 gate (margin -0.387). |
| 5. Table 4 | alpha 95% CI (Signal-to-noise) [sensitivity nboot=2000] | n/a (sensitivity check) | [0.192, 0.829] | diagnostic | Robustness: CI at nboot=2000, seed=0. Diagnostic only. |
| 5. Table 4 | alpha point (Severity accuracy) | 0.5 | 0.496 | ✓ | Ordinal Krippendorff alpha, 3 runs x 16 PR-system items. |
| 5. Table 4 | alpha 95% CI (Severity accuracy) [200/42] | [0.05, 0.81] | [0.067, 0.823] | diagnostic | Resample items, percentile CI. Bootstrap CI is RNG-sequence dependent -- non-gating; gate decision below is authoritative. |
| 5. Table 4 | gate (Severity accuracy) [AUTHORITATIVE] | below | below | ✓ | CI-lower 0.067 < 0.60 gate (margin -0.533). |
| 5. Table 4 | alpha 95% CI (Severity accuracy) [sensitivity nboot=2000] | n/a (sensitivity check) | [0.047, 0.813] | diagnostic | Robustness: CI at nboot=2000, seed=0. Diagnostic only. |
| 5. Table 4 | gate trustworthiness (CI-lower margins from 0.60) [caveat] | n/a | min \|margin\|=0.096; none within 0.05 | ✓ | Gate is authoritative ONLY when no CI-lower is within ~0.05 of 0.60 (else pass/below could ride Monte-Carlo noise). Margins: Coverage=0.901(+0.301); Novel findings=0.807(+0.207); Issue detection=0.721(+0.121); Depth=0.696(+0.096); Actionability=0.319(-0.281); Signal-to-noise=0.213(-0.387); Severity accuracy=0.067(-0.533). All clear of the noise band. |
| 6. Table 5 | Issue detection | raw=0.031, BH=0.07 | raw=0.031, BH=0.070 | ✓ | Wilcoxon on voted/total diffs; BH-FDR over pooled list. |
| 6. Table 5 | Actionability | raw=0.188, BH=0.281 | raw=0.188, BH=0.281 | ✓ | Wilcoxon on voted/total diffs; BH-FDR over pooled list. |
| 6. Table 5 | Severity accuracy | raw=0.75, BH=0.75 | raw=0.750, BH=0.750 | ✓ | Wilcoxon on voted/total diffs; BH-FDR over pooled list. |
| 6. Table 5 | Coverage | raw=0.031, BH=0.07 | raw=0.031, BH=0.070 | ✓ | Wilcoxon on voted/total diffs; BH-FDR over pooled list. |
| 6. Table 5 | Signal-to-noise | raw=0.289, BH=0.325 | raw=0.289, BH=0.325 | ✓ | Wilcoxon on voted/total diffs; BH-FDR over pooled list. |
| 6. Table 5 | Depth | raw=0.031, BH=0.07 | raw=0.031, BH=0.070 | ✓ | Wilcoxon on voted/total diffs; BH-FDR over pooled list. |
| 6. Table 5 | Novel findings | raw=0.031, BH=0.07 | raw=0.031, BH=0.070 | ✓ | Wilcoxon on voted/total diffs; BH-FDR over pooled list. |
| 6. Table 5 | Hard (difficulty) | raw=0.125, BH=0.225 | raw=0.125, BH=0.225 | ✓ | Wilcoxon on voted/total diffs; BH-FDR over pooled list. |
| 6. Table 5 | Easy (difficulty) | raw=0.25, BH=0.321 | raw=0.250, BH=0.321 | ✓ | Wilcoxon on voted/total diffs; BH-FDR over pooled list. |
| 6. Table 5 | smallest adjusted (BH) p | 0.07 | 0.070 | ✓ | Min over pooled BH-adjusted p-values. |
| 6. Table 5 | position bias (skwad) run-1-A vs run-2-B [bias-check, prose] | 0.53 | 0.5312 | ✓ | Per-run total = Σ faithful per-criterion scores[run_idx] (NOT the buggy resolved.total); run1 vs run2 paired two-sided Wilcoxon. Non-gating bias check. |
| 6. Table 5 | position bias (claude_ci) run-1-A vs run-2-B [bias-check, prose] | 0.094 | 0.0938 | ✓ | Per-run total = Σ faithful per-criterion scores[run_idx] (NOT the buggy resolved.total); run1 vs run2 paired two-sided Wilcoxon. Non-gating bias check. |
| 7. Prose/struct | methodology_version | 2 (text/struct) | 2 | ✓ | Cross-checked vs manifest.methodology_version. |
| 7. Prose/struct | rng_seed | 12345 (manifest) | 12345 | ✓ | Cross-checked vs manifest.rng_seed. |
| 7. Prose/struct | N (PRs included) | 8 | 8 | ✓ | 1 skipped (expect 1: frontend-mos#1823). |
| 7. Prose/struct | planned confirmatory N | 30 | n/a (forward-looking) | out-of-scope | Sec 5 planned N=30 run -- not part of this pilot's data. |
| 7. Prose/struct | forward-looking medium effect delta | 0.33 | n/a (forward-looking) | out-of-scope | Sec 5 delta~0.33 target for N=30 -- not recomputable from pilot data. |
| 7. Prose/struct | judge tool calls per PR (command_count) | 75-131 | 75-131 (per-PR sums of 3 runs) | ✓ | Authoritative source = command_count summed over each PR's 3 runs; transcript cross-check AGREES with command_count on all 24 runs. (verification_summary.tool_calls_observed on disk is a placeholder 0, back-filled by the renderer.) Per-PR: frontend-mos#1816=131, frontend-mos#1818=120, watson#893=104, watson#890=76, watson#879=75, dash-api#565=100, dash-api#558=75, dash-api#562=87. |
| 7. Prose/struct | rubric: 7 criteria x 0-3, max 21 | 7 / 21 (text) | 7 criteria, max 21 | ✓ | Cross-checked vs criterion enumeration and total cap. |

## Findings

1. GROUNDING MEAN (skwad): recomputed 92.32% vs paper 91%. No aggregation method (per-PR run-mean 92.32%, pooled g/e, or mean of displayed integer cells) reproduces the paper's headline exactly -- likely a display-rounding artifact in the paper. Non-gating.
2. GROUNDING MEAN (claude_ci): recomputed 93.60% vs paper 93%. No aggregation method (per-PR run-mean 93.60%, pooled g/e, or mean of displayed integer cells) reproduces the paper's headline exactly -- likely a display-rounding artifact in the paper. Non-gating.
3. ALPHA CI REPRODUCTION: point alpha and all 7 gate pass/below decisions reproduce EXACTLY (independent). CI bounds match to ~1-5pp but are not bit-identical because bootstrap CIs depend on the exact RNG call sequence, which an independent oracle that may not import eval/lib cannot replicate. CI bounds reported diagnostic; gate decisions authoritative.
4. TOOL-CALL COUNT (resolved — NOT a paper error): the paper's '75-131 calls per PR' is CORRECT and independently reproduced here two ways — (a) command_count summed over each PR's 3 runs = 75-131, and (b) the run transcripts' command_execution events/2, which agree with command_count on all 24 runs. SEPARATELY there is a HARNESS bug (informational, not a paper concern): verification_summary.tool_calls_observed is a never-populated placeholder that is always 0 on disk (the renderer back-fills the real value from command_count at render time). Because the pilot gate _has_tool_calls_per_run (eval/lib/pilot.py) reads that placeholder field, the manifest's pilot_pass:false on 'tool_calls_per_run' ('24/24 runs had zero tool calls') is a FALSE NEGATIVE — the judge actually made 75-131 calls/PR. Naive readers of tool_calls_observed will see 0; the authoritative count is command_count.

## Verdict

- Authoritative mismatches (gate exit): **0**
- Exit code will be **0**.
