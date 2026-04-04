package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupTestRepo creates a temp git repo with an initial commit on "main".
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-b", "main")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	writeTestFile(t, dir, "README.md", "# test repo\n")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "initial commit")
	return dir
}

// createBranch creates a branch with a single file commit, then checks out main.
func createBranch(t *testing.T, repoDir, branchName, fileName, content string) {
	t.Helper()
	run(t, repoDir, "git", "checkout", "-b", branchName)
	writeTestFile(t, repoDir, fileName, content)
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "add "+fileName)
	run(t, repoDir, "git", "checkout", "main")
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2020-01-01T00:00:00+00:00", "GIT_COMMITTER_DATE=2020-01-01T00:00:00+00:00")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConsolidate_NonConflicting(t *testing.T) {
	repo := setupTestRepo(t)
	createBranch(t, repo, "agent/coder", "coder.go", "package main\n")
	createBranch(t, repo, "agent/tester", "tester.go", "package main\n")

	result, err := Consolidate(repo, "main", []string{"agent/coder", "agent/tester"}, "skwad/consolidate")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.MergedFrom) != 2 {
		t.Errorf("expected 2 merged branches, got %d", len(result.MergedFrom))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped branches, got %d", len(result.Skipped))
	}
	if len(result.CommitHashes) != 2 {
		t.Errorf("expected 2 commit hashes, got %d", len(result.CommitHashes))
	}
}

func TestConsolidate_WithConflict(t *testing.T) {
	repo := setupTestRepo(t)

	// Both branches modify README.md differently — conflict.
	createBranch(t, repo, "agent/a", "README.md", "# from agent a\n")
	createBranch(t, repo, "agent/b", "README.md", "# from agent b\n")
	// A non-conflicting branch.
	createBranch(t, repo, "agent/c", "c.go", "package main\n")

	result, err := Consolidate(repo, "main", []string{"agent/a", "agent/b", "agent/c"}, "skwad/consolidate")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// agent/a merges first (sorted), agent/b conflicts with a, agent/c is clean.
	if len(result.MergedFrom) != 2 {
		t.Errorf("expected 2 merged (a + c), got %d: %v", len(result.MergedFrom), result.MergedFrom)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped (b), got %d: %v", len(result.Skipped), result.Skipped)
	}
	if _, ok := result.ConflictDetails["agent/b"]; !ok {
		t.Error("expected conflict details for agent/b")
	}
}

func TestConsolidate_EmptyBranch(t *testing.T) {
	repo := setupTestRepo(t)

	// Create a branch with no changes (just a branch point).
	run(t, repo, "git", "branch", "agent/empty")
	// And a branch with actual changes.
	createBranch(t, repo, "agent/real", "real.go", "package main\n")

	result, err := Consolidate(repo, "main", []string{"agent/empty", "agent/real"}, "skwad/consolidate")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Empty branch merge is a no-op but still succeeds.
	if len(result.MergedFrom) != 2 {
		t.Errorf("expected 2 merged, got %d: %v", len(result.MergedFrom), result.MergedFrom)
	}
}

func TestConsolidate_BranchAlreadyExists(t *testing.T) {
	repo := setupTestRepo(t)
	run(t, repo, "git", "branch", "skwad/consolidate")

	_, err := Consolidate(repo, "main", []string{}, "skwad/consolidate")
	if err == nil {
		t.Error("expected error when consolidation branch already exists")
	}
}

func TestConsolidate_MainCheckoutUntouched(t *testing.T) {
	repo := setupTestRepo(t)
	createBranch(t, repo, "agent/work", "work.go", "package main\n")

	// Record main HEAD before consolidation.
	cli := NewCLI(repo)
	headBefore, _ := cli.Run("rev-parse", "HEAD")

	_, err := Consolidate(repo, "main", []string{"agent/work"}, "skwad/consolidate")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Main HEAD should be unchanged.
	headAfter, _ := cli.Run("rev-parse", "HEAD")
	if headBefore != headAfter {
		t.Errorf("main HEAD changed: %s → %s", headBefore, headAfter)
	}

	// work.go should NOT exist in main checkout.
	if _, err := os.Stat(filepath.Join(repo, "work.go")); err == nil {
		t.Error("work.go should not exist in main checkout after consolidation")
	}
}
