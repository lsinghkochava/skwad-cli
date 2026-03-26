# Phase 4: Bug Fixes, Cleanup & Polish

## Goal

Fix known bugs, remove dead code, add developer tooling, structured logging, and documentation.

---

## Action Items

### 4.1 — Fix terminal session data race

**Problem**: `internal/terminal/session.go` has a data race between `Kill()` setting `s.exited = true` and `readLoop` goroutine reading it. 4 tests fail with `-race` flag.

**Fix**: Use `atomic.Bool` for the `exited` field, or protect with a mutex. Audit all field accesses in `readLoop`, `waitLoop`, `Kill()`, and `Resize()` for concurrent access.

**Files**: `internal/terminal/session.go`

**Verify**: `go test -race ./internal/terminal/` passes.

**Commit**: `fix: resolve data race in terminal session`

---

### 4.2 — Add exit code capture to Pool

**Problem**: `Pool.OnExit` callback sets status to idle but doesn't expose the real exit code. `skwad run` always reports `exit_code: 0`.

**Fix**:
- Add `ExitCode int` field to `terminal.Session` — set in the `waitLoop` when `cmd.Wait()` returns
- Add `ExitCode(agentID) int` method to `Pool`
- Update `internal/cli/run.go` to read real exit codes after sessions exit

**Files**: `internal/terminal/session.go`, `internal/terminal/pool.go`, `internal/cli/run.go`

**Commit**: `feat: capture real exit codes from agent sessions`

---

### 4.3 — Remove dead VTE/voice stubs

**Problem**: `internal/terminal/vte.go`, `vte_stub.go`, `vte_impl.h` and `internal/voice/service.go` are GUI remnants. Not used by CLI.

**Fix**: Delete them. Run `go build ./...` to confirm nothing breaks.

**Files**: Delete `internal/terminal/vte.go`, `internal/terminal/vte_stub.go`, `internal/terminal/vte_impl.h`, `internal/voice/` directory.

**Commit**: `chore: remove dead vte and voice stubs`

---

### 4.4 — Makefile

**Targets**:
- `make build` — `go build -o skwad-cli ./cmd/skwad-cli/`
- `make install` — `go install ./cmd/skwad-cli/`
- `make test` — `go test ./...`
- `make test-race` — `go test -race ./...`
- `make lint` — `go vet ./...` (and `golangci-lint run` if available)
- `make clean` — remove built binary
- `make help` — list targets

**Files**: `Makefile` (new)

**Commit**: `chore: add Makefile with build, test, and lint targets`

---

### 4.5 — Structured logging with `--verbose`/`--quiet`

**Problem**: No structured logging. Debug output mixed with user-facing output. No way to control verbosity.

**Fix**:
- Add `log/slog` logger initialized in `internal/cli/root.go`
- `--verbose` → `slog.LevelDebug` (show all internal operations)
- Default → `slog.LevelInfo` (show key events: agent spawned, MCP started, etc.)
- `--quiet` → `slog.LevelError` (errors only, clean for CI piping)
- Replace `fmt.Printf` user messages in CLI commands with `slog.Info`/`slog.Debug` where appropriate
- Keep user-facing table output (status, list) as direct stdout writes

**Files**: `internal/cli/root.go`, touch most CLI command files for logging

**Commit**: `feat: add structured logging with verbose and quiet modes`

---

### 4.6 — Signal handling hardening

**Problem**: `skwad start` has basic SIGINT/SIGTERM handling. May not be fully robust for edge cases (double Ctrl+C, child process cleanup).

**Fix**:
- Second SIGINT/SIGTERM during shutdown → force kill immediately
- Ensure all child PTY processes get SIGTERM on daemon shutdown
- Add `context.Context` propagation through Daemon for cancellation
- `skwad run` should also handle signals (currently only `start` does)

**Files**: `internal/cli/start.go`, `internal/cli/run.go`, `internal/daemon/daemon.go`

**Commit**: `feat: harden signal handling with force-kill and context propagation`

---

### 4.7 — README with usage docs

**Content**:
- What skwad-cli is (one paragraph)
- Installation (`go install`, binary download)
- Quick start (`skwad start --team review-team --set repo=.`)
- Commands reference table
- Team config JSON format
- Built-in templates
- macOS export conversion
- CI usage with `skwad run`
- `skwad report` with GitHub PR comments

**Files**: `README.md` (new)

**Commit**: `docs: add README with usage documentation`

---

## Status

- [ ] 4.1 — Fix terminal session data race
- [ ] 4.2 — Add exit code capture to Pool
- [ ] 4.3 — Remove dead VTE/voice stubs
- [ ] 4.4 — Makefile
- [ ] 4.5 — Structured logging with --verbose/--quiet
- [ ] 4.6 — Signal handling hardening
- [ ] 4.7 — README with usage docs
