"""Sandboxed Read/Grep/Glob tools for the OpenAI judge (Route B, #31).

These back the only three tools the judge is allowed to call. Every file access
is hard-scoped to a single root directory (the per-PR checkout): paths are
resolved through ``os.path.realpath`` and rejected if they land outside the
root — defeating ``..`` traversal, absolute-path escapes, and symlinks that
point out of the tree. Walks never follow symlinks and skip ``.git``.

Only ``Read``, ``Grep``, ``Glob`` are exposed (#9 — disallowed tools are
enforced by simply not offering them). The OpenAI function schemas and a
name->method dispatcher live here; the agentic loop that drives them is Phase 4.
"""

import fnmatch
import os
import re
from pathlib import Path

# Caps to keep tool output from blowing the model context window.
MAX_READ_BYTES = 256 * 1024
MAX_GREP_MATCHES = 200
MAX_GLOB_RESULTS = 200

# Directories never descended into during Grep/Glob walks.
_SKIP_DIRS = {".git", "node_modules", ".venv", "__pycache__"}

# The three tools the judge may use; mirrors ALLOWED_JUDGE_TOOLS in judge.py.
ALLOWED_TOOLS = {"Read", "Grep", "Glob"}


class PathEscapeError(ValueError):
    """Raised when a requested path resolves outside the sandbox root.

    Subclasses ValueError so callers can catch the broad family while keeping a
    precise type for the sandbox-escape case (#31).
    """


class RepoTools:
    """Filesystem access hard-scoped to ``root``.

    All public methods take repo-relative (or absolute-but-contained) paths and
    raise PathEscapeError for anything that resolves outside ``root``.
    """

    def __init__(self, root: str):
        # Resolve symlinks on the root itself so containment checks compare
        # real paths to a real root.
        self.root = Path(os.path.realpath(root))
        if not self.root.is_dir():
            raise NotADirectoryError(f"sandbox root is not a directory: {root}")

    def _safe_path(self, rel: str) -> Path:
        """Resolve ``rel`` against the root and verify it stays inside it."""
        if rel is None or rel == "":
            raise PathEscapeError("path is required")
        real = Path(os.path.realpath(self.root / rel))
        if real != self.root and not real.is_relative_to(self.root):
            raise PathEscapeError(f"path escapes sandbox root: {rel!r}")
        return real

    def _rel(self, path: Path) -> str:
        """Path relative to root, for stable display in tool output."""
        return str(path.relative_to(self.root))

    def read(self, path: str, offset: int = 0, limit: int | None = None) -> str:
        """Read a file's contents (line-oriented, like the Claude Read tool).

        ``offset`` is a 0-based starting line; ``limit`` caps the line count.
        Output is byte-capped at MAX_READ_BYTES.
        """
        real = self._safe_path(path)
        if not real.is_file():
            raise FileNotFoundError(f"not a file: {path}")
        data = real.read_bytes()[:MAX_READ_BYTES]
        text = data.decode("utf-8", errors="replace")
        lines = text.splitlines()
        if offset or limit is not None:
            end = None if limit is None else offset + limit
            lines = lines[offset:end]
        return "\n".join(lines)

    def grep(
        self, pattern: str, path: str = ".", glob: str | None = None
    ) -> list[str]:
        """Regex-search files under ``path`` (a dir or single file) within root.

        Optional ``glob`` filters filenames (e.g. ``*.go``). Returns up to
        MAX_GREP_MATCHES ``relpath:lineno:line`` rows; ``[]`` when nothing matches.
        """
        try:
            regex = re.compile(pattern)
        except re.error as exc:
            raise ValueError(f"invalid grep pattern {pattern!r}: {exc}") from exc

        base = self._safe_path(path)
        matches: list[str] = []
        for file_path in self._iter_files(base, glob):
            try:
                content = file_path.read_text(encoding="utf-8", errors="replace")
            except OSError:
                continue
            rel = self._rel(file_path)
            for lineno, line in enumerate(content.splitlines(), start=1):
                if regex.search(line):
                    matches.append(f"{rel}:{lineno}:{line}")
                    if len(matches) >= MAX_GREP_MATCHES:
                        matches.append(
                            f"... (truncated at {MAX_GREP_MATCHES} matches)"
                        )
                        return matches
        return matches

    def glob(self, pattern: str) -> list[str]:
        """List files matching a glob ``pattern`` (recursive ``**`` supported).

        Paths are returned relative to root, sorted, capped at MAX_GLOB_RESULTS;
        ``[]`` when nothing matches.
        """
        results: list[str] = []
        for file_path in self._iter_files(self.root, None):
            rel = self._rel(file_path)
            if fnmatch.fnmatch(rel, pattern) or fnmatch.fnmatch(
                file_path.name, pattern
            ):
                results.append(rel)
        results.sort()
        if len(results) > MAX_GLOB_RESULTS:
            extra = len(results) - MAX_GLOB_RESULTS
            results = results[:MAX_GLOB_RESULTS]
            results.append(f"... (truncated, {extra} more)")
        return results

    def _iter_files(self, base: Path, glob: str | None):
        """Yield files under ``base`` (within root), skipping VCS/vendor dirs and
        never following symlinks out of the tree."""
        if base.is_file():
            if glob is None or fnmatch.fnmatch(base.name, glob):
                yield base
            return
        for dirpath, dirnames, filenames in os.walk(base, followlinks=False):
            dirnames[:] = [d for d in dirnames if d not in _SKIP_DIRS]
            for name in filenames:
                if glob is not None and not fnmatch.fnmatch(name, glob):
                    continue
                candidate = Path(dirpath) / name
                # Guard against symlinked files pointing outside the root.
                real = Path(os.path.realpath(candidate))
                if real != self.root and not real.is_relative_to(self.root):
                    continue
                yield candidate


# OpenAI function-tool schemas — the ONLY tools exposed to the judge (#9).
TOOL_SCHEMAS = [
    {
        "type": "function",
        "function": {
            "name": "Read",
            "description": (
                "Read a file from the repository checkout. Paths are relative to "
                "the repo root. Optionally start at a 0-based line offset and cap "
                "the number of lines returned."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "Repo-relative file path."},
                    "offset": {"type": "integer", "description": "0-based start line."},
                    "limit": {"type": "integer", "description": "Max lines to return."},
                },
                "required": ["path"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "Grep",
            "description": (
                "Regex-search files in the repository checkout. Returns "
                "relpath:lineno:line rows. Optionally restrict to a sub-path and "
                "a filename glob."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "pattern": {"type": "string", "description": "Regular expression."},
                    "path": {"type": "string", "description": "Repo-relative dir/file (default repo root)."},
                    "glob": {"type": "string", "description": "Filename glob filter, e.g. *.go."},
                },
                "required": ["pattern"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "Glob",
            "description": (
                "List files in the repository checkout matching a glob pattern "
                "(supports ** for recursion). Paths are returned relative to root."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "pattern": {"type": "string", "description": "Glob pattern, e.g. **/*.go."},
                },
                "required": ["pattern"],
            },
        },
    },
]


def dispatch_tool_call(tools: RepoTools, name: str, arguments: dict) -> str:
    """Route an OpenAI tool call to the sandbox and return a string result (the
    shape an OpenAI ``role=tool`` message requires). Disallowed names are
    rejected (#9). Tool errors are returned as text so the loop can surface them
    to the model rather than crashing the run (an errored Read is legitimate
    verification, per plan M4)."""
    if name not in ALLOWED_TOOLS:
        return f"ERROR: tool {name!r} is not allowed; use Read, Grep, or Glob."
    try:
        if name == "Read":
            return tools.read(
                arguments["path"],
                offset=arguments.get("offset", 0),
                limit=arguments.get("limit"),
            )
        if name == "Grep":
            rows = tools.grep(
                arguments["pattern"],
                path=arguments.get("path", "."),
                glob=arguments.get("glob"),
            )
            return "\n".join(rows) if rows else "(no matches)"
        # name == "Glob"
        paths = tools.glob(arguments["pattern"])
        return "\n".join(paths) if paths else "(no matches)"
    except (PathEscapeError, FileNotFoundError, ValueError, KeyError) as exc:
        return f"ERROR: {type(exc).__name__}: {exc}"
