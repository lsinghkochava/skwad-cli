package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

// testLogHandler captures slog records in memory for test assertions.
type testLogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *testLogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *testLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r)
	h.mu.Unlock()
	return nil
}

func (h *testLogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *testLogHandler) WithGroup(_ string) slog.Handler      { return h }

// setDefaultLogger installs h as the slog default for the duration of the test.
func setDefaultLogger(t *testing.T, h slog.Handler) {
	t.Helper()
	orig := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(orig) })
}

// unsetenvClean removes key from the process environment and restores it after the test.
// Use instead of t.Setenv when the key must be absent (not just empty) — godotenv.Load
// skips keys that are present even if empty.
func unsetenvClean(t *testing.T, key string) {
	t.Helper()
	prev, was := os.LookupEnv(key)
	os.Unsetenv(key)
	t.Cleanup(func() {
		if was {
			os.Setenv(key, prev)
		} else {
			os.Unsetenv(key)
		}
	})
}

func TestPersistentPreRunRequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	err := rootCmd.PersistentPreRunE(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY unset, got nil")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error message should mention ANTHROPIC_API_KEY, got: %v", err)
	}
}

func TestPersistentPreRunPassesWithAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	if err := rootCmd.PersistentPreRunE(&cobra.Command{}, nil); err != nil {
		t.Fatalf("expected nil error with key set, got: %v", err)
	}
}

func TestLoadDotenvHappyPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	unsetenvClean(t, "ANTHROPIC_API_KEY")

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("ANTHROPIC_API_KEY=test-key\n"), 0600); err != nil {
		t.Fatal(err)
	}

	loadDotenv()

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "test-key" {
		t.Errorf("expected ANTHROPIC_API_KEY=test-key after loadDotenv, got %q", got)
	}
}

func TestLoadDotenvAbsentFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MY_EXISTING_VAR", "preserved")

	h := &testLogHandler{}
	setDefaultLogger(t, h)

	loadDotenv()

	h.mu.Lock()
	warnCount := 0
	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			warnCount++
		}
	}
	h.mu.Unlock()

	if warnCount != 0 {
		t.Errorf("expected no slog.Warn for absent .env, got %d", warnCount)
	}
	if got := os.Getenv("MY_EXISTING_VAR"); got != "preserved" {
		t.Errorf("existing shell env should be preserved, got %q", got)
	}
}

func TestLoadDotenvShellEnvTakesPrecedence(t *testing.T) {
	// godotenv.Load (not Overload) skips keys already present in the environment —
	// shell value must win over the value in .env.
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("ANTHROPIC_API_KEY", "shell-value")

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("ANTHROPIC_API_KEY=file-value\n"), 0600); err != nil {
		t.Fatal(err)
	}

	loadDotenv()

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "shell-value" {
		t.Errorf("shell env should take precedence over .env: got %q", got)
	}
}

func TestLoadDotenvMalformedFileEmitsWarn(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Unclosed quote produces a parse error from godotenv's string parser.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("KEY=\"unclosed\n"), 0600); err != nil {
		t.Fatal(err)
	}

	h := &testLogHandler{}
	setDefaultLogger(t, h)

	loadDotenv() // must not panic or propagate error to caller

	h.mu.Lock()
	var warnMsgs []string
	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			warnMsgs = append(warnMsgs, r.Message)
		}
	}
	h.mu.Unlock()

	if len(warnMsgs) == 0 {
		t.Fatal("expected slog.Warn for malformed .env, got none")
	}
	if !strings.Contains(warnMsgs[0], ".env") {
		t.Errorf("warn message should mention .env, got %q", warnMsgs[0])
	}
}

func TestPersistentPreRunEFailsWhenDotenvLacksKey(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	unsetenvClean(t, "ANTHROPIC_API_KEY")

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OTHER_VAR=value\n"), 0600); err != nil {
		t.Fatal(err)
	}

	loadDotenv()

	err := rootCmd.PersistentPreRunE(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected hard-gate error when ANTHROPIC_API_KEY absent from both .env and shell")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error should reference ANTHROPIC_API_KEY, got: %v", err)
	}
}
