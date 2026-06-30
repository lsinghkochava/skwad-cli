# Independent pilot-statistics verifier

`verify_pilot_stats.py` is a **clean-room oracle** that independently re-derives every statistic in the pilot paper (`eval/output/pilot/paper/skwad-paper.html`) straight from the raw judge JSON, then diffs its fresh values against the paper. It deliberately imports **nothing** from `eval/lib/*` (it only uses `scipy`/`numpy`/`krippendorff` as math engines and parses all raw JSON itself), and it **scrapes** the paper's reference numbers from the HTML rather than hardcoding them — so a match is real evidence, not a tautology.

## Run

```bash
pip install -r eval/verify/requirements.txt
python eval/verify/verify_pilot_stats.py        # exit 0 = all authoritative rows match; nonzero = a real discrepancy
```

The pinned trio (`scipy==1.17.1`, `numpy==2.3.3`, `krippendorff==0.8.2`) matches the manifest's recorded environment. Deterministic rows (Wilcoxon p, point δ, medians, point α, position bias, counts) run on any version; **bootstrap rows (BCa CI, α CI) hard-fail to `ERROR` if the installed versions differ from the pin**, so a version drift never shows up as a false mismatch.

## Outputs

- `eval/output/pilot/verification/verify_results.json` — every recomputed number, structured by section, machine-diffable.
- `eval/output/pilot/verification/verify_trace.md` — the human-readable diff: one row per statistic as `Section | Statistic | Paper (scraped) | Recomputed | Match | Notes`, plus a **Findings** section (e.g. paper inconsistencies) and a final **Verdict**.

## Reading the trace

The **Match** column is the thing to scan: `✓` = reproduced within tolerance; `✗` = an **authoritative** mismatch (these gate the exit code); `diagnostic` = a non-gating comparison (reported for insight, e.g. pooled-vs-mean rates or RNG-path-dependent bootstrap CI bounds); `ERROR` = a bootstrap row skipped on version mismatch; `out-of-scope` = a forward-looking claim (e.g. the planned N=30 run) that isn't derivable from pilot data. Tolerances are stated at the top of the trace. Read the **Findings** section for any real paper errors the oracle surfaced, then the **Verdict** for the pass/fail summary.
