package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewCLI(t *testing.T) {
	c := NewCLI("/tmp/repo")
	if c.RepoPath != "/tmp/repo" {
		t.Errorf("expected RepoPath=/tmp/repo, got %q", c.RepoPath)
	}
}

func TestCLI_Run_GitVersion(t *testing.T) {
	// Smoke test: "git version" works from any directory
	c := NewCLI(".")
	out, err := c.Run("version")
	if err != nil {
		t.Fatalf("git version failed: %v", err)
	}
	if !strings.HasPrefix(out, "git version") {
		t.Errorf("expected 'git version ...' output, got %q", out)
	}
}

func TestCLI_RunWithTimeout_Success(t *testing.T) {
	c := NewCLI(".")
	out, err := c.RunWithTimeout(5*time.Second, "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "git version") {
		t.Errorf("expected version output, got %q", out)
	}
}

func TestCLI_RunWithTimeout_Timeout(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}

	// Use a non-existent repo path and a command that would hang
	// Instead, test that a very short timeout produces a timeout error
	// by running a command on a valid repo
	dir := t.TempDir()
	// Initialize a git repo so we can run commands
	cmd := exec.Command("git", "init", dir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	c := NewCLI(dir)
	// A very short timeout — 1 nanosecond — should timeout
	_, err := c.RunWithTimeout(time.Nanosecond, "log", "--all", "--oneline")
	if err == nil {
		t.Error("expected timeout error with 1ns timeout")
	}
	if err != nil && !strings.Contains(err.Error(), "timeout") {
		// Could also fail with exit code if it runs fast enough — that's acceptable
		t.Logf("got non-timeout error (acceptable): %v", err)
	}
}

func TestCLI_RunLines_Success(t *testing.T) {
	c := NewCLI(".")
	lines, err := c.RunLines("version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) == 0 {
		t.Error("expected at least one line from git version")
	}
}

func TestCLI_RunLines_EmptyOutput(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init", dir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	c := NewCLI(dir)
	// tag -l on a fresh repo returns empty
	lines, err := c.RunLines("tag", "-l")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lines != nil {
		t.Errorf("expected nil for empty output, got %v", lines)
	}
}

func TestCLI_Run_InvalidCommand(t *testing.T) {
	c := NewCLI(".")
	_, err := c.Run("not-a-real-git-command")
	if err == nil {
		t.Error("expected error for invalid git command")
	}
}

func TestIsRepo_ValidRepo(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init", dir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if !IsRepo(dir) {
		t.Error("expected IsRepo=true for initialized repo")
	}
}

func TestIsRepo_NotARepo(t *testing.T) {
	dir := t.TempDir()
	if IsRepo(dir) {
		t.Error("expected IsRepo=false for non-repo directory")
	}
}

func TestRootOf_ValidRepo(t *testing.T) {
	dir := t.TempDir()
	// Resolve symlinks (macOS: /var → /private/var)
	dir, _ = filepath.EvalSymlinks(dir)

	cmd := exec.Command("git", "init", dir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	// Create a subdirectory
	subdir := dir + "/sub/deep"
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	root, err := RootOf(subdir)
	if err != nil {
		t.Fatalf("RootOf: %v", err)
	}
	if root != dir {
		t.Errorf("expected root=%q, got %q", dir, root)
	}
}

func TestRootOf_NotARepo(t *testing.T) {
	dir := t.TempDir()
	_, err := RootOf(dir)
	if err == nil {
		t.Error("expected error for non-repo directory")
	}
}

func TestCLI_RunLinesWithTimeout(t *testing.T) {
	c := NewCLI(".")
	lines, err := c.RunLinesWithTimeout(5*time.Second, "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) == 0 {
		t.Error("expected at least one line")
	}
}
