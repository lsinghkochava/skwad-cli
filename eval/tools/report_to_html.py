#!/usr/bin/env python3
"""Convert the research-report.md into a SINGLE self-contained styled HTML file
(research-report.html) for publishing as an Artifact.

Output is BODY CONTENT ONLY — a leading <meta charset="utf-8">, a <title>, one inline
<style>, then the page markup. No <!doctype>/<html>/<head>/<body> (the Artifact wrapper
adds those). The in-body <meta charset> is idempotent under the Artifact path and ensures
the file renders correctly when viewed/served standalone. All CSS inlined; no external
fonts/CDN/images/JS. Faithful: all 11 sections, all PRs, every table, and the
<details> collapsibles (claim traces + raw reviews) preserved.

Usage: python eval/tools/report_to_html.py [<output-dir>]   (default ./eval/output/pilot/)
"""
import html
import os
import re
import sys

# --------------------------------------------------------------------------- inline md

def esc(s: str) -> str:
    return html.escape(s or "", quote=False)


def inline(text: str) -> str:
    """Escape, then apply `code`, **bold**, [link](url) — order is escape-first so the
    markdown markers (which html.escape leaves untouched) still match."""
    t = html.escape(text or "", quote=False)
    t = re.sub(r"`([^`]+)`", r"<code>\1</code>", t)
    t = re.sub(r"\*\*([^*]+)\*\*", r"<strong>\1</strong>", t)
    t = re.sub(r"\[([^\]]+)\]\(([^)]+)\)", r'<a href="\2">\1</a>', t)
    return t


_OUTCOME_CLASS = {
    "verified": "verified",
    "contradicted": "contradicted",
    "unverified": "neutral",
    "non_falsifiable": "neutral",
}


def _row_cells(line: str) -> list[str]:
    """Split a markdown table row `| a | b |` into trimmed cells, on UNESCAPED `|` only
    (so `\\|` inside a cell stays in the cell), then unescape `\\|` → `|`."""
    parts = re.split(r"(?<!\\)\|", line.strip())
    if parts and parts[0].strip() == "":
        parts = parts[1:]
    if parts and parts[-1].strip() == "":
        parts = parts[:-1]
    return [p.replace("\\|", "|").strip() for p in parts]


def _is_sep(line: str) -> bool:
    return bool(re.match(r"^\s*\|[\s:|-]+\|\s*$", line))


def render_table(header: list[str], rows: list[list[str]]) -> str:
    is_claim = header[:4] == ["Claim", "Outcome", "Tools", "Evidence"]
    is_crit = header[:3] == ["Criterion", "skwad-cli", "Claude CI"]
    ths = "".join(f"<th>{inline(h)}</th>" for h in header)
    body = []
    for r in rows:
        # pad/truncate to header width
        cells = (r + [""] * len(header))[: len(header)]
        is_total = cells and cells[0].replace("*", "").strip().lower() == "total"
        win_idx = None
        if is_crit and is_total:
            def num(c):
                m = re.search(r"-?\d+", c.replace("*", ""))
                return int(m.group()) if m else None
            a, b = num(cells[1]), num(cells[2])
            if a is not None and b is not None and a != b:
                win_idx = 1 if a > b else 2
        tds = []
        for i, c in enumerate(cells):
            cls = []
            inner = inline(c)
            if is_claim and i == 1:  # Outcome cell → semantic badge
                oc = _OUTCOME_CLASS.get(c.strip().lower(), "neutral")
                inner = f'<span class="badge out-{oc}">{esc(c.strip())}</span>'
            if is_claim and i == 3:  # Evidence cell → mono
                cls.append("mono-cell")
            if win_idx is not None and i == win_idx:
                cls.append("win")
            clsattr = f' class="{" ".join(cls)}"' if cls else ""
            tds.append(f"<td{clsattr}>{inner}</td>")
        trcls = ' class="total"' if is_total else ""
        body.append(f"<tr{trcls}>{''.join(tds)}</tr>")
    return (
        '<div class="tablewrap"><table>'
        f"<thead><tr>{ths}</tr></thead>"
        f"<tbody>{''.join(body)}</tbody>"
        "</table></div>"
    )


# --------------------------------------------------------------------------- block parse

def convert_blocks(lines: list[str]) -> str:
    out: list[str] = []
    i, n = 0, len(lines)
    para: list[str] = []
    quote: list[str] = []

    def flush_para():
        if para:
            out.append(f"<p>{inline(' '.join(para))}</p>")
            para.clear()

    def flush_quote():
        if quote:
            out.append(f'<blockquote class="caution">{inline(" ".join(quote))}</blockquote>')
            quote.clear()

    while i < n:
        line = lines[i]
        stripped = line.strip()

        # code fence
        if stripped.startswith("```"):
            flush_para(); flush_quote()
            code: list[str] = []
            i += 1
            while i < n and not lines[i].strip().startswith("```"):
                code.append(lines[i]); i += 1
            i += 1  # closing fence
            out.append(f"<pre><code>{esc(chr(10).join(code))}</code></pre>")
            continue

        # details / summary (raw HTML already in the md)
        if stripped.startswith("<details>"):
            flush_para(); flush_quote()
            m = re.search(r"<summary>(.*?)</summary>", stripped)
            summ = m.group(1) if m else ""
            out.append(f"<details><summary>{inline(summ)}</summary><div class=\"det-body\">")
            i += 1
            continue
        if stripped.startswith("</details>"):
            flush_para(); flush_quote()
            out.append("</div></details>")
            i += 1
            continue

        # blockquote (caution)
        if stripped.startswith(">"):
            flush_para()
            quote.append(stripped.lstrip(">").strip())
            i += 1
            continue
        else:
            flush_quote()

        # table: a row followed by a separator row
        if stripped.startswith("|") and i + 1 < n and _is_sep(lines[i + 1]):
            flush_para()
            header = _row_cells(stripped)
            i += 2  # skip header + separator
            rows = []
            while i < n and lines[i].strip().startswith("|"):
                if not _is_sep(lines[i]):
                    rows.append(_row_cells(lines[i]))
                i += 1
            out.append(render_table(header, rows))
            continue

        # sub-headings inside a section
        if stripped.startswith("#### "):
            flush_para(); out.append(f"<h4>{inline(stripped[5:])}</h4>"); i += 1; continue
        if stripped.startswith("### "):
            flush_para(); out.append(f'<h3>{inline(stripped[4:])}</h3>'); i += 1; continue

        # list
        if stripped.startswith("- "):
            flush_para()
            items = []
            while i < n and lines[i].strip().startswith("- "):
                items.append(f"<li>{inline(lines[i].strip()[2:])}</li>")
                i += 1
            out.append(f"<ul>{''.join(items)}</ul>")
            continue

        # blank line ends a paragraph
        if not stripped:
            flush_para()
            i += 1
            continue

        para.append(stripped)
        i += 1

    flush_para(); flush_quote()
    return "\n".join(out)


# --------------------------------------------------------------------------- hero data

def extract_hero(text: str) -> dict:
    h: dict = {}
    m = re.search(r"\*Generated:\s*([\d-]+)[^|]*\|\s*N=(\d+)", text)
    h["date"] = m.group(1) if m else ""
    h["n"] = m.group(2) if m else "?"
    m = re.search(r"skwad-cli wins \(strict >\):\s*(\d+)", text)
    h["wins"] = m.group(1) if m else "?"
    m = re.search(r"Cliff's δ\s*=\s*([\d.]+)\s*\(([^)]+)\)", text)
    if m:
        h["delta"], h["delta_interp"] = f"{float(m.group(1)):.2f}", m.group(2)  # clean 2dp for hero
    else:
        h["delta"], h["delta_interp"] = "?", ""
    m = re.search(r"Wilcoxon p-value:\s*([\d.]+)", text)
    h["p"] = m.group(1) if m else "?"
    m = re.search(r"\*\*Judge\*\*:\s*(.+)", text)
    h["judge"] = m.group(1).strip().rstrip(".") if m else ""
    # grounding range: last cell (%) of verification-summary rows (start Skwad / Claude CI)
    grs = []
    for ln in text.splitlines():
        if ln.strip().startswith(("| Skwad |", "| Claude CI |")):
            cells = _row_cells(ln)
            mm = re.match(r"(\d+)%", cells[-1]) if cells else None
            if mm:
                grs.append(int(mm.group(1)))
    h["grounding"] = f"{min(grs)}–{max(grs)}%" if grs else "n/a"
    return h


# --------------------------------------------------------------------------- assemble

STYLE = """
:root{
--ground:#E8EDF2;--surface:#FBFCFE;--ink:#15202B;--muted:#5B6B7D;
--hair:#C9D4DF;--hair-soft:#DCE4EC;--panel:#F1F4F8;
--accent:#3A4FE0;--accent-soft:#E2E6FB;
--amber:#E8843C;--amber-soft:#FBE8D6;
--caution:#E8843C;--caution-soft:#FBE8D6;
--verified:#1F9D6B;--verified-soft:#DBF1E8;
--contradicted:#D14545;--contradicted-soft:#F7DEDE;
--neutral:#5B6B7D;--neutral-soft:#E7ECF1;
--serif:"Iowan Old Style","Charter",Georgia,"Times New Roman",serif;
--mono:ui-monospace,"SF Mono","JetBrains Mono",Menlo,Consolas,monospace;
--sans:system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
}
*{box-sizing:border-box}
body{margin:0;color:var(--ink);font:16px/1.62 var(--sans);
background-color:var(--ground);
background-image:linear-gradient(var(--hair-soft) 1px,transparent 1px),linear-gradient(90deg,var(--hair-soft) 1px,transparent 1px);
background-size:28px 28px;-webkit-font-smoothing:antialiased;}
a{color:var(--accent);text-decoration:none}
a:hover{text-decoration:underline}
.page{max-width:1100px;margin:0 auto;padding:32px 24px 80px;display:grid;gap:28px;grid-template-columns:1fr}
main{display:flex;flex-direction:column;gap:20px;min-width:0}
.eyebrow,.nav,.statlabel,th,.sec-num,summary,.badge{
font-family:var(--mono);text-transform:uppercase;letter-spacing:.14em;}
.eyebrow{font-size:12px;color:var(--accent);font-weight:600}
/* hero */
.hero{background:var(--surface);border:1px solid var(--hair);border-top:3px solid var(--accent);
border-radius:14px;padding:30px 30px 26px;display:flex;flex-direction:column;gap:18px;box-shadow:0 1px 0 #fff inset}
.hero h1{margin:0;font:700 clamp(30px,4.6vw,42px)/1.05 var(--serif);letter-spacing:-.015em}
.statrow{display:flex;flex-wrap:wrap;gap:14px}
.stat{flex:1 1 160px;background:var(--accent-soft);border:1px solid var(--hair);border-radius:10px;padding:14px 16px}
.statfig{font-family:var(--mono);font-weight:700;font-size:26px;color:var(--accent);line-height:1.1}
.statlabel{font-size:11px;color:var(--muted);margin-top:6px;letter-spacing:.06em}
.judgeline{font-family:var(--mono);font-size:12px;color:var(--muted);text-transform:none;letter-spacing:0}
/* nav rail */
.rail{font-family:var(--mono)}
.rail ol{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:2px}
.rail a{display:block;padding:6px 9px;font-size:12px;color:var(--muted);border-radius:7px}
.rail a:hover{background:var(--accent-soft);color:var(--accent);text-decoration:none}
.rail .rn{color:var(--accent);font-weight:700;margin-right:8px}
/* sections */
section.card{background:var(--surface);border:1px solid var(--hair);border-radius:14px;padding:26px 28px;min-width:0;box-shadow:0 1px 0 #fff inset}
.sec-num{font-size:12px;color:var(--accent);font-weight:700}
section.card>h2{margin:.25em 0 .55em;font:700 clamp(23px,2.8vw,30px)/1.12 var(--serif);letter-spacing:-.015em}
h3{margin:1.4em 0 .5em;font:700 20px/1.25 var(--serif)}
h4{margin:1.1em 0 .35em;font:700 14px/1.3 var(--sans)}
p{margin:.6em 0}
ul{margin:.5em 0;padding-left:1.2em}
li{margin:.2em 0}
/* caution */
blockquote.caution{margin:.9em 0;padding:14px 16px;background:var(--caution-soft);
border-left:3px solid var(--caution);border-radius:0 8px 8px 0;color:var(--ink)}
/* tables */
.tablewrap{overflow-x:auto;margin:.9em 0;border:1px solid var(--hair);border-radius:10px}
table{border-collapse:collapse;width:100%;font-size:13.5px}
th{font-size:11px;background:var(--panel);color:var(--muted);text-align:left;
padding:8px 10px;border-bottom:1px solid var(--hair);white-space:nowrap;letter-spacing:.05em}
td{padding:7px 10px;border-bottom:1px solid var(--hair-soft);vertical-align:top}
tbody tr:nth-child(even){background:var(--panel)}
tbody tr:last-child td{border-bottom:none}
tr.total td{font-weight:700;font-family:var(--mono)}
td.win{background:var(--accent-soft)}
td.mono-cell{font-family:var(--mono);font-size:12px;color:var(--muted);white-space:pre-wrap}
.badge{display:inline-block;font-size:11px;padding:2px 9px;border-radius:999px;font-weight:700;letter-spacing:.04em}
.out-verified{background:var(--verified-soft);color:var(--verified)}
.out-contradicted{background:var(--contradicted-soft);color:var(--contradicted)}
.out-neutral{background:var(--neutral-soft);color:var(--neutral)}
/* collapsibles */
details{margin:.6em 0;border:1px solid var(--hair);border-radius:10px;overflow:hidden}
summary{cursor:pointer;list-style:none;padding:10px 13px;font-size:12px;color:var(--accent);
background:var(--surface);letter-spacing:.1em}
summary::-webkit-details-marker{display:none}
summary::before{content:"\\25B8";margin-right:8px;color:var(--accent)}
details[open]>summary::before{content:"\\25BE"}
.det-body{padding:8px 14px 12px;margin-left:6px;border-left:1px solid var(--hair)}
.det-body p{font-family:var(--sans)}
pre{background:var(--panel);border:1px solid var(--hair-soft);border-radius:8px;padding:12px 14px;
overflow-x:auto;margin:.6em 0}
code{font-family:var(--mono);font-size:12.5px}
pre code{font-size:12px;line-height:1.5;color:var(--ink)}
:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
@media (min-width:1000px){
.page{grid-template-columns:210px 1fr;align-items:start}
.rail{position:sticky;top:24px}
}
@media (max-width:999px){
.rail ol{flex-direction:row;overflow-x:auto;gap:4px;padding-bottom:6px}
.rail a{white-space:nowrap}
}
@media (prefers-reduced-motion:reduce){*{transition:none!important;animation:none!important}}
"""


def build_html(md_text: str) -> str:
    hero = extract_hero(md_text)
    lines = md_text.splitlines()

    # Split into sections at "## N. Title". Pre-section content (H1 + generated) → hero only.
    sections: list[dict] = []
    cur: dict | None = None
    for ln in lines:
        m = re.match(r"^##\s+(\d+)\.\s+(.*)$", ln)
        if m:
            cur = {"num": m.group(1), "title": m.group(2).strip(), "lines": []}
            sections.append(cur)
        elif cur is not None:
            cur["lines"].append(ln)

    nav_items = "".join(
        f'<li><a href="#sec-{s["num"]}"><span class="rn">{int(s["num"]):02d}</span>'
        f'{esc(s["title"])}</a></li>'
        for s in sections
    )
    nav = f'<nav class="rail" aria-label="Sections"><ol>{nav_items}</ol></nav>'

    sec_html = []
    for s in sections:
        body = convert_blocks(s["lines"])
        sec_html.append(
            f'<section class="card" id="sec-{s["num"]}">'
            f'<div class="sec-num">{int(s["num"]):02d}</div>'
            f'<h2>{esc(s["title"])}</h2>{body}</section>'
        )

    hero_html = (
        '<header class="hero">'
        f'<div class="eyebrow">EMPIRICAL EVALUATION · {esc(hero["date"])} · N={esc(hero["n"])}</div>'
        '<h1>skwad-cli vs Claude CI — Review Quality</h1>'
        '<div class="statrow">'
        f'<div class="stat"><div class="statfig">{esc(hero["wins"])}/{esc(hero["n"])} wins</div>'
        '<div class="statlabel">skwad-cli (strict &gt;)</div></div>'
        f'<div class="stat"><div class="statfig">δ {esc(hero["delta"])}</div>'
        f'<div class="statlabel">Cliff\'s ({esc(hero["delta_interp"])})</div></div>'
        f'<div class="stat"><div class="statfig">p {esc(hero["p"])}</div>'
        '<div class="statlabel">Wilcoxon</div></div>'
        '</div>'
        f'<div class="judgeline">{esc(hero["judge"])} · grounded {esc(hero["grounding"])}</div>'
        '</header>'
    )

    return (
        '<meta charset="utf-8">\n'
        "<title>skwad-cli vs Claude CI — Review Quality</title>\n"
        f"<style>{STYLE}</style>\n"
        '<div class="page">\n'
        f"{nav}\n"
        f"<main>{hero_html}\n{''.join(sec_html)}</main>\n"
        "</div>\n"
    )


def main():
    out_dir = sys.argv[1] if len(sys.argv) > 1 else "./eval/output/pilot/"
    md_path = os.path.join(out_dir, "research-report.md")
    html_path = os.path.join(out_dir, "research-report.html")
    with open(md_path, encoding="utf-8") as f:
        md_text = f.read()
    html_out = build_html(md_text)
    with open(html_path, "w", encoding="utf-8") as f:
        f.write(html_out)
    print(f"HTML rendered: {html_path}  ({len(html_out):,} bytes)")


if __name__ == "__main__":
    main()
