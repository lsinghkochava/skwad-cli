"""Verbatim `codex exec --json` trace samples (codex-cli 0.141.0, captured live by
the Explorer) — the SINGLE SOURCE OF TRUTH for `_parse_codex_trace` tests.

Both the parser implementation and its tests reference these exact lines so the
canned fixtures can't drift from the real envelope shape. Each value is ONE JSONL
event line (an `item.completed` / `command_execution` event, except `TURN_COMPLETED`).

Key facts these samples lock:
- Envelope: {"type":"item.completed","item":{"id","type":"command_execution",
  "command":"/bin/zsh -lc '<inner>'","aggregated_output","exit_code","status"}}
- rg no-match → exit_code:1 + empty output + status:"failed"  (branch on exit_code, NOT status)
- rg dir-search → path-prefixed lines (path:line:text); rg single-file → no prefix (line:text)
- echo / python3 -c → output looks like content but command is synthesis (unattributable)
- `cat f | sed 's/…/…/'` → output MUTATED (transform stage → unattributable)
"""

import json

# (1) rg dir-search — path-PREFIXED match line
RG_DIR_PRESENCE = json.dumps({
    "type": "item.completed",
    "item": {"id": "item_1", "type": "command_execution",
             "command": "/bin/zsh -lc 'rg -n --no-heading \"move_to_end\" .'",
             "aggregated_output": "./cache.py:8:            self.store.move_to_end(key)  # LRU recency bump\n",
             "exit_code": 0, "status": "completed"}})

# (2) rg single-file — NO path prefix (line:text), attribute to the file arg
RG_FILE_PRESENCE = json.dumps({
    "type": "item.completed",
    "item": {"id": "item_2", "type": "command_execution",
             "command": "/bin/zsh -lc 'rg -n capacity cache.py'",
             "aggregated_output": "2:    def __init__(self, capacity):\n3:        self.capacity = capacity\n",
             "exit_code": 0, "status": "completed"}})

# (3) rg no-match — ABSENCE: empty output, exit_code 1, status "failed" (NOT an error)
RG_ABSENCE = json.dumps({
    "type": "item.completed",
    "item": {"id": "item_3", "type": "command_execution",
             "command": "/bin/zsh -lc 'rg -n --hidden --no-ignore-vcs \"zzz_nonexistent_func_42\" .'",
             "aggregated_output": "", "exit_code": 1, "status": "failed"}})

# (4) cat <file> — read-like, whole output attributed to the file
CAT_READ = json.dumps({
    "type": "item.completed",
    "item": {"id": "item_4", "type": "command_execution",
             "command": "/bin/zsh -lc 'cat cache.py'",
             "aggregated_output": "class LRUCache:\n    def __init__(self, capacity):\n        self.capacity = capacity\n",
             "exit_code": 0, "status": "completed"}})

# (5) sed -n '1,8p' — read-like (print range); DOUBLE-quoted wrapper (inner has single quotes)
SED_READ = json.dumps({
    "type": "item.completed",
    "item": {"id": "item_5", "type": "command_execution",
             "command": "/bin/zsh -lc \"sed -n '1,8p' cache.py\"",
             "aggregated_output": "class LRUCache:\n    def __init__(self, capacity):\n",
             "exit_code": 0, "status": "completed"}})

# (6) 🔴 echo — synthesis: output looks like code but came from NO file (fabrication hole)
ECHO_FABRICATION = json.dumps({
    "type": "item.completed",
    "item": {"id": "item_6", "type": "command_execution",
             "command": "/bin/zsh -lc \"echo 'def secret_backdoor(): pass'\"",
             "aggregated_output": "def secret_backdoor(): pass\n",
             "exit_code": 0, "status": "completed"}})

# (7) 🔴 python3 -c — synthesis that DID read a real file; classify by COMMAND, not output
PYTHON_C_SYNTHESIS = json.dumps({
    "type": "item.completed",
    "item": {"id": "item_7", "type": "command_execution",
             "command": "/bin/zsh -lc \"python3 -c \\\"print(open('cache.py').read())\\\"\"",
             "aggregated_output": "class LRUCache:\n    def __init__(self, capacity):\n        self.capacity = capacity\n",
             "exit_code": 0, "status": "completed"}})

# (8) 🔴 cat | sed 's///g' — transform stage MUTATES output (capacity→HACKED) → unattributable
TRANSFORM_PIPELINE = json.dumps({
    "type": "item.completed",
    "item": {"id": "item_8", "type": "command_execution",
             "command": "/bin/zsh -lc \"cat cache.py | sed 's/capacity/HACKED/g'\"",
             "aggregated_output": "class LRUCache:\n    def __init__(self, HACKED):\n        self.HACKED = HACKED\n",
             "exit_code": 0, "status": "completed"}})

# (9) rg | head — pipeline search, multi-file path-prefixed output
SEARCH_PIPELINE = json.dumps({
    "type": "item.completed",
    "item": {"id": "item_9", "type": "command_execution",
             "command": "/bin/zsh -lc 'rg -n move_to_end . | head -5'",
             "aggregated_output": "./events.jsonl:3:x\n./cache.py:8:            self.store.move_to_end(key)\n",
             "exit_code": 0, "status": "completed"}})

# (10) turn.completed — usage/cost (no command); parser must skip it
TURN_COMPLETED = json.dumps({
    "type": "turn.completed",
    "usage": {"input_tokens": 22039, "cached_input_tokens": 19712,
              "output_tokens": 253, "reasoning_output_tokens": 52}})

# Named map + a convenience full-stream concatenation.
CODEX_SAMPLES = {
    "rg_dir_presence": RG_DIR_PRESENCE,
    "rg_file_presence": RG_FILE_PRESENCE,
    "rg_absence": RG_ABSENCE,
    "cat_read": CAT_READ,
    "sed_read": SED_READ,
    "echo_fabrication": ECHO_FABRICATION,
    "python_c_synthesis": PYTHON_C_SYNTHESIS,
    "transform_pipeline": TRANSFORM_PIPELINE,
    "search_pipeline": SEARCH_PIPELINE,
    "turn_completed": TURN_COMPLETED,
}


def full_stream(*names: str) -> str:
    """Join named samples (default: all) into one JSONL string, as `--json` emits."""
    keys = names or list(CODEX_SAMPLES)
    return "\n".join(CODEX_SAMPLES[k] for k in keys) + "\n"
